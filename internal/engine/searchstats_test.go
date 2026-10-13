package engine

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// The statistics refresh reads a sample of the repository, so it must not
// hold the lock every lexical search takes: a caller holding stale
// statistics answers them while another caller reads, and a caller holding
// none waits for that read rather than starting its own.
func TestSearchStatsRefreshDoesNotHoldSearchesBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stale := searchStats{n: 1, at: time.Now().Add(-2 * statsTTL)}
	next := searchStats{n: 2, at: time.Now()}

	// blockingRead stands for a slow sample: it reports that it started and
	// returns next once released.
	blockingRead := func(reads *atomic.Int32, started, release chan struct{}) func(context.Context) (searchStats, error) {
		return func(context.Context) (searchStats, error) {
			reads.Add(1)
			close(started)
			<-release
			return next, nil
		}
	}

	t.Run("stale statistics answer while another caller reads", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{s: stale}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		refreshed := make(chan searchStats, 1)
		go func() {
			s, _ := c.get(ctx, blockingRead(&reads, started, release))
			refreshed <- s
		}()
		<-started
		answered := make(chan searchStats, 1)
		go func() {
			s, _ := c.get(ctx, func(context.Context) (searchStats, error) {
				reads.Add(1)
				return next, nil
			})
			answered <- s
		}()
		select {
		case s := <-answered:
			if s.n != stale.n {
				t.Fatalf("a caller during the refresh answered n=%v, want the held n=%v", s.n, stale.n)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a caller holding stale statistics waited on another caller's refresh")
		}
		close(release)
		if s := <-refreshed; s.n != next.n {
			t.Fatalf("the refreshing caller answered n=%v, want the read's n=%v", s.n, next.n)
		}
		if got := reads.Load(); got != 1 {
			t.Fatalf("%d reads, want the one refresh", got)
		}
		if s, _ := c.get(ctx, nil); s.n != next.n {
			t.Fatalf("after the refresh the cache answered n=%v, want n=%v", s.n, next.n)
		}
	})

	t.Run("a caller with nothing held waits for the read in flight", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		go func() { _, _ = c.get(ctx, blockingRead(&reads, started, release)) }()
		<-started
		answered := make(chan searchStats, 1)
		go func() {
			s, _ := c.get(ctx, func(context.Context) (searchStats, error) {
				reads.Add(1)
				return searchStats{n: 3, at: time.Now()}, nil
			})
			answered <- s
		}()
		select {
		case s := <-answered:
			t.Fatalf("a caller with nothing held answered n=%v before the read finished", s.n)
		case <-time.After(100 * time.Millisecond):
		}
		close(release)
		if s := <-answered; s.n != next.n {
			t.Fatalf("the waiting caller answered n=%v, want the finished read's n=%v", s.n, next.n)
		}
		if got := reads.Load(); got != 1 {
			t.Fatalf("%d reads, want one: the waiting caller found the statistics fresh", got)
		}
	})

	t.Run("a failed read answers its error and keeps nothing", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{}
		boom := errors.New("boom")
		if _, err := c.get(ctx, func(context.Context) (searchStats, error) { return searchStats{}, boom }); !errors.Is(err, boom) {
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
