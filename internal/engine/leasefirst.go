package engine

// THE LEASE BEFORE THE MIGRATIONS (decision record 0324). A start that
// writes takes every repository's writer lease before its first migration,
// on the session that then runs the migrations, and keeps them for the life
// of the process. A server running beside it holds a repository's lease, so
// the start is refused before it has applied a migration or written a row;
// an older server of this release whose lease connection dropped finds the
// lease taken while the new one runs, and its retake, once the new one has
// stopped, finds the migrations moved and stays refused (writerlease.go
// retake).
//
// THE DATABASE HAS ONE WRITER PROCESS (decision record 0325). Before any of
// that the start takes the database's writer lock, on the same session, so
// a start is the only way a process becomes a writer and two cannot run at
// once, whatever repositories each saw. Holding everything, it leaves a mark
// no other start leaves, so a retake also sees a process of this release
// that started, wrote and stopped meanwhile, and a database restored under
// it.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// migrateHolding takes the database's writer lock and every repository's
// lease and applies the pending migrations on the lease's own session, so a
// migration's own lease check (0021's) reads these leases as its session's,
// and then leaves this start's mark. What schema_migrations and the mark
// hold after it is what a retake compares.
func (l *writerLease) migrateHolding(ctx context.Context, log *slog.Logger) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	conn, err := l.pinned(ctx)
	if err != nil {
		return err
	}
	got, holder, err := l.lockDatabase(ctx, conn)
	if err != nil {
		return fmt.Errorf("substrate/engine: take the database's writer lock: %w", err)
	}
	if !got {
		return databaseWriterRefusal{holder: holder}
	}
	l.writer = true
	if err := l.holdEveryRepository(ctx, conn); err != nil {
		return err
	}
	if err := migrateOn(ctx, conn, math.MaxInt, readVectorVersion, log, l.guardMigration); err != nil {
		return err
	}
	if l.startedUnder, err = migrationPrint(ctx, conn); err != nil {
		return fmt.Errorf("substrate/engine: read the migrations this start applied: %w", err)
	}
	mark := newStartMark()
	if err := markStart(ctx, conn, mark); err != nil {
		return fmt.Errorf("substrate/engine: leave the start's mark: %w", err)
	}
	l.startMark = mark
	return nil
}

