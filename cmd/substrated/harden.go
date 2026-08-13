package main

import (
	"log/slog"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/sandbox"
)

// reportSandbox says, once and where an operator will see it, what the
// function sandbox is actually doing. A confinement that silently degraded on
// a kernel without Landlock would be worse than none: it would be believed.
func reportSandbox() {
	c := runner.Shared.Sandbox()
	switch {
	case c.Mode() == sandbox.ModeOff:
		slog.Warn("function sandbox OFF: bodies run unconfined — they can read this process's environment and reach the network regardless of what they declare",
			"mode", string(c.Mode()))
	case c.Degraded():
		slog.Error("function sandbox DEGRADED: this kernel does not offer every layer, and bodies run with less confinement than the mode implies — set SUBSTRATE_SANDBOX=enforce to refuse instead",
			"mode", string(c.Mode()), "kernel", c.Report().String())
	default:
		slog.Info("function sandbox active", "mode", string(c.Mode()), "kernel", c.Report().String())
	}
}
