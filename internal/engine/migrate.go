package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockID keys the advisory lock the runner serializes on; the value
// only needs to be stable across processes ("SUBSTR01" as a pg_locks hint).
const migrationLockID int64 = 0x5355425354523031

type migration struct {
	Version int
	Name    string
	UpSQL   string
	SHA256  string
}

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

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("substrate/engine: migration lock: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockID) }()

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
	// Every recorded hash is checked before anything is applied: a pending
	// migration must not land on a schema this binary has already refused.
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
func checkRecorded(migrations []migration, applied map[int]string) error {
	var drift []string
	for _, m := range migrations {
		sum, ok := applied[m.Version]
		if !ok || sum == "" || sum == m.SHA256 {
			continue
		}
		if superseded(m.Version, sum) {
			continue
		}
		drift = append(drift, fmt.Sprintf("  %d (%s): recorded %s, file %s", m.Version, m.Name, sum, m.SHA256))
	}
	if len(drift) == 0 {
		return nil
	}
	return fmt.Errorf(`substrate/engine: %d migration(s) this database applied are not the ones this binary carries. `+
		`The recorded hash is the file that actually ran, so it differs when the database was migrated by a build `+
		`whose migration has changed since, in practice a build from a branch that was still editing it. `+
		`Nothing pending is applied, because a new migration must not land on a schema its predecessors did not build. `+
		`A development database is thrown away with mise run dev:wipe, and anything else is restored from a dump a `+
		`matching binary wrote. A migration corrected before it landed is accepted instead by naming its old hash in `+
		`supersededSHA256, together with the later migration that closes the gap. What diverges:`+"\n%s",
		len(drift), strings.Join(drift, "\n"))
}

func superseded(version int, sum string) bool {
	for _, known := range supersededSHA256[version] {
		if known == sum {
			return true
		}
	}
	return false
}

func appliedMigrations(ctx context.Context, conn *sql.Conn) (map[int]string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version, sha256 FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
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
