package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// oauth/start takes the account's record path and nothing shorter (record
// 0102): a bare id is a validation refusal that names the id and the form,
// and a path naming no row is not-found rather than a search. The engine
// runs with the OAuth facility configured, so the refusals are the record's,
// not the facility's absence.
func TestOAuthStartRefusesABareIdAndAMissingRecord(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withEngine(engine.WithOAuth("test-state-key", "https://substrate.example/cb", nil)))

	rec := env.do(t, http.MethodPost, "/api/v1/oauth/start", env.token, map[string]any{"record": "owner"})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"owner" is a bare record id`) || !strings.Contains(msg, oauthStartForm) {
		t.Errorf("the bare-id refusal does not name the id and the form: %s", msg)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/oauth/start", env.token,
		map[string]any{"record": "providers.substrate.reamde.dev/google/account/owner"})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
