package engine_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The marks name the source records behind a value (decisions 0094, 0284):
// every alternative is keyed by the source record it was read from, so a
// reader sees WHICH mirror said what when eight of them share one writer; a
// value sync wrote names the records that back it; and a person's pin names
// none, because nothing stands behind a hand edit but the hand.

type altTuple struct{ value, source string }

func altsOf(e *substrate.Record, property string) []altTuple {
	var out []altTuple
	for _, a := range e.Marks[property].Alternatives {
		v, _ := a.Value.(string)
		out = append(out, altTuple{v, altFrom(a)})
	}
	return out
}

func TestTheMarksNameTheSourceRecordsBehindEachOfferAndEachValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	// The owner writes the person first, so `name` and `emails` are pinned
	// when the two mirrors arrive.
	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, fid("g-sam"), map[string]any{
		"name":   map[string]any{"displayName": "Samuel Jones"},
		"emails": gemails("sam@acme.com", "sam@home.example"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, fid("s-sam"), map[string]any{
		"realName": "Sam Jones", "displayName": "sam", "email": "sam@acme.com",
	})
	gPath := vocabulary.RecordPath(typeGoogleContact, g.ID)
	sPath := vocabulary.RecordPath(typeSlackUser, s.ID)

	got := mustGet(t, ds, sam.Kind, sam.ID)
	name := got.Marks["name"]
	if markSetBy(name) != vocabulary.ActorID(owner) || !name.Pinned || len(name.DerivedFrom) != 0 {
		t.Fatalf("an owner's pin names a source: %+v", name)
	}
	// One alternative per source record, the latest first.
	want := []altTuple{{"Sam Jones", sPath}, {"Samuel Jones", gPath}}
	if alts := altsOf(got, "name"); !slices.Equal(alts, want) {
		t.Fatalf("pinned name alternatives = %v, want %v", alts, want)
	}
	// displayName is slack's alone and nothing pinned it: derived from the
	// record the value was read from, set by no hand.
	dn := got.Marks["displayName"]
	if from := markFrom(dn); dn.Pinned || dn.SetBy != nil || !slices.Equal(from, []string{sPath}) {
		t.Fatalf("displayName = %+v, want derived from %s", dn, sPath)
	}

	// Release: the latest source wins and the mark names its record, and the
	// other mirror's value is the one alternative left, still naming its own.
	if _, err := ds.Release(ctx, owner, sam.Kind, sam.ID, substrate.MarkInput{Properties: []string{"name", "emails"}}); err != nil {
		t.Fatalf("release: %v", err)
	}
	got = mustGet(t, ds, sam.Kind, sam.ID)
	name = got.Marks["name"]
	if from := markFrom(name); name.Pinned || name.SetBy != nil || !slices.Equal(from, []string{sPath}) {
		t.Fatalf("released name = %+v, want derived from %s", name, sPath)
	}
	if alts := altsOf(got, "name"); !slices.Equal(alts, []altTuple{{"Samuel Jones", gPath}}) {
		t.Fatalf("released name alternatives = %v, want google's", alts)
	}
	// A union: the stored value is the union across both mirrors, the mark
	// names every contributing record, and each one's offer — a subset of
	// the stored items — backs the value rather than competing with it.
	emails := got.Marks["emails"]
	wantFrom := []string{gPath, sPath}
	slices.Sort(wantFrom)
	if from := markFrom(emails); emails.Pinned || !slices.Equal(from, wantFrom) {
		t.Fatalf("released emails = %+v, want derived from %v", emails, wantFrom)
	}
	if n := len(emails.Alternatives); n != 0 {
		t.Fatalf("a union's own contributors are listed as alternatives: %+v", emails.Alternatives)
	}
	stored, _ := got.Properties["emails"].([]any)
	if len(stored) != 2 {
		t.Fatalf("emails = %v, want the union of both mirrors", got.Properties["emails"])
	}
	_ = strings.Compare
}
