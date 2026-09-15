package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The writer lease: a repository has ONE writer process, and a second is
// refused when it opens the repository (decision 0083).
//
// The per-directory flock (changelogfile.LockWriter) cannot see a second
// server running under a DATA ROOT OF ITS OWN, which is exactly the shape a
// box with several checkouts of this repository falls into: two servers, one
// database, two directories, neither holding the other's lock. Each one
// discovered the other only at its next append — catchUpBeforePrepare, which
// repairs the directory and logs an error nobody reads — after both had
// already run a boot upgrade, a sweep and a trigger pass over the same rows.
// The lease is the same exclusion taken where both processes can see it: in
// the database they share.
//
// ONE PINNED CONNECTION per process holds one SESSION-level advisory lock per
// repository. One connection, because session locks stack on a session and a
// lock per repository would otherwise pin a connection per repository;
// session, because the lease outlives every transaction written under it;
// pinned, because a session lock released from a different pooled connection
// is a silent no-op (lockRegistration's lesson, repositories.go).
//
// THE KEY, so an operator can read `pg_locks`:
//
//	SELECT hashtext(current_schema() || '|writer|' || 'ada.example.com')::bigint;
//
// hashtext and current_schema(), because every other advisory lock the engine
// takes is composed that way (advisoryKeySQL, identity.go), so one query reads
// them all, and because two substrates sharing a database in separate schemas
// must not hold each other's leases. `writer` names the purpose, so the lease
// is not one of the repository's own write locks.
const writerLeaseKeySQL = `hashtext(current_schema() || '|writer|' || $1)::bigint`

// writerLeaseHeartbeat is how often the pinned connection is proven alive. A
// session's advisory locks can only be released by that session or by its
// end, so a live pinned session IS the lease — nothing else has to be read
// back.
const writerLeaseHeartbeat = 5 * time.Second

// writerLeaseBeatTimeout bounds every round trip a beat makes. A pinned
// connection does not have to CLOSE to be gone: a host that vanished leaves
// the socket open and a ping on it never answers, and without a deadline the
// beat would block forever with `lost` false — writes still accepted, the
// mutex still held against an acquire, and the five-second window this file
// promises unbounded. Shorter than the interval, so a beat cannot outlive the
// next one.
const writerLeaseBeatTimeout = 3 * time.Second

// ErrRepositoryHasAnotherWriter is the refusal a second server meets when it
// opens a repository this database already has a writer for. It is a boot
// refusal: the process does not open the repository and does not serve.
var ErrRepositoryHasAnotherWriter = errors.New("substrate/engine: another process is this repository's writer")

// ErrWriterLeaseLost is the standing refusal after the pinned connection
// dropped and the lease could not be retaken: this process may no longer be
// the writer, so it writes nothing. It is ErrUnavailable, so the API answers
// 503 with a Retry-After rather than an invalid-token or a validation error —
// the condition is the host's and a retry is the right advice, because the
// heartbeat retakes the lease as soon as it can.
var ErrWriterLeaseLost = fmt.Errorf("%w: this process lost the repository's writer lease and will not write until it is retaken", substrate.ErrUnavailable)

// writerLease is a process's claim on the repositories it writes.
type writerLease struct {
	db  *sql.DB
	log *slog.Logger

	// lost is read on every write path, so it is an atomic and not the mutex
	// below: a refusal must not queue behind a heartbeat's round trip.
	lost atomic.Bool

	stop    context.CancelFunc
	stopped chan struct{}

	mu   sync.Mutex
	conn *sql.Conn
	// held is every repository this process has taken the lease on, in the
	// order it took them, so a retake after a dropped connection asks for the
	// same set. Nothing is ever removed: the lease is held for the life of the
	// process, because a repository this server opened is one it may write
	// again at any tick of any loop.
	held []string
}

// newWriterLease starts the lease and its heartbeat. The pool must be the
// maintenance one: the lease is not a repository's own lock, and the
// connection it pins carries no repository setting.
func newWriterLease(db *sql.DB, log *slog.Logger) *writerLease {
	ctx, cancel := context.WithCancel(context.Background())
	l := &writerLease{db: db, log: log, stop: cancel, stopped: make(chan struct{})}
	go l.heartbeat(ctx)
	return l
}

