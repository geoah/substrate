package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Phase-1 adversarial-review regressions for the paged-checkpoint drain
//: the cursor-ownership CAS fence, the cumulative
// drain budget that spans retries, resume-before-every-delivery, the
// continuation secret scrub, and the paged-row lifecycle owner + sweep.

// withMaxDrainEffects lowers the cumulative effect budget for one test.
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

// TestPagedCursorOwnershipCAS (#1): the resume row is a compare-and-swap fence.
// Two dispatchers draining the same chain cannot both commit — a fresh claim of
// a chain another already owns, an advance from a stale version, and a delete
// under a stale version all miss and return errCursorMoved, so the loser's page
// transaction rolls back whole.
func TestPagedCursorOwnershipCAS(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	chain := "owner.test.dev/on-cas/1"
	owner := pagedOwner{triggerID: "on-cas", kind: pagedKindRecord, identity: "1"}

	claim := func() error {
		return ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			return tx.claimPagedCursor(chain, owner, 1, 1, 1, 1, nowUTC(), tx.now)
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
		return ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
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
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		return tx.clearPagedCursorCAS(chain, 1)
	}); !errors.Is(err, errCursorMoved) {
		t.Fatalf("stale clear returned %v, want errCursorMoved", err)
	}
	if _, ok := pagedCursor(t, ds, chain); !ok {
		t.Fatalf("a stale delete cleared the chain — the fence failed")
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		return tx.clearPagedCursorCAS(chain, 2)
	}); err != nil {
		t.Fatalf("version-matched clear: %v", err)
	}
	if _, ok := pagedCursor(t, ds, chain); ok {
		t.Fatalf("the version-matched delete left the row behind")
	}
}

// TestPagedDrainBudgetSpansRetries (#2): the cumulative effect budget bounds the
// WHOLE chain including automatic retries. A runaway pager parks deterministically
// at the budget, and a retry — which reloads the persisted counter — parks again
// at once WITHOUT resetting the budget and replaying more pages.
func TestPagedDrainBudgetSpansRetries(t *testing.T) {
	source := `
def main(input, host):
    page = input.get("resume") or 0
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "p-%d" % page, "properties": {"title": "x"}}],
            "more": {"cursor": page + 1}}
`
	ds, triggerID := openPagedDataset(t, "pagedbudget.test.dev", source)
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedbudget")

	defer withMaxDrainEffects(3)()

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedbudget.test.dev/widget", Properties: map[string]any{"name": "runaway"}})
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
		if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", fmt.Sprintf("p-%d", page)); err != nil {
			t.Fatalf("page %d missing under the budget: %v", page, err)
		}
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "p-3"); !errors.Is(err, substrate.ErrNotFound) {
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
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "p-3"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the retry reset the budget and committed page 3")
	}
	if cur, ok := pagedCursor(t, ds, chain); !ok || cur != 3 {
		t.Fatalf("resume cursor after the retry re-park: %v ok=%v, want a still-3", cur, ok)
	}
}

// TestPagedRedispatchResumesFromCursor (#3): an ordinary redelivery of an
// existing chain (a post-crash dispatch that never wrote the park row, so the
// trigger cursor never advanced) loads the committed resume cursor and continues
// from the last committed page instead of replaying page zero.
func TestPagedRedispatchResumesFromCursor(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedredispatch.test.dev", pagedBody(5))
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedredispatch")

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedredispatch.test.dev/widget", Properties: map[string]any{"name": "crashed"}})
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
	pagedSecretAuthority = "pagedsecret.bundles.substrate.reamde.dev"
	pagedSecretValue     = "sk-paged-continuation-secret-77"
)

