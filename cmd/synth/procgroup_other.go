//go:build !unix

package main

import "os/exec"

// ownProcessGroup leaves cmd in the loader's group where the platform has no
// process groups.
func ownProcessGroup(*exec.Cmd) {}
