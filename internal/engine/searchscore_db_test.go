package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const scoreTaskKind = "samples.substrate.reamde.dev/tasks/task"

func putScoreTask(t *testing.T, ds *dataset, id, name, description string) {
	t.Helper()
	props := map[string]any{"name": name}
	if description != "" {
		props["description"] = description
	}
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: scoreTaskKind, ID: id, Properties: props,
	}); err != nil {
		t.Fatal(err)
	}
}

// longRepetitive is prose that repeats word among many others: a long Drive
// file or mail that mentions it in passing.
func longRepetitive(word string) string {
	var b strings.Builder
	for i := range 20 {
		fmt.Fprintf(&b, "%s w%d ", word, i)
	}
	for i := 20; i < 600; i++ {
		fmt.Fprintf(&b, "w%d ", i)
	}
	return b.String()
}

// Scoring that runs out of the budget answers the candidates in the order
// the passes found them, the records matching every word first and then by
// pre-rank, marked capped, instead of failing a search that found matches.
func TestLexicalSearchFallsBackWhenScoringRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const budget = 2 * time.Second
	ds := openInternalDataset(t, WithSearchBudget(budget))
	importVocabulary(t, ds, "tasks")
	putScoreTask(t, ds, "both", "flight booking", "")
	putScoreTask(t, ds, "short", "flight", "")
	putScoreTask(t, ds, "long", "errand", "booking "+longRepetitive("booking"))
	ds.svc.searchScoreHook = func() { time.Sleep(budget + budget/10) }
	res, err := ds.Search(ctx, substrate.SearchInput{Q: "flight booking", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("scoring past the budget failed the search: %v", err)
	}
	var got []string
	for _, h := range res.Hits {
		got = append(got, h.Record.ID)
		if h.Lexical <= 0 || h.Lexical >= 1 {
			t.Errorf("%s scored %v, want its pre-rank in (0, 1)", h.Record.ID, h.Lexical)
		}
	}
	if strings.Join(got, ",") != "both,short,long" {
		t.Fatalf("hits = %v, want the strict match, then the short relaxed match above the long one", got)
	}
	if !res.Capped || !strings.HasPrefix(res.Note, "search ran out of its 2s budget while ranking, so the hits are in a cheaper order") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}
}

// The pre-rank that picks which candidates are scored does not prefer a long
// document repeating the word over a short one holding it once, in the same
// band: unnormalized, ts_rank put the long one first and it filled the pool.
func TestLexicalPreRankDoesNotRewardLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	importVocabulary(t, ds, "tasks")
	// "long" sorts before "short", so a tie would pick it.
	putScoreTask(t, ds, "long", "errand", longRepetitive("flight"))
	putScoreTask(t, ds, "short", "errand", "flight")
	pass := lexicalPass{plan: parseSearch("flight"), readMax: searchReadMax, matchMax: searchReadMax}
	cands, _, err := ds.lexicalCandidates(ctx, pass, false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].id.ID != "short" {
		t.Fatalf("candidates = %+v, want short first", cands)
	}
	if cands[0].rank <= cands[1].rank {
		t.Fatalf("pre-ranks short %v, long %v: want short above", cands[0].rank, cands[1].rank)
	}
}
