package sandbox

import (
	"os/exec"
	"runtime"
	"testing"
)

// The platform-independent half of the package, so `go test ./internal/sandbox`
// does something on a macOS laptop instead of compiling to an empty suite,
// which is exactly where a mistake in the OFF path would otherwise hide.

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{
		"": ModeBestEffort, "off": ModeOff, "best-effort": ModeBestEffort, "enforce": ModeEnforce,
	} {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("strict"); err == nil {
		t.Fatal("expected an error for an unknown mode")
	}
}

// The mode exists so a deployment can insist, and insisting has to mean
// refusing rather than logging.
func TestEnforceRefusesWithoutTheKernel(t *testing.T) {
	c := &Confiner{mode: ModeEnforce, report: Report{OS: runtime.GOOS}}
	if err := c.Wrap(exec.Command("/bin/true"), Policy{}); err == nil {
		t.Fatal("enforce mode admitted a child with no kernel support")
	}
	if !c.Degraded() {
		t.Fatal("a confiner with no kernel support is degraded")
	}
}

// ModeOff is the one mode that must never refuse: it is the escape hatch.
func TestOffNeverRefuses(t *testing.T) {
	c := New(ModeOff)
	if err := c.Wrap(exec.Command("/bin/true"), Policy{}); err != nil {
		t.Fatalf("off mode refused a child: %v", err)
	}
	if c.Degraded() {
		t.Fatal("off mode is not a degradation: it is the setting asked for")
	}
}

// An unsupported platform has to SAY so. Reporting it as a kernel missing a
// layer would send an operator looking for an lsm= setting that does not exist,
// and the boot log's advice branches on exactly this.
func TestReportNamesAnUnsupportedPlatform(t *testing.T) {
	darwin := Report{OS: "darwin"}
	if darwin.Supported() {
		t.Fatal("darwin reported as supported")
	}
	if got := darwin.String(); !contains(got, "darwin") || !contains(got, "cannot be confined") {
		t.Fatalf("unsupported platform reads as %q", got)
	}
	linux := Report{OS: "linux", LandlockABI: 4, Seccomp: true}
	if !linux.Supported() || !linux.FS() {
		t.Fatal("a linux report with landlock is supported")
	}
	if got := linux.String(); !contains(got, "ABI v4") {
		t.Fatalf("supported platform reads as %q", got)
	}
	// This build's own platform must agree with the constructor.
	if New(ModeBestEffort).Report().OS != runtime.GOOS {
		t.Fatal("the report does not name the platform it ran on")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
