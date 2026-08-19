package oauthflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// TestMain clears SUBSTRATE_EGRESS_ALLOW so the refusal cases see the default
// gate: a stray allowlist in the shell would otherwise open loopback and turn
// every "egress blocked" assertion green for the wrong reason. The allow cases
// set it back with t.Setenv, which restores this cleared baseline after each.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("SUBSTRATE_EGRESS_ALLOW")
	os.Exit(m.Run())
}

// tokenServer answers a token endpoint with a valid bearer token so a permitted
// dial completes rather than failing on the response shape.
func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Every server-side OAuth dial routes through the default gated client when a
// caller supplies no HTTP seam. httptest binds 127.0.0.1, so each case dials
// the exact loopback range the bundle loader's `tokenEndpoint` escape admits,
// and every one must be refused at connect with the egress reason. Without the
// gate (the unfixed path) these dials reach the server and the cases fail.
func TestServerDialsGateLoopback(t *testing.T) {
	srv := tokenServer(t)
	c := &Client{StateKey: []byte("k"), CallbackURL: "https://substrate.example/cb"}
	ep := Endpoints{
		TokenURL:      srv.URL,
		RevocationURL: srv.URL,
		ClientID:      "id",
		ClientSecret:  "secret",
	}
	tok := &oauth2.Token{AccessToken: "at", RefreshToken: "rt"}

	cases := []struct {
		name string
		dial func() error
	}{
		{"exchange", func() error {
			_, err := c.Exchange(context.Background(), ep, "code", "")
			return err
		}},
		{"refresh", func() error {
			_, err := c.Refresh(context.Background(), ep, tok)
			return err
		}},
		{"revoke", func() error {
			return c.Revoke(context.Background(), ep.RevocationURL, tok)
		}},
		{"account email", func() error {
			_, err := c.AccountEmail(context.Background(), srv.URL, "at")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.dial()
			if err == nil {
				t.Fatalf("%s dialed a loopback endpoint and returned no error: the gate did not fire", tc.name)
			}
			if !strings.Contains(err.Error(), "egress blocked") {
				t.Fatalf("%s error = %v, want it to name egress blocked", tc.name, err)
			}
		})
	}
}

// The operator allowlist (SUBSTRATE_EGRESS_ALLOW) is #241's documented escape
// for a local provider. With the loopback range allowed, the same dials reach
// the server: the exchange returns a token and the userinfo GET returns an
// address, neither refused by the gate.
func TestAllowlistedLoopbackReaches(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")

	tsrv := tokenServer(t)
	c := &Client{StateKey: []byte("k"), CallbackURL: "https://substrate.example/cb"}
	ep := Endpoints{TokenURL: tsrv.URL, ClientID: "id", ClientSecret: "secret"}

	tok, err := c.Exchange(context.Background(), ep, "code", "")
	if err != nil {
		t.Fatalf("an allowlisted loopback exchange was refused: %v", err)
	}
	if tok.AccessToken != "at" {
		t.Fatalf("access token = %q, want %q", tok.AccessToken, "at")
	}

	usrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"person@example.com"}`))
	}))
	defer usrv.Close()
	email, err := c.AccountEmail(context.Background(), usrv.URL, "at")
	if err != nil {
		t.Fatalf("an allowlisted loopback userinfo fetch was refused: %v", err)
	}
	if email != "person@example.com" {
		t.Fatalf("email = %q, want %q", email, "person@example.com")
	}
}

// The egress transport composes UNDER the redirect refusal, not in place of it:
// with loopback allowed so the first dial is permitted, a provider that answers
// a credential-bearing POST with a redirect is still refused before the hop, so
// the refresh token is never replayed to the named host.
func TestNoRedirectHoldsUnderGate(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://127.0.0.1:1/next")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	c := &Client{StateKey: []byte("k"), CallbackURL: "https://substrate.example/cb"}
	err := c.Revoke(context.Background(), srv.URL, &oauth2.Token{RefreshToken: "rt"})
	if err == nil {
		t.Fatal("revoke followed a redirect on a credential-bearing request")
	}
	if !strings.Contains(err.Error(), "refusing to follow a redirect") {
		t.Fatalf("revoke error = %v, want the no-redirect refusal", err)
	}
}
