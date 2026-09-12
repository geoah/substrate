package engine

// The trigger machinery's internal regressions: a schedule fire is idempotent
// under stable fire ids and missed occurrences drain in order, and a host
// Call at the causal-depth cap refuses; the default trigger installer is
// create-only; a trigger's status resolves an agent callable as well as a
// function one; and a record trigger carrying an account syncs only that
// account. (The dispatcher's per-repository
// independence and its self-actor exclusion are proved end to end, through the
// public surface, in functions_db_test.go.)

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func maxSeqOf(t *testing.T, ds *dataset) int64 {
	t.Helper()
	var seq int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// withScheduleDrain lowers the per-pass occurrence bound for one test.
// Package-level, like withMaxPages: a caller MUST NOT call t.Parallel.
func withScheduleDrain(n int) func() {
	prev := scheduleDrainPerPass
	scheduleDrainPerPass = n
	return func() { scheduleDrainPerPass = prev }
}

func TestScheduleFiresMissedOccurrencesInOrder(t *testing.T) {
	// Missed occurrences each fire, oldest first, under stable ids, at most
	// scheduleDrainPerPass of them per pass; the fire state advances
	// compare-and-swap in each delivery's transaction to the occurrence
	// fired, and a pass with nothing due fires nothing.
	defer withScheduleDrain(2)()
	ctx := context.Background()
	ds := openCursorDataset(t)
	const pkg = "widgets.test.dev/widgets"
	// Anchored three and a half hours back: four occurrences lie between the
	// anchor and now once the fire state is rewound to it.
	startsAt := nowUTC().Add(-3*time.Hour - 30*time.Minute).Truncate(time.Minute)

	_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC",
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/hourly"),
		},
	})
	if err == nil {
		t.Fatal("a schedule trigger with an unknown callable landed")
	}

	// Install the fire-counting function, then the trigger.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(pkg, "hourly", map[string]any{
			"description": "mints one task per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"],
                         "properties": {"name": input["mode"] + " " + fire["at"]}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install function: %v", err)
	}
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/hourly"),
		},
	})
	if err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}

	// Nothing is due at creation: the fire state initialized at now.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 0 {
		t.Fatalf("a fresh schedule backfilled: %d tasks", n)
	}

	// Rewind the fire state to just before the anchor: four occurrences are
	// now overdue. Two fire per pass, oldest first, none coalesced away, and
	// the fire state is the last occurrence fired, not the clock.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, tr.ID, startsAt.Add(-time.Minute)); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	firedAt := func() time.Time {
		var at time.Time
		if err := ds.db.QueryRowContext(ctx, `SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1`, tr.ID).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at.UTC()
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 2 {
		t.Fatalf("the first pass fired %d occurrences, want the bound of 2", n)
	}
	if got := firedAt(); !got.Equal(startsAt.Add(time.Hour)) {
		t.Fatalf("fire state after the first pass %s, want %s", got, startsAt.Add(time.Hour))
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 4 {
		t.Fatalf("four missed occurrences fired %d times over two passes", n)
	}
	if got := firedAt(); !got.Equal(startsAt.Add(3 * time.Hour)) {
		t.Fatalf("fire state after the drain %s, want %s", got, startsAt.Add(3*time.Hour))
	}
	// The fire ids are the occurrence instants, stable rather than random
	// mints, and the run ledger holds them in the order they were due.
	rows, err := ds.db.QueryContext(ctx, `
		SELECT props->>'fireId' FROM records
		WHERE kind = $1 AND props->>'status' = 'ok' AND deleted_at IS NULL
		ORDER BY created_at, id`, typeTriggerRun)
	if err != nil {
		t.Fatalf("run rows: %v", err)
	}
	var fires []time.Time
	for rows.Next() {
		var fid string
		if err := rows.Scan(&fid); err != nil {
			t.Fatal(err)
		}
		at, err := time.Parse(time.RFC3339, fid)
		if err != nil {
			t.Fatalf("fire id %q is not an occurrence instant: %v", fid, err)
		}
		if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "fire-"+fid); err != nil {
			t.Fatalf("the body did not see the stable fire id %s: %v", fid, err)
		}
		fires = append(fires, at)
	}
	_ = rows.Close()
	if len(fires) != 4 {
		t.Fatalf("run rows: %d, want 4", len(fires))
	}
	for i, at := range fires {
		if want := startsAt.Add(time.Duration(i) * time.Hour); !at.Equal(want) {
			t.Fatalf("fire %d at %s, want %s: the occurrences did not fire oldest first", i, at, want)
		}
	}

	// Idempotence: the same instant fires nothing twice.
	before := maxSeqOf(t, ds)
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := maxSeqOf(t, ds); got != before {
		t.Fatalf("a second pass re-fired: seq %d → %d", before, got)
	}
}

