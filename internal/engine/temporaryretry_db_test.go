package engine_test

// A temporary failure is retried over minutes (decision 0266): the body says
// so with host.Temporary, the delivery parks at once under an error that says
// when it is retried, no alert is raised, and the walk each dispatcher pass
// runs retries it on its own. A permanent failure is still set aside at once.
// The walk's clock is a TestClock, advanced past each wait.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// outageSource fails temporarily while the outage gadget's wire reads
// "down", the way a body whose provider is unreachable would, and delivers
// a task otherwise.
var outageSource = fmt.Sprintf(`
def main(input, host):
    flag = host.records.get(%q, %q)
    if flag and flag["properties"].get("wire") == "down":
        raise host.Temporary("the provider is down")
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": %q,
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": "delivered"}}]}
`, gadgetType, fid("outage"), taskType)

// outageDataset is one trigger on widgets bound to outageSource, with the
// walk's clock on a TestClock and the outage on.
func outageDataset(t *testing.T, source string) (substrate.Dataset, *engine.TestClock) {
	t.Helper()
	clock := &engine.TestClock{}
	_, ds := newDataset(t, engine.WithTestRetryClock(clock.Now))
	fn := pyFn("sync", map[string]any{"permissions": map[string]any{
		"reads": map[string]any{"kinds": []any{gadgetType}},
	}}, []any{taskType}, source)
	if err := enginetest.Install(context.Background(), ds, owner, fnConnector(
		[]enginetest.Trigger{trigOn("sync", map[string]any{"kinds": []any{widgetType}})}, fn)); err != nil {
		t.Fatalf("install: %v", err)
	}
	setOutage(t, ds, "down")
	return ds, clock
}

func setOutage(t *testing.T, ds substrate.Dataset, wire string) {
	t.Helper()
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: gadgetType, ID: fid("outage"), Properties: map[string]any{"wire": wire},
	})
}

// parkedRows is the trigger's parked deliveries.
func parkedRows(t *testing.T, ds substrate.Dataset) []substrate.TriggerFailure {
	t.Helper()
	rows, err := ds.TriggerFailures(context.Background(), trigID("sync"))
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	return rows
}

// passUntil runs a dispatcher pass and waits for the detached walk it starts
// to leave the parked rows as want says.
func passUntil(t *testing.T, ds substrate.Dataset, what string, want func([]substrate.TriggerFailure) bool) []substrate.TriggerFailure {
	t.Helper()
	process(t, ds)
	deadline := time.Now().Add(20 * time.Second)
	for {
		rows := parkedRows(t, ds)
		if want(rows) {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: parked %+v", what, rows)
		}
		time.Sleep(50 * time.Millisecond)
		// A pass that met the previous walk still running started none.
		process(t, ds)
	}
}

// waitingAt reads a waiting row's attempt count, failing on a row that does
// not wait for its automatic retry.
func waitingAt(t *testing.T, rows []substrate.TriggerFailure) int {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("parked %d deliveries, want 1: %+v", len(rows), rows)
	}
	if !strings.HasPrefix(rows[0].LastError, "temporary, retried automatically at ") ||
		!strings.Contains(rows[0].LastError, "the provider is down") {
		t.Fatalf("the parked row does not wait for its retry: %q", rows[0].LastError)
	}
	return rows[0].Attempts
}

func parkedAlert(t *testing.T, ds substrate.Dataset) bool {
	t.Helper()
	_, err := ds.Get(context.Background(), alertKind, "trigger.parked/"+trigID("sync"))
	return err == nil
}

func deliveredTask(t *testing.T, ds substrate.Dataset) bool {
	t.Helper()
	_, err := ds.Get(context.Background(), taskType, "t-"+fid("w1"))
	return err == nil
}

