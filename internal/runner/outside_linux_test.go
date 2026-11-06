//go:build linux

package runner

import (
	"context"
	"testing"
)

func TestOutsideBodyCannotBypassTheHostProxy(t *testing.T) {
	r := New()
	requireSandbox(t, r)
	requireConnectGate(t, r)
	spec := Spec{Repository: "outside", Function: "publisher.example.com/outside/probe", Runtime: "python", Outside: true, Network: []string{"api.example.com"}, TimeoutMs: 5000, Source: `import socket
import urllib.request

def main(input, host):
    refused = 0
    try:
        socket.create_connection(("1.1.1.1", 443), timeout=1)
    except OSError:
        refused += 1
    try:
        socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    except OSError:
        refused += 1
    for proto in (132, 262):  # SCTP and MPTCP: stream sockets that are not TCP
        try:
            socket.socket(socket.AF_INET, socket.SOCK_STREAM, proto)
        except OSError:
            refused += 1
    try:
        urllib.request.urlopen("https://other.example.com/", timeout=1)
    except OSError:
        refused += 1
    return {"output": refused}
`}
	result, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != float64(5) {
		t.Fatalf("only %v of five bypasses refused", result.Output)
	}
	spec.Network = nil
	result, err = r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != float64(5) {
		t.Fatalf("outside body without network: %v of five refused", result.Output)
	}
}
