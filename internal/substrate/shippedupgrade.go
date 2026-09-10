package substrate

// ShippedUpgrade is what the running binary's boot upgrade would do to one
// package the binary ships and seeds (the `core` package), computed at read
// against this repository's stored declarations. The boot upgrade runs at a
// repository's first open under a new binary and, when a refuse-breakage
// guard refuses it, skips the upgrade and logs the guard lines; this is the
// same diff and the same guard lines, served to a repository token as
// `GET /api/v1/vocabulary/upgrade`, so a withheld core upgrade is not only a
// server log line.
//
// The answer reflects the RUNNING binary: a binary whose boot was refused and
// has since been replaced is not what this reports.
type ShippedUpgrade struct {
	// Package is the shipped package's identity ("substrate.reamde.dev/core").
	Package string `json:"package"`
	// Upgrade is the version motion and the blockers, the same shape a
	// catalog entry carries for an installed provider. Available is false when
	// the repository already holds every declaration at the shipped version.
	// Blockers non-empty means the boot upgrade refused, and the stored
	// declarations stand until the rows the lines name are migrated.
	Upgrade BundleUpgrade `json:"upgrade"`
}
