package substrate

import "context"

// BundleStatus is one installed bundle's runtime state, computed on read:
// lifecycle (Installed/Enabled), each declared input's resolution, and the
// setup steps that stand between the bundle and every runtime path it ships.
// A bundle that needs nothing carries empty Inputs and Setup, and reads as
// simply enabled.
type BundleStatus struct {
	// ID is the bundle's record id — the package it is named for.
	ID        string `json:"id"`
	Name      string `json:"name"`
	Authority string `json:"authority"`
	// Package is the owned package's own word, the same as Name.
	Package string `json:"package"`
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
	// LiveRecords counts live data rows across the owned package's kinds —
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
	// Origin is the shipped bundle id an imported SAMPLE was copied from
	// ("samples.substrate.reamde.dev/tasks"), read off the owned package
	// row where the import stamped it (decision record 0048's copy, with its
	// provenance). Empty on a provider, on a hand-applied closure, and on a
	// sample imported before the stamp existed: nothing reconstructs it.
	Origin string `json:"origin,omitempty"`
	// OriginVersion is the shipped package version the copy was taken at.
	// Zero when Origin is empty.
	OriginVersion int64 `json:"originVersion,omitempty"`
	// Modified reports the copy's declarations no longer match what the
	// import landed: a kind, trait, property type, mapping, function, agent
	// or bundle document edited, added or removed since. Versions alone
	// cannot say so (a kind edit moves the kind's version and not the
	// package's, and an addition moves nothing), so the import stamps a
	// digest of the landed closure and the status recomputes it from the
	// stored rows. False when Origin is empty.
	Modified bool `json:"modified,omitempty"`
}

// BundleUpgrade is what re-installing a bundle's SHIPPED closure over this
// repository's stored declarations would do, computed on read and stored
// nowhere. The install verb is already the upgrade (a bundle document
// replaces its package whole, breakage refused); this is that verb's
// preview, so the console can offer the upgrade, or explain why it would be
// refused, before anyone asks for it.
type BundleUpgrade struct {
	// Available reports the shipped closure moves at least one stored
	// declaration: a declaration this repository lacks, one whose shipped
	// version is newer, or one the closure stopped shipping (which a
	// re-install prunes).
	Available bool `json:"available"`
	// From and To are the stored and shipped versions of the bundle's owned
	// package. Zero is the absent version: From is 0 (and omitted) when the
	// stored package row carries none.
	From int64 `json:"from,omitempty"`
	To   int64 `json:"to,omitempty"`
	// Changes lists each declaration the upgrade would move.
	Changes []BundleUpgradeChange `json:"changes,omitempty"`
	// Blockers are the refuse-breakage guard lines the install verb would
	// refuse this closure on: the same guards, with the live-row counts, and
	// a conversion above the work ceiling. Empty means the upgrade would be
	// admitted, with a confirmation where Lossy says so.
	Blockers []string `json:"blockers,omitempty"`
	// The record rewrites the upgrade performs, counted against the live
	// records (decision 0067).
	ConversionPlan
	// Renames is the plan's rename steps in the shape this field had before
	// Steps existed, derived from Steps and never a second count.
	//
	// Deprecated: read Steps, where a rename is `step: rename`. The field
	// stays because the bundles feature is stable and frozen means additive
	// only (decision 0067).
	Renames []BundleUpgradeRename `json:"renames,omitempty"`
}

// BundleUpgradeRename is one property rename an upgrade performs, the shape
// the deprecated BundleUpgrade.Renames carries; a ConversionStep with
// StepRename says the same and more.
type BundleUpgradeRename struct {
	// Kind is the full reference of the kind whose property moves.
	Kind string `json:"kind"`
	// From and To are the old and the new property names.
	From string `json:"from"`
	To   string `json:"to"`
	// Records is the number of live records carrying the old name, each of
	// which the upgrade rewrites.
	Records int64 `json:"records"`
}

