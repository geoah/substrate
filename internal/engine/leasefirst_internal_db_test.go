package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// leaseStart starts a writing engine over dsn and root.
func leaseStart(t *testing.T, dsn, root string, opts ...Option) (*service, error) {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), dsn, append([]Option{
		WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds),
	}, opts...)...)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc.(*service), nil
}

// schemaMigrations is schema_migrations as one string.
func schemaMigrations(t *testing.T, db *sql.DB) string {
	t.Helper()
	print, err := migrationPrint(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return print
}

// unrecordNewest takes the newest migration out of schema_migrations, so the
// next start applies it again: its DDL is written to meet a schema that has
// it.
func unrecordNewest(t *testing.T, db *sql.DB) migration {
	t.Helper()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	newest := migrations[len(migrations)-1]
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, newest.Version); err != nil {
		t.Fatal(err)
	}
	return newest
}

// holdLease holds key's writer lease on a session of its own, and answers
// that session's pid and a release.
func holdLease(t *testing.T, db *sql.DB, key string) (int, func()) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(`+writerLeaseKeySQL+`)`, key); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock(`+writerLeaseKeySQL+`)`, key)
			_ = conn.Close()
		})
	}
	t.Cleanup(release)
	return pid, release
}

// A START WITH A MIGRATION PENDING APPLIES NONE WHILE ANOTHER SESSION HOLDS A
// REPOSITORY'S LEASE (decision record 0324): it is refused, names the holder,
// and schema_migrations is as it was, whether the lease is held by the
// repository's id or, as a release before ids held it, by its name. Free, the
// start applies it. A running server refuses a start with nothing pending,
// at the lease, as before, and a read-only process beside it starts, applies
// none and takes no lease.
func TestAMigrationWaitsForEveryLease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	root := t.TempDir()
	svc, err := leaseStart(t, dsn, root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.CreateRepository(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	newest := unrecordNewest(t, raw)
	before := schemaMigrations(t, raw)

	for _, by := range []struct{ name, key string }{{"its id", info.ID}, {"its name", info.Authority}} {
		pid, release := holdLease(t, raw, by.key)
		_, err := leaseStart(t, dsn, root)
		if !errors.Is(err, ErrRepositoryHasAnotherWriter) || !strings.Contains(err.Error(), fmt.Sprintf("pid %d", pid)) ||
			!strings.Contains(err.Error(), "applies no migration") || !strings.Contains(err.Error(), info.Authority) ||
			!strings.Contains(err.Error(), "hashtext(current_schema() || '|writer|' || '"+by.key+"')") {
			t.Errorf("a start with migration %d pending beside a lease held by %s: %v, want it refused naming the repository, its lock and pid %d", newest.Version, by.name, err, pid)
		}
		if got := schemaMigrations(t, raw); got != before {
			t.Errorf("a refused start beside a lease held by %s changed schema_migrations:\n before %s\n after  %s", by.name, before, got)
		}
		release()
	}

	running, err := leaseStart(t, dsn, root)
	if err != nil {
		t.Fatalf("the start once no lease is held: %v", err)
	}
	var applied bool
	if err := raw.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, newest.Version).Scan(&applied); err != nil || !applied {
		t.Fatalf("the start once no lease is held did not apply migration %d: %v", newest.Version, err)
	}
	before = schemaMigrations(t, raw)
	if _, err := leaseStart(t, dsn, t.TempDir()); !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Errorf("a start with nothing pending beside a running server: %v, want it refused at the lease", err)
	}
	if got := schemaMigrations(t, raw); got != before {
		t.Errorf("a start refused beside a running server changed schema_migrations")
	}
	// A read-only process beside it starts, applies none and takes no lease.
	ro, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds), WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("a read-only start beside a running server: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	if ro.(*service).lease != nil {
		t.Error("a read-only start took a writer lease")
	}
	if got := schemaMigrations(t, raw); got != before {
		t.Errorf("a read-only start changed schema_migrations")
	}
	_ = running
}

