package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// notePath is the repository-local shape: one segment for the collection, two
// for a record. It is where both silent creates lived (#202).
const notePath = "/api/v1/note"

// A POST to a RECORD path used to resolve the collection, discard the id and
// create a record under a server-assigned one, answering 201: a client that
// believed it was upserting accumulated duplicates under ids it never chose
// (#202). Both address depths refuse it now, and nothing is written.
func TestPostToRecordPathIsMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	for _, path := range []string{notePath + "/n1", peoplePath + "/p1"} {
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
}

// The mirror: a PUT to a COLLECTION path used to create a record under a random
// id, so a client that meant an upsert got a new row every call.
func TestPutToCollectionPathIsMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	for _, path := range []string{peoplePath, notePath} {
		ds.lastPut = substrate.PutInput{}
		rec := env.do(t, http.MethodPut, path, tok,
			map[string]any{"properties": map[string]any{"title": "Ada"}})
		wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
		if ds.lastPut.Kind != "" {
			t.Fatalf("PUT %s wrote %+v; a refused method writes nothing", path, ds.lastPut)
		}
		if msg := decodeJSON[errorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "POST "+path) {
			t.Fatalf("PUT %s said %q; it must name the POST that creates", path, msg)
		}
	}
	// PATCH and DELETE address a record too, and used to reach the engine with
	// an empty id.
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, peoplePath, tok, map[string]any{})
		wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	}
}

// A record's URL is its stored reference value. The path after the version
// prefix is exactly what a `reference` property holds, for both kind shapes,
// which is the whole reason the collection segment is the kind's name
// (decision 0033).
func TestRecordURLIsItsReference(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	for _, kind := range []string{"people.substrate.reamde.dev/person", "note"} {
		const id = "r1"
		ds.put(&substrate.Record{ID: id, Kind: kind, Version: 1})
		path := "/api/" + APIVersion + "/" + vocabulary.RecordPath(kind, id)
		rec := env.do(t, http.MethodGet, path, tok, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.Record](t, rec); got.Kind != kind || got.ID != id {
			t.Fatalf("GET %s served %s/%s", path, got.Kind, got.ID)
		}
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

// A three-segment path whose first segment is not an authority spells no
// address, so it is a 404 rather than a lookup of a kind that cannot exist.
func TestNonAuthorityThreeSegmentPathIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/note/n1/extra", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
