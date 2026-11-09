// Package health serves /healthz and /metrics on a local port, as
// slack-socket does. Nothing else listens.
package health

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// Report is what /healthz answers. Healthy is false when the bridge cannot
// deliver: a batch parked on the server, or a token the server refused.
type Report struct {
	Healthy  bool    `json:"healthy"`
	Instance string  `json:"instance"`
	Version  string  `json:"version"`
	Detail   any     `json:"detail"`
	Metrics  []Gauge `json:"-"`
}

// Gauge is one metric line.
type Gauge struct {
	Name  string
	Kind  string
	Help  string
	Value float64
}

// Source builds a report on demand.
type Source func() Report

// Mux serves the two endpoints.
func Mux(src Source) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		rep := src()
		w.Header().Set("Content-Type", "application/json")
		if !rep.Healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(rep)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = WriteMetrics(w, src().Metrics)
	})
	return mux
}

// WriteMetrics writes gauges in the Prometheus text format, by hand: a
// client library for a dozen numbers is not worth its weight.
func WriteMetrics(w io.Writer, gauges []Gauge) error {
	sorted := append([]Gauge(nil), gauges...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, g := range sorted {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", g.Name, g.Help, g.Name, g.Kind, g.Name, g.Value); err != nil {
			return err
		}
	}
	return nil
}
