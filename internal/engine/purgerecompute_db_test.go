package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A source tombstoned before tombstones recomputed their subjects (a cascade
// from an earlier release) left its subject holding its value and offer. The
// purge is the last moment the source's subject reference is there to follow,
// so hardDelete recomputes it: the old tombstone converges when the sweep
// reaches it, with no source write and no rebuild.
func TestPurgeRecomputesTheSubjectOfAnOldTombstone(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	installContactSource(t, ds)

	sam, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: subjPerson, Properties: map[string]any{"emails": []any{"sam@acme.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	contact := eref{Kind: csrcContact, ID: "c-sam"}
	if _, err := ds.Put(ctx, csrcActor, substrate.PutInput{
		Kind: contact.Kind, ID: contact.ID, Properties: map[string]any{"email": "sam@acme.com", "name": "Samuel"},
	}); err != nil {
		t.Fatal(err)
	}
	name := func() (string, bool) {
		var v *string
		if err := ds.db.QueryRowContext(ctx, `SELECT props->>'name' FROM records WHERE kind = $1 AND id = $2`,
			sam.Kind, sam.ID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v == nil {
			return "", false
		}
		return *v, true
	}
	if got, ok := name(); !ok || got != "Samuel" {
		t.Fatalf("the contact did not fill Sam's name, so the test would prove nothing: %q %v", got, ok)
	}

	// The tombstone as the cascade wrote it before afterTombstone existed: the
	// fold effect and the entry that reports it, and no recompute.
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if _, err := t.tombstone(contact, ""); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpGC, contact.ID, contact.Kind,
			map[string]any{"reason": "owner_collected"})
	}); err != nil {
		t.Fatal(err)
	}
	if got, ok := name(); !ok || got != "Samuel" {
		t.Fatalf("the old-style tombstone recomputed on its own (%q %v); the purge has nothing to prove", got, ok)
	}

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, contact.Kind, contact.ID); err == nil {
		t.Fatal("the sweep should have purged the contact")
	}
	if got, ok := name(); ok {
		t.Fatalf("name = %q after its only source was purged, want it released", got)
	}
	var rows int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM property_offers WHERE record_kind = $1 AND record_id = $2)
		     + (SELECT count(*) FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'name')`,
		sam.Kind, sam.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("%d offer or manager rows survive the purge of their source", rows)
	}
}
