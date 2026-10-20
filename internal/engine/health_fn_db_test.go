package engine_test

// Trigger health on FUNCTION triggers (#879, decision 0152): the function
// path's settlement resolves the failing alert, the newest ok run outlives
// twenty skips, a skip neither clears nor breaks a streak, a disabled
// trigger raises nothing and its open alert resolves (decision 0177), a
// retry that skips the last park resolves the alert, parked runs older than
// the newest ok run leave with the retention (decision 0178), and a webhook
// trigger's failing fires open the alert. The read's clock is a TestClock,
// advanced past the window.

import (
	"context"
	"fmt"
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

// runsByStatus counts the trigger's live runs by status.
func runsByStatus(t *testing.T, ds substrate.Dataset, triggerID string) map[any]int {
	t.Helper()
	runs, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{triggerRunType},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath("substrate.reamde.dev/core/trigger", triggerID)},
		},
		First: 100,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	out := map[any]int{}
	for _, r := range runs.Records {
		out[r.Properties["status"]]++
	}
	return out
}

// A retry whose guard no longer matches settles the last park as a skip:
// the row retires and, with nothing left standing, the failing alert
// resolves in the same transaction (decision 0177).
func TestARetryThatSkipsTheLastParkResolvesTheFailingAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := boomHealthDataset(t)
	triggerID, parked := parkOne(t, ds)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "open" {
		t.Fatalf("the failing alert is %v, want open", got)
	}

	mustPatch(t, ds, fnActor, widgetType, "w1", substrate.PatchInput{Properties: map[string]any{"mode": "skip"}})
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(left) != 0 {
		t.Fatalf("parked %+v (%v) after the skipped retry, want none", left, err)
	}
	if got := failingAlertState(t, ds, triggerID); got != "resolved" {
		t.Fatalf("after the skipped retry the failing alert is %v, want resolved", got)
	}
	if st := statusOf(t, ds, triggerID); st.Health != substrate.HealthOK || st.Parked != 0 {
		t.Fatalf("the trigger reads %q with %d parked, want ok with none", st.Health, st.Parked)
	}
}

// Disabling a trigger resolves its open failing alert at the next pass.
// Enabling it again reopens nothing from the parks before the resolve, as
// after a resolve by hand; a park after it does.
func TestDisablingATriggerResolvesItsFailingAlert(t *testing.T) {
	t.Parallel()
	ds, clock := boomHealthDataset(t)
	triggerID, _ := parkOne(t, ds)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "open" {
		t.Fatalf("the failing alert is %v, want open", got)
	}

	setBoomEnabled(t, ds, false)
	clock.Advance(2 * time.Minute)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "resolved" {
		t.Fatalf("after the disable the failing alert is %v, want resolved", got)
	}
	if st := statusOf(t, ds, triggerID); st.Health != substrate.HealthOK || st.Parked != 1 {
		t.Fatalf("the disabled trigger reads %q with %d parked, want ok with its park listed", st.Health, st.Parked)
	}

	setBoomEnabled(t, ds, true)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	if got := failingAlertState(t, ds, triggerID); got != "resolved" {
		t.Fatalf("re-enabling reopened the alert from the old park: %v", got)
	}
	putWidget(t, ds, "w2")
	process(t, ds)
	clock.Advance(pastFailingWindow)
	process(t, ds)
	rec, err := ds.Get(context.Background(), alertKind, "trigger.failing/"+triggerID)
	if err != nil || rec.Properties["state"] != "open" {
		t.Fatalf("a park after the resolve left the alert %v (%v), want open", rec, err)
	}
	if got := fmt.Sprint(rec.Properties["count"]); got != "1" {
		t.Fatalf("the reopened alert counts %s parks, want w2's", got)
	}
}

// Retention (decision 0178): a parked run older than the trigger's newest
// ok run, whose delivery no longer stands, leaves with the runs past the
// twenty. A park still standing keeps its run, and so do the parks after
// the newest ok run, which health counts.
func TestParkedRunsOlderThanTheNewestOkRunLeaveWithTheRetention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, clock := boomHealthDataset(t)
	triggerID := trigID("boom")
	for _, w := range []string{"w1", "w2", "w3", "w4"} {
		putWidget(t, ds, w)
	}
	process(t, ds)
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 4 {
		t.Fatalf("parked %+v (%v), want four", failures, err)
	}
	for _, f := range failures[:3] {
		if err := ds.ForgetTriggerFailure(ctx, triggerID, f.ID); err != nil {
			t.Fatalf("forget %d: %v", f.ID, err)
		}
	}
	fixBoom(t, ds)
	putWidget(t, ds, "ok")
	process(t, ds)
	for i := range 20 {
		putSkippedWidget(t, ds, "skip"+string(rune('a'+i)))
	}
	process(t, ds)
	if got := runsByStatus(t, ds, triggerID); got["ok"] != 1 || got["parked"] != 1 || got["skipped"] != 20 {
		t.Fatalf("the retention kept %v, want the ok run, the standing park and twenty skips", got)
	}

	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		pyFn("boom", map[string]any{}, []any{taskType}, raisingSource),
	}); err != nil {
		t.Fatalf("apply the raising body: %v", err)
	}
	putWidget(t, ds, "w5")
	putWidget(t, ds, "w6")
	process(t, ds)
	if got := runsByStatus(t, ds, triggerID); got["parked"] != 3 {
		t.Fatalf("the retention kept %v, want the standing park and the two after the ok run", got)
	}
	clock.Advance(pastFailingWindow)
	process(t, ds)
	rec, err := ds.Get(ctx, alertKind, "trigger.failing/"+triggerID)
	if err != nil || rec.Properties["state"] != "open" {
		t.Fatalf("the failing alert is %v (%v), want open", rec, err)
	}
	if got := fmt.Sprint(rec.Properties["count"]); got != "2" {
		t.Fatalf("the alert counts %s parks, want the two after the ok run", got)
	}
}

// An ok delivery of a kind that does not bind `sync` retires no park of
// another record: each park is its own record's change (decision 0142).
func TestARecordDeliveryOfAPlainKindLeavesItsParksParked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := boomHealthDataset(t)
	triggerID, parked := parkOne(t, ds)
	fixBoom(t, ds)
	putWidget(t, ds, "w2")
	process(t, ds)
	if got := runsByStatus(t, ds, triggerID); got["ok"] != 1 {
		t.Fatalf("runs %v, want w2's ok run", got)
	}
	left, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(left) != 1 || left[0].ID != parked.ID {
		t.Fatalf("parked %+v (%v), want w1's park standing", left, err)
	}
	if st := statusOf(t, ds, triggerID); st.Parked != 1 {
		t.Fatalf("the trigger counts %d parked, want 1", st.Parked)
	}
}
