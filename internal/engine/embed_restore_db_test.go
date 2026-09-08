package engine_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The vectors and the embed queue are Postgres-only, and the repository
// directory is the backup unit (decision 0051), so a repository restored into
// an empty database has neither. The import queues every embeddable property
// in the transaction that completes its fold, at generation 1, and the drain
// buys the vectors back; until it has, semantic search says "no vectors yet"
// rather than answering "no matches".
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

	// Before the drain the semantic arm has nothing, and says so with the
	// count, instead of an empty answer.
	_, err = ds2.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic})
	if !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(err.Error(), "3 properties pending") {
		t.Fatalf("semantic search before the drain = %v, want ErrUnavailable naming 3 pending", err)
	}
	// Hybrid still answers with its lexical arm.
	hits, err := ds2.Search(ctx, substrate.SearchInput{Q: "marmalade"})
	if err != nil || len(hits) == 0 || hits[0].Record.ID != ids[0] {
		t.Fatalf("hybrid search before the drain = %v, %v", hitIDs(hits), err)
	}

	if n, err := ds2.ProcessEmbedQueue(ctx, 20); err != nil || n != len(ids) {
		t.Fatalf("drain after the restore = %d, %v, want %d, nil", n, err, len(ids))
	}
	if n := countRows(t, raw, "embed_queue"); n != 0 {
		t.Fatalf("%d queue rows outlived the drain", n)
	}
	got := semanticIDs(t, ds2, "marmalade prose")
	if !sameSet(got, want) || got[0] != want[0] {
		t.Fatalf("semantic search after the restore = %v, want %v", got, want)
	}
}

// With no llmprovider row the import queues the properties all the same, and
// they stay pending: the drain idles over them, semantic search names the
// missing row, and the first row that resolves buys them.
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
	_, err = ds2.Search(ctx, substrate.SearchInput{Q: "marmalade prose", Mode: substrate.SearchSemantic})
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
