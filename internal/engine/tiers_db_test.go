package engine

// The manager tiers: a bundle write on a mapped property is a VISIBLE pin —
// legible in propertyMeta, releasable by the null-patch — never a silent
// recompute freeze; the tier comes from what the hand IS (a function the
// registry declares, a dispatch context), never from the actor's spelling;
// the `offer:` write-kind is gone from the v1 contract and its key is refused
// at decode naming the removal; merge migrates manager rows with their tier
// and split takes exactly them back.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	tierPackage = "tiers.test.dev/tiers"
	// tierSync is a reserved hand no declaration backs: the machine tier's
	// connector half.
	tierSync = substrate.Actor("connector:sync")
	// tierFixer is the actor of a function the fixture package declares:
	// installed code, at the bundle tier because the registry declares it.
	tierFixer = substrate.Actor("function:tiers.test.dev:tiers:fixer")

	typeTierRecord  = tierPackage + "/record"
	typeTierProfile = tierPackage + "/profile"
)

// newTierDataset provisions a repository with one source type mapped onto one
// profile type and hands back the INTERNAL dataset, so tests can drive
// txn-level paths (applyEffect, decodeEffect) directly.
func newTierDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn,
		WithDataRoot(t.TempDir()),
		WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorPerson, enginetest.Manifest{
		Name: "tiers", Authority: tierPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(tierPackage, 0),
			vocabulary.FunctionManifest(tierPackage, "fixer", map[string]any{
				"description": "the hand that fixes a profile by hand",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{typeTierProfile}},
				"source":      "def main(input, host):\n    return {}\n",
			}),
			vocabulary.KindManifest(tierPackage, map[string]any{"singular": "profile"},
				map[string]any{
					"displayTemplate": "{name}",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"emails":   map[string]any{"type": "email", "repeated": true},
						"nickname": map[string]any{"type": "string"},
					},
				}),
			vocabulary.KindManifest(tierPackage, map[string]any{"singular": "record"},
				map[string]any{
					"displayTemplate": "{name}",
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"email": map[string]any{"type": "email"},
						"profile": map[string]any{
							"type": "reference", "kind": typeTierProfile,
							"required": true, "mustExist": true, "subject": true,
						},
					},
				}),
			vocabulary.MappingManifest(tierPackage, "recordprofile", map[string]any{
				"from": typeTierRecord, "to": typeTierProfile, "property": "profile",
				"match": []any{map[string]any{"from": "email", "to": "emails"}},
				"map": map[string]any{
					"name":   map[string]any{"path": "name"},
					"emails": map[string]any{"path": "email", "merge": "union"},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return ds
}

// tierSyncRecord is one connector sync of a source record.
func tierSyncRecord(t *testing.T, ds *dataset, id, name, email string) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), tierSync, substrate.PutInput{
		Kind: typeTierRecord, ID: id,
		Properties: map[string]any{"name": name, "email": email},
	})
	if err != nil {
		t.Fatalf("sync record %s: %v", id, err)
	}
	return e
}

func tierProfileOf(t *testing.T, ds *dataset, recordID string) string {
	t.Helper()
	rec, err := ds.Get(context.Background(), typeTierRecord, recordID)
	if err != nil {
		t.Fatalf("get %s: %v", recordID, err)
	}
	ids := refIDs(rec, "profile")
	if len(ids) != 1 {
		t.Fatalf("%s points at %d profiles, want 1", recordID, len(ids))
	}
	return ids[0]
}

