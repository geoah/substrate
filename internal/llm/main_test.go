package llm

import (
	"os"
	"testing"
)

// The wire-adapter tests point a client at an httptest fake on loopback, which
// the #241 egress gate refuses by default: a real deployment's Postgres and
// metadata endpoint sit in exactly those ranges. These tests are not the gate's
// own (that is internal/egress), so they take the operator escape the gate
// documents. A test that means to prove the gate BLOCKS clears the variable for
// itself with t.Setenv, so it does not depend on this default.
//
// The run's code goes through the live ledger (live_test.go): a pass that
// bought anything prints its spend, and a pass past a ceiling exits 1 even
// when every case passed. A hermetic run books nothing and keeps its code.
func TestMain(m *testing.M) {
	if os.Getenv("SUBSTRATE_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	os.Exit(liveLedger.Exit(m.Run(), os.Stdout))
}
