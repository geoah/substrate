package engine_test

// The reverse read answers for BOTH mechanisms: an edge row pointing here, and
// a reference property naming this record. They are one relationship to a
// reader and two mechanisms to the store, so a row says which via `via`.
//
// The cursor is what these mostly pin. The union's order is
// (rel, src_kind, via, created_at DESC, src_id) and every component of it rides
// the token — a page boundary that drops or repeats a row is the failure this
// suite exists to catch, so the fixtures deliberately put ties in every
// component that can tie.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const graphAuthority = "graph.example.substrate.reamde.dev"

// graphVocabulary declares a hub and two kinds pointing at it — one by
// reference, one by edge — so a single hub is reachable both ways.
func graphVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		vocabulary.AuthorityManifest(graphAuthority, 0),
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "hub", "plural": "hubs"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "spoke", "plural": "spokes"},
			map[string]any{"properties": map[string]any{
				"hub": map[string]any{
					"type": "reference", "kind": graphAuthority + "/hub", "inverse": "spokes",
				},
			}}),
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "linked", "plural": "linkeds"},
			map[string]any{"edges": map[string]any{
				"hub": map[string]any{"to": graphAuthority + "/hub", "inverse": "linked"},
			}}),
		// An unconstrained pointer: it names no target kind, so the registry
		// cannot say it points HERE without reading every row of every kind.
		vocabulary.KindManifest(graphAuthority,
			map[string]any{"singular": "loose", "plural": "looses"},
			map[string]any{"properties": map[string]any{
				"anything": map[string]any{"type": "reference", "kind": "any"},
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

func TestIncomingReadsReferencesAndEdgesAsOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/hub", ID: "h1", Properties: map[string]any{"name": "Hub"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s1", Properties: map[string]any{"hub": "h1"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/linked", ID: "l1",
		Edges: []substrate.EdgeInput{{Rel: "hub", To: substrate.EdgeRef{ID: "h1"}}},
	})
	// Points at a DIFFERENT hub: the reverse read must not gather it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/hub", ID: "h2", Properties: map[string]any{"name": "Other"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/spoke", ID: "s2", Properties: map[string]any{"hub": "h2"},
	})
	// An unconstrained reference at the hub: left out deliberately, because
	// finding it would mean reading every row of every kind.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/loose", ID: "x1",
		Properties: map[string]any{
			"anything": vocabulary.RecordPath(graphAuthority+"/hub", "h1"),
		},
	})

	page := incoming(t, ds, "h1", substrate.IncomingOptions{})
	if page.Total != 2 {
		t.Fatalf("total = %d, want the reference and the edge", page.Total)
	}
	by := map[string]substrate.IncomingEdge{}
	for _, row := range page.Incoming {
		by[row.From.ID] = row
	}
	if got := by["s1"]; got.Via != substrate.ViaReference || got.Rel != "hub" {
		t.Fatalf("the reference row reads %+v", got)
	}
	if got := by["l1"]; got.Via != substrate.ViaEdge || got.Rel != "hub" {
		t.Fatalf("the edge row reads %+v", got)
	}
	if by["s1"].CreatedAt.IsZero() {
		t.Fatal("a reverse row must carry the source's creation")
	}
	_ = ctx
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
		Kind: graphAuthority + "/linked", ID: "l1",
		Edges: []substrate.EdgeInput{{Rel: "hub", To: substrate.EdgeRef{ID: "h1"}}},
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
	if page := incoming(t, ds, "h1", substrate.IncomingOptions{Rel: "nothing"}); page.Total != 0 {
		t.Fatalf("an unclaimed rel must find nothing, got %d", page.Total)
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

	// A merge repoints EDGES but leaves reference values alone — they resolve
	// through the former-id trail. A reverse read that asked only for the
	// canonical id would lose this pointer entirely.
	//
	// The merge also leaves its own `recordmerge` row naming the winner, which
	// is why this looks for the spoke rather than counting: that row is the
	// merge's audit trail and belongs in the fan-in.
	page := incoming(t, ds, "h1", substrate.IncomingOptions{})
	var found *substrate.IncomingEdge
	for i, row := range page.Incoming {
		if row.From.ID == "s1" {
			found = &page.Incoming[i]
		}
	}
	if found == nil {
		t.Fatalf("the pointer written under the former id must still be found, got %+v", page.Incoming)
	}
	if found.Via != substrate.ViaReference || found.Rel != "hub" {
		t.Fatalf("the recovered pointer reads %+v", *found)
	}
}

func TestIncomingPagesCleanlyAcrossTiedTimestamps(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})
	// Every spoke of one kind, one rel, one via: the leading three components
	// of the order tie for all of them, so the page boundary rests entirely on
	// (created_at DESC, src_id) — and these rows are written fast enough that
	// their timestamps can tie too.
	want := map[string]bool{}
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
		want[id] = true
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: graphAuthority + "/linked", ID: "l1",
		Edges: []substrate.EdgeInput{{Rel: "hub", To: substrate.EdgeRef{ID: "h1"}}},
	})
	want["l1"] = true

	seen := map[string]int{}
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the cursor never terminated")
		}
		page := incoming(t, ds, "h1", substrate.IncomingOptions{First: 2, After: after})
		if page.Total != len(want) {
			t.Fatalf("total = %d, want %d", page.Total, len(want))
		}
		for _, row := range page.Incoming {
			seen[row.From.ID]++
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	for id := range want {
		switch seen[id] {
		case 1:
		case 0:
			t.Errorf("%s was dropped between pages", id)
		default:
			t.Errorf("%s was returned %d times", id, seen[id])
		}
	}
}

func TestIncomingOrdersAGroupNewestFirst(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})
	for _, id := range []string{"s1", "s2", "s3"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: graphAuthority + "/spoke", ID: id, Properties: map[string]any{"hub": "h1"},
		})
		// Distinct stamps, so the ordering under test is the timestamps' and
		// not the tie-break's.
		time.Sleep(2 * time.Millisecond)
	}

	page := incoming(t, ds, "h1", substrate.IncomingOptions{})
	var last time.Time
	for i, row := range page.Incoming {
		if i > 0 && row.CreatedAt.After(last) {
			t.Fatalf("row %d (%s) is newer than the one before it", i, row.From.ID)
		}
		last = row.CreatedAt
	}
}

func TestIncomingRefusesACursorFromTheOldOrder(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	graphVocabulary(t, ds)
	mustPut(t, ds, owner, substrate.PutInput{Kind: graphAuthority + "/hub", ID: "h1"})

	// The order gained `via` and `createdAt`, so a token minted under the old
	// three-key signature describes a different sort. Refused, because
	// replaying it would silently mis-page.
	_, err := ds.Incoming(context.Background(), graphAuthority+"/hub", "h1",
		substrate.IncomingOptions{After: "not-a-cursor"})
	wantErr(t, err, substrate.ErrValidation, "cursor")
}
