package substrate

// KindInfo is the projection of one declared type the read surfaces use.
type KindInfo struct {
	Identity   string         `json:"identity"` // "<authority>/<name>"
	Name       string         `json:"name"`
	Authority  string         `json:"authority"`
	Version    string         `json:"version"`
	Plural     string         `json:"plural"`
	Source     string         `json:"source"` // "builtin" | "installed"
	Definition map[string]any `json:"definition"`
}
