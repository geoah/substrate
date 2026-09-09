package engine

// The changelog dialect gate. A repository's changelog is written in exactly
// one DIALECT (a per-repository monotonic integer naming the ops and fold
// effects a binary must understand to replay the history), and this gate is
// where a binary that does not understand it says so.
//
// It answers the half of the downgrade question dialect.go does not. That gate
// governs stored DECLARATION rows; nothing governed the entries. A binary
// meeting a fold effect it does not know refuses at the fold (fold.go: foldOne
// on an unknown effect, foldRefuses on a merge with no resync), which is
// correct and far too late: the store opens, serves and appends for weeks, and
// the refusal surfaces on the day somebody rebuilds, the one day they needed
// the changelog to be replayable.
//
// THE STAMP RIDES THE APPEND. Opening READS the stamp and refuses a newer one;
// the stamp itself is written by the first transaction this binary appends to
// the changelog (settleChecksums), in that transaction, so it is exactly as
// durable as the first entry it claims. The invariant is then tight in both
// directions: no entry exists that the stamp does not cover, and no store is
// barred over entries nobody wrote. An open that fails before it appends, the
// case an operator rolls the image back for, leaves the stamp alone; an open
// whose vocabulary upgrade committed has already written entries, and barring
// the older binary from those is the gate working.
//
// The stamp is the WRITER's maximum, not a minimum derived from the entries.
// A second rung could derive one (an entry's ops and effects say what a
// replayer needs), and the stamped column is what makes that refinement
// possible later without guessing at unstamped history.
//
// DIALECT 1 IS THE ONLY RUNG: the ops, fold effects and entry frame this
// binary writes. A change that teaches the writer a spelling an older binary's
// fold would refuse or misread (a new fold effect kind, a new op, a payload
// shape an old decoder reads differently) bumps maxChangelogDialect in the
// same commit, and changelogdialect_internal_test.go is what makes the first
// two say so: it reads the declared ops and effect kinds and fails on any the
// rung does not list.

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
// this step must leave the store exactly as it found it.
func (ds *dataset) gateChangelogDialect(ctx context.Context) error {
	stored, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		return err
	}
	if err := admitChangelogDialect(ds.info.ID, stored, maxChangelogDialect); err != nil {
		return err
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
	stored, err := readChangelogDialect(t.ctx, t.tx)
	if err != nil {
		return err
	}
	return admitChangelogDialect(t.ds.info.ID, stored, maxChangelogDialect)
}

// admitChangelogDialect is the one comparison every gate makes: a repository
// stamped above the binary's maximum is refused, everything at or below it is
// admitted. It takes the maximum as a parameter so a test can hold a binary
// whose maximum is 1 to a repository stamped 2.
func admitChangelogDialect(repository string, stored, max int) error {
	if stored > max {
		return fmt.Errorf("%w: repository %s stores changelog dialect %d, this binary replays <= %d: upgrade the substrate",
			ErrChangelogDialectNewer, repository, stored, max)
	}
	return nil
}

func newerChangelogDialect(repository string, stored int) error {
	return admitChangelogDialect(repository, stored, maxChangelogDialect)
}

// readChangelogDialect reads the repository's stamp through a pool or a
// transaction; an absent row is 0, a changelog no binary has claimed yet.
func readChangelogDialect(ctx context.Context, q dbx) (int, error) {
	var d int
	err := q.QueryRowContext(ctx, `SELECT dialect FROM changelog_dialect`).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: read changelog dialect: %w", err)
	}
	return d, nil
}

// changelogDialectStamp only ever moves the claim up (GREATEST), so an older
// binary racing a newer one can never wind the changelog's claim back to a
// number the history has already outgrown.
const changelogDialectStamp = `
	INSERT INTO changelog_dialect (dialect) VALUES ($1)
	ON CONFLICT (repository) DO UPDATE
	SET dialect = GREATEST(changelog_dialect.dialect, EXCLUDED.dialect), updated_at = now()`

// stampChangelogDialect claims the dialect from inside a transaction that is
// APPENDING (settleChecksums calls it, so creation's seed and every later write
// run through the same place). The claim is written by the transaction that
// writes the entries, so it commits with them or not at all, and the dataset
// only remembers it AFTER that commit: a flag set on a rolled-back stamp would
// let the next append land with nothing claiming it. The transaction is
// marked as the claimant, so commitAndMirror rewrites the directory's
// manifest with the claim BEFORE it appends the entries (repodir.go
// writeManifestBeforeCommit): the manifest is what an import reads for the
// dialect, so it must say what the segments require before they require it.
func (t *txn) stampChangelogDialect() error {
	if t.ds.changelogStamped.Load() {
		return nil
	}
	if _, err := t.exec(changelogDialectStamp, maxChangelogDialect); err != nil {
		return fmt.Errorf("substrate/engine: stamp changelog dialect %d: %w", maxChangelogDialect, err)
	}
	t.claimsChangelogDialect = true
	t.afterCommit = append(t.afterCommit, func() { t.ds.changelogStamped.Store(true) })
	return nil
}
