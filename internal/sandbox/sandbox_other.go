//go:build !linux

package sandbox

import (
	"os/exec"
	"runtime"
)

// Off Linux there is nothing to apply: Landlock and seccomp are Linux
// facilities, and macOS's Seatbelt is a different design that would need its
// own policy language. A laptop running `mise run dev` therefore runs bodies
// exactly as it did before, and says so at boot rather than implying a
// confinement it does not have.

// New returns a confiner that applies nothing and reports why. The report
// carries the platform, so the boot line names it rather than blaming a kernel
// configuration the operator cannot change.
func New(mode Mode) *Confiner {
	return &Confiner{mode: mode, report: Report{OS: runtime.GOOS}}
}

func (c *Confiner) wrap(*exec.Cmd, Policy) error { return nil }
