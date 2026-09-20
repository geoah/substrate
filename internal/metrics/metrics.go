// Package metrics is the substrate's ONE Prometheus registry and the
// instruments the HTTP layer and the engine record into it. The instruments
// always record — an observation is a few nanoseconds, and the process is
// the only reader — while EXPOSING them is the API's decision: /metrics is
// mounted only under SUBSTRATE_METRICS=1 (internal/api), and never behind
// the ingress (the deployment blocks the path at the edge).
//
// A registry of its own rather than the client library's default one, so a
// test that builds two routers or two services does not fight the global
// over a collector name, and so nothing else linked into the binary lands a
// metric here without going through this package.
package metrics

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds every substrate metric, plus the Go runtime and process
// collectors: heap, goroutines, GC pauses and open file descriptors are the
// first things an operator wants beside the request latencies.
var Registry = func() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return r
}()

var (
	// HTTPRequestDuration is one observation per request, labeled with the
	// chi ROUTE PATTERN (never the raw path: a path carries ids and would make
	// a series per record), the method and the status class (2xx, 4xx, 5xx).
	// The buckets run to a minute because the slow list and schedule reads
	// this deployment saw ran tens of seconds before they were fixed (T-089),
	// and a histogram that tops out at ten seconds cannot show the next one.
	HTTPRequestDuration = promauto.With(Registry).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "substrate_http_request_duration_seconds",
		Help:    "HTTP request latency by chi route pattern, method and status class.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"route", "method", "status_class"})

	// HTTPRequestsInFlight counts requests between arrival and the handler's
	// return, watch streams included — a long-lived stream sits in this gauge
	// for its whole life, which is what makes it visible.
	HTTPRequestsInFlight = promauto.With(Registry).NewGauge(prometheus.GaugeOpts{
		Name: "substrate_http_requests_in_flight",
		Help: "HTTP requests currently being served, streams included.",
	})

	// TriggerPassSeconds is one observation per ProcessTriggers pass per
	// repository: the whole pass, every enabled trigger's drain or fire.
	TriggerPassSeconds = promauto.With(Registry).NewHistogram(prometheus.HistogramOpts{
		Name:    "substrate_trigger_pass_seconds",
		Help:    "Duration of one trigger dispatcher pass over one repository.",
		Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	})

	// TriggerDeliveries counts deliveries that APPLIED effects, by trigger id.
	// Skips, parks and yields are not deliveries and are not counted here.
	TriggerDeliveries = promauto.With(Registry).NewCounterVec(prometheus.CounterOpts{
		Name: "substrate_trigger_deliveries_total",
		Help: "Trigger deliveries that applied effects, by trigger id.",
	}, []string{"trigger"})
)

// RegisterDBStats publishes a pool's sql.DBStats as the standard go_sql_*
// gauges under db_name=name (open, idle, in-use, wait count and duration,
// closes by max-idle/max-lifetime). It returns the matching unregister,
// which the pool's owner calls when it closes the pool.
//
// A name already registered is TAKEN OVER rather than refused: the engine
// reopens a repository under the same authority after a close, and tests
// open a service per case, so the newest pool under a name is the live one
// and the stale collector would otherwise pin a closed pool forever.
func RegisterDBStats(name string, db *sql.DB) (unregister func()) {
	c := collectors.NewDBStatsCollector(db, name)
	if err := Registry.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if !errors.As(err, &already) {
			panic(err)
		}
		Registry.Unregister(already.ExistingCollector)
		Registry.MustRegister(c)
	}
	return func() { Registry.Unregister(c) }
}

// Handler serves the registry in the Prometheus text exposition format. It
// reports a failing collector in the body rather than dropping the scrape.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}
