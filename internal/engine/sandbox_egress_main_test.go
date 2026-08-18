package engine_test

import (
	"os"
	"testing"
)

// The connector bundle suites point a network-granted body at an httptest fake
// on loopback, which the sandbox connect gate refuses by default: a real
// deployment's Postgres and metadata endpoint sit in exactly those ranges
// (0035-a-network-body-connect-is-filtered-by-destination). These tests are not
// the gate's own (that is internal/runner and internal/sandbox); they need the
// loopback fake reachable, so they take the operator escape the gate documents.
// Set before m.Run, so the runner reads it when it starts the first body.
func TestMain(m *testing.M) {
	if os.Getenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	os.Exit(m.Run())
}