// acquire takes the lease on one repository, or refuses with
// ErrRepositoryHasAnotherWriter. It is idempotent: the boot check and every
// later open ask for the same repository, and the second ask is free.
// A nil lease grants everything: that is the read-only service, which writes
// nothing, and the test seam.
func (l *writerLease) acquire(ctx context.Context, repository string) error {
	_, err := l.acquireNew(ctx, repository)
	return err
}

// acquireNew is acquire, reporting whether THIS CALL is the one that took the
// lease. A creation asks this way (engine.go createSeededRepository), so that
// a creation which fails releases only a lease it introduced and never one
// this process already holds for a repository it is serving.
func (l *writerLease) acquireNew(ctx context.Context, repository string) (bool, error) {
	if l == nil {
		return false, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if slices.Contains(l.held, repository) {
		// A lease this process lost is not one it still holds, and the
		// heartbeat is the only thing that retakes it: reporting "held"
		// here would let an open proceed on a claim nobody has.
		if l.lost.Load() {
			return false, ErrWriterLeaseLost
		}
		return false, nil
	}
	conn, err := l.pinned(ctx)
	if err != nil {
		return false, err
	}
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+writerLeaseKeySQL+`)`, repository).Scan(&got); err != nil {
		return false, fmt.Errorf("substrate/engine: take the writer lease on %s: %w", repository, err)
	}
	if !got {
		return false, fmt.Errorf("%w: repository %s. One server per database: this one refuses to open the repository rather than write it from a directory of its own. "+
			"If no server is running, the holder is a connection that has not gone away yet; the lease is the advisory lock "+
			"hashtext(current_schema() || '|writer|' || '%s')::bigint, visible in pg_locks",
			ErrRepositoryHasAnotherWriter, repository, repository)
	}
	l.held = append(l.held, repository)
	return true, nil
}

// release gives one repository's lease back. It is for A CREATION THAT FAILED
// and nothing else: a repository that exists is written by this process until
// it exits, so there is no other moment at which a lease should be handed back
// while the process runs.
func (l *writerLease) release(repository string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.held, repository)
	if i < 0 {
		return
	}
	l.held = slices.Delete(l.held, i, i+1)
	if l.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), writerLeaseBeatTimeout)
	defer cancel()
	if _, err := l.conn.ExecContext(ctx, `SELECT pg_advisory_unlock(`+writerLeaseKeySQL+`)`, repository); err != nil {
		// The lock outlives a failed unlock and only its own session can drop
		// it, so the connection goes: ending the session releases this lease
		// and every other on it, and the refusal below has the heartbeat
		// retake the ones that are still this process's.
		l.log.Error("substrate: could not release the writer lease of a creation that failed; discarding its connection, and the leases on it are retaken",
			"repository", repository, "error", err)
		l.lost.Store(true)
		_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = l.conn.Close()
		l.conn = nil
	}
}

// err is the standing refusal every write path consults: nil while the lease
// is whole, ErrWriterLeaseLost once the pinned connection dropped and the
// lease could not be retaken. One atomic read, no round trip.
func (l *writerLease) err() error {
	if l == nil || !l.lost.Load() {
		return nil
	}
	return ErrWriterLeaseLost
}

// pinned returns the one connection every lock is taken on, opening it on
// first use. Called with mu held.
func (l *writerLease) pinned(ctx context.Context) (*sql.Conn, error) {
	if l.conn != nil {
		return l.conn, nil
	}
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: the writer lease needs a connection of its own: %w", err)
	}
	l.conn = conn
	return conn, nil
}

// heartbeat proves the pinned connection alive and retakes the lease when it
// is not. A dropped connection ENDED the session, and a session's end releases
// every advisory lock it held, so this process may no longer be the writer of
// anything: it fails closed (err() refuses every write) and only clears the
// refusal once every repository it held is held again.
func (l *writerLease) heartbeat(ctx context.Context) {
	defer close(l.stopped)
	t := time.NewTicker(writerLeaseHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.beat(ctx)
		}
	}
}

