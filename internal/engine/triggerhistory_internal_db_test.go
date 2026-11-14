package engine

// A TRIGGER'S OWN HISTORY KEEPS SEVEN DAYS (decision 0320): past a week the
// compaction pass replaces a trigger's delivery lines and the edits to it
// with its ledger entry and its state, so its latest state, its cursor, its
// schedule's fire state, its parked failures and the resume row of a paged
// drain that has not finished stay, and a rebuild folds them back as they
// were. Who shipped a trigger still answers, a history longer than the
// pass's line bound is worked through in steps over passes, and an owner's
// override keeps every line.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// triggerLines counts the lines addressed to trigger id, and of them the
// ones carrying an effect other than a state or a ledger entry: a delivery
// or an edit as it was written.
func triggerLines(t *testing.T, ds *dataset, id string) (all, written int) {
	t.Helper()
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT count(*),
		       count(*) FILTER (WHERE jsonb_path_exists(payload, '$.fold[*] ? (@.kind != "state" && @.kind != "ledger")'))
		FROM changelog WHERE kind = $1 AND record_id = $2`, typeTrigger, id).Scan(&all, &written); err != nil {
		t.Fatal(err)
	}
	return all, written
}

// A trigger's history past a week: a record trigger that delivered and was
// edited three times, a paged drain parked mid-chain, an hourly schedule
// that fired and a webhook parked with its request. A pass a week later
// replaces the lines of each but the webhook, whose parked fire holds its
// lines, and every trigger stands where it stood: its row, its cursor, its
// fire state, its parked failures and its drain's resume row, which a
// rebuild folds back as they were. Then the drain resumes from its saved
// page, the webhook replays its request, the record trigger delivers
// nothing twice, and the pass a week after that compacts every trigger,
// the webhook too, and leaves no saved position of the finished drain. Not
// parallel: the paged cap and the schedule bound are package-level.
func TestATriggersHistoryPastAWeekGoesAndItsDeliveryStateStays(t *testing.T) {
	restoreCap := withMaxPages(2)
	defer restoreCap()
	defer withScheduleDrain(1)()
	ctx := context.Background()
	clock := time.Now().UTC()
	d := openInternalDataset(t, WithCompactionInterval(-1), WithTestCompactionClock(func() time.Time { return clock }))
	startsAt := nowUTC().Add(-3*time.Hour - 30*time.Minute).Truncate(time.Minute)
	installLedgerConnector(t, d, startsAt)

	g, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerGadget, Properties: map[string]any{"name": "big"}})
	if err != nil {
		t.Fatalf("put gadget: %v", err)
	}
	gch, err := d.latestChangeOf(ctx, ledgerGadget, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	var widgets []string
	for i := range 3 {
		w, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, Properties: map[string]any{"name": fmt.Sprintf("widget %d", i)}})
		if err != nil {
			t.Fatalf("put widget: %v", err)
		}
		widgets = append(widgets, w.ID)
		if _, err := d.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
		if _, err := d.Patch(ctx, substrate.ActorPerson, typeTrigger, ledgerMirror, substrate.PatchInput{
			Properties: map[string]any{"label": fmt.Sprintf("the mirror, edit %d", i)},
		}); err != nil {
			t.Fatalf("edit the mirror: %v", err)
		}
	}
	for _, id := range widgets {
		if taskTitle(t, d, "t-"+id) == "" {
			t.Fatalf("the mirror did not deliver %s", id)
		}
	}
	chain := chainKey(d, ledgerPager, gch.Seq)
	if cur, ok := pagedCursor(t, d, chain); !ok || cur != 2 {
		t.Fatalf("paged cursor %v ok=%v, want 2 after the cap parked the drain", cur, ok)
	}
	pageIDs := failureIDs(t, d, ledgerPager)
	if len(pageIDs) != 1 {
		t.Fatalf("paged failures: %v", pageIDs)
	}
	const body = `{"say":"call the dentist"}`
	if _, err := d.svc.receiveWebhook(ctx, testdb.Repository(t), ledgerHook, "", substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"x-github-event": "parked", "content-type": "application/json"},
		Body:    []byte(body),
	}, webhookFireInline); err != nil {
		t.Fatalf("receive: %v", err)
	}
	hookIDs := failureIDs(t, d, ledgerHook)
	if len(hookIDs) != 1 {
		t.Fatalf("webhook failures: %v", hookIDs)
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`,
		ledgerHourly, startsAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	processRecordTriggers(t, d)
	if got := firedAtOf(t, d, ledgerHourly); !got.Equal(startsAt) {
		t.Fatalf("fire state %s, want the oldest occurrence %s", got, startsAt)
	}
	// The head is never compacted; a contact is no trigger's source.
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}

	triggers := []string{ledgerMirror, ledgerPager, ledgerHourly, ledgerHook}
	before := map[string]int{}
	for _, id := range triggers {
		all, written := triggerLines(t, d, id)
		if written < 2 {
			t.Fatalf("%s has %d lines as written before the pass, want two at least: the test proves nothing", id, written)
		}
		before[id] = all
	}
	rows := map[string]*substrate.Record{}
	for _, id := range triggers {
		rec, err := d.Get(ctx, typeTrigger, id)
		if err != nil {
			t.Fatal(err)
		}
		rows[id] = rec
	}
	// The fold to hold the pass to is the history's replay: a record
	// trigger's scan position past rows it did not match is the table's
	// alone (delivery.go), and a rebuild resumes it at its last delivery.
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild before the pass: %v", err)
	}
	mirrorAt, pagerAt := cursorSeq(t, d, ledgerMirror), cursorSeq(t, d, ledgerPager)
	want := FoldRows(t, d)

	clock = time.Now().UTC().Add(8 * 24 * time.Hour)
	report, err := d.CompactionPass(ctx)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !slices.Contains(report.Kinds, typeTrigger) {
		t.Fatalf("the pass compacted no window of %s: %+v", typeTrigger, report)
	}
	for _, id := range []string{ledgerMirror, ledgerPager, ledgerHourly} {
		all, written := triggerLines(t, d, id)
		if all >= before[id] || written != 0 {
			t.Errorf("%s keeps %d lines after the pass, %d as written, of %d before: want its state and its ledger entry alone", id, all, written, before[id])
		}
	}
	// A parked webhook fire's line says whom the fire acts for and stays, and
	// so does every delivery effect of its trigger under it (compact.go).
	if all, _ := triggerLines(t, d, ledgerHook); all != before[ledgerHook] {
		t.Errorf("the webhook keeps %d lines while its fire is parked, want its %d", all, before[ledgerHook])
	}
	check := func(when string) {
		t.Helper()
		for _, id := range triggers {
			rec, err := d.Get(ctx, typeTrigger, id)
			if err != nil {
				t.Fatalf("%s: read %s: %v", when, id, err)
			}
			if rec.Version != rows[id].Version || fmt.Sprint(rec.Properties) != fmt.Sprint(rows[id].Properties) {
				t.Errorf("%s, %s is version %d %v, want version %d %v", when, id, rec.Version, rec.Properties, rows[id].Version, rows[id].Properties)
			}
		}
		if got := cursorSeq(t, d, ledgerMirror); got != mirrorAt {
			t.Errorf("%s, the mirror's cursor is %d, want %d", when, got, mirrorAt)
		}
		if got := cursorSeq(t, d, ledgerPager); got != pagerAt {
			t.Errorf("%s, the pager's cursor is %d, want %d", when, got, pagerAt)
		}
		if got := firedAtOf(t, d, ledgerHourly); !got.Equal(startsAt) {
			t.Errorf("%s, the schedule's fire state is %s, want %s", when, got, startsAt)
		}
		if got := failureIDs(t, d, ledgerPager); !slices.Equal(got, pageIDs) {
			t.Errorf("%s, the pager's parked failures are %v, want %v", when, got, pageIDs)
		}
		if got := failureIDs(t, d, ledgerHook); !slices.Equal(got, hookIDs) {
			t.Errorf("%s, the webhook's parked failures are %v, want %v", when, got, hookIDs)
		}
		if cur, ok := pagedCursor(t, d, chain); !ok || cur != 2 {
			t.Errorf("%s, the drain's resume row is at %v (%v), want page 2", when, cur, ok)
		}
		if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
			t.Errorf("%s, the fold is not what it was:\n%s", when, strings.Join(diff, "\n"))
		}
	}
	check("after the pass")
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	check("after a rebuild")

	// The drain resumes from its saved page: pages 0 and 1 are deleted first,
	// and a start from zero would write them again.
	for _, pid := range []string{"p-0", "p-1"} {
		if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, pid, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete %s: %v", pid, err)
		}
	}
	restoreCap()
	if _, err := d.RetryTriggerFailure(ctx, ledgerPager, pageIDs[0]); err != nil {
		t.Fatalf("retry the paged drain: %v", err)
	}
	for _, pid := range []string{"p-2", "p-3", "p-4"} {
		if !liveExists(t, d, pid) {
			t.Errorf("resumed page %s missing", pid)
		}
	}
	for _, pid := range []string{"p-0", "p-1"} {
		if liveExists(t, d, pid) {
			t.Errorf("page %s was run again: the retry started over", pid)
		}
	}
	// The webhook replays the request it was parked with.
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, ID: fid("gate"), Properties: map[string]any{"name": "open"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RetryTriggerFailure(ctx, ledgerHook, hookIDs[0]); err != nil {
		t.Fatalf("retry the webhook: %v", err)
	}
	if echo, err := d.Get(ctx, ledgerWidget, "hook-echo"); err != nil || echo.Properties["name"] != body {
		t.Errorf("the retried webhook wrote %v (%v), want the body it was parked with", echo, err)
	}
	// The mirror delivers nothing twice.
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	for _, id := range widgets {
		if n := entriesOf(t, d, ledgerTask, "t-"+id); n != 1 {
			t.Errorf("the task the mirror wrote for %s has %d entries after the pass and a delivery, want one", id, n)
		}
	}

	// A week after the fire settled and the drain finished, the pass compacts
	// every trigger, and no line of the pager holds a saved position.
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the next head"}}); err != nil {
		t.Fatal(err)
	}
	clock = time.Now().UTC().Add(16 * 24 * time.Hour)
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatalf("the pass a week later: %v", err)
	}
	for _, id := range triggers {
		if all, written := triggerLines(t, d, id); written != 0 || all > 2 {
			t.Errorf("a week after it settled, %s keeps %d lines, %d as written, want its state and its ledger entry", id, all, written)
		}
	}
	var positions int
	if err := d.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2
		  AND (jsonb_path_exists(payload, '$.fold[*].ledger.pages[*]') OR jsonb_path_exists(payload, '$.fold[*].page'))`,
		typeTrigger, ledgerPager).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	if positions != 0 {
		t.Errorf("%d lines of the pager hold a saved position a week after its drain finished, want none", positions)
	}
}

// Who shipped a webhook trigger answers after the pass a week later: Apple
// Health's trigger, which the owner keyed and turned on, is compacted past
// its package's lines, and the owner's write that turns it on with no key
// is refused as before.
func TestWhoShippedATriggerAnswersPastAWeek(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	ds := openCoreDataset(t, WithCompactionInterval(-1), WithTestCompactionClock(func() time.Time { return clock }))
	installAppleHealth(t, ds)
	packageHand := actorID(substrate.BundleActor("providers.substrate.reamde.dev", "applehealth"))
	_, _, keyless := hookState(t, ds)
	if err := putHook(ctx, ds, true, withHook(keyless, "key", ownersHookKey)); err != nil {
		t.Fatalf("the owner keys the trigger and turns it on: %v", err)
	}
	requireOpenRefused(t, "turning it on with no key, before the pass", putHook(ctx, ds, true, keyless), appleHealthHook, appleHealthPackage)
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	clock = time.Now().UTC().Add(8 * 24 * time.Hour)
	if _, err := ds.CompactionPass(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	var packageLines int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND actor = $3`,
		typeTrigger, appleHealthHook, packageHand).Scan(&packageLines); err != nil {
		t.Fatal(err)
	}
	if packageLines != 0 {
		t.Fatalf("the pass kept %d of the package's lines of the trigger: the test proves nothing", packageLines)
	}
	if pkg, err := shippedBy(ctx, ds.db, ds.registry(), appleHealthHook); err != nil || pkg != appleHealthPackage {
		t.Errorf("after the pass the trigger was shipped by %q (%v), want %s", pkg, err, appleHealthPackage)
	}
	requireOpenRefused(t, "turning it on with no key, after the pass", putHook(ctx, ds, true, keyless), appleHealthHook, appleHealthPackage)
}

