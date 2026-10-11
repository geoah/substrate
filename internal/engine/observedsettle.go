package engine

// The observed index's per-transaction recompute (observedindex.go): the
// rollup rows of every bucket the transaction's point writes touched, and the
// latest row of every stream they touched, each computed once from the
// points as they stand at the end of the transaction.

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// observedPageSize bounds one read of a stream's points while a rebuild
// recomputes every rollup row.
const observedPageSize = 20000

// observedInsertBatch bounds one insert of rollup rows.
const observedInsertBatch = 2000

// observedLockKey serializes the recompute of rollup and latest rows per
// repository. The background re-derivation writes points beside a live
// write, and two transactions recomputing one bucket at once would each read
// the points before the other's commit, and the second insert of the bucket's
// row would collide with the first's. Taken last, after every record lock,
// and held to the commit: the global order is changelog < registry-dep <
// subject-type < record < observed, and nothing that holds it waits on a
// record.
const observedLockKey = "observed"

// settleObserved recomputes what the transaction's point writes touched. A
// replay recomputes every row (rederiveObservedAll).
func (t *txn) settleObserved() error {
	touch := t.observed
	if touch == nil {
		return nil
	}
	t.observed = nil
	if err := t.lockKey(observedLockKey); err != nil {
		return err
	}
	if touch.all {
		return t.rederiveObservedAll()
	}
	if err := t.recomputeRollups(touch.buckets); err != nil {
		return fmt.Errorf("substrate/engine: recompute the observed rollups: %w", err)
	}
	if err := t.recomputeLatest(touch.streams); err != nil {
		return fmt.Errorf("substrate/engine: recompute the observed latest rows: %w", err)
	}
	return nil
}

// obsGroup is one rollup row's group inside a bucket.
type obsGroup struct {
	code, unit string
	delta      bool
	offset     int64
}

func (g obsGroup) key() string {
	return g.code + "\x1f" + g.unit + "\x1f" + strconv.FormatBool(g.delta) + "\x1f" + strconv.FormatInt(g.offset, 10)
}

// rollupRow is one computed row of observed_rollups.
type rollupRow struct {
	group obsGroup
	start time.Time
	stats *observed.Stats
}

// pointColumns are the columns a recompute reads a point by, in scanPoint's
// order.
const pointColumns = `p.ts, p.ts_end, p.value, p.value_exact::text, p.value_base, p.code, p.unit, p.delta,
	p.utc_offset_min, p.bounded, p.hold_ms, p.reset_rule, p.reset_at, p.wrap_at::text, p.circular`

// availablePoint is vocabulary.ObservedPointAvailable in SQL, over a point
// aliased p: a value, or a code that is not empty. Every statement that
// counts or picks available points reads it, so the count an insertion adds
// and a deletion takes back, the rollups and a rebuild cannot disagree.
const availablePoint = `(p.value IS NOT NULL OR p.code <> '')`

// holdStreamSQL matches one obsHold's points, its six columns bound at
// $1 to $6.
const holdStreamSQL = `p.metric = $1 AND p.kind = $2 AND p.source = $3 AND p.about = $4 AND p.channel = $5 AND p.form = $6`

// scannedPoint is one point read back, with the group it rolls up into.
type scannedPoint struct {
	point observed.Point
	group obsGroup
}

// scanPoint reads pointColumns, after any leading columns in lead.
func scanPoint(rows *sql.Rows, lead ...any) (scannedPoint, error) {
	var (
		sp                       scannedPoint
		value, base              sql.NullFloat64
		exact, resetRule, wrapAt sql.NullString
		hold                     sql.NullInt64
		resetAt                  sql.NullTime
		circular                 bool
		ts, tsEnd                time.Time
		code, unit               string
		delta, bounded           bool
		offset                   int64
	)
	dests := append(lead, &ts, &tsEnd, &value, &exact, &base, &code, &unit, &delta,
		&offset, &bounded, &hold, &resetRule, &resetAt, &wrapAt, &circular)
	if err := rows.Scan(dests...); err != nil {
		return sp, err
	}
	p := observed.Point{
		At: ts, End: tsEnd, Code: code, Bounded: bounded, Unit: unit,
		Value: value.Float64, HasValue: value.Valid, Exact: exact.String,
		Unavailable: !vocabulary.ObservedPointAvailable(value.Valid, code),
		Reset:       resetRule.String, WrapAt: wrapAt.String,
	}
	if hold.Valid {
		d := time.Duration(hold.Int64) * time.Millisecond
		p.Hold = &d
	}
	if resetAt.Valid {
		p.ResetAt = resetAt.Time
	}
	// A circular point's angle in radians: rad is its own, any other angle
	// unit reaches degrees, the measure's base.
	if circular && value.Valid {
		switch {
		case unit == "rad":
			p.Circular, p.Angle = true, value.Float64
		case base.Valid:
			p.Circular, p.Angle = true, base.Float64*math.Pi/180
		}
	}
	sp.group = obsGroup{code: code, unit: unit, delta: delta, offset: offset}
	p.Group = sp.group.key()
	sp.point = p
	return sp, nil
}

