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
	// only: nothing addresses a kind by it (decision 0117).
	Label *KindLabel `json:"label,omitempty"`
	// Definition is the kind's DECLARATION, rendered from the parsed one: the
	// authored data map (names, properties, traits, indices), which is also
	// what the declaration's row stores as its properties. It is not a stored
	// `definition` blob — that spelling is refused everywhere now — and the name
	// survives here because a client reading a kind's shape is reading the same
	// map it always was.
	Definition map[string]any `json:"definition"`
	// Control is the control class of a control kind (decision 0276):
	// `triggers`, `policies`, `providers`, `settings`, `applets`,
	// `declarations`, `actors` or `roles`, which a role's rule reaches only
	// by naming the kind; `sign-in` for sign-in material, held the same way.
	// Absent for every other kind.
	Control string `json:"control,omitempty"`
}

// KindLabel is what a client calls one record of a kind and what it calls
// the collection, as the kind's `label:` declares them. Both are always set.
type KindLabel struct {
	Singular string `json:"singular"`
	Plural   string `json:"plural"`
}

// KindRetention is how long one kind's history is kept in a repository past
// each record's latest state (decision 0282): what its declaration states,
// the repository's override, and the one that applies. Each value is `all`,
// `none` or an ISO 8601 duration.
type KindRetention struct {
	Kind string `json:"kind"`
	// Declared is what the declaration states, the kind's own key else its
	// package header's, and absent where neither states one.
	Declared string `json:"declared,omitempty"`
	// Override is the repository's `core/retention` record's value, and
	// OverrideID that record's id, both absent where none stands.
	Override   string `json:"override,omitempty"`
	OverrideID string `json:"overrideId,omitempty"`
	// Effective is the one that applies: the override, else the declared,
	// else all.
	Effective string `json:"effective"`
}

// RetentionChange is one kind whose history an upgrade would keep for less
// time: its effective retention before and after, each `all`, `none` or an
// ISO 8601 duration (decision 0282).
type RetentionChange struct {
	Kind string `json:"kind"`
	From string `json:"from"`
	To   string `json:"to"`
}
