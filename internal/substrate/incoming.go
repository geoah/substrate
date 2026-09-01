package substrate

// IncomingReference is one reverse pointer: some other live record's reference
// property names this record. Property is that property's declared name and
// Path addresses the site inside it, so a nested pointer says where it sits
// rather than reading as a second property of the same name.
type IncomingReference struct {
	Property string `json:"property"`
	// Path is the dotted address of a nested reference site
	// ("tools.fields.callable"), empty for a kind's own property.
	Path string `json:"path,omitempty"`
	// From is the source record, shallow by design: a reverse row names what
	// points here, and the reader fetches it if they want more. A reader that
	// wants the source's timestamps reads the source: the reverse row carries
	// no time of its own, because the refs index stores none (migration 0011).
	From IncomingSource `json:"from"`
}

// IncomingSource is the record end of a reverse pointer, shallow by design.
type IncomingSource struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
}

// IncomingOptions narrows and pages a reverse read. Property and FromKind are
// what let a drill-down expand ONE group without pulling the rest: a record with
// a thousand inbound rows across five properties is five small reads, not one
// large one.
type IncomingOptions struct {
	First int
	After string
	// Property narrows to one reference property's name.
	Property string
	// FromKind narrows to one source kind, by full identity.
	FromKind string
}

// IncomingPage is one page of references pointing at a record. They are a
// derived view, not part of the record manifest, so they page on their own
// resource.
type IncomingPage struct {
	Incoming []IncomingReference `json:"incoming"`
	Cursor   string              `json:"cursor,omitempty"`
	Total    int                 `json:"total"`
}
