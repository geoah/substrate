package engine

// Two aggregate reads that take no rollup: the latest read (each group's
// newest point, straight from observed_latest) and the property read (a
// declared number of the filtered kinds, from records, in exact arithmetic).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggregateLatest answers one row per group of the groupBy stream keys: the
// newest point of the group's streams (the latest instant, then the greater
// record id), with the streams' point count.
func (ds *dataset) aggregateLatest(ctx context.Context, tx *sql.Tx, p *aggPlan) (*substrate.AggregateResult, error) {
	streams, err := ds.selectStreams(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	res := &substrate.AggregateResult{Metric: p.metric, From: substrate.AggregateFromRaw, Exact: true}
	var units *aggUnits
	if p.unit != "" {
		if units, err = ds.aggregateUnits(ctx, tx, p, streams); err != nil {
			return nil, err
		}
		res.Unit = units.value
	}
	type group struct {
		key    substrate.AggregateGroup
		sort   string
		newest *aggStream
		points int64
	}
	byKey := map[string]*group{}
	var list []*group
	for _, s := range streams {
		key, k := p.groupKey(s, false)
		g := byKey[k]
		if g == nil {
			g = &group{key: key, sort: k}
			byKey[k] = g
			list = append(list, g)
		}
		g.points += s.points
		if g.newest == nil || newer(s, g.newest) {
			g.newest = s
		}
	}
	if len(list) > p.maxGroups {
		return nil, refuse("aggregate.budget.maxGroups", "the latest read makes %d groups, over %d; narrow the filter or raise maxGroups (the cap is %d)",
			len(list), p.maxGroups, substrate.AggregateMaxGroupsCap)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].sort < list[j].sort })
	sources := map[string]bool{}
	for _, g := range list {
		s := g.newest
		zone := time.FixedZone("", int(s.offset)*60)
		row := substrate.AggregateRow{
			Start: s.ts.In(zone), End: s.tsEnd.In(zone), Form: s.form, From: substrate.AggregateFromRaw,
			Count: g.points,
		}
		if s.form == vocabulary.FormSummary {
			row.From = substrate.AggregateFromSummaries
		}
		if s.bounded {
			row.Bounded = 1
		}
		if len(p.groupBy) > 0 {
			key := g.key
			row.Group = &key
		}
		if s.source != "" {
			row.Sources = []string{s.source}
			sources[s.source] = true
		}
		v := substrate.AggregateValue{Reducer: substrate.AggregateReducer{Name: substrate.ReducerLast}, Code: s.code}
		if s.value.Valid {
			digits, unit := s.value.String, s.unit
			if units != nil && s.unit != "" {
				c, err := units.factor(s.unit, s.delta)
				if err != nil {
					return nil, refuse("aggregate.unit", "the stream from %s stores %s, which does not convert to %s: %v", orOwner(s.source), s.unit, units.value, err)
				}
				var exact bool
				if digits, exact = c.digits(digits); !exact {
					res.Exact = false
				}
				unit = units.value
			}
			v.Value, v.Unit = &digits, unit
		}
		row.Values = []substrate.AggregateValue{v}
		res.Rows = append(res.Rows, row)
	}
	for s := range sources {
		res.Sources = append(res.Sources, s)
	}
	sort.Strings(res.Sources)
	return res, nil
}

// newer orders two streams' newest points as observed_latest orders points
// (observedsettle.go latestOrder): a record is in one stream, so the record
// id decides a tie.
func newer(a, b *aggStream) bool {
	if !a.ts.Equal(b.ts) {
		return a.ts.After(b.ts)
	}
	return a.recordID > b.recordID
}

// --- the property read ---

// propKind is the family a reduced property belongs to.
type propKind int

const (
	propNumber propKind = iota
	propMoney
	propQuantity
)

