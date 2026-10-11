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

// observedPageSize bounds one read of observed points, by a write's recompute
// of its buckets and by a rebuild's of every row alike: what either holds at
// once is a page and each group's running state, however many points one
// bucket has.
const observedPageSize = 20000

// observedPage is observedPageSize, or the test seam's smaller page.
func (t *txn) observedPage() int {
	if n := t.ds.svc.testObservedPage; n > 0 {
		return n
	}
	return observedPageSize
}

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

// streamedBucket computes one bucket's rows from its points as they are read,
// a page at a time, in (ts, record_id, ord) order: the running state is
// observed.BucketBuilder's, and the group columns each row key stands for.
type streamedBucket struct {
	start   time.Time
	builder *observed.BucketBuilder
	groups  map[string]obsGroup
}

// newStreamedBucket starts the bucket at start of a stream of form; prev is
// the stream's last available point before it.
func newStreamedBucket(form string, start time.Time, prev *observed.Point) *streamedBucket {
	return &streamedBucket{
		start:   start,
		builder: observed.NewBucketBuilder(form, start, start.Add(observed.BucketWidth), prev),
		groups:  map[string]obsGroup{},
	}
}

func (b *streamedBucket) add(sp scannedPoint) {
	b.groups[sp.point.Group] = sp.group
	b.builder.Add(sp.point)
}

// rows finishes the bucket: its rows in group order.
func (b *streamedBucket) rows() []rollupRow {
	stats := b.builder.Rows()
	out := make([]rollupRow, 0, len(stats))
	for k, s := range stats {
		out = append(out, rollupRow{group: b.groups[k], start: b.start, stats: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].group.key() < out[j].group.key() })
	return out
}

// pointKey is a point's place in the order a recompute reads points in, the
// keyset the next page starts after.
type pointKey struct {
	ts       time.Time
	recordID string
	ord      int64
}

// pointOrder is that order, and pointAfter the keyset predicate past a key
// bound at $7 to $9.
const (
	pointOrder = `p.ts, p.record_id, p.ord`
	pointAfter = `(p.ts, p.record_id, p.ord) > ($7, $8, $9)`
)

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

