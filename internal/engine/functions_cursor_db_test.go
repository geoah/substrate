package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The cursor motion is compare-and-swap on the seq the pass read: a replay
// that lands while a pass is in flight must win — the stale advance rolls
// back, effects included, instead of silently swallowing the reset
// (retrospective runs are cursor resets). Internal on purpose: the race
// window sits between a pass reading its cursor and its delivery committing,
// and only the dispatcher's own pieces can hold it open deterministically.
func TestTriggerCursorSwapLosesToReplay(t *testing.T) {
	ds := openCursorDataset(t)
	ctx := context.Background()

	const authority = "widgets.test.dev"
	actor := substrate.Actor(authority)
	widgetType := authority + "/widget"
	const triggerID = "on-mirror.widgets.test.dev"

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "one"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		t.Fatalf("resolve trigger: %v", err)
	}
	// The pass under test reads its cursor here…
	cursor, err := ds.ensureCursor(ctx, triggerID)
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	if _, err := ds.Patch(ctx, actor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "two"}}); err != nil {
		t.Fatalf("patch widget: %v", err)
	}
	ch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("latest change: %v", err)
	}
	// …the replay lands inside the window…
	if err := ds.ReplayTrigger(ctx, triggerID, 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	// …and the stale delivery loses whole: errCursorMoved, the effect rolled
	// back, the reset cursor intact.
	if _, err := ds.deliver(ctx, tr, ch, cursor, 0, pagedProgress{}); !errors.Is(err, errCursorMoved) {
		t.Fatalf("stale delivery returned %v, want errCursorMoved", err)
	}
	if got := taskTitle(t, ds, "t-"+w.ID); got != "one" {
		t.Fatalf("stale effects landed: task title %q", got)
	}
	if got := cursorSeq(t, ds, triggerID); got != 0 {
		t.Fatalf("stale delivery clobbered the replay: cursor %d", got)
	}
	// The stale pass's batch-tail advance loses the same way.
	if err := ds.advanceCursor(ctx, triggerID, cursor, ch.Seq); !errors.Is(err, errCursorMoved) {
		t.Fatalf("stale tail advance returned %v, want errCursorMoved", err)
	}
	if got := cursorSeq(t, ds, triggerID); got != 0 {
		t.Fatalf("stale tail advance clobbered the replay: cursor %d", got)
	}

	// The next ordinary pass owns the reset cursor and reconciles.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process after replay: %v", err)
	}
	if got := taskTitle(t, ds, "t-"+w.ID); got != "two" {
		t.Fatalf("replayed task title %q", got)
	}
}

// openCursorDataset provisions a repository with one widget type, one mirror
// function and its default trigger.
func openCursorDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	const authority = "widgets.test.dev"
	widgetType := authority + "/widget"
	d := openInternalDataset(t)
	if err := enginetest.Install(ctx, d, substrate.ActorAPI, enginetest.Manifest{
		Name: "widgets", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, ""),
			vocabulary.ActorManifest(authority, vocabulary.AuthorityActor(authority)),
			vocabulary.KindManifest(authority, map[string]any{"singular": "widget", "plural": "widgets"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(authority, "mirror", map[string]any{
				"description":  "mirrors widgets into tasks",
				"runtime":      vocabulary.RuntimePython,
				"capabilities": map[string]any{"emit": []any{"tasks.substrate.reamde.dev/task"}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"title": env["record"]["properties"]["name"]}}]}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: "on-mirror." + authority,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
				"callable": map[string]any{"kind": "core.substrate.reamde.dev/function", "id": authority + "/mirror"},
			},
		}},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return d
}

// openInternalDataset opens a throwaway repository and returns the INTERNAL
// dataset, so tests can drive txn-level paths directly.
func openInternalDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svc, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/core.substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	importVocabulary(t, ds)
	return ds
}

// importVocabulary imports the shipped vocabulary bundles (all of them when
// none are named) through the ordinary install path. Repository creation seeds
// CORE ALONE — people/tasks/messaging/calendar/media are vocabulary bundles a
// user imports — so any test touching that vocabulary imports it first.
func importVocabulary(t *testing.T, ds substrate.Dataset, names ...string) {
	t.Helper()
	if err := enginetest.ImportVocabulary(context.Background(), ds, names...); err != nil {
		t.Fatalf("import the shipped vocabulary: %v", err)
	}
}

func taskTitle(t *testing.T, d substrate.Dataset, id string) string {
	t.Helper()
	e, err := d.Get(context.Background(), "tasks.substrate.reamde.dev/task", id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return e.Title
}

func cursorSeq(t *testing.T, ds *dataset, triggerID string) int64 {
	t.Helper()
	var seq int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, triggerID).Scan(&seq); err != nil {
		t.Fatalf("cursor seq: %v", err)
	}
	return seq
}
