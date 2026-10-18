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
// search's whole budget, so it runs as a background task, one at a time: a
// caller holding stale statistics answers them at once, a caller holding
// none waits a moment and then answers none, and the read lands for the next
// caller even when the caller that started it has gone. No outcome of the
// read fails a search.
func TestSearchStatsRefreshDoesNotHoldSearchesBack(t *testing.T) {
	t.Parallel()
	stale := searchStats{n: 1, at: time.Now().Add(-2 * statsTTL)}
	next := searchStats{n: 2, at: time.Now()}

	// spawn stands for dataset.spawn: a goroutine on a background context,
	// with the panic containment the service gives every task.
	spawn := func(fn func(context.Context)) bool {
		go func() {
			defer func() { _ = recover() }()
			fn(context.Background())
		}()
		return true
	}
	noWarn := func(t *testing.T) func(error) {
		return func(err error) { t.Errorf("unexpected warning: %v", err) }
	}
	// blockingRead stands for a slow sample: it reports that it started and
	// returns next once released.
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
	countingRead := func(reads *atomic.Int32) func(context.Context) (searchStats, error) {
		return func(context.Context) (searchStats, error) {
			reads.Add(1)
			return next, nil
		}
	}
	// settled waits for the refresh in flight, if any, to end.
	settled := func(t *testing.T, c *searchStatsCache) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			c.mu.Lock()
			r := c.refreshing
			c.mu.Unlock()
			if r == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the refresh never ended")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	t.Run("stale statistics answer at once and one read refreshes them", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{s: stale}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		if s := c.get(context.Background(), blockingRead(&reads, started, release), spawn, noWarn(t)); s.n != stale.n {
			t.Fatalf("get = n=%v, want the held n=%v at once", s.n, stale.n)
		}
		<-started
		if s := c.get(context.Background(), countingRead(&reads), spawn, noWarn(t)); s.n != stale.n {
			t.Fatalf("a caller during the refresh answered n=%v, want the held n=%v", s.n, stale.n)
		}
		close(release)
		settled(t, c)
		if got := reads.Load(); got != 1 {
			t.Fatalf("%d reads, want the one refresh", got)
		}
		if s := c.get(context.Background(), nil, nil, nil); s.n != next.n {
			t.Fatalf("after the refresh the cache answered n=%v, want n=%v", s.n, next.n)
		}
	})

	t.Run("callers with nothing held share the read that lands in time", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 5 * time.Second}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		first := make(chan searchStats, 1)
		go func() { first <- c.get(context.Background(), blockingRead(&reads, started, release), spawn, noWarn(t)) }()
		<-started
		second := make(chan searchStats, 1)
		go func() { second <- c.get(context.Background(), countingRead(&reads), spawn, noWarn(t)) }()
		time.Sleep(20 * time.Millisecond)
		close(release)
		for _, ch := range []chan searchStats{first, second} {
			if s := <-ch; s.n != next.n {
				t.Fatalf("a waiting caller answered n=%v, want the read's n=%v", s.n, next.n)
			}
		}
		if got := reads.Load(); got != 1 {
			t.Fatalf("%d reads, want one shared read", got)
		}
	})

	t.Run("a slow read outlives the search that started it", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 20 * time.Millisecond}
		var reads atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		if s := c.get(ctx, blockingRead(&reads, started, release), spawn, noWarn(t)); !s.at.IsZero() {
			t.Fatalf("get = %+v, want no statistics after the wait", s)
		}
		cancel()
		<-started
		close(release)
		settled(t, c)
		if s := c.held(); s.n != next.n {
			t.Fatalf("the cache holds n=%v, want the read's n=%v", s.n, next.n)
		}
	})

	t.Run("a failed read warns, answers what was held and keeps it", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		failing := func(context.Context) (searchStats, error) { return searchStats{}, boom }
		var warned atomic.Int32
		warn := func(err error) {
			if errors.Is(err, boom) {
				warned.Add(1)
			}
		}
		c := &searchStatsCache{wait: 5 * time.Second}
		if s := c.get(context.Background(), failing, spawn, warn); !s.at.IsZero() {
			t.Fatalf("with nothing held a failed read answered %+v, want none", s)
		}
		c = &searchStatsCache{s: stale}
		if s := c.get(context.Background(), failing, spawn, warn); s.n != stale.n {
			t.Fatalf("with stale statistics held a failed read answered n=%v, want n=%v", s.n, stale.n)
		}
		settled(t, c)
		if s := c.held(); s.n != stale.n {
			t.Fatalf("a failed read replaced the held statistics with n=%v", s.n)
		}
		if got := warned.Load(); got != 2 {
			t.Fatalf("%d warnings, want one per failed read", got)
		}
	})

	t.Run("a refused spawn reads nothing and leaves no refresh open", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 5 * time.Second}
		var reads atomic.Int32
		refuse := func(func(context.Context)) bool { return false }
		began := time.Now()
		if s := c.get(context.Background(), countingRead(&reads), refuse, noWarn(t)); !s.at.IsZero() {
			t.Fatalf("get = %+v, want none", s)
		}
		if waited := time.Since(began); waited > time.Second {
			t.Fatalf("a refused spawn waited %v for a read that never started", waited)
		}
		if s := c.get(context.Background(), countingRead(&reads), spawn, noWarn(t)); s.n != next.n {
			t.Fatalf("the next caller answered n=%v, want a fresh read's n=%v", s.n, next.n)
		}
	})

	t.Run("a panicking read ends its refresh", func(t *testing.T) {
		t.Parallel()
		c := &searchStatsCache{wait: 5 * time.Second}
		panicking := func(context.Context) (searchStats, error) { panic("boom") }
		_ = c.get(context.Background(), panicking, spawn, noWarn(t))
		settled(t, c)
		var reads atomic.Int32
		if s := c.get(context.Background(), countingRead(&reads), spawn, noWarn(t)); s.n != next.n || reads.Load() != 1 {
			t.Fatalf("after a panic the next caller answered n=%v with %d reads, want a fresh read", s.n, reads.Load())
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
