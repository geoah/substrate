package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockKey, hashed with current_schema(), keys the advisory lock the
// runner serializes on. An advisory lock knows nothing about schemas, so the
// schema is folded into the key the way identity.go, repositories.go and
// dialect.go fold it into theirs: a deployment has one schema per database
// and notices nothing, and the test binaries that give every test a schema
// of its own in one database migrate them side by side instead of one at a
// time behind one lock.
const migrationLockKey = "|migrate"

type migration struct {
	Version int
	Name    string
	UpSQL   string
	SHA256  string
}

// recorded is one schema_migrations row: the name says which release wrote
// it, the hash says which file ran.
type recorded struct {
	Name   string
	SHA256 string
}

// ErrDatabaseNewer is the database's downgrade refusal, beside the two
// per-repository ones (ErrVocabularyDialectNewer, ErrChangelogDialectNewer):
// schema_migrations records a migration this binary does not carry, so a
// newer binary migrated the database. Every boot step after the runner writes
// to the schema (the orphan sweep, the declared indexes, the data root
// import), so the open refuses before applying or serving anything. The
// operator commands that open the engine refuse the same way: the writing
// ones (rebuild, rotate-generation, snapshot, user reset) run the same
// runner, and the read-only ones (verify, reembed) read the same rows
// (checkMigrated).
var ErrDatabaseNewer = errors.New("substrate/engine: the database applied migrations this binary does not carry")

// ErrDatabaseOlder is the read-only open's refusal of a database this binary
// would migrate: schema_migrations lacks a migration the binary carries. A
// read-only process (WithDirectoryReadOnly) runs beside a live server, so
// applying the migration would move that server's database past the release
// the server runs and close its rollback. The open refuses instead, and the
// process that writes (the server's boot, or a writing operator command with
// the server stopped) applies it.
var ErrDatabaseOlder = errors.New("substrate/engine: the database has not applied migrations this binary carries")

// migrate applies every pending migration to the schema the DSN's
// search_path pins, atomically per migration. Idempotent.
func migrate(ctx context.Context, db *sql.DB) error {
	return migrateThrough(ctx, db, math.MaxInt)
}

// migrateThrough is migrate stopped after the migration numbered through, so
// the schema it leaves is the one a binary whose newest migration is that one
// would have built. The boot passes math.MaxInt; the refold test
// (migrationrefold_db_test.go) stages a database at each migration with a
// lower bound.
//
// The advisory lock is held on one pinned connection: releasing it from a
// different pooled connection is a silent no-op.
func migrateThrough(ctx context.Context, db *sql.DB, through int) error {
	return migrateWith(ctx, db, through, readVectorVersion)
}

// migrateWith is migrateThrough with the pgvector version reader passed in,
// so a test can stand in a version older than the test image carries.
func migrateWith(ctx context.Context, db *sql.DB, through int, vectorVersion func(context.Context, *sql.Conn) (string, error)) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: migration conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext(current_schema() || $1)::bigint)`, migrationLockKey); err != nil {
		return fmt.Errorf("substrate/engine: migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtext(current_schema() || $1)::bigint)`, migrationLockKey)
	}()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
		    version    integer PRIMARY KEY,
		    name       text NOT NULL,
		    sha256     text NOT NULL,
		    applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("substrate/engine: bootstrap schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return err
	}
	// Every recorded row is checked before anything is applied: a pending
	// migration must not land on a schema this binary has already refused,
	// and nothing at all may land on a schema a newer binary built.
	if err := checkRecorded(migrations, applied); err != nil {
		return err
	}
	// Migrations commit one at a time, so a floor checked by the migration
	// that needs it would leave the ones before it recorded and the database
	// refused by the previous release as well. It is checked before any
	// pending migration applies.
	if pendingThrough(migrations, applied, through, vectorFloorMigration) {
		version, err := vectorVersion(ctx, conn)
		if err != nil {
			return fmt.Errorf("substrate/engine: read the pgvector version: %w", err)
		}
		if err := checkVectorFloor(version); err != nil {
			return err
		}
	}
	for _, m := range migrations {
		if m.Version > through {
			break
		}
		if _, ok := applied[m.Version]; ok {
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return fmt.Errorf("substrate/engine: apply migration %d (%s): %w", m.Version, m.Name, err)
		}
	}
	return nil
}

// vectorFloorMigration is the first migration that needs vectorFloor: 0015
// builds its index over `halfvec`, which pgvector added in 0.7.0.
const vectorFloorMigration = 15