// THE DONE-WHEN: a one-minute outage needs no person. On the walk's clock,
// the delivery fails as temporary at 0s, is retried and fails at 15s and at
// 45s, the outage ends at 60s, and the retry due at 1m45s delivers. No retry
// runs before its time, nothing is set aside and no alert is raised.
func TestATemporaryFailureForAMinuteNeedsNoPerson(t *testing.T) {
	t.Parallel()
	ds, clock := outageDataset(t, outageSource)
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1"},
	})
	rows := passUntil(t, ds, "the first delivery to park", func(rows []substrate.TriggerFailure) bool { return len(rows) == 1 })
	if got := waitingAt(t, rows); got != 1 {
		t.Fatalf("the first park is at attempt %d, want 1: a temporary failure is not tried again in the pass", got)
	}
	if st := statusOf(t, ds, trigID("sync")); st.Lag != 0 {
		t.Fatalf("the trigger's cursor waits behind the retry: lag %d", st.Lag)
	}
	// notYet advances the clock short of the next retry and finds the row
	// as it was.
	notYet := func(d time.Duration, attempts int) {
		t.Helper()
		clock.Advance(d)
		process(t, ds)
		time.Sleep(300 * time.Millisecond)
		if got := waitingAt(t, parkedRows(t, ds)); got != attempts {
			t.Fatalf("a retry ran before its time: attempt %d, want %d", got, attempts)
		}
	}
	// retried advances the clock to the retry and waits for it to fail
	// again.
	retried := func(d time.Duration, attempts int) {
		t.Helper()
		clock.Advance(d)
		waitingAt(t, passUntil(t, ds, fmt.Sprintf("retry %d", attempts-1), func(rows []substrate.TriggerFailure) bool {
			return len(rows) == 1 && rows[0].Attempts == attempts
		}))
	}

	notYet(10*time.Second, 1) // 10s
	retried(5*time.Second, 2) // 15s, due 15s after the park at 0s
	notYet(25*time.Second, 2) // 40s
	retried(5*time.Second, 3) // 45s, due 30s after the park at 15s
	if parkedAlert(t, ds) {
		t.Fatal("a delivery the server retries by itself raised the parked alert")
	}
	notYet(15*time.Second, 3) // 60s
	setOutage(t, ds, "up")    // the outage ends at 60s
	notYet(40*time.Second, 3) // 1m40s

	clock.Advance(5 * time.Second) // 1m45s, due a minute after the park at 45s
	passUntil(t, ds, "the retry that delivers", func(rows []substrate.TriggerFailure) bool { return len(rows) == 0 })
	if !deliveredTask(t, ds) {
		t.Fatal("the row retired and the delivery's task is not there")
	}
	if parkedAlert(t, ds) {
		t.Fatal("the parked alert was raised for a delivery nobody had to retry")
	}
}

// A function that may run an agent, through `agents` or through a function
// it `call`s, is not retried unattended: a retry would run the agent and its
// writes again. Its temporary failure is set aside for a person at once.
func TestATemporaryFailureOfAFunctionThatMayRunAnAgentIsSetAside(t *testing.T) {
	t.Parallel()
	clock := &engine.TestClock{}
	_, ds := newDataset(t, engine.WithTestRetryClock(clock.Now))
	sync := pyFn("sync", map[string]any{"permissions": map[string]any{
		"reads": map[string]any{"kinds": []any{gadgetType}},
		"call":  []any{fnPackage + "/helper"},
	}}, []any{taskType}, outageSource)
	helper := pyFn("helper", map[string]any{}, []any{}, "def main(input, host):\n    return {}\n")
	if err := enginetest.Install(context.Background(), ds, owner, fnConnector(
		[]enginetest.Trigger{trigOn("sync", map[string]any{"kinds": []any{widgetType}})}, helper, sync)); err != nil {
		t.Fatalf("install: %v", err)
	}
	setOutage(t, ds, "down")
	// The widget changes, and the sync puts its task back.
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1, changed"},
	})
	rows := passUntil(t, ds, "the delivery to park", func(rows []substrate.TriggerFailure) bool { return len(rows) == 1 })
	if strings.HasPrefix(rows[0].LastError, "temporary") || !strings.Contains(rows[0].LastError, "the provider is down") {
		t.Fatalf("the delivery of a function that may run an agent parked as %q, want it set aside", rows[0].LastError)
	}
	if !parkedAlert(t, ds) {
		t.Fatal("a delivery set aside for a person raised no alert")
	}
	clock.Advance(time.Hour)
	process(t, ds)
	time.Sleep(300 * time.Millisecond)
	if rows := parkedRows(t, ds); len(rows) != 1 || rows[0].Attempts != 1 {
		t.Fatalf("it was retried on its own: %+v", rows)
	}
}

