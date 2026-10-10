package engine_test

// Trigger health on FUNCTION triggers (#879, decision 0152): the function
// path's settlement resolves the failing alert, the newest ok run outlives
// twenty skips, a skip neither clears nor breaks a streak, a disabled
// trigger raises nothing, and a webhook trigger's failing fires open the
// alert. The read's clock is a TestClock, advanced past the window.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const failingWindow = 10 * time.Minute

// pastFailingWindow moves the read's clock past the window and past the
// read's own once-a-minute bound.
const pastFailingWindow = failingWindow + 2*time.Minute

// boomHealthDataset is boomDataset with the ten-minute window on a test
// clock: one trigger, guarded on the widget's `mode`, on a body that raises.
func boomHealthDataset(t *testing.T) (substrate.Dataset, *engine.TestClock) {
	t.Helper()
	clock := &engine.TestClock{}
	_, ds := newDataset(t, engine.WithHealthFailingAfter(failingWindow), engine.WithTestHealthClock(clock.Now))
	if err := enginetest.Install(context.Background(), ds, owner, fnConnector(
		[]enginetest.Trigger{trigOn("boom", map[string]any{
			"kinds": []any{widgetType},
			"when":  `record != null && record.properties.mode == "go"`,
		})},
		pyFn("boom", map[string]any{}, []any{taskType}, raisingSource))); err != nil {
		t.Fatalf("install: %v", err)
	}
	return ds, clock
}

func failingAlertState(t *testing.T, ds substrate.Dataset, triggerID string) any {
	t.Helper()
	rec, err := ds.Get(context.Background(), alertKind, "trigger.failing/"+triggerID)
	if err != nil {
		return nil
	}
	return rec.Properties["state"]
}

func putSkippedWidget(t *testing.T, ds substrate.Dataset, id string) {
	t.Helper()
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: id, Properties: map[string]any{"name": id, "mode": "skip"},
	})
}

// A function trigger fails for the window and reads failing; the dispatched
// delivery that succeeds after the fix settles in the function path's one
// transaction and resolves the alert there.
func TestAFunctionTriggerFailingForTheWindowClearsOnItsFirstOkDelivery(t *testing.T) {
	t.Parallel()
	ds, clock := boomHealthDataset(t)
	triggerID, _ := parkOne(t, ds)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	st := statusOf(t, ds, triggerID)
	if st.Health != substrate.HealthFailing || st.FailingSince == nil {
		t.Fatalf("after the window the trigger reads %q since %v, want failing", st.Health, st.FailingSince)
	}
	if got := failingAlertState(t, ds, triggerID); got != "open" {
		t.Fatalf("the failing alert is %v, want open", got)
	}

	fixBoom(t, ds)
	putWidget(t, ds, "w2")
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "resolved" {
		t.Fatalf("after an ok delivery the failing alert is %v, want resolved", got)
	}
	st = statusOf(t, ds, triggerID)
	if st.Health != substrate.HealthOK || st.FailingSince != nil || st.LastOkAt == nil {
		t.Fatalf("after an ok delivery the trigger reads %q since %v, last ok %v; want ok with a last ok", st.Health, st.FailingSince, st.LastOkAt)
	}
}

// REPRODUCTION 2. A park, an ok run, then more skips than the retention
// keeps: the ok run survives the prune, so past the window the old park is
// not read as a current streak.
func TestTheNewestOkRunOutlivesTheRetention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := boomHealthDataset(t)
	triggerID, _ := parkOne(t, ds)
	fixBoom(t, ds)
	putWidget(t, ds, "w2")
	process(t, ds)
	for i := range 21 {
		putSkippedWidget(t, ds, "skip"+string(rune('a'+i)))
	}
	process(t, ds)

	runs, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{triggerRunType},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath("substrate.reamde.dev/core/trigger", triggerID)},
		},
		First: 100,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	byStatus := map[any]int{}
	for _, r := range runs.Records {
		byStatus[r.Properties["status"]]++
	}
	if byStatus["ok"] != 1 || byStatus["parked"] != 1 || byStatus["skipped"] != 20 {
		t.Fatalf("the retention kept %v, want the ok run, the park and twenty skips", byStatus)
	}

	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != nil {
		t.Fatalf("the old park read as a current streak once its ok run aged: alert %v", got)
	}
	if st := statusOf(t, ds, triggerID); st.Health != substrate.HealthOK || st.LastOkAt == nil {
		t.Fatalf("the trigger reads %q, last ok %v; want ok with the kept ok run", st.Health, st.LastOkAt)
	}
}

// A skip says nothing about whether the callable works: it neither breaks a
// streak nor resolves an open alert.
func TestASkipNeitherClearsNorBreaksAFailingStreak(t *testing.T) {
	t.Parallel()
	ds, clock := boomHealthDataset(t)
	triggerID, _ := parkOne(t, ds)
	putSkippedWidget(t, ds, "s1")
	process(t, ds)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "open" {
		t.Fatalf("a skip after the park kept the alert from opening: %v", got)
	}
	putSkippedWidget(t, ds, "s2")
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "open" {
		t.Fatalf("a skip resolved the failing alert: %v", got)
	}
}

// A disabled trigger runs nothing, so it raises nothing, however old its
// parks.
func TestADisabledTriggerRaisesNoFailingAlert(t *testing.T) {
	t.Parallel()
	ds, clock := boomHealthDataset(t)
	triggerID, _ := parkOne(t, ds)
	setBoomEnabled(t, ds, false)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != nil {
		t.Fatalf("a disabled trigger raised a failing alert: %v", got)
	}
	if st := statusOf(t, ds, triggerID); st.Health != substrate.HealthOK {
		t.Fatalf("the disabled trigger reads %q, want ok", st.Health)
	}
}

// A WEBHOOK trigger whose fires park opens its alert like any other.
func TestAWebhookTriggerWhoseFiresFailOpensTheAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := boomHealthDataset(t)
	tr, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger",
		Properties: map[string]any{
			"source":   map[string]any{"webhook": map[string]any{}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", fnPackage+"/boom"),
		},
	})
	if err != nil {
		t.Fatalf("put the webhook trigger: %v", err)
	}
	if _, err := ds.WakeTrigger(ctx, tr.ID); err != nil {
		t.Fatalf("wake: %v", err)
	}
	if st := statusOf(t, ds, tr.ID); st.Parked != 1 {
		t.Fatalf("the webhook fire parked %d deliveries, want 1", st.Parked)
	}
	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, tr.ID); got != "open" {
		t.Fatalf("the webhook trigger's failing alert is %v, want open", got)
	}
	if st := statusOf(t, ds, tr.ID); st.Health != substrate.HealthFailing {
		t.Fatalf("the webhook trigger reads %q, want failing", st.Health)
	}
}