// THE SCENARIO OF #1352. An old server's lease connection drops; while it is
// down a new server starts, takes the leases, applies a migration and stops.
// The old server's heartbeat then retakes the leases, finds the migrations
// are not the ones it started under, and stays refused for the rest of its
// life, its passes stopped and its leases let go, so the new server can start
// again. The newest migration is taken out of schema_migrations first, and
// the new server's start is held to applying it, so the old server's
// refusal is the migration's and nothing else's.
func TestAServerThatLostItsLeaseToAMigrationStaysRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	retaking, held := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	release := func() { released.Do(func() { close(held) }) }
	old, err := leaseStart(t, dsn, t.TempDir(), func(o *options) {
		o.beforeLeaseRetake = func() {
			once.Do(func() {
				close(retaking)
				<-held
			})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// After the server's own cleanup is registered, so it runs first: a
	// heartbeat still held here would keep the server's Close waiting.
	t.Cleanup(release)
	info, err := old.CreateRepository(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := old.Dataset(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	ds := opened.(*dataset)
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	appliedAt := func(version int) (time.Time, error) {
		var at time.Time
		err := raw.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = $1`, version).Scan(&at)
		return at, err
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	newest := migrations[len(migrations)-1]
	first, err := appliedAt(newest.Version)
	if err != nil {
		t.Fatal(err)
	}
	unrecordNewest(t, raw)

	// The old server's lease connection drops.
	if _, err := raw.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = $1`, old.lease.name); err != nil {
		t.Fatal(err)
	}
	select {
	case <-retaking:
	case <-time.After(30 * time.Second):
		t.Fatal("the old server's heartbeat never found its lease lost")
	}
	if err := old.lease.err(); !errors.Is(err, ErrWriterLeaseLost) {
		t.Fatalf("the old server with its lease lost answers %v", err)
	}

	// The new server starts, migrates and stops.
	fresh, err := leaseStart(t, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("the new server's start while the old one's lease is down: %v", err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if again, err := appliedAt(newest.Version); err != nil || !again.After(first) {
		t.Fatalf("the new server's start did not apply migration %d again (applied at %v, first at %v): %v", newest.Version, again, first, err)
	}

	// The old server's heartbeat comes back.
	release()
	deadline := time.Now().Add(30 * time.Second)
	for !errors.Is(old.lease.err(), ErrWriterLeaseStale) {
		if time.Now().After(deadline) {
			t.Fatalf("the old server's retake left it answering %v, want it refused for good", old.lease.err())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "after"}}); !errors.Is(err, ErrWriterLeaseStale) {
		t.Errorf("a write on the old server after its retake: %v, want ErrWriterLeaseStale", err)
	}
	for {
		ds.reindex.mu.Lock()
		reindexClosed := ds.reindex.closed
		ds.reindex.mu.Unlock()
		ds.reproject.mu.Lock()
		reprojectClosed := ds.reproject.closed
		ds.reproject.mu.Unlock()
		if reindexClosed && reprojectClosed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the old server's passes are not stopped: reindex %v, reprojection %v", reindexClosed, reprojectClosed)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Its leases went with the refusal: the new server starts again.
	if _, err := leaseStart(t, dsn, t.TempDir()); err != nil {
		t.Errorf("the new server's second start beside the refused old one: %v", err)
	}
}

// A RETAKE REFUSES EVERY WRITE UNTIL THE PASSES ARE RESET (decision record
// 0324). The reset's seam holds it with the leases already taken back: a
// write submitted then is refused with the lease's error, and the same write
// lands once the reset is done.
func TestARetakeRefusesWritesUntilThePassesAreReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	resetting, held := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	release := func() { released.Do(func() { close(held) }) }
	svc, err := leaseStart(t, dsn, t.TempDir(), func(o *options) {
		o.retakeResetHook = func() bool {
			once.Do(func() {
				close(resetting)
				<-held
			})
			return false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// After the server's own cleanup is registered, so it runs first.
	t.Cleanup(release)
	info, err := svc.CreateRepository(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := svc.Dataset(ctx, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	ds := opened.(*dataset)
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	var dropped int
	if err := raw.QueryRow(`SELECT pid FROM pg_stat_activity WHERE application_name = $1`, svc.lease.name).Scan(&dropped); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`SELECT pg_terminate_backend($1)`, dropped); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resetting:
	case <-time.After(30 * time.Second):
		t.Fatal("the heartbeat never retook the lease and reset the passes")
	}
	// The reset is held: the leases are back on a session of the process's
	// name, and every write is still refused.
	var retaken bool
	if err := raw.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING (pid)
		WHERE l.locktype = 'advisory' AND a.application_name = $1 AND a.pid <> $2)`, svc.lease.name, dropped).Scan(&retaken); err != nil || !retaken {
		t.Fatalf("the reset runs and the leases are not back (%v)", err)
	}
	in := substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "during the reset"}}
	if _, err := ds.Put(ctx, substrate.ActorPerson, in); !errors.Is(err, ErrWriterLeaseLost) {
		t.Fatalf("a write while the passes are being reset: %v, want ErrWriterLeaseLost", err)
	}

	release()
	deadline := time.Now().Add(30 * time.Second)
	for svc.lease.err() != nil {
		if time.Now().After(deadline) {
			t.Fatalf("the reset is done and the process still answers %v", svc.lease.err())
		}
		time.Sleep(50 * time.Millisecond)
	}
	landed, err := ds.Put(ctx, substrate.ActorPerson, in)
	if err != nil {
		t.Fatalf("the same write once the reset is done: %v", err)
	}
	if got, err := ds.Get(ctx, landed.Kind, landed.ID); err != nil || got.Properties["name"] != "during the reset" {
		t.Errorf("the write once the reset is done reads back %v (%v)", got, err)
	}
}

// A REINDEX A RETAKE STOPPED IS STARTED AGAIN, even where the reset that
// stopped it timed out and the next one finds it over. The seam counts the
// first reset's stop as timed out, so the refusal stands; the next beat
// retakes, and its reset finds the canceled run ended. The reindex runs
// again and finishes.
func TestARetakeStartsAgainAReindexWhoseStopTimedOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := staleSearchIndex(t)
	reached := make(chan struct{})
	var holding atomic.Bool
	hook := func(ctx context.Context, kind string) error {
		// The first run is held at its first page until the retake stops it.
		if holding.CompareAndSwap(false, true) {
			close(reached)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	var resets atomic.Int32
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithTestSearchReindex(reindexPage, hook), func(o *options) {
		o.retakeResetHook = func() bool { return resets.Add(1) == 1 }
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	s := svc.(*service)
	opened, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := opened.(*dataset)
	select {
	case <-reached:
	case <-time.After(time.Minute):
		t.Fatal("the reindex never started")
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := raw.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = $1`, s.lease.name); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	for searchIndexVersionOf(t, ds) != searchIndexVersion || s.lease.err() != nil {
		if time.Now().After(deadline) {
			t.Fatalf("after %d resets the reindex left the version at %d and the process answers %v, want %d and nil",
				resets.Load(), searchIndexVersionOf(t, ds), s.lease.err(), searchIndexVersion)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n := resets.Load(); n < 2 {
		t.Errorf("the refusal cleared after %d reset, want the first one's timed-out stop to keep it", n)
	}
}

// A database with no tables starts: there is no repository to hold, and the
// start applies every migration and leaves its start mark.
func TestAFreshDatabaseStarts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if err := ensureRoles(ctx, raw); err != nil {
		t.Fatal(err)
	}
	svc, err := leaseStart(t, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("a start on a database with no tables: %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := raw.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != len(migrations) {
		t.Errorf("a fresh start recorded %d migrations (%v), want %d", count, err, len(migrations))
	}
	if mark := startMarkOf(t, raw); mark == "" || mark != svc.lease.startMark {
		t.Errorf("a fresh start left the start mark %q, and remembers %q", mark, svc.lease.startMark)
	}
}

// EVERY PASS IS CLASSED FOR A RETAKE (decision record 0324). It reads the
// engine package's own source, every file but the tests, and enumerates two
// things. Every task started through a `spawn` or a pass's `start` method,
// by the name it is started under, a string literal or a constant of this
// package: retakePasses classes each, and a name it cannot read fails here.
// And every goroutine started any other way, a bare `go` statement, a
// time.AfterFunc or context.AfterFunc callback, or a `.Go` call (an
// errgroup.Group's or a sync.WaitGroup's), counted by the function it is
// in: directGoroutines says how many each function starts and why none
// holds a position a retake could leave wrong, so a second one in a listed
// function fails here too. Work started outside this package is not seen;
// it writes through the engine's methods, which refuse while the lease is
// lost and, once a retake found the database migrated, for good.
func TestEveryBackgroundPassIsClassedForARetake(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	consts := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, file)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							consts[name.Name], _ = strconv.Unquote(lit.Value)
						}
					}
				}
			}
		}
	}
	started, goroutines := map[string]bool{}, map[string]int{}
	for _, file := range parsed {
		name := filepath.Base(fset.Position(file.Pos()).Filename)
		inFuncs := 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			where := name + " " + funcName(fn)
			// spawn and start themselves pass on the name their caller gave.
			forwards := fn.Name.Name == "spawn" || fn.Name.Name == "start"
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if startsGoroutine(n) {
					goroutines[where]++
					inFuncs++
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				var arg ast.Expr
				switch {
				case sel.Sel.Name == "spawn" && len(call.Args) > 0:
					arg = call.Args[0]
				case sel.Sel.Name == "start" && len(call.Args) == 3:
					arg = call.Args[1]
				default:
					return true
				}
				switch task, ok := taskName(consts, arg); {
				case ok:
					started[task] = true
				case !forwards:
					t.Errorf("%s starts a task under a name that is neither a string literal nor a constant: name it with one", fset.Position(arg.Pos()))
				}
				return true
			})
		}
		all := 0
		ast.Inspect(file, func(n ast.Node) bool {
			if startsGoroutine(n) {
				all++
			}
			return true
		})
		if all != inFuncs {
			t.Errorf("%s starts a goroutine outside any function or method, which this test cannot name", name)
		}
	}
	if len(started) == 0 || len(goroutines) == 0 {
		t.Fatal("found no pass or goroutine the engine starts")
	}
	for _, task := range slices.Sorted(maps.Keys(started)) {
		if _, ok := retakePasses[task]; !ok {
			t.Errorf("the engine starts %q and retakePasses does not class it: say whether a retake restarts it, or why it holds no position", task)
		}
	}
	for task := range retakePasses {
		if !started[task] {
			t.Errorf("retakePasses classes %q, which the engine does not start", task)
		}
	}
	for _, where := range slices.Sorted(maps.Keys(goroutines)) {
		listed, ok := directGoroutines[where]
		switch {
		case !ok:
			t.Errorf("%s starts a goroutine outside spawn and directGoroutines does not say why it holds no position a retake could leave wrong", where)
		case listed.sites != goroutines[where]:
			t.Errorf("%s starts %d goroutines outside spawn and directGoroutines says %d: say why each one holds no position a retake could leave wrong", where, goroutines[where], listed.sites)
		}
	}
	for where := range directGoroutines {
		if goroutines[where] == 0 {
			t.Errorf("directGoroutines lists %s, which starts no goroutine", where)
		}
	}
}

