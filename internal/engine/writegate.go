package engine

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The bounds after which a queued background writer becomes eligible for
// admission while a person's writes are still on their way to the changelog lock:
// backgroundMaxPersonPasses entries of a person's write since it queued, or backgroundMaxWait
// in the queue. Eligible is not admitted: it still waits for the background
// writer admitted before it to take the lock.
var (
	backgroundMaxPersonPasses = 8
	backgroundMaxWait         = 2 * time.Second
)

// writeGate orders the writers that go through inTx into one dataset's
// changelog lock. Postgres grants an advisory lock in arrival order, so the
// order is decided here, before a writer takes a pool connection, and a
// writer waiting here holds no connection and no lock.
//
// Background writers (every internal write, and every write at the bundle or
// machine tier) are admitted one at a time, from before the pool connection
// until the changelog lock, so at most one of them waits on the lock. None is
// admitted while a person's write is on its way to the lock until the head of
// the queue becomes eligible under the bounds above. The bounds cover neither
// the wait for a pool connection nor the wait on the lock.
//
// A person's write (personFirst: in practice a person's put, patch or delete of a
// data record through the API or the console, the webhook spool drain's
// admission of a request the door already answered, and a detached chat
// turn's settle after a failed write or model call) is not queued: it goes straight
// on to the lock, and people's writes do not wait for one another here. It still
// waits for the transaction holding the lock and at most one admitted
// background writer; priority never shortens the transaction already holding
// the lock.
//
// One process writes a repository (0083), so the gate is in-process. It is
// given back as soon as the changelog lock is held, so it is never held while
// waiting for writerMu or any later lock.
type writeGate struct {
	// maxPasses and maxWait replace the package bounds when set (tests).
	maxPasses int
	maxWait   time.Duration

	mu sync.Mutex
	// admitted is set while one background writer is between its admission
	// and the changelog lock.
	admitted bool
	// queue is the background writers waiting for admission, oldest first.
	queue []*gateWaiter
	// people counts the people's writes between their entry and the
	// changelog lock; personEntries counts every such entry, for the pass
	// bound.
	people        int
	personEntries uint64
}

// gateWaiter is one queued background writer. Every field but ready is read
// and written under the gate's mu.
type gateWaiter struct {
	// ready is closed when the writer is admitted.
	ready chan struct{}
	// entries is the gate's personEntries when the writer queued.
	entries uint64
	// overdue is set once the writer has waited maxWait.
	overdue  bool
	admitted bool
}

// personFirst reports whether a write goes ahead of waiting background writers
// at the write gate: a request's own write, a person's or an app's, or a
// write on a context withForegroundWrites marked. Other internal writes and
// the substrate's own hands (a package's, a function's, an agent's, the
// system's) are background.
func (ds *dataset) personFirst(ctx context.Context, actor substrate.Actor, internal bool) bool {
	if foreground, _ := ctx.Value(foregroundWritesKey{}).(bool); foreground {
		return true
	}
	// An app's write is a request somebody waits on, like a person's own,
	// though its actor is reserved.
	if internal || (substrate.ReservedActor(actor) && !substrate.IsAppActor(actor)) {
		return false
	}
	switch substrate.ActorTypeOf(actor) {
	case substrate.ActorTypePerson, substrate.ActorTypeApp:
		return true
	}
	return false
}

type foregroundWritesKey struct{}

// withForegroundWrites marks ctx's writes as a person's at the write gate,
// whatever their actor. It is for the internal writes that record a request
// a caller outside the substrate was already answered for: the webhook spool
// drain's admission, which settles the request's blobs and parks it in one
// transaction. The sender holds a 202 for a request that is in no list until
// that commit, so it cannot queue behind every agent, sync and function
// effect of the repository. The admission takes it, and the fire the drain
// spawns runs on its own context, as background. The one other taker is a
// detached chat turn's settle after a failed write or model call
// (agentloop.go settleAfterFailure): one row, written only on that failure,
// whose thread otherwise stays `running` until its lease expires. Each
// waiting settle holds one pool connection, and at most the chat turns its
// people run at once (chatTurnsPerPerson) wait at once.
func withForegroundWrites(ctx context.Context) context.Context {
	return context.WithValue(ctx, foregroundWritesKey{}, true)
}

// enter waits until the writer may go on to the changelog lock and returns
// the pass it gives back once it holds that lock. The pass is idempotent, so
// the caller also defers it and every exit before the lock gives it back. A
// background writer whose ctx ends while it waits leaves the queue and
// returns ctx.Err(); one that was admitted at the moment it gave up hands the
// admission on.
func (g *writeGate) enter(ctx context.Context, person bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if person {
		g.people++
		g.personEntries++
		g.admitNext()
		g.mu.Unlock()
		return passOnce(g.personLeft), nil
	}
	if !g.admitted && len(g.queue) == 0 && g.people == 0 {
		g.admitted = true
		g.mu.Unlock()
		return passOnce(g.backgroundLeft), nil
	}
	w := &gateWaiter{ready: make(chan struct{}), entries: g.personEntries}
	g.queue = append(g.queue, w)
	g.mu.Unlock()

	overdue := time.NewTimer(g.wait())
	defer overdue.Stop()
	for {
		select {
		case <-w.ready:
			return passOnce(g.backgroundLeft), nil
		case <-overdue.C:
			g.mu.Lock()
			w.overdue = true
			g.admitNext()
			g.mu.Unlock()
		case <-ctx.Done():
			g.mu.Lock()
			if w.admitted {
				g.admitted = false
			} else {
				g.queue = slices.DeleteFunc(g.queue, func(q *gateWaiter) bool { return q == w })
			}
			g.admitNext()
			g.mu.Unlock()
			return nil, ctx.Err()
		}
	}
}

// admitNext admits the oldest queued background writer if no other is
// admitted and either no person's write is on its way or that writer has waited
// past a fairness bound. Every change to the gate's state calls it, under mu.
func (g *writeGate) admitNext() {
	if g.admitted || len(g.queue) == 0 {
		return
	}
	head := g.queue[0]
	if g.people > 0 && !head.overdue && g.personEntries-head.entries < uint64(g.passes()) {
		return
	}
	g.queue = slices.Delete(g.queue, 0, 1)
	g.admitted = true
	head.admitted = true
	close(head.ready)
}

func (g *writeGate) backgroundLeft() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admitted = false
	g.admitNext()
}

func (g *writeGate) personLeft() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.people--
	g.admitNext()
}

func (g *writeGate) passes() int {
	if g.maxPasses > 0 {
		return g.maxPasses
	}
	return backgroundMaxPersonPasses
}

func (g *writeGate) wait() time.Duration {
	if g.maxWait > 0 {
		return g.maxWait
	}
	return backgroundMaxWait
}

func passOnce(leave func()) func() {
	var once sync.Once
	return func() { once.Do(leave) }
}
