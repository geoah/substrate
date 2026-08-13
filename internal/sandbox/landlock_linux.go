//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Landlock: the filesystem half, called directly rather than through
// github.com/landlock-lsm/go-landlock. That library is good and current, but it
// carries kernel.org/pub/linux/libs/security/libcap/psx to apply a domain
// across every OS thread: machinery this stub does not need, because it locks
// one thread and exec's immediately, and a dependency that compiles a C
// implementation whenever cgo is on. The whole surface used here is three
// syscalls and two structs.
//
// The ruleset is a DENY-BY-DEFAULT list: every access right the kernel is asked
// to handle is refused everywhere except the paths granted below. That is the
// property that closes /proc (no rule names it, so /proc/1/environ cannot be
// opened) and the shared build and uv caches (readable and executable, never
// writable, so one installation cannot plant an artifact another will exec).

// The ABI 1 filesystem access rights, plus the two later ones worth handling.
const (
	llExecute = 1 << iota
	llWriteFile
	llReadFile
	llReadDir
	llRemoveDir
	llRemoveFile
	llMakeChar
	llMakeDir
	llMakeReg
	llMakeSock
	llMakeFifo
	llMakeBlock
	llMakeSym
	llRefer    // ABI 2 (Linux 5.19)
	llTruncate // ABI 3 (Linux 6.2)
	// LANDLOCK_ACCESS_FS_IOCTL_DEV (ABI 5) is deliberately NOT handled:
	// handling a right obliges us to grant it everywhere it is legitimately
	// used, and an unforeseen ioctl on a device file (a tty, a terminal size
	// query in a body's logging) would fail for no security gain here.
)

// llCreateRulesetVersion asks landlock_create_ruleset for the ABI version
// instead of a ruleset.
const llCreateRulesetVersion = 1

// rulesetAttr is struct landlock_ruleset_attr. Only handled_access_fs is
// passed (size 8, the ABI 1 struct): the kernel accepts a short struct and
// zero-fills the rest, and neither the network nor the scope fields are used,
// Landlock's network rules are TCP PORT rules, useless against connectors that
// all speak 443, and scoping is ABI 6.
type rulesetAttr struct {
	HandledAccessFS uint64
}

// pathBeneathAttr is struct landlock_path_beneath_attr, which is PACKED: the
// kernel copies exactly 12 bytes (a u64 then an s32), so the trailing pad
// below is never read.
type pathBeneathAttr struct {
	AllowedAccess uint64
	ParentFD      int32
	_             int32
}

// landlockABI returns the kernel's Landlock ABI version. ENOSYS means the
// kernel was built without it; EOPNOTSUPP means it was built in but left out of
// the boot-time LSM list: two different operator problems, so both are named.
func landlockABI() (int, error) {
	ret, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, llCreateRulesetVersion)
	switch {
	case errno == 0:
		return int(ret), nil
	case errors.Is(errno, unix.ENOSYS):
		return 0, fmt.Errorf("landlock: not supported by this kernel")
	case errors.Is(errno, unix.EOPNOTSUPP):
		return 0, fmt.Errorf("landlock: built in but disabled: add it to the kernel's lsm= list")
	default:
		return 0, fmt.Errorf("landlock: %w", errno)
	}
}

// handledFor is the access-right set to hand the kernel at a given ABI. Asking
// for a right the running kernel does not know is EINVAL, so the set is
// downgraded rather than assumed.
func handledFor(abi int) uint64 {
	handled := uint64(llExecute | llWriteFile | llReadFile | llReadDir |
		llRemoveDir | llRemoveFile | llMakeChar | llMakeDir | llMakeReg |
		llMakeSock | llMakeFifo | llMakeBlock | llMakeSym)
	if abi >= 2 {
		// REFER must be HANDLED to be grantable at all: at ABI 2 and above a
		// rename or link across directories is denied outright unless the
		// ruleset handles REFER and both sides allow it. Not handling it would
		// mean a body could never rename a file within its own work dir.
		handled |= llRefer
	}
	if abi >= 3 {
		handled |= llTruncate
	}
	return handled
}

// applyLandlock builds and enforces the ruleset for one policy. A policy with
// no paths at all still enforces: the domain then denies every handled right
// everywhere, which is the correct answer for a body that needs nothing.
func applyLandlock(p Policy) error {
	// NOTE the absence of an early return for a policy with no paths. A policy
	// that grants nothing means a body that may touch nothing, and enforcing it
	// is one ruleset with zero rules, whereas skipping would hand a
	// mis-assembled empty policy the whole filesystem, including the /proc entry
	// this layer exists to close. Fail closed.
	abi, err := landlockABI()
	if err != nil {
		// best-effort: the parent already reported the degradation at boot,
		// and refusing here would take down every delivery on a kernel the
		// operator was told about. Under enforce it stops the body instead.
		if p.Require {
			return err
		}
		return nil
	}
	handled := handledFor(abi)
	attr := rulesetAttr{HandledAccessFS: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("create ruleset: %w", errno)
	}
	defer func() { _ = unix.Close(int(fd)) }()

	ro := uint64(llReadFile|llReadDir) & handled
	rx := ro | llExecute
	rw := handled // everything handled, on the paths the body owns

	for _, grant := range []struct {
		paths  []string
		access uint64
	}{
		{p.ReadExec, rx},
		{p.ReadOnly, ro},
		{p.ReadWrite, rw},
	} {
		for _, path := range grant.paths {
			if err := addPath(int(fd), path, grant.access); err != nil {
				return err
			}
		}
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("restrict self: %w", errno)
	}
	return nil
}

// addPath grants one prefix. A missing path is skipped, not an error: the
// interpreter and toolchain prefixes differ between the alpine image, a
// Debian dev box and a macOS-hosted container, and a policy that listed them
// all would otherwise refuse to launch on every one of them.
func addPath(rulesetFD int, path string, access uint64) error {
	pathFD, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = unix.Close(pathFD) }()
	// A rule on a NON-DIRECTORY may only carry the rights that mean something
	// for a file; naming a directory-only right (READ_DIR, any MAKE_* or
	// REMOVE_*, REFER) on one is EINVAL, not a no-op. Policies grant
	// individual device nodes (/dev/null and friends) alongside directories, so
	// the mask is applied here rather than left to every caller.
	if fi, statErr := statFD(pathFD); statErr == nil && fi&unix.S_IFMT != unix.S_IFDIR {
		access &= llExecute | llWriteFile | llReadFile | llTruncate
	}
	rule := pathBeneathAttr{AllowedAccess: access, ParentFD: int32(pathFD)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(rulesetFD),
		unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("add rule %s: %w", path, errno)
	}
	return nil
}

// statFD returns the mode of whatever a descriptor names. The descriptor is an
// O_PATH one, which fstat answers for even though almost nothing else does.
func statFD(fd int) (uint32, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return 0, err
	}
	return st.Mode, nil
}
