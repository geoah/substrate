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
// THE LADDER HAS TWO RUNGS. Dialect 1 was the changelog while edges existed:
// `link`/`unlink` ops and `edge`/`unedge`/`edge1` fold effects. Dialect 2 is the
// changelog as it stands, after references absorbed the edge (decision 0044): those
// five spellings are gone and this binary refuses any entry carrying one
// (fold.go foldRefuses), because a reference's meaning now lives in the source
// record's own properties, which no such entry carries.
//
// A STORE BELOW THE MAXIMUM IS PROBED, NOT ASSUMED. Migration 0010 drops the
// edges table, so a store whose changelog holds `link`/`unlink` entries has
// already lost the rows those entries fold into: opening it would serve a
// repository whose links are gone and whose history no binary can replay
// (fold.go foldRefuses). Decision 0044 says such a store refuses to OPEN, so
// the gate asks the changelog directly for a retired op and refuses there,
// rather than leaving the discovery to the day somebody rebuilds. The probe is
// one indexed-free `LIMIT 1` on a store that is below the maximum, which after
// this release is only ever a fresh, unstamped one.
//
// A change that teaches the writer a spelling an older binary's fold would
// refuse or misread (a new fold effect kind, a new op, a payload shape an old
// decoder reads differently) bumps maxChangelogDialect in the same commit, and
// changelogdialect_internal_test.go is what makes the first two say so: it
// reads the declared ops and effect kinds and fails on any the rung does not
// list.

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

// ErrChangelogPredatesReferences is the upgrade-side refusal: the repository's
// changelog still holds the `link`/`unlink` ops that dialect 1 wrote, and the
// edges those entries fold into no longer exist (migration 0010 drops the
// table). There is no rung that translates them — a reference's meaning lives in
// the source record's own properties, which no such entry carries — so the open
// refuses instead of serving a repository whose links are silently gone.
var ErrChangelogPredatesReferences = errors.New("substrate/engine: the changelog predates reference-only links")

// maxChangelogDialect is the newest changelog dialect this binary can replay.
// It is what this binary stamps when it appends; a repository stored above it
// refuses to open.
const maxChangelogDialect = 2

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
	stored, err := readChangelogDialect(ctx, ds.db)
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
		return nil
	}
	return ds.refuseRetiredLinkEntries(ctx)
}

// refuseRetiredLinkEntries refuses a changelog that still holds a dialect-1
// `link` or `unlink` entry. It only READS, like the rest of the gate, and it
// runs on the dataset's pool inside the repository's scope, so the statement
// sees this repository's changelog and no other.
func (ds *dataset) refuseRetiredLinkEntries(ctx context.Context) error {
	var one int
	err := ds.db.QueryRowContext(ctx,
		`SELECT 1 FROM changelog WHERE op IN ($1, $2) LIMIT 1`, opLinkRetired, opUnlinkRetired).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("substrate/engine: probe retired link entries: %w", err)
	}
	return fmt.Errorf("%w: repository %s holds `%s`/`%s` changelog entries, which dialect 1 wrote and migration 0010 left nothing to fold into; there is no rung that translates them (decision 0044), so wipe the store: mise run dev:wipe in development, and restore a dump taken before the upgrade anywhere else",
		ErrChangelogPredatesReferences, ds.info.Name, opLinkRetired, opUnlinkRetired)
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
	if stored > maxChangelogDialect {
		return newerChangelogDialect(t.ds.info.Name, stored)
	}
	return nil
}

func newerChangelogDialect(repository string, stored int) error {
	return fmt.Errorf("%w: repository %s stores changelog dialect %d, this binary replays <= %d: upgrade the substrate",
		ErrChangelogDialectNewer, repository, stored, maxChangelogDialect)
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
