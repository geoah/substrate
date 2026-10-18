package engine_test

// A lexical search is bounded: the rows matching every word are gathered
// first, the relaxed top-up runs only when they are fewer than the pool, each
// pass reads a bounded number of matching rows before ranking anything, and
// the whole arm runs under a time budget. A search that stopped short says so
// in band.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const boundTaskKind = "samples.substrate.reamde.dev/tasks/task"

func putBoundTask(t *testing.T, ds substrate.Dataset, id, name string) {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{Kind: boundTaskKind, ID: id, Properties: map[string]any{"name": name}})
}

func lexicalSearch(t *testing.T, ds substrate.Dataset, q string) substrate.SearchResult {
	t.Helper()
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: q, Mode: substrate.SearchLexical, K: 20})
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return res
}

func boundHitIDs(res substrate.SearchResult) []string {
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.Record.ID)
	}
	return out
}

// Under the default read cap a small repository is read whole: every match
// ranks, the rows matching every word first, and nothing says capped.
func TestLexicalSearchRanksStrictMatchesFirstAndTopsUp(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	putBoundTask(t, ds, "both", "flight booking confirmation BA286")
	putBoundTask(t, ds, "flight-only", "flight to Lisbon")
	putBoundTask(t, ds, "booking-only", "booking for dinner")
	putBoundTask(t, ds, "neither", "groceries")
	res := lexicalSearch(t, ds, "flight booking confirmation")
	got := boundHitIDs(res)
	if len(got) != 3 || got[0] != "both" {
		t.Fatalf("hits = %v, want both first, then the two relaxed matches", got)
	}
	if res.Capped || res.Note != "" {
		t.Fatalf("an uncut search says capped=%v note=%q", res.Capped, res.Note)
	}
}

// A pass reads at most the read cap of matching rows, newest first, and says
// it was cut: the strict pass on a word every row holds, and the relaxed
// top-up when the strict pass found fewer than the pool.
func TestLexicalSearchReadsABoundedShareOfTheMatches(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t, engine.WithSearchReadMax(2))
	for i := 1; i <= 5; i++ {
		putBoundTask(t, ds, fmt.Sprintf("common-%d", i), "common errand")
	}
	res := lexicalSearch(t, ds, "common")
	if got := boundHitIDs(res); !equalStrings(slices.Sorted(slices.Values(got)), []string{"common-4", "common-5"}) {
		t.Fatalf("strict pass hits = %v, want the two newest", got)
	}
	if !res.Capped || !strings.Contains(res.Note, "only the newest 2 matches were ranked") {
		t.Fatalf("a cut pass says capped=%v note=%q", res.Capped, res.Note)
	}

	// One row matches every word; the relaxed top-up reads two of the rest.
	putBoundTask(t, ds, "both", "rare common errand")
	res = lexicalSearch(t, ds, "rare errand")
	got := boundHitIDs(res)
	if len(got) != 3 || got[0] != "both" {
		t.Fatalf("hits = %v, want the strict match first and two from the top-up", got)
	}
	for _, id := range got[1:] {
		if id == "common-1" || id == "common-2" || id == "common-3" {
			t.Fatalf("the top-up read %s, older than the two newest relaxed matches: %v", id, got)
		}
	}
	if !res.Capped {
		t.Fatalf("a cut top-up does not say capped: %+v", res)
	}
}

// A budget spent before anything was found is an error naming it, never an
// empty ranking that reads as "no matches".
func TestLexicalSearchPastItsBudgetBeforeAnyMatchFails(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t, engine.WithSearchBudget(time.Nanosecond))
	putBoundTask(t, ds, "a", "flight booking")
	_, err := ds.Search(context.Background(), substrate.SearchInput{Q: "flight booking", Mode: substrate.SearchLexical})
	if !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("err = %v, want unavailable naming the budget", err)
	}
}
