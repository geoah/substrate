package observed

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// composeStream is a generated stream: points in Before order, each with a
// group that names its offset, so a test can tell which bucket of a tier
// holds it.
type composeStream struct {
	form    string
	points  []Point
	offsets map[string]int64
}

// genStream builds n points of one form over about a day and a half from
// t0, with gaps, nulls, two codes or units, offsets that change the way a
// clock change or a trip does, holds, counter resets and censored values.
func genStream(rng *rand.Rand, form string, n int) composeStream {
	s := composeStream{form: form, offsets: map[string]int64{}}
	offsets := []int64{60, 120, 0, 330}
	at := t0.Add(-time.Duration(rng.Intn(90)) * time.Minute)
	total := big.NewRat(0, 1)
	for i := range n {
		step := time.Duration(1+rng.Intn(9)) * time.Minute
		if rng.Intn(25) == 0 {
			step += time.Duration(rng.Intn(5)) * time.Hour
		}
		at = at.Add(step)
		off := offsets[(i*len(offsets))/n]
		if rng.Intn(40) == 0 {
			off = offsets[rng.Intn(len(offsets))]
		}
		unit := "a"
		if form != vocabulary.FormCounter && rng.Intn(6) == 0 {
			unit = "b"
		}
		code := ""
		if form == vocabulary.FormAmount && rng.Intn(5) == 0 {
			code = "x"
		}
		group := fmt.Sprintf("%s|%s|%d", code, unit, off)
		s.offsets[group] = off
		p := Point{At: at, End: at, Unit: unit, Code: code, Group: group, RecordID: fmt.Sprintf("r%03d", i/7), Ord: int64(i % 7)}
		if rng.Intn(15) == 0 {
			p.Unavailable = true
			p.Group = "||" + strconv.FormatInt(off, 10)
			s.offsets[p.Group] = off
			s.points = append(s.points, p)
			continue
		}
		v := rng.Intn(1000)
		switch form {
		case vocabulary.FormCounter:
			if rng.Intn(12) == 0 {
				total = big.NewRat(int64(rng.Intn(20)), 1)
			} else {
				total.Add(total, big.NewRat(int64(rng.Intn(50)), 1))
			}
			v = int(total.Num().Int64())
		case vocabulary.FormAmount:
			p.End = at.Add(time.Duration(1+rng.Intn(6)) * time.Minute)
		case vocabulary.FormGauge:
			if rng.Intn(3) == 0 {
				h := time.Duration(1+rng.Intn(20)) * time.Minute
				p.Hold = &h
			}
		}
		digits := strconv.Itoa(v) + ".5"
		if form == vocabulary.FormCounter {
			digits = strconv.Itoa(v)
		}
		p.Exact, p.HasValue = digits, true
		p.Value, _ = strconv.ParseFloat(digits, 64)
		if form == vocabulary.FormGauge && rng.Intn(30) == 0 {
			p.Comparator = "<"
		}
		s.points = append(s.points, p)
	}
	return s
}

// lastAvailableBefore is the stream's last available point before t.
func (s composeStream) lastAvailableBefore(t time.Time) *Point {
	var out *Point
	for i := range s.points {
		p := s.points[i]
		if !p.At.Before(t) {
			break
		}
		if !p.Unavailable {
			out = &p
		}
	}
	return out
}

// direct computes a bucket of width w at start from the points, keeping
// the groups aligned to it.
func (s composeStream) direct(start time.Time, w time.Duration) map[string]*Stats {
	end := start.Add(w)
	b := NewBucketBuilder(s.form, start, end, s.lastAvailableBefore(start))
	for _, p := range s.points {
		if !p.At.Before(start) && p.At.Before(end) {
			b.Add(p)
		}
	}
	rows := b.Rows()
	for g := range rows {
		if TierStart(start, w, s.offsets[g]) != start {
			delete(rows, g)
		}
	}
	return rows
}

