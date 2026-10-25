package engine

import (
	"context"
	"errors"
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

// fallBackSearch runs q with scoring forced past a 2s budget and returns the
// hit ids, failing unless the result is marked capped with the fallback note.
func fallBackSearch(t *testing.T, ds *dataset, q string) []string {
	t.Helper()
	const budget = 2 * time.Second
	ds.svc.searchBudget = budget
	ds.svc.searchScoreHook = func() { time.Sleep(budget + budget/10) }
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: q, Mode: substrate.SearchLexical})
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
	if !res.Capped || !strings.HasPrefix(res.Note, "Search ran out of its 2s budget while ranking, so the lexical hits are ordered by ts_rank instead of BM25F.") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}
	return got
}

// Scoring that runs out of the budget answers the candidates in the order
// the passes found them, the records matching every word first and then by
// pre-rank, marked capped, instead of failing a search that found matches.
// The whole match here has the lowest pre-rank of the three, so only the
// tier puts it first.
func TestLexicalSearchFallsBackWhenScoringRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "whole", "errand", "flight booking "+longRepetitive("w"))
	putScoreTask(t, ds, "short", "flight", "")
	putScoreTask(t, ds, "long", "errand", "booking "+longRepetitive("booking"))
	if got := fallBackSearch(t, ds, "flight booking"); strings.Join(got, ",") != "whole,short,long" {
		t.Fatalf("hits = %v, want the whole match, then the short relaxed match above the long one", got)
	}
}

// An exclusion does not flatten the fallback order: the pre-rank scores the
// positive words alone, so a title match ranks above a shorter record that
// holds the word in prose, and the excluded record is absent.
func TestLexicalSearchFallbackRanksAnExclusionQueryByItsWords(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "prose", "errand", "flight")
	putScoreTask(t, ds, "title", "flight plan for the lisbon trip", "")
	putScoreTask(t, ds, "gone", "flight delayed", "")
	if got := fallBackSearch(t, ds, "flight -delayed"); strings.Join(got, ",") != "title,prose" {
		t.Fatalf("hits = %v, want the title match above the prose one, the excluded record absent", got)
	}
}

// A caller that goes away while the search scores gets the error, never a
// fallback ranking.
func TestLexicalSearchFailsWhenTheCallerCancelsDuringScoring(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "a", "flight booking", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ds.svc.searchScoreHook = cancel
	_, err := ds.Search(ctx, substrate.SearchInput{Q: "flight booking", Mode: substrate.SearchLexical})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the caller's cancellation", err)
	}
}

// The pre-rank that picks which candidates are scored does not prefer a long
// document repeating the word over a short one holding it once, in the same
// band: unnormalized, ts_rank put the long one first and it filled the pool.
func TestLexicalPreRankDoesNotRewardLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	// "long" sorts before "short", so a tie would pick it.
	putScoreTask(t, ds, "long", "errand", longRepetitive("flight"))
	putScoreTask(t, ds, "short", "errand", "flight")
	pass := lexicalPass{plan: parseSearch("flight"), readMax: searchReadMax, matchMax: searchReadMax}
	cands, _, _, err := ds.lexicalCandidates(ctx, pass, false, 2)
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

// coldRead is a statement that runs until its context cancels it, on a
// connection of the dataset's own pool: what a cold read of TOASTed vectors
// does to the budget, a real statement canceled and a connection the pool
// must replace before the search's next one.
func coldRead(t *testing.T, ds *dataset) func(context.Context) {
	return func(ctx context.Context) {
		if _, err := ds.db.ExecContext(ctx, `SELECT pg_sleep(60)`); err == nil {
			t.Error("the cold read finished instead of being canceled")
		}
	}
}

