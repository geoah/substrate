//go:build linux && arm64

package sandbox

import "golang.org/x/sys/unix"

// AUDIT_ARCH_AARCH64.
const auditArch = 0xc00000b7

const (
	sysSocket = unix.SYS_SOCKET
	// See the amd64 file: 435 on every architecture, unexported by x/sys.
	sysClone3 = 435
)

// archDenied is empty on arm64: the I/O-port and LDT syscalls the x86 list
// refuses do not exist here.
func archDenied() []denial { return nil }
