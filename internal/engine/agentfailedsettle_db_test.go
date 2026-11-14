package engine

// A detached chat turn whose own message write or model call fails settles
// its thread `error` with the reason, on a context of its own and ahead of
// queued background writers: the write's budget is spent and the usual
// cause is a burst of background writes on the changelog lock. The settle
// leaves a thread another run claimed since alone, and a settle that fails
// anyway leaves its reason for the lease reaper (settleLostThreads).

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// changelogHolder holds the repository's changelog lock from another
// transaction, from hold until release.
type changelogHolder struct {
	ds             *dataset
	held, released chan struct{}
	once, freed    sync.Once
}

func newChangelogHolder(t *testing.T, ds *dataset) *changelogHolder {
	t.Helper()
	h := &changelogHolder{ds: ds, held: make(chan struct{}), released: make(chan struct{})}
	t.Cleanup(h.release)
	return h
}

// hold takes the lock once and returns when it is held.
func (h *changelogHolder) hold() {
	h.once.Do(func() {
		go func() {
			_ = h.ds.inTx(context.Background(), substrate.ActorSystem, true, func(*txn) error {
				close(h.held)
				<-h.released
				return nil
			})
		}()
	})
	<-h.held
}

func (h *changelogHolder) release() { h.freed.Do(func() { close(h.released) }) }

// releaseAfter frees the lock d after it was taken, without blocking.
func (h *changelogHolder) releaseAfter(d time.Duration) {
	go func() {
		<-h.held
		time.Sleep(d)
		h.release()
	}()
}

// onFirstReply runs fn just before the first assistant row is written; the
// write then goes ahead.
func onFirstReply(ds *dataset, fn func()) {
	var once sync.Once
	ds.agentRowWrite = func(_ context.Context, props map[string]any) error {
		if props["role"] == "assistant" {
			once.Do(fn)
		}
		return nil
	}
}

// chatInBackground starts a detached chat turn and returns its outcome
// channel and event log.
func chatInBackground(ds *dataset, agent, message string) (<-chan error, *eventLog) {
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, crewPackage+"/"+agent, substrate.ChatInput{Message: message}, log.emit)
		turn <- err
	}()
	return turn, log
}

func waitTurn(t *testing.T, turn <-chan error) error {
	t.Helper()
	select {
	case err := <-turn:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("the turn did not return")
		return nil
	}
}

// The production shape: a detached chat turn's reply write waits out its
// budget behind a changelog lock held longer than that budget. The settle
// after it waits for the lock on a budget of its own and settles the thread
// `error`, naming the timeout, before the turn returns and without the
// reaper.
func TestASettleAfterAWriteTimedOutOnTheChangelogLockLands(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ds.agentWriteTimeout = 300 * time.Millisecond
	ds.failedSettleTimeout = 30 * time.Second
	lock := newChangelogHolder(t, ds)
	onFirstReply(ds, lock.hold)
	// Past the write's budget and a second one like it: a settle on the
	// write's budget would have given up by then.
	lock.releaseAfter(1500 * time.Millisecond)
	fake.script("chat", fakeTurn{content: "never lands"})
	turn, log := chatInBackground(ds, "chatter", "hi")
	if err := waitTurn(t, turn); err == nil {
		t.Fatal("a turn whose reply could not be written succeeded")
	}
	thread := log.snapshot()[0].Thread
	if _, live := ds.runningThreads.Load(thread); live {
		t.Fatal("the loop still holds the thread")
	}
	props := threadProps(t, ds, thread)
	reason, _ := props["reason"].(string)
	if props["status"] != threadError || !strings.Contains(reason, "message write failed") ||
		!strings.Contains(reason, context.DeadlineExceeded.Error()) {
		t.Fatalf("thread = %v (%q), want error naming the timed out write", props["status"], reason)
	}
}

// A model call fails while the changelog lock is held longer than a write's
// budget: the settle waits for the lock and the thread ends `error` with the
// model's failure, before the turn returns and without the reaper.
func TestASettleAfterAModelErrorWaitsOutTheChangelogLock(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ds.agentWriteTimeout = 300 * time.Millisecond
	ds.failedSettleTimeout = 30 * time.Second
	lock := newChangelogHolder(t, ds)
	arrived, answer := make(chan struct{}), make(chan struct{})
	fake.script("chat", fakeTurn{status: 400, arrived: arrived, release: answer})
	turn, log := chatInBackground(ds, "chatter", "hi")
	waitArrived(t, arrived, "the model call")
	lock.hold()
	lock.releaseAfter(1500 * time.Millisecond)
	close(answer)
	_ = waitTurn(t, turn)
	thread := log.snapshot()[0].Thread
	props := threadProps(t, ds, thread)
	if reason, _ := props["reason"].(string); props["status"] != threadError || !strings.Contains(reason, "400") {
		t.Fatalf("thread = %v (%q), want error naming the model's failure", props["status"], reason)
	}
}

