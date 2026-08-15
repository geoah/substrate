package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const changesPath = "/api/v1/core.substrate.reamde.dev/changes"

// --- A1: discovery + versioning + the deprecation channel ---------------

func TestDiscoveryReportsVersionsFeaturesDialect(t *testing.T) {
	svc := newFakeService()
	h := New(Config{Service: svc, MaxDialect: 6, Embeddings: true})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /.well-known/substrate/server.json status = %d, body %s", rec.Code, rec.Body.String())
	}
	doc := decodeJSON[discoveryDoc](t, rec)

	// ONE served version: v1, and nothing beside it — no alias prefix ever
	// shipped, so discovery advertises no deprecated one.
	if len(doc.Versions) != 1 {
		t.Fatalf("served versions = %+v, want exactly v1", doc.Versions)
	}
	if v1 := doc.Versions[0]; v1.Name != "v1" || v1.Status != "served" || v1.ReplacedBy != "" {
		t.Fatalf("v1 version = %+v", v1)
	}

	// Binary max dialect surfaces here (per-repository dialect noted elsewhere).
	if doc.Vocabulary.MaxDialect != 6 {
		t.Fatalf("maxDialect = %d, want 6", doc.Vocabulary.MaxDialect)
	}
	if doc.Vocabulary.Note == "" {
		t.Fatalf("schema note missing")
	}

	// Retention horizon is 0 today.
	if doc.Changelog.Horizon != 0 {
		t.Fatalf("horizon = %d, want 0", doc.Changelog.Horizon)
	}

	// Feature list: agents carry alpha stability straight from the marker;
	// the rest are stable and present.
	stab := map[string]string{}
	for _, f := range doc.Features {
		stab[f.Name] = f.Stability
	}
	if stab[substrate.FeatureAgents] != substrate.AgentStability || stab["agents"] != "alpha" {
		t.Fatalf("agents feature = %q, want alpha", stab["agents"])
	}
	for _, want := range []string{"triggers", "functions", "bundles", "blobs", "changefeed", "search", "embeddings"} {
		if stab[want] != substrate.StabilityStable {
			t.Fatalf("feature %q stability = %q, want stable", want, stab[want])
		}
	}
	if version := doc.Server.Version; version == "" {
		t.Fatalf("server version missing")
	}
}

// Every feature says which surfaces serve it, because the two are not
// equivalent: search is the GraphQL query's alone, and a client that read
// "stable" as "a REST route exists" went looking for one that never shipped.
func TestDiscoveryFeaturesNameTheirSurfaces(t *testing.T) {
	svc := newFakeService()
	h := New(Config{Service: svc, Embeddings: true})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil))
	doc := decodeJSON[discoveryDoc](t, rec)

	want := map[string][]string{
		"triggers":   {surfaceREST},
		"functions":  {surfaceREST},
		"bundles":    {surfaceREST},
		"blobs":      {surfaceREST},
		"changefeed": {surfaceREST, surfaceGraphQL},
		"search":     {surfaceGraphQL},
		"embeddings": {surfaceGraphQL},
		"agents":     {surfaceREST},
	}
	seen := map[string]bool{}
	for _, f := range doc.Features {
		if seen[f.Name] {
			t.Fatalf("feature %q listed twice", f.Name)
		}
		seen[f.Name] = true
		if len(f.Surfaces) == 0 {
			t.Fatalf("feature %q names no surface", f.Name)
		}
		if _, ok := want[f.Name]; !ok {
			t.Fatalf("unknown feature %q: a new one has to declare its surfaces here", f.Name)
		}
		if !slices.Equal(f.Surfaces, want[f.Name]) {
			t.Fatalf("feature %q surfaces = %v, want %v", f.Name, f.Surfaces, want[f.Name])
		}
	}
	for name := range want {
		if !seen[name] {
			t.Fatalf("feature %q missing from discovery", name)
		}
	}
}

// The embeddings feature is the one this deployment may not have: without an
// embedder the semantic arm refuses and nothing drains the embed queue, so
// listing it would be the advertised-but-not-served bug again. `search` stays
// listed, because it degrades to lexical.
func TestDiscoveryOmitsEmbeddingsWithoutAnEmbedder(t *testing.T) {
	h := New(Config{Service: newFakeService()})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil))
	doc := decodeJSON[discoveryDoc](t, rec)

	names := map[string]bool{}
	for _, f := range doc.Features {
		names[f.Name] = true
	}
	if names["embeddings"] {
		t.Fatalf("embeddings listed with no embedder configured: %+v", doc.Features)
	}
	if !names["search"] {
		t.Fatalf("search dropped with no embedder: %+v", doc.Features)
	}
}

