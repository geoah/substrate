package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A POST to a RECORD path used to resolve the collection, discard the id and
// create a record under a server-assigned one, answering 201: a client that
// believed it was upserting accumulated duplicates under ids it never chose
// (#202). A record path is three segments now (decision 0042); it refuses the
// POST, and nothing is written.
func TestPostToRecordPathIsMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	path := peoplePath + "/p1"
	ds.lastPut = substrate.PutInput{}
	rec := env.do(t, http.MethodPost, path, tok,
		map[string]any{"properties": map[string]any{"text": "hello"}})
	wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	if ds.lastPut.Kind != "" {
		t.Fatalf("POST %s wrote %+v; a refused method writes nothing", path, ds.lastPut)
	}
	if msg := decodeJSON[errorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "PUT "+path) {
		t.Fatalf("POST %s said %q; it must name the PUT that writes this record", path, msg)
	}
}

// The mirror: a PUT to a COLLECTION path used to create a record under a random
// id, so a client that meant an upsert got a new row every call.
func TestPutToCollectionPathIsMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	ds.lastPut = substrate.PutInput{}
	rec := env.do(t, http.MethodPut, peoplePath, tok,
		map[string]any{"properties": map[string]any{"title": "Ada"}})
	wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	if ds.lastPut.Kind != "" {
		t.Fatalf("PUT %s wrote %+v; a refused method writes nothing", peoplePath, ds.lastPut)
	}
	if msg := decodeJSON[errorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "POST "+peoplePath) {
		t.Fatalf("PUT %s said %q; it must name the POST that creates", peoplePath, msg)
	}
	// PATCH and DELETE address a record too, and used to reach the engine with
	// an empty id.
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, peoplePath, tok, map[string]any{})
		wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	}
}

// A record's URL is its stored reference value. The path after the version
// prefix is exactly what a `reference` property holds, which is the whole
// reason the collection segment is the kind's name (decision 0033).
func TestRecordURLIsItsReference(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	const kind = "people.substrate.reamde.dev/person"
	const id = "r1"
	ds.put(&substrate.Record{ID: id, Kind: kind, Version: 1})
	path := "/api/" + APIVersion + "/" + vocabulary.RecordPath(kind, id)
	rec := env.do(t, http.MethodGet, path, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != kind || got.ID != id {
		t.Fatalf("GET %s served %s/%s", path, got.Kind, got.ID)
	}
}

// The collections the merge/split actions used to shadow are reachable:
// `recordmerge` is a shipped kind, and `POST /{core}/recordmerges` sat where
// its collection does. The action moved to /api/v1/merge (#202).
func TestShadowedCollectionsAreReachable(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.types = append(ds.types, substrate.KindInfo{
		Identity: "core.substrate.reamde.dev/recordmerge", Name: "recordmerge",
		Authority: coreAuthority, Version: 1, Plural: "recordmerges", Source: "builtin",
	})
	rec := env.do(t, http.MethodGet, "/api/v1/"+coreAuthority+"/recordmerge", tok, nil)
	wantStatus(t, rec, http.StatusOK)
}

// A record id may not be a sub-resource word. `…/{kind}/{id}/incoming` is a
// static route, so a record whose id is `incoming` reads through the incoming
// handler and 405s while a PUT there used to create a person nothing could
// read. Both directions refuse now, and nothing is written, so the corner is
// symmetric (decision 0033).
func TestReservedRecordIdsRefuseBothDirections(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	for _, id := range []string{"incoming", "edges"} {
		ds.lastPut = substrate.PutInput{}
		put := env.do(t, http.MethodPut, peoplePath+"/"+id, tok,
			map[string]any{"properties": map[string]any{"name": "x"}})
		wantErrorCode(t, put, http.StatusBadRequest, codeBadRequest)
		if ds.lastPut.Kind != "" {
			t.Fatalf("PUT %s/%s wrote %+v; a reserved id is refused, not written", peoplePath, id, ds.lastPut)
		}
		del := env.do(t, http.MethodDelete, peoplePath+"/"+id, tok, nil)
		wantErrorCode(t, del, http.StatusBadRequest, codeBadRequest)
	}
}

// A one-segment path names no kind: every kind carries an authority, so the
// old authority-less collection shape (`/api/v1/note`) is gone and answers 404
// rather than the console's index.html (decision 0042).
func TestOneSegmentPathIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, "/api/v1/note", tok, map[string]any{})
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	}
}
