package engine

// The advisory-lock composition barriers, driven from inside the package so
// the interleavings land exactly where they must. Every write path shares ONE
// global lock order: registry-dep < subject-kind < record. Each test holds the
// lock that comes FIRST in that order and proves the racing transaction parks
// there without having reached for a later one, which is the shape a
// reintroduced cycle would break. The last two hold the same line for the
// GC sweep: it serializes with a put at the same id, and both take the
// changelog lock before the row.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// tryLockFree reports whether a record/subject/dep advisory key is currently
// free — a fresh transaction's non-blocking probe, rolled back immediately.
// The key is composed the way the engine composes it: per repository, so the
// probe asks about THIS dataset's lock and not another repository's.
func tryLockFree(t *testing.T, ds *dataset, key string) bool {
	key = ds.scope.lockKey(key)
	t.Helper()
	ctx := context.Background()
	probe, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Rollback() }()
	var free bool
	if err := probe.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock(`+advisoryKeySQL+`)`, key).Scan(&free); err != nil {
		t.Fatal(err)
	}
	return free
}

// A former id and its canonical target locked in opposite
// dependency positions. Pre-fix lockEffectTargets locked only the RAW
// addresses, discovering the canonical hop when the effect applied — so a
// former id `a`→`x` let one list lock {a, z} then wait for x while another
// locked x then waited for z, and Postgres aborted one. The fix folds every
// address's canonical into the id set and locks raw+canonical in one order.
//
// The barrier holds the canonical id `mmm` (which sorts BETWEEN the former id
// `aaa` and `zzz`): a list addressing the former id must now park at `mmm`
// before it ever reaches `zzz`.
func TestEffectFormerIDFoldsCanonicalIntoLockOrder(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()

	for _, id := range []string{"aaa", "mmm", "zzz"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: raceWidget, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Merge aaa INTO mmm: aaa becomes a former id of the canonical mmm.
	if err := ds.inTx(ctx, raceActor, false, func(tx *txn) error {
		return tx.applyEffect(effect{Action: effectMerge, Type: raceWidget, ID: "mmm", Loser: "aaa"})
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The barrier holds the canonical id.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("record|"+raceWidget+"|mmm")); err != nil {
		t.Fatal(err)
	}

	// The list addresses the FORMER id first, then zzz. The
	// canonical mmm must be locked before zzz, so this parks at the barrier.
	list := []effect{
		{Action: effectPatch, Type: raceWidget, ID: "aaa", Properties: map[string]any{"name": "a2"}},
		{Action: effectPatch, Type: raceWidget, ID: "zzz", Properties: map[string]any{"name": "z2"}},
	}
	done := make(chan error, 1)
	go func() {
		done <- ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			if err := tx.lockEffectTargets(list); err != nil {
				return err
			}
			for _, ef := range list {
				if err := tx.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
	}()

	time.Sleep(400 * time.Millisecond)
	// zzz must still be free: the transaction parked at the canonical mmm in
	// the global order, having NOT yet locked the larger id. Pre-fix it would
	// already hold zzz (raw list) and be waiting on mmm only inside applyEffect.
	if !tryLockFree(t, ds, "record|"+raceWidget+"|zzz") {
		t.Fatal("the list locked zzz before the canonical mmm — the former-id lock cycle is back")
	}
	select {
	case err := <-done:
		t.Fatalf("the list did not park at the canonical barrier: %v", err)
	default:
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("effect list: %v", err)
	}
}

const (
	csrcPackage = "csrc.connectors.substrate.reamde.dev/csrc"
	csrcContact = csrcPackage + "/contact"
	csrcNote    = csrcPackage + "/note"
	csrcActor   = substrate.Actor(csrcPackage)
	subjPerson  = "samples.substrate.reamde.dev/people/person"
)

// installContactSource registers a minimal mapping-source connector: a contact
// record with an email, mapped onto the shipped person subject by email.
func installContactSource(t *testing.T, ds *dataset) {
	t.Helper()
	if err := enginetest.Install(context.Background(), ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "csrc", Authority: csrcPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(csrcPackage, 1),
			vocabulary.ActorManifest(csrcPackage, vocabulary.PackageActor(csrcPackage)),
			vocabulary.KindManifest(csrcPackage,
				map[string]any{"singular": "note"},
				map[string]any{
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
						// Pinned at the SUBJECT kind, and written with a
						// contact path: the write takes the subject hop, which
						// resolves under subject|person.
						"about": map[string]any{
							"type": "reference", "kind": subjPerson, "mustExist": true,
						},
					},
				}),
			vocabulary.KindManifest(csrcPackage,
				map[string]any{"singular": "contact"},
				map[string]any{
					"properties": map[string]any{
						"email": map[string]any{"type": "email"},
						"name":  map[string]any{"type": "string"},
						"person": map[string]any{
							"type": "reference", "kind": subjPerson,
							"required": true, "mustExist": true, "subject": true,
						},
					},
				}),
		},
	}); err != nil {
		t.Fatalf("register contact source: %v", err)
	}
	// The mapping belongs to the package that owns `person` (record 49), so
	// the repository declares it, not the connector.
	if err := enginetest.DeclareMappings(context.Background(), ds,
		enginetest.PeopleMapping("contactperson", map[string]any{
			"from": csrcContact, "property": "person",
			"match": []any{map[string]any{"from": "email", "to": "emails"}},
			"map": map[string]any{
				"name":   map[string]any{"path": "name"},
				"emails": map[string]any{"path": "email", "merge": "union"},
			},
		})); err != nil {
		t.Fatalf("declare the contact mapping: %v", err)
	}
}

// personOfContact reads the subject a contact record resolved to.
func personOfContact(t *testing.T, ds *dataset, contactID string) string {
	t.Helper()
	var dst string
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT dst FROM refs WHERE src = $1 AND property = 'person' AND path = ''`, contactID).Scan(&dst); err != nil {
		t.Fatalf("read the subject: %v", err)
	}
	return dst
}

