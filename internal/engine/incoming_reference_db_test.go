package engine_test

// The reverse read: who points at this record. There is ONE mechanism now — a
// reference value — and the refs index is keyed on the target, so `incoming`
// answers whatever shape the declaration takes: pinned or unpinned, single,
// repeated or nested inside an object.
//
// The cursor is what these mostly pin. The order is the index's own key,
// (src_kind, src, property, path, ord), and every component of it rides the
// token — a page boundary that drops or repeats a row is the failure this suite
// exists to catch, so the fixtures deliberately tie every component that can.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const graphAuthority = "graph.example.substrate.reamde.dev"

// graphVocabulary declares a hub and four kinds pointing at it: pinned,
// unpinned, repeated, and one whose pointer sits inside an object.
func graphVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	hub := graphAuthority + "/hub"
	docs := []map[string]any{
		vocabulary.AuthorityManifest(graphAuthority, 0),
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "hub", "plural": "hubs"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "spoke", "plural": "spokes"},
			map[string]any{"properties": map[string]any{
				"hub": map[string]any{"type": "reference", "kind": hub, "inverse": "spokes"},
			}}),
		// An UNCONSTRAINED pointer. The edge-and-probe union this replaced could
		// not see one: it named no target kind, so the registry could not say it
		// pointed here without reading every row of every kind. The index is
		// keyed on the target, so there is nothing to enumerate.
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "loose", "plural": "looses"},
			map[string]any{"properties": map[string]any{
				"anything": map[string]any{"type": "reference", "kind": "any"},
			}}),
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "fan", "plural": "fans"},
			map[string]any{"properties": map[string]any{
				"hubs": map[string]any{"type": "reference", "kind": hub, "repeated": true},
			}}),
		// A pointer INSIDE an object: the reverse row names the property and the
		// path to the site, so a nested pointer does not read as a second
		// property of the same name.
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "nester", "plural": "nesters"},
			map[string]any{"properties": map[string]any{
				"tool": map[string]any{
					"type": "object",
					"fields": map[string]any{
						"callable": map[string]any{"type": "reference", "kind": hub},
					},
				},
			}}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install the graph vocabulary: %v", err)
	}
}

func incoming(t *testing.T, ds substrate.Dataset, id string, opts substrate.IncomingOptions) *substrate.IncomingPage {
	t.Helper()
	page, err := ds.Incoming(context.Background(), graphAuthority+"/hub", id, opts)
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	return page
}

func TestIncomingReadsEveryReferenceShape(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	hub := graphAuthority + "/hub"

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: hub, ID: "h1", Properties: map[string]any{"name": "Hub"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h1"},
	})
	// Points at a DIFFERENT hub: the reverse read must not gather it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: hub, ID: "h2", Properties: map[string]any{"name": "Other"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s2", Properties: map[string]any{"hub": "h2"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(hub, "h1")},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/fan", ID: "f1",
		Properties: map[string]any{"hubs": []any{"h2", "h1"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/nester", ID: "n1",
		Properties: map[string]any{"tool": map[string]any{"callable": "h1"}},
	})

	page := incoming(t, ds, "h1", substrate.IncomingOptions{})
	if page.Total != 4 {
		t.Fatalf("total = %d, want the spoke, the unpinned pointer, the repeated one and the nested one: %+v",
			page.Total, page.Incoming)
	}
	by := map[string]substrate.IncomingReference{}
	for _, row := range page.Incoming {
		by[row.From.ID] = row
	}
	if got := by["s1"]; got.Property != "hub" || got.Path != "" {
		t.Fatalf("the pinned pointer reads %+v", got)
	}
	if got := by["x1"]; got.Property != "anything" || got.Path != "" {
		t.Fatalf("the unpinned pointer reads %+v", got)
	}
	if got := by["f1"]; got.Property != "hubs" || got.Path != "" {
		t.Fatalf("the repeated pointer reads %+v", got)
	}
	// The nested site names its property AND the address inside it.
	if got := by["n1"]; got.Property != "tool" || got.Path != "callable" {
		t.Fatalf("the nested pointer reads %+v", got)
	}
}

