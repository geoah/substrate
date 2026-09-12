package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
			vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"},
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
		if _, err := ds.Delete(ctx, substrate.ActorAPI, "samples.substrate.reamde.dev/tasks/task", id, substrate.DeleteInput{}); err != nil {
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
	return fmt.Sprintf("%s/%s/%d", ds.Repository().ID, triggerID, seq)
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
		typeTriggerRun, vocabulary.RecordPath(typeTrigger, triggerID)).Scan(&raw)
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

// The paged-checkpoint drain's resume row
//: the cursor-ownership CAS fence, the cumulative
// drain budget that spans retries, resume-before-every-delivery, the
// continuation secret scrub, and the paged-row lifecycle owner + sweep.

// withMaxDrainEffects lowers the cumulative effect budget for one test.
//
// Package-level, like withMaxPages: a caller MUST NOT call t.Parallel.
func withMaxDrainEffects(n int64) func() {
	prev := maxDrainEffects
	maxDrainEffects = n
	return func() { maxDrainEffects = prev }
}

// withSweepGrace shortens the orphan-sweep grace for one test.
func withSweepGrace(d time.Duration) func() {
	prev := pagedSweepGrace
	pagedSweepGrace = d
	return func() { pagedSweepGrace = prev }
}

// pagedVersion reads a chain's CAS version.
func pagedVersion(t *testing.T, ds *dataset, chain string) (int64, bool) {
	t.Helper()
	var v int64
	err := ds.db.QueryRowContext(context.Background(),
		`SELECT version FROM paged_cursors WHERE chain = $1`, chain).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("read paged version: %v", err)
	}
	return v, true
}

// TestPagedCursorOwnershipCAS: the resume row is a compare-and-swap fence.
// Two dispatchers draining the same chain cannot both commit — a fresh claim of
// a chain another already owns, an advance from a stale version, and a delete
// under a stale version all miss and return errCursorMoved, so the loser's page
// transaction rolls back whole. Each motion is a ledger effect, so the
// transaction that makes one appends the delivery entry it rides.
func TestPagedCursorOwnershipCAS(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	chain := "owner.test.dev/owner/on-cas/1"
	owner := pagedOwner{triggerID: "on-cas", kind: pagedKindRecord, identity: "1"}

	motion := func(fn func(tx *txn) error) error {
		return ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			if err := fn(tx); err != nil {
				return err
			}
			return tx.settleDelivery(owner.triggerID)
		})
	}
	claim := func() error {
		return motion(func(tx *txn) error {
			return tx.claimPagedCursor(chain, owner, 1, 1, 1, 1, nowUTC())
		})
	}
	// First dispatcher claims the absent chain; the second, also starting fresh,
	// finds the row already there and loses.
	if err := claim(); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := claim(); !errors.Is(err, errCursorMoved) {
		t.Fatalf("second claim of an owned chain returned %v, want errCursorMoved", err)
	}
	if v, _ := pagedVersion(t, ds, chain); v != 1 {
		t.Fatalf("version after the losing claim is %d, want 1 (the loser must not have advanced it)", v)
	}

	advance := func(from int64) error {
		return motion(func(tx *txn) error {
			return tx.advancePagedCursor(chain, from, 2, 2, 2, 2)
		})
	}
	// Two dispatchers both read version 1 and both try to advance from it: one
	// wins (version → 2), the other misses.
	if err := advance(1); err != nil {
		t.Fatalf("first advance: %v", err)
	}
	if err := advance(1); !errors.Is(err, errCursorMoved) {
		t.Fatalf("advance from a stale version returned %v, want errCursorMoved", err)
	}
	if v, _ := pagedVersion(t, ds, chain); v != 2 {
		t.Fatalf("version after the losing advance is %d, want 2", v)
	}

	// The final delete requires the current version: a stale delete misses and
	// cannot clear a chain the winner still owns.
	if err := motion(func(tx *txn) error {
		return tx.clearPagedCursorCAS(chain, 1)
	}); !errors.Is(err, errCursorMoved) {
		t.Fatalf("stale clear returned %v, want errCursorMoved", err)
	}
	if _, ok := pagedCursor(t, ds, chain); !ok {
		t.Fatalf("a stale delete cleared the chain — the fence failed")
	}
	if err := motion(func(tx *txn) error {
		return tx.clearPagedCursorCAS(chain, 2)
	}); err != nil {
		t.Fatalf("version-matched clear: %v", err)
	}
	if _, ok := pagedCursor(t, ds, chain); ok {
		t.Fatalf("the version-matched delete left the row behind")
	}
}

