//go:build linux

package sandbox

import (
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The probe is the only place that sees the errno, and the errno is what tells
// two fixes apart: a kernel built without Landlock needs a rebuild, one that
// left it out of lsm= needs a boot parameter. Each reason is pinned to the fix
// it names, because the boot line prints it verbatim.
func TestProbeReasonsNameTheirFix(t *testing.T) {
	for _, tc := range []struct {
		name, got, prefix, fix string
	}{
		{"landlock not built", landlockUnavailable(unix.ENOSYS), "not supported by this kernel", "CONFIG_SECURITY_LANDLOCK=y"},
		{"landlock not in lsm=", landlockUnavailable(unix.EOPNOTSUPP), "built in but disabled", "lsm= list"},
		{"landlock other errno", landlockUnavailable(unix.EPERM), "landlock_create_ruleset: operation not permitted", ""},
		{"seccomp not built", seccompUnavailable(unix.ENOSYS), "seccomp(2): function not implemented", "CONFIG_SECCOMP_FILTER=y"},
		{"seccomp denied", seccompUnavailable(unix.EPERM), "seccomp(2): operation not permitted", "seccomp profile"},
		{"seccomp denied with EACCES", seccompUnavailable(unix.EACCES), "seccomp(2): permission denied", "seccomp profile"},
		{"seccomp other errno", seccompUnavailable(unix.EFAULT), "seccomp(2): bad address", ""},
		{"no syscall table", noSyscallTable("arm"), "no syscall table for arm in this build", "linux/arm64 build"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasPrefix(tc.got, tc.prefix) {
				t.Fatalf("reason = %q, want it to start with %q", tc.got, tc.prefix)
			}
			if !strings.Contains(tc.got, tc.fix) {
				t.Fatalf("reason = %q, want it to name the fix %q", tc.got, tc.fix)
			}
		})
	}
}

// The real probe on this host: a layer is either there or has a reason, never
// both and never neither, so the DEGRADED line can always say why.
func TestProbeRecordsAReasonForEveryMissingLayer(t *testing.T) {
	r := probe()
	if (r.LandlockABI == 0) != (r.LandlockErr != "") {
		t.Fatalf("landlock ABI %d with reason %q", r.LandlockABI, r.LandlockErr)
	}
	if r.Seccomp != (r.SeccompErr == "") {
		t.Fatalf("seccomp %v with reason %q", r.Seccomp, r.SeccompErr)
	}
}
