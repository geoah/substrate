package engine_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The vectors and the embed queue are Postgres-only, and the repository
// directory is the backup unit (decision 0051), so a repository restored into
// an empty database has neither. The import queues every embeddable property
// in the transaction that completes its fold, at generation 1, and the drain
// buys the vectors back; until it has, semantic search returns ErrUnavailable
// with the pending count rather than an empty answer.
func TestImportQueuesEveryEmbeddableProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	svc, ds := shelfRepository(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	ids := putBooks(t, ds)
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != len(ids) {
		t.Fatalf("drain = %d, %v, want %d, nil", n, err, len(ids))
	}
	want := semanticIDs(t, ds, "marmalade prose")
	if len(want) != len(ids) || want[0] != ids[0] {
		t.Fatalf("semantic search before the copy = %v", want)
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	raw := scopedDB(t, dsn2, "geoah")
	assertQueued(t, raw, ids)
	if n := countRows(t, raw, "embeddings"); n != 0 {
		t.Fatalf("the restore brought %d vectors; the directory holds none", n)
	}

	// Before the drain semantic search returns ErrUnavailable with the count,
	// not an empty answer.
	_, err = searchHits(ds2.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic}))
	if !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(err.Error(), "3 properties pending") {
		t.Fatalf("semantic search before the drain = %v, want ErrUnavailable naming 3 pending", err)
	}
	// Hybrid still answers with its lexical arm, and says how much the
	// semantic arm is missing.
	res, err := ds2.Search(ctx, substrate.SearchInput{Q: "marmalade"})
	if err != nil || len(res.Hits) == 0 || res.Hits[0].Record.ID != ids[0] || res.Pending != len(ids) {
		t.Fatalf("hybrid search before the drain = %v, pending %d, %v; want pending %d", hitIDs(res.Hits), res.Pending, err, len(ids))
	}

	// Mid-drain the semantic arm answers over a partial index and the answer
	// carries the backlog, so a caller can tell partial coverage from full.
	if n, err := ds2.ProcessEmbedQueue(ctx, 1); err != nil || n != 1 {
		t.Fatalf("first drained batch = %d, %v, want 1, nil", n, err)
	}
	res, err = ds2.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic})
	if err != nil || res.Pending != len(ids)-1 {
		t.Fatalf("semantic search mid-drain = %+v, %v; want pending %d", res, err, len(ids)-1)
	}
	if n, err := ds2.ProcessEmbedQueue(ctx, 20); err != nil || n != len(ids)-1 {
		t.Fatalf("drain after the restore = %d, %v, want %d, nil", n, err, len(ids)-1)
	}
	if n := countRows(t, raw, "embed_queue"); n != 0 {
		t.Fatalf("%d queue rows outlived the drain", n)
	}
	res, err = ds2.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic})
	if err != nil || res.Pending != 0 {
		t.Fatalf("semantic search after the drain = %+v, %v; want pending 0", res, err)
	}
	got := hitIDs(res.Hits)
	if !sameSet(got, want) || got[0] != want[0] {
		t.Fatalf("semantic search after the restore = %v, want %v", got, want)
	}
}

// With no llmprovider row the import queues the properties all the same, and
// they stay pending: the drain idles over them, semantic search returns the
// error naming the missing row, and the first row that resolves buys them.
func TestImportQueuesEmbedsWithoutAProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := shelfRepository(t)
	ids := putBooks(t, ds)
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 0 {
		t.Fatalf("drain with no provider = %d, %v, want 0, nil", n, err)
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	svc2 := mustReopen(t, dsn2, root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	raw := scopedDB(t, dsn2, "geoah")
	assertQueued(t, raw, ids)
	if n, err := ds2.ProcessEmbedQueue(ctx, 20); err != nil || n != 0 {
		t.Fatalf("drain with no provider after the restore = %d, %v, want 0, nil", n, err)
	}
	assertQueued(t, raw, ids)
	_, err = searchHits(ds2.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic}))
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "embedModel") {
		t.Fatalf("semantic search with no provider = %v, want the missing row named", err)
	}

	emb := newFakeEmbedServer(t)
	installEmbedProvider(t, ds2, "vectors", emb.srv.URL, "text-embedding-3-small")
	if n, err := ds2.ProcessEmbedQueue(ctx, 20); err != nil || n != len(ids) {
		t.Fatalf("drain once a row resolves = %d, %v, want %d, nil", n, err, len(ids))
	}
	got := semanticIDs(t, ds2, "marmalade prose")
	if !sameSet(got, ids) || got[0] != ids[0] {
		t.Fatalf("semantic search once the row resolves = %v, want %v", got, ids)
	}
}

