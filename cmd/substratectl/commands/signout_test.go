package commands

// `logout --everywhere` presents the factors and no token, asks for the code
// the way a login does, and forgets the context's token, which the sign-out
// ended with the rest.

import (
	"strings"
	"testing"
)

func TestLogoutEverywhereSignsOutWithTheFactorsAndForgetsTheToken(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("hunter2\n")
	out, _ := h.mustRun("logout", "--everywhere", "--password-stdin", "--totp-code", "654321")
	if got := h.lastRequest(); got != "POST "+pathSignOutAll {
		t.Fatalf("logout --everywhere hit %q, want the sign out everywhere", got)
	}
	if h.fake.lastAuth != "" {
		t.Errorf("logout --everywhere sent a bearer token (%q): the factors are the evidence", h.fake.lastAuth)
	}
	if !strings.Contains(out, "signed out everywhere") || !strings.Contains(out, "3 tokens ended") {
		t.Errorf("logout --everywhere output:\n%s", out)
	}
	cfg, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got := cfg.Contexts[0]; got.Token != "" || got.TokenID != "" || got.Server == "" {
		t.Errorf("the context after the sign-out = %+v, want the server kept and the token forgotten", got)
	}
}
