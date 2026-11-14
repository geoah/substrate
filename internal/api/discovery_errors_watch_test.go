package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const changesPath = "/api/v1/changes"

// --- A1: discovery + versioning + the deprecation channel ---------------

func TestDiscoveryReportsVersionsAndBuild(t *testing.T) {
	h := New(Config{Service: &stubService{}})

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
	// handle a missing one. What is in it is features_test.go's.
	if body := rec.Body.String(); !strings.Contains(body, `"features":[{`) {
		t.Fatalf("features did not serialize as a list: %s", body)
	}
}

// The surface verdict is one object, not a stamp per feature: REST is the
// supported developer interface (decision 0053), and MCP the assistants'
// door, a preview (decision 0230). Each names its endpoint, and the object
// is read off the wire as bytes too, because a client reads JSON and a
// renamed key would still decode into the Go struct.
func TestDiscoveryNamesEachSurfaceWithItsCompatibility(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[discoveryDoc](t, rec)

	if want := (surfaceInfo{Endpoint: "/api/v1", Compatibility: "supported"}); doc.Surfaces.REST != want {
		t.Fatalf("surfaces.rest = %+v, want %+v", doc.Surfaces.REST, want)
	}
	if want := (surfaceInfo{Endpoint: "/mcp", Compatibility: "preview"}); doc.Surfaces.MCP != want {
		t.Fatalf("surfaces.mcp = %+v, want %+v", doc.Surfaces.MCP, want)
	}
	wire := `"surfaces":{"rest":{"endpoint":"/api/v1","compatibility":"supported"},"mcp":{"endpoint":"/mcp","compatibility":"preview"}}`
	if body := rec.Body.String(); !strings.Contains(body, wire) {
		t.Fatalf("surfaces did not serialize as %s: %s", wire, body)
	}

	// Every surface a feature names is a key of the object, so a client can
	// follow a feature to its door.
	for _, f := range doc.Features {
		for _, s := range f.Surfaces {
			if s != surfaceREST && s != surfaceMCP {
				t.Fatalf("feature %q names surface %q, which the surfaces object does not carry", f.Name, s)
			}
		}
	}

	// The grammar names the two routes a client addresses records through:
	// the one list route and the record path.
	if doc.Grammar.Collection != recordsPath {
		t.Fatalf("grammar.collection = %q, want %q", doc.Grammar.Collection, recordsPath)
	}
	if want := "/api/v1/{authority}/{package}/{kind}/{id}"; doc.Grammar.RecordPath != want {
		t.Fatalf("grammar.recordPath = %q, want %q", doc.Grammar.RecordPath, want)
	}
}

// A search path of its own under /api/v1 names nothing: the `search`
// feature is the records route ranking (discovery_errors_watch_db_test.go),
// and this path is the router's 404.
func TestSearchHasNoPathOfItsOwn(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, "/api/v1/search?q=hello", stubToken, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if body := rec.Body.String(); !strings.Contains(body, "no such API path") {
		t.Fatalf("GET /api/v1/search = %s, want the router's 404", body)
	}
}

func TestDiscoveryDoesNotRequireAuth(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
}

// Discovery names the host a short repository name is completed under, the
// public URL's (decision record 0288), and names it empty where none is
// configured: always present, so a client tells that server from an older
// one, which sends no field and completes under the request's host.
func TestDiscoveryNamesTheHostAShortNameIsCompletedUnder(t *testing.T) {
	for publicURL, want := range map[string]string{
		"https://substrate.example.com": "substrate.example.com",
		"http://localhost:8080":         "localhost",
		"":                              "",
	} {
		env := newStubEnv(t, nil, func(c *Config) { c.PublicURL = publicURL })
		rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
		wantStatus(t, rec, http.StatusOK)
		var doc struct {
			Registration map[string]any `json:"registration"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if got, present := doc.Registration["nameHost"]; !present || got != want {
			t.Errorf("public URL %q: registration.nameHost = %v (present %v), want %q", publicURL, got, present, want)
		}
	}
}

// discoveryEmptyList is a repository whose list answers an empty page at a
// fixed head: the one read GET /records makes.
type discoveryEmptyList struct{ changesStub }

func (discoveryEmptyList) List(context.Context, substrate.Query) (*substrate.Page, error) {
	return &substrate.Page{Records: []*substrate.Record{}}, nil
}

func TestPrimaryPrefixServesResources(t *testing.T) {
	env := newStubEnv(t, discoveryEmptyList{})
	rec := env.do(t, http.MethodGet, recordsPath, stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	if w := rec.Header().Get("Warning"); w != "" {
		t.Fatalf("primary /api/v1 carried a Warning header: %q", w)
	}
}

// An unknown version prefix is an ordinary unknown API path: a 404 problem
// object, never a quietly aliased 200.
func TestUnknownVersionPrefixIsNotServed(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, "/api/v1alpha1/records", stubToken, nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// --- A6: the closed error set is real -----------------------------------

func TestUnavailableIs503WithRetryAfter(t *testing.T) {
	env := newStubEnvOn(t, requestLogUnavailable{stubService: &stubService{}, err: errors.New("repository open failed")})
	rec := env.do(t, http.MethodGet, recordsPath, stubToken, nil)
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatalf("503 unavailable must carry Retry-After")
	}
}

func TestBadRequestIsEmittedAndCoded(t *testing.T) {
	env := newStubEnv(t, nil)
	// A non-numeric changelog cursor is a bad_request, named, not silence.
	rec := env.do(t, http.MethodGet, changesPath+"?from=notanumber", stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

// --- A4: watch frames + horizon -----------------------------------------

func TestCompactedBelowHorizonNonWatch(t *testing.T) {
	env := newStubEnv(t, changesStub{})
	// horizon is 0; a from below it exercises the compacted signal path.
	rec := env.do(t, http.MethodGet, changesPath+"?from=-1", stubToken, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
}

func TestCompactedBelowHorizonWatch(t *testing.T) {
	env := newStubEnv(t, changesStub{})
	// The watch path rejects a below-horizon resume before any 200/stream byte.
	rec := env.do(t, http.MethodGet, changesPath+"?watch=1&from=-5", stubToken, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
}
