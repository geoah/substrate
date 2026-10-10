package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// raceBundleActor is the race package's own declared hand: a background
// writer at the write gate.
var raceBundleActor = substrate.Actor(vocabulary.PackageActor(racePackage))

// backgroundEffect writes n widgets in one transaction under the race
// package's hand at the bundle tier, the shape a function's effects apply in
// (runner.go), and reports how long the transaction held the changelog lock:
// from the body's start to the commit's return.
func backgroundEffect(ctx context.Context, ds *dataset, n int, tag string) (time.Duration, error) {
	var held time.Time
	err := ds.inTx(ctx, raceBundleActor, false, func(t *txn) error {
		held = time.Now()
		t.setEffectEmit([]string{raceWidget})
		for i := range n {
			if _, err := t.put(substrate.PutInput{
				Kind: raceWidget, Properties: map[string]any{"name": fmt.Sprintf("%s %d", tag, i)},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return time.Since(held), err
}

func ownerSave(ctx context.Context, ds *dataset, name string) error {
	_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, Properties: map[string]any{"name": name},
	})
	return err
}

func percentile(ds []time.Duration, p float64) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// An owner's save waits for the transaction holding the changelog lock and at
// most one admitted background writer, not for every background writer of
// the repository (#893). Twelve writers apply ten-record effects in a loop,
// more than the repository's eight connections, so without the gate an owner
// save also waited for a connection. The bound is counted in background
// transactions, not milliseconds, so a slow runner moves both sides of it.
// Not parallel: it times writes, and the package's parallel tests would be
// timed with it.
func TestOwnerSavesStayAheadOfBusyBackgroundWriters(t *testing.T) {
	ds := newRaceDataset(t)
	const (
		backgroundWriters = 12
		effectSize        = 10
		ownerSaves        = 60
		// The p95 owner save may take this many median background
		// transactions.
		boundInTransactions = 6
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		holds []time.Duration
	)
	stop := make(chan struct{})
	errs := make(chan error, backgroundWriters)
	for w := range backgroundWriters {
		wg.Go(func() {
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				held, err := backgroundEffect(ctx, ds, effectSize, fmt.Sprintf("background %d.%d", w, i))
				if err != nil {
					errs <- err
					return
				}
				mu.Lock()
				holds = append(holds, held)
				mu.Unlock()
			}
		})
	}
	stopBackground := func() {
		close(stop)
		wg.Wait()
		select {
		case err := <-errs:
			t.Fatalf("a background writer failed: %v", err)
		default:
		}
	}
	// The loop is busy once every writer has committed at least once.
	for {
		mu.Lock()
		n := len(holds)
		mu.Unlock()
		if n >= 2*backgroundWriters {
			break
		}
		if ctx.Err() != nil {
			stopBackground()
			t.Fatal("the background writers never got going")
		}
		time.Sleep(5 * time.Millisecond)
	}

	saves := make([]time.Duration, 0, ownerSaves)
	for i := range ownerSaves {
		started := time.Now()
		if err := ownerSave(ctx, ds, fmt.Sprintf("owner %d", i)); err != nil {
			stopBackground()
			t.Fatalf("owner save %d: %v", i, err)
		}
		saves = append(saves, time.Since(started))
		time.Sleep(5 * time.Millisecond)
	}
	stopBackground()

	ownerP95, ownerP50 := percentile(saves, 0.95), percentile(saves, 0.5)
	holdP50 := percentile(holds, 0.5)
	t.Logf("owner save p50 %s p95 %s max %s; background transaction p50 %s p95 %s over %d; p95 owner save = %.1f background transactions",
		ownerP50, ownerP95, slices.Max(saves), holdP50, percentile(holds, 0.95), len(holds), float64(ownerP95)/float64(holdP50))
	if ownerP95 > boundInTransactions*holdP50 {
		t.Fatalf("the p95 owner save took %s, over %d median background transactions of %s each: owner writes queue behind the background writers",
			ownerP95, boundInTransactions, holdP50)
	}
}

// A writer that gives up while it waits, at the gate or on the changelog
// lock, leaves no admission held and no owner write counted.
func TestACanceledWriterLeavesTheWriteGateClean(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	g := &ds.gate

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = barrier.Rollback() }()
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		t.Fatal(err)
	}

	// The admitted background writer parks on the changelog lock.
	first := make(chan error, 1)
	go func() {
		_, err := backgroundEffect(ctx, ds, 1, "first")
		first <- err
	}()
	waitParked(t, ds, changelogLockKey, 1)

	// The next waits at the gate, holding no connection, and returns as soon
	// as its context ends, with the lock still held.
	waitCtx, cancelWait := context.WithCancel(ctx)
	waiting := make(chan error, 1)
	go func() {
		_, err := backgroundEffect(waitCtx, ds, 1, "waiting")
		waiting <- err
	}()
	waitGate(t, g, "the second background writer queues", func(_ bool, q, _ int) bool { return q >= 1 })
	cancelWait()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a background writer canceled at the gate returned %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a background writer canceled at the gate did not return while the lock was held")
	}
	waitGate(t, g, "the canceled writer leaves the queue", func(_ bool, q, _ int) bool { return q == 0 })

	// An owner write goes past the gate to the lock, and its deadline there
	// takes it off the owner count.
	ownerCtx, cancelOwner := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancelOwner()
	if err := ownerSave(ownerCtx, ds, "owner"); err == nil {
		t.Fatal("an owner save went through while the changelog lock was held")
	}
	waitGate(t, g, "the owner write that gave up is uncounted", func(_ bool, _, o int) bool { return o == 0 })

	if err := barrier.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatalf("the parked background writer: %v", err)
	}
	waitGateIdle(t, g)
	if _, err := backgroundEffect(ctx, ds, 1, "after"); err != nil {
		t.Fatalf("a background write after the cancellations: %v", err)
	}
	if err := ownerSave(ctx, ds, "after"); err != nil {
		t.Fatalf("an owner write after the cancellations: %v", err)
	}
	waitGateIdle(t, g)
}

// At the smallest connection cap a repository gets two connections. Owner
// writes, background writers and reads all finish: the gate holds background
// writers off the pool rather than waiting for a connection while it holds
// anything another writer needs.
func TestWritesFinishOnTheSmallestPool(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t, WithRepositoryConnections(MinRepositoryConnections))
	if got, want := ds.db.Stats().MaxOpenConnections, repositoryConnsPerHandle(MinRepositoryConnections); got != want {
		t.Fatalf("the repository pool allows %d connections, want %d", got, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const rounds = 8

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for w := range 6 {
		wg.Go(func() {
			for i := range rounds {
				if _, err := backgroundEffect(ctx, ds, 5, fmt.Sprintf("background %d.%d", w, i)); err != nil {
					errs <- fmt.Errorf("background writer %d: %w", w, err)
					return
				}
			}
		})
	}
	for w := range 3 {
		wg.Go(func() {
			for i := range rounds {
				if err := ownerSave(ctx, ds, fmt.Sprintf("owner %d.%d", w, i)); err != nil {
					errs <- fmt.Errorf("owner writer %d: %w", w, err)
					return
				}
			}
		})
	}
	for r := range 2 {
		wg.Go(func() {
			for range 2 * rounds {
				if _, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{raceWidget}}}); err != nil {
					errs <- fmt.Errorf("reader %d: %w", r, err)
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if ctx.Err() != nil {
		t.Fatalf("the writers did not finish on a %d-connection pool: %v", repositoryConnsPerHandle(MinRepositoryConnections), ctx.Err())
	}
	waitGateIdle(t, &ds.gate)
}
