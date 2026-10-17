package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// socketClient is the part of socketmode.Client the event loop uses.
type socketClient interface {
	AckCtx(ctx context.Context, envelopeID string, payload any) error
}

// eventLoop reads Socket Mode client events, queues events_api envelopes on
// the batcher and acks everything else. It returns when ctx ends or the
// events channel closes. It must keep reading: slack-go blocks its reader
// while the channel is full.
type eventLoop struct {
	client  socketClient
	batcher *Batcher
	metrics *Metrics
	now     func() time.Time
}

func (l *eventLoop) run(ctx context.Context, events <-chan socketmode.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			l.handle(ctx, evt)
		}
	}
}

func (l *eventLoop) handle(ctx context.Context, evt socketmode.Event) {
	m := l.metrics
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		m.Connected.Store(false)
		if ce, ok := evt.Data.(*slack.ConnectingEvent); ok {
			slog.Info("connecting to Slack", "attempt", ce.Attempt, "connection", ce.ConnectionCount)
		}

	case socketmode.EventTypeConnectionError:
		m.Connected.Store(false)
		if ce, ok := evt.Data.(*slack.ConnectionErrorEvent); ok {
			slog.Warn("a Slack connection attempt failed", "error", ce.ErrorObj, "attempt", ce.Attempt, "backoff", ce.Backoff.String())
		}

	case socketmode.EventTypeInvalidAuth:
		m.Connected.Store(false)
		slog.Error("Slack refused the app-level token")

	case socketmode.EventTypeConnected:
		m.Connected.Store(true)
		count := 0
		if ce, ok := evt.Data.(*socketmode.ConnectedEvent); ok {
			count = ce.ConnectionCount
		}
		if count > 0 {
			m.Reconnects.Add(1)
			slog.Info("reconnected to Slack", "connection", count)
		} else {
			slog.Info("connected to Slack")
		}

	case socketmode.EventTypeHello:
		// More than one connection means another process holds a socket
		// for this app, and Slack splits events between them.
		if evt.Request != nil {
			slog.Info("Slack said hello",
				"app_id", evt.Request.ConnectionInfo.AppID,
				"num_connections", evt.Request.NumConnections,
				"approximate_connection_time_s", evt.Request.DebugInfo.ApproximateConnectionTime)
		}

	case socketmode.EventTypeDisconnect:
		// slack-go handles `disconnect` itself by reconnecting and does not
		// pass it on today; kept in case a future version does.
		m.Connected.Store(false)
		slog.Info("Slack asked for a reconnect")

	case socketmode.EventTypeEventsAPI:
		if evt.Request != nil {
			l.enqueue(ctx, evt.Request)
		}

	case socketmode.EventTypeErrorBadMessage:
		// slack-go reports an events_api envelope whose inner event type it
		// has no struct for (or cannot decode) as a bad message and does not
		// ack it. The raw envelope is all this service needs, so recover it.
		bad, ok := evt.Data.(*socketmode.ErrorBadMessage)
		if !ok {
			return
		}
		var req socketmode.Request
		if err := json.Unmarshal(bad.Message, &req); err != nil {
			slog.Warn("dropped an unreadable Socket Mode message", "error", err, "bytes", len(bad.Message))
			return
		}
		if req.Type == socketmode.RequestTypeEventsAPI {
			slog.Debug("slack-go could not decode the inner event, forwarding it raw", "cause", bad.Cause)
			l.enqueue(ctx, &req)
			return
		}
		slog.Warn("acked a Socket Mode message slack-go could not decode", "type", req.Type, "cause", bad.Cause)
		l.ack(ctx, req.EnvelopeID)

	case socketmode.EventTypeInteractive, socketmode.EventTypeSlashCommand:
		// The app has no interactivity or commands; ack so Slack stops
		// retrying and the user sees no error.
		if evt.Request != nil {
			slog.Debug("acked an envelope this service does not handle", "type", evt.Request.Type)
			l.ack(ctx, evt.Request.EnvelopeID)
		}

	case socketmode.EventTypeIncomingError:
		if ie, ok := evt.Data.(*slack.IncomingEventError); ok {
			slog.Warn("a Socket Mode read failed", "error", ie.ErrorObj)
		}

	case socketmode.EventTypeErrorWriteFailed:
		if we, ok := evt.Data.(*socketmode.ErrorWriteFailed); ok {
			slog.Warn("a Socket Mode ack write failed, Slack will redeliver", "error", we.Cause)
		}

	default:
		if evt.Request != nil && evt.Request.EnvelopeID != "" {
			slog.Debug("acked an unknown envelope type", "type", evt.Request.Type)
			l.ack(ctx, evt.Request.EnvelopeID)
		}
	}
}

// enqueue hands one events_api envelope to the batcher. The batcher acks it
// once the substrate accepted it.
func (l *eventLoop) enqueue(ctx context.Context, req *socketmode.Request) {
	now := l.now()
	if req.EnvelopeID == "" {
		slog.Warn("dropped an events_api envelope without an envelope_id")
		return
	}
	if len(req.Payload) == 0 || string(req.Payload) == "null" {
		// Nothing to forward; ack so Slack does not resend an empty shell.
		slog.Warn("acked an events_api envelope with no payload", "retry_attempt", req.RetryAttempt)
		l.ack(ctx, req.EnvelopeID)
		return
	}
	l.metrics.ObserveEvent(req.RetryAttempt, now)
	if req.RetryAttempt > 0 {
		slog.Debug("Slack redelivered an event", "retry_attempt", req.RetryAttempt, "retry_reason", req.RetryReason)
	}
	l.batcher.Add(ctx, envelope{id: req.EnvelopeID, payload: req.Payload, received: now})
}

func (l *eventLoop) ack(ctx context.Context, envelopeID string) {
	if envelopeID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := l.client.AckCtx(ctx, envelopeID, nil); err != nil {
		slog.Warn("ack failed", "error", err)
	}
}
