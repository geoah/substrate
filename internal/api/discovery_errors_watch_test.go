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

// wantFeatureSurfaces is the whole roster a deployment can report, with the
// surfaces each entry claims. Every discovery test reads it, so a new feature
// is declared once and no test can drift from another.
var wantFeatureSurfaces = map[string][]string{
	"triggers":   {surfaceREST},
	"functions":  {surfaceREST},
	"bundles":    {surfaceREST},
	"blobs":      {surfaceREST},
	"changefeed": {surfaceREST, surfaceGraphQL},
	"search":     {surfaceGraphQL},
	"embeddings": {surfaceGraphQL},
	"agents":     {surfaceREST},
}

func TestDiscoveryReportsVersionsFeaturesDialect(t *testing.T) {
	svc := newFakeService()
	h := New(Config{Service: svc, MaxDialect: 6, MaxChangelogDialect: 3})

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

	// Retention horizon is 0 today, and the changelog carries its own binary
	// max: the dialect of entries this binary can replay.
	if doc.Changelog.Horizon != 0 {
		t.Fatalf("horizon = %d, want 0", doc.Changelog.Horizon)
	}
	if doc.Changelog.MaxDialect != 3 {
		t.Fatalf("changelog maxDialect = %d, want 3", doc.Changelog.MaxDialect)
	}

	// The feature list follows the fake's seams: it carries ChangeFeedOps and
	// nothing else, and the fake has no embedder, so every other extension
	// feature is absent rather than advertised over a 501.
	stab := map[string]string{}
	for _, f := range doc.Features {
		stab[f.Name] = f.Stability
	}
	for name, want := range map[string]string{
		"changefeed": substrate.StabilityBeta,
		"search":     substrate.StabilityBeta,
	} {
		if stab[name] != want {
			t.Fatalf("feature %q stability = %q, want %q", name, stab[name], want)
		}
	}
	for _, absent := range []string{"triggers", "functions", "bundles", "blobs", featureEmbeddings, substrate.FeatureAgents} {
		if _, ok := stab[absent]; ok {
			t.Fatalf("feature %q advertised, but nothing here serves it", absent)
		}
	}
	if version := doc.Server.Version; version == "" {
		t.Fatalf("server version missing")
	}
}

// Every feature says which surfaces serve it, because the two are not
// equivalent: search is the GraphQL query's alone, and a client that read a
// listed feature as "a REST route exists" went looking for one that never
// shipped. Read against a dataset carrying every seam, so the whole roster is
// on the wire.
func TestDiscoveryFeaturesNameTheirSurfaces(t *testing.T) {
	svc := newFakeService()
	svc.embeddings = true
	h := New(Config{Service: allSeamsService{svc}})

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

// The gql-only marker is a claim about the routes, so hold the routes to it:
// a search path under /api/v1 is read as an ordinary collection ("unknown
// collection"), which is what "there is no search route" looks like from
// outside. The door that does rank is TestGraphQLSearch's.
func TestSearchHasNoRESTRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	// A one-segment path names no kind (decision 0042), so it is the router's
	// generic 404; a two-segment path is a collection lookup that misses.
	for path, want := range map[string]string{
		"/api/v1/search": "no such API path",
		"/api/v1/core.substrate.reamde.dev/search": "unknown collection",
	} {
		rec := env.do(t, http.MethodGet, path+"?q=hello", tok, nil)
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		if body := rec.Body.String(); !strings.Contains(body, want) {
			t.Fatalf("GET %s = %s, want %q", path, body, want)
		}
	}
}

// allSeams satisfies every optional Dataset extension by embedding the
// interfaces. Discovery only type-asserts the probe, so no method is ever
// called and none needs a body.
type allSeams struct {
	substrate.Dataset
	substrate.AutomationOps
	substrate.BundleOps
	substrate.BundleInstaller
	substrate.BlobStore
	substrate.ChangeFeedOps
	substrate.AgentOps
}

type allSeamsService struct{ *fakeService }

func (s allSeamsService) DatasetSeams() substrate.Dataset { return (*allSeams)(nil) }

// bundleLifecycleOnly serves the bundle lifecycle verbs and nothing else: it
// is a BundleOps that cannot install a catalog closure.
type bundleLifecycleOnly struct {
	substrate.Dataset
	substrate.BundleOps
}

type bundleLifecycleService struct{ *fakeService }

