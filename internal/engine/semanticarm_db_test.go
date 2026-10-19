package engine_test

// The semantic arm's index and its bounds (decision 0174): the candidate read
// is one the HNSW index over half-precision copies serves, and a hybrid
// search whose semantic arm runs out of its budget or loses its embeddings
// provider answers from its words, with a note, while semantic mode errs.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The candidate read's ORDER BY spells the index's expression, so the planner
// can answer it from embeddings_vec_hnsw_idx, with and without a kind filter.
// Sorts are priced out on top of the arm's own settings because on an empty
// table a primary-key scan and a sort is cheaper than the graph; what is
// asserted is that the index can serve the read at all, which a mismatched
// cast or operator would make impossible.
func TestSemanticCandidateReadUsesTheHNSWIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	for _, kinds := range [][]string{nil, {"shelf.test.dev/shelf/book"}} {
		plan, err := engine.SemanticCandidatePlan(ctx, ds, kinds, 40, true, false)
		if err != nil {
			t.Fatalf("explain the candidate read (kinds %v): %v", kinds, err)
		}
		if !strings.Contains(plan, "embeddings_vec_hnsw_idx") {
			t.Fatalf("the candidate read (kinds %v) does not use the HNSW index:\n%s", kinds, plan)
		}
	}
}

// Under the arm's own settings, and nothing else, the candidate read never
// scans the embeddings table: a sequential scan reads every vector out of
// TOAST, far dearer than the planner prices it, so the arm holds the planner
// to an index path, and the generic plan a statement cache settles on obeys
// the same settings as a custom one.
func TestSemanticCandidateReadNeverScansTheTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	for _, kinds := range [][]string{nil, {"shelf.test.dev/shelf/book"}} {
		for _, generic := range []bool{false, true} {
			plan, err := engine.SemanticCandidatePlan(ctx, ds, kinds, 40, false, generic)
			if err != nil {
				t.Fatalf("explain the candidate read (kinds %v, generic %v): %v", kinds, generic, err)
			}
			if strings.Contains(plan, "Seq Scan on embeddings") {
				t.Fatalf("the candidate read (kinds %v, generic %v) scans the table:\n%s", kinds, generic, plan)
			}
		}
	}
}

