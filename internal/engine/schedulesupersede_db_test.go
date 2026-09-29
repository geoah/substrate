package engine

// A schedule fire that settles retires the parked fires of its own trigger
// at or before its occurrence (issue #746): a provider's scheduled sync that
// recovered no longer reads as 110 runs waiting to be tried again. A later
// occurrence's park stays, another trigger's parks stay, and a record
// trigger's park stays whatever its later deliveries do, because each one is
// its own record's change.

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	supersedePkg  = "widgets.test.dev/widgets"
	supersedeJob  = supersedePkg + "/job"
	supersedeSync = "job-scheduled"
	supersedeRec  = "job-on-request"
	supersedeTool = "other-scheduled"
	// supersedeRaise is a body that fails every run.
	supersedeRaise = "def main(input, host):\n    raise RuntimeError(\"upstream returned HTTP 500\")\n"
	// supersedeOK is a body that delivers every run, writing nothing.
	supersedeOK = "def main(input, host):\n    return {\"effects\": []}\n"
)

// supersedeFunction is the job sync function's declaration over source.
func supersedeFunction(name, source string) map[string]any {
	return vocabulary.FunctionManifest(supersedePkg, name, map[string]any{
		"description": "syncs jobs",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{"writes": []any{supersedeJob}},
		"source":      source,
	})
}

// supersedeDataset installs a `sync`-trait job kind, a record trigger on it
// firing on ops, a schedule trigger firing the same function, and a schedule
// trigger firing another function, every body failing. The schedule starts
// at startsAt, hourly.
func supersedeDataset(t *testing.T, startsAt time.Time, ops ...any) *dataset {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	connector := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	owner := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	callable := func(name string) string {
		return vocabulary.RecordPath("substrate.reamde.dev/core/function", supersedePkg+"/"+name)
	}
	schedule := map[string]any{"schedule": map[string]any{
		"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
	}}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "widgets", Authority: supersedePkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(supersedePkg, 0),
			vocabulary.ActorManifest(supersedePkg, vocabulary.PackageActor(supersedePkg)),
			vocabulary.KindManifest(supersedePkg, map[string]any{"singular": "job"}, map[string]any{
				"traits":          []any{vocabulary.TraitSyncCore},
				"displayTemplate": "{name}",
				"properties": map[string]any{
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
				},
			}),
			supersedeFunction("jobsync", supersedeRaise),
			supersedeFunction("other", supersedeRaise),
		},
		Triggers: []enginetest.Trigger{
			{ID: supersedeRec, Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{supersedeJob}, "ops": ops}},
				"callable": callable("jobsync"),
			}},
			{ID: supersedeSync, Properties: map[string]any{
				"enabled": true, "source": schedule, "callable": callable("jobsync"),
			}},
			{ID: supersedeTool, Properties: map[string]any{
				"enabled": true, "source": schedule, "callable": callable("other"),
			}},
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return ds
}

// setJobSync replaces the job sync function's body.
func setJobSync(t *testing.T, ds *dataset, source string) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI,
		[]map[string]any{supersedeFunction("jobsync", source)}); err != nil {
		t.Fatalf("apply jobsync: %v", err)
	}
}

