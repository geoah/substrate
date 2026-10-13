package engine

import (
	"context"
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
	}})
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

// Hour and minute buckets in a zone hold every instant of their range across
// a repeated hour: Europe/Athens repeats 03:00 to 04:00 on 2026-10-25, and a
// read starting in its first pass (03:30+03:00, 00:30Z) starts its first
// bucket there, not an hour later at the second pass. New York, west of UTC,
// repeats 01:00 on 2026-11-01.
func TestAggregateCalendarHourAndMinuteAcrossARepeatedHour(t *testing.T) {
	for _, x := range []struct {
		calendar, gte, lt, zone string
		buckets                 int
	}{
		{"hour", "2026-10-25T03:30:00+03:00", "2026-10-25T06:00:00+02:00", "Europe/Athens", 4},
		{"hour", "2026-10-25T03:00:00+03:00", "2026-10-25T05:00:00+02:00", "Europe/Athens", 3},
		{"minute", "2026-10-25T03:10:00+03:00", "2026-10-25T03:20:00+02:00", "Europe/Athens", 70},
		{"hour", "2026-11-01T01:30:00-04:00", "2026-11-01T03:00:00-05:00", "America/New_York", 3},
		{"minute", "2026-11-01T01:50:00-04:00", "2026-11-01T01:10:00-05:00", "America/New_York", 20},
	} {
		label := x.zone + " " + x.calendar + " from " + x.gte
		p, bs := planBuckets(t, x.gte, x.lt, substrate.AggregateBucket{Calendar: x.calendar, Timezone: x.zone})
		partitions(t, label, p, bs)
		if len(bs) != x.buckets {
			t.Errorf("%s: %d buckets, want %d", label, len(bs), x.buckets)
		}
		for i, b := range bs[1:] {
			w := b.start.In(p.bucket.loc)
			if w.Second() != 0 || (x.calendar == "hour" && w.Minute() != 0) {
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
		{"maxRecords", substrate.Aggregate{Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{MaxRecords: -5}}}},
	} {
		a := x.a
		a.Metric = &substrate.AggregateMetric{Ref: "a.example.com/observations/metric/m"}
		a.Range = &substrate.AggregateRange{Gte: "2026-09-01T00:00:00Z", Lt: "2026-09-02T00:00:00Z"}
		a.Reducers = []substrate.AggregateReducer{{Name: substrate.ReducerCount}}
		_, err := planAggregate(substrate.AggregateQuery{Aggregate: &a})
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
	r := &aggRun{p: &aggPlan{bucket: bucketSpec{mode: bucketRecords}}, buckets: windows}
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
		if got, w := r.bucketsMeeting(from, to), want(from, to); !slices.Equal(got, w) {
			t.Fatalf("[%s, %s): %d windows, want %d", from.Format(time.RFC3339), to.Format(time.RFC3339), len(got), len(w))
		}
		r.p.from, r.p.to = start, start
		if got, w := r.bucketsOf(from, 0), want(from, from.Add(time.Nanosecond)); !slices.Equal(got, w) {
			t.Fatalf("a point at %s: %d windows, want %d", from.Format(time.RFC3339), len(got), len(w))
		}
	}
}
