package engine

// THE COMPACTION PASS (decision 0300). A background pass per repository that
// replaces a record's entries past its kind's retention with its state entry
// (compact.go): the latest state of every record is kept, and so is every
// entry inside the retention.
//
// WHAT IT PICKS. A kind keeps its history for all time, for a length, or for
// nothing past each record's latest state (keptFor). For a kind with a
// length, the pass's cutoff is the newest entry written before now minus the
// length; for nothing, the head. Entries are measured from their own time,
// the line's `ts`. The pass keeps, per kind, the seq it has compacted the
// kind's records up to (compaction_marks), so each pass works the window
// between that and the new cutoff: the records of the kind with a line in
// it, or an effect on another record's line in it (the foreign-effects
// index, compactindex.go), each compacted up to the cutoff by reading its
// own history, never the whole history. A kind kept for all time is left
// alone.
//
// ONE UNIT OF WORK is a batch of records of one kind, compacted in one
// transaction that holds the changelog lock, which every writer takes: a
// write waits for at most one batch. A batch is 25 records or 1,000 lines of
// their history, whichever comes first, and a record longer than that is
// compacted alone in steps of it (compactLong): what one batch's transaction
// reads is bounded by the lines, beside the transactions it re-frames and
// the repository's barriers (compactionBatchLines). After each batch the pass
// sleeps as long as the batch took, so writes have the lock at least half the
// time, and it stops at its budget and goes on at the next pass. The
// window's progress, its cutoff and the last record it finished, is kept with
// each batch's commit, so a pass stopped part way, by its budget or a crash,
// goes on at the next pass from where it stopped before it chooses another
// window, and no record a batch finished is read again. A record a package
// ships is passed by its key (shippedAmong) before anything reads its
// history.
//
// FIRST, THE INDEX. A history that came in without the write path's index
// (the upgrade, a restore, an import) is indexed by the pass, in batches,
// before it compacts anything (backfillForeign).
//
// A RECORD A PACKAGE SHIPS (shippedAmong) is left whole, for now (decision
// 0310): the pass replaces none of its entries past its kind's retention
// (compactOver) and never forgets its deletion (forgetDeletions), because a
// seed or an install reads its history to tell whether its owner changed or
// deleted it. A record the seeds' lists stop naming, and that no package's
// hand created, is the pass's like any other from then on, for what the
// pass has not gone past yet: a window, and the deletions it looked at, are
// not read again.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

// DefaultCompactionInterval is how often a repository's compaction pass
// runs when nothing says otherwise.
const DefaultCompactionInterval = time.Hour

// DefaultDeletionsKept is how long a deletion is remembered before the pass
// forgets the record: the longest retention the shipped kinds state short of
// all, so a mirror's deletions outlive its own history.
const DefaultDeletionsKept = 30 * 24 * time.Hour

// The pass's stages, which its test seam stops at (seams.go).
const (
	compactionInBatch    = "inside a batch, before its commit"
	compactionAfterBatch = "after a batch committed"
	compactionBeforeMark = "after a window's batches, before its mark moves"
	compactionInIndex    = "inside a batch of the index's derivation"
	compactionInForget   = "inside a batch that forgets deletions"
	compactionToForget   = "before a batch that forgets deletions takes the lock"
	erasureBetween       = "after a permanent delete or a forget committed, before its erasure"
)

// compactionFault is the pass's test seam: nil but in a test.
func (ds *dataset) compactionFault(stage string) error {
	if ds.svc.compactionFault == nil {
		return nil
	}
	return ds.svc.compactionFault(stage)
}

// compactionBatchRecords and compactionBatchLines bound every transaction of
// the pass, a window's batch, a held retry and a forgetting alike: the
// records it compacts and the lines of history they hold, whichever it meets
// first. Of one record a transaction reads what the record keeps below the
// step and the step, never more than twice compactionBatchLines: a record
// longer than it is compacted alone in steps of it, and one that still keeps
// as many after a step is passed by (compactLong). Across a batch's records,
// held ones included, it reads no more than compactionBatchLines. Beside
// them a transaction reads the other lines of each transaction it re-frames,
// one whose end it removes (reframedDamage), and the repository's barriers
// (compactBarriers: the lines its parked failures, its drains and its parked
// webhook fires name), which grow with what waits to be delivered, not with
// the history.
const (
	compactionBatchRecords = 25
	compactionBatchLines   = 1000
)

// compactionBudget bounds the time one pass works before it stops and leaves
// the rest to the next one.
const compactionBudget = 2 * time.Minute

// historyKept is how long a kind's history is kept: all of it, or the
// entries younger than keep, where keep 0 keeps nothing past each record's
// latest state.
type historyKept struct {
	all  bool
	keep time.Duration
}

// keptFor is how long kind's history is kept in this repository: its
// retention, or the repository's override of it (retention.go retentionOf,
// decision 0282), but where a test says otherwise.
func (ds *dataset) keptFor(ctx context.Context, kind string) (historyKept, error) {
	if ds.svc.compactionKept != nil {
		return ds.svc.compactionKept(kind), nil
	}
	r, err := ds.retentionOf(ctx, kind)
	if err != nil {
		return historyKept{}, err
	}
	if r.KeepsAll() {
		return historyKept{all: true}, nil
	}
	return historyKept{keep: r.For}, nil
}

// compactionNow is the pass's clock.
func (ds *dataset) compactionNow() time.Time {
	if ds.svc.compactionNow != nil {
		return ds.svc.compactionNow().UTC()
	}
	return nowUTC()
}

// CompactionPassReport is what one pass did.
type CompactionPassReport struct {
	// Indexed says the foreign-effects index covers the history; the pass
	// compacts nothing until it does.
	Indexed bool `json:"indexed"`
	// Kinds is each kind the pass compacted a window of, Records and
	// Removed the records it wrote a state for and the entries it removed.
	Kinds   []string `json:"kinds,omitempty"`
	Records int      `json:"records"`
	Removed int      `json:"removed"`
	// Forgotten is how many deleted records the pass forgot whole.
	Forgotten int `json:"forgotten"`
	// Erased is how many records' erasures the pass finished.
	Erased   int  `json:"erased"`
	Finished bool `json:"finished"`
	Took     time.Duration
}