// A strict pass whose vectors read cold past its share of the budget still
// answers: the newest matches are scored instead of the best by pre-rank,
// marked capped, where the pass that pre-ranked every match before returning
// one failed the search with nothing ranked (#968).
func TestLexicalSearchScoresTheNewestWhenThePreRankRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "old", "flight", "")
	putScoreTask(t, ds, "new", "errand", "flight "+longRepetitive("w"))
	const budget = 2 * time.Second
	ds.svc.searchBudget = budget
	ds.svc.searchPreRankHook = coldRead(t, ds)
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: "flight", Mode: substrate.SearchLexical, K: 1})
	if err != nil {
		t.Fatalf("a pre-rank past the budget failed the search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Record.ID != "old" {
		t.Fatalf("hits = %+v, want BM25F to rank the title match first", res.Hits)
	}
	if !res.Capped || !strings.Contains(res.Note, "so the newest matches were ranked instead of the closest ones") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}
}

// When scoring runs out of the budget too, the newest matches answer newest
// first.
func TestLexicalSearchAnswersNewestFirstWhenPreRankAndScoringRunOut(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "old", "flight", "")
	putScoreTask(t, ds, "new", "errand", "flight "+longRepetitive("w"))
	const budget = 2 * time.Second
	ds.svc.searchBudget = budget
	ds.svc.searchPreRankHook = func(ctx context.Context) { <-ctx.Done() }
	ds.svc.searchScoreHook = func() { time.Sleep(budget) }
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: "flight", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("a pre-rank and scoring past the budget failed the search: %v", err)
	}
	var got []string
	for _, h := range res.Hits {
		got = append(got, h.Record.ID)
	}
	if strings.Join(got, ",") != "new,old" {
		t.Fatalf("hits = %v, want newest first", got)
	}
	if !res.Capped || !strings.Contains(res.Note, "ordered newest first") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}
}

// A word too common for records_matching is matched on the row, newest first,
// and under the policy that reads every vector the walk passes: cold, the
// identity read alone spent the budget and the search answered 503 before
// any candidate existed (#968). Past the strict pass's share it takes some
// matches through the index instead, marked capped, and skips the top-up.
func TestLexicalSearchTakesIndexedMatchesWhenTheIdentityReadRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t, WithSearchMatchMax(0))
	putScoreTask(t, ds, "old", "flight", "")
	putScoreTask(t, ds, "new", "errand", "flight "+longRepetitive("w"))
	putScoreTask(t, ds, "partial", "flight", "nothing about it")
	const budget = 2 * time.Second
	ds.svc.searchBudget = budget
	ds.svc.searchIdentityHook = coldRead(t, ds)
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: "flight booking", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("an identity read past the budget failed the search: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits = %+v, want none: nothing matches every word and the top-up is skipped", res.Hits)
	}
	if !res.Capped || !strings.Contains(res.Note, "were not searched") || strings.Contains(res.Note, "four fifths") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}

	res, err = ds.Search(context.Background(), substrate.SearchInput{Q: "flight", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("an identity read past the budget failed the search: %v", err)
	}
	var got []string
	for _, h := range res.Hits {
		got = append(got, h.Record.ID)
	}
	if len(got) != 3 {
		t.Fatalf("hits = %v, want every match", got)
	}
	// Fewer matches than the pass keeps: the index answered every one, and
	// BM25F ranked them all.
	if res.Capped {
		t.Fatalf("capped with every match ranked: note = %q", res.Note)
	}
}

// The count that picks a pass's path is a statement under the strict pass's
// share like the reads after it. Through records_matching it could scan the
// table and read every vector (migration 0018), and it ran under the whole
// budget before any other bound: cold, it alone answered 503 (#968). Past the
// share the pass takes its matches through the index instead.
func TestLexicalSearchTakesIndexedMatchesWhenTheCountRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "both", "flight booking", "")
	putScoreTask(t, ds, "partial", "flight", "")
	const budget = 2 * time.Second
	ds.svc.searchBudget = budget
	ds.svc.searchCountHook = coldRead(t, ds)
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: "flight booking", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("a count past the budget failed the search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Record.ID != "both" {
		t.Fatalf("hits = %+v, want the strict match alone", res.Hits)
	}
	if !res.Capped || !strings.Contains(res.Note, "were not searched") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}
}

