package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// oauthStartForm is the record path form every oauth/start refusal names.
const oauthStartForm = "<authority>/<package>/<kind>/<id>"

// oauth/start takes the account's record path and nothing shorter (record
// 0102): an empty body names the form before the dataset is reached (the
// stub has no StartOAuth, so reaching it is a 500). What the engine answers
// for a bare id or a path naming no row is
// TestOAuthStartRefusesABareIdAndAMissingRecord. The message is read off the
// decoded envelope: the encoder escapes the form's angle brackets in the raw
// body.
func TestOAuthStartTakesTheRecordPathOnly(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/oauth/start", stubToken, map[string]any{})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, oauthStartForm) {
		t.Errorf("the empty-record refusal does not name the form: %s", msg)
	}
}
