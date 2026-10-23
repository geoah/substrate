package engine

// The observed index's per-transaction settle (observedindex.go): the
// segments of every record the transaction wrote, then the rollup rows of
// every bucket their old and new points touched, tier by tier, and the
// latest row of every stream they touched, each computed once from the
// segments as they stand at the end of the transaction.

import (
	"bytes"
	"database/sql"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// observedLockKey serializes the settle per repository. The background
// re-derivation writes segments beside a live write, and two transactions
// recomputing one bucket at once would each read the segments before the
// other's commit, and the second insert of the bucket's row would collide
// with the first's. Every write to the observed tables happens under it, so a
// stream's id is allocated and its extent widened by one transaction at a
// time. Taken last, after every record lock, and held to the commit: the
// global order is changelog < registry-dep < subject-type < record <
// observed, and nothing that holds it waits on a record. A metric write
// writes an index_reprojections row under it (requestMetrics), so a
// background page, which shares the registry lock with that write, settles
// before it writes its own request row (reprojectKind).
const observedLockKey = "observed"

// observedInsertBatch bounds one insert of rollup rows or segments.
const observedInsertBatch = 2000

// settleObserved applies what the transaction's writes left: segments,
// then rollups, then latest rows, then the re-derivation a metric's moved
// tiers owe. A replay settles nothing.
func (t *txn) settleObserved() error {
	touch := t.observed
	if touch == nil {
		return nil
	}
	t.observed = nil
	if touch.all || (len(touch.ops) == 0 && len(touch.metrics) == 0) {
		return nil
	}
	if err := t.lockKey(observedLockKey); err != nil {
		return err
	}
	units, err := t.observedUnits()
	if err != nil {
		return err
	}
	s := &obsSettle{
		t: t, touch: touch, store: newObsStore(t.ctx, t.tx, t.derivationView(), units),
		ranges: map[obsSF][]obsSpan{}, latest: map[int64]int64{}, tiers: map[string][]time.Duration{},
	}
	if err := s.applySegments(); err != nil {
		return fmt.Errorf("substrate/engine: write the observed segments: %w", err)
	}
	if err := s.recomputeRollups(); err != nil {
		return fmt.Errorf("substrate/engine: recompute the observed rollups: %w", err)
	}
	if err := s.recomputeLatest(); err != nil {
		return fmt.Errorf("substrate/engine: recompute the observed latest rows: %w", err)
	}
	return s.requestMetrics()
}

// obsSettle is one settle in progress.
type obsSettle struct {
	t       *txn
	touch   *observedTouch
	store   *obsStore
	streams map[int64]*obsStreamInfo
	// ranges are the instants each stream form's old and new segments
	// covered, whose buckets are recomputed.
	ranges map[obsSF][]obsSpan
	// latest is each touched stream's change in its count of available
	// points.
	latest map[int64]int64
	// tiers caches each metric's rollup tiers.
	tiers map[string][]time.Duration
	// fresh are the rows of the segments the settle wrote, by stream form.
	fresh map[obsSF][]*observedRows
}

// obsGroup is one rollup row's group inside a bucket. offsetUnknown is the
// record's offset_known false: its points carried no UTC offset and offset
// is 0 for want of one. Its zero value is a recorded offset.
type obsGroup struct {
	code, unit    string
	delta         bool
	offset        int64
	offsetUnknown bool
}

func (g obsGroup) key() string {
	k := g.code + "\x1f" + g.unit + "\x1f" + strconv.FormatBool(g.delta) + "\x1f" + strconv.FormatInt(g.offset, 10)
	if g.offsetUnknown {
		k += "\x1f?"
	}
	return k
}

// rollupRow is one computed row of observed_rollups.
type rollupRow struct {
	group obsGroup
	start time.Time
	stats *observed.Stats
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

// --- segments ---

// applySegments writes each written record's segment where its fingerprint
// moved (or the pass forces it), allocating stream ids and widening
// extents, and records the instants and streams the old and new segments
// touched.
func (s *obsSettle) applySegments() error {
	refs := make([]eref, 0, len(s.touch.ops))
	for ref := range s.touch.ops {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].ID < refs[j].ID
	})
	olds, err := s.oldSegments(refs)
	if err != nil {
		return err
	}
	var gone []eref
	var fresh []*observedRows
	for _, ref := range refs {
		nr := s.touch.ops[ref]
		old, had := olds[ref]
		if nr != nil {
			s.store.put(ref, nr)
		}
		if had && nr != nil && !s.touch.force && bytes.Equal(old.fingerprint, nr.fingerprint()) {
			continue
		}
		if had {
			gone = append(gone, ref)
			s.touchSegment(old)
			s.latest[old.stream] -= int64(old.available)
		}
		if nr != nil {
			fresh = append(fresh, nr)
		}
	}
	if err := s.deleteSegments(gone); err != nil {
		return err
	}
	ids, err := s.streamIDs(fresh)
	if err != nil {
		return err
	}
	segs := make([]obsSegment, len(fresh))
	s.fresh = map[obsSF][]*observedRows{}
	for i, nr := range fresh {
		segs[i] = nr.segment()
		segs[i].stream = ids[nr.key]
		sf := obsSF{stream: segs[i].stream, form: nr.form}
		s.fresh[sf] = append(s.fresh[sf], nr)
		s.touchSegment(segs[i])
		s.latest[segs[i].stream] += int64(segs[i].available)
	}
	if err := s.insertSegments(segs); err != nil {
		return err
	}
	if err := s.widenExtents(segs); err != nil {
		return err
	}
	var touched []int64
	for sf := range s.ranges {
		touched = append(touched, sf.stream)
	}
	for id := range s.latest {
		touched = append(touched, id)
	}
	slices.Sort(touched)
	s.streams, err = streamInfos(s.t.ctx, s.t.tx, slices.Compact(touched))
	return err
}

