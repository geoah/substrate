package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// A vocabulary apply that removes a mapping derives the target kind's offers
// again INSIDE its transaction, against the candidate, so the table its commit
// publishes is already the one a rebuild derives. The post-commit recompute
// (recomputeTargets) would remove the same rows a moment later, which is why
// this looks at the table through beforePublish: after the commit, before the
// hook. A crash in that window must leave nothing for a rebuild to disagree
// with.
func TestApplyPublishesTheOffersOfAChangedMapping(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	installContactSource(t, ds)

	sam, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: subjPerson, Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The contact matches Sam by email and yields on the owner's name: an offer.
	if _, err := ds.Put(ctx, csrcActor, substrate.PutInput{
		Kind: csrcContact, ID: "c-sam", Properties: map[string]any{"email": "sam@acme.com", "name": "Samuel"},
	}); err != nil {
		t.Fatal(err)
	}
	offers := func() int {
		var n int
		if err := ds.db.QueryRowContext(ctx,
			`SELECT count(*) FROM property_offers WHERE record_kind = $1 AND record_id = $2`,
			sam.Kind, sam.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if offers() == 0 {
		t.Fatal("the contact offers Sam nothing, so the test would prove nothing")
	}

	atPublish := -1
	ds.mu.Lock()
	ds.beforePublish = func(*txn) { atPublish = offers() }
	ds.mu.Unlock()
	defer func() {
		ds.mu.Lock()
		ds.beforePublish = nil
		ds.mu.Unlock()
	}()
	// The people closure again, with no mapping: the apply prunes contactperson.
	if err := enginetest.DeclareMappings(ctx, ds); err != nil {
		t.Fatalf("apply the people closure without the contact mapping: %v", err)
	}
	if atPublish != 0 {
		t.Fatalf("%d offer rows published for a mapping the same commit removed", atPublish)
	}
	if n := offers(); n != 0 {
		t.Fatalf("%d offer rows after the apply", n)
	}
}
