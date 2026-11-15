package engine

// AN ERASURE IN STEPS (decision 0330). An erasure (erase.go) reads and
// rewrites what holds a record's values: the record's history up to its
// intent, the saved positions of drains over its lines, the copies merges
// hold of its values, its intents. No transaction of it reads more than the
// compaction pass's bounds (compactpass.go compactionBatchLines,
// compactionBatchBytes), whatever the record's size: one whose every part
// fits is erased in one transaction, as it always was (eraseOne); a larger
// one in steps, each a transaction of its own, until what is left fits and
// the last step erases it as eraseOne does.
//
// THE STEPS, in order, each read again from the history and the progress
// row at every step, so a crash, a restart or a spent budget leaves the
// erasure pending and the next step goes on from what the history now holds:
//
//   - the search: the saved positions that hold the record's values ride
//     the trigger lines above the record's first line (checkpointLines). A
//     step reads one range of them, held to the bounds, strips what it finds
//     and keeps how far it has read on the erasure's progress row
//     (compaction_erasure_progress.searched): the one thing the history does
//     not carry. A rebuild keeps the row, the lines it read unchanged; a
//     restore or an import starts without it, and a position stripped no
//     longer matches, so the search read again finds nothing twice. It goes
//     first, while the record's lines it matches against are whole.
//   - the merge copies: the merge and split lines whose payload copies the
//     record's values, a batch held to the bounds a step (stripMergeCopies).
//     A line stripped carries no copy, so a second strip is a no-op.
//   - the history: the erasure's own compaction up to a step's line
//     (erasureStepAt), never a plain one, so a step crosses a merge's or a
//     split's resync as the last step does, and a state between steps holds
//     the record's values as of its step. A purged record's steps below its
//     purge line are a live erasure's; its last step is the forgetting. A
//     step never spans an earlier purge of the record: it ends on the
//     purge's line, whose place its state takes, so a purge a live erasure
//     keeps (formerBelow) never holds a purged record's steps.
//   - a range of the search with no trigger line costs no step: the step
//     that follows notes it read in its own transaction.
//   - the last step: eraseOne, as one transaction, once the record's lines,
//     the merge copies left and the rest of the search fit within the bounds
//     together. It takes the intents off their lines and the rows out.
//
// What holds an erasure holds each step (a line that does not verify, an
// index still derived, a line a parked failure or a drain names), and the
// reason is reported as it always was. An erasure reads pending, wherever
// it is reported, until its last step commits.
//
// THE BOUNDS count what a step reads again of the record: the state the
// step before left, the lines of its merges and its id trail below the
// first line a step takes. A step takes fewer new lines for them, and
// always one, so where they alone pass a bound (a record that won more
// merges than half the line bound) each step reads them and one new line:
// past the bounds by what the record retains, and by nothing else.
//
// THE SHARE. A request does one step of each erasure it starts and answers
// pending where one is left. A compaction pass spends at most erasureShare
// of its budget on pending erasures, a step of each in turn, so a small one
// behind a large one finishes in its first step, then compacts. A step that
// fails on its own (a step refused, a line found not to verify) notes why on
// the erasure's progress row and the pass goes on to the next and to the
// compaction; only the server's failures end it.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/substrate"
)

// erasureShare is the part of a pass's budget its pending erasures take
// before it compacts: a quarter, so an erasure in steps, however long and
// however it fails, leaves the rest of every pass to the compaction.
const erasureShare = compactionBudget / 4

// erasureShare is the pass's share for its erasures, a test's where it
// sets one (WithTestErasureShare).
func (ds *dataset) erasureShare() time.Duration {
	if s := ds.svc.erasureShare; s > 0 {
		return s
	}
	return erasureShare
}

// erasureStep is what one step of an erasure did: done once the erasure
// finished, held with why where something holds it, and otherwise where it
// stands, for the reply.
type erasureStep struct {
	done    bool
	held    string
	damaged int64
	at      string
	report  CompactReport
}