// rollupRowsOf computes the rows of the named buckets of one stream. Each
// bucket's points are read in pages of at most observedPage rows and fed to
// a running state (streamedBucket), so what the recompute holds is one page
// and each group's state, whatever the bucket's size, and its work is the
// bucket's points once. The buckets are taken a page's worth at a time.
func (t *txn) rollupRowsOf(h obsHold, starts []time.Time) ([]rollupRow, error) {
	page := t.observedPage()
	var out []rollupRow
	for len(starts) > 0 {
		chunk := starts[:min(len(starts), page)]
		starts = starts[len(chunk):]
		rows, err := t.rollupRowsOfChunk(h, chunk, page)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// rollupRowsOfChunk computes at most a page of buckets. Their first pages
// come in one statement, the page split evenly between them, one range of
// the stream index per bucket whatever the planner guesses about the
// stream's size (as a plain join it put the stream outside the loop and read
// every point of it on every write); a bucket whose share came back full
// reads on alone, a page at a time, after the last point it read.
func (t *txn) rollupRowsOfChunk(h obsHold, starts []time.Time, page int) ([]rollupRow, error) {
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
		if err := finishRows(rows); err != nil {
			return nil, err
		}
	}
	buckets := map[int64]*streamedBucket{}
	read := map[int64]int{}
	last := map[int64]pointKey{}
	share := max(page/len(starts), 1)
	rows, err := t.query(`
		SELECT b.start, p.record_id, p.ord, `+pointColumns+`
		FROM unnest($7::timestamptz[]) AS b(start)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_points p
			WHERE `+holdStreamSQL+` AND p.ts >= b.start AND p.ts < b.start + interval '15 minutes'
			ORDER BY `+pointOrder+` LIMIT $8) p
		ORDER BY b.start, `+pointOrder, append(h.args(), starts, share)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var start time.Time
		var key pointKey
		sp, err := scanPoint(rows, &start, &key.recordID, &key.ord)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		ms := start.UnixMilli()
		b := buckets[ms]
		if b == nil {
			b = newStreamedBucket(h.form, start.UTC(), prevs[ms])
			buckets[ms] = b
		}
		b.add(sp)
		key.ts = sp.point.At
		read[ms]++
		last[ms] = key
	}
	if err := finishRows(rows); err != nil {
		return nil, err
	}
	var out []rollupRow
	for _, start := range starts {
		ms := start.UnixMilli()
		b := buckets[ms]
		if b == nil {
			continue
		}
		after := last[ms]
		end := start.Add(observed.BucketWidth)
		for full := read[ms] == share; full; {
			n, err := t.readPointsAfter(h, &after, &end, page, b.add)
			if err != nil {
				return nil, err
			}
			full = n == page
		}
		out = append(out, b.rows()...)
	}
	return out, nil
}

// readPointsAfter reads one page of a stream's points after the key at
// after, in pointOrder and before `before` where it is set, feeds each to
// add and moves after to the last; it answers how many it read. `p.ts >= $7`
// is what starts the stream index's range at the key: the row comparison
// alone is a filter the index cannot seek by, and each page would read the
// stream from its start.
func (t *txn) readPointsAfter(h obsHold, after *pointKey, before *time.Time, page int, add func(scannedPoint)) (int, error) {
	rows, err := t.query(`
		SELECT p.record_id, p.ord, `+pointColumns+` FROM observed_points p
		WHERE `+holdStreamSQL+` AND p.ts >= $7 AND `+pointAfter+`
		  AND ($10::timestamptz IS NULL OR p.ts < $10)
		ORDER BY `+pointOrder+` LIMIT $11`,
		append(h.args(), after.ts, after.recordID, after.ord, before, page)...)
	if err != nil {
		return 0, err
	}
	n := 0
	for rows.Next() {
		var key pointKey
		sp, err := scanPoint(rows, &key.recordID, &key.ord)
		if err != nil {
			_ = rows.Close()
			return 0, err
		}
		key.ts = sp.point.At
		add(sp)
		*after = key
		n++
	}
	return n, finishRows(rows)
}

// finishRows ends an iteration: the error that stopped Next, or else the one
// Close reports. database/sql closes the rows itself when Next fails, so the
// Close after a failed iteration answers nil, and only Err says the rows ran
// short; a read that took Close's nil would accept what it had as complete.
func finishRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
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
		spanEnd                         = make([]*time.Time, n)
	)
	for i, r := range rows {
		s := r.stats
		if !s.SpanEnd.IsZero() {
			spanEnd[i] = ptrTo(s.SpanEnd)
		}
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
			max_gap_ms, start_gap_ms, increase, resets, sin_sum, cos_sum, span_end)
		SELECT $1, $2, $3, $4, $5, $6,
			u.code, u.unit, u.delta, u.off, u.bucket, u.n, u.bounded, u.sum, u.min, u.max, u.mean, u.m2,
			u.first_ts, u.first_value::numeric, u.last_ts, u.last_value::numeric, u.covered, u.held, u.twa, u.last_hold,
			u.max_gap, u.start_gap, u.increase::numeric, u.resets, u.sin, u.cos, u.span_end
		FROM unnest($7::text[], $8::text[], $9::boolean[], $10::int[], $11::timestamptz[], $12::bigint[], $13::bigint[],
		            $14::float8[], $15::float8[], $16::float8[], $17::float8[], $18::float8[],
		            $19::timestamptz[], $20::text[], $21::timestamptz[], $22::text[], $23::bigint[], $24::bigint[],
		            $25::float8[], $26::bigint[], $27::bigint[], $28::bigint[], $29::text[], $30::int[],
		            $31::float8[], $32::float8[], $33::timestamptz[])
		     AS u(code, unit, delta, off, bucket, n, bounded, sum, min, max, mean, m2,
		          first_ts, first_value, last_ts, last_value, covered, held, twa, last_hold,
		          max_gap, start_gap, increase, resets, sin, cos, span_end)`,
		h.metric, h.kind, h.source, h.about, h.channel, h.form,
		code, unit, delta, offset, bucket, count, bounded, sum, minV, maxV, mean, m2,
		firstTS, firstValue, lastTS, lastValue, covered, held, twa, lastHold,
		maxGap, startGap, increase, resets, sin, cos, spanEnd)
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
	if err := finishRows(rows); err != nil {
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
	if err := finishRows(rows); err != nil {
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

// rederiveHold recomputes every rollup row of one stream from its points,
// read a page at a time in pointOrder and fed to one bucket's running state
// after another, so a bucket of any size is read once and never held whole.
// The last available point of a bucket is the next one's prev, a counter's
// step in.
func (t *txn) rederiveHold(h obsHold) error {
	page := t.observedPage()
	var (
		prev    *observed.Point
		cur     *streamedBucket
		pending []rollupRow
		failed  error
	)
	finish := func() {
		if cur == nil {
			return
		}
		if p := cur.builder.LastAvailable(); p != nil {
			prev = p
		}
		pending = append(pending, cur.rows()...)
		cur = nil
	}
	add := func(sp scannedPoint) {
		start := bucketOf(sp.point.At)
		if cur != nil && cur.start.UnixMilli() != start {
			finish()
		}
		if cur == nil {
			cur = newStreamedBucket(h.form, time.UnixMilli(start).UTC(), prev)
		}
		cur.add(sp)
	}
	flush := func() {
		if failed == nil && len(pending) > 0 {
			failed = t.insertRollups(h, pending)
			pending = nil
		}
	}
	// The first page starts before every point; each later one after the
	// last point read.
	after := pointKey{ord: -1}
	for {
		n, err := t.readPointsAfter(h, &after, nil, page, add)
		if err != nil {
			return err
		}
		if len(pending) >= observedInsertBatch {
			flush()
		}
		if n < page {
			break
		}
	}
	finish()
	flush()
	return failed
}