// CompactionPass runs one compaction pass over the repository, bounded by
// the context and the pass's budget.
func (ds *dataset) CompactionPass(ctx context.Context) (report CompactionPassReport, err error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, compactionBudget)
	defer cancel()
	defer func() {
		if err == nil {
			ds.notePass(context.WithoutCancel(ctx), !report.Finished)
		}
	}()
	pace := func(ctx context.Context, took time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(took):
			return nil
		}
	}
	// A note of a line that does not verify goes once its row is whole
	// again; the ones that stay hold the pass below them (damage.go).
	still, err := ds.lookAtDamage(ctx)
	if err != nil {
		return report, budgetEnded(err)
	}
	ds.noticeDamage(still)
	indexed, err := ds.backfillForeign(ctx, func(ctx context.Context) error { return pace(ctx, 0) })
	report.Indexed = indexed
	if err != nil || !indexed {
		return report, budgetEnded(err)
	}
	// What a call left of an erasure (erase.go): a crash between its two
	// transactions, a hold that ended, a row written back.
	erased, err := ds.eraseNow(ctx, nil, false)
	report.Erased = len(erased.Erased)
	if err != nil {
		report.Took = time.Since(started)
		return report, budgetEnded(err)
	}
	kinds, err := ds.compactionKinds(ctx)
	if err != nil {
		return report, err
	}
	now := ds.compactionNow()
	for _, kind := range kinds {
		kept, err := ds.keptFor(ctx, kind)
		if err != nil {
			return report, err
		}
		if kept.all {
			continue
		}
		n, removed, err := ds.compactKind(ctx, kind, now.Add(-kept.keep), kept.keep == 0, pace)
		report.Records += n
		report.Removed += removed
		if n > 0 {
			report.Kinds = append(report.Kinds, kind)
		}
		if err != nil {
			report.Took = time.Since(started)
			return report, budgetEnded(err)
		}
	}
	forgotten, err := ds.forgetDeletions(ctx, now, pace)
	report.Forgotten = forgotten
	if err != nil {
		report.Took = time.Since(started)
		return report, budgetEnded(err)
	}
	report.Finished = true
	report.Took = time.Since(started)
	return report, nil
}

