package observed

import (
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

func bucketAt(min float64) (time.Time, time.Time) { return at(min), at(min).Add(BucketWidth) }

func ms(d time.Duration) int64 { return d.Milliseconds() }

// One gauge bucket worked by hand: 60 at 10:00, 62 at 10:05, a null at
// 10:07, 64 at 10:10, every value held four minutes.
func TestBucketOfAGauge(t *testing.T) {
	hold := dur(4)
	points := []Point{held(gauge(0, 60), hold), held(gauge(5, 62), hold), null(7), held(gauge(10, 64), hold)}
	start, end := bucketAt(0)
	rows := Bucket(vocabulary.FormGauge, start, end, nil, points)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one group", len(rows))
	}
	s := rows[""]
	// 60 holds its 4 minutes, 62 until the null, 64 its 4 minutes.
	wantHeld := ms(dur(4) + dur(2) + dur(4))
	wantTWA := 60*float64(ms(dur(4))) + 62*float64(ms(dur(2))) + 64*float64(ms(dur(4)))
	if s.N != 3 || s.Values != 3 || s.Sum != 186 || s.Min != 60 || s.Max != 64 || !near(s.Mean, 62) || !near(s.M2, 8) {
		t.Errorf("moments = %+v", s)
	}
	if s.First.At != at(0) || s.Last.At != at(10) || s.CoveredMs != ms(dur(10)) {
		t.Errorf("first %v last %v covered %d", s.First.At, s.Last.At, s.CoveredMs)
	}
	if s.HeldMs != wantHeld || !near(s.TWASum, wantTWA) {
		t.Errorf("held %d twa_sum %v, want %d and %v", s.HeldMs, s.TWASum, wantHeld, wantTWA)
	}
	if s.LastHoldMs == nil || *s.LastHoldMs != ms(hold) {
		t.Errorf("last_hold_ms = %v, want its hold", s.LastHoldMs)
	}
	// The gaps: 5, 2 and 3 minutes between points, 5 from 10:10 to the end.
	if s.MaxGapMs != ms(dur(5)) || s.StartGapMs != 0 {
		t.Errorf("max_gap %d start_gap %d", s.MaxGapMs, s.StartGapMs)
	}

	// With no hold each value holds until the next point, and the last one
	// to the end of the bucket and past it.
	for i := range points {
		points[i].Hold = nil
	}
	s = Bucket(vocabulary.FormGauge, start, end, nil, points)[""]
	if s.HeldMs != ms(dur(5)+dur(2)+dur(5)) || s.LastHoldMs != nil {
		t.Errorf("held %d last_hold %v, want 12 minutes and unbounded", s.HeldMs, s.LastHoldMs)
	}
}

// A coded gauge keeps one row per code, the held time of each built from
// every transition of the bucket.
func TestBucketOfACodedGauge(t *testing.T) {
	points := []Point{coded(1, "closed"), coded(4, "open"), coded(6, "closed"), coded(12, "open")}
	start, end := bucketAt(0)
	rows := Bucket(vocabulary.FormGauge, start, end, nil, points)
	open, closed := rows["open"], rows["closed"]
	if open.N != 2 || closed.N != 2 || open.Values != 0 {
		t.Fatalf("rows = %+v %+v", open, closed)
	}
	if open.HeldMs != ms(dur(2)+dur(3)) || closed.HeldMs != ms(dur(3)+dur(6)) {
		t.Errorf("held open %d closed %d", open.HeldMs, closed.HeldMs)
	}
	// closed's last point is followed by open at 10:12, so it carries nothing
	// out; open's is the bucket's last and holds on.
	if closed.LastHoldMs == nil || *closed.LastHoldMs != ms(dur(6)) || open.LastHoldMs != nil {
		t.Errorf("last_hold closed %v open %v", closed.LastHoldMs, open.LastHoldMs)
	}
	if open.StartGapMs != ms(dur(1)) || closed.MaxGapMs != ms(dur(6)) {
		t.Errorf("start_gap %d max_gap %d", open.StartGapMs, closed.MaxGapMs)
	}
}

