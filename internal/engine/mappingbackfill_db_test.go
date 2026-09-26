package engine_test

// A MAPPING REACHES THE SOURCES THAT ALREADY EXIST (#584). A mapping projects
// on the source's own write, and admitting one is not a write to its sources,
// so a mirror synced before the mapping landed kept an empty slot until the
// provider happened to write that row again. The apply that admits a mapping,
// changes one or re-applies one now links every live source whose slot names
// no live record, as ordinary record writes in the apply's transaction.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// cardSubject reads the person a card's synthesized slot names, "" when it
// names none.
func cardSubject(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	full := mustGet(t, ds, typeSlotlessCard, id)
	kind, pid, ok := vocabulary.SplitRecordPath(refPathValue(full, "person"))
	if !ok {
		return ""
	}
	if kind != typePerson {
		t.Fatalf("card %s names a %s, want a person", id, kind)
	}
	return pid
}

// Sources synced BEFORE their mapping exists are linked by the apply that
// admits it: a probe hit links the person it finds, a miss mints one and
// projects onto it, and a source offering nothing stays unlinked, exactly as
// its own write would have decided.
func TestInstallingAMappingLinksTheSourcesThatAlreadyExist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, slotlessManifest()); err != nil {
		t.Fatalf("install the card mirror: %v", err)
	}
	// The people closure alone first, so a person exists for a probe to find.
	if err := enginetest.DeclareMappings(ctx, ds); err != nil {
		t.Fatalf("install the people sample: %v", err)
	}
	grace := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Grace Hopper", "emails": []any{"grace@example.com"}},
	})

	syncSource(t, ds, people, typeSlotlessCard, "card-ada", map[string]any{
		"fullName": "Ada Lovelace", "email": "ada@example.com",
	})
	syncSource(t, ds, people, typeSlotlessCard, "card-grace", map[string]any{
		"fullName": "Grace B. Hopper", "email": "grace@example.com",
	})
	syncSource(t, ds, people, typeSlotlessCard, "card-empty", map[string]any{})
	if n := len(livePersons(t, ds)); n != 1 {
		t.Fatalf("%d live persons before the mapping, want Grace alone", n)
	}

	if err := enginetest.DeclareMappings(ctx, ds, slotlessMapping()); err != nil {
		t.Fatalf("declare the card mapping: %v", err)
	}

	if got := cardSubject(t, ds, "card-grace"); got != grace.ID {
		t.Fatalf("card-grace links %q, want the Grace its probe finds (%s)", got, grace.ID)
	}
	adaID := cardSubject(t, ds, "card-ada")
	if adaID == "" || adaID == grace.ID {
		t.Fatalf("card-ada links %q, want a person minted for it", adaID)
	}
	if ada := mustGet(t, ds, typePerson, adaID); ada.Properties["name"] != "Ada Lovelace" {
		t.Fatalf("the minted person did not recompute from its card: %v", ada.Properties)
	}
	if got := cardSubject(t, ds, "card-empty"); got != "" {
		t.Fatalf("a card offering nothing linked %s", got)
	}
	if n := len(livePersons(t, ds)); n != 2 {
		t.Fatalf("%d live persons, want Grace and the one minted for Ada", n)
	}

	// The link is the mapping's, as on a source's own write (record 96).
	full := mustGet(t, ds, typeSlotlessCard, "card-ada")
	if want := "mapping:" + enginetest.SamplePackage("people") + "/cardperson"; full.PropertyMeta["person"].Manager != want {
		t.Fatalf("person manager = %q, want %q", full.PropertyMeta["person"].Manager, want)
	}

	// A second apply of the same closure finds nothing to link and mints
	// nothing.
	if err := enginetest.DeclareMappings(ctx, ds, slotlessMapping()); err != nil {
		t.Fatalf("re-apply the card mapping: %v", err)
	}
	if n := len(livePersons(t, ds)); n != 2 {
		t.Fatalf("%d live persons after a re-apply, want 2", n)
	}
	if got := cardSubject(t, ds, "card-ada"); got != adaID {
		t.Fatalf("a re-apply moved card-ada from %s to %s", adaID, got)
	}
}

// RE-APPLYING AN UNCHANGED MAPPING IS THE REPROJECT DOOR. A source parked on
// an ambiguous probe stays unlinked after the owner settles the ambiguity,
// because settling it writes the candidates and not the source. Applying the
// mapping again links it.
func TestReapplyingAMappingLinksASourceItParked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{slotlessManifest()}, []map[string]any{slotlessMapping()})
	const addr = "family@example.com"
	a, b := twoAlexes(t, ds, addr)

	syncSource(t, ds, people, typeSlotlessCard, "card-fam", map[string]any{
		"fullName": "The Family", "email": addr,
	})
	if got := cardSubject(t, ds, "card-fam"); got != "" {
		t.Fatalf("an ambiguous probe under park linked %s", got)
	}

	if _, err := ds.Delete(ctx, owner, typePerson, b.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete one of the two candidates: %v", err)
	}
	if got := cardSubject(t, ds, "card-fam"); got != "" {
		t.Fatalf("settling the ambiguity linked the source on its own: %s", got)
	}

	if err := enginetest.DeclareMappings(ctx, ds, slotlessMapping()); err != nil {
		t.Fatalf("re-apply the card mapping: %v", err)
	}
	if got := cardSubject(t, ds, "card-fam"); got != a.ID {
		t.Fatalf("card-fam links %q after the re-apply, want the remaining candidate %s", got, a.ID)
	}
}
