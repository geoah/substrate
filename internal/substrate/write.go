package substrate

// PutInput is the one create/upsert mutation's input. ID is the writer's own
// key — supply it and the put is a primary-key upsert, which is what makes a
// re-sync free; omit it and the server assigns a random one. A type some
// mapping points at is always server-assigned, so an id there is refused
// .
type PutInput struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`

	// Properties carries everything authored — `title`, `body` and the
	// temporal properties among the declared ones. A state property may name
	// only the state creations are born into; transitions happen through
	// Patch (MODEL §11.4).
	Properties  map[string]any `json:"properties,omitempty"`
	Labels      map[string]any `json:"labels,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`

	IfVersion *int64 `json:"ifVersion,omitempty"`
}

// PatchInput mutates in place. Maps merge key-wise (a null value deletes
// the key); nil fields are untouched. A state property named in Properties
// is a machine transition (MODEL §11.4); anyone may perform any declared
// transition.
type PatchInput struct {
	Properties  map[string]any `json:"properties,omitempty"`
	Labels      map[string]any `json:"labels,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`

	AddFinalizers    []string `json:"addFinalizers,omitempty"`
	RemoveFinalizers []string `json:"removeFinalizers,omitempty"`

	IfVersion *int64 `json:"ifVersion,omitempty"`
}