// installPagedSecretBundle stands up a bundle whose config carries a secret and
// a PAGED function that copies that secret into its `more.cursor`, plus a widget
// type and a trigger.
func installPagedSecretBundle(t *testing.T, ds *dataset) string {
	t.Helper()
	ctx := context.Background()
	triggerID := "on-pagedsecret"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "pagedsecret", Authority: pagedSecretAuthority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(pagedSecretAuthority, ""),
			vocabulary.ActorManifest(pagedSecretAuthority, vocabulary.AuthorityActor(pagedSecretAuthority)),
			vocabulary.BundleManifest(pagedSecretAuthority, map[string]any{
				"description": "the paged secret bundle",
				"configType":  pagedSecretAuthority + "/sconfig",
				"installs": []any{
					pagedSecretAuthority + "/sconfig", pagedSecretAuthority + "/widget",
					pagedSecretAuthority + "/pnote", pagedSecretAuthority + "/leakpage",
				},
			}),
			vocabulary.KindManifest(pagedSecretAuthority,
				map[string]any{"singular": "sconfig", "plural": "sconfigs"},
				map[string]any{"traits": []any{"bundleconfig"}, "properties": map[string]any{
					"apiToken": map[string]any{"type": "secret"},
				}}),
			vocabulary.KindManifest(pagedSecretAuthority, map[string]any{"singular": "widget", "plural": "widgets"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(pagedSecretAuthority, map[string]any{"singular": "pnote", "plural": "pnotes"},
				map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(pagedSecretAuthority, "leakpage", map[string]any{
				"description":  "leaks the config secret into the paged continuation cursor",
				"runtime":      vocabulary.RuntimePython,
				"capabilities": map[string]any{"emit": []any{pagedSecretAuthority + "/pnote"}},
				"source": `
def main(input, host):
    tok = input["config"]["config"]["properties"]["apiToken"]
    return {"effects": [{"action": "put", "kind": "pagedsecret.bundles.substrate.reamde.dev/pnote",
                         "id": "pn-1", "properties": {"text": "hi"}}],
            "more": {"cursor": tok}}
`,
			}),
		},
		Triggers: []enginetest.Trigger{{
			ID: triggerID,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{pagedSecretAuthority + "/widget"}}},
				"callable": map[string]any{"kind": "core.substrate.reamde.dev/function", "id": pagedSecretAuthority + "/leakpage"},
			},
		}},
	}); err != nil {
		t.Fatalf("register paged secret bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pagedSecretAuthority + "/sconfig", ID: "ps-cfg",
		Properties: map[string]any{"apiToken": pagedSecretValue},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	return triggerID
}

// TestPagedContinuationSecretRejected (#4): a body that copies an injected
// secret into its continuation cursor is rejected like a secret-bearing effect —
// the invocation fails, nothing commits (no page effect, no resume row), and the
// raw secret never reaches the durable failure ledger.
func TestPagedContinuationSecretRejected(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	triggerID := installPagedSecretBundle(t, ds)

	w, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pagedSecretAuthority + "/widget", Properties: map[string]any{"name": "trip"},
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
	if _, err := ds.Get(ctx, pagedSecretAuthority+"/pnote", "pn-1"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the page effect committed even though the continuation was rejected")
	}
	if _, ok := pagedCursor(t, ds, chainKey(ds, triggerID, wch.Seq)); ok {
		t.Fatalf("a rejected continuation wrote a resume cursor")
	}
}

// TestPagedCursorLifecycleDropAndSweep (#8): a paged row is dropped when its
// trigger is deleted, and the sweep collects a row whose trigger no longer lives
// (and a stale, unreferenced row).
func TestPagedCursorLifecycleDropAndSweep(t *testing.T) {
	ds, triggerID := openPagedDataset(t, "pagedlifecycle.test.dev", pagedBody(5))
	ctx := context.Background()
	actor := substrate.Actor("connector:pagedlifecycle")

	restore := withMaxPages(2)

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: "pagedlifecycle.test.dev/widget", Properties: map[string]any{"name": "leaky"}})
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
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeTrigger, triggerID); err != nil {
		t.Fatalf("delete trigger: %v", err)
	}
	if _, ok := pagedCursor(t, ds, chain); ok {
		t.Fatalf("the paged row outlived its deleted trigger")
	}

	// A row whose trigger never existed is orphaned; the sweep collects it.
	orphan := "pagedlifecycle.test.dev/on-ghost/9"
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
