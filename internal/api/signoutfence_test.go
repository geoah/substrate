package api

// A password change whose sign out everywhere did not finish says which half
// landed and the way on, in the door's own words, whatever the cause. What a
// sign-out ends is the engine's (signoutfence_db_test.go, and
// internal/testenv's signout_db_test.go).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// halfSignOutService answers a password change's sign-out with err: a
// sign-out that failed after the password changed, which the engine cannot
// be made to produce on demand.
type halfSignOutService struct {
	*stubService
	err error
}

func (s halfSignOutService) SignOutEverywhere(context.Context, substrate.LoginInput, string) (int, error) {
	return 0, s.err
}

func TestAPasswordChangeWhoseSignOutDidNotFinishSaysSo(t *testing.T) {
	cause := errors.New("driver: bad connection")
	for _, c := range []struct {
		name   string
		err    error
		reason string
		says   []string
	}{
		// The password changed, and ending the tokens rolled back.
		{
			"changed", fmt.Errorf("%w: %w", substrate.ErrSignOutIncomplete, cause), "",
			[]string{"the password changed", "sign out everywhere again with the new password"},
		},
		// The password write, or the sign-out after it, may have landed.
		{
			"in doubt", fmt.Errorf("%w: %w", substrate.ErrSignOutIncomplete, fmt.Errorf("%w: %w", substrate.ErrWriteInDoubt, cause)), substrate.ReasonInDoubt,
			[]string{"the password may have changed", "sign out everywhere again", "the old one"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := newStubEnvOn(t, halfSignOutService{&stubService{ds: stubDataset{}}, c.err})
			rec := env.do(t, http.MethodPost, "/password", "", map[string]any{
				"repository": stubRepository, "password": "the-current-password", "totpCode": "123456",
				"newPassword": "a-new-and-longer-password", "signOutEverywhere": true,
			})
			wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
			p := decodeJSON[substrate.ErrorEnvelope](t, rec).Error
			if p.Reason != c.reason {
				t.Fatalf("the reason was %q, want %q", p.Reason, c.reason)
			}
			for _, want := range c.says {
				if !strings.Contains(p.Message, want) {
					t.Fatalf("the answer said %q, want %q in it", p.Message, want)
				}
			}
			if strings.Contains(p.Message, "driver") {
				t.Fatalf("the answer said %q, which names the cause", p.Message)
			}
		})
	}
}