// newStartMark mints a start's mark: random, so no other start, and no copy
// of the database from before this one, holds it.
func newStartMark() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// markStart writes mark as the database's start mark. It runs only on the
// lease's session holding the database's writer lock and every repository's
// lease: a start refused at either leaves none, so a server whose lock it
// met is never refused at its next retake for it (decision record 0325).
func markStart(ctx context.Context, q dbx, mark string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO writer_starts (mark) VALUES ($1)
		ON CONFLICT (one) DO UPDATE SET mark = EXCLUDED.mark`, mark)
	return err
}

// readStartMark reads the database's start mark: sql.ErrNoRows where the
// row is gone.
func readStartMark(ctx context.Context, q dbx) (string, error) {
	var mark string
	err := q.QueryRowContext(ctx, `SELECT mark FROM writer_starts`).Scan(&mark)
	return mark, err
}

// missingTable reports whether err is Postgres's undefined_table: a table a
// retake reads was dropped, which it reads as the database replaced.
func missingTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// leaseKey is one repository as the lease reads it: its id, which is its
// name on a release before ids (migration 0020), and its name.
type leaseKey struct{ id, name string }

// repositoriesUnderLock reads every repository under a share lock on
// `repositories`, which stops a row from appearing until tx ends: a
// registration takes its lease before it writes its row, so a row that has
// not committed is held at the table. A database with no `repositories`
// table holds none.
func repositoriesUnderLock(ctx context.Context, tx *sql.Tx) ([]leaseKey, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('repositories') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("substrate/engine: look for the repositories: %w", err)
	}
	if !exists {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `LOCK TABLE repositories IN SHARE MODE`); err != nil {
		return nil, fmt.Errorf("substrate/engine: lock the repositories: %w", err)
	}
	// Not prepared: the migrations run on this session and change the
	// columns' types under any plan cached for it.
	rows, err := tx.QueryContext(ctx, `SELECT id, authority FROM repositories ORDER BY id`, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the repositories: %w", err)
	}
	var out []leaseKey
	for rows.Next() {
		var k leaseKey
		if err := rows.Scan(&k.id, &k.name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, k)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// errHeldBeforeMigrating is the refusal of a start another server process
// runs beside, whose lease of k's under key it met: no migration has been
// applied and no row written.
// It names the repository by its name and the lock by its key, as the
// lease's own refusal does (acquireNew).
func errHeldBeforeMigrating(k leaseKey, key string, holder *leaseHolder) error {
	return fmt.Errorf("%w: repository %s (id %s). One server per database: another server is running against it, so this start applies no migration and writes no row; "+
		"stop every running server, then start this one. The lease is the advisory lock hashtext(current_schema() || '|writer|' || '%s')::bigint, visible in pg_locks. Held by %s",
		ErrRepositoryHasAnotherWriter, k.name, k.id, key, holder)
}

// holdEveryRepository takes the lease of every repository the database
// holds, on conn, before any migration. Each is taken for the session, so it
// outlives the transaction the table lock is held in. A release before ids
// keyed its leases by name, so a repository's name is asked of too, for the
// transaction alone; before ids its id is its name, and the keying lets the
// name's lease go once the repository has its id (releaseKeyingLeases). A
// lease another session holds refuses the start.
func (l *writerLease) holdEveryRepository(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	repos, err := repositoriesUnderLock(ctx, tx)
	if err != nil {
		return err
	}
	for _, k := range repos {
		got, holder, err := l.lock(ctx, tx, k.id)
		if err != nil {
			return fmt.Errorf("substrate/engine: take the writer lease on %s before migrating: %w", k.name, err)
		}
		if !got {
			return errHeldBeforeMigrating(k, k.id, holder)
		}
		l.held = append(l.held, k.id)
		if err := heldByName(ctx, tx, k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// heldByName refuses k where a server of a release before ids holds its
// name's lease, asked of for tx alone. Before ids the name is the id, whose
// lease this session already holds.
func heldByName(ctx context.Context, tx *sql.Tx, k leaseKey) error {
	if k.name == k.id {
		return nil
	}
	var free bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(`+writerLeaseKeySQL+`)`, k.name).Scan(&free); err != nil {
		return err
	}
	if free {
		return nil
	}
	holder, err := readLeaseHolder(ctx, tx, k.name)
	if err != nil {
		return err
	}
	return errHeldBeforeMigrating(k, k.name, holder)
}

