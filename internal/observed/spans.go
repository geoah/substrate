package observed

import (
	"context"
	"sort"
	"time"
)

// Span is one held interval with the point that holds it. Held answers the
// intervals of one stream; a read that resolves several streams by priority
// (Prioritize) or splits a range into buckets (Clip) works on spans, and the
// held reducers below answer over them, so TWA, TimeInState and DurationIn
// over one stream's points and the aggregate read over resolved spans are one
// implementation.
type Span struct {
	Start, End time.Time
	Point      Point
}

// Spans is Held as spans: each available point's interval inside [from, to),
// under its own hold, the next point and maxGap.
func Spans(points []Point, from, to time.Time, maxGap time.Duration) []Span {
	ivs := Held(points, from, to, maxGap)
	out := make([]Span, len(ivs))
	for i, iv := range ivs {
		out[i] = Span{Start: iv.Start, End: iv.End, Point: points[iv.Point]}
	}
	return out
}

// Clip is the part of each span inside [from, to), in order.
func Clip(spans []Span, from, to time.Time) []Span {
	var out []Span
	for _, s := range spans {
		start, end := s.Start, s.End
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if end.After(start) {
			out = append(out, Span{Start: start, End: end, Point: s.Point})
		}
	}
	return out
}

// Prioritize resolves streams by priority, per stored interval: streams[0]
// wins wherever it holds a value, streams[1] fills what streams[0] does not
// cover, and so on. Each stream's spans keep only the time no earlier stream
// holds, so a lower source never doubles a higher one and a gap in the higher
// source is filled by the next. The answer is parallel to streams.
//
// The work is a sort and a merge per stream and a binary search per span:
// each span visits only the cover intervals it overlaps, so two fragmented
// sources of n spans each resolve in O(n log n), never n squared. It answers
// ctx's error, checked before each stream and every thousand spans, so a
// caller that stops waiting stops the work.
func Prioritize(ctx context.Context, streams [][]Span) ([][]Span, error) {
	out := make([][]Span, len(streams))
	var cover [][2]time.Time
	for i, spans := range streams {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = make([]Span, 0, len(spans))
		for j, s := range spans {
			if j&(1<<10-1) == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			out[i] = subtract(out[i], s, cover)
		}
		mine := make([][2]time.Time, 0, len(spans))
		for _, s := range spans {
			mine = append(mine, [2]time.Time{s.Start, s.End})
		}
		cover = mergeSorted(cover, mergeCover(mine))
	}
	return out, nil
}

// Covered reports whether t lies inside one of the spans.
func Covered(spans []Span, t time.Time) bool {
	for _, s := range spans {
		if !t.Before(s.Start) && t.Before(s.End) {
			return true
		}
	}
	return false
}

// subtract appends to dst the parts of s outside a sorted, merged cover: a
// binary search to the first interval that ends after s starts, then a walk
// over the intervals that start before s ends, and no further.
func subtract(dst []Span, s Span, cover [][2]time.Time) []Span {
	j := sort.Search(len(cover), func(j int) bool { return cover[j][1].After(s.Start) })
	if !s.End.After(s.Start) {
		if j < len(cover) && cover[j][0].Before(s.Start) {
			return dst
		}
		return append(dst, s)
	}
	start := s.Start
	for ; j < len(cover) && cover[j][0].Before(s.End); j++ {
		if cover[j][0].After(start) {
			dst = append(dst, Span{Start: start, End: cover[j][0], Point: s.Point})
		}
		if cover[j][1].After(start) {
			start = cover[j][1]
		}
	}
	if s.End.After(start) {
		dst = append(dst, Span{Start: start, End: s.End, Point: s.Point})
	}
	return dst
}

// mergeCover sorts intervals and joins the ones that touch.
func mergeCover(cover [][2]time.Time) [][2]time.Time {
	sort.Slice(cover, func(i, j int) bool { return cover[i][0].Before(cover[j][0]) })
	var out [][2]time.Time
	for _, c := range cover {
		out = appendJoined(out, c)
	}
	return out
}

// mergeSorted joins two sorted, merged covers into one, in one pass.
func mergeSorted(a, b [][2]time.Time) [][2]time.Time {
	out := make([][2]time.Time, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if j == len(b) || (i < len(a) && a[i][0].Before(b[j][0])) {
			out = appendJoined(out, a[i])
			i++
		} else {
			out = appendJoined(out, b[j])
			j++
		}
	}
	return out
}

// appendJoined appends c to a sorted, merged cover, joining it to the last
// interval where the two touch.
func appendJoined(out [][2]time.Time, c [2]time.Time) [][2]time.Time {
	if n := len(out); n > 0 && !c[0].After(out[n-1][1]) {
		if c[1].After(out[n-1][1]) {
			out[n-1][1] = c[1]
		}
		return out
	}
	return append(out, c)
}

// Extent is one piece of an amount's coverage a reader joins: a rollup row's
// union of spans, Ms long and lying inside [Start, End) (its First.At and
// SpanEnd), or one raw point's span, Exact, which covers all of [Start, End).
type Extent struct {
	Start, End time.Time
	Ms         int64
	Exact      bool
}

// UnionMs is the length of the union of the extents, the coverage a reader
// reports for an amount, and false where it cannot be known: where a row's
// extent overlaps another extent, because a row keeps its union's length
// and not its spans, so how much of it the other covers is unknown. Exact
// extents that overlap join as raw spans do. The reader then takes the union
// from the raw spans.
func UnionMs(extents []Extent) (int64, bool) {
	sorted := append([]Extent(nil), extents...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var total int64
	for i := 0; i < len(sorted); {
		run := sorted[i]
		j := i + 1
		for ; j < len(sorted); j++ {
			next := sorted[j]
			exact := run.Exact && next.Exact
			if next.Start.After(run.End) || (next.Start.Equal(run.End) && !exact) {
				break
			}
			if !exact {
				return 0, false
			}
			if next.End.After(run.End) {
				run.End = next.End
			}
		}
		if j > i+1 {
			run.Ms = run.End.Sub(run.Start).Milliseconds()
		}
		total += run.Ms
		i = j
	}
	return total, true
}

// HeldMs is the time the spans hold, in milliseconds.
func HeldMs(spans []Span) int64 {
	var ms int64
	for _, s := range spans {
		ms += s.End.Sub(s.Start).Milliseconds()
	}
	return ms
}

// TWAOf is the time-weighted mean over spans: each number weighed by the time
// its span holds. Spans in which nothing holds have no answer.
func TWAOf(spans []Span) (float64, bool) {
	var sum float64
	var held int64
	for _, s := range spans {
		if !s.Point.HasValue {
			continue
		}
		d := s.End.Sub(s.Start).Milliseconds()
		sum += s.Point.Value * float64(d)
		held += d
	}
	if held == 0 {
		return 0, false
	}
	return sum / float64(held), true
}

// TimeInStateOf is the time each code holds over spans.
func TimeInStateOf(spans []Span) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, s := range spans {
		if c := s.Point.Code; c != "" {
			out[c] += s.End.Sub(s.Start)
		}
	}
	return out
}

// DurationInOf is the time a number in [lo, hi) holds over spans.
func DurationInOf(spans []Span, lo, hi float64) time.Duration {
	var d time.Duration
	for _, s := range spans {
		if s.Point.HasValue && s.Point.Value >= lo && s.Point.Value < hi {
			d += s.End.Sub(s.Start)
		}
	}
	return d
}
