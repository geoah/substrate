package main

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/runner"
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
	for _, want := range []string{"`network:`", "filesystem", "resource limits"} {
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
		sandbox.Report{OS: "linux", LandlockABI: 0, Seccomp: true, ConnectGate: true})
	if level != slog.LevelError {
		t.Fatalf("level = %v, want ERROR", level)
	}
	if !strings.Contains(msg, "DEGRADED") || !strings.Contains(msg, "SUBSTRATE_SANDBOX=enforce") {
		t.Fatalf("a degraded kernel must be named and answerable: %q", msg)
	}
}

func TestBootLineWhenEverythingApplies(t *testing.T) {
	level, msg, attrs := sandboxReport(sandbox.ModeEnforce,
		sandbox.Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGate: true})
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
	level, msg, _ := sandboxReport(sandbox.ModeOff, sandbox.Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGate: true})
	if level != slog.LevelWarn {
		t.Fatalf("level = %v, want WARN", level)
	}
	if !strings.Contains(msg, "OFF") || !strings.Contains(msg, "unconfined") {
		t.Fatalf("message = %q", msg)
	}
}

// The container case the stock docker profile produces: every layer installs
// and the connect gate's supervisor cannot answer a single notification, so
// the line must name the capability rather than a kernel setting, and say that
// network bodies are refused rather than quietly unfiltered. This is the
// failure that shipped as "install a provider and every uv fetch is denied".
func TestBootLineWhenTheConnectGateCannotBeServiced(t *testing.T) {
	level, msg, attrs := sandboxReport(sandbox.ModeBestEffort,
		sandbox.Report{
			OS: "linux", LandlockABI: 4, Seccomp: true,
			ConnectGate: false, ConnectGateErr: "pidfd_getfd: operation not permitted",
		})
	if level != slog.LevelError {
		t.Fatalf("level = %v, want ERROR", level)
	}
	if !strings.Contains(msg, "connect gate") || !strings.Contains(msg, "REFUSED") {
		t.Fatalf("the line does not name the gate and what stops working: %q", msg)
	}
	if !strings.Contains(attr(attrs, "refused"), "pidfd_getfd") {
		t.Fatalf("the line does not name the syscall that refused: %q", attr(attrs, "refused"))
	}
	// The remedy, not a kernel setting: this is the one degradation an
	// operator fixes in their deployment file.
	advice := attr(attrs, "advice")
	for _, want := range []string{"CAP_SYS_PTRACE", "process_vm_readv", "cap_add"} {
		if !strings.Contains(advice, want) {
			t.Fatalf("the advice does not mention %q: %q", want, advice)
		}
	}
}

// The trust line is read by the same person for the same reason: a store that
// does not exist fails every network body, silently, and only on some hosts.
// All four branches are asserted here because the interesting one — an
// interpreter whose compiled-in store is empty — is the python.org macOS
// build, which no CI machine has.

func TestTrustLineNamesTheSystemBundleWhenTheInterpreterHasNone(t *testing.T) {
	level, msg, attrs := trustReport(runner.TrustStore{Path: "/etc/ssl/cert.pem", Injected: true}, nil)

	if level != slog.LevelInfo {
		t.Fatalf("level = %v, want INFO: the store was found, nothing is wrong", level)
	}
	if !strings.Contains(msg, "trust") {
		t.Fatalf("message does not say what bodies trust: %q", msg)
	}
	if got := attr(attrs, "bundle"); got != "/etc/ssl/cert.pem" {
		t.Fatalf("bundle = %q, want the chosen path", got)
	}
	// The mechanism is named, because a reader debugging a handshake needs to
	// know a variable is being set for them.
	if got := attr(attrs, "source"); got != "SSL_CERT_FILE" {
		t.Fatalf("source = %q, want SSL_CERT_FILE", got)
	}
}

func TestTrustLineNamesTheInterpretersOwnStore(t *testing.T) {
	level, _, attrs := trustReport(runner.TrustStore{Path: "/usr/lib/ssl/cert.pem"}, nil)

	if level != slog.LevelInfo {
		t.Fatalf("level = %v, want INFO", level)
	}
	if got := attr(attrs, "bundle"); got != "/usr/lib/ssl/cert.pem" {
		t.Fatalf("bundle = %q, want the interpreter's own path", got)
	}
	if got := attr(attrs, "source"); got != "interpreter default" {
		t.Fatalf("source = %q, want the interpreter default", got)
	}
}

// Nothing found anywhere: a WARN that says what breaks and where a store was
// looked for, not "no trust store configured".
func TestTrustLineWarnsWhenNothingIsTrusted(t *testing.T) {
	level, msg, attrs := trustReport(runner.TrustStore{}, nil)

	if level != slog.LevelWarn {
		t.Fatalf("level = %v, want WARN", level)
	}
	if !strings.Contains(msg, "NONE") || !strings.Contains(msg, "network:") {
		t.Fatalf("message does not name the consequence for a network body: %q", msg)
	}
	for _, bundle := range runner.SystemCertBundles() {
		if !strings.Contains(attr(attrs, "searched"), bundle) {
			t.Fatalf("searched does not list %s: %q", bundle, attr(attrs, "searched"))
		}
	}
}

func TestTrustLineWarnsWhenTheInterpreterCannotBeAsked(t *testing.T) {
	level, msg, attrs := trustReport(runner.TrustStore{}, errors.New("python3 not found"))

	if level != slog.LevelWarn {
		t.Fatalf("level = %v, want WARN", level)
	}
	if !strings.Contains(msg, "UNKNOWN") {
		t.Fatalf("message claims to know something it does not: %q", msg)
	}
	if !strings.Contains(attr(attrs, "error"), "python3 not found") {
		t.Fatalf("the probe failure is not carried: %v", attrs)
	}
}