// eraseNow does what compaction_erasures holds for the refs only names (every
// record where only is nil), a step at a time, each in a transaction of its
// own; finish passes every trigger it would wait for. With until zero, as a
// request has it, each erasure takes one step; otherwise rounds of a step of
// each go on until until, pacing each step by pace, while a round moves one.
// A step that fails on its own is noted and the next goes on. What it leaves
// is in the report, with what holds it or where it stands.
func (ds *dataset) eraseNow(ctx context.Context, only []eref, finish bool, until time.Time,
	pace func(context.Context, time.Duration) error,
) (EraseReport, error) {
	var report EraseReport
	// A note whose row was written back goes first (damage.go).
	if _, err := ds.lookAtDamage(ctx); err != nil {
		return report, err
	}
	left := map[eref]pendingErasure{}
	settled := map[eref]bool{}
	steps := 0
	for {
		pending, err := ds.pendingErasures(ctx, ds.db, only)
		if err != nil {
			return report, err
		}
		if err := ds.leastRecentlyStepped(ctx, pending); err != nil {
			return report, err
		}
		moved, spent := false, false
		for _, e := range pending {
			ref := eref{Kind: e.Kind, ID: e.ID}
			if settled[ref] {
				continue
			}
			// The share is held before every step but the call's first, so
			// a pass moves its erasures however small its share and never
			// spends its budget inside a round of them.
			if spent = spent || !until.IsZero() && steps > 0 && !time.Now().Before(until); spent {
				if _, ok := left[ref]; !ok {
					left[ref] = e
				}
				continue
			}
			if !finish && !e.Live {
				waits, err := ds.erasureWaits(ctx, e.Kind, e.UpTo)
				if err != nil {
					return report, err
				}
				if len(waits) > 0 {
					e.Waits = waits
					left[ref], settled[ref] = e, true
					continue
				}
			}
			started := time.Now()
			steps++
			step, err := ds.eraseStep(ctx, e)
			switch {
			case err != nil && erasureServerFailure(ctx, err):
				return report, fmt.Errorf("substrate/engine: erase %s %s: %w", e.Kind, e.ID, err)
			case err != nil:
				e.Held = fmt.Sprintf("its last step failed: %v", err)
				if nerr := ds.noteErasureHeld(ctx, ref, e.Held); nerr != nil {
					return report, nerr
				}
				ds.svc.log.Warn("substrate: a step of an erasure failed; the pass goes on to the next and tries it again at the next pass",
					"repository", logSafeID(ds.info.ID), "kind", logSafeID(e.Kind), "id", logSafeID(e.ID), "error", err)
				left[ref], settled[ref] = e, true
				continue
			}
			ds.compacted(step.report)
			ds.sawBatch(e.Kind, []string{e.ID}, e.UpTo, step.report)
			switch {
			case step.done:
				report.Erased = append(report.Erased, ref)
				report.Removed += step.report.Removed
				delete(left, ref)
				settled[ref] = true
			case step.held != "":
				e.Held, e.Damaged = step.held, step.damaged
				left[ref], settled[ref] = e, true
			default:
				e.Held = step.at
				left[ref] = e
				moved = true
			}
			if pace != nil {
				if err := pace(ctx, time.Since(started)); err != nil {
					return report, err
				}
			}
		}
		if spent || until.IsZero() || !moved || !time.Now().Before(until) {
			break
		}
	}
	for _, ref := range slices.SortedFunc(func(yield func(eref) bool) {
		for r := range left {
			if !yield(r) {
				return
			}
		}
	}, func(a, b eref) int { return strings.Compare(a.Kind+"/"+a.ID, b.Kind+"/"+b.ID) }) {
		report.Waiting = append(report.Waiting, left[ref])
	}
	return report, nil
}

// erasureServerFailure is whether err is the server's, which ends a pass,
// rather than an erasure's own: the context, the budget, a connection lost
// or refused, the repository's writer lease.
func erasureServerFailure(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, ErrRepositoryHasAnotherWriter) || errors.Is(err, ErrWriterLeaseLost) || errors.Is(err, ErrWriterLeaseStale) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Connection exceptions, insufficient resources, operator
		// intervention (a shutdown, a cancel), a system error. Classes 40 (a
		// deadlock, a serialization failure) and XX (data or an index found
		// corrupt) are the erasure's own here, where a start's failureOf
		// (setaside.go) names them the server's: they end the step that met
		// them, which the next pass tries again, and the erasures behind it
		// and the compaction go on, where a repository that cannot open has
		// nothing else to go on with.
		switch pgErr.Code[:min(2, len(pgErr.Code))] {
		case "08", "53", "57", "58":
			return true
		}
	}
	return pgconn.Timeout(err)
}

