package engine_test

// The two graph arms of the one list query. `referencing` is the reverse
// read: who points at this record. There is ONE mechanism — a reference value
// — and the refs index is keyed on the target, so the filter answers whatever
// shape the declaration takes: pinned or unpinned, single, repeated or nested
// inside an object, and the page's `matches` says from which site. `expand`
// is the forward hop: the referents a page names ride beside it, keyed by the
// path each row wrote, so a reader joins them by the value it holds.

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const graphPackage = "graph.example.substrate.reamde.dev/graph"

// graphVocabulary declares a hub and four kinds pointing at it: pinned,
// unpinned, repeated, and one whose pointer sits inside an object.
func graphVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	hub := graphPackage + "/hub"
	docs := []map[string]any{
		vocabulary.PackageManifest(graphPackage, 0),
		vocabulary.KindManifest(graphPackage,
			map[string]any{"singular": "hub"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(graphPackage,
			map[string]any{"singular": "spoke"},
			map[string]any{"properties": map[string]any{
				"hub": map[string]any{"type": "reference", "kind": hub, "inverse": "spokes"},
			}}),
		// An UNCONSTRAINED pointer: it names no target kind, so only an index
		// keyed on the target can say it points here without reading every
		// row of every kind.
		vocabulary.KindManifest(graphPackage,
			map[string]any{"singular": "loose"},
			map[string]any{"properties": map[string]any{
				"anything": map[string]any{"type": "reference", "kind": "any"},
			}}),
		vocabulary.KindManifest(graphPackage,
			map[string]any{"singular": "fan"},
			map[string]any{"properties": map[string]any{
				"hubs": map[string]any{"type": "reference", "kind": hub, "repeated": true},
			}}),
		// A pointer INSIDE an object: the match names the property and the
		// path to the site, so a nested pointer does not read as a second
		// property of the same name.
		vocabulary.KindManifest(graphPackage,
			map[string]any{"singular": "nester"},
			map[string]any{"properties": map[string]any{
				"tool": map[string]any{
					"type": "object",
					"fields": map[string]any{
						"callable": map[string]any{"type": "reference", "kind": hub},
					},
				},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install the graph vocabulary: %v", err)
	}
}

// referencing lists the records pointing at one target, with the optional
// narrowing the filter carries beside it.
func referencing(t *testing.T, ds substrate.Dataset, targetKind, targetID string, f substrate.Filter, first int, after string) *substrate.Page {
	t.Helper()
	if f.Referencing == nil {
		f.Referencing = &substrate.Referencing{Ref: vocabulary.RecordPath(targetKind, targetID)}
	}
	page, err := ds.List(context.Background(), substrate.Query{Filter: f, First: first, After: after})
	if err != nil {
		t.Fatalf("list referencing %s/%s: %v", targetKind, targetID, err)
	}
	return page
}

func pathsOf(page *substrate.Page) []string {
	out := make([]string, 0, len(page.Records))
	for _, e := range page.Records {
		out = append(out, vocabulary.RecordPath(e.Kind, e.ID))
	}
	return out
}

func TestReferencingReadsEveryReferenceShape(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: hub, ID: "h1", Properties: map[string]any{"name": "Hub"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h1"},
	})
	// Points at a DIFFERENT hub: the reverse read must not gather it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: hub, ID: "h2", Properties: map[string]any{"name": "Other"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/spoke", ID: "s2", Properties: map[string]any{"hub": "h2"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(hub, "h1")},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/fan", ID: "f1",
		Properties: map[string]any{"hubs": []any{"h2", "h1"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/nester", ID: "n1",
		Properties: map[string]any{"tool": map[string]any{"callable": "h1"}},
	})

	page := referencing(t, ds, hub, "h1", substrate.Filter{}, 50, "")
	if len(page.Records) != 4 {
		t.Fatalf("records = %v, want the spoke, the unpinned pointer, the repeated one and the nested one", pathsOf(page))
	}
	if len(page.Matches) != 4 {
		t.Fatalf("matches = %+v, want one entry per record", page.Matches)
	}
	site := func(kind, id string) substrate.ReferenceSite {
		sites := page.Matches[vocabulary.RecordPath(graphPackage+"/"+kind, id)]
		if len(sites) != 1 {
			t.Fatalf("%s %s matches %+v, want one site", kind, id, sites)
		}
		return sites[0]
	}
	if got := site("spoke", "s1"); got.Property != "hub" || got.Path != "" {
		t.Fatalf("the pinned pointer reads %+v", got)
	}
	if got := site("loose", "x1"); got.Property != "anything" || got.Path != "" {
		t.Fatalf("the unpinned pointer reads %+v", got)
	}
	if got := site("fan", "f1"); got.Property != "hubs" || got.Path != "" {
		t.Fatalf("the repeated pointer reads %+v", got)
	}
	// The nested site names its property AND the address inside it.
	if got := site("nester", "n1"); got.Property != "tool" || got.Path != "callable" {
		t.Fatalf("the nested pointer reads %+v", got)
	}
}

func TestReferencingNarrowsByKindAndProperty(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h1"})
	for _, id := range []string{"s1", "s2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphPackage + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/fan", ID: "f1", Properties: map[string]any{"hubs": []any{"h1"}},
	})

	// The kinds arm of the filter is the drill-down into one source kind.
	page := referencing(t, ds, hub, "h1", substrate.Filter{Kinds: []string{graphPackage + "/spoke"}}, 50, "")
	if len(page.Records) != 2 {
		t.Fatalf("kinds must narrow to the spokes, got %v", pathsOf(page))
	}
	for _, e := range page.Records {
		if e.Kind != graphPackage+"/spoke" {
			t.Fatalf("kinds leaked %s", e.Kind)
		}
	}
	ref := func(property string) *substrate.Referencing {
		return &substrate.Referencing{Ref: vocabulary.RecordPath(hub, "h1"), Property: property}
	}
	if page := referencing(t, ds, hub, "h1", substrate.Filter{Referencing: ref("hubs")}, 50, ""); len(page.Records) != 1 {
		t.Fatalf("property must narrow to the repeated pointer, got %v", pathsOf(page))
	}
	if page := referencing(t, ds, hub, "h1", substrate.Filter{Referencing: ref("nothing")}, 50, ""); len(page.Records) != 0 {
		t.Fatalf("an unclaimed property must find nothing, got %v", pathsOf(page))
	}
}

func TestReferencingExcludesADeletedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h1"})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h1"},
	})
	if page := referencing(t, ds, hub, "h1", substrate.Filter{}, 50, ""); len(page.Records) != 1 {
		t.Fatalf("referencing before the delete = %v", pathsOf(page))
	}
	if _, err := ds.Delete(ctx, owner, graphPackage+"/spoke", "s1", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the source: %v", err)
	}
	// The tombstone keeps its rows in the index (a delete touches `records`
	// alone), so this is the list's own liveness predicate doing the
	// filtering: a deleted record points at nothing.
	if page := referencing(t, ds, hub, "h1", substrate.Filter{}, 50, ""); len(page.Records) != 0 {
		t.Fatalf("referencing after the delete = %v", pathsOf(page))
	}
}

