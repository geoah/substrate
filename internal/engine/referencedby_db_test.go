package engine_test

// The `referencedBy` arm of the records list (referencedby.go, decision
// 0322): a list narrowed to the records a record of one other kind points
// at, or to the records none does, by conditions on the record that points.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	pointedPackage = "pointed.example.substrate.reamde.dev/pointed"
	pointedItem    = pointedPackage + "/item"
	pointedFlag    = pointedPackage + "/flag"
	pointedOther   = pointedPackage + "/other"
)

// pointedVocabulary declares an item, a flag that points at one item and
// says something about it, and a kind no flag can point at.
func pointedVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		vocabulary.PackageManifest(pointedPackage, 0),
		vocabulary.KindManifest(pointedPackage,
			map[string]any{"singular": "item"},
			map[string]any{"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"seenAt": map[string]any{"type": "datetime"},
				"n":      map[string]any{"type": "int"},
			}}),
		vocabulary.KindManifest(pointedPackage,
			map[string]any{"singular": "other"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pointedPackage,
			map[string]any{"singular": "flag"},
			map[string]any{"properties": map[string]any{
				"item":  map[string]any{"type": "reference", "kind": pointedItem},
				"items": map[string]any{"type": "reference", "kind": pointedItem, "repeated": true},
				"on":    map[string]any{"type": "bool"},
				"by":    map[string]any{"type": "string"},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install the pointed vocabulary: %v", err)
	}
}

// pointedBy lists the items under one arm, oldest name first, and answers
// their names.
func pointedBy(t *testing.T, ds substrate.Dataset, rb substrate.ReferencedBy) []string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{pointedItem}, ReferencedBy: &rb},
		OrderBy: []substrate.Order{{Property: "name"}}, First: 50,
	})
	if err != nil {
		t.Fatalf("list under %+v: %v", rb, err)
	}
	return namesOf(page)
}

func namesOf(page *substrate.Page) []string {
	out := make([]string, 0, len(page.Records))
	for _, rec := range page.Records {
		name, _ := rec.Properties["name"].(string)
		out = append(out, name)
	}
	return out
}

func no() *bool { v := false; return &v }

// The arm keeps the records a matching record points at, and with
// `exists: false` the records none points at: a record nothing points at, one
// pointed at only by records that do not meet the conditions, and one whose
// only matching pointer was deleted. It composes with the list's own
// conditions, its count and its cursor.
func TestReferencedByKeepsTheRecordsAMatchingRecordPointsAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	pointedVocabulary(t, ds)
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: pointedItem, ID: fid("i-" + name), Properties: map[string]any{"name": name, "n": i}})
	}
	flag := func(id, item string, on bool, by string) {
		mustPut(t, ds, owner, substrate.PutInput{Kind: pointedFlag, ID: fid(id), Properties: map[string]any{"item": fid("i-" + item), "on": on, "by": by}})
	}
	flag("f-a-ada", "a", true, "ada")
	flag("f-a-bo", "a", false, "bo")
	flag("f-b-bo", "b", true, "bo")
	flag("f-c-ada", "c", false, "ada")
	flag("f-d-ada", "d", true, "ada")
	// e has no flag, and d's only one is deleted.
	if _, err := ds.Delete(ctx, owner, pointedFlag, fid("f-d-ada"), substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	on := map[string]substrate.Cond{"on": {Eq: true}}
	adasOn := map[string]substrate.Cond{"on": {Eq: true}, "by": {Eq: "ada"}}
	for _, tc := range []struct {
		what string
		rb   substrate.ReferencedBy
		want []string
	}{
		{"any flag", substrate.ReferencedBy{Kind: pointedFlag, Property: "item"}, []string{"a", "b", "c"}},
		{"no flag", substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Exists: no()}, []string{"d", "e"}},
		{"a flag that is on", substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: on}, []string{"a", "b"}},
		{"no flag that is on", substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: on, Exists: no()}, []string{"c", "d", "e"}},
		{"ada's flag that is on", substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: adasOn}, []string{"a"}},
		{"no flag of ada's that is on", substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: adasOn, Exists: no()}, []string{"b", "c", "d", "e"}},
	} {
		if got := pointedBy(t, ds, tc.rb); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.what, got, tc.want)
		}
	}

	// With the list's own condition, its count, and a cursor walked a row at
	// a time: the arm filters before the page is cut.
	q := substrate.Query{
		Filter: substrate.Filter{
			Kinds:        []string{pointedItem},
			Properties:   map[string]substrate.Cond{"n": {Gte: 1}},
			ReferencedBy: &substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: adasOn, Exists: no()},
		},
		OrderBy: []substrate.Order{{Property: "name"}}, First: 1, Count: true,
	}
	var walked []string
	for {
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("page after %q: %v", q.After, err)
		}
		if page.Count == nil || *page.Count != 4 {
			t.Fatalf("count = %v, want 4", page.Count)
		}
		walked = append(walked, namesOf(page)...)
		if page.Cursor == "" {
			break
		}
		q.After = page.Cursor
	}
	if want := []string{"b", "c", "d", "e"}; !slices.Equal(walked, want) {
		t.Fatalf("walked %v, want %v", walked, want)
	}
	// A cursor cut under one arm does not continue under another.
	q.After = ""
	first, err := ds.List(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	other := q
	other.Filter.ReferencedBy = &substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: on, Exists: no()}
	other.After = first.Cursor
	if _, err := ds.List(ctx, other); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a cursor replayed under another referencedBy: %v, want refused", err)
	}
}

