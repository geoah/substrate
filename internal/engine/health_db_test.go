package engine

// Trigger health (#879, decision 0152): an agent trigger whose every delivery
// fails for the window reads `failing` and opens its `trigger.failing/<id>`
// alert at the end of a pass; one ok delivery, dispatched or retried by hand,
// resolves it in its own settling transaction and writes the ok run the next
// read counts from; a failed retry is a park the read counts; a schedule
// trigger fails the same way and fails its sync; a rebuild replays the alert.
//
// The window is ten minutes and the read's clock is a TestClock
// (WithTestHealthClock): a test advances it past the window instead of
// sleeping, so no assertion depends on how fast a pass runs.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const healthWindow = 10 * time.Minute

// pastTheWindow is how far a test moves the read's clock to put every park
// so far outside the window, and the read's own once-a-minute bound behind
// it.
const pastTheWindow = healthWindow + 2*time.Minute

// healthAgentDataset opens a repository with the ten-minute window on a test
// clock, the crew agents on the fake model server, and one record trigger
// that runs the scribe agent on every widget created. With no turn queued
// for the scribe's model, every request answers 500 and the delivery parks.
func healthAgentDataset(t *testing.T) (*dataset, *fakeLLM, string, *TestClock) {
	t.Helper()
	clock := &TestClock{}
	ds := openInternalDataset(t, WithHealthFailingAfter(healthWindow), WithTestHealthClock(clock.Now))
	fake := provisionAgents(t, ds)
	tr, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath(kindAgent, crewPackage+"/scribe"),
		},
	})
	if err != nil {
		t.Fatalf("put the scribe trigger: %v", err)
	}
	return ds, fake, tr.ID, clock
}

func putCrewWidget(t *testing.T, ds *dataset, id string) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: id, Properties: map[string]any{"name": id},
	}); err != nil {
		t.Fatalf("put widget %s: %v", id, err)
	}
}

func healthPass(t *testing.T, ds *dataset) {
	t.Helper()
	if _, err := ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("process triggers: %v", err)
	}
}

// healthRun is one run row as the tests read it.
type healthRun struct {
	id     string
	mode   string
	at     time.Time
	reason string
}

// runsOf reads the trigger's runs in one status, oldest first.
func runsOf(t *testing.T, ds *dataset, triggerID, status string) []healthRun {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT id, props->>'mode', (props->>'finishedAt')::timestamptz, coalesce(props->>'reason', '') FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", "trigger")+` = $2
		  AND props->>'status' = $3
		ORDER BY 3`,
		typeTriggerRun, vocabulary.RecordPath(typeTrigger, triggerID), status)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []healthRun
	for rows.Next() {
		var r healthRun
		if err := rows.Scan(&r.id, &r.mode, &r.at, &r.reason); err != nil {
			t.Fatal(err)
		}
		r.at = r.at.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// failingAlertOf reads the trigger's failing alert, nil when none was ever
// written.
func failingAlertOf(t *testing.T, ds *dataset, triggerID string) *substrate.Record {
	t.Helper()
	rec, err := ds.Get(context.Background(), kindAlert, alertID(failingAlertKey(triggerID)))
	if errors.Is(err, substrate.ErrNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("the failing alert of %s: %v", triggerID, err)
	}
	return rec
}

func requireAlertState(t *testing.T, ds *dataset, triggerID, want string) *substrate.Record {
	t.Helper()
	a := failingAlertOf(t, ds, triggerID)
	if a == nil {
		t.Fatalf("no failing alert for %s, want %s", triggerID, want)
	}
	if a.Properties["state"] != want {
		t.Fatalf("the failing alert of %s is %v, want %s", triggerID, a.Properties["state"], want)
	}
	return a
}

// parkScribe parks one delivery per widget named, in one pass, and checks
// the pass read the trigger as healthy: the parks are inside the window.
func parkScribe(t *testing.T, ds *dataset, triggerID string, widgets ...string) []healthRun {
	t.Helper()
	for _, w := range widgets {
		putCrewWidget(t, ds, w)
	}
	healthPass(t, ds)
	parks := runsOf(t, ds, triggerID, runStatusParked)
	if len(parks) != len(widgets) {
		t.Fatalf("parked %d runs, want %d", len(parks), len(widgets))
	}
	if st := triggerStatusOf(t, ds, triggerID); st.Health != substrate.HealthOK || st.FailingSince != nil {
		t.Fatalf("inside the window the trigger reads %q since %v, want ok", st.Health, st.FailingSince)
	}
	if a := failingAlertOf(t, ds, triggerID); a != nil {
		t.Fatalf("inside the window a failing alert was written: %v", a.Properties)
	}
	return parks
}

// changeTxn is the transaction (the changelog's txn) of the newest entry
// that wrote one record.
func changeTxn(t *testing.T, ds *dataset, kind, id string) int64 {
	t.Helper()
	var txn int64
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT txn FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq DESC LIMIT 1`,
		kind, id).Scan(&txn); err != nil {
		t.Fatalf("the newest entry of %s/%s: %v", kind, id, err)
	}
	return txn
}

