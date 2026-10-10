package engine_test

// Alerts (decision record 0148): a trigger whose deliveries park raises one
// core/alert record, the repeat throttle keeps a burst of parks to one write,
// the unpark paths resolve it once nothing stands parked, a rebuild
// reproduces it, and a notifier watching alerts never receives the alert
// about its own failures.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const alertKind = "substrate.reamde.dev/core/alert"

// parkedAlertID is the alert id of a trigger's parked deliveries: the key
// itself, because the key sits inside the record id alphabet.
func parkedAlertID(triggerID string) string { return "trigger.parked/" + triggerID }

func alertOf(t *testing.T, ds substrate.Dataset, triggerID string) *substrate.Record {
	t.Helper()
	return mustGet(t, ds, alertKind, parkedAlertID(triggerID))
}

func alertWrites(t *testing.T, ds substrate.Dataset) int {
	t.Helper()
	out, err := ds.Changes(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{alertKind}}, 1000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	return len(out)
}

func aboutPaths(t *testing.T, rec *substrate.Record) []string {
	t.Helper()
	raw, _ := rec.Properties["about"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		m, _ := v.(map[string]any)
		path, _ := m[vocabulary.ReferenceValueKey].(string)
		out = append(out, path)
	}
	return out
}

func alertCountOf(rec *substrate.Record) float64 {
	switch n := rec.Properties["count"].(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return -1
}

func putWidget(t *testing.T, ds substrate.Dataset, id string) {
	t.Helper()
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: id, Properties: map[string]any{"name": id, "mode": "go"},
	})
}

// A park raises one open error alert naming the trigger, the function and
// the record, written under the function's actor. A second park within the
// repeat interval writes nothing: same version, same count, one alert entry.
func TestAParkRaisesOneAlertAndARepeatWithinTheIntervalWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	a := alertOf(t, ds, triggerID)
	if a.Properties["state"] != "open" || a.Properties["level"] != "error" {
		t.Fatalf("the alert is %v/%v, want open/error", a.Properties["state"], a.Properties["level"])
	}
	if a.Properties["key"] != "trigger.parked/"+triggerID {
		t.Fatalf("the alert's key is %v", a.Properties["key"])
	}
	if got := alertCountOf(a); got != 1 {
		t.Fatalf("the alert counts %v parked deliveries, want 1", got)
	}
	if summary, _ := a.Properties["summary"].(string); !strings.Contains(summary, triggerID) || !strings.Contains(summary, fnPackage+"/boom") {
		t.Fatalf("the summary %q names neither the trigger nor the function", summary)
	}
	if a.Title != a.Properties["summary"] {
		t.Fatalf("the title %q is not the summary %q", a.Title, a.Properties["summary"])
	}
	if detail, _ := a.Properties["detail"].(string); !strings.Contains(detail, "the endpoint is dead") || detail != parked.LastError {
		t.Fatalf("the detail %q is not the park's error %q", detail, parked.LastError)
	}
	want := []string{
		vocabulary.RecordPath("substrate.reamde.dev/core/trigger", triggerID),
		vocabulary.RecordPath("substrate.reamde.dev/core/function", fnPackage+"/boom"),
		vocabulary.RecordPath(widgetType, "w1"),
	}
	if got := aboutPaths(t, a); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the alert is about %v, want %v", got, want)
	}
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Kinds: []string{alertKind}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	boomActor := substrate.FunctionActor(vocabulary.SplitKindRef(fnPackage + "/boom"))
	if len(changes) != 1 || changes[0].Actor != boomActor {
		t.Fatalf("the alert was written by %+v, want one entry under %s", changes, boomActor)
	}
	// The read the console's panel makes: open alerts about one record.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{alertKind},
			Referencing: &substrate.Referencing{Ref: want[1]},
		},
		First: 10,
	})
	if err != nil {
		t.Fatalf("referencing read: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != parkedAlertID(triggerID) {
		t.Fatalf("referencing %s found %d alerts, want the one parked alert", want[1], len(page.Records))
	}

	putWidget(t, ds, "w2")
	process(t, ds)
	if st := statusOf(t, ds, triggerID); st.Parked != 2 {
		t.Fatalf("the second delivery did not park: parked=%d", st.Parked)
	}
	again := alertOf(t, ds, triggerID)
	if again.Version != a.Version || alertCountOf(again) != 1 {
		t.Fatalf("a repeat within the interval rewrote the alert: version %d -> %d, count %v",
			a.Version, again.Version, alertCountOf(again))
	}
	if n := alertWrites(t, ds); n != 1 {
		t.Fatalf("%d alert entries after two parks inside the interval, want 1", n)
	}
}