// eraseStep takes one step of e's erasure in a transaction of its own.
func (ds *dataset) eraseStep(ctx context.Context, e pendingErasure) (erasureStep, error) {
	var step erasureStep
	err := ds.withCompactionTx(ctx, true, func(t *txn) error {
		var err error
		if step, err = t.eraseStepIn(e); err != nil {
			return err
		}
		if !step.done {
			if err := t.noteErasureStepped(eref{Kind: e.Kind, ID: e.ID}); err != nil {
				return err
			}
		}
		return ds.compactionFault(erasureInStep + ": " + e.Kind + "/" + e.ID)
	})
	if err != nil {
		return step, err
	}
	return step, ds.compactionFault(erasureAfterStep)
}

// eraseStepIn is one step of e's erasure in the caller's transaction, which
// holds the changelog lock: the last step where everything left fits within
// the bounds, else the next range of the search, the next batch of merge
// copies, or the next step of the history, in that order.
func (t *txn) eraseStepIn(e pendingErasure) (erasureStep, error) {
	var step erasureStep
	ref := eref{Kind: e.Kind, ID: e.ID}
	// What remains is read again under the changelog lock: another call may
	// have finished it, or a later intent widened it, since it was read.
	fresh, err := t.ds.pendingErasures(t.ctx, t.tx, []eref{ref})
	if err != nil {
		return step, err
	}
	if len(fresh) == 0 {
		step.done = true
		return step, nil
	}
	e.UpTo, e.Live, e.Lines = fresh[0].UpTo, fresh[0].Live, fresh[0].Lines
	searched, err := t.erasureSearched(ref, e.UpTo)
	if err != nil {
		return step, err
	}
	first, err := t.firstLineOf(ref, e.UpTo)
	if err != nil {
		return step, err
	}
	head, err := tableChangelogHead(t.ctx, t.tx)
	if err != nil {
		return step, err
	}
	from := max(searched, first)
	search, err := t.searchRange(from, head)
	if err != nil {
		return step, err
	}
	// A range that holds no trigger line holds no saved position: it is
	// noted read in this step's transaction and costs no step, so the step
	// after a pause on a repository that is written to is not spent on it.
	if search.to > from && search.size.lines == 0 {
		if err := t.noteErasureSearched(ref, search.to, e.UpTo); err != nil {
			return step, err
		}
		from = search.to
	}
	// The merge copies and the history are found through the
	// foreign-effects index, so a step waits until it covers the history.
	if covered, err := t.indexCovers(e.UpTo); err != nil {
		return step, err
	} else if !covered {
		// A derivation stops below a line that does not verify, and that
		// line is what holds the erasure, as it holds a step (historyStep).
		if cut, err := t.damageCut(); err != nil {
			return step, err
		} else if cut > 0 && cut <= e.UpTo {
			step.held, step.damaged = damagedHold(cut), cut
			return step, nil
		}
		step.held = "the history's index is still being derived: the next pass derives it and finishes the erasure"
		return step, nil
	}
	merges, err := t.mergeCopies(ref, e.UpTo)
	if err != nil {
		return step, err
	}
	// What the record retains below its first line a step can take (the
	// state the step before left, its merges' lines, its id trail) every
	// step reads again, so it counts in both bounds (the file's header).
	taken, err := t.firstTakenLine(ref, e.UpTo)
	if err != nil {
		return step, err
	}
	// A resync after the record's state on the state's own line restates
	// the record there, and the step that puts its state above takes it:
	// a step counts from past that line, which it cannot change.
	after := taken - 1
	var retained, record lineSize
	if taken > 0 {
		if placed, err := t.stateLine(ref, taken); err != nil {
			return step, err
		} else if placed == taken {
			after = taken
		}
		if retained, err = t.recordLinesAbove(ref, 0, after); err != nil {
			return step, err
		}
		if record, err = t.recordLinesAbove(ref, after, e.UpTo); err != nil {
			return step, err
		}
	}
	left := retained.plus(record).plus(search.size)
	for _, m := range merges {
		left = left.plus(m.size)
	}
	switch {
	case search.whole && t.ds.within(left):
		return t.lastErasureStep(e, from)
	case search.to > from:
		return t.searchStep(ref, e, from, search)
	case len(merges) > 0:
		return t.mergeStep(ref, merges)
	case taken == 0:
		return t.lastErasureStep(e, from)
	}
	at, err := t.erasureStepAt(ref, after, e.UpTo, retained)
	if err != nil {
		return step, err
	}
	// A state goes on a line of the record's own (compact.go), so a step
	// that ends on another record's line, a merge's or a split's, reaches
	// the record's next own line that carries an effect of it: its state
	// goes there, above every line the step takes.
	if place, err := t.nextOwnLine(ref, at, e.UpTo); err != nil {
		return step, err
	} else if place == 0 {
		at = e.UpTo
	} else {
		at = max(at, place)
	}
	// A step never spans a purge of the record: it ends on the purge's line,
	// where its state takes the purge's place, holding no row, and replays
	// as the purge does. A step past it would keep a purge that took a
	// former id below (formerBelow) and be held at every pass, where the
	// last step, a purged record's forgetting, crosses every purge.
	if purge, err := t.purgeLineIn(ref, taken, at); err != nil {
		return step, err
	} else if purge > 0 {
		at = purge
	}
	if at >= e.UpTo {
		return t.lastErasureStep(e, from)
	}
	return t.historyStep(ref, e, at)
}

