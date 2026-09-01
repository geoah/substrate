package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A MERGE REPOINTS NOTHING (decision 0044): a source record whose subject won or
// lost a merge keeps naming the id it was written with, and every reader of that
// pointer resolves it through the former-id trail. The subject readers are the
// ones this is dangerous for — a reader that liveness-checks the literal stored
// id sees a tombstone, calls the record unpointed, and the next sync mints a
// duplicate person over the merge the owner just made.
func TestSyncAfterASubjectMergeMintsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	// A contact with nothing shared: no probe can match it, so its subject is a
	// shell minted for it — and a re-resolution would mint a SECOND one, which
	// is what makes the assertion below sharp.
	contact := syncSource(t, ds, people, typeGoogleContact, "g-ned", map[string]any{
		"name": aname("Nameless Ned"),
	})
	shell := personOf(t, ds, contact)

	// The owner decides the shell is a person they already have.
	ada := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Ada Lovelace"},
	})
	if _, err := ds.Merge(ctx, owner, typePerson, ada.ID, shell); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The connector syncs the same contact again, carrying no subject of its
	// own — the ordinary re-sync.
	syncSource(t, ds, people, typeGoogleContact, "g-ned", map[string]any{
		"name": aname("Nameless Ned"),
	})

	live := livePersons(t, ds)
	if len(live) != 1 || live[0].ID != ada.ID {
		ids := make([]string, 0, len(live))
		for _, p := range live {
			ids = append(ids, p.ID)
		}
		t.Fatalf("live persons after the re-sync = %v, want only the merge winner %s", ids, ada.ID)
	}
	// RESOLUTION IS READ-SIDE: the stored value still spells what was written.
	if got := personOf(t, ds, contact); got != shell {
		t.Fatalf("the stored subject moved to %s; a merge repoints nothing, it should still name %s", got, shell)
	}

	// And the immutability guard has not evaporated with the merge: the subject
	// is still set, so a write moving it elsewhere is refused.
	grace := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Grace Hopper"},
	})
	_, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-ned",
		Properties: map[string]any{"name": aname("Nameless Ned"), "person": grace.ID},
	})
	wantErr(t, err, substrate.ErrGuard, "re-pointing a subject after its target was merged")

	// Re-asserting the SAME record under either spelling is not a move: the
	// guard compares canonically, so the id the connector first saw still
	// passes.
	for _, id := range []string{shell, ada.ID} {
		if _, err := ds.Put(ctx, people, substrate.PutInput{
			Kind: typeGoogleContact, ID: "g-ned",
			Properties: map[string]any{"name": aname("Nameless Ned"), "person": id},
		}); err != nil {
			t.Fatalf("re-asserting the subject as %s was refused: %v", id, err)
		}
	}
	if got := personOf(t, ds, contact); got != ada.ID {
		t.Fatalf("the last write named %s, stored %s", ada.ID, got)
	}
	if n := len(livePersons(t, ds)); n != 2 {
		t.Fatalf("%d live persons, want the winner and Grace", n)
	}
}
