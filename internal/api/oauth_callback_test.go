package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// oauthSvc wraps the fake service with the substrate.OAuthCompleter half the
// callback asserts at runtime — a canned outcome, so the test drives the
// return-page.
type oauthSvc struct {
	*fakeService
	record string
	err    error
}

func (s oauthSvc) CompleteOAuth(context.Context, string, string) (string, error) {
	return s.record, s.err
}

var _ substrate.OAuthCompleter = oauthSvc{}

func callbackHandler(record string, err error, consoleURL string) http.Handler {
	return New(Config{Service: oauthSvc{fakeService: newFakeService(), record: record, err: err}, ConsoleURL: consoleURL})
}

const callbackPath = "/api/v1/-/oauth/callback?state=abc&code=xyz"

func getCallback(h http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The success return-page posts the exact console contract and offers the
// fallback redirect into the configured console.
func TestOAuthCallbackSuccessReturnPage(t *testing.T) {
	rec := getCallback(callbackHandler("account.geoah:1", nil, "https://console.example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want html", ct)
	}
	body := rec.Body.String()
	// The postMessage payload — source, ok:true, record — and the console origin
	// as the targetOrigin (scheme+host, no path).
	for _, want := range []string{
		`"source":"substrate-oauth"`,
		`"ok":true`,
		`"record":"account.geoah:1"`,
		`postMessage(msg, "https://console.example.com")`,
		`window.close()`,
		`window.location.replace("https://console.example.com/registry?connected=account.geoah%3A1")`,
		"you can close this tab",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("success page missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, `"ok":false`) {
		t.Fatal("success page must not post ok:false")
	}
}

// The failure return-page posts ok:false with only the correlation id, leaks no
// provider detail, and falls back to the console error redirect.
func TestOAuthCallbackFailureReturnPage(t *testing.T) {
	rec := getCallback(callbackHandler("", context.DeadlineExceeded, "https://console.example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ok":false`) || !strings.Contains(body, `"source":"substrate-oauth"`) {
		t.Fatalf("failure page must post ok:false\n%s", body)
	}
	if !strings.Contains(body, `"correlation":`) {
		t.Fatalf("failure page must carry a correlation id\n%s", body)
	}
	if !strings.Contains(body, "Could not complete the connection (correlation ") {
		t.Fatalf("failure page must render the fixed message\n%s", body)
	}
	if !strings.Contains(body, "/registry?error=") {
		t.Fatalf("failure page must offer the error redirect\n%s", body)
	}
	// No provider/engine error detail reaches the browser.
	if strings.Contains(body, context.DeadlineExceeded.Error()) {
		t.Fatal("failure page leaked the underlying error")
	}
}

// With no configured console (local dev) the target origin is "*" and the page
// renders no redirect — it just says the tab can be closed.
func TestOAuthCallbackNoConsoleURL(t *testing.T) {
	rec := getCallback(callbackHandler("account.geoah:1", nil, ""))
	body := rec.Body.String()
	if !strings.Contains(body, `postMessage(msg, "*")`) {
		t.Fatalf("local dev must post to targetOrigin *\n%s", body)
	}
	if strings.Contains(body, "window.location.replace") {
		t.Fatalf("local dev must render no redirect\n%s", body)
	}
	if !strings.Contains(body, "you can close this tab") {
		t.Fatalf("local dev must still render the close hint\n%s", body)
	}
}