func TestIncomingNarrowsToOneGroup(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})
	for _, id := range []string{"s1", "s2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/fan", ID: "f1", Properties: map[string]any{"hubs": []any{"h1"}},
	})

	// A drill-down expands ONE group: the total is that group's, not the
	// record's, or the pager would promise rows the page cannot show.
	page := incoming(t, ds, "h1", substrate.IncomingOptions{FromKind: graphAuthority + "/spoke"})
	if page.Total != 2 || len(page.Incoming) != 2 {
		t.Fatalf("fromKind must narrow to the spokes, got total=%d rows=%d", page.Total, len(page.Incoming))
	}
	for _, row := range page.Incoming {
		if row.From.Kind != graphAuthority+"/spoke" {
			t.Fatalf("fromKind leaked %s", row.From.Kind)
		}
	}
	if page := incoming(t, ds, "h1", substrate.IncomingOptions{Property: "hubs"}); page.Total != 1 {
		t.Fatalf("property must narrow to the repeated pointer, got %d", page.Total)
	}
	if page := incoming(t, ds, "h1", substrate.IncomingOptions{Property: "nothing"}); page.Total != 0 {
		t.Fatalf("an unclaimed property must find nothing, got %d", page.Total)
	}
}

func TestIncomingExcludesADeletedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h1"},
	})
	if page := incoming(t, ds, "h1", substrate.IncomingOptions{}); page.Total != 1 {
		t.Fatalf("incoming before the delete = %+v", page)
	}
	if _, err := ds.Delete(ctx, owner, graphAuthority+"/spoke", "s1"); err != nil {
		t.Fatalf("delete the source: %v", err)
	}
	// The tombstone keeps its rows in the index (a delete touches `records`
	// alone), so this is the JOIN doing the filtering: a deleted record points
	// at nothing.
	if page := incoming(t, ds, "h1", substrate.IncomingOptions{}); page.Total != 0 {
		t.Fatalf("incoming after the delete = %+v", page)
	}
}

func TestIncomingFindsAPointerWrittenUnderAFormerID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/hub", ID: "h1", Properties: map[string]any{"name": "Winner"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/hub", ID: "h2", Properties: map[string]any{"name": "Loser"},
	})
	// Written while h2 was still a record of its own.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h2"},
	})
	if _, err := ds.Merge(ctx, owner, graphAuthority+"/hub", winner.ID, "h2"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// A merge REPOINTS NOTHING: reference values resolve forward through the
	// former-id trail on read. A reverse read that asked only for the canonical
	// id would lose this pointer entirely.
	//
	// The merge also leaves its own `recordmerge` row naming both sides, which
	// is why this looks for the spoke rather than counting: that row is the
	// merge's audit trail and belongs in the fan-in.
	page := incoming(t, ds, "h1", substrate.IncomingOptions{})
	var found *substrate.IncomingReference
	for i, row := range page.Incoming {
		if row.From.ID == "s1" {
			found = &page.Incoming[i]
		}
	}
	if found == nil {
		t.Fatalf("the pointer written under the former id must still be found, got %+v", page.Incoming)
	}
	if found.Property != "hub" || found.Path != "" {
		t.Fatalf("the recovered pointer reads %+v", *found)
	}
	// And the stored value still spells the loser's id: nothing rewrote it.
	if got := refPathValue(mustGet(t, ds, graphAuthority+"/spoke", "s1"), "hub"); got !=
		vocabulary.RecordPath(graphAuthority+"/hub", "h2") {
		t.Fatalf("the merge rewrote a reference value: hub = %q", got)
	}
}

func TestIncomingPagesCleanlyAcrossEveryTie(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})
	// Five spokes of one kind under one property: the leading components of the
	// key tie for all of them, so the boundary rests on `src` alone.
	want := map[string]int{}
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
		want[id]++
	}
	// One record pointing at the hub TWICE from two sites: (property, path)
	// separates them, and a boundary that read only the source would collapse
	// the pair into one row.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/nester", ID: "n1",
		Properties: map[string]any{"tool": map[string]any{"callable": "h1"}},
	})
	want["n1"]++
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(graphAuthority+"/hub", "h1")},
	})
	want["x1"]++

	total := 0
	for _, n := range want {
		total += n
	}
	seen := map[string]int{}
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the cursor never terminated")
		}
		page := incoming(t, ds, "h1", substrate.IncomingOptions{First: 2, After: after})
		if page.Total != total {
			t.Fatalf("total = %d, want %d", page.Total, total)
		}
		for _, row := range page.Incoming {
			seen[row.From.ID]++
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	for id, n := range want {
		switch {
		case seen[id] == n:
		case seen[id] == 0:
			t.Errorf("%s was dropped between pages", id)
		default:
			t.Errorf("%s was returned %d times, want %d", id, seen[id], n)
		}
	}
}

func TestIncomingOrdersByTheIndexKey(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})
	// Written out of id order, so the ordering under test is the index's and
	// not the writing order's.
	for _, id := range []string{"s3", "s1", "s2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}

	page := incoming(t, ds, "h1", substrate.IncomingOptions{FromKind: graphAuthority + "/spoke"})
	var got []string
	for _, row := range page.Incoming {
		got = append(got, row.From.ID)
	}
	if len(got) != 3 || got[0] != "s1" || got[1] != "s2" || got[2] != "s3" {
		t.Fatalf("rows came back %v, want the index's (src_kind, src, ...) order", got)
	}
}

