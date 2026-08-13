//go:build !linux

package sandbox

import "os/exec"

// Off Linux there is nothing to apply: Landlock and seccomp are Linux
// facilities, and macOS's Seatbelt is a different design that would need its
// own policy language. A laptop running `mise run dev` therefore runs bodies
// exactly as it did before, and says so at boot rather than implying a
// confinement it does not have.

// New returns a confiner that applies nothing and reports why.
func New(mode Mode) *Confiner {
	return &Confiner{mode: mode}
}

func (c *Confiner) wrap(*exec.Cmd, Policy) error { return nil }
