package main

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics holds the service's counters and gauges. They are written out in
// the Prometheus text format by hand, which keeps the Prometheus client
// library out of the module for ten numbers.
type Metrics struct {
	EventsReceived   atomic.Int64
	EventsRedelivery atomic.Int64
	EventsForwarded  atomic.Int64
	EventsFailed     atomic.Int64
	BatchesForwarded atomic.Int64
	BatchesFailed    atomic.Int64
	AcksFailed       atomic.Int64
	Reconnects       atomic.Int64
	Connected        atomic.Bool
	LastEventUnixNs  atomic.Int64
}

// ObserveEvent records one received events_api envelope.
func (m *Metrics) ObserveEvent(retryAttempt int, at time.Time) {
	m.EventsReceived.Add(1)
	if retryAttempt > 0 {
		m.EventsRedelivery.Add(1)
	}
	m.LastEventUnixNs.Store(at.UnixNano())
}

// WriteTo writes the metrics in the Prometheus text exposition format.
func (m *Metrics) WriteTo(w io.Writer) (int64, error) {
	var total int64
	write := func(name, kind, help string, value float64) error {
		n, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
		total += int64(n)
		return err
	}
	connected := 0.0
	if m.Connected.Load() {
		connected = 1
	}
	lastEvent := 0.0
	if ns := m.LastEventUnixNs.Load(); ns > 0 {
		lastEvent = float64(ns) / 1e9
	}
	rows := []struct {
		name, kind, help string
		value            float64
	}{
		{"slack_socket_events_received_total", "counter", "Events API envelopes received from Slack.", float64(m.EventsReceived.Load())},
		{"slack_socket_events_redelivered_total", "counter", "Received envelopes Slack marked as a retry.", float64(m.EventsRedelivery.Load())},
		{"slack_socket_events_forwarded_total", "counter", "Events the substrate webhook accepted with 202.", float64(m.EventsForwarded.Load())},
		{"slack_socket_events_failed_total", "counter", "Events in batches the substrate webhook did not accept.", float64(m.EventsFailed.Load())},
		{"slack_socket_batches_forwarded_total", "counter", "Batches the substrate webhook accepted with 202.", float64(m.BatchesForwarded.Load())},
		{"slack_socket_batches_failed_total", "counter", "Batches that failed or got a status other than 202.", float64(m.BatchesFailed.Load())},
		{"slack_socket_acks_failed_total", "counter", "Accepted batches whose acks could not be sent to Slack.", float64(m.AcksFailed.Load())},
		{"slack_socket_reconnects_total", "counter", "Socket Mode reconnections after the first connection.", float64(m.Reconnects.Load())},
		{"slack_socket_connected", "gauge", "1 while the Socket Mode connection is open.", connected},
		{"slack_socket_last_event_timestamp_seconds", "gauge", "Unix time of the last Events API envelope received.", lastEvent},
	}
	for _, r := range rows {
		if err := write(r.name, r.kind, r.help, r.value); err != nil {
			return total, err
		}
	}
	return total, nil
}

// ServeHTTP serves /metrics.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = m.WriteTo(w)
}
