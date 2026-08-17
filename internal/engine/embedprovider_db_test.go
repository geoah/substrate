package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Where a repository buys its vectors is its own data: the one llmprovider row
// that declares embedModel. These tests cover the four states the ticket named
// — no provider at all, a wire with no embeddings endpoint, a model of the
// wrong width, and a re-embed interrupted halfway — plus the one the design
// adds, two rows claiming the job.

// TestEmbedQueueIdlesWithNoProvider: a repository that names no embeddings
// provider is a STATE, not a failure. Nothing refuses to boot, the queue simply
// does not drain, hybrid search returns its lexical arm, and the two verbs that
// need a provider say which row they wanted.
func TestEmbedQueueIdlesWithNoProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installShelf(t, ds)

	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{
			"title": "The Work", "description": "alpha unique marmalade prose",
		},
	})

	// The write enqueued the embed; the drain finds no provider and idles.
	n, err := ds.ProcessEmbedQueue(ctx, 10)
	if err != nil {
		t.Fatalf("drain with no provider: %v", err)
	}
	if n != 0 {
		t.Fatalf("drained %d with no provider configured, want 0", n)
	}

	// Hybrid degrades to lexical rather than failing.
	hits, err := ds.Search(ctx, substrate.SearchInput{Q: "marmalade"})
	if err != nil {
		t.Fatalf("hybrid search with no provider: %v", err)
	}
	if len(hits) == 0 || hits[0].Record.ID != book.ID {
		t.Fatalf("hybrid search fell over instead of degrading: %v", hitIDs(hits))
	}

	// Semantic asked for by name says what is missing.
	_, err = ds.Search(ctx, substrate.SearchInput{Q: "marmalade", Mode: substrate.SearchSemantic})
	if err == nil || !strings.Contains(err.Error(), "embedModel") {
		t.Fatalf("semantic search with no provider: %v", err)
	}
	// So does the re-embed verb.
	if _, err := ds.Reembed(ctx, false); err == nil || !strings.Contains(err.Error(), "embedModel") {
		t.Fatalf("reembed with no provider: %v", err)
	}
}

// TestEmbedProviderRowRefusals: every rule about the embeddings row is held at
// the WRITE, where the person who caused the refusal is present to read it —
// the wire that has no embeddings endpoint, the model whose width the column
// cannot hold, the model nothing knows the width of, and the second row
// claiming a job one row does.
func TestEmbedProviderRowRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	emb := newFakeEmbedServer(t)

	// The anthropic wire sells no embeddings at all.
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: typeProvider, ID: "claude",
		Properties: map[string]any{
			"wire": "anthropic", "apiKey": "row-key-claude",
			"embedModel": "text-embedding-3-small",
		},
	})
	if err == nil || !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("an anthropic row named an embedModel: %v", err)
	}
	if !strings.Contains(err.Error(), "no embeddings endpoint") {
		t.Fatalf("the refusal does not say why: %v", err)
	}

	// 3072 wide, and the column is 1536 (decision record 0026).
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: typeProvider, ID: "wide",
		Properties: map[string]any{
			"wire": "openai", "baseURL": emb.srv.URL, "apiKey": "row-key-wide",
			"embedModel": "text-embedding-3-large",
		},
	})
	if err == nil || !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a 3072-wide model was accepted: %v", err)
	}
	if !strings.Contains(err.Error(), "3072") || !strings.Contains(err.Error(), "1536") {
		t.Fatalf("the width refusal names neither width: %v", err)
	}

	// A model nothing here knows the width of is refused rather than assumed.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: typeProvider, ID: "mystery",
		Properties: map[string]any{
			"wire": "openai", "baseURL": emb.srv.URL, "apiKey": "row-key-mystery",
			"embedModel": "some-local-embedder",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "text-embedding-3-small") {
		t.Fatalf("an unknown model was accepted, or the refusal lists nothing to use instead: %v", err)
	}

	// One row does the job. A second claiming it names the first.
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: typeProvider, ID: "vectors2",
		Properties: map[string]any{
			"wire": "openai", "baseURL": emb.srv.URL, "apiKey": "row-key-vectors2",
			"embedModel": "text-embedding-3-small",
		},
	})
	if err == nil || !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a second row claimed the embeddings job: %v", err)
	}
	if !strings.Contains(err.Error(), "vectors") {
		t.Fatalf("the refusal does not name the row that already holds it: %v", err)
	}
}

// TestProviderRowsAreOutsideTheMergeSurface: merge is the one verb that could
// get a SECOND live claimant past the write. It does not migrate properties,
// so a merged-away llmprovider row keeps its embedModel while another row is
// free to take the job, and split would then restore it beside that row by
// clearing the tombstone and folding directly.
//
// That sequence cannot start: llmprovider is core's, and the generic merge
// surface refuses every core kind. A recordmerge naming one cannot be forged
// either, because recordmerge is a system kind no external write may create.
// The split path carries the claim check anyway (merge.go), so the invariant
// does not silently depend on guardMergeType continuing to exclude core kinds.
func TestProviderRowsAreOutsideTheMergeSurface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	emb := newFakeEmbedServer(t)

	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: typeProvider, ID: "completions",
		Properties: map[string]any{
			"name": "completions", "wire": "openai",
			"baseURL": emb.srv.URL, "apiKey": "row-key-completions",
		},
	}); err != nil {
		t.Fatalf("put the completions row: %v", err)
	}

	_, err := ds.Merge(ctx, owner, typeProvider, "completions", "vectors")
	if err == nil || !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an llmprovider row was merged: %v", err)
	}

	// And the merge record that a split would need cannot be written by hand.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/recordmerge", ID: "forged",
	})
	if err == nil || !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a recordmerge was forged: %v", err)
	}

	// The one claimant still answers, so nothing above changed resolution.
	report, err := ds.Reembed(ctx, false)
	if err != nil || report.Provider != "vectors" {
		t.Fatalf("reembed = %+v, %v", report, err)
	}
}

