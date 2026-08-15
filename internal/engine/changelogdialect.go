package engine

// The changelog dialect gate. A repository's changelog is written in exactly
// one DIALECT (a per-repository monotonic integer naming the ops and fold
// effects a binary must understand to replay the history), and this gate is
// where a binary that does not understand it says so.
//
// It answers the half of the downgrade question dialect.go does not. That
// ladder governs stored DECLARATION rows; nothing governed the entries. A
// binary meeting a fold effect it does not know refuses at the fold (fold.go:
// foldOne on an unknown effect, foldRefuses on a merge with no resync), which
// is correct and far too late: the store opens, serves and appends for weeks,
// and the refusal surfaces on the day somebody rebuilds, the one day they
// needed the changelog to be replayable.
//
// THE STAMP IS TAKEN BEFORE ANYTHING WRITES: in the transaction that creates
// the repository, and at every open ahead of the steps that append. That
// ordering is the whole guarantee. The stamp is at least the dialect of every
// entry in the history, because no binary writes an entry into a repository it
// has not first claimed it can replay, and a stamp that lagged its writer
// would promise a replayability the store does not have.
//
// The cost is deliberate and worth naming: once a newer binary opens a
// repository, an older one refuses it even if not one new-spelling entry was
// ever appended. The alternative, raising the stamp only when an entry
// actually uses the new spelling, makes the refusal depend on which entries
// happen to have been written, so a downgrade is safe until the moment it
// silently is not. Refusing on the binary that touched it is the reading an
// operator can act on.
//
// THE LADDER HAS ONE RUNG. Dialect 1 is the changelog as it stands: the ops in
// substrate.Change and the fold effects in fold.go. A change that teaches the
// writer a spelling an older binary's fold would refuse or misread (a new fold
// effect kind, a new op, a payload shape an old decoder reads differently)
// bumps maxChangelogDialect in the same commit.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrChangelogDialectNewer is the named downgrade refusal: the repository's
// changelog is written in a dialect this binary cannot replay. Opening anyway
// would serve a repository whose history this binary can neither rebuild nor
// safely extend, so the open refuses instead; deploy a substrate whose maximum
// changelog dialect covers the store. Like the vocabulary refusal it is a
// service condition, not an auth failure: Authenticate passes it through
// un-wrapped and the API maps it to `503` with Retry-After.
var ErrChangelogDialectNewer = errors.New("substrate/engine: the changelog speaks a newer dialect than this binary can replay")

// maxChangelogDialect is the newest changelog dialect this binary can replay.
// A fresh repository is stamped here when it is created; a repository stored
// above it refuses to open.
const maxChangelogDialect = 1

// MaxChangelogDialect is the newest changelog dialect this binary can replay,
// the value GET /.well-known/substrate/server.json reports as the binary
// maximum. Exported so the API layer can surface it without reaching into the
// engine's tables.
func MaxChangelogDialect() int { return maxChangelogDialect }

// gateChangelogDialect runs the gate at repository open: refuse a changelog
// this binary cannot replay, and stamp an unstamped or older one.
//
// There is no advisory lock and no promotion step, unlike the vocabulary
// ladder: nothing rewrites history, so the whole gate is one read and one
// upsert, and two processes opening the same repository at once both stamp the
// same number through the same GREATEST.
func (ds *dataset) gateChangelogDialect(ctx context.Context) error {
	stored, err := ds.storedChangelogDialect(ctx)
	if err != nil {
		return err
	}
	if stored > maxChangelogDialect {
		return newerChangelogDialect(ds.info.Name, stored)
	}
	if stored == maxChangelogDialect {
		return nil
	}
	return ds.stampChangelogDialect(ctx, maxChangelogDialect)
}

// refuseNewerChangelogDialect re-reads the stamp inside the caller's
// transaction, for a replay that must not run on a stale reading. The open-time
// gate cannot cover this on its own: a process holding an OPEN dataset never
// runs it again, so a rebuild started after another process raised the stamp
// would fold entries in a spelling this binary does not know. The deployment
// is one writer process (#159), and this is the brace to that belt on the one
// operation whose whole job is to interpret history.
func (t *txn) refuseNewerChangelogDialect() error {
	var stored int
	switch err := t.row(`SELECT dialect FROM changelog_dialect`).Scan(&stored); {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("substrate/engine: read changelog dialect: %w", err)
	case stored > maxChangelogDialect:
		return newerChangelogDialect(t.ds.info.Name, stored)
	}
	return nil
}

func newerChangelogDialect(repository string, stored int) error {
	return fmt.Errorf("%w: repository %s stores changelog dialect %d, this binary replays <= %d: upgrade the substrate",
		ErrChangelogDialectNewer, repository, stored, maxChangelogDialect)
}

// storedChangelogDialect reads the repository's stamp; an absent row is 0, a
// changelog no binary has claimed yet.
func (ds *dataset) storedChangelogDialect(ctx context.Context) (int, error) {
	var d int
	err := ds.db.QueryRowContext(ctx, `SELECT dialect FROM changelog_dialect`).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: read changelog dialect: %w", err)
	}
	return d, nil
}

// changelogDialectStamp is the upsert both stampers run. It only ever moves up
// (GREATEST), so an older binary racing a newer one can never wind the
// changelog's claim back to a number the history has already outgrown.
const changelogDialectStamp = `
	INSERT INTO changelog_dialect (dialect) VALUES ($1)
	ON CONFLICT (repository) DO UPDATE
	SET dialect = GREATEST(changelog_dialect.dialect, EXCLUDED.dialect), updated_at = now()`

func (ds *dataset) stampChangelogDialect(ctx context.Context, dialect int) error {
	if _, err := ds.db.ExecContext(ctx, changelogDialectStamp, dialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp changelog dialect %d: %w", dialect, err)
	}
	return nil
}

// stampChangelogDialect stamps inside the caller's transaction: creation writes
// a repository's first entries (the seed) without ever going through the open
// path, so the claim about their dialect is written by the same transaction
// that writes them, and a seed that rolls back takes its stamp with it.
func (t *txn) stampChangelogDialect(dialect int) error {
	if _, err := t.exec(changelogDialectStamp, dialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp changelog dialect %d: %w", dialect, err)
	}
	return nil
}
