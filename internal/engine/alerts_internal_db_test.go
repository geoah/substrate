package engine

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A settled schedule occurrence that retires its trigger's older parked fires
// (decision 0142) resolves the trigger's parked-deliveries alert in the same
// transaction, and leaves another trigger's alert open.
func TestAScheduleFireThatRetiresTheLastParkResolvesTheAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Two occurrences lie in the past: s and s+1h.
	s := nowUTC().Add(-90 * time.Minute).Truncate(time.Minute)
	ds := supersedeDataset(t, s, "update")
	rewindSchedule(t, ds, supersedeSync, s.Add(-time.Minute))
	rewindSchedule(t, ds, supersedeTool, s.Add(-time.Minute))
	processOnce(t, ds)
	if got := parkedFires(t, ds, supersedeSync); len(got) != 2 {
		t.Fatalf("%s parked %v, want both occurrences", supersedeSync, got)
	}
	alertState := func(triggerID string) any {
		t.Helper()
		rec, err := ds.Get(ctx, kindAlert, alertID(parkedAlertKey(triggerID)))
		if err != nil {
			t.Fatalf("the alert of %s: %v", triggerID, err)
		}
		return rec.Properties["state"]
	}
	if got := alertState(supersedeSync); got != alertStateOpen {
		t.Fatalf("the alert of %s is %v after two parks, want open", supersedeSync, got)
	}

	// s+1h settles and retires both parks: nothing of the trigger stands.
	setJobSync(t, ds, supersedeOK)
	rewindSchedule(t, ds, supersedeSync, s)
	processOnce(t, ds)
	if got := parkedFires(t, ds, supersedeSync); len(got) != 0 {
		t.Fatalf("%s parked %v after s+1h settled, want none", supersedeSync, got)
	}
	if got := alertState(supersedeSync); got != alertStateResolved {
		t.Fatalf("the alert of %s is %v after the retirement, want resolved", supersedeSync, got)
	}
	if got := alertState(supersedeTool); got != alertStateOpen {
		t.Fatalf("the alert of %s is %v, want its own parks to keep it open", supersedeTool, got)
	}
}

// alertID is the key itself where the key fits the record id alphabet. Any
// other key maps to an id inside the alphabet, the same id on every call, and
// the keys below, a key and its slug among them, map to distinct ids.
func TestAlertIDIsTheKeyWhereItFitsAndAHashedSlugElsewhere(t *testing.T) {
	t.Parallel()
	if got := alertID("trigger.parked/on-a.b/c"); got != "trigger.parked/on-a.b/c" {
		t.Fatalf("a key inside the alphabet became %q", got)
	}
	cases := []string{"has space", "has~tilde", "-leading", "é", string(make([]byte, 200))}
	seen := map[string]string{}
	for _, key := range cases {
		id := alertID(key)
		if !vocabulary.ValidID(id) || id == key {
			t.Fatalf("key %q mapped to %q, want a hashed id inside the record id alphabet", key, id)
		}
		if other, dup := seen[id]; dup {
			t.Fatalf("keys %q and %q share the id %q", key, other, id)
		}
		seen[id] = key
		if alertID(key) != id {
			t.Fatalf("key %q mapped to two ids", key)
		}
	}
	if alertID("has space") == alertID("has-space") {
		t.Fatal("a mapped key took the id of the key it maps to")
	}
}

