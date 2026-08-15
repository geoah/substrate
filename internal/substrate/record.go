package substrate

import "time"

// Record is the wire shape of one stored record. Annotations and Edges are
// populated only when requested (list calls omit them by default).
//
// EVERYTHING AUTHORED IS A PROPERTY: `body` and the temporal properties read
// back inside Properties beside the declared ones, so the JSON shows one map.
// The Go fields below carry the same values for callers that hold a Record
// in-process, and are not serialized.
//
// `title` is the ONE exception, and it is why it sits at the top level — see
// Title.
type Record struct {
	ID string `json:"id"`
	// Kind is the record's kind REFERENCE: "calendar.substrate.reamde.dev/calendarevent"
	// for a published kind, bare "task" for a repository-local one.
	Kind string `json:"kind"`
	// CanonicalID is set only when the read was addressed by a FORMER id: a
	// merged-away record's id still resolves, and the answer says which
	// record it resolved to (the canonical-id contract, proposal §6.3).
	CanonicalID string `json:"canonicalId,omitempty"`
	// FormerIDs lists the ids merges fused into this one, flattened and
	// server-set. A former id is this record's own discarded name.
	FormerIDs []string `json:"formerIds,omitempty"`

	// Title is the record's DISPLAY title: what a row, a chip or a page header
	// calls this record. It is at the top level and not in Properties because
	// for most kinds it is not a property at all — a kind with a
	// `displayTemplate` derives it, and the derived value used to be injected
	// into the property map, where it sat indistinguishable from a declared
	// one and OVERWROTE a kind that declared `title` itself.
	//
	// A kind with NO displayTemplate is the other case: there `title` is the
	// built-in authored slot, it is written through `properties.title` like
	// any other property, and it reads back in Properties as well as here.
	// Same value, two honest meanings — the property is what was authored, and
	// this is what to render.
	Title string `json:"title,omitempty"`
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
