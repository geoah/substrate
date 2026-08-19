package embed

import (
	"os"
	"testing"
)

// The embedder tests point a client at an httptest fake on loopback, which the
// #241 egress gate refuses by default. They take the operator escape the gate
// documents, exactly as internal/llm and the engine's connector suites do.
func TestMain(m *testing.M) {
	if os.Getenv("SUBSTRATE_EGRESS_ALLOW") == "" {
		_ = os.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	os.Exit(m.Run())
}
