package engine

// The lease's recovery path, driven at the lease itself: a heartbeat that
// cannot reach the database has to keep trying, because nothing else clears
// the refusal it left standing.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A pool pointed at a port nothing serves: every connection attempt fails
// immediately, which is what a Postgres that is down looks like from here.
const leaseDownDSN = "postgres://postgres:postgres@127.0.0.1:1/substrate?sslmode=disable"

// A lost lease keeps retrying on every beat, INCLUDING the beats after a
// retake that reached nothing. The early return that used to stand here — no
// connection, so no beat — meant a writer whose database went away refused
// every write for the rest of the process's life, even once Postgres was back.
func TestWriterLeaseRetriesUntilTheDatabaseComesBack(t *testing.T) {
	t.Parallel()
	dsn := MigratedDSN(t)
	const repository = "ada.example.com"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	down, err := sql.Open("pgx", leaseDownDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = down.Close() })
	var logs bytes.Buffer
	l := &writerLease{
		db:      down,
		log:     slog.New(slog.NewTextHandler(&logs, nil)),
		stop:    func() {},
		stopped: make(chan struct{}),
		held:    []string{repository},
	}
	l.lost.Store(true)

	// Beat one: the retake cannot reach the database, so the refusal stands
	// and the connection is left unopened.
	l.beat(ctx)
	if !strings.Contains(logs.String(), "could not be retaken") {
		t.Fatalf("the first beat did not try to retake the lease:\n%s", logs.String())
	}
	if l.err() == nil {
		t.Fatal("a lease that could not be retaken must keep refusing writes")
	}

	// Beat two, with the database still down: it MUST try again. This is the
	// regression — the state beat one left (no connection, still lost) used to
	// return early here, forever.
	logs.Reset()
	l.beat(ctx)
	if !strings.Contains(logs.String(), "could not be retaken") {
		t.Fatalf("a second beat with the database still down did not retry:\n%s", logs.String())
	}
	if l.err() == nil {
		t.Fatal("the refusal must still stand while the database is unreachable")
	}

	// The database comes back.
	up, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = up.Close() })
	logs.Reset()
	l.db = up
	l.beat(ctx)
	if l.err() != nil {
		t.Fatalf("the lease did not clear once the database came back: %v\n%s", l.err(), logs.String())
	}
	if !strings.Contains(logs.String(), "retaken after its connection dropped") {
		t.Fatalf("the recovery was not logged:\n%s", logs.String())
	}

	// And the retaken lease is really HELD: another session cannot take it.
	// Without this the test would pass on a lease that merely stopped
	// complaining.
	other, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	var got bool
	if err := other.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+writerLeaseKeySQL+`)`, repository).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("another session took the lease the retake claims to hold")
	}
}

// A beat with nothing claimed touches nothing: the first acquire is what opens
// the connection, so a service that has opened no repository must not have a
// heartbeat opening one for it.
func TestWriterLeaseBeatsWithNothingHeldDoNothing(t *testing.T) {
	t.Parallel()
	down, err := sql.Open("pgx", leaseDownDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = down.Close() })
	var logs bytes.Buffer
	l := &writerLease{db: down, log: slog.New(slog.NewTextHandler(&logs, nil)), stop: func() {}, stopped: make(chan struct{})}
	l.beat(context.Background())
	if l.conn != nil {
		t.Fatal("the heartbeat opened a connection for a lease holding nothing")
	}
	if l.err() != nil || logs.Len() != 0 {
		t.Fatalf("a beat with nothing held said something: %v\n%s", l.err(), logs.String())
	}
}

// stallProxy is a TCP relay in front of Postgres that can be FROZEN: once
// stalled it reads from the client and forwards nothing, so a connection
// already established through it stays open and stops answering. That is the
// condition a deadline exists for — a host that vanished leaves the socket up,
// and a ping on it never returns — and no amount of closing pools reproduces
// it.
type stallProxy struct {
	dsn   string
	stall atomic.Bool
}

func newStallProxy(t *testing.T, upstream string) *stallProxy {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse the upstream dsn: %v", err)
	}
	// A DSN this cannot take apart would leave the proxy bypassed and the
	// test passing on a connection that never stalls, so it fails here
	// instead.
	if u.Host == "" {
		t.Fatalf("the test dsn names no host, so it cannot be proxied: %q", upstream)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p := &stallProxy{}
	// Everything but the host is kept, search_path included: the lease key is
	// composed with current_schema(), so a proxy that lost it would compute a
	// different lock.
	proxied := *u
	proxied.Host = ln.Addr().String()
	p.dsn = proxied.String()
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", u.Host)
			if err != nil {
				_ = client.Close()
				continue
			}
			go p.pump(server, client)
			go p.pump(client, server)
		}
	}()
	return p
}

