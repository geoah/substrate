package engine

// The marks (decision 0284): a function's write on a mapped property is a
// VISIBLE pin — legible in the marks, released by `release` — never a silent
// recompute freeze; whether a write pins is the type of the hand that wrote
// it, stored when it is written; a value sync wrote names the source records
// behind it, and each live source's offer is an alternative of its own; the
// `offer:` write-kind is gone from the v1 contract and its key is refused at
// decode naming the removal; merge migrates marks, pinned and all, and split
// takes exactly them back.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	tierPackage = "marks.test.dev/marks"
	// tierSync is a package's hand, by the retired connector spelling: its
	// direct writes pin nothing.
	tierSync = substrate.Actor("connector:sync")
	// tierFixer is the actor of a function the fixture package declares: its
	// direct writes pin.
	tierFixer = substrate.Actor("function:marks.test.dev:marks:fixer")

	typeTierRecord  = tierPackage + "/record"
	typeTierProfile = tierPackage + "/profile"
)

// newTierDataset provisions a repository with one source type mapped onto one
// profile type and hands back the INTERNAL dataset, so tests can drive
// txn-level paths (applyEffect, decodeEffect) directly.
func newTierDataset(t *testing.T, opts ...Option) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, append([]Option{
		WithDataRoot(t.TempDir()),
		WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds),
	}, opts...)...)
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
		Name: "marks", Authority: tierPackage,
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

// wantMark holds one property's mark: who set it ("" for sync), whether it
// is pinned, and the source records behind it.
func wantMark(t *testing.T, e *substrate.Record, property string, setBy substrate.Actor, pinned bool, derivedFrom ...string) {
	t.Helper()
	m, ok := e.Marks[property]
	if !ok {
		t.Fatalf("%s: no mark for %s (have %v)", e.ID, property, e.Marks)
	}
	var by string
	if m.SetBy != nil {
		by = m.SetBy.Ref
	}
	want := ""
	if setBy != "" {
		want = vocabulary.RecordPath(kindActor, vocabulary.ActorID(setBy))
	}
	var from []string
	for _, l := range m.DerivedFrom {
		from = append(from, l.Ref)
	}
	if by != want || m.Pinned != pinned || strings.Join(from, " ") != strings.Join(derivedFrom, " ") {
		t.Fatalf("%s.%s = setBy %q pinned %v derivedFrom %v, want %q %v %v", e.ID, property, by, m.Pinned, from, want, pinned, derivedFrom)
	}
	if m.SetAt.IsZero() {
		t.Fatalf("%s.%s carries no setAt", e.ID, property)
	}
}

// tierSource is the record path of one fixture source record.
func tierSource(id string) string { return vocabulary.RecordPath(typeTierRecord, id) }

// A function's direct write on a mapped property is a visible pin, not a
// silent freeze: the recompute yields to it exactly as it yields to a person,
// the mark says it is pinned and who set it, the fresher source value stays
// readable as an alternative keyed by its source record, and a release hands
// the property back to sync, which fills it from that source.
func TestAFunctionsWriteIsAVisiblePin(t *testing.T) {
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
	// Sync's value is set by no hand, pinned by nothing, and names its source.
	wantMark(t, p, "name", "", false, tierSource(fid("rec-1")))

	// The bundle actor overwrites the mapped property directly.
	if _, err := ds.Patch(ctx, tierFixer, typeTierProfile, pid, substrate.PatchInput{
		Properties: map[string]any{"name": "Fixer Name"},
	}); err != nil {
		t.Fatalf("bundle patch: %v", err)
	}
	p = tierGet(t, ds, pid)
	wantMark(t, p, "name", tierFixer, true)

	// A fresher sync yields to the pin — visibly, as an alternative.
	tierSyncRecord(t, ds, fid("rec-1"), "Fresher Name", "ada@example.com")
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fixer Name" {
		t.Fatalf("the sync overwrote a bundle pin: %v", p.Properties["name"])
	}
	wantMark(t, p, "name", tierFixer, true)
	alts := p.Marks["name"].Alternatives
	if len(alts) != 1 || alts[0].DerivedFrom == nil || alts[0].DerivedFrom.Ref != tierSource(fid("rec-1")) || alts[0].Value != "Fresher Name" {
		t.Fatalf("the yielded source value is not an alternative: %+v", alts)
	}

	// The release hands the property back to sync, which fills it from the
	// live source on the spot.
	if _, err := ds.Release(ctx, substrate.ActorPerson, typeTierProfile, pid, substrate.MarkInput{Properties: []string{"name"}}); err != nil {
		t.Fatalf("release: %v", err)
	}
	p = tierGet(t, ds, pid)
	if p.Properties["name"] != "Fresher Name" {
		t.Fatalf("release did not refill from the live source: %v", p.Properties["name"])
	}
	wantMark(t, p, "name", "", false, tierSource(fid("rec-1")))
}

