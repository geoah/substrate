package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A NEW REPOSITORY DOES NOT WAIT BEHIND A BUSY ONE (#639). The control plane
// lists repositories oldest first, so a repository registered today is the
// last one a serial walk reaches: while an older repository's pass is still
// draining a backlog, the new repository's triggers sit at their lag until
// somebody wakes them by hand. The busy repository here never finishes its
// pass while the test watches; the new one must still be dispatched, more
// than once, within a few dispatcher intervals.
func TestANewRepositoryDispatchesWhileAnotherRepositoryIsBusy(t *testing.T) {
	const interval = 10 * time.Millisecond
	release := make(chan struct{})
	busyStarted := make(chan struct{})
	var busyOnce sync.Once
	fresh := make(chan struct{}, 64)
	svc := &dispatchService{datasets: []*dispatchDataset{
		{id: "busy.localhost", process: func(ctx context.Context) {
			busyOnce.Do(func() { close(busyStarted) })
			select {
			case <-release:
			case <-ctx.Done():
			}
		}},
		{id: "new.localhost", process: func(context.Context) {
			select {
			case fresh <- struct{}{}:
			default:
			}
		}},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() {
		close(release)
		cancel()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		loop(ctx, "trigger dispatch", interval, newTriggerDispatcher(svc, &wg).dispatch)
	}()

	select {
	case <-busyStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the busy repository was never dispatched")
	}
	for i := range 2 {
		select {
		case <-fresh:
		case <-time.After(50 * interval):
			t.Fatalf("the new repository's pass %d did not run while an older repository's pass was still draining", i+1)
		}
	}
	if n := svc.datasets[0].calls(); n != 1 {
		t.Fatalf("the busy repository was dispatched %d times while its first pass ran, want 1: a repository runs one pass at a time", n)
	}
}

// AT MOST triggerDispatchPasses REPOSITORY PASSES RUN AT ONCE. Each pass wants
// a connection and a runner process, so a host with hundreds of repositories
// must not start hundreds of passes on one tick. Every repository here holds
// its pass for a few intervals; the high-water mark of passes in flight stays
// at the cap, and every repository still gets its turn.
func TestTheDispatcherRunsAtMostTheCapOfRepositoryPasses(t *testing.T) {
	const interval = 5 * time.Millisecond
	var mu sync.Mutex
	inFlight, highWater := 0, 0
	repositories := 3 * triggerDispatchPasses
	seen := make(chan string, 16*repositories)
	svc := &dispatchService{}
	for i := range repositories {
		id := fmt.Sprintf("r%02d.localhost", i)
		svc.datasets = append(svc.datasets, &dispatchDataset{id: id, process: func(ctx context.Context) {
			mu.Lock()
			inFlight++
			highWater = max(highWater, inFlight)
			mu.Unlock()
			select {
			case <-time.After(3 * interval):
			case <-ctx.Done():
			}
			mu.Lock()
			inFlight--
			mu.Unlock()
			select {
			case seen <- id:
			default:
			}
		}})
	}
	stop := runDispatcher(t, svc, interval)

	pending := map[string]bool{}
	for _, ds := range svc.datasets {
		pending[ds.id] = true
	}
	deadline := time.After(10 * time.Second)
	for len(pending) > 0 {
		select {
		case id := <-seen:
			delete(pending, id)
		case <-deadline:
			t.Fatalf("%d repositories never got a pass: %v", len(pending), pending)
		}
	}
	stop()
	mu.Lock()
	defer mu.Unlock()
	if highWater > triggerDispatchPasses {
		t.Fatalf("%d repository passes ran at once, want at most %d", highWater, triggerDispatchPasses)
	}
	if highWater < 2 {
		t.Fatalf("at most %d repository pass ran at once: the dispatcher did not run passes side by side", highWater)
	}
}

// A NEW REPOSITORY IS NOT STARVED BY A FULL CAP. As many older repositories as
// the cap allows always have work: each pass ends after a few intervals and
// the repository wants another. A tick that filled its free slots from the
// oldest repository down would hand every freed slot back to them and never
// reach the newest; the dispatcher starts the repository that has waited
// longest instead, so the new one gets a pass within a few ticks.
func TestANewRepositoryGetsAPassWhileTheCapIsBusy(t *testing.T) {
	const interval = 5 * time.Millisecond
	fresh := make(chan struct{}, 64)
	svc := &dispatchService{}
	for i := range triggerDispatchPasses {
		svc.datasets = append(svc.datasets, &dispatchDataset{
			id: fmt.Sprintf("old%02d.localhost", i),
			process: func(ctx context.Context) {
				select {
				case <-time.After(4 * interval):
				case <-ctx.Done():
				}
			},
		})
	}
	svc.datasets = append(svc.datasets, &dispatchDataset{id: "new.localhost", process: func(context.Context) {
		select {
		case fresh <- struct{}{}:
		default:
		}
	}})
	runDispatcher(t, svc, interval)

	for i := range 2 {
		select {
		case <-fresh:
		case <-time.After(100 * interval):
			t.Fatalf("the new repository's pass %d did not run while %d older repositories kept the cap busy", i+1, triggerDispatchPasses)
		}
	}
}

// EVERY IDLE REPOSITORY GETS A PASS PER TICK. The cap bounds how many passes
// run at once, not how many a tick hands out: with many more repositories
// than slots and passes that return at once, each repository still gets a
// pass on (almost) every tick, as it did under the serial walk. A dispatcher
// that only filled its slots on the tick would give each of the 80 here one
// pass every ten ticks.
func TestEveryIdleRepositoryGetsAPassPerTick(t *testing.T) {
	const (
		interval     = 20 * time.Millisecond
		repositories = 80
	)
	svc := &dispatchService{}
	for i := range repositories {
		svc.datasets = append(svc.datasets, &dispatchDataset{
			id: fmt.Sprintf("idle%02d.localhost", i), process: func(context.Context) {},
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var ticks atomic.Int64
	d := newTriggerDispatcher(svc, &wg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		loop(ctx, "trigger dispatch", interval, func(ctx context.Context) {
			ticks.Add(1)
			d.dispatch(ctx)
		})
	}()
	time.Sleep(50 * interval)
	cancel()
	wg.Wait()

	n := int(ticks.Load())
	if n < 10 {
		t.Fatalf("only %d ticks in %s: the box is too loaded to measure", n, 50*interval)
	}
	for _, ds := range svc.datasets {
		if got := ds.calls(); got < n/2 {
			t.Fatalf("repository %s got %d passes over %d ticks, want one on most ticks", ds.id, got, n)
		}
	}
}

// A FAILED LISTING KEEPS THE TURN RECORDS, AND A CANCELED CONTEXT STARTS
// NOTHING. An error from the control plane is not an empty control plane:
// forgetting every repository's last start would put them all back at the
// head of the order as if they had never run. And a tick that lands after
// shutdown began must not start passes the barrier is about to wait out.
func TestTheDispatcherKeepsItsOrderOnAFailedListingAndStartsNothingOnShutdown(t *testing.T) {
	var wg sync.WaitGroup
	svc := &dispatchService{datasets: []*dispatchDataset{{id: "a.localhost", process: func(context.Context) {}}}}
	d := newTriggerDispatcher(svc, &wg)
	d.started["a.localhost"] = 7

	svc.fail = errors.New("the control plane is down")
	d.dispatch(context.Background())
	if d.started["a.localhost"] != 7 {
		t.Fatalf("a failed listing pruned the turn records: %v", d.started)
	}

	svc.fail = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.dispatch(ctx)
	wg.Wait()
	if n := svc.datasets[0].calls(); n != 0 {
		t.Fatalf("a tick on a canceled context ran %d passes", n)
	}
}

// A REPOSITORY LISTED TWICE GETS ONE PASS. The dispatcher's one-pass-per-
// repository rule is its own to hold, whatever the listing answers: two
// queued passes for one repository would run side by side under the cap and
// race its cursors.
func TestARepositoryListedTwiceGetsOnePass(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	inFlight, highWater := 0, 0
	twice := &dispatchDataset{id: "twice.localhost", process: func(ctx context.Context) {
		mu.Lock()
		inFlight++
		highWater = max(highWater, inFlight)
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
	}}
	svc := &dispatchService{datasets: []*dispatchDataset{twice, twice}}
	var wg sync.WaitGroup
	d := newTriggerDispatcher(svc, &wg)
	d.dispatch(context.Background())
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := twice.calls(); n != 1 {
		t.Fatalf("a repository listed twice ran %d passes on one tick, want 1", n)
	}
	if highWater != 1 {
		t.Fatalf("%d passes of one repository ran at once", highWater)
	}
}

// runDispatcher runs the dispatch loop until the returned stop, which the
// test's cleanup also calls: it cancels and waits for every pass in flight.
func runDispatcher(t *testing.T, svc substrate.Service, interval time.Duration) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		loop(ctx, "trigger dispatch", interval, newTriggerDispatcher(svc, &wg).dispatch)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
	t.Cleanup(stop)
	return stop
}

// dispatchService is the two calls the dispatcher makes of a service: the
// listing, oldest first as the control plane answers it, and the open.
type dispatchService struct {
	substrate.Service
	datasets []*dispatchDataset
	fail     error
}

func (s *dispatchService) Repositories(context.Context) ([]substrate.RepositoryInfo, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	out := make([]substrate.RepositoryInfo, 0, len(s.datasets))
	for _, ds := range s.datasets {
		out = append(out, substrate.RepositoryInfo{ID: ds.id, Authority: ds.id})
	}
	return out, nil
}

func (s *dispatchService) Dataset(_ context.Context, id string) (substrate.Dataset, error) {
	for _, ds := range s.datasets {
		if ds.id == id {
			return ds, nil
		}
	}
	return nil, substrate.ErrNotFound
}

// dispatchDataset is a repository whose dispatcher pass is a test's function.
type dispatchDataset struct {
	substrate.Dataset
	id      string
	process func(context.Context)
	mu      sync.Mutex
	n       int
}

func (d *dispatchDataset) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: d.id, Authority: d.id}
}

func (d *dispatchDataset) ProcessTriggers(ctx context.Context) (int, error) {
	d.mu.Lock()
	d.n++
	d.mu.Unlock()
	d.process(ctx)
	return 0, nil
}

func (d *dispatchDataset) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}
