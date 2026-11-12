package api

import (
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
	env := newTestEnv(t)
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

// The `search` feature is a claim about a route, so hold the route to it:
// `GET /records?q=` ranks, and the answer is the ranked page with a score per
// hit. A search path of its own under /api/v1 names nothing.
func TestSearchIsTheRecordsRouteRanking(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "p1", Kind: personKind, Title: "Ada Lovelace", Properties: map[string]any{}})
	ds.put(&substrate.Record{ID: "p2", Kind: personKind, Title: "Grace Hopper", Properties: map[string]any{}})

	rec := env.do(t, http.MethodGet, recordsPath+"?q=ada", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.RankedPage](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p1" {
		t.Fatalf("ranked page = %+v", page)
	}
	if page.Scores[personKind+"/p1"].Lexical == 0 {
		t.Fatalf("the hit carries no score: %+v", page.Scores)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/search?q=hello", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if body := rec.Body.String(); !strings.Contains(body, "no such API path") {
		t.Fatalf("GET /api/v1/search = %s, want the router's 404", body)
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
	rec := env.do(t, http.MethodGet, recordsPath, tok, nil)
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
	rec := env.do(t, http.MethodGet, "/api/v1alpha1/records", tok, nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// --- A6: the closed error set is real -----------------------------------

func TestUnavailableIs503WithRetryAfter(t *testing.T) {
	env := newTestEnv(t)
	env.svc.authErr = errors.New("repository open failed")
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, recordsPath, tok, nil)
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
