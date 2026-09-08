package engine

// The delivery ledger (delivery.go, decision 0064): a trigger's bookkeeping
// is folded from `delivery` changelog entries, so a repository directory
// imported into an empty database brings every trigger back where it stood,
// and a delivery's effects, run record and cursor motion commit together or
// not at all.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	ledgerPkg     = "ledger.test.dev/ledger"
	ledgerWidget  = ledgerPkg + "/widget"
	ledgerGadget  = ledgerPkg + "/gadget"
	ledgerTask    = "samples.substrate.reamde.dev/tasks/task"
	ledgerMirror  = "on-mirror"
	ledgerPager   = "on-page"
	ledgerHourly  = "on-hourly"
	ledgerHook    = "on-hook"
	ledgerHookKey = "x-api-key"
)

// installLedgerConnector stands up the four trigger shapes the ledger
// carries: a record trigger (widgets mirror into tasks), a paged record
// trigger (gadgets drain five pages), an hourly schedule anchored at
// startsAt, and a webhook whose body fails until a widget named `gate`
// exists and then echoes the request it was parked with.
func installLedgerConnector(t *testing.T, ds *dataset, startsAt time.Time) {
	t.Helper()
	ctx := context.Background()
	callable := func(name string) string {
		return vocabulary.RecordPath("substrate.reamde.dev/core/function", ledgerPkg+"/"+name)
	}
	err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "ledger", Authority: ledgerPkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(ledgerPkg, 0),
			vocabulary.ActorManifest(ledgerPkg, vocabulary.PackageActor(ledgerPkg)),
			vocabulary.KindManifest(ledgerPkg, map[string]any{"singular": "widget", "plural": "widgets"},
				map[string]any{"properties": map[string]any{
					"name":   map[string]any{"type": "string"},
					"want":   map[string]any{"type": "string", "fts": false},
					"target": map[string]any{"type": "string", "fts": false},
				}}),
			vocabulary.KindManifest(ledgerPkg, map[string]any{"singular": "gadget", "plural": "gadgets"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(ledgerPkg, "mirror", map[string]any{
				"description": "mirrors widgets into tasks",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{ledgerTask}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": env["record"]["properties"]["name"]}}]}
`,
			}),
			vocabulary.FunctionManifest(ledgerPkg, "page", map[string]any{
				"description": "a paged backfill body",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{ledgerTask}},
				"source":      pagedBody(5),
			}),
			vocabulary.FunctionManifest(ledgerPkg, "hourly", map[string]any{
				"description": "mints one task per fire",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{ledgerTask}},
				"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"],
                         "properties": {"name": input["mode"] + " " + fire["at"]}}]}
`,
			}),
			vocabulary.FunctionManifest(ledgerPkg, "hook", map[string]any{
				"description": "echoes a webhook request once the gate is open",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{
					"reads":  map[string]any{"kinds": []any{ledgerWidget}},
					"writes": []any{ledgerWidget},
				},
				"source": `
WIDGET = "` + ledgerWidget + `"

def main(input, host):
    if host.records.get(WIDGET, "gate") is None:
        raise RuntimeError("gate closed")
    env = input.get("envelope") or {}
    req = env.get("request") or {}
    headers = req.get("headers") or {}
    body = (req.get("body") or {}).get("text") or ""
    note = ""
    for p in req.get("parts") or []:
        if p.get("name") == "note":
            note = p.get("value") or ""
    host.effects.put(WIDGET, "hook-echo", properties={
        "name": body or note,
        "want": headers.get("x-github-event") or "",
        "target": headers.get("` + ledgerHookKey + `") or "absent",
    })
    return {"output": {}}
`,
			}),
		},
		Triggers: []enginetest.Trigger{
			{ID: ledgerMirror, Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{ledgerWidget}}},
				"callable": callable("mirror"),
			}},
			{ID: ledgerPager, Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{ledgerGadget}}},
				"callable": callable("page"),
			}},
			{ID: ledgerHourly, Properties: map[string]any{
				"enabled": true,
				"source": map[string]any{"schedule": map[string]any{
					"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
				}},
				"callable": callable("hourly"),
			}},
			{ID: ledgerHook, Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"webhook": map[string]any{}},
				"callable": callable("hook"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("install the ledger connector: %v", err)
	}
}

// firedAtOf reads a schedule trigger's fire state.
func firedAtOf(t *testing.T, ds *dataset, triggerID string) time.Time {
	t.Helper()
	var at time.Time
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1`, triggerID).Scan(&at); err != nil {
		t.Fatalf("fire state of %s: %v", triggerID, err)
	}
	return at.UTC()
}

// failureIDs lists a trigger's parked failure ids through the retry API's
// own read.
func failureIDs(t *testing.T, ds *dataset, triggerID string) []int64 {
	t.Helper()
	failures, err := ds.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatalf("failures of %s: %v", triggerID, err)
	}
	ids := make([]int64, 0, len(failures))
	for _, f := range failures {
		ids = append(ids, f.ID)
	}
	return ids
}

// deliveryEntries reads every `delivery` entry's payload as the table holds
// it, joined, so a test can hold the ledger to what it must not carry.
func deliveryEntries(t *testing.T, ds *dataset) (int, string) {
	t.Helper()
	var n int
	var joined string
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT count(*), coalesce(string_agg(payload::text, ' '), '') FROM changelog WHERE op = $1`,
		string(substrate.OpDelivery)).Scan(&n, &joined); err != nil {
		t.Fatal(err)
	}
	return n, joined
}

// visibleChanges reads the public feed whole, forward and backward, and
// fails on a delivery entry in either.
func visibleChanges(t *testing.T, ds *dataset) []substrate.Change {
	t.Helper()
	ctx := context.Background()
	forward, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 100000)
	if err != nil {
		t.Fatal(err)
	}
	backward, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{}, 100000)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range append(forward, backward...) {
		if ch.Op == substrate.OpDelivery {
			t.Fatalf("a delivery entry reached the public feed: seq %d", ch.Seq)
		}
	}
	return forward
}

