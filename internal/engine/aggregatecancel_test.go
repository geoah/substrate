package engine

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/vocabulary"
)

// endingContext answers nil from Err for its first n calls and
// context.Canceled after, counting them: a context that ends at a known
// check inside the work.
type endingContext struct {
	context.Context
	n, calls int
}

func (c *endingContext) Err() error {
	c.calls++
	if c.calls > c.n {
		return context.Canceled
	}
	return nil
}

// stopsAtTheNextCheck runs work under a context that never ends, then under
// contexts that end after its first check, half way through and before its
// last, and wants context.Canceled at the very check after the end each
// time. It answers how many checks the whole work made.
func stopsAtTheNextCheck(t *testing.T, name string, work func(ctx context.Context) error) int {
	t.Helper()
	whole := &endingContext{Context: context.Background(), n: math.MaxInt}
	if err := work(whole); err != nil {
		t.Fatalf("%s: %v under a context that never ends", name, err)
	}
	for _, k := range []int{1, whole.calls / 2, whole.calls - 1} {
		c := &endingContext{Context: context.Background(), n: k}
		if err := work(c); !errors.Is(err, context.Canceled) || c.calls != k+1 {
			t.Errorf("%s under a context that ends after check %d of %d: %v at check %d, want context.Canceled at check %d",
				name, k, whole.calls, err, c.calls, k+1)
		}
	}
	return whole.calls
}

// sortChecks is how many checks SortStableFunc makes sorting n intervals in
// the order the work hands them over.
func sortChecks(t *testing.T, spans [][2]time.Time) int {
	t.Helper()
	ctx := &endingContext{Context: context.Background(), n: math.MaxInt}
	if err := observed.SortStableFunc(ctx, slices.Clone(spans), func(a, b [2]time.Time) int { return a[0].Compare(b[0]) }); err != nil {
		t.Fatal(err)
	}
	return ctx.calls
}

// An amount's coverage and the summaries policy's choice of source pass over
// a cell's whole input, and each checks the read's context at least once per
// thousand elements it passes, stopping at the first check after the context
// ends: 100,000 disjoint extents of one bucket, which amountCoverage once
// joined with no check between its cell check and the next cell; the
// members of a summary bucket from two sources, which the source choice once
// scanned and filtered with none; and a raw amount's members, whose joined
// cover was summed with none.
func TestAggregateCoverageAndSourceChoiceStopWhereTheirContextEnds(t *testing.T) {
	const n = 100000
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	bucket := &aggBucket{start: start, end: start.Add(n * 15 * time.Minute)}
	// slot is the k-th quarter hour of the bucket, visited out of order.
	slot := func(i int) time.Time { return start.Add(time.Duration((i*7919)%n) * 15 * time.Minute) }
	run := func(ctx context.Context) *aggRun {
		return &aggRun{ctx: ctx, p: &aggPlan{}, buckets: []*aggBucket{bucket}}
	}

	extents := make([]observed.Extent, n)
	for i := range extents {
		at := slot(i)
		extents[i] = observed.Extent{Start: at, End: at.Add(5 * time.Minute), Ms: (5 * time.Minute).Milliseconds()}
	}
	checks := stopsAtTheNextCheck(t, "amountCoverage", func(ctx context.Context) error {
		ev := newEvidence(vocabulary.FormAmount, false)
		ev.extents = extents
		if err := run(ctx).amountCoverage(&aggGroup{}, aggCells{bucket: {"": ev}}); err != nil {
			return err
		}
		if want := int64(n) * (5 * time.Minute).Milliseconds(); ev.coverage != want {
			t.Fatalf("amountCoverage: %d ms, want %d", ev.coverage, want)
		}
		return nil
	})
	if want := 2 * n / 1024; checks < want {
		t.Errorf("amountCoverage checks its context %d times over %d extents, want at least %d: one per thousand copied and walked", checks, n, want)
	}

	streams := []*aggStream{{obsStream: obsStream{source: "first"}}, {obsStream: obsStream{source: "second"}}}
	checks = stopsAtTheNextCheck(t, "firstSummarySource", func(ctx context.Context) error {
		ev := newEvidence(vocabulary.FormSummary, true)
		for i := range n {
			ev.members = append(ev.members, aggPoint{Point: observed.Point{At: slot(i)}, stream: 1 - i%2})
		}
		if err := run(ctx).firstSummarySource(ev, streams); err != nil {
			return err
		}
		if len(ev.members) != n/2 || !ev.sources["first"] || len(ev.sources) != 1 {
			t.Fatalf("firstSummarySource kept %d members from %v, want %d from the first", len(ev.members), ev.sources, n/2)
		}
		return nil
	})
	if want := 2 * n / 1024; checks < want {
		t.Errorf("firstSummarySource checks its context %d times over %d members, want at least %d: one per thousand scanned and filtered", checks, n, want)
	}

	members := make([]aggPoint, n)
	spans := make([][2]time.Time, n)
	for i := range members {
		at := slot(i)
		members[i] = aggPoint{Point: observed.Point{At: at, End: at.Add(5 * time.Minute)}}
		spans[i] = [2]time.Time{at, at.Add(5 * time.Minute)}
	}
	checks = stopsAtTheNextCheck(t, "memberCoverage", func(ctx context.Context) error {
		ev := newEvidence(vocabulary.FormAmount, true)
		ev.members = members
		ms, err := run(ctx).memberCoverage(ev)
		if err != nil {
			return err
		}
		if want := int64(n) * (5 * time.Minute).Milliseconds(); ms != want {
			t.Fatalf("memberCoverage: %d ms, want %d", ms, want)
		}
		return nil
	})
	if sorted, want := sortChecks(t, spans), 3*n/1024; checks < sorted+want {
		t.Errorf("memberCoverage checks its context %d times over %d members, want at least %d: its sort's %d, and one per thousand spans built, joined and summed",
			checks, n, sorted+want, sorted)
	}
}
