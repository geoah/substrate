package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The `sync` trait's fixture: a kind binding it beside the test package's
// widgets, declared the way a provider's account kind declares it — the
// owner's two hands `writer: owner`, the rest `writer: connector`.
const jobType = fnPackage + "/job"

func syncProps() map[string]any {
	connector := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	owner := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	return map[string]any{
		"name":               map[string]any{"type": "string"},
		"syncState":          connector("string"),
		"syncMessage":        connector("string"),
		"lastSyncedAt":       connector("datetime"),
		"lastSyncStartedAt":  connector("datetime"),
		"lastSyncDurationMs": connector("int"),
		"syncRequestedAt":    owner("datetime"),
		"syncRequestedAck":   connector("datetime"),
		"syncPaused":         owner("bool"),
		"syncProgress":       connector("json"),
		"syncError":          connector("string"),
		"syncErrorAt":        connector("datetime"),
		"syncStreams":        connector("json"),
	}
}

// newSyncDataset installs the widgets connector plus the job kind and one
// record trigger on it bound to fn.
func newSyncDataset(t *testing.T, fn, source string) substrate.Dataset {
	t.Helper()
	return newSyncDatasetOn(t, fn, source, "create")
}

// newSyncDatasetOn is newSyncDataset with the trigger firing on ops.
func newSyncDatasetOn(t *testing.T, fn, source string, ops ...any) substrate.Dataset {
	t.Helper()
	return newSyncKindDataset(t,
		[]enginetest.Trigger{trigOn(fn, map[string]any{"kinds": []any{jobType}, "ops": ops})},
		pyFn(fn, map[string]any{}, []any{jobType}, source),
	)
}

// newSyncKindDataset installs the widgets connector plus the job kind, with
// the given triggers and functions of the job's own package.
func newSyncKindDataset(t *testing.T, triggers []enginetest.Trigger, fns ...map[string]any) substrate.Dataset {
	t.Helper()
	_, ds := newDataset(t)
	m := fnConnector(triggers, fns...)
	m.Manifests = append(m.Manifests, vocabulary.KindManifest(fnPackage, map[string]any{"singular": "job"}, map[string]any{
		"traits":          []any{"substrate.reamde.dev/core/sync"},
		"displayTemplate": "{name}",
		"properties":      syncProps(),
	}))
	if err := enginetest.Install(context.Background(), ds, owner, m); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return ds
}

// A body that reports its own half of the trait: the message, progress and
// the finish instant. The engine's half — running, the start, the duration
// and ok — it never writes.
const syncOKSource = `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "patch", "kind": env["change"]["kind"], "id": env["change"]["id"],
                         "properties": {"syncMessage": "drained 3", "lastSyncedAt": "2026-09-19T10:00:00Z",
                                        "syncProgress": {"phase": "drain", "done": 3, "total": 3, "pending": 0}}}]}
`

// TestSyncDeliveryStampsRunningThenOK: a record-sourced delivery of a
// `sync`-trait record writes `running` before the body and `ok` with the
// duration beside the body's effects, under the callable's actor, and the
// status read lists the record joined with its trigger.
func TestSyncDeliveryStampsRunningThenOK(t *testing.T) {
	t.Parallel()
	ds := newSyncDataset(t, "drain", syncOKSource)
	ctx := context.Background()

	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox"}})
	process(t, ds)

	got := mustGet(t, ds, jobType, job.ID)
	if got.Properties["syncState"] != substrate.SyncStateOK {
		t.Fatalf("syncState = %v, want ok (properties %v)", got.Properties["syncState"], got.Properties)
	}
	if got.Properties["lastSyncStartedAt"] == nil || got.Properties["lastSyncDurationMs"] == nil {
		t.Fatalf("the engine's stamps are missing: %v", got.Properties)
	}
	if got.Properties["syncMessage"] != "drained 3" {
		t.Fatalf("the body's own word did not land: %v", got.Properties)
	}
	// The stamps are the callable's writes: the trigger never delivers them
	// back to itself, so the run ledger holds exactly one ok run.
	if runs := runRowsOf(t, ds, trigID("drain"), "ok"); len(runs) != 1 {
		t.Fatalf("ok runs = %d, want 1", len(runs))
	}
	if st := statusOf(t, ds, trigID("drain")); st.Lag != 0 || st.Parked != 0 {
		t.Fatalf("trigger status after the sync: %+v", st)
	}

	statuses, err := ds.SyncStatuses(ctx)
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("sync statuses = %+v, want the one job", statuses)
	}
	s := statuses[0]
	if s.Kind != jobType || s.ID != job.ID || s.State != substrate.SyncStateOK || s.Message != "drained 3" || s.Title != "inbox" {
		t.Fatalf("sync status = %+v", s)
	}
	if s.LastSyncedAt == nil || s.LastSyncStartedAt == nil || s.Progress == nil || s.Progress.Done != 3 || s.Progress.Total != 3 {
		t.Fatalf("sync status fields = %+v", s)
	}
	if len(s.Triggers) != 1 || s.Triggers[0].ID != trigID("drain") || s.Triggers[0].Kind != substrate.TriggerKindRecord {
		t.Fatalf("sync status triggers = %+v, want the job trigger", s.Triggers)
	}
}