// entriesOf counts the changelog rows addressed to one record.
func entriesOf(t *testing.T, ds *dataset, kind, id string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2`, kind, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The ticket's story (#366): a record trigger delivers once, a schedule fires
// one of its overdue occurrences, a paged drain parks mid-chain and a webhook
// parks with its request; the directory is imported into an empty database
// and every trigger stands where it stood. Not parallel: the paged cap and
// the schedule bound are package-level.
func TestARestoredRepositoryResumesItsDeliveries(t *testing.T) {
	restoreCap := withMaxPages(2)
	defer restoreCap()
	defer withScheduleDrain(1)()
	ctx := context.Background()
	d := openInternalDataset(t)
	startsAt := nowUTC().Add(-3*time.Hour - 30*time.Minute).Truncate(time.Minute)
	installLedgerConnector(t, d, startsAt)

	w, err := d.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: ledgerWidget, Properties: map[string]any{"name": "one"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := d.latestChangeOf(ctx, ledgerWidget, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	g, err := d.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: ledgerGadget, Properties: map[string]any{"name": "big"}})
	if err != nil {
		t.Fatalf("put gadget: %v", err)
	}
	gch, err := d.latestChangeOf(ctx, ledgerGadget, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if taskTitle(t, d, "t-"+w.ID) != "one" {
		t.Fatal("the mirror did not deliver")
	}
	chain := chainKey(d, ledgerPager, gch.Seq)
	if cur, ok := pagedCursor(t, d, chain); !ok || cur != 2 {
		t.Fatalf("paged cursor %v ok=%v, want 2 after the cap parked the drain", cur, ok)
	}
	pageIDs := failureIDs(t, d, ledgerPager)
	if len(pageIDs) != 1 {
		t.Fatalf("paged failures: %v", pageIDs)
	}

	// A webhook delivery parks with its request: only the headers a replay
	// needs and a reference to the body enter the ledger.
	const secret = "very-secret-key-value"
	const body = `{"say":"call the dentist"}`
	req := substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{
			"x-github-event": "parked", ledgerHookKey: secret, "content-type": "application/json",
			// A name that contains a kept word and is not a kept name.
			"x-event-authorization": "Bearer " + secret,
		},
		// A credential in the query string, the way a sender that cannot set
		// headers authenticates.
		Query: map[string][]string{"token": {secret}},
		Body:  []byte(body),
	}
	fid, err := d.svc.receiveWebhook(ctx, "geoah.example.com", ledgerHook, "", req, webhookFireInline)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	hookIDs := failureIDs(t, d, ledgerHook)
	if len(hookIDs) != 1 {
		t.Fatalf("webhook failures: %v", hookIDs)
	}
	// The schedule initialized at creation, so nothing was due; rewind it to
	// before its anchor so four occurrences are overdue, of which one pass
	// fires the oldest. The same pass scans the other triggers past the
	// entries the park and the fire wrote.
	if _, err := d.db.ExecContext(ctx, `UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`,
		ledgerHourly, startsAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := firedAtOf(t, d, ledgerHourly); !got.Equal(startsAt) {
		t.Fatalf("fire state %s, want the oldest occurrence %s", got, startsAt)
	}
	n, ledger := deliveryEntries(t, d)
	if n == 0 {
		t.Fatal("no delivery entry was written")
	}
	if strings.Contains(ledger, secret) {
		t.Fatal("the ledger carries the credential-shaped header")
	}
	if strings.Contains(ledger, "call the dentist") {
		t.Fatal("the ledger carries the request body")
	}
	if !strings.Contains(ledger, `"x-github-event"`) || !strings.Contains(ledger, `"blob"`) {
		t.Fatalf("the parked envelope lost its kept header or its body reference:\n%s", ledger)
	}
	// The failure's id is the seq of the entry that parked it.
	var op, addressed string
	if err := d.db.QueryRowContext(ctx, `SELECT op, record_id FROM changelog WHERE seq = $1`, hookIDs[0]).Scan(&op, &addressed); err != nil {
		t.Fatal(err)
	}
	if op != string(substrate.OpDelivery) || addressed != ledgerHook {
		t.Fatalf("failure %d names entry %s %s, want the webhook trigger's delivery entry", hookIDs[0], op, addressed)
	}
	// Nothing of the ledger reaches the public feed, and a continuation past
	// the last visible row reads nothing; the dispatcher's scan position still
	// covered the hidden rows, so the trigger reads no lag.
	visible := visibleChanges(t, d)
	if tail, err := d.Changes(ctx, visible[len(visible)-1].Seq, substrate.ChangeFilter{}, 100); err != nil || len(tail) != 0 {
		t.Fatalf("a continuation past the last visible row read %d rows (%v)", len(tail), err)
	}
	statuses, err := d.TriggerStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.ID == ledgerMirror && st.Lag != 0 {
			t.Fatalf("the mirror reads lag %d over the ledger's own entries", st.Lag)
		}
	}

	// Copy the directory into a fresh database.
	id := d.Repository().ID
	src := d.dir
	if err := d.svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	root2 := t.TempDir()
	dst, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	svc2, err := Open(ctx, testdb.NewSchema(t), WithDataRoot(root2), WithCredentialKey(TestCredentialKey),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("import the directory: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	d2 := ds2.(*dataset)

	// Every trigger stands where it stood: the cursors at the seq each
	// acknowledged, the schedule at the occurrence it fired, the failures under
	// the ids the retry API handed out, the drain at its last committed page.
	if got := cursorSeq(t, d2, ledgerMirror); got != wch.Seq {
		t.Fatalf("restored mirror cursor %d, want the delivered seq %d", got, wch.Seq)
	}
	if got := cursorSeq(t, d2, ledgerPager); got != gch.Seq {
		t.Fatalf("restored pager cursor %d, want the parked seq %d", got, gch.Seq)
	}
	if got := firedAtOf(t, d2, ledgerHourly); !got.Equal(startsAt) {
		t.Fatalf("restored fire state %s, want the acknowledged occurrence %s", got, startsAt)
	}
	if got := failureIDs(t, d2, ledgerPager); len(got) != 1 || got[0] != pageIDs[0] {
		t.Fatalf("restored paged failures %v, want %v", got, pageIDs)
	}
	if got := failureIDs(t, d2, ledgerHook); len(got) != 1 || got[0] != hookIDs[0] {
		t.Fatalf("restored webhook failures %v, want %v", got, hookIDs)
	}
	if cur, ok := pagedCursor(t, d2, chain); !ok || cur != 2 {
		t.Fatalf("restored paged cursor %v ok=%v, want 2", cur, ok)
	}
	visibleChanges(t, d2)

	// A pass delivers nothing again and drains the next overdue occurrence.
	if n := entriesOf(t, d2, ledgerTask, "t-"+w.ID); n != 1 {
		t.Fatalf("the mirrored task has %d entries before the pass", n)
	}
	if _, err := d2.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process after the restore: %v", err)
	}
	if n := entriesOf(t, d2, ledgerTask, "t-"+w.ID); n != 1 {
		t.Fatalf("the restored pass redelivered the widget: %d entries", n)
	}
	if got := firedAtOf(t, d2, ledgerHourly); !got.Equal(startsAt.Add(time.Hour)) {
		t.Fatalf("fire state after the restored pass %s, want the next occurrence %s", got, startsAt.Add(time.Hour))
	}
	if _, err := d2.Get(ctx, ledgerTask, "fire-"+fireID(startsAt.Add(time.Hour))); err != nil {
		t.Fatalf("the next occurrence did not fire: %v", err)
	}

	// The parked drain resumes from page 2: pages 0 and 1 are erased first,
	// and a restart from zero would recreate them.
	for _, pid := range []string{"p-0", "p-1"} {
		if _, err := d2.Delete(ctx, substrate.ActorAPI, ledgerTask, pid, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete %s: %v", pid, err)
		}
	}
	restoreCap()
	if _, err := d2.RetryTriggerFailure(ctx, ledgerPager, pageIDs[0]); err != nil {
		t.Fatalf("retry the paged drain: %v", err)
	}
	for _, pid := range []string{"p-2", "p-3", "p-4"} {
		if !liveExists(t, d2, pid) {
			t.Fatalf("resumed page %s missing", pid)
		}
	}
	for _, pid := range []string{"p-0", "p-1"} {
		if liveExists(t, d2, pid) {
			t.Fatalf("page %s was re-run: the retry restarted from zero", pid)
		}
	}
	if _, ok := pagedCursor(t, d2, chain); ok {
		t.Fatal("the resume row outlived the resumed drain")
	}
	if got := failureIDs(t, d2, ledgerPager); len(got) != 0 {
		t.Fatalf("paged failures after the retry: %v", got)
	}

	// The parked webhook replays the request it arrived with, body read back
	// from the blob store, without the header the ledger dropped.
	if _, err := d2.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: ledgerWidget, ID: "gate", Properties: map[string]any{"name": "open"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.RetryTriggerFailure(ctx, ledgerHook, hookIDs[0]); err != nil {
		t.Fatalf("retry the webhook: %v", err)
	}
	echo, err := d2.Get(ctx, ledgerWidget, "hook-echo")
	if err != nil {
		t.Fatalf("the retried webhook wrote nothing: %v", err)
	}
	if echo.Properties["name"] != body || echo.Properties["want"] != "parked" || echo.Properties["target"] != "absent" {
		t.Fatalf("the retried webhook saw %v; want the body, the kept header and no credential header", echo.Properties)
	}
	if got := failureIDs(t, d2, ledgerHook); len(got) != 0 {
		t.Fatalf("webhook failures after the retry: %v", got)
	}
	_ = fid

	// The restored fold, ledger included, is what a rebuild reproduces.
	before, err := d2.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.(*service).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, err := d2.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the rebuilt fold is not the restored one\n%s", firstDifferenceOf(before, after))
	}
}

// firstDifferenceOf names where two snapshots diverge.
func firstDifferenceOf(a, b []byte) string {
	as, bs := string(a), string(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		lo := max(0, i-200)
		return fmt.Sprintf("at byte %d:\n--- before\n%s\n--- after\n%s", i, as[lo:min(len(as), i+200)], bs[lo:min(len(bs), i+200)])
	}
	return fmt.Sprintf("lengths differ: %d vs %d", len(as), len(bs))
}

// A delivery's effects, its cursor motion and its run record are one
// transaction. With the settlement failing after the effects applied, the
// delivery parks and nothing of any attempt reached the tables or the
// changelog; with the fault gone, a retry lands the effects and retires the
// failure under one transaction group, and a dispatched delivery lands its
// effects, its delivery entry and its run record under one.
func TestADeliveryCommitsEffectsCursorAndRunTogether(t *testing.T) {
	t.Parallel()
	ds := openCursorDataset(t)
	ctx := context.Background()
	const pkg = "widgets.test.dev/widgets"
	widgetType := pkg + "/widget"
	triggerID := "on-mirror." + pkg
	actor := substrate.Actor(pkg)

	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error { return errors.New("the settlement failed") }
	ds.mu.Unlock()

	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "one"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if liveExists(t, ds, "t-"+w.ID) {
		t.Fatal("effects committed without their settlement")
	}
	if n := entriesOf(t, ds, "samples.substrate.reamde.dev/tasks/task", "t-"+w.ID); n != 0 {
		t.Fatalf("a rolled-back attempt logged %d entries", n)
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 || !strings.Contains(failures[0].LastError, "the settlement failed") {
		t.Fatalf("failures after the faulted attempts: %+v (%v)", failures, err)
	}
	var okRuns int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND `+referencePathSQL("props", "trigger")+` = $2 AND props->>'status' = 'ok'`,
		typeRun, vocabulary.RecordPath(typeTrigger, triggerID)).Scan(&okRuns); err != nil {
		t.Fatal(err)
	}
	if okRuns != 0 {
		t.Fatalf("%d OK run records for a delivery that never settled", okRuns)
	}
	if got := cursorSeq(t, ds, triggerID); got < wch.Seq {
		t.Fatalf("the park did not move the cursor past the change: %d < %d", got, wch.Seq)
	}
	parkedHead := maxSeqOf(t, ds)

	// The retry: the effect, the unpark and nothing else, one transaction.
	ds.mu.Lock()
	ds.deliveryFault = nil
	ds.mu.Unlock()
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if taskTitle(t, ds, "t-"+w.ID) != "one" {
		t.Fatal("the retry did not deliver")
	}
	if left, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retry: %+v (%v)", left, err)
	}
	if groups, rows := txnGroups(t, ds, parkedHead); groups != 1 || rows != 2 {
		t.Fatalf("the retry wrote %d rows in %d transaction groups, want 2 rows in 1", rows, groups)
	}

	// A dispatched delivery: the effect, the delivery entry and the run
	// record share one transaction group.
	head := maxSeqOf(t, ds)
	if _, err := ds.Put(ctx, actor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "two"}}); err != nil {
		t.Fatal(err)
	}
	putHead := maxSeqOf(t, ds)
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if groups, rows := txnGroups(t, ds, putHead); groups != 1 || rows != 3 {
		t.Fatalf("the delivery wrote %d rows in %d transaction groups, want 3 rows in 1", rows, groups)
	}
	var ops []string
	list, err := ds.db.QueryContext(ctx, `SELECT op || ' ' || kind FROM changelog WHERE seq > $1 ORDER BY seq`, putHead)
	if err != nil {
		t.Fatal(err)
	}
	for list.Next() {
		var s string
		if err := list.Scan(&s); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, s)
	}
	_ = list.Close()
	want := []string{"put samples.substrate.reamde.dev/tasks/task", "delivery " + typeTrigger, "put " + typeRun}
	if strings.Join(ops, "|") != strings.Join(want, "|") {
		t.Fatalf("the delivery's entries are %v, want %v", ops, want)
	}
	_ = head
}

