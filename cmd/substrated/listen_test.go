package main

import (
	"bytes"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"

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
