package engine

// THE CONVERSION CHAIN (decision 0260, docs/stored-format.md "Conversions").
//
// A change to a stored shape ships one conversion step, and the server reads
// only the newest shape. The steps are numbered like migrations and keyed on
// the stamp a repository already carries, its changelog dialect
// (changelogdialect.go): step N converts a history in dialect N-1 to dialect
// N, dialect 1 has no step, and a repository runs the steps it lacks, in
// order and in one pass over its history, at the boot check, before anything
// folds or serves it. A repository stamped above the binary's last step is
// refused, as it always was.
//
// WHAT A STEP IS HANDED: one history line, which it rewrites in place. The
// runner re-encodes the line and recomputes its checksum; the step may not
// move the line's seq or txn. Nothing else, so a step reads the same however
// the history is stored: in the segment files and the table today, in
// Postgres with the files an archive after the history moves (the plan's
// step 7), where only this runner changes.
//
// WHAT RUNS IT: the boot check (repodir.go reconcileDir), on a repository
// whose table and files agree, and on an import between inserting the
// directory's rows and folding them. In order:
//
//  1. Every line is converted into `changelog.converting-<dialect>` beside the
//     live directory (changelogfile.ConvertingDir).
//  2. One transaction rewrites every table row whose line changed, raises the
//     stamp to the last step's dialect and sets the import-progress marker,
//     because the fold was folded from the old lines.
//  3. The converted directory replaces the live one
//     (changelogfile.SwapConverted).
//  4. The fold is rebuilt from the converted files, which clears the marker,
//     and the manifest is written with the new dialect.
//
// A CRASH ANYWHERE IS FINISHED BY THE NEXT BOOT (finishConversion): a
// conversion directory over a table still below its dialect is discarded and
// the conversion runs again; one over a converted table is swapped in; the
// marker then has the fold rebuilt before any open. A copy of the directory
// restored into another database meets a table that took nothing, so it
// keeps its unconverted lines, moved back where the swap had moved them away:
// the history is never left in neither place. A step is IDEMPOTENT, a
// line already in its dialect coming back unchanged, because the manifest is
// rewritten after the swap and a copy taken in between names the old dialect
// over converted lines.
//
// A CONVERSION IS NOT A REPOSITORY MIGRATION (repomigrate.go, decision 0099).
// A migration changes what records HOLD, within the current shapes, by
// appending ordinary writes, and its ledger is the database's, so it runs
// again over every import. A conversion changes the SHAPE history is written
// in, by rewriting lines in place and appending nothing, and its record is
// the repository's own stamp, which travels in the manifest. A change that
// needs both lands both: the step, which runs at the boot check, and the
// migration, which runs at the first open after it, over converted history.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

// conversionStep is one step of the chain.
type conversionStep struct {
	// Dialect is the changelog dialect the step converts a history TO. The
	// first step is 2, and each next one the one after.
	Dialect int
	// Name says what the step changes, in snake case, and is the suffix of
	// the step's file, `conversion_NNNN_<name>.go`.
	Name string
	// Line rewrites one history line in place. It is called for every line,
	// in seq order, and must leave a line already in its dialect unchanged.
	Line func(e *changelogfile.Entry) error
}

// conversionSteps is every step this binary carries, in order. A new one
// takes the next dialect, raises maxChangelogDialect to it, and lands with a
// test that converts every kept directory (TestKeptDirectories does for the
// import; the step's own test says what the lines become).
var conversionSteps = []conversionStep{}

// The conversion's durable steps, as the import's test seam names them
// (testImportFault): a crash after each leaves a state the next boot
// finishes.
const (
	conversionAfterStaging = "after the converted changelog was written beside the live one"
	conversionAfterCommit  = "after the table took the converted lines"
	conversionAfterSwap    = "after the converted changelog replaced the live one"
)

// ErrConversionPending is the refusal a repository whose history is in an
// older dialect meets at open: only the boot check converts it, so the answer
// is a restart of a process that writes.
var ErrConversionPending = fmt.Errorf("%w: the repository's history is in an older changelog dialect and has not been converted", substrate.ErrUnavailable)

// checkConversionSteps holds a chain to its numbering: the first step is
// dialect 2, each next one the one after, every step named and with a Line.
func checkConversionSteps(steps []conversionStep) error {
	var errs []error
	names := map[string]bool{}
	for i, s := range steps {
		if want := i + 2; s.Dialect != want {
			errs = append(errs, fmt.Errorf("step %d converts to dialect %d, want %d: the chain starts at 2 and has no gap", i, s.Dialect, want))
		}
		if s.Name == "" || names[s.Name] {
			errs = append(errs, fmt.Errorf("step %d has an empty or repeated name %q", i, s.Name))
		}
		names[s.Name] = true
		if s.Line == nil {
			errs = append(errs, fmt.Errorf("step %d (%s) has no Line", i, s.Name))
		}
	}
	return errors.Join(errs...)
}

