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
// runner holds the row to its stored checksum first and stops the whole
// conversion at a row that does not verify (walkedLine), then re-encodes the
// line the step left and recomputes its checksum only where the line
// changed; the step may not move the line's seq or txn. So a conversion never
// stamps a row it could not verify, and a line a step leaves as it found it
// keeps its checksum. A step that needs nothing else declares Line
// alone. One that reads differently in each repository (a kind is the
// repository's own only under its own authority) declares ForRepository
// instead, which the runner calls once per repository it converts, with
// the repository, and which returns that repository's Line and, where the
// step renames what rows outside history name too (a sealed row binds its
// record's kind; a vector is kept by its record's kind and id), a Carry
// that rewrites those rows in the same transaction as the lines. A step that
// must know the repository's own vocabulary to read a line (which positions
// of a value are references: a stored reference is `{"ref": ...}` with no
// marker, byte for byte a person's own JSON) asks the repository for its
// Declarations: every version of every declaration its history holds, in
// the dialect the step sees its lines in.
//
// A LINE IS CONVERTED ONCE BY EACH STEP, in that step's walk over the table:
// a step's Line sees every line exactly once, in seq order, as the steps
// below it left it, and nothing else calls it. Its Declarations are read
// from the table those walks wrote, never by running a step's Line again. No
// copy of the history is ever held to the table across a conversion
// (decision 0279): whether an archive is this database's is decided by what
// the database recorded of it, and a directory a release before the archive
// wrote is compared, line for line, before its history is converted. So a
// step's Line may lean on what the repository hands it, Declarations
// included, and a Carry may count on what its Line saw.
//
// WHERE IT RUNS: on the changelog table, which is the history (decision
// 0262). The boot check runs it on a repository the table holds below this
// binary's dialect, and on an import after the source's sealed rows and lines
// went in and before they fold. ONE TRANSACTION, under the
// changelog lock, walks the table once for each step, in step order,
// rewriting every row whose line the step changed, then runs every step's
// Carry in order, and raises the stamp to the last step's dialect. Where a
// step changed a line of the repository, or a step says the fold must be
// rebuilt (Refolds), it sets the import-progress marker too, because the fold
// was folded from the old lines; the fold is then rebuilt from the table,
// which clears the marker. A raise that changed no line leaves the fold as it
// stands: the lines it was folded from are the lines it holds (decision
// 0309). A crash before the commit leaves the history and every carried row
// as they were, and the next boot converts them again; one after leaves the
// marker where it was set, and the next boot rebuilds the fold before any
// open. Nothing unseals or folds
// between the lines and the carry. The archive follows on its own: its pieces are kept under
// their dialect, and the next pass writes the converted history beside the
// old and moves its manifest to it (archive.go).
//
// A step is IDEMPOTENT, a line already in its dialect coming back unchanged,
// because an archive restored from before a conversion names its dialect in
// its manifest and its lines are converted again as they are imported.
//
// WHO CLAIMS THE DIALECT. The stamp says which dialect a history's lines are
// in, and an unclaimed one (no row) holds dialect 1, the history from before
// the stamp. A history below the chain's dialect is claimed by its
// conversion, at the boot check, whether or not a step changed a line: the
// runner's transaction raises the stamp, so a history converts once and an
// open finds nothing pending. An empty history, which the runner skips, is
// claimed by its first append (changelogdialect.go stampChangelogDialect), as
// a new repository's is. An open claims nothing. So with any number of steps
// a repository stands at the chain's dialect from its first boot under the
// binary or from its creation, and the archive's next manifest says so. A
// test of the append's claim over an unclaimed history holds a binary that
// carries no step (export_test.go WithoutConversionSteps).
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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// conversionStep is one step of the chain. It declares Line or
// ForRepository, never both.
type conversionStep struct {
	// Dialect is the changelog dialect the step converts a history TO. The
	// first step is 2, and each next one the one after.
	Dialect int
	// Name says what the step changes, in snake case, and is the suffix of
	// the step's file, `conversion_NNNN_<name>.go`.
	Name string
	// Line rewrites one history line in place. It is called once for every
	// line, in seq order, in the step's own walk over the table, and must
	// leave a line already in its dialect unchanged.
	Line func(e *changelogfile.Entry) error
	// ForRepository is called once for each repository the step converts,
	// before its first line, and returns what the step does in that
	// repository. An error refuses the conversion, and so the boot.
	ForRepository func(r conversionRepository) (conversionPass, error)
	// Refolds says the fold must be rebuilt after this step even where it
	// changed no line of the repository (decision 0309): its Carry changes a
	// row the fold is derived from, or it comes with a fold table the lines
	// already held the content of, which the schema migration beside it left
	// empty. A step that rewrites lines needs no flag: a changed line
	// rebuilds the fold whatever the steps say.
	Refolds bool
}