var vectorFloor = [3]int{0, 7, 0}

// ErrVectorTooOld is the boot's refusal of a pgvector older than
// vectorFloor while vectorFloorMigration is pending. Nothing is applied, so
// the previous release still boots on the database.
var ErrVectorTooOld = errors.New("substrate/engine: the database's pgvector is older than this binary needs")

// pendingThrough reports whether migration version is embedded, unapplied
// and at or below through.
func pendingThrough(migrations []migration, applied map[int]recorded, through, version int) bool {
	if version > through {
		return false
	}
	if _, ok := applied[version]; ok {
		return false
	}
	for _, m := range migrations {
		if m.Version == version {
			return true
		}
	}
	return false
}

// readVectorVersion is the `vector` extension's version in this database,
// or, before 0001 has created it, the version CREATE EXTENSION would install.
// An empty answer means the server has no pgvector at all, which 0001 refuses
// with Postgres's own error.
func readVectorVersion(ctx context.Context, conn *sql.Conn) (string, error) {
	var version sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT coalesce(
		    (SELECT extversion FROM pg_extension WHERE extname = 'vector'),
		    (SELECT default_version FROM pg_available_extensions WHERE name = 'vector'))`).Scan(&version)
	return version.String, err
}

// checkVectorFloor refuses a pgvector version below vectorFloor. A version
// it cannot parse is refused too: guessing would apply migrations a failed
// 0015 then strands.
func checkVectorFloor(version string) error {
	if version == "" {
		return nil
	}
	if parsed, ok := parseVectorVersion(version); ok && slices.Compare(parsed[:], vectorFloor[:]) >= 0 {
		return nil
	}
	return fmt.Errorf("%w: pgvector %s is installed and migration %04d needs %d.%d.%d or newer. "+
		"Nothing was applied, so the previous release still boots on this database. "+
		"Install pgvector %d.%d or newer on the Postgres host, run `ALTER EXTENSION vector UPDATE` in this database, "+
		"and start this binary again; `SELECT extversion FROM pg_extension WHERE extname = 'vector'` shows the version",
		ErrVectorTooOld, version, vectorFloorMigration, vectorFloor[0], vectorFloor[1], vectorFloor[2],
		vectorFloor[0], vectorFloor[1])
}

// parseVectorVersion reads pgvector's `major.minor[.patch]`; a missing patch
// is 0.
func parseVectorVersion(version string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(version, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// checkMigrated is migrate for a read-only open: it reads what the database
// recorded, refuses every divergence migrate refuses, and refuses a migration
// still pending instead of applying it (checkPending). It takes no lock and
// writes nothing, not even the schema_migrations table, so a database no
// binary migrated has every migration pending. Without the lock it can read a
// boot's run half way; each migration commits with its row, so what it sees
// is a prefix, and it refuses that as pending.
func checkMigrated(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: migration conn: %w", err)
	}
	defer func() { _ = conn.Close() }()
	var exists bool
	if err := conn.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return fmt.Errorf("substrate/engine: look for schema_migrations: %w", err)
	}
	applied := map[int]recorded{}
	if exists {
		if applied, err = appliedMigrations(ctx, conn); err != nil {
			return err
		}
	}
	if err := checkRecorded(migrations, applied); err != nil {
		return err
	}
	return checkPending(migrations, applied)
}

// checkPending refuses every migration this binary carries that the database
// has not applied, naming each by version and name (ErrDatabaseOlder). Only a
// read-only open asks: every other open applies them.
func checkPending(migrations []migration, applied map[int]recorded) error {
	var pending []string
	for _, m := range migrations {
		if _, ok := applied[m.Version]; !ok {
			pending = append(pending, fmt.Sprintf("%d (%s)", m.Version, m.Name))
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d migration(s) pending, %s, and this process opened the database read-only; "+
		"open it once with a process that writes (the server's boot applies them), "+
		"or run the substratectl of the release the server runs",
		ErrDatabaseOlder, len(pending), strings.Join(pending, ", "))
}

// checkRecorded compares what the database recorded against what this binary
// embeds, and names EVERY divergence rather than the first: a tree behind by
// several edited migrations otherwise learns about them one boot at a time.
//
// Three shapes refuse. A recorded version the binary does not embed is a
// newer binary's migration (ErrDatabaseNewer). An embedded version recorded
// under another hash is an edited migration. An embedded version unrecorded
// below the highest recorded one is a gap: the runner applies in order, so
// that migration would land on a schema its successors already changed.
func checkRecorded(migrations []migration, applied map[int]recorded) error {
	embedded := map[int]bool{}
	highest := 0
	for _, m := range migrations {
		embedded[m.Version] = true
		highest = max(highest, m.Version)
	}
	highestRecorded := 0
	var unknown []int
	for v := range applied {
		highestRecorded = max(highestRecorded, v)
		if !embedded[v] {
			unknown = append(unknown, v)
		}
	}
	// Numeric, not lexical: 10 follows 9 in the refusal text.
	sort.Ints(unknown)
	newer := make([]string, 0, len(unknown))
	for _, v := range unknown {
		newer = append(newer, fmt.Sprintf("  %d (%s)", v, applied[v].Name))
	}
	var drift, gap []string
	for _, m := range migrations {
		r, ok := applied[m.Version]
		if !ok {
			if m.Version < highestRecorded {
				gap = append(gap, fmt.Sprintf("  %d (%s)", m.Version, m.Name))
			}
			continue
		}
		if r.SHA256 == "" || r.SHA256 == m.SHA256 {
			continue
		}
		drift = append(drift, fmt.Sprintf("  %d (%s): recorded %s, file %s", m.Version, m.Name, r.SHA256, m.SHA256))
	}
	var errs []error
	if len(newer) > 0 {
		errs = append(errs, fmt.Errorf(`%w: %d migration(s) recorded that this binary does not carry (it carries up to %d), `+
			`so a newer binary migrated this database. Nothing is applied or served, because every later boot step `+
			`writes to a schema this binary does not know. The recorded name is the migration file's name, and the `+
			`tree's history says which release added it: run that release or a later one, or, to run this binary, `+
			`restore the database and the data root (SUBSTRATE_DATA_ROOT) together from the copies taken before that `+
			`release ran, under the same credential key. A database restored alone meets the newer release's repository `+
			`directories at the next boot. What the database recorded:`+"\n%s",
			ErrDatabaseNewer, len(newer), highest, strings.Join(newer, "\n")))
	}
	if len(drift) > 0 {
		errs = append(errs, fmt.Errorf(`substrate/engine: %d migration(s) this database applied are not the ones this binary carries. `+
			`The recorded hash is the file that actually ran, so it differs when the database was migrated by a build `+
			`whose migration has changed since, in practice a build from a branch that was still editing it. `+
			`Nothing pending is applied, because a new migration must not land on a schema its predecessors did not build. `+
			`A development database is thrown away with mise run dev:wipe, and anything else is restored from a dump a `+
			`matching binary wrote. What diverges:`+"\n%s",
			len(drift), strings.Join(drift, "\n")))
	}
	if len(gap) > 0 {
		errs = append(errs, fmt.Errorf(`substrate/engine: %d migration(s) this binary carries are pending below the highest `+
			`one this database recorded (%d). The runner applies in order, so a pending migration must not land on a `+
			`schema its successors already changed: a schema_migrations row was removed by hand, or the database was `+
			`migrated by a build whose numbering differs. Nothing is applied. Restore the database from a dump a `+
			`matching binary wrote. What is pending out of order:`+"\n%s",
			len(gap), highestRecorded, strings.Join(gap, "\n")))
	}
	return errors.Join(errs...)
}

