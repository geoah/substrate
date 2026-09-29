package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// searchReindexBatch bounds one transaction of the open-time reindex: the
// rows of one kind it locks, re-derives and writes before it commits. A
// write that touches one of those rows waits for that commit and no longer.
const searchReindexBatch = 2000

// checkSearchIndex is the open ladder's step: it reads which rule set indexed
// the rows, and when that is older than this binary's (searchIndexVersion) it
// remembers the version, so startSearchReindex re-derives every row's `fts`
// once the open completes. The reindex does not run here: on a repository of
// a few hundred thousand rows it takes minutes, and the open is what every
// request on the repository waits for.
//
// A read-only process skips it and serves the index it finds; the next
// writer brings it up to date. A newer version than the binary's is left
// alone: the newer rules indexed variants these rules do not, which costs
// this binary nothing to read.
func (ds *dataset) checkSearchIndex(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	var have int
	err := ds.db.QueryRowContext(ctx, `SELECT version FROM search_index`).Scan(&have)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		have = 1
	case err != nil:
		return fmt.Errorf("substrate/engine: repository %s: read the search index version: %w", ds.info.ID, err)
	}
	if have < searchIndexVersion {
		ds.reindexFrom = have
	}
	return nil
}

// startSearchReindex starts the reindex checkSearchIndex asked for in a
// goroutine the dataset owns: close cancels it and waits for it, and so does
// a rebuild, which re-derives every row itself. It is called once, as the
// open publishes the dataset, and sets the two fields close reads before the
// dataset is visible to anybody else.
func (ds *dataset) startSearchReindex() {
	if ds.reindexFrom == 0 {
		return
	}
	from := ds.reindexFrom
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ds.reindexCancel, ds.reindexDone = cancel, done
	started := ds.spawn("reindex search", func(bg context.Context) {
		defer close(done)
		defer cancel()
		// The service's shutdown cancels it too: Close drains the detached
		// tasks before it closes any dataset.
		stop := context.AfterFunc(bg, cancel)
		defer stop()
		ds.reindexSearch(ctx, from)
	})
	if !started {
		cancel()
		close(done)
	}
}

// searchReindexMaxPause caps the pause between tries of a step that failed
// (reindexStep).
const searchReindexMaxPause = time.Minute

// stopSearchReindex cancels the open's reindex and waits for it to return,
// up to the drain budget the service's own shutdown gives a detached task.
// A dataset whose open started none returns at once.
func (ds *dataset) stopSearchReindex() {
	if ds.reindexCancel == nil {
		return
	}
	ds.reindexCancel()
	// The service's shutdown already waited its one budget for every detached
	// task, this one included, and said so if any outlived it. Waiting again
	// here, once per repository it closes, would multiply that budget.
	if ds.svc.bg.stopping() {
		return
	}
	timer := time.NewTimer(backgroundDrainTimeout)
	defer timer.Stop()
	select {
	case <-ds.reindexDone:
	case <-timer.C:
		ds.svc.log.Error("substrate: the search index re-derivation did not stop within the drain budget",
			"repository", logSafeID(ds.scope.Repository), "timeout", backgroundDrainTimeout)
	}
}

// reindexSearch re-derives every row's `fts`, live and tombstoned, under this
// binary's rules, kind by kind and within a kind in pages of
// searchReindexBatch rows by id, each page its own transaction. It moves
// nothing but `fts`, through the rederiveFTS a kind edit uses: no version, no
// updated_at, no changelog entry, because no record changed, only the index
// over it. Reads and writes are served throughout, and a row keeps its old
// `fts`, still searchable, until its page commits.
//
// A step that fails is tried again after a pause (reindexStep), so a passing
// database error or a dropped writer lease costs a pause and not the rest of
// the reindex. The version is recorded only after the last row, so a reindex
// the close or a rebuild stops leaves the old version, and the next open
// starts it again from the first kind. A row redone twice lands at the same
// bands.
//
// WHY A WRITE DURING THE REINDEX IS NEVER UNDONE BY IT. A page derives
// exactly what the write path would, so its overwrite is harmless. It holds
// the shared registry-dependency lock, as every data write does, so the
// registry it reads after taking it is the published one a write derives
// under (commitAndPublish swaps it before that lock releases), and a kind
// edit waits for the page or the page for it. It locks each row FOR UPDATE
// and derives from the row as locked, so no write lands between its read and
// its UPDATE. Skipping rows whose `version` moved since the page was planned
// was the other option, and it misses a kind edit: reprojectFTS rewrites
// `fts` without moving `version`.
//
// SKIP LOCKED, THEN ONE AT A TIME. A page never waits on a row. A sync write
// holds many rows of one kind in one transaction, and a page that waited on
// one of them while holding another the write wants next is a deadlock whose
// victim may be the write. So a page locks what is free, and the rows a write
// held are redone after the kind's pages, each in a transaction of its own
// that waits for that one row and holds no other.
func (ds *dataset) reindexSearch(ctx context.Context, from int) {
	started := time.Now()
	repo := logSafeID(ds.scope.Repository)
	var kinds []string
	var total, done int64
	stopped := func() {
		ds.svc.log.Info("substrate: the search index re-derivation stopped before it finished; the next open runs it again",
			"repository", repo, "done", done, "total", total)
	}
	if ds.reindexStep(ctx, "plan", func() error {
		var err error
		kinds, total, err = ds.searchReindexPlan(ctx)
		return err
	}) != nil {
		stopped()
		return
	}
	ds.svc.log.Info("substrate: re-deriving the search index",
		"repository", repo, "from", from, "to", searchIndexVersion, "kinds", len(kinds), "rows", total)
	prog := ds.svc.progress("substrate: re-deriving the search index", "repository", repo)
	for _, kind := range kinds {
		n, err := ds.reindexKind(ctx, kind, func(rows int) {
			done += int64(rows)
			prog.report("kind", kind, "done", done, "total", total)
		})
		if err != nil {
			stopped()
			return
		}
		ds.svc.log.Info("substrate: re-derived the search index of one kind",
			"repository", repo, "kind", kind, "rows", n, "done", done, "total", total)
	}
	if ds.reindexStep(ctx, "record the version", func() error {
		return ds.inRawTx(ctx, func(t *txn) error { return t.markSearchIndexed() })
	}) != nil {
		stopped()
		return
	}
	ds.svc.log.Info("substrate: re-derived the search index",
		"repository", repo, "from", from, "to", searchIndexVersion, "rows", done,
		"took", time.Since(started).Round(time.Millisecond))
}