// conversionRepository is the repository a step converts.
type conversionRepository struct {
	// ID is the repository's id (decision 0261); Authority its name, the
	// authority its own kinds are declared under.
	ID        string
	Authority string
	// Reseal opens a sealed payload bound to the record it was sealed for
	// and seals it again bound to another (decision 0023), under the
	// repository's data key, for a Carry that moves a sealed row to the
	// kind or id its lines now name. The data key never leaves the runner.
	Reseal func(payload []byte, ref, fromKind, fromID, toKind, toID string) ([]byte, error)
	// SecretsLeftBehind says a restore left the repository's secrets behind
	// (lostsecrets.go): a sealed value that does not open under its data key
	// (Reseal answers errSealedUnopened) is one of them, kept as the record
	// of its loss until the open clears it, and a Carry moves it to its new
	// binding as it is. In any other repository such a value is damage.
	SecretsLeftBehind bool
	// Actors is every version of every actor record (core/actor) the history
	// holds, read as Declarations is and in the same dialect: what a step
	// that moves an actor record learns each hand from (its type and its
	// link), whether the hand's code is still there or long gone.
	Actors func() ([]declarationVersion, error)
	// Declarations is every version of the repository's own vocabulary its
	// history holds: each write, tombstone and purge of a record of a
	// declaration kind (core/kind, core/trait, core/package and the rest of
	// vocabularyRecordKinds), in seq order, each the record as it stood after
	// that line. The lines are the history being converted, the table, read
	// through its kind index, so a step never reads every line twice for
	// them, and an import's are there before its fold is. They are in the
	// dialect this step sees its lines in: the steps below this one have
	// walked the table and rewritten them there before this step is built,
	// and no step's Line is run again to make them. A step converts a line from the
	// versions at or below the line's transaction (Seq <= Txn), the
	// declarations in force when it was written, and never from a later
	// one, so the line converts the same in every copy that holds the
	// history up to it (decision 0279). The runner builds them at the step's
	// first call, once per step and conversion; a step that never calls it
	// costs nothing.
	Declarations func() ([]declarationVersion, error)
}

// declarationVersion is one version of one declaration record, as a
// conversion step's Declarations hands it.
type declarationVersion struct {
	// Seq is the line that wrote this version.
	Seq int64
	// Ref is the declaration's record kind (kindKind, kindTrait, ...), ID
	// its record id: the declared kind's, trait's or package's reference.
	Ref, ID string
	// Deleted is a tombstone or a purge: the declaration is gone from here
	// on, and the rest is empty.
	Deleted bool
	// Properties is the record's properties after the line, whole.
	Properties map[string]any
	// Document is Properties as the vocabulary reads a stored declaration
	// (rowDocument), and Unread why it could not, for a version an older
	// release wrote in a shape this binary no longer reads: a step reads
	// what it needs from Properties then, rather than refuse the history.
	Document vocabulary.Document
	Unread   error
}

// conversionPass is what one step does in one repository.
type conversionPass struct {
	// Line is the step's Line for this repository: every line once, in seq
	// order, in the step's own walk, and a line already in the step's
	// dialect left unchanged.
	Line func(e *changelogfile.Entry) error
	// Carry, when set, runs after every step has walked every line of the
	// history, in the same transaction, in step order:
	// it rewrites the rows outside history that name what the lines renamed
	// (the sealed table, the vectors in `embeddings`), through tx, which is
	// scoped to the repository. It is not handed lines, and it must leave a
	// row already in the step's dialect unchanged, because an import loads
	// rows from an archive of any dialect.
	Carry func(ctx context.Context, tx *sql.Tx) error
}