func TestBucketOfACounterTakesItsStepIn(t *testing.T) {
	points := counter([]Point{reading(0, "100"), reading(5, "2"), reading(10, "7")}, vocabulary.ResetMonotonic, "")
	prev := counter([]Point{reading(-3, "96")}, vocabulary.ResetMonotonic, "")[0]
	start, end := bucketAt(0)
	s := Bucket(vocabulary.FormCounter, start, end, &prev, points)[""]
	if s.Increase.RatString() != "11" || s.Resets != 1 || s.CoveredMs != ms(dur(13)) {
		t.Errorf("increase %s resets %d covered %d, want 4 + 2 + 5 = 11, 1, 13 minutes", s.Increase.RatString(), s.Resets, s.CoveredMs)
	}
	if s.First.Exact != "100" || s.Last.Exact != "7" {
		t.Errorf("first %s last %s", s.First.Exact, s.Last.Exact)
	}
	alone := Bucket(vocabulary.FormCounter, start, end, nil, points)[""]
	if alone.Increase.RatString() != "7" {
		t.Errorf("a stream's first bucket = %s, want 7", alone.Increase.RatString())
	}
	// One reading with none before it holds no step: no answer, as over the
	// raw points, and not a change of zero.
	first := Bucket(vocabulary.FormCounter, start, end, nil, points[:1])[""]
	if _, _, _, ok := Increase(points[:1], Window{From: start, To: end}); ok || first.Increase != nil {
		t.Errorf("a lone first reading: rows say %v, raw says ok %v; want no increase from either", first.Increase, ok)
	}
}

// The extremes are compared exactly and keep their stored digits, in a row,
// across merged rows and over raw points alike: 2^53 + 1 and 2^53 are one
// float64, and so are two decimals 1e-17 apart.
func TestExtremesKeepTheirDigits(t *testing.T) {
	for _, digits := range [][2]string{
		{"9007199254740993", "9007199254740992"},
		{"1.00000000000000002", "1.00000000000000001"},
	} {
		hi, lo := reading(1, digits[0]), reading(2, digits[1])
		start, end := bucketAt(0)
		row := Bucket(vocabulary.FormAmount, start, end, nil, []Point{hi, lo})[""]
		if row.MinExact != digits[1] || row.MaxExact != digits[0] {
			t.Errorf("row extremes %s/%s, want %s/%s", row.MinExact, row.MaxExact, digits[1], digits[0])
		}
		a := Bucket(vocabulary.FormAmount, start, end, nil, []Point{hi})[""]
		b := Bucket(vocabulary.FormAmount, end, end.Add(BucketWidth), nil, []Point{reading(16, digits[1])})[""]
		if m := Merge(vocabulary.FormAmount, *a, *b); m.MinExact != digits[1] || m.MaxExact != digits[0] {
			t.Errorf("merged extremes %s/%s, want %s/%s", m.MinExact, m.MaxExact, digits[1], digits[0])
		}
		w := window(0, 15)
		p, _ := MinPoint([]Point{hi, lo}, w)
		q, _ := MaxPoint([]Point{lo, hi}, w)
		if p.Exact != digits[1] || q.Exact != digits[0] {
			t.Errorf("raw extremes %s/%s, want %s/%s", p.Exact, q.Exact, digits[1], digits[0])
		}
	}
}

func amount(from, to, v float64, code string) Point {
	return Point{At: at(from), End: at(to), Value: v, HasValue: true, Code: code, Group: code}
}

// An amount belongs whole to the bucket its span starts in; its coverage is
// the union of the row's spans, unclipped.
func TestBucketOfAnAmount(t *testing.T) {
	points := []Point{amount(0, 5, 100, ""), amount(5, 10, 50, ""), amount(10, 25, 300, "")}
	start, end := bucketAt(0)
	s := Bucket(vocabulary.FormAmount, start, end, nil, points)[""]
	if s.Sum != 450 || s.N != 3 || s.CoveredMs != ms(dur(25)) || s.HeldMs != 0 || s.Increase != nil {
		t.Errorf("amount row = %+v", s)
	}
	stages := []Point{amount(0, 7, 7, "light"), amount(7, 9, 2, "rem"), amount(9, 15, 6, "light")}
	rows := Bucket(vocabulary.FormAmount, start, end, nil, stages)
	if rows["light"].Sum != 13 || rows["light"].CoveredMs != ms(dur(13)) || rows["rem"].Sum != 2 {
		t.Errorf("stage rows = %+v %+v", rows["light"], rows["rem"])
	}
}

func TestASummaryHasNoRollup(t *testing.T) {
	start, end := bucketAt(0)
	if rows := Bucket(vocabulary.FormSummary, start, end, nil, []Point{gauge(0, 67)}); rows != nil {
		t.Errorf("a summary rolled up: %+v", rows)
	}
}

// A bucket of unavailable points alone still has a row: where the hold from
// an earlier bucket stops is the read's to see.
func TestABucketOfNullsHasARow(t *testing.T) {
	start, end := bucketAt(0)
	rows := Bucket(vocabulary.FormGauge, start, end, nil, []Point{null(3)})
	if s := rows[""]; s == nil || s.N != 0 || s.StartGapMs != ms(dur(3)) {
		t.Errorf("rows = %+v", rows)
	}
}

