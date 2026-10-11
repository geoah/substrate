package observed

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// t0 is 10:00 UTC; every instant below is minutes after it.
var t0 = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

func at(min float64) time.Time { return t0.Add(time.Duration(min * float64(time.Minute))) }

func dur(min float64) time.Duration { return time.Duration(min * float64(time.Minute)) }

func gauge(min, v float64) Point {
	return Point{At: at(min), End: at(min), Value: v, HasValue: true, Exact: big.NewRat(int64(v*1000), 1000).FloatString(3)}
}

func coded(min float64, code string) Point {
	return Point{At: at(min), End: at(min), Code: code, Group: code}
}

func null(min float64) Point { return Point{At: at(min), End: at(min), Unavailable: true} }

func held(p Point, hold time.Duration) Point {
	p.Hold = &hold
	return p
}

func reading(min float64, digits string) Point {
	r, _ := new(big.Rat).SetString(digits)
	f, _ := r.Float64()
	return Point{At: at(min), End: at(min), Value: f, HasValue: true, Exact: digits, Unit: "kW.h"}
}

func window(from, to float64) Window { return Window{From: at(from), To: at(to)} }

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

// Finding 3: intervals are built from every transition first and filtered
// by code after. Filtering `closed` out first lets each `open` hold until the
// next `open`, which is the overcount the rule prevents.
func TestTimeInStateBuildsIntervalsBeforeFiltering(t *testing.T) {
	for _, tc := range []struct {
		name         string
		points       []Point
		open, closed time.Duration
		overcount    time.Duration // open, from the open points alone
	}{
		{
			name:   "closed before open",
			points: []Point{coded(0, "closed"), coded(20, "open"), coded(25, "closed")},
			open:   dur(5), closed: dur(20) + dur(35),
			overcount: dur(40),
		},
		{
			name:   "open, closed, open",
			points: []Point{coded(0, "open"), coded(10, "closed"), coded(30, "open")},
			open:   dur(10) + dur(30), closed: dur(20),
			overcount: dur(60),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := window(0, 60)
			got := TimeInState(tc.points, w)
			if got["open"] != tc.open || got["closed"] != tc.closed {
				t.Errorf("timeInState = open %v closed %v, want %v and %v", got["open"], got["closed"], tc.open, tc.closed)
			}
			var opens []Point
			for _, p := range tc.points {
				if p.Code == "open" {
					opens = append(opens, p)
				}
			}
			if wrong := TimeInState(opens, w)["open"]; wrong != tc.overcount {
				t.Errorf("filtering first gives %v, want the overcount %v", wrong, tc.overcount)
			}
		})
	}
}

// Decision 0158: a null ends the hold of the point before it and counts in
// nothing.
func TestANullInATraceEndsTheHold(t *testing.T) {
	points := []Point{gauge(0, 20), null(10), gauge(30, 22)}
	w := window(0, 60)
	// 20 holds 10 minutes, then nothing until 22, which holds 30.
	if got, _ := TWA(points, w); !near(got, (20*10+22*30)/40.0) {
		t.Errorf("twa = %v, want %v", got, (20*10+22*30)/40.0)
	}
	if got := Count(points, w); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
	if got, _ := Sum(points, w); got != 42 {
		t.Errorf("sum = %v, want 42", got)
	}
	ivs := Held(points, w.From, w.To, 0)
	if len(ivs) != 2 || ivs[0].End != at(10) || ivs[1].Start != at(30) || ivs[1].End != at(60) {
		t.Errorf("intervals = %+v", ivs)
	}
}

// A sensor silent for three hours: with no hold and no maxGap the old value
// holds the whole silence; a maxGap or a hold ends it.
func TestASilentSensorHoldsUntilMaxGapOrItsHold(t *testing.T) {
	points := []Point{gauge(0, 20), gauge(180, 22)}
	w := window(0, 240)
	if got, _ := TWA(points, w); !near(got, (20*180+22*60)/240.0) {
		t.Errorf("twa with no maxGap = %v, want 20.5", got)
	}
	w.MaxGap = time.Hour
	if got, _ := TWA(points, w); !near(got, 21) {
		t.Errorf("twa with maxGap PT1H = %v, want 21", got)
	}
	w.MaxGap = 0
	tenMinutes := []Point{held(gauge(0, 20), dur(10)), held(gauge(180, 22), dur(10))}
	if got, _ := TWA(tenMinutes, w); !near(got, 21) {
		t.Errorf("twa with hold PT10M = %v, want 21", got)
	}
	if got := DurationIn(points, Window{From: w.From, To: w.To, MaxGap: time.Hour}, 21, 30); got != time.Hour {
		t.Errorf("durationIn [21, 30) = %v, want 1h", got)
	}
}