// TestSyncParkStampsErroring: a body that fails out parks the delivery and,
// in the same commit, moves the record to `erroring` with the cause.
func TestSyncParkStampsErroring(t *testing.T) {
	t.Parallel()
	ds := newSyncDataset(t, "fail", `
def main(input, host):
    raise RuntimeError("upstream returned HTTP 403")
`)
	ctx := context.Background()

	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox"}})
	process(t, ds)

	got := mustGet(t, ds, jobType, job.ID)
	if got.Properties["syncState"] != substrate.SyncStateErroring {
		t.Fatalf("syncState = %v, want erroring", got.Properties["syncState"])
	}
	if msg, _ := got.Properties["syncError"].(string); !strings.Contains(msg, "HTTP 403") {
		t.Fatalf("syncError = %q, want the body's cause", msg)
	}
	if got.Properties["syncErrorAt"] == nil || got.Properties["lastSyncStartedAt"] == nil {
		t.Fatalf("the park's stamps are missing: %v", got.Properties)
	}
	parked, err := ds.TriggerFailures(ctx, trigID("fail"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 1 {
		t.Fatalf("parked = %+v, want one", parked)
	}
	statuses, err := ds.SyncStatuses(ctx)
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if len(statuses) != 1 || statuses[0].State != substrate.SyncStateErroring || statuses[0].Parked != 1 || statuses[0].Triggers[0].Parked != 1 {
		t.Fatalf("sync statuses = %+v", statuses)
	}
}

// TestSyncPausedRecordIsSkipped: the owner's pause stops the sync without
// the body knowing — the delivery is a settled skip naming the pause, and
// nothing on the record moves.
func TestSyncPausedRecordIsSkipped(t *testing.T) {
	t.Parallel()
	ds := newSyncDataset(t, "drain", syncOKSource)

	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox", "syncPaused": true}})
	process(t, ds)

	got := mustGet(t, ds, jobType, job.ID)
	if _, ok := got.Properties["syncState"]; ok {
		t.Fatalf("a paused record must not be stamped: %v", got.Properties)
	}
	runs := runRowsOf(t, ds, trigID("drain"), "skipped")
	if len(runs) != 1 {
		t.Fatalf("skipped runs = %d, want 1", len(runs))
	}
	if reason, _ := runs[0].Properties["reason"].(string); !strings.Contains(reason, "paused") {
		t.Fatalf("skip reason = %q, want the pause named", reason)
	}
	if st := statusOf(t, ds, trigID("drain")); st.Lag != 0 {
		t.Fatalf("a skip is a settled attempt, not lag: %+v", st)
	}
	statuses, err := ds.SyncStatuses(context.Background())
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if len(statuses) != 1 || !statuses[0].Paused || statuses[0].State != substrate.SyncStateNever {
		t.Fatalf("sync statuses = %+v, want paused and never", statuses)
	}
}

// A second package's function on the job kind, the way a user bundle's
// identity resolver watches provider accounts: it records each delivery as a
// note of its own kind, and never writes the job.
const (
	watcherPackage = "watcher.test.dev/watcher"
	watcherNote    = watcherPackage + "/note"
	watcherFn      = watcherPackage + "/owneridentity"
	watcherTrigger = "on-job-owneridentity"
)

func installJobWatcher(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	m := enginetest.Manifest{
		Name:      "watcher",
		Authority: watcherPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(watcherPackage, 0),
			vocabulary.ActorManifest(watcherPackage, vocabulary.PackageActor(watcherPackage)),
			vocabulary.KindManifest(watcherPackage, map[string]any{"singular": "note"}, map[string]any{
				"properties": map[string]any{"job": map[string]any{"type": "string", "fts": false}},
			}),
			vocabulary.FunctionManifest(watcherPackage, "owneridentity", map[string]any{
				"description": "notes every job it is delivered",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{watcherNote}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "` + watcherNote + `", "id": env["change"]["id"],
                         "properties": {"job": env["change"]["id"]}}]}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: watcherTrigger,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{jobType}, "ops": []any{"create", "update"}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", watcherFn),
			},
		}},
	}
	if err := enginetest.Install(context.Background(), ds, owner, m); err != nil {
		t.Fatalf("install the watcher: %v", err)
	}
}

