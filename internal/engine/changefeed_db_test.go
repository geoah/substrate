package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The cross-collection feed seam (changefeed.go): newest-first history
// paging, the q substring filter, and per-change trigger states.

// feedOps is the seam the API asserts at runtime; tests reach it the same way.
type feedOps interface {
	ChangesBefore(ctx context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error)
	ChangeTriggers(ctx context.Context, changes []substrate.Change) (map[int64][]substrate.ChangeTrigger, error)
}

func feedOf(t *testing.T, ds substrate.Dataset) feedOps {
	t.Helper()
	feed, ok := ds.(feedOps)
	if !ok {
		t.Fatal("dataset does not implement the change-feed seam")
	}
	return feed
}

func TestChangesBeforePagesNewestFirst(t *testing.T) {
	ds, _ := newFnDataset(t, nil)
	feed := feedOf(t, ds)
	ctx := context.Background()

	for _, name := range []string{"one", "two", "three"} {
		mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": name}})
	}
	forward, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(forward) < 3 {
		t.Fatalf("changelog rows: %d", len(forward))
	}
	head := forward[len(forward)-1].Seq

	// before=0 reads from the head; each page continues strictly below the
	// previous page's oldest row.
	page, err := feed.ChangesBefore(ctx, 0, substrate.ChangeFilter{}, 2)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	if len(page) != 2 || page[0].Seq != head || page[1].Seq != head-1 {
		t.Fatalf("first page seqs = %v, want %d,%d", seqsOf(page), head, head-1)
	}
	rest, err := feed.ChangesBefore(ctx, page[1].Seq, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	if len(rest) != len(forward)-2 || rest[0].Seq != head-2 {
		t.Fatalf("second page seqs = %v", seqsOf(rest))
	}
}

func seqsOf(changes []substrate.Change) []int64 {
	out := make([]int64, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Seq)
	}
	return out
}

func TestChangesQSubstringFilter(t *testing.T) {
	ds, _ := newFnDataset(t, nil)
	feed := feedOf(t, ds)
	ctx := context.Background()

	ada := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"assignee": "kim"}})
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: gadgetType, Properties: map[string]any{"count": 2.0}})

	// Case-insensitive over payload text. The payload is a delta CARRYING
	// VALUES now, so the haystack is what was written, not
	// just the property name: "kim" appears in exactly one row's payload, and
	// the feed's one search box finds a change by the value it wrote.
	hits, err := feed.ChangesBefore(ctx, 0, substrate.ChangeFilter{Q: "KIM"}, 500)
	if err != nil {
		t.Fatalf("q filter: %v", err)
	}
	if len(hits) != 1 || hits[0].RecordID != ada.ID {
		t.Fatalf("q=KIM hits = %+v", hits)
	}
	// …and over the record id, on the forward read the watch drains too.
	hits, err = ds.Changes(ctx, 0, substrate.ChangeFilter{Q: ada.ID}, 500)
	if err != nil {
		t.Fatalf("q filter: %v", err)
	}
	if len(hits) != 1 || hits[0].RecordID != ada.ID {
		t.Fatalf("q=id hits = %+v", hits)
	}
	// LIKE metacharacters are literals: nothing here contains a percent sign.
	hits, err = feed.ChangesBefore(ctx, 0, substrate.ChangeFilter{Q: "%"}, 500)
	if err != nil {
		t.Fatalf("q filter: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("q=%% hits = %+v", hits)
	}
}

func TestChangeTriggersStates(t *testing.T) {
	// The mirror errors on a widget without a name (record.properties.name),
	// which is what parks a delivery; taskType is in the source so the
	// function's own task writes exercise self-actor exclusion.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType, taskType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	feed := feedOf(t, ds)
	ctx := context.Background()
	const mirror = fnAuthority + "/mirror"

	processed := mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "fine"}})
	poisoned := mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType})
	process(t, ops)
	pending := mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "later"}})
	unmatched := mustPut(t, ds, owner, substrate.PutInput{Kind: gadgetType, Properties: map[string]any{"count": 1.0}})

	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	states, err := feed.ChangeTriggers(ctx, changes)
	if err != nil {
		t.Fatalf("change triggers: %v", err)
	}

	chipOf := func(recordID string, actor substrate.Actor) (substrate.ChangeTrigger, bool) {
		for _, ch := range changes {
			if ch.RecordID != recordID || ch.Actor != actor {
				continue
			}
			for _, ct := range states[ch.Seq] {
				if ct.Trigger == trigID("mirror") {
					return ct, true
				}
			}
		}
		return substrate.ChangeTrigger{}, false
	}

	if ct, ok := chipOf(processed.ID, owner); !ok || ct.State != substrate.ChangeTriggerProcessed || ct.Callable != mirror {
		t.Fatalf("processed chip = %+v (%v)", ct, ok)
	}
	if ct, ok := chipOf(poisoned.ID, owner); !ok || ct.State != substrate.ChangeTriggerParked || ct.Error == "" {
		t.Fatalf("parked chip = %+v (%v)", ct, ok)
	}
	if ct, ok := chipOf(pending.ID, owner); !ok || ct.State != substrate.ChangeTriggerPending {
		t.Fatalf("pending chip = %+v (%v)", ct, ok)
	}
	// A gadget never fires the mirror: no chip at all, not a fourth state.
	if ct, ok := chipOf(unmatched.ID, owner); ok {
		t.Fatalf("unmatched change carries a chip: %+v", ct)
	}
	// The callable's own task write matches the source by type but is its
	// own echo: self-actor exclusion drops the chip.
	if ct, ok := chipOf("t-"+processed.ID, substrate.FunctionActor(vocabulary.KindName(mirror))); ok {
		t.Fatalf("self write carries a chip: %+v", ct)
	}
}