// launches is how many goroutines one function starts outside spawn, and
// why none holds a position a retake could leave wrong.
type launches struct {
	sites int
	why   string
}

// directGoroutines is every function of the engine that starts a goroutine
// other than through spawn, how many it starts, and why none holds a
// position a retake could leave wrong
// (TestEveryBackgroundPassIsClassedForARetake).
var directGoroutines = map[string]launches{
	"agentparallel.go (*agentLoop).dispatchRun":  {2, "the launcher and the tool calls of one agent turn, which the turn waits for inside its own task (chat turn, judge, …)"},
	"background.go (*service).spawn":             {1, "spawn's own goroutine: the task it runs is read by its name"},
	"background.go (*service).stopBackground":    {1, "the shutdown's wait for the detached tasks; it writes nothing"},
	"background.go (*backgroundPass).start":      {1, "a cancel when the service shuts down"},
	"dataset.go (*broadcaster).subscribe":        {1, "ends one change-stream subscription with its context; it writes nothing"},
	"dataset.go (*broadcaster).signal":           {1, "the change stream's flush; it writes nothing"},
	"dataset.go (*broadcaster).nudge":            {1, "the change stream's flush; it writes nothing"},
	"dataset.go newWriteCtx":                     {1, "a write's cancel with its caller"},
	"dataset.go (*writeCtx).detach":              {1, "a write's commit budget, which cancels it"},
	"functions.go (*dataset).ProcessTriggers":    {1, "the schedule lane of one trigger pass, which the pass waits for; the pass is a call its caller makes, and reads its triggers when it starts"},
	"functions.go (*dataset).scheduleLane":       {1, "one schedule's firing inside a trigger pass, which the lane waits for"},
	"scope.go closeRepositoryPool":               {1, "the shutdown's close of the repository pool; it writes nothing"},
	"archive.go (*dataset).startArchiver":        {1, "a cancel when the service shuts down"},
	"compactpass.go (*dataset).startCompactor":   {1, "a cancel when the service shuts down"},
	"agents.go (*dataset).ChatAgent":             {1, "a cancel when the service shuts down"},
	"agentloop.go (*agentLoop).dispatchSubAgent": {1, "a cancel when the parent run ends"},
	"agentjobs.go (*agentJobs).retire":           {1, "forgets a finished job in this process's memory"},
	"runner.go (*callBackend).callAgent":         {1, "a cancel when the run hub stops"},
	"writerlease.go newWriterLease":              {1, "the lease's own heartbeat, which retakes the leases and keeps every write refused until the passes are reset"},
}