// An effect list's subject lock and a mapping-source write's
// subject lock must share ONE order. Pre-fix the effect plan took only record
// locks, so an effect prelocking target x, patching it, then putting source
// s would wait for subject|<type> while holding record|x — and a concurrent
// source write holding subject|<type> and recomputing into x closed the cycle.
// The fix puts the subject-type lock in the effect plan, ahead of every record
// lock, matching the ordinary source write.
//
// The barrier holds subject|person: an effect list touching a source must park
// there before it locks any record.
func TestEffectSubjectLockPrecedesRecordLocks(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installContactSource(t, ds)
	ctx := context.Background()

	// A subject person x, minted by a first contact.
	cx, err := ds.Put(ctx, csrcActor, substrate.PutInput{
		Kind: csrcContact, ID: "c-x", Properties: map[string]any{"email": "x@example.com", "name": "X"},
	})
	if err != nil {
		t.Fatal(err)
	}
	person := personOfContact(t, ds, cx.ID)

	// The effect list: patch the subject x, and put a NEW source
	// s. The put makes the plan take subject|person; the patch targets x.
	list := []effect{
		{Action: effectPatch, Type: subjPerson, ID: person, Properties: map[string]any{"name": "Xavier"}},
		{Action: effectPut, Type: csrcContact, ID: "c-s", Properties: map[string]any{"email": "s@example.com", "name": "S"}},
	}

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("subject|"+subjPerson)); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- ds.inTx(ctx, csrcActor, false, func(tx *txn) error {
			if err := tx.lockEffectTargets(list); err != nil {
				return err
			}
			for _, ef := range list {
				if err := tx.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
	}()

	time.Sleep(400 * time.Millisecond)
	// Neither record may be locked yet: the plan parked at subject|person, its
	// first lock. Pre-fix it would already hold record|<person> and record|c-s.
	if !tryLockFree(t, ds, "record|"+subjPerson+"|"+person) {
		t.Fatal("the effect plan locked the subject record before subject|person — the mapping cycle is back")
	}
	if !tryLockFree(t, ds, "record|"+csrcContact+"|c-s") {
		t.Fatal("the effect plan locked a source record before subject|person")
	}
	select {
	case err := <-done:
		t.Fatalf("the effect plan did not park at subject|person: %v", err)
	default:
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("effect list: %v", err)
	}

	// And the two orders compose without deadlock: the patch landed and the new
	// source resolved to a subject.
	got := personOfContact(t, ds, "c-s")
	if got == "" {
		t.Fatal("the put source never got a subject")
	}
}

// TestEffectSubjectLockCoversAReferencedSource is the same order for the OTHER
// way an effect list reaches a subject: not by writing a mapping source, but by
// REFERENCING one. A `note` whose `about` names a contact takes the subject hop
// when it applies, and that hop resolves under subject|person, so the plan has
// to take that key too, ahead of every record lock. A list holding
// record|n-1 would wait on a key an ordinary source write holds while reaching
// for that same record.
//
// The branch is new with record 49's per-mapping lock planning; before it, the
// plan took subject keys only for the effect's OWN kind.
func TestEffectSubjectLockCoversAReferencedSource(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installContactSource(t, ds)
	ctx := context.Background()

	// A contact, so the reference has something live to hop through.
	if _, err := ds.Put(ctx, csrcActor, substrate.PutInput{
		Kind: csrcContact, ID: "c-x", Properties: map[string]any{"email": "x@example.com", "name": "X"},
	}); err != nil {
		t.Fatal(err)
	}

	// The list writes NO mapping source: it writes a note that POINTS at one.
	list := []effect{{
		Action: effectPut, Type: csrcNote, ID: "n-1",
		Properties: map[string]any{
			"text":  "about X",
			"about": vocabulary.RecordPath(csrcContact, "c-x"),
		},
	}}

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("subject|"+subjPerson)); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- ds.inTx(ctx, csrcActor, false, func(tx *txn) error {
			if err := tx.lockEffectTargets(list); err != nil {
				return err
			}
			for _, ef := range list {
				if err := tx.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
	}()

	time.Sleep(400 * time.Millisecond)
	if !tryLockFree(t, ds, "record|"+csrcNote+"|n-1") {
		t.Fatal("the effect plan locked the note before subject|person: a referenced source's subject key is missing from the plan")
	}
	select {
	case err := <-done:
		t.Fatalf("the effect plan did not park at subject|person: %v", err)
	default:
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("effect list: %v", err)
	}

	// And the hop landed: the note points at the contact's subject.
	note, err := ds.Get(ctx, csrcNote, "n-1")
	if err != nil {
		t.Fatalf("get the note: %v", err)
	}
	kind, id, ok := vocabulary.SplitRecordPath(refPath(note, "about"))
	if !ok || kind != subjPerson || id != personOfContact(t, ds, "c-x") {
		t.Fatalf("note.about = %v, want the contact's person", note.Properties["about"])
	}
}

// The live race: {patch x, put s} against an ordinary source
// write resolving to x. Both now take subject|person before any record lock,
// so neither Postgres-aborts the other.
func TestEffectAndSourceWriteComposeWithoutDeadlock(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installContactSource(t, ds)
	ctx := context.Background()

	cx, err := ds.Put(ctx, csrcActor, substrate.PutInput{
		Kind: csrcContact, ID: "r-x", Properties: map[string]any{"email": "rx@example.com", "name": "RX"},
	})
	if err != nil {
		t.Fatal(err)
	}
	person := personOfContact(t, ds, cx.ID)

	list := []effect{
		{Action: effectPatch, Type: subjPerson, ID: person, Properties: map[string]any{"name": "Renamed"}},
		{Action: effectPut, Type: csrcContact, ID: "r-s", Properties: map[string]any{"email": "rs@example.com", "name": "RS"}},
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- ds.inTx(ctx, csrcActor, false, func(tx *txn) error {
			if err := tx.lockEffectTargets(list); err != nil {
				return err
			}
			for _, ef := range list {
				if err := tx.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
	}()
	go func() {
		defer wg.Done()
		// An ordinary source write that resolves to the SAME subject x by email.
		_, err := ds.Put(ctx, csrcActor, substrate.PutInput{
			Kind: csrcContact, ID: "r-o", Properties: map[string]any{"email": "rx@example.com", "name": "RO"},
		})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a transaction aborted — the subject/record orders still cross: %v", err)
		}
	}
}

// An owner trigger write and connector registration must take
// the shared registry-dep lock in the SAME position relative to the trigger's
// record lock. Pre-fix the owner write locked the trigger record first and
// asked for the shared dep lock in apply — while registration held the dep
// lock EXCLUSIVE and its default-trigger installer reached for that same record
// — so each waited on the other. The fix takes the shared dep lock BEFORE the
// record lock on owner trigger writes.
//
// The barrier holds the registry-dep lock exclusive (the registration side):
// an owner trigger write must park there before it locks the trigger record.
func TestOwnerTriggerTakesRegistryDepBeforeRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := reopenableWidgetDataset(t)
	ds := open()
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, widgetsManifest(false)); err != nil {
		t.Fatalf("register: %v", err)
	}

	const trigID = widgetsPackage + "/owntrig"
	props := map[string]any{
		"enabled":  true,
		"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetsWidget}, "ops": []any{"create", "update"}}},
		"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", widgetsMirror),
	}

	// The barrier: hold the registry-dep lock EXCLUSIVE, as a schema batch /
	// connector registration does across its dropped-reference query.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(registryDepKey(ds))); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeTrigger, ID: trigID, Properties: props,
		})
		done <- err
	}()

	time.Sleep(400 * time.Millisecond)
	// The trigger record must still be free: the owner write parked at the
	// shared dep lock, its FIRST lock. Pre-fix it would already hold the
	// record lock and be waiting on the dep lock inside apply — the exact
	// deadlock against a registration that holds the dep lock and wants the
	// record.
	if !tryLockFree(t, ds, "record|substrate.reamde.dev/core/trigger|"+trigID) {
		t.Fatal("the owner trigger write locked the trigger record before the registry-dep lock — the registration cycle is back")
	}
	select {
	case err := <-done:
		t.Fatalf("the owner trigger write did not park at the registry-dep lock: %v", err)
	default:
	}

	_ = barrier.Rollback()
	if err := <-done; err != nil {
		t.Fatalf("owner trigger write: %v", err)
	}
	if _, _, err := ds.triggerByID(ctx, trigID); err != nil {
		t.Fatalf("the owner trigger did not land after the barrier lifted: %v", err)
	}
}

