package engine

// The aggregate read's reduction from raw points (aggregateread.go): held
// intervals built per stream from every transition, the source policy
// resolved over the range, then each point and interval placed in the
// buckets it lies in.

import (
	"sort"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// rawStream is one stream of a group as a raw read holds it.
type rawStream struct {
	s *aggStream
	// points is every point read in the windows, in time order.
	points []aggPoint
	// spans are a gauge's held intervals, built from every transition; each
	// span's point carries its group's key (rowGroups), and its stored unit
	// until placeRaw takes it.
	spans []observed.Span
	// prevOf is, for a counter, the last available point before each point,
	// by the point's index.
	prevOf map[int]*aggPoint
}

// evalRaw reduces a group's streams from raw points: held intervals built
// per stream from every transition, the source policy resolved over the
// range, then each bucket's members and clipped intervals.
func (r *aggRun) evalRaw(g *aggGroup, streams []*aggStream, form string) (aggCells, error) {
	list, err := r.rawStreams(streams, form)
	if err != nil {
		return nil, err
	}
	if r.p.policy == substrate.SourcesPriority && len(list) > 1 {
		if err := r.resolvePriority(list, form); err != nil {
			return nil, err
		}
	}
	out, err := r.placeRaw(list, form)
	if err != nil {
		return nil, err
	}
	if r.p.policy == substrate.SourcesSummaries && form == vocabulary.FormSummary {
		for _, codes := range out {
			for _, ev := range codes {
				if err := r.firstSummarySource(ev, streams); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, r.settleRaw(out, list, form)
}

// rawStreams holds each stream's points in the read's windows and, for a
// gauge, the intervals they hold, the value carried into each window first.
func (r *aggRun) rawStreams(streams []*aggStream, form string) ([]*rawStream, error) {
	var list []*rawStream
	for si, s := range streams {
		st := &rawStream{s: s, prevOf: map[int]*aggPoint{}}
		for _, seg := range r.series[s].segs {
			if form == vocabulary.FormGauge {
				obs := make([]observed.Point, 0, len(seg.points)+1)
				if seg.carry != nil {
					obs = append(obs, seg.carry.Point)
				}
				for _, ap := range seg.points {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					obs = append(obs, ap.Point)
				}
				held, err := observed.HeldContext(r.ctx, obs, seg.from, seg.to, r.p.maxGap)
				if err != nil {
					return nil, err
				}
				for _, iv := range held {
					if err := r.canceled(); err != nil {
						return nil, err
					}
					st.spans = append(st.spans, observed.Span{Start: iv.Start, End: iv.End, Point: obs[iv.Point]})
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
	return list, r.ctx.Err()
}

// resolvePriority applies the priority policy over the range, before any
// bucket: a gauge's held intervals by observed.Prioritize, a lower stream's
// point inside a higher one's hold dropped; another form's point dropped
// where it overlaps a higher stream's point.
func (r *aggRun) resolvePriority(list []*rawStream, form string) error {
	if form != vocabulary.FormGauge {
		var higher [][2]time.Time
		for _, st := range list {
			hc, err := r.cover(higher)
			if err != nil {
				return err
			}
			var kept []aggPoint
			for _, ap := range st.points {
				if err := r.canceled(); err != nil {
					return err
				}
				if !ap.Unavailable && hc.overlaps(ap.At, pointEnd(ap)) {
					continue
				}
				kept = append(kept, ap)
			}
			st.points = kept
			for _, ap := range kept {
				if err := r.canceled(); err != nil {
					return err
				}
				if !ap.Unavailable {
					higher = append(higher, [2]time.Time{ap.At, pointEnd(ap)})
				}
			}
		}
		return r.ctx.Err()
	}
	in := make([][]observed.Span, len(list))
	for i, st := range list {
		in[i] = st.spans
	}
	resolved, err := observed.Prioritize(r.ctx, in)
	if err != nil {
		return err
	}
	var higher [][2]time.Time
	for i, st := range list {
		// cover sorts higher in place, which its later appends do not mind.
		hc, err := r.cover(higher)
		if err != nil {
			return err
		}
		kept := st.points[:0]
		for _, ap := range st.points {
			if err := r.canceled(); err != nil {
				return err
			}
			if ap.Unavailable || !hc.holds(ap.At) {
				kept = append(kept, ap)
			}
		}
		st.points = kept
		for _, sp := range st.spans {
			if err := r.canceled(); err != nil {
				return err
			}
			higher = append(higher, [2]time.Time{sp.Start, sp.End})
		}
		st.spans = resolved[i]
	}
	return r.ctx.Err()
}

// placeRaw puts each stream's available points and held intervals into the
// cells of the buckets they lie in, with a counter's step from the point
// before. A held interval's value is converted here, the code filter and
// the priority policy having kept it; a point's in settleRaw, after the
// summaries policy.
func (r *aggRun) placeRaw(list []*rawStream, form string) (aggCells, error) {
	out := aggCells{}
	for si, st := range list {
		for i := range st.points {
			if err := r.canceled(); err != nil {
				return nil, err
			}
			ap := st.points[i]
			if ap.Unavailable || !r.admits(ap.Code) {
				continue
			}
			buckets, err := r.bucketsOf(ap.At, ap.off)
			if err != nil {
				return nil, err
			}
			for _, b := range buckets {
				ev := cell(out, b, r.codeKey(ap.Code), form, true)
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
			og := r.rowGroups[sp.Point.Group]
			if err := r.convertPoint(&sp.Point, og.delta, st.s); err != nil {
				return nil, err
			}
			off, _ := r.offsetAt(og, sp.Point.At)
			parts, err := r.spanParts(sp, off)
			if err != nil {
				return nil, err
			}
			for _, part := range parts {
				ev := cell(out, part.bucket, r.codeKey(sp.Point.Code), form, true)
				ev.spans = append(ev.spans, part.span)
				ev.sources[st.s.source] = true
				if b := part.bucket; b.local {
					b.ends.hold(part.span.Start, part.span.End, si, off)
					ev.ends.hold(part.span.Start, part.span.End, si, off)
				}
			}
		}
	}
	return out, nil
}

// settleRaw converts each cell's members into the read's unit, marks a
// recorded-local row's first and last point with them, orders them as every
// reader takes points and its spans by start, and counts the cell's points
// and coverage. It runs after the summaries policy, so a summary the policy
// sets aside decides no row's start or end, and walks the cells in bucket
// and code order, so the value a refusal names does not follow a map's
// order.
func (r *aggRun) settleRaw(out aggCells, list []*rawStream, form string) error {
	for _, b := range r.buckets {
		for _, code := range sortedKeys(out[b]) {
			ev := out[b][code]
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
				return err
			}
			if err := observed.SortStableFunc(r.ctx, ev.spans, func(a, b observed.Span) int { return a.Start.Compare(b.Start) }); err != nil {
				return err
			}
			ev.count = int64(len(ev.members))
			for i := range ev.members {
				if err := r.canceled(); err != nil {
					return err
				}
				m := &ev.members[i]
				if err := r.convertPoint(&m.Point, m.delta, list[m.stream].s); err != nil {
					return err
				}
				if b.local {
					b.ends.mark(m.Point, m.stream, m.off)
					ev.ends.mark(m.Point, m.stream, m.off)
				}
				if m.Bounded {
					ev.bounded++
				}
				if m.fallback {
					ev.offsetless++
				}
			}
			var err error
			switch form {
			case vocabulary.FormGauge:
				ev.coverage, err = observed.HeldMsContext(r.ctx, ev.spans)
			case vocabulary.FormCounter:
				ev.coverage = ev.steps.covered
			default:
				ev.coverage, err = r.memberCoverage(ev)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
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

// cover joins the intervals (joinSpans) under the read's context. It sorts
// spans in place, so a caller hands over a slice whose order it does not
// need, and is spared a copy as long as the cover.
func (r *aggRun) cover(spans [][2]time.Time) (cover, error) {
	return joinSpans(r.ctx, spans, r.canceled)
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
// the callers that hold a point do (aggBucket.mark). Each bucket it compares
// is a step of the read (canceled).
func (r *aggRun) bucketsOf(t time.Time, off int64) ([]*aggBucket, error) {
	mode := r.p.bucket.mode
	if mode != bucketRecords && (t.Before(r.p.readFrom) || !t.Before(r.p.to)) {
		return nil, nil
	}
	switch mode {
	case bucketLocal:
		b := r.localBucket(t, wallOf(t, off))
		if b == nil {
			return nil, nil
		}
		return []*aggBucket{b}, nil
	case bucketRecords:
		return r.windowsMeeting(t, t.Add(time.Nanosecond))
	}
	i, err := firstEndingAfter(r.buckets, t, r.canceled)
	if err != nil || i == len(r.buckets) || t.Before(r.buckets[i].start) {
		return nil, err
	}
	return []*aggBucket{r.buckets[i]}, nil
}

// localBucket is the recorded-local bucket an instant t at wall-clock time
// wall lands in: a bucket of the range from the range's start, a lookback
// bucket before it, made where the enumeration did not reach. A value
// before the range on a day the range answers lands in none: the rows a
// rolling read answers are the rows the read answers without it.
func (r *aggRun) localBucket(t, wall time.Time) *aggBucket {
	lookback := t.Before(r.p.from)
	if lookback && !wall.Before(r.firstLocal()) {
		return nil
	}
	var b *aggBucket
	r.buckets, b = localBucketAt(r.buckets, wall, r.p.bucket, lookback)
	return b
}

// firstLocal is the wall-clock start of the recorded-local bucket the range
// starts in, at the range's own offset.
func (r *aggRun) firstLocal() time.Time {
	return calStart(wallOf(r.p.from, offsetMinutes(r.p.from)), r.p.bucket)
}

type spanPart struct {
	bucket *aggBucket
	span   observed.Span
}

// spanParts cuts a held interval at the buckets' boundaries: a UTC bucket's, or
// for a recorded-local bucket the boundaries at the holding point's offset
// and the range's start, where the lookback ends.
func (r *aggRun) spanParts(sp observed.Span, off int64) ([]spanPart, error) {
	var out []spanPart
	switch r.p.bucket.mode {
	case bucketLocal:
		shift := time.Duration(off) * time.Minute
		start := sp.Start
		for start.Before(sp.End) {
			wall := start.UTC().Add(shift)
			end := calNext(calStart(wall, r.p.bucket), r.p.bucket).Add(-shift)
			if start.Before(r.p.from) && end.After(r.p.from) {
				end = r.p.from
			}
			if end.After(sp.End) {
				end = sp.End
			}
			if b := r.localBucket(start, wall); b != nil {
				part := sp
				part.Start, part.End = start, end
				out = append(out, spanPart{bucket: b, span: part})
			}
			start = end
		}
		return out, nil
	}
	buckets, err := r.bucketsMeeting(sp.Start, sp.End)
	if err != nil {
		return nil, err
	}
	for _, b := range buckets {
		for _, c := range observed.Clip([]observed.Span{sp}, b.start, b.end) {
			out = append(out, spanPart{bucket: b, span: c})
		}
	}
	return out, nil
}

// bucketsMeeting lists the UTC buckets [from, to) meets, each bucket it
// compares or lists a step of the read.
func (r *aggRun) bucketsMeeting(from, to time.Time) ([]*aggBucket, error) {
	if r.p.bucket.mode == bucketRecords {
		return r.windowsMeeting(from, to)
	}
	i, err := firstEndingAfter(r.buckets, from, r.canceled)
	if err != nil {
		return nil, err
	}
	var out []*aggBucket
	for ; i < len(r.buckets) && r.buckets[i].start.Before(to); i++ {
		if err := r.canceled(); err != nil {
			return nil, err
		}
		out = append(out, r.buckets[i])
	}
	return out, nil
}

// windowsMeeting lists the records-mode windows [from, to) meets, in order.
// The windows are sorted by start but overlap and differ in length, so
// neither their starts nor their ends find them by binary search, and one
// long window keeps every later one's running latest end high. reach is
// therefore a tree over the windows in start order, each node holding the
// latest end below it: the walk descends only into nodes whose first window
// starts before to and whose latest end passes from, so a point takes steps
// (canceled) in the windows it meets times the tree's depth, not in the
// windows before it.
func (r *aggRun) windowsMeeting(from, to time.Time) ([]*aggBucket, error) {
	if r.reach == nil {
		size := 1
		for size < len(r.buckets) {
			size *= 2
		}
		r.reach = make([]time.Time, 2*size)
		for i, b := range r.buckets {
			if err := r.canceled(); err != nil {
				r.reach = nil
				return nil, err
			}
			r.reach[size+i] = b.end
		}
		for i := size - 1; i > 0; i-- {
			r.reach[i] = r.reach[2*i]
			if r.reach[2*i+1].After(r.reach[i]) {
				r.reach[i] = r.reach[2*i+1]
			}
		}
	}
	var out []*aggBucket
	var walk func(node, first, width int) error
	walk = func(node, first, width int) error {
		if first >= len(r.buckets) || !r.buckets[first].start.Before(to) || !r.reach[node].After(from) {
			return nil
		}
		if err := r.canceled(); err != nil {
			return err
		}
		if width == 1 {
			out = append(out, r.buckets[first])
			return nil
		}
		half := width / 2
		if err := walk(2*node, first, half); err != nil {
			return err
		}
		return walk(2*node+1, first+half, half)
	}
	return out, walk(1, 0, len(r.reach)/2)
}
