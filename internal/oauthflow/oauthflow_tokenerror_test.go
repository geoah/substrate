package oauthflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// A refresh the token endpoint refuses comes back as a TokenError through the
// wrap Refresh puts on it, and only `invalid_grant` reads as a refused grant.
func TestRefreshRefusalIsATokenError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		code    string
		text    string
		refused bool
	}{
		{"invalid_grant", http.StatusBadRequest, "invalid_grant", `provider answered 400, error code "invalid_grant"`, true},
		{"invalid_client", http.StatusUnauthorized, "invalid_client", `provider answered 401, error code "invalid_client"`, false},
		{"5xx without a code", http.StatusServiceUnavailable, "", "provider answered 503", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				body := map[string]any{}
				if tc.code != "" {
					body["error"] = tc.code
					body["error_description"] = "SECRET-PROVIDER-DETAIL"
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			t.Cleanup(srv.Close)
			c := &Client{StateKey: []byte("k"), CallbackURL: srv.URL + "/callback", HTTP: srv.Client()}
			_, err := c.Refresh(context.Background(), Endpoints{TokenURL: srv.URL, ClientID: "c", ClientSecret: "s"},
				&oauth2.Token{RefreshToken: "rt"})
			if err == nil {
				t.Fatal("a refused refresh succeeded")
			}
			if !strings.HasSuffix(err.Error(), tc.text) || strings.Contains(err.Error(), "SECRET-PROVIDER-DETAIL") {
				t.Fatalf("error text: %q, want it to end in %q and carry no description", err, tc.text)
			}
			if got := GrantRefused(err); got != tc.refused {
				t.Fatalf("GrantRefused = %v, want %v", got, tc.refused)
			}
		})
	}
	if GrantRefused(context.DeadlineExceeded) {
		t.Fatal("a timeout read as a refused grant")
	}
}
