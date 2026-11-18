// Command substrate-push-relay is the publisher's push relay
// (docs/plans/push.md, section 1; docs/operations.md, "Running the push
// relay"): it registers the installs of the publisher's apps and sends the
// pushes substrate servers sign for them to Apple, with the publisher's
// Apple keys. It keeps nothing on disk and is configured from the
// environment alone.
//
//	substrate-push-relay          serve
//	substrate-push-relay check    mint a provider token per environment and send one alert
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/geoah/substrate/internal/apns"
	"github.com/geoah/substrate/internal/build"
	"github.com/geoah/substrate/internal/pushrelay"
	"github.com/geoah/substrate/internal/pushwire"
)

// appleHosts are Apple's, by environment; tests point them at a fake.
var appleHosts = map[string]string{
	pushwire.EnvironmentProduction:  apns.HostProduction,
	pushwire.EnvironmentDevelopment: apns.HostDevelopment,
}

// appleTLS is nil outside tests, which trust a fake Apple's certificate.
var appleTLS *tls.Config

const usage = `substrate-push-relay: the publisher's push relay

Usage:
  substrate-push-relay           serve, configured from the environment
  substrate-push-relay check     mint a provider token for each environment and send one alert
  substrate-push-relay version   print the version

docs/operations.md, "Running the push relay", lists the variables.
`

func main() {
	// slog's default prints a message unquoted through the stdlib log
	// package, so a quoting handler is in place before anything can log.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "check":
			return check(ctx, args[1:], stdout, stderr)
		case "version":
			_, _ = fmt.Fprintln(stdout, build.Version())
			return 0
		case "help", "-h", "-help", "--help":
			_, _ = io.WriteString(stdout, usage)
			return 0
		default:
			_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
			return 2
		}
	}
	if err := serve(ctx, stderr); err != nil {
		slog.Error("substrate-push-relay stopped", "error", err.Error())
		return 1
	}
	return 0
}

func newLogger(c config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.logLevel}
	if c.logJSON {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

// clients makes one Apple client per configured environment, each with its
// own pool of connections.
func clients(c config, metrics *pushrelay.Metrics) (map[string]*apns.Client, error) {
	out := map[string]*apns.Client{}
	for _, k := range c.keys {
		env := k.environment
		client, err := apns.New(apns.Config{
			BaseURL: appleHosts[env], TeamID: c.teamID, KeyID: k.id, Key: k.key,
			Transport: apns.NewTransport(appleTLS),
			OnMint: func() {
				if metrics != nil {
					metrics.Minted(env)
				}
			},
		})
		if err != nil {
			return nil, err
		}
		out[env] = client
	}
	return out, nil
}

func serve(ctx context.Context, logOut io.Writer) error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	log := newLogger(c, logOut)
	slog.SetDefault(log)
	metrics := pushrelay.NewMetrics()
	apple, err := clients(c, metrics)
	if err != nil {
		return err
	}
	sealer, err := pushrelay.NewSealer(c.sealKey, c.sealPrevious, rand.Reader)
	if err != nil {
		return err
	}
	senders := map[string]pushrelay.Sender{}
	envs := make([]string, 0, len(apple))
	for env, client := range apple {
		senders[env] = client
		envs = append(envs, env)
	}
	relay, err := pushrelay.New(pushrelay.Config{
		Topics: c.topics, Apple: senders, Sealer: sealer, Limits: c.limits,
		TrustedProxies: c.trusted, Log: log, Metrics: metrics,
	})
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", c.listen)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           relay,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Apple's own timeout, a replaced provider token's second send and
		// a margin.
		WriteTimeout:   2*apns.Timeout + 5*time.Second,
		IdleTimeout:    2 * time.Minute,
		MaxHeaderBytes: 16 << 10,
		ErrorLog:       slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}
	errs := make(chan error, 2)
	go func() { errs <- srv.Serve(ln) }()

	var metricsSrv *http.Server
	if c.metricsAddress != "" {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", metrics.Handler())
		metricsSrv = &http.Server{
			Addr: c.metricsAddress, Handler: mux,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
			WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 16 << 10,
		}
		go func() { errs <- metricsSrv.ListenAndServe() }()
	}

	if c.unproxied() {
		log.Warn("substrate-push-relay listens beyond loopback and SUBSTRATE_TRUSTED_PROXIES is not set: " +
			"behind a TLS terminator every caller is the terminator, and the per-caller limits hold them all as one. " +
			"Set it to the address the terminator connects from (docs/operations.md, \"Running the push relay\")")
	}
	log.Info("substrate-push-relay listening",
		"version", build.Version(), "address", ln.Addr().String(), "environments", envs, "topics", c.topics,
		"previous_seal_key", c.sealPrevious != nil, "metrics_address", c.metricsAddress)

	// Ready once every environment's key parsed, which loadConfig held,
	// and minted a provider token. Apple is not called: an outage there
	// should not take the relay out of service.
	for env, client := range apple {
		if _, err := client.Token(); err != nil {
			return fmt.Errorf("mint a provider token for %s: %w", env, err)
		}
	}
	relay.MarkReady()

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-sigCtx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	log.Info("substrate-push-relay shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 2*apns.Timeout+5*time.Second)
	defer cancel()
	if metricsSrv != nil {
		_ = metricsSrv.Shutdown(shutdown)
	}
	return srv.Shutdown(shutdown)
}