// aggregateProperty reduces one declared number of the filtered kinds,
// bucketed by a datetime property, in exact rational arithmetic: money by
// currency, a quantity converted into one unit of its measure.
func (ds *dataset) aggregateProperty(ctx context.Context, tx *sql.Tx, p *aggPlan) (*substrate.AggregateResult, error) {
	switch p.bucket.mode {
	case bucketLocal:
		return nil, refuse("aggregate.bucket.timezone", "a record's property carries no UTC offset of its own; name the zone its days are in")
	case bucketRecords:
		return nil, refuse("aggregate.bucket.records", "a property read buckets by its anchor; the records mode is the metric read's")
	}
	if p.bucket.placeEnd {
		return nil, refuse("aggregate.bucket.place", "a property read places a record by its anchor; name endsAt as the anchor to place it by its end")
	}
	name, anchor := p.a.Property.Name, p.a.Property.Anchor
	if anchor == "" {
		anchor = substrate.PropAt
	}
	b := &builder{overRecords: true}
	types, err := ds.buildFilter(ctx, tx, b, p.q.Filter)
	if err != nil {
		return nil, err
	}
	if len(types) == 0 {
		return nil, refuse("filter.kinds", "a property read names the kinds whose property it reduces")
	}
	if err := planWithValues(ctx, tx, b); err != nil {
		return nil, err
	}
	family := propKind(-1)
	var measure string
	for _, t := range types {
		prop, ok := t.Prop(name)
		if !ok {
			return nil, refuse("aggregate.property.name", "%s declares no property %q", t.Identity, name)
		}
		var k propKind
		switch {
		case prop.Repeated || prop.Keyed:
			return nil, refuse("aggregate.property.name", "%s on %s holds many values; a property read reduces one per record", name, t.Identity)
		case prop.Datatype == vocabulary.DatatypeInt || prop.Datatype == vocabulary.DatatypeFloat || prop.Datatype == vocabulary.DatatypeDecimal:
			k = propNumber
		case prop.Datatype == vocabulary.DatatypeMoney:
			k = propMoney
		case prop.Datatype == vocabulary.DatatypeQuantity:
			k = propQuantity
			if prop.Quantity != nil && prop.Quantity.Measure != "" {
				measure = prop.Quantity.Measure
			}
		default:
			return nil, refuse("aggregate.property.name", "%s on %s is %s; a property read reduces a number, a quantity or money", name, t.Identity, prop.Datatype)
		}
		if family >= 0 && k != family {
			return nil, refuse("aggregate.property.name", "%s is a different datatype on two of the filtered kinds", name)
		}
		family = k
	}
	if p.unit != "" && family != propQuantity {
		return nil, refuse("aggregate.unit", "%s is not a quantity, so no unit converts it", name)
	}
	anchorExpr, err := propertyAnchor(b, p, types, anchor)
	if err != nil {
		return nil, err
	}
	b.add(anchorExpr + ` >= ` + b.arg(p.readFrom) + ` AND ` + anchorExpr + ` < ` + b.arg(p.to))
	b.add(`jsonb_exists(props, ` + b.arg(name) + `)`)
	where := strings.Join(b.where, " AND ")
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE `+where, b.args...).Scan(&n); err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate count: %w", err)
	}
	if n > p.maxSamples {
		return nil, refuse("aggregate.budget.maxSamples", "the read takes %d records, over %d; narrow the filter or the range, or raise maxSamples (the cap is %d)",
			n, p.maxSamples, substrate.AggregateMaxSamplesCap)
	}
	// The groups are counted before a value is read, as the records are: a
	// small maxGroups or maxBuckets refuses the read before its scan.
	var keys []string
	if p.grouped[substrate.GroupKind] {
		keys = append(keys, `kind`)
	}
	if family == propMoney {
		keys = append(keys, `props->`+sqlLiteral(name)+`->>'`+vocabulary.MoneyCurrency+`'`)
	}
	nGroups := min(n, 1)
	if len(keys) > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT DISTINCT `+strings.Join(keys, ", ")+` FROM records WHERE `+where+`) g`,
			b.args...).Scan(&nGroups); err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate groups: %w", err)
		}
	}
	if nGroups > int64(p.maxGroups) {
		return nil, refuse("aggregate.budget.maxGroups", "the read makes %d groups (kinds and currencies), over %d; narrow the filter or raise maxGroups (the cap is %d)",
			nGroups, p.maxGroups, substrate.AggregateMaxGroupsCap)
	}
	buckets, err := ds.buckets(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	if cells := nGroups * int64(len(buckets)); cells > int64(p.maxBuckets) {
		return nil, refuse("aggregate.budget.maxBuckets", "%d groups times %d buckets is %d, over %d; raise maxBuckets or widen the bucket (the cap is %d)",
			nGroups, len(buckets), cells, p.maxBuckets, substrate.AggregateMaxBucketsCap)
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind, `+anchorExpr+`, (props->`+b.arg(name)+`)::text FROM records WHERE `+where+
		` ORDER BY `+anchorExpr+`, kind, id`, b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate property: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type acc struct {
		group      *aggGroup
		bucket     int
		n          int64
		sum        *big.Rat
		lo, hi     *big.Rat
		unit, curr string
	}
	cells := map[string]*acc{}
	groups := map[string]*aggGroup{}
	target := p.unit
	var steps everyThousand
	for rows.Next() {
		var kind string
		var at time.Time
		var raw string
		if err := rows.Scan(&kind, &at, &raw); err != nil {
			return nil, err
		}
		v, unit, currency, ok := propValue(family, raw)
		if !ok {
			continue
		}
		if family == propQuantity {
			if target == "" {
				target = unit
			}
			m := measure
			if m == "" {
				mm, err := vocabulary.MeasureOf(target)
				if err != nil {
					return nil, refuse("aggregate.unit", "%v", err)
				}
				m = mm.Name
			}
			if !resolves(target, m) {
				return nil, refuse("aggregate.unit", "%s is not a %s unit, the measure of %s", target, m, name)
			}
			cv, err := vocabulary.ConvertIn(m, v, unit, target)
			if err != nil {
				return nil, refuse("aggregate.unit", "a %s of %s does not convert to %s: %v", name, unit, target, err)
			}
			v, unit = cv, target
		}
		bi, err := firstEndingAfter(buckets, at, func() error { return steps.check(ctx) })
		if err != nil {
			return nil, err
		}
		if bi == len(buckets) || at.Before(buckets[bi].start) {
			continue
		}
		var key substrate.AggregateGroup
		var parts []string
		if p.grouped[substrate.GroupKind] {
			key.Kind = kind
			parts = append(parts, kind)
		}
		if family == propMoney {
			key.Currency = currency
			parts = append(parts, currency)
		}
		gk := strings.Join(parts, "\x1f")
		g := groups[gk]
		if g == nil {
			// The groups were counted before the scan; one more than
			// counted stops it rather than reduce past the budget.
			if int64(len(groups)) >= nGroups {
				return nil, refuse("aggregate.budget.maxGroups", "the read met more groups than the %d it counted; read again", nGroups)
			}
			g = &aggGroup{key: key, sortKey: gk}
			groups[gk] = g
		}
		ck := gk + "\x1e" + strconv.Itoa(bi)
		c := cells[ck]
		if c == nil {
			c = &acc{group: g, bucket: bi, sum: new(big.Rat), unit: unit, curr: currency}
			cells[ck] = c
		}
		c.n++
		c.sum.Add(c.sum, v)
		if c.lo == nil || v.Cmp(c.lo) < 0 {
			c.lo = new(big.Rat).Set(v)
		}
		if c.hi == nil || v.Cmp(c.hi) > 0 {
			c.hi = new(big.Rat).Set(v)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res := &substrate.AggregateResult{Property: name, Unit: target, Bucket: p.bucket.echo, Exact: true}
	meta := &aggMeta{sources: map[string]bool{}}
	var out []aggOut
	for _, c := range cells {
		bk := buckets[c.bucket]
		start, end := bk.shown(nil, p.from, p.to)
		row := substrate.AggregateRow{Start: start, End: end, Count: c.n}
		if p.grouped[substrate.GroupKind] || family == propMoney {
			key := c.group.key
			row.Group = &key
		}
		unit := c.unit
		if family == propMoney {
			unit = c.curr
		}
		write := func(red substrate.AggregateReducer, r *big.Rat) {
			digits, exact := vocabulary.RatDecimal(r, vocabulary.ConvertedDigits)
			if !exact {
				res.Exact = false
			}
			row.Values = append(row.Values, substrate.AggregateValue{Reducer: red, Value: &digits, Unit: unit})
		}
		for _, red := range p.reducers {
			switch red.Name {
			case substrate.ReducerCount:
				v := strconv.FormatInt(c.n, 10)
				row.Values = append(row.Values, substrate.AggregateValue{Reducer: red, Value: &v})
			case substrate.ReducerSum:
				write(red, c.sum)
			case substrate.ReducerAvg:
				write(red, new(big.Rat).Quo(c.sum, big.NewRat(c.n, 1)))
			case substrate.ReducerMin:
				write(red, c.lo)
			case substrate.ReducerMax:
				write(red, c.hi)
			}
		}
		out = append(out, aggOut{bucket: c.bucket, group: c.group, row: row})
	}
	if res.Rows, err = p.fillAndRoll(ctx, out, buckets, nil, meta); err != nil {
		return nil, err
	}
	if meta.inexact {
		res.Exact = false
	}
	return res, nil
}

// propertyAnchor is the SQL instant a property read places each record at:
// one of the record's time columns, a declared datetime, or a declared date
// at the start of its day in the calendar's zone (UTC outside the calendar
// mode), so a date lands in the day it names. Every other column is refused
// by name: it is not a time.
func propertyAnchor(b *builder, p *aggPlan, types []*vocabulary.Kind, anchor string) (string, error) {
	col, err := columnFor(anchor)
	if err != nil {
		return "", err
	}
	switch anchor {
	case substrate.PropAt, substrate.PropEndsAt, substrate.PropDueAt, "createdAt", "updatedAt":
		return col, nil
	}
	if col != "" {
		return "", refuse("aggregate.property.anchor", "%s is not a time to place a record at; the anchor is at, endsAt, dueAt, createdAt, updatedAt, or a declared datetime or date", anchor)
	}
	var datatype vocabulary.Datatype
	for _, t := range types {
		prop, ok := t.Prop(anchor)
		if !ok || prop.Repeated || prop.Keyed || (prop.Datatype != vocabulary.DatatypeDatetime && prop.Datatype != vocabulary.DatatypeDate) {
			return "", refuse("aggregate.property.anchor", "%s declares no datetime or date property %q to place its records in time", t.Identity, anchor)
		}
		if datatype != "" && prop.Datatype != datatype {
			return "", refuse("aggregate.property.anchor", "%s is a datetime on one filtered kind and a date on another", anchor)
		}
		datatype = prop.Datatype
	}
	if datatype == vocabulary.DatatypeDate {
		zone := "UTC"
		if p.bucket.mode == bucketCalendar {
			zone = p.bucket.loc.String()
		}
		return `(((props->>` + b.arg(anchor) + `)::date)::timestamp AT TIME ZONE ` + b.arg(zone) + `::text)`, nil
	}
	return `(props->>` + b.arg(anchor) + `)::timestamptz`, nil
}

// propValue reads one stored value of the family as an exact number.
func propValue(family propKind, raw string) (v *big.Rat, unit, currency string, ok bool) {
	switch family {
	case propNumber:
		s := strings.Trim(raw, `"`)
		v, ok = new(big.Rat).SetString(s)
		return v, "", "", ok
	case propMoney:
		var m struct {
			Amount   json.Number `json:"amount"`
			Currency string      `json:"currency"`
		}
		if err := json.Unmarshal([]byte(raw), &m); err != nil || m.Currency == "" {
			return nil, "", "", false
		}
		amount, err := m.Amount.Int64()
		if err != nil {
			return nil, "", "", false
		}
		d, known := vocabulary.CurrencyDecimals(m.Currency)
		if !known {
			return nil, "", "", false
		}
		v = new(big.Rat).SetFrac(big.NewInt(amount), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil))
		return v, "", m.Currency, true
	case propQuantity:
		var q struct {
			Value string `json:"value"`
			Unit  string `json:"unit"`
		}
		if err := json.Unmarshal([]byte(raw), &q); err != nil || q.Unit == "" {
			return nil, "", "", false
		}
		v, ok = new(big.Rat).SetString(q.Value)
		return v, q.Unit, "", ok
	}
	return nil, "", "", false
}
