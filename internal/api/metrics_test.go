package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/metrics"
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

// requestSeries is the request histogram's series: their count and the
// method labels they carry.
func requestSeries(t *testing.T) (int, map[string]bool) {
	t.Helper()
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]bool{}
	for _, f := range families {
		if f.GetName() != "substrate_http_request_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "method" {
					methods[l.GetValue()] = true
				}
			}
		}
		return len(f.GetMetric()), methods
	}
	return 0, methods
}

// net/http admits any token as a method and the router's 405 is recorded
// like any answer, so the method label is a fixed set: a hundred invented
// methods add one series, `other`, not a hundred.
func TestMetricsMethodLabelIsAFixedSet(t *testing.T) {
	h := New(Config{Service: newFakeService()})
	before, _ := requestSeries(t)
	for i := range 100 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(fmt.Sprintf("M%07d", i), "/api/"+APIVersion+"/records", nil))
	}
	after, methods := requestSeries(t)
	if after-before > 1 {
		t.Fatalf("100 invented methods added %d series", after-before)
	}
	if !methods["other"] {
		t.Fatalf("no series is labeled other: %v", methods)
	}
	for m := range methods {
		if metricMethod(m) != m {
			t.Fatalf("a series is labeled with the method %q", m)
		}
	}
}
