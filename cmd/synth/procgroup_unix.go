//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts cmd as the leader of a process group of its own, so
// a signal to the loader's group (a terminal's Ctrl-C) does not reach it.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