// bucketRows computes one bucket's rows from its points and the stream's
// last available point before it.
func bucketRows(form string, start time.Time, prev *observed.Point, points []scannedPoint) []rollupRow {
	groups := map[string]obsGroup{}
	list := make([]observed.Point, len(points))
	for i, sp := range points {
		list[i] = sp.point
		groups[sp.point.Group] = sp.group
	}
	stats := observed.Bucket(form, start, start.Add(observed.BucketWidth), prev, list)
	out := make([]rollupRow, 0, len(stats))
	for k, s := range stats {
		out = append(out, rollupRow{group: groups[k], start: start, stats: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].group.key() < out[j].group.key() })
	return out
}

// lastAvailable is the last available point of a bucket's points, or prev
// where it has none.
func lastAvailable(prev *observed.Point, points []scannedPoint) *observed.Point {
	for i := len(points) - 1; i >= 0; i-- {
		if !points[i].point.Unavailable {
			p := points[i].point
			return &p
		}
	}
	return prev
}

func (h obsHold) args() []any {
	return []any{h.metric, h.kind, h.source, h.about, h.channel, h.form}
}

func holdLess(a, b obsHold) bool {
	if a.obsStream != b.obsStream {
		return streamLess(a.obsStream, b.obsStream)
	}
	return a.form < b.form
}

func streamLess(a, b obsStream) bool {
	for _, c := range [][2]string{{a.metric, b.metric}, {a.kind, b.kind}, {a.source, b.source}, {a.about, b.about}, {a.channel, b.channel}} {
		if c[0] != c[1] {
			return c[0] < c[1]
		}
	}
	return false
}

// recomputeRollups recomputes the rows of every touched bucket, stream by
// stream: a counter's next bucket holding an available point joins them,
// because its first step comes from the touched bucket's last point.
func (t *txn) recomputeRollups(touched map[obsBucket]bool) error {
	byHold := map[obsHold][]int64{}
	for b := range touched {
		byHold[b.obsHold] = append(byHold[b.obsHold], b.start)
	}
	holds := make([]obsHold, 0, len(byHold))
	for h := range byHold {
		holds = append(holds, h)
	}
	sort.Slice(holds, func(i, j int) bool { return holdLess(holds[i], holds[j]) })
	for _, h := range holds {
		starts := byHold[h]
		if h.form == vocabulary.FormCounter {
			next, err := t.nextCounterBuckets(h, starts)
			if err != nil {
				return err
			}
			starts = append(starts, next...)
		}
		buckets := uniqueStarts(starts)
		rows, err := t.rollupRowsOf(h, buckets)
		if err != nil {
			return err
		}
		if _, err := t.exec(`DELETE FROM observed_rollups p WHERE `+holdStreamSQL+` AND p.bucket15 = ANY($7::timestamptz[])`,
			append(h.args(), buckets)...); err != nil {
			return err
		}
		if err := t.insertRollups(h, rows); err != nil {
			return err
		}
	}
	return nil
}

func uniqueStarts(ms []int64) []time.Time {
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	out := make([]time.Time, 0, len(ms))
	for i, m := range ms {
		if i > 0 && m == ms[i-1] {
			continue
		}
		out = append(out, time.UnixMilli(m).UTC())
	}
	return out
}

// nextCounterBuckets names, for each touched bucket of a counter stream, the
// bucket of the stream's next available point after it.
func (t *txn) nextCounterBuckets(h obsHold, starts []int64) ([]int64, error) {
	after := make([]time.Time, len(starts))
	for i, s := range starts {
		after[i] = time.UnixMilli(s).UTC().Add(observed.BucketWidth)
	}
	rows, err := t.query(`
		SELECT DISTINCT n.ts FROM unnest($7::timestamptz[]) AS b(after)
		CROSS JOIN LATERAL (
			SELECT p.ts FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts >= b.after AND `+availablePoint+`
			ORDER BY p.ts LIMIT 1) n`, append(h.args(), after)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var ts time.Time
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		out = append(out, bucketOf(ts))
	}
	return out, rows.Err()
}

