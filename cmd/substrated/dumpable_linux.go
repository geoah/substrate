//go:build linux

package main

import (
	"fmt"

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
// A failure is FATAL. Setting the flag to 0 is permitted for every process, so
// a failure here is not a limitation to work around: it means the kernel is not
// behaving as this program assumes, and the assumption in question is the one
// keeping the credential key out of a same-uid reader. Booting anyway would
// leave the environment exposed for the process's whole life with nothing but a
// log line to show for it.
func hideProcess() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("clear the dumpable flag: %w (this process's /proc entry would stay readable to same-uid processes)", err)
	}
	return nil
}