// busySchedule moves the hourly schedule's fire state n times, each motion
// a delivery line of its own, as n fires would.
func busySchedule(t *testing.T, d *dataset, n int) {
	t.Helper()
	at := firedAtOf(t, d, ledgerHourly)
	for range n {
		at = at.Add(time.Hour)
		if err := d.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
			if err := tx.setScheduleTx(ledgerHourly, at); err != nil {
				return err
			}
			return tx.settleDelivery(ledgerHourly)
		}); err != nil {
			t.Fatalf("move the schedule: %v", err)
		}
	}
}

// A trigger with a history longer than the pass's line bound, as a sync
// firing every few minutes for years has: each pass stops after one batch,
// as a pass out of budget does, and the next goes on, so the history is
// worked through in steps, no step reading more than twice the bound, and
// ends as the trigger's state and its ledger entry, at the fire state it
// had.
func TestATriggerLongerThanTheBoundIsCompactedOverPasses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	var mu sync.Mutex
	var reads []int
	d := openInternalDataset(t, WithCompactionInterval(-1), WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionBatches(0, passBound, func(kind string, ids []string, _ int64, lines int) {
			if kind == typeTrigger && slices.Contains(ids, ledgerHourly) {
				mu.Lock()
				reads = append(reads, lines)
				mu.Unlock()
			}
		}),
		WithTestCompactionFault(func(stage string) error {
			if stage == compactionAfterBatch {
				return context.DeadlineExceeded
			}
			return nil
		}))
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
	busySchedule(t, d, 4*passBound)
	firedAt := firedAtOf(t, d, ledgerHourly)
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	if all, _ := triggerLines(t, d, ledgerHourly); all <= 4*passBound {
		t.Fatalf("the schedule has %d lines, want more than four times the bound: the test proves nothing", all)
	}
	clock = time.Now().UTC().Add(8 * 24 * time.Hour)
	passes := 0
	for ; passes < 30; passes++ {
		if _, written := triggerLines(t, d, ledgerHourly); written == 0 {
			break
		}
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", passes+1, err)
		}
	}
	all, written := triggerLines(t, d, ledgerHourly)
	if written != 0 || all > 2 {
		t.Fatalf("after %d passes the schedule keeps %d lines, %d as written, want its state and its ledger entry", passes, all, written)
	}
	mu.Lock()
	defer mu.Unlock()
	if passes < 2 || len(reads) < 3 {
		t.Errorf("the schedule was compacted over %d passes in %d batches, want steps over passes", passes, len(reads))
	}
	for i, n := range reads {
		if n > 2*passBound {
			t.Errorf("batch %d read %d lines of the schedule, past twice the bound", i, n)
		}
	}
	if got := firedAtOf(t, d, ledgerHourly); !got.Equal(firedAt) {
		t.Errorf("the fire state is %s after the passes, want %s", got, firedAt)
	}
}

