package engine

// The aggregate read's statements over the observed index
// (aggregateread.go): a stream's raw points in the read's windows, the point
// before each window and its rollup rows, and the conversion of a value the
// read takes into the read's unit.

import (
	"database/sql"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// spanArrays are the spans' starts and ends as a statement's two arrays.
func spanArrays(spans [][2]time.Time) (from, to []time.Time) {
	for _, s := range spans {
		from, to = append(from, s[0]), append(to, s[1])
	}
	return from, to
}

func (r *aggRun) countPoints(s *aggStream, spans [][2]time.Time) (int64, error) {
	from, to := spanArrays(spans)
	var n int64
	err := r.tx.QueryRowContext(r.ctx, `
		SELECT coalesce(sum(x.c), 0)::bigint FROM unnest($8::timestamptz[], $9::timestamptz[]) AS s(a, b)
		CROSS JOIN LATERAL (
			SELECT count(*) AS c FROM observed_points p
			WHERE `+aggStreamSQL+` AND `+placedIn(r.p.bucket.placeEnd, "s.a", "s.b")+`) x`,
		append(s.args(), from, to)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: aggregate count: %w", err)
	}
	return n, nil
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
		return nil, refuse("aggregate.unit", "a value%s stored in %s does not convert to %s: %v", from, unit, r.u.value, err)
	}
	return c, nil
}

// readSeries reads one stream's points over the spans, with the point current
// at each span's start and the last available point before it.
func (r *aggRun) readSeries(s *aggStream, spans [][2]time.Time) (*aggSeries, error) {
	from, to := spanArrays(spans)
	args := append(s.args(), from, to)
	ser := &aggSeries{segs: make([]aggSegment, len(spans))}
	for i, sp := range spans {
		ser.segs[i].from, ser.segs[i].to = sp[0], sp[1]
	}
	rows, err := r.tx.QueryContext(r.ctx, `
		SELECT s.i, `+aggPointLead+pointColumns+`
		FROM unnest($8::timestamptz[], $9::timestamptz[]) WITH ORDINALITY AS s(a, b, i)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE `+aggStreamSQL+` AND `+placedIn(r.p.bucket.placeEnd, "s.a", "s.b")+` OFFSET 0) p
		ORDER BY s.i, p.ts, p.record_id, p.ord`, args...)
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
