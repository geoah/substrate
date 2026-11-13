package engine_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A rebuild keeps the vectors (rebuild.go), and it replays every purge. A
// record purged and written again at the same id holds vectors bought for its
// second life, and the replayed purge must not take them: nothing in the
// replay queues them again, so the record would drop out of semantic search
// until somebody re-embedded the repository by hand.
func TestARebuildKeepsTheVectorsOfAnIdWrittenAgainAfterItsPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	const pkg, kind = "ada.example.com/journal", "ada.example.com/journal/entry"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "entry"}, map[string]any{
			"properties": map[string]any{"text": map[string]any{"type": "string", "embed": true}},
		}),
	}); err != nil {
		t.Fatalf("declare the entry kind: %v", err)
	}
	emb := newFakeEmbedServer(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")

	entry := mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, Properties: map[string]any{"text": "alpha unique marmalade prose"},
	})
	drain(t, ds)
	raw := scopedDB(t, dsn, testdb.Repository(t))
	if vectorsOf(t, raw, entry.ID) == 0 {
		t.Fatal("the first write bought no vectors")
	}
	if _, err := ds.Delete(ctx, owner, kind, entry.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n := vectorsOf(t, raw, entry.ID); n != 0 {
		t.Fatalf("the purge left %d vectors of the record", n)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: entry.ID, Properties: map[string]any{"text": "beta zeppelin narrative here"},
	})
	drain(t, ds)

	before := vectorsOf(t, raw, entry.ID)
	if before == 0 {
		t.Fatal("the record written again holds no vectors before the rebuild")
	}
	if got := semanticIDs(t, ds, "beta zeppelin narrative here"); !slices.Contains(got, entry.ID) {
		t.Fatalf("semantic search before the rebuild = %v, want %s", got, entry.ID)
	}

	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := vectorsOf(t, raw, entry.ID); after != before {
		t.Fatalf("the record holds %d vectors after the rebuild, want the %d it held before", after, before)
	}
	if got := semanticIDs(t, ds, "beta zeppelin narrative here"); !slices.Contains(got, entry.ID) {
		t.Fatalf("semantic search after the rebuild = %v, want %s", got, entry.ID)
	}
}

func drain(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if _, err := ds.ProcessEmbedQueue(context.Background(), 20); err != nil {
		t.Fatalf("drain the embed queue: %v", err)
	}
}

func vectorsOf(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM embeddings WHERE record_id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count the vectors of %s: %v", id, err)
	}
	return n
}

// The replay leaves a purged record's vectors to whatever runs after it, and
// the import's reconcile walks the kinds the registry knows. An older
// database restored beside a newer directory, in which a record was purged
// and its kind then removed, holds vectors and a queued embed of a record
// nothing folds: they go with the import, whatever the kind's admission.
func TestAnImportDropsTheVectorsOfARecordPurgedUnderAKindNoLongerDeclared(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	const pkg, kind = "ada.example.com/journal", "ada.example.com/journal/entry"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "entry"}, map[string]any{
			"properties": map[string]any{"text": map[string]any{"type": "string", "embed": true}},
		}),
	}); err != nil {
		t.Fatalf("declare the entry kind: %v", err)
	}
	emb := newFakeEmbedServer(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	entry := mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, Properties: map[string]any{"text": "alpha unique marmalade prose"},
	})
	drain(t, ds)
	// Queued after the drain: the older database holds a vector and a queue
	// row of the record.
	mustPatch(t, ds, owner, kind, entry.ID, substrate.PatchInput{Properties: map[string]any{"text": "beta zeppelin narrative here"}})
	raw := scopedDB(t, dsn, testdb.Repository(t))
	if vectorsOf(t, raw, entry.ID) == 0 || queuedOf(t, raw, entry.ID) == 0 {
		t.Fatal("the older database holds no vector or no queued embed of the record")
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The directory moves on in another database: the record is purged, and
	// then its kind is removed.
	root2 := copyRepositoryDir(t, root, id)
	svc2 := mustReopen(t, engine.MigratedDSN(t), root2)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds2.Delete(ctx, owner, kind, entry.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := ds2.Delete(ctx, owner, "substrate.reamde.dev/core/kind", kind, substrate.DeleteInput{}); err != nil {
		t.Fatalf("remove the kind: %v", err)
	}
	_ = svc2.Close()

	svc3 := reopenBesideNewerDirectory(t, dsn, root2, id)
	if _, err := svc3.Dataset(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("open the repository restored over the older database: %v", err)
	}
	if n, q := vectorsOf(t, raw, entry.ID), queuedOf(t, raw, entry.ID); n != 0 || q != 0 {
		t.Fatalf("the purged record kept %d vectors and %d queued embeds through the import", n, q)
	}
}

func queuedOf(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM embed_queue WHERE record_id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count the queued embeds of %s: %v", id, err)
	}
	return n
}
