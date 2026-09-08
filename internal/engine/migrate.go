package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
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
// operator commands that open the engine (verify, rebuild, reembed, user
// reset) run the same runner and refuse the same way.
var ErrDatabaseNewer = errors.New("substrate/engine: the database applied migrations this binary does not carry")

// supersededSHA256 lists, per version, the hashes a migration's file carried
// on an unmerged branch before it landed. A database an in-development build
// migrated recorded one of those, and this binary's file no longer hashes to
// it.
//
// A hash belongs here only once a LATER migration brings the schema it left
// up to what the file now says, and the comment beside it names that
// migration. Without the catch-up, accepting the hash accepts a schema that
// is genuinely different.
var supersededSHA256 = map[int][]string{
	// ff9bfff, PR #89's branch: the landed 0005 exactly, but for the
	// repositories_signed_from_positive CHECK added before the merge. 0007
	// adds that constraint to whatever lacks it.
	5: {"63fd9e709feefca7bd5ab040d268988d8f6f24c740f0384759f125f7f8adcc40"},
}

// migrate applies every pending migration to the schema the DSN's
// search_path pins, atomically per migration. Idempotent.
//
// The advisory lock is held on one pinned connection: releasing it from a
// different pooled connection is a silent no-op.
func migrate(ctx context.Context, db *sql.DB) error {
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
	for _, m := range migrations {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return fmt.Errorf("substrate/engine: apply migration %d (%s): %w", m.Version, m.Name, err)
		}
	}
	return nil
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
		if r.SHA256 == "" || r.SHA256 == m.SHA256 || superseded(m.Version, r.SHA256) {
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
			`restore the database from the copy taken before that release ran. What the database recorded:`+"\n%s",
			ErrDatabaseNewer, len(newer), highest, strings.Join(newer, "\n")))
	}
	if len(drift) > 0 {
		errs = append(errs, fmt.Errorf(`substrate/engine: %d migration(s) this database applied are not the ones this binary carries. `+
			`The recorded hash is the file that actually ran, so it differs when the database was migrated by a build `+
			`whose migration has changed since, in practice a build from a branch that was still editing it. `+
			`Nothing pending is applied, because a new migration must not land on a schema its predecessors did not build. `+
			`A development database is thrown away with mise run dev:wipe, and anything else is restored from a dump a `+
			`matching binary wrote. A migration corrected before it landed is accepted instead by naming its old hash in `+
			`supersededSHA256, together with the later migration that closes the gap. What diverges:`+"\n%s",
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

func superseded(version int, sum string) bool {
	for _, known := range supersededSHA256[version] {
		if known == sum {
			return true
		}
	}
	return false
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
