package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack/socketmode"
)

// webhook is an httptest server standing in for the substrate webhook.
type webhook struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies [][]byte
	status int
}

func newWebhook(t *testing.T, status int) *webhook {
	t.Helper()
	w := &webhook{status: status}
	w.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("got %s with Content-Type %q", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		w.mu.Lock()
		w.bodies = append(w.bodies, body)
		status := w.status
		w.mu.Unlock()
		if status == http.StatusServiceUnavailable {
			rw.Header().Set("Retry-After", "1")
		}
		rw.WriteHeader(status)
	}))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *webhook) batches() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte(nil), w.bodies...)
}

// acker records acked envelope ids.
type acker struct {
	mu  sync.Mutex
	ids []string
}

func (a *acker) ack(_ context.Context, ids []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ids = append(a.ids, ids...)
	return nil
}

func (a *acker) acked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.ids...)
}

func newTestBatcher(post postFunc, a *acker, opts BatcherOptions) *Batcher {
	opts.Post = post
	opts.Ack = a.ack
	if opts.MaxEvents == 0 {
		opts.MaxEvents = 200
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 512 << 10
	}
	if opts.MaxWait == 0 {
		opts.MaxWait = time.Hour
	}
	return NewBatcher(opts)
}

func event(i int) envelope {
	return envelope{
		id:       fmt.Sprintf("env-%d", i),
		payload:  json.RawMessage(fmt.Sprintf(`{"type":"event_callback","event_id":"Ev%d","event":{"type":"reaction_added"}}`, i)),
		received: time.Now(),
	}
}

func decode(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	var b struct {
		Version int               `json:"version"`
		Events  []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("batch body is not JSON: %v: %s", err, body)
	}
	if b.Version != 1 {
		t.Fatalf("version = %d, want 1", b.Version)
	}
	return b.Events
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBatcherFlushesAtMaxEvents(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	a := &acker{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{MaxEvents: 3})
	defer b.Close()

	for i := range 3 {
		b.Add(context.Background(), event(i))
	}
	waitFor(t, "three acks", func() bool { return len(a.acked()) == 3 })

	got := wh.batches()
	if len(got) != 1 {
		t.Fatalf("posted %d batches, want 1", len(got))
	}
	if n := len(decode(t, got[0])); n != 3 {
		t.Fatalf("batch has %d events, want 3", n)
	}
	if want := []string{"env-0", "env-1", "env-2"}; strings.Join(a.acked(), ",") != strings.Join(want, ",") {
		t.Fatalf("acked %v, want %v", a.acked(), want)
	}
}

func TestBatcherFlushesAtMaxBytes(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	a := &acker{}
	one := len(event(0).payload)
	maxBytes := len(batchPrefix) + len(batchSuffix) + 2*one + 1 // room for exactly two events
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{MaxBytes: maxBytes})

	for i := range 5 {
		b.Add(context.Background(), event(i))
	}
	b.Close()

	got := wh.batches()
	var sizes []int
	for _, body := range got {
		if len(body) > maxBytes {
			t.Errorf("body is %d bytes, over the %d cap", len(body), maxBytes)
		}
		sizes = append(sizes, len(decode(t, body)))
	}
	if fmt.Sprint(sizes) != "[2 2 1]" {
		t.Fatalf("batch sizes = %v, want [2 2 1]", sizes)
	}
	if len(a.acked()) != 5 {
		t.Fatalf("acked %d, want 5", len(a.acked()))
	}
}

func TestBatcherFlushesAfterMaxWait(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	a := &acker{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{MaxWait: 50 * time.Millisecond})
	defer b.Close()

	start := time.Now()
	b.Add(context.Background(), event(0))
	waitFor(t, "the timed flush", func() bool { return len(a.acked()) == 1 })
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("flushed after %v, before the 50ms wait", elapsed)
	}
	if len(wh.batches()) != 1 {
		t.Fatalf("posted %d batches, want 1", len(wh.batches()))
	}
}

func TestBatcherKeepsPayloadBytes(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	a := &acker{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{})

	// Spacing and a field slack-go has no struct for must survive.
	raw := `{ "type": "event_callback", "event_id": "Ev1", "event": {"type":"pin_added","x_new_field":[1, 2]} }`
	b.Add(context.Background(), envelope{id: "env-1", payload: json.RawMessage(raw), received: time.Now()})
	b.Close()

	got := wh.batches()
	if len(got) != 1 {
		t.Fatalf("posted %d batches, want 1", len(got))
	}
	if want := batchPrefix + raw + batchSuffix; string(got[0]) != want {
		t.Fatalf("body = %s\nwant   %s", got[0], want)
	}
}

func TestBatcherDoesNotAckOn503(t *testing.T) {
	wh := newWebhook(t, http.StatusServiceUnavailable)
	a := &acker{}
	m := &Metrics{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{Metrics: m})

	b.Add(context.Background(), event(0))
	b.Add(context.Background(), event(1))
	b.Close()

	if len(wh.batches()) != 1 {
		t.Fatalf("posted %d batches, want 1", len(wh.batches()))
	}
	if len(a.acked()) != 0 {
		t.Fatalf("acked %v after a 503", a.acked())
	}
	if m.EventsFailed.Load() != 2 || m.BatchesFailed.Load() != 1 || m.EventsForwarded.Load() != 0 {
		t.Fatalf("metrics failed=%d batches_failed=%d forwarded=%d", m.EventsFailed.Load(), m.BatchesFailed.Load(), m.EventsForwarded.Load())
	}
}

