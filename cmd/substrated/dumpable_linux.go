//go:build linux

package main

import (
	"log/slog"

	"golang.org/x/sys/unix"
)

// hideProcess clears the dumpable flag on the substrate itself. A non-dumpable
// process has the SENSITIVE entries under its /proc directory (environ, mem,
// maps, fd and their neighbors) reassigned to root and gated behind the
// kernel's ptrace access check; the directory itself and the harmless entries
// stay readable.
//
// It is three lines and it closes the same hole the function sandbox's
// Landlock layer does, from the other side: a body that somehow starts
// unconfined (an old kernel, SUBSTRATE_SANDBOX=off, a future exec path that
// forgets to go through the runner) still cannot read the credential key out
// of this process's environment, because a same-uid process fails the ptrace
// check, and so does root inside a container, which does not carry
// CAP_SYS_PTRACE in the default capability set.
//
// Dumpability RESETS to 1 on execve for an ordinary binary (a privileged
// execution instead follows /proc/sys/fs/suid_dumpable), so this protects THIS
// process and does not follow children, which is right: the children are the
// untrusted half, and what confines them is internal/sandbox.
//
// A failure is logged, not fatal. Setting the flag to 0 is permitted for any
// process, so a failure here means something very unusual, and the primary
// boundary is the sandbox, which has its own SUBSTRATE_SANDBOX=enforce for
// operators who want a refusal rather than a warning. Taking the substrate down
// over a defense-in-depth prctl would trade a small exposure for a total one.
func hideProcess() {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		slog.Error("could not clear the dumpable flag: this process's /proc entry stays readable to same-uid processes", "error", err)
	}
}