// A retry that delivers resolves the alert in the transaction that retires
// the row, under the same actor.
func TestARetryThatDeliversResolvesTheParkedAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	fixBoom(t, ds)
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	requireNoneParked(t, ds, triggerID)
	a := alertOf(t, ds, triggerID)
	if a.Properties["state"] != "resolved" || alertCountOf(a) != 0 {
		t.Fatalf("after the retry delivered the alert is %v with count %v, want resolved with 0",
			a.Properties["state"], alertCountOf(a))
	}
}

// Forget recounts the open alert, resolves it once the last parked row is
// gone, and a park after that reopens the resolved row at once, inside the
// repeat interval, with firstSeenAt restarted.
func TestForgetResolvesTheParkedAlertAndAParkReopensIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, _ := parkOne(t, ds)
	putWidget(t, ds, "w2")
	putWidget(t, ds, "w3")
	process(t, ds)
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 3 {
		t.Fatalf("parked %d deliveries (%v), want 3", len(failures), err)
	}
	first := alertOf(t, ds, triggerID)
	if alertCountOf(first) != 1 {
		t.Fatalf("the throttled alert counts %v, want the first park's 1", alertCountOf(first))
	}

	if err := ds.ForgetTriggerFailure(ctx, triggerID, failures[0].ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if a := alertOf(t, ds, triggerID); a.Properties["state"] != "open" || alertCountOf(a) != 2 {
		t.Fatalf("after one forget the alert is %v with count %v, want open with 2", a.Properties["state"], alertCountOf(a))
	}
	for _, f := range failures[1:] {
		if err := ds.ForgetTriggerFailure(ctx, triggerID, f.ID); err != nil {
			t.Fatalf("forget: %v", err)
		}
	}
	resolved := alertOf(t, ds, triggerID)
	if resolved.Properties["state"] != "resolved" || alertCountOf(resolved) != 0 {
		t.Fatalf("after the last forget the alert is %v with count %v, want resolved with 0",
			resolved.Properties["state"], alertCountOf(resolved))
	}

	putWidget(t, ds, "w4")
	process(t, ds)
	reopened := alertOf(t, ds, triggerID)
	if reopened.Properties["state"] != "open" || alertCountOf(reopened) != 1 {
		t.Fatalf("a park after the resolve left the alert %v with count %v, want open with 1",
			reopened.Properties["state"], alertCountOf(reopened))
	}
	if reopened.Properties["firstSeenAt"] == first.Properties["firstSeenAt"] {
		t.Fatalf("the reopened alert kept firstSeenAt %v", first.Properties["firstSeenAt"])
	}
	if got := aboutPaths(t, reopened); got[len(got)-1] != vocabulary.RecordPath(widgetType, "w4") {
		t.Fatalf("the reopened alert is about %v, want the parked record w4 last", got)
	}
}

// The alert is ordinary record writes through the changelog, so a rebuild
// replays it: the fold after the rebuild is the fold before, alert included.
func TestARebuildReproducesTheParkedAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	if err := enginetest.Install(ctx, ds, owner, fnConnector(
		[]enginetest.Trigger{trigOn("boom", map[string]any{
			"kinds": []any{widgetType},
			"when":  `record != null && record.properties.mode == "go"`,
		})},
		pyFn("boom", map[string]any{}, []any{taskType}, raisingSource))); err != nil {
		t.Fatalf("install: %v", err)
	}
	triggerID, parked := parkOne(t, ds)
	putWidget(t, ds, "w2")
	process(t, ds)
	if err := ds.ForgetTriggerFailure(ctx, triggerID, parked.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	a := alertOf(t, ds, triggerID)

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatal("the rebuilt fold differs from the live one")
	}
	b := alertOf(t, ds, triggerID)
	if b.Version != a.Version || b.Properties["state"] != a.Properties["state"] || alertCountOf(b) != alertCountOf(a) {
		t.Fatalf("the rebuilt alert is version %d %v count %v, want version %d %v count %v",
			b.Version, b.Properties["state"], alertCountOf(b), a.Version, a.Properties["state"], alertCountOf(a))
	}
}

