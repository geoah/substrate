//go:build unix

package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// After the first signal serve stops listening for more, so a second one
// takes the default action and ends a shutdown that does not return. It once
// kept the channel registered, and the second SIGTERM was swallowed. SIGUSR1
// stands in for SIGTERM, and a second channel holds it before it is sent
// again, so the test process never meets the default action.
func TestServeHandsTheSecondSignalBackToTheDefaultAction(t *testing.T) {
	syncLog(t)
	srv := newServer(http.NotFoundHandler(), make(chan struct{}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGUSR1)
	defer signal.Stop(stopCh)

	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), srv, ln, stopCh, func() {}, time.Second) }()
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down on the first signal")
	}

	probe := make(chan os.Signal, 1)
	signal.Notify(probe, syscall.SIGUSR1)
	defer signal.Stop(probe)
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe:
	case <-time.After(5 * time.Second):
		t.Fatal("the second signal never arrived")
	}
	if n := len(stopCh); n != 0 {
		t.Fatalf("serve's channel took the second signal too (%d queued): it is still registered", n)
	}
}
