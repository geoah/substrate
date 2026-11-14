package api

import (
	"net/http"
	"testing"
)

// A patch addressed through another kind's path is a 404 and writes nothing:
// the person keeps its properties and its version.
func TestPatchThroughTheWrongKindWritesNothing(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	id := createPerson(t, env.testEnv, env.token)
	before := env.get(t, personKind, id)

	rec := env.do(t, http.MethodPatch, tasksPath+"/"+id, env.token,
		map[string]any{"properties": map[string]any{"name": "pwned"}})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)

	got := env.get(t, personKind, id)
	if got.Properties["name"] != "Ada" {
		t.Fatalf("404 must mean nothing happened; the patch was committed: %v", got.Properties)
	}
	if got.Version != before.Version {
		t.Fatalf("version = %d, want %d — the patch was committed", got.Version, before.Version)
	}
}

// A delete addressed through another kind's path is a 404 and tombstones
// nothing: the token kind's path does not delete a person, nor a task's.
func TestDeleteThroughTheWrongKindIsRefused(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	id := createPerson(t, env.testEnv, env.token)

	rec := env.do(t, http.MethodDelete, "/api/v1/substrate.reamde.dev/core/token/"+id, env.token, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if got := env.get(t, personKind, id); got == nil || got.DeletedAt != nil {
		t.Fatal("delete through a foreign kind tombstoned the record")
	}

	rec = env.do(t, http.MethodDelete, tasksPath+"/"+id, env.token, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if got := env.get(t, personKind, id); got == nil || got.DeletedAt != nil {
		t.Fatal("delete through a foreign kind tombstoned the record")
	}
}

// The kind's own path still patches and deletes.
func TestWritesThroughTheRightKindStillWork(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	id := createPerson(t, env.testEnv, env.token)

	rec := env.do(t, http.MethodPatch, peoplePath+"/"+id, env.token,
		map[string]any{"properties": map[string]any{"displayName": "Analytical"}})
	wantStatus(t, rec, http.StatusOK)
	if got := env.get(t, personKind, id); got.Properties["displayName"] != "Analytical" {
		t.Fatalf("the patch did not land: %v", got.Properties)
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/"+id, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := env.get(t, personKind, id); got == nil || got.DeletedAt == nil {
		t.Fatal("delete through the right kind must tombstone")
	}
}

// A patch or a delete of a record that is not there is a 404.
func TestWritingAMissingRecordIs404(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	wantErrorCode(t, env.do(t, http.MethodPatch, peoplePath+"/nope", env.token,
		map[string]any{"properties": map[string]any{"name": "x"}}), http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodDelete, peoplePath+"/nope", env.token, nil),
		http.StatusNotFound, codeNotFound)
}