// conversionSteps is every step this binary carries, in order. A new one
// takes the next dialect, raises maxChangelogDialect to it, and lands with a
// test that converts every kept directory (TestKeptDirectories does for the
// import; the step's own test says what the lines become).
var conversionSteps = []conversionStep{
	{Dialect: 2, Name: "drop_recovery_wrap", Line: convertDropRecoveryWrap},
	{Dialect: 3, Name: "link_actors", Line: linkActorsLine},
	{Dialect: 4, Name: "move_body_into_properties", Line: moveBodyIntoProperties},
	// The marks are a fold table SQL 0026 created empty, of what the
	// lines held as `manager` effects, and it re-keyed the derived offers.
	{Dialect: 5, Name: "marks", Line: markLine, Refolds: true},
	// The local namespace rewrites the lines that spell the repository's
	// name, so the records are rebuilt for it without Refolds.
	{Dialect: localNamespaceDialect, Name: "local_namespace", ForRepository: convertLocalNamespace},
}

// The conversion's steps, as the import's test seam names them
// (testImportFault): every line and carried row rewritten and nothing
// committed; and the table took them, the fold not rebuilt yet.
const (
	conversionBeforeCommit = "after the lines and the carried rows were rewritten, before the commit"
	conversionAfterCommit  = "after the table took the converted lines"
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
		if (s.Line == nil) == (s.ForRepository == nil) {
			errs = append(errs, fmt.Errorf("step %d (%s) declares Line or ForRepository, and exactly one of them", i, s.Name))
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

// convertTable converts the repository's history in the table into this
// binary's dialect, as the header says, and reports whether it converted. The
// caller rebuilds the fold where the marker says so, which a conversion sets
// only where a line changed or a step refolds.
func (ds *dataset) convertTable(ctx context.Context) (bool, error) {
	return ds.convertTableTo(ctx, 0)
}

// convertTableTo is convertTable stopped at dialect to, 0 for no stop: an
// import that meets a source in a later dialect than the table's converts the
// table to the source's and no further, so the source's lines go in beside
// lines of their own dialect and are converted with them after.
func (ds *dataset) convertTableTo(ctx context.Context, to int) (bool, error) {
	stored, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		return false, err
	}
	steps := pendingConversions(ds.svc.conversion, stored)
	if to > 0 {
		steps = slices.DeleteFunc(steps, func(step conversionStep) bool { return step.Dialect > to })
	}
	if len(steps) == 0 {
		return false, nil
	}
	target := steps[len(steps)-1].Dialect
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	t := &txn{ctx: ctx, ds: ds, tx: tx, now: nowUTC(), internal: true}
	if err := t.lockKey(changelogLockKey); err != nil {
		return false, err
	}
	head, err := tableChangelogHead(ctx, tx)
	if err != nil {
		return false, err
	}
	if head == 0 {
		return false, nil
	}
	ds.svc.log.Info("substrate: converting the changelog",
		"repository", ds.scope.Repository, "from", stored, "to", target, "steps", conversionNames(steps), "head", head)
	prog := ds.svc.progress("converting the changelog", "repository", ds.scope.Repository, "head", head*int64(len(steps)))
	// One walk per step, in step order, over the whole table, in this one
	// transaction: a step's Line sees every line once, in seq order, as the
	// steps below it left it, and the step after reads the table they wrote
	// (its Declarations included), so no line is converted outside a walk.
	// How many line rewrites the steps made, summed over the steps: a count
	// and nothing per line, since a history from the last release has every
	// line rewritten and may hold tens of millions.
	var rewrites int64
	passes := make([]conversionPass, 0, len(steps))
	for i, step := range steps {
		pass, err := ds.conversionPass(ctx, tx, step)
		if err != nil {
			return false, err
		}
		passes = append(passes, pass)
		n, err := ds.walkConversionStep(ctx, t, step, pass, head, int64(i)*head, prog)
		if err != nil {
			return false, err
		}
		rewrites += n
	}
	for i, pass := range passes {
		if pass.Carry == nil {
			continue
		}
		if err := pass.Carry(ctx, tx); err != nil {
			return false, fmt.Errorf("substrate/engine: conversion step %d (%s): carry the rows outside history: %w", steps[i].Dialect, steps[i].Name, err)
		}
	}
	if err := ds.importFault(conversionBeforeCommit); err != nil {
		return false, err
	}
	if _, err := t.exec(changelogDialectStamp, target); err != nil {
		return false, fmt.Errorf("substrate/engine: stamp changelog dialect %d: %w", target, err)
	}
	// The fold was folded from the lines as they stood: a changed line, or
	// a step that says so, rebuilds it (the marker); a raise that changed no
	// line leaves it standing (decision 0309). A marker already set, by an
	// import or a start cut short, stays.
	refold := rewrites > 0 || slices.ContainsFunc(steps, func(s conversionStep) bool { return s.Refolds })
	marked := refold
	if refold {
		if err := markImportIncomplete(ctx, tx, head); err != nil {
			return false, err
		}
	} else if _, marked, err = importIncomplete(ctx, tx); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	switch {
	case refold:
		ds.svc.log.Info("substrate: converted the changelog; the fold is rebuilt from it next",
			"repository", ds.scope.Repository, "dialect", target, "lineRewritesSummedOverSteps", rewrites)
	case marked:
		ds.svc.log.Info("substrate: converted the changelog; no line changed, and the import in progress rebuilds the fold next",
			"repository", ds.scope.Repository, "dialect", target)
	default:
		ds.svc.log.Info("substrate: converted the changelog; no line changed, so the fold stands",
			"repository", ds.scope.Repository, "dialect", target)
	}
	if err := ds.importFault(conversionAfterCommit); err != nil {
		return true, err
	}
	return true, nil
}

// walkConversionStep runs one step's Line over every line of the table, in
// seq order, writes back each line it changed with its new checksum, and
// returns how many it changed. A step may not move a line's seq or txn.
func (ds *dataset) walkConversionStep(ctx context.Context, t *txn, step conversionStep, pass conversionPass, head, done int64, prog *progress) (int64, error) {
	var rewritten int64
	for after := int64(0); after < head; {
		page, err := scanChecksumPage(ctx, t.tx, after, rebuildBatch)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			e, err := walkedLine(row)
			if err != nil {
				return 0, fmt.Errorf("substrate/engine: conversion step %d (%s): %w", step.Dialect, step.Name, err)
			}
			seq, txnSeq := e.Seq, e.Txn
			after = seq
			if err := pass.Line(&e); err != nil {
				return 0, fmt.Errorf("substrate/engine: conversion step %d (%s) at seq %d: %w", step.Dialect, step.Name, seq, err)
			}
			if e.Seq != seq || e.Txn != txnSeq {
				return 0, fmt.Errorf("substrate/engine: conversion step %d (%s) moved seq %d: a step may not move a line's seq or txn", step.Dialect, step.Name, seq)
			}
			_, sum, err := changelogfile.Encode(e)
			if err != nil {
				return 0, fmt.Errorf("substrate/engine: seq %d does not encode after conversion step %d: %w", seq, step.Dialect, err)
			}
			// The row was held to its stored checksum before the step ran,
			// so an unchanged checksum is a line the step left as it found
			// it, and keeps the checksum it had.
			if bytes.Equal(row.hash, sum[:]) {
				continue
			}
			var causedBy sql.NullInt64
			if e.CausedByOK {
				causedBy = sql.NullInt64{Int64: e.CausedBy, Valid: true}
			}
			if _, err := t.exec(`
				UPDATE changelog SET ts = $2, actor = $3, on_behalf_of = $4, principal = $5, op = $6, record_id = $7,
					kind = $8, payload = $9::jsonb, caused_by = $10, txn = $11, hash = $12
				WHERE seq = $1`,
				e.Seq, e.TS, e.Actor, e.OnBehalfOf, lineToken(e), e.Op, e.RecordID, e.Kind, []byte(e.Payload), causedBy, e.Txn, sum[:]); err != nil {
				return 0, fmt.Errorf("substrate/engine: convert seq %d: %w", e.Seq, err)
			}
			rewritten++
		}
		prog.tick(changelogfile.Position{Seq: done + after})
	}
	return rewritten, nil
}

