package engine

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/oauthflow"
)

// A tokenError opens with a sentence the owner acts on and keeps the
// facility's own words on its second line, where the console folds them.
func TestTokenReasonIsASentenceThenTheRawWords(t *testing.T) {
	since := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	grant := &oauthflow.TokenError{Status: http.StatusBadRequest, Code: "invalid_grant"}
	for _, c := range []struct {
		name  string
		cause error
		first string
	}{
		{"revoked grant", grant, "no longer accepts this sign-in"},
		{"wrapped revoked grant", fmt.Errorf("refresh: %w", grant), "no longer accepts this sign-in"},
		{"client refused", &oauthflow.TokenError{Status: http.StatusUnauthorized, Code: "invalid_client"}, "client ID or secret"},
		{"other refusal", &oauthflow.TokenError{Status: http.StatusBadRequest, Code: "invalid_request"}, "refused to renew this sign-in"},
		{"provider outage", &oauthflow.TokenError{Status: http.StatusBadGateway}, "tries again on its own"},
		{"no refresh token", errCannotRefresh{account: "a1"}, "can't be renewed on its own"},
		{"no facility", errCannotRefresh{account: "a1", noFacility: true}, "no OAuth facility"},
		{"another account's grant", errOtherAccountsGrant, "belongs to another account"},
		{"transport", errors.New("dial tcp: connection refused"), "tries again on its own"},
		{"repeated", repeatedRefusal(7, since, grant), "7 times since 2026-11-09T08:00:00Z,"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := tokenReason(c.cause)
			first, rest, ok := strings.Cut(got, "\n")
			if !ok {
				t.Fatalf("tokenReason = %q, want a sentence and the raw words on two lines", got)
			}
			if !strings.Contains(first, c.first) {
				t.Fatalf("first line %q, want it to say %q", first, c.first)
			}
			if strings.Contains(first, "provider answered") || strings.Contains(first, "invalid_") {
				t.Fatalf("first line %q carries the protocol's words", first)
			}
			if rest != firstLine(c.cause.Error()) {
				t.Fatalf("second line %q, want the raw %q", rest, c.cause.Error())
			}
		})
	}
}

// The client-change stamp is compared whole, so it is written whole.
func TestTokenReasonKeepsTheClientChangeStamp(t *testing.T) {
	for _, cause := range []error{errOAuthClientChanged, fmt.Errorf("refresh: %w", errOAuthClientChanged)} {
		if got := tokenReason(cause); got != reasonOAuthClientChanged {
			t.Fatalf("tokenReason(%v) = %q, want %q", cause, got, reasonOAuthClientChanged)
		}
	}
}

// A failed run's syncError keeps the lines a body raised under its sentence
// and drops the traceback after them.
func TestFailureTextDropsOnlyTheTraceback(t *testing.T) {
	for in, want := range map[string]string{
		"Notion no longer accepts the token.\nHTTP 401 unauthorized\nTraceback (most recent call last):\n  File \"x\"": "Notion no longer accepts the token.\nHTTP 401 unauthorized",
		"RuntimeError: mail: HTTP 403\nTraceback (most recent call last):": "RuntimeError: mail: HTTP 403",
		"run: context deadline exceeded": "run: context deadline exceeded",
	} {
		if got := failureText(in); got != want {
			t.Errorf("failureText(%q) = %q, want %q", in, got, want)
		}
	}
}