// TestSyncForeignFunctionIsNotStamped: a function of another package
// delivered a `sync`-trait record runs as any delivery does, paused record
// included, and the dispatcher writes none of the sync stamps: its runs are
// not the sync's, and the record's version does not move.
func TestSyncForeignFunctionIsNotStamped(t *testing.T) {
	t.Parallel()
	ds := newSyncKindDataset(t, nil)
	installJobWatcher(t, ds)

	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox"}})
	paused := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "outbox", "syncPaused": true}})
	process(t, ds)

	for _, rec := range []*substrate.Record{job, paused} {
		got := mustGet(t, ds, jobType, rec.ID)
		for _, name := range []string{"syncState", "lastSyncStartedAt", "lastSyncDurationMs", "syncError", "syncErrorAt"} {
			if v, ok := got.Properties[name]; ok {
				t.Fatalf("job %s: %s = %v, want unset: another package's function is not the sync", rec.ID, name, v)
			}
		}
		if got.Version != rec.Version {
			t.Fatalf("job %s: version %d, want %d: the watcher's delivery moved the record", rec.ID, got.Version, rec.Version)
		}
		// The body ran, paused record included: the pause is the owner's hand
		// on the sync, not on every function watching the record.
		if note := mustGet(t, ds, watcherNote, rec.ID); note.Properties["job"] != rec.ID {
			t.Fatalf("watcher note for %s = %v", rec.ID, note.Properties)
		}
	}
	if runs := runRowsOf(t, ds, watcherTrigger, "skipped"); len(runs) != 0 {
		t.Fatalf("skipped watcher runs = %d, want 0", len(runs))
	}
	if runs := runRowsOf(t, ds, watcherTrigger, "ok"); len(runs) != 2 {
		t.Fatalf("ok watcher runs = %d, want 2", len(runs))
	}
}

// TestSyncOwnFunctionStampsBesideForeign: with a second package's function
// on the same kind, the kind's own sync still stamps running then ok, and
// every stamp is the sync's: the watcher's actor never writes the job.
func TestSyncOwnFunctionStampsBesideForeign(t *testing.T) {
	t.Parallel()
	ds := newSyncDataset(t, "drain", syncOKSource)
	installJobWatcher(t, ds)

	job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "inbox"}})
	process(t, ds)

	got := mustGet(t, ds, jobType, job.ID)
	if got.Properties["syncState"] != substrate.SyncStateOK {
		t.Fatalf("syncState = %v, want ok (properties %v)", got.Properties["syncState"], got.Properties)
	}
	if got.Properties["lastSyncStartedAt"] == nil || got.Properties["lastSyncDurationMs"] == nil {
		t.Fatalf("the sync's stamps are missing: %v", got.Properties)
	}
	if runs := runRowsOf(t, ds, trigID("drain"), "ok"); len(runs) != 1 {
		t.Fatalf("ok drain runs = %d, want 1", len(runs))
	}
	if note := mustGet(t, ds, watcherNote, job.ID); note.Properties["job"] != job.ID {
		t.Fatalf("watcher note = %v", note.Properties)
	}
	for _, ch := range actorChanges(t, ds, watcherFn) {
		if ch.Kind == jobType {
			t.Fatalf("the watcher wrote the job: %+v", ch)
		}
	}
}

// A body whose outcome the record's name picks: `broken` fails out and parks,
// `throttled` and `self-ok` write that state themselves, anything else leaves
// the state to the engine.
const syncByNameSource = `
def main(input, host):
    env = input["envelope"]
    name = env["record"]["properties"].get("name")
    if name == "broken":
        raise RuntimeError("upstream returned HTTP 403")
    props = {"syncMessage": "drained"}
    if name == "throttled":
        props["syncState"] = "throttled"
    if name == "self-ok":
        props["syncState"] = "ok"
    return {"effects": [{"action": "patch", "kind": env["change"]["kind"], "id": env["change"]["id"],
                         "properties": props}]}
`

// TestSyncGoodRunClearsTheParkedError: a run that settles `ok` after a park
// clears `syncError` and `syncErrorAt`, whether the engine wrote the `ok` or
// the body did. A body that wrote `throttled` keeps its word, and the error
// pair with it.
func TestSyncGoodRunClearsTheParkedError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		state     string
		wantError bool
	}{
		{name: "fixed", state: substrate.SyncStateOK},
		{name: "self-ok", state: substrate.SyncStateOK},
		{name: "throttled", state: substrate.SyncStateThrottled, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds := newSyncDatasetOn(t, "drain", syncByNameSource, "create", "update")

			job := mustPut(t, ds, owner, substrate.PutInput{Kind: jobType, Properties: map[string]any{"name": "broken"}})
			process(t, ds)
			parked := mustGet(t, ds, jobType, job.ID)
			if parked.Properties["syncState"] != substrate.SyncStateErroring || parked.Properties["syncError"] == nil || parked.Properties["syncErrorAt"] == nil {
				t.Fatalf("the park did not stamp the error pair: %v", parked.Properties)
			}

			mustPatch(t, ds, owner, jobType, job.ID, substrate.PatchInput{Properties: map[string]any{"name": tc.name}})
			process(t, ds)

			got := mustGet(t, ds, jobType, job.ID)
			if got.Properties["syncState"] != tc.state {
				t.Fatalf("syncState = %v, want %s (properties %v)", got.Properties["syncState"], tc.state, got.Properties)
			}
			_, hasError := got.Properties["syncError"]
			_, hasErrorAt := got.Properties["syncErrorAt"]
			if hasError != tc.wantError || hasErrorAt != tc.wantError {
				t.Fatalf("syncError present %v, syncErrorAt present %v, want %v: %v",
					hasError, hasErrorAt, tc.wantError, got.Properties)
			}
		})
	}
}