func TestReferencingFindsAPointerWrittenUnderAFormerID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: hub, ID: "h1", Properties: map[string]any{"name": "Winner"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: hub, ID: "h2", Properties: map[string]any{"name": "Loser"},
	})
	// Written while h2 was still a record of its own.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h2"},
	})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: hub, Winner: winner.ID, Loser: "h2"}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// A merge REPOINTS NOTHING: reference values resolve forward through the
	// former-id trail on read. A reverse read that asked only for the canonical
	// id would lose this pointer entirely.
	//
	// The merge also leaves its own `recordmerge` row naming both sides, which
	// is why this looks for the spoke rather than counting: that row is the
	// merge's audit trail and belongs in the fan-in.
	for _, target := range []string{"h1", "h2"} {
		page := referencing(t, ds, hub, target, substrate.Filter{}, 50, "")
		sites := page.Matches[vocabulary.RecordPath(graphPackage+"/spoke", "s1")]
		if len(sites) != 1 || sites[0].Property != "hub" || sites[0].Path != "" {
			t.Fatalf("referencing %s: the pointer written under the former id must be found and named, got %v %+v",
				target, pathsOf(page), page.Matches)
		}
	}
	// And the stored value still spells the loser's id: nothing rewrote it.
	if got := refPathValue(mustGet(t, ds, graphPackage+"/spoke", "s1"), "hub"); got != vocabulary.RecordPath(hub, "h2") {
		t.Fatalf("the merge rewrote a reference value: hub = %q", got)
	}
}