func tierGet(t *testing.T, ds *dataset, id string) *substrate.Record {
	t.Helper()
	e, err := ds.Get(context.Background(), typeTierProfile, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return e
}

func wantMeta(t *testing.T, e *substrate.Record, property, manager string, tier substrate.Tier) {
	t.Helper()
	m, ok := e.PropertyMeta[property]
	if !ok {
		t.Fatalf("%s: no propertyMeta for %s (have %v)", e.ID, property, e.PropertyMeta)
	}
	// The manager row links the actor record of the hand (decision 0270).
	manager = vocabulary.ActorID(substrate.Actor(manager))
	if m.Manager != manager || m.Tier != tier {
		t.Fatalf("%s.%s manager = %q tier = %q, want %q at %q", e.ID, property, m.Manager, m.Tier, manager, tier)
	}
}

// A bundle actor's direct write on a mapped property is a visible pin, not a
// silent freeze: the recompute yields to it exactly as it yields to the owner,
// propertyMeta says which tier holds the property, the fresher source value
// stays readable as an alternative, and the owner's null-patch releases it. The
// actor's tier is what the hand is, a function the registry declares, never
// its spelling.
func TestExtensionWriteIsAVisiblePin(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	tierSyncRecord(t, ds, fid("rec-1"), "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, fid("rec-1"))
	p := tierGet(t, ds, pid)
	if p.Properties["name"] != "Synced Name" {
		t.Fatalf("recompute did not fill name: %v", p.Properties["name"])
	}
	// Recompute's rows are the machine's hand, whatever actor they credit.
	wantMeta(t, p, "name", string(tierSync), substrate.TierMachine)

	// The bundle actor overwrites the mapped property directly.
	if _, err := ds.Patch(ctx, tierFixer, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Fixer Name"},
	}); err != nil {
		t.Fatalf("bundle patch: %v", err)
	}
	p = tierGet(t, ds, pid)
	wantMeta(t, p, "name", string(tierFixer), substrate.TierBundle)

	// A fresher sync yields to the pin — visibly, as an alternative.
	tierSyncRecord(t, ds, fid("rec-1"), "Fresher Name", "ada@example.com")
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fixer Name" {
		t.Fatalf("the sync overwrote a bundle pin: %v", p.Properties["name"])
	}
	wantMeta(t, p, "name", string(tierFixer), substrate.TierBundle)
	alts := p.PropertyMeta["name"].Alternatives
	if len(alts) != 1 || alts[0].Actor != vocabulary.ActorID(tierSync) || alts[0].Value != "Fresher Name" {
		t.Fatalf("the yielded source value is not an alternative: %+v", alts)
	}

	// Release stays the null-patch: the property refills from live sources
	// on the spot, back at the machine tier.
	if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fresher Name" {
		t.Fatalf("release did not refill from the live source: %v", p.Properties["name"])
	}
	wantMeta(t, p, "name", string(tierSync), substrate.TierMachine)
}

// The tier is what the hand is, never the actor's spelling (ticket 002): a
// name that only LOOKS like a function's holds where a person stands, the
// owner tier, because no declaration backs it. The declared function above
// is the other half of the assertion, pinned here side by side. The one thing
// the name decides is RESERVED or not, and that is the next test.
func TestTierIsActorDataNotSpelling(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	tierSyncRecord(t, ds, fid("rec-1"), "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, fid("rec-1"))

	// A stranger's actor SPELLED like a function is still a stranger: no
	// function document backs it, so it holds at the owner tier.
	ghost := substrate.Actor("function.ghost." + tierPackage)
	if _, err := ds.Patch(ctx, ghost, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Ghost Name"},
	}); err != nil {
		t.Fatalf("ghost patch: %v", err)
	}
	p := tierGet(t, ds, pid)
	wantMeta(t, p, "name", string(ghost), substrate.TierOwner)

	// Release, then the declared function writes: bundle, because the
	// registry declares it.
	if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := ds.Patch(ctx, tierFixer, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Fixer Name"},
	}); err != nil {
		t.Fatalf("fixer patch: %v", err)
	}
	p = tierGet(t, ds, pid)
	wantMeta(t, p, "name", string(tierFixer), substrate.TierBundle)

	// And the reserved connector hand stays machine: its direct write is
	// recompute's to replace.
	if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := ds.Patch(ctx, tierSync, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Hand Sync"},
	}); err != nil {
		t.Fatalf("sync patch: %v", err)
	}
	tierSyncRecord(t, ds, fid("rec-1"), "Fresher Name", "ada@example.com")
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fresher Name" {
		t.Fatalf("a machine-tier hold blocked recompute: %v", p.Properties["name"])
	}
}

