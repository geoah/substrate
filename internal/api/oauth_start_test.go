package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// oauth/start takes the account's record path and nothing shorter (record
// 0102): an empty body names the form, a bare id is a validation refusal that
// names the id and the form, and a path naming no row is not-found rather than
// a search. The message is read off the decoded envelope: the encoder escapes
// the form's angle brackets in the raw body.
func TestOAuthStartTakesTheRecordPathOnly(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	const form = "<authority>/<package>/<kind>/<id>"

	rec := env.do(t, http.MethodPost, "/api/v1/oauth/start", tok, map[string]any{})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, form) {
		t.Errorf("the empty-record refusal does not name the form: %s", msg)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/oauth/start", tok, map[string]any{"record": "owner"})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"owner" is a bare record id`) || !strings.Contains(msg, form) {
		t.Errorf("the bare-id refusal does not name the id and the form: %s", msg)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/oauth/start", tok,
		map[string]any{"record": "providers.substrate.reamde.dev/google/account/owner"})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