// takenKinds are the effects on a record a step of its erasure replaces:
// every effect of its two families but the states that stand for them.
var takenKinds = []foldKind{
	foldRecord, foldTombstone, foldPurge, foldAnnotation, foldMark, foldUnmark,
	foldCursor, foldSchedule, foldPark, foldUnpark, foldPage, foldUnpage, foldForget,
}

// firstTakenLine is the first of ref's lines up to upTo that carries an
// effect a step of its erasure takes: one of takenKinds on it, or a resync
// that restates an annotation or a mark row of its (erase.go resyncWithout).
// Below it lie only the lines the record retains whatever step comes, the
// states the steps before left, the lines of its merges whose resyncs no
// longer restate its values and the lines its id trail rides; from it on,
// every line is new to the stepping. It is read
// from the history at every step, so a rebuild, a restore or an import
// between two steps loses nothing, and in order of seq it stops at the
// first such line: it reads what the record retains, which a step reads
// too, and one line. 0 where none is left.
func (t *txn) firstTakenLine(ref eref, upTo int64) (int64, error) {
	kinds := make([]string, len(takenKinds))
	for i, k := range takenKinds {
		kinds[i] = `@.kind == "` + string(k) + `"`
	}
	var seq int64
	err := t.row(`
		SELECT seq FROM changelog WHERE seq <= $3
		  AND seq IN (SELECT seq FROM changelog WHERE kind = $1 AND record_id = $2
		              UNION SELECT seq FROM changelog_foreign WHERE record_kind = $1 AND record_id = $2)
		  AND jsonb_path_exists(payload, '$.fold[*] ? ((@.ref == $k && @.id == $i && (`+strings.Join(kinds, " || ")+`))
		      || (@.kind == "`+string(foldResync)+`" && (exists(@.rows.annotations[*] ? (@.kind == $k && @.id == $i))
		          || exists(@.rows.marks[*] ? (@.kind == $k && @.id == $i)))))',
		      jsonb_build_object('k', $1::text, 'i', $2::text))
		ORDER BY seq LIMIT 1`, ref.Kind, ref.ID, upTo).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq, err
}

// nextOwnLine is the first line addressed to ref, from from up to upTo,
// that carries an effect on it of either family, a state's or a ledger's
// included: a line a state of it can go on. 0 where none is.
func (t *txn) nextOwnLine(ref eref, from, upTo int64) (int64, error) {
	var seq int64
	err := t.row(`
		SELECT seq FROM changelog WHERE kind = $1 AND record_id = $2 AND seq >= $3 AND seq <= $4
		  AND jsonb_path_exists(payload, '$.fold[*] ? (@.ref == $k && @.id == $i && @.kind != "`+string(foldFormerID)+`" && @.kind != "`+string(foldErase)+`")',
		      jsonb_build_object('k', $1::text, 'i', $2::text))
		ORDER BY seq LIMIT 1`, ref.Kind, ref.ID, from, upTo).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq, err
}