func TestIncomingRefusesACursorFromAnotherOrder(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})

	// The order is the refs index's key now, so a token minted under any other
	// signature describes a different sort. Refused, because replaying it would
	// silently mis-page.
	_, err := ds.Incoming(context.Background(), graphAuthority+"/hub", "h1",
		substrate.IncomingOptions{After: "not-a-cursor"})
	wantErr(t, err, substrate.ErrValidation, "cursor")
}

// A CURSOR BELONGS TO ONE READ. Its key addresses a row in the index, not a
// row in this page's match set, so replaying it under a different narrowing
// used to seek past unrelated keys and answer a short page that read as a
// complete one. The target, the `property` and the `fromKind` are stamped into
// the cursor, and a mismatch is the same refusal a token from another order
// gets.
func TestIncomingRefusesACursorReplayedWithAnotherNarrowing(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	ctx := context.Background()

	for _, id := range []string{"h1", "h2"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: id})
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/loose", ID: "x1",
		Properties: map[string]any{"anything": vocabulary.RecordPath(graphAuthority+"/hub", "h1")},
	})

	minted := incoming(t, ds, "h1", substrate.IncomingOptions{
		First: 1, Property: "hub", FromKind: graphAuthority + "/spoke",
	})
	if minted.Cursor == "" {
		t.Fatal("the first page minted no cursor")
	}
	// The same cursor, still good for the read it came from.
	next := incoming(t, ds, "h1", substrate.IncomingOptions{
		First: 1, Property: "hub", FromKind: graphAuthority + "/spoke", After: minted.Cursor,
	})
	if len(next.Incoming) != 1 || next.Incoming[0].From.ID != "s2" {
		t.Fatalf("the second page of the same read = %+v", next.Incoming)
	}

	for name, opts := range map[string]substrate.IncomingOptions{
		"another property": {First: 1, Property: "anything", FromKind: graphAuthority + "/spoke"},
		"another source kind": {
			First: 1, Property: "hub", FromKind: graphAuthority + "/loose",
		},
		"no narrowing at all": {First: 1},
	} {
		opts.After = minted.Cursor
		if _, err := ds.Incoming(ctx, graphAuthority+"/hub", "h1", opts); err == nil {
			t.Fatalf("%s replayed the cursor instead of refusing it", name)
		} else {
			wantErr(t, err, substrate.ErrValidation, "cursor")
		}
	}
	// And a cursor minted at one target says nothing about another.
	_, err := ds.Incoming(ctx, graphAuthority+"/hub", "h2", substrate.IncomingOptions{
		First: 1, Property: "hub", FromKind: graphAuthority + "/spoke", After: minted.Cursor,
	})
	wantErr(t, err, substrate.ErrValidation, "cursor")
}

// A MERGE INTO THE TARGET MID-WALK INVALIDATES THE CURSOR. The loser's id joins
// the match, so pointers stored against it appear in the middle of the order —
// including before the cursor, where the rest of the walk would never look at
// them again. The cursor is stamped with the target's whole id set, so the
// merge refuses the outstanding cursor and the client restarts on a complete
// answer instead of paging through a silently short one.
func TestIncomingRefusesACursorAfterAMergeIntoTheTarget(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	ctx := context.Background()

	for _, id := range []string{"h1", "h2"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: id})
	}
	// s2 and s3 point at the winner; s1 points at the hub about to lose the
	// merge, and sorts FIRST — the row a cursor minted before the merge would
	// carry the walk straight past.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h2"},
	})
	for _, id := range []string{"s2", "s3"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}

	minted := incoming(t, ds, "h1", substrate.IncomingOptions{First: 1})
	if minted.Cursor == "" || len(minted.Incoming) != 1 || minted.Incoming[0].From.ID != "s2" {
		t.Fatalf("the first page = %+v", minted.Incoming)
	}

	if _, err := ds.Merge(ctx, owner, graphAuthority+"/hub", "h1", "h2"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, err := ds.Incoming(ctx, graphAuthority+"/hub", "h1",
		substrate.IncomingOptions{First: 1, After: minted.Cursor})
	wantErr(t, err, substrate.ErrValidation, "cursor")

	// Restarting answers with everything, s1 among it.
	seen := map[string]bool{}
	after := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("the restarted walk never terminated")
		}
		page := incoming(t, ds, "h1", substrate.IncomingOptions{First: 2, After: after})
		for _, row := range page.Incoming {
			seen[row.From.ID] = true
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		if !seen[id] {
			t.Fatalf("the restarted walk missed %s: saw %v", id, seen)
		}
	}
}
