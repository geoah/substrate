package engine

// The cursors a dispatch and a list hand out: a trigger cursor moves by
// compare-and-swap on the seq its pass read, and a list cursor is refused
// when it was minted against another history generation.

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
	t.Parallel()
	ds := openCursorDataset(t)
	ctx := context.Background()

	const pkg = "widgets.test.dev/widgets"
	actor := substrate.Actor(pkg)
	widgetType := pkg + "/widget"
	triggerID := "on-mirror." + pkg

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
	settle := ds.dispatchSettlement(tr, ch, cursor, nowUTC())
	settle.attempt = 1
	if _, err := ds.deliver(ctx, tr, ch, cursor, 0, pagedProgress{}, settle); !errors.Is(err, errCursorMoved) {
		t.Fatalf("stale delivery returned %v, want errCursorMoved", err)
	}
	if got := taskTitle(t, ds, "t-"+w.ID); got != "one" {
		t.Fatalf("stale effects landed: task title %q", got)
	}
	if got := cursorSeq(t, ds, triggerID); got != 0 {
		t.Fatalf("stale delivery clobbered the replay: cursor %d", got)
	}
	// The stale pass's batch-tail advance loses the same way.
	if err := ds.advanceCursor(ctx, tr, cursor, ch.Seq); !errors.Is(err, errCursorMoved) {
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
	const pkg = "widgets.test.dev/widgets"
	widgetType := pkg + "/widget"
	d := openInternalDataset(t)
	if err := enginetest.Install(ctx, d, substrate.ActorAPI, enginetest.Manifest{
		Name: "widgets", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(pkg, "mirror", map[string]any{
				"description": "mirrors widgets into tasks",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": env["record"]["properties"]["name"]}}]}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: "on-mirror." + pkg,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/mirror"),
			},
		}},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return d
}

// openInternalDataset opens a throwaway repository and returns the INTERNAL
// dataset, so tests can drive txn-level paths directly.
func openInternalDataset(t *testing.T, opts ...Option) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn,
		append([]Option{
			WithDataRoot(t.TempDir()),
			WithKindsDir(SeedKindsDir),
			WithCredentialKey(TestCredentialKey),
		}, opts...)...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	importVocabulary(t, ds, "tasks")
	return ds
}

// openCoreDataset is openInternalDataset without the sample import: core
// alone, which is what a repository looks like the moment it is created. A
// provider closure installs on it since record 49, because it names no kind
// outside its own package.
func openCoreDataset(t *testing.T, opts ...Option) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn,
		append([]Option{
			WithDataRoot(t.TempDir()),
			WithKindsDir(SeedKindsDir),
			WithCredentialKey(TestCredentialKey),
		}, opts...)...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	return ds
}

// importVocabulary imports the named shipped sample packages through the
// ordinary install path, each package's `requires:` ahead of it. Repository
// creation seeds the core package alone, so a test touching sample vocabulary
// imports it first, and it names what it reads: every package is a registry
// build and a projection pass. An empty list would mean all five, which is
// never what a caller wants here.
func importVocabulary(t *testing.T, ds substrate.Dataset, names ...string) {
	t.Helper()
	if len(names) == 0 {
		t.Fatal("importVocabulary: name the sample packages this test reads")
	}
	if err := enginetest.ImportVocabulary(context.Background(), ds, names...); err != nil {
		t.Fatalf("import the shipped vocabulary: %v", err)
	}
}

// declarePersonMapping declares the repository's OWN mapping from a provider
// mirror kind onto the shipped person. Since record 49 a provider ships no
// mapping (the package that owns `person` is the only one that may declare one
// onto it), so a test that wants the shell mint, the projection or the one-hop
// resolution declares it here, exactly as a repository would.
func declarePersonMapping(t *testing.T, ds substrate.Dataset, name string, data map[string]any) {
	t.Helper()
	if err := enginetest.DeclareMappings(context.Background(), ds,
		enginetest.PeopleMapping(name, data)); err != nil {
		t.Fatalf("declare the %s mapping: %v", name, err)
	}
}

func taskTitle(t *testing.T, d substrate.Dataset, id string) string {
	t.Helper()
	e, err := d.Get(context.Background(), "samples.substrate.reamde.dev/tasks/task", id)
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

// A list cursor carries the head of the history it was minted in, and that
// head is the watch handoff. A cursor minted before an import replaced the
// changelog would hand the client a head the new history never reached, so
// List refuses a cursor whose generation is not the dataset's (decision 0056).
func TestListRefusesACursorFromAnotherHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCursorDataset(t)
	const widgetType = "widgets.test.dev/widgets/widget"
	for _, name := range []string{"one", "two"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": name}}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{widgetType}}, First: 1}
	first, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if first.Cursor == "" || first.Generation != ds.generation || first.Head == 0 {
		t.Fatalf("first page = cursor %q, head %d, generation %q; want a continuation under %q", first.Cursor, first.Head, first.Generation, ds.generation)
	}
	tok, err := decodeKeyset(first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if tok.G != ds.generation {
		t.Fatalf("the cursor carries generation %q, want the dataset's %q", tok.G, ds.generation)
	}

	// The same cursor, as a client that saved it before a restore would
	// resend it: the head it carries belongs to a history this dataset does
	// not hold, so the page is refused rather than handed a stale head.
	q.After = encodeKeyset(tok.O, tok.K, tok.H, "another-history")
	if _, err := ds.List(ctx, q); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a cursor from another generation listed: %v", err)
	}
	// And the genuine continuation still walks.
	q.After = first.Cursor
	second, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Records) != 1 || second.Head != first.Head || second.Generation != ds.generation {
		t.Fatalf("second page = %d records, head %d, generation %q; want one record under the first page's head %d", len(second.Records), second.Head, second.Generation, first.Head)
	}
}
