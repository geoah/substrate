package engine

// THE OBSERVED INDEX (decision 0160, migration 0012).
//
// Three tables derived from the rows of every kind that binds core's
// `observed` trait, the way `refs` is derived: `observed_points` (one row per
// value), `observed_rollups` (one row per stream, group and 15-minute bucket)
// and `observed_latest` (one row per stream). Every column is a function of
// one record's properties and its kind's binding (deriveObserved), or of the
// points those produce (internal/observed computes a rollup row), and of
// nothing else: no clock, no other record. That is what lets `repository
// rebuild` clear the three and reproduce them from the changelog.
//
// The fold writes the points of one record (syncObserved, from foldRecordOp,
// and dropObserved, from the tombstone and the purge) and records which
// buckets and streams their old and new points touched. The rollups and the
// latest rows are recomputed from the points once, at the end of the
// transaction (settleObserved): inTx and inRawTx call it, and a rebuild
// recomputes every row after its replay (t.rebuild), because a replay never
// passes through inTx.
//
// A stream is (kind, metric, source, about, channel): one row of
// observed_latest. Its points with one form are what a hold runs across and
// a counter steps across (obsHold); a rollup row is one bucket of that, for
// one group: a code, a unit, the delta flag and a UTC offset.

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// obsStream is one stream, the key of observed_latest.
type obsStream struct {
	metric, kind, source, about, channel string
}

// obsHold is one stream's points of one form: what a hold and a counter's
// steps run across, and what a rollup recompute reads.
type obsHold struct {
	obsStream
	form string
}

// obsBucket is one bucket of one obsHold, its start in Unix milliseconds.
type obsBucket struct {
	obsHold
	start int64
}

// observedTouch is what a transaction's point writes left to recompute.
type observedTouch struct {
	// all is a replay's: every row is recomputed after it, so nothing is
	// tracked.
	all     bool
	buckets map[obsBucket]bool
	// streams holds the change in each touched stream's count of available
	// points.
	streams map[obsStream]int64
}

func (t *txn) observedTouched() *observedTouch {
	if t.observed == nil {
		t.observed = &observedTouch{buckets: map[obsBucket]bool{}, streams: map[obsStream]int64{}}
	}
	return t.observed
}

// bindsObserved reports whether a kind binds core's `observed`.
func bindsObserved(ty *vocabulary.Kind) bool {
	if ty == nil {
		return false
	}
	_, ok := observedBinding(ty)
	return ok
}

// observedShape is the part of a kind's declaration the derivation reads:
// the variant it binds and the measure its `value` declares. Two
// declarations with the same shape derive the same points from the same
// row, so a vocabulary change re-derives a kind's points only where it
// moves (reprojection.go).
func observedShape(reg kindLookup, ident string) string {
	ty, ok := reg.ByIdentity(ident)
	if !ok || !bindsObserved(ty) {
		return ""
	}
	b, _ := observedBinding(ty)
	return b.Variant + "|" + declaredMeasure(ty)
}

func declaredMeasure(ty *vocabulary.Kind) string {
	if p, ok := ty.Prop("value"); ok && p.Quantity != nil {
		return p.Quantity.Measure
	}
	return ""
}

// observedRows is what one record derives: the stream's columns, the
// columns every point of the record shares, and its points.
type observedRows struct {
	obsHold
	id        string
	unit      string
	delta     bool
	statistic *string
	quality   *string
	holdMs    *int64
	resetRule *string
	resetAt   *time.Time
	wrapAt    *string
	circular  bool
	revision  *int64
	points    []obsPoint
}

// obsPoint is one point of a record.
type obsPoint struct {
	ord         int
	ts, tsEnd   time.Time
	offset      int64
	value, base *float64
	exact       *string
	code        string
	bounded     bool
}

// available is vocabulary.ObservedPointAvailable, the definition every reader
// of the index shares; availablePoint is the same test in SQL.
func (p obsPoint) available() bool {
	return vocabulary.ObservedPointAvailable(p.value != nil, p.code)
}