// indexCovers is whether the foreign-effects index covers the history up to
// upTo (compactindex.go), read as a compaction reads it (compactOver).
func (t *txn) indexCovers(upTo int64) (bool, error) {
	var indexed int64
	var whole bool
	err := t.row(`
		SELECT coalesce((SELECT foreign_upto FROM compaction_state), 0),
			coalesce((SELECT foreign_done FROM compaction_state), false)`).Scan(&indexed, &whole)
	return whole || indexed >= upTo, err
}

// purgeLineIn is the first line, from from and below to, that purges ref,
// 0 for none. A purge rides the gc line its delete appends (rows.go
// hardDelete), so the payloads read are those lines' alone.
func (t *txn) purgeLineIn(ref eref, from, to int64) (int64, error) {
	var seq int64
	err := t.row(`
		SELECT coalesce(min(seq), 0) FROM changelog WHERE kind = $1 AND record_id = $2 AND op = $3 AND seq >= $4 AND seq < $5
		  AND jsonb_path_exists(payload, '$.fold[*] ? (@.kind == "`+string(foldPurge)+`" && @.ref == $k && @.id == $i)',
		      jsonb_build_object('k', $1::text, 'i', $2::text))`,
		ref.Kind, ref.ID, string(substrate.OpGC), from, to).Scan(&seq)
	return seq, err
}

// stateLine is the line of ref's own, up to upTo, that its newest state
// stands on, 0 for none: a step reads it at firstTakenLine's line, so it
// reads what the record retains and one line.
func (t *txn) stateLine(ref eref, upTo int64) (int64, error) {
	var seq int64
	err := t.row(`
		SELECT coalesce(max(seq), 0) FROM changelog WHERE kind = $1 AND record_id = $2 AND seq <= $3
		  AND jsonb_path_exists(payload, '$.fold[*] ? (@.kind == "`+string(foldState)+`" && @.ref == $k && @.id == $i)',
		      jsonb_build_object('k', $1::text, 'i', $2::text))`, ref.Kind, ref.ID, upTo).Scan(&seq)
	return seq, err
}

// recordLinesAbove is what ref's first lines past after and up to upTo come
// to, at most the line bound and one: past that many no step takes them
// within the bounds, so the rest is never counted, and a request's step
// reads as much of a long record as of a short one.
func (t *txn) recordLinesAbove(ref eref, after, upTo int64) (lineSize, error) {
	var s lineSize
	err := t.row(`
		SELECT count(*), coalesce(sum(size), 0)::bigint, coalesce(max(size), 0)::bigint FROM (
			SELECT size FROM (`+recordLinesSQL+`) l WHERE seq > $4 ORDER BY seq LIMIT $5) l`,
		ref.Kind, []string{ref.ID}, upTo, after, t.ds.batchLines()+1).Scan(&s.lines, &s.bytes, &s.largest)
	return s, err
}

// erasureStepAt is where the next step of ref's erasure ends: past after,
// the line before its first a step takes, at the last line that keeps what
// the record retains below and the lines past after within the line bound
// and within the byte bound beyond the largest of them, or at upTo where
// none comes first. It always takes the first line past after, so a step
// takes a new line where what the record retains passes a bound alone, or
// where that line does.
func (t *txn) erasureStepAt(ref eref, after, upTo int64, retained lineSize) (int64, error) {
	var seq, prev int64
	var n int
	err := t.row(`
		SELECT seq, coalesce(prev, 0), n FROM (
			SELECT seq, lag(seq) OVER w AS prev, count(*) OVER w AS n,
				sum(size) OVER w - max(size) OVER w AS over
			FROM (SELECT DISTINCT seq, size FROM (`+recordLinesSQL+`) l WHERE seq > $4) l
			WINDOW w AS (ORDER BY seq)
		) s WHERE n > $5 OR over > $6 ORDER BY seq LIMIT 1`,
		ref.Kind, []string{ref.ID}, upTo, after, t.ds.batchLines()-retained.lines, t.ds.batchBytes()-retained.bytes).Scan(&seq, &prev, &n)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return upTo, nil
	case err != nil:
		return 0, err
	case n == 1:
		return seq, nil
	}
	return prev, nil
}

// lastErasureStep is eraseOne, the erasure's last step: everything left of
// it fits within the bounds, the search from from on included.
func (t *txn) lastErasureStep(e pendingErasure, from int64) (erasureStep, error) {
	var step erasureStep
	report, held, err := t.eraseOne(e, from)
	if err != nil {
		return step, err
	}
	step.report = report
	if held != "" {
		step.held, step.damaged = held, report.Damaged
		return step, t.clearErasureHeld(eref{Kind: e.Kind, ID: e.ID})
	}
	step.done = true
	return step, nil
}