func (p *stallProxy) pump(dst, src net.Conn) {
	defer func() { _ = dst.Close() }()
	defer func() { _ = src.Close() }()
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		for n > 0 && p.stall.Load() {
			// Held, not dropped: the bytes are never delivered and the peer
			// waits for an answer that cannot come.
			time.Sleep(20 * time.Millisecond)
		}
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// A ping that never answers must not hang the beat. Without a deadline the
// heartbeat blocked forever on a stalled session with `lost` false — writes
// still accepted, and the mutex held against every acquire — so the
// five-second window this file promises was unbounded.
func TestWriterLeaseRefusesWritesWhenTheHeartbeatStalls(t *testing.T) {
	t.Parallel()
	dsn := MigratedDSN(t)
	const repository = "stalled.example.com"
	ctx := context.Background()

	proxy := newStallProxy(t, dsn)
	through, err := sql.Open("pgx", proxy.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = through.Close() })
	var logs bytes.Buffer
	l := &writerLease{
		db:      through,
		log:     slog.New(slog.NewTextHandler(&logs, nil)),
		stop:    func() {},
		stopped: make(chan struct{}),
	}
	if err := l.acquire(ctx, repository); err != nil {
		t.Fatalf("take the lease through the proxy: %v", err)
	}
	if l.err() != nil {
		t.Fatalf("a freshly taken lease is not whole: %v", l.err())
	}
	// A beat while everything answers changes nothing.
	l.beat(ctx)
	if l.err() != nil {
		t.Fatalf("a beat on a live session latched a refusal: %v\n%s", l.err(), logs.String())
	}

	proxy.stall.Store(true)
	start := time.Now()
	l.beat(ctx)
	elapsed := time.Since(start)

	if l.err() == nil {
		t.Fatalf("a beat whose ping never answered left writes enabled:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "stopped answering") {
		t.Fatalf("the stall was not reported as a lost lease:\n%s", logs.String())
	}
	// Bounded, and by the beat's own deadline rather than anything the test
	// did: the retake shares the same expired context, so one timeout covers
	// the whole beat.
	if elapsed > 4*writerLeaseBeatTimeout {
		t.Fatalf("the beat took %s; it must be bounded by writerLeaseBeatTimeout (%s)", elapsed, writerLeaseBeatTimeout)
	}
}

// An acquisition runs UNDER the mutex every beat needs, so its round trips are
// bounded by the same deadline. Without that, a stalled pinned connection held
// the lock past every beat: `lost` was never set, the repositories already
// held kept accepting writes against a lease nothing had verified, and
// shutdown waited on the blocked beat.
func TestWriterLeaseAcquireIsBoundedWhenTheConnectionStalls(t *testing.T) {
	t.Parallel()
	dsn := MigratedDSN(t)
	ctx := context.Background()

	proxy := newStallProxy(t, dsn)
	through, err := sql.Open("pgx", proxy.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = through.Close() })
	var logs bytes.Buffer
	l := &writerLease{
		db:      through,
		log:     slog.New(slog.NewTextHandler(&logs, nil)),
		stop:    func() {},
		stopped: make(chan struct{}),
	}
	// One lease first, so the stalled acquisition below has something to lose.
	if err := l.acquire(ctx, "held.example.com"); err != nil {
		t.Fatalf("take the first lease through the proxy: %v", err)
	}

	proxy.stall.Store(true)
	start := time.Now()
	err = l.acquire(ctx, "second.example.com")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("an acquisition on a session that never answered reported success:\n%s", logs.String())
	}
	if elapsed > 4*writerLeaseBeatTimeout {
		t.Fatalf("the acquisition took %s; it must be bounded by writerLeaseBeatTimeout (%s)", elapsed, writerLeaseBeatTimeout)
	}
	// The session could not be reached, so the lease it was holding cannot be
	// vouched for: writes are refused until the heartbeat has it back.
	if l.err() == nil {
		t.Fatalf("a lease whose session stopped answering mid-acquisition still accepted writes:\n%s", logs.String())
	}
	// And the mutex came back: a beat can still run, which is what repairs it.
	done := make(chan struct{})
	go func() { l.beat(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * writerLeaseBeatTimeout):
		t.Fatal("the beat could not take the lease's mutex after the stalled acquisition")
	}
}

// backendPID is the pid of the lease's own session, read through the lease's
// own connection: the only way to reach the backend a test means to kill.
func backendPID(t *testing.T, l *writerLease) int {
	t.Helper()
	var pid int
	if err := l.conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read the lease session's pid: %v", err)
	}
	return pid
}

