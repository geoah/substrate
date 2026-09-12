package api

import (
	"net/http"
	"testing"
)

const tasksPath = "/api/v1/samples.substrate.reamde.dev/tasks/task"

// createPerson returns the id of a person record in the geoah dataset.
func createPerson(t *testing.T, env *testEnv, tok string) string {
	t.Helper()
	return createRecord(t, env, tok, personKind, map[string]any{"title": "Ada", "name": "Ada"}).ID
}

func TestPatchThroughTheWrongKindWritesNothing(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
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

func TestDeleteThroughTheWrongKindIsRefused(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	id := createPerson(t, env, tok)

	// The token kind's path must not delete a person (nor a person path a
	// token).
	rec := env.do(t, http.MethodDelete, "/api/v1/substrate.reamde.dev/core/token/"+id, tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if ds.records[id].DeletedAt != nil {
		t.Fatal("delete through a foreign kind tombstoned the record")
	}

	rec = env.do(t, http.MethodDelete, tasksPath+"/"+id, tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if ds.records[id].DeletedAt != nil {
		t.Fatal("delete through a foreign kind tombstoned the record")
	}
}

func TestWritesThroughTheRightKindStillWork(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	id := createPerson(t, env, tok)

	rec := env.do(t, http.MethodPatch, peoplePath+"/"+id, tok,
		map[string]any{"properties": map[string]any{"company": "Analytical"}})
	wantStatus(t, rec, http.StatusOK)

	rec = env.do(t, http.MethodDelete, peoplePath+"/"+id, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if ds.records[id].DeletedAt == nil {
		t.Fatal("delete through the right kind must tombstone")
	}
}

func TestWritingAMissingRecordIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	wantErrorCode(t, env.do(t, http.MethodPatch, peoplePath+"/nope", tok,
		map[string]any{"properties": map[string]any{"title": "x"}}), http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodDelete, peoplePath+"/nope", tok, nil),
		http.StatusNotFound, codeNotFound)
}
