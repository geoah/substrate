package pushrelay

import (
	"container/heap"
	"sync"
	"time"
)

// maxReplays bounds the replay window. Past it the entry that would leave
// soonest is dropped first.
const maxReplays = 200_000

// replays is every accepted signature, kept until its time plus the window,
// so one dated ahead of the relay's clock is kept as long as it could be
// accepted. It is per replica.
type replays struct {
	mu    sync.Mutex
	until map[string]time.Time
	queue replayQueue
	max   int
}

func newReplays(max int) *replays {
	return &replays{until: map[string]time.Time{}, max: max}
}

// accept records sig, kept until until, and reports false when it is held
// already: a repeat.
func (r *replays) accept(sig []byte, until, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.queue) > 0 && r.queue[0].until.Before(now) {
		e := heap.Pop(&r.queue).(replayEntry)
		if r.until[e.sig].Equal(e.until) {
			delete(r.until, e.sig)
		}
	}
	key := string(sig)
	if u, ok := r.until[key]; ok && !u.Before(now) {
		return false
	}
	for len(r.until) >= r.max && len(r.queue) > 0 {
		e := heap.Pop(&r.queue).(replayEntry)
		delete(r.until, e.sig)
	}
	r.until[key] = until
	heap.Push(&r.queue, replayEntry{sig: key, until: until})
	return true
}

func (r *replays) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.until)
}

type replayEntry struct {
	sig   string
	until time.Time
}

// replayQueue is a min-heap on until.
type replayQueue []replayEntry

func (q replayQueue) Len() int           { return len(q) }
func (q replayQueue) Less(i, j int) bool { return q[i].until.Before(q[j].until) }
func (q replayQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *replayQueue) Push(x any)        { *q = append(*q, x.(replayEntry)) }
func (q *replayQueue) Pop() any {
	old := *q
	e := old[len(old)-1]
	*q = old[:len(old)-1]
	return e
}
