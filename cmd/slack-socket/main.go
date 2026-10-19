// Command slack-socket holds a Slack Socket Mode connection and forwards
// every Events API callback, in batches, to a substrate webhook trigger: the
// `slack-live-events` trigger of the providers.substrate.reamde.dev/slacklive
// bundle. It acks an event to Slack only after the substrate accepted the
// batch carrying it, so Slack redelivers anything that failed.
//
// It is configured from the environment alone: the Slack app-level token and
// the webhook URL, nothing that names a workspace or an app.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/geoah/substrate/internal/build"
)

// Config is read from the environment.
type Config struct {
	// AppToken is the Slack app-level token (xapp-...) with
	// connections:write. Socket Mode needs no bot token.
	AppToken string `envconfig:"SLACK_APP_TOKEN" required:"true"`
	// WebhookURL is the full substrate webhook URL, trigger key included.
	WebhookURL string `envconfig:"SUBSTRATE_WEBHOOK_URL" required:"true"`

	ListenAddr     string        `envconfig:"LISTEN_ADDR" default:":8080"`
	BatchMaxEvents int           `envconfig:"BATCH_MAX_EVENTS" default:"200"`
	BatchMaxBytes  int           `envconfig:"BATCH_MAX_BYTES" default:"524288"`
	BatchMaxWait   time.Duration `envconfig:"BATCH_MAX_WAIT" default:"1s"`
	PostTimeout    time.Duration `envconfig:"POST_TIMEOUT" default:"1500ms"`
	AckTimeout     time.Duration `envconfig:"ACK_TIMEOUT" default:"1s"`
	ShutdownWait   time.Duration `envconfig:"SHUTDOWN_WAIT" default:"5s"`
	Debug          bool          `envconfig:"DEBUG" default:"false"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("slack-socket stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return err
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	if !strings.HasPrefix(cfg.AppToken, "xapp-") {
		return errors.New("SLACK_APP_TOKEN is not an app-level token (expected the xapp- prefix)")
	}
	webhookHost, err := checkWebhookURL(cfg.WebhookURL)
	if err != nil {
		return err
	}

	slog.Info("starting slack-socket",
		"version", build.Version(),
		"webhook_host", webhookHost,
		"batch_max_events", cfg.BatchMaxEvents,
		"batch_max_bytes", cfg.BatchMaxBytes,
		"batch_max_wait", cfg.BatchMaxWait.String(),
		"post_timeout", cfg.PostTimeout.String())

	metrics := &Metrics{}

	// Socket Mode only needs the app-level token: apps.connections.open is
	// called with it, and the bot token slot stays empty.
	api := slack.New("", slack.OptionAppLevelToken(cfg.AppToken))
	smc := socketmode.New(api,
		socketmode.OptionDebug(cfg.Debug),
		socketmode.OptionLog(slog.NewLogLogger(handler, slog.LevelDebug)),
	)

	forwarder := NewForwarder(cfg.WebhookURL, cfg.PostTimeout)
	batcher := NewBatcher(BatcherOptions{
		Post: forwarder.Post,
		Ack: func(ctx context.Context, ids []string) error {
			for _, id := range ids {
				if err := smc.AckCtx(ctx, id, nil); err != nil {
					return err
				}
			}
			return nil
		},
		MaxEvents:  cfg.BatchMaxEvents,
		MaxBytes:   cfg.BatchMaxBytes,
		MaxWait:    cfg.BatchMaxWait,
		AckTimeout: cfg.AckTimeout,
		Metrics:    metrics,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           newMux(metrics),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("health server failed", "error", err)
		}
	}()

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// The socket outlives the event loop on shutdown so the final batch can
	// still be acked over it.
	socketCtx, cancelSocket := context.WithCancel(context.Background())
	defer cancelSocket()
	runErr := make(chan error, 1)
	go func() { runErr <- smc.RunContext(socketCtx) }()

	loopCtx, cancelLoop := context.WithCancel(sigCtx)
	defer cancelLoop()
	loop := &eventLoop{client: smc, batcher: batcher, metrics: metrics, now: time.Now}
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		loop.run(loopCtx, smc.Events)
	}()

	var exitErr error
	select {
	case <-sigCtx.Done():
		slog.Info("shutting down, forwarding the pending batch")
	case err := <-runErr:
		// RunContext reconnects on its own and returns only on a fatal
		// error such as a revoked token. Let the pod restart.
		exitErr = err
		metrics.Connected.Store(false)
		slog.Error("the Socket Mode client stopped", "error", err)
	}

	// Stop taking events, flush and ack what is pending, then close the
	// socket. Events Slack sends meanwhile stay unacked and are redelivered
	// to the next connection.
	cancelLoop()
	<-loopDone
	batcher.Close()
	cancelSocket()
	if exitErr == nil {
		select {
		case <-runErr:
		case <-time.After(cfg.ShutdownWait):
			slog.Warn("the Socket Mode client did not stop in time")
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	slog.Info("stopped")
	return exitErr
}

// checkWebhookURL validates the webhook URL without echoing it: the path
// holds the trigger key. It returns the host, which is safe to log.
func checkWebhookURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("SUBSTRATE_WEBHOOK_URL is not an absolute http(s) URL")
	}
	return u.Host, nil
}

// newMux serves /healthz and /metrics.
func newMux(m *Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		connected := m.Connected.Load()
		w.Header().Set("Content-Type", "application/json")
		if !connected {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"connected": connected})
	})
	mux.Handle("GET /metrics", m)
	return mux
}
