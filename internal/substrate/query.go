package substrate

// Cond is one property/state/label predicate. Exactly the operators the
// schema declares for the property's type are legal; others error.
type Cond struct {
	Eq       any    `json:"eq,omitempty"`
	In       []any  `json:"in,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Gt       any    `json:"gt,omitempty"`
	Gte      any    `json:"gte,omitempty"`
	Lt       any    `json:"lt,omitempty"`
	Lte      any    `json:"lte,omitempty"`
	Contains any    `json:"contains,omitempty"`
	Exists   *bool  `json:"exists,omitempty"`
}

// Filter is the grammar of the one generic records query. Filterable ≡
// indexed ≡ declared. State properties filter through Properties like every
// other property: there is no separate `states` arm.
type Filter struct {
	Kinds []string `json:"kinds,omitempty"`
	// Implements narrows to a trait or machine interface, cross-authority. It
	// INTERSECTS with Kinds rather than unioning: every filter arm narrows, so
	// a list that names its kinds never answers with a row of another. Alone,
	// it means every implementor.
	Implements string          `json:"implements,omitempty"`
	IDs        []string        `json:"ids,omitempty"`
	Properties map[string]Cond `json:"properties,omitempty"`
	Labels     map[string]Cond `json:"labels,omitempty"`
	// Deleted: nil = only live records (the default), true = only
	// tombstoned, false = only live.
	Deleted *bool `json:"deleted,omitempty"`
	// Referencing narrows to the records holding a reference AT one record:
	// the reverse read, as a predicate over the refs index rather than a
	// sub-resource of its own. The target is matched by its canonical id and
	// every former id, so a pointer written before a merge still counts.
	Referencing *Referencing `json:"referencing,omitempty"`
}

// Referencing names the target of a reverse read. Ref is the target's record
// path, "<kind>/<id>"; Property, when set, narrows to one reference property
// of the pointing records.
type Referencing struct {
	Ref      string `json:"ref"`
	Property string `json:"property,omitempty"`
}

// ReferenceSite is one place a record points at the referencing target: the
// declared property, and the dotted address of a nested site
// ("tools.fields.callable"), empty for a kind's own property.
type ReferenceSite struct {
	Property string `json:"property"`
	Path     string `json:"path,omitempty"`
}

// Order is one sort key; Property may be a declared property, a hot property
// ("at", "endsAt", "dueAt"), or "createdAt"/"updatedAt". One casing rule: the
// snake spellings are errors, and the error names the replacement.
type Order struct {
	Property string `json:"property"`
	Desc     bool   `json:"desc,omitempty"`
}

// Query is the paged list request.
type Query struct {
	Filter  Filter  `json:"filter"`
	OrderBy []Order `json:"orderBy,omitempty"`
	First   int     `json:"first,omitempty"` // default 50, max 500
	After   string  `json:"after,omitempty"` // opaque cursor
	// WithAnnotations opts heavier data into list responses.
	WithAnnotations bool `json:"withAnnotations,omitempty"`
	// Expand names reference properties whose referents the page carries in
	// Included, one hop. Each must be a declared reference property of a kind
	// the filter admits; a name no admitted kind declares is a validation
	// error, because a silently ignored expansion reads as a dangling graph.
	Expand []string `json:"expand,omitempty"`
}

// Page is a page of records plus continuation cursor ("" = exhausted).
// Head is the changelog's highest committed seq at the snapshot this page was
// read from, and Generation the history generation it belongs to: a client
// that Lists then opens `watch?from={head}&generation={generation}` sees every
// subsequent change with neither a gap nor a double-see. Head is
// always emitted, 0 included (an empty changelog), so the list→watch handoff
// is never ambiguous.
type Page struct {
	Records    []*Record `json:"records"`
	Cursor     string    `json:"cursor,omitempty"`
	Head       int64     `json:"head"`
	Generation string    `json:"generation"`
	// Included holds the referents Query.Expand asked for, keyed by record
	// path, each once however many rows point at it. A dangling pointer has
	// no entry; the row's own reference value still says where it pointed.
	Included map[string]*Record `json:"included,omitempty"`
	// Matches is set on a Filter.Referencing read: for each record on the
	// page, keyed by its record path, every site at which it points at the
	// target. One source can point from two sites, so a page of distinct
	// records needs this beside it to say which properties matched.
	Matches map[string][]ReferenceSite `json:"matches,omitempty"`
}

// RankedPage is a ranked read's answer: the records in rank order, each one's
// per-arm scores keyed by record path, and how much of the semantic index is
// still being built (SearchResult.Pending). No cursor, head or generation: a
// ranking has no keyset and opens no single snapshot, so it claims none.
type RankedPage struct {
	Records []*Record         `json:"records"`
	Scores  map[string]Scores `json:"scores"`
	Pending int               `json:"pending"`
}

// Scores are one hit's raw per-arm scores: ts_rank for the lexical arm, cosine
// similarity for the semantic one, 0 where an arm did not rank.
type Scores struct {
	Lexical  float64 `json:"lexical,omitempty"`
	Semantic float64 `json:"semantic,omitempty"`
}
