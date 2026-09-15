package engine

// The lease's recovery path, driven at the lease itself: a heartbeat that
// cannot reach the database has to keep trying, because nothing else clears
// the refusal it left standing.

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
