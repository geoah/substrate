package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/sandbox"
)

// reportSandbox says, once and where an operator will see it, what the
// function sandbox is actually doing. A confinement that silently degraded on
// a kernel without Landlock would be worse than none: it would be believed.
func reportSandbox() {
	c := runner.Shared.Sandbox()
	level, msg, attrs := sandboxReport(c.Mode(), c.Report())
	slog.Log(context.Background(), level, msg, attrs...)
}

// sandboxReport builds the boot line. It takes the mode and the report rather
// than the confiner so the UNSUPPORTED-platform branch can be asserted from a
// Linux test: that branch is the one a macOS developer actually reads, and it
// is the one no CI machine would otherwise execute.
func sandboxReport(mode sandbox.Mode, report sandbox.Report) (slog.Level, string, []any) {
	switch {
	case mode == sandbox.ModeOff:
		return slog.LevelWarn,
			"function sandbox OFF: bodies run unconfined, and reach the network regardless of what they declare",
			[]any{"mode", string(mode)}

	case !report.Supported():
		// A developer laptop, almost always. Saying "degraded" and pointing at
		// SUBSTRATE_SANDBOX=enforce would be bad advice twice over: there is no
		// kernel setting to fix, and enforce here refuses to run any function
		// at all. WARN and not ERROR, because running the substrate on this
		// platform is a development choice, not a broken deployment.
		//
		// The limitations are ENUMERATED rather than summarized as
		// "unconfined". A reader who does not already know what the sandbox
		// does cannot expand that word into what they are actually exposed to,
		// and this line is the only place they will be told.
		return slog.LevelWarn,
			"function sandbox unavailable on " + report.OS + ": function bodies are NOT confined on this platform",
			[]any{
				"mode", string(mode),
				"platform", report.OS,
				"reason", "landlock and seccomp are Linux facilities, and nothing here stands in for them",
				"not_enforced", strings.Join([]string{
					"a function's declared `network:` allowlist (a body reaches the internet whether or not it declares egress)",
					"filesystem confinement (a body reads this process's environment, its files, and other functions' data)",
					"resource limits",
				}, "; "),
				"still_enforced", "one process per function, the capability-scoped host reads and writes, and the child environment allowlist",
				"advice", "run bodies you do not trust on Linux, or in the Linux VM behind Docker",
			}

	case report.Degraded(mode):
		return slog.LevelError,
			"function sandbox DEGRADED: this kernel does not offer every layer, and bodies run with less confinement than the mode implies. Set SUBSTRATE_SANDBOX=enforce to refuse instead",
			[]any{"mode", string(mode), "kernel", report.String()}

	default:
		return slog.LevelInfo, "function sandbox active",
			[]any{"mode", string(mode), "kernel", report.String()}
	}
}
