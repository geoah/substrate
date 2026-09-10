package substrate

// PutInput is the one create/upsert mutation's input. ID is the writer's own
// key — supply it and the put is a primary-key upsert, which is what makes a
// re-sync free; omit it and the server assigns a random one. A type some
// mapping points at is always server-assigned, so an id there is refused
// .
type PutInput struct {
	// Kind is omitted when empty: a REST body never needs it, the collection
	// path names the kind and the handler overwrites whatever the body said.
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id,omitempty"`

	// Properties carries everything authored — `title`, `body` and the
	// temporal properties among the declared ones. A state property may name
	// only the state creations are born into; transitions happen through
	// Patch.
	Properties  map[string]any `json:"properties,omitempty"`
	Labels      map[string]any `json:"labels,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`

	IfVersion *int64 `json:"ifVersion,omitempty"`
}

// PatchInput mutates in place. Maps merge key-wise (a null value deletes
// the key); nil fields are untouched. A state property named in Properties
// is a machine transition; anyone may perform any declared
// transition.
type PatchInput struct {
	Properties  map[string]any `json:"properties,omitempty"`
	Labels      map[string]any `json:"labels,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`

	AddFinalizers    []string `json:"addFinalizers,omitempty"`
	RemoveFinalizers []string `json:"removeFinalizers,omitempty"`

	IfVersion *int64 `json:"ifVersion,omitempty"`
}

// DeleteInput is a delete's precondition. IfVersion holds the tombstone to the
// version the caller read: a record edited since fails the whole delete with
// ErrConflict and stays live, as a put or patch under IfVersion would. Nil
// checks nothing. A delete addressed through a former id compares the
// canonical record, because that is the row the tombstone lands on.
type DeleteInput struct {
	IfVersion *int64 `json:"ifVersion,omitempty"`
}

// MergeInput names the two records a merge joins: identity is the (kind, id)
// pair, so the one kind travels beside both ids. WinnerVersion and
// LoserVersion each hold one participant to the version the caller read;
// either alone is a precondition on that record only, and a mismatch fails
// the whole merge with ErrConflict before anything moves.
type MergeInput struct {
	Kind   string `json:"kind"`
	Winner string `json:"winner"`
	Loser  string `json:"loser"`

	WinnerVersion *int64 `json:"winnerVersion,omitempty"`
	LoserVersion  *int64 `json:"loserVersion,omitempty"`
}

// SplitInput names the recordmerge record a split reverses. IfVersion holds
// the split to that record's version, not the pair's: the winner and the
// loser change with every edit after the merge, and a split reverts the merge
// alone, leaving those edits where they are.
type SplitInput struct {
	Merge string `json:"merge"`

	IfVersion *int64 `json:"ifVersion,omitempty"`
}