// A search narrowed to a kind that holds a small share of the vectors finds
// that kind's record by meaning: the kind filter rides the candidate read, so
// the arm does not fill its candidates with the other kind's chunks and come
// back empty.
func TestSemanticSearchFindsARareKindByMeaning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newFakeEmbedServer(t)
	_, ds := newDataset(t)
	installShelf(t, ds)
	const pkg = "rarekind.example.substrate.reamde.dev/notes"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "memo"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"note": map[string]any{"type": "text", "embed": true},
			}}),
	}); err != nil {
		t.Fatalf("declare the memo kind: %v", err)
	}
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	for i := range 60 {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: "shelf.test.dev/shelf/book", Properties: map[string]any{
				"title": fmt.Sprintf("Book %d", i), "description": fmt.Sprintf("volume %d of the marmalade almanac", i),
			},
		})
	}
	memo := mustPut(t, ds, owner, substrate.PutInput{
		Kind: pkg + "/memo", Properties: map[string]any{"name": "Log", "note": "the lighthouse keeper writes at dawn"},
	})
	for {
		n, err := ds.ProcessEmbedQueue(ctx, 64)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if n == 0 {
			break
		}
	}
	for _, mode := range []substrate.SearchMode{substrate.SearchSemantic, substrate.SearchHybrid} {
		res, err := ds.Search(ctx, substrate.SearchInput{Q: "lighthouse keeper", Mode: mode, Kinds: []string{pkg + "/memo"}})
		if err != nil {
			t.Fatalf("%s search narrowed to the memo kind: %v", mode, err)
		}
		if len(res.Hits) == 0 || res.Hits[0].Record.ID != memo.ID || res.Hits[0].Semantic <= 0 {
			t.Fatalf("%s search narrowed to the memo kind = %v, want the memo with a semantic score", mode, hitIDs(res.Hits))
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
	if !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(err.Error(), "failed to embed the query") {
		t.Fatalf("semantic search with the embedder down = %v, want ErrUnavailable naming the failed embed", err)
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
		// A statement the database is running when the budget ends: the
		// cancellation reaches Postgres, as it would for a slow index scan.
		engine.WithTestSemanticHook(func(ctx context.Context, tx *sql.Tx) {
			if stall.Load() {
				_, _ = tx.ExecContext(ctx, `SELECT pg_sleep(30)`)
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
	f := newSemanticFixture(t, engine.WithTestSemanticHook(func(context.Context, *sql.Tx) {
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

// The vector index migration rebuilds an index of its name that the reader
// cannot use, and leaves alone both the index it builds and a relation of that
// name in another schema. Each case runs the migration again, in a
// transaction rolled back after it, over an arranged namesake.
func TestTheVectorIndexMigrationReplacesAnIndexTheReaderCannotUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("pgx", engine.MigratedDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migration, err := engine.MigrationSQL("0015_embeddings_vec_index.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	// The index the reader can use: found beside the embeddings table, never
	// through the search path, and checked whole.
	const usable = `SELECT count(*) FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_am a ON a.oid = c.relam
		JOIN pg_opclass o ON o.oid = i.indclass[0]
		WHERE c.relname = 'embeddings_vec_hnsw_idx'
		  AND c.relnamespace = (SELECT relnamespace FROM pg_class WHERE oid = to_regclass('embeddings'))
		  AND i.indrelid = to_regclass('embeddings') AND i.indisvalid AND i.indpred IS NULL AND i.indnatts = 1
		  AND a.amname = 'hnsw' AND o.opcname = 'halfvec_cosine_ops'
		  AND regexp_replace(pg_get_expr(i.indexprs, i.indrelid), '[()[:space:]]|public[.]', '', 'g') = 'vec::halfvec1536'`
	run := func(t *testing.T, arrange []string, check func(*sql.Tx)) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		for _, stmt := range arrange {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("arrange %q: %v", stmt, err)
			}
		}
		if _, err := tx.ExecContext(ctx, migration); err != nil {
			t.Fatalf("run the migration again: %v", err)
		}
		var n int
		if err := tx.QueryRowContext(ctx, usable).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("after the migration embeddings_vec_hnsw_idx is not the index the reader uses")
		}
		if check != nil {
			check(tx)
		}
	}
	oid := func(t *testing.T, tx *sql.Tx, rel string) int64 {
		t.Helper()
		var o int64
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(to_regclass($1)::oid::bigint, 0)`, rel).Scan(&o); err != nil {
			t.Fatal(err)
		}
		return o
	}
	drop := `DROP INDEX embeddings_vec_hnsw_idx`
	for name, arrange := range map[string][]string{
		"full precision":         {drop, `CREATE INDEX embeddings_vec_hnsw_idx ON embeddings USING hnsw (vec vector_cosine_ops)`},
		"another operator class": {drop, `CREATE INDEX embeddings_vec_hnsw_idx ON embeddings USING hnsw ((vec::halfvec(1536)) halfvec_l2_ops)`},
		"another expression":     {drop, `CREATE INDEX embeddings_vec_hnsw_idx ON embeddings USING hnsw (((vec + vec)::halfvec(1536)) halfvec_cosine_ops)`},
		"part of the table":      {drop, `CREATE INDEX embeddings_vec_hnsw_idx ON embeddings USING hnsw ((vec::halfvec(1536)) halfvec_cosine_ops) WHERE provider = 'other'`},
		"another table":          {drop, `CREATE TABLE scratch_vectors (vec vector(1536))`, `CREATE INDEX embeddings_vec_hnsw_idx ON scratch_vectors USING hnsw ((vec::halfvec(1536)) halfvec_cosine_ops)`},
		"invalid":                {`UPDATE pg_index SET indisvalid = false WHERE indexrelid = to_regclass('embeddings_vec_hnsw_idx')`},
		"missing":                {drop},
	} {
		t.Run(name, func(t *testing.T) { run(t, arrange, nil) })
	}
	t.Run("the index it builds is kept", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		before := oid(t, tx, "embeddings_vec_hnsw_idx")
		if _, err := tx.ExecContext(ctx, migration); err != nil {
			t.Fatalf("run the migration again: %v", err)
		}
		if after := oid(t, tx, "embeddings_vec_hnsw_idx"); before == 0 || after != before {
			t.Fatalf("the migration rebuilt the index it builds (oid %d, was %d)", after, before)
		}
	})
	// The bug this guards against: with no index of the name beside the
	// table and another schema's namesake on the search path, a lookup through
	// the search path found the stranger and dropped it.
	t.Run("another schema's namesake is left alone", func(t *testing.T) {
		var foreign int64
		run(t, []string{
			drop,
			`CREATE SCHEMA scratch_other`,
			`CREATE TABLE scratch_other.vectors (vec vector(1536))`,
			`CREATE INDEX embeddings_vec_hnsw_idx ON scratch_other.vectors USING hnsw (vec vector_cosine_ops)`,
			`SELECT set_config('search_path', current_setting('search_path') || ', scratch_other', true)`,
		}, func(tx *sql.Tx) {
			if err := tx.QueryRowContext(ctx, `SELECT coalesce(to_regclass('scratch_other.embeddings_vec_hnsw_idx')::oid::bigint, 0)`).Scan(&foreign); err != nil {
				t.Fatal(err)
			}
			if foreign == 0 {
				t.Fatal("the migration dropped another schema's index of its name")
			}
		})
	})

	t.Run("a relation of the name that is not an index refuses", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		for _, stmt := range []string{drop, `CREATE TABLE embeddings_vec_hnsw_idx (x int)`} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("arrange %q: %v", stmt, err)
			}
		}
		if _, err := tx.ExecContext(ctx, migration); err == nil || !strings.Contains(err.Error(), "not an index") {
			t.Fatalf("the migration over a table of its index's name = %v, want the refusal naming the clash", err)
		}
	})
}