// chainDialect is the dialect a chain converts to: its last step's, or 1.
func chainDialect(steps []conversionStep) int {
	if len(steps) == 0 {
		return 1
	}
	return steps[len(steps)-1].Dialect
}

// pendingConversions is the steps a repository stamped `stored` lacks. An
// unstamped changelog (0) holds dialect-1 lines: the stamp arrived after
// them.
func pendingConversions(steps []conversionStep, stored int) []conversionStep {
	var out []conversionStep
	for _, s := range steps {
		if s.Dialect > max(stored, 1) {
			out = append(out, s)
		}
	}
	return out
}

func conversionNames(steps []conversionStep) string {
	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, fmt.Sprintf("%d (%s)", s.Dialect, s.Name))
	}
	return strings.Join(names, ", ")
}

// refusePendingConversion is the open's half: the boot check converts every
// repository it reconciles, so a pending step here is a process that does
// not run the check (a read-only one) or a history it never reached.
func (ds *dataset) refusePendingConversion(ctx context.Context, stored int) error {
	pending := pendingConversions(ds.svc.conversion, stored)
	if len(pending) == 0 {
		return nil
	}
	head, err := tableChangelogHead(ctx, ds.db)
	if err != nil || head == 0 {
		return err
	}
	return fmt.Errorf("%w: repository %s stores dialect %d and this binary converts it through %s at its boot check; restart a server that writes",
		ErrConversionPending, ds.info.ID, stored, conversionNames(pending))
}

// finishConversion finishes what a conversion cut short left in the
// repository directory, before the boot check compares the files with the
// table (changelogfile.SettleConversion): a conversion directory over a table
// that took its lines is swapped in, and one over a table that did not (a
// crash before the commit, or a copy of the directory restored into another
// database) is discarded, with the live changelog moved back where the swap
// had moved it away, so the conversion runs again.
func (ds *dataset) finishConversion(ctx context.Context) error {
	dialects, err := changelogfile.Converting(ds.dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(changelogfile.PreviousDir(ds.dir)); len(dialects) == 0 && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	stored, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		return err
	}
	ds.svc.log.Warn("substrate: settling an interrupted conversion of the changelog",
		"repository", ds.scope.Repository, "stored", stored, "converting", dialects)
	if err := changelogfile.SettleConversion(ds.dir, func(d int) bool { return stored >= d }); err != nil {
		return fmt.Errorf("substrate/engine: settle the interrupted conversion: %w", err)
	}
	return nil
}

// convertHistory converts a repository whose table and files agree into
// this binary's dialect: steps 1 to 3 of the header. It reports whether it
// converted, and returns the Log of the converted files; the caller rebuilds
// the fold from it (the marker it set says so). verified is a Log the caller
// already checked, which the fresh open reuses.
func (ds *dataset) convertHistory(ctx context.Context, verified *changelogfile.Log) (*changelogfile.Log, bool, error) {
	// A history stamped above this binary has no step pending here; the open
	// refuses it (gateChangelogDialect), one repository and not the boot.
	stored, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		return nil, false, err
	}
	steps := pendingConversions(ds.svc.conversion, stored)
	if len(steps) == 0 {
		return verified, false, nil
	}
	dir := changelogfile.ChangelogDir(ds.dir)
	log, err := changelogfile.OpenWith(dir, changelogfile.OpenOptions{Verified: verified, TrustSidecars: true})
	if err != nil {
		return nil, false, directoryOpenErr(err)
	}
	if log.Head() == 0 {
		return log, false, nil
	}
	target := chainDialect(steps)
	ds.svc.log.Info("substrate: converting the changelog",
		"repository", ds.scope.Repository, "from", max(stored, 1), "to", target, "steps", conversionNames(steps), "head", log.Head())
	if err := changelogfile.DiscardConverting(ds.dir, target); err != nil {
		return nil, false, err
	}
	if err := ds.writeConverted(log, steps, target); err != nil {
		return nil, false, err
	}
	if err := ds.importFault(conversionAfterStaging); err != nil {
		return nil, false, err
	}
	converted, err := changelogfile.OpenWith(changelogfile.ConvertingDir(ds.dir, target), changelogfile.OpenOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("substrate/engine: read back the converted changelog: %w", err)
	}
	if converted.Head() != log.Head() {
		return nil, false, fmt.Errorf("substrate/engine: the converted changelog ends at seq %d, the history at %d", converted.Head(), log.Head())
	}
	if err := ds.commitConverted(ctx, converted, target); err != nil {
		return nil, false, err
	}
	if err := ds.importFault(conversionAfterCommit); err != nil {
		return nil, false, err
	}
	if err := changelogfile.SwapConverted(ds.dir, target); err != nil {
		return nil, false, fmt.Errorf("substrate/engine: swap the converted changelog in: %w", err)
	}
	if err := ds.importFault(conversionAfterSwap); err != nil {
		return nil, false, err
	}
	out, err := changelogfile.OpenWith(dir, changelogfile.OpenOptions{})
	if err != nil {
		return nil, false, directoryOpenErr(err)
	}
	return out, true, nil
}

