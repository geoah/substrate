// Package observed is the executable specification of how the observed index
// reads points (decisions 0157, 0158 and 0160): which interval a gauge's value
// holds, what each reducer answers over a window, how a counter is differenced
// under its contract, what one 15-minute rollup row holds, how two rows merge,
// and when a row may answer for raw points. The engine computes
// `observed_rollups` with Bucket, and the aggregate read is held to the
// window reducers here: a rollup answer and a raw answer over the same points
// must agree.
//
// It reads points and nothing else: no database, no clock, no registry. A
// point's number is a float64 in the point's own unit; where float64 would
// lose an increment (a counter's steps, a row's first and last value) the
// stored digits in Exact are read instead.
package observed

import (
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// BucketWidth is the width of one rollup row. Fifteen minutes divides every
// UTC offset in use, so a civil day at a point's own offset is a run of whole
// buckets aligned on UTC.
const BucketWidth = 15 * time.Minute

// MinResultantLength is the shortest mean resultant length (the length of the
// mean of the unit vectors, between 0 and 1) a circular mean is answered at.
// Below it the angles cancel (north and south equally), and atan2 would
// report whatever rounding left as a direction.
const MinResultantLength = 1e-9

// Point is one point of a stream.
type Point struct {
	// At is the point's instant; End closes an amount's span [At, End) and
	// equals At otherwise.
	At, End time.Time
	// Value is the number in the point's unit; HasValue says there is one. A
	// coded gauge has none; a coded amount has both.
	Value    float64
	HasValue bool
	// Exact is the number's stored digits.
	Exact string
	// Code is the categorical value, or "".
	Code string
	// Unavailable is a null in a trace: no value from this point on. It ends
	// the hold of the point before it and counts in nothing.
	Unavailable bool
	// Bounded marks a value written with a comparator (a lab's `<0.5`).
	Bounded bool
	// Hold is how long a gauge's value stands without a newer point; nil
	// means until the next point.
	Hold *time.Duration
	// Reset, ResetAt and WrapAt are a counter's contract: Reset is monotonic,
	// signed or wrap ("" reads as monotonic), ResetAt the start of the
	// current accumulation (zero when the point names none), WrapAt the
	// digits a wrap register returns to zero at.
	Reset   string
	ResetAt time.Time
	WrapAt  string
	// Unit is the point's unit. A counter's step runs only between points of
	// one unit; anything else in this package ignores it.
	Unit string
	// Circular marks an angle averaged on the circle, and Angle is the value
	// in radians.
	Circular bool
	Angle    float64
	// Group names the rollup row the point belongs to. The index groups by
	// code, unit, delta and offset; this package treats it as opaque.
	Group string
}

// Interval is the span during which one point's value holds.
type Interval struct {
	// Point is the index of the point in the slice Held read.
	Point      int
	Start, End time.Time
}

// Held builds the held intervals of one stream inside [from, to). points is
// every point of the stream in time order (ties in the order given),
// unavailable points included, and may start before from: the point current
// at from is how a window's start is carried in, read from the stored points
// and never copied into a later trace. Each available point holds from its
// instant until the earliest of the next point of the stream (whatever its
// code, an unavailable one included), its own hold, maxGap after it (zero
// means no maxGap) and to. Intervals are built from every transition before
// anything filters them, so a door's `closed` ends the `open` before it.
func Held(points []Point, from, to time.Time, maxGap time.Duration) []Interval {
	var out []Interval
	for i, p := range points {
		if p.Unavailable || !p.At.Before(to) {
			continue
		}
		end := to
		if i+1 < len(points) && points[i+1].At.Before(end) {
			end = points[i+1].At
		}
		if p.Hold != nil {
			if e := p.At.Add(*p.Hold); e.Before(end) {
				end = e
			}
		}
		if maxGap > 0 {
			if e := p.At.Add(maxGap); e.Before(end) {
				end = e
			}
		}
		start := p.At
		if start.Before(from) {
			start = from
		}
		if end.After(start) {
			out = append(out, Interval{Point: i, Start: start, End: end})
		}
	}
	return out
}

// Window is a read's time range [From, To) and its maxGap, zero for none.
type Window struct {
	From, To time.Time
	MaxGap   time.Duration
}

func (w Window) holds(p Point) bool { return !p.At.Before(w.From) && p.At.Before(w.To) }

// inWindow lists the available points whose instant is inside the window. An
// amount belongs whole to the window its span starts in.
func inWindow(points []Point, w Window) []Point {
	var out []Point
	for _, p := range points {
		if !p.Unavailable && w.holds(p) {
			out = append(out, p)
		}
	}
	return out
}

func numbers(points []Point, w Window) []float64 {
	var out []float64
	for _, p := range inWindow(points, w) {
		if p.HasValue {
			out = append(out, p.Value)
		}
	}
	return out
}

// Count is the number of available points in the window.
func Count(points []Point, w Window) int64 { return int64(len(inWindow(points, w))) }

// Sum adds the numbers in the window.
func Sum(points []Point, w Window) (float64, bool) {
	vs := numbers(points, w)
	var s float64
	for _, v := range vs {
		s += v
	}
	return s, len(vs) > 0
}

// Min is the smallest number in the window.
func Min(points []Point, w Window) (float64, bool) {
	vs := numbers(points, w)
	if len(vs) == 0 {
		return 0, false
	}
	m := vs[0]
	for _, v := range vs[1:] {
		m = math.Min(m, v)
	}
	return m, true
}

// Max is the largest number in the window.
func Max(points []Point, w Window) (float64, bool) {
	vs := numbers(points, w)
	if len(vs) == 0 {
		return 0, false
	}
	m := vs[0]
	for _, v := range vs[1:] {
		m = math.Max(m, v)
	}
	return m, true
}

// First is the earliest available point in the window, a code or a number.
func First(points []Point, w Window) (Point, bool) {
	in := inWindow(points, w)
	if len(in) == 0 {
		return Point{}, false
	}
	return in[0], true
}

// Last is the latest available point in the window; of two at one instant,
// the later in order.
func Last(points []Point, w Window) (Point, bool) {
	in := inWindow(points, w)
	if len(in) == 0 {
		return Point{}, false
	}
	return in[len(in)-1], true
}

// Avg is the mean of the numbers in the window, each point weighing one.
func Avg(points []Point, w Window) (float64, bool) {
	var m moments
	for _, v := range numbers(points, w) {
		m.add(v)
	}
	return m.mean, m.n > 0
}

// StdDev is the population standard deviation of the numbers in the window,
// the square root of m2 over n.
func StdDev(points []Point, w Window) (float64, bool) {
	var m moments
	for _, v := range numbers(points, w) {
		m.add(v)
	}
	if m.n == 0 {
		return 0, false
	}
	return math.Sqrt(m.m2 / float64(m.n)), true
}

// TWA is the time-weighted mean: each number weighed by the time it holds
// inside the window. A window in which nothing holds has no answer.
func TWA(points []Point, w Window) (float64, bool) {
	return TWAOf(Spans(points, w.From, w.To, w.MaxGap))
}

// TimeInState is the time each code holds inside the window.
func TimeInState(points []Point, w Window) map[string]time.Duration {
	return TimeInStateOf(Spans(points, w.From, w.To, w.MaxGap))
}

// DurationIn is the time a number in [lo, hi) holds inside the window: the
// time above 140 mg/dL, the time in a heart rate zone.
func DurationIn(points []Point, w Window, lo, hi float64) time.Duration {
	return DurationInOf(Spans(points, w.From, w.To, w.MaxGap), lo, hi)
}

// Percentile is the q-quantile (0 to 1) of the numbers in the window,
// interpolated linearly between the two closest ranks, as Postgres's
// percentile_cont answers it.
func Percentile(points []Point, w Window, q float64) (float64, bool) {
	vs := numbers(points, w)
	if len(vs) == 0 || q < 0 || q > 1 {
		return 0, false
	}
	sort.Float64s(vs)
	pos := q * float64(len(vs)-1)
	lo := int(math.Floor(pos))
	if lo >= len(vs)-1 {
		return vs[len(vs)-1], true
	}
	frac := pos - float64(lo)
	return vs[lo] + frac*(vs[lo+1]-vs[lo]), true
}

// CircularMean is the mean direction of the circular points in the window,
// in degrees in [0, 360), from the sums of their sines and cosines. It has no
// answer when the mean resultant length is below MinResultantLength.
func CircularMean(points []Point, w Window) (float64, bool) {
	var sin, cos float64
	var n int64
	for _, p := range inWindow(points, w) {
		if p.Circular {
			sin += math.Sin(p.Angle)
			cos += math.Cos(p.Angle)
			n++
		}
	}
	return circularMean(sin, cos, n)
}

func circularMean(sin, cos float64, n int64) (float64, bool) {
	if n == 0 || math.Hypot(sin, cos)/float64(n) < MinResultantLength {
		return 0, false
	}
	deg := math.Atan2(sin, cos) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}
	return deg, true
}