// lineFrame is the key set a row's line was encoded in, which only its
// stored checksum tells: the table keeps a line's token in its `principal`
// column whatever key the line held it under.
type lineFrame int

const (
	// frameNone is a row whose stored checksum no frame reproduces: it is
	// not the line that was stamped.
	frameNone lineFrame = iota
	// frameToken is this binary's: from changelog dialect 3 a line writes
	// `token`, and only where one stood (decision 0270).
	frameToken
	// framePrincipal is changelog dialects 1 and 2's: `principal` on every
	// line, an empty one included.
	framePrincipal
)

// tokenFrameDialect is the changelog dialect whose lines are the first in
// frameToken: a history stamped there or later holds no row in an earlier
// frame.
const tokenFrameDialect = 3

// framedLine is the line a row of the table holds, in the frame whose
// encoding reproduces the row's stored checksum. Exactly one can, because
// the two frames' key sets differ.
func framedLine(row checksumRow) (changelogfile.Entry, lineFrame) {
	e := row.entry.fileEntry()
	if _, sum, err := changelogfile.Encode(e); err == nil && bytes.Equal(sum[:], row.hash) {
		return e, frameToken
	}
	old := e
	old.Principal, old.HasPrincipal, old.Token = e.Token, true, ""
	if _, sum, err := changelogfile.Encode(old); err == nil && bytes.Equal(sum[:], row.hash) {
		return old, framePrincipal
	}
	return e, frameNone
}

