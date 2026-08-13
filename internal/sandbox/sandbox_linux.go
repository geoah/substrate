//go:build linux

package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/sys/unix"
)

// stubArgv is the sentinel that turns this binary into the confinement stub.
// It is deliberately not a cobra subcommand: the stub has to exist in EVERY
// binary that starts runner children, and `go test ./internal/runner` builds a
// test binary that is not substrated. An init() in this package is carried by
// whatever links it, which is exactly the set that needs it.
const stubArgv = "__substrate_sandbox"

func init() {
	// argv: [self, stubArgv, policy, target, args...]
	if len(os.Args) >= 4 && os.Args[1] == stubArgv {
		stubMain(os.Args[2], os.Args[3:])
	}
}

// stubMain applies the policy to THIS process and exec's the real program. It
// never returns: every failure is fatal, because a stub that carried on after a
// failed restriction would silently launch an unconfined body.
//
// The thread lock is load-bearing. no_new_privs, seccomp(2) without TSYNC and
// landlock_restrict_self all act on the CALLING THREAD, and the Go runtime
// moves goroutines between OS threads at any preemption point, including
// between applying the policy and exec'ing. Locking pins all of it to one
// thread, and execve then keeps that thread as the sole survivor, so the
// policy is what the real program starts with.
func stubMain(encoded string, argv []string) {
	runtime.LockOSThread()

	var p Policy
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	if err != nil {
		dief("policy: %v", err)
	}

	// Anything the parent left open above stderr is marked close-on-exec, so
	// an inherited descriptor cannot survive into the body: a descriptor is a
	// capability neither Landlock nor seccomp can revoke once it is open.
	//
	// CLOEXEC rather than an outright close: closing the runtime's epoll
	// descriptor under a live runtime would crash before the exec that makes it
	// moot.
	markCloexec()

	if err := applyRlimits(p); err != nil {
		dief("rlimits: %v", err)
	}
	// no_new_privs before both: seccomp requires it of an unprivileged caller,
	// Landlock accepts it in place of CAP_SYS_ADMIN, and on its own it stops a
	// body escalating through any setuid binary it can still execute.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		dief("no_new_privs: %v", err)
	}
	// Landlock first: it is the layer that closes /proc/1/environ and the
	// shared caches, and it is applied while the process can still open the
	// paths it needs to name.
	if err := applyLandlock(p); err != nil {
		dief("landlock: %v", err)
	}
	if err := applySeccomp(p); err != nil {
		dief("seccomp: %v", err)
	}

	target, err := exec.LookPath(argv[0])
	if err != nil {
		dief("resolve %s: %v", argv[0], err)
	}
	if err := unix.Exec(target, argv, os.Environ()); err != nil {
		dief("exec %s: %v", target, err)
	}
}

// dief reports a confinement failure on stderr, where the runner's capped
// stderr ring picks it up and surfaces it on the delivery error, and exits.
func dief(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "substrate sandbox: "+format+"\n", args...)
	os.Exit(126)
}

// New probes the kernel once and returns the confiner every child goes
// through.
func New(mode Mode) *Confiner {
	return &Confiner{mode: mode, report: probe()}
}

// probe asks the kernel what it actually supports. Landlock answers its own
// version query; seccomp is probed for REACHABILITY rather than by installing
// a filter, because a filter cannot be uninstalled and the probe runs in the
// substrate's own process. An outer profile that denies seccomp(2), the case
// that matters, answers EPERM to the probe just as it would to the install.
func probe() Report {
	r := Report{OS: runtime.GOOS}
	abi, err := landlockABI()
	if err == nil {
		r.LandlockABI = abi
	}
	r.Seccomp = seccompAvailable()
	return r
}

// wrap rewrites cmd to run through the stub. The policy rides argv rather than
// the environment: the child env is a default-deny allowlist the runner builds
// deliberately, and a sandbox variable added to it would be one more name a
// body reads, and one more the runner has to remember to strip.
func (c *Confiner) wrap(cmd *exec.Cmd, p Policy) error {
	if len(cmd.Args) == 0 {
		return fmt.Errorf("sandbox: command has no argv")
	}
	// Only ask for what the kernel has. In best-effort a missing layer is a
	// logged degradation; Wrap has already refused for enforce.
	if !c.report.FS() {
		p.ReadExec, p.ReadOnly, p.ReadWrite = nil, nil, nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)

	// /proc/self/exe rather than a resolved path: it survives a binary that
	// has been replaced or renamed under a running substrate, and it is what
	// the kernel hands back for the running image.
	self := "/proc/self/exe"
	cmd.Args = append([]string{self, stubArgv, encoded}, cmd.Args...)
	cmd.Path = self
	return nil
}

// applyRlimits sets the ceilings rlimits bound honestly. Every one is set on
// BOTH cur and max so the body cannot raise its own.
func applyRlimits(p Policy) error {
	// A core dump of a body would write the body's memory, including whatever
	// config secrets it was handed, to disk outside its work dir.
	if err := setRlimit(unix.RLIMIT_CORE, 0); err != nil {
		return err
	}
	if p.NoFile > 0 {
		if err := setRlimit(unix.RLIMIT_NOFILE, p.NoFile); err != nil {
			return err
		}
	}
	if p.FileSize > 0 {
		if err := setRlimit(unix.RLIMIT_FSIZE, p.FileSize); err != nil {
			return err
		}
	}
	return nil
}

func setRlimit(which int, v uint64) error {
	lim := unix.Rlimit{Cur: v, Max: v}
	// Never raise: a hard limit the environment already set lower is the
	// operator's, and lowering ours to match keeps the stub from failing on a
	// constrained host.
	var cur unix.Rlimit
	if err := unix.Getrlimit(which, &cur); err == nil {
		if cur.Max < v {
			lim = unix.Rlimit{Cur: cur.Max, Max: cur.Max}
		}
	}
	return unix.Setrlimit(which, &lim)
}

// markCloexec marks every descriptor above stderr close-on-exec.
//
// close_range(2) with CLOSE_RANGE_CLOEXEC does it in one call, but it needs
// Linux 5.11 (below the floor this package otherwise targets), so a failure
// falls back to walking the descriptor table by hand. The fallback is bounded
// by RLIMIT_NOFILE, which is what the kernel would have walked anyway, and
// capped besides: a host with a soft limit in the millions must not turn a
// process start into a million syscalls.
//
// A failure is NOT fatal, and deliberately so. os/exec passes only 0, 1 and 2,
// and the Go runtime opens everything O_CLOEXEC, so there is normally nothing
// here to find; refusing to launch would take the substrate's functions down on
// an old kernel to close a gap that is already closed by other means. This is
// the belt beside those braces.
func markCloexec() {
	if err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err == nil {
		return
	}
	const maxWalk = 4096
	limit := uint64(maxWalk)
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &lim); err == nil && lim.Cur < limit {
		limit = lim.Cur
	}
	for fd := 3; fd < int(limit); fd++ {
		// EBADF is the ordinary answer for a descriptor that is not open, which
		// is most of them.
		_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC)
	}
}
