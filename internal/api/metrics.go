package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/geoah/substrate/internal/metrics"
)

// httpMetrics is the FIRST middleware on the router: it wraps everything
// after it, Recoverer included, so a recovered panic is observed as the 500
// it answered rather than lost. The route label is chi's pattern, read AFTER
// the handler ran (before it, nothing has been matched yet): `/api/v1/{kind}`
// for every kind, never the path with the id in it, so the series count is
// the route count. A request no route claimed — the SPA fallback, a mistyped
// API path — is labeled `unmatched`, one series rather than one per typo.
func httpMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.HTTPRequestsInFlight.Inc()
		defer metrics.HTTPRequestsInFlight.Dec()
		start := time.Now()
		// chi's wrapper keeps Flusher and Hijacker where the underlying writer
		// has them, so the watch streams and the export still flush.
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := "unmatched"
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if p := rctx.RoutePattern(); p != "" {
				route = p
			}
		}
		status := ww.Status()
		if status == 0 {
			// Nothing wrote a header: net/http answers 200 on the way out.
			status = http.StatusOK
		}
		metrics.HTTPRequestDuration.
			WithLabelValues(route, metricMethod(r.Method), strconv.Itoa(status/100)+"xx").
			Observe(time.Since(start).Seconds())
	})
}

// metricMethod is the method label: a method some route here answers, or
// `other`. net/http admits any token as a method and the router's 405 is
// recorded like any answer, so the raw method would let an unauthenticated
// client add one series per invented method, each held for the life of the
// process.
func metricMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	}
	return "other"
}