// ONE READ OVER SEVERAL TARGETS. An owner holds several addresses, and "what
// points at me" is what points at any of them: `refs` is that read, an OR
// over the targets, and a record pointing at two of them is one row of the
// page, one unit of the count and one entry of `matches`.
func TestReferencingManyTargetsIsOneRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"
	spoke := graphPackage + "/spoke"
	path := func(kind, id string) string { return vocabulary.RecordPath(graphPackage+"/"+kind, id) }

	for _, id := range []string{"work", "home", "other"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: id})
	}
	for id, at := range map[string]string{"s-work": "work", "s-home": "home", "s-other": "other"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: spoke, ID: id, Properties: map[string]any{"hub": at}})
	}
	// One record pointing at BOTH addresses.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/fan", ID: "f-both", Properties: map[string]any{"hubs": []any{"work", "home"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/nester", ID: "n-work",
		Properties: map[string]any{"tool": map[string]any{"callable": "work"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/loose", ID: "x-home", Properties: map[string]any{"anything": vocabulary.RecordPath(hub, "home")},
	})
	// Points at a SPOKE: only a target list spanning two kinds gathers it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/loose", ID: "x-spoke", Properties: map[string]any{"anything": path("spoke", "s-other")},
	})

	both := []string{vocabulary.RecordPath(hub, "work"), vocabulary.RecordPath(hub, "home")}
	want := []string{path("fan", "f-both"), path("loose", "x-home"), path("nester", "n-work"), path("spoke", "s-home"), path("spoke", "s-work")}
	list := func(f substrate.Filter, first int, after string) *substrate.Page {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{Filter: f, First: first, After: after, Count: true})
		if err != nil {
			t.Fatalf("list %+v: %v", f.Referencing, err)
		}
		return page
	}
	sorted := func(paths []string) string {
		out := append([]string(nil), paths...)
		sort.Strings(out)
		return strings.Join(out, " ")
	}

	page := list(substrate.Filter{Referencing: &substrate.Referencing{Refs: both}}, 50, "")
	if got := sorted(pathsOf(page)); got != sorted(want) {
		t.Fatalf("refs over both addresses = %s\nwant %s", got, sorted(want))
	}
	if page.Count == nil || *page.Count != int64(len(want)) {
		t.Fatalf("count = %v, want %d: the record pointing at both counts once", page.Count, len(want))
	}
	if sites := page.Matches[path("fan", "f-both")]; len(sites) != 1 || sites[0].Property != "hubs" {
		t.Fatalf("the record pointing at both matches %+v, want the one hubs site", sites)
	}
	for _, p := range want {
		if len(page.Matches[p]) == 0 {
			t.Fatalf("%s is on the page without its match: %+v", p, page.Matches)
		}
	}

	// A walk in pages of two sees each record exactly once.
	seen := map[string]int{}
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the cursor never terminated")
		}
		page := list(substrate.Filter{Referencing: &substrate.Referencing{Refs: both}}, 2, after)
		for _, p := range pathsOf(page) {
			seen[p]++
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	for _, p := range want {
		if seen[p] != 1 {
			t.Errorf("the paged walk returned %s %d times", p, seen[p])
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("the paged walk saw %v, want %v", seen, want)
	}

	// The same narrowing the single form takes: one property, one source kind.
	page = list(substrate.Filter{Referencing: &substrate.Referencing{Refs: both, Property: "hub"}}, 50, "")
	if got := sorted(pathsOf(page)); got != sorted([]string{path("spoke", "s-home"), path("spoke", "s-work")}) {
		t.Fatalf("refs narrowed to hub = %s, want the two spokes", got)
	}
	page = list(substrate.Filter{Kinds: []string{graphPackage + "/fan"}, Referencing: &substrate.Referencing{Refs: both}}, 50, "")
	if got := sorted(pathsOf(page)); got != path("fan", "f-both") {
		t.Fatalf("refs narrowed to the fan kind = %s", got)
	}

	// Order and repetition in the list do not change the answer.
	repeated := []string{both[1], both[0], both[1]}
	if got := sorted(pathsOf(list(substrate.Filter{Referencing: &substrate.Referencing{Refs: repeated}}, 50, ""))); got != sorted(want) {
		t.Fatalf("refs %v = %s, want %s", repeated, got, sorted(want))
	}

	// Targets of two kinds are one read too.
	mixed := []string{vocabulary.RecordPath(hub, "other"), path("spoke", "s-other")}
	page = list(substrate.Filter{Referencing: &substrate.Referencing{Refs: mixed}}, 50, "")
	if got := sorted(pathsOf(page)); got != sorted([]string{path("loose", "x-spoke"), path("spoke", "s-other")}) {
		t.Fatalf("refs across a hub and a spoke = %s", got)
	}
}

// A former id of one address still counts: the list resolves each target's
// trail as the single form does, whether it names the canonical id or the
// discarded one.
func TestReferencingManyTargetsFollowsAFormerID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"
	spoke := graphPackage + "/spoke"

	for _, id := range []string{"work", "home", "old-home"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: id})
	}
	for id, at := range map[string]string{"s-work": "work", "s-home": "home", "s-old": "old-home"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: spoke, ID: id, Properties: map[string]any{"hub": at}})
	}
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: hub, Winner: "home", Loser: "old-home"}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	for name, refs := range map[string][]string{
		"the canonical ids": {vocabulary.RecordPath(hub, "work"), vocabulary.RecordPath(hub, "home")},
		"a former id":       {vocabulary.RecordPath(hub, "work"), vocabulary.RecordPath(hub, "old-home")},
	} {
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds: []string{spoke}, Referencing: &substrate.Referencing{Refs: refs},
		}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := pathsOf(page)
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join([]string{
			vocabulary.RecordPath(spoke, "s-home"), vocabulary.RecordPath(spoke, "s-old"), vocabulary.RecordPath(spoke, "s-work"),
		}, " ") {
			t.Fatalf("%s: refs = %v, want every spoke, the one written under the former id included", name, got)
		}
	}
}

