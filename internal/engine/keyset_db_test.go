package engine_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// TestListKeysetWalkSeesEachRowOnce is the stability guarantee: a
// cursor walk sees every row that existed for the WHOLE walk exactly once,
// even when rows are inserted and deleted mid-walk. An OFFSET walk would skip
// a row when one behind the cursor is deleted, or repeat one when a newer row
// is inserted; keyset seeks a position in the order, so neither happens.
func TestListKeysetWalkSeesEachRowOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// 17 rows up front — the set that exists for the whole walk (until some
	// are deleted below, which removes them from that set).
	stable := map[string]bool{}
	for i := range 17 {
		p := mustPut(t, ds, owner, substrate.PutInput{
			Kind: "person", Properties: map[string]any{"name": fmt.Sprintf("p%02d", i)},
		})
		stable[p.ID] = true
	}

	seen := map[string]int{}
	after := ""
	mutated := false
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(ctx, substrate.Query{
			Filter: substrate.Filter{Kinds: []string{"person"}},
			First:  4,
			After:  after,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, e := range page.Records {
			seen[e.ID]++
		}
		// After the first page, churn the collection: delete three of the
		// original rows (seen or not) and insert four new ones.
		if !mutated {
			mutated = true
			del := 0
			for id := range stable {
				if del >= 3 {
					break
				}
				if _, err := ds.Delete(ctx, owner, "person", id); err != nil {
					t.Fatalf("delete: %v", err)
				}
				delete(stable, id) // no longer exists for the whole walk
				del++
			}
			for i := range 4 {
				mustPut(t, ds, owner, substrate.PutInput{
					Kind: "person", Properties: map[string]any{"name": fmt.Sprintf("n%02d", i)},
				})
			}
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}

	for id := range stable {
		if seen[id] != 1 {
			t.Fatalf("stable row %s seen %d times, want exactly 1", id, seen[id])
		}
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("row %s seen %d times (a duplicate)", id, n)
		}
	}
}

// TestListCarriesHeadForGaplessWatch is the list→watch handoff:
// every listed row's change is at or before the page's head seq, and a write
// made AFTER the list resumes at seq > head — so `watch?from=head` replays
// exactly the writes the list did not see, with no gap and no double-see.
func TestListCarriesHeadForGaplessWatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	for i := range 3 {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: "person", Properties: map[string]any{"name": fmt.Sprintf("p%d", i)},
		})
	}

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{"person"}}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Head <= 0 {
		t.Fatalf("head = %d, want > 0", page.Head)
	}
	listed := map[string]bool{}
	for _, e := range page.Records {
		listed[e.ID] = true
	}

	// Every listed row's creating change is at or before head (no listed row
	// hides beyond the resume point).
	all, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 1000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	for _, c := range all {
		if listed[c.RecordID] && c.Seq > page.Head {
			t.Fatalf("listed row %s has change seq %d beyond head %d", c.RecordID, c.Seq, page.Head)
		}
	}

	// A write after the list resumes strictly past head.
	late := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "late"},
	})
	resumed, err := ds.Changes(ctx, page.Head, substrate.ChangeFilter{}, 1000)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	sawLate := false
	for _, c := range resumed {
		if c.Seq <= page.Head {
			t.Fatalf("resume from head saw seq %d <= head %d (a double-see)", c.Seq, page.Head)
		}
		if c.RecordID == late.ID {
			sawLate = true
		}
	}
	if !sawLate {
		t.Fatalf("resume from head missed the late write %s (a gap)", late.ID)
	}
}