// FORGETTING A DELETION. A record purged longer ago than the deletions are
// kept is forgotten: every entry of it goes, its purge and its state with
// them, but for a state with neither row nor values where its effects moved
// the unit graph's revision. A client that resumed from below the line its
// deletion was on would never hear of it, so the repository's horizon moves
// to that line, and a resume from below it is told `compacted` and lists
// again (api watch.go). A record trigger reads every entry past its cursor,
// so no deletion past the lowest cursor is forgotten: a trigger behind it
// holds the horizon back, and its status says so. A permanent delete
// erases the record's history at once and keeps its deletion's line, so it
// moves no horizon; the pass forgets that line after the deletion period. A
// record a package ships is never forgotten (decision 0310): it leaves each
// page before anything acts on it, so a page of nothing else starts no
// trigger over.
//
// A forgetting reads every line of what it forgets, so it is held to the
// line bound as a compaction is: a page's records of one kind are forgotten
// as many at a time as the bound admits by their lines (lineBatch), and one
// longer than the bound alone is first compacted in steps up to the cutoff
// (compactLong), whatever its kind keeps, since all of it goes after. That
// leaves it its state with no row on its purge's line, which is forgotten
// next. One that still keeps more than the bound, under a purge of an
// earlier life or a line something holds, is passed by: its deletion stays,
// as one a hold keeps does, and the next pass looks at it again.
func (ds *dataset) forgetDeletions(ctx context.Context, now time.Time, pace func(context.Context, time.Duration) error) (int, error) {
	kept := ds.svc.deletionsKept
	if kept <= 0 {
		return 0, nil
	}
	cutoff, err := ds.cutoffBefore(ctx, now.Add(-kept))
	if err != nil {
		return 0, err
	}
	var from int64
	if err := ds.db.QueryRowContext(ctx, `SELECT coalesce((SELECT forgot_upto FROM compaction_state), 0)`).Scan(&from); err != nil {
		return 0, err
	}
	cursor := cutoff
	if held, err := ds.horizonHeldBy(ctx); err != nil {
		return 0, err
	} else if held != nil {
		cursor = held.Seq
	}
	// Never the head: a deletion forgotten from it would empty the line the
	// next write takes its seq after, and a seq is never used twice.
	head, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return 0, err
	}
	cutoff = min(cutoff, cursor, head-1)
	if cut, err := ds.knownDamage(ctx); err != nil {
		return 0, err
	} else if cut > 0 {
		cutoff = min(cutoff, cut-1)
	}
	if cutoff <= from {
		return 0, nil
	}
	forgotten, held, limit, restarted := 0, int64(0), cutoff, false
	passBy := func(seq int64) {
		if held == 0 || seq < held {
			held = seq
		}
	}
	for after := (eref{}); ; {
		page, err := ds.purgedRecords(ctx, from, cutoff, after)
		if err != nil || len(page) == 0 {
			if err != nil {
				return forgotten, err
			}
			break
		}
		after = page[len(page)-1].ref
		refs := make([]eref, len(page))
		purged := map[eref]int64{}
		for i, p := range page {
			refs[i], purged[p.ref] = p.ref, p.seq
		}
		// A purged record has no row: whether a package shipped it is read
		// from its lines, as the forgetting reads them.
		shipped, err := ds.shippedAmong(ctx, ds.db, refs)
		if err != nil {
			return forgotten, err
		}
		if refs = slices.DeleteFunc(refs, func(r eref) bool { _, ok := shipped[r]; return ok }); len(refs) == 0 {
			continue
		}
		if !restarted {
			if err := ds.startOverBelow(ctx, cutoff); err != nil {
				return forgotten, err
			}
			restarted = true
		}
		// The page is in (kind, id) order: each run of one kind is batched
		// by its lines.
		for len(refs) > 0 {
			kind := refs[0].Kind
			n := 1
			for n < len(refs) && refs[n].Kind == kind {
				n++
			}
			ids := make([]string, n)
			for i, r := range refs[:n] {
				ids[i] = r.ID
			}
			refs = refs[n:]
			lines, err := ds.recordLines(ctx, kind, ids, cutoff)
			if err != nil {
				return forgotten, err
			}
			stepped := map[string]bool{}
			for len(ids) > 0 {
				batch, total, last := ds.lineBatch(ids, lines, nil, "")
				ids = ids[slices.Index(ids, last)+1:]
				if len(batch) == 1 && total > ds.batchLines() {
					id := batch[0]
					ref := eref{Kind: kind, ID: id}
					if stepped[id] {
						passBy(purged[ref])
						continue
					}
					stepped[id] = true
					// A step takes the record off its kind's held list and
					// puts it back where a hold still keeps it; one that was
					// on it stays there until the step that finishes it.
					var wasHeld bool
					if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM compaction_held WHERE record_kind = $1 AND record_id = $2)`,
						kind, id).Scan(&wasHeld); err != nil {
						return forgotten, err
					}
					hold := func(t *txn) error {
						if !wasHeld {
							return nil
						}
						return holdRecord(t, kind, id)
					}
					passed, err := ds.compactLong(ctx, kind, id, cutoff, func(t *txn, _ CompactReport, done bool) error {
						if done {
							return nil
						}
						return hold(t)
					}, hold, pace, func(CompactReport) {})
					if err != nil {
						return forgotten, err
					}
					if passed {
						passBy(purged[ref])
						continue
					}
					now, err := ds.recordLines(ctx, kind, []string{id}, cutoff)
					if err != nil {
						return forgotten, err
					}
					lines[id] = now[id]
					ids = append([]string{id}, ids...)
					continue
				}
				batchStarted := time.Now()
				report, err := ds.forgetBatch(ctx, kind, batch, cutoff)
				if err != nil {
					return forgotten, fmt.Errorf("substrate/engine: forget deletions up to seq %d: %w", cutoff, err)
				}
				forgotten += report.Records
				limit = min(limit, report.UpTo)
				if report.heldBack > 0 {
					passBy(report.heldBack)
				}
				if err := pace(ctx, time.Since(batchStarted)); err != nil {
					return forgotten, err
				}
			}
		}
	}
	// A deletion an effect that stays holds, or one past where a trigger
	// stood when its batch ran, is looked at again by the next pass, so the
	// mark stops below it.
	cutoff = limit
	if held > 0 {
		cutoff = min(cutoff, held-1)
	}
	_, err = ds.db.ExecContext(ctx, `
		INSERT INTO compaction_state (forgot_upto) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET forgot_upto = EXCLUDED.forgot_upto, updated_at = now()`, cutoff)
	return forgotten, err
}

// forgetBatch forgets the purged records ids of kind up to cutoff, or up
// to the lowest live record trigger's cursor where it stands lower, in one
// transaction. The report's UpTo is how far it looked.
func (ds *dataset) forgetBatch(ctx context.Context, kind string, ids []string, cutoff int64) (CompactReport, error) {
	if err := ds.compactionFault(compactionToForget); err != nil {
		return CompactReport{}, err
	}
	refs := make([]eref, len(ids))
	for i, id := range ids {
		refs[i] = eref{Kind: kind, ID: id}
	}
	var report CompactReport
	if err := ds.withCompactionTx(ctx, true, func(t *txn) error {
		// Under the lock a trigger rewound or enabled since the floor was
		// read cannot move: the batch forgets nothing past where any live
		// record trigger stands now.
		upTo := cutoff
		var floor sql.NullInt64
		if err := t.row(`
			SELECT min(c.seq) FROM trigger_cursors c
			JOIN records r ON r.kind = $1 AND r.id = c.trigger_id AND r.deleted_at IS NULL
			WHERE c.seq > 0`, typeTrigger).Scan(&floor); err != nil {
			return err
		}
		if floor.Valid {
			upTo = min(upTo, floor.Int64)
		}
		var err error
		if report, err = t.forgetRecords(upTo, refs); err != nil {
			return err
		}
		report.UpTo = min(report.UpTo, upTo)
		return ds.compactionFault(compactionInForget)
	}); err != nil {
		return report, err
	}
	ds.compacted(report)
	if seen := ds.svc.compactionBatch; seen != nil {
		seen(kind, ids, report.UpTo, report.read)
	}
	return report, nil
}

// HorizonHold is the record trigger whose cursor stands lowest among the
// enabled ones: no deletion past it is forgotten.
type HorizonHold struct {
	Trigger string    `json:"trigger"`
	Seq     int64     `json:"seq"`
	Since   time.Time `json:"since"`
}

// horizonHeldBy is the enabled, live record trigger whose cursor stands
// lowest past 0, nil for none. One at 0 has delivered nothing, and from the
// beginning meets a forgotten record not at all. A trigger that is not
// enabled holds nothing back: the pass starts it over from the beginning
// before it forgets a deletion past its cursor (startOverBelow).
func (ds *dataset) horizonHeldBy(ctx context.Context) (*HorizonHold, error) {
	var h HorizonHold
	err := ds.db.QueryRowContext(ctx, `
		SELECT c.trigger_id, c.seq, c.updated_at FROM trigger_cursors c
		JOIN records r ON r.kind = $1 AND r.id = c.trigger_id AND r.deleted_at IS NULL
		WHERE coalesce((r.props ->> 'enabled')::boolean, true) AND c.seq > 0
		ORDER BY c.seq, c.trigger_id LIMIT 1`, typeTrigger).Scan(&h.Trigger, &h.Seq, &h.Since)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.Since = h.Since.UTC()
	return &h, nil
}

// startOverBelow starts every live record trigger that is not enabled, and
// whose cursor stands past 0 and below upTo, over from the beginning, before
// the pass forgets deletions up to upTo. Such a trigger holds nothing back,
// so a deletion past its cursor may go, and a walk from where it stood would
// never deliver it; from the beginning it meets each record once, in its
// latest state, and a forgotten one not at all. Its cursor is then 0, so it
// is started over once.
func (ds *dataset) startOverBelow(ctx context.Context, upTo int64) error {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT c.trigger_id, c.seq FROM trigger_cursors c
		JOIN records r ON r.kind = $1 AND r.id = c.trigger_id AND r.deleted_at IS NULL
		WHERE NOT coalesce((r.props ->> 'enabled')::boolean, true) AND c.seq > 0 AND c.seq < $2
		ORDER BY c.trigger_id`, typeTrigger, upTo)
	if err != nil {
		return err
	}
	type standing struct {
		id     string
		cursor int64
	}
	var below []standing
	for rows.Next() {
		var s standing
		if err := rows.Scan(&s.id, &s.cursor); err != nil {
			_ = rows.Close()
			return err
		}
		below = append(below, s)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, s := range below {
		ds.svc.log.Warn("substrate: a record trigger that is not enabled stands below deletions the compaction pass forgets; it starts over from the beginning",
			"repository", ds.info.ID, "trigger", s.id, "cursor", s.cursor, "forgetting", upTo)
		if err := ds.ReplayTrigger(ctx, s.id, 0); err != nil {
			return err
		}
	}
	return nil
}

// CompactionStatus is where a repository's compaction stands: its horizon,
// how far the pass looked for deletions to forget, the trigger that holds
// that back, and per kind the seq the pass compacted up to.
type CompactionStatus struct {
	Horizon    int64 `json:"horizon"`
	ForgotUpTo int64 `json:"forgotUpTo"`
	// LastPass is when the last pass ended, and LastPassEnded whether its
	// budget ended it, leaving the rest to the next.
	LastPass      *time.Time      `json:"lastPass,omitempty"`
	LastPassEnded bool            `json:"lastPassEnded,omitempty"`
	HeldBy        *HorizonHold    `json:"heldBy,omitempty"`
	Kinds         []CompactedKind `json:"kinds,omitempty"`
	// Damaged is every line a reader found not to verify: nothing at or
	// above the lowest is compacted or erased until its row is written back
	// from a whole copy (damage.go).
	Damaged []int64 `json:"damaged,omitempty"`
	// Erasures is every record whose history's erasure waits (erase.go):
	// since when, up to which line, and whether its current state stays.
	Erasures []WaitingErasure `json:"erasures,omitempty"`
}

// WaitingErasure is one record whose history waits to be erased.
type WaitingErasure struct {
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	UpTo  int64     `json:"upTo"`
	Live  bool      `json:"live"`
	Since time.Time `json:"since"`
}

// CompactedKind is one kind's compaction mark.
type CompactedKind struct {
	Kind    string `json:"kind"`
	UpTo    int64  `json:"upTo"`
	Records int64  `json:"records"`
	Removed int64  `json:"removed"`
	// Held is how many of its records wait on a hold that ends (a parked
	// failure's line, a drain's, the webhook spool) for the rest of their
	// compaction.
	Held int64 `json:"held"`
	// HeldRound is how many passes a round of their retries takes: each
	// pass retries one batch of them.
	HeldRound int64     `json:"heldRound,omitempty"`
	At        time.Time `json:"at"`
}

// CompactionStatus reads where a repository's compaction stands.
func (s *service) CompactionStatus(ctx context.Context, repository string) (CompactionStatus, error) {
	var st CompactionStatus
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return st, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return st, err
	}
	if err := ds.db.QueryRowContext(ctx, `
		SELECT coalesce((SELECT horizon FROM compaction_state), 0), coalesce((SELECT forgot_upto FROM compaction_state), 0),
			(SELECT pass_at FROM compaction_state), coalesce((SELECT pass_ended FROM compaction_state), false)`).
		Scan(&st.Horizon, &st.ForgotUpTo, &st.LastPass, &st.LastPassEnded); err != nil {
		return st, err
	}
	if st.LastPass != nil {
		at := st.LastPass.UTC()
		st.LastPass = &at
	}
	if st.HeldBy, err = ds.horizonHeldBy(ctx); err != nil {
		return st, err
	}
	damaged, err := ds.db.QueryContext(ctx, `SELECT seq FROM changelog_damage ORDER BY seq`)
	if err != nil {
		return st, err
	}
	for damaged.Next() {
		var seq int64
		if err := damaged.Scan(&seq); err != nil {
			_ = damaged.Close()
			return st, err
		}
		st.Damaged = append(st.Damaged, seq)
	}
	if err := errors.Join(damaged.Err(), damaged.Close()); err != nil {
		return st, err
	}
	pending, err := ds.pendingErasures(ctx, ds.db, nil)
	if err != nil {
		return st, err
	}
	for _, e := range pending {
		st.Erasures = append(st.Erasures, WaitingErasure{Kind: e.Kind, ID: e.ID, UpTo: e.UpTo, Live: e.Live, Since: e.Since})
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT m.kind, m.upto, m.records, m.removed,
			(SELECT count(*) FROM compaction_held h WHERE h.record_kind = m.kind), m.updated_at
		FROM compaction_marks m ORDER BY m.kind`)
	if err != nil {
		return st, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k CompactedKind
		if err := rows.Scan(&k.Kind, &k.UpTo, &k.Records, &k.Removed, &k.Held, &k.At); err != nil {
			return st, err
		}
		k.At = k.At.UTC()
		batch := int64(ds.batchRecords())
		k.HeldRound = (k.Held + batch - 1) / batch
		st.Kinds = append(st.Kinds, k)
	}
	return st, rows.Err()
}

// purgedRecord is a record purgedRecords found, and the line of its newest
// purge it looked at.
type purgedRecord struct {
	ref eref
	seq int64
}

// purgedRecords is the next batch of records, after after, whose purge is
// on a line in (from, to] and which have no row now.
func (ds *dataset) purgedRecords(ctx context.Context, from, to int64, after eref) ([]purgedRecord, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT c.kind, c.record_id, max(c.seq) FROM changelog c
		WHERE c.op = 'gc' AND c.seq > $1 AND c.seq <= $2 AND (c.kind, c.record_id) > ($3, $4)
		  AND NOT EXISTS (SELECT 1 FROM records r WHERE r.kind = c.kind AND r.id = c.record_id)
		GROUP BY c.kind, c.record_id
		ORDER BY c.kind, c.record_id LIMIT $5`, from, to, after.Kind, after.ID, ds.batchRecords())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []purgedRecord
	for rows.Next() {
		var r purgedRecord
		if err := rows.Scan(&r.ref.Kind, &r.ref.ID, &r.seq); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// forgetRecords forgets the records refs up to upTo, as forgetDeletions
// says, and moves the horizon to the newest line it took from. A record
// with an effect that stays (a line a parked failure or a drain names, a
// resync) is not forgotten in part: it is left whole, and the report's
// heldBack names its deletion's line for the caller to come back to.
//
// The horizon moves only where a deletion's own line leaves the history,
// since only then can a reader below it no longer learn of the deletion. An
// erasure keeps that line, with a state that holds no value (erase.go), so
// it moves nothing: a reader below reads on and meets the deletion.
func (t *txn) forgetRecords(upTo int64, refs []eref) (CompactReport, error) {
	t.forgetting, t.forgetHeld = true, 0
	defer func() { t.forgetting = false }()
	report, err := t.compactRecords(upTo, refs)
	report.heldBack = t.forgetHeld
	if err != nil || len(report.touched) == 0 || t.erasing {
		return report, err
	}
	return report, t.raiseHorizon(report.touched[len(report.touched)-1])
}

// raiseHorizon moves the repository's horizon up to seq, never down.
func (t *txn) raiseHorizon(seq int64) error {
	_, err := t.exec(`
		INSERT INTO compaction_state (horizon) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET horizon = greatest(compaction_state.horizon, EXCLUDED.horizon), updated_at = now()`, seq)
	return err
}

// budgetEnded reads a pass that ran out of its budget as a pass that
// stopped, not one that failed.
func budgetEnded(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// compactionKinds is every kind the repository's history addresses a line
// to, in byte order.
func (ds *dataset) compactionKinds(ctx context.Context) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		WITH RECURSIVE k(kind) AS (
			(SELECT kind FROM changelog ORDER BY kind LIMIT 1)
			UNION ALL
			SELECT (SELECT c.kind FROM changelog c WHERE c.kind > k.kind ORDER BY c.kind LIMIT 1) FROM k WHERE k.kind IS NOT NULL
		) SELECT kind FROM k WHERE kind IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// compactKind compacts one kind's history past its retention: the records a
// hold kept, one batch a pass, then the window between the kind's mark and
// the cutoff, every entry written before `before`, or every entry up to the
// head where the kind keeps nothing. A window's progress is kept with each
// batch's commit (compaction_marks.window_upto, its cutoff, and window_after,
// the last record it finished): a pass stopped anywhere, by its budget or a
// crash, goes on at the next pass from there before it chooses another
// window, so a window larger than one pass is done over several and what
// lies past it is reached.
//
// No batch compacts past the cutoff the retention in force gives the pass
// that runs it, the damage bound included: a mark, a held record and a
// stored window are progress, never permission. A stored window is lowered
// to this pass's cutoff and goes on from its last record; where that leaves
// nothing above the mark it is dropped and the mark stays, so a retention
// lengthened since takes effect for every record this pass has not reached,
// and what earlier passes compacted stays as they left it. A line known not
// to verify below the mark takes the mark below it, so once the line is
// written back the pass reads again every record above it.
func (ds *dataset) compactKind(ctx context.Context, kind string, before time.Time, nothing bool,
	pace func(context.Context, time.Duration) error,
) (records, removed int, err error) {
	var cutoff, mark, windowUpTo int64
	var heldAfter, windowAfter string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT coalesce(m.upto, 0), coalesce(m.held_after, ''), coalesce(m.window_upto, 0), coalesce(m.window_after, '')
		FROM (SELECT 1) one LEFT JOIN compaction_marks m ON m.kind = $1`, kind).Scan(&mark, &heldAfter, &windowUpTo, &windowAfter); err != nil {
		return 0, 0, err
	}
	if nothing {
		cutoff, err = tableChangelogHead(ctx, ds.db)
	} else {
		cutoff, err = ds.cutoffBefore(ctx, before)
	}
	if err != nil {
		return 0, 0, err
	}
	if cut, err := ds.knownDamage(ctx); err != nil {
		return 0, 0, err
	} else if cut > 0 {
		cutoff = min(cutoff, cut-1)
		if cut-1 < mark {
			mark, windowUpTo, windowAfter = cut-1, 0, ""
			if _, err := ds.db.ExecContext(ctx, dropWindow, kind, mark); err != nil {
				return 0, 0, err
			}
		}
	}
	tally := func(r CompactReport) {
		records += r.Records + r.Triggers
		removed += r.Removed
	}
	// The records a hold kept, first: their history is below the mark,
	// where no window reads. One batch a pass, in turn from where the last
	// pass's got to, so what stays held takes no more of a pass than that.
	ids, err := ds.heldRecords(ctx, kind, heldAfter)
	if err == nil && len(ids) == 0 && heldAfter != "" {
		ids, err = ds.heldRecords(ctx, kind, "")
	}
	if err != nil {
		return records, removed, err
	}
	if len(ids) > 0 {
		last, err := ds.compactHeld(ctx, kind, ids, cutoff, mark, pace, tally)
		if err != nil {
			return records, removed, err
		}
		next := ""
		if last != ids[len(ids)-1] || len(ids) == ds.batchRecords() {
			next = last
		}
		if _, err := ds.db.ExecContext(ctx, `
			INSERT INTO compaction_marks (kind, held_after) VALUES ($1, $2)
			ON CONFLICT (repository, kind) DO UPDATE SET held_after = EXCLUDED.held_after`, kind, next); err != nil {
			return records, removed, err
		}
	}
	for {
		stored := windowUpTo > mark
		windowUpTo = min(windowUpTo, cutoff)
		if windowUpTo <= mark {
			if stored {
				if _, err := ds.db.ExecContext(ctx, dropWindow, kind, mark); err != nil {
					return records, removed, err
				}
			}
			if cutoff <= mark {
				return records, removed, nil
			}
			windowUpTo, windowAfter = cutoff, ""
		}
		if err := ds.compactWindow(ctx, kind, mark, windowUpTo, windowAfter, pace, tally); err != nil {
			return records, removed, err
		}
		if err := ds.compactionFault(compactionBeforeMark); err != nil {
			return records, removed, err
		}
		// A batch that met a line that does not verify compacted below it
		// alone, so the mark stops there, lower than it stood where it must.
		upto := windowUpTo
		if cut, err := ds.knownDamage(ctx); err != nil {
			return records, removed, err
		} else if cut > 0 {
			upto, cutoff = min(upto, cut-1), min(cutoff, cut-1)
		}
		if _, err := ds.db.ExecContext(ctx, dropWindow, kind, upto); err != nil {
			return records, removed, err
		}
		mark, windowUpTo, windowAfter = upto, 0, ""
	}
}

// dropWindow sets a kind's mark ($2) and forgets its window.
const dropWindow = `
	INSERT INTO compaction_marks (kind, upto) VALUES ($1, $2)
	ON CONFLICT (repository, kind) DO UPDATE SET upto = EXCLUDED.upto,
		window_upto = 0, window_after = '', updated_at = now()`

// lineBatch is the next batch of ids, in order after the ones skip passes
// by: as many as the line bound admits by their lines up to the batch's
// seq, at least one. It answers the batch, its lines and the last id it
// took or passed by.
func (ds *dataset) lineBatch(ids []string, lines map[string]int, skip func(string) bool, last string) ([]string, int, string) {
	var batch []string
	total := 0
	for _, id := range ids {
		if skip != nil && skip(id) {
			last = id
			continue
		}
		n := lines[id]
		if len(batch) > 0 && total+n > ds.batchLines() {
			break
		}
		batch, total, last = append(batch, id), total+n, id
		if total >= ds.batchLines() {
			break
		}
	}
	return batch, total, last
}

// compactWindow compacts the records of kind with a line in (from, to] in
// byte order after after, batch by batch, each batch's commit keeping how far
// the window got. A batch is the next records the line bound admits
// (lineBatch). A record a package ships is passed by its key (shippedAmong)
// before anything reads its history, and one longer than the line bound alone
// is compacted in steps (compactLong).
func (ds *dataset) compactWindow(ctx context.Context, kind string, from, to int64, after string,
	pace func(context.Context, time.Duration) error, tally func(CompactReport),
) error {
	for {
		ids, err := ds.windowRecords(ctx, kind, from, to, after)
		if err != nil || len(ids) == 0 {
			return err
		}
		refs := make([]eref, len(ids))
		for i, id := range ids {
			refs[i] = eref{Kind: kind, ID: id}
		}
		shipped, err := ds.shippedAmong(ctx, ds.db, refs)
		if err != nil {
			return err
		}
		ships := func(id string) bool {
			_, ok := shipped[eref{Kind: kind, ID: id}]
			return ok
		}
		var open []string
		for _, id := range ids {
			if !ships(id) {
				open = append(open, id)
			}
		}
		lines, err := ds.recordLines(ctx, kind, open, to)
		if err != nil {
			return err
		}
		batch, total, last := ds.lineBatch(ids, lines, ships, after)
		started := time.Now()
		switch {
		case len(batch) == 0:
			// Every record of the page ships: the window moves past them.
			if _, err := ds.db.ExecContext(ctx, windowProgress, kind, to, last, 0, 0); err != nil {
				return err
			}
		case len(batch) == 1 && total > ds.batchLines():
			id := batch[0]
			if _, err := ds.compactLong(ctx, kind, id, to, func(t *txn, r CompactReport, done bool) error {
				finished := after
				if done {
					finished = last
				}
				_, err := t.exec(windowProgress, kind, to, finished, r.Records+r.Triggers, r.Removed)
				return err
			}, func(t *txn) error { return holdRecord(t, kind, id) }, pace, tally); err != nil {
				return err
			}
		default:
			report, err := ds.compactBatch(ctx, kind, batch, to, func(t *txn, r CompactReport) error {
				_, err := t.exec(windowProgress, kind, to, last, r.Records+r.Triggers, r.Removed)
				return err
			})
			tally(report)
			if err != nil {
				return err
			}
		}
		after = last
		if err := pace(ctx, time.Since(started)); err != nil {
			return err
		}
	}
}

// windowProgress keeps a window's progress, the window's cutoff ($2) and
// the last record it finished ($3), and counts a batch's records ($4) and
// removed entries ($5) into the kind's.
const windowProgress = `
	INSERT INTO compaction_marks (kind, window_upto, window_after, records, removed) VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (repository, kind) DO UPDATE SET window_upto = EXCLUDED.window_upto,
		window_after = EXCLUDED.window_after,
		records = compaction_marks.records + EXCLUDED.records,
		removed = compaction_marks.removed + EXCLUDED.removed, updated_at = now()`

// compactHeld retries one batch of kind's held records, ids in byte order
// from where the last retry got to, by the window's rule up to the cutoff
// in force: as many as the line bound admits, or a longer one alone in steps
// (compactLong). Each stays held where a hold still keeps it, where it still
// keeps as many lines as the bound, where the cutoff stands below the mark,
// whose lines between the two a later pass compacts, and, for a longer one,
// until the step that finishes it. It answers the last record it took.
func (ds *dataset) compactHeld(ctx context.Context, kind string, ids []string, cutoff, mark int64,
	pace func(context.Context, time.Duration) error, tally func(CompactReport),
) (string, error) {
	lines, err := ds.recordLines(ctx, kind, ids, cutoff)
	if err != nil {
		return "", err
	}
	batch, total, last := ds.lineBatch(ids, lines, nil, "")
	keep := func(t *txn) error {
		if cutoff >= mark {
			return nil
		}
		for _, id := range batch {
			if err := holdRecord(t, kind, id); err != nil {
				return err
			}
		}
		return nil
	}
	started := time.Now()
	if len(batch) == 1 && total > ds.batchLines() {
		id := batch[0]
		_, err = ds.compactLong(ctx, kind, id, cutoff, func(t *txn, _ CompactReport, done bool) error {
			// A held record leaves the list only in the step that finishes
			// it: every other step leaves it there, so a pass stopped between
			// two goes on with it at the next.
			if !done {
				if err := holdRecord(t, kind, id); err != nil {
					return err
				}
			}
			return keep(t)
		}, func(t *txn) error { return holdRecord(t, kind, id) }, pace, tally)
	} else {
		var report CompactReport
		report, err = ds.compactBatch(ctx, kind, batch, cutoff, func(t *txn, _ CompactReport) error { return keep(t) })
		tally(report)
	}
	if err != nil {
		return "", err
	}
	return last, pace(ctx, time.Since(started))
}

// compactLong compacts one record whose history up to to is longer than the
// line bound in steps, each a transaction of its own up to the seq of the
// record's next line-bound-th line after the step before: the state a step
// leaves stands for all it replaced, so the next step replays that state, what
// the record keeps below it, and the bound's lines past the step before.
// What a compaction keeps (a line something holds, a purge that is not the
// record's last, every effect under them) stays and is read again, so after
// each step the record's lines at or below it are counted, in the step's own
// transaction: where they are still as many as the bound, the step gained
// nothing that lets the next stay within twice it, and the pass stops
// stepping and passes the record by, through passBy in the same transaction.
// No step therefore reads more than twice the bound of the record, and a
// record that cannot be compacted within it costs a bounded read at each
// later pass. progress runs in each step's transaction, done where the step
// is the last or the record is passed by; it answers whether it was.
func (ds *dataset) compactLong(ctx context.Context, kind, id string, to int64,
	progress func(t *txn, r CompactReport, done bool) error, passBy func(t *txn) error,
	pace func(context.Context, time.Duration) error, tally func(CompactReport),
) (bool, error) {
	for prev := int64(0); ; {
		step, err := ds.lineAt(ctx, kind, id, prev, to, ds.batchLines())
		if err != nil {
			return false, err
		}
		final := step >= to
		if final {
			step = to
		}
		done, passed := final, false
		started := time.Now()
		report, err := ds.compactBatch(ctx, kind, []string{id}, step, func(t *txn, r CompactReport) error {
			done, passed = final, false
			if !final {
				var kept int
				if err := t.row(`
					SELECT (SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND seq <= $3)
					     + (SELECT count(*) FROM changelog_foreign WHERE record_kind = $1 AND record_id = $2 AND seq <= $3)`,
					kind, id, step).Scan(&kept); err != nil {
					return err
				}
				if kept >= ds.batchLines() {
					if err := passBy(t); err != nil {
						return err
					}
					done, passed = true, true
				}
			}
			return progress(t, r, done)
		})
		tally(report)
		if err != nil || done {
			return passed, err
		}
		prev = step
		if err := pace(ctx, time.Since(started)); err != nil {
			return false, err
		}
	}
}

// holdRecord puts the record on the held list (compaction_held), for the
// pass to retry.
func holdRecord(t *txn, kind, id string) error {
	_, err := t.exec(`INSERT INTO compaction_held (record_kind, record_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, kind, id)
	return err
}

// compactBatch compacts the records ids of kind up to upTo in one
// transaction, notes the ones a hold that ends kept part of for the next
// pass (compaction_held), and runs progress inside the same transaction.
func (ds *dataset) compactBatch(ctx context.Context, kind string, ids []string, upTo int64,
	progress func(*txn, CompactReport) error,
) (CompactReport, error) {
	refs := make([]eref, len(ids))
	for i, id := range ids {
		refs[i] = eref{Kind: kind, ID: id}
	}
	var report CompactReport
	if err := ds.withCompactionTx(ctx, true, func(t *txn) error {
		var err error
		if report, err = t.compactRecords(upTo, refs); err != nil {
			return err
		}
		if _, err := t.exec(`DELETE FROM compaction_held WHERE record_kind = $1 AND record_id = ANY($2)`, kind, ids); err != nil {
			return err
		}
		for _, r := range report.held {
			if _, err := t.exec(`INSERT INTO compaction_held (record_kind, record_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, r.Kind, r.ID); err != nil {
				return err
			}
		}
		if progress != nil {
			if err := progress(t, report); err != nil {
				return err
			}
		}
		return ds.compactionFault(compactionInBatch)
	}); err != nil {
		return report, fmt.Errorf("substrate/engine: compact %s up to seq %d: %w", kind, upTo, err)
	}
	ds.compacted(report)
	if seen := ds.svc.compactionBatch; seen != nil {
		seen(kind, ids, upTo, report.read)
	}
	return report, ds.compactionFault(compactionAfterBatch)
}

// recordLines is how many lines up to to each record of ids of kind has: its
// own and the ones that carry an effect on it for another record, what a
// compaction of it reads.
func (ds *dataset) recordLines(ctx context.Context, kind string, ids []string, to int64) (map[string]int, error) {
	out := map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, count(*) FROM (
			SELECT record_id AS id FROM changelog WHERE kind = $1 AND record_id = ANY($2) AND seq <= $3
			UNION ALL
			SELECT record_id FROM changelog_foreign WHERE record_kind = $1 AND record_id = ANY($2) AND seq <= $3
		) l GROUP BY id`, kind, ids, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// lineAt is the seq of the n-th line of the record, its own or one carrying
// an effect on it, after after and up to to, or to where it has fewer.
func (ds *dataset) lineAt(ctx context.Context, kind, id string, after, to int64, n int) (int64, error) {
	var seq int64
	err := ds.db.QueryRowContext(ctx, `
		SELECT seq FROM (
			SELECT seq FROM changelog WHERE kind = $1 AND record_id = $2 AND seq > $3 AND seq <= $4
			UNION
			SELECT seq FROM changelog_foreign WHERE record_kind = $1 AND record_id = $2 AND seq > $3 AND seq <= $4
		) l ORDER BY seq OFFSET $5 LIMIT 1`, kind, id, after, to, n-1).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return to, nil
	}
	return seq, err
}

// batchRecords and batchLines are the bounds of one batch: the records, and
// the lines of history they hold, a batch's transaction reads.
func (ds *dataset) batchRecords() int {
	if n := ds.svc.compactionBatches[0]; n > 0 {
		return n
	}
	return compactionBatchRecords
}

func (ds *dataset) batchLines() int {
	if n := ds.svc.compactionBatches[1]; n > 0 {
		return n
	}
	return compactionBatchLines
}

// cutoffBefore is the newest seq whose line was written before t, 0 for
// none: the history is read backward from its head, so the cost is the
// entries inside the retention.
func (ds *dataset) cutoffBefore(ctx context.Context, t time.Time) (int64, error) {
	var seq int64
	err := ds.db.QueryRowContext(ctx, `
		SELECT coalesce((SELECT seq FROM changelog WHERE ts < $1 ORDER BY seq DESC LIMIT 1), 0)`, t.UTC()).Scan(&seq)
	return seq, err
}

// notePass records when a pass ended and whether its budget ended it, for
// the operator's status: a pass that keeps ending on its budget is a pass
// that does not keep up.
func (ds *dataset) notePass(ctx context.Context, ended bool) {
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO compaction_state (pass_at, pass_ended) VALUES (now(), $1)
		ON CONFLICT (repository) DO UPDATE SET pass_at = EXCLUDED.pass_at, pass_ended = EXCLUDED.pass_ended, updated_at = now()`, ended); err != nil {
		ds.svc.log.Warn("substrate: the compaction pass's end was not recorded", "repository", ds.info.ID, "error", err)
	}
}

// heldRecords is the next batch of records of kind, in byte order after
// after, that a hold that ends kept part of (compaction_held).
func (ds *dataset) heldRecords(ctx context.Context, kind, after string) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT record_id FROM compaction_held WHERE record_kind = $1 AND record_id > $2
		ORDER BY record_id LIMIT $3`, kind, after, ds.batchRecords())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// windowRecords is the next batch of records of kind, in byte order after
// after, with a line or an effect on another record's line in (from, to].
func (ds *dataset) windowRecords(ctx context.Context, kind string, from, to int64, after string) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id FROM (
			SELECT DISTINCT record_id AS id FROM changelog
			WHERE kind = $1 AND seq > $2 AND seq <= $3 AND record_id > $4
			UNION
			SELECT DISTINCT record_id FROM changelog_foreign
			WHERE record_kind = $1 AND seq > $2 AND seq <= $3 AND record_id > $4
		) w ORDER BY id LIMIT $5`, kind, from, to, after, ds.batchRecords())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return out, rows.Err()
}

// startCompactor runs the repository's compaction passes, one per interval,
// in the server's process.
func (ds *dataset) startCompactor() {
	if !ds.svc.compacting {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ds.compactionStop = func() {
		cancel()
		<-done
	}
	started := ds.spawn("compaction", func(bg context.Context) {
		defer close(done)
		stop := context.AfterFunc(bg, cancel)
		defer stop()
		tick := time.NewTicker(ds.svc.compactionInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			report, err := ds.CompactionPass(ctx)
			switch {
			case err != nil && ctx.Err() == nil:
				ds.svc.log.Error("substrate: a compaction pass failed; the next one tries again",
					"repository", ds.info.ID, "error", err)
			case report.Removed > 0:
				ds.svc.log.Info("substrate: compacted history past its retention",
					"repository", ds.info.ID, "kinds", report.Kinds, "records", report.Records,
					"removed", report.Removed, "finished", report.Finished, "took", report.Took)
			}
		}
	})
	if !started {
		close(done)
	}
}

// stopCompactor ends the repository's passes; a batch in flight rolls back.
func (ds *dataset) stopCompactor() {
	if ds.compactionStop != nil {
		ds.compactionStop()
	}
}

// HistoryKind is one kind's share of a repository's history: the lines
// addressed to its records and the bytes their payloads hold as stored.
type HistoryKind struct {
	Kind    string `json:"kind"`
	Entries int64  `json:"entries"`
	Bytes   int64  `json:"bytes"`
	// Oldest is when the kind's oldest kept line was written.
	Oldest time.Time `json:"oldest"`
}

// HistoryByKind measures a repository's history by kind, largest first: the
// numbers a kind's retention is chosen from. Postgres sums it, so nothing of
// the history is loaded into this process.
func (s *service) HistoryByKind(ctx context.Context, repository string) ([]HistoryKind, error) {
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return nil, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, count(*), sum(octet_length(payload::text)), min(ts)
		FROM changelog GROUP BY kind ORDER BY 3 DESC, 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []HistoryKind
	for rows.Next() {
		var h HistoryKind
		if err := rows.Scan(&h.Kind, &h.Entries, &h.Bytes, &h.Oldest); err != nil {
			return nil, err
		}
		h.Oldest = h.Oldest.UTC()
		out = append(out, h)
	}
	return out, rows.Err()
}