// beat takes SHUTDOWN's context and derives its own deadline from it: every
// round trip below is bounded, and the two are told apart afterwards —
// shutdown is not a lost lease, and a deadline is.
func (l *writerLease) beat(shutdown context.Context) {
	ctx, cancel := context.WithTimeout(shutdown, writerLeaseBeatTimeout)
	defer cancel()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.held) == 0 {
		// Nothing is claimed yet, so there is no lease to prove: the first
		// acquire is what opens the connection.
		return
	}
	if !l.lost.Load() && !l.dropped(shutdown, ctx) {
		return
	}
	// The retake runs whether the lease was lost THIS beat or several beats
	// ago with no connection to be had. A database that was down comes back,
	// and nothing but this clears the refusal, so a beat that returned early
	// because the last retake reached nothing would refuse every write for
	// the rest of the process's life.
	l.retake(shutdown, ctx)
}

// dropped reports whether the pinned session is gone, and latches the refusal
// when it is. A session's advisory locks are released only by that session or
// by its end, so a LIVE pinned session is the lease and nothing else has to be
// read back. Called with mu held.
func (l *writerLease) dropped(shutdown, ctx context.Context) bool {
	if l.conn == nil {
		// A claim with no connection behind it: a retake that could not reach
		// the database left it this way, and the leases went with the session.
		l.lost.Store(true)
		return true
	}
	err := l.conn.PingContext(ctx)
	if err == nil {
		return false
	}
	// A canceled heartbeat is Close, not a lost lease: the release runs next
	// and must not be preceded by a refusal nothing will clear. The BEAT's own
	// deadline is the opposite case — a session that did not answer inside it
	// cannot be proven to hold anything, so it is treated as holding nothing.
	if shutdown.Err() != nil {
		return false
	}
	l.lost.Store(true)
	l.log.Error("substrate: the writer lease's connection dropped or stopped answering, so this process may no longer be any repository's writer; every write is refused until the lease is retaken",
		"repositories", len(l.held), "error", err)
	return true
}

// retake opens a connection of its own and asks for every lease this process
// held. Anything less than all of them leaves the refusal standing: a process
// that writes some repositories and not others would be a second writer of
// the rest. Called with mu held.
func (l *writerLease) retake(shutdown, ctx context.Context) {
	if l.conn != nil {
		// The old connection is gone as far as the pool is concerned: handing
		// a broken one back would have it dealt out again.
		_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = l.conn.Close()
		l.conn = nil
	}
	conn, err := l.pinned(ctx)
	if err != nil {
		if shutdown.Err() == nil {
			l.log.Error("substrate: the writer lease could not be retaken", "error", err)
		}
		return
	}
	for _, id := range l.held {
		var got bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+writerLeaseKeySQL+`)`, id).Scan(&got); err != nil {
			if shutdown.Err() == nil {
				l.log.Error("substrate: the writer lease could not be retaken", "repository", id, "error", err)
			}
			return
		}
		if !got {
			l.log.Error("substrate: another process took this repository's writer lease while this one's connection was down; every write stays refused. Run one server per database and restart this one",
				"repository", id)
			return
		}
	}
	l.lost.Store(false)
	l.log.Warn("substrate: the writer lease was retaken after its connection dropped; writes are accepted again", "repositories", len(l.held))
}

// close stops the heartbeat and RELEASES every lease, in that order. The
// release is explicit because sql.Conn.Close returns the connection to the
// pool instead of ending its session, and a session advisory lock outlives
// everything but its session: without the unlock the lease would be held by a
// connection nobody owns until the pool retired it.
func (l *writerLease) close() {
	if l == nil {
		return
	}
	l.stop()
	<-l.stopped
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := l.conn.ExecContext(ctx, `SELECT pg_advisory_unlock_all()`); err != nil {
		// A connection whose unlock failed still holds the leases, so it must
		// not be pooled: discarding it ends the session, which releases them.
		l.log.Error("substrate: could not release the writer leases; discarding their connection", "error", err)
		_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = l.conn.Close()
	l.conn = nil
	l.held = nil
}