// rollupRowsOf computes the rows of the named buckets of one stream.
func (t *txn) rollupRowsOf(h obsHold, starts []time.Time) ([]rollupRow, error) {
	byStart := map[int64][]scannedPoint{}
	// One range of the stream index per bucket, whatever the planner guesses
	// about the stream's size: as a plain join it put the stream outside the
	// loop and read every point of it on every write. OFFSET 0 keeps the
	// subquery from being pulled up into that join.
	rows, err := t.query(`
		SELECT b.start, `+pointColumns+`
		FROM unnest($7::timestamptz[]) AS b(start)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts >= b.start AND p.ts < b.start + interval '15 minutes'
			OFFSET 0) p
		ORDER BY b.start, p.ts, p.record_id, p.ord`, append(h.args(), starts)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var start time.Time
		sp, err := scanPoint(rows, &start)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		byStart[start.UnixMilli()] = append(byStart[start.UnixMilli()], sp)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	prevs := map[int64]*observed.Point{}
	if h.form == vocabulary.FormCounter {
		rows, err := t.query(`
			SELECT b.start, `+pointColumns+`
			FROM unnest($7::timestamptz[]) AS b(start)
			CROSS JOIN LATERAL (
				SELECT * FROM observed_points p
				WHERE `+holdStreamSQL+` AND p.ts < b.start AND `+availablePoint+`
				ORDER BY p.ts DESC, p.record_id DESC, p.ord DESC LIMIT 1) p`, append(h.args(), starts)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var start time.Time
			sp, err := scanPoint(rows, &start)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			p := sp.point
			prevs[start.UnixMilli()] = &p
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	var out []rollupRow
	for _, start := range starts {
		points := byStart[start.UnixMilli()]
		if len(points) == 0 {
			continue
		}
		out = append(out, bucketRows(h.form, start, prevs[start.UnixMilli()], points)...)
	}
	return out, nil
}

// insertRollups writes one stream's computed rows, in batches.
func (t *txn) insertRollups(h obsHold, rows []rollupRow) error {
	for len(rows) > 0 {
		n := min(len(rows), observedInsertBatch)
		if err := t.insertRollupBatch(h, rows[:n]); err != nil {
			return err
		}
		rows = rows[n:]
	}
	return nil
}

func (t *txn) insertRollupBatch(h obsHold, rows []rollupRow) error {
	n := len(rows)
	var (
		code, unit                      = make([]string, n), make([]string, n)
		delta                           = make([]bool, n)
		offset, count, bounded, covered = make([]int64, n), make([]int64, n), make([]int64, n), make([]int64, n)
		maxGap, startGap                = make([]int64, n), make([]int64, n)
		bucket                          = make([]time.Time, n)
		sum, minV, maxV, mean, m2       = make([]*float64, n), make([]*float64, n), make([]*float64, n), make([]*float64, n), make([]*float64, n)
		twa, sin, cos                   = make([]*float64, n), make([]*float64, n), make([]*float64, n)
		firstTS, lastTS                 = make([]*time.Time, n), make([]*time.Time, n)
		firstValue, lastValue, increase = make([]*string, n), make([]*string, n), make([]*string, n)
		held, lastHold, resets          = make([]*int64, n), make([]*int64, n), make([]*int64, n)
	)
	for i, r := range rows {
		s := r.stats
		code[i], unit[i], delta[i], offset[i] = r.group.code, r.group.unit, r.group.delta, r.group.offset
		bucket[i], count[i], bounded[i], covered[i] = r.start, s.N, s.Bounded, s.CoveredMs
		maxGap[i], startGap[i] = s.MaxGapMs, s.StartGapMs
		if s.Values > 0 {
			sum[i], minV[i], maxV[i], mean[i], m2[i] = ptrTo(s.Sum), ptrTo(s.Min), ptrTo(s.Max), ptrTo(s.Mean), ptrTo(s.M2)
		}
		if s.First != nil {
			firstTS[i], firstValue[i] = ptrTo(s.First.At), digitsOf(s.First)
			lastTS[i], lastValue[i] = ptrTo(s.Last.At), digitsOf(s.Last)
		}
		if h.form == vocabulary.FormGauge {
			held[i], lastHold[i] = ptrTo(s.HeldMs), s.LastHoldMs
			if s.Values > 0 {
				twa[i] = ptrTo(s.TWASum)
			}
		}
		if s.Increase != nil {
			d, _ := vocabulary.RatDecimal(s.Increase, vocabulary.ConvertedDigits)
			increase[i], resets[i] = &d, ptrTo(s.Resets)
		}
		if s.Angles > 0 {
			sin[i], cos[i] = ptrTo(s.SinSum), ptrTo(s.CosSum)
		}
	}
	_, err := t.exec(`
		INSERT INTO observed_rollups (metric, kind, source, about, channel, form,
			code, unit, delta, utc_offset_min, bucket15, n, bounded, sum, min, max, mean, m2,
			first_ts, first_value, last_ts, last_value, covered_ms, held_ms, twa_sum, last_hold_ms,
			max_gap_ms, start_gap_ms, increase, resets, sin_sum, cos_sum)
		SELECT $1, $2, $3, $4, $5, $6,
			u.code, u.unit, u.delta, u.off, u.bucket, u.n, u.bounded, u.sum, u.min, u.max, u.mean, u.m2,
			u.first_ts, u.first_value::numeric, u.last_ts, u.last_value::numeric, u.covered, u.held, u.twa, u.last_hold,
			u.max_gap, u.start_gap, u.increase::numeric, u.resets, u.sin, u.cos
		FROM unnest($7::text[], $8::text[], $9::boolean[], $10::int[], $11::timestamptz[], $12::bigint[], $13::bigint[],
		            $14::float8[], $15::float8[], $16::float8[], $17::float8[], $18::float8[],
		            $19::timestamptz[], $20::text[], $21::timestamptz[], $22::text[], $23::bigint[], $24::bigint[],
		            $25::float8[], $26::bigint[], $27::bigint[], $28::bigint[], $29::text[], $30::int[],
		            $31::float8[], $32::float8[])
		     AS u(code, unit, delta, off, bucket, n, bounded, sum, min, max, mean, m2,
		          first_ts, first_value, last_ts, last_value, covered, held, twa, last_hold,
		          max_gap, start_gap, increase, resets, sin, cos)`,
		h.metric, h.kind, h.source, h.about, h.channel, h.form,
		code, unit, delta, offset, bucket, count, bounded, sum, minV, maxV, mean, m2,
		firstTS, firstValue, lastTS, lastValue, covered, held, twa, lastHold,
		maxGap, startGap, increase, resets, sin, cos)
	return err
}

// digitsOf is a point's stored digits, nil for a code.
func digitsOf(p *observed.Point) *string {
	if !p.HasValue || p.Exact == "" {
		return nil
	}
	return ptrTo(p.Exact)
}

// latestColumns are the columns observed_latest takes from a point, in
// insertLatest's order.
const latestColumns = `p.ts, p.ts_end, p.value_exact::text, p.code, p.unit, p.delta, p.form, p.statistic,
	p.bounded, p.hold_ms, p.utc_offset_min, p.record_id, p.revision`

// latestOrder picks a stream's newest point: the latest instant, then the
// higher revision, then the greater record id, then the later point.
const latestOrder = `p.ts DESC, p.revision DESC NULLS LAST, p.record_id DESC, p.ord DESC`

// recomputeLatest rewrites the latest row of every touched stream from its
// newest available point, and its point count by the transaction's change in
// it; a stream left with no available point loses its row.
func (t *txn) recomputeLatest(touched map[obsStream]int64) error {
	if len(touched) == 0 {
		return nil
	}
	streams := make([]obsStream, 0, len(touched))
	for s := range touched {
		streams = append(streams, s)
	}
	sort.Slice(streams, func(i, j int) bool { return streamLess(streams[i], streams[j]) })
	metric, kind, source, about, channel := streamColumns(streams)
	rows, err := t.query(`
		SELECT k.i, `+latestColumns+`
		FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
		     WITH ORDINALITY AS k(metric, kind, source, about, channel, i)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE p.metric = k.metric AND p.kind = k.kind AND p.source = k.source
			  AND p.about = k.about AND p.channel = k.channel AND `+availablePoint+`
			ORDER BY `+latestOrder+` LIMIT 1) p`, metric, kind, source, about, channel)
	if err != nil {
		return err
	}
	found := map[int]latestRow{}
	for rows.Next() {
		var i int
		var l latestRow
		if err := rows.Scan(&i, &l.ts, &l.tsEnd, &l.value, &l.code, &l.unit, &l.delta, &l.form, &l.statistic,
			&l.bounded, &l.hold, &l.offset, &l.recordID, &l.revision); err != nil {
			_ = rows.Close()
			return err
		}
		found[i-1] = l
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var keep []latestRow
	var gone []obsStream
	for i, s := range streams {
		l, ok := found[i]
		if !ok {
			gone = append(gone, s)
			continue
		}
		l.obsStream, l.points = s, touched[s]
		keep = append(keep, l)
	}
	if len(gone) > 0 {
		m, k, so, a, c := streamColumns(gone)
		if _, err := t.exec(`
			DELETE FROM observed_latest l USING unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
			     AS k(metric, kind, source, about, channel)
			WHERE l.metric = k.metric AND l.kind = k.kind AND l.source = k.source AND l.about = k.about AND l.channel = k.channel`,
			m, k, so, a, c); err != nil {
			return err
		}
	}
	return t.upsertLatest(keep)
}

// latestRow is one row of observed_latest; points is the change in the
// stream's count of available points.
type latestRow struct {
	obsStream
	ts, tsEnd        time.Time
	value, statistic sql.NullString
	code, unit, form string
	delta, bounded   bool
	hold, revision   sql.NullInt64
	offset           int64
	recordID         string
	points           int64
}

func streamColumns(streams []obsStream) (metric, kind, source, about, channel []string) {
	for _, s := range streams {
		metric, kind = append(metric, s.metric), append(kind, s.kind)
		source, about, channel = append(source, s.source), append(about, s.about), append(channel, s.channel)
	}
	return metric, kind, source, about, channel
}

func (t *txn) upsertLatest(rows []latestRow) error {
	if len(rows) == 0 {
		return nil
	}
	streams := make([]obsStream, len(rows))
	n := len(rows)
	ts, tsEnd := make([]time.Time, n), make([]time.Time, n)
	value, statistic := make([]*string, n), make([]*string, n)
	code, unit, form, recordID := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	delta, bounded := make([]bool, n), make([]bool, n)
	hold, revision := make([]*int64, n), make([]*int64, n)
	offset, points := make([]int64, n), make([]int64, n)
	for i, l := range rows {
		streams[i] = l.obsStream
		ts[i], tsEnd[i], code[i], unit[i], form[i], recordID[i] = l.ts, l.tsEnd, l.code, l.unit, l.form, l.recordID
		delta[i], bounded[i], offset[i], points[i] = l.delta, l.bounded, l.offset, l.points
		if l.value.Valid {
			value[i] = ptrTo(l.value.String)
		}
		if l.statistic.Valid {
			statistic[i] = ptrTo(l.statistic.String)
		}
		if l.hold.Valid {
			hold[i] = ptrTo(l.hold.Int64)
		}
		if l.revision.Valid {
			revision[i] = ptrTo(l.revision.Int64)
		}
	}
	metric, kind, source, about, channel := streamColumns(streams)
	_, err := t.exec(`
		INSERT INTO observed_latest (metric, kind, source, about, channel, ts, ts_end, value, code, unit, delta,
			form, statistic, bounded, hold_ms, utc_offset_min, record_id, revision, points)
		SELECT u.metric, u.kind, u.source, u.about, u.channel, u.ts, u.ts_end, u.value::numeric, u.code, u.unit, u.delta,
			u.form, u.statistic, u.bounded, u.hold, u.off, u.record_id, u.revision, u.points
		FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::timestamptz[], $7::timestamptz[],
		            $8::text[], $9::text[], $10::text[], $11::boolean[], $12::text[], $13::text[], $14::boolean[],
		            $15::bigint[], $16::int[], $17::text[], $18::bigint[], $19::bigint[])
		     AS u(metric, kind, source, about, channel, ts, ts_end, value, code, unit, delta,
		          form, statistic, bounded, hold, off, record_id, revision, points)
		ON CONFLICT (repository, metric, kind, source, about, channel) DO UPDATE SET
			ts = EXCLUDED.ts, ts_end = EXCLUDED.ts_end, value = EXCLUDED.value, code = EXCLUDED.code,
			unit = EXCLUDED.unit, delta = EXCLUDED.delta, form = EXCLUDED.form, statistic = EXCLUDED.statistic,
			bounded = EXCLUDED.bounded, hold_ms = EXCLUDED.hold_ms, utc_offset_min = EXCLUDED.utc_offset_min,
			record_id = EXCLUDED.record_id, revision = EXCLUDED.revision,
			points = observed_latest.points + EXCLUDED.points`,
		metric, kind, source, about, channel, ts, tsEnd, value, code, unit, delta,
		form, statistic, bounded, hold, offset, recordID, revision, points)
	return err
}

// rederiveObservedAll recomputes every rollup and latest row from the points,
// after a replay folded them: the latest rows in one statement, the rollup
// rows stream by stream, a page of points at a time.
func (t *txn) rederiveObservedAll() error {
	for _, table := range []string{"observed_rollups", "observed_latest"} {
		if _, err := t.exec(`DELETE FROM ` + table); err != nil {
			return err
		}
	}
	if _, err := t.exec(`
		INSERT INTO observed_latest (metric, kind, source, about, channel, ts, ts_end, value, code, unit, delta,
			form, statistic, bounded, hold_ms, utc_offset_min, record_id, revision, points)
		SELECT DISTINCT ON (p.metric, p.kind, p.source, p.about, p.channel)
			p.metric, p.kind, p.source, p.about, p.channel, p.ts, p.ts_end, p.value_exact, p.code, p.unit, p.delta,
			p.form, p.statistic, p.bounded, p.hold_ms, p.utc_offset_min, p.record_id, p.revision,
			count(*) OVER (PARTITION BY p.metric, p.kind, p.source, p.about, p.channel)
		FROM observed_points p WHERE ` + availablePoint + `
		ORDER BY p.metric, p.kind, p.source, p.about, p.channel, ` + latestOrder); err != nil {
		return fmt.Errorf("substrate/engine: rebuild the observed latest rows: %w", err)
	}
	rows, err := t.query(`SELECT DISTINCT metric, kind, source, about, channel, form FROM observed_points WHERE form <> $1`,
		vocabulary.FormSummary)
	if err != nil {
		return err
	}
	var holds []obsHold
	for rows.Next() {
		var h obsHold
		if err := rows.Scan(&h.metric, &h.kind, &h.source, &h.about, &h.channel, &h.form); err != nil {
			_ = rows.Close()
			return err
		}
		holds = append(holds, h)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	sort.Slice(holds, func(i, j int) bool { return holdLess(holds[i], holds[j]) })
	for _, h := range holds {
		if err := t.rederiveHold(h); err != nil {
			return fmt.Errorf("substrate/engine: rebuild the observed rollups: %w", err)
		}
	}
	return nil
}

// rederiveHold recomputes every rollup row of one stream from its points, a
// page at a time. A page that ends inside a bucket holds that bucket back
// and the next page starts at it, so no bucket is computed from part of its
// points.
func (t *txn) rederiveHold(h obsHold) error {
	var prev *observed.Point
	cursor := time.Time{}
	limit := observedPageSize
	var pending []rollupRow
	for {
		rows, err := t.query(`SELECT `+pointColumns+` FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts >= $7
			ORDER BY p.ts, p.record_id, p.ord LIMIT $8`, append(h.args(), cursor, limit)...)
		if err != nil {
			return err
		}
		var page []scannedPoint
		for rows.Next() {
			sp, err := scanPoint(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			page = append(page, sp)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		full := len(page) == limit
		cut := len(page)
		if full {
			last := bucketOf(page[len(page)-1].point.At)
			for cut > 0 && bucketOf(page[cut-1].point.At) == last {
				cut--
			}
			if cut == 0 {
				// One bucket holds more points than a page: read it whole.
				limit *= 2
				continue
			}
			cursor = time.UnixMilli(last).UTC()
		}
		for i := 0; i < cut; {
			b := bucketOf(page[i].point.At)
			j := i
			for j < cut && bucketOf(page[j].point.At) == b {
				j++
			}
			pending = append(pending, bucketRows(h.form, time.UnixMilli(b).UTC(), prev, page[i:j])...)
			prev = lastAvailable(prev, page[i:j])
			i = j
		}
		if len(pending) >= observedInsertBatch || !full {
			if err := t.insertRollups(h, pending); err != nil {
				return err
			}
			pending = nil
		}
		if !full {
			return nil
		}
	}
}
