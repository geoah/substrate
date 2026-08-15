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
// THE STAMP RIDES THE APPEND. Opening READS the stamp and refuses a newer one;
// the stamp itself is written by the first transaction this binary appends to
// the changelog (settleChain), in that transaction, so it is exactly as
// durable as the first entry it claims. The invariant is then tight in both
// directions: no entry exists that the stamp does not cover, and no store is
// barred over entries nobody wrote. An open that fails halfway, the case an
// operator rolls the image back for, leaves the stamp alone.
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
// It is what this binary stamps when it appends; a repository stored above it
// refuses to open.
const maxChangelogDialect = 1

// MaxChangelogDialect is the newest changelog dialect this binary can replay,
// the value GET /.well-known/substrate/server.json reports as the binary
// maximum. Exported so the API layer can surface it without reaching into the
// engine's tables.
func MaxChangelogDialect() int { return maxChangelogDialect }

// gateChangelogDialect runs the gate at repository open, and it only READS:
// refuse a changelog this binary cannot replay, write nothing. An open has no
// business claiming history it may never add to, and an open that fails after
// this step must leave the store exactly as an older binary would find it.
//
// There is no advisory lock and no promotion step, unlike the vocabulary
// ladder: nothing rewrites history, so the gate is one query.
func (ds *dataset) gateChangelogDialect(ctx context.Context) error {
	stored, err := ds.storedChangelogDialect(ctx)
	if err != nil {
		return err
	}
	if stored > maxChangelogDialect {
		return newerChangelogDialect(ds.info.Name, stored)
	}
	// A store already at this binary's maximum needs no stamp from it, which
	// is the common case: remember that, so the first append does not run the
	// upsert for a claim already on the row.
	if stored == maxChangelogDialect {
		ds.changelogStamped.Store(true)
	}
	return nil
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

// stampChangelogDialect claims the dialect from inside a transaction that is
// APPENDING (settleChain calls it, so creation's seed and every later write
// run through the same place). The claim is written by the transaction that
// writes the entries, so it commits with them or not at all, and the dataset
// only remembers it AFTER that commit: a flag set on a rolled-back stamp would
// let the next append land with nothing claiming it.
func (t *txn) stampChangelogDialect() error {
	if t.ds.changelogStamped.Load() {
		return nil
	}
	if _, err := t.exec(changelogDialectStamp, maxChangelogDialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp changelog dialect %d: %w", maxChangelogDialect, err)
	}
	t.afterCommit = append(t.afterCommit, func() { t.ds.changelogStamped.Store(true) })
	return nil
}