// rewindSchedule sets a schedule trigger's fire state, so the occurrences
// after at are due on the next pass.
func rewindSchedule(t *testing.T, ds *dataset, triggerID string, at time.Time) {
	t.Helper()
	if _, err := ds.db.ExecContext(context.Background(),
		`UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, triggerID, at); err != nil {
		t.Fatalf("rewind %s: %v", triggerID, err)
	}
}

// processOnce runs one dispatcher pass.
func processOnce(t *testing.T, ds *dataset) {
	t.Helper()
	if _, err := ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("process: %v", err)
	}
}

// parkedFires lists a trigger's parked rows by fire id, oldest first.
func parkedFires(t *testing.T, ds *dataset, triggerID string) []string {
	t.Helper()
	failures, err := ds.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatalf("parked deliveries of %s: %v", triggerID, err)
	}
	out := []string{}
	for _, f := range failures {
		out = append(out, f.FireID)
	}
	return out
}

// jobStatus is the one job's sync status and the named trigger's status.
func jobStatus(t *testing.T, ds *dataset, triggerID string) (substrate.SyncStatus, substrate.TriggerStatus) {
	t.Helper()
	ctx := context.Background()
	syncs, err := ds.SyncStatuses(ctx)
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if len(syncs) != 1 {
		t.Fatalf("sync statuses = %+v, want the one job", syncs)
	}
	triggers, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	for _, st := range triggers {
		if st.ID == triggerID {
			return syncs[0], st
		}
	}
	t.Fatalf("no status for %s", triggerID)
	return substrate.SyncStatus{}, substrate.TriggerStatus{}
}

func TestAScheduleFireThatSettlesRetiresItsOlderParkedFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Three occurrences lie in the past: s, s+1h and s+2h.
	s := nowUTC().Add(-150 * time.Minute).Truncate(time.Minute)
	s1, s2 := s.Add(time.Hour), s.Add(2*time.Hour)
	ds := supersedeDataset(t, s, "update")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: supersedeJob, ID: "inbox", Properties: map[string]any{"name": "inbox"},
	}); err != nil {
		t.Fatalf("put job: %v", err)
	}
	processOnce(t, ds)

	// Every body fails: all three occurrences of both schedules park.
	rewindSchedule(t, ds, supersedeSync, s.Add(-time.Minute))
	rewindSchedule(t, ds, supersedeTool, s.Add(-time.Minute))
	processOnce(t, ds)
	want := []string{fireID(s), fireID(s1), fireID(s2)}
	if got := parkedFires(t, ds, supersedeSync); !slices.Equal(got, want) {
		t.Fatalf("%s parked %v, want %v", supersedeSync, got, want)
	}
	if got := parkedFires(t, ds, supersedeTool); !slices.Equal(got, want) {
		t.Fatalf("%s parked %v, want %v", supersedeTool, got, want)
	}
	if sync, tr := jobStatus(t, ds, supersedeSync); sync.Parked != 3 || tr.Parked != 3 {
		t.Fatalf("parked: sync status %d, trigger status %d, want 3 each", sync.Parked, tr.Parked)
	}

	// The body delivers every occurrence but the last. s+1h settles and
	// retires the parks of s and s+1h; the park of s+2h is a later
	// occurrence and stays, and s+2h then fails again beside it.
	setJobSync(t, ds, "def main(input, host):\n"+
		"    if input[\"envelope\"][\"fire\"][\"id\"] == \""+fireID(s2)+"\":\n"+
		"        raise RuntimeError(\"upstream returned HTTP 500\")\n"+
		"    return {\"effects\": []}\n")
	rewindSchedule(t, ds, supersedeSync, s)
	processOnce(t, ds)
	if got := parkedFires(t, ds, supersedeSync); !slices.Equal(got, []string{fireID(s2), fireID(s2)}) {
		t.Fatalf("%s parked %v after s+1h settled, want s+2h's first park and its second", supersedeSync, got)
	}

	// s+2h settles: nothing of the trigger is left parked, on any read.
	setJobSync(t, ds, supersedeOK)
	rewindSchedule(t, ds, supersedeSync, s1)
	processOnce(t, ds)
	if got := parkedFires(t, ds, supersedeSync); len(got) != 0 {
		t.Fatalf("%s parked %v after its latest occurrence settled, want none", supersedeSync, got)
	}
	sync, tr := jobStatus(t, ds, supersedeSync)
	if tr.Parked != 0 || tr.LastParkedError != "" || tr.LastParkedAt != nil {
		t.Fatalf("trigger status = %+v, want nothing parked", tr)
	}
	if sync.Parked != 0 || sync.LastParkedError != "" || sync.LastParkedAt != nil {
		t.Fatalf("sync status = %+v, want nothing parked", sync)
	}
	if runs := runRowsFor(t, ds, supersedeSync, runStatusOK); runs != 2 {
		t.Fatalf("%s wrote %d ok runs, want s+1h's and s+2h's", supersedeSync, runs)
	}
	// Another trigger's parks are its own.
	if got := parkedFires(t, ds, supersedeTool); !slices.Equal(got, want) {
		t.Fatalf("%s parked %v, want its three parks untouched", supersedeTool, got)
	}

	// The retirement rode the ledger: a rebuild from the segment files
	// reproduces the fold, and the parks stay retired.
	before, err := ds.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, err := ds.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the rebuilt fold is not the live one\n%s", firstDifferenceOf(before, after))
	}
	if got := parkedFires(t, ds, supersedeSync); len(got) != 0 {
		t.Fatalf("%s parked %v after the rebuild, want none", supersedeSync, got)
	}
}

func TestARecordDeliveryThatSettlesLeavesItsTriggersParksParked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	ds := supersedeDataset(t, s, "create", "update")

	// The job's creation parks on the record trigger.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: supersedeJob, ID: "inbox", Properties: map[string]any{"name": "inbox"},
	}); err != nil {
		t.Fatalf("put job: %v", err)
	}
	processOnce(t, ds)
	parked, err := ds.TriggerFailures(ctx, supersedeRec)
	if err != nil || len(parked) != 1 || parked[0].RecordID != "inbox" || parked[0].FireID != "" {
		t.Fatalf("%s parked %+v (%v), want the creation's one park", supersedeRec, parked, err)
	}

	// The body is fixed. The record's next change delivers, and a schedule
	// fire of the same function settles in the same pass; neither is the
	// creation's delivery, so its park stays.
	setJobSync(t, ds, supersedeOK)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, supersedeJob, "inbox", substrate.PatchInput{
		Properties: map[string]any{"name": "inbox, renamed"},
	}); err != nil {
		t.Fatalf("patch job: %v", err)
	}
	rewindSchedule(t, ds, supersedeSync, s.Add(-time.Minute))
	processOnce(t, ds)
	if runs := runRowsFor(t, ds, supersedeRec, runStatusOK); runs != 1 {
		t.Fatalf("%s wrote %d ok runs, want the patch's", supersedeRec, runs)
	}
	if runs := runRowsFor(t, ds, supersedeSync, runStatusOK); runs != 1 {
		t.Fatalf("%s wrote %d ok runs, want the occurrence's", supersedeSync, runs)
	}
	left, err := ds.TriggerFailures(ctx, supersedeRec)
	if err != nil || len(left) != 1 || left[0].ID != parked[0].ID {
		t.Fatalf("%s parked %+v (%v) after later deliveries settled, want the creation's park %d", supersedeRec, left, err, parked[0].ID)
	}
	sync, tr := jobStatus(t, ds, supersedeRec)
	if tr.Parked != 1 || sync.Parked != 1 {
		t.Fatalf("parked: sync status %d, trigger status %d, want the creation's 1", sync.Parked, tr.Parked)
	}
}

// An agent's schedule fire settles in the thread's own transaction
// (settlement.complete), and it retires the older parks the same way.
func TestAnAgentScheduleFireThatSettlesRetiresItsOlderParkedFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	s := nowUTC().Add(-90 * time.Minute).Truncate(time.Minute)
	s1 := s.Add(time.Hour)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": s.Format(time.RFC3339),
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/agent", crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	// Two occurrences, three attempts each, then the fire that settles.
	var turns []fakeTurn
	for range 7 {
		turns = append(turns, fakeTurn{content: "kept"})
	}
	fake.script("keep", turns...)

	// Every completion fails: both occurrences park, their claims rewritten
	// with the error.
	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error { return errors.New("the completion failed") }
	ds.mu.Unlock()
	rewindSchedule(t, ds, tr.ID, s.Add(-time.Minute))
	processOnce(t, ds)
	if got := parkedFires(t, ds, tr.ID); !slices.Equal(got, []string{fireID(s), fireID(s1)}) {
		t.Fatalf("parked %v, want both occurrences", got)
	}

	ds.mu.Lock()
	ds.deliveryFault = nil
	ds.mu.Unlock()
	rewindSchedule(t, ds, tr.ID, s)
	processOnce(t, ds)
	if got := parkedFires(t, ds, tr.ID); len(got) != 0 {
		t.Fatalf("parked %v after s+1h settled, want none", got)
	}
	if runs := runRowsFor(t, ds, tr.ID, runStatusOK); runs != 1 {
		t.Fatalf("wrote %d ok runs, want s+1h's", runs)
	}
}

// runRowsFor counts a trigger's run rows of one status.
func runRowsFor(t *testing.T, ds *dataset, triggerID, status string) int {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds: []string{typeTriggerRun},
			Properties: map[string]substrate.Cond{
				"trigger": {Eq: vocabulary.RecordPath("substrate.reamde.dev/core/trigger", triggerID)},
				"status":  {Eq: status},
			},
		},
		First: 500,
	})
	if err != nil {
		t.Fatalf("runs of %s: %v", triggerID, err)
	}
	return len(page.Records)
}