func (s *obsSettle) touchSegment(seg obsSegment) {
	sf := obsSF{stream: seg.stream, form: seg.form}
	s.ranges[sf] = append(s.ranges[sf], obsSpan{lo: seg.first, hi: seg.last})
}

// oldSegments reads the stored segments of the written records.
func (s *obsSettle) oldSegments(refs []eref) (map[eref]obsSegment, error) {
	out := map[eref]obsSegment{}
	if len(refs) == 0 {
		return out, nil
	}
	kinds, ids := make([]string, len(refs)), make([]string, len(refs))
	for i, r := range refs {
		kinds[i], ids[i] = r.Kind, r.ID
	}
	rows, err := s.t.query(`SELECT `+segmentColumns+`, fingerprint FROM observed_segments
		WHERE (kind, record_id) IN (SELECT * FROM unnest($1::text[], $2::text[]))`, kinds, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var seg obsSegment
		var last sql.NullTime
		var codes []byte
		if err := rows.Scan(&seg.ref.Kind, &seg.ref.ID, &seg.stream, &seg.form, &seg.first, &seg.last, &seg.end, &seg.points, &seg.available,
			&last, &seg.offset, &seg.known, &codes, &seg.fingerprint); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if last.Valid {
			seg.lastAvailable = last.Time
		}
		seg.codes = jsonStrings(codes)
		out[seg.ref] = seg
	}
	return out, finishRows(rows)
}

func (s *obsSettle) deleteSegments(refs []eref) error {
	if len(refs) == 0 {
		return nil
	}
	kinds, ids := make([]string, len(refs)), make([]string, len(refs))
	for i, r := range refs {
		kinds[i], ids[i] = r.Kind, r.ID
	}
	_, err := s.t.exec(`DELETE FROM observed_segments g USING unnest($1::text[], $2::text[]) AS w(kind, id)
		WHERE g.kind = w.kind AND g.record_id = w.id`, kinds, ids)
	return err
}

