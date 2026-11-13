package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Domain verification's pure parts and the doors beside it. The check
// itself, which reads and writes the repository record's pending proof and
// stamp, runs on the engine in domain_db_test.go.

type resolverFunc func(ctx context.Context, name string) ([]string, error)

func (f resolverFunc) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return f(ctx, name)
}

func TestUnderHostTakesOneLabelUnderTheHost(t *testing.T) {
	for _, tc := range []struct {
		repository, host string
		want             bool
	}{
		{"ada.example.net", "example.net", true},
		{"ada.example.net", "Example.NET", true},
		{"a.b.example.net", "example.net", false},
		{"example.net", "example.net", false},
		{"ada.example.org", "example.net", false},
		{"adaexample.net", "example.net", false},
		{"ada.example.net", "", false},
	} {
		if got := underHost(tc.repository, tc.host); got != tc.want {
			t.Errorf("underHost(%q, %q) = %v, want %v", tc.repository, tc.host, got, tc.want)
		}
	}
}

func TestDiscoveryServesTheInstanceID(t *testing.T) {
	e := newStubEnvOn(t, &stubService{})
	rec := e.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[discoveryDoc](t, rec)
	if doc.Server.Instance != stubInstanceID {
		t.Fatalf("server.instance = %q, want %q", doc.Server.Instance, stubInstanceID)
	}
}

func TestFetchServerInstanceRefusesALoopbackOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"server":{"instance":"x"}}`))
	}))
	defer srv.Close()
	if id, err := fetchServerInstance(context.Background(), srv.URL); err == nil {
		t.Fatalf("fetched %q from a loopback origin", id)
	}
}

// The authorization endpoint parks the request without reaching the service,
// and a login_hint naming a valid repository rides the redirect to the
// approval page, resolved under the request's host; any other hint is dropped.
func TestTheAuthorizationEndpointCarriesAValidLoginHintToTheApprovalPage(t *testing.T) {
	e := newStubEnvOn(t, &stubService{})
	for hint, want := range map[string]string{
		"geoah.example.org": "?login_hint=geoah.example.org",
		"Ada":               "?login_hint=ada.substrate.example.com",
		"":                  "",
		"not a name":        "",
		"ada@example.org":   "",
		"-bad.example.org":  "",
		strings.Repeat("a", 300) + ".example.org": "",
	} {
		params := appRequest("read:" + personKind)
		if hint != "" {
			params["login_hint"] = hint
		}
		req := httptest.NewRequest(http.MethodGet, authorizeURL(params), nil)
		req.Host = "substrate.example.com"
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("login_hint %q: authorize = %d %s, want 303", hint, rec.Code, rec.Body.String())
		}
		_, query, _ := strings.Cut(rec.Header().Get("Location"), "?")
		if query != strings.TrimPrefix(want, "?") {
			t.Errorf("login_hint %q: redirected with %q, want %q", hint, query, want)
		}
		// One source parks two requests an interval.
		e.clock.advance(defaultAuthInterval)
	}
}
