package engine_test

import (
	"os"
	"testing"
)

// Two gates refuse loopback by default, and the engine suites lean on both
// through the same httptest fakes on loopback. The sandbox connect gate refuses
// a network-granted body's dial (0035-a-network-body-connect-is-filtered-by-destination),
// and the #241 server-side gate refuses the engine's own dial of a
// repository-chosen llmprovider baseURL. A real deployment's Postgres and
// metadata endpoint sit in exactly those ranges. The agent-loop and embeddings
// suites point provider rows at loopback fakes, so they take the operator escape
// each gate documents. Set before m.Run, so the runner reads the sandbox one
// when it starts the first body and llm.New/embed.New read the server one when
// they build the first client.
func TestMain(m *testing.M) {
	if os.Getenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	if os.Getenv("SUBSTRATE_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	os.Exit(m.Run())
}
