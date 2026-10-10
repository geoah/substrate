package engine

// A RECORD TRIGGER WITH LAG DRAINS WITHOUT A WAKE (#885). A trigger once sat
// on thousands of rows of lag with nothing in flight and nothing parked, and
// moved only when somebody ran `trigger wake`. These tests drive passes back
// to back, as the dispatcher does, and never wake a trigger.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// recordLags reads each named trigger's lag off TriggerStatuses.
func recordLags(t *testing.T, ds *dataset, ids []string) map[string]int64 {
	t.Helper()
	statuses, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	lags := map[string]int64{}
	for _, st := range statuses {
		if want[st.ID] {
			lags[st.ID] = st.Lag
		}
	}
	if len(lags) != len(ids) {
		t.Fatalf("statuses %+v name %d of the triggers %v", statuses, len(lags), ids)
	}
	return lags
}

func TestRecordTriggerBacklogsDrainToZeroWithoutAWake(t *testing.T) {
	// Three record triggers each owe ten widgets, and every delivery outlasts
	// the budget, so a pass delivers at most one widget per trigger and the
	// drain takes at least ten passes. Nothing calls WakeTrigger.
	const records, widgets = 3, 10
	const bound = 90 * time.Second
	defer withTriggerPassBudget(100 * time.Millisecond)()
	ds := laneDataset(t, 250*time.Millisecond, 0, records, widgets)
	ids := make([]string, records)
	for r := range records {
		ids[r] = fmt.Sprintf("a-slow-%d", r)
	}
	for id, lag := range recordLags(t, ds, ids) {
		if lag == 0 {
			t.Fatalf("%s starts with no lag: the test would measure nothing", id)
		}
	}

	began := time.Now()
	stop := dispatchLoop(t, ds)
	deadline := began.Add(bound)
	for {
		lags := recordLags(t, ds, ids)
		drained := countTasks(t, ds, "slow-") == records*widgets
		for _, lag := range lags {
			drained = drained && lag == 0
		}
		if drained {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("after %s: delivered %d of %d widgets, lags %v", bound, countTasks(t, ds, "slow-"), records*widgets, lags)
		}
		time.Sleep(50 * time.Millisecond)
	}
	took := time.Since(began)
	spans := stop()
	t.Logf("three backlogs of %d drained to zero lag in %s over %d passes", widgets, took.Round(time.Millisecond), len(spans))

	// The budget cut every trigger's drain short, so the passes carried it
	// and not one long first pass.
	if len(spans) < widgets {
		t.Fatalf("drained in %d passes, want at least %d: a pass delivered more than one widget per trigger", len(spans), widgets)
	}
	for _, id := range ids {
		if failures, err := ds.TriggerFailures(context.Background(), id); err != nil || len(failures) != 0 {
			t.Fatalf("%s parked deliveries: %+v (%v)", id, failures, err)
		}
		if got := len(okRuns(t, ds, id)); got != widgets {
			t.Fatalf("%s wrote %d ok runs, want one per widget (%d)", id, got, widgets)
		}
	}
}

// passContained runs one pass and fails the test if a panic escapes it.
func passContained(t *testing.T, ds *dataset) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a delivery's panic escaped the pass: %v", r)
		}
	}()
	_, err = ds.ProcessTriggers(context.Background())
	return err
}

