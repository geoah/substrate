package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/config"
)

// captureLog points the default logger at a buffer for one test. Not
// parallel: the default logger is process-wide.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// The default opens a socket on loopback and says nothing about cleartext:
// nothing beyond this machine can reach it.
func TestListenOpensLoopbackWithNoSetting(t *testing.T) {
	logs := captureLog(t)
	ln, err := listen(config.Config{BindAddress: "127.0.0.1", Port: "0"})
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
		t.Fatalf("listening on %s, want a loopback address", ln.Addr())
	}
	if strings.Contains(logs.String(), "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT") {
		t.Fatalf("a loopback bind warned about cleartext:\n%s", logs.String())
	}
}

// Every interface without the setting is refused before anything binds, and
// the refusal names the address and the setting; with the setting the server
// starts, and the boot says what the deployment claimed.
func TestListenOnEveryInterfaceNeedsTheCleartextSetting(t *testing.T) {
	logs := captureLog(t)
	_, err := listen(config.Config{BindAddress: "0.0.0.0", Port: "0"})
	if err == nil {
		t.Fatal("listen on 0.0.0.0 with no setting opened a socket")
	}
	for _, named := range []string{"SUBSTRATE_BIND_ADDRESS", `"0.0.0.0"`, "0.0.0.0:0", "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true"} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the refusal does not name %s: %v", named, err)
		}
	}

	ln, err := listen(config.Config{BindAddress: "0.0.0.0", Port: "0", InsecureAllowCleartext: true})
	if err != nil {
		t.Fatalf("listen on 0.0.0.0 with SUBSTRATE_INSECURE_ALLOW_CLEARTEXT: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsUnspecified() {
		t.Fatalf("listening on %s, want every interface", ln.Addr())
	}
	// 0.0.0.0 on "tcp" is Go's dual-stack wildcard, the socket ":8080" used to
	// open, so the image's setting still takes IPv6 where the host has it.
	port := ln.Addr().(*net.TCPAddr).Port
	targets := []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	if probe, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		_ = probe.Close()
		targets = append(targets, net.JoinHostPort("::1", strconv.Itoa(port)))
	}
	for _, target := range targets {
		conn, err := net.Dial("tcp", target)
		if err != nil {
			t.Fatalf("a bind of 0.0.0.0 does not answer on %s: %v", target, err)
		}
		_ = conn.Close()
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT is set") ||
		!strings.Contains(out, ln.Addr().String()) {
		t.Fatalf("the boot did not warn naming the setting and %s:\n%s", ln.Addr(), out)
	}
}

// syncLog points the default logger at a buffer the server's goroutines may
// write while the test reads it. Not parallel: the default logger is
// process-wide.
func syncLog(t *testing.T) *syncWriter {
	t.Helper()
	logs := &syncWriter{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

// serveOnLoopback serves srv on a free loopback port and returns its URL.
func serveOnLoopback(t *testing.T, srv *http.Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

// openStream starts a GET whose handler writes one line and then holds the
// response open, and returns the body once that line has arrived.
func openStream(t *testing.T, url string) *bufio.Reader {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	br := bufio.NewReader(resp.Body)
	if line, err := br.ReadString('\n'); err != nil || line != "open\n" {
		t.Fatalf("first line = %q, %v", line, err)
	}
	return br
}

// A stream the API ends on its draining channel lets Shutdown finish at
// once: the server closes the channel when it starts to shut down, and the
// stream's body reaches a clean end. Before, nothing closed it until the
// service did, after Shutdown, so every stop with a console tab open spent
// the whole budget and exited 1.
func TestShutdownClosesDrainingSoAStreamEnds(t *testing.T) {
	logs := syncLog(t)
	draining := make(chan struct{})
	srv := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("open\n"))
		w.(http.Flusher).Flush()
		select {
		case <-draining:
		case <-r.Context().Done():
		}
	}), draining)
	br := openStream(t, serveOnLoopback(t, srv)+"/stream")

	begun := time.Now()
	shutdown(srv, 3*time.Second)
	if took := time.Since(begun); took > time.Second {
		t.Fatalf("shutdown took %s with one stream open, want it to end the stream and return at once", took)
	}
	if rest, err := io.ReadAll(br); err != nil || len(rest) != 0 {
		t.Fatalf("the stream ended with %q, %v; want a clean end", rest, err)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("a shutdown inside its budget warned:\n%s", logs.String())
	}
}

