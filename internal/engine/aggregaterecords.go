package engine

// The records' source policy (aggregate.go): the records mode's windows and
// the property read's records are records, not points, and several sources
// write one night or one run as a record each. Under priority a record whose
// own span [at, endsAt) overlaps a kept record of a higher-ranked source is
// dropped, the rule the priority policy applies to amounts (decision 0161),
// before a window is made or a value reduced. Which property holds a
// record's source is the request's to name: core contracts no source on a
// record that does not bind observed.

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// checkRecordSources checks a records source policy's shape before the read
// touches the repository, and reports whether it drops anything.
func checkRecordSources(path string, s *substrate.AggregateRecordSources) (bool, error) {
	if s == nil {
		return false, nil
	}
	switch s.Policy {
	case "", substrate.RecordSourcesSeparate:
		if len(s.Order) > 0 || s.Property != "" || len(s.Match) > 0 {
			return false, refuse(path, "order, property and match rank the records of the priority policy; separate keeps every record")
		}
		return false, nil
	case substrate.RecordSourcesPriority:
	default:
		return false, refuse(path+".policy", "%q is not a policy for records; it is separate or priority", s.Policy)
	}
	if s.Property == "" {
		return false, refuse(path+".property", "name the records' property that holds their source, such as source")
	}
	if len(s.Order) == 0 {
		return false, refuse(path+".order", "priority ranks the sources the order names, first highest; name them")
	}
	for i, m := range s.Match {
		if m == "" || slices.Contains(s.Match[:i], m) {
			return false, refuse(fmt.Sprintf("%s.match[%d]", path, i), "names a declared property once, not %q", m)
		}
	}
	return true, nil
}

// recordPolicy is a records source policy as a read applies it: the SQL text
// of each record's source and of its match properties, and each named
// source's rank.
type recordPolicy struct {
	// source and match are SQL expressions over records, '' where absent;
	// match is one text holding every match property's value.
	source, match string
	rank          map[string]int
	// last is the rank of a source the order does not name.
	last int
}

// newRecordPolicy resolves a priority policy against the kinds the records
// are of: the property and every match property declared on each, the
// order's entries as the property's values (a reference's bare id completed
// with the kind it pins, as the list's filter completes it).
func newRecordPolicy(types []*vocabulary.Kind, path string, s *substrate.AggregateRecordSources) (*recordPolicy, error) {
	if len(types) == 0 {
		return nil, refuse(path, "the policy reads a property the records' kinds declare; name the kinds in the filter")
	}
	source, prop, err := propertyText(types, path+".property", s.Property)
	if err != nil {
		return nil, err
	}
	pol := &recordPolicy{source: source, rank: map[string]int{}, last: len(s.Order)}
	for i, o := range s.Order {
		v := o
		if prop.Datatype == vocabulary.DatatypeReference {
			if v, err = referenceFilterPath(fmt.Sprintf("%s.order[%d]", path, i), prop, o); err != nil {
				return nil, err
			}
		}
		if _, ok := pol.rank[v]; !ok {
			pol.rank[v] = i
		}
	}
	pol.match = `'[]'`
	if len(s.Match) > 0 {
		exprs := make([]string, len(s.Match))
		for i, m := range s.Match {
			if exprs[i], _, err = propertyText(types, fmt.Sprintf("%s.match[%d]", path, i), m); err != nil {
				return nil, err
			}
		}
		pol.match = `jsonb_build_array(` + strings.Join(exprs, ", ") + `)::text`
	}
	return pol, nil
}

// rankOf is a source value's rank, first highest.
func (pol *recordPolicy) rankOf(source string) int {
	if r, ok := pol.rank[source]; ok {
		return r
	}
	return pol.last
}

// propertyText is the SQL text of a declared scalar string, enum or
// reference property every kind of types declares (” where a record holds
// none), with the declaration a reference's bare ids complete against: its
// pin where every kind pins one kind, none otherwise.
func propertyText(types []*vocabulary.Kind, path, name string) (string, *vocabulary.Property, error) {
	if col, _ := columnFor(name); col != "" {
		return "", nil, refuse(path, "%s is a record column; name a declared property", name)
	}
	var shape *vocabulary.Property
	for _, t := range types {
		prop, ok := t.Prop(name)
		switch {
		case !ok:
			return "", nil, refuse(path, "%s declares no property %q", t.Identity, name)
		case prop.Repeated || prop.Keyed:
			return "", nil, refuse(path, "%s on %s holds many values; name a property holding one", name, t.Identity)
		case prop.Sensitive():
			return "", nil, refuse(path, "%s on %s is sensitive, and a read never compares its values", name, t.Identity)
		}
		switch prop.Datatype {
		case vocabulary.DatatypeReference, vocabulary.DatatypeString, vocabulary.DatatypeEnum:
		default:
			return "", nil, refuse(path, "%s on %s is %s; name a string, an enum or a reference", name, t.Identity, prop.Datatype)
		}
		if shape == nil {
			shape = &vocabulary.Property{Datatype: prop.Datatype, To: prop.To}
			continue
		}
		if (shape.Datatype == vocabulary.DatatypeReference) != (prop.Datatype == vocabulary.DatatypeReference) {
			return "", nil, refuse(path, "%s is a reference on one filtered kind and not on another", name)
		}
		if shape.To != prop.To {
			shape.To = ""
		}
	}
	if shape.Datatype == vocabulary.DatatypeReference {
		return `coalesce(` + referencePathSQL("props", name) + `, '')`, shape, nil
	}
	return `coalesce(props->>` + sqlLiteral(name) + `, '')`, shape, nil
}