// searchStep reads the trigger lines in the span past from for the saved
// positions that hold ref's values, strips what it finds, and keeps how far
// it read.
func (t *txn) searchStep(ref eref, e pendingErasure, from int64, span searchSpan) (erasureStep, error) {
	var step erasureStep
	to := span.to
	found, err := t.checkpointLines(ref, e.UpTo, from, to)
	if err != nil {
		return step, err
	}
	lines := slices.Sorted(func(yield func(int64) bool) {
		for seq := range found {
			if !yield(seq) {
				return
			}
		}
	})
	if held, damaged, err := t.stepHeldByDamage(lines); err != nil || held != "" {
		step.held, step.damaged = held, damaged
		return step, err
	}
	stripped, err := t.stripCheckpoints(found)
	if err != nil {
		return step, err
	}
	if len(stripped.touched) > 0 {
		if err := t.markArchiveRewrite(stripped.touched); err != nil {
			return step, err
		}
	}
	step.report = stripped
	step.report.read, step.report.readBytes, step.report.readLargest = span.size.lines, span.size.bytes, span.size.largest
	step.at = fmt.Sprintf("its erasure goes on in steps the compaction pass takes, each within its bounds: the search for the saved positions that hold its values has read the history up to seq %d", to)
	return step, t.noteErasureSearched(ref, to, e.UpTo)
}

// mergeStep strips the copies of ref's values off the first merge and
// split lines that fit within the bounds.
func (t *txn) mergeStep(ref eref, merges []mergeCopy) (erasureStep, error) {
	var step erasureStep
	var size lineSize
	var seqs []int64
	for _, m := range merges {
		next := size.plus(m.size)
		if len(seqs) > 0 && !t.ds.within(next) {
			break
		}
		size, seqs = next, append(seqs, m.seq)
	}
	if held, damaged, err := t.stepHeldByDamage(seqs); err != nil || held != "" {
		step.held, step.damaged = held, damaged
		return step, err
	}
	stripped, err := t.stripMergeCopies(seqs)
	if err != nil {
		return step, err
	}
	if len(stripped.touched) > 0 {
		if err := t.markArchiveRewrite(stripped.touched); err != nil {
			return step, err
		}
	}
	step.report = stripped
	step.report.read, step.report.readBytes, step.report.readLargest = size.lines, size.bytes, size.largest
	step.at = fmt.Sprintf("its erasure goes on in steps the compaction pass takes, each within its bounds: %d merge and split lines still copy its values", len(merges)-len(seqs))
	return step, t.clearErasureHeld(ref)
}

// historyStep is the erasure's own compaction of ref's history up to at, a
// line below its intent: a live erasure's, which leaves a state holding the
// record's values as of at, crossing what the last step crosses.
func (t *txn) historyStep(ref eref, e pendingErasure, at int64) (erasureStep, error) {
	var step erasureStep
	if cut, err := t.damageCut(); err != nil {
		return step, err
	} else if cut > 0 && cut <= at {
		step.held, step.damaged = damagedHold(cut), cut
		return step, nil
	}
	t.erasing = true
	report, err := t.compactRecords(at, []eref{ref})
	t.erasing = false
	step.report = report
	switch {
	case err != nil:
		return step, err
	case report.Damaged > 0:
		step.held, step.damaged = damagedHold(report.Damaged), report.Damaged
		return step, nil
	case report.unindexed:
		step.held = "the history's index is still being derived: the next pass derives it and finishes the erasure"
		return step, nil
	case slices.Contains(report.held, ref):
		if why, err := t.unerasableLive(ref); err != nil || why != "" {
			step.held = why
			return step, err
		}
		step.held = "a line of the record a parked failure or a drain names: retry it or forget it, then the next pass or a forget finishes the erasure"
		return step, nil
	}
	if len(report.touched) == 0 {
		// A step always takes a line past what the record retains
		// (stepAt), so one that erases nothing meets something no step
		// within the bounds can take: it is held, and says where.
		step.held = fmt.Sprintf("a step of its erasure up to seq %d erased nothing: what lies below that line cannot be erased within the compaction pass's bounds", at)
		return step, nil
	}
	step.at = fmt.Sprintf("its erasure goes on in steps the compaction pass takes, each within its bounds: its history is erased up to seq %d of seq %d", at, e.UpTo)
	return step, t.clearErasureHeld(ref)
}