// Finding 4: the point current at a window's start is read, never copied;
// passing it in carries its value into the window up to the first point.
func TestTheStartPointIsCarriedIn(t *testing.T) {
	points := []Point{gauge(-10, 18), gauge(5, 20)}
	w := window(0, 15)
	if got, _ := TWA(points, w); !near(got, (18*5+20*10)/15.0) {
		t.Errorf("twa = %v, want %v", got, (18*5+20*10)/15.0)
	}
	if got := Count(points, w); got != 1 {
		t.Errorf("count = %d: the carried point is not the window's", got)
	}
	w.MaxGap = dur(12)
	// The carried value expires 12 minutes after it was read, at 10:02.
	if got, _ := TWA(points, w); !near(got, (18*2+20*10)/12.0) {
		t.Errorf("twa with maxGap = %v, want %v", got, (18*2+20*10)/12.0)
	}
}

func TestMaxGapEndsAHoldInsideAGap(t *testing.T) {
	points := []Point{gauge(0, 20), gauge(50, 30)}
	ivs := Held(points, at(0), at(60), dur(30))
	if len(ivs) != 2 || ivs[0].End != at(30) || ivs[1].End != at(60) {
		t.Fatalf("intervals = %+v, want 20 to end at 10:30", ivs)
	}
	// A hold of zero holds nothing: a doorbell press counts and holds no time.
	press := []Point{held(coded(5, "pressed"), 0)}
	if got := TimeInState(press, window(0, 15)); len(got) != 0 {
		t.Errorf("a press held %v", got)
	}
	if got := Count(press, window(0, 15)); got != 1 {
		t.Errorf("a press counts %d, want 1", got)
	}
}

func TestPointReducers(t *testing.T) {
	var points []Point
	for i, v := range []float64{2, 4, 4, 4, 5, 5, 7, 9} {
		points = append(points, gauge(float64(i), v))
	}
	w := window(0, 15)
	for _, tc := range []struct {
		name string
		got  func() (float64, bool)
		want float64
	}{
		{"sum", func() (float64, bool) { return Sum(points, w) }, 40},
		{"min", func() (float64, bool) { return Min(points, w) }, 2},
		{"max", func() (float64, bool) { return Max(points, w) }, 9},
		{"avg", func() (float64, bool) { return Avg(points, w) }, 5},
		{"stddev", func() (float64, bool) { return StdDev(points, w) }, 2},
		{"percentile 0.5", func() (float64, bool) { return Percentile(points, w, 0.5) }, 4.5},
		{"percentile 0.25", func() (float64, bool) { return Percentile(points, w, 0.25) }, 4},
		{"percentile 0.1", func() (float64, bool) { return Percentile(points, w, 0.1) }, 3.4},
		{"percentile 1", func() (float64, bool) { return Percentile(points, w, 1) }, 9},
	} {
		got, ok := tc.got()
		if !ok || !near(got, tc.want) {
			t.Errorf("%s = %v (%v), want %v", tc.name, got, ok, tc.want)
		}
	}
	if f, _ := First(points, w); f.Value != 2 {
		t.Errorf("first = %v", f.Value)
	}
	if l, _ := Last(points, w); l.Value != 9 {
		t.Errorf("last = %v", l.Value)
	}
	if _, ok := Avg(nil, w); ok {
		t.Error("avg of nothing answered")
	}
}

func degrees(min, deg float64) Point {
	p := gauge(min, deg)
	p.Circular, p.Angle = true, deg*math.Pi/180
	return p
}

// A wind direction crossing north: 350 and 10 average to north, never to
// the 180 a plain mean gives; opposite directions have no mean.
func TestCircularMeanCrossesNorth(t *testing.T) {
	w := window(0, 15)
	points := []Point{degrees(0, 350), degrees(5, 10)}
	got, ok := CircularMean(points, w)
	if !ok || !(near(got, 0) || near(got, 360)) {
		t.Errorf("circular mean = %v (%v), want 0", got, ok)
	}
	if plain, _ := Avg(points, w); plain != 180 {
		t.Errorf("plain mean = %v, the answer the circular mean replaces", plain)
	}
	if got, ok := CircularMean([]Point{degrees(0, 90), degrees(1, 100)}, w); !ok || !near(got, 95) {
		t.Errorf("circular mean of 90 and 100 = %v", got)
	}
	if _, ok := CircularMean([]Point{degrees(0, 90), degrees(5, 270)}, w); ok {
		t.Error("east and west have a mean direction")
	}
}

func counter(points []Point, rule, wrapAt string) []Point {
	for i := range points {
		points[i].Reset, points[i].WrapAt = rule, wrapAt
	}
	return points
}

// counterCase holds a counter's readings to an increase, a reset count and
// a covered time, from the raw points over the window and from the rollup
// rows of every bucket inside it alike, so a step across a bucket boundary is
// counted once.
func counterCase(t *testing.T, points []Point, from, to float64, increase string, resets int64, covered time.Duration) {
	t.Helper()
	w := window(from, to)
	inc, n, cov, ok := Increase(points, w)
	if !ok || inc.RatString() != increase || n != resets || cov != covered {
		t.Errorf("raw: increase %s resets %d covered %v (%v), want %s, %d, %v", inc.RatString(), n, cov, ok, increase, resets, covered)
	}
	sum, sumResets, sumCovered := new(big.Rat), int64(0), int64(0)
	for _, r := range bucketRows(vocabulary.FormCounter, points, w.From, w.To) {
		sum.Add(sum, r.Increase)
		sumResets += r.Resets
		sumCovered += r.CoveredMs
	}
	if sum.RatString() != increase || sumResets != resets || sumCovered != covered.Milliseconds() {
		t.Errorf("rows: increase %s resets %d covered %dms, want %s, %d, %v", sum.RatString(), sumResets, sumCovered, increase, resets, covered)
	}
}

