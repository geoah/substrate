package oauthflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// A token or revocation endpoint that accepts the connection and never
// answers fails the call within the default client's timeout instead of
// holding it, and with it the engine's walk over every repository, forever.
// The bound is shortened here; the wiring under test is the default client a
// Client with no HTTP seam builds.
func TestDefaultClientTimesOutAHangingEndpoint(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	prev := callTimeout
	callTimeout = 200 * time.Millisecond
	t.Cleanup(func() { callTimeout = prev })

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	c := &Client{StateKey: []byte("k"), CallbackURL: "https://substrate.example/cb"}
	ep := Endpoints{TokenURL: srv.URL, ClientID: "id", ClientSecret: "secret"}
	cases := []struct {
		name string
		call func() error
	}{
		{"refresh", func() error {
			_, err := c.Refresh(context.Background(), ep, &oauth2.Token{RefreshToken: "rt"})
			return err
		}},
		{"revoke", func() error {
			return c.Revoke(context.Background(), srv.URL, &oauth2.Token{RefreshToken: "rt"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- tc.call() }()
			select {
			case err := <-done:
				var ne interface{ Timeout() bool }
				if !errors.As(err, &ne) || !ne.Timeout() {
					t.Fatalf("%s against a hanging endpoint returned %v, want a timeout", tc.name, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s against a hanging endpoint was still waiting after 5s: the default client has no timeout", tc.name)
			}
		})
	}
}

// The production bound is the documented one.
func TestDefaultClientTimeoutIsCallTimeout(t *testing.T) {
	if got := defaultClient().Timeout; got != CallTimeout || CallTimeout != 30*time.Second {
		t.Fatalf("default client timeout = %v, want %v (CallTimeout = %v)", got, 30*time.Second, CallTimeout)
	}
}

// Refused is the token endpoint refusing the request itself: a 4xx other
// than 408 and 429. Transport errors, 5xx answers and the two retryable 4xx
// answers may pass and are not refusals.
func TestRefusedIsAFinal4xx(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&TokenError{Status: http.StatusBadRequest, Code: "invalid_request"}, true},
		{&TokenError{Status: http.StatusBadRequest, Code: "invalid_grant"}, true},
		{&TokenError{Status: http.StatusBadRequest}, true},
		{&TokenError{Status: http.StatusUnauthorized, Code: "invalid_client"}, true},
		{&TokenError{Status: http.StatusForbidden}, true},
		{&TokenError{Status: http.StatusRequestTimeout}, false},
		{&TokenError{Status: http.StatusTooManyRequests}, false},
		{&TokenError{Status: http.StatusInternalServerError}, false},
		{&TokenError{Status: http.StatusServiceUnavailable}, false},
		{&TokenError{Status: 0}, false},
		{fmt.Errorf("oauthflow: refresh: %w", &TokenError{Status: http.StatusBadRequest, Code: "invalid_request"}), true},
		{context.DeadlineExceeded, false},
		{errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		if got := Refused(tc.err); got != tc.want {
			t.Errorf("Refused(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