// TestPagedDrainBudgetSpansRetries: the cumulative effect budget bounds the
// WHOLE chain including automatic retries. A runaway pager parks deterministically
// at the budget, and a retry — which reloads the persisted counter — parks again
// at once WITHOUT resetting the budget and replaying more pages.
func TestPagedDrainBudgetSpansRetries(t *testing.T) {
	source := `
def main(input, host):
    page = input.get("resume") or 0
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "p-%d" % page, "properties": {"name": "x"}}],
            "more": {"cursor": page + 1}}
`
	ds, triggerID := openPagedDataset(t, "pagedbudget.test.dev/pagedbudget", source)
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedbudget")

	defer withMaxDrainEffects(3)()

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedbudget.test.dev/pagedbudget/widget", Properties: map[string]any{"name": "runaway"}})
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

	chain := chainKey(ds, triggerID, wch.Seq)
	// The budget allowed exactly three effects; the fourth page never committed.
	for page := range 3 {
		if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", fmt.Sprintf("p-%d", page)); err != nil {
			t.Fatalf("page %d missing under the budget: %v", page, err)
		}
	}
	if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "p-3"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("page 3 committed past the effect budget")
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %v (%d)", err, len(failures))
	}
	if cur, ok := pagedCursor(t, ds, chain); !ok || cur != 3 {
		t.Fatalf("resume cursor after the budget park: %v ok=%v, want 3", cur, ok)
	}

	// Retry: the drain reloads the persisted effect counter (3) and parks again
	// on the very first page it would add — the budget did not reset. If it had
	// reset, page 3 (and more) would commit.
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID); err == nil {
		t.Fatalf("retry of a budget-exhausted chain succeeded, want a re-park")
	}
	if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "p-3"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the retry reset the budget and committed page 3")
	}
	if cur, ok := pagedCursor(t, ds, chain); !ok || cur != 3 {
		t.Fatalf("resume cursor after the retry re-park: %v ok=%v, want a still-3", cur, ok)
	}
}

// TestPagedRedispatchResumesFromCursor: an ordinary redelivery of an
// existing chain (a post-crash dispatch that never wrote the park row, so the
// trigger cursor never advanced) loads the committed resume cursor and continues
// from the last committed page instead of replaying page zero.
func TestPagedRedispatchResumesFromCursor(t *testing.T) {
	t.Parallel()
	ds, triggerID := openPagedDataset(t, "pagedredispatch.test.dev/pagedredispatch", pagedBody(5))
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedredispatch")

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedredispatch.test.dev/pagedredispatch/widget", Properties: map[string]any{"name": "crashed"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("widget change: %v", err)
	}

	// Simulate a crash after pages 0 and 1 committed but BEFORE the delivery
	// parked: the paged row stands at cursor 2, but the trigger cursor never
	// advanced, so the change is still pending. Pages 0 and 1 are deliberately
	// NOT created — a replay-from-zero would recreate them.
	chain := chainKey(ds, triggerID, wch.Seq)
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO paged_cursors (chain, cursor, pages, version, effects, bytes, started_at, trigger_id, kind, identity, updated_at)
		VALUES ($1, '2'::jsonb, 2, 2, 2, 2, $2, $3, 'record', $4, $2)`,
		chain, nowUTC(), triggerID, fmt.Sprintf("%d", wch.Seq)); err != nil {
		t.Fatalf("seed crashed chain: %v", err)
	}

	// A fresh dispatcher pass redelivers the pending change: it must resume.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	for _, id := range []string{"p-2", "p-3", "p-4"} {
		if !liveExists(t, ds, id) {
			t.Fatalf("resumed page %s missing — the redispatch did not continue the chain", id)
		}
	}
	for _, id := range []string{"p-0", "p-1"} {
		if liveExists(t, ds, id) {
			t.Fatalf("page %s was created — the redispatch replayed from zero instead of resuming", id)
		}
	}
	if _, ok := pagedCursor(t, ds, chain); ok {
		t.Fatalf("resume cursor outlived the resumed drain")
	}
	if got, head := cursorSeq(t, ds, triggerID), headSeq(t, ds); got != head {
		t.Fatalf("trigger cursor %d, want head %d after the drain finished", got, head)
	}
}

const (
	pagedSecretPackage = "pagedsecret.bundles.substrate.reamde.dev/pagedsecret"
	pagedSecretValue   = "sk-paged-continuation-secret-77"
)

// installPagedSecretBundle stands up a bundle whose config carries a secret and
// a PAGED function that copies that secret into its `more.cursor`, plus a widget
// type and a trigger.
func installPagedSecretBundle(t *testing.T, ds *dataset) string {
	t.Helper()
	ctx := context.Background()
	triggerID := "on-pagedsecret"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "pagedsecret", Authority: pagedSecretPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pagedSecretPackage, 0),
			vocabulary.ActorManifest(pagedSecretPackage, vocabulary.PackageActor(pagedSecretPackage)),
			vocabulary.BundleManifest(pagedSecretPackage, map[string]any{
				"description": "the paged secret bundle",
				"inputs": map[string]any{
					"connector": map[string]any{"kind": pagedSecretPackage + "/sconfig", "inject": "functions"},
				},
				"installs": []any{
					pagedSecretPackage + "/sconfig", pagedSecretPackage + "/widget",
					pagedSecretPackage + "/pnote", pagedSecretPackage + "/leakpage",
				},
			}),
			vocabulary.KindManifest(pagedSecretPackage,
				map[string]any{"singular": "sconfig"},
				map[string]any{"properties": map[string]any{
					"apiToken": map[string]any{"type": "secret"},
				}}),
			vocabulary.KindManifest(pagedSecretPackage, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(pagedSecretPackage, map[string]any{"singular": "pnote"},
				map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(pagedSecretPackage, "leakpage", map[string]any{
				"description": "leaks the config secret into the paged continuation cursor",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{pagedSecretPackage + "/pnote"}},
				"source": `