func TestARecordTriggerThatPanicsDoesNotStopTheTriggersAfterIt(t *testing.T) {
	// a-0-boom sorts ahead of the slow triggers and runs a function of its
	// own whose every delivery panics in its settlement. A panic that
	// unwound the whole pass kept the triggers after it from ever running.
	const records, widgets = 2, 3
	ctx := context.Background()
	ds := laneDataset(t, 0, 0, records, widgets)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(lanePkg, "boom", map[string]any{
			"description": "a delivery whose settlement panics",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "boom-" + input["envelope"]["change"]["id"], "properties": {"name": "boom"}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install boom: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "a-0-boom",
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{lanePkg + "/widget"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", lanePkg+"/boom"),
		},
	}); err != nil {
		t.Fatalf("put boom trigger: %v", err)
	}
	// The cursor starts at the trigger's creation, past the widgets
	// laneDataset wrote: rewind it so it owes them too.
	if err := ds.ReplayTrigger(ctx, "a-0-boom", 0); err != nil {
		t.Fatalf("replay boom: %v", err)
	}
	boom := substrate.FunctionActor("widgets.test.dev", "widgets", "boom")
	ds.mu.Lock()
	ds.deliveryFault = func(tx *txn) error {
		if tx.actor == boom {
			panic("boom's delivery panicked")
		}
		return nil
	}
	ds.mu.Unlock()

	for pass := range 2 {
		err := passContained(t, ds)
		if err == nil || !strings.Contains(err.Error(), "trigger a-0-boom: delivery panicked: boom's delivery panicked") {
			t.Fatalf("pass %d returned %v, want a-0-boom's panic reported", pass, err)
		}
	}
	for r := range records {
		id := fmt.Sprintf("a-slow-%d", r)
		if got := len(okRuns(t, ds, id)); got != widgets {
			t.Fatalf("%s wrote %d ok runs behind the panicking trigger, want %d", id, got, widgets)
		}
	}
	// The panicking delivery rolled back: nothing of boom's landed, nothing
	// parked, nothing is left running, and its cursor still owes the widgets.
	if n := countTasks(t, ds, "boom-"); n != 0 {
		t.Fatalf("the panicking delivery applied %d effects", n)
	}
	st := triggerStatusOf(t, ds, "a-0-boom")
	if st.Parked != 0 || st.InFlight != 0 || st.Lag == 0 || len(okRuns(t, ds, "a-0-boom")) != 0 {
		t.Fatalf("a-0-boom after its panics: %+v, want nothing parked, run or in flight, and lag left", st)
	}
	if st.LastPassAt == nil || st.LastDeliveredAt != nil {
		t.Fatalf("a-0-boom's last pass %v and last delivery %v, want a pass and no delivery", st.LastPassAt, st.LastDeliveredAt)
	}
}

func TestARunningRecordDeliveryCountsAsInFlight(t *testing.T) {
	// A function delivery writes no row until it settles, so the status read
	// counts the deliveries the server runs now from memory.
	ds := laneDataset(t, 1500*time.Millisecond, 0, 1, 1)
	const id = "a-slow-0"
	if st := triggerStatusOf(t, ds, id); st.InFlight != 0 || st.LastPassAt != nil || st.LastDeliveredAt != nil {
		t.Fatalf("status before any pass: %+v, want nothing in flight and neither moment", st)
	}
	began := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := ds.ProcessTriggers(context.Background())
		done <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		st := triggerStatusOf(t, ds, id)
		if st.InFlight == 1 {
			if st.LastPassAt == nil || st.LastPassAt.Before(began.Add(-time.Second)) || st.LastDeliveredAt != nil {
				t.Fatalf("status mid-delivery: %+v, want this pass and no settled delivery", st)
			}
			if st.Parked != 0 {
				t.Fatalf("the running delivery counts as parked: %+v", st)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the running delivery never counted as in flight: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatalf("process: %v", err)
	}
	// Lag is not checked: the dataset's mirror trigger, after this one in
	// id order, writes past its cursor in the same pass.
	st := triggerStatusOf(t, ds, id)
	if st.InFlight != 0 || st.LastDeliveredAt == nil || st.LastDeliveredAt.Before(*st.LastPassAt) {
		t.Fatalf("status after the pass: %+v, want nothing in flight and a delivery settled after the pass reached it", st)
	}
	if got := len(okRuns(t, ds, id)); got != 1 {
		t.Fatalf("%s wrote %d ok runs, want the one delivery", id, got)
	}
}

func TestARunningAgentRecordDeliveryCountsInFlightOnce(t *testing.T) {
	// An agent delivery's claim row is listed as running and counted in
	// flight already; the same delivery is not counted again.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	arrived := make(chan struct{})
	held := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	fake.script("keep", fakeTurn{content: "kept", arrived: arrived, release: held})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", Properties: map[string]any{"name": "held"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ds.ProcessTriggers(ctx)
		done <- err
	}()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the loop never reached its first model turn")
	}
	if st := triggerStatusOf(t, ds, tr.ID); st.InFlight != 1 || st.Parked != 0 {
		t.Fatalf("status under the running loop: %+v, want one in flight and nothing parked", st)
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("process: %v", err)
	}
	if st := triggerStatusOf(t, ds, tr.ID); st.InFlight != 0 || st.Parked != 0 || st.LastDeliveredAt == nil {
		t.Fatalf("status after the loop settled: %+v, want nothing in flight or parked and a settled delivery", st)
	}
}

