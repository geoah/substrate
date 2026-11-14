package substrate

import "time"

// PropertyMark is where one property's value came from, and whether sync
// may replace it (decision 0284). The value stays in the record's
// properties; the mark sits beside it under the same name, which the CLI
// and the console render as `status.properties`.
//
// Pinned and the rest are two facts stored apart: Pinned says sync leaves the
// value alone, and SetBy, OnBehalfOf, DerivedFrom and SetAt say where the
// value came from. Pinned is stored when the value is written, and never
// worked out again: a direct write by a person, an app, a function or an
// agent pins, one by a package or the engine does not, and `pin` and
// `release` set it explicitly.
type PropertyMark struct {
	Pinned bool `json:"pinned,omitempty"`
	// SetBy is the actor record that wrote the value directly; absent on a
	// value sync wrote.
	SetBy *RecordLink `json:"setBy,omitempty"`
	// OnBehalfOf is the actor record SetBy acted for, where it acted for one.
	OnBehalfOf *RecordLink `json:"onBehalfOf,omitempty"`
	// DerivedFrom is every live source record whose offer backs the value:
	// the one offering it (several where they agree), or, on a union, each
	// contributing one. Empty where the value came from no source record, or
	// its last source has gone.
	DerivedFrom []RecordLink `json:"derivedFrom,omitempty"`
	// SetAt is when the value was set: not a later pin, and not a change in
	// the alternatives.
	SetAt time.Time `json:"setAt,omitzero"`
	// Alternatives is what each other live source record offers, where it
	// differs from the stored value.
	Alternatives []PropertyAlternative `json:"alternatives,omitempty"`
}

// PropertyAlternative is what one live source record offers for a property
// whose stored value differs from it: "the work address book says X" beside
// a pinned value. Alternatives are keyed by source record, so two accounts of
// one provider show as two. Adopting one is releasing the property, or
// writing the value.
type PropertyAlternative struct {
	// DerivedFrom is the source record the offer is read from. Absent only
	// where a read's kind grant hides that record's kind (MarksWithinKinds).
	DerivedFrom *RecordLink `json:"derivedFrom,omitempty"`
	Value       any         `json:"value"`
	// UpdatedAt is the source record's last change.
	UpdatedAt time.Time `json:"updatedAt"`
}

// RecordLink links one record the way a stored reference does:
// `{ref: <kind>/<id>}`.
type RecordLink struct {
	Ref string `json:"ref"`
}

// LinkTo is the link to the record at path ("<kind>/<id>"), nil for none.
func LinkTo(path string) *RecordLink {
	if path == "" {
		return nil
	}
	return &RecordLink{Ref: path}
}

// MarkInput names the properties a pin or a release acts on, and the version
// the caller read: a record written since fails the action whole.
type MarkInput struct {
	Properties []string `json:"properties"`
	IfVersion  *int64   `json:"ifVersion,omitempty"`
}

// MarksWithinKinds holds a record's marks to a kind grant: every source
// record a mark or an alternative links, whose kind allowPath refuses, is
// left out, while the value and the stamps stay. A source names a record of
// ANOTHER kind (record 0094), so the read gates that hold `linkedFrom` to the
// allowlist (record 0088, rule 5) hold this the same way, each splitting the
// path with the kind grammar it already holds. The map is returned as given
// when every path is admitted, and copied where one is not, because the
// caller's record may be shared.
func MarksWithinKinds(marks map[string]PropertyMark, allowPath func(path string) bool) map[string]PropertyMark {
	refused := func(l *RecordLink) bool { return l != nil && !allowPath(l.Ref) }
	dirty := false
	for _, m := range marks {
		for i := range m.DerivedFrom {
			dirty = dirty || refused(&m.DerivedFrom[i])
		}
		for _, a := range m.Alternatives {
			dirty = dirty || refused(a.DerivedFrom)
		}
	}
	if !dirty {
		return marks
	}
	out := make(map[string]PropertyMark, len(marks))
	for name, m := range marks {
		if len(m.DerivedFrom) > 0 {
			var kept []RecordLink
			for _, l := range m.DerivedFrom {
				if !refused(&l) {
					kept = append(kept, l)
				}
			}
			m.DerivedFrom = kept
		}
		if len(m.Alternatives) > 0 {
			alts := make([]PropertyAlternative, len(m.Alternatives))
			for i, a := range m.Alternatives {
				if refused(a.DerivedFrom) {
					a.DerivedFrom = nil
				}
				alts[i] = a
			}
			m.Alternatives = alts
		}
		out[name] = m
	}
	return out
}
