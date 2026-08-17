package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// notePath is the repository-local shape: one segment for the collection, two
// for a record. It is where both silent creates lived.
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

// The mirror: a PUT to a COLLECTION path used to create a record under a
// random id, so a client that meant an upsert got a new row every call.
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
// which is the whole reason the collection segment is the kind's name.
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

// The reserved segment is a verb's, never an id's or a kind's. A record whose
// id is `incoming`, `edges` or `status` is addressed like any other, because
// no verb sits where an id can be.
func TestReservedVerbSegmentKeepsIdsAddressable(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	for _, id := range []string{"incoming", "edges", "status", "call"} {
		ds.put(&substrate.Record{ID: id, Kind: "note", Version: 1})
		rec := env.do(t, http.MethodGet, notePath+"/"+id, tok, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.Record](t, rec); got.ID != id {
			t.Fatalf("GET %s/%s served %q", notePath, id, got.ID)
		}
	}
	// And the segment itself addresses nothing: no id begins with it and no
	// kind is spelled with it.
	wantErrorCode(t, env.do(t, http.MethodGet, notePath+"/"+verbSegment, tok, nil),
		http.StatusNotFound, codeNotFound)
}

// The collections the verb routes used to shadow are reachable: `recordmerge`
// and `recordsplit` are shipped kinds, and `POST /{core}/recordmerges` sat
// where their collections do.
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

// The four-segment dashless shapes the old grammar served. Neither answers
// 405: `…/{kind}/{id}/incoming` is now an ordinary record read of a record
// whose id is `incoming`, which is the whole point of the reserved segment,
// and `…/{kind}/{id}/edges/{rel}` matches no route at all.
func TestDashlessSubresourcePathsAreNotVerbs(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.put(&substrate.Record{ID: "incoming", Kind: "people.substrate.reamde.dev/person", Version: 1})

	rec := env.do(t, http.MethodGet, peoplePath+"/incoming", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.ID != "incoming" {
		t.Fatalf("GET %s/incoming served %q, want the record", peoplePath, got.ID)
	}

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := env.do(t, method, peoplePath+"/p1/edges/member_of", tok, map[string]any{"id": "org1"})
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	}
}
