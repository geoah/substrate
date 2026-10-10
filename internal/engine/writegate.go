package engine

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The bounds after which a queued background writer becomes eligible for
// admission while owner writes are still on their way to the changelog lock:
// backgroundMaxOwnerPasses owner entries since it queued, or backgroundMaxWait
// in the queue. Eligible is not admitted: it still waits for the background
// writer admitted before it to take the lock.
var (
	backgroundMaxOwnerPasses = 8
	backgroundMaxWait        = 2 * time.Second
)

// writeGate orders the writers that go through inTx into one dataset's
// changelog lock. Postgres grants an advisory lock in arrival order, so the
// order is decided here, before a writer takes a pool connection, and a
// writer waiting here holds no connection and no lock.
//
// Background writers (every internal write, and every write at the bundle or
// machine tier) are admitted one at a time, from before the pool connection
// until the changelog lock, so at most one of them waits on the lock. None is
// admitted while an owner write is on its way to the lock until the head of
// the queue becomes eligible under the bounds above. The bounds cover neither
// the wait for a pool connection nor the wait on the lock.
//
// An owner write (ownerFirst: in practice an owner's put, patch or delete of a
// data record through the API or the console) is not queued: it goes straight
// on to the lock, and owner writes do not wait for one another here. It still
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
	// owners counts the owner writes between their entry and the changelog
	// lock; ownerEntries counts every owner entry, for the pass bound.
	owners       int
	ownerEntries uint64
}

// gateWaiter is one queued background writer. Every field but ready is read
// and written under the gate's mu.
type gateWaiter struct {
	// ready is closed when the writer is admitted.
	ready chan struct{}
	// entries is the gate's ownerEntries when the writer queued.
	entries uint64
	// overdue is set once the writer has waited maxWait.
	overdue  bool
	admitted bool
}

// ownerFirst reports whether a write goes ahead of waiting background writers
// at the write gate: a request's own write at the owner tier. Internal writes
// and the substrate's reserved hands (bundle:, function:, agent:, substrate)
// are background whatever tier a declaration gives them.
func (ds *dataset) ownerFirst(actor substrate.Actor, internal bool) bool {
	if internal || substrate.ReservedActor(actor) {
		return false
	}
	return ds.actorTier(actor) == substrate.TierOwner
}

// enter waits until the writer may go on to the changelog lock and returns
// the pass it gives back once it holds that lock. The pass is idempotent, so
// the caller also defers it and every exit before the lock gives it back. A
// background writer whose ctx ends while it waits leaves the queue and
// returns ctx.Err(); one that was admitted at the moment it gave up hands the
// admission on.
func (g *writeGate) enter(ctx context.Context, owner bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if owner {
		g.owners++
		g.ownerEntries++
		g.admitNext()
		g.mu.Unlock()
		return passOnce(g.ownerLeft), nil
	}
	if !g.admitted && len(g.queue) == 0 && g.owners == 0 {
		g.admitted = true
		g.mu.Unlock()
		return passOnce(g.backgroundLeft), nil
	}
	w := &gateWaiter{ready: make(chan struct{}), entries: g.ownerEntries}
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
// admitted and either no owner write is on its way or that writer has waited
// past a fairness bound. Every change to the gate's state calls it, under mu.
func (g *writeGate) admitNext() {
	if g.admitted || len(g.queue) == 0 {
		return
	}
	head := g.queue[0]
	if g.owners > 0 && !head.overdue && g.ownerEntries-head.entries < uint64(g.passes()) {
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

func (g *writeGate) ownerLeft() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.owners--
	g.admitNext()
}

func (g *writeGate) passes() int {
	if g.maxPasses > 0 {
		return g.maxPasses
	}
	return backgroundMaxOwnerPasses
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
