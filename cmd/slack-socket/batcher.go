package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// The batch body is `{"version":1,"events":[<payload>,<payload>,...]}`. The
// payloads are copied in byte for byte, so the substrate sees every field
// Slack sent, including ones slack-go has no struct field for.
const (
	batchPrefix = `{"version":1,"events":[`
	batchSuffix = `]}`
)

// envelope is one Socket Mode `events_api` request waiting to be forwarded
// and then acknowledged.
type envelope struct {
	id       string          // Socket Mode envelope_id, the ack key
	payload  json.RawMessage // the Events API callback JSON as received
	received time.Time
}

// postFunc sends one batch body and reports the HTTP status and the
// Retry-After header. A non-nil error means no response arrived.
type postFunc func(ctx context.Context, body []byte) (status int, retryAfter string, err error)

// ackFunc acknowledges envelopes to Slack.
type ackFunc func(ctx context.Context, ids []string) error

// BatcherOptions configures a Batcher.
type BatcherOptions struct {
	Post       postFunc
	Ack        ackFunc
	MaxEvents  int           // flush when the batch holds this many events
	MaxBytes   int           // flush when the body reaches this many bytes
	MaxWait    time.Duration // flush this long after the first event arrived
	AckTimeout time.Duration // give up acking a batch after this long
	Metrics    *Metrics
}

// Batcher collects envelopes and forwards them in batches. It acks a batch's
// envelopes only after the POST carrying them returned 202, so Slack
// redelivers anything the substrate did not accept. One goroutine owns the
// batch and posts sequentially, which keeps events in arrival order.
type Batcher struct {
	opts BatcherOptions
	in   chan envelope
	done chan struct{}
}

// NewBatcher starts a Batcher. Call Close to flush and stop it.
func NewBatcher(opts BatcherOptions) *Batcher {
	if opts.Metrics == nil {
		opts.Metrics = &Metrics{}
	}
	if opts.AckTimeout <= 0 {
		opts.AckTimeout = time.Second
	}
	b := &Batcher{
		opts: opts,
		in:   make(chan envelope, 1024),
		done: make(chan struct{}),
	}
	go b.run()
	return b
}

// Add queues an envelope. It blocks while the queue is full and returns
// false if ctx ends first; the envelope then stays unacked and Slack
// redelivers it. Add must not be called after Close.
func (b *Batcher) Add(ctx context.Context, e envelope) bool {
	select {
	case b.in <- e:
		return true
	case <-ctx.Done():
		return false
	}
}

// Close stops accepting envelopes, forwards what is pending and returns once
// the last batch has been posted and acked.
func (b *Batcher) Close() {
	close(b.in)
	<-b.done
}

func (b *Batcher) run() {
	defer close(b.done)

	overhead := len(batchPrefix) + len(batchSuffix)
	var (
		pending []envelope
		size    = overhead
	)
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	flush := func() {
		if len(pending) == 0 {
			return
		}
		timer.Stop()
		batch := pending
		pending = nil
		size = overhead
		b.forward(batch)
	}

	for {
		select {
		case e, ok := <-b.in:
			if !ok {
				flush()
				return
			}
			// Keep the body under MaxBytes: send what is pending before an
			// event that would push it over. A single event larger than
			// MaxBytes still goes out, alone.
			if len(pending) > 0 && size+1+len(e.payload) > b.opts.MaxBytes {
				flush()
			}
			if len(pending) > 0 {
				size++ // comma
			}
			size += len(e.payload)
			pending = append(pending, e)
			if len(pending) == 1 {
				// The wait counts from when the event reached us, not from
				// when this loop picked it up after a slow POST.
				timer.Reset(max(0, time.Until(e.received.Add(b.opts.MaxWait))))
			}
			if len(pending) >= b.opts.MaxEvents || size >= b.opts.MaxBytes {
				flush()
			}
		case <-timer.C:
			flush()
		}
	}
}

// forward posts one batch and acks it on 202.
func (b *Batcher) forward(batch []envelope) {
	m := b.opts.Metrics
	body := encodeBatch(batch)
	n := int64(len(batch))

	status, retryAfter, err := b.opts.Post(context.Background(), body)
	switch {
	case err != nil:
		m.EventsFailed.Add(n)
		m.BatchesFailed.Add(1)
		slog.Error("forward failed, leaving events unacked for Slack to redeliver",
			"error", err, "events", n, "bytes", len(body))
		return
	case status != http.StatusAccepted:
		m.EventsFailed.Add(n)
		m.BatchesFailed.Add(1)
		slog.Warn("forward refused, leaving events unacked for Slack to redeliver",
			"status", status, "retry_after", retryAfter, "events", n, "bytes", len(body))
		return
	}

	m.EventsForwarded.Add(n)
	m.BatchesForwarded.Add(1)

	ids := make([]string, len(batch))
	for i, e := range batch {
		ids[i] = e.id
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.opts.AckTimeout)
	defer cancel()
	if err := b.opts.Ack(ctx, ids); err != nil {
		m.AcksFailed.Add(1)
		slog.Warn("ack failed after forward, Slack will redeliver the batch",
			"error", err, "events", n)
		return
	}
	slog.Debug("forwarded batch", "events", n, "bytes", len(body))
}

// encodeBatch writes the batch body with the payloads copied verbatim.
func encodeBatch(batch []envelope) []byte {
	size := len(batchPrefix) + len(batchSuffix) + len(batch)
	for _, e := range batch {
		size += len(e.payload)
	}
	var buf bytes.Buffer
	buf.Grow(size)
	buf.WriteString(batchPrefix)
	for i, e := range batch {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(e.payload)
	}
	buf.WriteString(batchSuffix)
	return buf.Bytes()
}

// Forwarder POSTs batch bodies to the substrate webhook. The URL carries the
// trigger key, so no error or log line it produces may include it.
type Forwarder struct {
	url    string
	client *http.Client
}

// NewForwarder returns a Forwarder that gives up on a POST after timeout.
func NewForwarder(webhookURL string, timeout time.Duration) *Forwarder {
	return &Forwarder{url: webhookURL, client: &http.Client{Timeout: timeout}}
}

// Post implements postFunc.
func (f *Forwarder) Post(ctx context.Context, body []byte) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url, bytes.NewReader(body))
	if err != nil {
		// The error text would quote the URL.
		return 0, "", errors.New("building the webhook request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, "", redactURLError(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, resp.Header.Get("Retry-After"), nil
}

// redactURLError strips the request URL from a *url.Error, whose text quotes
// it in full.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s webhook: %w", ue.Op, ue.Err)
	}
	return err
}
