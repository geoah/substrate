package engine

// The property read (aggregate.go): a declared number, quantity, money or
// duration of the filtered kinds, read from records and reduced in exact
// rational arithmetic, each record placed in time by its anchor.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// propKind is the family a reduced property belongs to.
type propKind int

const (
	propNumber propKind = iota
	propMoney
	propQuantity
	propDuration
)

// propCell is one bucket and group of a property read: what its reducers
// answer from.
type propCell struct {
	group      *aggGroup
	bucket     *aggBucket
	n          int64
	sum, sumsq *big.Rat
	lo, hi     *big.Rat
	unit, curr string
	// ends are a recorded-local row's first and last records, its start and
	// end written at their offsets; offsetless counts the records placed by
	// the fallback zone.
	ends       rowEnds
	offsetless int64
}

type propCellKey struct {
	group  string
	bucket *aggBucket
}

// aggregateProperty reduces one declared number of the filtered kinds,
// bucketed by a datetime property, in exact rational arithmetic: money by
// currency, a quantity converted into one unit of its measure, a duration
// into a duration unit.
func (ds *dataset) aggregateProperty(ctx context.Context, tx *sql.Tx, p *aggPlan) (*substrate.AggregateResult, error) {
	prop := p.a.Property
	local := p.bucket.mode == bucketLocal
	switch {
	case p.bucket.mode == bucketRecords:
		return nil, refuse("aggregate.bucket.records", "a property read buckets by its anchor; the records mode is the metric read's")
	case local && prop.UTCOffset == "":
		return nil, refuse("aggregate.property.utcOffset", "the recorded-local calendar places each record by its own UTC offset; name the declared property holding it in minutes east of UTC (such as utcOffsetEnd beside the anchor endsAt), or name bucket.timezone")
	case !local && prop.UTCOffset != "":
		return nil, refuse("aggregate.property.utcOffset", "an offset places a record in the recorded-local calendar (calendar with no timezone); every and a calendar in a timezone place it by its instant")
	}
	if p.bucket.placeEnd {
		return nil, refuse("aggregate.bucket.place", "a property read places a record by its anchor; name endsAt as the anchor to place it by its end")
	}
	name, anchor := prop.Name, prop.Anchor
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
		case prop.Datatype == vocabulary.DatatypeDuration:
			k = propDuration
		default:
			return nil, refuse("aggregate.property.name", "%s on %s is %s; a property read reduces a number, a quantity, money or a duration", name, t.Identity, prop.Datatype)
		}
		if family >= 0 && k != family {
			return nil, refuse("aggregate.property.name", "%s is a different datatype on two of the filtered kinds", name)
		}
		family = k
	}
	target := p.unit
	switch {
	case family == propDuration && target == "":
		target = "s"
	case family == propDuration && !resolves(target, "duration"):
		return nil, refuse("aggregate.unit", "%s is a duration, and %s is not a duration unit (s, min, h, d)", name, target)
	case p.unit != "" && family != propQuantity && family != propDuration:
		return nil, refuse("aggregate.unit", "%s is not a quantity or a duration, so no unit converts it", name)
	}
	anchorExpr, err := propertyAnchor(b, p, types, anchor)
	if err != nil {
		return nil, err
	}
	offsetExpr := `NULL::bigint`
	if local {
		if offsetExpr, err = propertyOffset(types, prop.UTCOffset); err != nil {
			return nil, err
		}
	}
	groupExpr := `'[]'`
	var groupExprs []string
	for i, g := range prop.GroupBy {
		path := fmt.Sprintf("aggregate.property.groupBy[%d]", i)
		expr, _, err := propertyText(types, path, g)
		if err != nil {
			return nil, err
		}
		groupExprs = append(groupExprs, expr)
	}
	if len(groupExprs) > 0 {
		groupExpr = `jsonb_build_array(` + strings.Join(groupExprs, ", ") + `)::text`
	}
	const policyPath = "aggregate.property.sources"
	var pol *recordPolicy
	if p.propBySource {
		if pol, err = newRecordPolicy(types, policyPath, prop.Sources); err != nil {
			return nil, err
		}
	}
	b.add(`jsonb_exists(props, ` + b.arg(name) + `)`)
	base := b.clone()
	inRange := func(b *builder) string {
		return anchorExpr + ` >= ` + b.arg(p.readFrom) + ` AND ` + anchorExpr + ` < ` + b.arg(p.to)
	}
	b.add(inRange(b))
	where := strings.Join(b.where, " AND ")
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE `+where, b.args...).Scan(&n); err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate count: %w", err)
	}
	if n > p.maxSamples {
		return nil, refuse("aggregate.budget.maxSamples", "the read takes %d records, over %d; narrow the filter or the range, or raise maxSamples (the cap is %d)",
			n, p.maxSamples, substrate.AggregateMaxSamplesCap)
	}
	if local && p.bucket.fallback == nil {
		// A record with no offset has no local day: refused before any value
		// is read, as the metric read refuses an offsetless point.
		var missing string
		err := tx.QueryRowContext(ctx, `SELECT kind || '/' || id FROM records WHERE `+where+` AND `+offsetExpr+` IS NULL ORDER BY kind, id LIMIT 1`,
			b.args...).Scan(&missing)
		switch {
		case err == nil:
			return nil, refuse("aggregate.property.utcOffset", "%s holds no %s, so the recorded-local calendar cannot place it; name bucket.fallbackTimezone to place such records by a zone's clock, or read in bucket.timezone",
				missing, prop.UTCOffset)
		case err != sql.ErrNoRows:
			return nil, fmt.Errorf("substrate/engine: aggregate offsets: %w", err)
		}
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
	keys = append(keys, groupExprs...)
	nGroups := min(n, 1)
	if len(keys) > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT DISTINCT `+strings.Join(keys, ", ")+` FROM records WHERE `+where+`) g`,
			b.args...).Scan(&nGroups); err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate groups: %w", err)
		}
	}
	if nGroups > int64(p.maxGroups) {
		return nil, refuse("aggregate.budget.maxGroups", "the read makes %d groups (kinds, currencies and property.groupBy values), over %d; narrow the filter or raise maxGroups (the cap is %d)",
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
	source, match := `''`, `'[]'`
	if pol != nil {
		source, match = pol.source, pol.match
	}
	cols := `kind, id, ` + anchorExpr + `, ` + offsetExpr + `, ` + groupExpr + `, at, ends_at, ` + source + `, ` + match +
		`, (props->` + b.arg(name) + `)::text`
	rows, err := tx.QueryContext(ctx, `SELECT `+cols+` FROM records WHERE `+where+
		` ORDER BY `+anchorExpr+`, kind, id`, b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate property: %w", err)
	}
	defer func() { _ = rows.Close() }()
	cells := map[propCellKey]*propCell{}
	var order []*propCell
	groups := map[string]*aggGroup{}
	// A recorded-local read places a record in the day its own offset names,
	// made where the range's enumeration did not reach (localBuckets).
	lr := &aggRun{ctx: ctx, p: p, buckets: buckets}
	var steps everyThousand
	take := func(r propRecord) error {
		v, unit, currency, ok := propValue(family, r.raw)
		if !ok {
			return nil
		}
		switch family {
		case propQuantity:
			if target == "" {
				target = unit
			}
			m := measure
			if m == "" {
				mm, err := vocabulary.MeasureOf(target)
				if err != nil {
					return refuse("aggregate.unit", "%v", err)
				}
				m = mm.Name
			}
			if !resolves(target, m) {
				return refuse("aggregate.unit", "%s is not a %s unit, the measure of %s", target, m, name)
			}
			cv, err := vocabulary.ConvertIn(m, v, unit, target)
			if err != nil {
				return refuse("aggregate.unit", "a %s of %s does not convert to %s: %v", name, unit, target, err)
			}
			v, unit = cv, target
		case propDuration:
			cv, err := vocabulary.ConvertIn("duration", v, "s", target)
			if err != nil {
				return refuse("aggregate.unit", "%s does not convert to %s: %v", name, target, err)
			}
			v, unit = cv, target
		}
		var placed []*aggBucket
		off, fallback := r.offset.Int64, false
		if local {
			if !r.offset.Valid {
				off, fallback = offsetMinutes(r.anchor.In(p.bucket.fallback)), true
			}
			if err := steps.check(ctx); err != nil {
				return err
			}
			placed = lr.localBuckets(r.anchor, wallOf(r.anchor, off))
		} else {
			bi, err := firstEndingAfter(lr.buckets, r.anchor, func() error { return steps.check(ctx) })
			if err != nil {
				return err
			}
			if bi < len(lr.buckets) && !r.anchor.Before(lr.buckets[bi].start) {
				placed = []*aggBucket{lr.buckets[bi]}
			}
		}
		if len(placed) == 0 {
			return nil
		}
		var key substrate.AggregateGroup
		var parts []string
		if p.grouped[substrate.GroupKind] {
			key.Kind = r.kind
			parts = append(parts, r.kind)
		}
		if family == propMoney {
			key.Currency = currency
			parts = append(parts, currency)
		}
		if len(prop.GroupBy) > 0 {
			var values []string
			if err := json.Unmarshal([]byte(r.groups), &values); err != nil {
				return fmt.Errorf("substrate/engine: aggregate property groups %q: %w", r.groups, err)
			}
			if len(values) != len(prop.GroupBy) {
				return fmt.Errorf("substrate/engine: aggregate property groups %q: want %d values", r.groups, len(prop.GroupBy))
			}
			for i, g := range prop.GroupBy {
				if values[i] == "" {
					continue
				}
				if key.Properties == nil {
					key.Properties = map[string]string{}
				}
				key.Properties[g] = values[i]
			}
			parts = append(parts, r.groups)
		}
		gk := strings.Join(parts, "\x1f")
		g := groups[gk]
		if g == nil {
			// The groups were counted before the scan; one more than
			// counted stops it rather than reduce past the budget.
			if int64(len(groups)) >= nGroups {
				return refuse("aggregate.budget.maxGroups", "the read met more groups than the %d it counted; read again", nGroups)
			}
			g = &aggGroup{key: key, sortKey: gk}
			groups[gk] = g
		}
		for _, bk := range placed {
			c := cells[propCellKey{gk, bk}]
			if c == nil {
				c = &propCell{group: g, bucket: bk, sum: new(big.Rat), sumsq: new(big.Rat), unit: unit, curr: currency}
				cells[propCellKey{gk, bk}] = c
				order = append(order, c)
			}
			c.n++
			c.sum.Add(c.sum, v)
			c.sumsq.Add(c.sumsq, new(big.Rat).Mul(v, v))
			if c.lo == nil || v.Cmp(c.lo) < 0 {
				c.lo = new(big.Rat).Set(v)
			}
			if c.hi == nil || v.Cmp(c.hi) > 0 {
				c.hi = new(big.Rat).Set(v)
			}
			if local {
				pt := observed.Point{At: r.anchor, RecordID: r.kind + "/" + r.id}
				c.ends.mark(pt, 0, off)
				bk.ends.mark(pt, 0, off)
				if fallback {
					c.offsetless++
				}
			}
		}
		return nil
	}
	// Without a source policy each record is reduced as it is read; with
	// one, the read holds the records (maxSamples bounds them) until the
	// policy has weighed them all.
	var held []propRecord
	var weighed []policyRecord
	var lo, hi time.Time
	for rows.Next() {
		var r propRecord
		var at, endsAt sql.NullTime
		var source, match string
		if err := rows.Scan(&r.kind, &r.id, &r.anchor, &r.offset, &r.groups, &at, &endsAt, &source, &match, &r.raw); err != nil {
			return nil, err
		}
		if pol == nil {
			if err := take(r); err != nil {
				return nil, err
			}
			continue
		}
		w := newPolicyRecord(pol, at, endsAt, source, match)
		w.widen(&lo, &hi)
		held, weighed = append(held, r), append(weighed, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if pol != nil && len(held) > 0 {
		var around []policyRecord
		if !lo.IsZero() {
			outside := func(b *builder) string { return `NOT coalesce(` + inRange(b) + `, false)` }
			if around, err = policyContext(ctx, tx, base, pol, outside, lo, hi, int(p.maxSamples-n), "aggregate.budget.maxSamples"); err != nil {
				return nil, err
			}
		}
		keep, err := keepBySource(ctx, append(weighed, around...))
		if err != nil {
			return nil, err
		}
		for i, r := range held {
			if !keep[i] {
				continue
			}
			if err := take(r); err != nil {
				return nil, err
			}
		}
	}
	buckets = lr.buckets
	if cells := nGroups * int64(len(buckets)); local && cells > int64(p.maxBuckets) {
		return nil, refuse("aggregate.budget.maxBuckets", "the records fall in %d recorded-local buckets, times %d groups is %d, over %d; narrow the range or raise maxBuckets",
			len(buckets), nGroups, cells, p.maxBuckets)
	}
	index := make(map[*aggBucket]int, len(buckets))
	for i, bk := range buckets {
		index[bk] = i
	}
	res := &substrate.AggregateResult{Property: name, Unit: target, Bucket: p.bucket.echo, Exact: true}
	meta := &aggMeta{sources: map[string]bool{}}
	out := make([]aggOut, 0, len(order))
	for _, c := range order {
		row := c.row(p, family, res, meta)
		out = append(out, aggOut{bucket: index[c.bucket], group: c.group, row: row})
	}
	if res.Rows, err = p.fillAndRoll(ctx, out, buckets, nil, meta); err != nil {
		return nil, err
	}
	if meta.inexact {
		res.Exact = false
	}
	return res, nil
}

// row answers a property read's reducers over one cell.
func (c *propCell) row(p *aggPlan, family propKind, res *substrate.AggregateResult, meta *aggMeta) substrate.AggregateRow {
	from, to := p.rangeOf(c.bucket)
	start, end := c.bucket.shown(&c.ends, from, to)
	row := substrate.AggregateRow{Start: start, End: end, Count: c.n, Offsetless: c.offsetless}
	if p.grouped[substrate.GroupKind] || family == propMoney || len(p.a.Property.GroupBy) > 0 {
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
		case substrate.ReducerStdDev:
			sd, exact := ratStdDev(c.sum, c.sumsq, c.n)
			if !exact {
				meta.inexact = true
			}
			write(red, sd)
		}
	}
	return row
}

// ratStdDev is the population standard deviation of n values from their sum
// and sum of squares: the variance exactly, so no cancellation loses digits,
// then its square root to ConvertedDigits significant digits, exact only
// where the variance is the square of that.
func ratStdDev(sum, sumsq *big.Rat, n int64) (*big.Rat, bool) {
	count := big.NewRat(n, 1)
	mean := new(big.Rat).Quo(sum, count)
	variance := new(big.Rat).Quo(sumsq, count)
	variance.Sub(variance, new(big.Rat).Mul(mean, mean))
	if variance.Sign() <= 0 {
		return new(big.Rat), true
	}
	f := new(big.Float).SetPrec(256).SetRat(variance)
	f.Sqrt(f)
	r, _ := new(big.Rat).SetString(f.Text('e', vocabulary.ConvertedDigits-1))
	return r, new(big.Rat).Mul(r, r).Cmp(variance) == 0
}

// propertyOffset is the SQL of a declared int property every kind of types
// declares, read as each record's UTC offset in minutes; NULL where the
// record holds none.
func propertyOffset(types []*vocabulary.Kind, name string) (string, error) {
	const path = "aggregate.property.utcOffset"
	if col, _ := columnFor(name); col != "" {
		return "", refuse(path, "%s is a record column; name a declared int property holding minutes east of UTC", name)
	}
	for _, t := range types {
		prop, ok := t.Prop(name)
		if !ok || prop.Repeated || prop.Keyed || prop.Datatype != vocabulary.DatatypeInt {
			return "", refuse(path, "%s declares no int property %q holding minutes east of UTC", t.Identity, name)
		}
	}
	lit := sqlLiteral(name)
	return `(CASE WHEN jsonb_typeof(props->` + lit + `) = 'number' THEN (props->>` + lit + `)::numeric::bigint END)`, nil
}

// propertyAnchor is the SQL instant a property read places each record at:
// one of the record's time columns, a declared datetime, or a declared date
// at the start of its day in the calendar's zone (UTC outside the calendar
// mode), so a date lands in the day it names. Every other column is refused
// by name: it is not a time. A date names its day already, so the
// recorded-local calendar refuses it.
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
		switch p.bucket.mode {
		case bucketCalendar:
			zone = p.bucket.loc.String()
		case bucketLocal:
			return "", refuse("aggregate.property.anchor", "%s is a date, which names its day already; read it in bucket.timezone", anchor)
		}
		return `(((props->>` + b.arg(anchor) + `)::date)::timestamp AT TIME ZONE ` + b.arg(zone) + `::text)`, nil
	}
	return `(props->>` + b.arg(anchor) + `)::timestamptz`, nil
}

// propRecord is one record a property read reduces: its kind and id, its
// anchor's instant, its UTC offset (on a recorded-local read), its
// property.groupBy values as a JSON array, and the property's stored value.
type propRecord struct {
	kind, id string
	anchor   time.Time
	offset   sql.NullInt64
	groups   string
	raw      string
}

// propValue reads one stored value of the family as an exact number; a
// duration in seconds.
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
	case propDuration:
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, "", "", false
		}
		d, err := vocabulary.ParseISODuration(s)
		if err != nil {
			return nil, "", "", false
		}
		return big.NewRat(int64(d), int64(time.Second)), "s", "", true
	}
	return nil, "", "", false
}
