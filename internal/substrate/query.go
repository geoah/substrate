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

// EdgeFilter is the one-hop edge predicate: records with an edge Rel
// (empty = any rel) pointing at To. ToType narrows the target to one type —
// identity is the (type, id) pair, so two types may hold the same id.
type EdgeFilter struct {
	Rel    string `json:"rel,omitempty"`
	To     string `json:"to"`
	ToKind string `json:"toKind,omitempty"`
}

// Filter is the grammar of the one generic records query. Filterable ≡
// indexed ≡ declared. State properties filter through Properties like every
// other property — there is no separate `states` arm (MODEL §11.4).
type Filter struct {
	Kinds []string `json:"kinds,omitempty"`
	// Implements narrows to a capability or machine interface, cross-authority. It
	// INTERSECTS with Types rather than unioning: every filter arm narrows, so
	// a collection read (whose Types the path forces) never answers with a row
	// outside its own collection. Alone, it means every implementor.
	Implements string          `json:"implements,omitempty"`
	IDs        []string        `json:"ids,omitempty"`
	Properties map[string]Cond `json:"properties,omitempty"`
	Labels     map[string]Cond `json:"labels,omitempty"`
	Edge       *EdgeFilter     `json:"edge,omitempty"`
	// Deleted: nil = only live records (the default), true = only
	// tombstoned, false = only live.
	Deleted *bool `json:"deleted,omitempty"`
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
	// WithEdges/WithAnnotations opt heavier data into list responses.
	WithEdges       bool `json:"withEdges,omitempty"`
	WithAnnotations bool `json:"withAnnotations,omitempty"`
}

// Page is a page of records plus continuation cursor ("" = exhausted).
// Head is the changelog's highest committed seq at the snapshot this page was
// read from: a client that Lists then opens `watch?from={head}` sees every
// subsequent change with neither a gap nor a double-see. It is
// always emitted, 0 included (an empty changelog), so the list→watch handoff
// is never ambiguous.
type Page struct {
	Records []*Record `json:"records"`
	Cursor  string    `json:"cursor,omitempty"`
	Head    int64     `json:"head"`
}