// stepHeldByDamage is what holds a step that edits lines: the first of them,
// or of their transactions, found not to verify, which it notes, or a line
// at or above one known not to verify.
func (t *txn) stepHeldByDamage(lines []int64) (string, int64, error) {
	if damaged, err := t.linesDamage(lines); err != nil {
		return "", 0, err
	} else if damaged > 0 {
		return damagedHold(damaged), damaged, t.noteDamage(damaged, "erasure")
	}
	if cut, err := t.damageCut(); err != nil {
		return "", 0, err
	} else if cut > 0 && slices.ContainsFunc(lines, func(l int64) bool { return l >= cut }) {
		return damagedHold(cut), cut, nil
	}
	return "", 0, nil
}

// searchSpan is the next range of the search: up to to, what its trigger
// lines come to, and whether it reaches the head.
type searchSpan struct {
	to    int64
	size  lineSize
	whole bool
}

// searchRange is the next range of trigger lines past from and up to head
// that the bounds admit, at least one line: the lines a search reads for
// saved positions (checkpointLines), sized from their rows as a batch is.
func (t *txn) searchRange(from, head int64) (searchSpan, error) {
	span := searchSpan{to: from, whole: true}
	if from >= head {
		return span, nil
	}
	var seq, prev int64
	var n int
	err := t.row(`
		SELECT seq, coalesce(prev, 0), n FROM (
			SELECT seq, lag(seq) OVER w AS prev, count(*) OVER w AS n,
				sum(size) OVER w - max(size) OVER w AS over
			FROM (SELECT seq, pg_column_size(payload)::bigint AS size FROM changelog
				WHERE kind = $1 AND seq > $2 AND seq <= $3) l
			WINDOW w AS (ORDER BY seq)
		) s WHERE n > $4 OR over > $5 ORDER BY seq LIMIT 1`,
		typeTrigger, from, head, t.ds.batchLines(), t.ds.batchBytes()).Scan(&seq, &prev, &n)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		span.to = head
	case err != nil:
		return span, err
	case n == 1:
		span.to, span.whole = seq, false
	default:
		span.to, span.whole = prev, false
	}
	err = t.row(`
		SELECT count(*), coalesce(sum(pg_column_size(payload)), 0)::bigint, coalesce(max(pg_column_size(payload)), 0)::bigint
		FROM changelog WHERE kind = $1 AND seq > $2 AND seq <= $3`, typeTrigger, from, span.to).
		Scan(&span.size.lines, &span.size.bytes, &span.size.largest)
	return span, err
}

// mergeCopy is a merge or split line that still copies a record's values,
// and what it comes to.
type mergeCopy struct {
	seq  int64
	size lineSize
}