// An owner's override for `core/trigger`, written before the pass, keeps a
// trigger's whole history a week later and after.
func TestAnOverrideKeepsATriggersWholeHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	d := openInternalDataset(t, WithCompactionInterval(-1), WithTestCompactionClock(func() time.Time { return clock }))
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/retention", ID: "triggers", Properties: map[string]any{
		"appliesTo": map[string]any{"ref": "substrate.reamde.dev/core/kind/" + typeTrigger},
		"retention": "all",
	}}); err != nil {
		t.Fatalf("the owner's override: %v", err)
	}
	busySchedule(t, d, 5)
	if _, err := d.Patch(ctx, substrate.ActorPerson, typeTrigger, ledgerHourly, substrate.PatchInput{Properties: map[string]any{"label": "hourly, edited"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	triggers := []string{ledgerMirror, ledgerPager, ledgerHourly, ledgerHook}
	before := map[string][2]int{}
	for _, id := range triggers {
		all, written := triggerLines(t, d, id)
		before[id] = [2]int{all, written}
	}
	for _, days := range []int{8, 30} {
		clock = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass: %v", err)
		}
		for _, id := range triggers {
			if all, written := triggerLines(t, d, id); [2]int{all, written} != before[id] {
				t.Errorf("%d days on, %s keeps %d lines, %d as written, want all %d, %d as written", days, id, all, written, before[id][0], before[id][1])
			}
		}
	}
}
