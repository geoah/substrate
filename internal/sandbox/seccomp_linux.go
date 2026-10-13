//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// seccomp: the syscall half, a hand-assembled classic BPF program.
//
// Hand-assembled rather than github.com/elastic/go-seccomp-bpf, which is pure
// Go, current, and does support argument conditions. It is not used because
// hardcodes EPERM as its errno, and this filter needs two others to be
// CORRECT, not merely tidy:
//
//   - clone3 must answer ENOSYS. glibc ≥2.34 calls clone3 for pthread_create
//     and falls back to clone ONLY on ENOSYS; EPERM would break every threaded
//     body on a glibc image. It must be denied at all, rather than left alone,
//     because clone3 passes its flags behind a POINTER: seccomp cannot read
//     them, so "deny clone with namespace flags" is trivially bypassed through
//     clone3 unless clone3 is gone.
//   - socket(2) must answer EAFNOSUPPORT, the error a program gets for an
//     address family the kernel lacks. A body that is denied the network should
//     see the same thing it would see on a host without IPv6, not a permission
//     error it may retry forever.
//
// SHAPE. A deny-list, not an allowlist, and this is a deliberate limit on what
// the layer claims. An allowlist over CPython and uv would be
// a standing breakage risk for third-party bodies: every wheel with a new
// syscall is an outage, and it would not be the boundary anyway: Landlock is.
// This filter removes the classes with no legitimate use in a function body and
// enforces the network capability. It is defense in depth, and the comments do
// not pretend otherwise.
const (
	seccompSetModeFilter   = 1
	seccompGetActionAvail  = 2
	seccompFilterFlagTSync = 1

	seccompRetAllow = 0x7fff0000
	seccompRetErrno = 0x00050000

	// Offsets into struct seccomp_data. An argument's low half, on the
	// little-endian targets this filter is written for.
	sdNR   = 0
	sdArch = 4
	sdArg0 = 16
	sdArg1 = 24

	// The x32 ABI shares AUDIT_ARCH_X86_64 but sets this bit in the syscall
	// number, so its numbering is a different table entirely. Refusing it
	// keeps a body from reaching an allowed syscall under a number this
	// filter compares against the wrong table.
	x32Bit = 0x40000000
)

// The classic-BPF opcodes used here.
const (
	bpfLdAbsW = 0x20 // BPF_LD | BPF_W | BPF_ABS
	bpfJeqK   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
	bpfJgeK   = 0x35 // BPF_JMP | BPF_JGE | BPF_K
	bpfRetK   = 0x06 // BPF_RET | BPF_K
)

type sockFilter struct {
	Code uint16
	JT   uint8
	JF   uint8
	K    uint32
}

// sockFprog is struct sock_fprog. The pad matches the 64-bit ABI's alignment
// of the pointer that follows the length.
type sockFprog struct {
	Len uint16
	_   [6]byte
	Fil *sockFilter
}

// seccompAvailable reports whether an unprivileged filter can be installed,
// probed WITHOUT attaching one, because a filter cannot be removed, and the
// reason when it cannot. EPERM here means an outer profile denies seccomp(2)
// entirely; EINVAL means the kernel predates SECCOMP_GET_ACTION_AVAIL (4.14)
// but still has filters, so it counts as available.
//
// An architecture this package has no syscall table for counts as UNAVAILABLE
// however willing the kernel is: a filter is written against one numbering, and
// a wrong one does not fail loudly, it denies and permits the wrong calls. The
// report has to say so, or SUBSTRATE_SANDBOX=enforce would pass its check at
// boot and every body would then fail to launch.
func seccompAvailable() (bool, string) {
	if auditArch == 0 {
		return false, noSyscallTable(runtime.GOARCH)
	}
	action := uint32(seccompRetErrno)
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, seccompGetActionAvail, 0,
		uintptr(unsafe.Pointer(&action)))
	switch {
	case errno == 0:
		return true, ""
	case errors.Is(errno, unix.EINVAL):
		return true, ""
	default:
		return false, seccompUnavailable(errno)
	}
}

