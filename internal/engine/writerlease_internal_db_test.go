package engine

// The lease's recovery path, driven at the lease itself: a heartbeat that
// cannot reach the database has to keep trying, because nothing else clears
// the refusal it left standing.

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
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
