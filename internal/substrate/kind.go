package substrate

// KindInfo is the projection of one declared type the read surfaces use.
type KindInfo struct {
	Identity  string `json:"identity"` // "<authority>/<name>"
	Name      string `json:"name"`
	Authority string `json:"authority"`
	// Version is the declaration's incremental version, server-maintained:
	// 1 for a first declaration, +1 per change (or whatever higher number an
	// explicit apply pinned).
	Version int64  `json:"version"`
	Plural  string `json:"plural"`
	Source  string `json:"source"` // "builtin" | "installed"
	// Description is what the kind is for, as its declaration says it: the
	// line a reader gets above the collection, empty when undeclared.
	Description string `json:"description"`
	// Definition is the kind's DECLARATION, rendered from the parsed one: the
	// authored data map (names, properties, edges, traits, indices), which is also
	// what the declaration's row stores as its properties. It is not a stored
	// `definition` blob — that spelling is refused everywhere now — and the name
	// survives here because a client reading a kind's shape is reading the same
	// map it always was.
	Definition map[string]any `json:"definition"`
}