// A pinned connection that died while the lease held NOTHING is repaired at
// the next acquisition. There is no beat to notice it — a lease with nothing
// to prove proves nothing — so a Postgres restart between one registration and
// the next used to leave every later registration reusing a dead session, and
// broken until the server restarted.
func TestWriterLeaseAcquiresAgainAfterAnIdleConnectionDied(t *testing.T) {
	t.Parallel()
	dsn := MigratedDSN(t)
	ctx := context.Background()
	const taken = "taken.example.com"
	const next = "next.example.com"

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var logs bytes.Buffer
	l := &writerLease{
		db:      db,
		log:     slog.New(slog.NewTextHandler(&logs, nil)),
		stop:    func() {},
		stopped: make(chan struct{}),
	}

	// Releasing the last lease closes the connection, so it cannot be the one
	// that rots: that is the first half of the fix.
	if err := l.acquire(ctx, next); err != nil {
		t.Fatalf("take a lease: %v", err)
	}
	l.release(next)
	if l.conn != nil {
		t.Fatal("releasing the last lease left an idle pinned connection behind")
	}

	// The other way to be left holding nothing: an acquisition REFUSED by
	// another writer opens the connection and claims nothing. Somebody else
	// takes the key first.
	other := leaseProbe(t, dsn)
	var got bool
	if err := other.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+writerLeaseKeySQL+`)`, taken).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("the other session could not take the key it needs to hold")
	}
	if err := l.acquire(ctx, taken); !errors.Is(err, ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the refusal must be the lease's: %v", err)
	}
	if l.conn == nil {
		t.Fatal("a refused acquisition left no connection, so this test is not watching the state it means to")
	}

	// Now that connection dies with nothing held and no beat to see it.
	pid := backendPID(t, l)
	if _, err := other.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
		t.Fatalf("terminate the lease's backend: %v", err)
	}

	// The next registration must still be able to take its lease.
	if err := l.acquire(ctx, next); err != nil {
		t.Fatalf("a registration after an idle connection died could not take its lease: %v\n%s", err, logs.String())
	}
	if !slices.Contains(l.held, next) {
		t.Fatal("the lease reports success without holding the repository")
	}
	// Held for real, on the replacement session.
	if leaseKeyFree(t, other, next) {
		t.Fatal("another session took the lease the retry claims to hold")
	}
}

// A write that PASSED the lease check at its door and then waited — for a
// pool connection, for its own body, for the changelog lock another
// transaction was holding — must not commit under a lease that went away in
// the meantime. The door check alone let queued writes land past the window
// this file documents.
func TestAWriteThatOutlivesItsLeaseIsRefusedBeforeItCommits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var lease *writerLease
	armed := false
	// The seam fires with the changelog lines PREPARED and the commit not yet
	// run, holding the changelog lock: exactly where a write that queued
	// behind another one arrives.
	ds := newRaceDataset(t, WithTestCommitFault(func(stage string) error {
		if stage == commitAfterPrepare && armed {
			lease.lost.Store(true)
		}
		return nil
	}))
	lease = ds.svc.lease
	if lease == nil {
		t.Fatal("the service took no writer lease, so this test proves nothing")
	}
	racePut(t, ds, map[string]any{"name": "landed"})
	head, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		t.Fatal(err)
	}

	armed = true
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, Properties: map[string]any{"name": "must not land"},
	})
	armed = false
	if !errors.Is(err, ErrWriterLeaseLost) {
		t.Fatalf("a write whose lease went while it held the changelog lock was not refused: %v", err)
	}
	// The classification matters as much as the refusal: the API answers 503
	// with a Retry-After for ErrUnavailable, never an invalid token or a
	// validation problem.
	if !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("the refusal must read as unavailable: %v", err)
	}

	// NOTHING LANDED, in either store.
	if got, err := tableChangelogHead(ctx, ds.db); err != nil || got != head {
		t.Fatalf("the changelog head moved %d -> %d (err %v)", head, got, err)
	}
	var n int
	// `records` is the rows themselves, so its columns are `kind`/`id`; the
	// `record_kind`/`record_id` pair is how every OTHER table points at one.
	// Tombstones stay as rows, so live ones are the count.
	if err := ds.db.QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`, raceWidget).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d widget rows after the refused write, want the one that landed", n)
	}
	// And the prepared lines were cut: the file is back at the table's head,
	// not a line ahead of it.
	if fileHead := ds.writer.Head(); fileHead != head {
		t.Fatalf("the refused write left the file at seq %d with the table at %d", fileHead, head)
	}

	// The refusal LATCHED NOTHING: with the lease back, writing works.
	lease.lost.Store(false)
	racePut(t, ds, map[string]any{"name": "after the lease came back"})
}
