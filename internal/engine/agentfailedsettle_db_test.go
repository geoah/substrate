package engine

// A loop whose own message write fails settles its thread `error` naming the
// write, on a context of its own: the run's context may have ended and the
// write's budget is spent. A settle that fails anyway leaves its reason for
// the lease reaper (settleLostThreads).

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// holdChangelogOnReply makes the first assistant row's write wait on the
// repository's changelog lock, held by another transaction from just before
// that write until release is called.
func holdChangelogOnReply(t *testing.T, ds *dataset) (release func()) {
	t.Helper()
	held, released := make(chan struct{}), make(chan struct{})
	var once, closeOnce sync.Once
	ds.agentRowWrite = func(_ context.Context, props map[string]any) error {
		if props["role"] != "assistant" {
			return nil
		}
		once.Do(func() {
			go func() {
				_ = ds.inTx(context.Background(), substrate.ActorSystem, true, func(*txn) error {
					close(held)
					<-released
					return nil
				})
			}()
			<-held
		})
		return nil
	}
	release = func() { closeOnce.Do(func() { close(released) }) }
	t.Cleanup(release)
	return release
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
	release := holdChangelogOnReply(t, ds)
	fake.script("chat", fakeTurn{content: "never lands"})
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", "", "hi", log.emit)
		turn <- err
	}()
	// Past the write's budget and a second one like it: the settle the
	// write's own budget bounded would have failed by now.
	time.AfterFunc(1500*time.Millisecond, release)
	select {
	case err := <-turn:
		if err == nil {
			t.Fatal("a turn whose reply could not be written succeeded")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the turn did not return")
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

// A run that is not detached writes on its caller's context. When that
// context's deadline ends the write, the settle still lands on a context of
// its own instead of failing on the expired one.
func TestASettleAfterAFailedWriteOutlivesTheRunsExpiredContext(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ds.agentRowWrite = func(ctx context.Context, props map[string]any) error {
		if props["role"] != "assistant" {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	fake.script("chat", fakeTurn{content: "never lands"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ds.CallAgent(ctx, crewPackage+"/chatter", "hi"); err == nil {
		t.Fatal("a run whose reply could not be written succeeded")
	}
	threads := agentThreadsOf(t, ds, "chatter")
	if len(threads) != 1 {
		t.Fatalf("chatter threads = %+v, want one", threads)
	}
	reason, _ := threads[0]["reason"].(string)
	if threads[0]["status"] != threadError || !strings.Contains(reason, "message write failed") ||
		!strings.Contains(reason, context.DeadlineExceeded.Error()) {
		t.Fatalf("thread = %v (%q), want error naming the write", threads[0]["status"], reason)
	}
}

// A settle that fails too leaves the thread `running`, and the reaper that
// settles it once the lease expired says why the run failed.
func TestTheReaperNamesTheWriteALostRunFailedOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	ds.agentWriteTimeout = 200 * time.Millisecond
	ds.failedSettleTimeout = 200 * time.Millisecond
	release := holdChangelogOnReply(t, ds)
	fake.script("chat", fakeTurn{content: "never lands"})
	log := newEventLog()
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", "", "hi", log.emit); err == nil {
		t.Fatal("a turn whose reply could not be written succeeded")
	}
	release()
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
	if _, kept := ds.lostRunReasons.Load(thread); kept {
		t.Fatal("the reaper kept the reason after settling the thread")
	}
}
