package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// `source` on propertyMeta (record 0094): every alternative names the source
// record it was read from, and a machine-held manager names the record its
// value came from, so a reader sees WHICH mirror said what when eight of them
// share one actor — and an owner's hold names none, because nothing stands
// behind a hand edit but the hand.

type altTuple struct{ actor, value, source string }

func altsOf(e *substrate.Record, property string) []altTuple {
	var out []altTuple
	for _, a := range e.PropertyMeta[property].Alternatives {
		v, _ := a.Value.(string)
		out = append(out, altTuple{a.Actor, v, a.Source})
	}
	return out
}

func TestPropertyMetaNamesTheSourceRecordBehindEachOfferAndTheManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	// The owner writes the person first, so `name` and `emails` are held at
	// the owner tier when the two mirrors arrive.
	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name":   map[string]any{"displayName": "Samuel Jones"},
		"emails": gemails("sam@acme.com", "sam@home.example"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Sam Jones", "displayName": "sam", "email": "sam@acme.com",
	})
	gPath := vocabulary.RecordPath(typeGoogleContact, g.ID)
	sPath := vocabulary.RecordPath(typeSlackUser, s.ID)

	got := mustGet(t, ds, sam.Kind, sam.ID)
	name := got.PropertyMeta["name"]
	if name.Manager != string(owner) || name.Tier != substrate.TierOwner || name.Source != "" {
		t.Fatalf("an owner hold names a source: %+v", name)
	}
	want := []altTuple{
		{string(people), "Samuel Jones", gPath},
		{string(slack), "Sam Jones", sPath},
	}
	if alts := altsOf(got, "name"); len(alts) != 2 || alts[0] != want[0] || alts[1] != want[1] {
		t.Fatalf("held name alternatives = %v, want %v", alts, want)
	}
	// displayName is slack's alone and nothing held it: machine-held, and
	// the manager names the record the value was read from.
	dn := got.PropertyMeta["displayName"]
	if dn.Manager != string(slack) || dn.Tier != substrate.TierMachine || dn.Source != sPath {
		t.Fatalf("displayName = %+v, want slack's %s at machine", dn, sPath)
	}

	// Release: the latest source wins, the manager names its record, and
	// the other mirror's value is the one alternative left, still naming its
	// own record.
	if _, err := ds.Patch(ctx, owner, sam.Kind, sam.ID, substrate.PatchInput{
		Properties: map[string]any{"name": nil, "emails": nil},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	got = mustGet(t, ds, sam.Kind, sam.ID)
	name = got.PropertyMeta["name"]
	if name.Manager != string(slack) || name.Tier != substrate.TierMachine || name.Source != sPath {
		t.Fatalf("released name = %+v, want slack's %s at machine", name, sPath)
	}
	if alts := altsOf(got, "name"); len(alts) != 1 || alts[0] != want[0] {
		t.Fatalf("released name alternatives = %v, want %v", alts, want[:1])
	}
	// A union: the stored value is the union across both mirrors, the
	// manager is the latest contributing one, and its offer — a strict subset
	// of the stored items — backs the value rather than competing with it.
	emails := got.PropertyMeta["emails"]
	if emails.Manager != string(slack) || emails.Tier != substrate.TierMachine || emails.Source != sPath {
		t.Fatalf("released emails = %+v, want slack's %s at machine", emails, sPath)
	}
	if n := len(emails.Alternatives); n != 0 {
		t.Fatalf("a union's own contributors are listed as alternatives: %+v", emails.Alternatives)
	}
	stored, _ := got.Properties["emails"].([]any)
	if len(stored) != 2 {
		t.Fatalf("emails = %v, want the union of both mirrors", got.Properties["emails"])
	}
}