// Decision 0157: a monotonic meter that resets twice inside one bucket, and
// a reading that drops across a bucket boundary; a counter with no resetRule
// reads the same way.
func TestCounterIncreaseAcrossAMonotonicReset(t *testing.T) {
	counterCase(t, counter([]Point{
		reading(0, "100"), reading(1, "105"), reading(2, "2"),
		reading(3, "6"), reading(4, "1"), reading(5, "4"),
	}, vocabulary.ResetMonotonic, ""), 0, 15, "15", 2, dur(5))
	counterCase(t, counter([]Point{reading(10, "40"), reading(14, "42"), reading(16, "3"), reading(20, "5")},
		vocabulary.ResetMonotonic, ""), 0, 30, "7", 1, dur(10))
	counterCase(t, []Point{reading(0, "10"), reading(1, "4")}, 0, 15, "4", 1, dur(1))
}

// Decision 0157: a signed net meter falls within one resetAt epoch, and a
// new resetAt starts a new accumulation.
func TestCounterSignedIncreaseIsNetWithinAnEpoch(t *testing.T) {
	r1, r2 := t0.Add(-24*time.Hour), t0.Add(2*time.Minute)
	epoch := func(p Point, r time.Time) Point { p.ResetAt = r; return p }
	counterCase(t, counter([]Point{
		epoch(reading(0, "100"), r1), epoch(reading(1, "90"), r1),
		epoch(reading(14, "95"), r1), epoch(reading(16, "3"), r2), epoch(reading(17, "1"), r2),
	}, vocabulary.ResetSigned, ""), 0, 30, "-4", 1, dur(17))
}

// Decision 0157: a register wraps at its wrapAt, across a bucket boundary.
func TestCounterWrapsAtWrapAt(t *testing.T) {
	counterCase(t, counter([]Point{
		reading(0, "250"), reading(13, "254"), reading(16, "3"), reading(20, "10"),
	}, vocabulary.ResetWrap, "256"), 0, 30, "16", 1, dur(20))
}

// Decision 0157: an increment exact in the stored digits of a large reading
// comes back exact, where float64 would lose it.
func TestCounterIncreaseStaysExact(t *testing.T) {
	points := counter([]Point{
		reading(0, "123456789012.001"), reading(14, "123456789012.002"), reading(16, "123456789012.003"),
	}, vocabulary.ResetMonotonic, "")
	counterCase(t, points, 0, 30, "1/500", 0, dur(16))
	if f := points[2].Value - points[0].Value; f == 0.002 {
		t.Error("float64 kept the increment; the case proves nothing about exactness")
	}
	// A change of unit starts over: the later reading counted from zero.
	changed := []Point{reading(0, "5"), reading(1, "5100")}
	changed[1].Unit = "W.h"
	counterCase(t, changed, 0, 15, "5100", 1, dur(1))
}

// The step from the point current at a window's start into its first point
// is the window's, so two adjacent windows add up to the window over both.
func TestCounterStepInBelongsToTheLaterWindow(t *testing.T) {
	points := counter([]Point{reading(-5, "98"), reading(0, "100"), reading(14, "103"), reading(16, "110")},
		vocabulary.ResetMonotonic, "")
	first, _, _, _ := Increase(points, window(0, 15))
	second, _, _, _ := Increase(points, window(15, 30))
	both, _, covered, _ := Increase(points, window(0, 30))
	if first.RatString() != "5" || second.RatString() != "7" || both.RatString() != "12" || covered != dur(21) {
		t.Errorf("increase = %s + %s, over both %s covering %v; want 5 + 7 = 12 over 21m", first, second, both, covered)
	}
	if got, _ := Rate(points, window(0, 30)); !near(got, 12/dur(21).Seconds()) {
		t.Errorf("rate = %v", got)
	}
}

// Decision 0160: a bucket answers twa and timeInState from its stored held
// time only when the request's maxGap is absent or not below its largest gap.
func TestRollupServesTwaOnlyWithinMaxGap(t *testing.T) {
	for _, tc := range []struct {
		maxGap   time.Duration
		maxGapMs int64
		want     bool
	}{
		{0, 900000, true},
		{time.Hour, 900000, true},
		{15 * time.Minute, 900000, true},
		{10 * time.Minute, 900000, false},
		{time.Second, 1000, true},
		{time.Second, 1001, false},
	} {
		if got := Eligible(tc.maxGap, tc.maxGapMs); got != tc.want {
			t.Errorf("Eligible(%v, %d) = %v, want %v", tc.maxGap, tc.maxGapMs, got, tc.want)
		}
	}
}