// A boot that dies between the two fold passes leaves the import-progress
// marker and no queue rows: the queue is written by the transaction that
// clears the marker, so the resumed import is what queues them.
func TestAResumedImportQueuesEmbeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := shelfRepository(t)
	ids := putBooks(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := testdb.NewSchema(t)
	errKilled := errors.New("the process died here")
	_, err := engine.Open(ctx, dsn2,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithDataRoot(root2),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestImportFault(0, func(stage string) error {
			if stage == engine.ImportAfterFirstFold {
				return errKilled
			}
			return nil
		}))
	if !errors.Is(err, errKilled) {
		t.Fatalf("the boot did not die between the fold passes: %v", err)
	}
	raw := scopedDB(t, dsn2, "geoah")
	if n := countRows(t, raw, "embed_queue"); n != 0 {
		t.Fatalf("the crashed import queued %d rows before the transaction that completes it", n)
	}

	svc2 := mustReopen(t, dsn2, root2)
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("open the repository after the import resumed: %v", err)
	}
	assertQueued(t, raw, ids)
}

// A newer directory restored over an older database dump: the dump holds
// vectors for values the directory has since rewritten or cleared, and for
// values it has not. The import converges: the cleared property's vector goes
// (nothing would ever re-queue it, so it would be scored for good), the
// rewritten one's goes and the property is queued, the unchanged one is
// neither deleted nor queued nor bought again, a tombstone keeps its vectors
// the way the live path does until a purge, and a blank value is queued for
// nobody.
func TestImportConvergesTheVectorsAnOlderDatabaseHolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "people")
	installShelf(t, ds)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	rewritten := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Rewritten", "description": "alpha unique marmalade prose"},
	})
	cleared := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Cleared", "description": "gamma tangerine dictionary volume"},
	})
	kept := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Kept", "description": "delta saxophone almanac chapter"},
	})
	tombstoned := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Tombstoned", "description": "epsilon lighthouse ledger entry"},
	})
	// Whitespace alone: chunkText gives it no chunks, so the drain drops the
	// row without a vector.
	blank := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Blank", "description": "   "},
	})
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 5 {
		t.Fatalf("drain = %d, %v, want 5, nil", n, err)
	}
	if n := countRows(t, raw0(t, dsn), "embeddings"); n != 4 {
		t.Fatalf("%d vectors before the copy, want 4 (the blank blurb has none)", n)
	}
	if got := semanticIDs(t, ds, "tangerine dictionary"); len(got) == 0 || got[0] != cleared.ID {
		t.Fatalf("before the copy: %v", got)
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The directory moves on in another database: one blurb is rewritten,
	// one cleared, one record tombstoned, the rest left alone.
	root2 := copyRepositoryDir(t, root, id)
	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds2, owner, substrate.PutInput{
		Kind: "book", ID: rewritten.ID, Properties: map[string]any{"description": "beta zeppelin narrative here"},
	})
	mustPatch(t, ds2, owner, "book", cleared.ID, substrate.PatchInput{Properties: map[string]any{"description": nil}})
	if _, err := ds2.Delete(ctx, owner, "book", tombstoned.ID); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	_ = svc2.Close()

	// The first database is the older dump: its vectors are the old texts'.
	svc3 := mustReopen(t, dsn, root2)
	ds3, err := svc3.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the repository restored over the older database: %v", err)
	}
	raw := scopedDB(t, dsn, "geoah")
	if n := countRows(t, raw, "embeddings"); n != 2 {
		t.Fatalf("%d vectors after the import, want the unchanged blurb's and the tombstone's", n)
	}
	for _, id := range []string{kept.ID, tombstoned.ID} {
		var n int
		if err := raw.QueryRow(`SELECT count(*) FROM embeddings WHERE record_id = $1`, id).Scan(&n); err != nil || n != 1 {
			t.Fatalf("the vector of %s did not survive the import: %d, %v", id, n, err)
		}
	}
	_ = blank
	assertQueued(t, raw, []string{rewritten.ID})

	// Vectors exist, so the semantic arm answers; the cleared blurb is not in
	// it, and the answer says one property is still pending.
	res, err := ds3.Search(ctx, substrate.SearchInput{Q: "tangerine dictionary", Mode: substrate.SearchSemantic})
	if err != nil || res.Pending != 1 {
		t.Fatalf("semantic search before the drain = %+v, %v; want pending 1", res, err)
	}
	for _, h := range res.Hits {
		if h.Record.ID == cleared.ID {
			t.Fatal("the cleared blurb is still found by its old text")
		}
	}
	// The search above embedded its query; from here every text the fake
	// sees is a chunk the drain bought.
	_, textsBefore := emb.counts()
	if n, err := ds3.ProcessEmbedQueue(ctx, 20); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v, want 1, nil", n, err)
	}
	if _, textsAfter := emb.counts(); textsAfter-textsBefore != 1 {
		t.Fatalf("the drain embedded %d texts after the import, want 1 (the rewritten blurb alone)", textsAfter-textsBefore)
	}
	if got := semanticIDs(t, ds3, "zeppelin narrative"); len(got) == 0 || got[0] != rewritten.ID {
		t.Fatalf("the rewritten blurb is not found by its current text: %v", got)
	}
	if got := semanticIDs(t, ds3, "saxophone almanac"); len(got) == 0 || got[0] != kept.ID {
		t.Fatalf("the unchanged blurb is not found: %v", got)
	}
}

