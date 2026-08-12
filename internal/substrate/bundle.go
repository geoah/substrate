package substrate

// BundleStatus is one installed bundle's runtime state, computed on read:
// lifecycle (Installed/Enabled), the "needs configuration" signal
// (Configured is false until the configType's one live record exists), and
// what the bundle ships.
type BundleStatus struct {
	// ID is the bundle's record id — its owned authority.
	ID        string `json:"id"`
	Name      string `json:"name"`
	Authority string `json:"authority"`
	// ConfigType is the bundleconfig-trait record type whose one live record
	// configures the bundle.
	ConfigType string `json:"configType"`
	// Installed is true for a bundle in the live registry, and false only for a
	// quarantined one surfaced from its stored rows. An uninstalled
	// bundle has no status: uninstall tears its rows down, so it
	// simply stops being listed.
	Installed bool `json:"installed"`
	// Enabled is false when the bundle is disabled: execution is stopped.
	Enabled bool `json:"enabled"`
	// Configured reports the configType's live record exists.
	Configured bool `json:"configured"`
	// Accounts counts live records of the bundle's accountconfig-trait types.
	Accounts  int `json:"accounts"`
	Functions int `json:"functions"`
	Kinds     int `json:"kinds"`
	// LiveRecords counts live data rows across the owned authority's types —
	// what a purge would tombstone.
	LiveRecords int64 `json:"liveRecords"`
	// Quarantined reports the bundle's stored closure failed admission at
	// repository-open under the current binary: the repository opened
	// WITHOUT it — its types are not in the live registry — and re-installing
	// the bundle clears it. A quarantined bundle is Installed=false,
	// Enabled=false; QuarantineReason carries the admission error.
	Quarantined bool `json:"quarantined,omitempty"`
	// QuarantineReason is the admission error that quarantined the bundle.
	QuarantineReason string `json:"quarantineReason,omitempty"`
}
