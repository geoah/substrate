package substrate

import "time"

// PropertyMeta is one property's provenance: who manages it, at which tier,
// since when, and what every other live source says it should be. The CLI
// and console render it as `status.properties`. Tier says whether the hold
// is real: `machine` means recompute may replace the value; `owner` and
// `bundle` mean it yields — a bundle pin is a function's write,
// visible here instead of a silent recompute freeze.
type PropertyMeta struct {
	Manager   string    `json:"manager,omitempty"`
	Tier      Tier      `json:"tier,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	// Source is the record path ("<kind>/<id>") of the live source record
	// the stored value came from: present only where the manager holds at
	// the machine tier and the manager's own offer backs the stored value,
	// so a hand edit, a bundle pin and a machine write no source offers
	// carry none (record 0094).
	Source       string                `json:"source,omitempty"`
	Alternatives []PropertyAlternative `json:"alternatives,omitempty"`
}

// PropertyAlternative is one live mapping-source offer that disagrees with
// the stored value — "Google says X" beside a held property. Adopting one is
// just writing it.
type PropertyAlternative struct {
	Actor     string    `json:"actor"`
	Value     any       `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Source is the record path of the source record the offer is read
	// from: the one whose value this is (atomic), or the latest of the
	// actor's sources carrying the property (union), the same record the
	// stamp is taken from. Absent on a row derived before the column
	// existed, until its target next recomputes.
	Source string `json:"source,omitempty"`
}

// MetaWithinKinds holds a record's propertyMeta to a kind grant: every
// `source` path allowPath refuses is blanked, on the manager and on each
// alternative alike, while the value, the actor and the stamp stay as they
// were before the source was on the wire. A source names a record of ANOTHER
// kind (record 0094), so the read gates that hold `linkedFrom` to the
// allowlist (record 0088, rule 5) hold this the same way, each splitting the
// path with the kind grammar it already holds. The map is returned as given
// when every path is admitted, and copied where one is not, because the
// caller's record may be shared.
func MetaWithinKinds(meta map[string]PropertyMeta, allowPath func(path string) bool) map[string]PropertyMeta {
	refused := func(path string) bool { return path != "" && !allowPath(path) }
	dirty := false
	for _, m := range meta {
		if refused(m.Source) {
			dirty = true
			break
		}
		for _, a := range m.Alternatives {
			if refused(a.Source) {
				dirty = true
				break
			}
		}
	}
	if !dirty {
		return meta
	}
	out := make(map[string]PropertyMeta, len(meta))
	for name, m := range meta {
		if refused(m.Source) {
			m.Source = ""
		}
		if len(m.Alternatives) > 0 {
			alts := make([]PropertyAlternative, len(m.Alternatives))
			for i, a := range m.Alternatives {
				if refused(a.Source) {
					a.Source = ""
				}
				alts[i] = a
			}
			m.Alternatives = alts
		}
		out[name] = m
	}
	return out
}