func TestBatcherDoesNotAckOnOtherSuccessStatus(t *testing.T) {
	wh := newWebhook(t, http.StatusOK)
	a := &acker{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{})
	b.Add(context.Background(), event(0))
	b.Close()
	if len(a.acked()) != 0 {
		t.Fatalf("acked %v after a 200; only 202 counts", a.acked())
	}
}

func TestBatcherDoesNotAckOnNetworkError(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	target := wh.srv.URL + "/webhooks/example.com/slack-live-events/secret-key-0123456789"
	wh.srv.Close()

	a := &acker{}
	var postErr error
	fwd := NewForwarder(target, 200*time.Millisecond)
	post := func(ctx context.Context, body []byte) (int, string, error) {
		status, ra, err := fwd.Post(ctx, body)
		postErr = err
		return status, ra, err
	}
	b := newTestBatcher(post, a, BatcherOptions{})
	b.Add(context.Background(), event(0))
	b.Close()

	if len(a.acked()) != 0 {
		t.Fatalf("acked %v after a network error", a.acked())
	}
	if postErr == nil {
		t.Fatal("expected a network error")
	}
	if strings.Contains(postErr.Error(), "secret-key") {
		t.Fatalf("error leaks the webhook key: %v", postErr)
	}
}

func TestBatcherDoesNotAckOnTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		<-release
		rw.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	defer close(release)

	a := &acker{}
	b := newTestBatcher(NewForwarder(srv.URL, 50*time.Millisecond).Post, a, BatcherOptions{})
	b.Add(context.Background(), event(0))
	b.Close()
	if len(a.acked()) != 0 {
		t.Fatalf("acked %v after a timeout", a.acked())
	}
}

func TestBatcherCloseFlushesPending(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	a := &acker{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{})
	b.Add(context.Background(), event(0))
	b.Close()
	if len(a.acked()) != 1 {
		t.Fatalf("acked %d on close, want 1", len(a.acked()))
	}
}

func TestBatcherCountsAckFailure(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	m := &Metrics{}
	b := NewBatcher(BatcherOptions{
		Post:      NewForwarder(wh.srv.URL, time.Second).Post,
		Ack:       func(context.Context, []string) error { return errors.New("socket closed") },
		MaxEvents: 1, MaxBytes: 1 << 20, MaxWait: time.Hour,
		Metrics: m,
	})
	b.Add(context.Background(), event(0))
	b.Close()
	if m.AcksFailed.Load() != 1 || m.EventsForwarded.Load() != 1 {
		t.Fatalf("acks_failed=%d forwarded=%d", m.AcksFailed.Load(), m.EventsForwarded.Load())
	}
}

// fakeSocket records acks sent by the event loop.
type fakeSocket struct {
	mu  sync.Mutex
	ids []string
}

func (f *fakeSocket) AckCtx(_ context.Context, id string, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return nil
}

func TestEventLoopForwardsUndecodableEventsRaw(t *testing.T) {
	wh := newWebhook(t, http.StatusAccepted)
	a := &acker{}
	b := newTestBatcher(NewForwarder(wh.srv.URL, time.Second).Post, a, BatcherOptions{})
	sock := &fakeSocket{}
	m := &Metrics{}
	loop := &eventLoop{client: sock, batcher: b, metrics: m, now: time.Now}

	payload := `{"type":"event_callback","event_id":"Ev9","event":{"type":"some_future_event"}}`
	msg := `{"type":"events_api","envelope_id":"env-9","payload":` + payload + `,"retry_attempt":1}`
	loop.handle(context.Background(), socketmode.Event{
		Type: socketmode.EventTypeErrorBadMessage,
		Data: &socketmode.ErrorBadMessage{Cause: errors.New("inner Event does not exist"), Message: json.RawMessage(msg)},
	})
	loop.handle(context.Background(), socketmode.Event{
		Type:    socketmode.EventTypeSlashCommand,
		Request: &socketmode.Request{Type: socketmode.RequestTypeSlashCommands, EnvelopeID: "env-cmd"},
	})
	b.Close()

	got := wh.batches()
	if len(got) != 1 || !bytes.Contains(got[0], []byte(payload)) {
		t.Fatalf("batches = %q, want one carrying the raw payload", got)
	}
	if strings.Join(a.acked(), ",") != "env-9" {
		t.Fatalf("batcher acked %v, want [env-9]", a.acked())
	}
	if strings.Join(sock.ids, ",") != "env-cmd" {
		t.Fatalf("loop acked %v, want [env-cmd]", sock.ids)
	}
	if m.EventsReceived.Load() != 1 || m.EventsRedelivery.Load() != 1 {
		t.Fatalf("received=%d redelivered=%d", m.EventsReceived.Load(), m.EventsRedelivery.Load())
	}
}

func TestHealthz(t *testing.T) {
	m := &Metrics{}
	srv := httptest.NewServer(newMux(m))
	defer srv.Close()

	check := func(want int) {
		t.Helper()
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("/healthz = %d, want %d", resp.StatusCode, want)
		}
	}
	check(http.StatusServiceUnavailable)
	m.Connected.Store(true)
	check(http.StatusOK)

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(body, []byte("slack_socket_connected 1\n")) {
		t.Fatalf("/metrics missing the connected gauge:\n%s", body)
	}
}
