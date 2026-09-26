package substrate

// KindInfo is the projection of one declared type the read surfaces use.
type KindInfo struct {
	Identity  string `json:"identity"` // "<authority>/<package>/<name>"
	Name      string `json:"name"`
	Authority string `json:"authority"`
	// Package is the package's own word ("core", "tasks"): the middle segment
	// of the identity, carried beside the authority so a client can group by
	// one and then the other without splitting the reference itself.
	Package string `json:"package"`
	// Version is the declaration's incremental version, server-maintained:
	// 1 for a first declaration, +1 per change (or whatever higher number an
	// explicit apply pinned).
	Version int64 `json:"version"`
	// Source is where the declaration came from: "builtin" (the seed),
	// "published" (a provider install, whose declarations only a substrate path
	// writes) or "installed" (the repository's own).
	Source string `json:"source"`
	// Description is what the kind is for, as its declaration says it: the
	// line a reader gets above the collection, empty when undeclared.
	Description string `json:"description"`
	// Label is the kind's declared display label, absent when it declared
	// none, in which case a client humanizes Name itself. It is display text
	// only: nothing addresses a kind by it (decision 0106).
	Label *KindLabel `json:"label,omitempty"`
	// Definition is the kind's DECLARATION, rendered from the parsed one: the
	// authored data map (names, properties, traits, indices), which is also
	// what the declaration's row stores as its properties. It is not a stored
	// `definition` blob — that spelling is refused everywhere now — and the name
	// survives here because a client reading a kind's shape is reading the same
	// map it always was.
	Definition map[string]any `json:"definition"`
}

// KindLabel is what a client calls one record of a kind and what it calls
// the collection, as the kind's `label:` declares them. Both are always set.
type KindLabel struct {
	Singular string `json:"singular"`
	Plural   string `json:"plural"`
}