// walkedLine is the line a step is handed for a row: the one its stored
// checksum was computed over, so a step below dialect 3 sees the key the
// line was written with. A row no frame reproduces stops the conversion,
// naming its seq. A conversion never gives a line a checksum it did not have
// a right to: the walk stamps a fresh checksum on every line a step
// rewrites, and a step that rewrote a damaged row would leave it verifying.
func walkedLine(row checksumRow) (changelogfile.Entry, error) {
	e, frame := framedLine(row)
	if frame == frameNone {
		return e, fmt.Errorf("substrate/engine: seq %d: its stored checksum is the checksum of no line the row could hold, so the history is not converted; repair or restore the row first",
			row.entry.Seq)
	}
	return e, nil
}

// conversionPass is what step does in this repository, built in the
// conversion's transaction just before the step's walk: its Declarations
// read the declaration lines from the table as the steps below it left them,
// in the dialect this step sees its lines in, once, on the first call.
func (ds *dataset) conversionPass(ctx context.Context, tx *sql.Tx, step conversionStep) (conversionPass, error) {
	if step.ForRepository == nil {
		return conversionPass{Line: step.Line}, nil
	}
	var leftBehind bool
	if err := ds.svc.maint.QueryRowContext(ctx, `SELECT secrets_left_behind FROM repositories WHERE id = $1`, ds.info.ID).Scan(&leftBehind); err != nil {
		return conversionPass{}, fmt.Errorf("substrate/engine: conversion step %d (%s): read whether secrets were left behind: %w", step.Dialect, step.Name, err)
	}
	// Each reader reads the lines of its kinds from the table once, on its
	// first call.
	reader := func(kinds []string, fold func([]changelogfile.Entry) ([]declarationVersion, error)) func() ([]declarationVersion, error) {
		var versions []declarationVersion
		var versionsErr error
		built := false
		return func() ([]declarationVersion, error) {
			if !built {
				built = true
				rows, err := queryChecksumRows(ctx, tx, `WHERE kind = ANY($1) ORDER BY seq`, kinds)
				if err != nil {
					versionsErr = err
				} else {
					lines := make([]changelogfile.Entry, len(rows))
					for i, r := range rows {
						if lines[i], versionsErr = walkedLine(r); versionsErr != nil {
							return nil, versionsErr
						}
					}
					versions, versionsErr = fold(lines)
				}
			}
			return versions, versionsErr
		}
	}
	repo := conversionRepository{
		ID: ds.info.ID, Authority: ds.info.Authority, Reseal: ds.reseal, SecretsLeftBehind: leftBehind,
		Declarations: reader(vocabularyKindRefs, declarationVersions),
		Actors: reader([]string{kindActor}, func(lines []changelogfile.Entry) ([]declarationVersion, error) {
			return recordVersions(lines, func(ref string) bool { return ref == kindActor })
		}),
	}
	pass, err := step.ForRepository(repo)
	if err != nil {
		return conversionPass{}, fmt.Errorf("substrate/engine: conversion step %d (%s): %w", step.Dialect, step.Name, err)
	}
	if pass.Line == nil {
		return conversionPass{}, fmt.Errorf("substrate/engine: conversion step %d (%s) returned no Line for repository %s", step.Dialect, step.Name, ds.info.ID)
	}
	return pass, nil
}

