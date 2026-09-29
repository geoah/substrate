package engine

// Index builds and the shared records table. Every repository writes the one
// table, so a build of one repository's index must not hold a lock on it that
// another repository's write waits for. Each case holds a build open with a
// session of its own (holdRecords), because a build on the suite's small
// tables finishes before anything could be observed waiting on it: the
// concurrent build waits out every writer on records and every older
// snapshot, while the plain DROP INDEX it replaced waits for any reader and
// queues every later write behind it.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	meterKind   = publishPackage + "/meter"
	lampPackage = "lamp.example.substrate.reamde.dev/lamp"
	lampKind    = lampPackage + "/lamp"
)

// meterVocabulary declares the meter kind with one index, on the property
// named, at ordinal zero.
func meterVocabulary(indexed string) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "meter"},
			map[string]any{
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"email": map[string]any{"type": "string"},
				},
				"indices": []any{map[string]any{"properties": []any{indexed}}},
			}),
	}
}

// secondRepository creates another repository on ds's service and returns
// its dataset.
func secondRepository(t *testing.T, ds *dataset) *dataset {
	t.Helper()
	ctx := context.Background()
	name := testdb.RepositoryLabel(t) + "2.example.com"
	if _, err := ds.svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	d, err := ds.svc.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	return d.(*dataset)
}

// rawDB is a pool of the DSN's own user on the test's database, outside the
// service's pools, for the sessions that hold a build open and read
// pg_stat_activity.
func rawDB(t *testing.T, ds *dataset) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", ds.svc.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// holdRecords opens a transaction on its own connection that holds lock on
// records and returns its release. A REPEATABLE READ read holds ACCESS SHARE
// and a snapshot: a concurrent build waits out the snapshot, and a plain
// DROP INDEX waits for the lock. A ROW EXCLUSIVE lock is what an in-flight
// write holds: a build of either kind waits for it.
func holdRecords(t *testing.T, db *sql.DB, rowExclusive bool) func() {
	t.Helper()
	ctx := context.Background()
	opts := &sql.TxOptions{Isolation: sql.LevelRepeatableRead}
	hold := `SELECT count(*) FROM records`
	if rowExclusive {
		opts, hold = nil, `LOCK TABLE records IN ROW EXCLUSIVE MODE`
	}
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, hold); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	var once atomic.Bool
	release := func() {
		if once.CompareAndSwap(false, true) {
			_ = tx.Rollback()
		}
	}
	t.Cleanup(release)
	return release
}

