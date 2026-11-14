package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const personKind = "samples.substrate.reamde.dev/people/person"

// A precondition the server cannot read is refused by name, never dropped: a
// non-integer value, a miscased or unknown parameter and a purge that is not
// a boolean are each a `400 bad_request` before the dataset is reached (the
// stub has no Delete, so reaching it is a 500).
func TestRESTDeleteRefusesAMalformedPrecondition(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	for _, query := range []string{"?ifVersion=three", "?ifVersion=", "?ifversion=3", "?version=3", "?purge=yes", "?purge="} {
		rec := env.do(t, http.MethodDelete, peoplePath+"/p1"+query, stubToken, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		key := strings.TrimPrefix(strings.SplitN(query, "=", 2)[0], "?")
		if !strings.Contains(msg, key) {
			t.Fatalf("DELETE %s said %q; it must name %q", query, msg, key)
		}
	}
}

// The merge body is decoded strictly, so a misspelled precondition is `400
// bad_request` naming the key, before the dataset is reached.
func TestRESTMergeRefusesAMiscasedPrecondition(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/merge", stubToken, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2", "winnerversion": 4,
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "winnerversion") {
		t.Fatalf("a miscased key said %q; it must name the key", msg)
	}
}
