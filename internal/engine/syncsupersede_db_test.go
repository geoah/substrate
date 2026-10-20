package engine

// A delivery that stamps a sync and settles ok retires the account's older
// record parks of the same callable (decision 0178, amending 0142): the
// provider page stops reading "2 runs failed and are waiting to be tried
// again" once the account synced. Another account's parks stay, another
// callable's parks stay, and a park written after the settling delivery
// started stays.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	supersedeConnect = "job-on-connect"
	supersedeMirror  = "job-mirror"
)

// futureSchedule starts the fixture's schedules an hour ahead, so no fire
// runs during a test that is about record deliveries.
func futureSchedule() time.Time { return nowUTC().Add(time.Hour).Truncate(time.Minute) }

// putJobTrigger adds a record trigger on the job kind, firing the named
// function of the job package on ops.
func putJobTrigger(t *testing.T, ds *dataset, id, function string, ops ...any) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: id,
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{supersedeJob}, "ops": ops}},
			"callable": vocabulary.RecordPath(kindFunction, supersedePkg+"/"+function),
		},
	}); err != nil {
		t.Fatalf("put trigger %s: %v", id, err)
	}
}

func putJob(t *testing.T, ds *dataset, id string) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: supersedeJob, ID: id, Properties: map[string]any{"name": id},
	}); err != nil {
		t.Fatalf("put job %s: %v", id, err)
	}
}

func renameJob(t *testing.T, ds *dataset, id, name string) {
	t.Helper()
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, supersedeJob, id, substrate.PatchInput{
		Properties: map[string]any{"name": name},
	}); err != nil {
		t.Fatalf("patch job %s: %v", id, err)
	}
}

// parksOf lists a trigger's parked deliveries by record id.
func parksOf(t *testing.T, ds *dataset, triggerID string) []string {
	t.Helper()
	failures, err := ds.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatalf("parked deliveries of %s: %v", triggerID, err)
	}
	out := []string{}
	for _, f := range failures {
		out = append(out, f.RecordID)
	}
	return out
}

// jobSyncs is every job's sync status by id, and every trigger's status by
// id.
func jobSyncs(t *testing.T, ds *dataset) (map[string]substrate.SyncStatus, map[string]substrate.TriggerStatus) {
	t.Helper()
	ctx := context.Background()
	syncs, err := ds.SyncStatuses(ctx)
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	bySync := map[string]substrate.SyncStatus{}
	for _, st := range syncs {
		bySync[st.ID] = st
	}
	triggers, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	byTrigger := map[string]substrate.TriggerStatus{}
	for _, st := range triggers {
		byTrigger[st.ID] = st
	}
	return bySync, byTrigger
}

// THE REHEARSAL'S STEP D. The account's creation and a later request both
// park; the body is fixed and the next request syncs ok: both parks retire
// in its settling transaction, the trigger and the sync read nothing parked,
// the parked-deliveries alert resolves, and the run rows keep the history.
// A rebuild reproduces the fold.
func TestASyncDeliveryThatSettlesOkRetiresTheAccountsOlderParks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := supersedeDataset(t, futureSchedule(), "create", "update")
	putJob(t, ds, "inbox")
	processOnce(t, ds)
	renameJob(t, ds, "inbox", "inbox, again")
	processOnce(t, ds)
	if got := parksOf(t, ds, supersedeRec); len(got) != 2 {
		t.Fatalf("%s parked %v, want the creation's and the request's", supersedeRec, got)
	}
	syncs, triggers := jobSyncs(t, ds)
	if syncs["inbox"].Parked != 2 || triggers[supersedeRec].Parked != 2 {
		t.Fatalf("parked: sync status %d, trigger status %d, want 2 each", syncs["inbox"].Parked, triggers[supersedeRec].Parked)
	}
	if a := alertOf(t, ds, parkedAlertKey(supersedeRec)); a == nil || a.Properties["state"] != alertStateOpen {
		t.Fatalf("the parked-deliveries alert is %v, want open", a)
	}

	setJobSync(t, ds, supersedeOK)
	renameJob(t, ds, "inbox", "inbox, synced")
	processOnce(t, ds)
	if got := parksOf(t, ds, supersedeRec); len(got) != 0 {
		t.Fatalf("%s parked %v after the account synced, want none", supersedeRec, got)
	}
	syncs, triggers = jobSyncs(t, ds)
	if st := triggers[supersedeRec]; st.Parked != 0 || st.LastParkedError != "" || st.Health != substrate.HealthOK {
		t.Fatalf("trigger status = %+v, want nothing parked and ok", st)
	}
	if st := syncs["inbox"]; st.Parked != 0 || st.State != substrate.SyncStateOK || st.Health != substrate.HealthOK {
		t.Fatalf("sync status = %+v, want nothing parked, state ok", st)
	}
	if a := alertOf(t, ds, parkedAlertKey(supersedeRec)); a == nil || a.Properties["state"] != alertStateResolved {
		t.Fatalf("the parked-deliveries alert is %v, want resolved", a)
	}
	if a := failingAlertOf(t, ds, supersedeRec); a != nil {
		t.Fatalf("a failing alert was written: %v", a.Properties)
	}
	if runs := runRowsFor(t, ds, supersedeRec, runStatusParked); runs != 2 {
		t.Fatalf("%s keeps %d parked runs, want both as history", supersedeRec, runs)
	}

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
	if got := parksOf(t, ds, supersedeRec); len(got) != 0 {
		t.Fatalf("%s parked %v after the rebuild, want none", supersedeRec, got)
	}
}

