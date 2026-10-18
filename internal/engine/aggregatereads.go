package engine

// The aggregate read's reads of the observed index (aggregateread.go): a
// stream's segments in the read's windows and the points they derive, the
// point before each window, its rollup rows, and the conversion of a value
// the read takes into the read's unit.

import (
	"fmt"
	"sort"
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

// info is the stream's row of observed_streams, as the obsStore reads it.
func (s *aggStream) info() *obsStreamInfo {
	return &obsStreamInfo{id: s.id, key: obsKey{obsStream: s.obsStream, statistic: s.stat}, maxExtentMs: s.maxExtentMs}
}

// spanSegment is one segment that meets the span numbered span.
type spanSegment struct {
	span int
	seg  obsSegment
}

// spanSegmentsSQL lists, for each span ($4 and $5, numbered i), the
// segments of one stream ($1) and form ($2) whose points may meet it: those
// starting at or after $3, the span's start less the stream's longest
// extent, and before its end, and ending at or after its start. The lower
// bound is computed by the caller so that the index range takes both ends:
// Postgres keeps a bound it would compute from timestamptz arithmetic out of
// the index condition.
const spanSegmentsSQL = `
	SELECT w.i, ` + segmentColumns + ` FROM unnest($3::timestamptz[], $4::timestamptz[], $5::timestamptz[]) WITH ORDINALITY AS w(lo, a, b, i)
	CROSS JOIN LATERAL (
		SELECT * FROM observed_segments g
		WHERE g.stream = $1 AND g.form = $2 AND g.first_ts >= w.lo AND g.first_ts < w.b AND g.end_ts >= w.a OFFSET 0) g
	ORDER BY w.i, g.first_ts, g.record_id`

// reachBack is each span's start less the stream's longest extent: where
// the first records that may meet the span start (obsStreamInfo.reachBack).
func reachBack(spans [][2]time.Time, s *aggStream) []time.Time {
	info := s.info()
	out := make([]time.Time, len(spans))
	for i, sp := range spans {
		out[i] = info.reachBack(sp[0])
	}
	return out
}

// spanSegments reads the stream's segments of its form that meet each span.
func (r *aggRun) spanSegments(s *aggStream, form string, spans [][2]time.Time) ([]spanSegment, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	from, to := spanArrays(spans)
	rows, err := r.tx.QueryContext(r.ctx, spanSegmentsSQL, s.id, form, reachBack(spans, s), from, to)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate segments: %w", err)
	}
	var out []spanSegment
	for rows.Next() {
		var i int
		seg, err := scanSegment(rows, &i)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, spanSegment{span: i - 1, seg: seg})
	}
	return out, finishRows(rows)
}

// countPoints counts the points of the stream's segments that meet the
// spans, a segment once per span it meets: the points a read derives to take
// those placed in the spans, which count against maxSamples before they are
// derived.
func (r *aggRun) countPoints(s *aggStream, spans [][2]time.Time) (int64, error) {
	if len(spans) == 0 {
		return 0, nil
	}
	from, to := spanArrays(spans)
	var n int64
	err := r.tx.QueryRowContext(r.ctx, `
		SELECT coalesce(sum(g.points), 0)::bigint FROM unnest($3::timestamptz[], $4::timestamptz[], $5::timestamptz[]) AS w(lo, a, b)
		JOIN observed_segments g ON g.stream = $1 AND g.form = $2
		 AND g.first_ts >= w.lo AND g.first_ts < w.b AND g.end_ts >= w.a`,
		s.id, s.form, reachBack(spans, s), from, to).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: aggregate count: %w", err)
	}
	return n, nil
}

// overlaps reports whether the read places a point of the form in every
// window its span meets, by the part inside each (placeRaw): an amount in
// the records mode.
func (r *aggRun) overlaps(form string) bool {
	return r.p.bucket.mode == bucketRecords && form == vocabulary.FormAmount
}

// inSpan reports whether the read takes a point into the span [a, b): by its
// start, by its last instant under place end (the end of a span, which is
// in (a, b]), or for an amount in the records mode wherever its span meets
// the window.
func (r *aggRun) inSpan(p observed.Point, form string, a, b time.Time) bool {
	switch {
	case r.overlaps(form):
		return p.At.Before(b) && (!p.At.Before(a) || p.End.After(a))
	case r.p.bucket.placeEnd && p.End.After(p.At):
		return p.End.After(a) && !p.End.After(b)
	}
	return !p.At.Before(a) && p.At.Before(b)
}

