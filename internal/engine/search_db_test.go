package engine_test

// The search index: what full text search ranks and demotes, which properties
// a kind contributes to it, and how a vocabulary apply that changes those
// re-derives the index for that kind's rows without moving a version.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
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

// Body is a declared, column-backed text property (#68), and FTS follows the
// property's own `fts` flag: a declared body indexes as prose unless the
// declaration opts out, and a kind that declares no body carries none. Three
// kinds pin the three outcomes: an `article` with a default-fts body, a `memo`
// with `fts: false`, and a `tag` that declares no body at all.
func TestDeclaredBodyAndPerPropertyFTS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const pkg = "reader.connectors.substrate.reamde.dev/reader"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "reader", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, "connector:reader"),
			// A declared body, fts defaulting true for text: indexed.
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "article"},
				map[string]any{"properties": map[string]any{
					"body": map[string]any{"type": "text"},
				}}),
			// A declared body opting out of the index: stored and served, never
			// searched.
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "memo"},
				map[string]any{"properties": map[string]any{
					"body": map[string]any{"type": "text", "fts": false},
				}}),
			// No body declared: a body write is refused like any undeclared name.
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "tag"},
				map[string]any{"properties": map[string]any{
					"label": map[string]any{"type": "string"},
				}}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	act := substrate.Actor("connector:reader")

	// (b) A default-fts body indexes: the article is found by a word only its
	// body holds.
	article := mustPut(t, ds, act, substrate.PutInput{
		Kind: pkg + "/article", ID: "a1",
		Properties: map[string]any{"body": "the peregrine falcon dives"},
	})
	hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{
		Q: "peregrine", Mode: substrate.SearchLexical, Kinds: []string{pkg + "/article"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Record.ID != article.ID {
		t.Fatalf("indexed body not found: %v", hitIDs(hits))
	}

	// (a) `fts: false` stores and serves the body but never indexes it.
	memo := mustPut(t, ds, act, substrate.PutInput{
		Kind: pkg + "/memo", ID: "m1",
		Properties: map[string]any{"body": "the peregrine falcon dives"},
	})
	full := mustGet(t, ds, memo.Kind, memo.ID)
	if full.Body != "the peregrine falcon dives" || full.Properties["body"] != "the peregrine falcon dives" {
		t.Fatalf("fts:false body not served: body=%q props=%v", full.Body, full.Properties["body"])
	}
	hits, err = searchHits(ds.Search(ctx, substrate.SearchInput{
		Q: "peregrine", Mode: substrate.SearchLexical, Kinds: []string{pkg + "/memo"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("fts:false body was indexed: %v", hitIDs(hits))
	}

	// (c) A kind that declares no body refuses a body write.
	if _, err := ds.Put(ctx, act, substrate.PutInput{
		Kind: pkg + "/tag", ID: "t1",
		Properties: map[string]any{"label": "x", "body": "nope"},
	}); err == nil {
		t.Fatal("body on a kind that declares none must be refused")
	} else {
		wantErr(t, err, substrate.ErrValidation, "not declared")
	}
}

// The search index is derived from the folded row AND the kind's declaration,
// and a rebuild indexes every row under the declarations it ends with. These
// tests hold the live index to the same rule: a kind edit that changes what
// its records index re-indexes them in the apply (fold.go reprojectFTS), so a
// search answers the same before and after a rebuild, and the rows' own
// values, version and timestamps do not move for it.

const (
	ftsPackage = "ftsindex.example.substrate.reamde.dev/notes"
	ftsNote    = ftsPackage + "/note"
)

// declareNotes applies the notes package with `remark` indexed or opted out
// of search; every other part of the declaration is the same on both sides.
func declareNotes(t *testing.T, ds substrate.Dataset, remarkIndexed bool) {
	t.Helper()
	remark := map[string]any{"type": "text"}
	if !remarkIndexed {
		remark["fts"] = false
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(ftsPackage, 0),
		vocabulary.KindManifest(ftsPackage,
			map[string]any{"singular": "note"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"remark": remark,
			}}),
	}); err != nil {
		t.Fatalf("declare the notes package (remark indexed=%v): %v", remarkIndexed, err)
	}
}

// lexicalHits runs one lexical search and renders its hits in rank order, id
// and score together, so two runs compare as one value.
func lexicalHits(t *testing.T, ds substrate.Dataset, q string) []string {
	t.Helper()
	hits, err := searchHits(ds.Search(context.Background(), substrate.SearchInput{Q: q, Mode: substrate.SearchLexical}))
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	var out []string
	for _, h := range hits {
		out = append(out, fmt.Sprintf("%s@%.6f", h.Record.ID, h.Lexical))
	}
	return out
}

func idsOfHits(hits []string) []string {
	var out []string
	for _, h := range hits {
		for i := range h {
			if h[i] == '@' {
				out = append(out, h[:i])
				break
			}
		}
	}
	return out
}

// TestAKindEditReindexesItsRowsAndTheRebuildAgrees: flipping a property's
// `fts` flag changes what the kind's existing records index, live, in the
// apply that flips it, and a rebuild lands on the same index. The records
// themselves did not change, so their version and updated_at stand and the
// changelog carries nothing about them.
func TestAKindEditReindexesItsRowsAndTheRebuildAgrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	declareNotes(t, ds, false)

	titled := mustPut(t, ds, owner, substrate.PutInput{
		Kind: ftsNote, ID: "titled",
		Properties: map[string]any{"name": "quokka in the title", "remark": "nothing to see"},
	})
	remarked := mustPut(t, ds, owner, substrate.PutInput{
		Kind: ftsNote, ID: "remarked",
		Properties: map[string]any{"name": "plain", "remark": "a quokka in the remark"},
	})
	// A tombstone is indexed too, and search never sees it: the rebuild must
	// still agree on its bands.
	gone := mustPut(t, ds, owner, substrate.PutInput{
		Kind: ftsNote, ID: "gone",
		Properties: map[string]any{"name": "deleted", "remark": "a quokka nobody finds"},
	})
	if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := idsOfHits(lexicalHits(t, ds, "quokka")); !reflect.DeepEqual(got, []string{"titled"}) {
		t.Fatalf("with remark opted out, search finds %v, want [titled]", got)
	}

	rb := svc.(rebuilder)
	rebuildAgrees := func(when string, wantIDs []string) {
		t.Helper()
		live := lexicalHits(t, ds, "quokka")
		if got := idsOfHits(live); !reflect.DeepEqual(got, wantIDs) {
			t.Fatalf("%s: search finds %v, want %v", when, got, wantIDs)
		}
		before := foldOf(t, ds)
		if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("%s: rebuild: %v", when, err)
		}
		if after := foldOf(t, ds); string(before) != string(after) {
			t.Fatalf("%s: the rebuilt fold is not the fold\n%s", when, firstDifference(before, after))
		}
		if rebuilt := lexicalHits(t, ds, "quokka"); !reflect.DeepEqual(rebuilt, live) {
			t.Fatalf("%s: the rebuilt repository searches differently: live %v, rebuilt %v", when, live, rebuilt)
		}
	}

	// Index `remark`: the word in it becomes a hit without any record moving.
	head := maxSeq(t, ds)
	declareNotes(t, ds, true)
	changes, err := ds.Changes(ctx, head, substrate.ChangeFilter{Kinds: []string{ftsNote}}, 100)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("the kind edit appended %d entries about its records, want none: %+v", len(changes), changes)
	}
	for _, want := range []*substrate.Record{titled, remarked} {
		got := mustGet(t, ds, want.Kind, want.ID)
		if got.Version != want.Version || !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatalf("the kind edit moved %s to version %d at %s; it was version %d at %s",
				want.ID, got.Version, got.UpdatedAt, want.Version, want.UpdatedAt)
		}
	}
	// The title (band A) outranks the remark (band C), so the order is fixed.
	rebuildAgrees("after indexing remark", []string{"titled", "remarked"})

	// And back: the word drops out of the index again, live and rebuilt alike.
	declareNotes(t, ds, false)
	rebuildAgrees("after opting remark out again", []string{"titled"})
}

// TestARebuildAgreesAfterAnUninstallLeavesTombstones: a package uninstall
// drops its kinds, and the tombstoned rows of a dropped kind index under the
// unknown-kind bands from then on (fold.go foldFTS), because that is what a
// replay without the declaration computes for them. The uninstall re-indexes
// them so the fold before and after a rebuild is the same document.
func TestARebuildAgreesAfterAnUninstallLeavesTombstones(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	const pkg = "gizmos.example.substrate.reamde.dev/gizmo"
	const gizmo = pkg + "/gizmo"
	closure := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "a bundle to uninstall", "installs": []any{gizmo},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "gizmo"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"notes": map[string]any{"type": "text"},
			}}),
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(pkg)), closure, nil,
		substrate.BundleInstall{}); err != nil {
		t.Fatalf("install the closure: %v", err)
	}
	g := mustPut(t, ds, owner, substrate.PutInput{
		Kind: gizmo, Properties: map[string]any{"name": "Widget", "notes": "kept in the fold as a tombstone"},
	})
	if _, err := ds.Delete(ctx, owner, g.Kind, g.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := ds.UninstallBundle(ctx, pkg); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
}
