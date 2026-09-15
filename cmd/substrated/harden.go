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

// reportTrust says what a function body verifies a TLS peer against, beside
// the sandbox line and for the same reason: it is a per-host property that
// fails silently and late. An interpreter whose compiled-in certificate store
// does not exist — the python.org framework build on macOS — makes every
// function that declares `network:` fail CERTIFICATE_VERIFY_FAILED, and until
// this line existed nothing at boot said so.
func reportTrust() {
	level, msg, attrs := trustReport(runner.Shared.Trust())
	slog.Log(context.Background(), level, msg, attrs...)
}

// trustReport builds that line. It takes the result and the error rather than
// the runner so every branch, including the one only a bare host reaches, is
// assertable from a test.
func trustReport(trust runner.TrustStore, err error) (slog.Level, string, []any) {
	switch {
	case err != nil:
		// The interpreter could not be asked. Functions are unavailable
		// regardless, so this is the milder half of a failure a body start
		// will report again with its own error.
		return slog.LevelWarn,
			"function bodies trust: UNKNOWN — the interpreter could not be asked which certificate store it uses",
			[]any{"error", err.Error()}

	case trust.Path == "":
		return slog.LevelWarn,
			"function bodies trust: NONE — every function that declares `network:` will fail certificate verification",
			[]any{
				"reason", "this interpreter names a certificate store that does not exist, and no system bundle was found either",
				"searched", strings.Join(runner.SystemCertBundles(), ", "),
				"advice", "install this platform's CA certificates package",
			}

	case trust.Injected:
		return slog.LevelInfo,
			"function bodies trust: the system certificate bundle",
			[]any{
				"bundle", trust.Path,
				"source", "SSL_CERT_FILE",
				"reason", "this interpreter's own certificate store does not exist",
			}

	default:
		return slog.LevelInfo,
			"function bodies trust: the interpreter's own certificate store",
			[]any{"bundle", trust.Path, "source", "interpreter default"}
	}
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

	case !report.ConnectGate:
		// Named apart from the other degradations because it is the only one
		// whose remedy is not a kernel: every layer installed, and the calls
		// the gate's SUPERVISOR answers notifications with are what the
		// container's seccomp profile refused. The consequence is a refusal
		// and never an unfiltered body, so the line says what stops working
		// rather than what quietly widened.
		refused := report.ConnectGateErr
		if refused == "" && report.Err != nil {
			refused = report.Err.Error()
		}
		return slog.LevelError,
			"function sandbox DEGRADED: the connect gate cannot be serviced, so every function that declares `network:` is REFUSED until the capability is granted",
			[]any{
				"mode", string(mode),
				"kernel", report.String(),
				"refused", refused,
				"advice", sandbox.ConnectGateRemedy,
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
