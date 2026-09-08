// Package testdb provisions the shared Postgres+pgvector container the
// substrate integration tests run against: one container per test binary,
// and per test either a throwaway Postgres SCHEMA (NewSchema, empty) or a
// throwaway DATABASE copied from a template the suite prepared once
// (Template.Clone), both dropped on cleanup.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
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
				testcontainers.WithCmdArgs("-c", "max_connections=500",
					// The container is thrown away with the binary, so
					// durability buys nothing here and costs most of the run:
					// DROP DATABASE forces a checkpoint, and with fsync on,
					// a checkpoint under 16 parallel tests fsyncs every dirty
					// file (measured: 830 drops averaged 1.2 s, one took 26 s,
					// 72% of Postgres's time). Every commit's WAL flush went
					// the same way.
					"-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off"),
				testcontainers.WithWaitStrategy(
					wait.ForLog("database system is ready to accept connections").
						WithOccurrence(2).WithStartupTimeout(120*time.Second)),
			)
			if err != nil {
				pgErr = err
				return
			}
			pgDSN, pgErr = containerDSN(ctx, c)
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
		// A server somebody else points the suite at keeps its durability
		// unless they say it is disposable (CI's service container is; a dev
		// substrate's database is not, and fsync=off is a data-loss setting
		// on a server that crashes). The container above got the same three
		// on its command line.
		if os.Getenv("SUBSTRATE_TEST_DATABASE_DISPOSABLE") != "" {
			for _, stmt := range []string{
				`ALTER SYSTEM SET fsync = off`,
				`ALTER SYSTEM SET synchronous_commit = off`,
				`ALTER SYSTEM SET full_page_writes = off`,
				`SELECT pg_reload_conf()`,
			} {
				if _, err := db.ExecContext(ctx, stmt); err != nil {
					pgErr = fmt.Errorf("SUBSTRATE_TEST_DATABASE_DISPOSABLE: %s: %w", stmt, err)
					return
				}
			}
		}
	})
	if pgErr != nil {
		t.Fatalf("start pgvector container: %v", pgErr)
	}
	return pgDSN
}

// containerDSN addresses the container by its own IP where the host can
// route to it (Linux, the default bridge), and through the published port
// otherwise (Docker Desktop). The published port is docker-proxy, one
// userland process relaying every byte of every connection: with sixteen
// tests each running a thousand statements, that one process is the queue
// they all wait in, and a round trip through it measured 76 µs against 48 µs
// direct on an idle machine.
func containerDSN(ctx context.Context, c *postgres.PostgresContainer) (string, error) {
	published, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", err
	}
	ip, err := c.ContainerIP(ctx)
	if err != nil || ip == "" {
		return published, nil
	}
	direct := fmt.Sprintf("postgres://postgres:postgres@%s/substrate?sslmode=disable", net.JoinHostPort(ip, "5432"))
	db, err := sql.Open("pgx", direct)
	if err != nil {
		return published, nil
	}
	defer func() { _ = db.Close() }()
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return published, nil
	}
	return direct, nil
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

// TempDirOnTmpfs points TMPDIR, and with it every t.TempDir() of the binary,
// at a directory of its own under /dev/shm when that is a writable tmpfs, and
// leaves it alone otherwise (macOS, a container without one). A suite calls
// it from TestMain before m.Run and runs the returned cleanup after. The
// engine fsyncs every changelog and sealed write, and sixteen parallel tests
// fsyncing one ext4 journal serialize on it; a tmpfs fsync is free, and the
// tests assert what the files hold, never that a power cut would keep them.
func TempDirOnTmpfs() (cleanup func()) {
	if os.Getenv("TMPDIR") != "" {
		return func() {}
	}
	dir, err := os.MkdirTemp("/dev/shm", "substrate-test-")
	if err != nil {
		return func() {}
	}
	if err := os.Setenv("TMPDIR", dir); err != nil {
		_ = os.RemoveAll(dir)
		return func() {}
	}
	return func() { _ = os.RemoveAll(dir) }
}

// Template is one database per test binary that a caller prepares once
// (migrates, seeds, whatever its suite needs) and Clone copies per test with
// CREATE DATABASE ... TEMPLATE. Copying a prepared database is one catalog
// operation where migrating a fresh schema is every DDL statement again, and
// the engine suite runs its fixture eight hundred times on one Postgres.
//
// The copy carries everything the template's catalog holds: tables, indexes,
// policies, the grants to the cluster roles, the recorded migrations, the
// extensions. It carries no connection: Clone waits until nothing is
// connected to the template, because Postgres refuses to copy a database
// anyone is using.
type Template struct {
	key     string
	prepare func(ctx context.Context, dsn string) error

	once sync.Once
	name string
	err  error
}