// A request still running when the budget runs out is logged and left, and
// shutdown returns, so the caller goes on to stop the loops and close the
// service instead of returning the deadline as the run's error.
func TestShutdownPastItsBudgetWarnsAndReturns(t *testing.T) {
	logs := syncLog(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := newServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("open\n"))
		w.(http.Flusher).Flush()
		<-release
	}), make(chan struct{}))
	openStream(t, serveOnLoopback(t, srv)+"/stuck")

	shutdown(srv, 100*time.Millisecond)
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "drain budget ran out") || !strings.Contains(out, "budget=100ms") {
		t.Fatalf("a shutdown past its budget did not warn naming it:\n%s", out)
	}
}

// The server closes idle keep-alive connections, later than any proxy in
// front would, sets no ReadTimeout or WriteTimeout (either would cut every
// stream at a fixed age), and logs net/http's own lines through slog.
func TestNewServerTimeoutsAndErrorLog(t *testing.T) {
	logs := syncLog(t)
	srv := newServer(http.NotFoundHandler(), make(chan struct{}))
	if srv.IdleTimeout <= 2*time.Minute {
		t.Errorf("IdleTimeout = %s, want longer than Caddy's 2m upstream idle timeout", srv.IdleTimeout)
	}
	if srv.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout = %s, want one", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Errorf("ReadTimeout = %s, WriteTimeout = %s; want neither", srv.ReadTimeout, srv.WriteTimeout)
	}
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog is nil: net/http writes its lines to stderr outside slog")
	}
	srv.ErrorLog.Print("http: TLS handshake error from 192.0.2.1:1234: EOF")
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "TLS handshake error") {
		t.Fatalf("net/http's line did not reach slog at WARN:\n%s", out)
	}
}

// A state key with no callback URL is inert: the boot says so rather than
// refusing, and the facility stays off.
func TestAStateKeyWithoutACallbackURLWarns(t *testing.T) {
	logs := syncLog(t)
	if opt := oauthOption(config.Config{OAuthStateKey: "k"}); opt != nil {
		t.Fatal("the OAuth facility turned on without a callback URL")
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "SUBSTRATE_OAUTH_STATE_KEY is set without SUBSTRATE_OAUTH_CALLBACK_URL") {
		t.Fatalf("no warning named the unused key:\n%s", out)
	}
	logs = syncLog(t)
	if opt := oauthOption(config.Config{}); opt != nil || logs.String() != "" {
		t.Fatalf("neither set: option %v, log %q; want nothing", opt, logs.String())
	}
}

// serve after the boot: a signal starts the shutdown, the loops are stopped
// before the drain, and a drain that outlives its budget still returns nil,
// so run reaches the loop barrier and svc.Close and the process exits 0. It
// once returned the deadline, which main logged as fatal and exited 1 on.
func TestServeReturnsNilWhenTheDrainOutlivesItsBudget(t *testing.T) {
	logs := syncLog(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := newServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("open\n"))
		w.(http.Flusher).Flush()
		<-release
	}), make(chan struct{}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	stopCh := make(chan os.Signal, 1)
	var stopped atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- serve(context.Background(), srv, ln, stopCh, func() { stopped.Store(true) }, 100*time.Millisecond)
	}()
	openStream(t, "http://"+ln.Addr().String()+"/stuck")
	stopCh <- syscall.SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve = %v after a drain past its budget, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after its budget")
	}
	if !stopped.Load() {
		t.Fatal("serve returned without stopping the loops")
	}
	if !strings.Contains(logs.String(), "drain budget ran out") {
		t.Fatalf("the overrun was not logged:\n%s", logs.String())
	}
}
