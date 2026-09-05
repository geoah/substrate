package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The paged-checkpoint invocation: a delivery body returns a
// PAGE — its effects plus an opaque resume cursor — and the host re-invokes it
// OFF THE CAUSAL CHAIN until drained, committing each page's effects with the
// cursor. These exercise the four load-bearing properties: a multi-page drain
// re-invokes at CONSTANT causal depth committing each page, the max-pages cap
// bounds a runaway with the cursor intact, a parked mid-chain drain resumes
// from the last committed page rather than from zero, and a body that never
// pages takes the ordinary single-shot path untouched.

// openPagedDataset provisions a repository with one widget type, one PAGED
// function draining `source`, and an record trigger on the widget. A unique
// package per test keeps the shared runner's registration (hence any body-side
// module state) isolated across cases.
func openPagedDataset(t *testing.T, pkg, source string) (*dataset, string) {
	t.Helper()
	ctx := context.Background()
	authority, _ := vocabulary.SplitPackageRef(pkg)
	widgetType := pkg + "/widget"
	triggerID := "on-page." + authority
	d := openInternalDataset(t)
	if err := enginetest.Install(ctx, d, substrate.ActorAPI, enginetest.Manifest{
		Name: "widgets", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "widget", "plural": "widgets"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(pkg, "page", map[string]any{
				"description": "a paged backfill body",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source":      source,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: triggerID,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/page"),
			},
		}},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return d, triggerID
}

// pagedBody drains n pages: page k (0-based, k = the resume cursor, 0 when
// absent) puts task p-k titled with the causal depth the host handed it, and
// returns `more: {cursor: k+1}` until the last page. The depth-in-title lets
// the drain's causal-depth exemption be asserted from the stored data.
func pagedBody(n int) string {
	return fmt.Sprintf(`
def main(input, host):
    page = input.get("resume") or 0
    depth = input.get("causalDepth", 0)
    effects = [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                "id": "p-%%d" %% page, "properties": {"name": str(depth)}}]
    if page < %d - 1:
        return {"effects": effects, "more": {"cursor": page + 1}}
    return {"effects": effects}
`, n)
}

// TestPagedDrainConstantDepth: a body that returns `more` for three pages is
// re-invoked three times, each page's effects committed, and every page runs
// at the DELIVERY's causal depth — the self-continuation never consumes the
// causal-depth budget (the whole point over a self-emit).
func TestPagedDrainConstantDepth(t *testing.T) {
	t.Parallel()
	ds, triggerID := openPagedDataset(t, "pageddepth.test.dev/pageddepth", pagedBody(3))
	ctx := context.Background()
	actor := substrate.Actor("connector:pageddepth")

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pageddepth.test.dev/pageddepth/widget", Properties: map[string]any{"name": "big"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("widget change: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	// All three pages committed their task.
	for page := range 3 {
		id := fmt.Sprintf("p-%d", page)
		if taskTitle(t, ds, id) != "0" {
			t.Fatalf("page %d task missing or wrong depth (title %q)", page, taskTitle(t, ds, id))
		}
		// Constant causal depth: every page effect is caused DIRECTLY by the
		// widget change (depth 1), never by the previous page — a self-emit
		// chain would read 1, 2, 3.
		if got := causedByOf(t, ds, id); got != wch.Seq {
			t.Fatalf("page %d effect caused_by %d, want the widget change %d (causal chain grew)", page, got, wch.Seq)
		}
		if got := depthOf(t, ds, id); got != 1 {
			t.Fatalf("page %d effect sits at causal depth %d, want a constant 1", page, got)
		}
	}
	// The trigger drained to head (past the change and the task/run writes the
	// drain itself appended), and the resume cursor is gone — the drain
	// finished.
	if got, head := cursorSeq(t, ds, triggerID), headSeq(t, ds); got != head {
		t.Fatalf("trigger cursor %d, want head %d", got, head)
	}
	if _, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); ok {
		t.Fatalf("resume cursor outlived a drained delivery")
	}
	// The run ledger recorded the page count.
	if got := okRunPages(t, ds, triggerID); got != 3 {
		t.Fatalf("run ledger pages %d, want 3", got)
	}
}