// guardMigration runs first in each migration's transaction, on the lease's
// session: under a share lock on `repositories`, every repository's lease is
// this session's, or nobody's. One that appeared since the start took its
// leases, a registration another server had begun, refuses the migration.
func (l *writerLease) guardMigration(ctx context.Context, tx *sql.Tx) error {
	repos, err := repositoriesUnderLock(ctx, tx)
	if err != nil {
		return err
	}
	for _, k := range repos {
		var free bool
		if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(`+writerLeaseKeySQL+`)`, k.id).Scan(&free); err != nil {
			return err
		}
		if !free {
			holder, err := readLeaseHolder(ctx, tx, k.id)
			if err != nil {
				return err
			}
			return errHeldBeforeMigrating(k, k.id, holder)
		}
		if err := heldByName(ctx, tx, k); err != nil {
			return err
		}
	}
	return nil
}

// migrationPrint is schema_migrations as one string, each row's version,
// hash and moment of application, so a migration applied again by another
// server reads as a change.
func migrationPrint(ctx context.Context, q dbx) (string, error) {
	var print string
	err := q.QueryRowContext(ctx, `SELECT coalesce(string_agg(version || ':' || sha256 || ':' || applied_at::text, ',' ORDER BY version), '')
		FROM schema_migrations`).Scan(&print)
	return print, err
}

// What a retake of the writer lease does to a pass that runs behind a
// request or an open (retakePasses).
const (
	// restartedOnRetake: the pass holds a position in memory across its
	// writes, so a retake stops it and resets it while every write is still
	// refused, and starts it again, to plan from the database, once they are
	// accepted; a retake that found the database migrated stops it for good.
	restartedOnRetake = "restarted"
)

// retakePasses is every pass the engine starts, by the name it starts it
// under, and what a retake of the writer lease does to it: restarted, or why
// it holds no position a retake could leave wrong.
// TestEveryBackgroundPassIsClassedForARetake holds it against every pass the
// engine starts, so a new one is classed when it is added.
var retakePasses = map[string]string{
	"reindex search":                 restartedOnRetake,
	"re-derive the indexes":          restartedOnRetake,
	"archive":                        restartedOnRetake,
	"compaction":                     restartedOnRetake,
	"search statistics":              "a cache of the collection statistics a search ranks with; it writes nothing",
	"warm functions":                 "prepares the bodies' processes; it writes nothing to the database",
	"webhook spool drain":            "its position is this process's spool files themselves",
	"webhook fire":                   "one request over one trigger, read when it starts; its writes are refused while the lease is lost",
	"webhook resume":                 "one trigger's resume, read when it starts",
	"temporary retry":                "one delivery's retry, read when it starts",
	"judge":                          "one judgment over one request, read when it starts",
	"chat turn":                      "one agent turn over its thread, read when it starts",
	"settle approval":                "one approval, read when it starts",
	"title thread":                   "one thread's title, read when it starts",
	"resume notified thread":         "one thread's resume, read when it starts",
	"interrupted agent rerun":        "one delivery's rerun, read when it starts",
	"agent background job":           "one job, read when it starts",
	"prove a token commit ended":     "a wait for one lock, holding no position",
	"prove a names commit ended":     "a wait for one lock, holding no position",
	"reconcile a renamed repository": "one record set from the row it reads when it starts",
	"index a person":                 "one person's index, read when it starts",
	"usage inventory":                "a read for the metrics; it writes nothing",
	retakeRestartTask:                "the retake's own task",
}

// retakeRestartTask names the task a retake resets the passes in.
const retakeRestartTask = "reset the passes after a lease retake"

// leaseRetaken is the lease's retaken. On every open repository it stops
// the passes that hold a position in memory and resets them, or, where the
// retake found the database migrated by another server, stops them for good
// (decision record 0324). It runs as a detached task, so a shutdown drains
// it, and the lease's heartbeat waits for it, so every write stays refused
// until it is done: the heartbeat cannot take the service's mutex itself,
// which Close holds while it waits for the heartbeat to stop.
func (s *service) leaseRetaken(ctx context.Context, stale bool) (func(), bool) {
	var resumes []func()
	ok := true
	done := make(chan struct{})
	if !s.spawn(retakeRestartTask, "", func(context.Context) {
		defer close(done)
		s.mu.Lock()
		open := make([]*dataset, 0, len(s.datasets))
		for _, ds := range s.datasets {
			open = append(open, ds)
		}
		s.mu.Unlock()
		for _, ds := range open {
			resume, stopped := ds.resetForRetake(stale)
			ok = ok && stopped
			if resume != nil {
				resumes = append(resumes, resume)
			}
		}
	}) {
		return nil, false
	}
	select {
	case <-done:
	case <-ctx.Done():
		return nil, false
	}
	return func() {
		for _, resume := range resumes {
			resume()
		}
	}, ok
}

// resetForRetake stops one repository's passes that retakePasses says are
// restarted, resets what each holds, and answers what starts them again,
// nil where the retake found the database migrated and they stay stopped.
// stopped is false where one did not return within the drain budget.
func (ds *dataset) resetForRetake(stale bool) (resume func(), stopped bool) {
	if !closedChan(ds.reindex.finished()) {
		ds.reindexOwed.Store(true)
	}
	stopped = ds.stopSearchReindex(stale)
	if ds.svc.retakeResetHook != nil && ds.svc.retakeResetHook() {
		stopped = false
	}
	stopped = ds.stopIndexReprojection(stale) && stopped
	stopped = ds.endCompactionPass() && stopped
	if ds.archive != nil {
		// Its position is the archive record it read once this process, and
		// the lock waits out a pass in flight: the next pass reads it again.
		ds.archive.mu.Lock()
		ds.archive.known = false
		ds.archive.mu.Unlock()
	}
	if stale {
		if ds.archiveStop != nil {
			ds.archiveStop()
		}
		ds.stopCompactor()
		return nil, stopped
	}
	return func() {
		if ds.reindexOwed.Swap(false) {
			ds.startSearchReindex()
		}
		ds.kickIndexReprojection()
	}, stopped
}

// closedChan reports whether c is closed.
func closedChan(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