func TestEffectAddressingSerializesWithMerge(t *testing.T) {
	t.Parallel()
	// An effect addressed at the loser must not resolve BEFORE a concurrent
	// merge commits, wait out the merge on the row lock, and then resurrect
	// the tombstoned loser. The write takes the per-record advisory lock
	// before resolving;
	// this test parks a merge mid-flight on a held row lock (the merge holds
	// its advisory locks by then), proves the effect queues BEHIND the
	// advisory lock instead of resolving stale, and asserts the invariant
	// the old code violated.
	ds := newRaceDataset(t)
	ctx := context.Background()
	w := racePut(t, ds, map[string]any{"name": "winner"})
	l := racePut(t, ds, map[string]any{"name": "loser"})

	// The barrier: a raw transaction holds the loser's ROW lock, so the
	// merge (advisory locks acquired first) parks inside loadRow FOR UPDATE.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("barrier: %v", err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT id FROM records WHERE id = $1 FOR UPDATE`, l.ID); err != nil {
		t.Fatalf("barrier lock: %v", err)
	}

	mergeDone := make(chan error, 1)
	go func() {
		_, err := ds.Merge(ctx, substrate.ActorAPI, substrate.MergeInput{Kind: w.Kind, Winner: w.ID, Loser: l.ID})
		mergeDone <- err
	}()
	time.Sleep(300 * time.Millisecond) // the merge now holds the advisory locks

	effectDone := make(chan error, 1)
	go func() {
		effectDone <- ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			return tx.applyEffect(effect{
				Action: effectPut, Type: raceWidget, ID: l.ID,
				Properties: map[string]any{"name": "from-effect"},
			})
		})
	}()
	time.Sleep(300 * time.Millisecond) // the effect must queue behind the advisory lock

	select {
	case err := <-effectDone:
		t.Fatalf("the effect did not serialize behind the merge: %v", err)
	default:
	}
	_ = barrier.Rollback()

	if err := <-mergeDone; err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := <-effectDone; err != nil {
		t.Fatalf("effect: %v", err)
	}

	// The invariant: the loser is tombstoned AND its former-id trail names
	// the winner — never a live loser behind a trail (the resurrection).
	var deletedAt sql.NullTime
	if err := ds.db.QueryRowContext(ctx,
		`SELECT deleted_at FROM records WHERE id = $1`, l.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("loser row: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatal("the merge's loser is live again — the effect resurrected it")
	}
	var target string
	if err := ds.db.QueryRowContext(ctx,
		`SELECT record_id FROM former_ids WHERE former_id = $1`, l.ID).Scan(&target); err != nil {
		t.Fatalf("former id: %v", err)
	}
	if target != w.ID {
		t.Fatalf("former trail points at %s, want %s", target, w.ID)
	}
	// The effect landed on the canonical winner.
	winner, err := ds.Get(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("get winner: %v", err)
	}
	if winner.Properties["name"] != "from-effect" {
		t.Fatalf("the effect's write is lost: %v", winner.Properties)
	}
}

