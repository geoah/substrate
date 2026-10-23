package engine

// The observed index's segments (observedindex.go): what the index keeps of
// each observed record is its stream, its extent and what it holds, and its
// points are the record's own properties, derived again in Go by the same
// deriveObserved the fold runs. obsStore is that read, shared by the
// settle's rollup recompute and the aggregate read: a stream's segments that
// meet a range, the points they derive, and the point just before or after
// an instant, each found by one range of observed_segments_stream_idx bounded
// by the stream's longest extent.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// obsStreamInfo is one row of observed_streams.
type obsStreamInfo struct {
	id  int64
	key obsKey
	// maxExtentMs is the stream's longest segment extent in milliseconds. It
	// stays an integer count, never a time.Duration: a record may reach
	// centuries past its first point, past the 292 years a Duration holds,
	// and a wrapped bound would read every segment as out of reach.
	maxExtentMs int64
}

// obsEarliest is the lowest bound a segment read passes Postgres: no record
// starts before it (RFC 3339 starts at year 0, and an offset moves an
// instant less than a day), and Postgres holds instants back to 4713 BC, so
// a bound a long extent pushes past it is clamped here.
var obsEarliest = time.Date(-1, time.January, 1, 0, 0, 0, 0, time.UTC)

// reachBack is the earliest first instant a segment of the stream that
// reaches t can have: t less the stream's longest extent, clamped to
// obsEarliest.
func (i *obsStreamInfo) reachBack(t time.Time) time.Time {
	if lo := addMillis(t, -i.maxExtentMs); lo.After(obsEarliest) {
		return lo
	}
	return obsEarliest
}

// reach is the latest instant a segment of the stream starting at first can
// reach.
func (i *obsStreamInfo) reach(first time.Time) time.Time { return addMillis(first, i.maxExtentMs) }

// addMillis is t moved by ms milliseconds, exactly for any ms a record's
// extent reaches. It counts in seconds: a time.Duration of the same span
// wraps past 292 years.
func addMillis(t time.Time, ms int64) time.Time {
	secs, rem := ms/1000, ms%1000
	return time.Unix(t.Unix()+secs, int64(t.Nanosecond())+rem*int64(time.Millisecond)).In(t.Location())
}

// obsSegment is one row of observed_segments.
type obsSegment struct {
	ref               eref
	stream            int64
	form              string
	first, last, end  time.Time
	points, available int
	// lastAvailable is the newest available point's instant, meaningful only
	// where available is above zero: the zero time is an instant a point may
	// hold (0001-01-01T00:00:00Z), so it cannot stand for none.
	lastAvailable time.Time
	offset        int64
	known         bool
	codes         []string
	fingerprint   []byte
}

// segment is what one record's derived rows store in observed_segments.
func (r *observedRows) segment() obsSegment {
	s := obsSegment{ref: eref{Kind: r.key.kind, ID: r.id}, form: r.form, points: len(r.points)}
	codes := map[string]bool{}
	for i, p := range r.points {
		if i == 0 || p.ts.Before(s.first) {
			s.first = p.ts
		}
		if i == 0 || p.ts.After(s.last) {
			s.last = p.ts
		}
		if i == 0 || p.tsEnd.After(s.end) {
			s.end = p.tsEnd
		}
		s.offset, s.known = p.offset, p.offsetKnown
		if p.available() {
			s.available++
			codes[p.code] = true
			if s.available == 1 || p.ts.After(s.lastAvailable) {
				s.lastAvailable = p.ts
			}
		}
	}
	if s.end.Before(s.last) {
		s.end = s.last
	}
	s.codes = make([]string, 0, len(codes))
	for c := range codes {
		s.codes = append(s.codes, c)
	}
	sort.Strings(s.codes)
	s.fingerprint = r.fingerprint()
	return s
}