// TestEmbedQueueRefusesWrongWidth: the write-time check reads a table of model
// names, so an endpoint serving something else under a known name is the one
// way past it. The drain refuses the answer rather than letting Postgres refuse
// the insert with no model in the message.
func TestEmbedQueueRefusesWrongWidth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installShelf(t, ds)
	emb := newFakeEmbedServer(t)
	emb.width = 768
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{
			"title": "The Work", "description": "alpha unique marmalade prose",
		},
	})
	_, err := ds.ProcessEmbedQueue(ctx, 10)
	if err == nil || !strings.Contains(err.Error(), "768") {
		t.Fatalf("a 768-wide answer was stored under a 1536-wide model: %v", err)
	}
}

// TestReembedReplacesVectorsAndResumes: change the model and the vectors
// already stored came from a different one. Search stops scoring them
// immediately — a half-re-embedded repository returns fewer hits, never a
// ranking mixed across two models — and the re-embed verb queues their
// replacement, one drain at a time, resuming after an interruption because the
// queue is the only state it has.
func TestReembedReplacesVectorsAndResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installShelf(t, ds)
	emb := newFakeEmbedServer(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")

	titles := []string{
		"alpha unique marmalade prose", "beta zeppelin narrative here",
		"gamma tangerine dictionary volume",
	}
	ids := make([]string, 0, len(titles))
	for i, desc := range titles {
		row := mustPut(t, ds, owner, substrate.PutInput{
			Kind: "book", Properties: map[string]any{
				"title": "Book " + string(rune('A'+i)), "description": desc,
			},
		})
		ids = append(ids, row.ID)
	}
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != len(titles) {
		t.Fatalf("first drain = %d, %v, want %d, nil", n, err, len(titles))
	}
	if got := semanticIDs(t, ds, "marmalade prose"); len(got) == 0 || got[0] != ids[0] {
		t.Fatalf("the first model's vectors are not searchable: %v", got)
	}

	// The owner re-points the row at another model. Nothing re-embeds by
	// itself: the old vectors are simply no longer the resolved pair's, so
	// they stop being scored.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: typeProvider, ID: "vectors",
		Properties: map[string]any{"embedModel": "text-embedding-ada-002"},
	})
	if got := semanticIDs(t, ds, "marmalade prose"); len(got) != 0 {
		t.Fatalf("vectors from the old model were still scored: %v", got)
	}

	report, err := ds.Reembed(ctx, false)
	if err != nil {
		t.Fatalf("reembed: %v", err)
	}
	if report.Model != "text-embedding-ada-002" || report.Provider != "vectors" {
		t.Fatalf("reembed resolved %q/%q", report.Provider, report.Model)
	}
	if report.Enqueued != len(titles) {
		t.Fatalf("reembed queued %d properties, want %d", report.Enqueued, len(titles))
	}

	// INTERRUPTED: one batch of one, and then nothing. The repository is now
	// half re-embedded, and a search over it returns only the record the new
	// model has seen — fewer hits, never two models' vectors ranked together.
	if n, err := ds.ProcessEmbedQueue(ctx, 1); err != nil || n != 1 {
		t.Fatalf("interrupted drain = %d, %v, want 1, nil", n, err)
	}
	half := semanticIDs(t, ds, "marmalade prose")
	if len(half) != 1 {
		t.Fatalf("a half-re-embedded repository scored %d records, want 1: %v", len(half), half)
	}

	// RESUMED: the queue is the state, so a later pass finishes the job.
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != len(titles)-1 {
		t.Fatalf("resumed drain = %d, %v, want %d, nil", n, err, len(titles)-1)
	}
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 0 {
		t.Fatalf("the queue did not drain: %d %v", n, err)
	}
	all := semanticIDs(t, ds, "marmalade prose")
	if len(all) != len(titles) {
		t.Fatalf("after the re-embed %d records are searchable, want %d: %v", len(all), len(titles), all)
	}
	if all[0] != ids[0] {
		t.Fatalf("the new model's ranking is wrong: %v", all)
	}

	// --all ignores the provenance, which is the answer to a gateway swapped
	// behind an unchanged row and model.
	forced, err := ds.Reembed(ctx, true)
	if err != nil {
		t.Fatalf("reembed --all: %v", err)
	}
	if forced.Enqueued != len(titles) || !forced.All {
		t.Fatalf("reembed --all queued %d (all=%v), want %d", forced.Enqueued, forced.All, len(titles))
	}
}

func semanticIDs(t *testing.T, ds substrate.Dataset, q string) []string {
	t.Helper()
	hits, err := ds.Search(context.Background(), substrate.SearchInput{Q: q, Mode: substrate.SearchSemantic})
	if err != nil {
		t.Fatalf("semantic search %q: %v", q, err)
	}
	return hitIDs(hits)
}
