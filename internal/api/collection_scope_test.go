package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const tasksPath = "/api/v1/samples.substrate.reamde.dev/tasks/task"

// createPerson returns the id of a person record in the geoah dataset.
func createPerson(t *testing.T, env *testEnv, tok string) string {
	t.Helper()
	rec := env.do(t, http.MethodPost, peoplePath, tok, map[string]any{
		"properties": map[string]any{"title": "Ada", "name": "Ada"},
	})
	wantStatus(t, rec, http.StatusCreated)
	return decodeJSON[substrate.Record](t, rec).ID
}

func TestPatchThroughTheWrongCollectionWritesNothing(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	id := createPerson(t, env, tok)
	before := ds.records[id].Version

	rec := env.do(t, http.MethodPatch, tasksPath+"/"+id, tok,
		map[string]any{"properties": map[string]any{"note": "pwned"}})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)

	ent := ds.records[id]
	if _, ok := ent.Properties["note"]; ok {
		t.Fatal("404 must mean nothing happened; the patch was committed")
	}
	if ent.Version != before {
		t.Fatalf("version = %d, want %d — the patch was committed", ent.Version, before)
	}
}

func TestDeleteThroughTheWrongCollectionIsRefused(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	id := createPerson(t, env, tok)

	// The token collection must not delete a person (nor a person
	// collection a token).
	rec := env.do(t, http.MethodDelete, "/api/v1/substrate.reamde.dev/core/tokens/"+id, tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if ds.records[id].DeletedAt != nil {
		t.Fatal("delete through a foreign collection tombstoned the record")
	}

	rec = env.do(t, http.MethodDelete, tasksPath+"/"+id, tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if ds.records[id].DeletedAt != nil {
		t.Fatal("delete through a foreign collection tombstoned the record")
	}
}

func TestMutationsThroughTheRightCollectionStillWork(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	id := createPerson(t, env, tok)

	rec := env.do(t, http.MethodPatch, peoplePath+"/"+id, tok,
		map[string]any{"properties": map[string]any{"company": "Analytical"}})
	wantStatus(t, rec, http.StatusOK)

	rec = env.do(t, http.MethodDelete, peoplePath+"/"+id, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if ds.records[id].DeletedAt == nil {
		t.Fatal("delete through the right collection must tombstone")
	}
}

func TestMutatingAMissingResourceIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	wantErrorCode(t, env.do(t, http.MethodPatch, peoplePath+"/nope", tok,
		map[string]any{"properties": map[string]any{"title": "x"}}), http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodDelete, peoplePath+"/nope", tok, nil),
		http.StatusNotFound, codeNotFound)
}
