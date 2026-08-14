package engine

// review-final #5, #6, #7: the advisory-lock composition barriers, driven from
// inside the package so the interleavings land exactly where the reviewer's
// scenarios need them. All three share ONE global lock order (the contract "lock
// ordering"): registry-dep < subject-type < record. Each test holds the lock
// that comes FIRST in that order and proves the racing transaction parks there
// without having reached for a later one — the shape a reintroduced cycle
// would break.

import (
	"context"
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
		`SELECT pg_try_advisory_xact_lock(hashtext($1)::bigint)`, key).Scan(&free); err != nil {
		t.Fatal(err)
	}
	return free
}

// review-final #5: a former id and its canonical target locked in opposite
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
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey("record|"+raceWidget+"|mmm")); err != nil {
		t.Fatal(err)
	}

	// The reviewer's list: it addresses the FORMER id first, then zzz. The
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
	csrcAuthority = "csrc.connectors.substrate.reamde.dev"
	csrcContact   = csrcAuthority + "/contact"
	csrcActor     = substrate.Actor(csrcAuthority)
	subjPerson    = "people.substrate.reamde.dev/person"
)

// installContactSource registers a minimal mapping-source connector: a contact
// record with an email, mapped onto the shipped person subject by email.
func installContactSource(t *testing.T, ds *dataset) {
	t.Helper()
	if err := enginetest.Install(context.Background(), ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "csrc", Authority: csrcAuthority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(csrcAuthority, "v1alpha1"),
			vocabulary.ActorManifest(csrcAuthority, vocabulary.AuthorityActor(csrcAuthority)),
			vocabulary.KindManifest(csrcAuthority,
				map[string]any{"singular": "contact", "plural": "contacts"},
				map[string]any{
					"properties": map[string]any{
						"email": map[string]any{"type": "email"},
						"name":  map[string]any{"type": "string"},
					},
					"edges": map[string]any{"person": map[string]any{"to": subjPerson, "required": true}},
				}),
			vocabulary.MappingManifest(csrcAuthority, "contactperson", map[string]any{
				"from": csrcContact, "to": subjPerson, "edge": "person",
				"match": []any{map[string]any{"from": "email", "to": "emails"}},
				"map": map[string]any{
					"name":   map[string]any{"path": "name"},
					"emails": map[string]any{"path": "email", "merge": "union"},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("register contact source: %v", err)
	}
}

// personOfContact reads the subject a contact record resolved to.
func personOfContact(t *testing.T, ds *dataset, contactID string) string {
	t.Helper()
	var dst string
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT dst FROM edges WHERE src = $1 AND rel = 'person'`, contactID).Scan(&dst); err != nil {
		t.Fatalf("read subject edge: %v", err)
	}
	return dst
}

// review-final #6: an effect list's subject lock and a mapping-source write's
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

	// The reviewer's effect list: patch the subject x, and put a NEW source
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
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey("subject|"+subjPerson)); err != nil {
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

// review-final #6, the live race: {patch x, put s} against an ordinary source
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

// review-final #7: an owner trigger write and connector registration must take
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
	open, _ := w2Opener(t)
	ds := open()
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, w2Manifest(false)); err != nil {
		t.Fatalf("register: %v", err)
	}

	const trigID = w2Group + "/owntrig"
	props := map[string]any{
		"enabled":  true,
		"source":   map[string]any{"record": map[string]any{"kinds": []any{w2Widget}, "ops": []any{"create", "update"}}},
		"callable": map[string]any{"kind": "core.substrate.reamde.dev/function", "id": w2Mirror},
	}

	// The barrier: hold the registry-dep lock EXCLUSIVE, as a schema batch /
	// connector registration does across its dropped-reference query.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey(registryDepKey(ds))); err != nil {
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
	if !tryLockFree(t, ds, "record|core.substrate.reamde.dev/trigger|"+trigID) {
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
