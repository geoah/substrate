// Package testdb provisions the shared Postgres+pgvector container the
// substrate integration tests run against: one container per test binary,
// one throwaway Postgres SCHEMA per test, dropped on cleanup.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
	nameMu sync.Mutex
	nameN  int
)

// DSN returns the base Postgres+pgvector DSN. It skips under -short, honors
// SUBSTRATE_TEST_DATABASE_URL, and otherwise starts a shared container.
func DSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	pgOnce.Do(func() {
		ctx := context.Background()
		if env := os.Getenv("SUBSTRATE_TEST_DATABASE_URL"); env != "" {
			pgDSN = env
		} else {
			c, err := postgres.Run(ctx, "pgvector/pgvector:pg16",
				postgres.WithDatabase("substrate"),
				postgres.WithUsername("postgres"),
				postgres.WithPassword("postgres"),
				// The suite runs t.Parallel, so the connection ceiling is a
				// function of the MACHINE: one test holds ~4 connections
				// across its admin, maintenance and scoped pools, and Go runs
				// GOMAXPROCS of them at once. The stock 100 is comfortable at
				// 16 cores (measured: ~56) and would not survive 32, and the
				// failure — "too many clients already" — reads like a leak
				// rather than a limit, so it is raised here once.
				testcontainers.WithCmdArgs("-c", "max_connections=500"),
				testcontainers.WithWaitStrategy(
					wait.ForLog("database system is ready to accept connections").
						WithOccurrence(2).WithStartupTimeout(120*time.Second)),
			)
			if err != nil {
				pgErr = err
				return
			}
			pgDSN, pgErr = c.ConnectionString(ctx, "sslmode=disable")
			if pgErr != nil {
				return
			}
		}
		// Install the Postgres extensions once, here, where nothing races: parallel
		// CREATE EXTENSION statements hit Postgres's pg_extension_name_index
		// race.
		db, err := sql.Open("pgx", pgDSN)
		if err != nil {
			pgErr = err
			return
		}
		defer func() { _ = db.Close() }()
		for _, ext := range []string{"vector", "pgcrypto"} {
			if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS `+ext+` SCHEMA public`); err != nil {
				pgErr = err
				return
			}
		}
	})
	if pgErr != nil {
		t.Fatalf("start pgvector container: %v", pgErr)
	}
	return pgDSN
}

// NewSchema creates a throwaway Postgres schema and returns a DSN whose
// search_path is baked in. The schema is dropped when the test ends.
func NewSchema(t *testing.T) string {
	t.Helper()
	base := DSN(t)
	name := uniqueName()

	db, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+name); err != nil {
		_ = db.Close()
		t.Fatalf("create schema %s: %v", name, err)
	}
	t.Cleanup(func() {
		defer func() { _ = db.Close() }()
		if _, err := db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+name+` CASCADE`); err != nil {
			t.Logf("drop schema %s: %v", name, err)
		}
	})
	return WithSearchPath(base, name)
}

// WithSearchPath bakes a search_path into a DSN — never SET on a pooled
// connection.
//
// The substrate has ONE schema now: this does not partition repositories, it
// partitions TEST BINARIES, so a package's parallel tests each get their own
// copy of the whole substrate in one shared cluster.
func WithSearchPath(dsn, schema string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=" + schema + ",public"
}

// RepositoryID reads a user's repository id out of the control-plane table.
// Tests that poke at rows need it because the repository is a COLUMN now, not
// a schema: a raw pool has to be pinned to the id (engine.OpenScopedDB) before
// row level security will show it anything.
func RepositoryID(t *testing.T, dsn, username string) string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	var id string
	if err := db.QueryRowContext(context.Background(),
		`SELECT id FROM repositories WHERE username = $1`, username).Scan(&id); err != nil {
		t.Fatalf("look up repository %q: %v", username, err)
	}
	return id
}

// Username is the username a test registers its repository under: the
// test's name folded to the username grammar ([a-z][a-z0-9]{1,29}) with a
// hash of the whole name behind it, so no two tests in a binary share one.
// Every test used to register "geoah", which was harmless while the runner
// keyed function processes on a minted id. The authority is the repository
// id now (decision record 0052), so two parallel tests registering one
// username share one id in the process-wide runner.Shared, and either
// test's Close (Reconcile against an empty live set) retires the other's
// function process mid-delivery. A subtest's name is its own, so a subtest
// that opens the repository its parent created takes the parent's name.
func Username(t *testing.T) string {
	t.Helper()
	name := t.Name()
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	prefix := b.String()
	if prefix == "" || prefix[0] < 'a' {
		prefix = "t" + prefix
	}
	// 20 readable characters and 8 of hash leave room under the 30-byte cap
	// for a test that needs a second repository (Username(t) + "2").
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return prefix + fmt.Sprintf("%08x", h.Sum32())
}

// Authority is the repository authority a test registers, and so its
// repository id: Username(t) as a label under example.com, the shape
// DefaultRepositoryAuthority mints at a real registration.
func Authority(t *testing.T) string {
	t.Helper()
	return Username(t) + ".example.com"
}

func uniqueName() string {
	nameMu.Lock()
	defer nameMu.Unlock()
	nameN++
	return fmt.Sprintf("sub_test_%d_%d", time.Now().UnixNano(), nameN)
}