// Before ANY effect applies, the whole list's statically
// addressed records lock in one global ascending order. The barrier holds
// the SMALLEST id and proves neither concurrent effect list has touched the
// larger one — pre-fix, the list-order patch would already hold it, and the
// two merges would deadlock across the transactions.
func TestEffectListLocksInGlobalOrder(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	a, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, ID: "aaa", Properties: map[string]any{"name": "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	z, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, ID: "zzz", Properties: map[string]any{"name": "z"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The barrier: hold the smallest id's advisory lock, so both effect
	// transactions must park at the FIRST lock of the global order.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("record|"+raceWidget+"|"+a.ID)); err != nil {
		t.Fatal(err)
	}

	apply := func(effects []effect) error {
		return ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			if err := tx.lockEffectTargets(effects); err != nil {
				return err
			}
			for _, ef := range effects {
				if err := tx.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
	}
	// One list patches z FIRST then merges the
	// pair; the other patches a first then performs the same merge.
	calleeFirst := []effect{
		{Action: effectPatch, Type: raceWidget, ID: z.ID, Properties: map[string]any{"name": "z2"}},
		{Action: effectMerge, Type: raceWidget, ID: z.ID, Loser: a.ID},
	}
	callerFirst := []effect{
		{Action: effectPatch, Type: raceWidget, ID: a.ID, Properties: map[string]any{"name": "a2"}},
		{Action: effectMerge, Type: raceWidget, ID: z.ID, Loser: a.ID},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, effects := range [][]effect{calleeFirst, callerFirst} {
		wg.Add(1)
		go func(effects []effect) {
			defer wg.Done()
			errs <- apply(effects)
		}(effects)
	}
	// Both must be parked at the barrier — and neither may hold the LARGER
	// id yet: the probe's try-lock on z succeeds only if both transactions
	// queued at a first, in the global order.
	time.Sleep(400 * time.Millisecond)
	var free bool
	probe, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("record|"+raceWidget+"|"+z.ID)).Scan(&free); err != nil {
		t.Fatal(err)
	}
	_ = probe.Rollback()
	if !free {
		t.Fatal("an effect transaction locked the larger id before the global order let it — the deadlock ordering is back")
	}
	select {
	case err := <-errs:
		t.Fatalf("an effect transaction did not park at the barrier: %v", err)
	default:
	}

	_ = barrier.Rollback()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("effect transaction: %v", err)
		}
	}
	// The pair merged exactly once; the second merge replayed as a verified
	// no-op — and no transaction was aborted by the deadlock detector.
	merged, err := ds.Get(ctx, a.Kind, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if merged.CanonicalID != z.ID {
		t.Fatalf("the merge did not land: %+v", merged)
	}
}

