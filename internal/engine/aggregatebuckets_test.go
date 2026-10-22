package engine

import (
	"context"
	"math/bits"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// planBuckets plans an aggregate read over [gte, lt) with the bucket and
// enumerates its buckets, which reads no repository outside the records mode.
func planBuckets(t *testing.T, gte, lt string, bucket substrate.AggregateBucket) (*aggPlan, []*aggBucket) {
	t.Helper()
	p, err := planAggregate(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: "a.example.com/observations/metric/m"},
		Range:    &substrate.AggregateRange{Gte: gte, Lt: lt},
		Bucket:   &bucket,
		Reducers: []substrate.AggregateReducer{{Name: substrate.ReducerCount}},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := (&dataset{}).buckets(context.Background(), nil, p)
	if err != nil {
		t.Fatal(err)
	}
	return p, bs
}

// partitions fails unless the buckets cut [from, to) into consecutive
// pieces, each starting where the one before it ends.
func partitions(t *testing.T, label string, p *aggPlan, bs []*aggBucket) {
	t.Helper()
	if len(bs) == 0 {
		t.Fatalf("%s: no bucket", label)
	}
	at := p.from
	for i, b := range bs {
		if !b.start.Equal(at) {
			t.Errorf("%s: bucket %d starts at %s, want %s, where the one before it ends", label, i,
				b.start.UTC().Format(time.RFC3339), at.UTC().Format(time.RFC3339))
		}
		at = b.end
	}
	if !at.Equal(p.to) {
		t.Errorf("%s: the buckets end at %s, want the range's end %s", label, at.UTC().Format(time.RFC3339), p.to.UTC().Format(time.RFC3339))
	}
}

// Hour buckets in a zone hold every instant of their range across
// a repeated hour: Europe/Athens repeats 03:00 to 04:00 on 2026-10-25, and a
// read starting in its first pass (03:30+03:00, 00:30Z) starts its first
// bucket there, not an hour later at the second pass. New York, west of UTC,
// repeats 01:00 on 2026-11-01.
func TestAggregateCalendarHourAcrossARepeatedHour(t *testing.T) {
	for _, x := range []struct {
		calendar, gte, lt, zone string
		buckets                 int
	}{
		{"hour", "2026-10-25T03:30:00+03:00", "2026-10-25T06:00:00+02:00", "Europe/Athens", 4},
		{"hour", "2026-10-25T03:00:00+03:00", "2026-10-25T05:00:00+02:00", "Europe/Athens", 3},
		{"hour", "2026-11-01T01:30:00-04:00", "2026-11-01T03:00:00-05:00", "America/New_York", 3},
	} {
		label := x.zone + " " + x.calendar + " from " + x.gte
		p, bs := planBuckets(t, x.gte, x.lt, substrate.AggregateBucket{Calendar: x.calendar, Timezone: x.zone})
		partitions(t, label, p, bs)
		if len(bs) != x.buckets {
			t.Errorf("%s: %d buckets, want %d", label, len(bs), x.buckets)
		}
		for i, b := range bs[1:] {
			w := b.start.In(p.bucket.loc)
			if w.Second() != 0 || w.Minute() != 0 {
				t.Errorf("%s: bucket %d starts at %s, inside a wall-clock %s", label, i+1, w.Format(time.RFC3339), x.calendar)
			}
		}
	}
}

// A negative budget is refused as negative, not as over its cap.
func TestAggregateRefusesANegativeBudget(t *testing.T) {
	for _, x := range []struct {
		name string
		a    substrate.Aggregate
	}{
		{"maxBuckets", substrate.Aggregate{Budget: &substrate.AggregateBudget{MaxBuckets: -5}}},
		{"maxSamples", substrate.Aggregate{Budget: &substrate.AggregateBudget{MaxSamples: -5}}},
		{"maxGroups", substrate.Aggregate{Budget: &substrate.AggregateBudget{MaxGroups: -5}}},
	} {
		a := x.a
		a.Metric = &substrate.AggregateMetric{Ref: "a.example.com/observations/metric/m"}
		a.Range = &substrate.AggregateRange{Gte: "2026-09-01T00:00:00Z", Lt: "2026-09-02T00:00:00Z"}
		a.Reducers = []substrate.AggregateReducer{{Name: substrate.ReducerCount}}
		_, err := planAggregate(substrate.AggregateQuery{Aggregate: &a}, false)
		if err == nil || !strings.Contains(err.Error(), x.name+": -5 is negative") {
			t.Errorf("%s -5: %v, want it refused as negative", x.name, err)
		}
	}
}

// An every bucket's origin may lie centuries from the range: the buckets are
// counted from the origin in whole steps, which a time.Duration of the
// distance (292 years at most) cannot hold.
func TestAggregateEveryFromAFarOrigin(t *testing.T) {
	for _, origin := range []string{"1700-01-01T00:00:00Z", "1700-01-01T06:00:00Z", "2400-01-01T00:00:00Z"} {
		p, bs := planBuckets(t, "2026-09-01T00:00:00Z", "2026-09-04T00:00:00Z", substrate.AggregateBucket{Every: "P1D", Origin: origin})
		if len(bs) == 0 {
			t.Fatalf("origin %s: no bucket", origin)
		}
		partitions(t, "origin "+origin, p, bs)
		o, _ := time.Parse(time.RFC3339, origin)
		want := 3
		if o.Hour() != 0 {
			want = 4
		}
		if len(bs) != want {
			t.Errorf("origin %s: %d buckets, want %d", origin, len(bs), want)
		}
		for i, b := range bs[1:] {
			if b.start.Hour() != o.Hour() {
				t.Errorf("origin %s: bucket %d starts at %s, off the origin's whole days", origin, i+1, b.start.Format(time.RFC3339))
			}
		}
	}
}

// A point or a span is placed in every records-mode window that meets it,
// however the windows overlap: 400 windows of lengths from a minute to a day,
// sorted by start as the records are, against every window checked one by
// one.
func TestAggregateRecordWindowsMeetingOverlap(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rnd := rand.New(rand.NewPCG(1, 2))
	var windows []*aggBucket
	at := start
	for range 400 {
		at = at.Add(time.Duration(rnd.IntN(90)) * time.Minute)
		length := time.Duration(1+rnd.IntN(24*60)) * time.Minute
		windows = append(windows, &aggBucket{start: at, end: at.Add(length), zone: time.UTC})
	}
	r := &aggRun{ctx: context.Background(), p: &aggPlan{bucket: bucketSpec{mode: bucketRecords}}, buckets: windows}
	want := func(from, to time.Time) []*aggBucket {
		var out []*aggBucket
		for _, b := range windows {
			if b.start.Before(to) && b.end.After(from) {
				out = append(out, b)
			}
		}
		return out
	}
	for range 2000 {
		from := start.Add(time.Duration(rnd.IntN(700*60)) * time.Minute)
		to := from.Add(time.Duration(rnd.IntN(120)) * time.Minute)
		got, err := r.bucketsMeeting(from, to)
		if w := want(from, to); err != nil || !slices.Equal(got, w) {
			t.Fatalf("[%s, %s): %d windows (%v), want %d", from.Format(time.RFC3339), to.Format(time.RFC3339), len(got), err, len(w))
		}
		r.p.from, r.p.to = start, start
		got, err = r.bucketsOf(from, 0)
		if w := want(from, from.Add(time.Nanosecond)); err != nil || !slices.Equal(got, w) {
			t.Fatalf("a point at %s: %d windows (%v), want %d", from.Format(time.RFC3339), len(got), err, len(w))
		}
	}
}

// A point is placed among records-mode windows in steps that grow with the
// windows it meets and the depth of a tree over them, not with the windows
// before it: one window over all 10,000 minutes, whose end is every later
// window's running latest end, and 2,000 ten-minute windows every five
// minutes, under 20,000 points every 30 seconds, each meeting three. A scan
// from the first window that has not ended takes about a thousand steps per
// point here.
func TestAggregateRecordWindowsTakeStepsInTheWindowsTheyMeet(t *testing.T) {
	const n = 20000
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	windows := []*aggBucket{{start: start, end: start.Add(10000 * time.Minute), zone: time.UTC}}
	for i := range 2000 {
		at := start.Add(time.Duration(i) * 5 * time.Minute)
		windows = append(windows, &aggBucket{start: at, end: at.Add(10 * time.Minute), zone: time.UTC})
	}
	r := &aggRun{ctx: context.Background(), p: &aggPlan{bucket: bucketSpec{mode: bucketRecords}}, buckets: windows}
	met := 0
	for i := range n {
		at := start.Add(time.Duration(i)*30*time.Second + time.Second)
		bs, err := r.bucketsOf(at, 0)
		if err != nil {
			t.Fatal(err)
		}
		if want := min(3, 2+i/10); len(bs) != want {
			t.Fatalf("a point at %s meets %d windows, want %d", at.Format(time.RFC3339), len(bs), want)
		}
		met += len(bs)
	}
	depth := bits.Len(uint(len(windows))) + 1
	if limit := len(windows) + 2*depth*(met+n); int(r.steps) > limit {
		t.Errorf("%d steps placing %d points among %d windows, want at most %d: each point a walk down the tree to the %d windows met",
			r.steps, n, len(windows), limit, met)
	}
}

// A point or a record is placed among disjoint buckets, the property read's
// and every UTC bucket mode's, in steps that grow with the log of the
// buckets: 10,000 instants over 10,000 one-minute buckets take at most 15
// comparisons each, where a scan takes 5,000 on average.
func TestAggregateBucketSearchComparesLogBuckets(t *testing.T) {
	const n = 10000
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var buckets []*aggBucket
	for i := range n {
		at := start.Add(time.Duration(i) * time.Minute)
		buckets = append(buckets, &aggBucket{start: at, end: at.Add(time.Minute), zone: time.UTC})
	}
	steps := 0
	count := func() error { steps++; return nil }
	for i := range n + 1 {
		at := start.Add(time.Duration(i)*time.Minute + 30*time.Second)
		got, err := firstEndingAfter(buckets, at, count)
		if want := min(i, n); err != nil || got != want {
			t.Fatalf("an instant at %s: bucket %d (%v), want %d", at.Format(time.RFC3339), got, err, want)
		}
	}
	if limit := (n + 1) * (bits.Len(uint(n)) + 1); steps > limit {
		t.Errorf("%d comparisons placing %d instants among %d buckets, want at most %d", steps, n+1, n, limit)
	}
}

// A point is placed among a UTC bucket mode's buckets by bucketsOf in steps
// that grow with the log of the buckets, counted by the read's own step
// counter: 10,000 points over 10,000 one-minute buckets, where a scan from
// the first bucket takes 5,000 steps a point on average.
func TestAggregateUTCBucketsPlaceAPointInLogSteps(t *testing.T) {
	const n = 10000
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(n * time.Minute)
	var buckets []*aggBucket
	for i := range n {
		at := start.Add(time.Duration(i) * time.Minute)
		buckets = append(buckets, &aggBucket{start: at, end: at.Add(time.Minute), zone: time.UTC})
	}
	r := &aggRun{ctx: context.Background(), p: &aggPlan{bucket: bucketSpec{mode: bucketEvery}, from: start, readFrom: start, to: end}, buckets: buckets}
	for i := range n {
		at := start.Add(time.Duration(i)*time.Minute + 30*time.Second)
		got, err := r.bucketsOf(at, 0)
		if err != nil || len(got) != 1 || got[0] != buckets[i] {
			t.Fatalf("a point at %s: %d buckets (%v), want bucket %d", at.Format(time.RFC3339), len(got), err, i)
		}
	}
	if limit := n * (bits.Len(uint(n)) + 1); int(r.steps) > limit {
		t.Errorf("%d steps placing %d points among %d buckets, want at most %d", r.steps, n, n, limit)
	}
}
