//go:build linux && amd64

package sandbox

import "golang.org/x/sys/unix"

// AUDIT_ARCH_X86_64. The filter pins this so a syscall arriving under any
// other ABI's numbering never matches a rule written for this one.
const auditArch = 0xc000003e

const (
	sysSocket = unix.SYS_SOCKET
	// clone3 is 435 on every architecture: the syscall numbers from 424
	// onwards were deliberately aligned across all of them, but
	// golang.org/x/sys does not export it, so it is named here rather than
	// left as a bare number in the deny-list.
	sysClone3 = 435
)

// archDenied is the x86-only half of the deny-list: direct I/O-port access,
// and the local descriptor table, neither of which exists on arm64.
func archDenied() []denial {
	return []denial{
		{nr: unix.SYS_IOPERM}, {nr: unix.SYS_IOPL}, {nr: unix.SYS_MODIFY_LDT},
	}
}