// tierRows computes every bucket of width w the stream's points fall in,
// each group in the bucket its own offset aligns.
func (s composeStream) tierRows(w time.Duration) []Row {
	starts := map[time.Time]bool{}
	for _, p := range s.points {
		starts[TierStart(p.At, w, s.offsets[p.Group])] = true
	}
	var out []Row
	for start := range starts {
		for g, st := range s.direct(start, w) {
			out = append(out, Row{Start: start, End: start.Add(w), Group: g, Stats: *st})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// A coarser row composed from the finer rows inside its bucket answers what
// the points do, for every form, every offset alignment and every pair of
// tiers, wherever Compose says it can.
func TestComposeMatchesThePoints(t *testing.T) {
	tiers := []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour}
	composed, declined := 0, 0
	for seed := int64(1); seed <= 16; seed++ {
		rng := rand.New(rand.NewSource(seed))
		for _, form := range []string{vocabulary.FormGauge, vocabulary.FormAmount, vocabulary.FormCounter} {
			s := genStream(rng, form, 150+rng.Intn(250))
			for fi := 0; fi+1 < len(tiers); fi++ {
				fine := s.tierRows(tiers[fi])
				for ci := fi + 1; ci < len(tiers); ci++ {
					w := tiers[ci]
					seen := map[time.Time]bool{}
					for _, coarse := range s.tierRows(w) {
						if seen[coarse.Start] {
							continue
						}
						seen[coarse.Start] = true
						start, end := coarse.Start, coarse.End
						var inside []Row
						straddles := false
						for _, r := range fine {
							if !r.End.After(start) || !r.Start.Before(end) {
								continue
							}
							if r.Start.Before(start) || r.End.After(end) {
								straddles = true
							}
							inside = append(inside, r)
						}
						if straddles {
							continue
						}
						keep := func(g string) bool { return TierStart(start, w, s.offsets[g]) == start }
						got, ok := Compose(form, start, end, inside, keep)
						if !ok {
							declined++
							continue
						}
						composed++
						want := s.direct(start, w)
						name := fmt.Sprintf("seed %d %s %v from %v at %v", seed, form, w, tiers[fi], start)
						for g, ws := range want {
							gs := got[g]
							if gs == nil {
								t.Fatalf("%s: group %s missing", name, g)
							}
							sameStats(t, name+" "+g, form, gs, ws)
						}
						for g := range got {
							if want[g] == nil {
								t.Fatalf("%s: group %s composed but the points hold none", name, g)
							}
						}
					}
				}
			}
		}
	}
	if composed == 0 {
		t.Fatal("no bucket was composed")
	}
	t.Logf("composed %d buckets, %d declined", composed, declined)
}

func sameStats(t *testing.T, name, form string, got, want *Stats) {
	t.Helper()
	ints := []struct {
		what   string
		g, w   int64
		active bool
	}{
		{"n", got.N, want.N, true},
		{"bounded", got.Bounded, want.Bounded, true},
		{"values", got.Values, want.Values, true},
		{"covered", got.CoveredMs, want.CoveredMs, true},
		{"held", got.HeldMs, want.HeldMs, form == vocabulary.FormGauge},
		{"maxGap", got.MaxGapMs, want.MaxGapMs, true},
		{"startGap", got.StartGapMs, want.StartGapMs, true},
		{"endGap", got.EndGapMs, want.EndGapMs, true},
		{"resets", got.Resets, want.Resets, true},
	}
	for _, c := range ints {
		if c.active && c.g != c.w {
			t.Fatalf("%s: %s = %d, want %d", name, c.what, c.g, c.w)
		}
	}
	floats := [][3]any{{"sum", got.Sum, want.Sum}, {"mean", got.Mean, want.Mean}, {"m2", got.M2, want.M2}, {"twa", got.TWASum, want.TWASum}}
	for _, f := range floats {
		g, w := f[1].(float64), f[2].(float64)
		if math.Abs(g-w) > 1e-6*math.Max(1, math.Abs(w)) {
			t.Fatalf("%s: %s = %v, want %v", name, f[0], g, w)
		}
	}
	if got.MinExact != want.MinExact || got.MaxExact != want.MaxExact {
		t.Fatalf("%s: extremes %s..%s, want %s..%s", name, got.MinExact, got.MaxExact, want.MinExact, want.MaxExact)
	}
	if !got.SpanEnd.Equal(want.SpanEnd) {
		t.Fatalf("%s: span end %v, want %v", name, got.SpanEnd, want.SpanEnd)
	}
	samePoint := func(what string, g, w *Point) {
		if (g == nil) != (w == nil) || (g != nil && (!g.At.Equal(w.At) || g.RecordID != w.RecordID || g.Ord != w.Ord)) {
			t.Fatalf("%s: %s = %+v, want %+v", name, what, g, w)
		}
	}
	samePoint("first", got.First, want.First)
	samePoint("last", got.Last, want.Last)
	if (got.LastHoldMs == nil) != (want.LastHoldMs == nil) || (got.LastHoldMs != nil && *got.LastHoldMs != *want.LastHoldMs) {
		t.Fatalf("%s: last hold %v, want %v", name, ptrString(got.LastHoldMs), ptrString(want.LastHoldMs))
	}
	if (got.Increase == nil) != (want.Increase == nil) || (got.Increase != nil && got.Increase.Cmp(want.Increase) != 0) {
		t.Fatalf("%s: increase %v, want %v", name, got.Increase, want.Increase)
	}
}

func ptrString(p *int64) string {
	if p == nil {
		return "nil"
	}
	return strconv.FormatInt(*p, 10)
}