// mergeCopies is every merge and split line up to upTo that names ref and
// still carries a copy of the pair's values (mergeLines, stripMergeCopies).
func (t *txn) mergeCopies(ref eref, upTo int64) ([]mergeCopy, error) {
	seqs, err := t.mergeLines(ref, upTo)
	if err != nil || len(seqs) == 0 {
		return nil, err
	}
	rows, err := t.query(`
		SELECT seq, pg_column_size(payload)::bigint FROM changelog
		WHERE seq = ANY($1) AND (payload ? 'moved' OR payload ? 'skipped') ORDER BY seq`, seqs)
	if err != nil {
		return nil, err
	}
	var out []mergeCopy
	for rows.Next() {
		var m mergeCopy
		if err := rows.Scan(&m.seq, &m.size.bytes); err != nil {
			_ = rows.Close()
			return nil, err
		}
		m.size.lines, m.size.largest = 1, m.size.bytes
		out = append(out, m)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// firstLineOf is the seq of ref's first line up to upTo, 0 for none.
func (t *txn) firstLineOf(ref eref, upTo int64) (int64, error) {
	var first int64
	err := t.row(`SELECT coalesce(min(seq), 0) FROM changelog WHERE kind = $1 AND record_id = $2 AND seq <= $3`,
		ref.Kind, ref.ID, upTo).Scan(&first)
	return first, err
}

// erasureSearched is how far ref's erasure has searched the history for
// saved positions, for the intent up to upTo, 0 where it has not. A search
// made for another intent counts for nothing: a trigger line it passed may
// hold a position about a line of the record above that intent's line, which
// a wider intent erases, so the search starts again from the record's first
// line.
func (t *txn) erasureSearched(ref eref, upTo int64) (int64, error) {
	var searched int64
	err := t.row(`SELECT coalesce((SELECT searched FROM compaction_erasure_progress WHERE record_kind = $1 AND record_id = $2 AND upto = $3), 0)`,
		ref.Kind, ref.ID, upTo).Scan(&searched)
	return searched, err
}

// noteErasureSearched keeps how far ref's erasure has searched for the
// intent up to upTo, and clears what its last step failed on.
func (t *txn) noteErasureSearched(ref eref, to, upTo int64) error {
	_, err := t.exec(`
		INSERT INTO compaction_erasure_progress (record_kind, record_id, searched, upto) VALUES ($1, $2, $3, $4)
		ON CONFLICT (repository, record_kind, record_id) DO UPDATE SET searched = EXCLUDED.searched, upto = EXCLUDED.upto, held = '', updated_at = now()`,
		ref.Kind, ref.ID, to, upTo)
	return err
}

// noteErasureStepped marks ref's erasure stepped now, so a pass takes the
// erasures least recently stepped first (eraseNow).
func (t *txn) noteErasureStepped(ref eref) error {
	_, err := t.exec(`
		INSERT INTO compaction_erasure_progress (record_kind, record_id) VALUES ($1, $2)
		ON CONFLICT (repository, record_kind, record_id) DO UPDATE SET updated_at = now()`,
		ref.Kind, ref.ID)
	return err
}

// leastRecentlyStepped orders pending by when each erasure last took a step,
// one that never did first, the rest as they come: a pass whose share ends
// partway gives the next pass's share to the ones it did not reach.
func (ds *dataset) leastRecentlyStepped(ctx context.Context, pending []pendingErasure) error {
	rows, err := ds.db.QueryContext(ctx, `SELECT record_kind, record_id, updated_at FROM compaction_erasure_progress`)
	if err != nil {
		return err
	}
	stepped := map[eref]time.Time{}
	for rows.Next() {
		var ref eref
		var at time.Time
		if err := rows.Scan(&ref.Kind, &ref.ID, &at); err != nil {
			_ = rows.Close()
			return err
		}
		stepped[ref] = at
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	slices.SortStableFunc(pending, func(a, b pendingErasure) int {
		ta, oka := stepped[eref{Kind: a.Kind, ID: a.ID}]
		tb, okb := stepped[eref{Kind: b.Kind, ID: b.ID}]
		switch {
		case oka != okb && !oka:
			return -1
		case oka != okb:
			return 1
		}
		return ta.Compare(tb)
	})
	return nil
}

// clearErasureHeld clears what ref's erasure's last step failed on: a step
// went on.
func (t *txn) clearErasureHeld(ref eref) error {
	_, err := t.exec(`UPDATE compaction_erasure_progress SET held = '', updated_at = now() WHERE record_kind = $1 AND record_id = $2 AND held <> ''`,
		ref.Kind, ref.ID)
	return err
}

// noteErasureHeld notes on ref's progress row why its last step failed on
// its own, in a transaction of its own: the step's rolled back.
func (ds *dataset) noteErasureHeld(ctx context.Context, ref eref, why string) error {
	return ds.withCompactionTx(ctx, false, func(t *txn) error {
		_, err := t.exec(`
			INSERT INTO compaction_erasure_progress (record_kind, record_id, held) VALUES ($1, $2, $3)
			ON CONFLICT (repository, record_kind, record_id) DO UPDATE SET held = EXCLUDED.held, updated_at = now()`,
			ref.Kind, ref.ID, why)
		return err
	})
}

// erasureHeldNote is why ref's erasure's last step failed on its own, ""
// where none did.
func (ds *dataset) erasureHeldNote(ctx context.Context, ref eref) (string, error) {
	var held string
	err := ds.db.QueryRowContext(ctx, `SELECT coalesce((SELECT held FROM compaction_erasure_progress WHERE record_kind = $1 AND record_id = $2), '')`,
		ref.Kind, ref.ID).Scan(&held)
	return held, err
}
