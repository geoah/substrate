package engine

// The stored-schema dialect gate. A repository's stored declaration rows speak
// exactly one DIALECT — a per-repository monotonic integer stamped by the
// binary when the repository opens. The gate refuses, in both directions, and
// promotes nothing:
//
//   - a binary whose maximum dialect is BELOW the stored one REFUSES the open
//     with ErrVocabularyDialectNewer, rather than misreading rows it does not
//     understand. The refusal surfaces like any temporarily-unavailable
//     repository: Authenticate passes it through un-wrapped (it is not an auth
//     failure) and the API maps it to `503` with Retry-After;
//   - a store still holding dialect 1's `definition` blob REFUSES the open with
//     ErrDeclarationUntranslated, because no rung translates it any more.
//
// THE LADDER IS EMPTY, and dialect 3 is where it starts. Dialect 1 stored a
// declaration's content in a `definition` json blob and dialect 2 stored the
// declaration's own properties, both under a two-segment kind reference;
// dialect 3 is the same rows under `{authority}/{package}/{name}` (decision
// record 0047). Neither rung exists: no release ever produced a dialect-1
// store, and nothing translates a dialect-2 one, because every stored kind
// string would have to be rewritten and no package name can be invented for
// it. The standing answer for a development store below 3 is
// `mise run dev:wipe`.
//
// A NEW DIALECT ADDS A STEP BACK. dialectStep and the loop that ran it are gone
// with the rungs, so dialect 4 reintroduces both; what stays is the shape they
// need — a step is content-gated and idempotent, records itself in
// vocabulary_promotions, and stamps inside its own transaction where the
// rewrite and the stamp must be indivisible.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// ErrVocabularyDialectNewer is the named downgrade refusal: the repository's store
// speaks a schema dialect this binary does not know. Opening anyway would
// silently drop or misread declaration rows, so the open refuses instead —
// deploy a substrate whose maximum dialect covers the store.
var ErrVocabularyDialectNewer = errors.New("substrate/engine: the store speaks a newer schema dialect than this binary")

// ErrDeclarationUntranslated is the upward refusal: a stored declaration row in
// a shape this binary cannot read. It is what a `definition` blob meets, at the
// gate below and at every later read (rowDocument), and the two agree on
// purpose — a store the gate stamped and the reader then refused would carry a
// stamp that lies about its own rows.
var ErrDeclarationUntranslated = errors.New("substrate/engine: a stored declaration is in a shape this binary cannot read")

// dialectPackagedKinds is dialect 3's name in vocabulary_promotions.
const dialectPackagedKinds = "packaged-kinds"

// maxVocabularyDialect is the newest dialect this binary speaks. A fresh
// repository is stamped here at its first open; a repository stored above it
// refuses to open, and one stored below it cannot be read at all.
const maxVocabularyDialect = 3

// MaxSchemaDialect is the newest dialect this binary speaks — the value GET
// /api discovery reports as the binary maximum. Exported so the
// API layer can surface it without coupling to the ladder internals.
func MaxSchemaDialect() int { return maxVocabularyDialect }

// dialectLockClass is the advisory-lock classid the gate serializes under (the
// objid is a hash of the repository id, so two processes opening the SAME
// repository queue while different repositories proceed).
const dialectLockClass int32 = 0x53444c31 // "SDL1"

// promoteSchemaDialect runs the gate at repository open: refuse a store newer
// than the binary, refuse an older one this binary cannot read, then stamp.
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
	// BELOW THE MAXIMUM AND NOT PROMOTABLE. An unstamped store is a fresh
	// repository, and a STAMPED one below the maximum is a store this binary
	// cannot read: dialect 2 spelled every kind `{authority}/{name}`, and
	// nothing can invent the package segment the reader needs. The refusal is
	// the whole handling.
	if stored > 0 {
		return fmt.Errorf("%w: repository %s stores dialect %d, whose kind references carry no package segment (decision record 0047); no rung translates them, so wipe the store: mise run dev:wipe in development, and restore a dump taken before the upgrade anywhere else",
			ErrDeclarationUntranslated, ds.info.Name, stored)
	}
	// The dialect-1 shape is refused by its own tell, before the stamp: the
	// stamp is durable and one-way, and stamping a store whose rows the reader
	// then refuses (rowDocument) would leave a mark that lies about them.
	if left, err := ds.definitionBearingRows(ctx); err != nil {
		return err
	} else if len(left) > 0 {
		return fmt.Errorf("%w: repository %s: %d declaration row(s) carry a `%s` blob, which dialect 1 stored and no release ever produced; there is no rung that translates it (#217), so wipe the store: %s",
			ErrDeclarationUntranslated, ds.info.Name, len(left), propDeclarationBlob, strings.Join(left, ", "))
	}
	// A gate that promoted nothing still stamps: the dialect is the store's
	// SHAPE, not a count of promotions run, so a fresh repository leaves its
	// first open at the binary's maximum and never re-enters this path.
	return ds.recordDialectStep(ctx, maxVocabularyDialect, dialectPackagedKinds)
}

// definitionBearingRows lists the live declaration rows that still carry a
// `definition` property — the dialect-1 shape — as the ids the refusal names.
// It runs on the dataset's pool rather than in a transaction: the gate reads it
// before anything in the open has written.
func (ds *dataset) definitionBearingRows(ctx context.Context) ([]string, error) {
	args := make([]any, 0, len(vocabularyKindRefs))
	ph := make([]string, 0, len(vocabularyKindRefs))
	for i, ident := range vocabularyKindRefs {
		args = append(args, ident)
		ph = append(ph, "$"+strconv.Itoa(i+1))
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, id FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL
		  AND props ? '`+propDeclarationBlob+`'
		ORDER BY kind, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the dialect-1 rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, err
		}
		out = append(out, kind+" "+id)
	}
	return out, rows.Err()
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
