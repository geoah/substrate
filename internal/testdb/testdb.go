// Package testdb provisions the shared Postgres+pgvector container the
// substrate integration tests run against: one container per test binary,
// and per test either a throwaway Postgres SCHEMA (NewSchema, empty) or a
// throwaway DATABASE copied from a template the suite prepared once
// (Template.Clone). Main wires the binary: the data roots on tmpfs, m.Run,
// then every database the run made dropped in one batch.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
	// admin is the one pool every fixture's DDL rides: schemas, clones, the
	// template, the sweep and the drops. Capped, so a burst of parallel
	// tests cannot take the cluster's connections for CREATE DATABASE.
	admin  *sql.DB
	nameMu sync.Mutex
	nameN  int
	// runID names this binary's databases: the second it started and four
	// random bytes, so two binaries on one server (a plain `go test ./...`,
	// or two hosts) never mint one name, and the sweep can read the age of
	// a leftover off its name.
	runID = fmt.Sprintf("%d_%s", time.Now().Unix(), randHex(4))
)

// DSN returns the base Postgres+pgvector DSN. It skips under -short, honors
// SUBSTRATE_TEST_DATABASE_URL, and otherwise starts a shared container.
func DSN(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	pgOnce.Do(func() {
		ctx := context.Background()
		if env := os.Getenv("SUBSTRATE_TEST_DATABASE_URL"); env != "" {
			pgDSN = env
			if os.Getenv("SUBSTRATE_TEST_DATABASE_DISPOSABLE") == "true" {
				if pgErr = durabilityOffOnServer(ctx, pgDSN); pgErr != nil {
					return
				}
			}
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
				DurabilityOff(),
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
		db, err := sql.Open("pgx", pgDSN)
		if err != nil {
			pgErr = err
			return
		}
		db.SetMaxOpenConns(8)
		// Install the Postgres extensions once, here, where nothing races: parallel
		// CREATE EXTENSION statements hit Postgres's pg_extension_name_index
		// race.
		for _, ext := range []string{"vector", "pgcrypto"} {
			if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS `+ext+` SCHEMA public`); err != nil {
				pgErr = err
				_ = db.Close()
				return
			}
		}
		admin = db
	})
	if pgErr != nil {
		t.Fatalf("testdb: %v", pgErr)
	}
	return pgDSN
}

// durabilityGUCs are the settings that make a throwaway Postgres flush
// nothing. Durability buys a test database nothing and costs most of the run:
// DROP DATABASE forces a checkpoint, and with fsync on, a checkpoint under 16
// parallel tests fsyncs every dirty file (measured: 830 drops averaged 1.2 s,
// one took 26 s, 72% of Postgres's time), and every commit's WAL flush went
// the same way. All three are sighup-context or lower, so a running server
// takes them from ALTER SYSTEM plus a reload.
var durabilityGUCs = []string{"fsync=off", "synchronous_commit=off", "full_page_writes=off"}

// DurabilityOff is the container option every test-owned Postgres starts with;
// a test that starts a container of its own passes it too.
func DurabilityOff() testcontainers.CustomizeRequestOption {
	args := make([]string, 0, 2*len(durabilityGUCs))
	for _, guc := range durabilityGUCs {
		args = append(args, "-c", guc)
	}
	return testcontainers.WithCmdArgs(args...)
}

// durabilityOffOnServer applies durabilityGUCs to a server the suite was
// pointed at. It runs only under SUBSTRATE_TEST_DATABASE_DISPOSABLE=true,
// because ALTER SYSTEM rewrites the server's own configuration: a CI service
// container is disposable, a developer's database may not be.
func durabilityOffOnServer(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	for _, guc := range durabilityGUCs {
		name, value, _ := strings.Cut(guc, "=")
		if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER SYSTEM SET %s = %s", name, value)); err != nil {
			return fmt.Errorf("testdb: SUBSTRATE_TEST_DATABASE_DISPOSABLE=true asks for ALTER SYSTEM SET %s, "+
				"which this role may not run (%w); unset the variable, or connect as a superuser", name, err)
		}
	}
	if _, err := db.ExecContext(ctx, "SELECT pg_reload_conf()"); err != nil {
		return fmt.Errorf("testdb: SUBSTRATE_TEST_DATABASE_DISPOSABLE=true asks for pg_reload_conf(), "+
			"which this role may not run (%w); unset the variable, or connect as a superuser", err)
	}
	return nil
}

// containerDSN addresses the container by its own IP where the host can
// route to it (Linux, the default bridge), and through the published port
// otherwise (Docker Desktop). The published port is docker-proxy, one
// userland process relaying every byte of every connection: with sixteen
// tests each running a thousand statements, that one process is the queue
// they all wait in, and a round trip through it measured 76 µs against 48 µs
// direct on an idle machine. The direct DSN is the published one with the
// host swapped, so the credentials and parameters stay whatever the module
// minted.
func containerDSN(ctx context.Context, c *postgres.PostgresContainer) (string, error) {
	published, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", err
	}
	ip, err := c.ContainerIP(ctx)
	if err != nil || ip == "" {
		fmt.Fprintln(os.Stderr, "testdb: connecting through the container's published port (no container IP)")
		return published, nil
	}
	u, err := url.Parse(published)
	if err != nil {
		return published, nil
	}
	u.Host = net.JoinHostPort(ip, "5432")
	direct := u.String()
	db, err := sql.Open("pgx", direct)
	if err != nil {
		return published, nil
	}
	defer func() { _ = db.Close() }()
	probe, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if err := db.PingContext(probe); err != nil {
		fmt.Fprintf(os.Stderr, "testdb: connecting through the container's published port (%s unreachable: %v)\n", u.Host, err)
		return published, nil
	}
	fmt.Fprintf(os.Stderr, "testdb: connecting to the container directly at %s\n", u.Host)
	return direct, nil
}

// NewSchema creates a throwaway Postgres schema and returns a DSN whose
// search_path is baked in. The schema is dropped when the test ends. This is
// the from-empty fixture: whatever opens the DSN runs every migration.
func NewSchema(t testing.TB) string {
	t.Helper()
	base := DSN(t)
	name := schemaName()
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+name); err != nil {
		t.Fatalf("create schema %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+name+` CASCADE`); err != nil {
			t.Logf("drop schema %s: %v", name, err)
		}
	})
	return WithSearchPath(base, name)
}

// Main runs a test binary that uses this package: the data roots on tmpfs
// where there is one (TempDirOnTmpfs), m.Run, then every database the run
// made dropped, then the tmpfs directory removed. A TestMain is
// `os.Exit(testdb.Main(m))`, so a package cannot adopt half of it.
func Main(m *testing.M) int {
	cleanup := TempDirOnTmpfs()
	code := m.Run()
	DropAll()
	cleanup()
	return code
}

// TempDirOnTmpfs points TMPDIR, and with it every t.TempDir() of the binary,
// at a directory of its own under /dev/shm when that is a tmpfs with at
// least 512 MB free, and leaves it alone otherwise (macOS, a container whose
// /dev/shm is Docker's 64 MB default, TMPDIR already set). A suite calls it
// from TestMain before m.Run and runs the returned cleanup after. The engine
// fsyncs every changelog and sealed write, and sixteen parallel tests
// fsyncing one ext4 journal serialize on it; a tmpfs fsync is free, and the
// tests assert what the files hold, never that a power cut would keep them.
// `TMPDIR=/tmp` is the opt-out.
func TempDirOnTmpfs() (cleanup func()) {
	none := func() {}
	if os.Getenv("TMPDIR") != "" {
		return none
	}
	const shm = "/dev/shm"
	var st syscall.Statfs_t
	if err := syscall.Statfs(shm, &st); err != nil || st.Type != tmpfsMagic {
		return none
	}
	// Bavail and Bsize are the types the platform declares, and they differ
	// across them, so the product is taken in uint64.
	if free := uint64(st.Bavail) * uint64(st.Bsize); free < 512<<20 { //nolint:unconvert // the field types are per platform
		return none
	}
	dir, err := os.MkdirTemp(shm, "substrate-test-")
	if err != nil {
		return none
	}
	if err := os.Setenv("TMPDIR", dir); err != nil {
		_ = os.RemoveAll(dir)
		return none
	}
	fmt.Fprintf(os.Stderr, "testdb: TMPDIR=%s (tmpfs; set TMPDIR=/tmp to opt out)\n", dir)
	return func() { _ = os.RemoveAll(dir) }
}

// tmpfsMagic is TMPFS_MAGIC from linux/magic.h, what Statfs reports as the
// type of a tmpfs mount.
const tmpfsMagic = 0x01021994

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

// --- the template database and its clones -----------------------------------

// Template is one database per test binary that a caller prepares once
// (migrates, seeds, whatever its suite needs) and Clone copies per test with
// CREATE DATABASE ... TEMPLATE. Copying a prepared database is one catalog
// operation where migrating a fresh schema is every DDL statement again, and
// the engine suite runs its fixture eight hundred times on one Postgres.
//
// The copy carries everything the template's catalog holds: tables, indexes,
// policies, the grants to the cluster roles, the recorded migrations, the
// extensions. It carries no connection: build waits until nothing is
// connected to the template and then refuses connections to it, because
// Postgres refuses to copy a database anyone is using.
//
// The role the DSN names needs CREATEDB, and the DSN has to be a postgres://
// URL, so another database of the cluster can be addressed. Neither is
// checked by falling back to something slower: Clone fails and says which.
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
// when the test ends. The first call in a binary builds the template. (A
// dropper goroutine taking the drops off the tests' path was measured at
// 69 to 80 s against 67 s for the cleanup drop: DROP DATABASE's forced
// checkpoint is cheap with fsync off, and the drops then compete with the
// tests instead of pacing them.)
func (tp *Template) Clone(t *testing.T) string {
	t.Helper()
	base := DSN(t)
	tp.once.Do(func() { tp.name, tp.err = tp.build(base) })
	if tp.err != nil {
		t.Fatalf("the %s template database: %v", tp.key, tp.err)
	}
	name := cloneName()
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name+` TEMPLATE `+tp.name); err != nil {
		t.Fatalf("create database %s from %s: %v", name, tp.name, err)
	}
	register(name)
	t.Cleanup(func() { dropOne(name) })
	dsn, err := withDatabase(base, name)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// build creates the template database, installs the extensions, runs the
// caller's preparation and waits for its connections to go away. Leftovers
// of earlier runs are swept first, and a template this build leaves half
// made is dropped before the error is returned.
func (tp *Template) build(base string) (string, error) {
	ctx := context.Background()
	name := "sub_tpl_" + tp.key + "_" + runID
	// The DSN's shape, before anything is created under a name that needs it.
	dsn, err := withDatabase(base, name)
	if err != nil {
		return "", err
	}
	// Best effort: a leftover this role may not drop is somebody else's
	// problem, not a reason to refuse this run its own template.
	if err := sweepStale(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "testdb: sweep stale test databases: %v\n", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		if pgCode(err) == "42501" {
			return "", fmt.Errorf("the role needs CREATEDB to copy a migrated database per test; grant it (ALTER ROLE ... CREATEDB) or run against the container: %w", err)
		}
		return "", fmt.Errorf("create the template: %w", err)
	}
	register(name)
	fail := func(err error) (string, error) {
		if derr := dropDatabase(ctx, name); derr != nil {
			err = fmt.Errorf("%w (and dropping the half-built template failed: %w)", err, derr)
		}
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
				// A new database is a copy of template1, and vector is not a
				// trusted extension, so CREATEDB alone cannot install it there.
				if pgCode(err) == "42501" {
					return fmt.Errorf("the role may not create the %s extension in a new database: run the suite as a superuser, "+
						"or install vector and pgcrypto into template1 once (connect to template1 and CREATE EXTENSION each), "+
						"after which CREATEDB is enough because every new database inherits them: %w", ext, err)
				}
				return fmt.Errorf("create extension %s: %w", ext, err)
			}
		}
		return nil
	}(); err != nil {
		return fail(err)
	}
	if err := tp.prepare(ctx, dsn); err != nil {
		return fail(fmt.Errorf("prepare: %w", err))
	}
	// Closing a pool ends its connections from the client's side; the backends
	// take a moment to notice, and CREATE DATABASE refuses a template with a
	// backend still attached. Wait for the count, then refuse new ones so a
	// straggler cannot reattach between two clones. CREATE DATABASE itself
	// waits up to five seconds for a backend that is on its way out.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var n int
		if err := admin.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE datname = $1`, name).Scan(&n); err != nil {
			return fail(err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fail(fmt.Errorf("%d connection(s) to the template are still open after prepare returned", n))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := admin.ExecContext(ctx, `ALTER DATABASE `+name+` WITH ALLOW_CONNECTIONS false`); err != nil {
		return fail(fmt.Errorf("refuse connections to the template: %w", err))
	}
	return name, nil
}

// --- the drop list -----------------------------------------------------------

// Every database this binary creates is registered here and removed as it is
// dropped; DropAll, after m.Run, drops whatever is left: the template, and a
// clone whose test's cleanup did not get to run.
var (
	dropMu sync.Mutex
	made   = map[string]bool{}
)

func register(name string) {
	dropMu.Lock()
	defer dropMu.Unlock()
	made[name] = true
}

func dropOne(name string) {
	if err := dropDatabase(context.Background(), name); err != nil {
		fmt.Fprintf(os.Stderr, "testdb: drop database %s: %v\n", name, err)
		return
	}
	dropMu.Lock()
	delete(made, name)
	dropMu.Unlock()
}

// DropAll drops every database the binary created and has not dropped yet.
// The container case does not need it (the container dies with the binary);
// a server somebody points the suite at does, or every run leaves databases
// behind. Errors are printed, not returned: the tests already ran.
func DropAll() {
	if admin == nil {
		return
	}
	dropMu.Lock()
	names := make([]string, 0, len(made))
	for n := range made {
		names = append(names, n)
	}
	dropMu.Unlock()
	for _, n := range names {
		dropOne(n)
	}
}

// staleName is a database this package named, with the second it was made.
var staleName = regexp.MustCompile(`^sub_(?:tpl_[a-z0-9]+|test)_(\d{9,10})_`)

// sweepStale drops the databases of an earlier run that nothing is connected
// to and whose name says they are older than six hours: a binary killed
// before DropAll ran. Age, not liveness: two hosts sharing a server cannot
// see each other's processes, and a live run is never six hours old.
func sweepStale(ctx context.Context) error {
	rows, err := admin.QueryContext(ctx, `
		SELECT d.datname FROM pg_database d
		WHERE (d.datname LIKE 'sub\_tpl\_%' OR d.datname LIKE 'sub\_test\_%')
		  AND NOT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datname = d.datname)`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		m := staleName.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		ts, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || time.Since(time.Unix(ts, 0)) < 6*time.Hour {
			continue
		}
		stale = append(stale, name)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var errs []error
	for _, name := range stale {
		if err := dropDatabase(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// dropDatabase drops a database nothing should be using; FORCE ends a
// straggler rather than failing on it.
func dropDatabase(ctx context.Context, name string) error {
	_, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	return err
}

// withDatabase is the base DSN pointed at another database of the same
// cluster. The DSN is a URL (the container's is, and a SUBSTRATE_TEST_DATABASE_URL
// that is not one is refused here rather than mis-parsed).
func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("testdb: the DSN must be a postgres:// URL to address another database of the cluster, got %q", dsn)
	}
	u.Path = "/" + name
	return u.String(), nil
}

// pgCode is the SQLSTATE of a Postgres error, empty for any other error.
func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// Repository is the repository a test registers, and so its id: the test's
// name folded to a DNS label ([a-z][a-z0-9]{1,29}) with a hash of the whole
// name behind it, under example.com — the shape a real registration mints
// from a bare label and the request host. Every test used to register
// "geoah", which was harmless while the runner keyed function processes on a
// minted id. The authority is the repository id now (decision record 0052),
// so two parallel tests registering one name share one id in the
// process-wide runner.Shared, and either test's Close (Reconcile against an
// empty live set) retires the other's function process mid-delivery. A
// subtest's name is its own, so a subtest that opens the repository its
// parent created takes the parent's name.
func Repository(t *testing.T) string {
	t.Helper()
	return RepositoryLabel(t) + ".example.com"
}

// RepositoryLabel is Repository's bare label, for a test that builds a name
// of its own from it (a second repository, a package identity).
func RepositoryLabel(t *testing.T) string {
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
	// for a test that needs a second repository (RepositoryLabel(t) + "2").
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return prefix + fmt.Sprintf("%08x", h.Sum32())
}

// schemaName names a NewSchema schema: the nanosecond and a counter, so
// two binaries on one server do not collide.
func schemaName() string {
	nameMu.Lock()
	defer nameMu.Unlock()
	nameN++
	return fmt.Sprintf("sub_test_%d_%d", time.Now().UnixNano(), nameN)
}

// cloneName names a Clone database: runID (the second the binary started
// and four random bytes, which the sweep reads the age off) and a counter.
func cloneName() string {
	nameMu.Lock()
	defer nameMu.Unlock()
	nameN++
	return fmt.Sprintf("sub_test_%s_%d", runID, nameN)
}

func randHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic("testdb: no entropy: " + err.Error())
	}
	return hex.EncodeToString(raw)
}