// extentMillis is how far the record's points reach from its first instant,
// in whole milliseconds rounded up: a segment read bounds a record by it, so
// a bound rounded down drops the points of a record that reaches past it by
// less than a millisecond. It is counted from the instants' seconds and
// nanoseconds, never end.Sub(first), which saturates at 292 years.
func (s obsSegment) extentMillis() int64 {
	if !s.end.After(s.first) {
		return 0
	}
	nanos := int64(s.end.Nanosecond()) - int64(s.first.Nanosecond())
	ms := (s.end.Unix()-s.first.Unix())*1000 + nanos/int64(time.Millisecond)
	if nanos > 0 && nanos%int64(time.Millisecond) != 0 {
		ms++
	}
	return ms
}

// fingerprint hashes everything the index reads from a record: its stream,
// its form, the columns its points share and every point. Two writes of a
// record with the same fingerprint derive the same segment, rollup rows and
// latest row, so the second derives nothing again.
func (r *observedRows) fingerprint() []byte {
	h := sha256.New()
	var buf [8]byte
	str := func(s string) {
		binary.BigEndian.PutUint64(buf[:], uint64(len(s)))
		h.Write(buf[:])
		h.Write([]byte(s))
	}
	num := func(n int64) {
		binary.BigEndian.PutUint64(buf[:], uint64(n))
		h.Write(buf[:])
	}
	flag := func(b bool) {
		if b {
			num(1)
		} else {
			num(0)
		}
	}
	optStr := func(s *string) {
		flag(s != nil)
		if s != nil {
			str(*s)
		}
	}
	optNum := func(n *int64) {
		flag(n != nil)
		if n != nil {
			num(*n)
		}
	}
	for _, s := range []string{r.key.metric, r.key.kind, r.key.source, r.key.about, r.key.channel, r.key.statistic, r.form, r.unit} {
		str(s)
	}
	flag(r.delta)
	flag(r.circular)
	// The turn, which a circular point's angle reads, so a turn edit moves
	// the fingerprint.
	flag(r.turn != nil)
	if r.turn != nil {
		num(int64(math.Float64bits(*r.turn)))
	}
	optNum(r.holdMs)
	optStr(r.resetRule)
	flag(r.resetAt != nil)
	if r.resetAt != nil {
		num(r.resetAt.UnixNano())
	}
	optStr(r.wrapAt)
	num(int64(len(r.points)))
	for _, p := range r.points {
		num(int64(p.ord))
		num(p.ts.UnixNano())
		num(p.tsEnd.UnixNano())
		num(p.offset)
		flag(p.offsetKnown)
		optStr(p.exact)
		// Where a point's base value stood before decision 0186, always
		// absent since; hashing the flag keeps every stored fingerprint.
		flag(false)
		str(p.code)
		str(p.comparator)
	}
	return h.Sum(nil)[:16]
}

// group is the rollup group a point of the record falls in.
func (r *observedRows) group(p obsPoint) obsGroup {
	return obsGroup{code: p.code, unit: r.unit, delta: r.delta, offset: p.offset, offsetUnknown: !p.offsetKnown}
}

// observedPoint is the i-th point as internal/observed reads it, its Group
// the key of its rollup group.
func (r *observedRows) observedPoint(i int) observed.Point {
	p := r.points[i]
	g := r.group(p)
	out := observed.Point{
		At: p.ts, End: p.tsEnd, Code: p.code, Comparator: p.comparator, Unit: r.unit,
		Unavailable: !p.available(), Group: g.key(), RecordID: r.id, Ord: int64(p.ord),
	}
	if p.value != nil {
		out.Value, out.HasValue = *p.value, true
	}
	if p.exact != nil {
		out.Exact = *p.exact
	}
	if r.holdMs != nil {
		d := time.Duration(*r.holdMs) * time.Millisecond
		out.Hold = &d
	}
	if r.resetRule != nil {
		out.Reset = *r.resetRule
	}
	if r.resetAt != nil {
		out.ResetAt = *r.resetAt
	}
	if r.wrapAt != nil {
		out.WrapAt = *r.wrapAt
	}
	// A circular point's angle in radians. A circular component holds only
	// offset-free conversions whose turns agree, so value / turn is one
	// direction in every unit of it (decision 0186).
	if r.circular && p.value != nil && r.turn != nil {
		out.Circular, out.Angle = true, *p.value*2*math.Pi / *r.turn
	}
	return out
}