// writeConverted writes every line of log, through each step, into the
// conversion directory for target, a transaction at a time.
func (ds *dataset) writeConverted(log *changelogfile.Log, steps []conversionStep, target int) error {
	w, err := changelogfile.OpenWriter(changelogfile.ConvertingDir(ds.dir, target), ds.svc.writerOptions())
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	cur := log.Cursor(0)
	defer func() { _ = cur.Close() }()
	var txn []changelogfile.Line
	for {
		entries, err := cur.Next(rebuildBatch)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			seq, end := e.Seq, e.Txn
			for _, s := range steps {
				if err := s.Line(&e); err != nil {
					return fmt.Errorf("substrate/engine: conversion %d (%s) at seq %d: %w", s.Dialect, s.Name, seq, err)
				}
			}
			if e.Seq != seq || e.Txn != end {
				return fmt.Errorf("substrate/engine: a conversion step moved seq %d (txn %d) to seq %d (txn %d): a step rewrites what a line says, never where it stands",
					seq, end, e.Seq, e.Txn)
			}
			line, _, err := changelogfile.Encode(e)
			if err != nil {
				return fmt.Errorf("substrate/engine: seq %d does not encode after its conversion: %w", seq, err)
			}
			txn = append(txn, changelogfile.Line{Seq: e.Seq, Txn: e.Txn, Bytes: line})
			if e.EndsTransaction() {
				if err := w.AppendLines(txn); err != nil {
					return err
				}
				txn = nil
			}
		}
	}
	if len(txn) > 0 {
		return fmt.Errorf("%w: the history ends inside the transaction ending at seq %d", ErrChangelogDiverged, txn[0].Txn)
	}
	return w.Close()
}

// commitConverted rewrites, in one transaction under the changelog lock,
// every table row whose line the conversion changed, stamps target and marks
// the fold for rebuilding.
func (ds *dataset) commitConverted(ctx context.Context, converted *changelogfile.Log, target int) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	t := &txn{ctx: ctx, ds: ds, tx: tx, now: nowUTC(), internal: true}
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	cur := converted.Cursor(0)
	defer func() { _ = cur.Close() }()
	for {
		entries, err := cur.Next(rebuildBatch)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		if len(entries) == 0 {
			break
		}
		stamped, err := tableHashes(t, entries[0].Seq-1, entries[len(entries)-1].Seq)
		if err != nil {
			return err
		}
		for _, e := range entries {
			_, sum, err := changelogfile.Encode(e)
			if err != nil {
				return err
			}
			old, ok := stamped[e.Seq]
			if !ok {
				return fmt.Errorf("%w: seq %d is in the converted changelog and not in the table", ErrChangelogDiverged, e.Seq)
			}
			if bytes.Equal(old, sum[:]) {
				continue
			}
			var causedBy sql.NullInt64
			if e.CausedByOK {
				causedBy = sql.NullInt64{Int64: e.CausedBy, Valid: true}
			}
			if _, err := t.exec(`
				UPDATE changelog SET ts = $2, actor = $3, principal = $4, op = $5, record_id = $6, kind = $7,
					payload = $8::jsonb, caused_by = $9, txn = $10, hash = $11
				WHERE seq = $1`,
				e.Seq, e.TS, e.Actor, e.Principal, e.Op, e.RecordID, e.Kind, []byte(e.Payload), causedBy, e.Txn, sum[:]); err != nil {
				return fmt.Errorf("substrate/engine: convert seq %d: %w", e.Seq, err)
			}
		}
	}
	if _, err := t.exec(changelogDialectStamp, target); err != nil {
		return fmt.Errorf("substrate/engine: stamp changelog dialect %d: %w", target, err)
	}
	if err := markImportIncomplete(ctx, tx, converted.Head()); err != nil {
		return err
	}
	return tx.Commit()
}

// tableHashes is the checksum the table stamped for each seq in (after, last].
func tableHashes(t *txn, after, last int64) (map[int64][]byte, error) {
	rows, err := t.query(`SELECT seq, hash FROM changelog WHERE seq > $1 AND seq <= $2`, after, last)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64][]byte{}
	for rows.Next() {
		var seq int64
		var hash []byte
		if err := rows.Scan(&seq, &hash); err != nil {
			return nil, err
		}
		out[seq] = hash
	}
	return out, rows.Err()
}