// declarationVersions folds the declaration lines, as the table holds them
// for the step asking, into every version of every declaration record they
// write: a record delta onto the record as it stood (a creation starting it
// over), a tombstone or a purge ending it.
func declarationVersions(lines []changelogfile.Entry) ([]declarationVersion, error) {
	return recordVersions(lines, func(ref string) bool { _, ok := vocabularyRecordKinds[ref]; return ok })
}

// recordVersions folds lines into every version of every record of a kind
// keep admits, as declarationVersions does for the declaration kinds.
func recordVersions(lines []changelogfile.Entry, keep func(ref string) bool) ([]declarationVersion, error) {
	props := map[string]map[string]any{}
	var out []declarationVersion
	for _, e := range lines {
		var payload map[string]any
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return nil, fmt.Errorf("substrate/engine: seq %d carries an unreadable payload: %w", e.Seq, err)
		}
		effects, _ := payload[foldPayloadKey].([]any)
		for _, raw := range effects {
			eff, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ref, _ := eff["ref"].(string)
			id, _ := eff["id"].(string)
			if !keep(ref) || id == "" {
				continue
			}
			key := ref + "/" + id
			switch foldKind(fmt.Sprint(eff["kind"])) {
			case foldTombstone, foldPurge:
				delete(props, key)
				out = append(out, declarationVersion{Seq: e.Seq, Ref: ref, ID: id, Deleted: true})
			case foldRecord:
				delta, _ := eff["delta"].(map[string]any)
				cur := props[key]
				if created, _ := delta["created"].(bool); created || cur == nil {
					cur = map[string]any{}
				}
				next := make(map[string]any, len(cur))
				for k, v := range cur {
					next[k] = v
				}
				if set, ok := delta["set"].(map[string]any); ok {
					for k, v := range set {
						next[k] = v
					}
				}
				if del, ok := delta["del"].([]any); ok {
					for _, k := range del {
						if name, ok := k.(string); ok {
							delete(next, name)
						}
					}
				}
				props[key] = next
				v := declarationVersion{Seq: e.Seq, Ref: ref, ID: id, Properties: next}
				v.Document, _, v.Unread = rowDocument(id, ref, next)
				out = append(out, v)
			}
		}
	}
	return out, nil
}

// sameLineAsTable reports whether e, a line of a copy of the history in
// changelog dialect dialect, is the line the table holds at its seq: its
// stamped checksum. Lines are held to each other in one dialect only: a copy
// in another dialect than the table's is refused, never converted to
// compare (the header).
func (ds *dataset) sameLineAsTable(ctx context.Context, q dbx, e changelogfile.Entry, dialect int) (bool, error) {
	tableDialect, err := readChangelogDialect(ctx, q)
	if err != nil {
		return false, err
	}
	if max(dialect, 1) != max(tableDialect, 1) {
		return false, fmt.Errorf("substrate/engine: seq %d is in changelog dialect %d and the table's lines in %d; lines are compared in one dialect only", e.Seq, max(dialect, 1), max(tableDialect, 1))
	}
	_, sum, err := changelogfile.Encode(e)
	if err != nil {
		return false, err
	}
	var hash []byte
	err = q.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, e.Seq).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(sum[:], hash), nil
}

// reseal is conversionRepository.Reseal: the payload opened under the
// repository's data key bound to its old record, sealed again bound to the
// new one.
func (ds *dataset) reseal(payload []byte, ref, fromKind, fromID, toKind, toID string) ([]byte, error) {
	dek := ds.dek
	if dek == nil {
		repo, err := ds.svc.repositoryByID(context.Background(), ds.info.ID)
		if err != nil {
			return nil, err
		}
		if dek, err = ds.svc.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID); err != nil {
			return nil, err
		}
	}
	raw, err := openRepoPayload(payload, dek, sealedAAD(ref, fromKind, fromID))
	if err != nil {
		return nil, fmt.Errorf("%w: substrate/engine: open sealed %s bound to %s %s: %w", errSealedUnopened, ref, fromKind, fromID, err)
	}
	aead, err := aeadOf(dek)
	if err != nil {
		return nil, err
	}
	return sealWith(aead, raw, sealedAAD(ref, toKind, toID))
}

// errSealedUnopened is Reseal's refusal of a payload that does not open
// under the repository's data key.
var errSealedUnopened = errors.New("substrate/engine: the sealed value does not open under the repository's data key")

// refusePendingConversion refuses an open of a repository whose history is in
// a dialect this binary converts: only the boot check converts.
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
