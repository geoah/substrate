package engine_test

// THE RETIRED SPELLING (record 0025). An actor is part of the hashed changelog
// preimage, so an entry written as `connector:gmail` before actors carried the
// full authority keeps that string for the life of the repository. Nothing
// translates it on read: the fold replays the actor and the tier the entry
// recorded, and the door still refuses a request that tries to claim it.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// retiredActor is exactly what AuthorityActor minted before record 0025, for
// an authority whose first label was `gmail`.
const retiredActor = substrate.Actor("connector:gmail")

func TestOldActorSpellingSurvivesRebuild(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	svc, ds, dsn := newChainDataset(t)

	task := mustPut(t, ds, retiredActor, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "written by a hand that no longer has that name"},
	})

	var stored string
	if err := rawDB(t, dsn).QueryRow(
		`SELECT actor FROM changelog ORDER BY seq DESC LIMIT 1`).Scan(&stored); err != nil {
		t.Fatalf("read the head entry's actor: %v", err)
	}
	if stored != string(retiredActor) {
		t.Fatalf("the entry stored actor %q, want %q verbatim", stored, retiredActor)
	}

	// The retired spelling is still reserved, so no request can write as it.
	if !substrate.ReservedActor(retiredActor) {
		t.Fatalf("actor %q is claimable by a request", retiredActor)
	}

	// It gains no standing from the rename: undeclared and reserved is the
	// machine tier, which is where it already stood, so recompute may still
	// replace what it wrote.
	var actor, tier string
	if err := rawDB(t, dsn).QueryRow(`
		SELECT actor, tier FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 AND property = 'name'`,
		task.Kind, task.ID).Scan(&actor, &tier); err != nil {
		t.Fatalf("read the name manager: %v", err)
	}
	if actor != string(retiredActor) || tier != string(substrate.TierMachine) {
		t.Fatalf("the manager row is %q at %q, want %q at the machine tier", actor, tier, retiredActor)
	}

	before := foldOf(t, ds)
	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(context.Background(), "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatal("the rebuilt fold does not match: a retired actor spelling did not replay as written")
	}
	if report := mustVerify(t, svc, "geoah"); !report.OK {
		t.Fatalf("the chain does not verify over the retired spelling: %+v", report.Findings)
	}
}
