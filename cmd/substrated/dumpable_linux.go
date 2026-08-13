//go:build linux

package main

import (
	"log/slog"

	"golang.org/x/sys/unix"
)

// hideProcess clears the dumpable flag on the substrate itself, which makes
// the kernel reassign /proc/<pid>/{environ,mem,maps,fd} to root:root 0400 and
// gate them behind a ptrace access check.
//
// It is three lines and it closes the same hole the function sandbox's
// Landlock layer does, from the other side: a body that somehow starts
// unconfined — an old kernel, SUBSTRATE_SANDBOX=off, a future exec path that
// forgets to go through the runner — still cannot read the credential key out
// of this process's environment, because a same-uid process fails the ptrace
// check, and so does root inside a container, which does not carry
// CAP_SYS_PTRACE in the default capability set.
//
// The flag is cleared on execve for an ordinary binary, so it protects THIS
// process and does not follow children — which is right: the children are the
// untrusted half.
func hideProcess() {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		slog.Warn("could not clear the dumpable flag: this process's /proc entry stays readable to same-uid processes", "error", err)
	}
}
