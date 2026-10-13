package engine

// The aggregate read's reads and reductions (aggregatemetric.go has the
// rules for which group is served from where).

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggRun is one metric read in progress.
type aggRun struct {
	ds      *dataset
	ctx     context.Context
	tx      *sql.Tx
	p       *aggPlan
	u       *aggUnits
	buckets []*aggBucket
	meta    aggMeta
	// rows are each rollup-served stream's rows, by stream.
	rows map[*aggStream][]observed.Row
	// rowGroups maps a rollup row's group key to its group.
	rowGroups map[string]obsGroup
	// carries are the points current at each window's start, by stream and
	// in segments order, for a rollup-served gauge.
	carries map[*aggStream][]*observed.Point
	// series are the raw points read, by stream.
	series map[*aggStream]*aggSeries
	// bounds are the records mode's window starts and ends, sorted, where a
	// carry walked across empty rows is cut (chunkEnd).
	bounds []time.Time
	// nSeries is the series the read was budgeted for (seriesOf).
	nSeries int
	// samples is the raw points the read's statements have counted so far,
	// against maxSamples (charge).
	samples int64
	// steps counts the reduction's steps between context checks (canceled).
	steps int
}

// aggSeries is one stream's raw points: each span read, with the point
// current at its start and, for a counter, the last available point before
// it.
type aggSeries struct {
	segs []aggSegment
}

type aggSegment struct {
	from, to time.Time
	carry    *aggPoint
	prev     *aggPoint
	points   []aggPoint
}

// evidence is what one row reduces: raw members and held spans, or merged
// rollup statistics and held sums.
type aggEvidence struct {
	form     string
	raw      bool
	members  []aggPoint
	spans    []aggSpan
	stats    observed.Stats
	held     map[string]observed.HeldSum
	steps    aggSteps
	sources  map[string]bool
	coverage int64
	// extents are a rollup-served amount's pieces of coverage, joined into
	// coverage once every row and cut end has merged (amountCoverage).
	extents []observed.Extent
	// firstStream and lastStream are the streams stats.First and stats.Last
	// came from, the last key of pointBefore.
	firstStream, lastStream int
	count                   int64
	bounded                 int64
	// ends are a recorded-local row's own first and last points and held
	// time, which its start and end are written at.
	ends rowEnds
}

type aggSteps struct {
	inc     *big.Rat
	resets  int64
	covered int64
	any     bool
}

func (s *aggSteps) add(d *big.Rat, reset bool, covered int64) {
	if s.inc == nil {
		s.inc = new(big.Rat)
	}
	s.inc.Add(s.inc, d)
	if reset {
		s.resets++
	}
	s.covered += covered
	s.any = true
}

func newEvidence(form string, raw bool) *aggEvidence {
	return &aggEvidence{form: form, raw: raw, sources: map[string]bool{}, held: map[string]observed.HeldSum{}}
}

// --- planning ---

// plan decides where each group's buckets are served from, reads the rollup
// rows of the groups they serve, and counts the raw points the rest need
// against maxSamples before reading them.
func (r *aggRun) plan(groups []*aggGroup) error {
	r.rows, r.rowGroups = map[*aggStream][]observed.Row{}, map[string]obsGroup{}
	r.carries, r.series = map[*aggStream][]*observed.Point{}, map[*aggStream]*aggSeries{}
	for _, g := range groups {
		g.rollups = r.rollupable(g)
		if !g.rollups {
			continue
		}
		for _, s := range g.raw {
			rows, err := r.readRollups(s)
			if err != nil {
				return err
			}
			r.rows[s] = rows
			if tr := traceOf(r.ctx); tr != nil {
				tr.rollupRows += int64(len(rows))
			}
			for _, row := range rows {
				if err := r.canceled(); err != nil {
					return err
				}
				if g.form == vocabulary.FormGauge && !observed.Eligible(r.p.maxGap, row.MaxGapMs) {
					g.rollups = false
					break
				}
				// A recorded-local row lands whole in the day its quarter
				// hour names at its offset, which holds only where the
				// offset is whole quarter hours: at +00:01 a row from 23:45Z
				// holds points on both sides of local midnight.
				if r.p.bucket.mode == bucketLocal && !wholeQuarterHours(r.rowGroups[row.Group].offset) {
					g.rollups = false
					break
				}
			}
		}
		if g.rollups && g.form == vocabulary.FormGauge {
			if err := r.planCarries(g); err != nil {
				return err
			}
		}
	}
	want := map[*aggStream][][2]time.Time{}
	for _, g := range groups {
		for _, s := range g.raw {
			if g.rollups {
				if g.form != vocabulary.FormGauge {
					want[s] = r.fragments()
				}
				continue
			}
			want[s] = r.segments()
		}
		if r.p.source != substrate.AggregateFromRaw {
			for _, s := range g.sums {
				want[s] = r.segments()
			}
		}
	}
	var total int64
	for s, spans := range want {
		if len(spans) == 0 {
			continue
		}
		n, err := r.countPoints(s, spans)
		if err != nil {
			return err
		}
		total += n
	}
	if err := r.charge(total, "to reduce"); err != nil {
		return err
	}
	for s, spans := range want {
		if len(spans) == 0 {
			r.series[s] = &aggSeries{}
			continue
		}
		ser, err := r.readSeries(s, spans)
		if err != nil {
			return err
		}
		r.series[s] = ser
		if tr := traceOf(r.ctx); tr != nil {
			for _, seg := range ser.segs {
				tr.rawPoints += int64(len(seg.points))
			}
		}
	}
	return nil
}

// planCarries reads the value current at each window's start for a gauge
// group served from rollups. On a recorded-local read, a value that still
// holds into its window at an offset that is not whole quarter hours sends
// the group to raw points, as such a row does: walkHeld puts a slot's carried
// time whole in the day the slot's start names, and `open` at +00:01 from
// 23:44Z, held into the slot from 23:45Z, crosses local midnight at 23:59Z.
// It runs before the raw points are counted, so they are budgeted.
func (r *aggRun) planCarries(g *aggGroup) error {
	windows := r.segments()
	for _, s := range g.raw {
		c, err := r.readCarries(s, windows)
		if err != nil {
			return err
		}
		r.carries[s] = c
		if r.p.bucket.mode != bucketLocal {
			continue
		}
		for i, p := range c {
			if p == nil || p.Unavailable || carryEnded(p, floor15(windows[i][0]), r.p.maxGap) {
				continue
			}
			if !wholeQuarterHours(r.rowGroups[p.Group].offset) {
				g.rollups = false
			}
		}
	}
	if !g.rollups {
		for _, s := range g.raw {
			delete(r.carries, s)
		}
	}
	return nil
}

// wholeQuarterHours reports whether a UTC offset in minutes puts local
// midnight on a rollup row's boundary.
func wholeQuarterHours(offset int64) bool { return offset%int64(bucketWidth/time.Minute) == 0 }

// charge counts n more raw points against maxSamples before a statement
// reads them, and refuses the read once the points counted so far (the forms'
// discovery, the reduction's, the coverage's) pass it. what says what the n
// are for.
func (r *aggRun) charge(n int64, what string) error {
	r.samples += n
	if r.samples <= r.p.maxSamples {
		return nil
	}
	return refuse("aggregate.budget.maxSamples", "the read needs %d raw points (%d of them %s), over %d; narrow the range, use reducers rollups serve (not percentile or durationIn, and no maxGap tighter than the data), or raise maxSamples (the cap is %d)",
		r.samples, n, what, r.p.maxSamples, substrate.AggregateMaxSamplesCap)
}

// rollupable reports whether every reducer of the read merges exactly from a
// group's rollup rows in every bucket.
func (r *aggRun) rollupable(g *aggGroup) bool {
	p := r.p
	if p.forceRaw || len(g.raw) == 0 {
		return false
	}
	if p.policy == substrate.SourcesPriority && len(g.raw) > 1 {
		return false
	}
	for _, red := range p.reducers {
		switch red.Name {
		case substrate.ReducerPercentile, substrate.ReducerDurationIn:
			return false
		}
	}
	if p.bucket.mode == bucketLocal {
		if p.bucket.calendar == "minute" || p.bucket.offset%bucketWidth != 0 {
			return false
		}
		return aligned15(p.from) && aligned15(p.to)
	}
	if g.form == vocabulary.FormGauge {
		for _, b := range r.buckets {
			if !b.aligned() {
				return false
			}
		}
	}
	return true
}

