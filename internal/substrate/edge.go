package substrate

// IncomingEdge is one reverse edge: some other live record points here with
// rel. From is the shallow source, shaped like an outgoing edge's target. Its
// Properties are never set because a reverse row names only the source.
type IncomingEdge struct {
	Rel  string     `json:"rel"`
	From EdgeTarget `json:"from"`
}

// IncomingPage is one page of edges pointing at a record. Incoming edges are
// a derived graph view, not part of the record manifest, so they page on their
// own resource.
type IncomingPage struct {
	Incoming []IncomingEdge `json:"incoming"`
	Cursor   string         `json:"cursor,omitempty"`
	Total    int            `json:"total"`
}

// EdgeTarget is one end of a traversed edge, shallow by design.
type EdgeTarget struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
	// Properties are the EDGE's properties, not the target's.
	Properties map[string]any `json:"properties,omitempty"`
}

// EdgeRef names the destination of an edge in a write: `{kind, id}` — a
// record reference, split into its two parts so a writer never re-parses it.
// Bare `{id}` is input shorthand on a single-target edge, where the
// declaration already says which kind it points at; a `to: any` edge requires
// the kind. Nothing matches by value — there is no alias and no identifier
// bundle.
type EdgeRef struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id"`
}

// Identity reports the kind reference the target names, "" when it carries
// only an id.
func (r EdgeRef) Identity() string { return r.Kind }

// Ref renders the record reference this target names — "<authority>/<kind>/<id>"
// or "<kind>/<id>", and just the id when the edge declaration implies the kind.
func (r EdgeRef) Ref() string {
	if r.Kind == "" {
		return r.ID
	}
	return r.Kind + "/" + r.ID
}

// EdgeInput declares one edge on a put.
type EdgeInput struct {
	Rel        string         `json:"rel"`
	To         EdgeRef        `json:"to"`
	Properties map[string]any `json:"properties,omitempty"`
}
