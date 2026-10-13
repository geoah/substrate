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

// aggRecordOrderCap bounds a records source policy's order.
const aggRecordOrderCap = 100

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
	// The policy weighs the records once per rank they hold, so the ranks
	// are bounded.
	if len(s.Order) > aggRecordOrderCap {
		return false, refuse(path+".order", "an order ranks at most %d sources, not %d", aggRecordOrderCap, len(s.Order))
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

// above is the order's values ranked higher than rank, sorted.
func (pol *recordPolicy) above(rank int) []string {
	var out []string
	for v, r := range pol.rank {
		if r < rank {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// propertyText is the SQL text of a declared scalar string, enum or
// reference property every kind of types declares (empty where a record
// holds none), with the declaration a reference's bare ids complete
// against: its pin where every kind pins one kind, none otherwise.
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
// (placed false): it overlaps nothing, so it drops nothing and is kept. A
// record with no `endsAt` after its `at` is an instant: a span holding its
// `at`, or an instant at the same `at`, drops it, and it drops only an
// instant, since it has no span to hold another record's. A night a device
// wrote while it was still going on, before it had an end, therefore never
// drops another source's whole night.
type policyRecord struct {
	start, end      time.Time
	placed, instant bool
	rank            int
	match           string
}

// newPolicyRecord is a record's place in the policy: [at, endsAt), an
// instant's [at, at+1ns), so a span holding the instant overlaps it.
func newPolicyRecord(pol *recordPolicy, at, endsAt sql.NullTime, source, match string) policyRecord {
	r := policyRecord{rank: pol.rankOf(source), match: match}
	if !at.Valid {
		return r
	}
	r.start, r.end, r.placed, r.instant = at.Time, at.Time.Add(time.Nanosecond), true, true
	if endsAt.Valid && endsAt.Time.After(at.Time) {
		r.end, r.instant = endsAt.Time, false
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
// one rank never drop each other, and an instant drops only an instant at its
// own `at` (policyRecord). Each record compared is a step (check).
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
	instants := map[string]map[int64]bool{}
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
			keep[i] = !r.placed || !covers[r.match].overlaps(r.start, r.end) && !(r.instant && instants[r.match][r.start.UnixNano()])
		}
		changed := map[string]bool{}
		for _, i := range order[lo:hi] {
			switch r := recs[i]; {
			case !keep[i] || !r.placed:
			case r.instant:
				if instants[r.match] == nil {
					instants[r.match] = map[int64]bool{}
				}
				instants[r.match][r.start.UnixNano()] = true
			default:
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

// policyContext reads the records a priority policy weighs beside the ones
// a read matches (matched): those of the filter (base, without the range)
// outside it (the clause outside adds) that may drop a matched record. Only
// a kept record of a higher rank drops one, and whether it is kept turns on
// the records of a higher rank still that meet it, so the read takes every
// record ranked above the matched ones that meets their [lo, hi], then
// every record ranked above those that meets theirs, until a round finds
// none. Each round's ranks are above the last's, so the rounds are at most
// the order's length, and each record weighed has beside it every record
// that may drop it: a record is kept or dropped as a range holding them all
// would keep or drop it. The records it reads drop records and are never
// counted themselves; past limit, over is the read's refusal, naming the
// budget the limit is left of.
func policyContext(ctx context.Context, tx *sql.Tx, base *builder, pol *recordPolicy, outside func(b *builder) string, matched []policyRecord, limit int, over func() error) ([]policyRecord, error) {
	var out []policyRecord
	seen := map[string]bool{}
	for round := matched; ; {
		var lo, hi time.Time
		lowest := 0
		for _, r := range round {
			if r.placed {
				r.widen(&lo, &hi)
				lowest = max(lowest, r.rank)
			}
		}
		above := pol.above(lowest)
		if lo.IsZero() || len(above) == 0 {
			return out, nil
		}
		b := base.clone()
		b.add(outside(b))
		b.add(`at <= ` + b.arg(hi) + ` AND coalesce(ends_at, at) >= ` + b.arg(lo))
		b.add(pol.source + ` = ANY(` + b.textArray(above) + `)`)
		next, err := readPolicyRound(ctx, tx, pol, `SELECT kind, id, at, ends_at, `+pol.source+`, `+pol.match+` FROM records WHERE `+strings.Join(b.where, " AND ")+
			` ORDER BY at, kind, id LIMIT `+b.arg(limit+1), b.args, seen)
		if err != nil {
			return nil, err
		}
		if len(out)+len(next) > limit {
			return nil, over()
		}
		out, round = append(out, next...), next
	}
}

// readPolicyRound reads one round of policyContext: the records query
// answers that seen does not hold yet, which it adds. A round may meet a
// record an earlier round read, so its LIMIT is the whole limit and one
// more, enough to find more new records than the limit leaves.
func readPolicyRound(ctx context.Context, tx *sql.Tx, pol *recordPolicy, query string, args []any, seen map[string]bool) ([]policyRecord, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate source policy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []policyRecord
	for rows.Next() {
		var kind, id, source, match string
		var at, endsAt sql.NullTime
		if err := rows.Scan(&kind, &id, &at, &endsAt, &source, &match); err != nil {
			return nil, err
		}
		if ref := kind + "/" + id; !seen[ref] {
			seen[ref] = true
			out = append(out, newPolicyRecord(pol, at, endsAt, source, match))
		}
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
