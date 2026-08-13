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
	report := c.Report()
	switch {
	case c.Mode() == sandbox.ModeOff:
		slog.Warn("function sandbox OFF: bodies run unconfined — they can reach the network regardless of what they declare",
			"mode", string(c.Mode()))
	case !report.Supported():
		// A developer laptop, almost always. Saying "degraded" and pointing at
		// SUBSTRATE_SANDBOX=enforce would be bad advice twice over: there is no
		// kernel setting to fix, and enforce here refuses to run any function
		// at all. WARN and not ERROR, because running the substrate on this
		// platform is a development choice, not a broken deployment.
		slog.Warn("function sandbox unavailable on this platform: bodies run unconfined — deploy on Linux for a confined runner",
			"mode", string(c.Mode()), "platform", report.OS)
	case c.Degraded():
		slog.Error("function sandbox DEGRADED: this kernel does not offer every layer, and bodies run with less confinement than the mode implies — set SUBSTRATE_SANDBOX=enforce to refuse instead",
			"mode", string(c.Mode()), "kernel", report.String())
	default:
		slog.Info("function sandbox active", "mode", string(c.Mode()), "kernel", report.String())
	}
}