// The directory re-points the repository's llmprovider row at another model
// while every blurb stays the same: the older database's vectors carry the
// old pair, so the text hash alone would call them current, and semantic
// search would refuse naming `reembed` until an operator ran it. The import
// holds a chunk current only under the pair the folded records resolve, so
// the old vectors go, the property is queued and the answer counts it.
func TestImportRequeuesWhenTheDirectoryRepointsTheModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "people")
	installShelf(t, ds)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Same", "description": "alpha unique marmalade prose"},
	})
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v, want 1, nil", n, err)
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The directory moves on in another database: the row is re-pointed,
	// the blurb is not touched.
	root2 := copyRepositoryDir(t, root, id)
	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, ds2, owner, substrate.PutInput{
		Kind: typeProvider, ID: "vectors",
		Properties: map[string]any{"embedModel": "text-embedding-ada-002"},
	})
	_ = svc2.Close()

	// The first database is the older dump: its one vector is the old model's.
	svc3 := mustReopen(t, dsn, root2)
	ds3, err := svc3.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the repository restored over the older database: %v", err)
	}
	raw := scopedDB(t, dsn, "geoah")
	if n := countRows(t, raw, "embeddings"); n != 0 {
		t.Fatalf("%d vectors of the old model outlived the import", n)
	}
	assertQueued(t, raw, []string{book.ID})
	_, err = ds3.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic})
	if !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(err.Error(), `"text-embedding-ada-002": 1 properties pending`) {
		t.Fatalf("semantic search before the drain = %v, want ErrUnavailable naming the new model and 1 pending", err)
	}
	if n, err := ds3.ProcessEmbedQueue(ctx, 20); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v, want 1, nil", n, err)
	}
	if got := semanticIDs(t, ds3, "marmalade prose"); len(got) == 0 || got[0] != book.ID {
		t.Fatalf("the blurb is not found under the new model: %v", got)
	}
	var model string
	if err := raw.QueryRow(`SELECT model FROM embeddings WHERE record_id = $1`, book.ID).Scan(&model); err != nil || model != "text-embedding-ada-002" {
		t.Fatalf("the re-bought vector names model %q, %v; want the new one", model, err)
	}
}

