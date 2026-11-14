package e2e

import "strings"

// samplesAuthority is the authority the shipped samples are written under.
// Adding a sample copies it into the repository's own namespace, `~`
// (decision records 0258 and 0259), so every kind, collection, function and
// package of one that the suite names is rebound to `~`. The catalog still
// names a sample by its shipped id: that is what `…/add` and the catalog's
// entries take.
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

// localNamespace is the authority every repository's own packages live
// under, the samples' copies among them.
const localNamespace = "~"

// home is the authority the samples' copies land under: `~`, once
// bindSamples has run.
var home = samplesAuthority

// bindSamples rebinds sampleNames to the repository's own namespace, once the
// run's repository exists.
func bindSamples() {
	home = localNamespace
	for _, name := range sampleNames {
		*name = landed(*name, localNamespace)
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