// An undeclared RESERVED actor is the one undeclared name that does not hold
// like the owner (#102). `bundle:`, `function:`, `agent:`, the retired
// `connector:` spelling and the `substrate` namespace are the substrate's own
// writing hands, refused at the door, so an undeclared one is a hand whose
// declaration is gone or a facility like `substrate.oauth` — never the
// owner's edit. It holds at the machine tier, and recompute replaces it. The
// retired spelling is the case that matters after record 0025: every entry
// written before it carries one.
func TestUndeclaredReservedActorHoldsAtTheMachineTier(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	tierSyncRecord(t, ds, fid("rec-1"), "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, fid("rec-1"))

	// No authority declares this hand: nothing in the registry answers for
	// it, and the name is reserved — the spelling actors carried before
	// record 0025, which no repository can rewrite.
	gone := substrate.Actor("connector:gone")
	if _, err := ds.Patch(ctx, gone, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Gone Name"},
	}); err != nil {
		t.Fatalf("undeclared reserved-actor patch: %v", err)
	}
	p := tierGet(t, ds, pid)
	wantMeta(t, p, "name", string(gone), substrate.TierMachine)

	tierSyncRecord(t, ds, fid("rec-1"), "Fresher Name", "ada@example.com")
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fresher Name" {
		t.Fatalf("an undeclared reserved actor pinned a mapped property against recompute: %v", p.Properties["name"])
	}
}

// Ticket 002: the `offer:` key on a patch effect left the v1
// contract. Its presence — true, false or garbage — is refused at decode
// with an error naming the removal and the replacement pattern; a plain
// patch stays a plain patch.
func TestOfferRemovedAtDecode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	fn := &vocabulary.Function{
		Name: "fixer", Package: tierPackage, Description: "test",
		Caps: vocabulary.FunctionCaps{Emit: []string{typeTierProfile, typeTierRecord}},
	}

	for _, offer := range []any{true, false, "yes"} {
		_, err := ds.decodeEffect(fn, map[string]any{
			"action": "patch", "kind": typeTierProfile, "id": "x",
			"offer": offer, "properties": map[string]any{"name": "v"},
		})
		if err == nil || !strings.Contains(err.Error(), "offer was removed in v1") ||
			!strings.Contains(err.Error(), "source type + recordmapping") {
			t.Fatalf("offer=%v: err = %v, want the removal error naming the mapping pattern", offer, err)
		}
	}

	// An ordinary patch still decodes.
	ef, err := ds.decodeEffect(fn, map[string]any{
		"action": "patch", "kind": typeTierProfile, "id": "x",
		"properties": map[string]any{"nickname": "v"},
	})
	if err != nil {
		t.Fatalf("plain patch decode = %+v, %v", ef, err)
	}
}

// Merge migrates manager rows with their tier — a bundle pin on the
// loser is a bundle pin on the winner, and the winner's recompute yields
// to it — and split takes exactly the migrated rows back.
func TestMergeCarriesTier(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	// The winner is a bare profile with no manager rows at all.
	winner, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeTierProfile})
	if err != nil {
		t.Fatalf("put winner: %v", err)
	}
	// The loser is a synced profile whose name the bundle actor pinned.
	tierSyncRecord(t, ds, fid("rec-2"), "Synced Name", "bob@example.com")
	loserID := tierProfileOf(t, ds, fid("rec-2"))
	if _, err := ds.Patch(ctx, tierFixer, typeTierProfile, loserID, substrate.PatchInput{
		Properties: map[string]any{"name": "Held Name"},
	}); err != nil {
		t.Fatalf("bundle pin: %v", err)
	}

	rec, err := ds.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: typeTierProfile, Winner: winner.ID, Loser: loserID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The pin migrated tier and all, and the winner's own recompute — its
	// new source moved over in the same merge — yielded to it.
	w := tierGet(t, ds, winner.ID)
	wantMeta(t, w, "name", string(tierFixer), substrate.TierBundle)
	if w.Properties["name"] != nil {
		t.Fatalf("recompute overwrote a migrated bundle pin: %v", w.Properties["name"])
	}

	// Split takes exactly the migrated rows back.
	if _, err := ds.Split(ctx, substrate.ActorPerson, substrate.SplitInput{Merge: rec.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	w = tierGet(t, ds, winner.ID)
	if _, held := w.PropertyMeta["name"]; held {
		t.Fatalf("split left the migrated manager row on the winner: %+v", w.PropertyMeta)
	}
	l := tierGet(t, ds, loserID)
	wantMeta(t, l, "name", string(tierFixer), substrate.TierBundle)
	if l.Properties["name"] != "Held Name" {
		t.Fatalf("the restored loser lost its held value: %v", l.Properties["name"])
	}
}
