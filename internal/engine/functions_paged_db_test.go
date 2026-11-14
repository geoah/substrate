package engine

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
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
	if err := enginetest.Install(ctx, d, substrate.ActorPerson, enginetest.Manifest{
		Name: "widgets", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
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
		if _, err := ds.Delete(ctx, substrate.ActorPerson, "samples.substrate.reamde.dev/tasks/task", id, substrate.DeleteInput{}); err != nil {
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
			return tx.advancePagedCursor(chain, from, 2, 2, 2, 2, nowUTC())
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
	if err := enginetest.Install(ctx, ds, substrate.ActorPerson, enginetest.Manifest{
		Name: "pagedsecret", Authority: pagedSecretPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pagedSecretPackage, 0),
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
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: pagedSecretPackage + "/sconfig", ID: fid("ps-cfg"),
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

	w, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
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
	if _, err := ds.Delete(ctx, substrate.ActorPerson, typeTrigger, triggerID, substrate.DeleteInput{}); err != nil {
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

// brokenPagedBody commits page 0 and raises on every later page: a drain that
// parks MID-CHAIN on its first attempt, the shape issue #579's twenty rows
// were in (attempts 1, a resume cursor behind them).
const brokenPagedBody = `
def main(input, host):
    page = input.get("resume") or 0
    if page > 0:
        raise Exception("the endpoint is dead")
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "p-0", "properties": {"name": "0"}}],
            "more": {"cursor": 1}}
`

// parkMidChain opens a paged repository on brokenPagedBody, writes one widget
// and runs a pass, answering the widget's change seq and the parked row.
func parkMidChain(t *testing.T, pkg string) (*dataset, string, int64, substrate.TriggerFailure) {
	t.Helper()
	ctx := context.Background()
	ds, triggerID := openPagedDataset(t, pkg, brokenPagedBody)
	w, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pkg + "/widget", Properties: map[string]any{"name": "go"}})
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
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 || failures[0].Attempts != 1 {
		t.Fatalf("want one row parked at attempt 1, got %+v (%v)", failures, err)
	}
	if cur, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); !ok || cur != 1 {
		t.Fatalf("resume cursor after the mid-chain park: %v ok=%v, want 1", cur, ok)
	}
	return ds, triggerID, wch.Seq, failures[0]
}

// fixPagedBody applies pagedBody(3) over brokenPagedBody, same identity.
func fixPagedBody(t *testing.T, ds *dataset, pkg string) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.FunctionManifest(pkg, "page", map[string]any{
			"description": "a paged backfill body",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source":      pagedBody(3),
		}),
	}); err != nil {
		t.Fatalf("apply the fixed body: %v", err)
	}
}

// A drain parked mid-chain by its BODY, the body fixed and the trigger
// toggled off and on, then retried: the retry resumes from the committed
// page under the fixed body, finishes the chain, drops its resume row and
// retires the parked row (issue #579).
func TestAMidChainParkRetriedUnderAFixedBodyRetires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pkg := "pagedfix.test.dev/pagedfix"
	ds, triggerID, seq, parked := parkMidChain(t, pkg)

	fixPagedBody(t, ds, pkg)
	for _, enabled := range []bool{false, true} {
		if _, err := ds.Patch(ctx, substrate.ActorPerson, "substrate.reamde.dev/core/trigger", triggerID, substrate.PatchInput{
			Properties: map[string]any{"enabled": enabled},
		}); err != nil {
			t.Fatalf("set enabled=%v: %v", enabled, err)
		}
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
	}

	// Erase the committed page: a resume leaves it erased, a restart from
	// page zero would write it again.
	if _, err := ds.Delete(ctx, substrate.ActorPerson, "samples.substrate.reamde.dev/tasks/task", "p-0", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete p-0: %v", err)
	}
	ran, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ran != 1 {
		t.Fatalf("the retry applied %d deliveries, want 1", ran)
	}
	if liveExists(t, ds, "p-0") {
		t.Fatal("the retry restarted the drain from page zero")
	}
	for _, id := range []string{"p-1", "p-2"} {
		if !liveExists(t, ds, id) {
			t.Fatalf("the resumed drain did not write %s", id)
		}
	}
	if _, ok := pagedCursor(t, ds, chainKey(ds, triggerID, seq)); ok {
		t.Fatal("the resume row outlived the finished drain")
	}
	if failures, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(failures) != 0 {
		t.Fatalf("the retry finished the drain and left %+v parked (%v)", failures, err)
	}
}