// The gql-only marker is a claim about the routes, so hold the routes to it:
// a search path under /api/v1 is read as an ordinary collection ("unknown
// collection"), which is what "there is no search route" looks like from
// outside. The door that does rank is TestGraphQLSearch's.
func TestSearchHasNoRESTRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	for _, path := range []string{"/api/v1/search", "/api/v1/core.substrate.reamde.dev/search"} {
		rec := env.do(t, http.MethodGet, path+"?q=hello", tok, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, body %s", path, rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, "unknown collection") {
			t.Fatalf("GET %s = %s, want the generic collection 404", path, body)
		}
	}
}

func TestDiscoveryDoesNotRequireAuth(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
}

func TestPrimaryPrefixServesResources(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/people", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if w := rec.Header().Get("Warning"); w != "" {
		t.Fatalf("primary /api/v1 carried a Warning header: %q", w)
	}
}

// The pre-v1 prefix is not served: it never shipped, so it is an ordinary
// unknown API path — a 404 problem object, never a quietly aliased 200.
func TestPreV1PrefixIsNotServed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1alpha1/people.substrate.reamde.dev/people", tok, nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// --- A6: the closed error set is real -----------------------------------

func TestUnsupportedIs501(t *testing.T) {
	// The fake dataset carries no bundle machinery, so the bundle status verb
	// is a capability-absent 501 → code unsupported (never internal).
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/core.substrate.reamde.dev/bundles/status", tok, nil)
	wantErrorCode(t, rec, http.StatusNotImplemented, codeUnsupported)
}

func TestUnavailableIs503WithRetryAfter(t *testing.T) {
	env := newTestEnv(t)
	env.svc.authErr = errors.New("repository open failed")
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/people", tok, nil)
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatalf("503 unavailable must carry Retry-After")
	}
}

func TestBadRequestIsEmittedAndCoded(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// A non-numeric changelog cursor is a bad_request, named, not silence.
	rec := env.do(t, http.MethodGet, changesPath+"?from=notanumber", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestGraphQLErrorCarriesProblemInExtensions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	// Wrap so errors.Is matches the sentinel, exactly like the engine.
	ds.errs["List"] = fmt.Errorf("label ns: %w", substrate.ErrForbidden)

	rec := env.do(t, http.MethodPost, "/api/v1/graphql", tok,
		map[string]any{"query": `{ records(first: 10) { nodes { id } } }`})
	wantStatus(t, rec, http.StatusOK)

	out := decodeJSON[struct {
		Errors []struct {
			Message    string         `json:"message"`
			Extensions map[string]any `json:"extensions"`
		} `json:"errors"`
	}](t, rec)
	if len(out.Errors) == 0 {
		t.Fatalf("expected a graphql error, body %s", rec.Body.String())
	}
	ext := out.Errors[0].Extensions
	if ext == nil || ext["code"] != codeForbidden {
		t.Fatalf("error extensions = %v, want the problem object with code %q", ext, codeForbidden)
	}
}

// --- A4: watch frames + horizon -----------------------------------------

func TestWatchTerminalErrorFrameOnMidStreamFailure(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.h)
	defer srv.Close()
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	// from=0 skips headSeq so the 200 + bookmark go out first; then the drain's
	// Changes read fails, and the stream must end with a terminal error frame
	// carrying the problem object — not a silent EOF.
	ds.errs["Changes"] = errBoom
	br, stop := startWatch(t, srv, changesPath+"?watch=1&from=0", tok)
	defer stop()

	if first := readLine(t, br); first["bookmark"] == nil {
		t.Fatalf("first line = %v, want a bookmark control frame", first)
	}
	frame := readLine(t, br)
	if _, hasSeq := frame["seq"]; hasSeq {
		t.Fatalf("terminal frame carried a seq: %v", frame)
	}
	errObj, ok := frame["error"].(map[string]any)
	if !ok {
		t.Fatalf("terminal frame = %v, want an error control frame", frame)
	}
	if errObj["code"] != codeInternal {
		t.Fatalf("terminal frame code = %v, want %q", errObj["code"], codeInternal)
	}
}

func TestCompactedBelowHorizonNonWatch(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// horizon is 0; a from below it exercises the compacted signal path.
	rec := env.do(t, http.MethodGet, changesPath+"?from=-1", tok, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
}

func TestCompactedBelowHorizonWatch(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// The watch path rejects a below-horizon resume before any 200/stream byte.
	rec := env.do(t, http.MethodGet, changesPath+"?watch=1&from=-5", tok, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
}
