package engine_test

// The connections one process holds are bounded, however many repositories it
// has opened (#640). A pool per repository that nothing closes grew with every
// registration until Postgres refused every client.

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/config"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The admin pool's cap and the maintenance pool's (engine.go Open): what a
// process holds beside the pool its repositories share and the two budgets
// dialed outside it.
const adminConns, maintConns = 4, 5

// processBound is the documented bound (docs/operations.md): the shared
// cap, the admin and maintenance pools, and the migration and catch-up
// budgets.
func processBound(repositoryConns int) int {
	return repositoryConns + adminConns + maintConns + engine.MigrationConnections + engine.CatchUpConnections
}

// connectionCounter counts the backends connected to the test's own
// database on ONE connection of its own, which the count leaves out.
// MigratedDSN clones a database per test, so nothing another parallel test
// holds is counted.
type connectionCounter struct{ db *sql.DB }

func newConnectionCounter(t *testing.T, dsn string) connectionCounter {
	t.Helper()
	db := rawDB(t, dsn)
	db.SetMaxOpenConns(1)
	return connectionCounter{db: db}
}

func (c connectionCounter) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := c.db.QueryRow(`
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid()`).Scan(&n); err != nil {
		t.Errorf("count connections: %v", err)
	}
	return n
}

func TestConnectionsStayBoundedAsRepositoriesAccumulate(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	// More repositories than the process's whole connection budget: the old
	// shape kept at least one idle connection per opened repository, so it
	// cannot pass with this many.
	exerciseRepositories(t, svc, dsn, 32, processBound(engine.DefaultRepositoryConnections))
}

// SUBSTRATE_REPOSITORY_CONNECTIONS moves the cap, and repositories that
// outnumber it still all read at once, queued on the shared connections.
func TestTheRepositoryConnectionCapIsConfigurable(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t, engine.WithRepositoryConnections(engine.MinRepositoryConnections))
	exerciseRepositories(t, svc, dsn, 6, processBound(engine.MinRepositoryConnections))
}

// A cap under the floor refuses the Open, and the server's configuration
// holds the same floor.
func TestARepositoryConnectionCapUnderTheFloorIsRefused(t *testing.T) {
	t.Parallel()
	if config.MinRepositoryConnections != engine.MinRepositoryConnections {
		t.Fatalf("config floor %d, engine floor %d", config.MinRepositoryConnections, engine.MinRepositoryConnections)
	}
	_, err := engine.OpenForTest(t, context.Background(), engine.MigratedDSN(t),
		engine.WithDataRoot(t.TempDir()), engine.WithRepositoryConnections(engine.MinRepositoryConnections-1))
	if err == nil {
		t.Fatal("a repository pool under the floor opened")
	}
}

// exerciseRepositories creates and opens that many repositories, then has
// every one of them read its own self-description over and over, all at
// once, while the database's connection count is sampled. The peak is held
// to limit, and every read must see its own repository's row and no other.
func exerciseRepositories(t *testing.T, svc substrate.Service, dsn string, repositories, limit int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	counter := newConnectionCounter(t, dsn)
	datasets := make([]substrate.Dataset, 0, repositories)
	for i := range repositories {
		id := fmt.Sprintf("r%02d-%s", i, testdb.Repository(t))
		if _, err := svc.CreateRepository(ctx, id); err != nil {
			t.Fatalf("create repository %s: %v", id, err)
		}
		ds, err := svc.Dataset(ctx, id)
		if err != nil {
			t.Fatalf("open repository %s: %v", id, err)
		}
		datasets = append(datasets, ds)
	}

	var peak atomic.Int64
	sampled := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			if n := int64(counter.count(t)); n > peak.Load() {
				peak.Store(n)
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	const reads = 20
	selfDescription := substrate.Query{Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/repository"}}}
	var wg sync.WaitGroup
	errs := make(chan error, repositories)
	for _, ds := range datasets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := ds.Repository().ID
			for range reads {
				page, err := ds.List(ctx, selfDescription)
				if err != nil {
					errs <- fmt.Errorf("list %s: %w", id, err)
					return
				}
				if len(page.Records) != 1 || page.Records[0].ID != id {
					got := make([]string, 0, len(page.Records))
					for _, r := range page.Records {
						got = append(got, r.ID)
					}
					errs <- fmt.Errorf("repository %s read the self-descriptions %v while the others ran", id, got)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-sampled
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	t.Logf("%d repositories peaked at %d connections (limit %d)", repositories, peak.Load(), limit)
	if n := int(peak.Load()); n > limit {
		t.Fatalf("%d repositories peaked at %d connections, want at most %d", repositories, n, limit)
	}
}

// A boot, or a dispatcher pass, opens many repositories at once, and each
// first open runs the pending repository migrations on a connection dialed
// outside the shared pool. Those dials wait for a migration slot, so the
// process stays inside its documented bound however many open together.
func TestOpeningManyRepositoriesAtOnceKeepsMigrationsInsideTheBound(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	const cap = engine.MinRepositoryConnections
	svc, dsn := newService(t, engine.WithRepositoryConnections(cap))
	// More repositories than the whole bound: with every migration on its
	// own connection at once, they alone would pass it.
	const repositories = 18
	limit := processBound(cap)
	ids := make([]string, 0, repositories)
	for i := range repositories {
		id := fmt.Sprintf("m%02d-%s", i, testdb.Repository(t))
		if _, err := svc.CreateRepository(ctx, id); err != nil {
			t.Fatalf("create repository %s: %v", id, err)
		}
		ids = append(ids, id)
	}
	// Each migration holds its own connection long enough for the others
	// to pile up behind it.
	var running, most, ran atomic.Int64
	engine.SetTestMigrationHook(svc, func() {
		n := running.Add(1)
		defer running.Add(-1)
		ran.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	})
	counter := newConnectionCounter(t, dsn)
	var peak atomic.Int64
	sampled, stop := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			if n := int64(counter.count(t)); n > peak.Load() {
				peak.Store(n)
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	var wg sync.WaitGroup
	errs := make(chan error, repositories)
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Dataset(ctx, id); err != nil {
				errs <- fmt.Errorf("open %s: %w", id, err)
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-sampled
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if ran.Load() < repositories {
		t.Fatalf("%d repository migrations ran over %d first opens; the test is not watching what it means to", ran.Load(), repositories)
	}
	t.Logf("%d first opens: %d migrations, at most %d at once, peak %d connections (limit %d)",
		repositories, ran.Load(), most.Load(), peak.Load(), limit)
	if most.Load() > engine.MigrationConnections {
		t.Errorf("%d repository migrations held their own connections at once, want at most %d", most.Load(), engine.MigrationConnections)
	}
	if n := int(peak.Load()); n > limit {
		t.Fatalf("%d repositories opening at once peaked at %d connections, want at most %d", repositories, n, limit)
	}
}
