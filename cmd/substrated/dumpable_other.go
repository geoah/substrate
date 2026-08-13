//go:build !linux

package main

// hideProcess is a Linux facility; elsewhere the substrate's own /proc entry
// is not the thing to worry about, because there is no function sandbox either
// (internal/sandbox says so at boot).
func hideProcess() {}
