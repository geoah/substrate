// Package metrics is the substrate's ONE Prometheus registry and the
// instruments the HTTP layer and the engine record into it. The instruments
// always record — an observation is a few nanoseconds, and the process is
// the only reader — while EXPOSING them is the API's decision: /metrics is
// mounted only under SUBSTRATE_METRICS=1 (internal/api), and a deployment
// that turns it on keeps the path off its ingress.
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
	"sync"

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
	// The buckets run to a minute because a list or window read that misses
	// its index runs for tens of seconds on a large repository, and a
	// histogram that tops out at ten seconds cannot show one.
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
// and the stale collector would otherwise pin a closed pool forever. The
// registry unregisters by descriptor, not by identity, so the unregister of a
// collector that has been taken over is a no-op: without that, the old pool's
// late close would drop the NEW pool's collector.
func RegisterDBStats(name string, db *sql.DB) (unregister func()) {
	return register(name, collectors.NewDBStatsCollector(db, name))
}

// register publishes c under key, taking over a collector already published
// under it, and returns the unregister that drops c and only c.
func register(name string, c prometheus.Collector) (unregister func()) {
	dbStatsMu.Lock()
	defer dbStatsMu.Unlock()
	if err := Registry.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if !errors.As(err, &already) {
			panic(err)
		}
		Registry.Unregister(already.ExistingCollector)
		Registry.MustRegister(c)
	}
	dbStats[name] = c
	return func() {
		dbStatsMu.Lock()
		defer dbStatsMu.Unlock()
		if dbStats[name] != c {
			return
		}
		delete(dbStats, name)
		Registry.Unregister(c)
	}
}

// dbStats is the live collector under each db_name, so an unregister can tell
// whether its collector is still the one published.
var (
	dbStatsMu sync.Mutex
	dbStats   = map[string]prometheus.Collector{}
)

// Handler serves the registry in the Prometheus text exposition format. It
// reports a failing collector in the body rather than dropping the scrape.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}

// PoolStats is one reading of a connection pool that is not a *sql.DB: the
// pool every repository of the engine shares (a pgxpool).
type PoolStats struct {
	// Max is the pool's cap; Total the connections open, Acquired the ones
	// in use and Idle the rest.
	Max, Total, Acquired, Idle int32
	// Acquires counts every acquisition, Waits the ones that found no idle
	// connection and waited, WaitDuration their summed wait, and Canceled
	// the acquisitions whose context ended first.
	Acquires, Waits, Canceled int64
	WaitDuration              float64
}

var (
	poolLabels     = []string{"pool"}
	poolMaxDesc    = prometheus.NewDesc("substrate_db_pool_max_conns", "The connection pool's cap.", poolLabels, nil)
	poolTotalDesc  = prometheus.NewDesc("substrate_db_pool_conns", "Connections the pool has open.", poolLabels, nil)
	poolAcqDesc    = prometheus.NewDesc("substrate_db_pool_acquired_conns", "Connections in use.", poolLabels, nil)
	poolIdleDesc   = prometheus.NewDesc("substrate_db_pool_idle_conns", "Connections open and unused.", poolLabels, nil)
	poolAcqsDesc   = prometheus.NewDesc("substrate_db_pool_acquires_total", "Acquisitions from the pool.", poolLabels, nil)
	poolWaitsDesc  = prometheus.NewDesc("substrate_db_pool_waits_total", "Acquisitions that waited for a connection.", poolLabels, nil)
	poolWaitDesc   = prometheus.NewDesc("substrate_db_pool_wait_seconds_total", "Time acquisitions spent waiting for a connection.", poolLabels, nil)
	poolCancelDesc = prometheus.NewDesc("substrate_db_pool_canceled_acquires_total", "Acquisitions whose context ended before a connection was free.", poolLabels, nil)
)

type poolCollector struct {
	name string
	stat func() PoolStats
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{poolMaxDesc, poolTotalDesc, poolAcqDesc, poolIdleDesc, poolAcqsDesc, poolWaitsDesc, poolWaitDesc, poolCancelDesc} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stat()
	gauge := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, c.name)
	}
	counter := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, c.name)
	}
	gauge(poolMaxDesc, float64(s.Max))
	gauge(poolTotalDesc, float64(s.Total))
	gauge(poolAcqDesc, float64(s.Acquired))
	gauge(poolIdleDesc, float64(s.Idle))
	counter(poolAcqsDesc, float64(s.Acquires))
	counter(poolWaitsDesc, float64(s.Waits))
	counter(poolWaitDesc, s.WaitDuration)
	counter(poolCancelDesc, float64(s.Canceled))
}

// RegisterPoolStats publishes stat as the substrate_db_pool_* series under
// pool=name, with RegisterDBStats's take-over rule for a name registered
// twice, and returns the matching unregister.
func RegisterPoolStats(name string, stat func() PoolStats) (unregister func()) {
	return register("pool:"+name, &poolCollector{name: name, stat: stat})
}
