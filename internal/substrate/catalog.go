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
//     the API, never offered an upgrade.
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
	// none. A sample's version decides nothing: it has no upgrade path.
	Version int64 `json:"version"`
	// Tier is "provider" or "sample": which of the two doors this closure
	// takes, and which of the two authorities it lands under.
	Tier string `json:"tier"`
	// Inputs are the bundle's declared configuration needs, verbatim from
	// the manifest: input name → {kind, inject?, description?}. A bundle
	// with no needs carries none, and the console previews nothing.
	Inputs map[string]any `json:"inputs,omitempty"`
	// Requires names the PACKAGES this bundle's closure declares against: the
	// vocabulary its mappings, references and trigger subscriptions point at.
	// Vocabulary is imported now rather than seeded (repository creation seeds
	// core alone), so the console shows this before the button is pressed and
	// admission refuses while one is absent, naming what to take first.
	Requires []string `json:"requires,omitempty"`
	// SuggestedMappings are the mappings this closure declares onto its own
	// kinds FROM another package's, each with the state it has in this
	// repository. A sample ships them for the providers it knows and the
	// import keeps only the ones whose provider is here (decision record
	// 0049), so a reader can see what an import will and will not deliver.
	// Empty for every provider: a provider declares no mapping at all.
	SuggestedMappings []SuggestedMapping `json:"suggestedMappings,omitempty"`
	// Closure enumerates what taking this bundle lands, for the detail preview.
	Closure CatalogClosure `json:"closure"`
}

// SuggestedMapping is one mapping a SAMPLE ships onto a kind of its own from a
// PROVIDER's mirror kind (decision record 0049): the declaration's id, both
// ends, the provider package the source lives in, and whether this repository
// holds that package.
//
// It is conditional because its source kind is another package's: a mapping
// naming an absent kind is refused by admission, so the door drops the
// document (and its `installs:` entry) rather than offering the reader a
// refusal. Installing the provider and importing the sample AGAIN is what
// lands it.
type SuggestedMapping struct {
	// ID is the mapping declaration's id, in the spelling the closure ships:
	// a sample's is the placeholder authority, which the import rehomes with
	// everything else.
	ID string `json:"id"`
	// From is the source kind: a provider mirror this package does not own.
	From string `json:"from"`
	// To is the subject kind, always one this package declares.
	To string `json:"to"`
	// Package is the PROVIDER package `from` lives in: what has to be
	// installed for this mapping to land.
	Package string `json:"package"`
	// State is SuggestedMappingLanded or SuggestedMappingWaiting.
	State string `json:"state"`
}

// SuggestedMapping.State values, read against one repository:
//
//   - SuggestedMappingLanded: the provider package is here, so the mapping is
//     part of the closure this door admits (and part of what an earlier import
//     already admitted).
//   - SuggestedMappingWaiting: the provider package is absent, so the mapping
//     is dropped. Install that provider and import the sample again.
const (
	SuggestedMappingLanded  = "landed"
	SuggestedMappingWaiting = "waiting"
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