// stream is a gauge stream with every shape the read meets: holds, a null,
// codes changing, two points at one instant, a three-hour silence and a
// value straddling a bucket boundary.
func stream() []Point {
	return []Point{
		gauge(-20, 18),
		held(gauge(2, 20), dur(6)),
		gauge(9, 21),
		gauge(9, 22),
		null(16),
		gauge(29, 23),
		held(gauge(44, 24), dur(20)),
		gauge(70, 25),
		gauge(250, 26),
		gauge(251, 27),
	}
}

// bucketRows computes the rows of every bucket in [from, to) the way the
// index does, each bucket from its own points and the stream's last
// available point before it.
func bucketRows(form string, points []Point, from, to time.Time) []Row {
	var rows []Row
	for b := from; b.Before(to); b = b.Add(BucketWidth) {
		var in []Point
		var prev *Point
		for i := range points {
			p := points[i]
			switch {
			case p.At.Before(b):
				if !p.Unavailable {
					prev = &points[i]
				}
			case p.At.Before(b.Add(BucketWidth)):
				in = append(in, p)
			}
		}
		for g, s := range Bucket(form, b, b.Add(BucketWidth), prev, in) {
			rows = append(rows, Row{Start: b, Group: g, Stats: *s})
		}
	}
	return rows
}

// The read over rows answers what the raw points answer, wherever every row
// is eligible: no maxGap, and a maxGap no gap inside a bucket exceeds.
func TestRollupsAnswerWhatRawPointsAnswer(t *testing.T) {
	points := stream()
	from, to := at(0), at(270)
	rows := bucketRows(vocabulary.FormGauge, points, from, to)
	var carry *Point
	for i := range points {
		if points[i].At.Before(from) {
			carry = &points[i]
		}
	}
	for _, maxGap := range []time.Duration{0, 3 * time.Hour, 4 * time.Hour} {
		got, ok := HeldFromRows(rows, carry, from, to, maxGap)
		if !ok {
			t.Fatalf("maxGap %v: a row was not eligible", maxGap)
		}
		var want HeldSum
		for _, iv := range Held(points, from, to, maxGap) {
			d := iv.End.Sub(iv.Start).Milliseconds()
			want.Ms += d
			want.ValueMs += points[iv.Point].Value * float64(d)
		}
		if got[""].Ms != want.Ms || !near(got[""].ValueMs, want.ValueMs) {
			t.Errorf("maxGap %v: rows held %+v, raw %+v", maxGap, got[""], want)
		}
		if twa, _ := TWA(points, Window{From: from, To: to, MaxGap: maxGap}); !near(got[""].ValueMs/float64(got[""].Ms), twa) {
			t.Errorf("maxGap %v: twa from rows %v, raw %v", maxGap, got[""].ValueMs/float64(got[""].Ms), twa)
		}
	}
	// A maxGap below a bucket's largest gap sends that bucket to raw points.
	if _, ok := HeldFromRows(rows, carry, from, to, 5*time.Minute); ok {
		t.Error("rows answered under a maxGap their gaps exceed")
	}
}

func TestCodedRollupsAnswerWhatRawPointsAnswer(t *testing.T) {
	points := []Point{
		coded(-5, "open"), coded(3, "closed"), coded(14, "open"), coded(16, "closed"),
		null(40), coded(61, "open"), coded(62, "closed"),
	}
	from, to := at(0), at(90)
	rows := bucketRows(vocabulary.FormGauge, points, from, to)
	got, ok := HeldFromRows(rows, &points[0], from, to, 0)
	if !ok {
		t.Fatal("not eligible")
	}
	want := TimeInState(points, Window{From: from, To: to})
	for _, code := range []string{"open", "closed"} {
		if time.Duration(got[code].Ms)*time.Millisecond != want[code] {
			t.Errorf("%s: rows %v, raw %v", code, time.Duration(got[code].Ms)*time.Millisecond, want[code])
		}
	}
	// open 10:00-10:03, 10:14-10:16, 11:01-11:02; closed 10:03-10:14,
	// 10:16-10:40 and from 11:02 to the end.
	if want["open"] != dur(6) || want["closed"] != dur(11)+dur(24)+dur(28) {
		t.Errorf("raw = %v", want)
	}
}