func aligned15(t time.Time) bool { return t.Truncate(bucketWidth).Equal(t) }

func floor15(t time.Time) time.Time { return t.UTC().Truncate(bucketWidth) }

func ceil15(t time.Time) time.Time {
	f := floor15(t)
	if f.Equal(t) {
		return f
	}
	return f.Add(bucketWidth)
}

// segments are the spans a raw stream is read over: the read's windows.
func (r *aggRun) segments() [][2]time.Time { return r.p.windows(r.buckets) }

// split is a bucket's whole rollup rows and the ends that cut a row.
func split(b *aggBucket) (whole [2]time.Time, frags [][2]time.Time) {
	cs, fe := ceil15(b.start), floor15(b.end)
	if !cs.Before(fe) {
		return [2]time.Time{}, [][2]time.Time{{b.start, b.end}}
	}
	if b.start.Before(cs) {
		frags = append(frags, [2]time.Time{b.start, cs})
	}
	if fe.Before(b.end) {
		frags = append(frags, [2]time.Time{fe, b.end})
	}
	return [2]time.Time{cs, fe}, frags
}

// fragments are the bucket ends an amount's or a counter's rollup-served
// read takes from raw points.
func (r *aggRun) fragments() [][2]time.Time {
	if r.p.bucket.mode == bucketLocal {
		return nil
	}
	var spans [][2]time.Time
	for _, b := range r.buckets {
		_, frags := split(b)
		spans = append(spans, frags...)
	}
	return joinSpans(spans)
}