// reindexStep runs one step of the reindex until it succeeds or ctx ends,
// pausing between tries from a second up to searchReindexMaxPause, and
// returns an error only for ctx. Every step is safe to run again: a page
// that failed rolled back, and one redone lands at the same bands. A step
// that can never succeed logs its error at every try, which is how an
// operator hears of it.
func (ds *dataset) reindexStep(ctx context.Context, step string, fn func() error) error {
	pause := time.Second
	for {
		err := fn()
		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		ds.svc.log.Error("substrate: a search index re-derivation step failed; trying it again",
			"repository", logSafeID(ds.scope.Repository), "step", step, "retry_in", pause, "error", err)
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		pause = min(2*pause, searchReindexMaxPause)
	}
}

// searchReindexPlan names every kind with a stored row, in order, and counts
// the rows, so the progress lines can say how far along the reindex is. A
// kind whose first row is written after the plan is indexed by that write.
func (ds *dataset) searchReindexPlan(ctx context.Context) ([]string, int64, error) {
	rows, err := ds.db.QueryContext(ctx, `SELECT kind, count(*) FROM records GROUP BY kind ORDER BY kind`)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var kinds []string
	var total int64
	for rows.Next() {
		var kind string
		var n int64
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, 0, err
		}
		kinds = append(kinds, kind)
		total += n
	}
	return kinds, total, rows.Err()
}

// reindexKind re-derives every row of one kind: its pages first, then the
// rows a write held when their page came, and reports the rows it wrote to
// advanced as it goes. It returns an error only when ctx ends.
func (ds *dataset) reindexKind(ctx context.Context, kind string, advanced func(rows int)) (int, error) {
	batch := ds.svc.searchReindexBatch
	if batch <= 0 {
		batch = searchReindexBatch
	}
	redone := 0
	var held []string
	after := ""
	for {
		var page, missed []string
		var n int
		if err := ds.reindexStep(ctx, "page of "+kind, func() error {
			if hook := ds.svc.testSearchReindexHook; hook != nil {
				if err := hook(ctx, kind); err != nil {
					return err
				}
			}
			return ds.inRawTx(ctx, func(t *txn) error {
				if err := t.lockRegistryDepShared(); err != nil {
					return err
				}
				var err error
				if page, err = t.reindexPage(kind, after, batch); err != nil || len(page) == 0 {
					return err
				}
				n, missed, err = t.reindexRows(kind, page, true)
				return err
			})
		}); err != nil {
			return redone, err
		}
		if len(page) == 0 {
			break
		}
		redone += n
		held = append(held, missed...)
		after = page[len(page)-1]
		advanced(n)
		if len(page) < batch {
			break
		}
	}
	for _, id := range held {
		var n int
		if err := ds.reindexStep(ctx, "held row of "+kind, func() error {
			return ds.inRawTx(ctx, func(t *txn) error {
				if err := t.lockRegistryDepShared(); err != nil {
					return err
				}
				var err error
				n, _, err = t.reindexRows(kind, []string{id}, false)
				return err
			})
		}); err != nil {
			return redone, err
		}
		redone += n
		advanced(n)
	}
	return redone, nil
}

// reindexPage plans one page: the ids of the next batch rows of kind after
// the id the last page ended at, locked or not, so a row a write holds is
// still named and reindexRows can report it.
func (t *txn) reindexPage(kind, after string, batch int) ([]string, error) {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND id > $2 ORDER BY id LIMIT $3`,
		kind, after, batch)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// reindexRows locks the rows of kind named by ids, re-derives their `fts`
// under the published registry and reports how many it wrote and which ids
// it did not lock: a row a write holds, under skipLocked, or one that is gone.
// The caller holds the shared registry-dependency lock, so the registry read
// here is the one every write in flight derives under.
func (t *txn) reindexRows(kind string, ids []string, skipLocked bool) (int, []string, error) {
	lock := ` FOR UPDATE`
	if skipLocked {
		lock += ` SKIP LOCKED`
	}
	rows, err := t.ds.scanRows(t.ctx, t.tx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = ANY($2::text[]) ORDER BY id`+lock,
		[]any{kind, ids})
	if err != nil {
		return 0, nil, err
	}
	if err := t.rederiveFTS(t.ds.registry(), kind, rows); err != nil {
		return 0, nil, err
	}
	var missed []string
	if len(rows) < len(ids) {
		locked := make(map[string]bool, len(rows))
		for _, row := range rows {
			locked[row.ID] = true
		}
		for _, id := range ids {
			if !locked[id] {
				missed = append(missed, id)
			}
		}
	}
	return len(rows), missed, nil
}

// markSearchIndexed records that every row is indexed under this binary's
// rules.
func (t *txn) markSearchIndexed() error {
	_, err := t.exec(`
		INSERT INTO search_index (version) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET version = EXCLUDED.version, indexed_at = now()`,
		searchIndexVersion)
	return err
}