// Increase is a counter's change over the window under its contract: the sum
// of the steps whose later point is inside the window, each from the
// stream's previous available point, wherever that is. The step from the
// point current at the window's start into the window's first point is the
// window's, so consecutive windows add up and no step is counted twice or
// lost. Covered is the time those steps span, from the earlier point of the
// first to the later point of the last; resets counts the steps that started
// over (Step). Unavailable points are skipped: a counter is a running total,
// and an outage between two readings still accrued. ok is false when the
// window holds no step.
func Increase(points []Point, w Window) (increase *big.Rat, resets int64, covered time.Duration, ok bool) {
	increase = new(big.Rat)
	var prev *Point
	for i := range points {
		p := &points[i]
		if p.Unavailable {
			continue
		}
		if prev != nil && w.holds(*p) {
			d, reset := Step(*prev, *p)
			increase.Add(increase, d)
			if reset {
				resets++
			}
			covered += p.At.Sub(prev.At)
			ok = true
		}
		prev = p
	}
	return increase, resets, covered, ok
}

// Rate is Increase over the time its steps cover, per second, so a `kW.h`
// counter reads in kW.h per second. Nothing is extrapolated to the window's
// edges.
func Rate(points []Point, w Window) (float64, bool) {
	inc, _, covered, ok := Increase(points, w)
	if !ok || covered <= 0 {
		return 0, false
	}
	f, _ := inc.Float64()
	return f / covered.Seconds(), true
}