// deriveObserved computes a live row's points from its properties and its
// kind's binding alone. It is lenient on purpose: a row the write path
// admitted derives what it can read, and anything it cannot read derives no
// point rather than failing a replay. A single record derives a point only
// when it holds a value or a code; one that says why it has none
// (`absentReason`) or keeps a reading as received derives nothing.
func deriveObserved(ty *vocabulary.Kind, row *erow) *observedRows {
	if !bindsObserved(ty) || row == nil || row.At == nil || row.DeletedAt != nil {
		return nil
	}
	b, _ := observedBinding(ty)
	props := row.Props
	form, _ := props["form"].(string)
	if !slices.Contains(vocabulary.ObservedForms, form) {
		return nil
	}
	r := &observedRows{id: row.ID, obsHold: obsHold{form: form, obsStream: obsStream{
		metric:  referencePathOf(props["metric"]),
		kind:    row.Kind,
		source:  referencePathOf(props["source"]),
		about:   referencePathOf(props["about"]),
		channel: stringProp(props, "channel"),
	}}}
	if r.metric == "" {
		return nil
	}
	r.delta, _ = props["delta"].(bool)
	r.circular, _ = props["circular"].(bool)
	r.statistic = optString(props, "statistic")
	r.quality = optString(props, "quality")
	if h, err := vocabulary.ParseISODuration(stringProp(props, "hold")); err == nil && stringProp(props, "hold") != "" {
		ms := h.Milliseconds()
		r.holdMs = &ms
	}
	r.resetRule = optString(props, "resetRule")
	r.resetAt = parseTimeValue(stringProp(props, "resetAt"))
	r.wrapAt = optString(props, "wrapAt")
	if rev, ok := vocabulary.ObservedRevisionOf(props); ok {
		r.revision = &rev
	}
	switch b.Variant {
	case vocabulary.ObservedSingle:
		p, unit, ok := singlePoint(row)
		if !ok {
			return nil
		}
		r.unit = unit
		r.points = []obsPoint{p}
		if p.exact != nil {
			p.base = baseValue(measureFor(unit, declaredMeasure(ty), r.delta), unit, *p.exact)
			r.points[0] = p
		}
	case vocabulary.ObservedPacked:
		r.unit = stringProp(props, "unit")
		r.points = tracePoints(row, r)
	default:
		return nil
	}
	if len(r.points) == 0 {
		return nil
	}
	return r
}

// singlePoint is a single record's one point: its value or its code, at
// `at`, spanning to `endsAt` where it has one.
func singlePoint(row *erow) (obsPoint, string, bool) {
	props := row.Props
	p := obsPoint{ts: *row.At, tsEnd: *row.At, code: stringProp(props, "code")}
	if row.EndsAt != nil && row.EndsAt.After(*row.At) {
		p.tsEnd = *row.EndsAt
	}
	if off, ok := intOf(props["utcOffset"]); ok {
		p.offset = off
	}
	var unit string
	if q, ok := props["value"].(map[string]any); ok {
		digits, _ := q[vocabulary.QuantityValue].(string)
		unit, _ = q[vocabulary.QuantityUnit].(string)
		if f, err := strconv.ParseFloat(digits, 64); err == nil && digits != "" {
			p.value, p.exact = &f, &digits
			cmp, _ := q[vocabulary.QuantityComparator].(string)
			p.bounded = cmp != ""
		}
	}
	if p.value == nil {
		unit = ""
	}
	return p, unit, p.value != nil || p.code != ""
}

// tracePoints expands a packed record into its points. A point takes the
// record's start offset before the midpoint of the period and its end offset
// from the midpoint on; a gauge's and a counter's point is an instant, and
// an amount's spans to the next point (to `at` plus `every` with regular
// spacing, to `endsAt` for the last).
func tracePoints(row *erow, r *observedRows) []obsPoint {
	props := row.Props
	pts, err := vocabulary.TracePoints(*row.At, props)
	if err != nil {
		return nil
	}
	end := *row.At
	if row.EndsAt != nil {
		end = *row.EndsAt
	}
	mid := row.At.Add(end.Sub(*row.At) / 2)
	startOff, hasStart := intOf(props["utcOffsetStart"])
	endOff, hasEnd := intOf(props["utcOffsetEnd"])
	if !hasStart {
		startOff = endOff
	}
	if !hasEnd {
		endOff = startOff
	}
	var every time.Duration
	if s := stringProp(props, "every"); s != "" {
		every, _ = vocabulary.ParseISODuration(s)
	}
	measure := measureFor(r.unit, "", r.delta)
	factor, offset, baseErr := vocabulary.ToBase(measure, r.unit)
	out := make([]obsPoint, 0, len(pts))
	for i, tp := range pts {
		p := obsPoint{ord: i, ts: tp.At, tsEnd: tp.At, code: tp.Code, offset: startOff}
		if !tp.At.Before(mid) {
			p.offset = endOff
		}
		if r.form == vocabulary.FormAmount {
			switch {
			case every > 0:
				p.tsEnd = tp.At.Add(every)
			case i+1 < len(pts):
				p.tsEnd = pts[i+1].At
			default:
				p.tsEnd = end
			}
			if p.tsEnd.After(end) {
				p.tsEnd = end
			}
		}
		if tp.Value != "" {
			f, err := strconv.ParseFloat(tp.Value, 64)
			if err != nil {
				continue
			}
			digits := tp.Value
			p.value, p.exact = &f, &digits
			if baseErr == nil {
				p.base = toBase(factor, offset, digits)
			}
		}
		out = append(out, p)
	}
	return out
}