// A permanent failure is still set aside at once: no wait, no automatic
// retry however far the clock moves, and the alert is raised.
func TestAPermanentFailureIsStillSetAside(t *testing.T) {
	t.Parallel()
	ds, clock := outageDataset(t, strings.Replace(outageSource,
		`raise host.Temporary("the provider is down")`, `raise Exception("the body is broken")`, 1))
	// The widget changes, and the sync puts its task back.
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1, changed"},
	})
	rows := passUntil(t, ds, "the delivery to park", func(rows []substrate.TriggerFailure) bool { return len(rows) == 1 })
	if strings.HasPrefix(rows[0].LastError, "temporary") || !strings.Contains(rows[0].LastError, "the body is broken") {
		t.Fatalf("a permanent failure parked as %q", rows[0].LastError)
	}
	if !parkedAlert(t, ds) {
		t.Fatal("a delivery set aside for a person raised no alert")
	}
	// An ordinary exception says nothing of whether it may pass, so the
	// dispatch still tries it three times before it sets it aside, as before
	// the waits (the step's disclosed deviation): only a failure that says it
	// is temporary skips that.
	attempts := rows[0].Attempts
	if attempts != 3 {
		t.Fatalf("a permanent failure parked after %d attempts, want the dispatch's three", attempts)
	}
	setOutage(t, ds, "up")
	clock.Advance(2 * time.Hour)
	process(t, ds)
	time.Sleep(300 * time.Millisecond)
	if rows := parkedRows(t, ds); len(rows) != 1 || rows[0].Attempts != attempts {
		t.Fatalf("a permanent failure was retried on its own: %+v", rows)
	}
}

// An outage that outlasts the waits ends set aside for a person: eight
// retries, then the row stops waiting and the alert is raised.
func TestATemporaryFailurePastItsRetriesIsSetAside(t *testing.T) {
	t.Parallel()
	ds, clock := outageDataset(t, outageSource)
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1"},
	})
	passUntil(t, ds, "the first delivery to park", func(rows []substrate.TriggerFailure) bool { return len(rows) == 1 })
	gaps := []time.Duration{
		15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
		4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute,
	}
	for i, gap := range gaps {
		clock.Advance(gap)
		rows := passUntil(t, ds, fmt.Sprintf("retry %d", i+1), func(rows []substrate.TriggerFailure) bool {
			return len(rows) == 1 && rows[0].Attempts == i+2
		})
		last := i == len(gaps)-1
		if waits := strings.HasPrefix(rows[0].LastError, "temporary, retried automatically at "); waits == last {
			t.Fatalf("after retry %d the row reads %q", i+1, rows[0].LastError)
		}
		if parkedAlert(t, ds) != last {
			t.Fatalf("after retry %d the parked alert is raised=%v", i+1, !last)
		}
	}
	clock.Advance(2 * time.Hour)
	process(t, ds)
	time.Sleep(300 * time.Millisecond)
	if rows := parkedRows(t, ds); len(rows) != 1 || rows[0].Attempts != len(gaps)+1 {
		t.Fatalf("a row set aside was retried on its own: %+v", rows)
	}
}

