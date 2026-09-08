package engine_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

func TestLexicalSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "gcal-account:a",
		Properties: map[string]any{"provider": "gcal", "label": "Work"},
	})
	cal := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendar", ID: "gcal-cal:primary",
		Properties: map[string]any{"name": "Primary", "account": enginetest.AccountType + "/" + acc.ID},
	})
	titled := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent", ID: "gcal-event:e1",
		Properties: map[string]any{
			"at": "2026-08-05T13:00:00Z", "endsAt": "2026-08-05T14:00:00Z",
			"summary": "Rack layout review", "description": "Weekly sync",
			"calendar": cal.ID,
		},
	})
	bodied := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent", ID: "gcal-event:e2",
		Properties: map[string]any{
			"at": "2026-08-06T13:00:00Z", "endsAt": "2026-08-06T14:00:00Z",
			"summary": "Standup", "description": "we will discuss the rack layout",
			"calendar": cal.ID,
		},
	})
	mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent", ID: "gcal-event:e3",
		Properties: map[string]any{
			"at": "2026-08-07T13:00:00Z", "endsAt": "2026-08-07T14:00:00Z",
			"summary": "Lunch", "description": "food",
			"calendar": cal.ID,
		},
	})

	hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: "rack layout", Mode: substrate.SearchLexical}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d (%v)", len(hits), hitIDs(hits))
	}
	// Title (band A) outranks prose (band C).
	if hits[0].Record.ID != titled.ID || hits[1].Record.ID != bodied.ID {
		t.Fatalf("band weighting wrong: %v", hitIDs(hits))
	}
	if hits[0].Lexical <= 0 {
		t.Fatalf("raw lexical score missing: %+v", hits[0])
	}
	// Type narrowing.
	hits, err = searchHits(ds.Search(ctx, substrate.SearchInput{
		Q: "rack layout", Mode: substrate.SearchLexical, Kinds: []string{"conversationmessage"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("type filter ignored: %v", hitIDs(hits))
	}
	// Without an embedder, hybrid degrades to lexical rather than failing.
	hits, err = searchHits(ds.Search(ctx, substrate.SearchInput{Q: "rack layout"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hybrid without an embedder = %v", hitIDs(hits))
	}
}

func TestEmbedQueueAndHybridSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	_, ds := newDataset(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "gcal-account:a",
		Properties: map[string]any{"provider": "gcal", "label": "Work"},
	})
	cal := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendar", ID: "gcal-cal:primary",
		Properties: map[string]any{"name": "Primary", "account": enginetest.AccountType + "/" + acc.ID},
	})
	newEvent := func(ext, summary, desc, day string) *substrate.Record {
		return mustPut(t, ds, gcal, substrate.PutInput{
			Kind: "calendarevent", ID: extID("gcal.event", ext),
			Properties: map[string]any{
				"at": "2026-08-" + day + "T13:00:00Z", "endsAt": "2026-08-" + day + "T14:00:00Z",
				"summary": summary, "description": desc,
				"calendar": cal.ID,
			},
		})
	}
	wanted := newEvent("e1", "Standup", "we will discuss the datacentre rack layout", "05")
	newEvent("e2", "Lunch", "sandwiches and coffee", "06")

	n, err := ds.ProcessEmbedQueue(ctx, 10)
	if err != nil {
		t.Fatalf("process embed queue: %v", err)
	}
	if n != 2 {
		t.Fatalf("drained %d queue items, want 2 (one per embed:true property)", n)
	}
	if again, err := ds.ProcessEmbedQueue(ctx, 10); err != nil || again != 0 {
		t.Fatalf("queue not drained: %d %v", again, err)
	}

	hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: "datacentre rack layout", Mode: substrate.SearchSemantic}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Record.ID != wanted.ID {
		t.Fatalf("semantic hits = %v", hitIDs(hits))
	}
	if hits[0].Semantic <= 0.5 {
		t.Fatalf("cosine looks wrong: %v", hits[0].Semantic)
	}
	// Hybrid exposes both raw arms.
	hits, err = searchHits(ds.Search(ctx, substrate.SearchInput{Q: "datacentre rack layout", Mode: substrate.SearchHybrid}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Record.ID != wanted.ID {
		t.Fatalf("hybrid hits = %v", hitIDs(hits))
	}
	if hits[0].Lexical <= 0 || hits[0].Semantic <= 0 {
		t.Fatalf("hybrid should carry both raw scores: %+v", hits[0])
	}

	// Unchanged chunks are not re-embedded.
	long := strings.Repeat("the rack layout is long prose. ", 200)
	ev := newEvent("e3", "Long", long, "07")
	_, before := emb.counts()
	if _, err := ds.ProcessEmbedQueue(ctx, 10); err != nil {
		t.Fatal(err)
	}
	_, after := emb.counts()
	firstChunks := after - before
	if firstChunks < 3 {
		t.Fatalf("expected several chunks, embedded %d", firstChunks)
	}
	mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent", ID: ev.ID, Properties: map[string]any{"description": long + " and a tail"},
	})
	_, before = emb.counts()
	if _, err := ds.ProcessEmbedQueue(ctx, 10); err != nil {
		t.Fatal(err)
	}
	_, after = emb.counts()
	if after-before == 0 {
		t.Fatal("the changed tail chunk should have been re-embedded")
	}
	if after-before >= firstChunks {
		t.Fatalf("unchanged chunks were re-embedded: %d of %d", after-before, firstChunks)
	}
}

// TestHybridSearchSkipsTheSemanticArmWithoutVectors: while the resolved pair
// has no vectors (a restore the drain has not bought yet) there is nothing to
// score, so hybrid does not embed the query at all. A provider that is down
// cannot fail the search whose lexical arm has the answer, no query embedding
// is bought for nothing, and the answer still counts the backlog.
func TestHybridSearchSkipsTheSemanticArmWithoutVectors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var calls atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "embedder down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	_, ds := newDataset(t)
	installShelf(t, ds)
	installEmbedProvider(t, ds, "vectors", down.URL, "text-embedding-3-small")
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{
			"title": "The Work", "description": "alpha unique marmalade prose",
		},
	})

	res, err := ds.Search(ctx, substrate.SearchInput{Q: "marmalade"})
	if err != nil {
		t.Fatalf("hybrid search with the embedder down and no vectors: %v", err)
	}
	if len(res.Hits) == 0 || res.Hits[0].Record.ID != book.ID || res.Pending != 1 {
		t.Fatalf("hybrid = %v, pending %d; want the lexical hit and pending 1", hitIDs(res.Hits), res.Pending)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("hybrid embedded its query %d times with nothing to score against", n)
	}
	// Semantic mode says why instead of dialing the provider.
	_, err = ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchSemantic})
	if !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("semantic search with no vectors = %v, want ErrUnavailable", err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("semantic search dialed the provider %d times with nothing to score against", n)
	}
}

// searchHits unwraps a Search answer to its hits, for the assertions that are
// about the ranking and not about the pending count.
func searchHits(res substrate.SearchResult, err error) ([]substrate.Hit, error) {
	return res.Hits, err
}

func hitIDs(hits []substrate.Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Record.ID)
	}
	return out
}