// The target list is bounded, and a list that names no target, or names
// targets in both spellings at once, is refused rather than read as some
// other question.
func TestReferencingRefusesATargetListPastTheCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"
	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h0"})
	mustPut(t, ds, owner, substrate.PutInput{Kind: graphPackage + "/spoke", ID: "s0", Properties: map[string]any{"hub": "h0"}})

	atCap := make([]string, substrate.MaxReferencingTargets)
	for i := range atCap {
		atCap[i] = vocabulary.RecordPath(hub, "h"+strconv.Itoa(i))
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Referencing: &substrate.Referencing{Refs: atCap}}})
	if err != nil {
		t.Fatalf("a list AT the cap must be read: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != "s0" {
		t.Fatalf("refs at the cap = %v", pathsOf(page))
	}

	for name, tc := range map[string]struct {
		ref  substrate.Referencing
		want string
	}{
		"past the cap": {
			substrate.Referencing{Refs: append(atCap, vocabulary.RecordPath(hub, "one-more"))},
			"the cap is " + strconv.Itoa(substrate.MaxReferencingTargets),
		},
		"ref and refs": {
			substrate.Referencing{Ref: atCap[0], Refs: atCap[1:2]}, "set ref or refs, not both",
		},
		"ref and an empty refs": {
			substrate.Referencing{Ref: atCap[0], Refs: []string{}}, "set ref or refs, not both",
		},
		"no target": {substrate.Referencing{Property: "hub"}, "name a target"},
		"a bare id": {substrate.Referencing{Refs: []string{atCap[0], "h1"}}, "referencing.refs[1]"},
	} {
		_, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Referencing: &tc.ref}})
		wantErr(t, err, substrate.ErrValidation, name)
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: the refusal %q does not say %q", name, err, tc.want)
		}
	}
}

