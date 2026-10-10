package sandbox

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
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
	linux := Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGate: true}
	if !linux.Supported() || !linux.FS() {
		t.Fatal("a linux report with landlock is supported")
	}
	if got := linux.String(); !contains(got, "ABI v4") {
		t.Fatalf("supported platform reads as %q", got)
	}
	// The gate is a third layer with its own failure, so the line an operator
	// reads carries it beside the other two, and a refusal says which syscall.
	gone := Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGateErr: "pidfd_getfd: operation not permitted"}
	if !gone.Degraded(ModeBestEffort) {
		t.Fatal("a report whose connect gate cannot be serviced is degraded")
	}
	if got := gone.String(); !contains(got, "connect gate: unavailable") || !contains(got, "pidfd_getfd") {
		t.Fatalf("an unserviceable gate reads as %q", got)
	}
	// This build's own platform must agree with the constructor.
	if New(ModeBestEffort).Report().OS != runtime.GOOS {
		t.Fatal("the report does not name the platform it ran on")
	}
}

// The DEGRADED line has to say WHICH layer is missing and why, or an operator
// on a kernel that lacks one (a Raspberry Pi kernel ships without Landlock)
// cannot tell what to enable. One case per reason the probe writes, and the
// derived ones Missing writes itself.
func TestMissingNamesEachLayerAndItsReason(t *testing.T) {
	const (
		notBuilt  = "not supported by this kernel: build it with CONFIG_SECURITY_LANDLOCK=y and add landlock to its lsm= list"
		disabled  = "built in but disabled: add it to the kernel's lsm= list"
		noTable   = "no syscall table for arm in this build: run the linux/amd64 or linux/arm64 build"
		profile   = "seccomp(2): operation not permitted: allow seccomp(2) in the seccomp profile this process runs under"
		gateNoCap = "pidfd_getfd: operation not permitted"
	)
	full := Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGate: true}
	for _, tc := range []struct {
		name   string
		report Report
		want   []string // one entry per missing layer: the prefix each entry starts with
		exact  bool     // each entry is exactly its want, with nothing appended
		absent string   // a fix that must not appear: the advice for a different failure
	}{
		{
			name:   "landlock not built",
			report: Report{OS: "linux", LandlockErr: notBuilt, Seccomp: true, ConnectGate: true},
			want:   []string{"filesystem (Landlock): " + notBuilt},
		},
		{
			name:   "landlock left out of lsm=",
			report: Report{OS: "linux", LandlockErr: disabled, Seccomp: true, ConnectGate: true},
			want:   []string{"filesystem (Landlock): " + disabled},
		},
		{
			// Some confinement is applied, and it still does not count: below
			// MinLandlockABI truncate(2) is not mediated.
			name:   "landlock below the minimum ABI",
			report: Report{OS: "linux", LandlockABI: 2, Seccomp: true, ConnectGate: true},
			want: []string{"filesystem (Landlock): ABI v2 does not mediate truncate(2), so a body can still " +
				"empty any file this uid may write: run Linux 6.2 or newer for ABI v3"},
		},
		{
			name:   "no syscall table for the architecture",
			report: Report{OS: "linux", LandlockABI: 4, SeccompErr: noTable, ConnectGate: true},
			want:   []string{"syscall filter (seccomp): " + noTable},
		},
		{
			name:   "seccomp denied by a profile",
			report: Report{OS: "linux", LandlockABI: 4, SeccompErr: profile, ConnectGate: true},
			want:   []string{"syscall filter (seccomp): " + profile},
		},
		{
			name:   "connect gate refused by a profile",
			report: Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGateErr: gateNoCap, ConnectGateErrno: syscall.EPERM},
			want:   []string{"connect gate: " + gateNoCap + ": give the container CAP_SYS_PTRACE"},
			absent: "Linux 5.9",
		},
		{
			name: "connect gate refused with EACCES",
			report: Report{
				OS: "linux", LandlockABI: 4, Seccomp: true,
				ConnectGateErr: "process_vm_readv: permission denied", ConnectGateErrno: syscall.EACCES,
			},
			want: []string{"connect gate: process_vm_readv: permission denied: give the container CAP_SYS_PTRACE"},
		},
		{
			// A kernel without the syscall: no capability adds one, so the
			// capability advice would send the operator the wrong way.
			name: "connect gate on a kernel too old for it",
			report: Report{
				OS: "linux", LandlockABI: 4, Seccomp: true,
				ConnectGateErr: "pidfd_getfd: function not implemented", ConnectGateErrno: syscall.ENOSYS,
			},
			want: []string{"connect gate: pidfd_getfd: function not implemented: " +
				"this kernel is too old for the gate, which needs Linux 5.9 or newer: upgrade the kernel"},
			absent: "CAP_SYS_PTRACE",
		},
		{
			// Any other errno has no known fix: the entry names the syscall and
			// the errno, and nothing else.
			name: "connect gate refused with another errno",
			report: Report{
				OS: "linux", LandlockABI: 4, Seccomp: true,
				ConnectGateErr: "pidfd_getfd: invalid argument", ConnectGateErrno: syscall.EINVAL,
			},
			want:  []string{"connect gate: pidfd_getfd: invalid argument"},
			exact: true,
		},
		{
			// "We could not ask" must not read as a missing capability.
			name:   "connect gate not probed",
			report: Report{OS: "linux", LandlockABI: 4, Seccomp: true, Err: errors.New("start: no such file")},
			want:   []string{"connect gate: not probed: start: no such file"},
		},
		{
			// Every layer at once, in a fixed order, one entry each.
			name:   "everything missing",
			report: Report{OS: "linux", LandlockErr: notBuilt, SeccompErr: noTable, ConnectGateErr: gateNoCap},
			want:   []string{"filesystem (Landlock): " + notBuilt, "syscall filter (seccomp): " + noTable, "connect gate: " + gateNoCap},
		},
		{
			// A hand-built report with no reason still names the layer.
			name:   "no reason recorded",
			report: Report{OS: "linux", Seccomp: true, ConnectGate: true},
			want:   []string{"filesystem (Landlock): unavailable"},
		},
		{
			name:   "unsupported platform",
			report: Report{OS: "darwin"},
			want: []string{
				"filesystem (Landlock): darwin has no Landlock: run function bodies on Linux",
				"syscall filter (seccomp): darwin has no seccomp",
				"connect gate: darwin has no seccomp",
			},
		},
		{name: "nothing missing", report: full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.report.Missing(ModeBestEffort)
			if len(got) != len(tc.want) {
				t.Fatalf("Missing = %q, want %d entries like %q", got, len(tc.want), tc.want)
			}
			for i := range tc.want {
				if !strings.HasPrefix(got[i], tc.want[i]) || (tc.exact && got[i] != tc.want[i]) {
					t.Fatalf("entry %d = %q, want it to start with %q (exact: %v)", i, got[i], tc.want[i], tc.exact)
				}
				if tc.absent != "" && strings.Contains(got[i], tc.absent) {
					t.Fatalf("entry %d = %q gives the advice %q, which is for a different failure", i, got[i], tc.absent)
				}
			}
			// Missing and Degraded answer one question two ways, and the boot
			// line relies on them agreeing: a DEGRADED line with nothing to
			// name, or a missing layer under an INFO line, is the bug.
			for _, mode := range []Mode{ModeBestEffort, ModeEnforce} {
				if degraded, n := tc.report.Degraded(mode), len(tc.report.Missing(mode)); degraded != (n > 0) {
					t.Fatalf("%s: Degraded = %v but Missing has %d entries", mode, degraded, n)
				}
			}
			// Off is the operator's choice, not a gap, whatever the kernel lacks.
			if off := tc.report.Missing(ModeOff); off != nil {
				t.Fatalf("Missing(off) = %q, want nil", off)
			}
		})
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