// obsStore reads segments and derives their records' points, each record
// at most once.
type obsStore struct {
	ctx  context.Context
	q    dbx
	view kindLookup
	// rows is each derived record's rows; nil where the record derives none.
	rows map[eref]*observedRows
	// groups maps a point's Group key to its group.
	groups map[string]obsGroup
	// derived counts the points derived from stored records, which a read's
	// trace reports.
	derived int64
	// charge, where set, is handed the points of each record before or
	// atOrAfter is about to derive, before it derives them: an aggregate
	// read counts them against maxSamples, and a refusal stops the walk.
	charge func(n int64) error
	// units is the unit graph points derive through (deriveObserved).
	units *vocabulary.UnitGraph
}

func newObsStore(ctx context.Context, q dbx, view kindLookup, units *vocabulary.UnitGraph) *obsStore {
	return &obsStore{ctx: ctx, q: q, view: view, rows: map[eref]*observedRows{}, groups: map[string]obsGroup{}, units: units}
}

// segmentColumns are the columns of observed_segments scanSegment reads.
const segmentColumns = `kind, record_id, stream, form, first_ts, last_ts, end_ts, points, available,
	last_available, utc_offset_min, offset_known, to_jsonb(codes)`

// scanSegment reads segmentColumns after any leading columns in lead.
func scanSegment(rows *sql.Rows, lead ...any) (obsSegment, error) {
	var s obsSegment
	var last sql.NullTime
	var codes []byte
	if err := rows.Scan(append(lead, &s.ref.Kind, &s.ref.ID, &s.stream, &s.form, &s.first, &s.last, &s.end, &s.points, &s.available,
		&last, &s.offset, &s.known, &codes)...); err != nil {
		return s, err
	}
	if last.Valid {
		s.lastAvailable = last.Time
	}
	s.codes = jsonStrings(codes)
	return s, nil
}

func (st *obsStore) scanSegments(query string, args ...any) ([]obsSegment, error) {
	rows, err := st.q.QueryContext(st.ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: observed segments: %w", err)
	}
	var out []obsSegment
	for rows.Next() {
		s, err := scanSegment(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, s)
	}
	return out, finishRows(rows)
}

// segments lists the stream's segments of a form ("" for every form) whose
// points may meet [lo, hi): starting in [reachBack(lo), hi) and ending at or
// after lo, in first-point order.
func (st *obsStore) segments(info *obsStreamInfo, form string, lo, hi time.Time) ([]obsSegment, error) {
	return st.scanSegments(`SELECT `+segmentColumns+` FROM observed_segments
		WHERE stream = $1 AND ($2 = '' OR form = $2) AND first_ts >= $3 AND first_ts < $4 AND end_ts >= $5
		ORDER BY first_ts, record_id`, info.id, form, info.reachBack(lo), hi, lo)
}