func TestReferencingPagesCleanly(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h1"})
	want := map[string]bool{}
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphPackage + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
		want[vocabulary.RecordPath(graphPackage+"/spoke", id)] = true
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/nester", ID: "n1",
		Properties: map[string]any{"tool": map[string]any{"callable": "h1"}},
	})
	want[vocabulary.RecordPath(graphPackage+"/nester", "n1")] = true
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(hub, "h1")},
	})
	want[vocabulary.RecordPath(graphPackage+"/loose", "x1")] = true

	seen := map[string]int{}
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the cursor never terminated")
		}
		page := referencing(t, ds, hub, "h1", substrate.Filter{}, 2, after)
		for _, path := range pathsOf(page) {
			seen[path]++
			// The matches ride every page, not the first alone.
			if len(page.Matches[path]) == 0 {
				t.Fatalf("page %d carries %s without its match", pages, path)
			}
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	for path := range want {
		switch seen[path] {
		case 1:
		case 0:
			t.Errorf("%s was dropped between pages", path)
		default:
			t.Errorf("%s was returned %d times", path, seen[path])
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("saw %v, want %v", seen, want)
	}
}

// A CURSOR BELONGS TO ONE FILTER. Its key addresses a position in the order,
// not a row in this filter's match set, so replaying it under a different
// predicate would seek past rows the new one admits and answer a short page
// that read as a complete one. The filter is stamped into the cursor, and a
// mismatch is the same refusal a token from another orderBy gets.
func TestListRefusesACursorReplayedWithAnotherFilter(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	ctx := context.Background()
	hub := graphPackage + "/hub"

	for _, id := range []string{"h1", "h2"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: id})
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphPackage + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(hub, "h1")},
	})

	// Garbage is refused before any comparison.
	_, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{hub}}, After: "not-a-cursor"})
	wantErr(t, err, substrate.ErrValidation, "cursor")

	narrow := substrate.Filter{
		Kinds:       []string{graphPackage + "/spoke"},
		Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(hub, "h1"), Property: "hub"},
	}
	minted := referencing(t, ds, hub, "h1", narrow, 1, "")
	if minted.Cursor == "" || len(minted.Records) != 1 {
		t.Fatalf("the first page = %v cursor %q", pathsOf(minted), minted.Cursor)
	}
	// The same cursor, still good for the read it came from.
	next := referencing(t, ds, hub, "h1", narrow, 1, minted.Cursor)
	if len(next.Records) != 1 || next.Records[0].ID == minted.Records[0].ID {
		t.Fatalf("the second page of the same read = %v", pathsOf(next))
	}

	for name, f := range map[string]substrate.Filter{
		"another property": {
			Kinds:       []string{graphPackage + "/spoke"},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(hub, "h1"), Property: "anything"},
		},
		"another source kind": {
			Kinds:       []string{graphPackage + "/loose"},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(hub, "h1"), Property: "hub"},
		},
		"no narrowing at all": {
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(hub, "h1")},
		},
		"another target": {
			Kinds:       []string{graphPackage + "/spoke"},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(hub, "h2"), Property: "hub"},
		},
		"no referencing, the kind alone": {
			Kinds: []string{graphPackage + "/spoke"},
		},
	} {
		_, err := ds.List(ctx, substrate.Query{Filter: f, First: 1, After: minted.Cursor})
		if err == nil {
			t.Fatalf("%s replayed the cursor instead of refusing it", name)
		}
		wantErr(t, err, substrate.ErrValidation, name+": cursor")
		if !strings.Contains(err.Error(), "does not match this filter") {
			t.Fatalf("%s: the refusal must say the filter moved: %v", name, err)
		}
	}
}

// --- expand: the forward hop --------------------------------------------------