// THE ISSUE'S ASK. Every delivery of an agent trigger fails for the window:
// the trigger reads failing since its first park and the alert is open,
// written under the agent's actor. One ok delivery resolves the alert in the
// transaction that writes its ok run, and the trigger reads ok again with
// lastOkAt set; the next read past the window writes nothing.
func TestATriggerWhoseEveryDeliveryFailsForTheWindowReadsFailingAndOneOkClearsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, triggerID, clock := healthAgentDataset(t)
	parks := parkScribe(t, ds, triggerID, "w1")
	clock.Advance(pastTheWindow)
	healthPass(t, ds)

	st := triggerStatusOf(t, ds, triggerID)
	if st.Health != substrate.HealthFailing || st.FailingSince == nil || !st.FailingSince.Equal(parks[0].at) {
		t.Fatalf("after the window the trigger reads %q since %v, want failing since the first park %v", st.Health, st.FailingSince, parks[0].at)
	}
	if st.LastOkAt != nil {
		t.Fatalf("lastOkAt is %v on a trigger that never delivered", st.LastOkAt)
	}
	a := requireAlertState(t, ds, triggerID, alertStateOpen)
	if a.Properties["level"] != alertLevelError || a.Properties["key"] != "trigger.failing/"+triggerID {
		t.Fatalf("the alert is %v keyed %v, want error keyed trigger.failing/%s", a.Properties["level"], a.Properties["key"], triggerID)
	}
	if detail, _ := a.Properties["detail"].(string); detail != parks[0].reason || !strings.Contains(detail, "script exhausted") {
		t.Fatalf("the detail %q is not the park's error %q", detail, parks[0].reason)
	}
	if summary, _ := a.Properties["summary"].(string); !strings.Contains(summary, triggerID) || !strings.Contains(summary, crewPackage+"/scribe") || !strings.Contains(summary, "10m") {
		t.Fatalf("the summary %q names neither the trigger, the agent nor the window", summary)
	}
	var about []string
	for _, v := range a.Properties["about"].([]any) {
		ref, _ := v.(map[string]any)
		path, _ := ref[vocabulary.ReferenceValueKey].(string)
		about = append(about, path)
	}
	want := []string{vocabulary.RecordPath(typeTrigger, triggerID), vocabulary.RecordPath(kindAgent, crewPackage+"/scribe")}
	if strings.Join(about, " ") != strings.Join(want, " ") {
		t.Fatalf("the alert is about %v, want %v", about, want)
	}
	var actor string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT actor FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`,
		kindAlert, a.ID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if substrate.Actor(actor) != substrate.AgentActor(vocabulary.SplitKindRef(crewPackage+"/scribe")) {
		t.Fatalf("the alert was written by %q, want the scribe's actor", actor)
	}

	fake.script("sub", fakeTurn{content: "done"})
	putCrewWidget(t, ds, "w2")
	healthPass(t, ds)
	oks := runsOf(t, ds, triggerID, runStatusOK)
	if len(oks) != 1 || oks[0].mode != "record" {
		t.Fatalf("ok runs %+v, want one dispatched", oks)
	}
	resolved := requireAlertState(t, ds, triggerID, alertStateResolved)
	if got, want := changeTxn(t, ds, kindAlert, a.ID), changeTxn(t, ds, typeTriggerRun, oks[0].id); got != want {
		t.Fatalf("the resolve committed in transaction %d, the ok run in %d: want one transaction", got, want)
	}
	st = triggerStatusOf(t, ds, triggerID)
	if st.Health != substrate.HealthOK || st.FailingSince != nil || st.LastOkAt == nil || !st.LastOkAt.Equal(oks[0].at) {
		t.Fatalf("after an ok delivery the trigger reads %q since %v, last ok %v; want ok, last ok %v", st.Health, st.FailingSince, st.LastOkAt, oks[0].at)
	}
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	if again := failingAlertOf(t, ds, triggerID); again.Version != resolved.Version {
		t.Fatalf("a read after the ok delivery rewrote the alert: version %d -> %d, %v", resolved.Version, again.Version, again.Properties["state"])
	}
}

// A retry by hand that delivers writes an ok run, mode manual, and resolves
// the open alert in the same transaction. The read past the window after it
// leaves the alert resolved although the other park still stands.
func TestASuccessfulHandRetryClearsTheFailingAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, triggerID, clock := healthAgentDataset(t)
	parkScribe(t, ds, triggerID, "w1", "w2")
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	a := requireAlertState(t, ds, triggerID, alertStateOpen)
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 2 {
		t.Fatalf("parked deliveries %v (%v), want 2", failures, err)
	}

	fake.script("sub", fakeTurn{content: "done"})
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	oks := runsOf(t, ds, triggerID, runStatusOK)
	if len(oks) != 1 || oks[0].mode != "manual" {
		t.Fatalf("ok runs %+v, want the retry's, mode manual", oks)
	}
	requireAlertState(t, ds, triggerID, alertStateResolved)
	if got, want := changeTxn(t, ds, kindAlert, a.ID), changeTxn(t, ds, typeTriggerRun, oks[0].id); got != want {
		t.Fatalf("the resolve committed in transaction %d, the retry's run in %d: want one transaction", got, want)
	}
	st := triggerStatusOf(t, ds, triggerID)
	if st.Health != substrate.HealthOK || st.LastOkAt == nil || !st.LastOkAt.Equal(oks[0].at) || st.Parked != 1 {
		t.Fatalf("after the retry the trigger reads %q, last ok %v, parked %d; want ok, last ok %v, the other park standing", st.Health, st.LastOkAt, st.Parked, oks[0].at)
	}
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	requireAlertState(t, ds, triggerID, alertStateResolved)
}

// REPRODUCTION 1. Two deliveries park, one is retried by hand and delivers
// inside the window: past the window the alert does not open, because the
// retry's ok run ends the streak.
func TestAHandRetryThatDeliversInsideTheWindowKeepsTheAlertShut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, triggerID, clock := healthAgentDataset(t)
	parkScribe(t, ds, triggerID, "w1", "w2")
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 2 {
		t.Fatalf("parked deliveries %v (%v), want 2", failures, err)
	}
	fake.script("sub", fakeTurn{content: "done"})
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[1].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}

	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	if a := failingAlertOf(t, ds, triggerID); a != nil {
		t.Fatalf("a retry that delivered inside the window still let the alert open: %v", a.Properties)
	}
	if st := triggerStatusOf(t, ds, triggerID); st.Health != substrate.HealthOK || st.Parked != 1 {
		t.Fatalf("the trigger reads %q with %d parked, want ok with the other park standing", st.Health, st.Parked)
	}
}

// REPRODUCTION 3. The owner resolves the open alert by hand; a retry of an
// older park then fails again, which writes a parked run, mode manual. Past
// the window from that park the alert reopens, first seen at the retry. A
// later failure refreshes the reopened alert with this episode's two parks,
// never the earlier episode's as well.
//
// Not parallel: it sets the package's alertRepeatInterval to zero so the
// refresh is not held back five wall-clock minutes, and only a sequential
// test may write a package variable other tests read.
func TestAFailedHandRetryAfterAResolveReopensTheAlert(t *testing.T) {
	prev := alertRepeatInterval
	alertRepeatInterval = 0
	t.Cleanup(func() { alertRepeatInterval = prev })
	ctx := context.Background()
	ds, _, triggerID, clock := healthAgentDataset(t)
	parkScribe(t, ds, triggerID, "w1", "w2")
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	a := requireAlertState(t, ds, triggerID, alertStateOpen)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, kindAlert, a.ID, substrate.PatchInput{
		Properties: map[string]any{"state": alertStateResolved},
	}); err != nil {
		t.Fatalf("resolve by hand: %v", err)
	}
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	requireAlertState(t, ds, triggerID, alertStateResolved)

	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 2 {
		t.Fatalf("parked deliveries %v (%v), want 2", failures, err)
	}
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, failures[0].ID); !errors.Is(err, substrate.ErrParked) {
		t.Fatalf("the retry answered %v, want it to park again", err)
	}
	parks := runsOf(t, ds, triggerID, runStatusParked)
	retried := parks[len(parks)-1]
	if len(parks) != 3 || retried.mode != "manual" || !strings.Contains(retried.reason, "script exhausted") {
		t.Fatalf("parked runs %+v, want a third, mode manual, with the retry's error", parks)
	}
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	reopened := requireAlertState(t, ds, triggerID, alertStateOpen)
	if got := reopened.Properties["firstSeenAt"]; got != retried.at.Format(time.RFC3339Nano) {
		t.Fatalf("the reopened alert was first seen at %v, want the failed retry's park %v", got, retried.at)
	}
	if n, ok := alertCount(reopened.Properties["count"]); !ok || n != 1 {
		t.Fatalf("the reopened alert counts %v parks, want the one since the resolve", reopened.Properties["count"])
	}

	putCrewWidget(t, ds, "w3")
	clock.Advance(2 * time.Minute)
	healthPass(t, ds)
	if parks := runsOf(t, ds, triggerID, runStatusParked); len(parks) != 4 {
		t.Fatalf("parked %d runs, want the fourth from w3", len(parks))
	}
	refreshed := requireAlertState(t, ds, triggerID, alertStateOpen)
	if refreshed.Version == reopened.Version {
		t.Fatal("a failure after the repeat interval did not refresh the open alert")
	}
	if n, ok := alertCount(refreshed.Properties["count"]); !ok || n != 2 {
		t.Fatalf("the refreshed alert counts %v parks, want this episode's 2", refreshed.Properties["count"])
	}
	if got := refreshed.Properties["firstSeenAt"]; got != retried.at.Format(time.RFC3339Nano) {
		t.Fatalf("the refresh moved firstSeenAt to %v, want the episode's first park %v", got, retried.at)
	}
}

// Parks older than an ok run are not a failing streak, however old: the
// trigger reads ok with lastOkAt set, and no alert is written.
func TestAParkBeforeANewerOkRunIsNotFailing(t *testing.T) {
	t.Parallel()
	ds, fake, triggerID, clock := healthAgentDataset(t)
	parkScribe(t, ds, triggerID, "w1")
	fake.script("sub", fakeTurn{content: "done"})
	putCrewWidget(t, ds, "w2")
	healthPass(t, ds)
	oks := runsOf(t, ds, triggerID, runStatusOK)
	if len(oks) != 1 {
		t.Fatalf("%d ok runs, want 1", len(oks))
	}

	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	if a := failingAlertOf(t, ds, triggerID); a != nil {
		t.Fatalf("a park before an ok run opened a failing alert: %v", a.Properties)
	}
	st := triggerStatusOf(t, ds, triggerID)
	if st.Health != substrate.HealthOK || st.Parked != 1 || st.LastOkAt == nil || !st.LastOkAt.Equal(oks[0].at) {
		t.Fatalf("the trigger reads %q, parked %d, last ok %v; want ok, the old park standing, last ok %v", st.Health, st.Parked, st.LastOkAt, oks[0].at)
	}
}

// The failing alert is ordinary record writes through the changelog, so a
// rebuild replays it: same version, same state, same firstSeenAt.
func TestARebuildReproducesTheFailingAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, triggerID, clock := healthAgentDataset(t)
	parkScribe(t, ds, triggerID, "w1")
	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	before := requireAlertState(t, ds, triggerID, alertStateOpen)
	if _, err := ds.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after := requireAlertState(t, ds, triggerID, alertStateOpen)
	if after.Version != before.Version || after.Properties["firstSeenAt"] != before.Properties["firstSeenAt"] {
		t.Fatalf("the rebuilt alert is version %d since %v, want version %d since %v",
			after.Version, after.Properties["firstSeenAt"], before.Version, before.Properties["firstSeenAt"])
	}
	if st := triggerStatusOf(t, ds, triggerID); st.Health != substrate.HealthFailing {
		t.Fatalf("after the rebuild the trigger reads %q, want failing", st.Health)
	}
}

// A SCHEDULE trigger whose fires all park opens its alert like a record
// trigger, and the sync its function runs reads failing with it; a schedule
// trigger of another function fails on its own. A fire that settles resolves
// the alert and the sync reads ok.
func TestAFailingScheduleTriggerFailsItsSync(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &TestClock{}
	s := nowUTC().Add(-90 * time.Minute).Truncate(time.Minute)
	ds := supersedeDatasetOn(t, openInternalDataset(t, WithHealthFailingAfter(healthWindow), WithTestHealthClock(clock.Now)), s, "update")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: supersedeJob, ID: "inbox", Properties: map[string]any{"name": "inbox"},
	}); err != nil {
		t.Fatalf("put job: %v", err)
	}
	rewindSchedule(t, ds, supersedeSync, s.Add(-time.Minute))
	rewindSchedule(t, ds, supersedeTool, s.Add(-time.Minute))
	healthPass(t, ds)
	if got := parkedFires(t, ds, supersedeSync); len(got) != 2 {
		t.Fatalf("%s parked %v, want both occurrences", supersedeSync, got)
	}
	if sync, _ := jobStatus(t, ds, supersedeSync); sync.Health != substrate.HealthOK {
		t.Fatalf("inside the window the sync reads %q, want ok", sync.Health)
	}

	clock.Advance(pastTheWindow)
	healthPass(t, ds)
	requireAlertState(t, ds, supersedeSync, alertStateOpen)
	requireAlertState(t, ds, supersedeTool, alertStateOpen)
	sync, st := jobStatus(t, ds, supersedeSync)
	if st.Health != substrate.HealthFailing || sync.Health != substrate.HealthFailing || sync.FailingSince == nil || !sync.FailingSince.Equal(*st.FailingSince) {
		t.Fatalf("the schedule trigger reads %q and its sync %q since %v, want both failing since %v", st.Health, sync.Health, sync.FailingSince, st.FailingSince)
	}

	setJobSync(t, ds, supersedeOK)
	rewindSchedule(t, ds, supersedeSync, s)
	healthPass(t, ds)
	requireAlertState(t, ds, supersedeSync, alertStateResolved)
	requireAlertState(t, ds, supersedeTool, alertStateOpen)
	if sync, _ := jobStatus(t, ds, supersedeSync); sync.Health != substrate.HealthOK || sync.LastOkAt == nil {
		t.Fatalf("after a settled fire the sync reads %q, last ok %v; want ok with a last ok", sync.Health, sync.LastOkAt)
	}
}
