package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/sandbox"
)

// The boot line is the only place a developer is told what the sandbox is and
// is not doing, so it is asserted rather than eyeballed: and asserted for
// platforms this test is not running on, because the macOS branch is the one
// that matters most and the one no CI machine executes.

func attr(attrs []any, key string) string {
	for i := 0; i+1 < len(attrs); i += 2 {
		if k, _ := attrs[i].(string); k == key {
			v, _ := attrs[i+1].(string)
			return v
		}
	}
	return ""
}

// macOS: bodies are not confined, and the line has to say so in terms a reader
// can act on. "Unconfined" alone is not one of them.
func TestBootLineOnMacOSEnumeratesWhatIsNotEnforced(t *testing.T) {
	level, msg, attrs := sandboxReport(sandbox.ModeBestEffort, sandbox.Report{OS: "darwin"})

	if level != slog.LevelWarn {
		t.Fatalf("level = %v, want WARN: an unsupported platform is a development choice, not a broken deployment", level)
	}
	if !strings.Contains(msg, "darwin") || !strings.Contains(msg, "NOT confined") {
		t.Fatalf("message does not name the platform and the consequence: %q", msg)
	}
	// The advice must NOT be the one that cannot help here: there is no kernel
	// setting to change, and enforce would refuse to run any function at all.
	joined := msg + " " + strings.Join([]string{
		attr(attrs, "reason"), attr(attrs, "not_enforced"),
		attr(attrs, "still_enforced"), attr(attrs, "advice"),
	}, " ")
	if strings.Contains(joined, "SUBSTRATE_SANDBOX=enforce") {
		t.Fatal("the macOS line points at enforce, which refuses to run any function on this platform")
	}
	// Each limitation a reader is exposed to has to be named.
	for _, want := range []string{"capabilities.network", "filesystem", "resource limits"} {
		if !strings.Contains(attr(attrs, "not_enforced"), want) {
			t.Fatalf("the line does not mention %q: %q", want, attr(attrs, "not_enforced"))
		}
	}
	// And what SURVIVES, so the reader does not conclude that nothing does.
	if !strings.Contains(attr(attrs, "still_enforced"), "one process per function") {
		t.Fatalf("the line does not say what still holds: %q", attr(attrs, "still_enforced"))
	}
	if attr(attrs, "platform") != "darwin" {
		t.Fatalf("platform attribute = %q", attr(attrs, "platform"))
	}
}

// A Linux kernel missing a layer is a DIFFERENT problem with a different
// answer: it is fixable, so it is an error and it points at enforce.
func TestBootLineOnADegradedKernelIsAnError(t *testing.T) {
	level, msg, _ := sandboxReport(sandbox.ModeBestEffort,
		sandbox.Report{OS: "linux", LandlockABI: 0, Seccomp: true})
	if level != slog.LevelError {
		t.Fatalf("level = %v, want ERROR", level)
	}
	if !strings.Contains(msg, "DEGRADED") || !strings.Contains(msg, "SUBSTRATE_SANDBOX=enforce") {
		t.Fatalf("a degraded kernel must be named and answerable: %q", msg)
	}
}

func TestBootLineWhenEverythingApplies(t *testing.T) {
	level, msg, attrs := sandboxReport(sandbox.ModeEnforce,
		sandbox.Report{OS: "linux", LandlockABI: 4, Seccomp: true})
	if level != slog.LevelInfo {
		t.Fatalf("level = %v, want INFO", level)
	}
	if !strings.Contains(msg, "active") {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(attr(attrs, "kernel"), "ABI v4") {
		t.Fatalf("the line does not report the ABI it got: %q", attr(attrs, "kernel"))
	}
}

// Off is the operator's own choice, so it warns rather than erroring: but it
// still says what the choice costs.
func TestBootLineWhenTurnedOff(t *testing.T) {
	level, msg, _ := sandboxReport(sandbox.ModeOff, sandbox.Report{OS: "linux", LandlockABI: 4, Seccomp: true})
	if level != slog.LevelWarn {
		t.Fatalf("level = %v, want WARN", level)
	}
	if !strings.Contains(msg, "OFF") || !strings.Contains(msg, "unconfined") {
		t.Fatalf("message = %q", msg)
	}
}
