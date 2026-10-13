package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/geoah/substrate/internal/config"
)

// listen opens the server's socket at the address the configuration allows,
// and refuses the one it does not (config.ListenAddress) before binding
// anything. A non-loopback bind is said at boot, every time: the server
// cannot see what sits in front of it, so the log is where an operator reads
// which statement this deployment made.
func listen(cfg config.Config) (net.Listener, error) {
	addr, err := cfg.ListenAddress()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	// The socket's own address, not the configured text: what the kernel
	// bound is what the network can reach.
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		slog.Warn("SUBSTRATE_INSECURE_ALLOW_CLEARTEXT is set: serving plain HTTP on a non-loopback address; only a TLS terminator or a loopback port mapping may reach it",
			"address", ln.Addr().String())
	}
	return ln, nil
}

const (
	// idleTimeout closes a keep-alive connection nobody has used for this
	// long. It is longer than the idle timeout of any proxy that may sit in
	// front (Caddy's is 2 minutes, nginx's and a cloud load balancer's 60 s): a
	// server that closed first could close a connection the proxy was just
	// reusing, and the proxy would answer that request 502.
	idleTimeout = 5 * time.Minute
	// shutdownBudget is how long a shutdown waits for the requests in flight
	// before it closes the service under them.
	shutdownBudget = 10 * time.Second
)

// newServer is the HTTP server around handler. Shutdown closes draining
// (api.Config.Draining), which ends the watch streams and the export, the
// requests that would otherwise hold it for its whole budget. There is no
// ReadTimeout or WriteTimeout: either would cut every stream at a fixed age,
// so the API bounds each body read and each stream write itself. net/http's
// own error lines go through slog with everything else.
func newServer(handler http.Handler, draining chan<- struct{}) *http.Server {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
	srv.RegisterOnShutdown(func() { close(draining) })
	return srv
}

// serve answers on ln until a signal arrives on stopCh, ctx ends (a signal
// that landed as the boot returned) or the server fails, and then calls stop
// and shuts srv down within budget. A shutdown that runs out of budget is not
// a failed run: it returns nil, so the caller's barrier and svc.Close still
// run and the exit is 0.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, stopCh chan os.Signal, stop func(), budget time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "address", ln.Addr().String())
		errCh <- srv.Serve(ln)
	}()
	select {
	case err := <-errCh:
		return err
	case sig := <-stopCh:
		// The next signal takes the default action and ends the process at
		// once, for a shutdown that does not return promptly.
		signal.Stop(stopCh)
		slog.Info("shutting down", "signal", sig.String())
	case <-ctx.Done():
		slog.Info("shutting down", "cause", context.Cause(ctx).Error())
	}
	stop()
	shutdown(srv, budget)
	return nil
}

// shutdown stops srv taking requests and waits up to budget for the ones in
// flight. A request still running at the budget is logged and left: the
// caller goes on to stop the loops and close the service, because a process
// that exits before those finish tears the writes they hold.
func shutdown(srv *http.Server, budget time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Warn("shutdown: requests were still running when the drain budget ran out; closing the service under them",
			"budget", budget.String(), "error", err)
	}
}