// keeperRecordTrigger puts a record trigger on crew widgets that runs the
// keeper agent.
func keeperRecordTrigger(t *testing.T, ds *dataset) string {
	t.Helper()
	tr, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	return tr.ID
}

// heldKeeperTurn is a fake model turn that answers once released, and a
// way to wait until a loop is inside it.
func heldKeeperTurn(t *testing.T) (turn fakeTurn, arrived <-chan struct{}, release func()) {
	t.Helper()
	in := make(chan struct{})
	held := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	return fakeTurn{content: "kept", arrived: in, release: held}, in, release
}

// waitArrived waits for a held turn to be reached.
func waitArrived(t *testing.T, arrived <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s never reached its model turn", what)
	}
}

func TestAHandRetryBesideAReplayedDispatchCountsTwiceInFlight(t *testing.T) {
	// A hand retries a parked agent delivery while a replay sends the same
	// change through the dispatcher again: two loops over one change, two
	// rows in flight. Only the dispatch's own claim row is the dispatch's.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	trigger := keeperRecordTrigger(t, ds)
	fake.script("keep", fakeTurn{status: 500}, fakeTurn{status: 500}, fakeTurn{status: 500})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", Properties: map[string]any{"name": "twice"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	processOnce(t, ds)
	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures after the parked dispatch: %+v (%v)", failures, err)
	}

	retryTurn, retryArrived, releaseRetry := heldKeeperTurn(t)
	dispatchTurn, dispatchArrived, releaseDispatch := heldKeeperTurn(t)
	fake.script("keep", retryTurn, dispatchTurn)
	retried := make(chan error, 1)
	go func() {
		_, err := ds.RetryTriggerFailure(ctx, trigger, failures[0].ID)
		retried <- err
	}()
	waitArrived(t, retryArrived, "the retry")
	if err := ds.ReplayTrigger(ctx, trigger, 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	dispatched := make(chan error, 1)
	go func() {
		_, err := ds.ProcessTriggers(ctx)
		dispatched <- err
	}()
	waitArrived(t, dispatchArrived, "the replayed dispatch")
	if st := triggerStatusOf(t, ds, trigger); st.InFlight != 2 || st.Parked != 0 {
		t.Fatalf("status under both loops: %+v, want two in flight and nothing parked", st)
	}

	releaseRetry()
	releaseDispatch()
	if err := <-retried; err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := <-dispatched; err != nil {
		t.Fatalf("process: %v", err)
	}
	if st := triggerStatusOf(t, ds, trigger); st.InFlight != 0 || st.Parked != 0 {
		t.Fatalf("status after both loops settled: %+v, want nothing in flight or parked", st)
	}
}

func TestAFireThatLosesItsFireStateIsNotStampedDelivered(t *testing.T) {
	// Each fire finds its fire state moved inside its own transaction, the
	// way it does when another dispatcher fired the occurrence first: the
	// fire rolls back and settles nothing, so nothing stamps a delivery.
	ds := laneDataset(t, 0, 0, 0, 0)
	startsAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	putLaneSchedule(t, ds, "b-hourly", "FREQ=HOURLY", startsAt)
	processOnce(t, ds) // initializes the fire state: nothing due yet
	rewindSchedule(t, ds, "b-hourly", startsAt.Add(-time.Minute))
	syncFn := substrate.FunctionActor("widgets.test.dev", "widgets", "sync")
	ds.mu.Lock()
	ds.deliveryFault = func(tx *txn) error {
		if tx.actor != syncFn {
			return nil
		}
		_, err := tx.exec(`UPDATE trigger_schedule SET fired_at = fired_at + interval '1 second' WHERE trigger_id = $1`, "b-hourly")
		return err
	}
	ds.mu.Unlock()

	processOnce(t, ds)
	st := triggerStatusOf(t, ds, "b-hourly")
	if st.LastPassAt == nil || st.LastDeliveredAt != nil {
		t.Fatalf("status after fires that lost their fire state: %+v, want a pass and no delivery", st)
	}
	if runs, n := okRuns(t, ds, "b-hourly"), countTasks(t, ds, "fire-"); len(runs) != 0 || n != 0 {
		t.Fatalf("the fires that lost their fire state wrote %d ok runs and %d tasks", len(runs), n)
	}

	ds.mu.Lock()
	ds.deliveryFault = nil
	ds.mu.Unlock()
	processOnce(t, ds)
	if st := triggerStatusOf(t, ds, "b-hourly"); st.LastDeliveredAt == nil || len(okRuns(t, ds, "b-hourly")) == 0 {
		t.Fatalf("status after the fires settled: %+v, want a delivery", st)
	}
}

