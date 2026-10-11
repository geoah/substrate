package observed

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"testing"
	"time"
)

// ending is a context whose Err answers nil for its first n calls and
// context.Canceled after, counting them: a context that ends at a known
// check inside the work.
type ending struct {
	context.Context
	n, calls int
}

func (c *ending) Err() error {
	c.calls++
	if c.calls > c.n {
		return context.Canceled
	}
	return nil
}

// stopsThroughout runs work over n elements under a context that never ends,
// and then under contexts that end after its first check, half way through
// and before its last. The work checks at least once per checkEvery
// elements, and answers context.Canceled at the very check after the end,
// so it stops there instead of running on over the rest.
func stopsThroughout(t *testing.T, name string, n int, work func(ctx context.Context) error) {
	t.Helper()
	whole := &ending{Context: context.Background(), n: math.MaxInt}
	if err := work(whole); err != nil {
		t.Fatalf("%s: %v under a context that never ends", name, err)
	}
	if want := n / checkEvery; whole.calls < want {
		t.Errorf("%s checks its context %d times over %d elements, want at least %d", name, whole.calls, n, want)
	}
	for _, k := range []int{1, whole.calls / 2, whole.calls - 1} {
		c := &ending{Context: context.Background(), n: k}
		if err := work(c); !errors.Is(err, context.Canceled) || c.calls != k+1 {
			t.Errorf("%s under a context that ends after check %d of %d: %v at check %d, want context.Canceled at check %d",
				name, k, whole.calls, err, c.calls, k+1)
		}
	}
}

// Every reducer a read runs over a whole bucket checks its context as it
// goes and stops at the first check after the context ends: 50,000 points
// and the spans they hold, where Percentile once copied and sorted all of
// them, and each reducer passed over all of them, with no check between.
func TestReducersStopWhereTheirContextEnds(t *testing.T) {
	const n = 50000
	points := make([]Point, n)
	for i := range points {
		points[i] = degrees(float64(i)/60, float64((i*7919)%360))
	}
	w := window(0, n)
	spans := Spans(points, w.From, w.To, 0)
	if len(spans) != n {
		t.Fatalf("%d spans", len(spans))
	}
	point := func(f func(context.Context, []Point, Window) (float64, bool, error)) func(context.Context) error {
		return func(ctx context.Context) error { _, _, err := f(ctx, points, w); return err }
	}
	for _, x := range []struct {
		name string
		work func(ctx context.Context) error
	}{
		{"Sum", point(SumContext)},
		{"Avg", point(AvgContext)},
		{"StdDev", point(StdDevContext)},
		{"CircularMean", point(CircularMeanContext)},
		{"Percentile", func(ctx context.Context) error { _, _, err := PercentileContext(ctx, points, w, 0.9); return err }},
		{"MinPoint", func(ctx context.Context) error { _, _, err := MinPointContext(ctx, points, w); return err }},
		{"MaxPoint", func(ctx context.Context) error { _, _, err := MaxPointContext(ctx, points, w); return err }},
		{"Held", func(ctx context.Context) error { _, err := HeldContext(ctx, points, w.From, w.To, 0); return err }},
		{"HeldMs", func(ctx context.Context) error { _, err := HeldMsContext(ctx, spans); return err }},
		{"TWAOf", func(ctx context.Context) error { _, _, err := TWAOfContext(ctx, spans); return err }},
		{"TimeInStateOf", func(ctx context.Context) error { _, err := TimeInStateOfContext(ctx, spans); return err }},
		{"DurationInOf", func(ctx context.Context) error { _, err := DurationInOfContext(ctx, spans, 90, 180); return err }},
	} {
		stopsThroughout(t, x.name, n, x.work)
	}
	// The percentile's sort answers what sort.Float64s does.
	vs := make([]float64, n)
	for i, p := range points {
		vs[i] = p.Value
	}
	sort.Float64s(vs)
	pos := 0.9 * float64(n-1)
	lo := int(pos)
	want := vs[lo] + (pos-float64(lo))*(vs[lo+1]-vs[lo])
	if got, _ := Percentile(points, w, 0.9); got != want {
		t.Errorf("the 0.9 percentile = %v, want %v", got, want)
	}
}

// A priority resolution checks its context in each long span's walk over a
// fragmented cover, and while it builds, sorts and merges each stream's
// cover: one lower span over 50,000 higher holds once walked all of them
// with no check, and each cover was sorted and merged whole.
func TestPrioritizeStopsWhereItsContextEnds(t *testing.T) {
	const n = 50000
	high := fragmented(n)[0]
	long := Span{Start: high[0].Start, End: high[n-1].End}
	cover, err := mergeCover(context.Background(), spanIntervals(high), func() error { return nil })
	if err != nil || len(cover) != n {
		t.Fatalf("the cover: %d intervals, %v", len(cover), err)
	}
	stopsThroughout(t, "subtract", n, func(ctx context.Context) error {
		_, err := subtract(nil, long, cover, every(ctx))
		return err
	})
	stopsThroughout(t, "Prioritize", n, func(ctx context.Context) error {
		_, err := Prioritize(ctx, [][]Span{high, {long}})
		return err
	})
	stopsThroughout(t, "mergeCover", n, func(ctx context.Context) error {
		_, err := mergeCover(ctx, spanIntervals(high), every(ctx))
		return err
	})
}

func spanIntervals(spans []Span) [][2]time.Time {
	out := make([][2]time.Time, len(spans))
	for i, s := range spans {
		out[i] = [2]time.Time{s.Start, s.End}
	}
	return out
}

// SortStableFunc sorts as slices.SortStableFunc does, equal elements in
// their order, below and above one run's length, and does no more than one
// run's sort between two checks of its context.
func TestSortStableFuncSortsAsTheStandardSort(t *testing.T) {
	type item struct{ key, seq int }
	for _, n := range []int{0, 1, checkEvery, checkEvery + 1, 3*checkEvery + 7, 70000} {
		in := make([]item, n)
		for i := range in {
			in[i] = item{key: (i * 7919) % 97, seq: i}
		}
		want := slices.Clone(in)
		slices.SortStableFunc(want, func(a, b item) int { return a.key - b.key })
		got := slices.Clone(in)
		var since, most int
		ctx := &counting{Context: context.Background(), at: func() {
			most = max(most, since)
			since = 0
		}}
		err := SortStableFunc(ctx, got, func(a, b item) int {
			since++
			return a.key - b.key
		})
		most = max(most, since)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("%d items: %v, sorted %v", n, err, slices.Equal(got, want))
		}
		// slices.SortStableFunc over one run is an insertion sort of
		// blocks of 20 and log2(run/20) merges, each at most run
		// compares.
		if bound := 20 * checkEvery; most > bound {
			t.Errorf("%d items: %d compares between two checks, want at most %d", n, most, bound)
		}
	}
	stopsThroughout(t, "SortStableFunc", 70000, func(ctx context.Context) error {
		s := make([]int, 70000)
		for i := range s {
			s[i] = (i * 7919) % 70001
		}
		return SortStableFunc(ctx, s, func(a, b int) int { return a - b })
	})
}

// counting is a context that calls at on each Err and never ends.
type counting struct {
	context.Context
	at func()
}

func (c *counting) Err() error {
	c.at()
	return nil
}