// policyRecord is one record a priority policy weighs: its own span, its
// source's rank and its match values. A record with no `at` has no span
// (placed false): it overlaps nothing, so it drops nothing and is kept.
type policyRecord struct {
	start, end time.Time
	placed     bool
	rank       int
	match      string
}

// newPolicyRecord is a record's place in the policy: [at, endsAt), an
// instant's [at, at+1ns), so a record at one instant inside another's span
// overlaps it.
func newPolicyRecord(pol *recordPolicy, at, endsAt sql.NullTime, source, match string) policyRecord {
	r := policyRecord{rank: pol.rankOf(source), match: match}
	if !at.Valid {
		return r
	}
	r.start, r.end, r.placed = at.Time, at.Time.Add(time.Nanosecond), true
	if endsAt.Valid && endsAt.Time.After(at.Time) {
		r.end = endsAt.Time
	}
	return r
}

// widen grows the span [lo, hi] to hold the record's.
func (r policyRecord) widen(lo, hi *time.Time) {
	if !r.placed {
		return
	}
	if lo.IsZero() || r.start.Before(*lo) {
		*lo = r.start
	}
	if r.end.After(*hi) {
		*hi = r.end
	}
}

// keepBySource answers which records the priority policy keeps: by rank,
// first highest, a record overlapping a kept record of a higher rank with the
// same match values is dropped, so a record dropped drops nothing. Records of
// one rank never drop each other. Each record compared is a step (check).
func keepBySource(ctx context.Context, recs []policyRecord) ([]bool, error) {
	keep := make([]bool, len(recs))
	order := make([]int, len(recs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return recs[a].rank - recs[b].rank })
	var steps everyThousand
	check := func() error { return steps.check(ctx) }
	higher := map[string][][2]time.Time{}
	covers := map[string]cover{}
	for lo := 0; lo < len(order); {
		hi := lo
		for hi < len(order) && recs[order[hi]].rank == recs[order[lo]].rank {
			hi++
		}
		for _, i := range order[lo:hi] {
			if err := check(); err != nil {
				return nil, err
			}
			r := recs[i]
			keep[i] = !r.placed || !covers[r.match].overlaps(r.start, r.end)
		}
		changed := map[string]bool{}
		for _, i := range order[lo:hi] {
			if r := recs[i]; keep[i] && r.placed {
				higher[r.match] = append(higher[r.match], [2]time.Time{r.start, r.end})
				changed[r.match] = true
			}
		}
		for m := range changed {
			c, err := joinSpans(ctx, higher[m], check)
			if err != nil {
				return nil, err
			}
			covers[m] = c
			higher[m] = append([][2]time.Time(nil), c...)
		}
		lo = hi
	}
	return keep, ctx.Err()
}

// policyContext reads the records a priority policy weighs beside the
// records a read matches: those of the filter (base, without the range)
// outside the range (the clause outside adds) whose span overlaps the
// matched records' [lo, hi], so a night whose higher-ranked copy starts
// before the range is still the higher copy's. They drop records and are
// never counted themselves; past limit the read is refused naming path.
func policyContext(ctx context.Context, tx *sql.Tx, base *builder, pol *recordPolicy, outside func(b *builder) string, lo, hi time.Time, limit int, path string) ([]policyRecord, error) {
	b := base.clone()
	b.add(outside(b))
	b.add(`at <= ` + b.arg(hi) + ` AND coalesce(ends_at, at) >= ` + b.arg(lo))
	rows, err := tx.QueryContext(ctx, `SELECT at, ends_at, `+pol.source+`, `+pol.match+` FROM records WHERE `+strings.Join(b.where, " AND ")+
		` ORDER BY at, kind, id LIMIT `+b.arg(limit+1), b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate source policy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []policyRecord
	for rows.Next() {
		var at, endsAt sql.NullTime
		var source, match string
		if err := rows.Scan(&at, &endsAt, &source, &match); err != nil {
			return nil, err
		}
		if len(out) == limit {
			return nil, refuse(path, "the policy weighs more than %d records around the range beside the ones it matches; narrow the filter", limit)
		}
		out = append(out, newPolicyRecord(pol, at, endsAt, source, match))
	}
	return out, rows.Err()
}

// clone copies a builder, so a second statement adds its own clauses and
// arguments to the same filter.
func (b *builder) clone() *builder {
	c := *b
	c.args = slices.Clone(b.args)
	c.where = slices.Clone(b.where)
	return &c
}