// A claim no live run holds is parked work: the trigger status counts it, so
// the alert counts it too. Forgetting the trigger's other parked row leaves
// the alert open on the abandoned claim, at the count the status reads.
func TestAnAbandonedClaimKeepsTheParkedAlertOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The schedules start in an hour, so only the record trigger runs.
	ds := supersedeDataset(t, nowUTC().Add(time.Hour), "create")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: supersedeJob, ID: "inbox", Properties: map[string]any{"name": "inbox"},
	}); err != nil {
		t.Fatalf("put job: %v", err)
	}
	processOnce(t, ds)
	failures, err := ds.TriggerFailures(ctx, supersedeRec)
	if err != nil || len(failures) != 1 {
		t.Fatalf("%s parked %+v (%v), want one delivery", supersedeRec, failures, err)
	}

	// A claim an agent run left behind, which nothing in this process holds.
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		id, err := t.reserveSeq()
		if err != nil {
			return err
		}
		if err := t.parkTx(supersedeRec, foldFailure{
			ID: foldInt(id), Seq: foldInt(failures[0].Seq), LastError: inFlightError, ParkedAt: t.now,
		}); err != nil {
			return err
		}
		return t.appendDeliveryAt(supersedeRec, id)
	}); err != nil {
		t.Fatalf("plant the abandoned claim: %v", err)
	}

	if err := ds.ForgetTriggerFailure(ctx, supersedeRec, failures[0].ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	rec, err := ds.Get(ctx, kindAlert, alertID(parkedAlertKey(supersedeRec)))
	if err != nil {
		t.Fatalf("the alert of %s: %v", supersedeRec, err)
	}
	if rec.Properties["state"] != alertStateOpen {
		t.Fatalf("the alert is %v with an abandoned claim still parked, want open", rec.Properties["state"])
	}
	count, ok := alertCount(rec.Properties["count"])
	if !ok || count != 1 {
		t.Fatalf("the alert counts %v, want the abandoned claim's 1", rec.Properties["count"])
	}
	statuses, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.ID == supersedeRec && st.Parked != count {
			t.Fatalf("the status counts %d parked, the alert %d", st.Parked, count)
		}
	}
}

// An agent run a stop interrupted during its first delivery never parked, so
// no alert was raised before the stop. The next open's sweep rewrites the
// claim as an interrupted run and raises the alert in the same transaction,
// under the agent's actor; a rebuild reproduces it, and a retry that delivers
// resolves it.
func TestARestartRaisesTheAlertOfTheAgentRunItInterrupted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, restart := restartableAgentDataset(t)
	trigger := keeperWebhook(t, ds)
	stop, release := heldKeeperFire(t, ds, fake, trigger)
	stop()
	release()
	id := alertID(parkedAlertKey(trigger))
	if _, err := ds.Get(ctx, kindAlert, id); err == nil {
		t.Fatal("an alert stood before the restart, for a claim nothing parked yet")
	}

	ds = restart()

	rec, err := ds.Get(ctx, kindAlert, id)
	if err != nil {
		t.Fatalf("no alert after the restart parked the interrupted run: %v", err)
	}
	if rec.Properties["state"] != alertStateOpen || rec.Properties["detail"] != interruptedAgentError {
		t.Fatalf("the alert after the restart is %v with detail %v, want open naming the stop",
			rec.Properties["state"], rec.Properties["detail"])
	}
	if count, ok := alertCount(rec.Properties["count"]); !ok || count != 1 || triggerStatusOf(t, ds, trigger).Parked != 1 {
		t.Fatalf("the alert counts %v, want the one interrupted run the status counts", rec.Properties["count"])
	}
	keeper, err := ds.registry().ResolveAgent(crewPackage + "/keeper")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Kinds: []string{kindAlert}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Actor != substrate.Actor(keeper.Actor()) {
		t.Fatalf("the alert was written by %+v, want one entry under %s", changes, keeper.Actor())
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

	failures, err := ds.TriggerFailures(ctx, trigger)
	if err != nil || len(failures) != 1 {
		t.Fatalf("failures after the restart = %+v (%v), want the interrupted run", failures, err)
	}
	fake.script("keep", fakeTurn{content: "kept"})
	if _, err := ds.RetryTriggerFailure(ctx, trigger, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if rec, err := ds.Get(ctx, kindAlert, id); err != nil || rec.Properties["state"] != alertStateResolved {
		t.Fatalf("the alert after the retry delivered = %v (%v), want resolved", rec, err)
	}
}
