package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const peopleV1 = "/api/v1/samples.substrate.reamde.dev/people/person"

// The edge routes are GONE, not tombstoned: no route binds
// `…/{id}/edges/{rel}` in either method, so the path is the router's 404.
func TestRESTEdgeRoutesAreGone(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := env.do(t, method, peopleV1+"/p1/edges/member_of", stubToken, map[string]any{"id": "org1"})
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	}
}

// A body that still writes `edges` beside `properties` is refused NAMING the
// key AND what replaced it: the strict decode is what stops a pointer write
// from landing under a spelling nothing reads, and a raw HTTP client has no
// substratectl to tell it where the data goes now. Both write verbs, because
// only one of them was pinned and the two decode the same body. The stub
// has no Put and no Patch, so a body that reached the dataset would be a 500.
func TestRESTRefusesAnEdgesKeyNamingItsReplacement(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	tok := stubToken
	edges := map[string]any{"member_of": []any{map[string]any{"id": "org1"}}}

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		rec := env.do(t, method, peopleV1+"/p1", tok, map[string]any{
			"properties": map[string]any{"name": "Sam"},
			"edges":      edges,
		})
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		for _, want := range []string{`"edges"`, "data.properties", "type: reference"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s error must name %s: %q", method, want, msg)
			}
		}
	}
}

// THE ADVICE IS THE ENVELOPE'S, not the decoder's. A FILTER carrying `edges` is
// refused for the same reason and with the same words as any other unknown
// filter key: `data.properties` is where a record's pointers go, and a caller
// narrowing a list is not writing a record.
func TestRESTFilterWithAnEdgesKeyIsAPlainUnknownField(t *testing.T) {
	env := newStubEnv(t, nil)
	tok := stubToken
	rec := env.do(t, http.MethodGet, recordsPath+`?filter={"edges":{"member_of":"org1"}}`, tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
	if !strings.Contains(msg, `unknown field "edges"`) {
		t.Fatalf("the filter refusal must name the key: %q", msg)
	}
	if strings.Contains(msg, "data.properties") {
		t.Fatalf("a filter refusal must not send the caller to data.properties: %q", msg)
	}
}

// TestWatchRejectsListParams is ruling A8's unsupported-param rule: a param a
// mode does not honor is a bad_request naming it, not a silent success.
func TestWatchRejectsListParams(t *testing.T) {
	env := newStubEnv(t, nil)
	tok := stubToken
	rec := env.do(t, http.MethodGet, recordsOf(t, personKind, "watch=1", "orderBy=at:desc"), tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "orderBy") {
		t.Fatalf("error must name orderBy: %q", msg)
	}
}
