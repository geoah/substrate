package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// TestEmbedQueueDoesNotPublishStaleVectors: a property edited DURING a slow
// embed must not lose the edit, and the vectors of the superseded text must not
// win. The worker snapshots the queue generation before it embeds; an edit
// re-enqueues and bumps that generation; the finalize sees the bump, leaves the
// newer job pending and writes nothing. A second drain then embeds the current
// text and publishes it.
func TestEmbedQueueDoesNotPublishStaleVectors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds)
	installShelf(t, ds)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")

	// v1: a book whose blurb is embeddable. Putting it enqueues the embed.
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{
			"title": "The Work", "description": "alpha unique marmalade prose",
		},
	})

	// The edit lands mid-embed: while the worker is embedding v1, the blurb is
	// rewritten to v2, which re-enqueues and bumps the generation.
	emb.setHook(func() {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: "book", ID: book.ID, Properties: map[string]any{
				"description": "beta different zeppelin narrative",
			},
		})
	})

	// First drain: the worker embeds v1, but the generation moved, so nothing
	// is applied.
	n, err := ds.ProcessEmbedQueue(ctx, 10)
	if err != nil {
		t.Fatalf("first drain: %v", err)
	}
	if n != 0 {
		t.Fatalf("a stale job was applied (%d); the edit's generation must win", n)
	}

	repoID := testdb.RepositoryID(t, dsn, "geoah")
	raw, err := engine.OpenScopedDB(dsn, repoID, engine.RoleApp)
	if err != nil {
		t.Fatalf("open scoped pool: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	// The edit was not lost: its job is still queued.
	var queued int
	if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM embed_queue`).Scan(&queued); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	if queued != 1 {
		t.Fatalf("embed_queue holds %d jobs after a mid-flight edit, want 1 (the newer generation, still pending)", queued)
	}
	// The stale text's vectors never landed.
	var vecs int
	if err := raw.QueryRowContext(ctx,
		`SELECT count(*) FROM embeddings WHERE record_id = $1`, book.ID).Scan(&vecs); err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if vecs != 0 {
		t.Fatalf("the stale run published %d vectors; none of the superseded text may win", vecs)
	}

	// Second drain: the current text embeds and publishes, and the queue drains.
	emb.setHook(nil)
	if n, err := ds.ProcessEmbedQueue(ctx, 10); err != nil || n != 1 {
		t.Fatalf("second drain = %d, %v, want 1, nil", n, err)
	}
	hits, err := ds.Search(ctx, substrate.SearchInput{Q: "zeppelin narrative", Mode: substrate.SearchSemantic})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || hits[0].Record.ID != book.ID {
		t.Fatalf("the current text is not searchable after the second drain: %v", hitIDs(hits))
	}
}