// A drain parked mid-chain whose guard no longer matches: the retry settles
// the delivery as a skip, runs nothing and retires the parked row. The resume
// row is left for the sweep: its key is the seq, and a replay can park a
// second failure on the same seq, so this retry cannot tell whose it is.
func TestAMidChainParkRetriedAsASkipRetiresTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pkg := "pagedskip.test.dev/pagedskip"
	ds, triggerID, seq, parked := parkMidChain(t, pkg)

	if _, err := ds.Patch(ctx, substrate.ActorPerson, "substrate.reamde.dev/core/trigger", triggerID, substrate.PatchInput{
		Properties: map[string]any{"source": map[string]any{"record": map[string]any{
			"kinds": []any{pkg + "/widget"},
			"when":  `record != null && record.properties.name == "go"`,
		}}},
	}); err != nil {
		t.Fatalf("guard the trigger: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, pkg+"/widget", parked.RecordID, substrate.PatchInput{
		Properties: map[string]any{"name": "done"},
	}); err != nil {
		t.Fatalf("satisfy the widget: %v", err)
	}

	ran, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ran != 0 {
		t.Fatalf("the retry applied %d deliveries; the guard says no", ran)
	}
	if liveExists(t, ds, "p-1") {
		t.Fatal("a skipped retry ran the body")
	}
	if failures, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(failures) != 0 {
		t.Fatalf("the skip left %+v parked (%v)", failures, err)
	}
	if cur, ok := pagedCursor(t, ds, chainKey(ds, triggerID, seq)); !ok || cur != 1 {
		t.Fatalf("the skip touched the resume row: %v ok=%v, want it still at 1", cur, ok)
	}
}

// Forgetting a delivery parked mid-chain retires it without running the
// body, the operator's way out for a chain that should not resume.
func TestForgettingAMidChainParkRetiresTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pkg := "pagedforget.test.dev/pagedforget"
	ds, triggerID, _, parked := parkMidChain(t, pkg)
	// A body that WOULD write the next page, so running it is observable.
	fixPagedBody(t, ds, pkg)

	if err := ds.ForgetTriggerFailure(ctx, triggerID, parked.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if liveExists(t, ds, "p-1") {
		t.Fatal("forget ran the body")
	}
	if failures, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(failures) != 0 {
		t.Fatalf("forget left %+v parked (%v)", failures, err)
	}
}

