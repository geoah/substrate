package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	h := newStubEnv(t, newStubKinds(personKind), func(c *Config) { c.WebDir = dir }).h
	// A token, so the authenticated subtree answers about the PATH rather than
	// about the caller (an unauthenticated 401 is the right answer there, and it
	// is a JSON problem object already).
	tok := stubToken

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
		if got := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Code; got != codeNotFound {
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
	env := newStubEnv(t, nil)
	tok := stubToken
	// `/api/v1/nope` matches no route at all — not even the generic
	// {authority}/{package}/{kind}/{id} record, whose own 404 is a JSON
	// problem object already — so it is the router's fallback that has to
	// answer well.
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/nope", tok, nil),
		http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/nope", tok, nil),
		http.StatusNotFound, codeNotFound)
	// An unauthenticated request at a route that exists is refused before the
	// path's handler runs, which is a JSON problem object too — never HTML
	// with a 200.
	wantErrorCode(t, env.do(t, http.MethodGet, recordsPath, "", nil),
		http.StatusUnauthorized, codeAuth)
}

// --- unknown query parameters are refused ------------------------------------

// Ruling A8: an unsupported list parameter is a bad_request naming the key,
// never silence. A silently dropped parameter returns UNFILTERED rows that look
// filtered, which is the worst answer available.
func TestUnknownListParamsAreRefused(t *testing.T) {
	env := newStubEnv(t, nil)
	tok := stubToken

	for _, query := range []string{"?bogus=1", "?first=2&bogus=1", "?limit=5", "?watch=1&bogus=1"} {
		rec := env.do(t, http.MethodGet, recordsPath+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		if !strings.Contains(msg, "bogus") && !strings.Contains(msg, "limit") {
			t.Errorf("%s: message = %q, want the offending key named", query, msg)
		}
	}
	// The key is quoted so the message points at exactly one thing.
	rec := env.do(t, http.MethodGet, recordsPath+"?limit=5", tok, nil)
	msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
	if !strings.Contains(msg, `"limit"`) {
		t.Errorf("message = %q, want the offending key quoted", msg)
	}
	// A near miss is told the spelling that works: `orderby` is `orderBy`.
	rec = env.do(t, http.MethodGet, recordsPath+"?orderby=createdAt", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"orderBy"`) {
		t.Errorf("message = %q, want the working spelling suggested", msg)
	}
}

// A single-record GET honors no query parameter, so every one is refused by
// name with the body the list answers (#335). It was a silent 200 before: a
// client sending a stale `withEdges` or an `expand` the read never runs got
// the bare record back and could not tell. Every refusal is the door's: the
// stub has no Get and no List, so a read that reached the dataset would be a
// 500. The read without a parameter is TestRecordReadAnswersWithoutParameters.
func TestUnknownRecordReadParamsAreRefused(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	tok := stubToken
	id := "p1"

	// Refused on the list too, so the two bodies must match byte for byte.
	for _, query := range []string{"?withEdges=1", "?bogus=1", "?limit=5"} {
		rec := env.do(t, http.MethodGet, peoplePath+"/"+id+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		list := env.do(t, http.MethodGet, recordsPath+query, tok, nil)
		if got, want := rec.Body.String(), list.Body.String(); got != want {
			t.Errorf("GET record%s answered %s, want the list's %s", query, got, want)
		}
	}
	// Honored by the list, not by the record read: still refused, named. The
	// read always carries annotations, so `withAnnotations=0` would be
	// ignored as silently as `=1` is redundant.
	for query, key := range map[string]string{
		"?expand=manager":    "expand",
		"?first=5":           "first",
		"?withAnnotations=1": "withAnnotations",
	} {
		rec := env.do(t, http.MethodGet, peoplePath+"/"+id+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"`+key+`"`) {
			t.Errorf("GET record%s: message = %q, want %q named", query, msg, key)
		}
	}
	// The refusal runs before the read, so a missing id is refused, not
	// answered 404 (TestGetComputedOccurrence holds a computed id to it).
	rec := env.do(t, http.MethodGet, peoplePath+"/nope?bogus=1", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	// The kind is resolved first, as on DELETE: an unknown kind is still 404.
	rec = env.do(t, http.MethodGet, "/api/v1/samples.substrate.reamde.dev/people/widgets/"+id+"?bogus=1", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// A query string that does not parse is refused on every route that names its
// parameters. r.URL.Query() drops the pair it cannot read, so `?filter=%ZZ`
// listed every row and `?bogus=1;x=2` passed the name check as a clean query.
// The refusal is the door's: the recorder answers List alone, so any other
// read or write that reached the dataset would be a 500.
func TestMalformedQueryStringsAreRefused(t *testing.T) {
	ds := newRecordsRecorder()
	env := newStubEnv(t, ds)
	tok := stubToken
	id := "p1"

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, peoplePath + "/" + id + "?bogus=1;x=2"},
		{http.MethodGet, peoplePath + "/" + id + "?bogus=%ZZ"},
		{http.MethodGet, recordsPath + "?filter=%ZZ"},
		{http.MethodGet, recordsPath + "?first=1;bogus=2"},
		{http.MethodGet, recordsPath + "?q=ada&mode=%ZZ"},
		{http.MethodGet, recordsPath + "?watch=1&from=0;x=1"},
		{http.MethodGet, changesPath + "?ops=put;delete"},
		{http.MethodDelete, peoplePath + "/" + id + "?ifVersion=1;purge=true"},
	} {
		rec := env.do(t, c.method, c.path, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		if !strings.HasPrefix(msg, "malformed query string: ") || strings.Contains(msg, "not supported") {
			t.Errorf("%s %s: message = %q, want the parse error alone", c.method, c.path, msg)
		}
	}
	// An escaped `;` and `%` are ordinary characters, not a malformed query,
	// and reach the list as the characters they spell.
	rec := env.do(t, http.MethodGet, recordsPath+`?filter={"properties":{"name":{"eq":"A%3Bda%25"}}}`, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := ds.query.Filter.Properties["name"].Eq; got != "A;da%" {
		t.Fatalf("the list saw name eq %v, want A;da%%", got)
	}
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
// whole unfiltered feed looking like a filtered one. The refusal is the
// door's: the stub has no change feed, so a read that reached the dataset
// would be a 500.
func TestUnknownChangeParamsAreRefused(t *testing.T) {
	env := newStubEnv(t, nil)
	tok := stubToken

	for query, want := range map[string]string{
		"?kind=samples.substrate.reamde.dev/people/person": "kinds",
		"?op=put":      "ops",
		"?actor=owner": "actors",
		"?bogus=1":     "",
	} {
		rec := env.do(t, http.MethodGet, changesPath+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		if want != "" && !strings.Contains(msg, want) {
			t.Errorf("%s: message = %q, want the plural %q suggested", query, msg, want)
		}
	}
}
