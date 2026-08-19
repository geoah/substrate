package substrate

import "time"

// Record is the wire shape of one stored record. Annotations and Edges are
// populated only when requested (list calls omit them by default).
//
// EVERYTHING AUTHORED IS A PROPERTY: `title`, `body` and the
// temporal properties read back inside Properties beside the declared ones,
// so the JSON shows one map. The Go fields below carry the same values for
// callers that hold an Record in-process, and are not serialized.
type Record struct {
	ID string `json:"id"`
	// Kind is the record's kind REFERENCE, always authority-qualified
	// (decision 0042): "calendar.substrate.reamde.dev/calendarevent".
	Kind string `json:"kind"`
	// CanonicalID is set only when the read was addressed by a FORMER id: a
	// merged-away record's id still resolves, and the answer says which
	// record it resolved to (the canonical-id contract, proposal §6.3).
	CanonicalID string `json:"canonicalId,omitempty"`
	// FormerIDs lists the ids merges fused into this one, flattened and
	// server-set. A former id is this record's own discarded name.
	FormerIDs []string `json:"formerIds,omitempty"`

	Title string `json:"-"`
	Body  string `json:"-"`

	// Trait-backed hot columns (RFC 3339). Which of these a type uses
	// is declared by its capabilities; unmapped ones are nil.
	At     *time.Time `json:"-"`
	EndsAt *time.Time `json:"-"`
	DueAt  *time.Time `json:"-"`

	// Properties carries every declared value slot, state properties
	// included: a machine is a property of type `state` and its current
	// state reads back here like any other value (MODEL §11.4).
	Properties  map[string]any `json:"properties"`
	Labels      map[string]any `json:"labels"`
	Annotations map[string]any `json:"annotations,omitempty"`

	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	DeletedAt  *time.Time `json:"deletedAt,omitempty"`
	Finalizers []string   `json:"finalizers,omitempty"`

	Edges map[string][]EdgeTarget `json:"edges,omitempty"`

	// PropertyMeta is per-property provenance: the manager
	// the ledger names, when it changed, and the live offers whose value
	// differs from the stored one. Populated on SINGLE-RECORD reads only —
	// lists and changes never carry it.
	PropertyMeta map[string]PropertyMeta `json:"propertyMeta,omitempty"`
}
