package engine

// The stored-schema dialect gate. A repository's stored declaration rows speak
// exactly one DIALECT — a per-repository monotonic integer stamped by the
// binary when the repository opens. The gate does two things and no more:
//
//   - a binary whose maximum dialect is BELOW the stored one REFUSES the open
//     with ErrVocabularyDialectNewer, rather than misreading rows it does not
//     understand. The refusal surfaces like any temporarily-unavailable
//     repository: Authenticate passes it through un-wrapped (it is not an auth
//     failure) and the API maps it to `503` with Retry-After;
//   - an older store is promoted step by step, each step recorded in
//     vocabulary_promotions before the dialect is stamped.
//
// THE LADDER IS EMPTY. v1 is dialect 1 and it is the first stored shape that
// has ever existed: the substrate boots on a fresh database, so there is no
// older store to repair and no promotion to run. The mechanism stays because
// it is what makes a FUTURE shape change safe — a new dialect appends a step
// and bumps maxVocabularyDialect — but nothing in it borrows the shipped tree, and
// a repository's vocabulary is only ever its own rows (seed.go).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
)

// ErrVocabularyDialectNewer is the named downgrade refusal: the repository's store
// speaks a schema dialect this binary does not know. Opening anyway would
// silently drop or misread declaration rows, so the open refuses instead —
// deploy a substrate whose maximum dialect covers the store.
var ErrVocabularyDialectNewer = errors.New("substrate/engine: the store speaks a newer schema dialect than this binary")

// dialectStep is one recorded promotion N-1 → N of the stored declaration
// rows. Steps stay content-gated and idempotent (a step that finds nothing to
// do is free), so a crash between a step's own transactions and the stamp
// below re-runs it harmlessly at the next open.
type dialectStep struct {
	dialect int
	name    string
	run     func(*dataset, context.Context) error
}

// dialectSteps is the ladder: empty at v1 (see the file comment). A new
// dialect appends a step here and bumps maxVocabularyDialect; steps are never
// deleted or reordered once they ship, since vocabulary_promotions records which
// repositories passed which one.
var dialectSteps []dialectStep

// maxVocabularyDialect is the newest dialect this binary speaks. A fresh
// repository is stamped here at its first open; a repository stored above it
// refuses to open.
const maxVocabularyDialect = 1

// MaxSchemaDialect is the newest dialect this binary speaks — the value GET
// /api discovery reports as the binary maximum. Exported so the
// API layer can surface it without coupling to the ladder internals.
func MaxSchemaDialect() int { return maxVocabularyDialect }

// dialectLockClass is the advisory-lock classid the gate serializes under (the
// objid is a hash of the repository id, so two processes opening the SAME
// repository queue while different repositories proceed).
const dialectLockClass int32 = 0x53444c31 // "SDL1"

// promoteSchemaDialect runs the gate at repository open: refuse a store newer
// than the binary, promote an older one step by step, then stamp.
func (ds *dataset) promoteSchemaDialect(ctx context.Context) error {
	// The lock lives on one pinned connection (releasing from another pooled
	// connection is a silent no-op — the DDL runner's lesson) and is held
	// across the whole ladder; the steps' own transactions ride the pool.
	conn, err := ds.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: dialect lock conn: %w", err)
	}
	defer func() { _ = conn.Close() }()
	h := fnv.New32a()
	_, _ = h.Write([]byte(ds.scope.Repository))
	objid := int32(h.Sum32())
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, dialectLockClass, objid); err != nil {
		return fmt.Errorf("substrate/engine: dialect lock: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1, $2)`, dialectLockClass, objid) }()

	stored, err := ds.storedSchemaDialect(ctx)
	if err != nil {
		return err
	}
	if stored > maxVocabularyDialect {
		return fmt.Errorf("%w: repository %s stores dialect %d, this binary supports <= %d — upgrade the substrate",
			ErrVocabularyDialectNewer, ds.info.Name, stored, maxVocabularyDialect)
	}
	if stored == maxVocabularyDialect {
		return nil
	}
	for _, step := range dialectSteps {
		if step.dialect <= stored {
			continue
		}
		if err := step.run(ds, ctx); err != nil {
			return fmt.Errorf("substrate/engine: schema dialect step %d (%s): %w", step.dialect, step.name, err)
		}
		if err := ds.recordDialectStep(ctx, step.dialect, step.name); err != nil {
			return err
		}
	}
	// An empty ladder still stamps: the dialect is the store's SHAPE, not a
	// count of promotions run, so a fresh repository leaves its first open at
	// the binary's maximum and never re-enters this path.
	return ds.recordDialectStep(ctx, maxVocabularyDialect, "v1")
}

// storedSchemaDialect reads the repository's stored dialect; an absent row is
// 0, a store that has never been stamped.
func (ds *dataset) storedSchemaDialect(ctx context.Context) (int, error) {
	var d int
	err := ds.db.QueryRowContext(ctx, `SELECT dialect FROM vocabulary_dialect`).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: read schema dialect: %w", err)
	}
	return d, nil
}

// recordDialectStep records one completed step and stamps the dialect, in one
// transaction. The stamp only ever moves up (GREATEST), so a stale racer can
// never wind the dialect back.
func (ds *dataset) recordDialectStep(ctx context.Context, dialect int, name string) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO vocabulary_promotions (dialect, name) VALUES ($1, $2)
		ON CONFLICT (repository, dialect) DO NOTHING`, dialect, name); err != nil {
		return fmt.Errorf("substrate/engine: record dialect step %d: %w", dialect, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO vocabulary_dialect (dialect) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE
		SET dialect = GREATEST(vocabulary_dialect.dialect, EXCLUDED.dialect), updated_at = now()`,
		dialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp dialect %d: %w", dialect, err)
	}
	return tx.Commit()
}
