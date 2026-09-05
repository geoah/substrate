package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// --- unmatched API paths are JSON, never the console -------------------------

// The SPA fallback was chi's NotFound for the WHOLE router, so a mistyped API
// path answered 200 with an HTML page: nothing distinguished "wrong path" from
// "it worked", and the closed error-code contract held for every
// path except the ones a client gets wrong.
func TestUnmatchedAPIPathsAreJSONNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>console"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	h := New(Config{Service: svc, Now: clock.now, WebDir: dir})
	// A token, so the authenticated subtree answers about the PATH rather than
	// about the caller (an unauthenticated 401 is the right answer there, and it
	// is a JSON problem object already).
	tok := svc.token("geoah")

	for _, path := range []string{
		"/api",
		"/api/nope",
		"/api/v1/nope",
		"/api/v1/nope",
		"/api/v1/substrate.reamde.dev/core/nope",
		"/api/v1/changes/nope",
		"/api/v1/samples.substrate.reamde.dev/people/person/9f2k/nope",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
			continue
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("%s: content-type = %q, want json", path, got)
		}
		if strings.Contains(rec.Body.String(), "console") {
			t.Errorf("%s: served the console HTML: %s", path, rec.Body.String())
		}
		if got := decodeJSON[errorEnvelope](t, rec).Error.Code; got != codeNotFound {
			t.Errorf("%s: error code = %q, want %q", path, got, codeNotFound)
		}
	}

	// A console route is still the console's: the SPA fallback is why the
	// deep links work at all.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/types/people.samples.substrate.reamde.dev/people", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console") {
		t.Fatalf("console route: status = %d, body = %q", rec.Code, rec.Body.String())
	}
	// And a path that merely starts with the letters is not the API's.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apiary", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console") {
		t.Fatalf("/apiary: status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

// With no console built in, an unmatched API path is STILL the JSON problem
// object — the contract belongs to the API, not to the presence of a WebDir.
func TestUnmatchedAPIPathsAreJSONWithoutAWebDir(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// `/api/v1/nope` matches no route at all — not even the generic
	// {authority}/{plural} resource, whose own 404 is a JSON problem object
	// already — so it is the router's fallback that has to answer well.
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/nope", tok, nil),
		http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/nope", tok, nil),
		http.StatusNotFound, codeNotFound)
	// An unauthenticated one is refused before the path is considered at all,
	// which is a JSON problem object too — never HTML with a 200.
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/nope", "", nil),
		http.StatusUnauthorized, codeAuth)
}

// --- unknown query parameters are refused ------------------------------------