// NewTemplate declares a template; nothing runs until the first Clone. key
// names it in the cluster (one word, lowercase), and prepare runs once
// against the fresh database with the extensions installed, from the base
// DSN's own user, and must leave no connection open.
func NewTemplate(key string, prepare func(ctx context.Context, dsn string) error) *Template {
	return &Template{key: key, prepare: prepare}
}

// Clone returns the DSN of a new database copied from the template, dropped
// when the test ends. The first call in a binary builds the template.
func (tp *Template) Clone(t *testing.T) string {
	t.Helper()
	base := DSN(t)
	tp.once.Do(func() { tp.name, tp.err = tp.build(base) })
	if tp.err != nil {
		t.Fatalf("build the %s template database: %v", tp.key, tp.err)
	}
	name := uniqueName()
	db, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	ctx := context.Background()
	if err := createFromTemplate(ctx, db, name, tp.name); err != nil {
		_ = db.Close()
		t.Fatalf("create database %s from %s: %v", name, tp.name, err)
	}
	t.Cleanup(func() {
		defer func() { _ = db.Close() }()
		// FORCE: a pool a test forgot to close is not a reason to leave
		// the database behind, and the service's own pools closed already.
		if _, err := db.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Logf("drop database %s: %v", name, err)
		}
	})
	dsn, err := withDatabase(base, name)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// build creates the template database, installs the extensions, runs the
// caller's preparation and waits for its connections to go away. The name
// carries the pid, so two binaries preparing the same key at once (a plain
// `go test ./...`) never share one, and a template a killed binary leaves
// behind is as visible as the schemas were.
func (tp *Template) build(base string) (string, error) {
	ctx := context.Background()
	name := fmt.Sprintf("sub_tpl_%s_%d", tp.key, os.Getpid())
	admin, err := sql.Open("pgx", base)
	if err != nil {
		return "", err
	}
	defer func() { _ = admin.Close() }()
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
		return "", fmt.Errorf("drop a stale template: %w", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		return "", fmt.Errorf("create the template: %w", err)
	}
	dsn, err := withDatabase(base, name)
	if err != nil {
		return "", err
	}
	if err := func() error {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		for _, ext := range []string{"vector", "pgcrypto"} {
			if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS `+ext+` SCHEMA public`); err != nil {
				return fmt.Errorf("create extension %s: %w", ext, err)
			}
		}
		return nil
	}(); err != nil {
		return "", err
	}
	if err := tp.prepare(ctx, dsn); err != nil {
		return "", fmt.Errorf("prepare: %w", err)
	}
	// Closing a pool ends its connections from the client's side; the backends
	// take a moment to notice, and CREATE DATABASE refuses a template with a
	// backend still attached. Wait for the count, then refuse new ones so a
	// straggler cannot reattach between a test's clones.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var n int
		if err := admin.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE datname = $1`, name).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%d connection(s) to the template are still open after prepare returned", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := admin.ExecContext(ctx, `ALTER DATABASE `+name+` WITH ALLOW_CONNECTIONS false IS_TEMPLATE true`); err != nil {
		return "", fmt.Errorf("mark the template: %w", err)
	}
	return name, nil
}

// createFromTemplate copies the template. A backend that was still detaching
// from it when the count read zero answers 55006 (object_in_use), which is
// retried rather than failed: it is a timing, not a state.
func createFromTemplate(ctx context.Context, db *sql.DB, name, template string) error {
	var err error
	for range 50 {
		_, err = db.ExecContext(ctx, `CREATE DATABASE `+name+` TEMPLATE `+template)
		if err == nil || !strings.Contains(err.Error(), "55006") {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// withDatabase is the base DSN pointed at another database of the same
// cluster. The DSN is a URL (the container's is, and a SUBSTRATE_TEST_DATABASE_URL
// that is not one is refused here rather than mis-parsed).
func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("testdb: the DSN must be a postgres:// URL to address another database, got %q", dsn)
	}
	u.Path = "/" + name
	return u.String(), nil
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
	// A test's name begins with Test, so the prefix begins with a letter.
	prefix := b.String()
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
