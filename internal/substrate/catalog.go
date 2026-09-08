package substrate

// The catalog's wire shapes. They live here rather than in internal/catalog
// because the console mirrors them by hand and the wire golden
// (wire_test.go) can only reflect over this package: internal/catalog imports
// this one, so this one cannot import it back. internal/catalog embeds
// CatalogBundle and keeps the closure documents beside it, unexported.

// The two catalog TIERS (decision record 0048). A closure's tier is decided by
// the shipped tree it came from, never by its authority's shape:
//
//   - TierProvider is a package a publisher owns (kinds/providers.…). It
//     INSTALLS as a copy under the authority it names, and the publisher ships
//     each change with a version bump the upgrade preview offers.
//   - TierSample is a package the user copies (samples/). It IMPORTS under the
//     repository's own authority and is the user's afterwards: writable through
//     the API, and offered the shipped upgrade through the origin stamp its
//     import left, taken by importing again (decision record 0070).
const (
	TierProvider = "provider"
	TierSample   = "sample"
)

// CatalogBundle is one installable catalog entry: the shipped closure's
// identity and the metadata a reader previews before taking it. The closure's
// documents are not on the wire: Closure is the preview of what they land.
type CatalogBundle struct {
	// ID is the bundle's record id, the PACKAGE it is named for
	// ("providers.substrate.reamde.dev/google"), matching BundleStatus.ID once
	// installed. A SAMPLE lands under the repository's own authority, so its
	// stored bundle id is not this one.
	ID string `json:"id"`
	// Name is the owned package's own word ("google").
	Name string `json:"name"`
	// Authority is the authority the closure is published under. For a sample
	// that is the placeholder the tree authors under, never where it lands.
	Authority string `json:"authority"`
	// Package is the owned package's name, the same word as Name: the console
	// groups the catalog by authority, then by package.
	Package string `json:"package"`
	// Description is the bundle document's description.
	Description string `json:"description"`
	// Version is the owned package's version. Zero means the closure declares
	// none. The import records a sample's as the copy's OriginVersion, and the
	// preview compares the two to say the shipped sample moved (decision
	// record 0070).
	Version int64 `json:"version"`
	// Tier is "provider" or "sample": which of the two doors this closure
	// takes, and which of the two authorities it lands under.
	Tier string `json:"tier"`
	// Inputs are the bundle's declared configuration needs, verbatim from
	// the manifest, keyed by input name. A bundle with no needs carries none,
	// and the console previews nothing.
	Inputs map[string]CatalogInput `json:"inputs,omitempty"`
	// Requires names the PACKAGES this bundle's closure declares against: the
	// vocabulary its mappings, references and trigger subscriptions point at.
	// Vocabulary is imported now rather than seeded (repository creation seeds
	// core alone), so the console shows this before the button is pressed and
	// admission refuses while one is absent, naming what to take first.
	Requires []string `json:"requires,omitempty"`
	// RequiresAtLeast is the floor the closure puts under a required package
	// (decision record 0070): the least version of it that satisfies the
	// requirement, keyed by the package identity Requires lists. A package
	// with no entry is satisfied by any version. Admission refuses while the
	// repository holds the package below its floor, naming both versions, so
	// the console reads the floor against BundleStatus.Version before the
	// button is pressed.
	RequiresAtLeast map[string]int64 `json:"requiresAtLeast,omitempty"`
	// SuggestedMappings are the mappings this closure declares onto its own
	// kinds FROM another package's, each with the state it has in this
	// repository (below). A sample ships them for the providers it knows and
	// the door keeps only the ones that resolve here (decision record 0049),
	// so a reader can see what an import will and will not deliver. Empty for
	// every provider: a provider declares no mapping at all.
	SuggestedMappings []SuggestedMapping `json:"suggestedMappings,omitempty"`
	// Origin, OriginVersion and Modified are the HELD copy's provenance in
	// this repository, copied from its BundleStatus per request the way
	// SuggestedMappings is: which shipped id it was imported from, the
	// shipped version it was taken at, and whether its declarations have
	// moved since. Absent while the repository does not hold the bundle, on
	// a provider, and on a copy imported before the stamp existed.
	Origin        string `json:"origin,omitempty"`
	OriginVersion int64  `json:"originVersion,omitempty"`
	Modified      bool   `json:"modified,omitempty"`
	// Closure enumerates what taking this bundle lands, for the detail preview.
	Closure CatalogClosure `json:"closure"`
}

// CatalogInput is one declared input as the catalog previews it, the manifest's
// closed key set (internal/vocabulary's bundleInputKeys): the kind whose
// records satisfy it, who consumes it, and what it is for.
type CatalogInput struct {
	// Kind is the full identity of the kind whose records satisfy the input.
	Kind string `json:"kind"`
	// Inject is "functions" when the resolved record rides function
	// invocations; empty means facility-read only.
	Inject      string `json:"inject,omitempty"`
	Description string `json:"description,omitempty"`
}