// ConversionPlan is the composed set of record rewrites a declaration change
// performs on the live records (decisions 0063, 0066 and 0067), as the two
// previews report it and the two doors run it. Every count is a number, never
// a record id: the plan says how much moves, and the changelog says what did.
type ConversionPlan struct {
	// Steps lists each rewrite with the live records it touches, in kind then
	// step order. A step touching no record is not listed.
	Steps []ConversionStep `json:"steps,omitempty"`
	// Work is the estimated cost: the sum of the steps' record counts, an
	// upper bound on the changelog entries the transaction appends (a record
	// several steps touch is rewritten once). A plan whose Work is above the
	// deployment's ceiling (SUBSTRATE_CONVERSION_CEILING, in records, 10000
	// by default) is refused.
	Work int64 `json:"work"`
	// Lossy reports the plan collapses a distinction live records hold: a
	// `null` step, or a remap onto a value some live record already holds (or
	// that another remap lands its records on), judged across the whole plan. A lossy plan runs only with a ConversionConfirm
	// carrying this plan's PlanHash and ChangelogSeq; a lossless one runs
	// unconfirmed. The old values stay in the changelog either way: a lossy
	// step removes them from the fold and nothing erases them.
	Lossy bool `json:"lossy"`
	// PlanHash identifies the plan: a hash over the steps and their counts.
	// Empty when nothing was planned.
	PlanHash string `json:"planHash,omitempty"`
	// ChangelogSeq is the repository's changelog head when the plan was
	// counted. Any write moves it, and a confirmation carrying another seq is
	// refused.
	ChangelogSeq int64 `json:"changelogSeq,omitempty"`
}

// ConversionStep is one record rewrite a declaration change performs.
type ConversionStep struct {
	// Step is the rewrite: StepRename, StepBackfill, StepRemap or StepNull.
	Step string `json:"step"`
	// Kind is the full reference of the kind whose records move.
	Kind string `json:"kind"`
	// Property is the property the step writes, under the name the candidate
	// declaration gives it (a rename's To; a null's dropped name).
	Property string `json:"property"`
	// From and To are a rename's old and new property names, or a remap's old
	// and new values. Empty on a backfill and a null.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Records is the number of live records the step rewrites.
	Records int64 `json:"records"`
	// Lossy marks a step that removes values from the fold: every null, and a
	// remap whose target some live record already holds.
	Lossy bool `json:"lossy,omitempty"`
}

// ConversionStep.Step values, in the order the engine runs them on a record.
const (
	// StepRename moves a property's value to its new name (`renamedFrom`).
	StepRename = "rename"
	// StepBackfill writes a property's declared default onto every record
	// holding no value for it, where the property becomes required.
	StepBackfill = "backfill"
	// StepRemap rewrites an enum value to its new spelling (`renamedFrom` on
	// the value entry).
	StepRemap = "remap"
	// StepNull removes a dropped property's value from every record carrying
	// it. Always lossy.
	StepNull = "null"
)

// ConversionConfirm is the caller's consent to a lossy plan, bound to what
// was previewed: the plan's hash and the changelog head it was counted at.
// The door recounts the plan under its locks and refuses a confirmation whose
// seq is not the current head (a write landed since the preview) or whose
// hash is not the recounted plan's (the consent covers a different plan).
type ConversionConfirm struct {
	PlanHash     string `json:"planHash"`
	ChangelogSeq int64  `json:"changelogSeq"`
}