// The account's connect parked on another trigger of the same function and
// its failing alert opened. A request that syncs ok through the request
// trigger retires the connect's park, and resolves the connect trigger's
// parked and failing alerts in its own settling transaction.
func TestASyncDeliveryRetiresTheParksOfAnotherTriggerWithTheSameCallable(t *testing.T) {
	t.Parallel()
	clock := &TestClock{}
	ds := supersedeDatasetOn(t, openInternalDataset(t, WithHealthFailingAfter(healthWindow), WithTestHealthClock(clock.Now)),
		futureSchedule(), "update")
	putJobTrigger(t, ds, supersedeConnect, "jobsync", "create")
	putJob(t, ds, "inbox")
	healthPass(t, ds)
	if got := parksOf(t, ds, supersedeConnect); len(got) != 1 {
		t.Fatalf("%s parked %v, want the connect", supersedeConnect, got)
	}
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	failing := requireAlertState(t, ds, supersedeConnect, alertStateOpen)

	setJobSync(t, ds, supersedeOK)
	renameJob(t, ds, "inbox", "inbox, synced")
	healthPass(t, ds)
	if got := parksOf(t, ds, supersedeConnect); len(got) != 0 {
		t.Fatalf("%s parked %v after the request synced, want none", supersedeConnect, got)
	}
	oks := runsOf(t, ds, supersedeRec, runStatusOK)
	if len(oks) != 1 {
		t.Fatalf("%s ok runs %+v, want the request's", supersedeRec, oks)
	}
	requireAlertState(t, ds, supersedeConnect, alertStateResolved)
	parked := alertOf(t, ds, parkedAlertKey(supersedeConnect))
	if parked == nil || parked.Properties["state"] != alertStateResolved {
		t.Fatalf("the connect's parked-deliveries alert is %v, want resolved", parked)
	}
	okTxn := changeTxn(t, ds, typeTriggerRun, oks[0].id)
	if got := changeTxn(t, ds, kindAlert, failing.ID); got != okTxn {
		t.Fatalf("the failing alert resolved in transaction %d, the request's ok run in %d", got, okTxn)
	}
	if got := changeTxn(t, ds, kindAlert, parked.ID); got != okTxn {
		t.Fatalf("the parked alert resolved in transaction %d, the request's ok run in %d", got, okTxn)
	}
	if got := parksOf(t, ds, supersedeRec); len(got) != 0 {
		t.Fatalf("%s parked %v, want none", supersedeRec, got)
	}
	if st := triggerStatusOf(t, ds, supersedeConnect); st.Parked != 0 || st.Health != substrate.HealthOK {
		t.Fatalf("the connect trigger reads %q with %d parked, want ok with none", st.Health, st.Parked)
	}
}

