package pushrelay

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics are the relay's, in a registry of their own, served on a listener
// of their own because the relay's port is public by design. No label holds
// a token, a key, a thread or an address.
type Metrics struct {
	registry     *prometheus.Registry
	requests     *prometheus.CounterVec
	apple        *prometheus.CounterVec
	appleSeconds *prometheus.HistogramVec
	limited      *prometheus.CounterVec
	minted       *prometheus.CounterVec
}

// NewMetrics registers the relay's metrics, with the Go runtime's and the
// process's.
func NewMetrics() *Metrics {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		registry: r,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "substrate_push_relay_requests_total",
			Help: "Requests the relay answered, by route and by the code it answered (sent, registered, or a refusal's).",
		}, []string{"route", "code"}),
		apple: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "substrate_push_relay_apple_total",
			Help: "Sends to Apple, by environment, Apple's status (0 for no answer) and Apple's reason.",
		}, []string{"environment", "status", "reason"}),
		appleSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "substrate_push_relay_apple_seconds",
			Help:    "How long a send to Apple took, a replaced provider token's second send included.",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"environment"}),
		limited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "substrate_push_relay_limited_total",
			Help: "Requests a limit answered rate_limited, by limit.",
		}, []string{"limit"}),
		minted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "substrate_push_relay_tokens_minted_total",
			Help: "Apple provider tokens minted, by environment.",
		}, []string{"environment"}),
	}
	r.MustRegister(m.requests, m.apple, m.appleSeconds, m.limited, m.minted)
	return m
}

// Handler serves the metrics in Prometheus's text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Minted counts one provider token minted for environment.
func (m *Metrics) Minted(environment string) { m.minted.WithLabelValues(environment).Inc() }

func (m *Metrics) appleAnswer(environment string, status int, reason string, seconds float64) {
	m.apple.WithLabelValues(environment, strconv.Itoa(status), reason).Inc()
	m.appleSeconds.WithLabelValues(environment).Observe(seconds)
}
