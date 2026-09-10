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

const changesPath = "/api/v1/changes"

// --- A1: discovery + versioning + the deprecation channel ---------------

// wantFeatureSurfaces is the roster this deployment reports, with the
// surfaces each entry claims, written out here rather than read off
// api.features: a feature added to the literal without a route behind it has
// to be added here too, by hand.
var wantFeatureSurfaces = map[string][]string{
	"triggers":   {surfaceREST},
	"functions":  {surfaceREST},
	"bundles":    {surfaceREST},
	"blobs":      {surfaceREST},
	"export":     {surfaceREST},
	"changefeed": {surfaceREST, surfaceGraphQL},
	"search":     {surfaceGraphQL},
	"embeddings": {surfaceGraphQL},
	"agents":     {surfaceREST},
}

func TestDiscoveryReportsVersionsAndBuild(t *testing.T) {
	svc := newFakeService()
	h := New(Config{Service: svc})

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
	if v1 := doc.Versions[0]; v1.Name != "v1" || v1.Status != "served" {
		t.Fatalf("v1 version = %+v", v1)
	}

	// The retention horizon is 0 today: every committed seq is resumable.
	if doc.Changelog.Horizon != 0 {
		t.Fatalf("horizon = %d, want 0", doc.Changelog.Horizon)
	}

	if version := doc.Server.Version; version == "" {
		t.Fatalf("server version missing")
	}
	// A list, never null: a client that decodes an array must not have to
	// handle a missing one. What is in it is the two tests below.
	if body := rec.Body.String(); !strings.Contains(body, `"features":[{`) {
		t.Fatalf("features did not serialize as a list: %s", body)
	}
}

// Every feature says which surfaces serve it, because the two are not
// equivalent: search is the GraphQL query's alone, and a client that read a
// listed feature as "a REST route exists" went looking for one that never
// shipped.
func TestDiscoveryFeaturesNameTheirSurfaces(t *testing.T) {
	h := New(Config{Service: newFakeService()})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil))
	doc := decodeJSON[discoveryDoc](t, rec)

	want := wantFeatureSurfaces
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

// The surface verdict is one object, not a stamp per feature: REST is the
// supported developer interface and every part of GraphQL is preview (decision
// 0053). Each surface names its endpoint so a preview door is locatable from
// discovery alone, and the GraphQL endpoint is held to the mounted route. The
// object is read off the wire as bytes too, because a client reads JSON and a
// renamed key would still decode into the Go struct.
func TestDiscoveryNamesEachSurfaceWithItsCompatibility(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[discoveryDoc](t, rec)

	if want := (surfaceInfo{Endpoint: "/api/v1", Compatibility: "supported"}); doc.Surfaces.REST != want {
		t.Fatalf("surfaces.rest = %+v, want %+v", doc.Surfaces.REST, want)
	}
	if want := (surfaceInfo{Endpoint: "/api/v1/graphql", Compatibility: "preview"}); doc.Surfaces.GraphQL != want {
		t.Fatalf("surfaces.graphql = %+v, want %+v", doc.Surfaces.GraphQL, want)
	}
	wire := `"surfaces":{"rest":{"endpoint":"/api/v1","compatibility":"supported"},` +
		`"graphql":{"endpoint":"/api/v1/graphql","compatibility":"preview"}}`
	if body := rec.Body.String(); !strings.Contains(body, wire) {
		t.Fatalf("surfaces did not serialize as %s: %s", wire, body)
	}

	// Every surface a feature names is a key of the object, so a client can
	// follow a feature to its door.
	for _, f := range doc.Features {
		for _, s := range f.Surfaces {
			if s != surfaceREST && s != surfaceGraphQL {
				t.Fatalf("feature %q names surface %q, which the surfaces object does not carry", f.Name, s)
			}
		}
	}

	// The advertised GraphQL endpoint is the one that answers.
	tok := env.svc.token(fakeRepository)
	gql := env.do(t, http.MethodPost, doc.Surfaces.GraphQL.Endpoint, tok,
		map[string]any{"query": `{ __typename }`})
	wantStatus(t, gql, http.StatusOK)
}

