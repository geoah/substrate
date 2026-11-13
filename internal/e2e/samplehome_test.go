package e2e

import "strings"

// samplesAuthority is the authority the shipped samples are written under.
// Adding a sample copies it under the repository's own authority (decision
// record 0258), so every kind, collection, function and package of one that
// the suite names is rebound to the run's authority once the repository
// exists. The catalog still names a sample by its shipped id: that is what
// `…/add` and the catalog's entries take.
const samplesAuthority = "samples.substrate.reamde.dev"

// sampleNames are the suite's names of the samples' copies.
var sampleNames = []*string{
	&orgCollection, &personCollection, &teamCollection, &projectCollection,
	&calendarCollection, &eventCollection, &transcriptCollection,
	&personKind, &teamKind, &orgKind, &projectKind, &calendarKind,
	&tasksCollection, &tasksHeld,
	&transcriptKind, &eventKind, &taskKind,
	&xqTaskKind,
	&seriesCollection, &seriesKind,
	&xvNotesHeld, &xvNoteCollection, &xvStatsFunction, &xvNoteKind,
}

// home is the authority the samples' copies land under: the run's, once
// bindSamples has seen it.
var home = samplesAuthority

// bindSamples rebinds sampleNames to the repository the run registered.
func bindSamples(authority string) {
	home = authority
	for _, name := range sampleNames {
		*name = landed(*name, authority)
	}
}

// sampleHome is the authority the samples' copies land under in this run.
func sampleHome() string { return home }

// landed is s with the samples' authority replaced by the one a copy lands
// under: what a sample's own name, or a function body naming its kinds,
// reads as in this repository.
func landed(s, authority string) string {
	return strings.ReplaceAll(s, samplesAuthority, authority)
}