func appliedMigrations(ctx context.Context, conn *sql.Conn) (map[int]recorded, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version, name, sha256 FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int]recorded{}
	for rows.Next() {
		var v int
		var r recorded
		if err := rows.Scan(&v, &r.Name, &r.SHA256); err != nil {
			return nil, err
		}
		out[v] = r
	}
	return out, rows.Err()
}

func applyMigration(ctx context.Context, conn *sql.Conn, m migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.UpSQL); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, sha256) VALUES ($1, $2, $3)`,
		m.Version, m.Name, m.SHA256); err != nil {
		return fmt.Errorf("record applied: %w", err)
	}
	return tx.Commit()
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		idx := strings.IndexByte(name, '_')
		if idx <= 0 {
			return nil, fmt.Errorf("substrate/engine: expected NNNN_name.up.sql, got %q", name)
		}
		version, err := strconv.Atoi(name[:idx])
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: parse %s: %w", name, err)
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			Version: version,
			Name:    strings.TrimSuffix(name, ".up.sql"),
			UpSQL:   string(body),
			SHA256:  hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i := 1; i < len(out); i++ {
		if out[i].Version == out[i-1].Version {
			return nil, fmt.Errorf("substrate/engine: duplicate migration version %d", out[i].Version)
		}
	}
	return out, nil
}
