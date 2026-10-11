package observed

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A priority list resolves per stored interval: the higher source wins where
// it holds, the lower fills its gaps, and the total time is the union's, so
// it does not depend on how the range is later cut into buckets.
func TestPrioritizeFillsTheGapsOfTheHigherSource(t *testing.T) {
	strap := []Point{held(gauge(10, 120), dur(10)), held(gauge(40, 130), dur(10))}
	watch := []Point{held(gauge(0, 60), dur(60))}
	w := window(0, 60)
	got, err := Prioritize(context.Background(), [][]Span{Spans(strap, w.From, w.To, 0), Spans(watch, w.From, w.To, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if ms := HeldMs(got[0]); ms != dur(20).Milliseconds() {
		t.Errorf("the strap holds %d ms, want 20 minutes", ms)
	}
	if ms := HeldMs(got[1]); ms != dur(40).Milliseconds() {
		t.Errorf("the watch fills %d ms, want the 40 minutes the strap left", ms)
	}
	all := append(append([]Span(nil), got[0]...), got[1]...)
	twa, _ := TWAOf(all)
	if want := (120*10 + 130*10 + 60*40) / 60.0; !near(twa, want) {
		t.Errorf("twa = %v, want %v", twa, want)
	}
	// Cutting the resolved spans at 15 minutes changes nothing.
	var parts int64
	for start := 0.0; start < 60; start += 15 {
		parts += HeldMs(Clip(all, at(start), at(start+15)))
	}
	if parts != dur(60).Milliseconds() {
		t.Errorf("the 15-minute parts hold %d ms, want the whole hour", parts)
	}
	if !Covered(got[0], at(12)) || Covered(got[0], at(25)) {
		t.Error("Covered misreads the strap's spans")
	}
}

// fragmented is two sources of n one-second holds each, interleaved so the
// higher source's cover stays n disjoint intervals and each lower span half
// overlaps one of them.
func fragmented(n int) [][]Span {
	sec := func(halves int) time.Time { return t0.Add(time.Duration(halves) * time.Second / 2) }
	high, low := make([]Span, n), make([]Span, n)
	for k := range n {
		high[k] = Span{Start: sec(4 * k), End: sec(4*k + 2)}
		low[k] = Span{Start: sec(4*k + 1), End: sec(4*k + 3)}
	}
	return [][]Span{high, low}
}

// Two fragmented sources of 300,000 holds each resolve in one sweep: each
// lower span keeps the half second after the higher one's hold. Comparing
// every lower span with the whole higher cover took about 90 billion steps,
// minutes after the SQL had finished.
func TestPrioritizeIsLinearOverFragmentedCover(t *testing.T) {
	const n = 300000
	got, err := Prioritize(context.Background(), fragmented(n))
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0]) != n || len(got[1]) != n {
		t.Fatalf("resolved %d and %d spans, want %d each", len(got[0]), len(got[1]), n)
	}
	if ms := HeldMs(got[1]); ms != int64(n)*500 {
		t.Errorf("the lower source keeps %d ms, want half a second of each hold", ms)
	}
	if s := got[1][n-1]; !s.Start.Equal(t0.Add(time.Duration(4*(n-1)+2)*time.Second/2)) || s.End.Sub(s.Start) != time.Second/2 {
		t.Errorf("the last lower span is %v to %v", s.Start, s.End)
	}
}

// A resolution whose caller has stopped waiting stops: a context canceled
// before it starts answers its error and no spans.
func TestPrioritizeStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := Prioritize(ctx, fragmented(10000))
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Errorf("Prioritize under a canceled context = %d streams, %v; want nil and context.Canceled", len(got), err)
	}
}

func BenchmarkPrioritizeFragmented(b *testing.B) {
	streams := fragmented(300000)
	for b.Loop() {
		if _, err := Prioritize(context.Background(), streams); err != nil {
			b.Fatal(err)
		}
	}
}

// An amount's coverage joins extents: disjoint ones add, raw spans that
// overlap or touch join as one, and a row's union overlapping anything else
// cannot be measured from the row, so the reader is told to read the spans.
func TestUnionMsJoinsWhatItCanMeasure(t *testing.T) {
	row := func(from, to, covered float64) Extent {
		return Extent{Start: at(from), End: at(to), Ms: dur(covered).Milliseconds()}
	}
	span := func(from, to float64) Extent {
		return Extent{Start: at(from), End: at(to), Ms: dur(to - from).Milliseconds(), Exact: true}
	}
	for _, x := range []struct {
		name    string
		extents []Extent
		want    float64
		ok      bool
	}{
		{"two rows apart", []Extent{row(15, 50, 35), row(0, 35, 20)}, 0, false},
		{"rows touching", []Extent{row(0, 15, 10), row(15, 30, 15)}, 25, true},
		{"spans joining", []Extent{span(0, 10), span(5, 20), span(20, 25)}, 25, true},
		{"a span inside a row", []Extent{row(0, 60, 30), span(10, 20)}, 0, false},
		{"a span after a row", []Extent{row(0, 15, 15), span(15, 20)}, 20, true},
		{"nothing", nil, 0, true},
	} {
		got, ok := UnionMs(x.extents)
		if ok != x.ok || got != dur(x.want).Milliseconds() {
			t.Errorf("%s: %d ms, %v; want %v minutes, %v", x.name, got, ok, x.want, x.ok)
		}
	}
}

// The window reducers are the span reducers over Held: one implementation.
func TestWindowReducersAreTheSpanReducers(t *testing.T) {
	points := []Point{coded(0, "closed"), coded(20, "open"), coded(25, "closed")}
	w := window(0, 60)
	if got, want := TimeInState(points, w)["open"], TimeInStateOf(Spans(points, w.From, w.To, 0))["open"]; got != want || got != 5*time.Minute {
		t.Errorf("open = %v and %v, want 5m", got, want)
	}
}