// streamIDs is the id of each written record's stream, allocating the ones
// the repository has not seen. The observed lock makes this the only
// transaction allocating in the repository.
func (s *obsSettle) streamIDs(rows []*observedRows) (map[obsKey]int64, error) {
	out := map[obsKey]int64{}
	var keys []obsKey
	for _, r := range rows {
		if _, ok := out[r.key]; !ok {
			out[r.key] = 0
			keys = append(keys, r.key)
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	cols := keyColumns(keys)
	rs, err := s.t.query(`SELECT s.id, s.metric, s.kind, s.source, s.about, s.channel, s.statistic FROM observed_streams s
		JOIN unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[]) AS k(metric, kind, source, about, channel, statistic)
		  ON s.metric = k.metric AND s.kind = k.kind AND s.source = k.source AND s.about = k.about
		 AND s.channel = k.channel AND s.statistic = k.statistic`, cols...)
	if err != nil {
		return nil, err
	}
	if err := scanStreamIDs(rs, out); err != nil {
		return nil, err
	}
	var missing []obsKey
	for _, k := range keys {
		if out[k] == 0 {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	rs, err = s.t.query(`INSERT INTO observed_streams (metric, kind, source, about, channel, statistic)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
		RETURNING id, metric, kind, source, about, channel, statistic`, keyColumns(missing)...)
	if err != nil {
		return nil, err
	}
	return out, scanStreamIDs(rs, out)
}

// scanStreamIDs reads stream ids by key into out.
func scanStreamIDs(rs *sql.Rows, out map[obsKey]int64) error {
	for rs.Next() {
		var id int64
		var k obsKey
		if err := rs.Scan(&id, &k.metric, &k.kind, &k.source, &k.about, &k.channel, &k.statistic); err != nil {
			_ = rs.Close()
			return err
		}
		out[k] = id
	}
	return finishRows(rs)
}

func keyColumns(keys []obsKey) []any {
	cols := make([][]string, 6)
	for _, k := range keys {
		for i, v := range []string{k.metric, k.kind, k.source, k.about, k.channel, k.statistic} {
			cols[i] = append(cols[i], v)
		}
	}
	out := make([]any, 6)
	for i := range cols {
		out[i] = cols[i]
	}
	return out
}

func (s *obsSettle) insertSegments(segs []obsSegment) error {
	for len(segs) > 0 {
		n := min(len(segs), observedInsertBatch)
		if err := s.insertSegmentBatch(segs[:n]); err != nil {
			return err
		}
		segs = segs[n:]
	}
	return nil
}

func (s *obsSettle) insertSegmentBatch(segs []obsSegment) error {
	n := len(segs)
	kind, id, form, codes := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	stream, offset := make([]int64, n), make([]int64, n)
	first, last, end := make([]time.Time, n), make([]time.Time, n), make([]time.Time, n)
	lastAvailable := make([]*time.Time, n)
	points, available := make([]int64, n), make([]int64, n)
	known := make([]bool, n)
	fp := make([][]byte, n)
	for i, g := range segs {
		kind[i], id[i], form[i], stream[i] = g.ref.Kind, g.ref.ID, g.form, g.stream
		first[i], last[i], end[i], points[i], available[i] = g.first, g.last, g.end, int64(g.points), int64(g.available)
		if g.available > 0 {
			lastAvailable[i] = ptrTo(g.lastAvailable)
		}
		offset[i], known[i], fp[i] = g.offset, g.known, g.fingerprint
		raw, err := jsonb(g.codes)
		if err != nil {
			return err
		}
		codes[i] = string(raw)
	}
	_, err := s.t.exec(`
		INSERT INTO observed_segments (kind, record_id, stream, form, first_ts, last_ts, end_ts, points, available,
			last_available, utc_offset_min, offset_known, codes, fingerprint)
		SELECT u.kind, u.id, u.stream, u.form, u.first, u.last, u.e, u.points, u.available, u.last_available, u.off, u.known,
			ARRAY(SELECT jsonb_array_elements_text(u.codes::jsonb)), u.fp
		FROM unnest($1::text[], $2::text[], $3::bigint[], $4::text[], $5::timestamptz[], $6::timestamptz[], $7::timestamptz[],
		            $8::int[], $9::int[], $10::timestamptz[], $11::int[], $12::boolean[], $13::text[], $14::bytea[])
		     AS u(kind, id, stream, form, first, last, e, points, available, last_available, off, known, codes, fp)`,
		kind, id, stream, form, first, last, end, points, available, lastAvailable, offset, known, codes, fp)
	return err
}

// widenExtents raises each written stream's max_extent_ms to its longest
// new segment's, rounded up to the millisecond. It never lowers one: a bound
// too wide costs a read time, one too narrow its points.
func (s *obsSettle) widenExtents(segs []obsSegment) error {
	longest := map[int64]int64{}
	for _, g := range segs {
		longest[g.stream] = max(longest[g.stream], g.extentMillis())
	}
	if len(longest) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(longest))
	for id := range longest {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	ms := make([]int64, len(ids))
	for i, id := range ids {
		ms[i] = longest[id]
	}
	_, err := s.t.exec(`UPDATE observed_streams s SET max_extent_ms = w.ms
		FROM unnest($1::bigint[], $2::bigint[]) AS w(id, ms)
		WHERE s.id = w.id AND s.max_extent_ms < w.ms`, ids, ms)
	return err
}

// --- rollups ---

// tiersOf is a metric's rollup tiers, finest first: what its record's
// `rollups` names, the default where it names none or no metric record holds
// the path.
func (s *obsSettle) tiersOf(metric string) ([]time.Duration, error) {
	if tiers, ok := s.tiers[metric]; ok {
		return tiers, nil
	}
	c, ok, err := metricContract(s.t.ctx, s.t.tx, s.t.ds.registry(), metric)
	if err != nil {
		return nil, err
	}
	tiers := c.Rollups
	if !ok {
		tiers = rowTiers(nil)
	}
	s.tiers[metric] = tiers
	return tiers, nil
}

// mergeSpans sorts spans and joins those that overlap or touch.
func mergeSpans(spans []obsSpan) []obsSpan {
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo.Before(spans[j].lo) })
	var out []obsSpan
	for _, sp := range spans {
		if n := len(out); n > 0 && !sp.lo.After(out[n-1].hi) {
			if sp.hi.After(out[n-1].hi) {
				out[n-1].hi = sp.hi
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

// changedBuckets are the buckets a tier's recompute rewrote, each with the
// UTC offsets of the groups its old and new rows held: the next tier's
// buckets that hold them, one per alignment.
type changedBuckets map[time.Time]map[int64]bool

func (c changedBuckets) add(start time.Time, offset int64) {
	if c[start] == nil {
		c[start] = map[int64]bool{}
	}
	c[start][offset] = true
}

// recomputeRollups recomputes, stream form by stream form, every bucket of
// every tier the touched instants reach: the finest tier from the points,
// each coarser one from the tier below it.
func (s *obsSettle) recomputeRollups() error {
	forms := make([]obsSF, 0, len(s.ranges))
	for sf := range s.ranges {
		forms = append(forms, sf)
	}
	sort.Slice(forms, func(i, j int) bool {
		if forms[i].stream != forms[j].stream {
			return forms[i].stream < forms[j].stream
		}
		return forms[i].form < forms[j].form
	})
	for _, sf := range forms {
		if sf.form == vocabulary.FormSummary {
			continue
		}
		info := s.streams[sf.stream]
		if info == nil {
			continue
		}
		tiers, err := s.tiersOf(info.key.metric)
		if err != nil {
			return err
		}
		spans := mergeSpans(s.ranges[sf])
		// Rows of tiers the metric no longer names exist only after its
		// `rollups` changed, and that change queues the pass, which forces
		// every row of the metric; a live write leaves them to it.
		if s.touch.force {
			if err := s.dropUnnamedTiers(sf, tiers, spans); err != nil {
				return err
			}
		}
		if len(tiers) == 0 {
			continue
		}
		changed, err := s.recomputeFinest(sf, info, tiers[0], spans)
		if err != nil {
			return err
		}
		for i := 1; i < len(tiers) && len(changed) > 0; i++ {
			if changed, err = s.recomputeCoarser(sf, info, tiers[i], tiers[i-1], changed); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropUnnamedTiers deletes the rows of the tiers the metric no longer names
// whose buckets meet the spans: a metric whose `rollups` changed is
// re-derived row by row, and each row's spans take its stale tiers with it.
func (s *obsSettle) dropUnnamedTiers(sf obsSF, tiers []time.Duration, spans []obsSpan) error {
	var unnamed []int64
	for _, w := range allTiers() {
		if !slices.Contains(tiers, w) {
			unnamed = append(unnamed, tierSeconds(w))
		}
	}
	if len(unnamed) == 0 {
		return nil
	}
	lo, hi := spanArrays(spansOf(spans))
	_, err := s.t.exec(`DELETE FROM observed_rollups r USING unnest($4::timestamptz[], $5::timestamptz[]) AS w(lo, hi)
		WHERE r.stream = $1 AND r.form = $2 AND r.tier = ANY($3::int[])
		  AND r.bucket > w.lo - interval '1 day' AND r.bucket <= w.hi`, sf.stream, sf.form, unnamed, lo, hi)
	return err
}

func spansOf(spans []obsSpan) [][2]time.Time {
	out := make([][2]time.Time, len(spans))
	for i, sp := range spans {
		out[i] = [2]time.Time{sp.lo, sp.hi}
	}
	return out
}

// recomputeFinest recomputes the finest tier's buckets that meet the
// spans: those an old row stood in and those a current point falls in, and
// for a counter the bucket of the next available point after them, whose
// step in comes from their last. Each bucket is computed from the points in
// it (observed.BucketBuilder) and keeps the groups aligned to it.
func (s *obsSettle) recomputeFinest(sf obsSF, info *obsStreamInfo, w time.Duration, spans []obsSpan) (changedBuckets, error) {
	changed := changedBuckets{}
	done := map[time.Time]bool{}
	var all []rollupRow
	for _, sp := range spans {
		// The buckets to recompute are those an old point stood in, which
		// its row names, and those a new point falls in; the points of the
		// stream's other records are read for those buckets alone.
		old, err := s.oldBuckets(sf, w, sp.lo.Add(-w), sp.hi)
		if err != nil {
			return nil, err
		}
		window := map[time.Time]bool{}
		for start, offs := range old {
			window[start] = true
			for off := range offs {
				changed.add(start, off)
			}
		}
		for _, r := range s.fresh[sf] {
			for _, p := range r.points {
				if p.ts.Before(sp.lo) || p.ts.After(sp.hi) {
					continue
				}
				start := observed.TierStart(p.ts, w, p.offset)
				window[start] = true
				changed.add(start, p.offset)
			}
		}
		if len(window) == 0 {
			continue
		}
		starts := sortedTimes(window)
		pts, err := s.store.points(info, sf.form, starts[0], starts[len(starts)-1].Add(w))
		if err != nil {
			return nil, err
		}
		if sf.form == vocabulary.FormCounter {
			// The next available reading after the span takes its step in
			// from the span's last, and every bucket holding it reads that
			// step, at each alignment a row stands at: one may start inside
			// the window's buckets at another offset. Between the span and
			// that reading no point is available, so the points read stay
			// contiguous for the step-in lookups.
			next, err := s.store.atOrAfter(info, sf.form, sp.hi.Add(time.Nanosecond), true)
			if err != nil {
				return nil, err
			}
			if next != nil {
				g := s.store.groups[next.Group]
				holding, err := s.oldBuckets(sf, w, next.At.Add(-w), next.At)
				if err != nil {
					return nil, err
				}
				holding.add(observed.TierStart(next.At, w, g.offset), g.offset)
				for start, offs := range holding {
					for off := range offs {
						changed.add(start, off)
					}
					if window[start] {
						continue
					}
					more, err := s.store.points(info, sf.form, start, start.Add(w))
					if err != nil {
						return nil, err
					}
					pts = mergePoints(pts, more)
					window[start] = true
				}
			}
		}
		var todo []time.Time
		starts = sortedTimes(window)
		for _, start := range starts {
			if !done[start] {
				done[start] = true
				todo = append(todo, start)
			}
		}
		rows, err := s.bucketsFromPoints(sf, info, w, todo, pts, starts[0])
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}
	if err := s.replaceRows(sf, w, sortedTimes(done)); err != nil {
		return nil, err
	}
	return changed, s.insertRollups(sf, w, all)
}

// bucketsFromPoints computes the buckets of width w at starts from pts,
// every point of the stream form in each of them, in Before order; from is
// where pts begins, before which a counter's step in is looked up.
func (s *obsSettle) bucketsFromPoints(sf obsSF, info *obsStreamInfo, w time.Duration, starts []time.Time, pts []observed.Point, from time.Time) ([]rollupRow, error) {
	var out []rollupRow
	var earlier *observed.Point
	looked := false
	for _, start := range starts {
		var prev *observed.Point
		if sf.form == vocabulary.FormCounter {
			for i := len(pts) - 1; i >= 0; i-- {
				if pts[i].At.Before(start) && !pts[i].Unavailable {
					p := pts[i]
					prev = &p
					break
				}
			}
			if prev == nil {
				if !looked {
					var err error
					if earlier, err = s.store.before(info, sf.form, from, true); err != nil {
						return nil, err
					}
					looked = true
				}
				prev = earlier
			}
		}
		end := start.Add(w)
		b := observed.NewBucketBuilder(sf.form, start, end, prev)
		lo := sort.Search(len(pts), func(i int) bool { return !pts[i].At.Before(start) })
		for _, p := range pts[lo:] {
			if !p.At.Before(end) {
				break
			}
			b.Add(p)
		}
		out = append(out, s.alignedRows(start, w, b.Rows())...)
	}
	return out, nil
}

// alignedRows keeps the computed rows whose group the bucket at start of
// width w is aligned to, in group order.
func (s *obsSettle) alignedRows(start time.Time, w time.Duration, stats map[string]*observed.Stats) []rollupRow {
	var out []rollupRow
	for _, key := range sortedKeys(stats) {
		g := s.store.groups[key]
		if observed.TierStart(start, w, g.offset) != start {
			continue
		}
		out = append(out, rollupRow{group: g, start: start, stats: stats[key]})
	}
	return out
}

// recomputeCoarser recomputes the buckets of tier w that hold a changed
// bucket of the finer tier below it, one per alignment the changed bucket's
// groups name: composed from the finer rows inside it (observed.Compose), or
// from the points where those cannot answer exactly.
func (s *obsSettle) recomputeCoarser(sf obsSF, info *obsStreamInfo, w, finer time.Duration, below changedBuckets) (changedBuckets, error) {
	set := map[time.Time]bool{}
	for start, offs := range below {
		for off := range offs {
			set[observed.TierStart(start, w, off)] = true
		}
	}
	// A point ends the hold of the point before it whatever their offsets,
	// so a bucket of this tier at another alignment that overlaps a changed
	// finer bucket may have changed too, though none of its groups did.
	overlapping, err := s.bucketsOverlapping(sf, w, sortedTimes(belowStarts(below)), finer)
	if err != nil {
		return nil, err
	}
	for start := range overlapping {
		set[start] = true
	}
	starts := sortedTimes(set)
	changed := changedBuckets{}
	old, err := s.oldBucketsAt(sf, w, starts)
	if err != nil {
		return nil, err
	}
	for start, offs := range old {
		for off := range offs {
			changed.add(start, off)
		}
	}
	fine, err := s.rowsMeeting(sf, finer, starts, w)
	if err != nil {
		return nil, err
	}
	var all []rollupRow
	for _, start := range starts {
		end := start.Add(w)
		var inside []observed.Row
		for _, r := range fine {
			if r.End.After(start) && r.Start.Before(end) {
				inside = append(inside, r)
			}
		}
		keep := func(key string) bool {
			return observed.TierStart(start, w, s.store.groups[key].offset) == start
		}
		stats, ok := observed.Compose(sf.form, start, end, inside, keep)
		if !ok {
			pts, err := s.store.points(info, sf.form, start, end)
			if err != nil {
				return nil, err
			}
			rows, err := s.bucketsFromPoints(sf, info, w, []time.Time{start}, pts, start)
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				changed.add(start, r.group.offset)
			}
			all = append(all, rows...)
			continue
		}
		for _, r := range s.alignedRows(start, w, stats) {
			changed.add(start, r.group.offset)
			all = append(all, r)
		}
	}
	if err := s.replaceRows(sf, w, starts); err != nil {
		return nil, err
	}
	return changed, s.insertRollups(sf, w, all)
}

// belowStarts is the set of the changed buckets' starts.
func belowStarts(c changedBuckets) map[time.Time]bool {
	out := make(map[time.Time]bool, len(c))
	for start := range c {
		out[start] = true
	}
	return out
}

func sortedTimes(set map[time.Time]bool) []time.Time {
	out := make([]time.Time, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// mergePoints joins two Before-ordered point lists, dropping a point both
// hold.
func mergePoints(a, b []observed.Point) []observed.Point {
	out := append(slices.Clone(a), b...)
	sortPoints(out)
	return slices.CompactFunc(out, func(x, y observed.Point) bool {
		return x.RecordID == y.RecordID && x.Ord == y.Ord && x.At.Equal(y.At)
	})
}

// oldBuckets reads the buckets of tier w with a row starting in (lo, hi],
// each with its rows' offsets.
func (s *obsSettle) oldBuckets(sf obsSF, w time.Duration, lo, hi time.Time) (changedBuckets, error) {
	rows, err := s.t.query(`SELECT DISTINCT bucket, utc_offset_min FROM observed_rollups
		WHERE stream = $1 AND form = $2 AND tier = $3 AND bucket > $4 AND bucket <= $5`,
		sf.stream, sf.form, tierSeconds(w), lo, hi)
	if err != nil {
		return nil, err
	}
	return scanBuckets(rows)
}

// bucketsOverlapping reads the buckets of tier w holding a row that overlap
// one of the buckets of width span at starts, each with its rows' offsets.
func (s *obsSettle) bucketsOverlapping(sf obsSF, w time.Duration, starts []time.Time, span time.Duration) (changedBuckets, error) {
	if len(starts) == 0 {
		return changedBuckets{}, nil
	}
	rows, err := s.t.query(`SELECT DISTINCT r.bucket, r.utc_offset_min
		FROM unnest($4::timestamptz[]) AS c(a)
		JOIN observed_rollups r ON r.stream = $1 AND r.form = $2 AND r.tier = $3
		 AND r.bucket > c.a - $5::bigint * interval '1 second' AND r.bucket < c.a + $6::bigint * interval '1 second'`,
		sf.stream, sf.form, tierSeconds(w), starts, tierSeconds(w), tierSeconds(span))
	if err != nil {
		return nil, err
	}
	return scanBuckets(rows)
}

// oldBucketsAt reads which of the buckets of tier w at starts hold a row,
// each with its rows' offsets.
func (s *obsSettle) oldBucketsAt(sf obsSF, w time.Duration, starts []time.Time) (changedBuckets, error) {
	rows, err := s.t.query(`SELECT DISTINCT bucket, utc_offset_min FROM observed_rollups
		WHERE stream = $1 AND form = $2 AND tier = $3 AND bucket = ANY($4::timestamptz[])`,
		sf.stream, sf.form, tierSeconds(w), starts)
	if err != nil {
		return nil, err
	}
	return scanBuckets(rows)
}

func scanBuckets(rows *sql.Rows) (changedBuckets, error) {
	out := changedBuckets{}
	for rows.Next() {
		var start time.Time
		var off int64
		if err := rows.Scan(&start, &off); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.add(start.UTC(), off)
	}
	return out, finishRows(rows)
}

// rowsMeeting reads the rows of tier w whose bucket meets one of the buckets
// of width span at starts.
func (s *obsSettle) rowsMeeting(sf obsSF, w time.Duration, starts []time.Time, span time.Duration) ([]observed.Row, error) {
	ends := make([]time.Time, len(starts))
	for i, st := range starts {
		ends[i] = st.Add(span)
	}
	rows, err := s.t.query(`SELECT DISTINCT ON (r.bucket, r.code, r.unit, r.delta, r.utc_offset_min, r.offset_known) `+rollupColumns+`
		FROM unnest($4::timestamptz[], $5::timestamptz[]) AS c(a, b)
		CROSS JOIN LATERAL (
			SELECT * FROM observed_rollups r
			WHERE r.stream = $1 AND r.form = $2 AND r.tier = $3
			  AND r.bucket > c.a - $6::bigint * interval '1 second' AND r.bucket < c.b) r
		ORDER BY r.bucket, r.code, r.unit, r.delta, r.utc_offset_min, r.offset_known`,
		sf.stream, sf.form, tierSeconds(w), starts, ends, tierSeconds(w))
	if err != nil {
		return nil, err
	}
	var out []observed.Row
	for rows.Next() {
		row, g, err := scanRollup(rows, w)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		s.store.groups[g.key()] = g
		out = append(out, row)
	}
	return out, finishRows(rows)
}

// replaceRows deletes the rows of tier w at starts.
func (s *obsSettle) replaceRows(sf obsSF, w time.Duration, starts []time.Time) error {
	if len(starts) == 0 {
		return nil
	}
	_, err := s.t.exec(`DELETE FROM observed_rollups WHERE stream = $1 AND form = $2 AND tier = $3 AND bucket = ANY($4::timestamptz[])`,
		sf.stream, sf.form, tierSeconds(w), starts)
	return err
}

// insertRollups writes one stream form's computed rows of one tier, in
// batches.
func (s *obsSettle) insertRollups(sf obsSF, w time.Duration, rows []rollupRow) error {
	for len(rows) > 0 {
		n := min(len(rows), observedInsertBatch)
		if err := s.insertRollupBatch(sf, w, rows[:n]); err != nil {
			return err
		}
		rows = rows[n:]
	}
	return nil
}

func (s *obsSettle) insertRollupBatch(sf obsSF, w time.Duration, rows []rollupRow) error {
	n := len(rows)
	var (
		code, unit                      = make([]string, n), make([]string, n)
		delta, known                    = make([]bool, n), make([]bool, n)
		offset, count, bounded, covered = make([]int64, n), make([]int64, n), make([]int64, n), make([]int64, n)
		maxGap, startGap, endGap        = make([]int64, n), make([]int64, n), make([]int64, n)
		bucket                          = make([]time.Time, n)
		sum, minV, maxV, mean, m2       = make([]*float64, n), make([]*float64, n), make([]*float64, n), make([]*float64, n), make([]*float64, n)
		twa, sin, cos                   = make([]*float64, n), make([]*float64, n), make([]*float64, n)
		firstTS, lastTS                 = make([]*time.Time, n), make([]*time.Time, n)
		firstValue, lastValue, increase = make([]*string, n), make([]*string, n), make([]*string, n)
		held, lastHold, resets          = make([]*int64, n), make([]*int64, n), make([]*int64, n)
		spanEnd                         = make([]*time.Time, n)
		firstRecord, lastRecord         = make([]*string, n), make([]*string, n)
		firstOrd, lastOrd               = make([]*int64, n), make([]*int64, n)
		minExact, maxExact              = make([]*string, n), make([]*string, n)
	)
	for i, r := range rows {
		st := r.stats
		if !st.SpanEnd.IsZero() {
			spanEnd[i] = ptrTo(st.SpanEnd)
		}
		code[i], unit[i], delta[i], offset[i], known[i] = r.group.code, r.group.unit, r.group.delta, r.group.offset, !r.group.offsetUnknown
		bucket[i], count[i], bounded[i], covered[i] = r.start, st.N, st.Bounded, st.CoveredMs
		maxGap[i], startGap[i], endGap[i] = st.MaxGapMs, st.StartGapMs, st.EndGapMs
		if st.Values > 0 {
			sum[i], minV[i], maxV[i], mean[i], m2[i] = ptrTo(st.Sum), ptrTo(st.Min), ptrTo(st.Max), ptrTo(st.Mean), ptrTo(st.M2)
			minExact[i], maxExact[i] = ptrTo(st.MinExact), ptrTo(st.MaxExact)
		}
		if st.First != nil {
			firstTS[i], firstValue[i] = ptrTo(st.First.At), digitsOf(st.First)
			lastTS[i], lastValue[i] = ptrTo(st.Last.At), digitsOf(st.Last)
			firstRecord[i], firstOrd[i] = ptrTo(st.First.RecordID), ptrTo(st.First.Ord)
			lastRecord[i], lastOrd[i] = ptrTo(st.Last.RecordID), ptrTo(st.Last.Ord)
		}
		if sf.form == vocabulary.FormGauge {
			held[i], lastHold[i] = ptrTo(st.HeldMs), st.LastHoldMs
			if st.Values > 0 {
				twa[i] = ptrTo(st.TWASum)
			}
		}
		if st.Increase != nil {
			d, _ := vocabulary.RatDecimal(st.Increase, vocabulary.ConvertedDigits)
			increase[i], resets[i] = &d, ptrTo(st.Resets)
		}
		if st.Angles > 0 {
			sin[i], cos[i] = ptrTo(st.SinSum), ptrTo(st.CosSum)
		}
	}
	_, err := s.t.exec(`
		INSERT INTO observed_rollups (stream, form, tier,
			code, unit, delta, utc_offset_min, bucket, n, bounded, sum, min, max, mean, m2,
			first_ts, first_value, last_ts, last_value, covered_ms, held_ms, twa_sum, last_hold_ms,
			max_gap_ms, start_gap_ms, increase, resets, sin_sum, cos_sum, span_end,
			first_record_id, first_ord, last_record_id, last_ord, min_value, max_value, offset_known, end_gap_ms)
		SELECT $1, $2, $3,
			u.code, u.unit, u.delta, u.off, u.bucket, u.n, u.bounded, u.sum, u.min, u.max, u.mean, u.m2,
			u.first_ts, u.first_value::numeric, u.last_ts, u.last_value::numeric, u.covered, u.held, u.twa, u.last_hold,
			u.max_gap, u.start_gap, u.increase::numeric, u.resets, u.sin, u.cos, u.span_end,
			u.first_record, u.first_ord, u.last_record, u.last_ord, u.min_value::numeric, u.max_value::numeric, u.known, u.end_gap
		FROM unnest($4::text[], $5::text[], $6::boolean[], $7::int[], $8::timestamptz[], $9::bigint[], $10::bigint[],
		            $11::float8[], $12::float8[], $13::float8[], $14::float8[], $15::float8[],
		            $16::timestamptz[], $17::text[], $18::timestamptz[], $19::text[], $20::bigint[], $21::bigint[],
		            $22::float8[], $23::bigint[], $24::bigint[], $25::bigint[], $26::text[], $27::int[],
		            $28::float8[], $29::float8[], $30::timestamptz[],
		            $31::text[], $32::int[], $33::text[], $34::int[], $35::text[], $36::text[], $37::boolean[], $38::bigint[])
		     AS u(code, unit, delta, off, bucket, n, bounded, sum, min, max, mean, m2,
		          first_ts, first_value, last_ts, last_value, covered, held, twa, last_hold,
		          max_gap, start_gap, increase, resets, sin, cos, span_end,
		          first_record, first_ord, last_record, last_ord, min_value, max_value, known, end_gap)`,
		sf.stream, sf.form, tierSeconds(w),
		code, unit, delta, offset, bucket, count, bounded, sum, minV, maxV, mean, m2,
		firstTS, firstValue, lastTS, lastValue, covered, held, twa, lastHold,
		maxGap, startGap, increase, resets, sin, cos, spanEnd,
		firstRecord, firstOrd, lastRecord, lastOrd, minExact, maxExact, known, endGap)
	return err
}

// digitsOf is a point's stored digits, nil for a code.
func digitsOf(p *observed.Point) *string {
	if !p.HasValue || p.Exact == "" {
		return nil
	}
	return ptrTo(p.Exact)
}

// rollupColumns are the columns of observed_rollups scanRollup reads.
const rollupColumns = `r.code, r.unit, r.delta, r.utc_offset_min, r.offset_known, r.bucket, r.n, r.bounded, r.sum, r.min, r.max, r.mean, r.m2,
	r.first_ts, r.first_value::text, r.last_ts, r.last_value::text, r.covered_ms, r.held_ms, r.twa_sum, r.last_hold_ms,
	r.max_gap_ms, r.start_gap_ms, r.end_gap_ms, r.increase::text, r.resets, r.sin_sum, r.cos_sum, r.span_end,
	r.first_record_id, r.first_ord, r.last_record_id, r.last_ord, r.min_value::text, r.max_value::text`

// scanRollup reads rollupColumns, after any leading columns in lead, into a
// row of tier w and its group.
func scanRollup(rows *sql.Rows, w time.Duration, lead ...any) (observed.Row, obsGroup, error) {
	var (
		g                                            obsGroup
		known                                        bool
		bucket                                       time.Time
		n, bounded, covered, maxGap, startGp, endGap int64
		sum, minV, maxV, mean, m2, twa               sql.NullFloat64
		sin, cos                                     sql.NullFloat64
		firstTS, lastTS                              sql.NullTime
		firstV, lastV, increase                      sql.NullString
		held, lastHold                               sql.NullInt64
		resets                                       sql.NullInt64
		spanEnd                                      sql.NullTime
		firstRecord, lastRecord                      sql.NullString
		firstOrd, lastOrd                            sql.NullInt64
		minExact, maxExact                           sql.NullString
	)
	dests := append(lead, &g.code, &g.unit, &g.delta, &g.offset, &known, &bucket, &n, &bounded, &sum, &minV, &maxV, &mean, &m2,
		&firstTS, &firstV, &lastTS, &lastV, &covered, &held, &twa, &lastHold, &maxGap, &startGp, &endGap, &increase, &resets, &sin, &cos,
		&spanEnd, &firstRecord, &firstOrd, &lastRecord, &lastOrd, &minExact, &maxExact)
	if err := rows.Scan(dests...); err != nil {
		return observed.Row{}, g, err
	}
	g.offsetUnknown = !known
	key := g.key()
	st := observed.Stats{
		N: n, Bounded: bounded, Sum: sum.Float64, Min: minV.Float64, Max: maxV.Float64, Mean: mean.Float64, M2: m2.Float64,
		MinExact: minExact.String, MaxExact: maxExact.String,
		CoveredMs: covered, HeldMs: held.Int64, TWASum: twa.Float64, MaxGapMs: maxGap, StartGapMs: startGp, EndGapMs: endGap,
		Resets: resets.Int64, SinSum: sin.Float64, CosSum: cos.Float64,
	}
	if spanEnd.Valid {
		st.SpanEnd = spanEnd.Time.UTC()
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
		at := ts.Time.UTC()
		p := &observed.Point{At: at, End: at, Code: g.code, Unit: g.unit, Group: key}
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
	start := bucket.UTC()
	return observed.Row{Start: start, End: start.Add(w), Group: key, Stats: st}, g, nil
}

// --- latest ---

// recomputeLatest rewrites every touched stream's latest row from its
// newest available point, its point count moved by the transaction's change
// in it; a stream left with no available point loses its row.
func (s *obsSettle) recomputeLatest() error {
	ids := make([]int64, 0, len(s.latest))
	for id := range s.latest {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		info := s.streams[id]
		if info == nil {
			continue
		}
		r, at, err := s.store.newest(info)
		if err != nil {
			return err
		}
		if r == nil {
			if _, err := s.t.exec(`DELETE FROM observed_latest WHERE stream = $1`, id); err != nil {
				return err
			}
			continue
		}
		p := r.points[at]
		// The background pass counts the stream's points again from its
		// segments, so a re-derivation also repairs a count that drifted;
		// a write moves the count by what it changed.
		points, exact := s.latest[id], false
		if s.touch.force {
			if err := s.t.row(`SELECT coalesce(sum(available), 0) FROM observed_segments WHERE stream = $1`, id).Scan(&points); err != nil {
				return err
			}
			exact = true
		}
		if _, err := s.t.exec(`
			INSERT INTO observed_latest (stream, ts, ts_end, value, code, unit, delta, form, comparator,
				utc_offset_min, offset_known, record_id, points)
			VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (repository, stream) DO UPDATE SET
				ts = EXCLUDED.ts, ts_end = EXCLUDED.ts_end, value = EXCLUDED.value, code = EXCLUDED.code,
				unit = EXCLUDED.unit, delta = EXCLUDED.delta, form = EXCLUDED.form, comparator = EXCLUDED.comparator,
				utc_offset_min = EXCLUDED.utc_offset_min, offset_known = EXCLUDED.offset_known,
				record_id = EXCLUDED.record_id,
				points = CASE WHEN $14 THEN EXCLUDED.points ELSE observed_latest.points + EXCLUDED.points END`,
			id, p.ts, p.tsEnd, p.exact, p.code, r.unit, r.delta, r.form, p.comparator,
			p.offset, p.offsetKnown, r.id, points, exact); err != nil {
			return err
		}
	}
	return nil
}

// --- metrics ---

// requestMetrics owes the background re-derivation the rows of every metric
// whose tiers the transaction moved, kind by kind among the kinds holding
// a stream of it, and starts the pass once the transaction commits.
func (s *obsSettle) requestMetrics() error {
	if len(s.touch.metrics) == 0 {
		return nil
	}
	byKind := map[string][]string{}
	for _, metric := range sortedKeys(s.touch.metrics) {
		rows, err := s.t.query(`SELECT DISTINCT kind FROM observed_streams WHERE metric = $1`, metric)
		if err != nil {
			return err
		}
		for rows.Next() {
			var kind string
			if err := rows.Scan(&kind); err != nil {
				_ = rows.Close()
				return err
			}
			byKind[kind] = append(byKind[kind], metric)
		}
		if err := finishRows(rows); err != nil {
			return err
		}
	}
	if len(byKind) == 0 {
		return nil
	}
	var owed []indexReprojection
	for _, kind := range sortedKeys(byKind) {
		owed = append(owed, indexReprojection{kind: kind, observed: indexFilter{moved: true, metrics: byKind[kind]}})
	}
	if err := s.t.requestReprojections(owed); err != nil {
		return err
	}
	s.t.afterCommit = append(s.t.afterCommit, s.t.ds.kickIndexReprojection)
	return nil
}