func TestExpandCarriesReferentsKeyedByTheWrittenPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h1", Properties: map[string]any{"name": "One"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h2", Properties: map[string]any{"name": "Two"}})
	// Two spokes at one hub: the referent is included ONCE.
	for _, id := range []string{"s1", "s2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphPackage + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/spoke", ID: "s3", Properties: map[string]any{"hub": "h2"},
	})
	// A repeated pointer, and an unpinned one at a record that does not
	// exist: the dangling pointer has no entry, and the row still says where
	// it pointed.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/fan", ID: "f1", Properties: map[string]any{"hubs": []any{"h1", "h2"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(hub, "h-nowhere")},
	})

	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{graphPackage + "/spoke"}}, Expand: []string{"hub"}, First: 50,
	})
	if err != nil {
		t.Fatalf("list with expand: %v", err)
	}
	if len(page.Records) != 3 || len(page.Included) != 2 {
		t.Fatalf("records %v included %d, want 3 spokes and the 2 hubs once each", pathsOf(page), len(page.Included))
	}
	for _, id := range []string{"h1", "h2"} {
		got := page.Included[vocabulary.RecordPath(hub, id)]
		if got == nil || got.ID != id || got.Kind != hub {
			t.Fatalf("included[%s] = %+v", id, got)
		}
	}
	// Every row's value is a key into included.
	for _, e := range page.Records {
		if _, ok := page.Included[refPathValue(e, "hub")]; !ok {
			t.Fatalf("%s points at %q, which included does not carry: %v", e.ID, refPathValue(e, "hub"), page.Included)
		}
	}

	// Repeated: each element resolves; the two kinds together.
	page, err = ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{graphPackage + "/fan", graphPackage + "/loose"}},
		Expand: []string{"hubs", "anything"}, First: 50,
	})
	if err != nil {
		t.Fatalf("list with a repeated expand: %v", err)
	}
	if len(page.Included) != 2 {
		t.Fatalf("included = %v, want the two hubs and NOT the dangling pointer", sortedIncluded(page))
	}
	if _, dangling := page.Included[vocabulary.RecordPath(hub, "h-nowhere")]; dangling {
		t.Fatal("a dangling pointer grew an entry")
	}
	if got := refPathValue(mustGet(t, ds, graphPackage+"/loose", "x1"), "anything"); got != vocabulary.RecordPath(hub, "h-nowhere") {
		t.Fatalf("the dangling value was rewritten: %q", got)
	}

	// No expand, no included: the key is absent rather than empty.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{graphPackage + "/spoke"}}, First: 50})
	if err != nil || page.Included != nil {
		t.Fatalf("a plain list carries included %v (%v)", page.Included, err)
	}
}

func sortedIncluded(page *substrate.Page) []string {
	out := make([]string, 0, len(page.Included))
	for k := range page.Included {
		out = append(out, k)
	}
	return out
}

// A POINTER AT A FORMER ID EXPANDS TO THE CANONICAL RECORD, under the key the
// row wrote: the reader joins by the value it holds, and the value was never
// rewritten by the merge.
func TestExpandFollowsTheFormerIDTrail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h1", Properties: map[string]any{"name": "Winner"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h2", Properties: map[string]any{"name": "Loser"}})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphPackage + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h2"},
	})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: hub, Winner: "h1", Loser: "h2"}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{graphPackage + "/spoke"}}, Expand: []string{"hub"}, First: 50,
	})
	if err != nil {
		t.Fatalf("list with expand: %v", err)
	}
	written := vocabulary.RecordPath(hub, "h2")
	if refPathValue(page.Records[0], "hub") != written {
		t.Fatalf("the merge rewrote the value: %q", refPathValue(page.Records[0], "hub"))
	}
	got := page.Included[written]
	if got == nil || got.ID != "h1" || got.Properties["name"] != "Winner" {
		t.Fatalf("included[%s] = %+v, want the winner", written, got)
	}
}

func TestExpandRefusesANameThatIsNotAReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphPackage + "/hub"
	mustPut(t, ds, owner, substrate.PutInput{Kind: hub, ID: "h1", Properties: map[string]any{"name": "One"}})

	for name, q := range map[string]substrate.Query{
		"an unknown property": {
			Filter: substrate.Filter{Kinds: []string{graphPackage + "/spoke"}}, Expand: []string{"owner"},
		},
		"a string property": {
			Filter: substrate.Filter{Kinds: []string{hub}}, Expand: []string{"name"},
		},
		// `hub` is a spoke's property; the filter admits fans alone.
		"another kind's reference": {
			Filter: substrate.Filter{Kinds: []string{graphPackage + "/fan"}}, Expand: []string{"hub"},
		},
	} {
		_, err := ds.List(ctx, q)
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: expand answered %v, want a validation refusal", name, err)
		}
		if !strings.Contains(err.Error(), "expand") || !strings.Contains(err.Error(), q.Expand[0]) {
			t.Fatalf("%s: the refusal must name the arm and the property: %v", name, err)
		}
	}
	// A filter naming no kind expands any declared reference property.
	if _, err := ds.List(ctx, substrate.Query{Expand: []string{"hub"}, First: 5}); err != nil {
		t.Fatalf("expand over every kind refused a declared reference: %v", err)
	}
}

// --- the reverse read over the people sample ---------------------------------

func TestReferencingIsSeparateAndPaged(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name": aname("Samuel Jones"), "emails": gemails("sam@acme.com"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Sam J", "email": "sam@acme.com",
	})

	// The record manifest never carries its reverse pointers: they are derived
	// state, and a record can have an unbounded number of them, so the fan-in
	// is reachable only through the paged list below.
	first := referencing(t, ds, sam.Kind, sam.ID, substrate.Filter{}, 1, "")
	if len(first.Records) != 1 || first.Cursor == "" {
		t.Fatalf("first page = %v cursor %q", pathsOf(first), first.Cursor)
	}
	if sites := first.Matches[pathsOf(first)[0]]; len(sites) != 1 || sites[0].Property != "person" || sites[0].Path != "" {
		t.Fatalf("first page's match = %+v", sites)
	}
	second := referencing(t, ds, sam.Kind, sam.ID, substrate.Filter{}, 1, first.Cursor)
	if len(second.Records) != 1 || second.Cursor != "" {
		t.Fatalf("second page = %v cursor %q", pathsOf(second), second.Cursor)
	}
	got := map[string]*substrate.Record{first.Records[0].ID: first.Records[0], second.Records[0].ID: second.Records[0]}
	if got[g.ID] == nil || got[s.ID] == nil {
		t.Fatalf("the two pages named %v, want the google contact %s and the slack user %s", pathsOf(first), g.ID, s.ID)
	}
	if got[g.ID].Title != "Samuel Jones" {
		t.Fatalf("the google contact's title did not travel: %+v", got[g.ID])
	}
}

func TestReferencingExcludesTombstonedSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	g := syncSource(t, ds, people, typeGoogleContact, "g-ned", map[string]any{
		"name": aname("Nameless Ned"),
	})
	pid := personOf(t, ds, g)
	if page := referencing(t, ds, typePerson, pid, substrate.Filter{}, 50, ""); len(page.Records) != 1 {
		t.Fatalf("referencing before delete = %v", pathsOf(page))
	}
	if _, err := ds.Delete(ctx, people, g.Kind, g.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if page := referencing(t, ds, typePerson, pid, substrate.Filter{}, 50, ""); len(page.Records) != 0 {
		t.Fatalf("referencing after delete = %v", pathsOf(page))
	}
}

func TestReferencingResolvesFormerIDToCanonicalRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name": aname("Samuel Jones"), "emails": gemails("sam@acme.com"),
	})
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Sam J", "email": "sam@corp.example",
	})
	winner, loser := personOf(t, ds, g), personOf(t, ds, s)
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: typePerson, Winner: winner, Loser: loser}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	canonical := referencing(t, ds, typePerson, winner, substrate.Filter{}, 50, "")
	former := referencing(t, ds, typePerson, loser, substrate.Filter{}, 50, "")
	if len(canonical.Records) == 0 || strings.Join(pathsOf(canonical), ",") != strings.Join(pathsOf(former), ",") {
		t.Fatalf("former page = %v, canonical page = %v", pathsOf(former), pathsOf(canonical))
	}
}