// LOOP CONTAINMENT. A notifier on a record trigger over core/alert whose code
// always fails parks on the alert about another function, and its own park
// raises an alert about the notifier. That alert is written under the
// notifier's actor, so its trigger never receives it: the notifier parks once
// and runs once, however many passes follow.
func TestAFailingAlertNotifierParksOnceAndNeverReceivesItsOwnAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("boom", map[string]any{
				"kinds": []any{widgetType},
				"when":  `record != null && record.properties.mode == "go"`,
			}),
			trigOn("notify", map[string]any{"kinds": []any{alertKind}}),
		},
		pyFn("boom", map[string]any{}, []any{taskType}, raisingSource),
		pyFn("notify", map[string]any{}, []any{taskType}, raisingSource))

	putWidget(t, ds, "w1")
	for range 4 {
		process(t, ds)
	}

	boom := alertOf(t, ds, trigID("boom"))
	if boom.Properties["state"] != "open" {
		t.Fatalf("the boom alert is %v", boom.Properties["state"])
	}
	failures, err := ds.TriggerFailures(ctx, trigID("notify"))
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("the notifier parked %d deliveries, want 1 (the boom alert alone): %+v", len(failures), failures)
	}
	notify := alertOf(t, ds, trigID("notify"))
	if alertCountOf(notify) != 1 {
		t.Fatalf("the notifier's own alert counts %v, want 1", alertCountOf(notify))
	}
	runs, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{triggerRunType},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath("substrate.reamde.dev/core/trigger", trigID("notify"))},
		},
		First: 50,
	})
	if err != nil {
		t.Fatalf("list the notifier's runs: %v", err)
	}
	if len(runs.Records) != 1 {
		t.Fatalf("the notifier ran %d times, want once", len(runs.Records))
	}
}

// requireOneParkAbout asserts the trigger parked the change of the record id,
// its cursor stands past it, and its alert names each record once: the
// trigger and the callable, one of which is also the parked record.
func requireOneParkAbout(t *testing.T, ds substrate.Dataset, triggerID, recordID string, want []string) {
	t.Helper()
	failures, err := ds.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	parkedIt := false
	for _, f := range failures {
		parkedIt = parkedIt || f.RecordID == recordID
	}
	if !parkedIt {
		t.Fatalf("no parked delivery of %s: %+v", recordID, failures)
	}
	if st := statusOf(t, ds, triggerID); st.Lag != 0 {
		t.Fatalf("the cursor stands behind the park: lag %d", st.Lag)
	}
	if got := aboutPaths(t, alertOf(t, ds, triggerID)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the alert is about %v, want %v", got, want)
	}
}

// A trigger over core/trigger receives its own trigger record. The parked
// record is then the trigger the alert already names, and a reference listed
// twice is refused, which would roll the park back and leave the trigger on
// that change at every pass.
func TestAParkOfATriggersOwnRecordRaisesItsAlert(t *testing.T) {
	t.Parallel()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("watch", map[string]any{"kinds": []any{"substrate.reamde.dev/core/trigger"}})},
		pyFn("watch", map[string]any{}, []any{taskType}, raisingSource))
	process(t, ds)
	mustPatch(t, ds, owner, "substrate.reamde.dev/core/trigger", trigID("watch"), substrate.PatchInput{
		Labels: map[string]any{"owner/pinned": true},
	})
	process(t, ds)
	requireOneParkAbout(t, ds, trigID("watch"), trigID("watch"), []string{
		vocabulary.RecordPath("substrate.reamde.dev/core/trigger", trigID("watch")),
		vocabulary.RecordPath("substrate.reamde.dev/core/function", fnPackage+"/watch"),
	})
}

// A trigger over core/function receives its own function record when the
// function is applied again: the parked record is the callable.
func TestAParkOfACallablesOwnRecordRaisesItsAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("watchfn", map[string]any{"kinds": []any{"substrate.reamde.dev/core/function"}})},
		pyFn("watchfn", map[string]any{}, []any{taskType}, raisingSource))
	process(t, ds)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		pyFn("watchfn", map[string]any{}, []any{taskType},
			"def main(input, host):\n    raise Exception(\"still dead\")\n"),
	}); err != nil {
		t.Fatalf("apply the function again: %v", err)
	}
	process(t, ds)
	requireOneParkAbout(t, ds, trigID("watchfn"), fnPackage+"/watchfn", []string{
		vocabulary.RecordPath("substrate.reamde.dev/core/trigger", trigID("watchfn")),
		vocabulary.RecordPath("substrate.reamde.dev/core/function", fnPackage+"/watchfn"),
	})
}