func countLiveOf(t *testing.T, ds *dataset, typeIdent string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`, typeIdent).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCallAtDepthCapRefuses(t *testing.T) {
	t.Parallel()
	// The engine's half of the sub-call gates: a Call whose callee would sit
	// at the causal-depth cap refuses with the distinct error, and a Call to
	// a function already on the stack refuses as recursion.
	ds := openCursorDataset(t)
	ctx := context.Background()
	const ident = "widgets.test.dev/widgets/mirror"
	fn, err := ds.registry().ResolveFunction(ident)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	deep := &callBackend{
		inv: &invocation{ds: ds, stack: []string{"widgets.test.dev/widgets/root"}},
		fn:  fn, key: "k", causalDepth: causalDepthCap - 1,
	}
	if _, err := deep.Call(ctx, ident, nil); !errors.Is(err, errCausalDepth) {
		t.Fatalf("call at the cap returned %v, want errCausalDepth", err)
	}

	cyclic := &callBackend{
		inv: &invocation{ds: ds, stack: []string{ident}},
		fn:  fn, key: "k",
	}
	if _, err := cyclic.Call(ctx, ident, nil); err == nil || !strings.Contains(err.Error(), "recursion") {
		t.Fatalf("recursive call returned %v", err)
	}
}

// The default trigger installer is create-only, so TOMBSTONES count as existing and an
// owner's edit stands — a re-registration never resurrects a deliberately
// deleted default trigger and never rewires an edited one.
func TestDefaultTriggerCreateOnlyHonorsOwnerState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, widgetsManifest(true)); err != nil {
		t.Fatalf("register: %v", err)
	}
	triggerID := "on-" + widgetsMirror

	// The owner disables the trigger; re-registration must not re-enable it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTrigger, triggerID, substrate.PatchInput{
		Properties: map[string]any{"enabled": false},
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, widgetsManifest(true)); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Enabled {
		t.Fatal("re-registration re-enabled an owner-edited default trigger")
	}

	// The owner deletes it; re-registration must not resurrect the tombstone.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeTrigger, triggerID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, widgetsManifest(true)); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeTrigger, ID: triggerID})
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.DeletedAt == nil {
		t.Fatalf("re-registration resurrected a tombstoned default trigger: %+v", row)
	}
}

// `trigger status` reports the dispatcher's own verdict, and the dispatcher
// skips on runnable(): a trigger resolves into EITHER a function or an agent.
// Testing `Callable == nil` instead marked every agent-backed trigger
// "callable … does not resolve" while it was dispatching perfectly — on a clean
// install that is the shipped conformance bundle looking broken out of the box.
func TestTriggerStatusResolvesAgentCallables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)

	agentTrigger, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/agent", crewPackage+"/classifier"),
		},
	})
	if err != nil {
		t.Fatalf("put agent trigger: %v", err)
	}
	functionTrigger, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", crewPackage+"/annotate"),
		},
	})
	if err != nil {
		t.Fatalf("put function trigger: %v", err)
	}

	statuses, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	seen := map[string]substrate.TriggerStatus{}
	for _, st := range statuses {
		seen[st.ID] = st
	}
	for _, id := range []string{agentTrigger.ID, functionTrigger.ID} {
		st, ok := seen[id]
		if !ok {
			t.Fatalf("no status for trigger %s", id)
		}
		if st.Error != "" {
			t.Errorf("trigger %s (callable %s) reports %q; it resolves and dispatches", id, st.Callable, st.Error)
		}
	}

	// A callable that genuinely does not resolve still says so — the fix
	// widens the test, it does not silence it. The row is written past
	// admission (which refuses an unknown callable) the way an uninstall
	// leaves one behind.
	if _, err := ds.db.ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{callable}', to_jsonb($3::text))
		 WHERE kind = $1 AND id = $2`, typeTrigger, agentTrigger.ID,
		vocabulary.RecordPath(kindAgent, "crew.test.dev/crew/ghost")); err != nil {
		t.Fatalf("strand the callable: %v", err)
	}
	statuses, err = ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	for _, st := range statuses {
		if st.ID == agentTrigger.ID && st.Error == "" {
			t.Fatal("a stranded callable reports no error; the status is not honest")
		}
	}
}
