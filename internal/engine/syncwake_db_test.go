package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// The guard every shipped provider's on-request trigger carries, less the
// connection check: a request its acknowledgement has not answered.
const onRequestWhen = `record != null && "syncRequestedAt" in record.properties && (!("syncRequestedAck" in record.properties) || string(record.properties.syncRequestedAck) != string(record.properties.syncRequestedAt))`

// wakeRaceSource is a paged sync the way a provider writes one: page 0
// acknowledges the request and asks for page 1, which fails after a while.
// The acknowledgement commits with page 0, so from then on the guard reads
// false for the change still in hand.
const wakeRaceSource = `
import time
def main(input, host):
    env = input["envelope"]
    if not input.get("resume"):
        props = env["record"]["properties"]
        return {"effects": [{"action": "patch", "kind": env["change"]["kind"], "id": env["change"]["id"],
                             "properties": {"syncRequestedAck": props["syncRequestedAt"]}}],
                "more": {"cursor": 1}}
    time.sleep(2)
    raise RuntimeError("fake API answered HTTP 400")
`

// awaitProp polls the record until its property is set, and fails the test
// after ten seconds.
func awaitProp(t *testing.T, ds substrate.Dataset, kind, id, prop string, want func(any) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := mustGet(t, ds, kind, id)
		if want(got.Properties[prop]) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never reached the wanted value: %v", prop, got.Properties)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAWakeAndAPassWalkOneTriggerOneAtATime is the Sync now race of the
// #1005 rehearsal (decision 0182): a wake delivers the request, its first
// page acknowledges it, and the dispatcher's pass ticks while the second
// page fails. The pass must not skip the change in hand and move the cursor
// from under the wake: the park, the failure and `erroring` all land.
func TestAWakeAndAPassWalkOneTriggerOneAtATime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newSyncKindDataset(t,
		[]enginetest.Trigger{trigOn("wakerace", map[string]any{
			"kinds": []any{jobType}, "ops": []any{"create", "update"}, "when": onRequestWhen,
		})},
		pyFn("wakerace", map[string]any{"timeout": "PT30S"}, []any{jobType}, wakeRaceSource),
	)
	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox"}})
	process(t, ds)
	skippedBefore := len(runRowsOf(t, ds, trigID("wakerace"), "skipped"))

	mustPatch(t, ds, owner, jobType, job.ID, substrate.PatchInput{Properties: map[string]any{
		"syncRequestedAt": "2026-10-20T12:00:00Z",
	}})
	type woke struct {
		ran int
		err error
	}
	done := make(chan woke, 1)
	go func() {
		ran, err := ds.WakeTrigger(ctx, trigID("wakerace"))
		done <- woke{ran, err}
	}()
	// Page 0 committed: page 1 sleeps, and the guard now reads false.
	awaitProp(t, ds, jobType, job.ID, "syncRequestedAck", func(v any) bool { return v != nil })

	// The dispatcher's tick, while the wake's delivery is in hand.
	var w woke
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	timeout := time.After(30 * time.Second)
loop:
	for {
		select {
		case w = <-done:
			break loop
		case <-tick.C:
			if _, err := ds.ProcessTriggers(ctx); err != nil {
				t.Fatalf("pass during the wake: %v", err)
			}
		case <-timeout:
			t.Fatal("the wake never returned")
		}
	}
	if w.err != nil {
		t.Fatalf("wake: %v", w.err)
	}

	failures, err := ds.TriggerFailures(ctx, trigID("wakerace"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want the one parked run", failures)
	}
	if e := failures[0].LastError; !strings.Contains(e, "HTTP 400") || !strings.Contains(e, "paged drain parked mid-chain") {
		t.Fatalf("failure error = %q, want the body's cause on a parked chain", e)
	}
	got := mustGet(t, ds, jobType, job.ID)
	if got.Properties["syncState"] != substrate.SyncStateErroring {
		t.Fatalf("syncState = %v, want erroring (properties %v)", got.Properties["syncState"], got.Properties)
	}
	if msg, _ := got.Properties["syncError"].(string); !strings.Contains(msg, "HTTP 400") {
		t.Fatalf("syncError = %q, want the run's cause", msg)
	}
	if got.Properties["syncErrorAt"] == nil {
		t.Fatalf("syncErrorAt is missing: %v", got.Properties)
	}
	if n := len(runRowsOf(t, ds, trigID("wakerace"), "parked")); n != 1 {
		t.Fatalf("parked runs = %d, want 1", n)
	}
	if n := len(runRowsOf(t, ds, trigID("wakerace"), "skipped")); n != skippedBefore {
		t.Fatalf("skipped runs = %d, want %d: the pass skipped the change the wake held", n, skippedBefore)
	}
	if st := statusOf(t, ds, trigID("wakerace")); st.Lag != 0 || st.Parked != 1 {
		t.Fatalf("trigger status = %+v, want no lag and one parked", st)
	}
	if n := process(t, ds); n != 0 {
		t.Fatalf("a pass after the park ran %d, want 0", n)
	}
}

// TestAWakeOfATriggerThePassWalksRunsNothingAndWaitsForNothing: a Sync now
// that lands while the pass delivers the trigger answers 0 at once rather
// than wait for the delivery in hand, and the pass's walk settles as it
// would alone.
func TestAWakeOfATriggerThePassWalksRunsNothingAndWaitsForNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newSyncKindDataset(t,
		[]enginetest.Trigger{trigOn("slowwalk", map[string]any{"kinds": []any{jobType}, "ops": []any{"update"}})},
		pyFn("slowwalk", map[string]any{"timeout": "PT30S"}, []any{jobType}, `
import time
def main(input, host):
    env = input["envelope"]
    time.sleep(2)
    return {"effects": [{"action": "patch", "kind": env["change"]["kind"], "id": env["change"]["id"],
                         "properties": {"lastSyncedAt": "2026-10-20T12:00:00Z"}}]}
`),
	)
	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox"}})
	process(t, ds)
	mustPatch(t, ds, owner, jobType, job.ID, substrate.PatchInput{Properties: map[string]any{"name": "inbox 2"}})

	passed := make(chan error, 1)
	go func() {
		_, err := ds.ProcessTriggers(ctx)
		passed <- err
	}()
	// `running` is stamped before the body: the pass's delivery is in hand.
	awaitProp(t, ds, jobType, job.ID, "syncState", func(v any) bool { return v == substrate.SyncStateRunning })

	start := time.Now()
	ran, err := ds.WakeTrigger(ctx, trigID("slowwalk"))
	if err != nil {
		t.Fatalf("wake: %v", err)
	}
	if ran != 0 {
		t.Fatalf("wake ran %d beside the pass's walk, want 0", ran)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wake took %s: it waited for the pass's delivery", elapsed)
	}
	if err := <-passed; err != nil {
		t.Fatalf("pass: %v", err)
	}

	if n := len(runRowsOf(t, ds, trigID("slowwalk"), "ok")); n != 1 {
		t.Fatalf("ok runs = %d, want the pass's one", n)
	}
	if got := mustGet(t, ds, jobType, job.ID); got.Properties["syncState"] != substrate.SyncStateOK {
		t.Fatalf("syncState = %v, want ok", got.Properties["syncState"])
	}
	if n := len(runRowsOf(t, ds, trigID("slowwalk"), "parked")); n != 0 {
		t.Fatalf("parked runs = %d, want 0", n)
	}
	// Nothing owed: a second wake walks, finds nothing and runs nothing.
	if ran, err := ds.WakeTrigger(ctx, trigID("slowwalk")); err != nil || ran != 0 {
		t.Fatalf("second wake = %d, %v; want 0, nil", ran, err)
	}
	if n := len(runRowsOf(t, ds, trigID("slowwalk"), "ok")); n != 1 {
		t.Fatalf("ok runs after the second wake = %d, want 1", n)
	}
}