// aggPointOf is a derived point as the read holds it: its group kept in
// rowGroups under its Group, its offset the one the read places it by. The
// point keeps its stored unit: the read converts a value, and refuses a
// unit that does not convert, only where it takes the value (convertPoint),
// so a point it drops or a carry that ended before the range refuses
// nothing.
func (r *aggRun) aggPointOf(p observed.Point) aggPoint {
	g := r.store.groups[p.Group]
	r.rowGroups[p.Group] = g
	ap := aggPoint{Point: p, delta: g.delta}
	ap.off, ap.fallback = r.offsetAt(g, r.placed(p))
	return ap
}

// offsetAt is the UTC offset in minutes a value of the group placed at t is
// placed by: its own, or where it recorded none and the bucket names a
// fallback zone, that zone's at t (fallback true), so under place end a span
// that ends after a clock change takes the zone's offset at its end. A
// recorded offset is the value's start's, and a span ends at it. Only the
// recorded-local calendar reads it, and only that calendar takes a fallback
// zone.
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

// readSeries reads one stream's points over the spans, with the point current
// at each span's start and the last available point before it.
func (r *aggRun) readSeries(s *aggStream, spans [][2]time.Time) (*aggSeries, error) {
	ser := &aggSeries{segs: make([]aggSegment, len(spans))}
	for i, sp := range spans {
		ser.segs[i].from, ser.segs[i].to = sp[0], sp[1]
	}
	found, err := r.spanSegments(s, s.form, spans)
	if err != nil {
		return nil, err
	}
	segs := make([]obsSegment, len(found))
	for i, f := range found {
		segs[i] = f.seg
	}
	if err := r.store.derive(segs); err != nil {
		return nil, err
	}
	for _, f := range found {
		rows := r.store.rows[f.seg.ref]
		if rows == nil || rows.form != s.form {
			continue
		}
		seg := &ser.segs[f.span]
		for i := range rows.points {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			p := rows.observedPoint(i)
			if r.inSpan(p, s.form, seg.from, seg.to) {
				seg.points = append(seg.points, r.aggPointOf(p))
			}
		}
	}
	for i := range ser.segs {
		pts := ser.segs[i].points
		sort.Slice(pts, func(a, b int) bool { return observed.Before(pts[a].Point, pts[b].Point) })
	}
	// A gauge reads the value current at each span's start, a counter the
	// last available point before it; the other forms read neither.
	if s.form != vocabulary.FormGauge && s.form != vocabulary.FormCounter {
		return ser, nil
	}
	for i := range ser.segs {
		p, err := r.store.before(s.info(), s.form, ser.segs[i].from, s.form == vocabulary.FormCounter)
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate carry: %w", err)
		}
		if p == nil {
			continue
		}
		ap := r.aggPointOf(*p)
		if s.form == vocabulary.FormGauge {
			ser.segs[i].carry = &ap
		} else {
			ser.segs[i].prev = &ap
		}
	}
	return ser, nil
}

// readCarries is, for each window, the point current at its start (available
// or not), its group set to its rollup row's, for HeldFromRowsCarry; nil
// where the stream has no point before it.
func (r *aggRun) readCarries(s *aggStream, windows [][2]time.Time) ([]*observed.Point, error) {
	out := make([]*observed.Point, len(windows))
	for i, w := range windows {
		p, err := r.store.before(s.info(), s.form, w[0], false)
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate carry: %w", err)
		}
		if p != nil {
			r.rowGroups[p.Group] = r.store.groups[p.Group]
			out[i] = p
		}
	}
	return out, nil
}

// readRollups reads one stream's rollup rows of a tier over the spans,
// each a row's whole bucket.
func (r *aggRun) readRollups(s *aggStream, w time.Duration, spans [][2]time.Time) ([]observed.Row, error) {
	from, to := spanArrays(spans)
	rows, err := r.tx.QueryContext(r.ctx, `SELECT `+rollupColumns+`
		FROM unnest($4::timestamptz[], $5::timestamptz[]) AS w(a, b)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_rollups r
			WHERE r.stream = $1 AND r.form = $2 AND r.tier = $3 AND r.bucket >= w.a AND r.bucket < w.b) r
		ORDER BY r.bucket, r.code, r.unit, r.delta, r.utc_offset_min, r.offset_known`,
		s.id, s.form, tierSeconds(w), from, to)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate rollups: %w", err)
	}
	var out []observed.Row
	for rows.Next() {
		row, g, err := scanRollup(rows, w)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		r.rowGroups[row.Group] = g
		out = append(out, row)
	}
	return out, finishRows(rows)
}
