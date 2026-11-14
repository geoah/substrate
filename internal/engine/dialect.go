package engine

// The stored-schema dialect gate. A repository's stored declaration rows speak
// exactly one DIALECT — a per-repository monotonic integer stamped by the
// binary when the repository opens. The gate refuses in one direction and
// translates nothing: a binary whose maximum dialect is BELOW the stored one
// REFUSES the open with ErrVocabularyDialectNewer, rather than misreading rows
// it does not understand. The refusal surfaces like any temporarily-unavailable
// repository: Authenticate passes it through un-wrapped (it is not an auth
// failure) and the API maps it to `503` with Retry-After.
//
// There is no ladder and nothing to promote: a dialect-1 row is a dialect-2
// row. Dialect 2 marks the seeded closure that brought `oneOf` reference
// properties and the `quantity` type, which the v0.113.0 loader refuses, and
// dialect 3 the package header's `stability`, `interface` and `dialect`. That
// binary reads the manifest's dialect only when it imports a directory its
// database has no repository row for, so the stamp refuses that import by
// name and nothing else; a database restored with the row still in it imports
// without reading the manifest, and the restore-together text in the
// ErrDatabaseNewer refusal covers that case. A dialect that DOES change a
// row's shape adds a promotion step back, and the shape such a
// step needs is the one this file's stamp already has: content-gated,
// idempotent, and stamping inside the transaction that does the rewrite where
// the two must be indivisible.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// ErrVocabularyDialectNewer is the named downgrade refusal: the repository's store
// speaks a schema dialect this binary does not know. Opening anyway would
// silently drop or misread declaration rows, so the open refuses instead —
// deploy a substrate whose maximum dialect covers the store.
var ErrVocabularyDialectNewer = errors.New("substrate/engine: the store speaks a newer schema dialect than this binary")

// maxVocabularyDialect is the newest dialect this binary speaks: the
// loader's (vocabulary.Dialect, which a new declaration key raises and which
// discovery publishes as `vocabulary.dialect`, decision 0268). A repository
// is stamped here by its creation, one stored lower is raised here at its
// first open, and one stored above it refuses to open.
const maxVocabularyDialect = vocabulary.Dialect

// MaxSchemaDialect is the newest dialect this binary speaks, exported for the
// tests outside this package that assert what an open stamps and which
// manifest the gate refuses.
func MaxSchemaDialect() int { return maxVocabularyDialect }

// gateVocabularyDialect runs the gate at repository open: refuse a store newer
// than this binary, then stamp an unstamped one. The stamp is the store's
// SHAPE, not a count of promotions run, so a fresh repository leaves its first
// open at the binary's maximum and never writes here again.
func (ds *dataset) gateVocabularyDialect(ctx context.Context) error {
	stored, err := readVocabularyDialect(ctx, ds.db)
	if err != nil {
		return err
	}
	if err := admitVocabularyDialect(ds.info.ID, stored); err != nil {
		return err
	}
	if stored == maxVocabularyDialect {
		return nil
	}
	if _, err := ds.db.ExecContext(ctx, vocabularyDialectStamp, maxVocabularyDialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp vocabulary dialect %d: %w", maxVocabularyDialect, err)
	}
	return nil
}

// admitVocabularyDialect is the one comparison every gate over the stored
// declarations makes, at open (gateVocabularyDialect) and before a boot import
// writes a row (repodir.go importRepositoryDir): a store stamped above the
// binary's maximum is refused, everything at or below it is admitted.
func admitVocabularyDialect(repository string, stored int) error {
	if stored > maxVocabularyDialect {
		return fmt.Errorf("%w: repository %s stores dialect %d, this binary supports <= %d — upgrade the substrate",
			ErrVocabularyDialectNewer, repository, stored, maxVocabularyDialect)
	}
	return nil
}

// readVocabularyDialect reads the repository's stored dialect through a pool
// or a transaction; an absent row is 0, a store that has never been stamped.
func readVocabularyDialect(ctx context.Context, q dbx) (int, error) {
	var d int
	err := q.QueryRowContext(ctx, `SELECT dialect FROM vocabulary_dialect`).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: read schema dialect: %w", err)
	}
	return d, nil
}

// vocabularyDialectStamp only ever moves the stamp up (GREATEST), so a stale
// racer can never wind the dialect back. The gate runs it at open; a boot
// import runs it with the dialect the manifest recorded.
const vocabularyDialectStamp = `
	INSERT INTO vocabulary_dialect (dialect) VALUES ($1)
	ON CONFLICT (repository) DO UPDATE
	SET dialect = GREATEST(vocabulary_dialect.dialect, EXCLUDED.dialect), updated_at = now()`
