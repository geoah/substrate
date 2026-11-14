package substrate

import "time"

// Record is the wire shape of one stored record. Annotations are populated
// only when requested (list calls omit them by default).
//
// EVERYTHING AUTHORED IS A PROPERTY: `title`, `body` and the
// temporal properties read back inside Properties beside the declared ones,
// so the JSON shows one map. The Go fields below carry the same values for
// callers that hold an Record in-process, and are not serialized.
type Record struct {
	ID string `json:"id"`
	// Kind is the record's kind REFERENCE, always fully qualified
	// (decision 0042): "samples.substrate.reamde.dev/calendar/calendarevent".
	Kind string `json:"kind"`
	// CanonicalID is set only when the read was addressed by a FORMER id: a
	// merged-away record's id still resolves, and the answer says which
	// record it resolved to (the canonical-id contract).
	CanonicalID string `json:"canonicalId,omitempty"`
	// FormerIDs lists the ids merges fused into this one, flattened and
	// server-set. A former id is this record's own discarded name.
	FormerIDs []string `json:"formerIds,omitempty"`

	Title string `json:"-"`

	// Trait-backed hot columns (RFC 3339). Which of these a type uses
	// is declared by its capabilities; unmapped ones are nil.
	At     *time.Time `json:"-"`
	EndsAt *time.Time `json:"-"`
	DueAt  *time.Time `json:"-"`

	// Properties carries every declared value slot, state properties
	// included: a machine is a property of type `state` and its current
	// state reads back here like any other value.
	Properties  map[string]any `json:"properties"`
	Labels      map[string]any `json:"labels"`
	Annotations map[string]any `json:"annotations,omitempty"`

	// Version is the edit counter a write asserts with IfVersion.
	Version int64 `json:"version"`
	// KindVersion is the effective version of the kind declaration (its own
	// pin, else its package's) under which the record's data was last
	// written. Absent (0) on a record not written since the stamp existed
	// (decision 0060). Server-set; ignored on input.
	KindVersion int64      `json:"kindVersion,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
	Finalizers  []string   `json:"finalizers,omitempty"`
	// Computed marks an occurrence the window read derived from a series'
	// rule rather than a stored row: the series' kind, the id
	// `<seriesId>_<slot>`, the series' properties with the slot in the
	// temporal columns and `recurrenceOf`/`originalAt` filled, version 0.
	// Writing the envelope back at its id materializes it as an override.
	// The CLI and the console project it under the envelope's `status`.
	Computed bool `json:"computed,omitempty"`

	// Created is set on the record a write answers when that write created
	// it. Never on the wire: the HTTP status says it, and a version cannot,
	// because a write that changes nothing leaves a fresh row at 1.
	Created bool `json:"-"`

	// Marks is each property's mark: whether sync may replace the value,
	// where it came from, and the live offers whose value differs from the
	// stored one (decision 0284). Populated on SINGLE-RECORD reads only —
	// lists and changes never carry it.
	Marks map[string]PropertyMark `json:"marks,omitempty"`

	// LinkedFrom is the source records whose mapping-owned subject slot
	// points at this one: the mirrors that converged on this subject, one
	// entry each, ordered by kind then id. Populated on SINGLE-RECORD reads
	// only, and ABSENT — never an empty array — on a kind no recordmapping
	// targets, so a reader can tell "nothing maps onto this kind" from
	// "nothing has linked yet" (decision 0088).
	LinkedFrom []LinkedRecord `json:"linkedFrom,omitempty"`

	// Erasure is what remains of erasing the record's history, on the reply
	// of a permanent delete alone (decision 0304).
	Erasure *Erasure `json:"erasure,omitempty"`
}

// Erasure is where erasing one record's history stands (decision 0304): a
// permanent delete erases all of it, a forget all but the record's current
// state. Pending is false once it is done; until then Waits names the
// triggers it waits for and Held anything else that holds it.
type Erasure struct {
	Kind    string        `json:"kind"`
	ID      string        `json:"id"`
	Live    bool          `json:"live"`
	Pending bool          `json:"pending"`
	Since   *time.Time    `json:"since,omitempty"`
	Waits   []ErasureWait `json:"waits,omitempty"`
	Held    string        `json:"held,omitempty"`
	// Damaged is the line that does not verify the erasure is held on: it
	// waits for that row to be written back from a whole copy.
	Damaged int64 `json:"damaged,omitempty"`
	// Purged is every other record a permanent delete purged and erased
	// with this one, by record path: the change requests and the merge and
	// split records that named it, the records merged into it.
	Purged []string `json:"purged,omitempty"`
	// Tombstoned is how many records the record owned that its delete
	// tombstoned for the collector's sweep, as a delete's cascade does: they
	// are deleted with it and keep their own history until old deletions are
	// forgotten.
	Tombstoned int `json:"tombstoned,omitempty"`
}

// ErasureWait is an enabled trigger an erasure waits for: it watches the
// record's kind, has not yet been delivered the deletion, and is delivering.
type ErasureWait struct {
	Trigger string    `json:"trigger"`
	Cursor  int64     `json:"cursor"`
	Since   time.Time `json:"since"`
}

// ForgetInput is a forget's request: Finish passes every trigger the
// erasure would wait for.
type ForgetInput struct {
	Finish bool `json:"finish,omitempty"`
}

// LinkedRecord is one inbound mapping-owned link: the source record, and
// which mapping owns the slot it points from. Ref is the source's record
// path, "<kind>/<id>", and Kind its kind reference — the same kind Ref
// carries, said on its own because grouping by it is what a reader does with
// this list. Title is the source's display title, empty where it has none.
type LinkedRecord struct {
	Ref      string `json:"ref"`
	Kind     string `json:"kind"`
	Title    string `json:"title,omitempty"`
	Property string `json:"property"`
	// Mapping is the recordmapping's identity, "<authority>/<package>/<name>".
	Mapping string `json:"mapping"`
}
