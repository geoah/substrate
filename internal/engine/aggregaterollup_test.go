package engine

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An amount's bucket ends that cut its rollup rows are merged from the raw
// points in time proportional to the points and the buckets, not their
// product: 20,000 readings every 15 seconds under 5-minute buckets, each a
// cut end, take about one context check per thousand steps of that: no
// bucket scans the points of another.
func TestAggregateCutEndsTakeEachPointOnce(t *testing.T) {
	const n = 20000
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(n * 15 * time.Second)
	seg := aggSegment{from: start, to: end}
	for i := range n {
		at := start.Add(time.Duration(i) * 15 * time.Second)
		seg.points = append(seg.points, aggPoint{Point: observed.Point{
			At: at, End: at.Add(10 * time.Second), Value: 1, Exact: "1", HasValue: true,
		}})
	}
	var buckets []*aggBucket
	for at := start; at.Before(end); at = at.Add(5 * time.Minute) {
		buckets = append(buckets, &aggBucket{start: at, end: at.Add(5 * time.Minute), zone: time.UTC})
	}
	s := &aggStream{form: vocabulary.FormAmount}
	ctx := &endingContext{Context: context.Background(), n: math.MaxInt}
	r := &aggRun{
		ctx: ctx, p: &aggPlan{bucket: bucketSpec{mode: bucketEvery}, grouped: map[string]bool{}},
		u: &aggUnits{}, buckets: buckets, meta: aggMeta{sources: map[string]bool{}},
		series: map[*aggStream]*aggSeries{s: {segs: []aggSegment{seg}}},
	}
	out := aggCells{}
	if err := r.rollupFragments(out, &aggGroup{form: vocabulary.FormAmount, raw: []*aggStream{s}}, s, 0); err != nil {
		t.Fatal(err)
	}
	var count int64
	for _, b := range buckets {
		if ev := out[b][""]; ev != nil {
			count += ev.stats.N
		}
	}
	if count != n {
		t.Fatalf("the buckets hold %d points, want %d", count, n)
	}
	if limit := 4 * (n + len(buckets)) / 1024; ctx.calls > limit {
		t.Errorf("%d context checks over %d points in %d buckets, want at most %d: each point taken once", ctx.calls, n, len(buckets), limit)
	}
}
