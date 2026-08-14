// Package sandbox confines one runner child (an arbitrary third-party
// function body) to the syscalls and paths its capability envelope entitles
// it to, using only the kernel facilities an unprivileged process can apply to
// ITSELF: Landlock for the filesystem, seccomp-bpf for the syscall surface,
// rlimits for the cheap resource ceilings.
//
// It exists because the runner's env allowlist is NOT a boundary on its own.
// Every body runs as the substrate's own uid in the substrate's own container
// (the image declares no USER, so that uid is root), which means
// `open("/proc/1/environ")` reads the substrate's environment (the credential
// key that unseals every repository's OAuth, the database URL) straight past
// the allowlist that so carefully kept them out of the child's own env.
// Landlock is what closes that, and it is the layer this package is really
// for; the rest is defense in depth around it.
//
// WHY SELF-CONFINEMENT AND NOT A CONTAINER. Both alternatives are worse here.
// Namespaces are unavailable: Docker's default seccomp profile denies
// CLONE_NEWUSER (the clone(2) arg mask 0x7E020000 includes it) and refuses
// unshare(2) without CAP_SYS_ADMIN, and Ubuntu ≥23.10 additionally transitions
// unprivileged user namespaces into an AppArmor profile that cannot write a
// uid_map, so bubblewrap, nsjail and rootless runsc are all out inside a stock
// container. cgroup v2 is unavailable for the same class of reason:
// /sys/fs/cgroup is a read-only mount, so memory.max and pids.max cannot be
// written. Landlock and seccomp are the two things that DO work unprivileged in
// a default container, on musl, as root or not. Measured, not assumed.
//
// HOW THE POLICY IS APPLIED. There is no pre-exec hook in os/exec, and the
// three mechanisms are per-thread, so the policy cannot be applied from a
// goroutine in the parent. Instead the child is started as
//
//	/proc/self/exe <stubArgv> <policy> -- <real argv...>
//
// and the stub (an init() in THIS package, so every binary that links the
// runner carries it, test binaries included) locks its OS thread, applies the
// policy, and exec's the real program. no_new_privs, the seccomp filter and the
// Landlock domain all survive execve and are inherited by every descendant, so
// a body that spawns a helper inherits the same jail.
//
// WHAT IT DOES NOT DO. It is not a container, and the comments here say so
// rather than implying otherwise. A confined body still shares a pid namespace
// and a uid with the substrate, so it can signal it (Landlock scopes signals
// only at ABI 6, which the current deployment kernels do not all reach); it has
// no memory or pid ceiling beyond rlimits, because cgroups are unavailable; and
// a body GRANTED network reaches loopback, so it can talk to the substrate's
// own HTTP port. Those are named residual risks, not oversights.
package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

// Mode is how hard the sandbox insists.
type Mode string

const (
	// ModeOff applies nothing. The only honest setting on a platform with no
	// Landlock and no seccomp, and the escape hatch when a kernel surprises us.
	ModeOff Mode = "off"
	// ModeBestEffort applies every layer the kernel actually supports and
	// carries on, loudly, without the rest. The default, because refusing to
	// boot on an old kernel is a worse failure than a logged degradation.
	ModeBestEffort Mode = "best-effort"
	// ModeEnforce refuses to start a body at all unless the filesystem and
	// syscall layers both applied. The setting a real deployment wants.
	ModeEnforce Mode = "enforce"
)

// ParseMode reads the SUBSTRATE_SANDBOX setting; an empty value is the default.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.TrimSpace(s)) {
	case "":
		return ModeBestEffort, nil
	case ModeOff:
		return ModeOff, nil
	case ModeBestEffort:
		return ModeBestEffort, nil
	case ModeEnforce:
		return ModeEnforce, nil
	default:
		return "", fmt.Errorf("sandbox: unknown mode %q: want off, best-effort or enforce", s)
	}
}

// Policy is one child's confinement, derived from its installation identity
// and its capability envelope. Paths are absolute; a path that does not exist
// is skipped rather than failing the launch, because the interpreter and
// toolchain prefixes differ across images.
type Policy struct {
	// ReadExec are the prefixes the child may read and execute: the
	// interpreter, its standard library, the shared libraries it links, the
	// certificate store, the Go toolchain. Read-only: a body cannot rewrite
	// its own interpreter.
	ReadExec []string `json:"readExec,omitempty"`
	// ReadOnly are prefixes the child may read but not execute.
	ReadOnly []string `json:"readOnly,omitempty"`
	// ReadWrite are the prefixes the child owns: ITS installation's work dir
	// and its private scratch. Never a shared cache: a writable shared cache
	// is a cross-installation code-execution vector, which is most of the
	// reason this package exists.
	ReadWrite []string `json:"readWrite,omitempty"`
	// Network grants AF_INET/AF_INET6 sockets. False denies them at the
	// syscall, which is how `permissions.network` stops being decorative.
	// It is a binary gate: per-host allowlisting needs an egress proxy, not a
	// syscall filter, because a filter cannot read the sockaddr behind a
	// pointer.
	Network bool `json:"network,omitempty"`
	// NoFile, FileSize bound the cheap things rlimits actually bound well.
	// RLIMIT_AS and RLIMIT_NPROC are deliberately absent: AS counts virtual
	// address space, which the Go runtime and CPython reserve far more of than
	// they commit, and NPROC is per-UID: a child's NPROC limit is charged
	// against the SUBSTRATE's process count, so it would starve the server
	// rather than isolate the body.
	NoFile   uint64 `json:"noFile,omitempty"`
	FileSize uint64 `json:"fileSize,omitempty"`
	// Require makes every layer mandatory IN THE CHILD. The boot probe cannot
	// settle this on its own: it asks whether seccomp(2) is reachable, and an
	// outer policy is free to answer that and still refuse
	// SECCOMP_SET_MODE_FILTER when the filter is actually installed. Without
	// this flag the stub would swallow that refusal and exec the body
	// unconfined under SUBSTRATE_SANDBOX=enforce, which is the one mode that
	// exists to make that impossible.
	Require bool `json:"require,omitempty"`
}

