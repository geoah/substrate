package api

// A list cursor is bound to the request the client sent. An app's request is
// narrowed to what its grant reads before it reaches the engine, and that
// narrowing can change between pages with no change of the client's: a kind
// declared in a package the grant reads all of. That is the server's change,
// answered list-again, never the client's mistake.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAScopedListWhoseGrantReadsAnotherKindBetweenPagesListsAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	pkg := env.repository + "/details"
	kind := func(name string) map[string]any {
		return vocabulary.KindManifest(pkg, map[string]any{"singular": name}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		})
	}
	apply := func(docs ...map[string]any) {
		t.Helper()
		if _, err := env.ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	apply(vocabulary.PackageManifest(pkg, 0), kind("note"))
	for _, name := range []string{"a", "b", "c"} {
		if _, err := env.ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: pkg + "/note", Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
	}
	app := appScopedToken(t, env, "read:"+pkg+"/*")
	path := recordsPath + "?filter=%7B%7D&orderBy=name&first=1"
	first := env.do(t, http.MethodGet, path, app, nil)
	wantStatus(t, first, http.StatusOK)
	var page struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil || page.Cursor == "" {
		t.Fatalf("the first page: %v, cursor %q", err, page.Cursor)
	}
	// The cursor under another request is still the client's mistake.
	wantErrorCode(t, env.do(t, http.MethodGet, recordsPath+"?filter=%7B%7D&orderBy=-name&first=1&after="+page.Cursor, app, nil),
		http.StatusUnprocessableEntity, codeValidation)

	// The package gains a kind, which the grant reads: the same request now
	// resolves to two kinds where it resolved to one.
	apply(vocabulary.PackageManifest(pkg, 0), kind("note"), kind("memo"))
	next := env.do(t, http.MethodGet, path+"&after="+page.Cursor, app, nil)
	wantErrorCode(t, next, http.StatusGone, codeCompacted)
}

// A request the grant now resolves to nothing at all is still a valid
// request, answered fresh with an empty page; its continuation cannot go on,
// so it lists again rather than ending the walk as if it were done. Here the
// one kind it read stops matching the purpose it asks for.
func TestAScopedListThatNowReachesNothingListsAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	pkg := env.repository + "/details"
	note := func(purpose string) map[string]any {
		data := map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}
		if purpose != "" {
			data["purpose"] = purpose
		}
		return vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, data)
	}
	apply := func(docs ...map[string]any) {
		t.Helper()
		if _, err := env.ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	apply(vocabulary.PackageManifest(pkg, 0), note(""))
	for _, name := range []string{"a", "b"} {
		if _, err := env.ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: pkg + "/note", Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
	}
	app := appScopedToken(t, env, "read:"+pkg+"/*")
	path := recordsPath + "?filter=%7B%22purposes%22%3A%5B%22primary%22%5D%7D&orderBy=name&first=1"
	first := env.do(t, http.MethodGet, path, app, nil)
	wantStatus(t, first, http.StatusOK)
	var page struct {
		Records []json.RawMessage `json:"records"`
		Cursor  string            `json:"cursor"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil || page.Cursor == "" || len(page.Records) != 1 {
		t.Fatalf("the first page: %v, %d records, cursor %q", err, len(page.Records), page.Cursor)
	}

	// The one kind the request reached is no longer of the purpose it asks
	// for: the same request reaches nothing now.
	apply(vocabulary.PackageManifest(pkg, 0), note("supporting"))
	fresh := env.do(t, http.MethodGet, path, app, nil)
	wantStatus(t, fresh, http.StatusOK)
	var empty struct {
		Records []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(fresh.Body.Bytes(), &empty); err != nil || len(empty.Records) != 0 {
		t.Fatalf("the fresh request: %v, %d records, want an empty page", err, len(empty.Records))
	}
	wantErrorCode(t, env.do(t, http.MethodGet, path+"&after="+page.Cursor, app, nil), http.StatusGone, codeCompacted)
}