// A run claimed the thread again after this run's lease expired (simulated
// here by a new lease value on the row) before this run's reply write
// failed: the settle leaves the newer claim alone.
func TestASettleAfterAFailedWriteLeavesANewerClaimAlone(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	var newer string
	var claimErr error
	fake.script("chat", fakeTurn{content: "never lands"})
	turn, log := chatInBackground(ds, "chatter", "hi")
	ds.agentRowWrite = func(_ context.Context, props map[string]any) error {
		if props["role"] != "assistant" {
			return nil
		}
		thread := log.snapshot()[0].Thread
		claimErr = ds.inTx(context.Background(), substrate.ActorSystem, true, func(t *txn) error {
			newer = t.now.Add(time.Hour).Format(time.RFC3339Nano)
			_, err := t.patch(eref{Kind: typeThread, ID: thread}, substrate.PatchInput{Properties: map[string]any{
				"status": threadRunning, "leaseUntil": newer,
			}})
			return err
		})
		return errors.New("the disk is full")
	}
	if err := waitTurn(t, turn); err == nil {
		t.Fatal("a turn whose reply could not be written succeeded")
	}
	if claimErr != nil {
		t.Fatalf("the newer claim: %v", claimErr)
	}
	props := threadProps(t, ds, log.snapshot()[0].Thread)
	if props["status"] != threadRunning || props["leaseUntil"] != newer || props["reason"] != nil {
		t.Fatalf("thread = %v (lease %v, reason %v), want the newer claim untouched", props["status"], props["leaseUntil"], props["reason"])
	}
}

// A turn that finishes after its lease expired and another run claimed the
// thread leaves that claim's status, lease and reply alone, and still adds
// its own counters to the row (#1038 review: the terminal settle removed the
// newer run's lease).
func TestATurnThatLostItsLeaseLeavesTheNewerClaimRunning(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	var newer string
	var claimErr error
	fake.script("chat", fakeTurn{content: "lands late"})
	turn, log := chatInBackground(ds, "chatter", "hi")
	onFirstReply(ds, func() {
		thread := log.snapshot()[0].Thread
		claimErr = ds.inTx(context.Background(), substrate.ActorSystem, true, func(t *txn) error {
			newer = t.now.Add(time.Hour).Format(time.RFC3339Nano)
			_, err := t.patch(eref{Kind: typeThread, ID: thread}, substrate.PatchInput{Properties: map[string]any{
				"status": threadRunning, "leaseUntil": newer,
			}})
			return err
		})
	})
	if err := waitTurn(t, turn); err != nil {
		t.Fatalf("the late turn: %v", err)
	}
	if claimErr != nil {
		t.Fatalf("the newer claim: %v", claimErr)
	}
	props := threadProps(t, ds, log.snapshot()[0].Thread)
	if props["status"] != threadRunning || props["leaseUntil"] != newer {
		t.Fatalf("thread = %v (lease %v), want the newer claim running under lease %v", props["status"], props["leaseUntil"], newer)
	}
	if props["reason"] != nil || props["finishedAt"] != nil || props[propThreadLastReply] != nil {
		t.Fatalf("the late settle wrote reason %v, finishedAt %v, lastReply %v onto the newer claim",
			props["reason"], props["finishedAt"], props[propThreadLastReply])
	}
	if turns, _ := anyFloat(props["turns"]); turns != 1 {
		t.Fatalf("turns = %v, want the late turn's counters on the row", props["turns"])
	}
}

