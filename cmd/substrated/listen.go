package main

import (
	"log/slog"
	"net"

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