// Merging rows loses nothing a reducer reads, and a counter's rows add up to
// the window's increase.
func TestMergeIsTheBucketOverBoth(t *testing.T) {
	points := counter([]Point{
		reading(-2, "50"), reading(1, "52"), reading(7, "1"), reading(13, "5"),
		reading(16, "9"), reading(20, "3"), reading(29, "4"),
	}, vocabulary.ResetMonotonic, "")
	from, to := at(0), at(30)
	rows := bucketRows(vocabulary.FormCounter, points, from, to)
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	m := Merge(vocabulary.FormCounter, rows[0].Stats, rows[1].Stats)
	inc, resets, covered, _ := Increase(points, Window{From: from, To: to})
	if m.Increase.Cmp(inc) != 0 || m.Resets != resets || m.CoveredMs != covered.Milliseconds() {
		t.Errorf("merged %s %d %d, raw %s %d %v", m.Increase.RatString(), m.Resets, m.CoveredMs, inc.RatString(), resets, covered)
	}
	if m.N != 6 || m.First.Exact != "52" || m.Last.Exact != "4" {
		t.Errorf("merged n %d first %s last %s", m.N, m.First.Exact, m.Last.Exact)
	}

	gauges := []Point{gauge(1, 3), gauge(4, 5), gauge(11, 9), gauge(17, 1), gauge(24, 7)}
	g := bucketRows(vocabulary.FormGauge, gauges, from, to)
	mg := Merge(vocabulary.FormGauge, g[0].Stats, g[1].Stats)
	whole := Bucket(vocabulary.FormGauge, from, to, nil, gauges)[""]
	w := Window{From: from, To: to}
	mean, _ := Avg(gauges, w)
	sd, _ := StdDev(gauges, w)
	if mg.N != 5 || mg.Sum != 25 || mg.Min != 1 || mg.Max != 9 || !near(mg.Mean, mean) || !near(mg.M2, whole.M2) {
		t.Errorf("merged gauge = %+v", mg)
	}
	// 9 holds from 10:11 to 10:17, and the two minutes past 10:15 are the
	// read's to carry (HeldFromRows), not a row's: the rows hold 27 of 29.
	if !near(sd*sd*5, mg.M2) || mg.CoveredMs != ms(dur(23)) || mg.HeldMs != ms(dur(27)) {
		t.Errorf("m2 %v covered %d held %d", mg.M2, mg.CoveredMs, mg.HeldMs)
	}
}

func TestCircularRows(t *testing.T) {
	points := []Point{degrees(1, 350), degrees(14, 20), degrees(16, 10)}
	rows := bucketRows(vocabulary.FormGauge, points, at(0), at(30))
	m := Merge(vocabulary.FormGauge, rows[0].Stats, rows[1].Stats)
	got, ok := CircularMeanOf(m)
	want, _ := CircularMean(points, window(0, 30))
	if !ok || !near(got, want) || want < 6 || want > 7 {
		t.Errorf("circular mean from rows %v, raw %v", got, want)
	}
	if rows[0].Angles != 2 || rows[1].Angles != 1 {
		t.Errorf("angles %d %d", rows[0].Angles, rows[1].Angles)
	}
}

// Walking consecutive windows and handing each one's carry to the next holds
// what one walk over the whole range holds, window by window.
func TestHeldFromRowsCarryThreadsWindows(t *testing.T) {
	points := stream()
	from, to := at(0), at(270)
	rows := bucketRows(vocabulary.FormGauge, points, from, to)
	var carry *Point
	for i := range points {
		if points[i].At.Before(from) {
			carry = &points[i]
		}
	}
	for _, maxGap := range []time.Duration{0, 3 * time.Hour} {
		whole, ok := HeldFromRows(rows, carry, from, to, maxGap)
		if !ok {
			t.Fatalf("maxGap %v: a row was not eligible", maxGap)
		}
		var sum HeldSum
		c := carry
		for start := 0.0; start < 270; start += 45 {
			var part map[string]HeldSum
			part, c, ok = HeldFromRowsCarry(rows, c, at(start), at(start+45), maxGap)
			if !ok {
				t.Fatalf("maxGap %v: window %v was not eligible", maxGap, start)
			}
			raw := HeldMs(Spans(points, at(start), at(start+45), maxGap))
			if part[""].Ms != raw {
				t.Errorf("maxGap %v: window at %v held %d ms from rows, %d raw", maxGap, start, part[""].Ms, raw)
			}
			sum.Ms += part[""].Ms
			sum.ValueMs += part[""].ValueMs
		}
		if sum.Ms != whole[""].Ms || !near(sum.ValueMs, whole[""].ValueMs) {
			t.Errorf("maxGap %v: the windows held %+v, the whole %+v", maxGap, sum, whole[""])
		}
	}
}