// The settle after a failed write goes ahead of background writers queued at
// the write gate: with the lock held, one background writer admitted and two
// queued, the reply write times out in the queue, and the settle commits
// before either queued writer runs. The writers' wait is kept out of reach of
// the gate's bound (backgroundMaxWait), past which the gate admits a queued
// writer while a person's write is still on its way to the lock.
func TestASettleAfterAFailedWriteGoesAheadOfQueuedBackgroundWriters(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ds.agentWriteTimeout = 300 * time.Millisecond
	ds.failedSettleTimeout = 30 * time.Second
	ds.gate.maxWait = time.Hour
	lock := newChangelogHolder(t, ds)
	const writers = 3
	var mu sync.Mutex
	var seen []string // the thread status each background writer saw, in run order
	var wg sync.WaitGroup
	// placed is closed once the hook has the lock held and the writers at the
	// gate; stood says whether they stood one admitted and the rest queued.
	placed := make(chan struct{})
	var stood bool
	fake.script("chat", fakeTurn{content: "never lands"})
	turn, log := chatInBackground(ds, "chatter", "hi")
	onFirstReply(ds, func() {
		defer close(placed)
		thread := log.snapshot()[0].Thread
		lock.hold()
		for range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = ds.inTx(context.Background(), substrate.ActorSystem, true, func(t *txn) error {
					row, err := t.loadRow(eref{Kind: typeThread, ID: thread}, false)
					if err != nil || row == nil {
						return err
					}
					status, _ := row.Props["status"].(string)
					mu.Lock()
					seen = append(seen, status)
					mu.Unlock()
					return nil
				})
			}()
		}
		// The reply write queues behind them.
		stood = gateReaches(&ds.gate, func(admitted bool, queued, _ int) bool {
			return admitted && queued == writers-1
		})
	})
	// The settle is waited for only once the hook holds the lock: the chat's
	// own message is a person's write too, and taking it for the settle
	// releases the lock before the hook takes it, so the writers run with
	// nothing to queue behind.
	select {
	case <-placed:
	case err := <-turn:
		t.Fatalf("the turn ended (%v) before its reply write was held", err)
	case <-time.After(hangWait):
		t.Fatal("the turn never reached its reply write")
	}
	if !stood {
		admitted, queued, owners := gateState(&ds.gate)
		t.Fatalf("the writers never stood one admitted and %d queued: admitted=%v queued=%d owners=%d", writers-1, admitted, queued, owners)
	}
	if !gateReaches(&ds.gate, func(admitted bool, queued, owners int) bool {
		return admitted && queued == writers-1 && owners == 1
	}) {
		admitted, queued, owners := gateState(&ds.gate)
		t.Fatalf("the settle never reached the gate as a person's write with the writers in place: admitted=%v queued=%d owners=%d", admitted, queued, owners)
	}
	lock.release()
	_ = waitTurn(t, turn)
	wg.Wait()
	if got := threadProps(t, ds, log.snapshot()[0].Thread)["status"]; got != threadError {
		t.Fatalf("thread = %v, want error", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != writers || slices.ContainsFunc(seen[1:], func(s string) bool { return s != threadError }) {
		t.Fatalf("background writers saw %v, want both queued ones to see the settled thread", seen)
	}
}

// hangWait bounds a wait for a state a test sets up: a deadline only a hang
// reaches, since a loaded runner takes seconds over what a laptop does at
// once.
const hangWait = 2 * time.Minute

// gateReaches waits for the write gate to reach cond and reports whether it
// did within hangWait. It never fails the test, so the agent's own goroutine
// may wait with it.
func gateReaches(g *writeGate, cond func(admitted bool, queued, owners int) bool) bool {
	deadline := time.Now().Add(hangWait)
	for !cond(gateState(g)) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

// A settle that fails too leaves the thread `running`, and the reaper that
// settles it once the lease expired says why the run failed.
func TestTheReaperNamesTheWriteALostRunFailedOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	ds.agentWriteTimeout = 200 * time.Millisecond
	ds.failedSettleTimeout = 200 * time.Millisecond
	lock := newChangelogHolder(t, ds)
	onFirstReply(ds, lock.hold)
	fake.script("chat", fakeTurn{content: "never lands"})
	turn, log := chatInBackground(ds, "chatter", "hi")
	if err := waitTurn(t, turn); err == nil {
		t.Fatal("a turn whose reply could not be written succeeded")
	}
	lock.release()
	thread := log.snapshot()[0].Thread
	props := threadProps(t, ds, thread)
	if props["status"] != threadRunning {
		t.Fatalf("thread = %v (%v), want it left running by the failed settle", props["status"], props["reason"])
	}
	props["__id"] = thread
	expireThreadLease(t, ds, props)
	if _, err := ds.SweepResolutions(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	props = threadProps(t, ds, thread)
	reason, _ := props["reason"].(string)
	if props["status"] != threadError || !strings.HasPrefix(reason, lostThreadReason) ||
		!strings.Contains(reason, "the run failed with: message write failed") ||
		!strings.Contains(reason, context.DeadlineExceeded.Error()) {
		t.Fatalf("thread = %v (%q), want the lost run and its failed write named", props["status"], reason)
	}
	if lost := ds.lostRunReason(thread); lost != "" {
		t.Fatalf("the reaper kept the reason %q after settling the thread", lost)
	}
}