// txnGroups counts the changelog rows above a seq and the distinct
// transaction groups they belong to.
func txnGroups(t *testing.T, ds *dataset, after int64) (groups, rows int) {
	t.Helper()
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(DISTINCT txn), count(*) FROM changelog WHERE seq > $1`, after).Scan(&groups, &rows); err != nil {
		t.Fatal(err)
	}
	return groups, rows
}

// A repository written before the ledger keeps its trigger state: the first
// open under this release records the tables' rows as ledger entries, so a
// rebuild, which clears the four tables with the fold, brings every cursor,
// fire state, parked failure and paged drain back.
func TestAFirstOpenAdoptsLegacyTriggerBookkeeping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	root := t.TempDir()
	open := func() *service {
		svc, err := Open(ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey),
			WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		return svc.(*service)
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	d := ds.(*dataset)
	importVocabulary(t, d)
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour))

	// What a binary before the ledger leaves behind: rows in the four tables
	// that no changelog entry describes, under a stamp below the ledger's rung.
	past := nowUTC().Add(-90 * time.Minute).Truncate(time.Second)
	chain := chainKey(d, ledgerPager, 9)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE changelog_dialect SET dialect = $1`, []any{deliveryLedgerDialect - 1}},
		{`UPDATE trigger_cursors SET seq = 3 WHERE trigger_id = $1`, []any{ledgerMirror}},
		{`UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, []any{ledgerHourly, past}},
		{`INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		  VALUES (-7, $1, 3, '', 'w', 3, 'legacy', $2)`, []any{ledgerMirror, past}},
		// A webhook park from before the ledger: every header, the query and
		// the body, credentials in each position.
		{`INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at, payload)
		  VALUES (-8, $1, 0, 'hook-legacy', '', 3, 'gate closed', $2, $3::jsonb)`, []any{
			ledgerHook, past,
			`{"fire":{"id":"hook-legacy","at":"2026-09-01T00:00:00Z"},"request":{"method":"POST","contentType":"application/json",` +
				`"headers":{"authorization":"Bearer LEGACY-AUTH-SECRET","x-api-key":"LEGACY-KEY-SECRET","x-github-event":"push"},` +
				`"query":{"token":["LEGACY-QUERY-SECRET"]},"body":{"text":"LEGACY-BODY-SECRET"}}}`,
		}},
		{`INSERT INTO paged_cursors (chain, cursor, pages, version, effects, bytes, started_at, trigger_id, kind, identity, updated_at)
		  VALUES ($1, '2'::jsonb, 2, 2, 2, 2, $3, $2, 'record', '9', $3)`, []any{chain, ledgerPager, past}},
	} {
		if _, err := d.db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("plant legacy rows: %v", err)
		}
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	// An open whose adoption fails after the first trigger records nothing
	// and stamps nothing: the whole adoption is one transaction, so the next
	// open adopts every trigger.
	adopted := 0
	faulty, err := Open(ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		func(o *options) {
			o.adoptFault = func(string) error {
				adopted++
				return errors.New("adoption fault")
			}
		})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := faulty.Dataset(ctx, "geoah"); err == nil || !strings.Contains(err.Error(), "adoption fault") {
		t.Fatalf("the faulted open answered %v", err)
	}
	_ = faulty.Close()
	if adopted != 1 {
		t.Fatalf("the fault fired after %d triggers, want the first", adopted)
	}
	probe, err := open().Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// The probe opened, adopted and stamped; the assertions below run over a
	// service opened after it, so a stamp left by a partial adoption would
	// show as rows the rebuild loses.
	pd := probe.(*dataset)
	if stored, err := readChangelogDialect(ctx, pd.db); err != nil || stored != maxChangelogDialect {
		t.Fatalf("the adoption did not stamp the ledger's dialect: %d (%v)", stored, err)
	}
	_ = pd.svc.Close()

	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open under the ledger: %v", err)
	}
	d2 := ds2.(*dataset)
	if _, err := svc2.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := cursorSeq(t, d2, ledgerMirror); got != 3 {
		t.Fatalf("legacy cursor after the rebuild %d, want 3", got)
	}
	if got := firedAtOf(t, d2, ledgerHourly); !got.Equal(past) {
		t.Fatalf("legacy fire state after the rebuild %s, want %s", got, past)
	}
	if got := failureIDs(t, d2, ledgerMirror); len(got) != 1 || got[0] != -7 {
		t.Fatalf("legacy failures after the rebuild %v, want [-7]", got)
	}
	if cur, ok := pagedCursor(t, d2, chain); !ok || cur != 2 {
		t.Fatalf("legacy paged cursor after the rebuild %v ok=%v, want 2", cur, ok)
	}
	// The legacy webhook park came through the parking policy: the failure
	// survives with its kept header and a body reference, and no credential
	// reached the changelog.
	if got := failureIDs(t, d2, ledgerHook); len(got) != 1 || got[0] != -8 {
		t.Fatalf("legacy webhook failures after the rebuild %v, want [-8]", got)
	}
	_, ledger := deliveryEntries(t, d2)
	for _, secret := range []string{"LEGACY-AUTH-SECRET", "LEGACY-KEY-SECRET", "LEGACY-QUERY-SECRET", "LEGACY-BODY-SECRET"} {
		if strings.Contains(ledger, secret) {
			t.Fatalf("the adoption wrote %s into the changelog", secret)
		}
	}
	if !strings.Contains(ledger, `"x-github-event"`) || !strings.Contains(ledger, `"blob"`) {
		t.Fatalf("the adopted payload lost its kept header or its body reference:\n%s", ledger)
	}
}

// A ledger motion takes the changelog lock before its row (rows.go
// changelogLockKey): with the changelog held by a barrier, a replay parks
// there having locked nothing, so the cursor row stays free.
func TestALedgerMotionTakesTheChangelogFirst(t *testing.T) {
	t.Parallel()
	ds := openCursorDataset(t)
	ctx := context.Background()
	const triggerID = "on-mirror.widgets.test.dev/widgets"

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = barrier.Rollback() }()
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- ds.ReplayTrigger(ctx, triggerID, 0) }()
	waitParked(t, ds, changelogLockKey, 1)

	probe, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var one int
	err = probe.QueryRowContext(ctx, `SELECT 1 FROM trigger_cursors WHERE trigger_id = $1 FOR UPDATE NOWAIT`, triggerID).Scan(&one)
	_ = probe.Rollback()
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		t.Fatal("the replay locked the cursor row before the changelog lock")
	}
	if err != nil {
		t.Fatalf("probe the cursor row: %v", err)
	}
	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("replay after the barrier lifted: %v", err)
	}
	if got := cursorSeq(t, ds, triggerID); got != 0 {
		t.Fatalf("cursor after the replay %d, want 0", got)
	}
}

// An agent delivery is claimed before its loop runs and completed after it:
// with the completion failing on every attempt, the loop's effects are on
// the tables under a recorded claim (the cursor past the change, the delivery
// listed as parked), never as effects with no completion; the next pass
// redelivers nothing, and a retry by hand completes it. The claim's entry
// precedes every write of the loop.
func TestAnAgentDeliveryIsClaimedBeforeItsLoopAndCompletedAfter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/agent", crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	// Three attempts and the retry: each runs the tool and settles.
	var turns []fakeTurn
	for range 4 {
		turns = append(turns, fakeTurn{calls: []fakeCall{{"keyecho", `{}`}}}, fakeTurn{content: "kept"})
	}
	fake.script("keep", turns...)

	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error { return errors.New("the completion failed") }
	ds.mu.Unlock()
	w, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-claim", Properties: map[string]any{"name": "raw"},
	})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	// The loop's effects committed (three threads, one task), and the
	// delivery is recorded: the cursor past the change, one parked failure
	// carrying the completion's error, no OK run.
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 3 {
		t.Fatalf("keeper threads after three faulted attempts: %d", got)
	}
	// A completion that failed took the thread's terminal patch down with
	// it, and the loop settled the thread as an error in its own
	// transaction: none is left `running`.
	for _, th := range agentThreadsOf(t, ds, "keeper") {
		if th["status"] != threadError {
			t.Fatalf("a thread whose completion failed reads %v, want %s", th["status"], threadError)
		}
	}
	if !liveExists(t, ds, "t-idem") {
		t.Fatal("the loop's tool effect did not commit")
	}
	if got := cursorSeq(t, ds, tr.ID); got < wch.Seq {
		t.Fatalf("cursor %d below the claimed change %d", got, wch.Seq)
	}
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 || failures[0].Attempts != 3 || !strings.Contains(failures[0].LastError, "the completion failed") {
		t.Fatalf("failures after the faulted attempts: %+v (%v)", failures, err)
	}
	okRuns := func() int {
		var n int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL
			  AND `+referencePathSQL("props", "trigger")+` = $2 AND props->>'status' = 'ok'`,
			typeRun, vocabulary.RecordPath(typeTrigger, tr.ID)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if okRuns() != 0 {
		t.Fatal("an OK run record for a delivery that never completed")
	}
	// The claim came first: its entry, whose seq is the failure's id, sits
	// below every entry the loop wrote.
	var firstLoopSeq int64
	if err := ds.db.QueryRowContext(ctx, `SELECT min(seq) FROM changelog WHERE kind = $1`, typeThread).Scan(&firstLoopSeq); err != nil {
		t.Fatal(err)
	}
	if failures[0].ID >= firstLoopSeq {
		t.Fatalf("the claim (seq %d) did not precede the loop's first write (seq %d)", failures[0].ID, firstLoopSeq)
	}
	// Nothing redelivers by itself.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 3 {
		t.Fatalf("a second pass redelivered the claimed change: %d threads", got)
	}

	// Retried by hand once the completion can land: the claim retires. A
	// retry mints no run record, like every direct invocation.
	ds.mu.Lock()
	ds.deliveryFault = nil
	ds.mu.Unlock()
	if _, err := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, tr.ID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retry: %+v (%v)", left, err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 4 {
		t.Fatalf("keeper threads after the retry: %d, want 4", got)
	}
	// The completion rode the thread's settling transaction: the delivery
	// entry that retired the claim and the thread's terminal patch share one
	// transaction group.
	var shared int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog c JOIN changelog d ON c.txn = d.txn
		WHERE d.op = $1 AND d.seq = (SELECT max(seq) FROM changelog WHERE op = $1)
		  AND c.kind = $2 AND c.op = $3`,
		string(substrate.OpDelivery), typeThread, string(substrate.OpPatch)).Scan(&shared); err != nil {
		t.Fatal(err)
	}
	if shared != 1 {
		t.Fatalf("the completion and the thread's terminal patch share a transaction %d times, want 1", shared)
	}
}

// Two hands retry one failure at once: the first holds the failure in this
// process before its body runs, the second answers conflict before running
// anything, and the failure retires once. The settlement seam holds the first
// retry inside its settlement while the second is refused.
func TestConcurrentRetriesOfOneFailureRetireItOnce(t *testing.T) {
	t.Parallel()
	ds := openCursorDataset(t)
	ctx := context.Background()
	const pkg = "widgets.test.dev/widgets"
	widgetType := pkg + "/widget"
	triggerID := "on-mirror." + pkg
	actor := substrate.Actor(pkg)

	// A widget without a name parks the mirror; naming it makes the retry
	// deliver.
	w, err := ds.Put(ctx, actor, substrate.PutInput{Kind: widgetType})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures: %+v (%v)", failures, err)
	}
	if _, err := ds.Patch(ctx, actor, widgetType, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "fixed"}}); err != nil {
		t.Fatal(err)
	}

	inSettle := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error {
		once.Do(func() {
			close(inSettle)
			<-release
		})
		return nil
	}
	ds.mu.Unlock()

	first := make(chan error, 1)
	go func() {
		_, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID)
		first <- err
	}()
	<-inSettle
	// The first retry holds the failure and sits in its settlement; the
	// second is refused at once, before it runs the body.
	_, second := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID)
	if !errors.Is(second, substrate.ErrConflict) {
		t.Fatalf("the second retry answered %v, want conflict", second)
	}
	close(release)

	if err := <-first; err != nil {
		t.Fatalf("the first retry: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retries: %+v (%v)", left, err)
	}
	if taskTitle(t, ds, "t-"+w.ID) != "fixed" {
		t.Fatal("the retry did not deliver")
	}
	if n := entriesOf(t, ds, "samples.substrate.reamde.dev/tasks/task", "t-"+w.ID); n != 1 {
		t.Fatalf("the task has %d entries; the losing retry's effects committed", n)
	}
}

// A trigger's scan position is Postgres-only until the trigger is edited:
// the edit pins it into the ledger, so a restore applies the edited source
// only from the edit on. Trigger A watches widgets and scans past a gadget;
// A is then widened to gadgets; restored into an empty database and drained,
// A does not deliver the old gadget it scanned past, and does deliver a new
// one.
func TestARestoredTriggerAppliesAnEditedSourceFromTheEdit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t)
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour))
	put := func(ds *dataset, kind, id string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: map[string]any{"name": id}}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	process := func(ds *dataset) {
		t.Helper()
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	// The pager trigger watches gadgets: it is disabled here so the gadgets
	// below reach only the mirror.
	if _, err := d.Patch(ctx, substrate.ActorAPI, typeTrigger, ledgerPager, substrate.PatchInput{Properties: map[string]any{"enabled": false}}); err != nil {
		t.Fatal(err)
	}
	put(d, ledgerWidget, "w1")
	process(d)
	if taskTitle(t, d, "t-w1") != "w1" {
		t.Fatal("the mirror did not deliver the widget")
	}
	// A gadget the mirror scans past: no source matches it, so only the
	// Postgres-only scan position moves.
	put(d, ledgerGadget, "g-old")
	process(d)
	if liveExists(t, d, "t-g-old") {
		t.Fatal("the mirror delivered a kind outside its source")
	}
	// Widen the mirror to gadgets: the edit pins the scan position.
	if _, err := d.Patch(ctx, substrate.ActorAPI, typeTrigger, ledgerMirror, substrate.PatchInput{
		Properties: map[string]any{"source": map[string]any{"record": map[string]any{"kinds": []any{ledgerWidget, ledgerGadget}}}},
	}); err != nil {
		t.Fatalf("widen the trigger: %v", err)
	}
	// Nothing is delivered between the edit and the copy: the last
	// acknowledged position stays at the widget, below the gadget, so only
	// the pin at the edit tells a replay the gadget was already scanned.
	process(d)
	if liveExists(t, d, "t-g-old") {
		t.Fatal("the widened mirror delivered the gadget it had scanned past")
	}

	// Restore into an empty database and drain.
	id := d.Repository().ID
	src := d.dir
	if err := d.svc.Close(); err != nil {
		t.Fatal(err)
	}
	root2 := t.TempDir()
	dst, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	svc2, err := Open(ctx, testdb.NewSchema(t), WithDataRoot(root2), WithCredentialKey(TestCredentialKey),
		WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("import the directory: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	d2 := ds2.(*dataset)
	process(d2)
	if liveExists(t, d2, "t-g-old") {
		t.Fatal("the restore delivered the gadget the live trigger scanned past: an effect that never occurred")
	}
	if n := entriesOf(t, d2, ledgerTask, "t-w1"); n != 1 {
		t.Fatalf("the restore redelivered the widget: %d entries", n)
	}
	put(d2, ledgerGadget, "g-new")
	process(d2)
	if taskTitle(t, d2, "t-g-new") != "g-new" {
		t.Fatal("the restored trigger did not deliver a new gadget under its edited source")
	}
}

// A replay that reaches a change already parked parks it again, cursor
// motion included, and moves on to the healthy change behind it: an older
// park is not a claim.
func TestAReplayOverAParkedChangeParksItAgainAndMovesOn(t *testing.T) {
	t.Parallel()
	ds := openCursorDataset(t)
	ctx := context.Background()
	const pkg = "widgets.test.dev/widgets"
	widgetType := pkg + "/widget"
	triggerID := "on-mirror." + pkg
	actor := substrate.Actor(pkg)

	// A widget without a name parks the mirror; the named one behind it
	// delivers.
	if _, err := ds.Put(ctx, actor, substrate.PutInput{Kind: widgetType, ID: "poison"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Put(ctx, actor, substrate.PutInput{Kind: widgetType, ID: "fine", Properties: map[string]any{"name": "fine"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if taskTitle(t, ds, "t-fine") != "fine" {
		t.Fatal("the healthy change was not delivered")
	}
	if got := failureIDs(t, ds, triggerID); len(got) != 1 {
		t.Fatalf("failures after the first pass: %v", got)
	}

	// Replayed from the start, the poisoned change parks again and the pass
	// moves past it and the healthy one to the head.
	if err := ds.ReplayTrigger(ctx, triggerID, 0); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got, head := cursorSeq(t, ds, triggerID), headSeq(t, ds); got != head {
		t.Fatalf("cursor %d after the replay, want the head %d: the parked change wedged the trigger", got, head)
	}
	if got := failureIDs(t, ds, triggerID); len(got) != 2 {
		t.Fatalf("failures after the replay %v, want the old park and the new one", got)
	}
	if n := entriesOf(t, ds, "samples.substrate.reamde.dev/tasks/task", "t-fine"); n != 1 {
		t.Fatalf("the replayed healthy delivery wrote %d entries, want the no-op's 1", n)
	}
}

// A claim another dispatch holds is skipped: the pass moves its cursor past
// the change, writes a skipped run naming the claim, and runs no loop.
func TestAClaimAnotherDispatchHoldsIsSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/agent", crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	w, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-held", Properties: map[string]any{"name": "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wch, err := ds.latestChangeOf(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Another dispatch's claim on the change, as its loop would leave it.
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		VALUES (-9, $1, $2, '', $3, 0, $4, $5)`, tr.ID, wch.Seq, w.ID, inFlightError, nowUTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 0 {
		t.Fatalf("the pass ran the loop under another dispatch's claim: %d threads", got)
	}
	if got := cursorSeq(t, ds, tr.ID); got < wch.Seq {
		t.Fatalf("cursor %d did not move past the claimed change %d", got, wch.Seq)
	}
	var reason string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT props->>'reason' FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND `+referencePathSQL("props", "trigger")+` = $2 AND props->>'status' = 'skipped'`,
		typeRun, vocabulary.RecordPath(typeTrigger, tr.ID)).Scan(&reason); err != nil {
		t.Fatalf("the skipped run record: %v", err)
	}
	if !strings.Contains(reason, "claimed by another dispatch") {
		t.Fatalf("skipped run reason %q", reason)
	}
	// The claim stands for the dispatch that holds it.
	if got := failureIDs(t, ds, tr.ID); len(got) != 1 || got[0] != -9 {
		t.Fatalf("failures after the skip %v, want the claim alone", got)
	}
}

// Two hands retry one AGENT failure at once: the loop's tool effects commit
// as it runs, so the second hand must be refused before any loop starts. The
// first retry holds the failure in this process before its loop runs, the
// second answers conflict while the first is still inside its completion,
// one loop runs and one set of effects lands.
func TestConcurrentRetriesOfOneAgentFailureRunOneLoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/agent", crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	// Three transport failures park the dispatch; one tool turn and a reply
	// serve the single retry that runs.
	fake.script("keep",
		fakeTurn{status: 500}, fakeTurn{status: 500}, fakeTurn{status: 500},
		fakeTurn{calls: []fakeCall{{"keyecho", `{}`}}}, fakeTurn{content: "kept"},
	)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-race", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures after the parked dispatch: %+v (%v)", failures, err)
	}
	threadsBefore := len(agentThreadsOf(t, ds, "keeper"))

	inComplete := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error {
		once.Do(func() {
			close(inComplete)
			<-release
		})
		return nil
	}
	ds.mu.Unlock()

	first := make(chan error, 1)
	go func() {
		_, err := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID)
		first <- err
	}()
	<-inComplete
	// The first retry's loop ran and it sits in its completion; the second
	// is refused before it starts a loop.
	_, second := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID)
	if !errors.Is(second, substrate.ErrConflict) {
		t.Fatalf("the second retry answered %v, want conflict", second)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("the first retry: %v", err)
	}

	if got := len(agentThreadsOf(t, ds, "keeper")); got != threadsBefore+1 {
		t.Fatalf("keeper threads after the retries: %d, want %d (one loop)", got, threadsBefore+1)
	}
	if n := entriesOf(t, ds, "samples.substrate.reamde.dev/tasks/task", "t-idem"); n != 1 {
		t.Fatalf("the tool effect has %d entries, want one set of effects", n)
	}
	if left, err := ds.TriggerFailures(ctx, tr.ID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retries: %+v (%v)", left, err)
	}
	// The hold is released: a third retry answers not found, not conflict.
	if _, err := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a retry of the retired failure answered %v, want not found", err)
	}
}

// A pass that loaded a trigger before an edit cannot land its scan advance
// after the edit's pin: the advance is fenced on the trigger's version, so the
// stale pass ends with errCursorMoved, the cursor stays where the pin put it,
// and the next pass re-scans under the new source.
func TestAScanAdvanceLosesToATriggerEdit(t *testing.T) {
	t.Parallel()
	ds := openCursorDataset(t)
	ctx := context.Background()
	const triggerID = "on-mirror.widgets.test.dev/widgets"

	// The pass under test loads the trigger and reads its cursor here.
	stale, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := ds.ensureCursor(ctx, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	// Rows the mirror's source does not match: a pass scans past them.
	for _, id := range []string{"a", "b", "c"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: "samples.substrate.reamde.dev/tasks/task", ID: "unmatched-" + id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatal(err)
		}
	}
	scanned := headSeq(t, ds)
	// The edit lands inside the window: it pins the cursor and bumps the
	// record.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTrigger, triggerID, substrate.PatchInput{
		Properties: map[string]any{"source": map[string]any{"record": map[string]any{
			"kinds": []any{"widgets.test.dev/widgets/widget"}, "coalesce": true,
		}}},
	}); err != nil {
		t.Fatalf("edit the trigger: %v", err)
	}
	pinned := cursorSeq(t, ds, triggerID)
	// The stale pass's tail advance loses whole.
	if err := ds.advanceCursor(ctx, stale, cursor, scanned); !errors.Is(err, errCursorMoved) {
		t.Fatalf("a stale scan advance returned %v, want errCursorMoved", err)
	}
	if got := cursorSeq(t, ds, triggerID); got != pinned {
		t.Fatalf("the stale advance moved the cursor to %d past the pin at %d", got, pinned)
	}
	// The next pass, under the new definition, scans to the head.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if got, head := cursorSeq(t, ds, triggerID), headSeq(t, ds); got != head {
		t.Fatalf("cursor %d after the next pass, want the head %d", got, head)
	}
}

// A retry by hand of a claim whose dispatch is still running answers
// conflict and starts nothing; once the dispatch completes, the claim is gone.
func TestARetryOfARunningClaimAnswersConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/agent", crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	fake.script("keep", fakeTurn{calls: []fakeCall{{"keyecho", `{}`}}}, fakeTurn{content: "kept"})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-running", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}

	// The dispatch sits inside its completion, the claim still held.
	inComplete := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	ds.mu.Lock()
	ds.deliveryFault = func(*txn) error {
		once.Do(func() {
			close(inComplete)
			<-release
		})
		return nil
	}
	ds.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := ds.ProcessTriggers(ctx)
		done <- err
	}()
	<-inComplete
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 || failures[0].LastError != inFlightError {
		t.Fatalf("the running dispatch's claim: %+v (%v)", failures, err)
	}
	if _, err := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a retry of a running claim answered %v, want conflict", err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 1 {
		t.Fatalf("the refused retry ran a loop: %d threads", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the dispatch: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, tr.ID); err != nil || len(left) != 0 {
		t.Fatalf("the completed dispatch left its claim: %+v (%v)", left, err)
	}
}

// A multipart webhook that parks keeps its inline part values out of the
// changelog: each is spooled to the blob store by digest like a file part,
// and the retry reads it back and delivers the request as it arrived.
func TestAParkedMultipartWebhookKeepsItsPartValuesOutOfTheLedger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t)
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour))

	const secret = "PART-TOKEN-SECRET-VALUE"
	req := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data",
		Headers: map[string]string{"x-github-event": "form"},
		Query:   map[string][]string{},
		Parts: []substrate.WebhookPart{
			{Name: "token", Value: secret},
			{Name: "note", Value: "call the dentist"},
			{Name: "audio", MediaType: "audio/mp4", Data: []byte("not really audio")},
		},
	}
	if _, err := d.svc.receiveWebhook(ctx, "geoah.example.com", ledgerHook, "", req, webhookFireInline); err != nil {
		t.Fatalf("receive: %v", err)
	}
	ids := failureIDs(t, d, ledgerHook)
	if len(ids) != 1 {
		t.Fatalf("webhook failures: %v", ids)
	}
	_, ledger := deliveryEntries(t, d)
	if strings.Contains(ledger, secret) || strings.Contains(ledger, "call the dentist") {
		t.Fatalf("an inline part value reached the changelog:\n%s", ledger)
	}
	if !strings.Contains(ledger, `"valueBlob"`) || !strings.Contains(ledger, `"audio"`) {
		t.Fatalf("the parked parts lost their references or names:\n%s", ledger)
	}
	// The spooled values are held against the orphan sweep while parked.
	if _, err := d.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	// The retry delivers the request as it arrived, values read back.
	if _, err := d.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: ledgerWidget, ID: "gate", Properties: map[string]any{"name": "open"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RetryTriggerFailure(ctx, ledgerHook, ids[0]); err != nil {
		t.Fatalf("retry: %v", err)
	}
	echo, err := d.Get(ctx, ledgerWidget, "hook-echo")
	if err != nil {
		t.Fatalf("the retried webhook wrote nothing: %v", err)
	}
	if echo.Properties["name"] != "call the dentist" || echo.Properties["want"] != "form" {
		t.Fatalf("the retried webhook saw %v; want the part value and the kept header", echo.Properties)
	}
	if left := failureIDs(t, d, ledgerHook); len(left) != 0 {
		t.Fatalf("failures after the retry: %v", left)
	}
}
