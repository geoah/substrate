package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The ERROR lines of a retake (writerlease.go retake): another start's mark,
// no mark, a migration, and the database's writer lock held by another.
const (
	markNotOurs       = "the database's start mark is not this process's"
	markGone          = "the database holds no start mark"
	migratedMeanwhile = "another server migrated it meanwhile"
	databaseLockTaken = "another process took this database's writer lock"
)

// leaseGap is server A, with one repository open unless it was started
// without one, and its heartbeat's retake held by a seam. drop ends A's
// lease session and waits for the heartbeat to find it gone: from then until
// let, the database's writer lock and every lease A held are free, and what
// a test does in between is what A's retake meets.
type leaseGap struct {
	dsn, root string
	raw       *sql.DB
	log       *migrationLog
	a         *service
	info      substrate.RepositoryInfo
	ds        *dataset
	retaking  chan struct{}
	retakes   atomic.Int32
	let       func()
}

func startLeaseGap(t *testing.T) *leaseGap {
	t.Helper()
	return startLeaseGapOn(t, MigratedDSN(t), true)
}

func startLeaseGapOn(t *testing.T, dsn string, repository bool) *leaseGap {
	t.Helper()
	ctx := context.Background()
	g := &leaseGap{dsn: dsn, root: t.TempDir(), log: &migrationLog{}, retaking: make(chan struct{})}
	held := make(chan struct{})
	var once, released sync.Once
	g.let = func() { released.Do(func() { close(held) }) }
	a, err := leaseStart(t, g.dsn, g.root, WithLogger(slog.New(slog.NewTextHandler(g.log, nil))), func(o *options) {
		o.beforeLeaseRetake = func() {
			g.retakes.Add(1)
			once.Do(func() {
				close(g.retaking)
				<-held
			})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// After A's own cleanup is registered, so it runs first: a heartbeat
	// still held here would keep A's Close waiting.
	t.Cleanup(g.let)
	g.a = a
	if g.raw, err = sql.Open("pgx", g.dsn); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.raw.Close() })
	if !repository {
		return g
	}
	if g.info, err = a.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	opened, err := a.Dataset(ctx, g.info.ID)
	if err != nil {
		t.Fatal(err)
	}
	g.ds = opened.(*dataset)
	return g
}

func (g *leaseGap) drop(t *testing.T) {
	t.Helper()
	if _, err := g.raw.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = $1`, g.a.lease.name); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.retaking:
	case <-time.After(30 * time.Second):
		t.Fatal("A's heartbeat never found its lease session gone")
	}
	if err := g.a.lease.err(); !errors.Is(err, ErrWriterLeaseLost) {
		t.Fatalf("A with its lease session gone answers %v", err)
	}
}

// settle lets A's retake go and answers what A answers once it has run:
// nil where it resumed, ErrWriterLeaseStale where it stays refused.
func (g *leaseGap) settle(t *testing.T) error {
	t.Helper()
	g.let()
	return g.waitSettled(t)
}

func (g *leaseGap) waitSettled(t *testing.T) error {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := g.a.lease.err()
		if !errors.Is(err, ErrWriterLeaseLost) {
			return err
		}
		if time.Now().After(deadline) {
			t.Fatalf("A's retake never ran: it still answers %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// String is everything b holds.
func (b *migrationLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// mark is the database's start mark, "" where there is none.
func (g *leaseGap) mark(t *testing.T) string {
	t.Helper()
	return startMarkOf(t, g.raw)
}

func startMarkOf(t *testing.T, raw *sql.DB) string {
	t.Helper()
	mark, err := readStartMark(context.Background(), raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return mark
}

// refusedForGood holds A to the end of a retake that found another process
// held its leases: a write is refused for good, its passes are stopped, and
// the ERROR line says which of the three it found.
func (g *leaseGap) refusedForGood(t *testing.T, err error, want string) {
	t.Helper()
	if !errors.Is(err, ErrWriterLeaseStale) {
		t.Fatalf("A's retake left it answering %v, want ErrWriterLeaseStale", err)
	}
	if g.ds != nil {
		if _, err := g.ds.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "from A"}}); !errors.Is(err, ErrWriterLeaseStale) {
			t.Errorf("a write on A after its retake: %v, want ErrWriterLeaseStale", err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			g.ds.reindex.mu.Lock()
			reindexClosed := g.ds.reindex.closed
			g.ds.reindex.mu.Unlock()
			g.ds.reproject.mu.Lock()
			reprojectClosed := g.ds.reproject.closed
			g.ds.reproject.mu.Unlock()
			if reindexClosed && reprojectClosed {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("A's passes are not stopped: reindex %v, reprojection %v", reindexClosed, reprojectClosed)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	logged := g.log.String()
	for _, line := range []string{markNotOurs, markGone, migratedMeanwhile} {
		if strings.Contains(logged, line) != (line == want) {
			t.Errorf("A's log does not say %q alone:\n%s", want, logged)
			break
		}
	}
}

// THE EXAMPLE OF #1449 (decision record 0325). A's lease session ends; a
// second server of the same binary starts on the same database, writes a
// record and stops; A's heartbeat takes its leases back. The migrations did
// not move, the start mark did: A stays refused for good, and a restart of A
// serves the record B wrote.
func TestAServerThatAnotherStartedBesideStaysRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := startLeaseGap(t)
	g.drop(t)
	before := g.mark(t)

	b, err := leaseStart(t, g.dsn, t.TempDir())
	if err != nil {
		t.Fatalf("B's start while A's lock and leases are free: %v", err)
	}
	opened, err := b.Dataset(ctx, g.info.ID)
	if err != nil {
		t.Fatal(err)
	}
	written, err := opened.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "from B"}})
	if err != nil {
		t.Fatalf("B's write: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if got := g.mark(t); got == before || got == "" {
		t.Fatalf("B's start left the start mark at %q, A's was %q", got, before)
	}

	g.refusedForGood(t, g.settle(t), markNotOurs)

	if err := g.a.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := leaseStart(t, g.dsn, g.root)
	if err != nil {
		t.Fatalf("A's restart: %v", err)
	}
	reopened, err := restarted.Dataset(ctx, g.info.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, written.Kind, written.ID)
	if err != nil || got.Properties["name"] != "from B" {
		t.Errorf("A's restart serves %v (%v), want the record B wrote", got, err)
	}
}

// THE CONTROL. A start beside A is refused at the database's writer lock and
// leaves no mark, so it cannot leave A refused at a later retake; and a gap
// nothing else starts in is resumed from, as before, with A's mark in place.
func TestAServerWhoseLeasesNobodyTookResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := startLeaseGap(t)
	before := g.mark(t)
	if before != g.a.lease.startMark {
		t.Fatalf("the database holds the mark %q, A's start left %q", before, g.a.lease.startMark)
	}
	if _, err := leaseStart(t, g.dsn, t.TempDir()); !errors.Is(err, ErrDatabaseHasAnotherWriter) || !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Fatalf("a start beside A: %v, want it refused at the database's writer lock", err)
	}
	if got := g.mark(t); got != before {
		t.Fatalf("a start refused beside A replaced the mark %q with %q", before, got)
	}

	g.drop(t)
	if err := g.settle(t); err != nil {
		t.Fatalf("A's retake with nothing started meanwhile left it answering %v", err)
	}
	if _, err := g.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "from A"}}); err != nil {
		t.Errorf("a write on A after its retake: %v", err)
	}
	if got := g.mark(t); got != before {
		t.Errorf("the mark moved from %q to %q with nothing started", before, got)
	}
	if logged := g.log.String(); strings.Contains(logged, markNotOurs) || strings.Contains(logged, markGone) || strings.Contains(logged, migratedMeanwhile) {
		t.Errorf("A's log says another process held its leases:\n%s", logged)
	}
}

// AN OPERATOR COMMAND that opens as the writer in the gap, as every writing
// `substratectl --dsn` command does through OpenOperator, is seen as a server
// is.
func TestAServerThatAnOperatorCommandOpenedBesideStaysRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := startLeaseGap(t)
	g.drop(t)
	before := g.mark(t)
	op, err := OpenOperator(ctx, g.dsn, WithKindsFS(SeedKinds), WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey),
		WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatalf("an operator command opening as the writer while A's lock and leases are free: %v", err)
	}
	if err := op.Close(); err != nil {
		t.Fatal(err)
	}
	if got := g.mark(t); got == before {
		t.Fatal("the operator command left A's mark in place")
	}
	g.refusedForGood(t, g.settle(t), markNotOurs)
}

// A READ-ONLY OPEN in the gap, as `repository verify` makes, takes no lock,
// leaves the mark alone, and A resumes.
func TestAReadOnlyOpenBesideALostLeaseIsNotSeen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := startLeaseGap(t)
	g.drop(t)
	before := g.mark(t)
	ro, err := OpenOperator(ctx, g.dsn, WithKindsFS(SeedKinds), WithDataRoot(g.root), WithCredentialKey(TestCredentialKey),
		WithDirectoryReadOnly(), WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatalf("a read-only open while A's lock and leases are free: %v", err)
	}
	if ro.(*service).lease != nil {
		t.Error("the read-only open took a writer lease")
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	if got := g.mark(t); got != before {
		t.Fatalf("a read-only open replaced the mark %q with %q", before, got)
	}
	if err := g.settle(t); err != nil {
		t.Fatalf("A's retake after a read-only open left it answering %v", err)
	}
	if _, err := g.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "from A"}}); err != nil {
		t.Errorf("a write on A after its retake: %v", err)
	}
}

// TWO STARTS ON AN EMPTY DATABASE. B starts on a schema with no tables and
// migrates it; while it runs, a migration is taken out of schema_migrations
// and A starts. There is no repository to hold a lease of, and A is refused
// at the database's writer lock, naming B's session, having applied no
// migration and left no mark.
func TestTwoStartsOnAnEmptyDatabase(t *testing.T) {
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
	b, err := leaseStart(t, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("B's start on an empty database: %v", err)
	}
	unrecordNewest(t, raw)
	migrations, mark := schemaMigrations(t, raw), startMarkOf(t, raw)

	_, err = leaseStart(t, dsn, t.TempDir())
	if !errors.Is(err, ErrDatabaseHasAnotherWriter) || !strings.Contains(err.Error(), fmt.Sprintf("%q", b.lease.name)) {
		t.Fatalf("A's start beside B: %v, want it refused at the database's writer lock naming B's session %q", err, b.lease.name)
	}
	if got := schemaMigrations(t, raw); got != migrations {
		t.Errorf("A's refused start changed schema_migrations:\n before %s\n after  %s", migrations, got)
	}
	if got := startMarkOf(t, raw); got != mark {
		t.Errorf("A's refused start replaced B's mark %q with %q", mark, got)
	}
}

// THE INTERLEAVING THE LOCK CLOSES. With nothing to hold, two starts once
// both left their mark, and a writer that started first could later open a
// repository the other registered, while the other's session was down, and
// go unseen. Now B, started first on a database with no repository, holds
// it: A cannot start beside it to register one. Once B stops A starts and
// registers, and B cannot start beside A.
func TestAWriterThatStartedFirstHoldsTheDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	b, err := leaseStart(t, dsn, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaseStart(t, dsn, t.TempDir()); !errors.Is(err, ErrDatabaseHasAnotherWriter) {
		t.Fatalf("A's start beside B, which holds no repository: %v, want it refused at the database's writer lock", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := leaseStart(t, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("A's start once B stopped: %v", err)
	}
	if _, err := a.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := leaseStart(t, dsn, t.TempDir()); !errors.Is(err, ErrDatabaseHasAnotherWriter) {
		t.Errorf("B's start beside A: %v, want it refused at the database's writer lock", err)
	}
}

// A RETAKE TAKES THE DATABASE'S WRITER LOCK FIRST. A holds no repository, so
// no lease stands between it and a process that took its lock: B starts in
// the gap. While B runs, A's retakes are refused at the lock; once B stops,
// A takes it back, finds B's mark, and stays refused for good.
func TestARetakeWaitsForTheDatabasesWriterLock(t *testing.T) {
	t.Parallel()
	g := startLeaseGapOn(t, MigratedDSN(t), false)
	g.drop(t)
	b, err := leaseStart(t, g.dsn, t.TempDir())
	if err != nil {
		t.Fatalf("B's start while A's lock is free: %v", err)
	}
	g.let()
	deadline := time.Now().Add(30 * time.Second)
	for g.retakes.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("A made %d retakes, want two while B runs", g.retakes.Load())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := g.a.lease.err(); !errors.Is(err, ErrWriterLeaseLost) {
		t.Fatalf("A while B holds the database's writer lock answers %v, want ErrWriterLeaseLost", err)
	}
	if logged := g.log.String(); !strings.Contains(logged, databaseLockTaken) {
		t.Errorf("A's log does not say %q:\n%s", databaseLockTaken, logged)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	g.refusedForGood(t, g.waitSettled(t), markNotOurs)
}

// THE MARK REPLACED UNDER A. A restore puts back the mark a start before A
// left, and another start follows on that copy: a mark that counted would
// read A's own again. And a mark deleted, as a wipe leaves it, is no mark.
// Both leave A refused for good, each with its own line.
func TestTheStartMarkReplacedUnderAServerRefusesIt(t *testing.T) {
	t.Parallel()
	t.Run("restored", func(t *testing.T) {
		t.Parallel()
		dsn := MigratedDSN(t)
		first, err := leaseStart(t, dsn, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		earlier := first.lease.startMark
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		g := startLeaseGapOn(t, dsn, true)
		g.drop(t)
		if _, err := g.raw.Exec(`UPDATE writer_starts SET mark = $1`, earlier); err != nil {
			t.Fatal(err)
		}
		b, err := leaseStart(t, g.dsn, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		g.refusedForGood(t, g.settle(t), markNotOurs)
	})
	t.Run("deleted", func(t *testing.T) {
		t.Parallel()
		g := startLeaseGap(t)
		g.drop(t)
		if _, err := g.raw.Exec(`DELETE FROM writer_starts`); err != nil {
			t.Fatal(err)
		}
		g.refusedForGood(t, g.settle(t), markGone)
	})
}

// THE FIRST START OF THE RELEASE. On a database as the release before left
// it, the migration that adds the start mark and every one after it
// unrecorded and its table gone, a start refused at a lease reads and writes
// nothing of it; the start once the lease is free applies the migration and
// then leaves its mark.
func TestTheFirstStartOfTheReleaseLeavesItsMark(t *testing.T) {
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
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	added := 0
	for _, m := range migrations {
		if strings.HasSuffix(m.Name, "_writer_starts") {
			added = m.Version
		}
	}
	if added == 0 {
		t.Fatal("no migration adds the start mark")
	}
	// The ones after it are written to meet a schema that has them, so the
	// start applies them again.
	if _, err := raw.Exec(`DELETE FROM schema_migrations WHERE version >= $1`, added); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE writer_starts`); err != nil {
		t.Fatal(err)
	}
	exists := func() bool {
		t.Helper()
		var ok bool
		if err := raw.QueryRow(`SELECT to_regclass('writer_starts') IS NOT NULL`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	before := schemaMigrations(t, raw)

	_, release := holdLease(t, raw, info.ID)
	if _, err := leaseStart(t, dsn, root); !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the first start beside a held lease: %v, want it refused", err)
	}
	if exists() || schemaMigrations(t, raw) != before {
		t.Fatal("a start refused at the lease created the start mark or applied a migration")
	}
	release()

	first, err := leaseStart(t, dsn, root)
	if err != nil {
		t.Fatalf("the first start once the lease is free: %v", err)
	}
	if !exists() {
		t.Fatal("the first start did not create the start mark")
	}
	if got := startMarkOf(t, raw); got == "" || got != first.lease.startMark {
		t.Errorf("the first start left the mark %q, and remembers %q", got, first.lease.startMark)
	}
}