// funcName is fn's name as directGoroutines spells it: the receiver's type,
// then the method.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	star := ""
	if s, ok := recv.(*ast.StarExpr); ok {
		recv, star = s.X, "*"
	}
	if idx, ok := recv.(*ast.IndexExpr); ok {
		recv = idx.X
	}
	return "(" + star + recv.(*ast.Ident).Name + ")." + fn.Name.Name
}

// startsGoroutine reports whether n starts a goroutine: a `go` statement, a
// time.AfterFunc or context.AfterFunc, whose callback runs in one, or a
// `.Go` call, which is how an errgroup.Group and a sync.WaitGroup start
// one. Without the types, any method named Go counts.
func startsGoroutine(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.GoStmt:
		return true
	case *ast.CallExpr:
		sel, ok := n.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if sel.Sel.Name == "Go" {
			return true
		}
		if sel.Sel.Name != "AfterFunc" {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && (pkg.Name == "time" || pkg.Name == "context")
	}
	return false
}

// taskName is the task a spawn or a start names, where the source says it:
// a string literal, or a constant of the package.
func taskName(consts map[string]string, arg ast.Expr) (string, bool) {
	switch a := arg.(type) {
	case *ast.BasicLit:
		if a.Kind == token.STRING {
			name, err := strconv.Unquote(a.Value)
			return name, err == nil
		}
	case *ast.Ident:
		name, ok := consts[a.Name]
		return name, ok
	}
	return "", false
}
