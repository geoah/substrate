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

// A restore keeps the zone its record holds, where the restoring put leaves
// it out, as it is stored and whatever this host loads: both spellings an
// earlier release admitted everywhere, and one only another host loaded
// (`Mars/Olympus_Mons` stands for `Europe/athens` on a case-insensitive
// disk, which no host here loads). It is kept as a property and as an
// object's field, whose other fields stay with it. A put that carries the
// zone is still held to an IANA name (finding 1 of step 33's second review).
func TestARestoreKeepsAZoneItsRecordHolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	site := zonePackage + "/site"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(zonePackage, 0),
		vocabulary.KindManifest(zonePackage, map[string]any{"singular": "site"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"zone": map[string]any{"type": "timezone"},
				"where": map[string]any{"type": "object", "fields": map[string]any{
					"zone":  map[string]any{"type": "timezone"},
					"label": map[string]any{"type": "string"},
				}},
			}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, zone := range []string{"Local", "", "Mars/Olympus_Mons"} {
		rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: site, Properties: map[string]any{
			"name": "harbor", "zone": "UTC", "where": map[string]any{"zone": "UTC", "label": "quay"},
		}})
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		// The values an earlier binary admitted, folded as it folded them.
		if err := ds.inTx(ctx, substrate.ActorAPI, true, func(tx *txn) error {
			if _, err := tx.fold(foldOp{Kind: foldRecord, Ref: site, ID: rec.ID, Delta: &rowDelta{
				Set: map[string]any{"zone": zone, "where": map[string]any{"zone": zone, "label": "quay"}},
			}}); err != nil {
				return err
			}
			return tx.appendChange(substrate.ActorAPI, substrate.OpPatch, rec.ID, site, nil)
		}); err != nil {
			t.Fatalf("fold an earlier binary's zone %q: %v", zone, err)
		}
		if _, err := ds.Delete(ctx, substrate.ActorAPI, site, rec.ID, substrate.DeleteInput{}); err != nil {
			t.Fatalf("tombstone: %v", err)
		}
		// The restoring put names neither the zone nor the object.
		restored, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: site, ID: rec.ID, Properties: map[string]any{"name": "pier"}})
		if err != nil {
			t.Fatalf("restore a record holding zone %q: %v", zone, err)
		}
		where, _ := restored.Properties["where"].(map[string]any)
		if restored.Properties["zone"] != zone || where["zone"] != zone || where["label"] != "quay" || restored.Properties["name"] != "pier" {
			t.Fatalf("restored with zone %q: %v, want the zone, the object and its label kept", zone, restored.Properties)
		}
		// Apart from the restore: a later put that carries the zone is a
		// write of it, and held to the rule.
		_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: site, ID: rec.ID, Properties: map[string]any{"zone": zone}})
		wantValidation(t, "a put carrying zone "+zone, err, "props.zone: expected an IANA time zone name")
	}
}
