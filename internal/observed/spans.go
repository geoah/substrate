package observed

import (
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
func Prioritize(streams [][]Span) [][]Span {
	out := make([][]Span, len(streams))
	var cover [][2]time.Time
	for i, spans := range streams {
		for _, s := range spans {
			out[i] = append(out[i], subtract(s, cover)...)
		}
		for _, s := range spans {
			cover = append(cover, [2]time.Time{s.Start, s.End})
		}
		cover = mergeCover(cover)
	}
	return out
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

// subtract is the parts of s outside a sorted, merged cover.
func subtract(s Span, cover [][2]time.Time) []Span {
	parts := []Span{s}
	for _, c := range cover {
		var next []Span
		for _, p := range parts {
			if !c[0].Before(p.End) || !c[1].After(p.Start) {
				next = append(next, p)
				continue
			}
			if c[0].After(p.Start) {
				next = append(next, Span{Start: p.Start, End: c[0], Point: p.Point})
			}
			if c[1].Before(p.End) {
				next = append(next, Span{Start: c[1], End: p.End, Point: p.Point})
			}
		}
		parts = next
	}
	return parts
}

// mergeCover sorts intervals and joins the ones that touch.
func mergeCover(cover [][2]time.Time) [][2]time.Time {
	sort.Slice(cover, func(i, j int) bool { return cover[i][0].Before(cover[j][0]) })
	var out [][2]time.Time
	for _, c := range cover {
		if n := len(out); n > 0 && !c[0].After(out[n-1][1]) {
			if c[1].After(out[n-1][1]) {
				out[n-1][1] = c[1]
			}
			continue
		}
		out = append(out, c)
	}
	return out
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