// A pointer keeps the id it was written with, so one written before a merge
// names the record by a former id, and still counts.
func TestReferencedByFollowsAPointerWrittenBeforeAMerge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	pointedVocabulary(t, ds)
	for _, name := range []string{"kept", "gone", "plain"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: pointedItem, ID: fid("i-" + name), Properties: map[string]any{"name": name}})
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: pointedFlag, ID: fid("f"), Properties: map[string]any{"item": fid("i-gone"), "on": true}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: pointedItem, Winner: fid("i-kept"), Loser: fid("i-gone")}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	rb := substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: map[string]substrate.Cond{"on": {Eq: true}}}
	if got := pointedBy(t, ds, rb); !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("pointed at through a former id: %v, want kept", got)
	}
	rb.Exists = no()
	if got := pointedBy(t, ds, rb); !slices.Equal(got, []string{"plain"}) {
		t.Fatalf("pointed at by none: %v, want plain", got)
	}
}

// While a re-derivation of the pointing property's refs rows is owed, the
// arm reads where each record points from its own value, and answers the
// same.
func TestReferencedByAnswersTheSameWhileRefsAreOwed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn := newDatasetWithDSN(t)
	pointedVocabulary(t, ds)
	for _, name := range []string{"kept", "gone", "flagged", "plain"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: pointedItem, ID: fid("i-" + name), Properties: map[string]any{"name": name}})
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: pointedFlag, ID: fid("f-1"), Properties: map[string]any{"item": fid("i-flagged"), "on": true}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: pointedFlag, ID: fid("f-2"), Properties: map[string]any{"item": fid("i-gone"), "on": true}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: pointedFlag, ID: fid("f-3"), Properties: map[string]any{"item": fid("i-plain"), "on": false}})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: pointedItem, Winner: fid("i-kept"), Loser: fid("i-gone")}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// f-1's refs rows are gone, as a declaration change the pass has not
	// reached would leave them.
	oweReferences(t, ds, dsn, "item", pointedFlag, fid("f-1"), pointedFlag)
	rb := substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: map[string]substrate.Cond{"on": {Eq: true}}}
	if got := pointedBy(t, ds, rb); !slices.Equal(got, []string{"flagged", "kept"}) {
		t.Fatalf("with refs owed: %v, want flagged and kept", got)
	}
	rb.Exists = no()
	if got := pointedBy(t, ds, rb); !slices.Equal(got, []string{"plain"}) {
		t.Fatalf("with refs owed, pointed at by none: %v, want plain", got)
	}
}

// What the arm cannot answer it refuses by name: a kind or a property it
// cannot follow, a condition the pointing kind does not hold, a pair that
// can match nothing, and every read that is not the list.
func TestReferencedByRefusesWhatItCannotAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	pointedVocabulary(t, ds)
	mustPut(t, ds, owner, substrate.PutInput{Kind: pointedItem, ID: fid("i"), Properties: map[string]any{"name": "i"}})
	for what, c := range map[string]struct {
		kinds []string
		rb    substrate.ReferencedBy
		says  string
	}{
		"no kind":                 {[]string{pointedItem}, substrate.ReferencedBy{Property: "item"}, "names the kind"},
		"no property":             {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedFlag}, "names the kind"},
		"an unknown kind":         {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedPackage + "/nothing", Property: "item"}, "filter.referencedBy.kind"},
		"an unknown property":     {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedFlag, Property: "itm"}, "item"},
		"a repeated reference":    {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedFlag, Property: "items"}, "not a single reference"},
		"a property, not a link":  {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedFlag, Property: "on"}, "not a single reference"},
		"a misspelled condition":  {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: map[string]substrate.Cond{"onn": {Eq: true}}, Exists: no()}, "declares no onn"},
		"a kind it never names":   {[]string{pointedOther}, substrate.ReferencedBy{Kind: pointedFlag, Property: "item"}, "points at no kind in filter.kinds"},
		"a comparison of a link":  {[]string{pointedItem}, substrate.ReferencedBy{Kind: pointedFlag, Property: "item", Properties: map[string]substrate.Cond{"item": {Gt: "a"}}}, "is a reference"},
		"`me` where nobody reads": {[]string{vocabulary.KindLLMThread}, substrate.ReferencedBy{Kind: vocabulary.KindLLMThreadMark, Property: "thread", Properties: map[string]substrate.Cond{"readBy": {Eq: substrate.FilterMe}}}, "acts for nobody"},
	} {
		_, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: c.kinds, ReferencedBy: &c.rb}})
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v, want a validation refusal saying %q", what, err, c.says)
		}
	}

	arm := substrate.Filter{Kinds: []string{pointedItem}, ReferencedBy: &substrate.ReferencedBy{Kind: pointedFlag, Property: "item"}}
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "filter.referencedBy narrows a list of records alone") {
			t.Errorf("%s: %v, want the arm refused by name", what, err)
		}
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := ds.Window(ctx, substrate.WindowQuery{Filter: arm, From: from, To: from.Add(time.Hour), First: 10})
	refused("the window read", err)
	_, err = ds.Aggregate(ctx, substrate.AggregateQuery{Filter: arm, Aggregate: &substrate.Aggregate{
		Property: &substrate.AggregateProperty{Name: "n", Anchor: "seenAt"},
		Range:    rng(from, from.Add(time.Hour)),
		Reducers: reducers("count"),
	}})
	refused("the property aggregate", err)
	if got := substrate.AggregateFilterArm(arm); got != "referencedBy" {
		t.Errorf("a metric read names the arm %q, want referencedBy", got)
	}
}
