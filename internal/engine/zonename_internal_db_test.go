package engine

// A record an earlier binary wrote with `Local` as a time zone is served as
// stored, and a write that leaves the zone alone still lands. Only a write
// that carries the zone is held to an IANA name.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	zonePackage = "zone.test.dev/zone"
	zonePlace   = zonePackage + "/place"
)

func TestTimezoneHeldOnWriteAndServedAsStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(zonePackage, 0),
		vocabulary.KindManifest(zonePackage, map[string]any{"singular": "place"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"zone": map[string]any{"type": "timezone"},
			}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}

	for _, zone := range []string{"Local", ""} {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: zonePlace, Properties: map[string]any{"zone": zone},
		})
		wantValidation(t, "zone "+zone, err, "props.zone: expected an IANA time zone name")
	}

	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: zonePlace, Properties: map[string]any{"name": "home", "zone": "UTC"},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	// The value an earlier binary admitted, folded as it would have folded it.
	if err := ds.inTx(ctx, substrate.ActorAPI, true, func(tx *txn) error {
		if _, err := tx.fold(foldOp{Kind: foldRecord, Ref: zonePlace, ID: rec.ID, Delta: &rowDelta{
			Set: map[string]any{"zone": "Local"},
		}}); err != nil {
			return err
		}
		return tx.appendChange(substrate.ActorAPI, substrate.OpPatch, rec.ID, zonePlace, nil)
	}); err != nil {
		t.Fatalf("fold an earlier binary's zone: %v", err)
	}

	got, err := ds.Get(ctx, zonePlace, rec.ID)
	if err != nil || got.Properties["zone"] != "Local" {
		t.Fatalf("stored zone = %v (%v), want it served as stored", got.Properties["zone"], err)
	}
	patched, err := ds.Patch(ctx, substrate.ActorAPI, zonePlace, rec.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "cottage"},
	})
	if err != nil {
		t.Fatalf("a patch that leaves the zone alone: %v", err)
	}
	if patched.Properties["zone"] != "Local" || patched.Properties["name"] != "cottage" {
		t.Fatalf("patched = %v", patched.Properties)
	}
	_, err = ds.Patch(ctx, substrate.ActorAPI, zonePlace, rec.ID, substrate.PatchInput{
		Properties: map[string]any{"zone": "Local"},
	})
	wantValidation(t, "a patch writing Local", err, `"Local" is the server's own zone`)
	// A put is a merge of what it carries, so the record read back and put
	// again carries the zone, and is refused until the zone is named.
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: zonePlace, ID: rec.ID, Properties: patched.Properties,
	})
	wantValidation(t, "the record put back whole", err, `"Local" is the server's own zone`)
	fixed, err := ds.Patch(ctx, substrate.ActorAPI, zonePlace, rec.ID, substrate.PatchInput{
		Properties: map[string]any{"zone": "Europe/Athens"},
	})
	if err != nil || fixed.Properties["zone"] != "Europe/Athens" {
		t.Fatalf("a patch naming a zone: %v %v", fixed, err)
	}
}