def main(input, host):
    tok = input["config"]["inputs"]["connector"]["properties"]["apiToken"]
    return {"effects": [{"action": "put", "kind": "pagedsecret.bundles.substrate.reamde.dev/pagedsecret/pnote",
                         "id": "pn-1", "properties": {"text": "hi"}}],
            "more": {"cursor": tok}}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: triggerID,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{pagedSecretPackage + "/widget"}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pagedSecretPackage+"/leakpage"),
			},
		}},
	}); err != nil {
		t.Fatalf("register paged secret bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pagedSecretPackage + "/sconfig", ID: "ps-cfg",
		Properties: map[string]any{"apiToken": pagedSecretValue},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	return triggerID
}

// TestPagedContinuationSecretRejected: a body that copies an injected
// secret into its continuation cursor is rejected like a secret-bearing effect —
// the invocation fails, nothing commits (no page effect, no resume row), and the
// raw secret never reaches the durable failure ledger.
func TestPagedContinuationSecretRejected(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	triggerID := installPagedSecretBundle(t, ds)

	w, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pagedSecretPackage + "/widget", Properties: map[string]any{"name": "trip"},
	})
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

	// The delivery parked; nothing the leaking body returned committed.
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %v (%d)", err, len(failures))
	}
	if strings.Contains(failures[0].LastError, pagedSecretValue) {
		t.Fatalf("the raw secret reached the failure ledger: %s", failures[0].LastError)
	}
	if !strings.Contains(failures[0].LastError, "secret") {
		t.Fatalf("the park reason does not name the secret rejection: %s", failures[0].LastError)
	}
	if _, err := ds.Get(ctx, pagedSecretPackage+"/pnote", "pn-1"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the page effect committed even though the continuation was rejected")
	}
	if _, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); ok {
		t.Fatalf("a rejected continuation wrote a resume cursor")
	}
}

// TestPagedCursorLifecycleDropAndSweep: a paged row is dropped when its
// trigger is deleted, and the sweep collects a row whose trigger no longer lives
// (and a stale, unreferenced row).
func TestPagedCursorLifecycleDropAndSweep(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedlifecycle.test.dev/pagedlifecycle", pagedBody(5))
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedlifecycle")

	restore := withMaxPages(2)

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedlifecycle.test.dev/pagedlifecycle/widget", Properties: map[string]any{"name": "leaky"}})
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
	restore()

	chain := chainKey(ds, triggerID, wch.Seq)
	if _, ok := pagedCursor(t, ds, chain); !ok {
		t.Fatalf("the parked drain left no resume row to test the lifecycle against")
	}

	// Deleting the trigger drops its paged rows in the same transaction.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeTrigger, triggerID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete trigger: %v", err)
	}
	if _, ok := pagedCursor(t, ds, chain); ok {
		t.Fatalf("the paged row outlived its deleted trigger")
	}

	// A row whose trigger never existed is orphaned; the sweep collects it.
	orphan := "pagedlifecycle.test.dev/pagedlifecycle/on-ghost/9"
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO paged_cursors (chain, cursor, pages, version, effects, bytes, started_at, trigger_id, kind, identity, updated_at)
		VALUES ($1, '0'::jsonb, 1, 1, 1, 1, $2, 'on-ghost', 'record', '9', $2)`,
		orphan, nowUTC()); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	defer withSweepGrace(0)()
	if err := ds.sweepPagedCursors(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, ok := pagedCursor(t, ds, orphan); ok {
		t.Fatalf("the sweep left an orphaned paged row (no live trigger)")
	}
}
