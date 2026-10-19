package engine_test

// The semantic arm's index and its bounds (decision 0174): the candidate read
// is one the HNSW index over half-precision copies serves, and a hybrid
// search whose semantic arm runs out of its budget or loses its embeddings
// provider answers from its words, with a note, while semantic mode errs.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// The candidate read's ORDER BY spells the index's expression, so the planner
// can answer it from embeddings_vec_hnsw_idx, with and without a kind filter.
// Sequential scans and sorts are priced out (SemanticCandidatePlan) because
// on an empty table either is cheaper than the graph; what is asserted is
// that the index can serve the read at all, which a mismatched cast or
// operator would make impossible.
func TestSemanticCandidateReadUsesTheHNSWIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	for _, kinds := range [][]string{nil, {"shelf.test.dev/shelf/book"}} {
		plan, err := engine.SemanticCandidatePlan(ctx, ds, kinds, 40)
		if err != nil {
			t.Fatalf("explain the candidate read (kinds %v): %v", kinds, err)
		}
		if !strings.Contains(plan, "embeddings_vec_hnsw_idx") {
			t.Fatalf("the candidate read (kinds %v) does not use the HNSW index:\n%s", kinds, plan)
		}
	}
}

// semanticFixture is a repository with an embeddings provider whose endpoint
// a test can take down, and two books the drain has embedded: one the query
// `marmalade` matches by its words.
type semanticFixture struct {
	ds   substrate.Dataset
	down *atomic.Bool
	book string
}

func newSemanticFixture(t *testing.T, opts ...engine.Option) semanticFixture {
	t.Helper()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	down := &atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "embedder down", http.StatusInternalServerError)
			return
		}
		emb.handle(w, r)
	}))
	t.Cleanup(srv.Close)
	_, ds := newDataset(t, opts...)
	installShelf(t, ds)
	installEmbedProvider(t, ds, "vectors", srv.URL, "text-embedding-3-small")
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "shelf.test.dev/shelf/book", Properties: map[string]any{
			"title": "The Work", "description": "alpha unique marmalade prose",
		},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "shelf.test.dev/shelf/book", Properties: map[string]any{
			"title": "Other", "description": "sandwiches and coffee",
		},
	})
	if n, err := ds.ProcessEmbedQueue(ctx, 10); err != nil || n != 2 {
		t.Fatalf("drain = %d, %v; want 2", n, err)
	}
	return semanticFixture{ds: ds, down: down, book: book.ID}
}

// assertWordsOnly checks a hybrid answer that fell back to its lexical arm:
// the lexical hit, no semantic score, not capped, and a note saying why.
func assertWordsOnly(t *testing.T, res substrate.SearchResult, err error, book, why string) {
	t.Helper()
	if err != nil {
		t.Fatalf("hybrid search with the semantic arm down: %v", err)
	}
	if len(res.Hits) == 0 || res.Hits[0].Record.ID != book {
		t.Fatalf("hybrid hits = %v, want the lexical hit %s", hitIDs(res.Hits), book)
	}
	for _, h := range res.Hits {
		if h.Semantic != 0 {
			t.Fatalf("hit %s carries a semantic score %v from an arm that did not answer", h.Record.ID, h.Semantic)
		}
	}
	if res.Capped {
		t.Fatal("a fallen-back semantic arm marked the search capped")
	}
	if !strings.Contains(res.Note, why) || !strings.Contains(res.Note, "ranked by their words only") {
		t.Fatalf("note = %q, want it to say %q and that the ranking is by words only", res.Note, why)
	}
}

// An embeddings provider that fails to embed the query does not fail a hybrid
// search: it answers from the lexical arm and says so. Semantic mode has no
// other arm, so it returns the error.
func TestHybridSearchFallsBackToWordsWhenTheQueryEmbedFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newSemanticFixture(t)
	f.down.Store(true)

	res, err := f.ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchHybrid})
	assertWordsOnly(t, res, err, f.book, `llm/provider "vectors" failed to embed the query`)
	if strings.Contains(res.Note, "row-key-vectors") {
		t.Fatalf("the note echoes the provider's key: %q", res.Note)
	}

	_, err = f.ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchSemantic})
	if err == nil || !strings.Contains(err.Error(), "embed query") {
		t.Fatalf("semantic search with the embedder down = %v, want the embed error", err)
	}

	// The provider back, the arm is back.
	f.down.Store(false)
	res, err = f.ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchHybrid})
	if err != nil || res.Note != "" || len(res.Hits) == 0 || res.Hits[0].Semantic <= 0 {
		t.Fatalf("hybrid with the embedder back = %v, note %q, err %v; want both arms and no note", hitIDs(res.Hits), res.Note, err)
	}
}

// A semantic arm that runs past the search budget does not fail a hybrid
// search: it answers from the lexical arm and names the budget. Semantic mode
// answers unavailable, naming it too.
func TestHybridSearchFallsBackToWordsWhenTheSemanticArmRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const budget = 2 * time.Second
	var stall atomic.Bool
	f := newSemanticFixture(t,
		engine.WithSearchBudget(budget),
		engine.WithTestSemanticHook(func(ctx context.Context) {
			if stall.Load() {
				<-ctx.Done()
			}
		}))
	stall.Store(true)

	res, err := f.ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchHybrid})
	assertWordsOnly(t, res, err, f.book, "did not finish within the 2s search budget")

	_, err = f.ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchSemantic})
	if !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(err.Error(), "2s budget") {
		t.Fatalf("semantic search past its budget = %v, want ErrUnavailable naming the budget", err)
	}
}

// A caller that goes away while the semantic arm runs gets its cancellation,
// never a words-only answer: nobody is waiting for one.
func TestSearchFailsWhenTheCallerCancelsDuringTheSemanticArm(t *testing.T) {
	t.Parallel()
	var cancel atomic.Pointer[context.CancelFunc]
	f := newSemanticFixture(t, engine.WithTestSemanticHook(func(context.Context) {
		if c := cancel.Load(); c != nil {
			(*c)()
		}
	}))
	for _, mode := range []substrate.SearchMode{substrate.SearchHybrid, substrate.SearchSemantic} {
		ctx, c := context.WithCancel(context.Background())
		cancel.Store(&c)
		_, err := f.ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: mode})
		c()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s search canceled mid-arm = %v, want the caller's cancellation", mode, err)
		}
	}
}
