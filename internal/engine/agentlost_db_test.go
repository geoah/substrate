package engine

// An agent run that dies inside a live process (its context canceled, a
// write failing on the way out, a panic) leaves its thread `running` with no
// loop behind it. The resolution sweep settles such a thread to `error` once
// its lease expired, and leaves a thread whose loop still runs alone, whatever
// its lease says (settleLostThreads).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestTheSweepSettlesAThreadItsRunLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, _ := restartableAgentDataset(t)
	trigger := keeperWebhook(t, ds)

	stop, release := heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()
	lost := onlyKeeperThread(t, ds, threadRunning)

	// The lease still runs: the sweep leaves the thread for its loop.
	if _, err := ds.SweepResolutions(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	onlyKeeperThread(t, ds, threadRunning)

	expireThreadLease(t, ds, lost)
	if _, err := ds.SweepResolutions(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	got := onlyKeeperThread(t, ds, threadError)
	if reason, _ := got["reason"].(string); !strings.HasPrefix(reason, lostThreadReason) {
		t.Fatalf("reason = %q, want the lost run named", reason)
	}
	if fin, _ := got["finishedAt"].(string); fin == "" {
		t.Fatalf("the settled thread has no finishedAt: %+v", got)
	}
	// Nothing reruns it: the claim stays for a hand, listed as interrupted.
	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 || failures[0].Running || failures[0].LastError != interruptedAgentError {
		t.Fatalf("failures after the sweep = %+v (%v), want the one interrupted claim", failures, err)
	}
	if n := len(agentThreadsOf(t, ds, "keeper")); n != 1 {
		t.Fatalf("the sweep redelivered the run: %d threads", n)
	}
}

func TestTheSweepLeavesALiveLoopsThreadRunning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, _ := restartableAgentDataset(t)
	trigger := keeperWebhook(t, ds)

	_, release := heldKeeperFire(t, ds, fake, trigger)
	live := onlyKeeperThread(t, ds, threadRunning)
	expireThreadLease(t, ds, live)
	if _, err := ds.SweepResolutions(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	onlyKeeperThread(t, ds, threadRunning)

	release()
	deadline := time.Now().Add(30 * time.Second)
	for {
		threads := agentThreadsOf(t, ds, "keeper")
		if len(threads) == 1 && threads[0]["status"] == threadOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("keeper threads after the release = %+v, want the live run settled ok", threads)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// onlyKeeperThread requires the keeper agent to hold exactly one thread, in
// status, and returns it.
func onlyKeeperThread(t *testing.T, ds *dataset, status string) map[string]any {
	t.Helper()
	threads := agentThreadsOf(t, ds, "keeper")
	if len(threads) != 1 || threads[0]["status"] != status {
		t.Fatalf("keeper threads = %+v, want one %s", threads, status)
	}
	return threads[0]
}

// expireThreadLease moves a thread's lease a minute into the past, where a
// run that died would leave it once its deadline and slack ran out.
func expireThreadLease(t *testing.T, ds *dataset, thread map[string]any) {
	t.Helper()
	id, _ := thread["__id"].(string)
	if id == "" {
		t.Fatalf("thread without an id: %+v", thread)
	}
	err := ds.inTx(context.Background(), substrate.ActorSystem, true, func(t *txn) error {
		_, err := t.patch(eref{Kind: typeThread, ID: id}, substrate.PatchInput{Properties: map[string]any{
			"leaseUntil": t.now.Add(-time.Minute).Format(time.RFC3339Nano),
		}})
		return err
	})
	if err != nil {
		t.Fatalf("expire the lease: %v", err)
	}
}
