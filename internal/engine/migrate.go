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
	for _, m := range migrations {
		if sum, ok := applied[m.Version]; ok {
			if sum != "" && sum != m.SHA256 {
				return fmt.Errorf("substrate/engine: migration %d (%s) hash mismatch — recorded %s, file %s; add a new migration instead of editing a landed one",
					m.Version, m.Name, sum, m.SHA256)
			}
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return fmt.Errorf("substrate/engine: apply migration %d (%s): %w", m.Version, m.Name, err)
		}
	}
	return nil
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