// A row waiting for its retry whose function is changed meanwhile to one that
// may run an agent is set aside when the retry falls due, not run: the walk
// reads the callable as it stands then.
func TestAWaitingRowWhoseFunctionMayNowRunAnAgentIsSetAside(t *testing.T) {
	t.Parallel()
	ds, clock := outageDataset(t, outageSource)
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1"},
	})
	waitingAt(t, passUntil(t, ds, "the first delivery to park", func(rows []substrate.TriggerFailure) bool { return len(rows) == 1 }))

	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		pyFn("helper", map[string]any{}, []any{}, "def main(input, host):\n    return {}\n"),
		pyFn("sync", map[string]any{"permissions": map[string]any{
			"reads": map[string]any{"kinds": []any{gadgetType}},
			"call":  []any{fnPackage + "/helper"},
		}}, []any{taskType}, outageSource),
	}); err != nil {
		t.Fatalf("grant the function a call: %v", err)
	}
	// The outage is over, so a retry that ran would deliver and retire the
	// row.
	setOutage(t, ds, "up")
	clock.Advance(15 * time.Second)
	rows := passUntil(t, ds, "the row to be set aside", func(rows []substrate.TriggerFailure) bool {
		return len(rows) == 1 && !strings.HasPrefix(rows[0].LastError, "temporary")
	})
	if rows[0].Attempts != 1 || !strings.Contains(rows[0].LastError, "the provider is down") {
		t.Fatalf("the row set aside reads %+v, want attempt 1 and the error it waited on", rows[0])
	}
	if deliveredTask(t, ds) {
		t.Fatal("the walk ran a function that may now run an agent")
	}
	if !parkedAlert(t, ds) {
		t.Fatal("a delivery set aside for a person raised no alert")
	}
}

// A sync that puts a record back at an id whose permanent delete is still
// erasing what the record owned meets the refusal (substrate.ErrErasing),
// which waits for the walk as a temporary failure does: nobody retries it by
// hand and no alert is raised, and the retry due after the compaction pass
// has taken the closure delivers.
func TestADeliveryRefusedWhileErasingGetsThroughOnItsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &engine.TestClock{}
	_, ds, dsn := newDatasetWithDSN(t, engine.WithTestRetryClock(clock.Now), engine.WithTestErasureBatch(1), engine.WithCompactionInterval(-1))
	fn := pyFn("sync", map[string]any{"permissions": map[string]any{
		"reads": map[string]any{"kinds": []any{gadgetType}},
	}}, []any{taskType}, outageSource)
	if err := enginetest.Install(ctx, ds, owner, fnConnector(
		[]enginetest.Trigger{trigOn("sync", map[string]any{"kinds": []any{widgetType}})}, fn)); err != nil {
		t.Fatalf("install: %v", err)
	}
	// The task the sync writes for w1, owning three logs, so the delete's
	// one batch leaves two.
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1"},
	})
	process(t, ds)
	if !deliveredTask(t, ds) {
		t.Fatal("the sync did not write its task")
	}
	id := "t-" + fid("w1")
	for range 3 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/tasks/tasklog", Properties: map[string]any{
			"task": id, "status": "done", "at": "2026-09-01T09:00:00Z", "scheduledAt": "2026-09-01T09:00:00Z",
		}})
	}
	gone, err := ds.Delete(ctx, owner, taskType, id, substrate.DeleteInput{Purge: true})
	if err != nil || gone.Erasure == nil || !gone.Erasure.Pending {
		t.Fatalf("the permanent delete answered %+v (%v), want what it owns still to go", gone, err)
	}
	// The widget changes, and the sync puts its task back.
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: fid("w1"), Properties: map[string]any{"name": "w1, changed"},
	})
	rows := passUntil(t, ds, "the delivery to park", func(rows []substrate.TriggerFailure) bool { return len(rows) == 1 })
	if !strings.HasPrefix(rows[0].LastError, "temporary, retried automatically at ") || !strings.Contains(rows[0].LastError, "being erased") {
		t.Fatalf("the refused delivery parked as %q, want it waiting for its retry", rows[0].LastError)
	}
	if parkedAlert(t, ds) {
		t.Fatal("a delivery the server retries by itself raised the parked alert")
	}
	db := rawDB(t, dsn)
	for pass := 0; ; pass++ {
		if _, err := ds.(passer).CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		var owned bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM compaction_erasures WHERE owned)`).Scan(&owned); err != nil {
			t.Fatal(err)
		}
		if !owned {
			break
		}
		if pass == 5 {
			t.Fatal("the closure still stands after six passes")
		}
	}
	clock.Advance(15 * time.Second)
	passUntil(t, ds, "the retry that delivers", func(rows []substrate.TriggerFailure) bool { return len(rows) == 0 })
	if !deliveredTask(t, ds) {
		t.Fatal("the row retired and the delivery's task is not there")
	}
	if parkedAlert(t, ds) {
		t.Fatal("the parked alert was raised for a delivery nobody had to retry")
	}
}
