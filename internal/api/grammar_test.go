package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A POST to a RECORD path used to resolve the collection, discard the id and
// create a record under a server-assigned one, answering 201: a client that
// believed it was upserting accumulated duplicates under ids it never chose
// (#202). The record path is four segments (decisions 0042, 0047); it refuses
// the POST as a method the path does not serve, and nothing is written.
func TestPostToRecordPathIsMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	path := peoplePath + "/p1"
	ds.lastPut = substrate.PutInput{}
	rec := env.do(t, http.MethodPost, path, tok,
		map[string]any{"properties": map[string]any{"text": "hello"}})
	wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	if ds.lastPut.Kind != "" {
		t.Fatalf("POST %s wrote %+v; a refused method writes nothing", path, ds.lastPut)
	}
}

// The mirror: a PUT, PATCH or DELETE at the list route addresses no record,
// so it is refused and nothing is written. The create is the POST there.
func TestWriteVerbsAtTheListRouteAreMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	ds.lastPut = substrate.PutInput{}
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, recordsPath, tok,
			map[string]any{"kind": personKind, "properties": map[string]any{"title": "Ada"}})
		wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	}
	if ds.lastPut.Kind != "" {
		t.Fatalf("a refused method wrote %+v", ds.lastPut)
	}
}

// A record's URL is its stored reference value. The path after the version
// prefix is exactly what a `reference` property holds, which is the whole
// reason the collection segment is the kind's name (decision 0033).
func TestRecordURLIsItsReference(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	const kind = "samples.substrate.reamde.dev/people/person"
	const id = "r1"
	ds.put(&substrate.Record{ID: id, Kind: kind, Version: 1})
	path := "/api/" + APIVersion + "/" + vocabulary.RecordPath(kind, id)
	rec := env.do(t, http.MethodGet, path, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != kind || got.ID != id {
		t.Fatalf("GET %s served %s/%s", path, got.Kind, got.ID)
	}
}

// The kinds the merge/split actions used to shadow are reachable:
// `recordmerge` is a shipped kind, and `POST /{core}/recordmerges` sat where
// its records do. The action moved to /api/v1/merge (#202).
func TestShadowedKindsAreReachable(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	const kind = "substrate.reamde.dev/core/recordmerge"
	ds.types = append(ds.types, substrate.KindInfo{
		Identity: kind, Name: "recordmerge",
		Authority: coreAuthorityName, Package: "core", Version: 1, Source: "builtin",
	})
	ds.put(&substrate.Record{ID: "m1", Kind: kind, Properties: map[string]any{}})
	rec := env.do(t, http.MethodGet, recordsOf(t, kind), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	rec = env.do(t, http.MethodGet, "/api/v1/"+corePackage+"/recordmerge/m1", tok, nil)
	wantStatus(t, rec, http.StatusOK)
}

// A path shorter than a record reference names nothing: every kind carries an
// authority AND a package (decisions 0042, 0047) and there is no collection
// route, so the old authority-less shape (`/api/v1/note`), the old
// two-segment collection (`/api/v1/example.com/note`) and the kind reference
// alone (`/api/v1/example.com/notes/note`) are gone and answer 404 rather
// than the console's index.html.
func TestShortPathIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	for _, path := range []string{"/api/v1/note", "/api/v1/example.com/note", "/api/v1/example.com/notes/note"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			rec := env.do(t, method, path, tok, map[string]any{})
			wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		}
	}
}
