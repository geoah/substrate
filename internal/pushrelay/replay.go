package pushrelay

import (
	"container/heap"
	"time"

	"github.com/geoah/substrate/internal/pushwire"
)

// MaxReplays bounds the replay window: the signatures held to refuse a
// repeat.
const MaxReplays = 200_000

// replayHorizon is the longest one entry is held: a signature dated
// SignatureWindow ahead of the relay's clock is kept until its time plus
// the window.
const replayHorizon = 2 * pushwire.SignatureWindow

// ReplayCapacity is how many signatures the window must hold for a total
// limit of total pushes a minute: an entry is admitted only once the total
// limit lets its push through, so at most a full bucket plus the horizon's
// refill are live at once. An entry is never dropped while it lives, so a
// window that is full answers rate_limited instead.
func ReplayCapacity(total int) int {
	return total + total*int(replayHorizon/time.Minute)
}

// MaxTotal is the largest total limit whose replay window fits MaxReplays.
var MaxTotal = MaxReplays / ReplayCapacity(1)

// replays is every admitted signature, kept until its time plus the window,
// so one dated ahead of the relay's clock is kept as long as it could be
// accepted. It is per replica, and the relay's admission lock guards it.
type replays struct {
	until    map[string]time.Time
	queue    replayQueue
	capacity int
}

func newReplays(capacity int) *replays {
	return &replays{until: map[string]time.Time{}, capacity: capacity}
}

// prune drops every entry that can no longer be accepted at now.
func (r *replays) prune(now time.Time) {
	for len(r.queue) > 0 && r.queue[0].until.Before(now) {
		e := heap.Pop(&r.queue).(replayEntry)
		if r.until[e.sig].Equal(e.until) {
			delete(r.until, e.sig)
		}
	}
}

// seen reports whether sig is held, after a prune at the same clock.
func (r *replays) seen(sig []byte) bool {
	_, ok := r.until[string(sig)]
	return ok
}

func (r *replays) full() bool { return len(r.until) >= r.capacity }

// add holds sig until until. The caller checked seen and full.
func (r *replays) add(sig []byte, until time.Time) {
	key := string(sig)
	r.until[key] = until
	heap.Push(&r.queue, replayEntry{sig: key, until: until})
}

func (r *replays) len() int { return len(r.until) }

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