// The retirement is the account's and the callable's: another job's parks
// stay, and a park of another function on the same job stays, although that
// function belongs to the job's package and is stamped too. (The other
// function's stamps are updates the request trigger delivers and parks as
// well, so the counts are read per record.)
func TestASyncDeliveryLeavesOtherAccountsAndOtherCallablesParksParked(t *testing.T) {
	t.Parallel()
	ds := supersedeDataset(t, futureSchedule(), "create", "update")
	putJobTrigger(t, ds, supersedeMirror, "other", "create")
	putJob(t, ds, "inbox")
	putJob(t, ds, "outbox")
	processOnce(t, ds)
	perRecord := func(triggerID string) map[string]int {
		out := map[string]int{}
		for _, id := range parksOf(t, ds, triggerID) {
			out[id]++
		}
		return out
	}
	before := perRecord(supersedeRec)
	if before["inbox"] == 0 || before["outbox"] == 0 {
		t.Fatalf("%s parked %v, want parks on inbox and outbox", supersedeRec, before)
	}
	if got := perRecord(supersedeMirror); got["inbox"] != 1 || got["outbox"] != 1 {
		t.Fatalf("%s parked %v, want one on each job", supersedeMirror, got)
	}

	setJobSync(t, ds, supersedeOK)
	renameJob(t, ds, "inbox", "inbox, synced")
	processOnce(t, ds)
	after := perRecord(supersedeRec)
	if after["inbox"] != 0 || after["outbox"] != before["outbox"] {
		t.Fatalf("%s parked %v, want none on inbox and outbox's %d", supersedeRec, after, before["outbox"])
	}
	if got := perRecord(supersedeMirror); got["inbox"] != 1 || got["outbox"] != 1 {
		t.Fatalf("%s parked %v, want both of another function's parks", supersedeMirror, got)
	}
	syncs, triggers := jobSyncs(t, ds)
	if triggers[supersedeRec].Parked != int64(before["outbox"]) || triggers[supersedeMirror].Parked != 2 {
		t.Fatalf("trigger statuses count %d and %d parked, want %d and 2", triggers[supersedeRec].Parked, triggers[supersedeMirror].Parked, before["outbox"])
	}
	if syncs["inbox"].Parked != 1 || syncs["outbox"].Parked != int64(before["outbox"])+1 {
		t.Fatalf("sync statuses count inbox %d and outbox %d parked, want 1 and %d", syncs["inbox"].Parked, syncs["outbox"].Parked, before["outbox"]+1)
	}
}

// A retry by hand that delivers a sync retires the account's other older
// parks with the one it re-ran.
func TestARetryThatDeliversASyncRetiresTheAccountsOlderParks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := supersedeDataset(t, futureSchedule(), "create", "update")
	putJob(t, ds, "inbox")
	processOnce(t, ds)
	renameJob(t, ds, "inbox", "inbox, again")
	processOnce(t, ds)
	failures, err := ds.TriggerFailures(ctx, supersedeRec)
	if err != nil || len(failures) != 2 {
		t.Fatalf("parked %+v (%v), want two", failures, err)
	}
	newer := failures[0]
	if failures[1].ID > newer.ID {
		newer = failures[1]
	}

	setJobSync(t, ds, supersedeOK)
	if _, err := ds.RetryTriggerFailure(ctx, supersedeRec, newer.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := parksOf(t, ds, supersedeRec); len(got) != 0 {
		t.Fatalf("%s parked %v after the retry synced, want none", supersedeRec, got)
	}
	oks := runsOf(t, ds, supersedeRec, runStatusOK)
	if len(oks) != 1 || oks[0].mode != "manual" {
		t.Fatalf("ok runs %+v, want the retry's, mode manual", oks)
	}
}

// A park written after the settling delivery started is not one it
// superseded, and stays.
func TestAParkNewerThanTheSettlingDeliveryStays(t *testing.T) {
	t.Parallel()
	ds := supersedeDataset(t, futureSchedule(), "create", "update")
	putJob(t, ds, "inbox")
	processOnce(t, ds)
	failures, err := ds.TriggerFailures(context.Background(), supersedeRec)
	if err != nil || len(failures) != 1 {
		t.Fatalf("parked %+v (%v), want the creation's", failures, err)
	}
	// The same change, parked an hour from now.
	if _, err := ds.db.ExecContext(context.Background(), `
		INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		VALUES ($1, $2, $3, '', 'inbox', 3, 'RuntimeError: upstream returned HTTP 500', $4)`,
		int64(-7), supersedeRec, failures[0].Seq, nowUTC().Add(time.Hour)); err != nil {
		t.Fatalf("park ahead: %v", err)
	}

	setJobSync(t, ds, supersedeOK)
	renameJob(t, ds, "inbox", "inbox, synced")
	processOnce(t, ds)
	left, err := ds.TriggerFailures(context.Background(), supersedeRec)
	if err != nil || len(left) != 1 || left[0].ID != -7 {
		t.Fatalf("%s parked %+v (%v), want the park ahead alone", supersedeRec, left, err)
	}
}
