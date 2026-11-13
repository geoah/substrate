package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// An expired token authenticates as a spent credential: `auth`, not
// `forbidden`. A future expiry is fine.
func TestExpiredTokenRejected(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	past := env.clock.now().Add(-24 * time.Hour)
	future := env.clock.now().Add(24 * time.Hour)

	expired := env.mint(t, substrate.MintTokenInput{Label: "expired", ExpiresAt: &past})
	rec := env.do(t, http.MethodGet, peopleRecords, expired, nil)
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)

	live := env.mint(t, substrate.MintTokenInput{Label: "live", ExpiresAt: &future})
	rec = env.do(t, http.MethodGet, peopleRecords, live, nil)
	wantStatus(t, rec, http.StatusOK)
}