// TestPagedDrainMaxPagesCap: a body that ALWAYS returns `more` is bounded by
// the max-pages cap — the drain parks with the last committed cursor intact
// instead of spinning forever.
func TestPagedDrainMaxPagesCap(t *testing.T) {
	source := `
def main(input, host):
    page = input.get("resume") or 0
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "p-%d" % page, "properties": {"name": "x"}}],
            "more": {"cursor": page + 1}}
`
	ds, triggerID := openPagedDataset(t, "pagedcap.test.dev/pagedcap", source)
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedcap")

	defer withMaxPages(2)()

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedcap.test.dev/pagedcap/widget", Properties: map[string]any{"name": "endless"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("widget change: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Exactly the cap's worth of pages committed; the next never ran.
	for page := range 2 {
		if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", fmt.Sprintf("p-%d", page)); err != nil {
			t.Fatalf("page %d task missing under the cap: %v", page, err)
		}
	}
	if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "p-2"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("page 2 ran past the cap")
	}
	// Parked, with the cap as the reason, and the resume cursor left at 2 so a
	// retry continues the backfill.
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %v (%d)", err, len(failures))
	}
	if cur, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); !ok || cur != 2 {
		t.Fatalf("resume cursor after cap park: %v ok=%v, want 2", cur, ok)
	}
}

// TestPagedParkResumesFromCursor: a drain parked mid-chain (here by the cap)
// resumes from the last committed page on retry, NOT from zero. The already-
// committed pages are deleted before the retry; if the retry restarted from
// zero it would recreate them — it must not.
func TestPagedParkResumesFromCursor(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedresume.test.dev/pagedresume", pagedBody(5))
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedresume")

	restore := withMaxPages(2)

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedresume.test.dev/pagedresume/widget", Properties: map[string]any{"name": "mailbox"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("widget change: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	// Parked at cursor 2 after pages 0 and 1.
	if cur, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); !ok || cur != 2 {
		t.Fatalf("resume cursor before retry: %v ok=%v, want 2", cur, ok)
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %v (%d)", err, len(failures))
	}

	// Erase the already-committed pages: a resume must leave them erased, a
	// restart-from-zero would recreate them.
	for _, id := range []string{"p-0", "p-1"} {
		if _, err := ds.Delete(ctx, substrate.ActorAPI, "samples.substrate.reamde.dev/tasks/task", id); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}

	// Lift the cap and retry the parked delivery: it must resume from page 2.
	restore()
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}

	// Pages 2..4 ran; pages 0 and 1 stayed erased — the retry resumed, it did
	// not restart (a restart would re-put p-0/p-1, reviving the tombstones).
	for _, id := range []string{"p-2", "p-3", "p-4"} {
		if !liveExists(t, ds, id) {
			t.Fatalf("resumed page %s missing", id)
		}
	}
	for _, id := range []string{"p-0", "p-1"} {
		if liveExists(t, ds, id) {
			t.Fatalf("page %s was re-run — the retry restarted from zero", id)
		}
	}
	// Drain finished: cursor cleared, failure retired.
	if _, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); ok {
		t.Fatalf("resume cursor outlived the resumed drain")
	}
	failures, err = ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 0 {
		t.Fatalf("failures after retry: %v (%d)", err, len(failures))
	}
}

// TestNonPagedDeliveryUntouched: a body that returns no `more` delivers in the
// ordinary single transaction and never touches the paged bookkeeping — the
// continuation path is purely additive.
func TestNonPagedDeliveryUntouched(t *testing.T) {
	t.Parallel()
	source := `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": env["record"]["properties"]["name"]}}]}
`
	ds, triggerID := openPagedDataset(t, "pagedplain.test.dev/pagedplain", source)
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedplain")

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedplain.test.dev/pagedplain/widget", Properties: map[string]any{"name": "one"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("widget change: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if taskTitle(t, ds, "t-"+w.ID) != "one" {
		t.Fatalf("non-paged delivery did not apply its effect")
	}
	if got, head := cursorSeq(t, ds, triggerID), headSeq(t, ds); got != head {
		t.Fatalf("trigger cursor %d, want head %d", got, head)
	}
	// No paged bookkeeping was written for a non-paged delivery.
	if _, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); ok {
		t.Fatalf("a non-paged delivery wrote a resume cursor")
	}
	if n := pagedRowCount(t, ds); n != 0 {
		t.Fatalf("paged_cursors holds %d rows after a non-paged delivery, want 0", n)
	}
}

