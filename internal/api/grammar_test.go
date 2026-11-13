package api

import (
	"net/http"
	"testing"
)

// A POST to a RECORD path used to resolve the collection, discard the id and
// create a record under a server-assigned one, answering 201: a client that
// believed it was upserting accumulated duplicates under ids it never chose
// (#202). The record path is four segments (decisions 0042, 0047); it refuses
// the POST as a method the path does not serve, and nothing is written: the
// stub has no Put, and reaching it would be a 500.
func TestPostToRecordPathIsMethodNotAllowed(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	rec := env.do(t, http.MethodPost, peoplePath+"/p1", stubToken,
		map[string]any{"properties": map[string]any{"text": "hello"}})
	wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
}

// The mirror: a PUT, PATCH or DELETE at the list route addresses no record,
// so it is refused and nothing is written. The create is the POST there.
func TestWriteVerbsAtTheListRouteAreMethodNotAllowed(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, recordsPath, stubToken,
			map[string]any{"kind": personKind, "properties": map[string]any{"title": "Ada"}})
		wantErrorCode(t, rec, http.StatusMethodNotAllowed, codeBadRequest)
	}
}

// A path shorter than a record reference names nothing: every kind carries an
// authority AND a package (decisions 0042, 0047) and there is no collection
// route, so the old authority-less shape (`/api/v1/note`), the old
// two-segment collection (`/api/v1/example.com/note`) and the kind reference
// alone (`/api/v1/example.com/notes/note`) are gone and answer 404 rather
// than the console's index.html.
func TestShortPathIs404(t *testing.T) {
	env := newStubEnv(t, nil)
	for _, path := range []string{"/api/v1/note", "/api/v1/example.com/note", "/api/v1/example.com/notes/note"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			rec := env.do(t, method, path, stubToken, map[string]any{})
			wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		}
	}
}
