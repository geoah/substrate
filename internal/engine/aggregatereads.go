package engine

// The aggregate read's statements over the observed index
// (aggregateread.go): a stream's raw points in the read's windows, the point
// before each window and its rollup rows, and the conversion of a value the
// read takes into the read's unit.

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// spanArrays are the spans' starts and ends as a statement's two arrays.
func spanArrays(spans [][2]time.Time) (from, to []time.Time) {
	for _, s := range spans {
		from, to = append(from, s[0]), append(to, s[1])
	}
	return from, to
}

// countPointsSQL counts one stream's points (aggStreamSQL, $1 to $7) placed
// in each span, the spans' starts and ends at $8 and $9, or where overlap,
// meeting it (overlapPoints, the reach at $10).
func countPointsSQL(placeEnd, overlap bool) string {
	return `
		SELECT coalesce(sum(x.c), 0)::bigint FROM unnest($8::timestamptz[], $9::timestamptz[]) AS s(a, b)
		CROSS JOIN LATERAL (
			SELECT count(*) AS c FROM ` + pointsIn(placeEnd, overlap, aggStreamSQL, "s.a", "s.b") + `) x`
}

// pointsIn is placedPoints, or where overlap, overlapPoints with the reach
// bound at $10 in microseconds.
func pointsIn(placeEnd, overlap bool, where, a, b string) string {
	if overlap {
		return overlapPoints(where, a, b, `$10::bigint * interval '1 microsecond'`)
	}
	return placedPoints(placeEnd, where, a, b)
}

// overlapPoints is the points of observed_points that match where and meet
// [a, b), as a FROM item named p: those starting in it, and those starting
// up to reach before a whose span passes a. reach is the stream's longest
// span (aggRun.amountReach), so the read never scans a stream's history
// before the window for a span that might reach into it.
func overlapPoints(where, a, b, reach string) string {
	return `(SELECT * FROM observed_points p WHERE ` + where + ` AND p.ts >= ` + a + ` - ` + reach + ` AND p.ts < ` + b + `
			  AND (p.ts >= ` + a + ` OR p.ts_end > ` + a + `)) p`
}

// pointArgs are a stream's arguments to countPointsSQL and readSeriesSQL.
func (r *aggRun) pointArgs(s *aggStream, spans [][2]time.Time) []any {
	from, to := spanArrays(spans)
	args := append(s.args(), from, to)
	if r.overlaps(s.form) {
		args = append(args, r.spanReach[s.obsStream].Microseconds())
	}
	return args
}

// overlaps reports whether the read places a point of the form in every
// window its span meets, by the part inside each (placeRaw): an amount in
// the records mode.
func (r *aggRun) overlaps(form string) bool {
	return r.p.bucket.mode == bucketRecords && form == vocabulary.FormAmount
}

func (r *aggRun) countPoints(s *aggStream, spans [][2]time.Time) (int64, error) {
	var n int64
	err := r.tx.QueryRowContext(r.ctx, countPointsSQL(r.p.bucket.placeEnd, r.overlaps(s.form)), r.pointArgs(s, spans)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: aggregate count: %w", err)
	}
	return n, nil
}

// amountReachSQL is, for each stream ($1 to $5, numbered n), the spanned
// points the span index holds ending after $6, and the longest span of the
// amounts among them that start before $7, in whole microseconds. The index
// bounds a point by its end alone, so the statement visits every spanned
// point from the first window's start to each stream's newest; the LIMIT
// ($8) stops it one past maxSamples, and the count is of every point it
// visits, so the budget bounds the scan itself.
const amountReachSQL = `
	SELECT x.n, count(*), coalesce(ceil(extract(epoch FROM max(x.span) FILTER (WHERE x.meets)) * 1000000), 0)::bigint
	FROM (
		SELECT s.n, p.ts_end - p.ts AS span, p.form = 'amount' AND p.ts < $7 AS meets
		FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) WITH ORDINALITY AS s(metric, kind, source, about, channel, n)
		CROSS JOIN LATERAL (
			SELECT p.ts, p.ts_end, p.form FROM observed_points p
			WHERE p.metric = s.metric AND p.kind = s.kind AND p.source = s.source AND p.about = s.about AND p.channel = s.channel
			  AND p.ts_end > p.ts AND p.ts_end > $6) p
		LIMIT $8) x
	GROUP BY x.n`

