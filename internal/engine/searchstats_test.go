package engine

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// The statistics read samples the repository and can take longer than a
// search's whole budget, so it runs on a context of its own, one at a time:
// a caller holding stale statistics answers them at once, a caller holding
// none waits a moment and then answers none, and the read lands for the
// next caller even when the caller that started it has gone.
func TestSearchStatsRefreshDoesNotHoldSearchesBack(t *testing.T) {
	t.Parallel()
	stale := searchStats{n: 1, at: time.Now().Add(-2 * statsTTL)}
	next := searchStats{n: 2, at: time.Now()}

	// blockingRead stands for a slow sample: it reports that it started and
	// returns next once released, or the error its context ended with.
	blockingRead := func(reads *atomic.Int32, started, release chan struct{}) func(context.Context) (searchStats, error) {
		return func(ctx context.Context) (searchStats, error) {
			reads.Add(1)
			close(started)
			select {
			case <-release:
				return next, nil
			case <-ctx.Done():
				return searchStats{}, ctx.Err()
			}
		}
	}
	// landed waits for the cache to hold want.
	landed := func(t *testing.T, c *searchStatsCache, want float64) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for c.held().n != want {
			if time.Now().After(deadline) {
				t.Fatalf("the cache holds n=%v, want n=%v", c.held().n, want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	t.Run("stale statistics answer at once and one read refreshes them", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{s: stale}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		s, err := c.get(context.Background(), blockingRead(&reads, started, release))
		if err != nil || s.n != stale.n {
			t.Fatalf("get = n=%v, %v; want the held n=%v at once", s.n, err, stale.n)
		}
		<-started
		s, _ = c.get(context.Background(), func(context.Context) (searchStats, error) {
			reads.Add(1)
			return next, nil
		})
		if s.n != stale.n {
			t.Fatalf("a caller during the refresh answered n=%v, want the held n=%v", s.n, stale.n)
		}
		close(release)
		landed(t, c, next.n)
		if got := reads.Load(); got != 1 {
			t.Fatalf("%d reads, want the one refresh", got)
		}
		if s, _ := c.get(context.Background(), nil); s.n != next.n {
			t.Fatalf("after the refresh the cache answered n=%v, want n=%v", s.n, next.n)
		}
	})

	t.Run("a caller with nothing held answers the read that lands in time", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 5 * time.Second}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		go func() { <-started; close(release) }()
		s, err := c.get(context.Background(), blockingRead(&reads, started, release))
		if err != nil || s.n != next.n {
			t.Fatalf("get = n=%v, %v; want the read's n=%v", s.n, err, next.n)
		}
	})

	t.Run("a slow read outlives the search that started it", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 20 * time.Millisecond}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		s, err := c.get(ctx, blockingRead(&reads, started, release))
		if err != nil || !s.at.IsZero() {
			t.Fatalf("get = %+v, %v; want no statistics and no error after the wait", s, err)
		}
		cancel()
		<-started
		close(release)
		landed(t, c, next.n)
		if got := reads.Load(); got != 1 {
			t.Fatalf("%d reads, want one", got)
		}
	})

	t.Run("a failed read answers its error and keeps nothing", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 5 * time.Second}
		boom := errors.New("boom")
		if _, err := c.get(context.Background(), func(context.Context) (searchStats, error) { return searchStats{}, boom }); !errors.Is(err, boom) {
			t.Fatalf("get = %v, want the read's error", err)
		}
		if s := c.held(); !s.at.IsZero() {
			t.Fatalf("a failed read left statistics held: %+v", s)
		}
	})
}

// The band averages come from a SYSTEM sample of the table's pages, read
// again with BERNOULLI when SYSTEM kept too few of the repository's rows,
// and from every row, without TABLESAMPLE, for a small repository.
func TestBandAveragesSampleMethod(t *testing.T) {
	t.Parallel()
	type call struct {
		method string
		pct    float64
	}
	run := func(n, systemRows int64, systemErr error) ([]call, [4]float64, error) {
		var calls []call
		avg, err := bandAverages(n, func(method string, pct float64) (int64, [4]float64, error) {
			calls = append(calls, call{method, pct})
			switch method {
			case sampleSystem:
				return systemRows, [4]float64{1}, systemErr
			case sampleBernoulli:
				return statsSample, [4]float64{2}, nil
			default:
				return n, [4]float64{3}, nil
			}
		})
		return calls, avg, err
	}

	for _, n := range []int64{1, statsSample} {
		calls, avg, err := run(n, 0, nil)
		if err != nil || !slices.Equal(calls, []call{{sampleEvery, 100}}) || avg[0] != 3 {
			t.Fatalf("n=%d read %v (avg %v, %v), want every row once", n, calls, avg, err)
		}
	}
	calls, avg, err := run(100*statsSample, statsSample/4, nil)
	if err != nil || !slices.Equal(calls, []call{{sampleSystem, 1}}) || avg[0] != 1 {
		t.Fatalf("a large repository read %v (avg %v, %v), want one SYSTEM sample of 1%%", calls, avg, err)
	}
	calls, avg, err = run(100*statsSample, statsSample/4-1, nil)
	if err != nil || !slices.Equal(calls, []call{{sampleSystem, 1}, {sampleBernoulli, 1}}) || avg[0] != 2 {
		t.Fatalf("a thin SYSTEM sample read %v (avg %v, %v), want it read again with BERNOULLI", calls, avg, err)
	}
	boom := errors.New("boom")
	calls, _, err = run(100*statsSample, 0, boom)
	if !errors.Is(err, boom) || len(calls) != 1 {
		t.Fatalf("a failed SYSTEM sample read %v and answered %v, want its error and no second read", calls, err)
	}
}