func joinSpans(spans [][2]time.Time) [][2]time.Time {
	sort.Slice(spans, func(i, j int) bool { return spans[i][0].Before(spans[j][0]) })
	var out [][2]time.Time
	for _, s := range spans {
		if n := len(out); n > 0 && !s[0].After(out[n-1][1]) {
			if s[1].After(out[n-1][1]) {
				out[n-1][1] = s[1]
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// --- reads ---

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
		SELECT coalesce(sum(x.c), 0)::bigint FROM unnest($7::timestamptz[], $8::timestamptz[]) AS s(a, b)
		CROSS JOIN LATERAL (
			SELECT count(*) AS c FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts >= s.a AND p.ts < s.b) x`,
		append(s.hold().args(), from, to)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: aggregate count: %w", err)
	}
	return n, nil
}

// aggPointLead are the columns read before pointColumns.
const aggPointLead = `p.record_id, p.ord, coalesce(p.statistic, ''), `

// scanAggPoint reads aggPointLead and pointColumns after any leading
// columns, and converts a non-counter's value into the read's unit.
func (r *aggRun) scanAggPoint(rows *sql.Rows, s *aggStream, lead ...any) (aggPoint, error) {
	var ap aggPoint
	sp, err := scanPoint(rows, append(lead, &ap.rid, &ap.ord, &ap.stat)...)
	if err != nil {
		return ap, err
	}
	ap.Point, ap.off, ap.delta = sp.point, sp.group.offset, sp.group.delta
	ap.Group = strconv.FormatInt(ap.off, 10)
	ap.RecordID, ap.Ord = ap.rid, int64(ap.ord)
	if err := r.convertPoint(&ap, s); err != nil {
		return ap, err
	}
	return ap, nil
}

// convertPoint converts a point's number into the read's unit. A counter's
// stays as stored: its steps are taken on the stored digits, in the stored
// unit, as its rollup rows take them.
func (r *aggRun) convertPoint(ap *aggPoint, s *aggStream) error {
	if !ap.HasValue || ap.Unit == "" || s.form == vocabulary.FormCounter {
		return nil
	}
	c, err := r.conversion(ap.Unit, ap.delta, s.source)
	if err != nil {
		return err
	}
	if c.identity {
		return nil
	}
	ap.Value = ap.Value*c.ff + c.fo
	digits, exact := c.digits(ap.Exact)
	if !exact {
		r.meta.inexact = true
	}
	ap.Exact, ap.Unit = digits, r.u.value
	return nil
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
	args := append(s.hold().args(), from, to)
	ser := &aggSeries{segs: make([]aggSegment, len(spans))}
	for i, sp := range spans {
		ser.segs[i].from, ser.segs[i].to = sp[0], sp[1]
	}
	rows, err := r.tx.QueryContext(r.ctx, `
		SELECT s.i, `+aggPointLead+pointColumns+`
		FROM unnest($7::timestamptz[], $8::timestamptz[]) WITH ORDINALITY AS s(a, b, i)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts >= s.a AND p.ts < s.b OFFSET 0) p
		ORDER BY s.i, p.ts, p.record_id, p.ord`, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate points: %w", err)
	}
	for rows.Next() {
		var i int
		ap, err := r.scanAggPoint(rows, s, &i)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		ser.segs[i-1].points = append(ser.segs[i-1].points, ap)
	}
	if err := finishRows(rows); err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate points: %w", err)
	}
	for _, x := range []struct {
		filter string
		dst    func(seg *aggSegment, p *aggPoint)
	}{
		{"TRUE", func(seg *aggSegment, p *aggPoint) { seg.carry = p }},
		{availablePoint, func(seg *aggSegment, p *aggPoint) { seg.prev = p }},
	} {
		if x.filter == availablePoint && s.form != vocabulary.FormCounter {
			continue
		}
		rows, err := r.tx.QueryContext(r.ctx, `
			SELECT s.i, `+aggPointLead+pointColumns+`
			FROM unnest($7::timestamptz[], $8::timestamptz[]) WITH ORDINALITY AS s(a, b, i)
			CROSS JOIN LATERAL (
				SELECT * FROM observed_points p
				WHERE `+holdStreamSQL+` AND p.ts < s.a AND `+x.filter+`
				ORDER BY p.ts DESC, p.record_id DESC, p.ord DESC LIMIT 1) p`, args...)
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate carry: %w", err)
		}
		for rows.Next() {
			var i int
			ap, err := r.scanAggPoint(rows, s, &i)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			x.dst(&ser.segs[i-1], &ap)
		}
		if err := finishRows(rows); err != nil {
			return nil, fmt.Errorf("substrate/engine: aggregate carry: %w", err)
		}
	}
	return ser, nil
}

// readCarries is, for each window, the point current at its start (available
// or not), its group set to its rollup row's, for HeldFromRowsCarry; nil
// where the stream has no point before it.
func (r *aggRun) readCarries(s *aggStream, windows [][2]time.Time) ([]*observed.Point, error) {
	starts := make([]time.Time, len(windows))
	for i, w := range windows {
		starts[i] = w[0]
	}
	rows, err := r.tx.QueryContext(r.ctx, `
		SELECT s.i, `+pointColumns+`
		FROM unnest($7::timestamptz[]) WITH ORDINALITY AS s(a, i)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts < s.a
			ORDER BY p.ts DESC, p.record_id DESC, p.ord DESC LIMIT 1) p`,
		append(s.hold().args(), starts)...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate carry: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*observed.Point, len(windows))
	for rows.Next() {
		var i int
		sp, err := scanPoint(rows, &i)
		if err != nil {
			return nil, err
		}
		r.rowGroups[sp.point.Group] = sp.group
		p := sp.point
		out[i-1] = &p
	}
	return out, rows.Err()
}

// rollupColumns are the columns of observed_rollups a read takes.
const rollupColumns = `r.code, r.unit, r.delta, r.utc_offset_min, r.bucket15, r.n, r.bounded, r.sum, r.min, r.max, r.mean, r.m2,
	r.first_ts, r.first_value::text, r.last_ts, r.last_value::text, r.covered_ms, r.held_ms, r.twa_sum, r.last_hold_ms,
	r.max_gap_ms, r.start_gap_ms, r.increase::text, r.resets, r.sin_sum, r.cos_sum, r.span_end,
	r.first_record_id, r.first_ord, r.last_record_id, r.last_ord, r.min_value::text, r.max_value::text`

// rowSpans are the rollup rows the read's windows meet: each window from the
// row holding its start to the row holding its end, joined.
func (r *aggRun) rowSpans() [][2]time.Time {
	var spans [][2]time.Time
	for _, w := range r.segments() {
		spans = append(spans, [2]time.Time{floor15(w[0]), ceil15(w[1])})
	}
	return joinSpans(spans)
}

// readRollups reads one stream's rollup rows over the read's windows: the
// range, or every records-mode window, which is not clipped to the range.
func (r *aggRun) readRollups(s *aggStream) ([]observed.Row, error) {
	from, to := spanArrays(r.rowSpans())
	rows, err := r.tx.QueryContext(r.ctx, `SELECT `+rollupColumns+`
		FROM unnest($7::timestamptz[], $8::timestamptz[]) AS w(a, b)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_rollups r
			WHERE r.metric = $1 AND r.kind = $2 AND r.source = $3 AND r.about = $4 AND r.channel = $5 AND r.form = $6
			  AND r.bucket15 >= w.a AND r.bucket15 < w.b) r
		ORDER BY r.bucket15, r.code, r.unit, r.delta, r.utc_offset_min`,
		append(s.hold().args(), from, to)...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: aggregate rollups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []observed.Row
	for rows.Next() {
		var (
			g                                    obsGroup
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
		if err := rows.Scan(&g.code, &g.unit, &g.delta, &g.offset, &bucket, &n, &bounded, &sum, &minV, &maxV, &mean, &m2,
			&firstTS, &firstV, &lastTS, &lastV, &covered, &held, &twa, &lastHold, &maxGap, &startGp, &increase, &resets, &sin, &cos,
			&spanEnd, &firstRecord, &firstOrd, &lastRecord, &lastOrd, &minExact, &maxExact); err != nil {
			return nil, err
		}
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

// --- the run ---

// aggOut is one output row before fill and rolling.
type aggOut struct {
	bucket int
	group  *aggGroup
	code   string
	row    substrate.AggregateRow
}

// run reduces every group into rows. Every group is evaluated before any
// row is written, because a recorded-local read creates buckets as it goes.
func (r *aggRun) run(groups []*aggGroup) ([]aggOut, error) {
	raws, sums := make([]aggCells, len(groups)), make([]aggCells, len(groups))
	for gi, g := range groups {
		var err error
		if len(g.raw) > 0 && r.p.source != substrate.AggregateFromSummaries {
			if g.rollups {
				raws[gi], err = r.evalRollups(g)
			} else {
				raws[gi], err = r.evalRaw(g, g.raw, g.form)
			}
			if err != nil {
				return nil, err
			}
		}
		if len(g.sums) > 0 && r.p.source != substrate.AggregateFromRaw {
			if sums[gi], err = r.evalRaw(g, g.sums, vocabulary.FormSummary); err != nil {
				return nil, err
			}
		}
	}
	if n := len(r.buckets) * max(r.nSeries, 1); n > r.p.maxBuckets {
		return nil, refuse("aggregate.budget.maxBuckets", "the points fall in %d recorded-local buckets, times %d series is %d, over %d; narrow the range or raise maxBuckets",
			len(r.buckets), r.nSeries, n, r.p.maxBuckets)
	}
	var out []aggOut
	codeGroups := 0
	for gi, g := range groups {
		seen := map[string]bool{}
		for bi, b := range r.buckets {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			codes := map[string]bool{}
			for c := range raws[gi][b] {
				codes[c] = true
			}
			for c := range sums[gi][b] {
				codes[c] = true
			}
			for _, code := range sortedKeys(codes) {
				ev, from := r.choose(b, raws[gi][b][code], sums[gi][b][code])
				if ev == nil {
					continue
				}
				if !seen[code] {
					seen[code] = true
					codeGroups++
				}
				row, err := r.row(b, g, code, ev, from)
				if err != nil {
					return nil, err
				}
				out = append(out, aggOut{bucket: bi, group: g, code: code, row: row})
			}
		}
	}
	if codeGroups > r.p.maxGroups {
		return nil, refuse("aggregate.budget.maxGroups", "grouping by code makes %d groups, over %d; filter the codes or raise maxGroups (the cap is %d)",
			codeGroups, r.p.maxGroups, substrate.AggregateMaxGroupsCap)
	}
	return out, nil
}

// choose picks a bucket's evidence: raw points or summaries, never both;
// prefer-raw takes raw where the bucket holds a raw point and its coverage
// reaches minCoverage. A gauge's coverage counts a value held in from before
// the bucket, which alone is no reading of it.
func (r *aggRun) choose(b *aggBucket, raw, sums *aggEvidence) (*aggEvidence, string) {
	switch {
	case raw == nil && sums == nil:
		return nil, ""
	case sums == nil:
		return raw, substrate.AggregateFromRaw
	case raw == nil:
		return sums, substrate.AggregateFromSummaries
	}
	if length := b.length(&raw.ends, r.p.from, r.p.to).Milliseconds(); raw.count > 0 && length > 0 && float64(raw.coverage)/float64(length) >= r.p.minCoverage {
		return raw, substrate.AggregateFromRaw
	}
	return sums, substrate.AggregateFromSummaries
}

// codeKey is the row a point or a rollup row of a code lands in.
func (r *aggRun) codeKey(code string) string {
	if r.p.grouped[substrate.GroupCode] {
		return code
	}
	return ""
}

// admits reports whether filter.properties.code lets a code through.
func (r *aggRun) admits(code string) bool { return r.p.codes == nil || r.p.codes[code] }

// aggCells holds the evidence of each bucket and code. It is keyed by the
// bucket itself: a recorded-local read creates buckets as points name them.
type aggCells map[*aggBucket]map[string]*aggEvidence

func cell(m aggCells, b *aggBucket, code, form string, raw bool) *aggEvidence {
	if m[b] == nil {
		m[b] = map[string]*aggEvidence{}
	}
	ev := m[b][code]
	if ev == nil {
		ev = newEvidence(form, raw)
		m[b][code] = ev
	}
	return ev
}

// --- raw ---

// canceled counts one step of the reduction and answers the read's context
// error on the first step and every thousand after it, whichever loop takes
// them: the statement timeout bounds the SQL, not the work after it, so a
// long reduction stops when its caller does. A step is one point, span, row,
// slot or interval this package copies or walks; what runs in observed (a
// reducer, a sort, a priority resolution) checks the context through its
// Context forms, as often.
func (r *aggRun) canceled() error {
	n := r.steps
	r.steps++
	if n&(1<<10-1) != 0 {
		return nil
	}
	return r.ctx.Err()
}

// evalRaw reduces a group's streams from raw points: held intervals built
// per stream from every transition, the source policy resolved over the
// range, then each bucket's members and clipped intervals.
func (r *aggRun) evalRaw(g *aggGroup, streams []*aggStream, form string) (aggCells, error) {
	p := r.p
	type stream struct {
		s      *aggStream
		points []aggPoint // every point read, carries and prev included, in time order
		spans  []aggSpan
		// prevOf is, for a counter, the last available point before each
		// point, by the point's index.
		prevOf map[int]*aggPoint
	}
	var list []*stream
	for si, s := range streams {
		st := &stream{s: s, prevOf: map[int]*aggPoint{}}
		for _, seg := range r.series[s].segs {
			if form == vocabulary.FormGauge {
				slice := make([]aggPoint, 0, len(seg.points)+1)
				if seg.carry != nil {
					slice = append(slice, *seg.carry)
				}
				for _, ap := range seg.points {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					slice = append(slice, ap)
				}
				obs := make([]observed.Point, len(slice))
				for i := range slice {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					obs[i] = slice[i].Point
				}
				held, err := observed.HeldContext(r.ctx, obs, seg.from, seg.to, p.maxGap)
				if err != nil {
					return nil, err
				}
				for _, iv := range held {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					st.spans = append(st.spans, aggSpan{
						Span: observed.Span{Start: iv.Start, End: iv.End, Point: obs[iv.Point]},
						off:  slice[iv.Point].off, stream: si,
					})
				}
			}
			// A counter's step runs from the stream's previous available
			// point, read before the segment where the segment starts.
			prev := seg.prev
			for _, ap := range seg.points {
				if err := r.canceled(); err != nil {
					return nil, err
				}
				ap.stream = si
				idx := len(st.points)
				st.points = append(st.points, ap)
				if form == vocabulary.FormCounter && prev != nil && !ap.Unavailable {
					pp := *prev
					st.prevOf[idx] = &pp
				}
				if !ap.Unavailable {
					cp := ap
					prev = &cp
				}
			}
		}
		list = append(list, st)
	}
	// The source policy, resolved over the range before any bucket.
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if p.policy == substrate.SourcesPriority && len(list) > 1 {
		switch form {
		case vocabulary.FormGauge:
			var cover []aggSpan
			in := make([][]observed.Span, len(list))
			for i, st := range list {
				for _, sp := range st.spans {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					in[i] = append(in[i], sp.Span)
				}
			}
			resolved, err := observed.Prioritize(r.ctx, in)
			if err != nil {
				return nil, err
			}
			for i, st := range list {
				if err := r.ctx.Err(); err != nil {
					return nil, err
				}
				higher := make([][2]time.Time, 0, len(cover))
				for _, c := range cover {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					higher = append(higher, [2]time.Time{c.Start, c.End})
				}
				hc, err := r.cover(higher)
				if err != nil {
					return nil, err
				}
				kept := st.points[:0]
				for _, ap := range st.points {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					if ap.Unavailable || !hc.holds(ap.At) {
						kept = append(kept, ap)
					}
				}
				st.points = kept
				for _, sp := range st.spans {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					cover = append(cover, sp)
				}
				st.spans = st.spans[:0]
				for _, sp := range resolved[i] {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					off, _ := strconv.ParseInt(sp.Point.Group, 10, 64)
					st.spans = append(st.spans, aggSpan{Span: sp, off: off, stream: i})
				}
			}
		default:
			var cover [][2]time.Time
			for _, st := range list {
				hc, err := r.cover(cover)
				if err != nil {
					return nil, err
				}
				var kept []aggPoint
				for _, ap := range st.points {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					if !ap.Unavailable && hc.overlaps(ap.At, pointEnd(ap)) {
						continue
					}
					kept = append(kept, ap)
				}
				st.points = kept
				for _, ap := range kept {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					if !ap.Unavailable {
						cover = append(cover, [2]time.Time{ap.At, pointEnd(ap)})
					}
				}
			}
		}
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	out := aggCells{}
	for _, st := range list {
		for i := range st.points {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			ap := st.points[i]
			if ap.Unavailable || !r.admits(ap.Code) {
				continue
			}
			for _, b := range r.bucketsOf(ap.At, ap.off) {
				ev := cell(out, b, r.codeKey(ap.Code), form, true)
				if b.local {
					b.ends.mark(ap.Point, ap.stream, ap.off)
					ev.ends.mark(ap.Point, ap.stream, ap.off)
				}
				ev.members = append(ev.members, ap)
				ev.sources[st.s.source] = true
				if prev := st.prevOf[i]; prev != nil {
					d, reset := observed.Step(prev.Point, ap.Point)
					c, err := r.conversion(ap.Unit, false, st.s.source)
					if err != nil {
						return nil, err
					}
					if !c.identity {
						d.Mul(d, c.f)
					}
					ev.steps.add(d, reset, ap.At.Sub(prev.At).Milliseconds())
				}
			}
		}
		for _, sp := range st.spans {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			if !r.admits(sp.Point.Code) {
				continue
			}
			for _, part := range r.spanParts(sp) {
				ev := cell(out, part.bucket, r.codeKey(sp.Point.Code), form, true)
				ev.spans = append(ev.spans, part.span)
				ev.sources[st.s.source] = true
				if b := part.bucket; b.local {
					b.ends.hold(part.span.Start, part.span.End, sp.stream, sp.off)
					ev.ends.hold(part.span.Start, part.span.End, sp.stream, sp.off)
				}
			}
		}
	}
	// The summaries policy reads one source per bucket: the first in its
	// order with a summary there.
	if p.policy == substrate.SourcesSummaries && form == vocabulary.FormSummary {
		for _, codes := range out {
			for _, ev := range codes {
				if err := r.firstSummarySource(ev, streams); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, codes := range out {
		for _, ev := range codes {
			// A cell's sorts are as long as its points and spans, so they
			// check the context as they go.
			if err := observed.SortStableFunc(r.ctx, ev.members, func(a, b aggPoint) int {
				switch {
				case pointBefore(a.Point, a.stream, b.Point, b.stream):
					return -1
				case pointBefore(b.Point, b.stream, a.Point, a.stream):
					return 1
				}
				return 0
			}); err != nil {
				return nil, err
			}
			if err := observed.SortStableFunc(r.ctx, ev.spans, func(a, b aggSpan) int { return a.Start.Compare(b.Start) }); err != nil {
				return nil, err
			}
			for _, m := range ev.members {
				if err := r.canceled(); err != nil {
					return nil, err
				}
				ev.count++
				if m.Bounded {
					ev.bounded++
				}
			}
			switch form {
			case vocabulary.FormGauge:
				spans, err := r.spansOf(ev)
				if err != nil {
					return nil, err
				}
				if ev.coverage, err = observed.HeldMsContext(r.ctx, spans); err != nil {
					return nil, err
				}
			case vocabulary.FormCounter:
				ev.coverage = ev.steps.covered
			default:
				var err error
				if ev.coverage, err = r.memberCoverage(ev); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// firstSummarySource keeps a cell's members from one source, the first in
// the summaries policy's order with a summary there. A summary bucket's
// members are as many as its sources' summaries, so the scan and the filter
// check the context per step.
func (r *aggRun) firstSummarySource(ev *aggEvidence, streams []*aggStream) error {
	best := -1
	for _, m := range ev.members {
		if err := r.canceled(); err != nil {
			return err
		}
		if best < 0 || m.stream < best {
			best = m.stream
		}
	}
	kept := ev.members[:0]
	for _, m := range ev.members {
		if err := r.canceled(); err != nil {
			return err
		}
		if m.stream == best {
			kept = append(kept, m)
		}
	}
	clear(ev.members[len(kept):])
	ev.members = kept
	ev.sources = map[string]bool{}
	if best >= 0 {
		ev.sources[streams[best].source] = true
	}
	return nil
}

// memberCoverage is the time a cell's raw members' spans cover, joined, each
// span built, joined and summed with the context checked per step.
func (r *aggRun) memberCoverage(ev *aggEvidence) (int64, error) {
	spans := make([][2]time.Time, 0, len(ev.members))
	for _, m := range ev.members {
		if err := r.canceled(); err != nil {
			return 0, err
		}
		spans = append(spans, [2]time.Time{m.At, m.End})
	}
	joined, err := r.cover(spans)
	if err != nil {
		return 0, err
	}
	var ms int64
	for _, s := range joined {
		if err := r.canceled(); err != nil {
			return 0, err
		}
		ms += s[1].Sub(s[0]).Milliseconds()
	}
	return ms, nil
}

func pointEnd(ap aggPoint) time.Time {
	if ap.End.After(ap.At) {
		return ap.End
	}
	return ap.At.Add(time.Nanosecond)
}

// cover is a sorted, joined set of intervals.
type cover [][2]time.Time

// cover joins the intervals as joinSpans does, checking the context as it
// sorts and joins them: a cover is as long as a stream's points. It sorts
// spans in place, so a caller hands over a slice whose order it does not
// need, and is spared a copy as long as the cover.
func (r *aggRun) cover(spans [][2]time.Time) (cover, error) {
	if err := observed.SortStableFunc(r.ctx, spans, func(a, b [2]time.Time) int { return a[0].Compare(b[0]) }); err != nil {
		return nil, err
	}
	var out cover
	for _, s := range spans {
		if err := r.canceled(); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && !s[0].After(out[n-1][1]) {
			if s[1].After(out[n-1][1]) {
				out[n-1][1] = s[1]
			}
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// holds reports whether t lies inside the cover.
func (c cover) holds(t time.Time) bool {
	i := sort.Search(len(c), func(i int) bool { return c[i][1].After(t) })
	return i < len(c) && !t.Before(c[i][0])
}

// overlaps reports whether [a, b) meets the cover.
func (c cover) overlaps(a, b time.Time) bool {
	i := sort.Search(len(c), func(i int) bool { return c[i][1].After(a) })
	return i < len(c) && c[i][0].Before(b)
}

// bucketsOf names the buckets a point at t with the offset lands in: the
// bucket holding its instant, every records-mode window holding it, or the
// recorded-local bucket of its own wall clock, made where the range's own
// enumeration did not reach it. It marks no bucket's first or last point;
// the callers that hold a point do (aggBucket.mark).
func (r *aggRun) bucketsOf(t time.Time, off int64) []*aggBucket {
	mode := r.p.bucket.mode
	if mode != bucketRecords && (t.Before(r.p.from) || !t.Before(r.p.to)) {
		return nil
	}
	switch mode {
	case bucketLocal:
		var b *aggBucket
		r.buckets, b = localBucketAt(r.buckets, wallOf(t, off), r.p.bucket)
		return []*aggBucket{b}
	case bucketRecords:
		var out []*aggBucket
		for _, b := range r.buckets {
			if !t.Before(b.start) && t.Before(b.end) {
				out = append(out, b)
			}
		}
		return out
	}
	i := sort.Search(len(r.buckets), func(i int) bool { return r.buckets[i].end.After(t) })
	if i < len(r.buckets) && !t.Before(r.buckets[i].start) {
		return []*aggBucket{r.buckets[i]}
	}
	return nil
}

type spanPart struct {
	bucket *aggBucket
	span   aggSpan
}

// spanParts cuts a held interval at the buckets' boundaries: a UTC bucket's, or
// for a recorded-local bucket the boundaries at the holding point's own offset.
func (r *aggRun) spanParts(sp aggSpan) []spanPart {
	var out []spanPart
	switch r.p.bucket.mode {
	case bucketLocal:
		shift := time.Duration(sp.off) * time.Minute
		start := sp.Start
		for start.Before(sp.End) {
			wall := start.UTC().Add(shift)
			var b *aggBucket
			r.buckets, b = localBucketAt(r.buckets, wall, r.p.bucket)
			end := b.end.Add(-shift)
			if end.After(sp.End) {
				end = sp.End
			}
			part := sp
			part.Start, part.End = start, end
			out = append(out, spanPart{bucket: b, span: part})
			start = end
		}
		return out
	}
	for _, b := range r.bucketsMeeting(sp.Start, sp.End) {
		for _, c := range observed.Clip([]observed.Span{sp.Span}, b.start, b.end) {
			part := sp
			part.Span = c
			out = append(out, spanPart{bucket: b, span: part})
		}
	}
	return out
}

// bucketsMeeting lists the UTC buckets [from, to) meets.
func (r *aggRun) bucketsMeeting(from, to time.Time) []*aggBucket {
	var out []*aggBucket
	if r.p.bucket.mode == bucketRecords {
		for _, b := range r.buckets {
			if b.start.Before(to) && b.end.After(from) {
				out = append(out, b)
			}
		}
		return out
	}
	i := sort.Search(len(r.buckets), func(i int) bool { return r.buckets[i].end.After(from) })
	for ; i < len(r.buckets) && r.buckets[i].start.Before(to); i++ {
		out = append(out, r.buckets[i])
	}
	return out
}

// --- rollups ---

// evalRollups reduces a group from its rollup rows: whole rows merged after
// converting each from its own unit, an amount's or a counter's cut bucket ends
// from raw points, a gauge's held time walked across rows with its carry.
func (r *aggRun) evalRollups(g *aggGroup) (aggCells, error) {
	out := aggCells{}
	local := r.p.bucket.mode == bucketLocal
	tr := traceOf(r.ctx)
	for si, s := range g.raw {
		rows := r.rows[s]
		for _, row := range rows {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			if tr != nil {
				tr.mergedRows++
			}
			og := r.rowGroups[row.Group]
			if !r.admits(og.code) {
				continue
			}
			var targets []*aggBucket
			if local {
				// The row lands in the day its quarter hour names at its
				// offset; the day's first and last points are the rows' own.
				targets = r.bucketsOf(row.Start, og.offset)
			} else {
				for _, b := range r.bucketsMeeting(row.Start, row.Start.Add(bucketWidth)) {
					whole, _ := split(b)
					if !row.Start.Before(whole[0]) && row.Start.Before(whole[1]) {
						targets = append(targets, b)
					}
				}
			}
			if len(targets) == 0 {
				continue
			}
			st, err := r.convertStats(row.Stats, og, s.source)
			if err != nil {
				return nil, err
			}
			for _, b := range targets {
				ev := cell(out, b, r.codeKey(og.code), g.form, false)
				if local && row.First != nil {
					for _, pt := range []*observed.Point{row.First, row.Last} {
						b.ends.mark(*pt, si, og.offset)
						ev.ends.mark(*pt, si, og.offset)
					}
				}
				r.mergeStats(ev, st, g.form, si)
				if row.N > 0 {
					ev.sources[s.source] = true
				}
				switch {
				case g.form == vocabulary.FormAmount && row.First != nil:
					ev.extents = append(ev.extents, rowExtent(row.Stats))
				case g.form == vocabulary.FormCounter:
					ev.coverage += row.CoveredMs
				}
				if st.Increase != nil {
					ev.steps.add(st.Increase, false, row.CoveredMs)
					ev.steps.resets += row.Resets
				}
			}
		}
		if g.form != vocabulary.FormGauge && !local {
			if err := r.rollupFragments(out, g, s, si); err != nil {
				return nil, err
			}
		}
		if g.form == vocabulary.FormGauge {
			for i, w := range r.segments() {
				if err := r.walkHeld(out, g, s, si, rows, w, r.carries[s][i]); err != nil {
					return nil, err
				}
			}
		}
	}
	if g.form == vocabulary.FormAmount {
		if err := r.amountCoverage(g, out); err != nil {
			return nil, err
		}
	}
	for _, codes := range out {
		for _, ev := range codes {
			ev.count, ev.bounded = ev.stats.N, ev.stats.Bounded
		}
	}
	return out, nil
}

// convertStats converts one rollup row's statistics from its own unit into
// the read's: an affine map moves the sum by the count, the mean and the
// extremes by the offset, m2 by the factor squared; a counter's increase is a
// difference and takes the factor alone.
func (r *aggRun) convertStats(s observed.Stats, g obsGroup, source string) (observed.Stats, error) {
	if g.unit == "" {
		return s, nil
	}
	c, err := r.conversion(g.unit, g.delta, source)
	if err != nil {
		return s, err
	}
	if c.identity {
		return s, nil
	}
	f, o := c.ff, c.fo
	s.Sum = f*s.Sum + o*float64(s.Values)
	s.Min, s.Max, s.Mean = f*s.Min+o, f*s.Max+o, f*s.Mean+o
	s.M2 *= f * f
	s.TWASum = f*s.TWASum + o*float64(s.HeldMs)
	if s.Increase != nil {
		s.Increase = new(big.Rat).Mul(s.Increase, c.f)
	}
	conv := func(p *observed.Point) *observed.Point {
		if p == nil || !p.HasValue {
			return p
		}
		q := *p
		digits, exact := c.digits(p.Exact)
		if !exact {
			r.meta.inexact = true
		}
		q.Exact, q.Unit = digits, r.u.value
		q.Value = p.Value*f + o
		return &q
	}
	s.First, s.Last = conv(s.First), conv(s.Last)
	for _, d := range []*string{&s.MinExact, &s.MaxExact} {
		digits, exact := c.digits(*d)
		if !exact && r.wants(substrate.ReducerMin, substrate.ReducerMax) {
			r.meta.inexact = true
		}
		*d = digits
	}
	return s, nil
}

// wants reports whether the read asks for any of the reducers.
func (r *aggRun) wants(names ...string) bool {
	for _, red := range r.p.reducers {
		if slices.Contains(names, red.Name) {
			return true
		}
	}
	return false
}

// mergeStats merges converted statistics of the group's stream-th stream into
// a bucket's, keeping the first and last point in the order raw points are
// read in (pointBefore: the instant, the record id, the index, the stream),
// whatever order the rows arrive in: two groups of one bucket interleave, and
// two at one instant tie on time alone.
func (r *aggRun) mergeStats(ev *aggEvidence, s observed.Stats, form string, stream int) {
	first, last := ev.stats.First, ev.stats.Last
	ev.stats = observed.Merge(form, ev.stats, s)
	ev.stats.First, ev.stats.Last = first, last
	if s.First != nil && (first == nil || pointBefore(*s.First, stream, *first, ev.firstStream)) {
		ev.stats.First, ev.firstStream = s.First, stream
	}
	if s.Last != nil && (last == nil || pointBefore(*last, ev.lastStream, *s.Last, stream)) {
		ev.stats.Last, ev.lastStream = s.Last, stream
	}
}

// rowExtent is an amount row's coverage as UnionMs joins it.
func rowExtent(s observed.Stats) observed.Extent {
	return observed.Extent{Start: s.First.At, End: s.SpanEnd, Ms: s.CoveredMs}
}

// amountCoveragePoints are the raw spans of a group's amount streams ($1 to
// $5) in one cell c of amountCoverageCells: the points a raw read takes into
// the cell, those whose instant lies in [a, b), on a recorded-local read
// ($12) whose wall clock at their own offset lies in [wa, wb), of the cell's
// code where the read groups by code ($13), and of the admitted codes ($11)
// where the filter names some ($14).
const amountCoveragePoints = `
	SELECT p.ts, p.ts_end AS e
	FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]) AS s(metric, kind, source, about, channel)
	CROSS JOIN LATERAL (
		SELECT p.ts, p.ts_end FROM observed_points p
		WHERE p.metric = s.metric AND p.kind = s.kind AND p.source = s.source AND p.about = s.about AND p.channel = s.channel
		  AND p.ts >= c.a AND p.ts < c.b AND p.form = 'amount' AND ` + availablePoint + `
		  AND (NOT $12::boolean OR (p.ts + p.utc_offset_min * interval '1 minute' >= c.wa
		                            AND p.ts + p.utc_offset_min * interval '1 minute' < c.wb))
		  AND (NOT $13::boolean OR p.code = c.code)
		  AND (NOT $14::boolean OR p.code = ANY($11::text[]))) p`

// amountCoverageCells are the cells ($6 to $10) whose coverage is taken from
// the raw spans.
const amountCoverageCells = `unnest($6::timestamptz[], $7::timestamptz[], $8::timestamptz[], $9::timestamptz[], $10::text[]) WITH ORDINALITY AS c(a, b, wa, wb, code, i)`

// amountCoverageCountSQL counts the spans amountCoverageSQL reduces, every
// cell's, before it runs: they count against maxSamples like any raw point
// a read takes.
const amountCoverageCountSQL = `
	SELECT coalesce(sum((SELECT count(*) FROM (` + amountCoveragePoints + `) q)), 0)::bigint FROM ` + amountCoverageCells

// amountCoverageSQL is the union of each cell's spans (amountCoveragePoints).
// Spans that overlap or touch join, as joinSpans joins them, and each joined
// span counts in whole milliseconds.
const amountCoverageSQL = `
	SELECT c.i, coalesce((
		SELECT sum(floor(extract(epoch FROM z.e - z.s) * 1000))::bigint FROM (
			SELECT min(y.ts) AS s, max(y.e) AS e FROM (
				SELECT x.ts, x.e, sum(x.brk) OVER (ORDER BY x.ts, x.e ROWS UNBOUNDED PRECEDING) AS grp FROM (
					SELECT q.ts, q.e, CASE WHEN q.ts <= max(q.e) OVER (ORDER BY q.ts, q.e ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING)
						THEN 0 ELSE 1 END AS brk
					FROM (` + amountCoveragePoints + `) q
				) x
			) y GROUP BY y.grp
		) z), 0)
	FROM ` + amountCoverageCells

// amountCoverage settles each cell's coverage, the union of its amounts'
// spans: from the rows' and the cut ends' extents (observed.UnionMs) where no
// row's extent overlaps another, and from the raw spans in SQL in the cells
// where one does, so a cell served from rows reports the coverage, and
// prefer-raw makes the choice, that the raw points would.
func (r *aggRun) amountCoverage(g *aggGroup, cells aggCells) error {
	var pending []*aggEvidence
	var a, b, wa, wb []time.Time
	var codes []string
	local := r.p.bucket.mode == bucketLocal
	for _, bk := range r.buckets {
		for _, code := range sortedKeys(cells[bk]) {
			if err := r.canceled(); err != nil {
				return err
			}
			ev := cells[bk][code]
			ms, ok, err := observed.UnionMsContext(r.ctx, ev.extents)
			if err != nil {
				return err
			}
			if ok {
				ev.coverage = ms
				continue
			}
			pending = append(pending, ev)
			if local {
				a, b, wa, wb = append(a, r.p.from), append(b, r.p.to), append(wa, bk.start), append(wb, bk.end)
			} else {
				a, b, wa, wb = append(a, bk.start), append(b, bk.end), append(wa, bk.start), append(wb, bk.end)
			}
			codes = append(codes, code)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	ids := make([][]string, 5)
	for _, s := range g.raw {
		for i, v := range []string{s.metric, s.kind, s.source, s.about, s.channel} {
			ids[i] = append(ids[i], v)
		}
	}
	admitted := sortedKeys(r.p.codes)
	args := []any{ids[0], ids[1], ids[2], ids[3], ids[4], a, b, wa, wb, codes, admitted, local, r.p.grouped[substrate.GroupCode], r.p.codes != nil}
	var n int64
	if err := r.tx.QueryRowContext(r.ctx, amountCoverageCountSQL, args...).Scan(&n); err != nil {
		return fmt.Errorf("substrate/engine: aggregate coverage count: %w", err)
	}
	if err := r.charge(n, "to join the spans of amounts whose rollup rows overlap"); err != nil {
		return err
	}
	rows, err := r.tx.QueryContext(r.ctx, amountCoverageSQL, args...)
	if err != nil {
		return fmt.Errorf("substrate/engine: aggregate coverage: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var i int
		var ms int64
		if err := rows.Scan(&i, &ms); err != nil {
			return err
		}
		pending[i-1].coverage = ms
	}
	return rows.Err()
}

// rollupFragments merges the raw points of an amount's or a counter's cut bucket
// ends into the buckets, through observed.Bucket as a rollup row would, its
// groups in key order so the merge does not follow a map's. The segments and
// each segment's points are in time order, so a cut end's points are found
// by binary search: a bucket narrower than a rollup row is all cut end, and a
// scan per bucket would take buckets times points.
func (r *aggRun) rollupFragments(out aggCells, g *aggGroup, s *aggStream, si int) error {
	segs := r.series[s].segs
	for _, b := range r.buckets {
		if err := r.canceled(); err != nil {
			return err
		}
		_, frags := split(b)
		for _, e := range frags {
			k := sort.Search(len(segs), func(i int) bool { return !segs[i].to.Before(e[1]) })
			if k == len(segs) || segs[k].from.After(e[0]) {
				continue
			}
			seg := &segs[k]
			lo := sort.Search(len(seg.points), func(i int) bool { return !seg.points[i].At.Before(e[0]) })
			hi := sort.Search(len(seg.points), func(i int) bool { return !seg.points[i].At.Before(e[1]) })
			var pts []observed.Point
			groups := map[string]string{}
			for _, ap := range seg.points[lo:hi] {
				if err := r.canceled(); err != nil {
					return err
				}
				if !r.admits(ap.Code) {
					continue
				}
				q := ap.Point
				ck := r.codeKey(ap.Code)
				q.Group = ck + "\x1f" + ap.Unit
				groups[q.Group] = ck
				pts = append(pts, q)
			}
			if len(pts) == 0 {
				continue
			}
			var prev *observed.Point
			for i := lo - 1; i >= 0; i-- {
				if err := r.canceled(); err != nil {
					return err
				}
				if ap := seg.points[i]; !ap.Unavailable {
					p := ap.Point
					prev = &p
					break
				}
			}
			if prev == nil && seg.prev != nil {
				p := seg.prev.Point
				prev = &p
			}
			stats := observed.Bucket(g.form, e[0], e[1], prev, pts)
			for _, key := range sortedKeys(stats) {
				st := stats[key]
				unit := strings.SplitN(key, "\x1f", 2)[1]
				conv, err := r.convertStats(*st, obsGroup{unit: unit}, s.source)
				if err != nil {
					return err
				}
				ev := cell(out, b, groups[key], g.form, false)
				r.mergeStats(ev, conv, g.form, si)
				if st.N > 0 {
					ev.sources[s.source] = true
				}
				if g.form == vocabulary.FormCounter {
					ev.coverage += st.CoveredMs
				}
				if conv.Increase != nil {
					ev.steps.add(conv.Increase, false, st.CoveredMs)
					ev.steps.resets += st.Resets
				}
			}
			if g.form == vocabulary.FormAmount {
				// A cut end's spans are known exactly, so they join the
				// rows' extents as raw spans.
				for _, p := range pts {
					if err := r.canceled(); err != nil {
						return err
					}
					if !p.Unavailable {
						ev := cell(out, b, r.codeKey(p.Code), g.form, false)
						ev.extents = append(ev.extents, observed.Extent{Start: p.At, End: p.End, Ms: p.End.Sub(p.At).Milliseconds(), Exact: true})
					}
				}
			}
		}
	}
	return nil
}

// walkHeld walks a gauge stream's 15-minute slots across one of the read's
// windows (the range, or a records-mode window, which may reach past the
// range), carrying the held value from row to row (observed.HeldFromRowsCarry)
// from the point current at the window's start, and adds each slot's held
// time to the buckets it lies in: a recorded-local bucket by the offset of the
// point that held.
func (r *aggRun) walkHeld(out aggCells, g *aggGroup, s *aggStream, si int, rows []observed.Row, w [2]time.Time, carry *observed.Point) error {
	from, to := floor15(w[0]), w[1]
	i := sort.Search(len(rows), func(i int) bool { return !rows[i].Start.Before(from) })
	for slot := from; slot.Before(to); {
		if err := r.canceled(); err != nil {
			return err
		}
		for i < len(rows) && rows[i].Start.Before(slot) {
			i++
		}
		j := i
		for j < len(rows) && rows[j].Start.Equal(slot) {
			j++
		}
		end := slot.Add(bucketWidth)
		if i == j {
			// No row here: jump to the next row, the carry holding (or not)
			// across the empty slots, cut at bucket boundaries so each bucket gets
			// its share.
			next := to
			if i < len(rows) && rows[i].Start.Before(to) {
				next = rows[i].Start
			}
			if carry == nil || carryEnded(carry, slot, r.p.maxGap) {
				carry = nil
				slot = next
				continue
			}
			end = r.chunkEnd(slot, next, carry)
		}
		sums, nextCarry, ok := observed.HeldFromRowsCarry(rows[i:j], carry, slot, end, r.p.maxGap)
		if !ok {
			return fmt.Errorf("substrate/engine: aggregate: a rollup row of %s is not eligible after the plan read it so", s.metric)
		}
		// In key order: the sums add as floats, and bucketsOf may make a
		// recorded-local bucket, so a map's order must decide neither.
		for _, key := range sortedKeys(sums) {
			h := sums[key]
			og := r.rowGroups[key]
			if h.Ms == 0 || !r.admits(og.code) {
				continue
			}
			ms, valueMs := h.Ms, h.ValueMs
			if og.unit != "" {
				c, err := r.conversion(og.unit, og.delta, s.source)
				if err != nil {
					return err
				}
				valueMs = c.ff*valueMs + c.fo*float64(ms)
			}
			for _, b := range r.bucketsOf(slot, og.offset) {
				ev := cell(out, b, r.codeKey(og.code), g.form, false)
				if b.local {
					b.ends.hold(slot, end, si, og.offset)
					ev.ends.hold(slot, end, si, og.offset)
				}
				hs := ev.held[og.code]
				hs.Ms += ms
				hs.ValueMs += valueMs
				ev.held[og.code] = hs
				ev.coverage += ms
				ev.sources[s.source] = true
			}
		}
		carry, slot = nextCarry, end
	}
	return nil
}

// carryEnded reports whether a carried value has stopped holding by t.
func carryEnded(c *observed.Point, t time.Time, maxGap time.Duration) bool {
	if c.Hold != nil && !c.At.Add(*c.Hold).After(t) {
		return true
	}
	return maxGap > 0 && !c.At.Add(maxGap).After(t)
}

// chunkEnd is where a carry across empty slots is cut: the next row, the end
// of the bucket the slot lies in (the next start or end of any records-mode
// window, which may overlap), or the window's end.
func (r *aggRun) chunkEnd(slot, next time.Time, carry *observed.Point) time.Time {
	end := next
	switch {
	case r.p.bucket.mode == bucketLocal:
		// The day's end is computed, not looked up: a bucket made here would
		// stand for held time that may never land in it.
		og := r.rowGroups[carry.Group]
		shift := time.Duration(og.offset) * time.Minute
		day := calNext(calStart(slot.UTC().Add(shift), r.p.bucket), r.p.bucket)
		if e := day.Add(-shift); e.Before(end) {
			end = e
		}
	case r.p.bucket.mode == bucketRecords:
		if r.bounds == nil {
			for _, b := range r.buckets {
				r.bounds = append(r.bounds, b.start, b.end)
			}
			slices.SortFunc(r.bounds, time.Time.Compare)
		}
		i := sort.Search(len(r.bounds), func(i int) bool { return r.bounds[i].After(slot) })
		if i < len(r.bounds) && r.bounds[i].Before(end) {
			end = r.bounds[i]
		}
	default:
		bs := r.bucketsOf(slot, 0)
		if len(bs) == 0 {
			break
		}
		if e := bs[0].end; e.Before(end) {
			end = e
		}
	}
	if !end.After(slot) {
		end = slot.Add(bucketWidth)
	}
	return end
}

// --- values ---

// row assembles one output row from its evidence.
func (r *aggRun) row(b *aggBucket, g *aggGroup, code string, ev *aggEvidence, from string) (substrate.AggregateRow, error) {
	start, end := b.shown(&ev.ends, r.p.from, r.p.to)
	row := substrate.AggregateRow{
		Start: start, End: end, Form: ev.form, Record: b.record, From: from,
		Count: ev.count, Bounded: ev.bounded, Coverage: vocabulary.FormatISODuration(time.Duration(ev.coverage) * time.Millisecond),
		NonAdditive: r.p.bucket.mode == bucketRecords,
	}
	if len(r.p.groupBy) > 0 {
		key := g.key
		if r.p.grouped[substrate.GroupCode] {
			key.Code = code
		}
		row.Group = &key
	}
	for s := range ev.sources {
		row.Sources = append(row.Sources, s)
		r.meta.sources[s] = true
	}
	sort.Strings(row.Sources)
	var err error
	if row.Values, err = r.values(ev, g, code); err != nil {
		return row, err
	}
	if tr := traceOf(r.ctx); tr != nil {
		tr.rows++
	}
	return row, nil
}

// values answers every reducer over one row's evidence, in request order. A
// reducer over a row is as long as the row's points or spans, so the copies
// it reads (taken once per row) and the reducer itself check the context as
// they go (observed's Context forms).
func (r *aggRun) values(ev *aggEvidence, g *aggGroup, code string) ([]substrate.AggregateValue, error) {
	var out []substrate.AggregateValue
	all := observed.Window{From: time.Unix(-1<<40, 0), To: time.Unix(1<<40, 0)}
	points := map[string][]observed.Point{}
	pts := func(stat string) ([]observed.Point, error) {
		if list, ok := points[stat]; ok {
			return list, nil
		}
		var list []observed.Point
		for _, m := range ev.members {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			if stat == "" || m.stat == stat {
				list = append(list, m.Point)
			}
		}
		points[stat] = list
		return list, nil
	}
	var spans []observed.Span
	held := func() ([]observed.Span, error) {
		if spans != nil {
			return spans, nil
		}
		var err error
		spans, err = r.spansOf(ev)
		return spans, err
	}
	summary := ev.form == vocabulary.FormSummary
	statOf := func(name string) string {
		if summary {
			return name
		}
		return ""
	}
	float := func(red substrate.AggregateReducer, v float64, ok bool, unit string) {
		r.meta.inexact = true
		out = append(out, r.value(red, v, ok, unit))
	}
	// reduce answers a float reducer over the raw points of a statistic.
	reduce := func(red substrate.AggregateReducer, stat, unit string, f func(context.Context, []observed.Point, observed.Window) (float64, bool, error)) error {
		list, err := pts(stat)
		if err != nil {
			return err
		}
		v, ok, err := f(r.ctx, list, all)
		if err != nil {
			return err
		}
		float(red, v, ok, unit)
		return nil
	}
	tr := traceOf(r.ctx)
	for _, red := range r.p.reducers {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		if tr != nil && tr.beforeReducer != nil {
			tr.beforeReducer()
		}
		var err error
		switch red.Name {
		case substrate.ReducerCount:
			v := strconv.FormatInt(ev.count, 10)
			out = append(out, substrate.AggregateValue{Reducer: red, Value: &v})
		case substrate.ReducerSum:
			if ev.raw {
				err = reduce(red, statOf("sum"), r.u.value, observed.SumContext)
			} else {
				float(red, ev.stats.Sum, ev.stats.Values > 0, r.u.value)
			}
		case substrate.ReducerAvg:
			if ev.raw {
				err = reduce(red, statOf("avg"), r.u.value, observed.AvgContext)
			} else {
				float(red, ev.stats.Mean, ev.stats.Values > 0, r.u.value)
			}
		case substrate.ReducerMin, substrate.ReducerMax:
			// Compared exactly, answered in the winner's digits (converted
			// exactly where the unit's factor allows): a min or max is not a
			// float reduction.
			var digits string
			var ok bool
			switch {
			case ev.raw:
				stat, best := statOf("min"), observed.MinPointContext
				if red.Name == substrate.ReducerMax {
					stat, best = statOf("max"), observed.MaxPointContext
				}
				var list []observed.Point
				var p observed.Point
				if list, err = r.extremaPoints(ev, g, stat); err == nil {
					p, ok, err = best(r.ctx, list, all)
				}
				digits = p.Exact
			case red.Name == substrate.ReducerMin:
				digits, ok = ev.stats.MinExact, ev.stats.Values > 0
			default:
				digits, ok = ev.stats.MaxExact, ev.stats.Values > 0
			}
			value := substrate.AggregateValue{Reducer: red, Unit: r.u.value}
			if ok {
				value.Value = &digits
			}
			out = append(out, value)
		case substrate.ReducerFirst, substrate.ReducerLast:
			var p observed.Point
			var ok bool
			switch {
			case ev.raw:
				var list []observed.Point
				if list, err = pts(""); err == nil {
					if red.Name == substrate.ReducerFirst {
						p, ok, err = observed.FirstContext(r.ctx, list, all)
					} else {
						p, ok, err = observed.LastContext(r.ctx, list, all)
					}
				}
			case red.Name == substrate.ReducerFirst && ev.stats.First != nil:
				p, ok = *ev.stats.First, true
			case red.Name == substrate.ReducerLast && ev.stats.Last != nil:
				p, ok = *ev.stats.Last, true
			}
			if err == nil {
				var v substrate.AggregateValue
				v, err = r.pointValue(red, p, ok)
				out = append(out, v)
			}
		case substrate.ReducerStdDev:
			if ev.raw {
				err = reduce(red, "", r.u.value, observed.StdDevContext)
			} else {
				float(red, math.Sqrt(ev.stats.M2/float64(max(ev.stats.Values, 1))), ev.stats.Values > 0, r.u.value)
			}
		case substrate.ReducerPercentile:
			q := *red.Percentile
			err = reduce(red, "", r.u.value, func(ctx context.Context, list []observed.Point, w observed.Window) (float64, bool, error) {
				return observed.PercentileContext(ctx, list, w, q)
			})
		case substrate.ReducerCircularMean:
			if ev.raw {
				err = reduce(red, "", "deg", observed.CircularMeanContext)
			} else {
				v, ok := observed.CircularMeanOf(ev.stats)
				float(red, v, ok, "deg")
			}
		case substrate.ReducerTWA:
			var v float64
			var ok bool
			if ev.raw {
				var list []observed.Span
				if list, err = held(); err == nil {
					v, ok, err = observed.TWAOfContext(r.ctx, list)
				}
			} else {
				var h observed.HeldSum
				for _, x := range ev.held {
					h.Ms += x.Ms
					h.ValueMs += x.ValueMs
				}
				v, ok = h.ValueMs/float64(max(h.Ms, 1)), h.Ms > 0
			}
			float(red, v, ok, r.u.value)
		case substrate.ReducerDurationIn:
			var list []observed.Span
			var d time.Duration
			if list, err = held(); err == nil {
				d, err = observed.DurationInOfContext(r.ctx, list, red.Between[0], red.Between[1])
			}
			out = append(out, r.duration(red, d.Milliseconds(), ""))
		case substrate.ReducerTimeInState:
			per := map[string]int64{}
			if ev.raw {
				var list []observed.Span
				var states map[string]time.Duration
				if list, err = held(); err == nil {
					states, err = observed.TimeInStateOfContext(r.ctx, list)
				}
				for c, d := range states {
					per[c] = d.Milliseconds()
				}
			} else {
				for c, h := range ev.held {
					if c != "" {
						per[c] += h.Ms
					}
				}
			}
			states := red.States
			if len(states) == 0 {
				states = sortedKeys(per)
			}
			for _, st := range states {
				if r.p.grouped[substrate.GroupCode] && st != code {
					continue
				}
				one := substrate.AggregateReducer{Name: red.Name, States: []string{st}}
				out = append(out, r.duration(one, per[st], st))
			}
		case substrate.ReducerIncrease:
			if !ev.steps.any {
				out = append(out, substrate.AggregateValue{Reducer: red, Unit: r.u.value})
				continue
			}
			digits, exact := vocabulary.RatDecimal(ev.steps.inc, vocabulary.ConvertedDigits)
			if !exact {
				r.meta.inexact = true
			}
			out = append(out, substrate.AggregateValue{Reducer: red, Value: &digits, Unit: r.u.value})
		case substrate.ReducerRate:
			if !ev.steps.any || ev.steps.covered <= 0 {
				out = append(out, substrate.AggregateValue{Reducer: red, Unit: r.u.value + "/s"})
				continue
			}
			f, _ := ev.steps.inc.Float64()
			float(red, f/(float64(ev.steps.covered)/1000), true, r.u.value+"/s")
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// extremaPoints are the members a raw min or max compares, in the read's
// unit. A counter's members keep their stored digits, because its steps are
// taken on them (convertPoint), so they are converted here, each from its
// own unit, as a rollup row's extremes are (convertStats).
func (r *aggRun) extremaPoints(ev *aggEvidence, g *aggGroup, stat string) ([]observed.Point, error) {
	var list []observed.Point
	for _, m := range ev.members {
		if err := r.canceled(); err != nil {
			return nil, err
		}
		if stat != "" && m.stat != stat {
			continue
		}
		p := m.Point
		if ev.form == vocabulary.FormCounter && p.HasValue && p.Unit != "" {
			c, err := r.conversion(p.Unit, m.delta, g.sourceOf(m.stream, ev.form))
			if err != nil {
				return nil, err
			}
			if !c.identity {
				var exact bool
				p.Value = p.Value*c.ff + c.fo
				if p.Exact, exact = c.digits(p.Exact); !exact {
					r.meta.inexact = true
				}
				p.Unit = r.u.value
			}
		}
		list = append(list, p)
	}
	return list, nil
}

// spansOf is a row's held spans, copied as the reducers over spans take them.
func (r *aggRun) spansOf(ev *aggEvidence) ([]observed.Span, error) {
	out := make([]observed.Span, len(ev.spans))
	for i, s := range ev.spans {
		if err := r.canceled(); err != nil {
			return nil, err
		}
		out[i] = s.Span
	}
	return out, nil
}

// value writes a float answer, or null where the reducer had none.
func (r *aggRun) value(red substrate.AggregateReducer, v float64, ok bool, unit string) substrate.AggregateValue {
	out := substrate.AggregateValue{Reducer: red, Unit: unit}
	if ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
		s := formatFloat(v)
		out.Value = &s
	}
	return out
}

// pointValue writes a first or last: its code, or its digits in the read's
// unit. A counter's raw point arrives in its stored unit (convertPoint).
func (r *aggRun) pointValue(red substrate.AggregateReducer, p observed.Point, ok bool) (substrate.AggregateValue, error) {
	out := substrate.AggregateValue{Reducer: red}
	if !ok {
		out.Unit = r.u.value
		return out, nil
	}
	if !p.HasValue {
		out.Code = p.Code
		return out, nil
	}
	digits := p.Exact
	if p.Unit != "" && p.Unit != r.u.value {
		c, err := r.conversion(p.Unit, false, "")
		if err != nil {
			return out, err
		}
		var exact bool
		digits, exact = c.digits(p.Exact)
		if !exact {
			r.meta.inexact = true
		}
	}
	if digits == "" {
		digits = formatFloat(p.Value)
		r.meta.inexact = true
	}
	out.Value, out.Unit = &digits, r.u.value
	if p.Code != "" {
		out.Code = p.Code
	}
	return out, nil
}

// duration writes a time in the read's duration unit.
func (r *aggRun) duration(red substrate.AggregateReducer, ms int64, code string) substrate.AggregateValue {
	v, exact := durationIn(ms, r.u.duration)
	if !exact {
		r.meta.inexact = true
	}
	return substrate.AggregateValue{Reducer: red, Value: &v, Unit: r.u.duration, Code: code}
}

// durationIn writes milliseconds in a duration unit, exactly where it can.
func durationIn(ms int64, unit string) (string, bool) {
	s := big.NewRat(ms, 1000)
	v, err := vocabulary.ConvertIn("duration", s, "s", unit)
	if err != nil {
		v = s
	}
	return vocabulary.RatDecimal(v, vocabulary.ConvertedDigits)
}

// formatFloat writes a float64 answer to 15 significant digits, the digits it
// holds, so float noise (0.30000000000000004) is not reported as data.
func formatFloat(v float64) string {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 15, 64), 64)
	if err != nil {
		r = v
	}
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}