// Report is what the platform actually offered, resolved once at boot.
type Report struct {
	// OS is the platform the probe ran on, so a caller can tell "this kernel
	// is missing a layer" from "this operating system has none of them",
	// two different problems with two different answers, and only the first
	// one an operator can fix.
	OS string
	// LandlockABI is the kernel's Landlock ABI version, 0 when unavailable.
	LandlockABI int
	// Seccomp reports whether an unprivileged filter installs.
	Seccomp bool
	// Err carries the probe failure, if the probe itself could not run.
	Err error
}

// MinLandlockABI is the ABI at which the filesystem layer is COMPLETE enough
// to be called enforced.
//
// ABI 3 (Linux 6.2) is where LANDLOCK_ACCESS_FS_TRUNCATE arrives. Below it,
// truncate(2) is mediated by nothing at all: a root body outside its granted
// trees cannot read or write a file, but can still empty one. Reporting ABI 1
// or 2 as "enforced" would let SUBSTRATE_SANDBOX=enforce pass its check on a
// kernel where every file on the host is still destructible.
const MinLandlockABI = 3

// FS reports whether the filesystem layer is available AND complete. A kernel
// below MinLandlockABI still gets a ruleset applied (some confinement beats
// none), but it does not satisfy enforce and the boot line calls it degraded.
func (r Report) FS() bool { return r.LandlockABI >= MinLandlockABI }

// Supported reports whether this package can confine anything at all here.
// Landlock and seccomp are Linux facilities; macOS's Seatbelt is a different
// design with its own policy language, and nothing stands in for them.
func (r Report) Supported() bool { return r.OS == "linux" }

// Degraded reports whether a mode asks for more than this platform gave. It
// hangs off Report rather than Confiner alone so a caller can ask the question
// of a report it constructed, which is how the boot line's
// unsupported-platform branch is tested from a machine that supports the
// sandbox.
func (r Report) Degraded(mode Mode) bool {
	if mode == ModeOff {
		return false
	}
	return !r.FS() || !r.Seccomp
}

// String is the one line an operator reads at boot.
func (r Report) String() string {
	if !r.Supported() {
		return r.OS + " has no landlock and no seccomp: function bodies cannot be confined on this platform"
	}
	fs := "unavailable"
	if r.LandlockABI > 0 {
		fs = fmt.Sprintf("landlock ABI v%d", r.LandlockABI)
		if r.LandlockABI < MinLandlockABI {
			fs += fmt.Sprintf(" (below v%d: truncate(2) is not mediated)", MinLandlockABI)
		}
	}
	sec := "unavailable"
	if r.Seccomp {
		sec = "available"
	}
	return fmt.Sprintf("filesystem: %s, syscall filter: %s", fs, sec)
}

// Confiner applies a policy to children. Build it once with New.
type Confiner struct {
	mode   Mode
	report Report
}

// Mode is the configured mode.
func (c *Confiner) Mode() Mode { return c.mode }

// Report is what the kernel offered.
func (c *Confiner) Report() Report { return c.report }

// Degraded reports whether the configured mode is doing less than it says: the
// state an operator must be told about rather than left to infer.
func (c *Confiner) Degraded() bool { return c.report.Degraded(c.mode) }

// Wrap rewrites cmd to run under the policy. It never mutates cmd on failure,
// so a caller that ignores the error still launches an unconfined child rather
// than a broken one, which is why ModeEnforce returns the error instead of
// relying on the caller to notice.
func (c *Confiner) Wrap(cmd *exec.Cmd, p Policy) error {
	if c.mode == ModeOff {
		return nil
	}
	if c.mode == ModeEnforce && (!c.report.FS() || !c.report.Seccomp) {
		return fmt.Errorf("sandbox: SUBSTRATE_SANDBOX=enforce, but %s: "+
			"set SUBSTRATE_SANDBOX=best-effort to run bodies unconfined anyway", c.report)
	}
	return c.wrap(cmd, p)
}