// Ruling A8: an unsupported list parameter is a bad_request naming the key,
// never silence. A silently dropped parameter returns UNFILTERED rows that look
// filtered, which is the worst answer available.
func TestUnknownListParamsAreRefused(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	for _, query := range []string{"?bogus=1", "?first=2&bogus=1", "?limit=5", "?watch=1&bogus=1"} {
		rec := env.do(t, http.MethodGet, peoplePath+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[errorEnvelope](t, rec).Error.Message
		if !strings.Contains(msg, "bogus") && !strings.Contains(msg, "limit") {
			t.Errorf("%s: message = %q, want the offending key named", query, msg)
		}
	}
	// The key is quoted so the message points at exactly one thing.
	rec := env.do(t, http.MethodGet, peoplePath+"?limit=5", tok, nil)
	msg := decodeJSON[errorEnvelope](t, rec).Error.Message
	if !strings.Contains(msg, `"limit"`) {
		t.Errorf("message = %q, want the offending key quoted", msg)
	}
	// A near miss is told the spelling that works: `orderby` is `orderBy`.
	rec = env.do(t, http.MethodGet, peoplePath+"?orderby=createdAt", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[errorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"orderBy"`) {
		t.Errorf("message = %q, want the working spelling suggested", msg)
	}
}

// Every parameter the list DOES support keeps working — the refusal above must
// not be a blanket one.
func TestSupportedListParamsStillWork(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	createPerson(t, env, tok)

	for _, query := range []string{
		"",
		"?first=5",
		"?after=",
		"?orderBy=updatedAt:desc",
		"?withAnnotations=1",
		`?filter={"properties":{"name":{"eq":"Ada"}}}`,
		"?first=5&orderBy=createdAt&withAnnotations=1",
	} {
		rec := env.do(t, http.MethodGet, peoplePath+query, tok, nil)
		wantStatus(t, rec, http.StatusOK)
	}
	// The watch mode's own parameters: the switch and the resume cursor.
	wantNotRefused(t, env, peoplePath+"?watch=1&from=0", tok)
}

// wantNotRefused drives a WATCH request to completion: the stream would
// otherwise never end, so the request context is canceled up front — the
// parameter check runs long before any streaming, so a refusal still surfaces.
func wantNotRefused(t *testing.T, env *testEnv, path, token string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("%s was refused: %s", path, rec.Body.String())
	}
}

// The changes feed's filter keys are PLURAL, so the plausible singular guess
// (`type=`, `op=`, `actor=`) used to be dropped in silence and answer with the
// whole unfiltered feed looking like a filtered one.
func TestUnknownChangeParamsAreRefused(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	seedChanges(env.svc.datasets["geoah"], 3)

	for query, want := range map[string]string{
		"?kind=samples.substrate.reamde.dev/people/person": "kinds",
		"?op=put":      "ops",
		"?actor=owner": "actors",
		"?bogus=1":     "",
	} {
		rec := env.do(t, http.MethodGet, changesPath+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[errorEnvelope](t, rec).Error.Message
		if want != "" && !strings.Contains(msg, want) {
			t.Errorf("%s: message = %q, want the plural %q suggested", query, msg, want)
		}
	}
}

// The feed's real parameters keep working, in both of its modes.
func TestSupportedChangeParamsStillWork(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	seedChanges(env.svc.datasets["geoah"], 3)

	for _, query := range []string{
		"",
		"?from=0",
		"?first=2",
		"?first=2&before=3",
		"?kinds=samples.substrate.reamde.dev/people/person&ops=put&actors=owner",
		"?excludeKinds=samples.substrate.reamde.dev/tasks/task&excludeOps=delete&excludeActors=machine",
		"?recordId=e1&recordKind=samples.substrate.reamde.dev/people/person",
		"?q=ada",
	} {
		rec := env.do(t, http.MethodGet, changesPath+query, tok, nil)
		wantStatus(t, rec, http.StatusOK)
	}
	wantNotRefused(t, env, changesPath+"?watch=1&from=3&kinds=samples.substrate.reamde.dev/people/person", tok)
}

// --- discovery's schema note ------------------------------------------------

// The note claimed the per-repository stored dialect was "on the repository". It is
// not: it lives in each repository's own `vocabulary_dialect` table and appears on
// neither the repository record nor `substratectl repository list`, so a client following the
// note went looking for a field that does not exist.
func TestDiscoverySchemaNoteDoesNotPointAtTheRepository(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[discoveryDoc](t, rec)
	if strings.Contains(doc.Vocabulary.Note, "on the repository") {
		t.Errorf("note = %q, but nothing on the repository carries the stored dialect", doc.Vocabulary.Note)
	}
	if !strings.Contains(doc.Vocabulary.Note, "vocabulary_dialect") {
		t.Errorf("note = %q, want it to name where the stored dialect actually lives", doc.Vocabulary.Note)
	}
	// The claim's own subject: the repository surface carries no dialect at all, so
	// a note pointing there points at nothing. Should one ever be surfaced, this
	// fails and the note gets to make the promise again.
	ty := reflect.TypeOf(substrate.RepositoryInfo{})
	for i := range ty.NumField() {
		if strings.Contains(strings.ToLower(ty.Field(i).Name), "dialect") {
			t.Fatalf("RepositoryInfo carries %q — surface it in the note instead", ty.Field(i).Name)
		}
	}
}