// TestPagedDeliveryHonorsIfVersion: ifVersion threads through the PAGED apply
// path exactly as through a direct call. Page 0 creates the row guarded by
// version 0 (an absent record is version 0) and commits; page 1's patch guarded
// by the now-stale version 0 conflicts and parks — page 0's write stands, page
// 1's does not. Proves the paged path is not a CAS blind spot.
func TestPagedDeliveryHonorsIfVersion(t *testing.T) {
	t.Parallel()
	source := `
def main(input, host):
    page = input.get("resume") or 0
    if page == 0:
        host.effects.put("samples.substrate.reamde.dev/tasks/task", "pcas",
                         properties={"name": "a"}, if_version=0)
        return {"more": host.page.more(1)}
    host.effects.patch("samples.substrate.reamde.dev/tasks/task", "pcas",
                       properties={"name": "b"}, if_version=0)
    return {}
`
	ds, triggerID := openPagedDataset(t, "pagedcas.test.dev/pagedcas", source)
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedcas")

	if _, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedcas.test.dev/pagedcas/widget", Properties: map[string]any{"name": "mailbox"}}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	// Page 0 committed its create; page 1's stale-version patch did not land.
	if got := taskTitle(t, ds, "pcas"); got != "a" {
		t.Fatalf("paged CAS: title = %q, want the page-0 value \"a\" (page 1 must have conflicted)", got)
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("paged CAS failures = %v (%d), want 1 conflict park", err, len(failures))
	}
}

// --- helpers ------------------------------------------------------------------

// withMaxPages lowers the drain cap for one test, returning a restore.
//
// This writes a package-level var, so a test that calls it MUST NOT call
// t.Parallel — it would be changing the cap out from under every other test in
// flight. These are the only tests in the package that still run serially.
func withMaxPages(n int) func() {
	prev := maxPagesPerDrain
	maxPagesPerDrain = n
	return func() { maxPagesPerDrain = prev }
}

// chainKey is the paged-cursor key a dispatch delivery uses: the delivery's
// idempotency key.
func chainKey(ds *dataset, triggerID string, seq int64) string {
	return fmt.Sprintf("%s/%s/%d", ds.Repository().Name, triggerID, seq)
}

// pagedCursor reads one chain's stored resume cursor as an integer.
func pagedCursor(t *testing.T, ds *dataset, chain string) (int, bool) {
	t.Helper()
	var raw []byte
	err := ds.db.QueryRowContext(context.Background(),
		`SELECT cursor FROM paged_cursors WHERE chain = $1`, chain).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("read paged cursor: %v", err)
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode paged cursor %q: %v", raw, err)
	}
	return int(v), true
}

// headSeq is the newest changelog seq.
func headSeq(t *testing.T, ds *dataset) int64 {
	t.Helper()
	var head int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		t.Fatalf("head seq: %v", err)
	}
	return head
}

// liveExists reports whether a record is present and NOT tombstoned — the
// signal a re-run would flip by re-putting a deleted id.
func liveExists(t *testing.T, ds *dataset, id string) bool {
	t.Helper()
	var live bool
	err := ds.db.QueryRowContext(context.Background(),
		`SELECT deleted_at IS NULL FROM records WHERE id = $1`, id).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("liveness of %s: %v", id, err)
	}
	return live
}

// pagedRowCount counts every stored resume cursor.
func pagedRowCount(t *testing.T, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `SELECT count(*) FROM paged_cursors`).Scan(&n); err != nil {
		t.Fatalf("count paged cursors: %v", err)
	}
	return n
}

// causedByOf reads the caused_by of a task's newest changelog row.
func causedByOf(t *testing.T, ds *dataset, recordID string) int64 {
	t.Helper()
	ch, err := ds.latestChangeOf(context.Background(), "samples.substrate.reamde.dev/tasks/task", recordID)
	if err != nil {
		t.Fatalf("change of %s: %v", recordID, err)
	}
	var causedBy int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(caused_by, 0) FROM changelog WHERE seq = $1`, ch.Seq).Scan(&causedBy); err != nil {
		t.Fatalf("caused_by of %s: %v", recordID, err)
	}
	return causedBy
}

// depthOf is the causal depth of a task's newest changelog row.
func depthOf(t *testing.T, ds *dataset, recordID string) int {
	t.Helper()
	ch, err := ds.latestChangeOf(context.Background(), "samples.substrate.reamde.dev/tasks/task", recordID)
	if err != nil {
		t.Fatalf("change of %s: %v", recordID, err)
	}
	d, err := ds.causalDepth(context.Background(), ch.Seq)
	if err != nil {
		t.Fatalf("depth of %s: %v", recordID, err)
	}
	return d
}

// okRunPages reads the `pages` recorded on a trigger's newest OK run.
func okRunPages(t *testing.T, ds *dataset, triggerID string) int {
	t.Helper()
	var raw []byte
	err := ds.db.QueryRowContext(context.Background(), `
		SELECT props->'pages' FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		  AND `+referencePathSQL("props", "trigger")+` = $2 AND props->>'status' = 'ok'
		ORDER BY created_at DESC, id DESC LIMIT 1`,
		typeRun, vocabulary.RecordPath(typeTrigger, triggerID)).Scan(&raw)
	if err != nil {
		t.Fatalf("read run pages: %v", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode run pages %q: %v", raw, err)
	}
	return int(v)
}
