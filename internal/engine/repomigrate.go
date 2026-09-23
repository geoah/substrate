package engine

// REPOSITORY MIGRATIONS: code the boot runs ONCE per repository and records
// (decision record 0099).
//
// A schema migration (migrate.go) changes the tables and runs once per
// database. What it cannot do is decide something per repository from the
// rows: which kind a stored `kind: person` meant, which of two copies of a
// package a reference should follow. That is code, over one repository's
// rows, and this file is where it runs.
//
// The shape mirrors schema_migrations on purpose, because an operator already
// knows it. Each migration has a version and a name; the runner reads the
// repository's ledger (`repository_migrations`) at the repository's first
// open in this process, refuses the same three divergences the schema runner
// refuses (a recorded version this binary does not carry, a recorded name
// that differs, a pending version below one already recorded), and runs what
// is pending in order, each in ONE transaction with the ledger row it leaves.
// A landed migration is never edited: the ledger says which release wrote it
// and nothing else can (frozen:check, lint:migrations).
//
// THREE RULES A MIGRATION IS HELD TO.
//
//   - It writes through the changelog, as ordinary record writes, never into
//     the fold directly: the changelog is the truth, and a rebuild replays what
//     the migration wrote.
//   - It runs BEFORE the stored vocabulary loads (engine.go), so it sees the
//     rows as they are and the binary's own registry, and nothing else. What
//     it rewrites may be the very rows the load would refuse.
//   - It is IDEMPOTENT. The ledger is the database's, and a repository
//     directory imported into a fresh database carries its changelog and not
//     the ledger, so every migration runs once more there and must find
//     nothing to do.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// repositoryMigration is one migration: what the ledger records and the code
// it records having run.
type repositoryMigration struct {
	Version int
	Name    string
	// Run performs the migration inside t and says in one line what it did,
	// for the log. Returning an error rolls the transaction back and fails the
	// repository's open.
	Run func(t *txn) (string, error)
}

// repositoryMigrations is every migration this binary carries, in order. A
// new one takes the next version and a file of its own,
// `repomigration_NNNN_<name>.go`, whose suffix is its name (the test
// TestRepositoryMigrationsMatchTheirFiles holds the two together).
var repositoryMigrations = []repositoryMigration{
	{Version: 1, Name: "qualify_bare_declaration_names", Run: migrateQualifyBareDeclarationNames},
}

// ErrRepositoryMigrationsNewer is the per-repository downgrade refusal beside
// the two dialect gates: the repository's ledger records a migration this
// binary does not carry, so a newer binary migrated it, and an older one does
// not know what that code changed.
var ErrRepositoryMigrationsNewer = errors.New("substrate/engine: the repository recorded migrations this binary does not carry")