// Which racer the barrier lets reach the tombstone's row first.
type sweepRound int

const (
	// freeRound races the sweep and the put with no barrier.
	freeRound sweepRound = iota
	// sweepFirst parks the sweep on the row, probes its locks, queues the put
	// behind it, then releases: the sweep purges and the put creates.
	sweepFirst
	// putFirst parks the put on the row, queues the sweep behind it, then
	// releases: the put restores and the sweep skips the live row.
	putFirst
)

// The sweep and a put at the id it is purging never overlap: inTx takes the
// changelog lock before either body runs, so every round ends one of two ways
// and never between them: the put restores the tombstone and the sweep finds
// a live row, or the sweep purges and the put creates a fresh record. Neither
// side errors. The two parked shapes force one history each, so both are
// proven every run; the free rounds may land on either. The outcome set is
// the same without the lock in gcPass, because the changelog lock alone
// serializes these two; the sweepFirst probe is what fails there.
func TestGCSweepSerializesWithPutAtSameID(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	outcomes := map[string]int{}
	for i := range 12 {
		shape := sweepRound(i % 3)
		got := raceSweepAgainstPut(t, ds, fmt.Sprintf("widget-race-%d", i), shape)
		switch {
		case shape == sweepFirst && got != "purged then created":
			t.Fatalf("round %d: the sweep reached the row first and the history is %q", i, got)
		case shape == putFirst && got != "restored":
			t.Fatalf("round %d: the put reached the row first and the history is %q", i, got)
		}
		outcomes[got]++
	}
	t.Logf("outcomes: %v", outcomes)
	if outcomes["restored"] == 0 || outcomes["purged then created"] == 0 {
		t.Fatalf("both histories must occur in one run, got %v", outcomes)
	}
}