// Whether a write pins is the type of the hand that wrote it: a person's, an
// app's, a function's and an agent's pin, and so does a hand of no known type,
// which holds like a person's; a package's does not, the retired connector
// spelling included, so recompute replaces what it wrote.
func TestPinnedIsTheTypeOfTheHand(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	tierSyncRecord(t, ds, fid("rec-1"), "Synced Name", "ada@example.com")
	pid := tierProfileOf(t, ds, fid("rec-1"))

	for _, c := range []struct {
		hand   substrate.Actor
		pinned bool
	}{
		{substrate.ActorPerson, true},
		{substrate.AppActor("chat.example.com"), true},
		{tierFixer, true},
		{substrate.AgentActor("marks.test.dev", "marks", "helper"), true},
		{substrate.Actor("function.ghost." + tierPackage), true},
		{substrate.BundleActor("marks.test.dev", "marks"), false},
		{tierSync, false},
		{substrate.Actor("connector:gone"), false},
	} {
		if _, err := ds.Patch(ctx, c.hand, typeTierProfile, pid, substrate.PatchInput{
			Properties: map[string]any{"name": "By " + string(c.hand)},
		}); err != nil {
			t.Fatalf("%s writes: %v", c.hand, err)
		}
		p := tierGet(t, ds, pid)
		wantMark(t, p, "name", c.hand, c.pinned)
		// What it means: a fresher sync replaces only what nothing pinned.
		tierSyncRecord(t, ds, fid("rec-1"), "Fresher than "+string(c.hand), "ada@example.com")
		p = tierGet(t, ds, pid)
		if replaced := p.Properties["name"] == "Fresher than "+string(c.hand); replaced == c.pinned {
			t.Fatalf("%s: a sync replaced its value = %v, want %v", c.hand, replaced, !c.pinned)
		}
		if c.pinned {
			if _, err := ds.Release(ctx, substrate.ActorPerson, typeTierProfile, pid, substrate.MarkInput{Properties: []string{"name"}}); err != nil {
				t.Fatalf("release after %s: %v", c.hand, err)
			}
		}
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

// A merge moves no mark and no value: each pin stays with the value it
// protects, on the record it was pinned on (decision 0284). The winner keeps
// its own values and pins and gains none of the loser's; the loser's pinned
// values and their marks stay on its tombstone; a split returns both sides as
// they were.
func TestAMergeLeavesEachPinWithItsValue(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()

	// The winner is synced, and the person pinned its nickname.
	tierSyncRecord(t, ds, fid("rec-1"), "Winner Synced", "ann@example.com")
	winnerID := tierProfileOf(t, ds, fid("rec-1"))
	if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTierProfile, winnerID, substrate.PatchInput{
		Properties: map[string]any{"nickname": "Winnie"},
	}); err != nil {
		t.Fatalf("pin the winner's nickname: %v", err)
	}
	// The loser is synced too; a function pinned its name, the person its
	// nickname.
	tierSyncRecord(t, ds, fid("rec-2"), "Synced Name", "bob@example.com")
	loserID := tierProfileOf(t, ds, fid("rec-2"))
	if _, err := ds.Patch(ctx, tierFixer, typeTierProfile, loserID, substrate.PatchInput{
		Properties: map[string]any{"name": "Held Name"},
	}); err != nil {
		t.Fatalf("the function's pin: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, typeTierProfile, loserID, substrate.PatchInput{
		Properties: map[string]any{"nickname": "Bobby"},
	}); err != nil {
		t.Fatalf("the person's pin: %v", err)
	}
	loserHolds := func(t *testing.T) {
		t.Helper()
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			row, err := tx.loadRow(eref{Kind: typeTierProfile, ID: loserID}, false)
			if err != nil || row == nil {
				t.Fatalf("the loser's row: %v %v", row, err)
			}
			marks, err := tx.marksOf(row.ref())
			if err != nil {
				return err
			}
			if row.Props["name"] != "Held Name" || !marks["name"].Pinned || marks["name"].SetBy != actorID(tierFixer) ||
				row.Props["nickname"] != "Bobby" || !marks["nickname"].Pinned {
				t.Fatalf("the loser holds %v %+v, want its pinned values with their marks", row.Props, marks)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	rec, err := ds.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: typeTierProfile, Winner: winnerID, Loser: loserID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	w := tierGet(t, ds, winnerID)
	if w.Properties["name"] == "Held Name" || w.Properties["nickname"] != "Winnie" {
		t.Fatalf("the winner holds %v, want its own values and none of the loser's", w.Properties)
	}
	if w.Marks["name"].Pinned {
		t.Fatalf("the winner's name is pinned: %+v, want the loser's pin left on the loser", w.Marks["name"])
	}
	wantMark(t, w, "nickname", substrate.ActorPerson, true)
	for name, m := range w.Marks {
		if w.Properties[name] == nil {
			t.Fatalf("the merged winner carries a mark for %s and no value: %+v", name, m)
		}
	}
	if moved, _ := rec.Properties["moved"].(map[string]any); moved["marks"] != nil {
		t.Fatalf("the merge recorded moved marks: %v", moved["marks"])
	}
	loserHolds(t)

	if _, err := ds.Split(ctx, substrate.ActorPerson, substrate.SplitInput{Merge: rec.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	w = tierGet(t, ds, winnerID)
	if w.Properties["name"] != "Winner Synced" || w.Properties["nickname"] != "Winnie" {
		t.Fatalf("the split winner holds %v, want its own values", w.Properties)
	}
	wantMark(t, w, "name", "", false, tierSource(fid("rec-1")))
	wantMark(t, w, "nickname", substrate.ActorPerson, true)
	l := tierGet(t, ds, loserID)
	if l.Properties["name"] != "Held Name" || l.Properties["nickname"] != "Bobby" {
		t.Fatalf("the restored loser holds %v, want its pinned values", l.Properties)
	}
	wantMark(t, l, "name", tierFixer, true)
	wantMark(t, l, "nickname", substrate.ActorPerson, true)
}

// A mark a merge restates stands only beside its value: a merge recorded
// before marks restated the loser's holds onto a winner that held no value
// for the property, and replaying it drops that hold; a hold beside a value
// is a pin on it (decision 0284).
func TestARestatedMarkStandsOnlyBesideItsValue(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	ctx := context.Background()
	p, err := ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{Kind: typeTierProfile, Properties: map[string]any{"name": "Ann"}})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		if _, err := tx.fold(foldOp{Kind: foldResync, Scope: []foldRef{{Kind: typeTierProfile, ID: p.ID}}, Rows: &foldRows{Marks: []foldMarkRow{
			{Kind: typeTierProfile, ID: p.ID, Property: "name", Pinned: true, SetBy: actorID(substrate.ActorPerson), SetAt: at},
			{Kind: typeTierProfile, ID: p.ID, Property: "nickname", Pinned: true, SetBy: actorID(substrate.ActorPerson), SetAt: at},
		}}}); err != nil {
			return err
		}
		return tx.appendChange(substrate.ActorSystem, substrate.OpPatch, p.ID, typeTierProfile, map[string]any{})
	})
	if err != nil {
		t.Fatalf("restate the marks: %v", err)
	}
	got := tierGet(t, ds, p.ID)
	if !got.Marks["name"].Pinned {
		t.Fatalf("the hold beside a value is no pin: %+v", got.Marks)
	}
	if m, held := got.Marks["nickname"]; held {
		t.Fatalf("a hold on a property with no value stands: %+v", m)
	}
}

// A pinned value names, as its sources, only the ones whose offers still back
// it: a source that now offers something else is an alternative of the
// pinned value, never one of its sources.
func TestAPinnedValueNamesOnlyTheSourcesThatStillBackIt(t *testing.T) {
	ds := newTierDataset(t)
	ctx := context.Background()
	rec := tierSyncRecord(t, ds, fid("rec-1"), "Alice", "alice@example.com")
	pid := tierProfileOf(t, ds, rec.ID)
	if _, err := ds.Pin(ctx, substrate.ActorPerson, typeTierProfile, pid, substrate.MarkInput{Properties: []string{"name"}}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	wantMark(t, tierGet(t, ds, pid), "name", "", true, tierSource(rec.ID))

	tierSyncRecord(t, ds, rec.ID, "Bob", "alice@example.com")
	p := tierGet(t, ds, pid)
	if p.Properties["name"] != "Alice" {
		t.Fatalf("the sync replaced a pinned name: %v", p.Properties["name"])
	}
	wantMark(t, p, "name", "", true)
	alts := p.Marks["name"].Alternatives
	if len(alts) != 1 || alts[0].DerivedFrom == nil || alts[0].DerivedFrom.Ref != tierSource(rec.ID) || alts[0].Value != "Bob" {
		t.Fatalf("the source's new offer is not an alternative: %+v", alts)
	}
}