// The gql-only marker is a claim about the routes, so hold the routes to it:
// a search path under /api/v1 is read as an ordinary collection ("unknown
// collection"), which is what "there is no search route" looks like from
// outside. The door that does rank is TestGraphQLSearch's.
func TestSearchHasNoRESTRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	// A one-segment path names no kind (decision 0042), so it is the router's
	// generic 404; a two-segment path is a collection lookup that misses.
	for path, want := range map[string]string{
		"/api/v1/search": "no such API path",
		"/api/v1/substrate.reamde.dev/core/search": "unknown collection",
	} {
		rec := env.do(t, http.MethodGet, path+"?q=hello", tok, nil)
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		if body := rec.Body.String(); !strings.Contains(body, want) {
			t.Fatalf("GET %s = %s, want %q", path, body, want)
		}
	}
}

// discoveryFeatures reads the feature list one service reports.
func discoveryFeatures(t *testing.T, svc substrate.Service) map[string]string {
	t.Helper()
	h := New(Config{Service: svc})
	req := httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := map[string]string{}
	for _, f := range decodeJSON[discoveryDoc](t, rec).Features {
		got[f.Name] = f.Stability
	}
	return got
}

// Every feature carries the stability its surface has reached. `stable`
// means frozen for v1, and every feature of the supported REST surface is.
// `search` stays beta because its only door is the preview GraphQL surface,
// and that surface is the only door to embeddings too. `agents` and
// `embeddings` stay alpha because both shapes are still moving.
// Change a stamp here and in the features literal together, and a stable one
// only with a decision record, as 0053 scheduled this flip: it is a promise a
// client has already read.
func TestDiscoveryStampsEachFeatureStability(t *testing.T) {
	got := discoveryFeatures(t, newFakeService())

	want := map[string]string{
		"triggers":              substrate.StabilityStable,
		"functions":             substrate.StabilityStable,
		"bundles":               substrate.StabilityStable,
		"blobs":                 substrate.StabilityStable,
		"export":                substrate.StabilityBeta,
		"changefeed":            substrate.StabilityStable,
		"search":                substrate.StabilityBeta,
		"embeddings":            substrate.StabilityAlpha,
		substrate.FeatureAgents: substrate.AgentStability,
	}
	if len(got) != len(want) {
		t.Fatalf("features = %+v, want %d entries", got, len(want))
	}
	for name, stability := range want {
		if got[name] != stability {
			t.Fatalf("feature %q stability = %q, want %q", name, got[name], stability)
		}
	}
	// The agent surface reads its stability off the substrate marker, never a
	// literal here.
	if got[substrate.FeatureAgents] != "alpha" {
		t.Fatalf("agents feature = %q, want alpha", got[substrate.FeatureAgents])
	}
}

func TestDiscoveryDoesNotRequireAuth(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
}

func TestPrimaryPrefixServesResources(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1/samples.substrate.reamde.dev/people/person", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if w := rec.Header().Get("Warning"); w != "" {
		t.Fatalf("primary /api/v1 carried a Warning header: %q", w)
	}
}

// An unknown version prefix is an ordinary unknown API path: a 404 problem
// object, never a quietly aliased 200.
func TestUnknownVersionPrefixIsNotServed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1alpha1/samples.substrate.reamde.dev/people/person", tok, nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// --- A6: the closed error set is real -----------------------------------

func TestUnavailableIs503WithRetryAfter(t *testing.T) {
	env := newTestEnv(t)
	env.svc.authErr = errors.New("repository open failed")
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1/samples.substrate.reamde.dev/people/person", tok, nil)
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatalf("503 unavailable must carry Retry-After")
	}
}

func TestBadRequestIsEmittedAndCoded(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	// A non-numeric changelog cursor is a bad_request, named, not silence.
	rec := env.do(t, http.MethodGet, changesPath+"?from=notanumber", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestGraphQLErrorCarriesProblemInExtensions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
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
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

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
	tok := env.svc.token(fakeRepository)
	// horizon is 0; a from below it exercises the compacted signal path.
	rec := env.do(t, http.MethodGet, changesPath+"?from=-1", tok, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
}

func TestCompactedBelowHorizonWatch(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	// The watch path rejects a below-horizon resume before any 200/stream byte.
	rec := env.do(t, http.MethodGet, changesPath+"?watch=1&from=-5", tok, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
}