// raceSweepAgainstPut runs one round at one id and names the history it
// produced. In a parked round a barrier transaction holds the tombstone's row
// lock until the first racer is parked on it and the second is queued behind
// the first on the changelog lock. Every wait polls the server's lock tables
// under a deadline (waitUntil, waitParkedOn), because a sleep cannot show that
// a racer reached its wait, and the barrier rolls back on every exit so a
// failed assertion cannot leave a racer blocked behind it.
func raceSweepAgainstPut(t *testing.T, ds *dataset, id string, shape sweepRound) string {
	t.Helper()
	ctx := context.Background()
	put := func(name string) error {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: raceWidget, ID: id, Properties: map[string]any{"name": name},
		})
		return err
	}
	if err := put("first"); err != nil {
		t.Fatalf("%s: put: %v", id, err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, id, substrate.DeleteInput{}); err != nil {
		t.Fatalf("%s: delete: %v", id, err)
	}
	gcDone, putDone := make(chan error, 1), make(chan error, 1)
	startSweep := func() {
		go func() {
			_, err := ds.RunGC(ctx)
			gcDone <- err
		}()
	}
	startPut := func() { go func() { putDone <- put("second") }() }

	if shape == freeRound {
		startSweep()
		startPut()
	} else {
		var barrier *sql.Tx
		var barrierPID int
		var err error
		if barrier, err = ds.db.BeginTx(ctx, nil); err != nil {
			t.Fatalf("%s: barrier: %v", id, err)
		}
		defer func() { _ = barrier.Rollback() }()
		if err := barrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&barrierPID); err != nil {
			t.Fatalf("%s: barrier pid: %v", id, err)
		}
		if _, err := barrier.ExecContext(ctx,
			`SELECT id FROM records WHERE kind = $1 AND id = $2 FOR UPDATE`, raceWidget, id); err != nil {
			t.Fatalf("%s: barrier lock: %v", id, err)
		}
		if shape == sweepFirst {
			startSweep()
		} else {
			startPut()
		}
		waitUntil(t, id+": the first racer to park on the row", func() bool {
			return blockedBy(t, ds, barrierPID) >= 1
		})
		if shape == sweepFirst {
			// lockRecord precedes loadRow, so the parked sweep holds the
			// record's advisory lock, the lock an addressed write takes before
			// its own row lock (checkID, lockCanonical).
			if tryLockFree(t, ds, "record|"+eref{Kind: raceWidget, ID: id}.key()) {
				t.Fatalf("%s: the parked sweep does not hold the record's advisory lock", id)
			}
			startPut()
		} else {
			startSweep()
		}
		// The second racer queues on the changelog lock inTx takes first.
		if err := waitParkedOn(t, ds, changelogLockKey); err != nil {
			t.Fatalf("%s: the second racer did not queue behind the first: %v", id, err)
		}
		_ = barrier.Rollback()
	}
	if err := <-gcDone; err != nil {
		t.Fatalf("%s: gc: %v", id, err)
	}
	if err := <-putDone; err != nil {
		t.Fatalf("%s: put racing the sweep: %v", id, err)
	}
	return assertSweepOutcome(t, ds, id)
}