// BundleUpgradeChange is one declaration an upgrade would move.
type BundleUpgradeChange struct {
	// Kind is the declaration's manifest kind: "kind", "function", "package"…
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
	// Name is the input's declared name — also the reference property the bind verb
	// writes on the bundle's record row.
	Name string `json:"name"`
	// Kind is the full identity of the kind whose records satisfy the input.
	Kind string `json:"kind"`
	// Description is the input's declared purpose.
	Description string `json:"description,omitempty"`
	// Record is the resolved record's id, empty while unresolved.
	Record string `json:"record,omitempty"`
	// Via says how the record was chosen: bound (an explicit reference), default
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

// BundleOps is the bundle-lifecycle seam, an optional Dataset extension (see
// Dataset). Status is computed; disable/enable and uninstall are reversible
// runtime state; purge tombstones the owned package's data through the
// finalizer flow; StartOAuth begins the host connect flow for one account
// record. A dataset without it has no bundle verbs.
type BundleOps interface {
	BundleStatuses(ctx context.Context) ([]BundleStatus, error)
	BundleStatus(ctx context.Context, id string) (BundleStatus, error)
	// BundlePackage resolves the package a bundle owns (from the live registry
	// or stored rows) for the lifecycle scope gate.
	BundlePackage(ctx context.Context, id string) (string, error)
	DisableBundle(ctx context.Context, id string) error
	EnableBundle(ctx context.Context, id string) error
	// BindBundleInput points a bundle's input at a chosen record (empty
	// record clears the choice), the explicit step of input resolution.
	BindBundleInput(ctx context.Context, id, input, record string) error
	UninstallBundle(ctx context.Context, id string) error
	PurgeBundle(ctx context.Context, id string) (int, error)
	StartOAuth(ctx context.Context, actor Actor, recordID string) (string, error)
	TypesImplementing(ctx context.Context, trait string) ([]KindInfo, error)
}

// BundleUninstalled is the reply to an uninstall. The bundle has no status
// afterwards (uninstall tears its rows down), so the reply is the fact alone.
type BundleUninstalled struct {
	Uninstalled bool `json:"uninstalled"`
}

// BundlePurged is the reply to a purge: how many live data rows of the owned
// package it tombstoned.
type BundlePurged struct {
	Purged int `json:"purged"`
}

// OAuthStarted is the reply to StartOAuth: the provider consent URL the
// browser visits next.
type OAuthStarted struct {
	URL string `json:"url"`
}

// BundleInstaller is the atomic install verb an installable target must
// offer, an optional Dataset extension (see Dataset): the vocabulary closure
// AND the shipped delivery wiring admitted as ONE repository transaction, so
// a data-document failure rolls the vocabulary apply back with it.
type BundleInstaller interface {
	InstallBundleClosure(ctx context.Context, actor Actor, vocabularyDocs []map[string]any, dataDocs []PutInput, opts BundleInstall) ([]*Record, error)
}

// BundleInstall is what the caller decides about an install beyond the
// documents themselves.
type BundleInstall struct {
	// Published marks a PROVIDER install: the closure's packages land with
	// `source: published`, and afterwards only a substrate path (an install,
	// an upgrade) may write their declarations, the same refusal the seeded
	// core package gives (decision record 0048). The catalog sets it from the
	// tier it served the closure from; a hand-applied closure carries no tier,
	// so it lands `installed` and stays the repository's own.
	Published bool
	// Origin and OriginVersion are the SAMPLE import's provenance: the
	// shipped bundle id the closure was copied from and the shipped package
	// version it was copied at. The engine stamps them, with a digest of the
	// landed closure, as managed properties on the package row the origin
	// names, so a copy can be told from an original and an edited copy from
	// a pristine one (BundleStatus.Modified). The provider install and a hand
	// apply leave both empty and stamp nothing.
	Origin        string
	OriginVersion int64
	// Confirm is the caller's consent to a lossy conversion plan, or nil. The
	// install refuses a lossy plan without one and a lossless plan ignores it
	// (decision 0067).
	Confirm *ConversionConfirm
}

// BundleUpgradePlanner is the read-only preview beside BundleInstaller, an
// optional Dataset extension (see Dataset): what installing the shipped
// closure over the stored declarations would move, the guard lines the
// install would refuse it on, and the conversion plan it would run.
type BundleUpgradePlanner interface {
	PlanBundleUpgrade(ctx context.Context, vocabularyDocs []map[string]any) (BundleUpgrade, error)
}

// OAuthMaintainer is the OAuth upkeep pass the service loop drives, an
// optional Dataset extension (see Dataset): refresh keeps stored tokens
// fresh, the finalizer pass revokes and releases deleted accounts ahead of
// GC.
type OAuthMaintainer interface {
	RefreshOAuthTokens(ctx context.Context) (int, error)
	ProcessOAuthFinalizers(ctx context.Context) (int, error)
}

// OAuthCompleter is the Service half of the connect flow, an optional Service
// extension (see Service): the callback carries no bearer, the signed state
// IS the authentication, so it resolves the repository itself. Its Dataset
// half is BundleOps.StartOAuth.
type OAuthCompleter interface {
	CompleteOAuth(ctx context.Context, state, code string) (string, error)
}