// waitFor polls cond until it holds, failing the test after a bound.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// sessionsWaiting counts the sessions on this database whose statement, as
// matched by pattern, waits for a lock.
func sessionsWaiting(t *testing.T, db *sql.DB, pattern string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid()
		  AND wait_event_type = 'Lock' AND query ILIKE $1`, pattern).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// receive waits for the result of a background apply, failing after a bound.
func receive(t *testing.T, what string, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(60 * time.Second):
		t.Fatalf("%s never returned", what)
		return nil
	}
}

// requireCurrentIndex asserts that the meter kind's ordinal-zero index is
// valid, indexes want, carries its statement as its comment, and that no
// scratch name and no INVALID index is left on records.
func requireCurrentIndex(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	name := "idx_" + derivedID(meterKind, "0")
	var def string
	var valid bool
	var comment sql.NullString
	if err := db.QueryRow(`
		SELECT pg_get_indexdef(i.indexrelid), i.indisvalid, obj_description(i.indexrelid, 'pg_class')
		FROM pg_index i WHERE i.indexrelid = to_regclass($1)`, name).Scan(&def, &valid, &comment); err != nil {
		t.Fatalf("the ordinal-zero index %s: %v", name, err)
	}
	if !valid || !strings.Contains(def, want) {
		t.Fatalf("the ordinal-zero index is valid=%v with definition %s, want a valid index on %s", valid, def, want)
	}
	if !comment.Valid || !strings.HasPrefix(comment.String, "CREATE INDEX IF NOT EXISTS "+name+" ") || !strings.Contains(comment.String, want) {
		t.Fatalf("the ordinal-zero index's comment is not its statement: %v", comment)
	}
	var left, invalid int
	if err := db.QueryRow(`
		SELECT (SELECT count(*) FROM pg_class WHERE relname IN ($1 || '_new', $1 || '_old')),
		       (SELECT count(*) FROM pg_index WHERE indrelid = to_regclass('records') AND NOT indisvalid)`,
		name).Scan(&left, &invalid); err != nil {
		t.Fatal(err)
	}
	if left != 0 || invalid != 0 {
		t.Fatalf("the rebuild left %d scratch index(es) and %d INVALID index(es) on records", left, invalid)
	}
}

// A changed index definition is rebuilt while another repository writes. The
// held reader makes the rebuild wait: the concurrent build for its snapshot,
// a plain DROP INDEX for its lock, and every write queues behind that DROP's
// ACCESS EXCLUSIVE request, including a write to a repository that declares
// nothing the rebuild touches. The write here must finish while the rebuild
// still waits.
func TestAnIndexRebuildDoesNotStallAnotherRepositorysWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("name")); err != nil {
		t.Fatalf("declare the meter kind: %v", err)
	}
	other := secondRepository(t, ds)
	if _, err := other.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(lampPackage, 0),
		vocabulary.KindManifest(lampPackage,
			map[string]any{"singular": "lamp"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("declare the lamp kind in the other repository: %v", err)
	}
	db := rawDB(t, ds)

	release := holdRecords(t, db, false)
	applied := make(chan error, 1)
	go func() {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("email"))
		applied <- err
	}()
	waitFor(t, "the rebuild to wait on the held reader", func() bool {
		return sessionsWaiting(t, db, "%INDEX%") > 0
	})

	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	started := time.Now()
	_, werr := other.Put(wctx, substrate.ActorAPI, substrate.PutInput{
		Kind: lampKind, ID: "l1", Properties: map[string]any{"name": "on"},
	})
	took := time.Since(started)
	cancel()
	stillWaiting := sessionsWaiting(t, db, "%INDEX%") > 0
	release()
	if err := receive(t, "the rebuilding apply", applied); err != nil {
		t.Fatalf("the rebuilding apply: %v", err)
	}
	if werr != nil {
		t.Fatalf("the other repository's write waited on the rebuild for %s: %v", took, werr)
	}
	if !stillWaiting {
		t.Fatal("the rebuild stopped waiting before the write finished, so the write proved nothing")
	}
	requireCurrentIndex(t, db, "'email'")
}

// failTheBuild starts an apply that changes the meter index while a held
// reader keeps its concurrent build waiting, ends the build's session with
// fn (pg_cancel_backend or pg_terminate_backend) once it waits, and returns
// the apply's result channel and the reader's release.
func failTheBuild(t *testing.T, ds *dataset, db *sql.DB, fn string) (<-chan error, func()) {
	t.Helper()
	release := holdRecords(t, db, false)
	applied := make(chan error, 1)
	go func() {
		_, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, meterVocabulary("email"))
		applied <- err
	}()
	waitFor(t, "the concurrent build to wait on the held reader", func() bool {
		return sessionsWaiting(t, db, "CREATE INDEX CONCURRENTLY%") > 0
	})
	if _, err := db.Exec(`
		SELECT ` + fn + `(pid) FROM pg_stat_activity
		WHERE datname = current_database() AND query ILIKE 'CREATE INDEX CONCURRENTLY%'`); err != nil {
		t.Fatal(err)
	}
	return applied, release
}

// A build that fails leaves an INVALID index under the scratch name, which
// writes of the kind still maintain, and the failing apply drops it before it
// returns. Its drop waits for the held reader like the build did, which is
// how the test sees it run.
func TestAFailedIndexBuildDropsItsScratchIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("name")); err != nil {
		t.Fatalf("declare the meter kind: %v", err)
	}
	db := rawDB(t, ds)

	applied, release := failTheBuild(t, ds, db, "pg_cancel_backend")
	waitFor(t, "the failed build's scratch index to be dropped", func() bool {
		return sessionsWaiting(t, db, "DROP INDEX CONCURRENTLY%_new") > 0
	})
	release()
	if err := receive(t, "the canceled apply", applied); err == nil {
		t.Fatal("the apply whose build was canceled succeeded")
	}
	requireCurrentIndex(t, db, "'name'")
}

// A build whose session dies cannot drop what it leaves. The next
// ensureIndices over the kind drops it before anything else: one whose
// definition is unchanged drops it without a rebuild, and the retried apply
// drops it and builds again. The next boot drops it too, since the kind may
// never be applied again and boot builds only the shipped kinds.
func TestAScratchIndexADeadBuildLeftIsDroppedBeforeTheNextBuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, closeSvc := reopenableWidgetDataset(t)
	ds := open()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("name")); err != nil {
		t.Fatalf("declare the meter kind: %v", err)
	}
	db := rawDB(t, ds)
	name := "idx_" + derivedID(meterKind, "0")
	var before uint32
	if err := db.QueryRow(`SELECT to_regclass($1)::oid`, name).Scan(&before); err != nil {
		t.Fatal(err)
	}
	unchanged := func(when string) {
		t.Helper()
		var after uint32
		if err := db.QueryRow(`SELECT to_regclass($1)::oid`, name).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s rebuilt an unchanged definition (oid %d to %d)", when, before, after)
		}
		requireCurrentIndex(t, db, "'name'")
	}
	killTheBuild := func() {
		t.Helper()
		applied, release := failTheBuild(t, ds, db, "pg_terminate_backend")
		if err := receive(t, "the apply whose build died", applied); err == nil {
			t.Fatal("the apply whose build's session died succeeded")
		}
		release()
		var valid bool
		if err := db.QueryRow(`SELECT indisvalid FROM pg_index WHERE indexrelid = to_regclass($1)`,
			name+"_new").Scan(&valid); err != nil {
			t.Fatalf("the dead build left no scratch index, so this test proves nothing: %v", err)
		}
		if valid {
			t.Fatal("the dead build's scratch index is valid; a build that died leaves it INVALID")
		}
	}

	// The stored declaration still indexes name, and its index still is
	// that one: the leftover goes and nothing is rebuilt.
	killTheBuild()
	stored, ok := ds.registry().ByIdentity(meterKind)
	if !ok {
		t.Fatal("the meter kind is not declared")
	}
	var built int
	if err := ensureIndices(ctx, ds.svc.admin, []*vocabulary.Kind{stored}, indexProgress{
		building: func(string, string) { built++ },
	}); err != nil {
		t.Fatalf("ensureIndices over the unchanged kind: %v", err)
	}
	if built != 0 {
		t.Fatalf("ensureIndices over the unchanged kind built %d index(es)", built)
	}
	unchanged("ensureIndices over the unchanged kind")

	// A boot drops the leftover of a kind nothing applies.
	killTheBuild()
	closeSvc()
	ds = open()
	unchanged("the boot")

	// The retried apply drops the leftover and builds the new definition.
	killTheBuild()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("email")); err != nil {
		t.Fatalf("the retried apply: %v", err)
	}
	requireCurrentIndex(t, db, "'email'")
}

// Two repositories that declare one kind at the same moment build its index
// once. Both see no index; the first builds it while a held writer keeps the
// build open, and the second waits for the build lock and then finds the
// index current. Without the lock, the second either fails on the catalog's
// unique name (the plain build) or on the scratch name (the concurrent one).
func TestTwoRepositoriesDeclaringOneIndexBuildItOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	other := secondRepository(t, ds)
	db := rawDB(t, ds)

	release := holdRecords(t, db, true)
	first := make(chan error, 1)
	go func() {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("name"))
		first <- err
	}()
	waitFor(t, "the first build to wait on the held writer", func() bool {
		return sessionsWaiting(t, db, "CREATE INDEX%") > 0
	})
	second := make(chan error, 1)
	go func() {
		_, err := other.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, meterVocabulary("name"))
		second <- err
	}()
	// The second is parked once its own index statement waits too (a build
	// with no lock between the two) or once its session last asked for the
	// build lock and was refused.
	waitFor(t, "the second repository to reach its build", func() bool {
		if sessionsWaiting(t, db, "%INDEX%") > 1 {
			return true
		}
		var n int
		if err := db.QueryRow(`
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid()
			  AND query LIKE '%pg_try_advisory_lock(%|index|records%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	})
	release()
	errFirst, errSecond := receive(t, "the first apply", first), receive(t, "the second apply", second)
	if err := errors.Join(errFirst, errSecond); err != nil {
		t.Fatalf("two repositories declaring one index: %v", err)
	}
	requireCurrentIndex(t, db, "'name'")
}