// measureFor is the measure a value converts in: the declared one where the
// property names it, else the unit's own, and `temperature-difference` for
// a temperature written as a difference.
func measureFor(unit, declared string, delta bool) string {
	if unit == "" {
		return ""
	}
	m, err := vocabulary.ResolveMeasure(unit, declared)
	if err != nil {
		return ""
	}
	if delta && m.Name == "temperature" {
		return "temperature-difference"
	}
	return m.Name
}

// baseValue is a value in its measure's base unit, nil where the unit has
// none.
func baseValue(measure, unit, digits string) *float64 {
	if measure == "" {
		return nil
	}
	factor, offset, err := vocabulary.ToBase(measure, unit)
	if err != nil {
		return nil
	}
	return toBase(factor, offset, digits)
}

func toBase(factor, offset *big.Rat, digits string) *float64 {
	v, ok := new(big.Rat).SetString(digits)
	if !ok {
		return nil
	}
	v.Mul(v, factor).Add(v, offset)
	f, _ := v.Float64()
	return &f
}

func stringProp(props map[string]any, name string) string {
	s, _ := props[name].(string)
	return s
}

func optString(props map[string]any, name string) *string {
	if s, ok := props[name].(string); ok && s != "" {
		return &s
	}
	return nil
}

// intOf reads a stored integer the way it arrives: int64 from a write's
// coercion, float64 from a jsonb read-back, json.Number from a replay, whose
// segment line spells it with an exponent (180 is `1.8E2`).
func intOf(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != math.Trunc(n) {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		r, ok := new(big.Rat).SetString(string(n))
		if !ok || !r.IsInt() || !r.Num().IsInt64() {
			return 0, false
		}
		return r.Num().Int64(), true
	}
	return 0, false
}

// bucketOf is the start of the 15-minute bucket holding t, in Unix
// milliseconds.
func bucketOf(t time.Time) int64 {
	w := observed.BucketWidth.Milliseconds()
	ms := t.UnixMilli()
	return ms - ((ms%w)+w)%w
}

// --- the fold's hooks ---

// syncObserved makes a record's points what its row now derives: the record's
// points are deleted and its new ones inserted in one statement each, and
// the buckets and streams both touched are left for settleObserved. A
// tombstoned row derives nothing (its points went with its tombstone), and a
// kind that does not bind the trait derives nothing, so its stale points go.
func (t *txn) syncObserved(ref eref, ty *vocabulary.Kind, row *erow) error {
	if err := t.dropObserved(ref); err != nil {
		return err
	}
	if !bindsObserved(ty) || row.DeletedAt != nil {
		return nil
	}
	return t.insertObserved(deriveObserved(ty, row))
}

// derivesObserved reports whether a row of the kind can hold observed
// points: a kind that binds the trait, or one this binary no longer
// declares, whose stale points syncObserved removes. A replay folds every
// row under one registry and starts from empty tables, so an undeclared
// kind there has none to remove.
func (t *txn) derivesObserved(ty *vocabulary.Kind) bool {
	if ty == nil {
		return t.observed == nil || !t.observed.all
	}
	return bindsObserved(ty)
}

// dropObservedOf deletes the points of a record leaving the fold (a
// tombstone, a purge), where its kind can hold them.
func (t *txn) dropObservedOf(ref eref) error {
	ty, _ := t.derivationView().ByIdentity(ref.Kind)
	if !t.derivesObserved(ty) {
		return nil
	}
	return t.dropObserved(ref)
}

