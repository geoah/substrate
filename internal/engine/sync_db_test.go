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
	_, ds := newDataset(t)
	m := fnConnector(
		[]enginetest.Trigger{trigOn(fn, map[string]any{"kinds": []any{jobType}, "ops": []any{"create"}})},
		pyFn(fn, map[string]any{}, []any{jobType}, source),
	)
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
