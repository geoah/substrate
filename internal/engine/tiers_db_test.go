package engine

// The manager tiers as explicit actor data (ticket 002, ruling A10; the
// original tiers landed for primitives README §6 and wave-1 review #12): an
// bundle write on a mapped property is a VISIBLE pin — legible in
// propertyMeta, releasable by the null-patch — never a silent recompute
// freeze; the tier comes from the ACTOR RECORD (a declared actor's `tier:`
// attribute, a function/agent dispatch context), never from the actor's
// spelling; the `offer:` write-kind is gone from the v1 contract and its key
// is refused at decode naming the removal; merge migrates manager rows with
// their tier and split takes exactly them back.

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
	// tierSync is an authority-declared actor with no tier attribute: the machine
	// tier's connector half, by default.
	tierSync = substrate.Actor("connector:sync")
	// tierFixer is a declared actor carrying `tier: bundle` — installed
	// code by DATA, nothing in the name says so.
	tierFixer = substrate.Actor("connector:fixer")

	typeTierRecord  = tierPackage + "/record"
	typeTierProfile = tierPackage + "/profile"
)

// actorManifestTier renders an actor document carrying the explicit tier
// attribute.
func actorManifestTier(pkg, actor, tier string) map[string]any {
	m := vocabulary.ActorManifest(pkg, actor)
	m["data"].(map[string]any)["tier"] = tier
	return m
}

// newTierDataset provisions a repository with one source type mapped onto one
// profile type and hands back the INTERNAL dataset, so tests can drive
// txn-level paths (applyEffect, decodeEffect) directly.
func newTierDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svc, err := Open(ctx, dsn,
		WithDataRoot(t.TempDir()),
		WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "tiers", Authority: tierPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(tierPackage, 0),
			vocabulary.ActorManifest(tierPackage, string(tierSync)),
			actorManifestTier(tierPackage, string(tierFixer), "bundle"),
			vocabulary.KindManifest(tierPackage, map[string]any{"singular": "profile", "plural": "profiles"},
				map[string]any{
					"displayTemplate": "{name}",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"emails":   map[string]any{"type": "email", "repeated": true},
						"nickname": map[string]any{"type": "string"},
					},
				}),
			vocabulary.KindManifest(tierPackage, map[string]any{"singular": "record", "plural": "records"},
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
	if m.Manager != manager || m.Tier != tier {
		t.Fatalf("%s.%s manager = %q tier = %q, want %q at %q", e.ID, property, m.Manager, m.Tier, manager, tier)
	}
}

// An bundle actor's direct write on a mapped property is a visible
// EXTENSION pin: the recompute yields exactly as it yields to the owner,
// propertyMeta says which tier holds it, the fresher source value stays
// readable as an alternative, and the owner's null-patch releases it — the
// wave-1 review's silent-freeze (#12), made legible. The actor's tier is its
// DECLARED attribute (`tier: bundle`), not its spelling.
func TestExtensionWriteIsAVisiblePin(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	tierSyncRecord(t, ds, "rec-1", "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, "rec-1")
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
	tierSyncRecord(t, ds, "rec-1", "Fresher Name", "ada@example.com")
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fixer Name" {
		t.Fatalf("the sync overwrote a bundle pin: %v", p.Properties["name"])
	}
	wantMeta(t, p, "name", string(tierFixer), substrate.TierBundle)
	alts := p.PropertyMeta["name"].Alternatives
	if len(alts) != 1 || alts[0].Actor != string(tierSync) || alts[0].Value != "Fresher Name" {
		t.Fatalf("the yielded source value is not an alternative: %+v", alts)
	}

	// Release stays the null-patch: the property refills from live sources
	// on the spot, back at the machine tier.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTierProfile, pid, substrate.PatchInput{
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

// The tier is DATA on the actor record, never the actor's spelling (ticket
// 002): an UNDECLARED actor a request could have asserted holds where the
// token stands, the owner tier, and renaming a declared actor cannot change
// write semantics, because nothing derives from the name. The declared
// `tier: bundle` actor above (nothing in its name says bundle) is the
// other half of the assertion, pinned here side by side. The one thing the
// name decides is RESERVED or not, and that is the next test.
func TestTierIsActorDataNotSpelling(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	tierSyncRecord(t, ds, "rec-1", "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, "rec-1")

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

	// Release, then the DECLARED bundle actor writes: bundle by data.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTierProfile, pid, substrate.PatchInput{
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

	// And the authority-declared connector actor stays machine — the default
	// attribute, not an inference: its direct write is recompute's to
	// replace.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := ds.Patch(ctx, tierSync, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Hand Sync"},
	}); err != nil {
		t.Fatalf("sync patch: %v", err)
	}
	tierSyncRecord(t, ds, "rec-1", "Fresher Name", "ada@example.com")
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

	tierSyncRecord(t, ds, "rec-1", "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, "rec-1")

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

	tierSyncRecord(t, ds, "rec-1", "Fresher Name", "ada@example.com")
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fresher Name" {
		t.Fatalf("an undeclared reserved actor pinned a mapped property against recompute: %v", p.Properties["name"])
	}
}

// The loader refuses a tier outside the closed set — the attribute is
// validated data.
func TestActorTierValidated(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "badtier", Authority: "badtier.test.dev",
		Manifests: []map[string]any{
			vocabulary.PackageManifest("badtier.test.dev/badtier", 0),
			actorManifestTier("badtier.test.dev/badtier", "connector:helper", "superuser"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "tier must be owner, bundle or machine") {
		t.Fatalf("bad tier admitted: %v", err)
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
	winner, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeTierProfile})
	if err != nil {
		t.Fatalf("put winner: %v", err)
	}
	// The loser is a synced profile whose name the bundle actor pinned.
	tierSyncRecord(t, ds, "rec-2", "Synced Name", "bob@example.com")
	loserID := tierProfileOf(t, ds, "rec-2")
	if _, err := ds.Patch(ctx, tierFixer, typeTierProfile, loserID, substrate.PatchInput{
		Properties: map[string]any{"name": "Held Name"},
	}); err != nil {
		t.Fatalf("bundle pin: %v", err)
	}

	rec, err := ds.Merge(ctx, substrate.ActorAPI, typeTierProfile, winner.ID, loserID)
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
	if _, err := ds.Split(ctx, substrate.ActorAPI, rec.ID); err != nil {
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