// dropObserved deletes one record's points and records what they touched.
func (t *txn) dropObserved(ref eref) error {
	touch := t.observedTouched()
	if touch.all {
		_, err := t.exec(`DELETE FROM observed_points WHERE kind = $1 AND record_id = $2`, ref.Kind, ref.ID)
		return err
	}
	rows, err := t.query(`
		WITH d AS (
			DELETE FROM observed_points WHERE kind = $1 AND record_id = $2
			RETURNING metric, source, about, channel, form, ts, value, code)
		SELECT p.metric, p.source, p.about, p.channel, p.form,
		       (extract(epoch FROM date_bin('15 minutes', p.ts, TIMESTAMPTZ '2000-01-01 00:00:00+00')) * 1000)::bigint,
		       count(*) FILTER (WHERE `+availablePoint+`)
		FROM d p GROUP BY 1, 2, 3, 4, 5, 6`, ref.Kind, ref.ID)
	if err != nil {
		return fmt.Errorf("substrate/engine: observed points of %s %s: %w", ref.Kind, ref.ID, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h obsHold
		var bucket, available int64
		if err := rows.Scan(&h.metric, &h.source, &h.about, &h.channel, &h.form, &bucket, &available); err != nil {
			return err
		}
		h.kind = ref.Kind
		touch.streams[h.obsStream] -= available
		if h.form != vocabulary.FormSummary {
			touch.buckets[obsBucket{obsHold: h, start: bucket}] = true
		}
	}
	return rows.Err()
}

// insertObserved writes a record's points in one statement and records what
// they touched.
func (t *txn) insertObserved(r *observedRows) error {
	if r == nil {
		return nil
	}
	n := len(r.points)
	ord := make([]int64, n)
	ts := make([]time.Time, n)
	tsEnd := make([]time.Time, n)
	offset := make([]int64, n)
	value := make([]*float64, n)
	exact := make([]*string, n)
	base := make([]*float64, n)
	code := make([]string, n)
	bounded := make([]bool, n)
	quality := make([]*string, n)
	touch := t.observedTouched()
	unavailable := "unavailable"
	for i, p := range r.points {
		ord[i], ts[i], tsEnd[i], offset[i] = int64(p.ord), p.ts, p.tsEnd, p.offset
		value[i], exact[i], base[i], code[i], bounded[i] = p.value, p.exact, p.base, p.code, p.bounded
		quality[i] = r.quality
		if !p.available() {
			quality[i] = &unavailable
		}
		if touch.all {
			continue
		}
		if p.available() {
			touch.streams[r.obsStream]++
		} else if _, ok := touch.streams[r.obsStream]; !ok {
			touch.streams[r.obsStream] = 0
		}
		if r.form != vocabulary.FormSummary {
			touch.buckets[obsBucket{obsHold: r.obsHold, start: bucketOf(p.ts)}] = true
		}
	}
	if _, err := t.exec(`
		INSERT INTO observed_points (kind, record_id, ord, metric, source, about, channel, form, ts, ts_end,
			utc_offset_min, unit, delta, value, value_exact, value_base, code, statistic, bounded, quality,
			hold_ms, reset_rule, reset_at, wrap_at, circular, revision)
		SELECT $1, $2, u.ord, $3, $4, $5, $6, $7, u.ts, u.ts_end,
			u.off, $8, $9, u.value, u.exact::numeric, u.base, u.code, $10, u.bounded, u.quality,
			$11, $12, $13, $14::numeric, $15, $16
		FROM unnest($17::int[], $18::timestamptz[], $19::timestamptz[], $20::int[], $21::float8[],
		            $22::text[], $23::float8[], $24::text[], $25::boolean[], $26::text[])
		     AS u(ord, ts, ts_end, off, value, exact, base, code, bounded, quality)`,
		r.kind, r.id, r.metric, r.source, r.about, r.channel, r.form,
		r.unit, r.delta, r.statistic, r.holdMs, r.resetRule, r.resetAt, r.wrapAt, r.circular, r.revision,
		ord, ts, tsEnd, offset, value, exact, base, code, bounded, quality); err != nil {
		return fmt.Errorf("substrate/engine: observed points of %s %s: %w", r.kind, r.id, err)
	}
	return nil
}