// blockedBy counts the sessions waiting on a lock the given backend holds.
func blockedBy(t *testing.T, ds *dataset, pid int) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// assertSweepOutcome checks one round against the two admissible histories
// and names the one it found: exactly one live row at the id, whose version
// and changelog entries agree on which side won.
func assertSweepOutcome(t *testing.T, ds *dataset, id string) string {
	t.Helper()
	ctx := context.Background()
	var live, total int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*)
		FROM records WHERE kind = $1 AND id = $2`, raceWidget, id).Scan(&live, &total); err != nil {
		t.Fatalf("%s: rows: %v", id, err)
	}
	if live != 1 || total != 1 {
		t.Fatalf("%s: %d live of %d rows at the id, want exactly one live", id, live, total)
	}
	var version int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT version FROM records WHERE kind = $1 AND id = $2`, raceWidget, id).Scan(&version); err != nil {
		t.Fatalf("%s: version: %v", id, err)
	}

	rows, err := ds.db.QueryContext(ctx,
		`SELECT op, payload FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq`, raceWidget, id)
	if err != nil {
		t.Fatalf("%s: changelog: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var ops []string
	var last *rowDelta
	for rows.Next() {
		var op string
		var payload []byte
		if err := rows.Scan(&op, &payload); err != nil {
			t.Fatalf("%s: scan: %v", id, err)
		}
		ops = append(ops, op)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatalf("%s: payload: %v", id, err)
		}
		var effects []foldOp
		if raw, ok := body[foldPayloadKey]; ok {
			if err := json.Unmarshal(raw, &effects); err != nil {
				t.Fatalf("%s: fold effects: %v", id, err)
			}
		}
		last = nil
		for _, ef := range effects {
			if ef.Kind == foldRecord {
				last = ef.Delta
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: changelog rows: %v", id, err)
	}

	put, del, gc := string(substrate.OpPut), string(substrate.OpDelete), string(substrate.OpGC)
	switch {
	case slices.Equal(ops, []string{put, del, put}):
		// The put won: create, delete, restore moved the same row three times.
		if version != 3 || last == nil || !last.Restored {
			t.Fatalf("%s: restore history %v with version %d and delta %+v", id, ops, version, last)
		}
		return "restored"
	case slices.Equal(ops, []string{put, del, gc, put}):
		// The sweep won: the put created a fresh row after the purge.
		if version != 1 || last == nil || !last.Created {
			t.Fatalf("%s: purge history %v with version %d and delta %+v", id, ops, version, last)
		}
		return "purged then created"
	}
	t.Fatalf("%s: history %v is neither a restore nor a purge followed by a create", id, ops)
	return ""
}

// The global lock order begins with the changelog (rows.go changelogLockKey),
// and inTx takes it before a transaction locks anything else. The sweep locks
// its victim's row FOR UPDATE and a put onto the same tombstone takes its
// record lock; each used to reach the changelog lock later, at its first
// append, so a put holding the row could wait for the changelog while the
// sweep held the changelog and waited for the row, and Postgres broke the
// cycle with 40P01. With the changelog first, both park there, having locked
// nothing, and run one after the other.

// waitParked waits until n sessions are parked on one advisory key of this
// repository, the pg_locks probe registrydelete_db_test.go waitParkedOn makes
// for one, so a probe that follows runs after the writer reached the lock and
// not before, whatever the runner's load.
func waitParked(t *testing.T, ds *dataset, name string, n int) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted AND objsubid = 1
			  AND classid::bigint = ((hashtext(current_schema() || '|' || $1)::bigint >> 32) & 4294967295)
			  AND objid::bigint = (hashtext(current_schema() || '|' || $1)::bigint & 4294967295)`,
			ds.scope.lockKey(name)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%d sessions never parked on %s within the bound", n, name)
}

func TestPutAndSweepOnOneTombstoneTakeTheChangelogFirst(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, ID: "v", Properties: map[string]any{"name": "v"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, "v", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}

	// The barrier holds the changelog, so every writer parks at its first key.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Released on every exit, so a failing assertion below frees the parked
	// writers instead of hanging the pool close.
	defer func() { _ = barrier.Rollback() }()
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		t.Fatal(err)
	}

	gcDone := make(chan error, 1)
	go func() {
		_, err := ds.RunGC(ctx)
		gcDone <- err
	}()
	waitParked(t, ds, changelogLockKey, 1)
	if !rowLockFree(t, ds, raceWidget, "v") {
		t.Fatal("the sweep locked the victim's row before the changelog lock")
	}
	select {
	case err := <-gcDone:
		t.Fatalf("the sweep did not park at the changelog lock: %v", err)
	default:
	}

	putDone := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: raceWidget, ID: "v", Properties: map[string]any{"name": "back"},
		})
		putDone <- err
	}()
	waitParked(t, ds, changelogLockKey, 2)
	if !tryLockFree(t, ds, "record|"+raceWidget+"|v") {
		t.Fatal("the put locked its record before the changelog lock")
	}
	select {
	case err := <-putDone:
		t.Fatalf("the put did not park at the changelog lock: %v", err)
	default:
	}

	// Released, the two run in turn: the sweep purges and the put creates, or
	// the put restores and the sweep finds nothing to collect. Neither order
	// deadlocks, and either leaves the record live.
	_ = barrier.Rollback()
	for _, done := range []chan error{gcDone, putDone} {
		if err := <-done; err != nil {
			t.Fatalf("a writer parked behind the changelog lock failed: %v", err)
		}
	}
	rec, err := ds.Get(ctx, raceWidget, "v")
	if err != nil || rec.DeletedAt != nil {
		t.Fatalf("after both: %+v, %v", rec, err)
	}
}