// A sweep that pages through records in id order with a list token in its
// saved continuation, as the Gmail sweep and the WHOOP scan keep theirs, is
// interrupted under the order before migration 0021, the database's locale:
// a, b, Zed, zz. It leaves a continuation after `b`, with `Zed` and `zz`
// owed. 0021 puts the ids in byte order, Zed, a, b, zz, so that position,
// resumed, would pass over `Zed` for good. The token carries the order it was
// minted under, so it is refused as list again and a retry alone parks on
// it; repository migration 7 starts the drain over, and the retry then
// reaches `Zed`.
func TestAnInterruptedSweepStartsOverAfterTheByteOrderChange(t *testing.T) {
	ctx := context.Background()
	const pkg = "sweep.test.dev/sweep"
	widgetType, itemType := pkg+"/widget", pkg+"/item"
	triggerID := "on-sweep.sweep.test.dev"
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	open := func() *dataset {
		t.Helper()
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		d, err := svc.Dataset(ctx, repo)
		if err != nil {
			t.Fatalf("open dataset: %v", err)
		}
		return d.(*dataset)
	}
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	_ = svc.Close()
	ds := open()
	importVocabulary(t, ds, "tasks")
	raw := rawDBOf(t, dsn)
	if err := enginetest.Install(ctx, ds, substrate.ActorPerson, enginetest.Manifest{
		Name: "sweep", Authority: "sweep.test.dev",
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{
				"ids":        map[string]any{"type": vocabulary.IDsNamed},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			}),
			vocabulary.FunctionManifest(pkg, "sweep", map[string]any{
				"description": "sweeps every item in id order, one a page",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{
					"reads":  map[string]any{"kinds": []any{itemType}},
					"writes": []any{"samples.substrate.reamde.dev/tasks/task"},
				},
				"source": `
def main(input, host):
    page = host.records.list("` + itemType + `", first=1, order="id", after=input.get("resume") or None)
    effects = [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                "id": "seen-" + r["id"], "properties": {"name": r["id"]}}
               for r in page.get("records") or []]
    if page.get("cursor"):
        return {"effects": effects, "more": {"cursor": page["cursor"]}}
    return {"effects": effects}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: triggerID,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/sweep"),
			},
		}},
	}); err != nil {
		t.Fatalf("install the sweep: %v", err)
	}
	all := []string{"a", "b", "Zed", "zz"}
	for _, id := range all {
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: itemType, ID: id, Properties: map[string]any{"name": id}}); err != nil {
			t.Fatalf("put item %s: %v", id, err)
		}
	}
	seen := func() (out []string) {
		for _, id := range all {
			if liveExists(t, ds, "seen-"+id) {
				out = append(out, id)
			}
		}
		return out
	}

	// Before the upgrade: the identity columns in the database's own
	// collation, as every database stood before 0021, and no mark of
	// migration 7 in the repository's history.
	restoreViews := stageTheLocaleOrder(t, raw)
	forgetCachedPlans(ds)
	forgetContinuationOrder(t, ds)
	if got := itemOrder(t, ds.db, itemType); got != "a,b,Zed,zz" {
		t.Fatalf("the database's own collation orders the items %s; the test needs one that sorts case together (the pgvector image's en_US)", got)
	}
	restore := withMaxPages(2)
	w, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "go"}})
	if err != nil {
		t.Fatal(err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	restore()
	chain := chainKey(ds, triggerID, wch.Seq)
	if got := seen(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("the interrupted sweep saw %v, want a and b, the old order's first two pages", got)
	}

	// The continuation it left is after `b`. The server before the upgrade
	// minted it at its own order's version; this one mints its own, so that
	// one field is set back, and the position and the counts are kept.
	var token, oldToken string
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		owner, version, err := tx.lockPagedCursor(chain)
		if err != nil {
			return err
		}
		var cur []byte
		var pages, effects, bytes int64
		var startedAt time.Time
		if err := tx.row(`SELECT cursor, pages, effects, bytes, started_at FROM paged_cursors WHERE chain = $1`, chain).
			Scan(&cur, &pages, &effects, &bytes, &startedAt); err != nil {
			return err
		}
		if err := json.Unmarshal(cur, &token); err != nil {
			return fmt.Errorf("the saved continuation %s is no list token: %w", cur, err)
		}
		k, err := decodeKeyset(token)
		if err != nil {
			return err
		}
		if len(k.K) == 0 || k.K[0] == nil || *k.K[0] != "b" {
			return fmt.Errorf("the saved continuation stands at %v, want after b", k.K)
		}
		k.V = substrate.IdentityOrder - 1
		old, _ := json.Marshal(k)
		oldToken = base64.RawURLEncoding.EncodeToString(old)
		if err := tx.pageTx(owner, chain, oldToken, version+1, pages, effects, bytes, startedAt); err != nil {
			return err
		}
		return tx.settleDelivery(owner.triggerID)
	}); err != nil {
		t.Fatalf("the saved continuation: %v", err)
	}

	// The upgrade: the server stopped, 0021's own SQL, the server started.
	upgrade, err := migrationsFS.ReadFile("migrations/0021_identity_columns_byte_order.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.svc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, string(upgrade)); err != nil {
		t.Fatalf("0021: %v", err)
	}
	restoreViews()
	ds = open()
	if got := itemOrder(t, ds.db, itemType); got != "Zed,a,b,zz" {
		t.Fatalf("after 0021 the items order %s, want byte order", got)
	}
	// What the refusal prevents: that position, resumed in byte order,
	// passes over Zed.
	if page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{itemType}}, OrderBy: []substrate.Order{{Property: "id"}}, First: 10, After: token,
	}); err != nil || len(page.Records) != 1 || page.Records[0].ID != "zz" {
		t.Fatalf("the position after b, read in byte order: %v, %v; want zz alone", page, err)
	}

	retry := func() []substrate.TriggerFailure {
		t.Helper()
		failures, err := ds.TriggerFailures(ctx, triggerID)
		if err != nil || len(failures) != 1 {
			t.Fatalf("the parked sweep: %v, %v", failures, err)
		}
		_, _ = ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID)
		after, err := ds.TriggerFailures(ctx, triggerID)
		if err != nil {
			t.Fatal(err)
		}
		return after
	}
	// The retry alone resumes from the refused token and parks on the
	// refusal at once, the token dropped so no retry sends it again
	// (functions.go refusedContinuation).
	if after := retry(); len(after) != 1 || !strings.Contains(after[0].LastError, "list again") ||
		!strings.Contains(after[0].LastError, "the server's paging changed") {
		t.Fatalf("a retry from the old token: %+v, want it parked on the refusal", after)
	}
	if got := seen(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("the sweep went on past a token it was refused: saw %v", got)
	}
	if row, ok := pagedRowOf(t, ds, chain); !ok || row.cursor != "null" {
		t.Fatalf("the refused token was kept: %+v (%v)", row, ok)
	}

	// The upgrade's repository migration starts such a drain over before any
	// retry reads the old token: the token as the upgrade finds it, again,
	// and migration 7, which must null it.
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		owner, version, err := tx.lockPagedCursor(chain)
		if err != nil {
			return err
		}
		if err := tx.pageTx(owner, chain, oldToken, version+1, 2, 2, 0, time.Now().UTC()); err != nil {
			return err
		}
		return tx.settleDelivery(owner.triggerID)
	}); err != nil {
		t.Fatalf("the old token again: %v", err)
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		_, err := migrateStartPagedDrainsOver(tx)
		return err
	}); err != nil {
		t.Fatalf("migration 7: %v", err)
	}
	if row, ok := pagedRowOf(t, ds, chain); !ok || row.cursor != "null" {
		t.Fatalf("migration 7 left the drain at %+v (%v), want it started over", row, ok)
	}
	if after := retry(); len(after) != 0 {
		t.Fatalf("the sweep after the migration: %+v, want it done", after)
	}
	if got := seen(); !slices.Equal(got, all) {
		t.Errorf("the sweep that started over saw %v, want every item, Zed included", got)
	}
	if _, ok := pagedCursor(t, ds, chain); ok {
		t.Error("the finished sweep left its resume row")
	}
}

// Repository migration 7 leaves its mark in the repository's history, so an
// import, which runs it again under a ledger with nothing for the
// repository, finds the order the saved continuations are of. A creation
// leaves the mark, so the first open has nothing to do; a run that finds it
// writes nothing and keeps a parked drain where it stands; a rebuild replays
// it; and a directory without it, as one written before the mark existed,
// starts the drain over at its import and gains the mark there.
func TestMigrationSevenLeavesItsMarkAndAnImportWithoutItStartsTheDrainOver(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedmark.test.dev/pagedmark", keyedPagedBody)
	ctx := context.Background()
	restore := withMaxPages(2)
	defer restore()
	if order := continuationOrderOf(t, ds); order != substrate.IdentityOrder {
		t.Fatalf("a new repository's mark says %d, want %d", order, substrate.IdentityOrder)
	}

	w, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "pagedmark.test.dev/pagedmark/widget", ID: fid("a"), Properties: map[string]any{"name": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	chain := chainKey(ds, triggerID, wch.Seq)
	parked, ok := pagedRowOf(t, ds, chain)
	if !ok || parked.cursor != "2" {
		t.Fatalf("the drain: %+v (%v), want it parked at cursor 2", parked, ok)
	}

	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var did string
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		did, err = migrateStartPagedDrainsOver(tx)
		return err
	}); err != nil {
		t.Fatalf("migration 7 again: %v", err)
	}
	if after, err := ds.Head(ctx); err != nil || after != head {
		t.Fatalf("migration 7 run again (%q) moved the head from %+v to %+v (%v)", did, head, after, err)
	}
	if got, ok := pagedRowOf(t, ds, chain); !ok || got != parked {
		t.Fatalf("migration 7 run again left the drain at %+v (%v), want %+v", got, ok, parked)
	}

	if _, err := ds.svc.RebuildRepository(ctx, ds.Repository().ID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if order := continuationOrderOf(t, ds); order != substrate.IdentityOrder {
		t.Fatalf("after a rebuild the mark says %d, want %d", order, substrate.IdentityOrder)
	}

	forgetContinuationOrder(t, ds)
	imported := importIntoEmptyDatabase(t, ds)
	if got, ok := pagedRowOf(t, imported, chain); !ok || got.cursor != "null" || got.version != parked.version+1 {
		t.Fatalf("a directory without the mark imported the drain as %+v (%v), want it started over at version %d", got, ok, parked.version+1)
	}
	if order := continuationOrderOf(t, imported); order != substrate.IdentityOrder {
		t.Fatalf("the import left the mark at %d, want %d", order, substrate.IdentityOrder)
	}
}

// continuationOrderOf reads the mark migration 7 leaves on the repository's
// record.
func continuationOrderOf(t *testing.T, ds *dataset) int {
	t.Helper()
	var order int
	if err := ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
		var err error
		order, err = tx.continuationOrder(eref{Kind: kindRepository, ID: ds.Repository().ID})
		return err
	}); err != nil {
		t.Fatalf("read the continuation order: %v", err)
	}
	return order
}

// forgetContinuationOrder takes migration 7's mark off the repository's
// record, through the changelog, as a repository stood before the mark
// existed.
func forgetContinuationOrder(t *testing.T, ds *dataset) {
	t.Helper()
	self := eref{Kind: kindRepository, ID: ds.Repository().ID}
	if err := ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
		if _, err := tx.putAnnotation(self, annContinuationOrder, nil); err != nil {
			return err
		}
		return tx.appendChange(substrate.ActorSystem, substrate.OpPatch, self.ID, self.Kind, nil)
	}); err != nil {
		t.Fatalf("take migration 7's mark off: %v", err)
	}
}

// stageTheLocaleOrder puts every identity column 0021 changes back in the
// database's own collation, as a database stood before 0021: the
// migration's statement that changes them, run the other way. Its refusal
// beside a running server is the migration's, not the staging's, so it is
// left out. The engine's own views over those columns are a later
// migration's, which a database before 0021 did not have, and Postgres
// alters no column a view reads, so they are set aside with the staging: it
// answers what puts them back as they stood, which a test runs once its own
// 0021 has run, as the later migration would in a real upgrade.
func stageTheLocaleOrder(t *testing.T, raw *sql.DB) (restoreViews func()) {
	t.Helper()
	body, err := migrationsFS.ReadFile("migrations/0021_identity_columns_byte_order.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, change, found := strings.Cut(string(body), "DO $$\nDECLARE\n    byte_order oid")
	stmt, _, ok := strings.Cut("DO $$\nDECLARE\n    byte_order oid"+change, "-- A reprojection the server had not finished")
	ok = ok && found
	back := strings.NewReplacer(
		`a.attcollation IS DISTINCT FROM byte_order`, `a.attcollation = byte_order`,
		`ALTER COLUMN %I TYPE text COLLATE "C"`, `ALTER COLUMN %I TYPE text COLLATE "default"`,
	).Replace(stmt)
	if !ok || back == stmt {
		t.Fatal("0021 no longer reads as this staging expects")
	}
	views := setViewsAside(t, raw)
	if _, err := raw.ExecContext(context.Background(), back); err != nil {
		t.Fatalf("stage the locale's order: %v", err)
	}
	return views
}

// setViewsAside drops every view of the schema and answers what creates
// each again as it stood: its definition, its options and its grants.
func setViewsAside(t *testing.T, raw *sql.DB) func() {
	t.Helper()
	ctx := context.Background()
	type view struct{ name, def, options string }
	var views []view
	rows, err := raw.QueryContext(ctx, `
		SELECT c.relname, pg_get_viewdef(c.oid), coalesce(array_to_string(c.reloptions, ', '), '')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'v' AND n.nspname = current_schema() ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v view
		if err := rows.Scan(&v.name, &v.def, &v.options); err != nil {
			t.Fatal(err)
		}
		views = append(views, v)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	grants := map[string][]string{}
	for _, v := range views {
		rows, err := raw.QueryContext(ctx, `
			SELECT format('GRANT %s ON %I TO %I', privilege_type, table_name, grantee)
			FROM information_schema.role_table_grants
			WHERE table_schema = current_schema() AND table_name = $1 AND grantee <> current_user`, v.name)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var g string
			if err := rows.Scan(&g); err != nil {
				t.Fatal(err)
			}
			grants[v.name] = append(grants[v.name], g)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.ExecContext(ctx, `DROP VIEW `+pq(v.name)); err != nil {
			t.Fatalf("set the view %s aside: %v", v.name, err)
		}
	}
	return func() {
		t.Helper()
		for _, v := range views {
			with := ""
			if v.options != "" {
				with = " WITH (" + v.options + ")"
			}
			if _, err := raw.ExecContext(ctx, `CREATE VIEW `+pq(v.name)+with+` AS `+v.def); err != nil {
				t.Fatalf("put the view %s back: %v", v.name, err)
			}
			for _, g := range grants[v.name] {
				if _, err := raw.ExecContext(ctx, g); err != nil {
					t.Fatalf("grant on the view %s again: %v", v.name, err)
				}
			}
		}
	}
}

// pq quotes an identifier for a statement a test builds.
func pq(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// forgetCachedPlans closes the server's pooled connections: a statement a
// connection prepared before a column's collation changed is refused after
// it ("cached plan must not change result type"), which a real upgrade never
// meets, because it changes the columns before the server connects.
func forgetCachedPlans(ds *dataset) {
	ds.svc.repoPool.Reset()
	ds.svc.maint.SetMaxIdleConns(0)
	ds.svc.maint.SetMaxIdleConns(5)
}

// itemOrder is the ids of kind's rows in the order of their column.
func itemOrder(t *testing.T, raw *sql.DB, kind string) string {
	t.Helper()
	rows, err := raw.QueryContext(context.Background(), `SELECT id FROM records WHERE kind = $1 ORDER BY id`, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(ids, ",")
}