// Step is the change from one counter reading to the next under the later
// reading's contract, exact, and whether it started over:
//
//   - two readings in different units, or naming two different resetAt, start
//     over: the step is the later reading, which counted from zero;
//   - monotonic (and a counter with no resetRule, which the index reads as
//     monotonic): a drop is a reset to zero, so the step is the later
//     reading;
//   - signed: the step is the difference, which may be negative;
//   - wrap: a drop wrapped at WrapAt, so the step is WrapAt minus the earlier
//     reading plus the later one. More than one wrap between two readings
//     cannot be seen and reads as one.
func Step(prev, next Point) (*big.Rat, bool) {
	a, b := exact(prev), exact(next)
	if prev.Unit != next.Unit {
		return b, true
	}
	if !prev.ResetAt.IsZero() && !next.ResetAt.IsZero() && !prev.ResetAt.Equal(next.ResetAt) {
		return b, true
	}
	d := new(big.Rat).Sub(b, a)
	switch next.Reset {
	case vocabulary.ResetSigned:
		return d, false
	case vocabulary.ResetWrap:
		if d.Sign() >= 0 {
			return d, false
		}
		wrap, ok := new(big.Rat).SetString(next.WrapAt)
		if !ok {
			return b, true
		}
		return d.Add(d, wrap), true
	}
	if d.Sign() < 0 {
		return b, true
	}
	return d, false
}

// exact is a point's number as an exact rational: its digits, or the float
// where it carries none.
func exact(p Point) *big.Rat {
	if r, ok := new(big.Rat).SetString(p.Exact); ok {
		return r
	}
	r := new(big.Rat)
	if !math.IsNaN(p.Value) && !math.IsInf(p.Value, 0) {
		r.SetFloat64(p.Value)
	}
	return r
}

// moments is Welford's running mean and m2, in point order, so the same
// points always give the same bits.
type moments struct {
	n        int64
	mean, m2 float64
}

func (m *moments) add(v float64) {
	m.n++
	d := v - m.mean
	m.mean += d / float64(m.n)
	m.m2 += d * (v - m.mean)
}