// noSyscallTable is the reason for a build whose architecture has no syscall
// table here (arch_linux_other.go). The fix is a build, not a kernel: the
// kernel may well offer seccomp, and the filter still cannot be written.
func noSyscallTable(arch string) string {
	return "no syscall table for " + arch + " in this build: run the linux/amd64 or linux/arm64 build"
}

// seccompUnavailable is the reason, and the fix, for an errno from the
// availability probe. ENOSYS is a kernel built without seccomp; EPERM and
// EACCES are an outer seccomp filter refusing the call: a container's profile
// (the Docker and containerd defaults permit it) or a unit's SystemCallFilter=.
func seccompUnavailable(errno unix.Errno) string {
	switch {
	case errors.Is(errno, unix.ENOSYS):
		return fmt.Sprintf("seccomp(2): %v: build the kernel with CONFIG_SECCOMP_FILTER=y", errno)
	case errors.Is(errno, unix.EPERM), errors.Is(errno, unix.EACCES):
		return fmt.Sprintf("seccomp(2): %v: allow seccomp(2) in the seccomp profile this process runs under", errno)
	default:
		return fmt.Sprintf("seccomp(2): %v", errno)
	}
}

// applySeccomp installs the filter for one policy. TSYNC is requested so every
// thread of this process carries it; the stub is single-threaded by the time it
// gets here, but a kernel that cannot honor TSYNC tells us so rather than
// leaving a thread uncovered. parent is the substrate, which the filter
// refuses to signal.
func applySeccomp(p Policy, parent substrateProc) error {
	if auditArch == 0 {
		// No syscall table for this architecture. Reported as a degradation at
		// boot (seccompAvailable says so), so carrying on here is the
		// best-effort contract rather than a silent gap, and refusing would
		// take down every delivery on a platform the operator was told about.
		if p.Require {
			return fmt.Errorf("no syscall table for this architecture")
		}
		return nil
	}
	prog, err := buildFilter(p, parent)
	if err != nil {
		return err
	}
	fprog := sockFprog{Len: uint16(len(prog)), Fil: &prog[0]}
	err = installFilter(&fprog, seccompFilterFlagTSync)
	if errors.Is(err, unix.EINVAL) {
		// TSYNC is a request, not a requirement: a kernel that has filters but
		// not the flag (or refuses to sync because a sibling thread carries a
		// different filter) still applies one to the calling thread, which is
		// the thread the stub locked and is about to exec from, so the filter
		// still reaches the body.
		err = installFilter(&fprog, 0)
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM) {
		// The boot probe cannot see this coming: an outer policy may allow
		// SECCOMP_GET_ACTION_AVAIL and still refuse SECCOMP_SET_MODE_FILTER.
		// Under enforce that has to stop the body; otherwise it is the
		// degradation the operator was warned about at boot.
		if p.Require {
			return fmt.Errorf("set filter: %w (SUBSTRATE_SANDBOX=enforce)", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("set filter: %w", err)
	}
	return nil
}

// installFilter attaches one assembled program.
//
// The RETURN VALUE matters as much as the errno. With TSYNC, a thread that
// cannot be synchronized is reported as a POSITIVE return carrying that
// thread's id, with errno zero: checking only the errno would read a partial
// installation as a success and let the body run with a thread outside the
// filter. Nothing but 0 is success.
func installFilter(fprog *sockFprog, flags uintptr) error {
	ret, _, errno := unix.Syscall(unix.SYS_SECCOMP, seccompSetModeFilter,
		flags, uintptr(unsafe.Pointer(fprog)))
	if errno != 0 {
		return errno
	}
	if ret != 0 {
		return fmt.Errorf("thread %d could not be synchronized onto the filter", ret)
	}
	return nil
}

// errno returns the SECCOMP_RET_ERRNO action carrying one errno.
func retErrno(e unix.Errno) uint32 { return seccompRetErrno | (uint32(e) & 0x0000ffff) }

// buildFilter assembles the program. parent is the substrate, read by the
// stub before the filter installs: every body process descends from the stub,
// so the values are right for a body's own children too.
//
// Layout, so the relative jumps below are readable: a prologue that pins the
// architecture, one compare per denied syscall, one compare per syscall with
// an argument rule, the socket-domain block, one argument block per rule,
// then the three terminal returns. Every compare jumps FORWARD, so no offset
// ever exceeds the number of remaining instructions, which stays far below
// BPF's 255-offset and 4096-instruction ceilings.
func buildFilter(p Policy, parent substrateProc) ([]sockFilter, error) {
	if auditArch == 0 {
		return nil, fmt.Errorf("seccomp: no audit arch for this platform")
	}
	deny := deniedSyscalls()
	signals := signalRules(parent)

	// The socket domains a body may open AT ALL. An allowlist, not a deny-list:
	// refusing AF_INET and AF_INET6 while letting everything else through would
	// leave AF_PACKET, and the container's default capability set carries
	// CAP_NET_RAW, so a body could send and receive raw Ethernet frames and the
	// network capability would bound nothing. AF_UNIX is always allowed (it
	// addresses nothing off the machine); the internet families only when the
	// manifest declared egress, and AF_NETLINK with them because glibc's
	// resolver enumerates interfaces through it.
	allowedDomains := []uint32{unix.AF_UNIX}
	if p.Network {
		allowedDomains = append(allowedDomains, unix.AF_INET, unix.AF_INET6, unix.AF_NETLINK)
	}
	// jeq socket, ld arg0, one compare per allowed domain, then the refusal.
	socketBlock := 3 + len(allowedDomains)
	// Per rule: load the argument, one compare per refused value, then allow.
	signalBlocks := 0
	for _, s := range signals {
		signalBlocks += 2 + len(s.targets)
	}
	// Absolute indices of the blocks and the terminals.
	const prologue = 5
	dispatchAt := prologue + len(deny)
	socketAt := dispatchAt + len(signals)
	blocksAt := socketAt + socketBlock
	allowAt := blocksAt + signalBlocks
	epermAt := allowAt + 1
	enosysAt := allowAt + 2
	lastAt := enosysAt

	// jumpTo is the forward offset from the instruction at `from` to `to`.
	jumpTo := func(from, to int) (uint8, error) {
		d := to - from - 1
		if d < 0 || d > 255 {
			return 0, fmt.Errorf("seccomp: jump offset %d out of range", d)
		}
		return uint8(d), nil
	}

	prog := make([]sockFilter, 0, lastAt+1)
	// 0: load the architecture.
	prog = append(prog, sockFilter{Code: bpfLdAbsW, K: sdArch})
	// 1: a foreign architecture skips to the ENOSYS terminal: this filter's
	// syscall numbers mean nothing there.
	jt, err := jumpTo(1, enosysAt)
	if err != nil {
		return nil, err
	}
	prog = append(prog, sockFilter{Code: bpfJeqK, JT: 0, JF: jt, K: auditArch})
	// 2: load the syscall number.
	prog = append(prog, sockFilter{Code: bpfLdAbsW, K: sdNR})
	// 3: the x32 numbering space, same audit arch, different table.
	if jt, err = jumpTo(3, enosysAt); err != nil {
		return nil, err
	}
	prog = append(prog, sockFilter{Code: bpfJgeK, JT: jt, JF: 0, K: x32Bit})
	// 4: reload the number: the jge above left A holding it, but keeping the
	// prologue a fixed five instructions is worth one redundant load.
	prog = append(prog, sockFilter{Code: bpfLdAbsW, K: sdNR})

	for i, d := range deny {
		at := prologue + i
		target := epermAt
		if d.enosys {
			target = enosysAt
		}
		if jt, err = jumpTo(at, target); err != nil {
			return nil, err
		}
		prog = append(prog, sockFilter{Code: bpfJeqK, JT: jt, JF: 0, K: uint32(d.nr)})
	}

	// A still holds the syscall number: none of the compares above touch it.
	// A syscall with an argument rule jumps to its own block, past the socket
	// block; the block's first instruction reloads A.
	block := blocksAt
	for i, s := range signals {
		if jt, err = jumpTo(dispatchAt+i, block); err != nil {
			return nil, err
		}
		prog = append(prog, sockFilter{Code: bpfJeqK, JT: jt, JF: 0, K: uint32(s.nr)})
		block += 2 + len(s.targets)
	}

	base := socketAt
	jf, err := jumpTo(base, allowAt)
	if err != nil {
		return nil, err
	}
	// Not socket(2): allow, and let the terminals handle the rest.
	prog = append(prog, sockFilter{Code: bpfJeqK, JT: 0, JF: jf, K: uint32(sysSocket)})
	// The domain is arg0, an INTEGER copied into seccomp_data by the kernel,
	// not a pointer, so there is nothing for user space to change behind the
	// filter's back. Comparing the low half is exactly right: the kernel
	// truncates the argument to int, so the low half IS the domain.
	prog = append(prog, sockFilter{Code: bpfLdAbsW, K: sdArg0})
	for i, af := range allowedDomains {
		at := base + 2 + i
		if jt, err = jumpTo(at, allowAt); err != nil {
			return nil, err
		}
		prog = append(prog, sockFilter{Code: bpfJeqK, JT: jt, JF: 0, K: af})
	}
	// Every other domain lands here, AF_PACKET among them.
	prog = append(prog, sockFilter{Code: bpfRetK, K: retErrno(unix.EAFNOSUPPORT)})

	// Every argument compared here is a pid, a process group or a thread id,
	// an integer the kernel truncates to a pid_t, so the low half is the whole
	// value, and a body cannot dodge a compare by setting the high half.
	for _, s := range signals {
		prog = append(prog, sockFilter{Code: bpfLdAbsW, K: s.arg})
		for _, target := range s.targets {
			if jt, err = jumpTo(len(prog), epermAt); err != nil {
				return nil, err
			}
			prog = append(prog, sockFilter{Code: bpfJeqK, JT: jt, JF: 0, K: target})
		}
		prog = append(prog, sockFilter{Code: bpfRetK, K: seccompRetAllow})
	}

	prog = append(prog,
		sockFilter{Code: bpfRetK, K: seccompRetAllow},
		sockFilter{Code: bpfRetK, K: retErrno(unix.EPERM)},
		sockFilter{Code: bpfRetK, K: retErrno(unix.ENOSYS)},
	)
	if len(prog) != lastAt+1 {
		return nil, fmt.Errorf("seccomp: assembled %d instructions, expected %d", len(prog), lastAt+1)
	}
	return prog, nil
}

// substrateProc is what the stub learns about the substrate before the filter
// installs, and what the filter then refuses a body to signal.
type substrateProc struct {
	// pid is the substrate's pid, 0 when the stub could not tell.
	pid int
	// pgid is the substrate's process group, which need not be its pid: a
	// substrate started from a shell is in the shell's job, 0 when unknown.
	pgid int
	// shared is whether the body starts in that group. The runner gives a
	// body a group of its own, and a provision step it runs to completion
	// (`uv sync`, whose build backends are third-party code) starts in the
	// substrate's.
	shared bool
}

// readSubstrate reads the stub's parent, the substrate that started it.
func readSubstrate() substrateProc {
	s := substrateProc{pid: os.Getppid()}
	// getpgid(0) is the caller's own group, so a parent outside this pid
	// namespace (reported as 0) must not be asked about.
	if s.pid <= 0 {
		return substrateProc{}
	}
	if pgid, err := unix.Getpgid(s.pid); err == nil {
		s.pgid = pgid
		s.shared = unix.Getpgrp() == pgid
	}
	return s
}

// argRule is one syscall, the argument it reads, and the values of that
// argument the filter refuses.
type argRule struct {
	nr      int
	arg     uint32
	targets []uint32
}

// signalRules are what a body may not signal, given the substrate. A body
// shares a uid and a pid namespace with the substrate, so without these it can
// stop it: SIGTERM is its graceful shutdown and an unhandled SIGHUP ends it.
//
// Refused: the substrate itself, its process group, pid 1 (the substrate in
// its image, or the init that forwards signals to it), -1, which is every
// process this uid may signal, every body included, and moving into the
// substrate's group with setpgid(2), from where kill(0) would reach it. 0, the
// caller's own group, is refused only when that group is the substrate's.
// fcntl(F_SETSIG) is refused on every file: a body can still make the
// substrate a file's owner, and the kernel then signals it on I/O without a
// signal syscall, but only with SIGIO and SIGURG, which the Go runtime
// ignores, rather than a signal of the body's choosing.
// Allowed: the body's own group, the body itself and its threads (musl's
// raise(3) is tkill on its own tid, so tkill is refused by target, never
// outright), and its children. That is also the limit of the layer: another
// body's pid, or one of the substrate's thread ids, is a value no constant
// here can tell from a child's. The thread ids are the substrate's pid plus a
// small offset, and kill(2) on any of them signals the whole substrate, so
// below SignalScopeABI a body can still stop it. Landlock's signal scope
// closes both where the kernel has it.
//
// tgkill and rt_tgsigqueueinfo take the thread group first, and refuse any
// value below 1 themselves; tkill takes a thread id, which is never negative.
func signalRules(s substrateProc) []argRule {
	const everyone = ^uint32(0) // -1
	process := []uint32{everyone, 1}
	thread := []uint32{1}
	if s.pid > 0 {
		process = append(process, uint32(int32(s.pid)), uint32(-int32(s.pid)))
		thread = append(thread, uint32(int32(s.pid)))
	}
	rules := []argRule{
		{nr: unix.SYS_KILL, arg: sdArg0, targets: process},
		{nr: unix.SYS_RT_SIGQUEUEINFO, arg: sdArg0, targets: process},
		{nr: unix.SYS_TGKILL, arg: sdArg0, targets: thread},
		{nr: unix.SYS_RT_TGSIGQUEUEINFO, arg: sdArg0, targets: thread},
		{nr: unix.SYS_TKILL, arg: sdArg0, targets: thread},
		{nr: unix.SYS_FCNTL, arg: sdArg1, targets: []uint32{unix.F_SETSIG}},
	}
	if s.pgid <= 0 {
		return rules
	}
	group := uint32(-int32(s.pgid))
	rules[0].targets = append(rules[0].targets, group)
	rules[1].targets = append(rules[1].targets, group)
	if s.shared {
		rules[0].targets = append(rules[0].targets, 0)
	}
	return append(rules, argRule{nr: unix.SYS_SETPGID, arg: sdArg1, targets: []uint32{uint32(int32(s.pgid))}})
}

// denial is one refused syscall and the errno it answers with.
type denial struct {
	nr     int
	enosys bool
}

// deniedSyscalls is the deny-list: syscall classes a function body has no
// legitimate use for, each one either an escape primitive or a way to reach
// another process.
//
// Numbers come from golang.org/x/sys/unix, which defines them per GOARCH, so
// this list is correct on both image targets without a table of its own. The
// few that exist on only one architecture live in the arch files beside it.
func deniedSyscalls() []denial {
	d := []denial{
		// Reach into another process: the same-uid siblings and the
		// substrate itself are all in this process's pid namespace.
		{nr: unix.SYS_PTRACE},
		{nr: unix.SYS_PROCESS_VM_READV},
		{nr: unix.SYS_PROCESS_VM_WRITEV},
		{nr: unix.SYS_KCMP},
		{nr: unix.SYS_PIDFD_OPEN},
		{nr: unix.SYS_PIDFD_GETFD},
		{nr: unix.SYS_PROCESS_MADVISE},
		// A pidfd names its process by descriptor, so no argument rule can
		// tell the substrate's from a child's. Refused outright, because a
		// body without Landlock's filesystem layer opens /proc/<ppid> and
		// signals the substrate through it. The cost is a body's own
		// children: one started with pidfd_spawn(3) or clone(CLONE_PIDFD) is
		// signaled by its pid instead.
		{nr: unix.SYS_PIDFD_SEND_SIGNAL},
		// Rearrange the filesystem out from under Landlock. The classic mount
		// syscalls and the new mount API both, because the new one is a
		// complete alternative path to the same thing.
		{nr: unix.SYS_MOUNT},
		{nr: unix.SYS_UMOUNT2},
		{nr: unix.SYS_PIVOT_ROOT},
		{nr: unix.SYS_MOUNT_SETATTR},
		{nr: unix.SYS_FSOPEN},
		{nr: unix.SYS_FSCONFIG},
		{nr: unix.SYS_FSMOUNT},
		{nr: unix.SYS_MOVE_MOUNT},
		{nr: unix.SYS_OPEN_TREE},
		// Name a file by handle rather than by path: Landlock is path-based,
		// so a handle is precisely the way around it.
		{nr: unix.SYS_OPEN_BY_HANDLE_AT},
		{nr: unix.SYS_NAME_TO_HANDLE_AT},
		// io_uring performs opens, connects and sends from a RING, not from
		// syscalls, so nothing in this filter: least of all the socket gate,
		// can see them. It has to go entirely or the network capability is
		// decorative.
		{nr: unix.SYS_IO_URING_SETUP},
		{nr: unix.SYS_IO_URING_ENTER},
		{nr: unix.SYS_IO_URING_REGISTER},
		// Load code into the kernel, or read everyone's.
		{nr: unix.SYS_BPF},
		{nr: unix.SYS_PERF_EVENT_OPEN},
		{nr: unix.SYS_INIT_MODULE},
		{nr: unix.SYS_FINIT_MODULE},
		{nr: unix.SYS_DELETE_MODULE},
		{nr: unix.SYS_KEXEC_LOAD},
		{nr: unix.SYS_KEXEC_FILE_LOAD},
		// Namespaces. Denied not because they would succeed: a container's
		// own profile already refuses them, but because a body that could
		// enter one would leave this filter's assumptions behind.
		{nr: unix.SYS_SETNS},
		{nr: unix.SYS_UNSHARE},
		// clone3 hides its flags behind a pointer, so "clone without namespace
		// flags" cannot be expressed for it. ENOSYS, so glibc falls back to
		// clone(2) rather than failing to start a thread.
		{nr: sysClone3, enosys: true},
		// The kernel's key store, where another subsystem's secrets live.
		{nr: unix.SYS_ADD_KEY},
		{nr: unix.SYS_KEYCTL},
		{nr: unix.SYS_REQUEST_KEY},
		// Host-wide state.
		{nr: unix.SYS_SWAPON},
		{nr: unix.SYS_SWAPOFF},
		{nr: unix.SYS_REBOOT},
		{nr: unix.SYS_SETTIMEOFDAY},
		{nr: unix.SYS_CLOCK_SETTIME},
		{nr: unix.SYS_ACCT},
		{nr: unix.SYS_QUOTACTL},
		{nr: unix.SYS_SYSLOG},
		// Handling a page fault in user space is a classic way to widen a
		// TOCTOU window against anything that reads user memory.
		{nr: unix.SYS_USERFAULTFD},
		// Change the execution domain, and with it the syscall table this
		// filter was written against.
		{nr: unix.SYS_PERSONALITY},
	}
	return append(d, archDenied()...)
}
