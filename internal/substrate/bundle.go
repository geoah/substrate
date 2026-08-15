package substrate

// BundleStatus is one installed bundle's runtime state, computed on read:
// lifecycle (Installed/Enabled), each declared input's resolution, and the
// setup steps that stand between the bundle and every runtime path it ships.
// A bundle that needs nothing carries empty Inputs and Setup, and reads as
// simply enabled.
type BundleStatus struct {
	// ID is the bundle's record id — its owned authority.
	ID        string `json:"id"`
	Name      string `json:"name"`
	Authority string `json:"authority"`
	// Installed is true for a bundle in the live registry, and false only for a
	// quarantined one surfaced from its stored rows. An uninstalled
	// bundle has no status: uninstall tears its rows down, so it
	// simply stops being listed.
	Installed bool `json:"installed"`
	// Enabled is false when the bundle is disabled: execution is stopped.
	Enabled bool `json:"enabled"`
	// Inputs is each declared input's resolution, in name order: which
	// record satisfies it and how it was chosen. Empty when the bundle
	// declares no inputs.
	Inputs []InputStatus `json:"inputs,omitempty"`
	// Setup lists what stands between this bundle and every runtime path
	// it ships — unresolved inputs, an incomplete OAuth client, an agent
	// whose llmprovider row is missing or keyless. Empty means ready.
	// Every item mirrors a refusal dispatch would actually make; a
	// non-refusal is never a setup step.
	Setup []SetupItem `json:"setup,omitempty"`
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

// BundleUpgrade is what re-installing a bundle's SHIPPED closure over this
// repository's stored declarations would do, computed on read and stored
// nowhere. The install verb is already the upgrade (a bundle document
// replaces its authority whole, breakage refused); this is that verb's
// preview, so the console can offer the upgrade, or explain why it would be
// refused, before anyone asks for it.
type BundleUpgrade struct {
	// Available reports the shipped closure moves at least one stored
	// declaration: a declaration this repository lacks, one whose shipped
	// version is newer, or one the closure stopped shipping (which a
	// re-install prunes).
	Available bool `json:"available"`
	// From and To are the stored and shipped versions of the bundle's owned
	// authority. Zero is the absent version: From is 0 (and omitted) when the
	// stored authority row carries none.
	From int64 `json:"from,omitempty"`
	To   int64 `json:"to,omitempty"`
	// Changes lists each declaration the upgrade would move.
	Changes []BundleUpgradeChange `json:"changes,omitempty"`
	// Blockers are the refuse-breakage guard lines the install verb would
	// refuse this closure on: the same guards, with the live-row counts.
	// Empty means the upgrade would be admitted.
	Blockers []string `json:"blockers,omitempty"`
}

// BundleUpgradeChange is one declaration an upgrade would move.
type BundleUpgradeChange struct {
	// Kind is the declaration's manifest kind: "kind", "function", "authority"…
	Kind string `json:"kind"`
	// ID is the declaration's record id.
	ID string `json:"id"`
	// From is the stored version; 0 (omitted) when this repository lacks the
	// declaration.
	From int64 `json:"from,omitempty"`
	// To is the shipped version; 0 (omitted) when the closure stopped
	// shipping the declaration and the upgrade would prune it.
	To int64 `json:"to,omitempty"`
}

// InputStatus is one declared input's resolution.
type InputStatus struct {
	// Name is the input's declared name — also the edge rel the bind verb
	// writes on the bundle's record row.
	Name string `json:"name"`
	// Kind is the full identity of the kind whose records satisfy the input.
	Kind string `json:"kind"`
	// Description is the input's declared purpose.
	Description string `json:"description,omitempty"`
	// Record is the resolved record's id, empty while unresolved.
	Record string `json:"record,omitempty"`
	// Via says how the record was chosen: bound (an explicit edge), default
	// (the record named "default"), or sole (the only live record). Empty
	// while unresolved.
	Via string `json:"via,omitempty"`
}

// InputStatus.Via values — the resolution order, most explicit first.
const (
	InputViaBound   = "bound"
	InputViaDefault = "default"
	InputViaSole    = "sole"
)

// SetupItem is one thing standing between a bundle and a runtime path it
// ships. Code is machine-readable; Message mirrors the refusal the runtime
// would make.
type SetupItem struct {
	// Code is the stable reason: SetupMissing/SetupAmbiguous/SetupDangling
	// for an input, SetupOAuthClient for an incomplete client record,
	// SetupProvider for an agent whose llmprovider row is absent or keyless.
	Code string `json:"code"`
	// Input names the unresolved input, when the item is an input's.
	Input string `json:"input,omitempty"`
	// Kind is the kind whose record would clear the item: the input's kind,
	// or core's llmprovider.
	Kind string `json:"kind,omitempty"`
	// Record is the existing record to fix, when one exists — the incomplete
	// client, the keyless provider row.
	Record string `json:"record,omitempty"`
	// Message is one sentence naming the fix.
	Message string `json:"message"`
}

// SetupItem.Code values.
const (
	// SetupMissing: no record of the input's kind exists yet.
	SetupMissing = "missing"
	// SetupAmbiguous: several records exist and none is bound or named
	// "default" — an explicit choice is required, never a tie-break.
	SetupAmbiguous = "ambiguous"
	// SetupDangling: the input is bound to a record that no longer resolves.
	SetupDangling = "dangling"
	// SetupOAuthClient: the client input resolved but its record is missing
	// clientId or clientSecret.
	SetupOAuthClient = "oauth-client"
	// SetupProvider: an agent names an llmprovider row that is absent or
	// cannot dispatch (no key where one is required).
	SetupProvider = "provider"
)