func TestAHandRetryThatSettlesStampsTheDelivery(t *testing.T) {
	// The dispatch parks (a settled delivery, so it is stamped), and a hand
	// retry that then delivers the change stamps it again.
	ctx := context.Background()
	ds := laneDataset(t, 0, 0, 1, 1)
	const id = "a-slow-0"
	slow := substrate.FunctionActor("widgets.test.dev", "widgets", "slow")
	ds.mu.Lock()
	ds.deliveryFault = func(tx *txn) error {
		if tx.actor == slow {
			return errors.New("the settlement failed")
		}
		return nil
	}
	ds.mu.Unlock()
	processOnce(t, ds)
	failures, err := ds.TriggerFailures(ctx, id)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures after the parked dispatch: %+v (%v)", failures, err)
	}
	parked := triggerStatusOf(t, ds, id).LastDeliveredAt
	if parked == nil {
		t.Fatal("the parked dispatch was not stamped delivered")
	}

	ds.mu.Lock()
	ds.deliveryFault = nil
	ds.mu.Unlock()
	if _, err := ds.RetryTriggerFailure(ctx, id, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	st := triggerStatusOf(t, ds, id)
	if st.Parked != 0 || st.LastDeliveredAt == nil || !st.LastDeliveredAt.After(*parked) {
		t.Fatalf("status after the retry delivered: %+v, want nothing parked and a delivery after %s", st, parked)
	}
}

func TestAnAgentDeliveryThatPanicsKeepsItsClaimAsInterrupted(t *testing.T) {
	// An agent delivery commits its claim, moving the cursor past the
	// change, before its loop runs; the panic comes in the completion after
	// the loop. The pass contains it, the claim row stays and reads as
	// interrupted, and nothing delivers the change again by itself.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	trigger := keeperRecordTrigger(t, ds)
	fake.script("keep", fakeTurn{content: "kept"})
	keeper := substrate.AgentActor("crew.test.dev", "crew", "keeper")
	ds.mu.Lock()
	ds.deliveryFault = func(tx *txn) error {
		if tx.actor == keeper {
			panic("the completion panicked")
		}
		return nil
	}
	ds.mu.Unlock()
	w, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", Properties: map[string]any{"name": "held"},
	})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	change, err := ds.latestChangeOf(ctx, crewPackage+"/widget", w.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := passContained(t, ds); err == nil || !strings.Contains(err.Error(), "delivery panicked: the completion panicked") {
		t.Fatalf("the pass returned %v, want the completion's panic reported", err)
	}
	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 || failures[0].Running || failures[0].LastError != interruptedAgentError {
		t.Fatalf("failures after the panic: %+v (%v), want the claim alone, interrupted", failures, err)
	}
	st := triggerStatusOf(t, ds, trigger)
	if st.Cursor < change.Seq || st.Parked != 1 || st.InFlight != 0 || st.LastParkedError != interruptedAgentError || st.LastDeliveredAt != nil {
		t.Fatalf("status after the panic: %+v, want the cursor past seq %d, the claim parked as interrupted and no delivery", st, change.Seq)
	}

	// The next pass runs no second loop over the change.
	if err := passContained(t, ds); err != nil {
		t.Fatalf("the next pass: %v", err)
	}
	if n := len(fake.requestsOf("keep")); n != 1 {
		t.Fatalf("the model saw %d requests, want the one loop", n)
	}
}