// Past the share, the index's answer is a sample when more match than the
// pass keeps and every match when no more do, exactly as many included.
func TestLexicalCandidatesPastTheCountSampleOrReadEveryMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	for i := range 3 {
		putScoreTask(t, ds, fmt.Sprintf("t%d", i), "flight", "")
	}
	ds.svc.searchCountHook = coldRead(t, ds)
	for _, tc := range []struct {
		take, n int
		want    candOrder
	}{{2, 2, orderSampled}, {3, 3, orderIndexed}, {10, 3, orderIndexed}} {
		pass := lexicalPass{plan: parseSearch("flight"), readMax: searchReadMax, matchMax: searchReadMax, rankBy: time.Now().Add(time.Second)}
		cands, over, order, err := ds.lexicalCandidates(ctx, pass, false, tc.take)
		if err != nil {
			t.Fatalf("take %d: %v", tc.take, err)
		}
		if len(cands) != tc.n || over || order != tc.want {
			t.Fatalf("take %d: %d candidates, over %v, order %v; want %d, false, %v", tc.take, len(cands), over, order, tc.n, tc.want)
		}
	}
}

// The index's way out has its own deadline inside the budget. A word every
// row holds, searched for a kind few rows are, can overflow the bitmap and
// recheck every row's vector on the pages it marks lossy: cold that read
// alone spent the rest of the budget and the search answered 503 (#968).
// Past the deadline the search answers no hits, capped, saying the matches
// could not be read in time.
func TestLexicalSearchAnswersCappedWhenTheIndexedReadRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	putScoreTask(t, ds, "a", "flight", "")
	const budget = 2 * time.Second
	ds.svc.searchBudget = budget
	ds.svc.searchCountHook = coldRead(t, ds)
	ds.svc.searchSampleHook = coldRead(t, ds)
	began := time.Now()
	res, err := ds.Search(context.Background(), substrate.SearchInput{Q: "flight", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("an indexed read past its deadline failed the search: %v", err)
	}
	if took := time.Since(began); took > budget {
		t.Fatalf("the search took %s, past its %s budget", took, budget)
	}
	if len(res.Hits) != 0 || !res.Capped || !strings.Contains(res.Note, "before it could read any of the matching records in time") {
		t.Fatalf("hits = %d, capped = %v, note = %q", len(res.Hits), res.Capped, res.Note)
	}
}

// The index's way out applies the kinds before its cap: a search naming a
// kind that a small share of the matches hold still finds them.
func TestIndexedCandidatesApplyTheKindsBeforeTheCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	for i := range 5 {
		putScoreTask(t, ds, fmt.Sprintf("t%d", i), "flight", "")
	}
	const person = "samples.substrate.reamde.dev/people/person"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: person, ID: "pilot", Properties: map[string]any{"name": "flight"},
	}); err != nil {
		t.Fatal(err)
	}
	pass := lexicalPass{plan: parseSearch("flight"), types: []string{person}, restrict: true}
	cands, _, err := ds.indexedCandidates(ctx, pass, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].id.Kind != person || cands[0].id.ID != "pilot" {
		t.Fatalf("candidates = %+v, want the person", cands)
	}
	// The count applies them too: one person, six matches.
	b := &builder{}
	few, err := indexedMatch(ctx, ds.db, b, pass.plan.expr(b, false), passKinds(b, pass), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !few {
		t.Fatal("the count of one kind's matches counted every kind's")
	}
	pass.restrict = false
	b = &builder{}
	if few, err = indexedMatch(ctx, ds.db, b, pass.plan.expr(b, false), passKinds(b, pass), 1); err != nil || few {
		t.Fatalf("every kind: few = %v, err = %v, want six matches past one", few, err)
	}
}