// runRepositoryMigrations runs the pending migrations for this repository, in
// order, and refuses a ledger that diverges from what the binary carries.
func (ds *dataset) runRepositoryMigrations(ctx context.Context) error {
	applied, err := ds.appliedRepositoryMigrations(ctx)
	if err != nil {
		return err
	}
	if err := checkRepositoryMigrations(ds.info.ID, repositoryMigrations, applied); err != nil {
		return err
	}
	var pending []repositoryMigration
	for _, m := range repositoryMigrations {
		if _, ok := applied[m.Version]; !ok {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if ds.svc.readOnly {
		names := make([]string, 0, len(pending))
		for _, m := range pending {
			names = append(names, fmt.Sprintf("%d (%s)", m.Version, m.Name))
		}
		return fmt.Errorf("%w: repository %s has %d pending repository migration(s), %s, and this process opened its directory read-only; open it once with a process that writes",
			substrate.ErrUnavailable, ds.info.ID, len(pending), strings.Join(names, ", "))
	}
	for _, m := range pending {
		var did string
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			// The same lock the boot upgrade and every vocabulary apply take:
			// a migration rewrites declaration rows, and nothing else may
			// decide against the registry while it does.
			if err := t.lockKey(registryDepKey(ds)); err != nil {
				return err
			}
			summary, err := m.Run(t)
			if err != nil {
				return err
			}
			did = summary
			_, err = t.exec(`INSERT INTO repository_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name)
			return err
		})
		if err != nil {
			return fmt.Errorf("substrate/engine: repository %s: repository migration %d (%s): %w", ds.info.ID, m.Version, m.Name, err)
		}
		ds.svc.log.Info("substrate: ran a repository migration",
			"repository", logSafeID(ds.info.ID), "version", m.Version, "name", m.Name, "did", did)
	}
	return nil
}

// appliedRepositoryMigrations reads the ledger: version to recorded name.
func (ds *dataset) appliedRepositoryMigrations(ctx context.Context) (map[int]string, error) {
	rows, err := ds.db.QueryContext(ctx, `SELECT version, name FROM repository_migrations`)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the repository migrations of %s: %w", ds.info.ID, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]string{}
	for rows.Next() {
		var v int
		var name string
		if err := rows.Scan(&v, &name); err != nil {
			return nil, err
		}
		out[v] = name
	}
	return out, rows.Err()
}

// checkRepositoryMigrations compares the ledger against what the binary
// carries and names EVERY divergence, the way checkRecorded does for the
// schema. A recorded version the binary does not carry is a newer binary's
// migration (ErrRepositoryMigrationsNewer). A recorded name that differs is a
// migration renumbered or renamed since it ran, and the ledger can no longer
// say what ran. A pending version below the highest recorded one would run
// out of order.
func checkRepositoryMigrations(repository string, carried []repositoryMigration, applied map[int]string) error {
	byVersion := map[int]repositoryMigration{}
	highest := 0
	for _, m := range carried {
		byVersion[m.Version] = m
		highest = max(highest, m.Version)
	}
	highestRecorded := 0
	var unknown []int
	for v := range applied {
		highestRecorded = max(highestRecorded, v)
		if _, ok := byVersion[v]; !ok {
			unknown = append(unknown, v)
		}
	}
	sort.Ints(unknown)
	var errs []error
	if len(unknown) > 0 {
		lines := make([]string, 0, len(unknown))
		for _, v := range unknown {
			lines = append(lines, fmt.Sprintf("  %d (%s)", v, applied[v]))
		}
		errs = append(errs, fmt.Errorf("%w: repository %s recorded %d repository migration(s) this binary does not carry (it carries up to %d), "+
			"so a newer binary opened it. Run that release or a later one, or restore the copy taken before it ran. Recorded:\n%s",
			ErrRepositoryMigrationsNewer, repository, len(unknown), highest, strings.Join(lines, "\n")))
	}
	var drift, gap []string
	for _, m := range carried {
		name, ok := applied[m.Version]
		if !ok {
			if m.Version < highestRecorded {
				gap = append(gap, fmt.Sprintf("  %d (%s)", m.Version, m.Name))
			}
			continue
		}
		if name != m.Name {
			drift = append(drift, fmt.Sprintf("  %d: recorded %q, this binary carries %q", m.Version, name, m.Name))
		}
	}
	if len(drift) > 0 {
		errs = append(errs, fmt.Errorf("substrate/engine: repository %s recorded %d repository migration(s) under another name than this binary carries, "+
			"so the ledger no longer says what ran. A landed repository migration is never renamed or renumbered; the database this came from was "+
			"opened by a build that was still revising one. Throw it away or restore a copy a released binary wrote. What diverges:\n%s",
			repository, len(drift), strings.Join(drift, "\n")))
	}
	if len(gap) > 0 {
		errs = append(errs, fmt.Errorf("substrate/engine: repository %s has %d repository migration(s) pending below the highest one it recorded (%d). "+
			"They run in order, so one may not run after its successors did: a ledger row was removed by hand, or the numbering differs. "+
			"Restore a copy a matching binary wrote. Pending out of order:\n%s",
			repository, len(gap), highestRecorded, strings.Join(gap, "\n")))
	}
	return errors.Join(errs...)
}