// derive derives the records of segs it has not yet derived, a kind at a
// time.
func (st *obsStore) derive(segs []obsSegment) error {
	byKind := map[string][]string{}
	for _, s := range segs {
		if _, ok := st.rows[s.ref]; !ok {
			byKind[s.ref.Kind] = append(byKind[s.ref.Kind], s.ref.ID)
		}
	}
	for _, kind := range sortedKeys(byKind) {
		ids := byKind[kind]
		rows, err := st.q.QueryContext(st.ctx, `SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = ANY($2::text[])`, kind, ids)
		if err != nil {
			return fmt.Errorf("substrate/engine: observed records of %s: %w", kind, err)
		}
		ty, _ := st.view.ByIdentity(kind)
		for rows.Next() {
			row, err := scanRecord(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			st.put(row.ref(), deriveObserved(ty, row, st.units))
		}
		if err := finishRows(rows); err != nil {
			return err
		}
		for _, id := range ids {
			if _, ok := st.rows[eref{Kind: kind, ID: id}]; !ok {
				st.rows[eref{Kind: kind, ID: id}] = nil
			}
		}
	}
	return nil
}

// charged hands charge the points of a segment's record not yet derived.
func (st *obsStore) charged(s obsSegment) error {
	if st.charge == nil {
		return nil
	}
	if _, ok := st.rows[s.ref]; ok {
		return nil
	}
	return st.charge(int64(s.points))
}

// put records a record's derived rows, as a write derived them.
func (st *obsStore) put(ref eref, r *observedRows) {
	st.rows[ref] = r
	if r == nil {
		return
	}
	st.derived += int64(len(r.points))
	for _, p := range r.points {
		g := r.group(p)
		st.groups[g.key()] = g
	}
}

// pointsOf is the derived points of segs whose form is form and whose instant
// lies in [lo, hi), in observed.Before order.
func (st *obsStore) pointsOf(segs []obsSegment, form string, lo, hi time.Time) ([]observed.Point, error) {
	if err := st.derive(segs); err != nil {
		return nil, err
	}
	var out []observed.Point
	for _, s := range segs {
		r := st.rows[s.ref]
		if r == nil || r.form != form {
			continue
		}
		for i, p := range r.points {
			if !p.ts.Before(lo) && p.ts.Before(hi) {
				out = append(out, r.observedPoint(i))
			}
		}
	}
	sortPoints(out)
	return out, nil
}

// points is every derived point of the stream's form whose instant lies in
// [lo, hi), in observed.Before order.
func (st *obsStore) points(info *obsStreamInfo, form string, lo, hi time.Time) ([]observed.Point, error) {
	segs, err := st.segments(info, form, lo, hi)
	if err != nil {
		return nil, err
	}
	return st.pointsOf(segs, form, lo, hi)
}

func sortPoints(pts []observed.Point) {
	sort.Slice(pts, func(i, j int) bool { return observed.Before(pts[i], pts[j]) })
}

// obsSegmentPage is how many segments one step of a walk reads.
const obsSegmentPage = 16

// before is the stream's last point of the form before t in observed.Before
// order, available only where available is set; nil for none. It walks the
// segments that start before t, newest first, and stops once no older one
// can reach the best point found: a segment's points lie within maxExtentMs of
// its first.
func (st *obsStore) before(info *obsStreamInfo, form string, t time.Time, available bool) (*observed.Point, error) {
	var best *observed.Point
	afterTS, afterID := t, ""
	for {
		segs, err := st.scanSegments(`SELECT `+segmentColumns+` FROM observed_segments
			WHERE stream = $1 AND form = $2 AND (first_ts < $3 OR (first_ts = $3 AND record_id < $4))
			  AND ($5::boolean OR first_ts < $6)
			ORDER BY first_ts DESC, record_id DESC LIMIT $7`,
			info.id, form, afterTS, afterID, afterID != "", t, obsSegmentPage)
		if err != nil {
			return nil, err
		}
		for _, s := range segs {
			if best != nil && info.reach(s.first).Before(best.At) {
				return best, nil
			}
			reach := s.last
			if available {
				if s.available == 0 {
					continue
				}
				reach = s.lastAvailable
			}
			if reach.After(t) {
				reach = t
			}
			if best != nil && reach.Before(best.At) {
				continue
			}
			if err := st.charged(s); err != nil {
				return nil, err
			}
			if err := st.derive([]obsSegment{s}); err != nil {
				return nil, err
			}
			r := st.rows[s.ref]
			if r == nil {
				continue
			}
			for i, p := range r.points {
				if !p.ts.Before(t) || (available && !p.available()) {
					continue
				}
				q := r.observedPoint(i)
				if best == nil || observed.Before(*best, q) {
					best = &q
				}
			}
		}
		if len(segs) < obsSegmentPage {
			return best, nil
		}
		last := segs[len(segs)-1]
		afterTS, afterID = last.first, last.ref.ID
	}
}

// atOrAfter is the stream's first point of the form at or after t in
// observed.Before order, available only where available is set; nil for
// none. It walks the segments that may hold one, oldest first, and stops at
// the first segment starting after the best point found.
func (st *obsStore) atOrAfter(info *obsStreamInfo, form string, t time.Time, available bool) (*observed.Point, error) {
	var best *observed.Point
	afterTS, afterID := info.reachBack(t), ""
	first := true
	for {
		segs, err := st.scanSegments(`SELECT `+segmentColumns+` FROM observed_segments
			WHERE stream = $1 AND form = $2 AND (first_ts > $3 OR (first_ts = $3 AND (record_id > $4 OR $5::boolean)))
			  AND last_ts >= $6
			ORDER BY first_ts, record_id LIMIT $7`,
			info.id, form, afterTS, afterID, first, t, obsSegmentPage)
		if err != nil {
			return nil, err
		}
		first = false
		for _, s := range segs {
			if best != nil && s.first.After(best.At) {
				return best, nil
			}
			if available && (s.available == 0 || s.lastAvailable.Before(t)) {
				continue
			}
			if err := st.charged(s); err != nil {
				return nil, err
			}
			if err := st.derive([]obsSegment{s}); err != nil {
				return nil, err
			}
			r := st.rows[s.ref]
			if r == nil {
				continue
			}
			for i, p := range r.points {
				if p.ts.Before(t) || (available && !p.available()) {
					continue
				}
				q := r.observedPoint(i)
				if best == nil || observed.Before(q, *best) {
					best = &q
				}
			}
		}
		if len(segs) < obsSegmentPage {
			return best, nil
		}
		last := segs[len(segs)-1]
		afterTS, afterID = last.first, last.ref.ID
	}
}

// newest is the stream's newest available point of any form, the latest
// instant, then the greater record id, then the later point (the `last`
// reducer's order), with the record that holds it; nil for none. It walks the
// segments newest first and stops once no older one can reach the best.
func (st *obsStore) newest(info *obsStreamInfo) (*observedRows, int, error) {
	var best *obsSegment
	afterTS, afterID := time.Time{}, ""
	first := true
	for done := false; !done; {
		segs, err := st.scanSegments(`SELECT `+segmentColumns+` FROM observed_segments
			WHERE stream = $1 AND ($2::boolean OR first_ts < $3 OR (first_ts = $3 AND record_id < $4))
			ORDER BY first_ts DESC, record_id DESC LIMIT $5`,
			info.id, first, afterTS, afterID, obsSegmentPage)
		if err != nil {
			return nil, 0, err
		}
		first = false
		for i := range segs {
			s := segs[i]
			if best != nil && info.reach(s.first).Before(best.lastAvailable) {
				done = true
				break
			}
			if s.available == 0 {
				continue
			}
			if best == nil || s.lastAvailable.After(best.lastAvailable) ||
				(s.lastAvailable.Equal(best.lastAvailable) && s.ref.ID > best.ref.ID) {
				best = &s
			}
		}
		if len(segs) < obsSegmentPage {
			break
		}
		last := segs[len(segs)-1]
		afterTS, afterID = last.first, last.ref.ID
	}
	if best == nil {
		return nil, 0, nil
	}
	if err := st.derive([]obsSegment{*best}); err != nil {
		return nil, 0, err
	}
	r := st.rows[best.ref]
	if r == nil {
		return nil, 0, nil
	}
	at := -1
	for i, p := range r.points {
		if !p.available() {
			continue
		}
		if at < 0 || p.ts.After(r.points[at].ts) || (p.ts.Equal(r.points[at].ts) && p.ord > r.points[at].ord) {
			at = i
		}
	}
	if at < 0 {
		return nil, 0, nil
	}
	return r, at, nil
}

// tierSeconds is a tier's width as observed_rollups.tier stores it.
func tierSeconds(w time.Duration) int64 { return int64(w / time.Second) }

// allTiers is every tier width a metric may name, finest first.
func allTiers() []time.Duration {
	out := make([]time.Duration, 0, len(vocabulary.RollupTiers))
	for _, t := range vocabulary.RollupTiers {
		out = append(out, t.Width)
	}
	slices.Sort(out)
	return out
}
