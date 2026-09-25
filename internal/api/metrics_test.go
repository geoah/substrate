package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The exposition is a door like /healthz: unauthenticated, DB-free, and
// mounted ONLY when the config asks for it. Off, the path is unrouted and
// answers the same 404 any unknown non-API path does.
func TestMetricsEndpointServedOnlyWhenEnabled(t *testing.T) {
	svc := newFakeService()

	on := New(Config{Service: svc, Metrics: true})
	// One served request first, so the request histogram has a series to
	// print: a vec with no children is absent from the exposition.
	rec := httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: got %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics enabled: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"substrate_http_request_duration_seconds",
		`route="/healthz"`,
		"substrate_http_requests_in_flight",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics body lacks %q", want)
		}
	}

	off := New(Config{Service: svc})
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/metrics disabled: got %d, want 404", rec.Code)
	}
}
