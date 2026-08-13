package substrate

// KindInfo is the projection of one declared type the read surfaces use.
type KindInfo struct {
	Identity  string `json:"identity"` // "<authority>/<name>"
	Name      string `json:"name"`
	Authority string `json:"authority"`
	Version   string `json:"version"`
	Plural    string `json:"plural"`
	Source    string `json:"source"` // "builtin" | "installed"
	// Description is what the kind is for, as its declaration says it: the
	// line a reader gets above the collection, empty when undeclared.
	Description string         `json:"description"`
	Definition  map[string]any `json:"definition"`
}