// The directory's later declaration of the same closure turns `embed` off on
// the property the older database holds vectors for. Nothing queues them, so
// nothing would ever replace them, and the semantic arm would keep scoring
// them: the import deletes them with their queue rows, because the registry
// no longer embeds the pair. (A kind the registry does not know at all is a
// parked closure and is left alone.)
func TestImportDropsTheVectorsOfAPropertyNoLongerEmbedded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "people")
	installShelf(t, ds)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "book", Properties: map[string]any{"title": "Same", "description": "alpha unique marmalade prose"},
	})
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v, want 1, nil", n, err)
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The directory moves on in another database: the shelf closure is
	// declared again with the blurb no longer embeddable.
	root2 := copyRepositoryDir(t, root, id)
	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	if err := enginetest.InstallShelfVersion(ctx, ds2, 2, false); err != nil {
		t.Fatalf("declare the shelf again without embed: %v", err)
	}
	_ = svc2.Close()

	svc3 := mustReopen(t, dsn, root2)
	ds3, err := svc3.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open the repository restored over the older database: %v", err)
	}
	raw := scopedDB(t, dsn, "geoah")
	if n := countRows(t, raw, "embeddings"); n != 0 {
		t.Fatalf("%d vectors of a property no longer embedded outlived the import", n)
	}
	if n := countRows(t, raw, "embed_queue"); n != 0 {
		t.Fatalf("%d queue rows for a property no longer embedded", n)
	}
	// Nothing embeddable, nothing queued, nothing stored: an empty answer.
	res, err := ds3.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic})
	if err != nil || len(res.Hits) != 0 || res.Pending != 0 {
		t.Fatalf("semantic search = %+v, %v; want an empty answer", res, err)
	}
	_ = book
}

// A provider and nothing embeddable is an empty answer, not a refusal: the
// unavailable signal needs work in the queue, or a 503 would never clear.
func TestSemanticSearchWithNothingEmbeddableIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	_, ds := shelfRepository(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic}))
	if err != nil || len(hits) != 0 {
		t.Fatalf("semantic search with nothing embeddable = %v, %v; want an empty answer", hitIDs(hits), err)
	}
}

// raw0 is the scoped pool of the repository the test wrote first, before it
// is copied anywhere.
func raw0(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	return scopedDB(t, dsn, "geoah")
}

// shelfRepository is a repository with the shelf fixture installed:
// `book.description` is its one embeddable property. The fixture's authors
// are people, so the people sample comes first.
func shelfRepository(t *testing.T) (substrate.Service, substrate.Dataset) {
	t.Helper()
	svc, ds := newCoreDataset(t)
	importVocabulary(t, ds, "people")
	installShelf(t, ds)
	return svc, ds
}

// putBooks writes three books whose blurbs share no words, so "marmalade
// prose" ranks the first one first under the fake embedder, and returns their
// ids in write order.
func putBooks(t *testing.T, ds substrate.Dataset) []string {
	t.Helper()
	blurbs := []string{
		"alpha unique marmalade prose", "beta zeppelin narrative here",
		"gamma tangerine dictionary volume",
	}
	ids := make([]string, 0, len(blurbs))
	for i, blurb := range blurbs {
		row := mustPut(t, ds, owner, substrate.PutInput{
			Kind: "book", Properties: map[string]any{
				"title": "Book " + string(rune('A'+i)), "description": blurb,
			},
		})
		ids = append(ids, row.ID)
	}
	return ids
}

// scopedDB opens the repository-scoped pool the engine itself reads through,
// so the counts below see exactly what the drain sees.
func scopedDB(t *testing.T, dsn, username string) *sql.DB {
	t.Helper()
	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, username), engine.RoleApp)
	if err != nil {
		t.Fatalf("open the scoped pool: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// assertQueued holds the queue to exactly one row per book, on `description`,
// at generation 1: what a fresh import writes.
func assertQueued(t *testing.T, db *sql.DB, ids []string) {
	t.Helper()
	rows, err := db.Query(`SELECT record_id, property, generation FROM embed_queue ORDER BY record_id`)
	if err != nil {
		t.Fatalf("read the embed queue: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]bool{}
	for rows.Next() {
		var rid, prop string
		var gen int64
		if err := rows.Scan(&rid, &prop, &gen); err != nil {
			t.Fatal(err)
		}
		if prop != "description" || gen != 1 {
			t.Fatalf("queue row %s: property %q at generation %d, want description at 1", rid, prop, gen)
		}
		got[rid] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(ids) {
		t.Fatalf("the queue holds %d properties, want %d: %v", len(got), len(ids), got)
	}
	for _, id := range ids {
		if !got[id] {
			t.Fatalf("book %s is not queued", id)
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}