// CatalogItem is one catalog entry as the API serves it (`GET /api/v1/catalog`
// and `/catalog/{id}`): the shipped bundle plus whether THIS repository holds
// it and, for a held one whose shipped closure moved or whose preview could
// not run, what re-installing would do. The console and the CLI decode this
// one shape, and the wire golden holds it with the embedded bundle's fields
// promoted, the way encoding/json writes them.
type CatalogItem struct {
	// The shipped bundle by VALUE, not by pointer: the catalog holds one
	// parsed copy of each closure and serves every repository from it, and
	// `suggestedMappings` and the origin fields carry a per-repository state,
	// so the entry a request answers with is a copy the handler fills.
	CatalogBundle
	Installed bool `json:"installed"`
	// Upgrade is present when the shipped closure moves something here (the
	// version motion and the guard lines an install would refuse on), and
	// when the preview could not say (one blocker line, no motion). The
	// upgrade itself is the existing install verb, unchanged.
	Upgrade *BundleUpgrade `json:"upgrade,omitempty"`
}

// SuggestedMapping is one mapping a SAMPLE ships onto a kind of its own from a
// PROVIDER's mirror kind (decision record 0049): the declaration's id, both
// ends, the provider package the source lives in, and what that mapping is
// doing in this repository right now.
//
// It is conditional because its source kind is another package's: a mapping
// naming an absent or ill-fitting kind is refused by admission, so the door
// drops the document (and its `installs:` entry) rather than handing the
// reader a refusal for something they did not ask for.
type SuggestedMapping struct {
	// ID is the mapping declaration's id AS THIS REPOSITORY WOULD HOLD IT: a
	// sample's is rehomed onto the repository's own authority, because that is
	// the record the state below is read from.
	ID string `json:"id"`
	// From is the source kind: a provider mirror this package does not own.
	From string `json:"from"`
	// To is the subject kind, always one this package declares, rehomed with
	// the id.
	To string `json:"to"`
	// Package is the PROVIDER package `from` lives in: what has to be
	// installed for this mapping to land.
	Package string `json:"package"`
	// State is one of the four below.
	State string `json:"state"`
	// Problems are the resolution problems behind SuggestedMappingBlocked, as
	// the loader words them. Empty in every other state.
	Problems []string `json:"problems,omitempty"`
}

// SuggestedMapping.State values, read against one repository. The state is the
// MAPPING RECORD's, never the provider's alone: a provider install lands
// mirrors and no mapping, so "the provider is here" and "the projection runs"
// are two different answers and a reader needs both.
//
//   - SuggestedMappingLanded: the mapping declaration is in this repository.
//     The projection runs.
//   - SuggestedMappingReady: the provider is here and the mapping fits it, but
//     the declaration is not here. Importing the sample AGAIN lands it, and
//     that re-import replaces the package (record 0048).
//   - SuggestedMappingWaiting: the provider package is absent, so the mapping
//     is dropped from the closure. Install the provider, then import again.
//   - SuggestedMappingBlocked: the provider is here but the mapping does not
//     resolve against the version installed (a subject slot or a mapped
//     property it names is missing). Upgrading the provider is what unblocks
//     it, and Problems says what did not fit.
const (
	SuggestedMappingLanded  = "landed"
	SuggestedMappingReady   = "ready"
	SuggestedMappingWaiting = "waiting"
	SuggestedMappingBlocked = "blocked"
)

// CatalogClosure is what installing or importing a bundle lands, by kind: the
// detail preview the console shows first. EVERY member of it is a record: a
// kind, a function and an agent are records of the core meta-kinds, and Records
// are the ordinary data rows the same transaction writes beside them. The lists
// are split because a reader asks different questions of each, not because the
// things differ in nature.
type CatalogClosure struct {
	Kinds []string `json:"kinds"`
	// KindDescriptions is each kind's declared description, keyed by identity
	// what the closure's kinds ARE, before an install has put them in the
	// registry a reader could look them up in. Omitted where a kind declares
	// none, so the map is only as big as the prose.
	KindDescriptions map[string]string `json:"kindDescriptions,omitempty"`
	Functions        []string          `json:"functions"`
	Agents           []string          `json:"agents"`
	// Mappings answers "what will this project onto the vocabulary I already
	// have", the question a reader asks before taking a provider.
	Mappings []string `json:"mappings"`
	// Records are the DATA records the install writes after the declarations
	// land: a provider's triggers, the llm sample's two keyless provider rows.
	// They are ordinary records the moment they exist (editable, deletable),
	// and half of what a bundle DOES arrives this way, so a preview that named
	// only the declarations would hide the very row the reader is about to be
	// told to go and key.
	Records []CatalogShippedRecord `json:"records"`
}

// CatalogShippedRecord is one data record a bundle ships. A data record's
// identity is its KIND and its id together (a declaration's is one reference),
// so both travel, and the console addresses the record from them.
type CatalogShippedRecord struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