func (s bundleLifecycleService) DatasetSeams() substrate.Dataset {
	return (*bundleLifecycleOnly)(nil)
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

// A deployment whose datasets carry every seam advertises every feature, each
// with the stability the surface has actually reached. `stable` means frozen
// for v1 and nothing here is: the paths all move under the settled path
// grammar (#202). Change a stamp here and in features() together, and only
// with the ticket that froze the surface.
func TestDiscoveryStampsEachFeatureStability(t *testing.T) {
	svc := newFakeService()
	svc.embeddings = true
	got := discoveryFeatures(t, allSeamsService{svc})

	want := map[string]string{
		"triggers":              substrate.StabilityBeta,
		"functions":             substrate.StabilityBeta,
		"bundles":               substrate.StabilityBeta,
		"blobs":                 substrate.StabilityBeta,
		"changefeed":            substrate.StabilityBeta,
		"search":                substrate.StabilityBeta,
		featureEmbeddings:       substrate.StabilityAlpha,
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

// The embed queue drains through an Embedder the host wires in, and the
// semantic arm refuses without one, so a deployment with no embedder does not
// list the feature however many seams its datasets carry.
func TestDiscoveryOmitsEmbeddingsWithoutAnEmbedder(t *testing.T) {
	svc := newFakeService()
	svc.embeddings = false
	got := discoveryFeatures(t, allSeamsService{svc})

	if _, ok := got[featureEmbeddings]; ok {
		t.Fatalf("embeddings advertised on a deployment with no embedder: %+v", got)
	}
	// The other entries are undisturbed: this drops one, not the list.
	if got["search"] != substrate.StabilityBeta {
		t.Fatalf("search = %q, want %q", got["search"], substrate.StabilityBeta)
	}
	if len(got) != len(wantFeatureSurfaces)-1 {
		t.Fatalf("features = %+v, want every entry but embeddings", got)
	}

	// Dropping one entry must not disturb its neighbours' surfaces either.
	h := New(Config{Service: allSeamsService{svc}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil))
	for _, f := range decodeJSON[discoveryDoc](t, rec).Features {
		if !slices.Equal(f.Surfaces, wantFeatureSurfaces[f.Name]) {
			t.Fatalf("feature %q surfaces = %v, want %v", f.Name, f.Surfaces, wantFeatureSurfaces[f.Name])
		}
	}
}

// The `bundles` entry stands for the lifecycle verbs AND catalog install, and
// install is a different seam (substrate.BundleInstaller, asserted in
// internal/catalog). A dataset that serves only half the surface advertises
// none of it: half a feature is what a client cannot route around.
func TestDiscoveryOmitsBundlesWithoutTheInstaller(t *testing.T) {
	got := discoveryFeatures(t, bundleLifecycleService{newFakeService()})

	if _, ok := got["bundles"]; ok {
		t.Fatalf("bundles advertised without BundleInstaller: %+v", got)
	}
}

// noSeamReporter is a Service with the seam report hidden: embedding the
// INTERFACE promotes only the methods Service names, so DatasetSeams and
// EmbeddingsEnabled are both gone.
type noSeamReporter struct{ substrate.Service }

// A service that reports no seams lists exactly what substrate.Dataset itself
// guarantees. Search is on that interface and /api/v1/graphql serves it, so
// hiding it would hide a capability that works; every extension depends on a
// seam nobody reported, so listing one would send a client to a 501.
func TestDiscoveryWithoutASeamReporterListsTheCoreReads(t *testing.T) {
	var svc substrate.Service = noSeamReporter{newFakeService()}
	if _, ok := svc.(substrate.SeamReporter); ok {
		t.Fatalf("noSeamReporter reports seams; this test asserts the opposite")
	}
	h := New(Config{Service: svc})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/substrate/server.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	doc := decodeJSON[discoveryDoc](t, rec)
	if len(doc.Features) != 1 || doc.Features[0].Name != "search" {
		t.Fatalf("features = %+v, want search alone", doc.Features)
	}
	// A list, never null: a client that decodes an array must not have to
	// handle a missing one.
	if body := rec.Body.String(); !strings.Contains(body, `"features":[{"name":"search"`) {
		t.Fatalf("features did not serialize as a list: %s", body)
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
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/person", tok, nil)
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
	rec := env.do(t, http.MethodGet, "/api/v1alpha1/people.substrate.reamde.dev/person", tok, nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// --- A6: the closed error set is real -----------------------------------

func TestUnsupportedIs501(t *testing.T) {
	// The fake dataset carries no bundle machinery, so the bundle status verb
	// is a capability-absent 501 → code unsupported (never internal).
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/core.substrate.reamde.dev/bundle/status", tok, nil)
	wantErrorCode(t, rec, http.StatusNotImplemented, codeUnsupported)
}

func TestUnavailableIs503WithRetryAfter(t *testing.T) {
	env := newTestEnv(t)
	env.svc.authErr = errors.New("repository open failed")
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/people.substrate.reamde.dev/person", tok, nil)
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