// amountReach is how far before a records-mode window each stream's amounts
// may start and still reach into it: its longest span among those meeting
// the windows' extent. A stream it does not name has no span there. The
// amounts it reads count against maxSamples (read is how many) beside the
// spent the read counted before it, and past it the read is refused.
func (ds *dataset) amountReach(ctx context.Context, tx *sql.Tx, p *aggPlan, streams []*aggStream, windows [][2]time.Time, spent int64) (reach map[obsStream]time.Duration, read int64, err error) {
	out := map[obsStream]time.Duration{}
	if len(windows) == 0 {
		return out, 0, nil
	}
	var keys []*aggStream
	seen := map[obsStream]bool{}
	for _, s := range streams {
		if !seen[s.obsStream] {
			seen[s.obsStream] = true
			keys = append(keys, s)
		}
	}
	if len(keys) == 0 {
		return out, 0, nil
	}
	lo, hi := windows[0][0], windows[0][1]
	for _, w := range windows {
		if w[1].After(hi) {
			hi = w[1]
		}
	}
	ids := streamKeys(keys, false)
	rows, err := tx.QueryContext(ctx, amountReachSQL, ids[0], ids[1], ids[2], ids[3], ids[4], lo, hi, max(p.maxSamples-spent, 0)+1)
	if err != nil {
		return nil, 0, fmt.Errorf("substrate/engine: aggregate amount reach: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var n int
		var c, us int64
		if err := rows.Scan(&n, &c, &us); err != nil {
			return nil, 0, err
		}
		read += c
		out[keys[n-1].obsStream] = time.Duration(us) * time.Microsecond
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if read+spent > p.maxSamples {
		beside := ""
		if spent > 0 {
			beside = fmt.Sprintf(" beside the %d raw points the read counted before it", spent)
		}
		return nil, 0, refuse("aggregate.budget.maxSamples", "finding how far amounts reach into the windows reads every value with a span from the first window's start to each stream's newest%s, more than %d; read windows nearer the newest values, narrow the filter, or raise maxSamples (the cap is %d)",
			beside, p.maxSamples, substrate.AggregateMaxSamplesCap)
	}
	return out, read, nil
}

// aggPointLead are the columns read before pointColumns.
const aggPointLead = `p.record_id, p.ord, `

// scanAggPoint reads aggPointLead and pointColumns after any leading
// columns, and keeps the point's group (its code, unit, delta and offset) in
// rowGroups under the point's Group. The point keeps its stored unit: the
// read converts a value, and refuses a unit that does not convert, only
// where it takes the value (convertPoint), so a point it drops or a carry
// that ended before the range refuses nothing.
func (r *aggRun) scanAggPoint(rows *sql.Rows, lead ...any) (aggPoint, error) {
	var ap aggPoint
	var recordID string
	var ord int64
	sp, err := scanPoint(rows, append(lead, &recordID, &ord)...)
	if err != nil {
		return ap, err
	}
	ap.Point, ap.delta = sp.point, sp.group.delta
	ap.off, ap.fallback = r.offsetAt(sp.group, ap.At)
	ap.RecordID, ap.Ord = recordID, ord
	r.rowGroups[ap.Group] = sp.group
	return ap, nil
}

// offsetAt is the UTC offset in minutes a value of the group at t is placed
// by: its own, or where it recorded none and the bucket names a fallback
// zone, that zone's at t (fallback true). Only the recorded-local calendar
// reads it, and only that calendar takes a fallback zone.
func (r *aggRun) offsetAt(g obsGroup, t time.Time) (offset int64, fallback bool) {
	if !g.offsetUnknown || r.p.bucket.fallback == nil {
		return g.offset, false
	}
	return offsetMinutes(t.In(r.p.bucket.fallback)), true
}

// convertPoint converts the number of a point the read takes from stream s
// into the read's unit, refused where its unit does not convert. A
// counter's stays as stored: its steps are taken on the stored digits, in
// the stored unit, as its rollup rows take them.
func (r *aggRun) convertPoint(p *observed.Point, delta bool, s *aggStream) error {
	if !p.HasValue || p.Unit == "" || s.form == vocabulary.FormCounter {
		return nil
	}
	c, err := r.conversion(p.Unit, delta, s.source)
	if err != nil {
		return err
	}
	r.convertWith(p, c)
	return nil
}

// convertWith converts a point's number and digits with c into the read's
// unit, the digits exactly where c allows.
func (r *aggRun) convertWith(p *observed.Point, c *convFactor) {
	if c.identity {
		return
	}
	p.Value = p.Value*c.ff + c.fo
	digits, exact := c.digits(p.Exact)
	if !exact {
		r.meta.inexact = true
	}
	p.Exact, p.Unit = digits, r.u.value
}

// conversion is the factor from a stored unit into the read's, refused with
// a 422 naming the value's unit, and its source where the caller knows it:
// the read refuses a value it takes, never a stream for its other points.
func (r *aggRun) conversion(unit string, delta bool, source string) (*convFactor, error) {
	c, err := r.u.factor(unit, delta)
	if err != nil {
		from := ""
		if source != "" {
			from = " from " + source
		}
		return nil, refuse(r.p.path("unit"), "a value%s stored in %s does not convert to %s: %v", from, unit, r.u.value, err)
	}
	return c, nil
}

// readSeriesSQL reads one stream's points (aggStreamSQL, $1 to $7) placed in
// each span, the spans' starts and ends at $8 and $9, or where overlap,
// meeting it (the reach at $10), by span and in the order every reader takes
// points in.
func readSeriesSQL(placeEnd, overlap bool) string {
	return `
		SELECT s.i, ` + aggPointLead + pointColumns + `
		FROM unnest($8::timestamptz[], $9::timestamptz[]) WITH ORDINALITY AS s(a, b, i)
		CROSS JOIN LATERAL (
			SELECT * FROM ` + pointsIn(placeEnd, overlap, aggStreamSQL, "s.a", "s.b") + ` OFFSET 0) p
		ORDER BY s.i, p.ts, p.record_id, p.ord`
}

// readSeries reads one stream's points over the spans, with the point current
// at each span's start and the last available point before it.
func (r *aggRun) readSeries(s *aggStream, spans [][2]time.Time) (*aggSeries, error) {
	from, _ := spanArrays(spans)
	args := r.pointArgs(s, spans)
	ser := &aggSeries{segs: make([]aggSegment, len(spans))}
	for i, sp := range spans {
		ser.segs[i].from, ser.segs[i].to = sp[0], sp[1]
	}
	rows, err := r.tx.QueryContext(r.ctx, readSeriesSQL(r.p.bucket.placeEnd, r.overlaps(s.form)), args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate points: %w", err)
	}
	for rows.Next() {
		var i int
		ap, err := r.scanAggPoint(rows, &i)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		ser.segs[i-1].points = append(ser.segs[i-1].points, ap)
	}
	if err := finishRows(rows); err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate points: %w", err)
	}
	// A gauge reads the value current at each span's start, a counter the
	// last available point before it; the other forms read neither.
	for _, x := range []struct {
		form, filter string
		dst          func(seg *aggSegment, p *aggPoint)
	}{
		{vocabulary.FormGauge, "TRUE", func(seg *aggSegment, p *aggPoint) { seg.carry = p }},
		{vocabulary.FormCounter, availablePoint, func(seg *aggSegment, p *aggPoint) { seg.prev = p }},
	} {
		if s.form != x.form {
			continue
		}
		err := r.readBefore(s, from, x.filter, func(rows *sql.Rows, i *int) error {
			ap, err := r.scanAggPoint(rows, i)
			if err == nil {
				x.dst(&ser.segs[*i-1], &ap)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return ser, nil
}

// readBefore reads, for each start, the stream's last point before it that
// filter admits, by the order every reader takes points in; scan takes each
// row with the index of its start, from 1.
func (r *aggRun) readBefore(s *aggStream, starts []time.Time, filter string, scan func(rows *sql.Rows, i *int) error) error {
	rows, err := r.tx.QueryContext(r.ctx, `
		SELECT s.i, `+aggPointLead+pointColumns+`
		FROM unnest($8::timestamptz[]) WITH ORDINALITY AS s(a, i)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE `+aggStreamSQL+` AND p.ts < s.a AND `+filter+`
			ORDER BY p.ts DESC, p.record_id DESC, p.ord DESC LIMIT 1) p`,
		append(s.args(), starts)...)
	if err != nil {
		return fmt.Errorf("substrate/engine: aggregate carry: %w", err)
	}
	for rows.Next() {
		var i int
		if err := scan(rows, &i); err != nil {
			_ = rows.Close()
			return err
		}
	}
	if err := finishRows(rows); err != nil {
		return fmt.Errorf("substrate/engine: aggregate carry: %w", err)
	}
	return nil
}

// readCarries is, for each window, the point current at its start (available
// or not), its group set to its rollup row's, for HeldFromRowsCarry; nil
// where the stream has no point before it.
func (r *aggRun) readCarries(s *aggStream, windows [][2]time.Time) ([]*observed.Point, error) {
	starts, _ := spanArrays(windows)
	out := make([]*observed.Point, len(windows))
	err := r.readBefore(s, starts, "TRUE", func(rows *sql.Rows, i *int) error {
		var recordID string
		var ord int64
		sp, err := scanPoint(rows, i, &recordID, &ord)
		if err != nil {
			return err
		}
		r.rowGroups[sp.point.Group] = sp.group
		p := sp.point
		p.RecordID, p.Ord = recordID, ord
		out[*i-1] = &p
		return nil
	})
	return out, err
}

// rollupColumns are the columns of observed_rollups a read takes.
const rollupColumns = `r.code, r.unit, r.delta, r.utc_offset_min, r.offset_known, r.bucket15, r.n, r.bounded, r.sum, r.min, r.max, r.mean, r.m2,
	r.first_ts, r.first_value::text, r.last_ts, r.last_value::text, r.covered_ms, r.held_ms, r.twa_sum, r.last_hold_ms,
	r.max_gap_ms, r.start_gap_ms, r.increase::text, r.resets, r.sin_sum, r.cos_sum, r.span_end,
	r.first_record_id, r.first_ord, r.last_record_id, r.last_ord, r.min_value::text, r.max_value::text`

// readRollups reads one stream's rollup rows over the read's windows: the
// range, or every records-mode window, which is not clipped to the range.
func (r *aggRun) readRollups(s *aggStream) ([]observed.Row, error) {
	from, to := spanArrays(r.rowSpans)
	rows, err := r.tx.QueryContext(r.ctx, `SELECT `+rollupColumns+`
		FROM unnest($7::timestamptz[], $8::timestamptz[]) AS w(a, b)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_rollups r
			WHERE r.metric = $1 AND r.kind = $2 AND r.source = $3 AND r.about = $4 AND r.channel = $5 AND r.form = $6
			  AND r.bucket15 >= w.a AND r.bucket15 < w.b) r
		ORDER BY r.bucket15, r.code, r.unit, r.delta, r.utc_offset_min, r.offset_known`,
		append(s.streamForm().args(), from, to)...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate rollups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []observed.Row
	for rows.Next() {
		var (
			g                                    obsGroup
			known                                bool
			bucket                               time.Time
			n, bounded, covered, maxGap, startGp int64
			sum, minV, maxV, mean, m2, twa       sql.NullFloat64
			sin, cos                             sql.NullFloat64
			firstTS, lastTS                      sql.NullTime
			firstV, lastV, increase              sql.NullString
			held, lastHold                       sql.NullInt64
			resets                               sql.NullInt64
			spanEnd                              sql.NullTime
			firstRecord, lastRecord              sql.NullString
			firstOrd, lastOrd                    sql.NullInt64
			minExact, maxExact                   sql.NullString
		)
		if err := rows.Scan(&g.code, &g.unit, &g.delta, &g.offset, &known, &bucket, &n, &bounded, &sum, &minV, &maxV, &mean, &m2,
			&firstTS, &firstV, &lastTS, &lastV, &covered, &held, &twa, &lastHold, &maxGap, &startGp, &increase, &resets, &sin, &cos,
			&spanEnd, &firstRecord, &firstOrd, &lastRecord, &lastOrd, &minExact, &maxExact); err != nil {
			return nil, err
		}
		g.offsetUnknown = !known
		key := g.key()
		r.rowGroups[key] = g
		st := observed.Stats{
			N: n, Bounded: bounded, Sum: sum.Float64, Min: minV.Float64, Max: maxV.Float64, Mean: mean.Float64, M2: m2.Float64,
			MinExact: minExact.String, MaxExact: maxExact.String,
			CoveredMs: covered, HeldMs: held.Int64, TWASum: twa.Float64, MaxGapMs: maxGap, StartGapMs: startGp,
			Resets: resets.Int64, SinSum: sin.Float64, CosSum: cos.Float64, SpanEnd: spanEnd.Time,
		}
		if sum.Valid {
			st.Values = n
		}
		if sin.Valid {
			st.Angles = n
		}
		if lastHold.Valid {
			ms := lastHold.Int64
			st.LastHoldMs = &ms
		}
		if increase.Valid {
			st.Increase, _ = new(big.Rat).SetString(increase.String)
		}
		point := func(ts sql.NullTime, v sql.NullString) *observed.Point {
			if !ts.Valid {
				return nil
			}
			p := &observed.Point{At: ts.Time, End: ts.Time, Code: g.code, Unit: g.unit, Group: key}
			if v.Valid {
				p.Exact, p.HasValue = v.String, true
				p.Value, _ = strconv.ParseFloat(v.String, 64)
			}
			return p
		}
		if n > 0 {
			st.First, st.Last = point(firstTS, firstV), point(lastTS, lastV)
			if st.First != nil {
				st.First.RecordID, st.First.Ord = firstRecord.String, firstOrd.Int64
			}
			if st.Last != nil {
				st.Last.RecordID, st.Last.Ord = lastRecord.String, lastOrd.Int64
			}
		}
		out = append(out, observed.Row{Start: bucket, Group: key, Stats: st})
	}
	return out, rows.Err()
}
