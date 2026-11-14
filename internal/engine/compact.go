package engine

// COMPACTION (decision 0296): a record's entries up to one of them replaced
// by one STATE ENTRY, the record's whole state as of the newest entry it
// replaces. The history keeps every seq it keeps: a replaced entry leaves a
// gap and nothing is renumbered (decision 0017).
//
// A state entry is an EFFECT, not a line: one line carries the effects of
// every record its transaction wrote (appendChange), so the unit is one
// record's effects, and a compaction rewrites the lines that held them. Two
// effects are state entries, one per family of rows a record holds:
//
//   - `state` stands for the record's own rows: its records row, version and
//     times included, its annotations and its property marks. It replaces
//     the record, tombstone, annotation, mark, unmark and state effects on
//     the record.
//   - `ledger` stands for a trigger's delivery rows (delivery.go): its
//     cursor, fire state, parked failures and resume rows. It replaces the
//     trigger's cursor, schedule, park, unpark, page, unpage, forget and
//     ledger effects.
//
// WHERE IT GOES. A family's state goes on the newest line, at or below the
// compaction's seq, that is ADDRESSED to the record (its `kind` and
// `record_id` are the record's) and carries an effect of the family on it,
// in the place of the last such effect; a `state` never goes on a `delivery`
// line, which no public read shows. Every read that follows records by
// address (a trigger's, a watch's filter) therefore meets the record once
// at its state. The family's effects on lines below it are removed, and so
// are its effects on that line before the state. Its effects on other
// records' lines above the state stay, as every effect above the
// compaction's seq does: they apply on top of the state as they applied on
// top of what it replaced.
//
// WHAT IS NEVER REPLACED:
//
//   - `former` and `resync`: merge and split write the former-id trail and
//     re-state the side stores of the pair they join. A former effect can
//     take a former id from a third record, so no record's state could
//     re-state the trail exactly; kept, they reproduce it. They carry ids
//     and the side rows of two records a person merged, and they are rare.
//   - a `purge` that is not the record's last effect on its state's line:
//     a purge also removes the former-id rows that name the record, which
//     another record holds. The last one becomes a state with no row, which
//     does exactly what the purge did.
//   - every effect on a line a live parked failure names (trigger_failures
//     .seq), which a retry rebuilds its envelope from (changeAt), or a live
//     record drain's chain names (recordChainKey). Such a line is not edited
//     and no state goes on it.
//   - a trigger's delivery effects while the webhook spool holds a request:
//     its drain asks whether the history above the spool's head already
//     names the request's fire (webhookRecordedSQL). A copy of the spool
//     (the archive pass, an export) asks the same of the snapshot it reads
//     after it, so a compaction never commits between the two (spoolCopy).
//   - every effect of the family below one of the above that stays: an
//     effect that stays replays against the rows the effects under it left,
//     so they stay with it. Without them a tombstone kept on a line a parked
//     failure names would find no row, and move the unit graph's revision
//     by nothing where it moved it by one.
//   - a line that carries no effect, and every line above the seq.
//   - every line at or above the first that does not verify (below).
//
// A LINE THAT DOES NOT VERIFY is one whose columns are not what its stored
// checksum was stamped over (checksumRow.verifies), and a compaction keeps
// the conversion's rule: nothing gives a line a checksum it had no right
// to. It reads nothing of one but its seq, and stops below the first it
// meets, D: the plans, the places, the edits, the removals and the
// re-frames are all below D, as if D-1 were its seq. Every line below D
// verified, so nothing it builds trusts a damaged column; a transaction
// that ends below D lies wholly below it, and one that holds D ends at or
// above it, so its end is never removed and it is never re-framed. The
// report names D (Damaged), and there may be more above it, which it did
// not look at. Once the row is written back from a whole copy, the next
// compaction goes past it.
//
// A line left with no effect is removed. A line that ended its transaction
// and is removed moves the transaction's end to the newest line of it that
// is kept, so every kept line still frames its transaction; every line a
// compaction rewrites gets its checksum again. The head is never removed:
// it carries the newest effect of every record it touches.
//
// HOW A STATE IS TAKEN. A state is what the fold held at its place, which
// the live tables hold only when nothing touched the record since. So the
// compaction replays the family's effects up to the place, through foldOne,
// inside a savepoint, into a record cleared first, reads the rows, and rolls
// the savepoint back: the one fold, and no second account of what an effect
// does. The replay includes the effects that stay (a resync, an earlier
// purge, everything under them), since they moved the rows too. A record's own rows are a function
// of the effects that address it, which is what lets one record replay
// alone; the former-id rows are the exception, and they are why `former`
// and `resync` stay.
//
// WHAT A REPLAY OF A STATE DOES. A `state` writes the row whole, with the
// version, the kind version and the three times it carries, then derives
// from it what a record effect derives (projectRow), re-states the
// record's annotations and marks, and moves the unit graph's
// revision as far as the effects it replaced moved it. A `state` with no row
// does what a purge does. A `ledger` drops the trigger's delivery rows and
// writes the ones it carries. A replay of history with no state entry runs
// no new code, so it gives what it gave before.
//
// WHAT A READER MEETS. The change event names a state's record with
// `state` set, and a put or a patch whose state stands for the record's
// creation reads as the creation (changefeed.go projectAffected). A
// trigger's cursor is a seq: the entries at or below it were delivered, and
// a record whose change above it a compaction replaced is met at its state,
// which sits at or above that change. A trigger that starts from the
// beginning meets each record once, created in its latest state, then
// everything kept since; a watch resumed inside a replaced range meets each
// record it would have met, once. The oldest position a client may resume
// from does not move: several changes arriving as one is harmless, because
// a client fetches the record either way. Forgetting a deletion moves it.
//
// THE PROMISE. compactHistory is what a compaction pass and a permanent
// delete call. In one transaction that holds the
// changelog lock, it replaces each family's entries of every record scope
// admits up to upTo, and leaves every table `repository rebuild` fills as a
// replay of the history gave it before: rows, versions and times. It moves
// no seq and never the head, appends nothing, and changes no fold table,
// since the fold already holds what the states say, but for an erasure's
// own intent, whose row in compaction_erasures goes with its effect
// (erase.go), so the history it leaves replays into the table it leaves. It
// marks the archive to write its pieces again from the lowest seq it changed.

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

// recordState is a record's own rows as they stood at a state entry.
type recordState struct {
	// Row is the records row, absent where the record had none there: it
	// was purged, and the state does what a purge does.
	Row         *stateRow         `json:"row,omitempty"`
	Annotations []stateAnnotation `json:"annotations,omitempty"`
	// Marks are the record's property marks (decision 0284), each beside a
	// value its row holds.
	Marks []stateMark `json:"marks,omitempty"`
	// Created marks a state that stands for the entry that created the
	// record: a read presents it as the record's creation.
	Created bool `json:"created,omitempty"`
	// Creator is the three links of the line that created the record, where
	// the state stands for it: work a record starts acts for whoever its
	// creating line names (decision 0270, actors.go actingFor), and that
	// line is replaced.
	Creator *stateLinks `json:"creator,omitempty"`
	// Moves is how far the effects it replaced moved the unit graph's
	// revision (unitgraph.go), which counts the folds of unit and
	// conversion rows: carried, so a replay comes back at the revision it
	// left. Absent on any other kind.
	Moves foldInt `json:"moves,omitempty"`
}

// stateLinks are a write's three links (actors.go writeLinks), as its line
// held them: the actor record that wrote it, the one it acted for and the
// token behind it.
type stateLinks struct {
	Actor      string `json:"actor"`
	OnBehalfOf string `json:"onBehalfOf,omitempty"`
	Token      string `json:"token,omitempty"`
}

// stateRow is a records row, every column a replay writes. The jsonb columns
// travel as the text Postgres stored, so the replay writes the same value.
type stateRow struct {
	Title       string          `json:"title,omitempty"`
	States      json.RawMessage `json:"states"`
	At          *time.Time      `json:"at,omitempty"`
	EndsAt      *time.Time      `json:"endsAt,omitempty"`
	DueAt       *time.Time      `json:"dueAt,omitempty"`
	Props       json.RawMessage `json:"props"`
	Labels      json.RawMessage `json:"labels"`
	Finalizers  []string        `json:"finalizers,omitempty"`
	KindVersion foldVersion     `json:"kindVersion,omitempty"`
	Version     foldInt         `json:"version"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	DeletedAt   *time.Time      `json:"deletedAt,omitempty"`
}

type stateAnnotation struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// stateMark is one property's whole mark (marks.go), as a `mark` effect
// states it.
type stateMark struct {
	Property    string    `json:"property"`
	Pinned      bool      `json:"pinned,omitempty"`
	SetBy       string    `json:"setBy,omitempty"`
	OnBehalfOf  string    `json:"onBehalfOf,omitempty"`
	DerivedFrom []string  `json:"derivedFrom,omitempty"`
	SetAt       time.Time `json:"setAt"`
}

func (m stateMark) mark() mark {
	return mark{Pinned: m.Pinned, SetBy: m.SetBy, OnBehalfOf: m.OnBehalfOf, DerivedFrom: m.DerivedFrom, SetAt: m.SetAt.UTC()}
}

// triggerLedger is a trigger's delivery rows as they stood at a state entry.
type triggerLedger struct {
	Cursor   *ledgerCursor   `json:"cursor,omitempty"`
	Schedule *ledgerSchedule `json:"schedule,omitempty"`
	Failures []foldFailure   `json:"failures,omitempty"`
	Pages    []ledgerPage    `json:"pages,omitempty"`
}

type ledgerCursor struct {
	Seq       foldInt   `json:"seq"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ledgerSchedule struct {
	FiredAt   time.Time `json:"firedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ledgerPage is a resume row: the page effect's row and its stamp. Its
// cursor is carried whole where the replay that took it held the bytes, and
// by digest otherwise, as the page effect it replaced carried it (decision
// 0141).
type ledgerPage struct {
	foldPageRow
	UpdatedAt time.Time `json:"updatedAt"`
}

// --- the replay of a state entry ---

func (t *txn) applyState(op foldOp) (foldResult, error) {
	s := op.State
	ref := op.ref()
	if s == nil || ref.Kind == "" || ref.ID == "" {
		return foldResult{}, fmt.Errorf("substrate/engine: a state effect on %s %s carries no state", op.Ref, op.ID)
	}
	moves := int(s.Moves)
	ty, _ := t.derivationView().ByIdentity(ref.Kind)
	tiersBefore, err := t.metricTiersOf(ref)
	if err != nil {
		return foldResult{}, err
	}
	if err := t.beforeUnitFold(ref.Kind); err != nil {
		return foldResult{}, err
	}
	keyBefore, err := t.providerKeyOf(ref)
	if err != nil {
		return foldResult{}, err
	}
	if s.Row == nil {
		if err := t.applyPurge(ref); err != nil {
			return foldResult{}, err
		}
		t.noteProviderKey(ref, keyBefore, nil)
		if err := t.afterUnitFoldBy(ref.Kind, moves); err != nil {
			return foldResult{}, err
		}
		t.noteMetricRules(ty, ref, nil)
		if tiersBefore != nil {
			t.noteMetricTiers(ref, tiersBefore, nil)
		}
		return foldResult{changed: true}, nil
	}
	old, err := t.loadRow(ref, false)
	if err != nil {
		return foldResult{}, err
	}
	var usesBefore []string
	if old != nil {
		usesBefore = rowUnitUses(ty, old.Props)
	}
	row, err := s.Row.erow(ref)
	if err != nil {
		return foldResult{}, fmt.Errorf("substrate/engine: the state of %s %s: %w", ref.Kind, ref.ID, err)
	}
	if err := t.writeStateRow(row, s.Row); err != nil {
		return foldResult{}, err
	}
	// Restating a row takes nothing out of it: a property an earlier effect
	// of the line took out, and the state holds, keeps the mark the state
	// restates when the line ends (fold.go settleTakenOut).
	t.noteTakenOut(ref, row, nil)
	if err := t.restateSideRows(ref, s, row); err != nil {
		return foldResult{}, err
	}
	t.noteProviderKey(ref, keyBefore, row.Props["apiKey"])
	if err := t.projectRow(ref, row, old == nil, usesBefore, tiersBefore, bindsMetric(ty), moves); err != nil {
		return foldResult{}, err
	}
	return foldResult{changed: true, created: old == nil, row: row, version: row.Version}, nil
}

// erow is the row the state carries, its jsonb columns decoded the way a
// replayed delta's are (numbers kept as their lexemes).
func (r *stateRow) erow(ref eref) (*erow, error) {
	row := &erow{
		ID: ref.ID, Kind: ref.Kind, Title: r.Title,
		At: utcPtr(r.At), EndsAt: utcPtr(r.EndsAt), DueAt: utcPtr(r.DueAt),
		Version: int64(r.Version), KindVersion: int64(r.KindVersion),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), DeletedAt: utcPtr(r.DeletedAt),
		Finalizers: append([]string{}, r.Finalizers...),
	}
	var err error
	if row.Props, err = decodeNumberPreserving(r.Props); err != nil {
		return nil, fmt.Errorf("props: %w", err)
	}
	if row.Labels, err = decodeNumberPreserving(r.Labels); err != nil {
		return nil, fmt.Errorf("labels: %w", err)
	}
	row.Props, row.Labels = nonNilMap(row.Props), nonNilMap(row.Labels)
	row.States = map[string]string{}
	if len(r.States) > 0 {
		if err := json.Unmarshal(r.States, &row.States); err != nil {
			return nil, fmt.Errorf("states: %w", err)
		}
	}
	return row, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

// writeStateRow writes the row whole: every column the state carries, the
// version and the three times included, and the search bands derived from
// it. The two derived marks (orphaned_at, ambiguous_at) are left to their
// derivations, as every fold write leaves them.
func (t *txn) writeStateRow(row *erow, r *stateRow) error {
	finalizers, err := jsonb(nonNilStrings(row.Finalizers))
	if err != nil {
		return err
	}
	fts := t.foldFTS(row)
	_, err = t.exec(`
		INSERT INTO records (
			id, kind, title, states, at, ends_at, due_at, props, labels,
			finalizers, fts, version, kind_version, created_at, updated_at, deleted_at
		) VALUES (
			$1, $2, $3, $4::jsonb, $5, $6, $7, $8::jsonb, $9::jsonb,
			ARRAY(SELECT jsonb_array_elements_text($10::jsonb)),
			setweight(to_tsvector('english', $11), 'A') ||
			setweight(to_tsvector('english', $12), 'B') ||
			setweight(to_tsvector('english', $13), 'C'),
			$14, $15, $16, $17, $18
		)
		ON CONFLICT (repository, kind, id) DO UPDATE SET
			title = EXCLUDED.title, states = EXCLUDED.states,
			at = EXCLUDED.at, ends_at = EXCLUDED.ends_at, due_at = EXCLUDED.due_at,
			props = EXCLUDED.props, labels = EXCLUDED.labels, finalizers = EXCLUDED.finalizers,
			fts = EXCLUDED.fts, version = EXCLUDED.version, kind_version = EXCLUDED.kind_version,
			created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at,
			deleted_at = EXCLUDED.deleted_at`,
		row.ID, row.Kind, row.Title, rawOrEmpty(r.States, `{}`), row.At, row.EndsAt, row.DueAt,
		rawOrEmpty(r.Props, `{}`), rawOrEmpty(r.Labels, `{}`), finalizers, fts[0], fts[1], fts[2],
		row.Version, row.KindVersion, row.CreatedAt, row.UpdatedAt, row.DeletedAt)
	if err != nil {
		return fmt.Errorf("substrate/engine: write the state of %s %s: %w", row.Kind, row.ID, err)
	}
	return nil
}

func rawOrEmpty(raw json.RawMessage, empty string) []byte {
	if len(raw) == 0 {
		return []byte(empty)
	}
	return raw
}

// restateSideRows replaces the record's annotations and marks with the ones
// the state carries. A mark stands only beside a value its row holds
// (decision 0284), as a resync restates them: a state is taken from a fold
// that already keeps the rule, and a replay holds it again.
func (t *txn) restateSideRows(ref eref, s *recordState, row *erow) error {
	for _, q := range []string{
		`DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2`,
		`DELETE FROM property_marks WHERE record_kind = $1 AND record_id = $2`,
	} {
		if _, err := t.exec(q, ref.Kind, ref.ID); err != nil {
			return fmt.Errorf("substrate/engine: state of %s %s: %w", ref.Kind, ref.ID, err)
		}
	}
	for _, a := range s.Annotations {
		if _, err := t.exec(`
			INSERT INTO annotations (record_kind, record_id, key, value, updated_at)
			VALUES ($1, $2, $3, $4::jsonb, $5)`,
			ref.Kind, ref.ID, a.Key, rawOrNull(a.Value), a.UpdatedAt.UTC()); err != nil {
			return fmt.Errorf("substrate/engine: state annotation %s %s %s: %w", ref.Kind, ref.ID, a.Key, err)
		}
	}
	for _, m := range s.Marks {
		if row == nil || currentValue(row, m.Property) == nil {
			continue
		}
		if err := t.insertMark(ref, m.Property, m.mark()); err != nil {
			return fmt.Errorf("substrate/engine: state mark %s %s %s: %w", ref.Kind, ref.ID, m.Property, err)
		}
	}
	return nil
}

// applyLedger replaces the trigger's delivery rows with the ones the ledger
// carries, each with its own stamp.
func (t *txn) applyLedger(op foldOp) error {
	l := op.Ledger
	if l == nil || op.ID == "" {
		return fmt.Errorf("substrate/engine: a ledger effect on %s carries no ledger", op.ID)
	}
	for _, table := range deliveryTables {
		if _, err := t.exec(`DELETE FROM `+table+` WHERE trigger_id = $1`, op.ID); err != nil {
			return err
		}
	}
	if c := l.Cursor; c != nil {
		if _, err := t.exec(`INSERT INTO trigger_cursors (trigger_id, seq, updated_at) VALUES ($1, $2, $3)`,
			op.ID, int64(c.Seq), c.UpdatedAt.UTC()); err != nil {
			return err
		}
	}
	if s := l.Schedule; s != nil {
		if _, err := t.exec(`INSERT INTO trigger_schedule (trigger_id, fired_at, updated_at) VALUES ($1, $2, $3)`,
			op.ID, s.FiredAt.UTC(), s.UpdatedAt.UTC()); err != nil {
			return err
		}
	}
	for i := range l.Failures {
		if _, err := t.applyDelivery(foldOp{Kind: foldPark, Ref: op.Ref, ID: op.ID, Failure: &l.Failures[i]}); err != nil {
			return err
		}
	}
	for i := range l.Pages {
		if err := t.upsertPage(op.ID, &l.Pages[i].foldPageRow, l.Pages[i].UpdatedAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

// --- the compaction ---

// CompactReport is what one compaction did.
type CompactReport struct {
	Repository string `json:"repository"`
	// UpTo is the seq it compacted up to, the head where it was asked for
	// more.
	UpTo int64 `json:"upTo"`
	// Records and Triggers count the states and the ledgers it wrote.
	Records  int `json:"records"`
	Triggers int `json:"triggers"`
	// Effects is how many effects they replaced, Removed how many entries
	// were left with none and went, and Rewritten how many it wrote again.
	Effects   int `json:"effects"`
	Removed   int `json:"removed"`
	Rewritten int `json:"rewritten"`
	// From is the lowest seq it removed or rewrote, 0 where it changed
	// nothing.
	From int64 `json:"from,omitempty"`
	// Damaged is the first line it met that does not verify: it compacted
	// below it alone, UpTo is the line under it, and it did not look above
	// it. 0 where it met none.
	Damaged int64         `json:"damaged,omitempty"`
	Took    time.Duration `json:"took"`
	// touched is every seq the compaction rewrote or removed: the archive's
	// pieces holding one are written again.
	touched []int64
	// heldBack is the line of the lowest deletion a forgetting left whole
	// because an effect that stays holds it, 0 for none.
	heldBack int64
	// held is every record it left part of because a hold that ends kept
	// it, for the pass to try again (compaction_held).
	held []eref
	// unindexed is an erasure held because the foreign-effects index does
	// not cover its history yet (compactindex.go).
	unindexed bool
}

// compactFamily names which of a record's rows an effect writes.
type compactFamily uint8

const (
	familyRecord compactFamily = iota
	familyLedger
)

// familyOf is the family an effect replaces into, and whether a state may
// replace it. A resync moves the record's own rows too, so a capture
// replays it; it is never replaced.
func familyOf(k foldKind) (compactFamily, bool) {
	switch k {
	case foldRecord, foldTombstone, foldPurge, foldAnnotation, foldMark, foldUnmark, foldState:
		return familyRecord, true
	case foldCursor, foldSchedule, foldPark, foldUnpark, foldPage, foldUnpage, foldForget, foldLedger:
		return familyLedger, true
	}
	return 0, false
}

type compactKey struct {
	ref    eref
	family compactFamily
}

// compactOcc is one effect a family's capture replays: the line and the
// effect's place in it.
type compactOcc struct {
	seq  int64
	idx  int
	kind foldKind
}

type compactPlan struct {
	occ   []compactOcc
	place int64
}

// compactLine is what the compaction knows of one line it read.
type compactLine struct {
	seq, txn int64
	// effects is how many the line carries: one an edit leaves with none
	// is removed.
	effects int
	ts      time.Time
	addr    eref
	op      substrate.Op
	links   stateLinks
}

// compactEdit is what the compaction does to one line: effects it removes
// and the state it puts in the place of one.
type compactEdit struct {
	remove  map[int]bool
	replace map[int]foldOp
	// drop is the payload's keys beside the fold an erasure takes off: the
	// copies a merge or a split line carries of the pair's values (erase.go).
	drop []string
}

// compactHistory replaces, for every record scope admits, each family of its
// entries up to upTo with one state entry, as the file's header says. scope
// nil admits every record.
func (t *txn) compactHistory(upTo int64, scope func(eref) bool) (CompactReport, error) {
	return t.compactOver(upTo, scope, nil)
}

// compactRecords is compactHistory for the records refs alone: it reads
// their own lines (changelog_record_idx) and the lines that carry an effect
// on them for another record (compactindex.go), never the whole history, so
// its cost is the records' history and not the repository's. The index must
// cover the history (backfillForeign).
func (t *txn) compactRecords(upTo int64, refs []eref) (CompactReport, error) {
	if len(refs) == 0 {
		return CompactReport{Repository: t.ds.scope.Repository}, nil
	}
	in := make(map[eref]bool, len(refs))
	byKind := map[string][]string{}
	for _, r := range refs {
		if !in[r] {
			in[r] = true
			byKind[r.Kind] = append(byKind[r.Kind], r.ID)
		}
	}
	var only []int64
	for kind, ids := range byKind {
		rows, err := t.query(`
			SELECT seq FROM changelog WHERE record_id = ANY($2) AND kind = $1 AND seq <= $3
			UNION
			SELECT seq FROM changelog_foreign WHERE record_kind = $1 AND record_id = ANY($2) AND seq <= $3`, kind, ids, upTo)
		if err != nil {
			return CompactReport{}, err
		}
		for rows.Next() {
			var seq int64
			if err := rows.Scan(&seq); err != nil {
				_ = rows.Close()
				return CompactReport{}, err
			}
			only = append(only, seq)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return CompactReport{}, err
		}
	}
	slices.Sort(only)
	only = slices.Compact(only)
	if only == nil {
		only = []int64{}
	}
	t.held = map[eref]bool{}
	defer func() { t.held = nil }()
	report, err := t.compactOver(upTo, func(r eref) bool { return in[r] }, only)
	for r := range t.held {
		report.held = append(report.held, r)
	}
	slices.SortFunc(report.held, func(a, b eref) int { return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID)) })
	return report, err
}

// compactOver is compactHistory over the lines at only, or every line when
// only is nil.
func (t *txn) compactOver(upTo int64, scope func(eref) bool, only []int64) (CompactReport, error) {
	report := CompactReport{Repository: t.ds.scope.Repository}
	if err := t.lockChangelog(); err != nil {
		return report, err
	}
	head, err := tableChangelogHead(t.ctx, t.tx)
	if err != nil {
		return report, err
	}
	upTo = min(upTo, head)
	report.UpTo = upTo
	// Nothing at or above a line known not to verify is compacted (damage.go),
	// the cut read under the changelog lock.
	if cut, err := t.damageCut(); err != nil {
		return report, err
	} else if cut > 0 && cut <= upTo {
		// An erasure is whole or held on the line (erase.go), never cut.
		if t.erasing {
			report.Damaged = cut
			return report, nil
		}
		upTo, report.Damaged, report.UpTo = cut-1, cut, cut-1
	}
	// A batch finds its records' lines by the foreign-effects index
	// (compactindex.go), so it stays inside what the index covers, read in
	// the same transaction: a derivation that stopped below a line that does
	// not verify and could not note it is no cut, and this bound holds all
	// the same.
	if only != nil {
		var indexed int64
		var whole bool
		if err := t.row(`
			SELECT coalesce((SELECT foreign_upto FROM compaction_state), 0),
				coalesce((SELECT foreign_done FROM compaction_state), false)`).Scan(&indexed, &whole); err != nil {
			return report, err
		}
		if !whole && indexed < upTo {
			// An erasure is whole or held: the pass derives the index first.
			if t.erasing {
				report.unindexed = true
				return report, nil
			}
			upTo, report.UpTo = indexed, indexed
		}
	}
	if upTo <= 0 {
		return report, nil
	}
	// A state entry is spelled in the dialect that brought it, and a history
	// stamped below it is one the boot check has not converted yet.
	if stamp, err := readChangelogDialect(t.ctx, t.tx); err != nil {
		return report, err
	} else if max(stamp, 1) < conversionStateEntries.Dialect {
		return report, fmt.Errorf("%w: compaction writes state entries, of changelog dialect %d, and the history is in dialect %d",
			ErrConversionPending, conversionStateEntries.Dialect, max(stamp, 1))
	}
	if scope == nil {
		scope = func(eref) bool { return true }
	}
	barrier, err := t.compactBarriers()
	if err != nil {
		return report, err
	}
	spooled, err := t.ds.spoolHolds()
	if err != nil {
		return report, err
	}
	// A batch reads only its records' lines (only): a transaction it
	// re-frames, one whose end line it leaves with no effect, has other lines
	// whose `txn` moves, and they are read and held before any edit. A
	// damaged one among them cuts the batch lower, and it is planned again.
	var edits map[int64]*compactEdit
	var lines map[int64]compactLine
	for {
		held, forgetHeld := maps.Clone(t.held), t.forgetHeld
		report.Effects, report.Records, report.Triggers = 0, 0, 0
		plans, read, damaged, err := t.compactPlans(upTo, scope, only, barrier, spooled)
		if err != nil {
			return report, err
		}
		lines = read
		if damaged > 0 {
			if err := t.noteDamage(damaged, t.damageNoter()); err != nil {
				return report, err
			}
			report.Damaged, report.UpTo = damaged, damaged-1
			if t.erasing {
				return report, nil
			}
		}
		// A record a hold that ends keeps a line of (a parked failure's, a
		// drain's) is noted for the pass to come back to, before a plan with no
		// line for its state is dropped: one whose every line in reach is held
		// has none, and would otherwise be passed over for good.
		for k, p := range plans {
			if !slices.ContainsFunc(p.occ, func(o compactOcc) bool { return barrier[o.seq] }) {
				continue
			}
			switch {
			case t.forgetting && k.family == familyRecord && p.place == 0:
				if seq := p.occ[len(p.occ)-1].seq; t.forgetHeld == 0 || seq < t.forgetHeld {
					t.forgetHeld = seq
				}
			case !t.forgetting && t.held != nil:
				t.held[k.ref] = true
			}
		}
		keys := make([]compactKey, 0, len(plans))
		for k, p := range plans {
			if p.place > 0 {
				keys = append(keys, k)
			}
		}
		slices.SortFunc(keys, func(a, b compactKey) int {
			if a.ref != b.ref {
				if a.ref.less(b.ref) {
					return -1
				}
				return 1
			}
			return int(a.family) - int(b.family)
		})
		edits = map[int64]*compactEdit{}
		edit := func(seq int64) *compactEdit {
			e := edits[seq]
			if e == nil {
				e = &compactEdit{remove: map[int]bool{}, replace: map[int]foldOp{}}
				edits[seq] = e
			}
			return e
		}
		for _, k := range keys {
			p := plans[k]
			state, at, replaced, err := t.captureState(k, p, lines, barrier)
			if err != nil {
				return report, err
			}
			if len(replaced) == 0 {
				continue
			}
			for _, o := range replaced {
				if o == at && state.Kind != "" {
					edit(o.seq).replace[o.idx] = state
				} else {
					edit(o.seq).remove[o.idx] = true
				}
			}
			report.Effects += len(replaced)
			if k.family == familyRecord {
				report.Records++
			} else {
				report.Triggers++
			}
		}
		if only == nil || len(edits) == 0 {
			break
		}
		reframed, err := t.reframedDamage(edits, lines)
		if err != nil {
			return report, err
		}
		if reframed == 0 {
			break
		}
		if err := t.noteDamage(reframed, t.damageNoter()); err != nil {
			return report, err
		}
		t.held, t.forgetHeld = held, forgetHeld
		upTo, report.Damaged, report.UpTo = reframed-1, reframed, reframed-1
		if t.erasing {
			return report, nil
		}
		if upTo <= 0 {
			return report, nil
		}
	}
	if len(edits) == 0 {
		return report, nil
	}
	if err := t.applyCompactEdits(edits, lines, head, &report); err != nil {
		return report, err
	}
	return report, t.markArchiveRewrite(report.touched)
}

// compactBarriers is every line a compaction leaves alone: the lines a live
// parked failure names, which its retry rebuilds its envelope from, and the
// lines a live record drain's chain names.
func (t *txn) compactBarriers() (map[int64]bool, error) {
	out := map[int64]bool{}
	rows, err := t.query(`SELECT seq FROM trigger_failures WHERE seq > 0`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out[seq] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	chains, err := t.query(`SELECT chain FROM paged_cursors`)
	if err != nil {
		return nil, err
	}
	for chains.Next() {
		var chain string
		if err := chains.Scan(&chain); err != nil {
			_ = chains.Close()
			return nil, err
		}
		// A record chain ends in the seq it delivers (recordChainKey); a
		// fire chain's fire id that reads as a number holds a line back for
		// nothing, which costs a few effects and never a wrong fold.
		if i := strings.LastIndexByte(chain, '/'); i >= 0 {
			if seq, err := strconv.ParseInt(chain[i+1:], 10, 64); err == nil && seq > 0 {
				out[seq] = true
			}
		}
	}
	return out, errors.Join(chains.Err(), chains.Close())
}

// spoolHolds reports whether the webhook spool holds a request the
// changelog may not: while it does, no trigger's delivery entries are
// replaced (the file's header says why).
func (ds *dataset) spoolHolds() (bool, error) {
	if ds.dir == "" {
		return false, nil
	}
	files, err := changelogfile.SpoolFiles(ds.dir)
	if err != nil {
		return false, err
	}
	return len(files) > 0, nil
}

// compactPlans reads every line up to upTo once and finds, for each family
// of each record scope admits, the effects a capture replays and the line
// its state goes on.
// compactPlans reads, in order of seq, every line up to upTo or the ones at
// only, stopping before the first that does not verify, whose seq it
// reports, and finds for each family of each record scope admits the
// effects a capture replays and the line its state goes on.
func (t *txn) compactPlans(upTo int64, scope func(eref) bool, only []int64, barrier map[int64]bool, spooled bool) (plans map[compactKey]*compactPlan, lines map[int64]compactLine, damaged int64, err error) {
	plans = map[compactKey]*compactPlan{}
	lines = map[int64]compactLine{}
	plan := func(k compactKey) *compactPlan {
		p := plans[k]
		if p == nil {
			p = &compactPlan{}
			plans[k] = p
		}
		return p
	}
	// The lines read: every line up to upTo, or only the ones at only.
	next := func(after int64) ([]checksumRow, error) {
		if only == nil {
			return queryChecksumRows(t.ctx, t.tx, `WHERE seq > $1 AND seq <= $2 ORDER BY seq LIMIT $3`, after, upTo, rebuildBatch)
		}
		i, _ := slices.BinarySearch(only, after+1)
		j, _ := slices.BinarySearch(only, upTo+1)
		page := only[i:min(j, i+rebuildBatch)]
		if len(page) == 0 {
			return nil, nil
		}
		return queryChecksumRows(t.ctx, t.tx, `WHERE seq = ANY($1) ORDER BY seq`, page)
	}
	for after := int64(0); after < upTo; {
		page, err := next(after)
		if err != nil {
			return nil, nil, 0, err
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			e := row.entry
			// Nothing of a line that does not verify is read but its seq,
			// and the compaction stops below it (the file's header).
			if !row.verifies() {
				return plans, lines, e.Seq, nil
			}
			after = e.Seq
			ch, err := changeOfEntry(e.fileEntry())
			if err != nil {
				return nil, nil, 0, err
			}
			ops, err := foldOpsOf(ch)
			if err != nil {
				return nil, nil, 0, err
			}
			if len(ops) == 0 {
				continue
			}
			l := compactLine{
				seq: e.Seq, txn: e.Txn, effects: len(ops), ts: e.TS, addr: eref{Kind: e.Kind, ID: e.RecordID}, op: substrate.Op(e.Op),
				links: stateLinks{Actor: e.Actor, OnBehalfOf: e.OnBehalfOf, Token: e.Token},
			}
			lines[e.Seq] = l
			for i, op := range ops {
				if op.Kind == foldResync {
					for _, r := range op.Scope {
						if ref := eref(r); scope(ref) {
							p := plan(compactKey{ref, familyRecord})
							p.occ = append(p.occ, compactOcc{e.Seq, i, op.Kind})
						}
					}
					continue
				}
				family, ok := familyOf(op.Kind)
				ref := op.ref()
				if !ok || ref.Kind == "" || ref.ID == "" || !scope(ref) {
					continue
				}
				if family == familyLedger && spooled {
					if t.held != nil {
						t.held[ref] = true
					}
					continue
				}
				p := plan(compactKey{ref, family})
				p.occ = append(p.occ, compactOcc{e.Seq, i, op.Kind})
				if l.addr == ref && !barrier[e.Seq] && (family == familyLedger || l.op != substrate.OpDelivery) {
					p.place = e.Seq
				}
			}
		}
	}
	return plans, lines, 0, nil
}

// reframedDamage is the lowest line that does not verify in a transaction
// the edits re-frame, one whose end line they leave with no effect, 0 for
// none (transactionDamage).
func (t *txn) reframedDamage(edits map[int64]*compactEdit, lines map[int64]compactLine) (int64, error) {
	lowest := int64(0)
	for _, seq := range slices.Sorted(maps.Keys(edits)) {
		ed, l := edits[seq], lines[seq]
		if l.txn != seq || len(ed.replace) > 0 || len(ed.remove) < l.effects {
			continue
		}
		damaged, err := t.transactionDamage(seq)
		if err != nil {
			return 0, err
		}
		if damaged > 0 && (lowest == 0 || damaged < lowest) {
			lowest = damaged
		}
	}
	return lowest, nil
}

// transactionDamage is the lowest line of the transaction that ends at end
// that does not verify, 0 for none, read a page at a time. A transaction's
// lines are consecutive seqs ending at its `txn`, so they are the run back
// from end whose `txn` is end, found by walking the key back (`txn` has no
// index). The line just below the run counts too: if it does not verify,
// the damage may have taken its `txn`.
func (t *txn) transactionDamage(end int64) (int64, error) {
	var below sql.NullInt64
	if err := t.row(`SELECT max(seq) FROM changelog WHERE seq <= $1 AND txn IS DISTINCT FROM $1`, end).Scan(&below); err != nil {
		return 0, err
	}
	if below.Valid {
		row, err := queryChecksumRows(t.ctx, t.tx, `WHERE seq = $1`, below.Int64)
		if err != nil {
			return 0, err
		}
		if len(row) == 1 && !row[0].verifies() {
			return below.Int64, nil
		}
	}
	for after := below.Int64; after < end; {
		page, err := queryChecksumRows(t.ctx, t.tx, `WHERE seq > $1 AND seq <= $2 ORDER BY seq LIMIT $3`, after, end, rebuildBatch)
		if err != nil {
			return 0, err
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if !row.verifies() {
				return row.entry.Seq, nil
			}
			after = row.entry.Seq
		}
	}
	return 0, nil
}

// captureState takes one family's state at its place: it replays the
// family's effects up to the place inside a savepoint, into a record
// cleared first, reads the rows and rolls the savepoint back. It returns the
// state entry, the effect it takes the place of, and every effect it
// replaces.
func (t *txn) captureState(k compactKey, p *compactPlan, lines map[int64]compactLine, barrier map[int64]bool) (foldOp, compactOcc, []compactOcc, error) {
	// The last effect the state replaces on its line: the family's last
	// there, but a purge only where it is the record's last.
	last := -1
	for i, o := range p.occ {
		if o.seq == p.place && o.kind != foldResync {
			last = i
		}
	}
	if last < 0 {
		return foldOp{}, compactOcc{}, nil, nil
	}
	at := p.occ[last]
	// The state replaces only what follows the family's newest effect that
	// stays, which replays against the rows everything under it left. A
	// record forgotten keeps no purge.
	from := 0
	for i, o := range p.occ[:last] {
		// An erasure of a purged record replaces its merge's resync too: the
		// partners it names are erased with it (erase.go).
		resyncStays := o.kind == foldResync && !(t.erasing && t.forgetting)
		if resyncStays || barrier[o.seq] || (o.kind == foldPurge && !t.forgetting) {
			from = i + 1
		}
	}
	replaced := p.occ[from : last+1]
	// An erasure is whole or held: a live record's that would leave values
	// under an effect that stays waits for it instead (erase.go).
	if t.erasing && !t.forgetting && from > 0 && t.held != nil {
		t.held[k.ref] = true
		return foldOp{}, compactOcc{}, nil, nil
	}
	if t.forgetting {
		if k.family != familyRecord {
			return foldOp{}, compactOcc{}, nil, nil
		}
		// A record is forgotten whole or not at all: one with an effect that
		// stays keeps what is under it, and so its deletion, which the pass
		// looks at again once nothing holds it (forgetHeld).
		if from > 0 || last < len(p.occ)-1 {
			if seq := p.occ[len(p.occ)-1].seq; t.forgetHeld == 0 || seq < t.forgetHeld {
				t.forgetHeld = seq
			}
			return foldOp{}, compactOcc{}, nil, nil
		}
	}
	// A family whose only effect up to its place is already its state has
	// nothing to replace: compacting again rewrites no line.
	if len(replaced) == 1 && (at.kind == foldState || at.kind == foldLedger) && !t.forgetting {
		return foldOp{}, compactOcc{}, nil, nil
	}
	sc := &txn{
		ctx: t.ctx, ds: t.ds, tx: t.tx, actor: substrate.ActorSystem, tier: substrate.TierMachine,
		internal: true, replaying: true, observed: &observedTouch{all: true},
	}
	if _, err := t.exec(`SAVEPOINT substrate_compact`); err != nil {
		return foldOp{}, compactOcc{}, nil, err
	}
	state, err := sc.replayFamily(k, p.occ[:last+1], replaced, lines)
	if _, rerr := t.exec(`ROLLBACK TO SAVEPOINT substrate_compact`); rerr != nil && err == nil {
		err = rerr
	}
	if _, rerr := t.exec(`RELEASE SAVEPOINT substrate_compact`); rerr != nil && err == nil {
		err = rerr
	}
	if err != nil {
		return foldOp{}, compactOcc{}, nil, fmt.Errorf("substrate/engine: compact %s %s: %w", k.ref.Kind, k.ref.ID, err)
	}
	if t.forgetting {
		// A record forgotten has no row where its history ends: its
		// entries go, every purge included, and so does the state, but for
		// how far they moved the unit graph's revision, which a state with
		// neither row nor values keeps. A record with a row is not a
		// deletion, and is left.
		switch {
		case state.State == nil || state.State.Row != nil:
			return foldOp{}, compactOcc{}, nil, nil
		case state.State.Moves == 0 && !t.erasing:
			return foldOp{}, at, replaced, nil
		}
		state.State = &recordState{Moves: state.State.Moves}
	}
	return state, at, replaced, nil
}

// replayFamily is the savepoint's work: clear the family's rows, fold its
// effects in order, and read the rows back as a state entry.
func (sc *txn) replayFamily(k compactKey, occ, replaced []compactOcc, lines map[int64]compactLine) (foldOp, error) {
	ref := k.ref
	if k.family == familyRecord {
		if err := sc.applyPurge(ref); err != nil {
			return foldOp{}, err
		}
	} else {
		if err := sc.keepPagedCursorsOf(ref.ID); err != nil {
			return foldOp{}, err
		}
		for _, table := range deliveryTables {
			if _, err := sc.exec(`DELETE FROM `+table+` WHERE trigger_id = $1`, ref.ID); err != nil {
				return foldOp{}, err
			}
		}
	}
	isReplaced := map[compactOcc]bool{}
	for _, o := range replaced {
		isReplaced[o] = true
	}
	ops, err := sc.loadEffects(occ)
	if err != nil {
		return foldOp{}, err
	}
	// moves counts what the replaced effects moved the unit graph's
	// revision by; created whether the effect that created the row the
	// state lands is among them, rather than on a line the compaction keeps.
	moves, created := 0, false
	var creator *stateLinks
	// A line ends where the next begins, and the fold's own rule runs there
	// (fold.go settleTakenOut): a property a line took out of the row loses
	// its mark, so the marks the state reads are the ones the replay left.
	for i, o := range occ {
		op := ops[i]
		if i > 0 && occ[i-1].seq != o.seq {
			if err := sc.settleTakenOut(); err != nil {
				return foldOp{}, err
			}
		}
		sc.now = lines[o.seq].ts.UTC()
		before := sc.unitMoves
		if _, err := sc.foldOne(op); err != nil {
			return foldOp{}, fmt.Errorf("replay seq %d: %w", o.seq, err)
		}
		if isReplaced[o] {
			moves += sc.unitMoves - before
		}
		switch {
		case op.Kind == foldRecord && op.Delta != nil && op.Delta.Created:
			created, creator = isReplaced[o], nil
			if created {
				l := lines[o.seq].links
				creator = &l
			}
		case op.Kind == foldState && op.State != nil && op.State.Created:
			created, creator = isReplaced[o], op.State.Creator
		case op.Kind == foldPurge:
			created, creator = false, nil
		}
	}
	if err := sc.settleTakenOut(); err != nil {
		return foldOp{}, err
	}
	if k.family == familyLedger {
		l, err := sc.readLedger(ref.ID)
		if err != nil {
			return foldOp{}, err
		}
		return foldOp{Kind: foldLedger, Ref: ref.Kind, ID: ref.ID, Ledger: l}, nil
	}
	s, err := sc.readState(ref)
	if err != nil {
		return foldOp{}, err
	}
	s.Created = created && s.Row != nil
	if s.Created {
		s.Creator = creator
	}
	if isUnitGraphKind(ref.Kind) {
		s.Moves = foldInt(moves)
	}
	op := foldOp{Kind: foldState, Ref: ref.Kind, ID: ref.ID, State: s}
	if s.Row != nil {
		op.Version = json.Number(strconv.FormatInt(int64(s.Row.Version), 10))
	}
	return op, nil
}

// loadEffects reads the effects occ names from their lines, as stored.
func (t *txn) loadEffects(occ []compactOcc) ([]foldOp, error) {
	seqs := make([]int64, 0, len(occ))
	for _, o := range occ {
		if len(seqs) == 0 || seqs[len(seqs)-1] != o.seq {
			seqs = append(seqs, o.seq)
		}
	}
	rows, err := queryChecksumRows(t.ctx, t.tx, `WHERE seq = ANY($1) ORDER BY seq`, seqs)
	if err != nil {
		return nil, err
	}
	bySeq := make(map[int64][]foldOp, len(rows))
	for _, r := range rows {
		ch, err := changeOfEntry(r.entry.fileEntry())
		if err != nil {
			return nil, err
		}
		ops, err := foldOpsOf(ch)
		if err != nil {
			return nil, err
		}
		bySeq[r.entry.Seq] = ops
	}
	out := make([]foldOp, len(occ))
	for i, o := range occ {
		ops := bySeq[o.seq]
		if o.idx >= len(ops) || ops[o.idx].Kind != o.kind {
			return nil, fmt.Errorf("seq %d no longer holds the effect it held", o.seq)
		}
		out[i] = ops[o.idx]
	}
	return out, nil
}

// readState reads a record's own rows into a state.
func (t *txn) readState(ref eref) (*recordState, error) {
	s := &recordState{}
	var r stateRow
	var states, props, labels, finalizers []byte
	var kindVersion, version int64
	err := t.row(`
		SELECT title, states::text, at, ends_at, due_at, props::text, labels::text,
			to_jsonb(finalizers)::text, kind_version, version, created_at, updated_at, deleted_at
		FROM records WHERE kind = $1 AND id = $2`, ref.Kind, ref.ID).
		Scan(&r.Title, &states, &r.At, &r.EndsAt, &r.DueAt, &props, &labels,
			&finalizers, &kindVersion, &version, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return s, nil
	case err != nil:
		return nil, err
	}
	r.States, r.Props, r.Labels = states, props, labels
	r.Finalizers = jsonStrings(finalizers)
	r.KindVersion, r.Version = foldVersion(kindVersion), foldInt(version)
	r.At, r.EndsAt, r.DueAt, r.DeletedAt = utcPtr(r.At), utcPtr(r.EndsAt), utcPtr(r.DueAt), utcPtr(r.DeletedAt)
	r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
	s.Row = &r
	if err := t.scanInto(`
		SELECT key, value::text, updated_at FROM annotations
		WHERE record_kind = $1 AND record_id = $2 ORDER BY key`, ref, func(rows *sql.Rows) error {
		var a stateAnnotation
		var value []byte
		if err := rows.Scan(&a.Key, &value, &a.UpdatedAt); err != nil {
			return err
		}
		a.Value, a.UpdatedAt = value, a.UpdatedAt.UTC()
		s.Annotations = append(s.Annotations, a)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := t.scanInto(`
		SELECT property, pinned, set_by, on_behalf_of, derived_from::text, set_at FROM property_marks
		WHERE record_kind = $1 AND record_id = $2 ORDER BY property`, ref, func(rows *sql.Rows) error {
		var m stateMark
		var derived []byte
		if err := rows.Scan(&m.Property, &m.Pinned, &m.SetBy, &m.OnBehalfOf, &derived, &m.SetAt); err != nil {
			return err
		}
		if err := json.Unmarshal(derived, &m.DerivedFrom); err != nil {
			return fmt.Errorf("mark %s derived_from: %w", m.Property, err)
		}
		if len(m.DerivedFrom) == 0 {
			m.DerivedFrom = nil
		}
		m.SetAt = m.SetAt.UTC()
		s.Marks = append(s.Marks, m)
		return nil
	}); err != nil {
		return nil, err
	}
	return s, nil
}

// readLedger reads a trigger's delivery rows into a ledger.
func (t *txn) readLedger(triggerID string) (*triggerLedger, error) {
	l := &triggerLedger{}
	var c ledgerCursor
	var seq int64
	switch err := t.row(`SELECT seq, updated_at FROM trigger_cursors WHERE trigger_id = $1`, triggerID).Scan(&seq, &c.UpdatedAt); {
	case err == nil:
		c.Seq, c.UpdatedAt = foldInt(seq), c.UpdatedAt.UTC()
		l.Cursor = &c
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	var s ledgerSchedule
	switch err := t.row(`SELECT fired_at, updated_at FROM trigger_schedule WHERE trigger_id = $1`, triggerID).Scan(&s.FiredAt, &s.UpdatedAt); {
	case err == nil:
		s.FiredAt, s.UpdatedAt = s.FiredAt.UTC(), s.UpdatedAt.UTC()
		l.Schedule = &s
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	rows, err := t.query(`
		SELECT id, seq, fire_id, record_id, attempts, last_error, parked_at, payload::text
		FROM trigger_failures WHERE trigger_id = $1 ORDER BY id`, triggerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f foldFailure
		var id, fseq, attempts int64
		var payload sql.NullString
		if err := rows.Scan(&id, &fseq, &f.FireID, &f.RecordID, &attempts, &f.LastError, &f.ParkedAt, &payload); err != nil {
			_ = rows.Close()
			return nil, err
		}
		f.ID, f.Seq, f.Attempts, f.ParkedAt = foldInt(id), foldInt(fseq), foldInt(attempts), f.ParkedAt.UTC()
		if payload.Valid {
			f.Payload = json.RawMessage(payload.String)
		}
		l.Failures = append(l.Failures, f)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	pages, err := t.query(`
		SELECT chain, cursor::text, pages, version, effects, bytes, started_at, kind, identity, updated_at
		FROM paged_cursors WHERE trigger_id = $1 ORDER BY chain`, triggerID)
	if err != nil {
		return nil, err
	}
	for pages.Next() {
		var p ledgerPage
		var cursor []byte
		var n, version, effects, size int64
		if err := pages.Scan(&p.Chain, &cursor, &n, &version, &effects, &size, &p.StartedAt, &p.Kind, &p.Identity, &p.UpdatedAt); err != nil {
			_ = pages.Close()
			return nil, err
		}
		p.Cursor = cursor
		p.Pages, p.Version, p.Effects, p.Bytes = foldInt(n), foldInt(version), foldInt(effects), foldInt(size)
		p.StartedAt, p.UpdatedAt = p.StartedAt.UTC(), p.UpdatedAt.UTC()
		l.Pages = append(l.Pages, p)
	}
	return l, errors.Join(pages.Err(), pages.Close())
}

// keepPagedCursorsOf is keepPagedCursors for one trigger's chains: a
// capture's replay of a page that names its cursor by hash finds it here.
func (t *txn) keepPagedCursorsOf(triggerID string) error {
	if _, err := t.exec(`DROP TABLE IF EXISTS pg_temp.substrate_kept_cursors`); err != nil {
		return err
	}
	if _, err := t.exec(`
		CREATE TEMPORARY TABLE substrate_kept_cursors (
			chain  text  PRIMARY KEY,
			cursor jsonb NOT NULL,
			digest text  NOT NULL
		) ON COMMIT DROP`); err != nil {
		return err
	}
	if _, err := t.exec(`
		INSERT INTO pg_temp.substrate_kept_cursors (chain, cursor, digest)
		SELECT chain, cursor, `+cursorDigestSQL("cursor")+` FROM paged_cursors WHERE trigger_id = $1`, triggerID); err != nil {
		return err
	}
	t.keptCursors = true
	return nil
}

// applyCompactEdits rewrites every line the edits touch: each loses the
// effects removed and takes the states in their places, a line left with no
// effect goes, a transaction whose last line went ends at its newest kept
// line, and every line that changed gets its checksum again.
func (t *txn) applyCompactEdits(edits map[int64]*compactEdit, lines map[int64]compactLine, head int64, report *CompactReport) error {
	seqs := make([]int64, 0, len(edits))
	for seq := range edits {
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	report.From = seqs[0]
	changed := map[int64]bool{}
	reframe := map[int64]bool{}
	for _, seq := range seqs {
		ed := edits[seq]
		var stored []byte
		if err := t.row(`SELECT payload::text FROM changelog WHERE seq = $1`, seq).Scan(&stored); err != nil {
			return fmt.Errorf("substrate/engine: compact seq %d: %w", seq, err)
		}
		payload, err := decodeNumberPreserving(stored)
		if err != nil {
			return fmt.Errorf("substrate/engine: compact seq %d: %w", seq, err)
		}
		effects, _ := payload[foldPayloadKey].([]any)
		kept := make([]any, 0, len(effects))
		for i, e := range effects {
			if op, ok := ed.replace[i]; ok {
				generic, err := genericEffect(op)
				if err != nil {
					return err
				}
				kept = append(kept, generic)
				continue
			}
			if !ed.remove[i] {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 && seq == head {
			// The head stays, with no effect: the next write takes the seq
			// after it, and a seq is never used twice. A forget's line is
			// one, once its intent is taken off it (erase.go).
			kept = []any{}
		} else if len(kept) == 0 {
			if _, err := t.exec(`DELETE FROM changelog WHERE seq = $1`, seq); err != nil {
				return err
			}
			report.Removed++
			if l := lines[seq]; l.txn == seq {
				reframe[seq] = true
			}
			delete(changed, seq)
			continue
		}
		payload[foldPayloadKey] = kept
		for _, key := range ed.drop {
			delete(payload, key)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := t.exec(`UPDATE changelog SET payload = $2::jsonb WHERE seq = $1`, seq, raw); err != nil {
			return fmt.Errorf("substrate/engine: compact seq %d: %w", seq, err)
		}
		changed[seq] = true
	}
	// A transaction's lines are consecutive seqs ending at its `txn`, which
	// the archive cuts its pieces on, so what is left of one is the run
	// below its end whose `txn` is still that end: the nearest line below
	// the end ends it now, if it is one of them. `txn` has no index, so the
	// run is found by walking the key back from the end, at the cost of the
	// transaction's lines and never of the history.
	for end := range reframe {
		var newEnd sql.NullInt64
		if err := t.row(`
			SELECT (SELECT seq FROM (SELECT seq, txn FROM changelog WHERE seq < $1 ORDER BY seq DESC LIMIT 1) p WHERE txn = $1)`,
			end).Scan(&newEnd); err != nil {
			return err
		}
		if !newEnd.Valid {
			continue
		}
		rows, err := t.query(`
			UPDATE changelog SET txn = $2 WHERE seq <= $2 AND txn = $1
			AND seq > (SELECT coalesce(max(seq), 0) FROM changelog WHERE seq < $2 AND txn IS DISTINCT FROM $1)
			RETURNING seq`, end, newEnd.Int64)
		if err != nil {
			return err
		}
		for rows.Next() {
			var seq int64
			if err := rows.Scan(&seq); err != nil {
				_ = rows.Close()
				return err
			}
			changed[seq] = true
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	if err := t.restampChecksums(changed, report); err != nil {
		return err
	}
	report.touched = append(report.touched, seqs...)
	for seq := range changed {
		report.touched = append(report.touched, seq)
	}
	slices.Sort(report.touched)
	report.touched = slices.Compact(report.touched)
	return t.reindexForeign(seqs)
}

// verifies reports whether a row is what its stored checksum was stamped
// over, in the frame of the dialect a compaction runs at: verify's rule
// under --recanonicalize (verify.go).
func (r checksumRow) verifies() bool {
	_, sum, err := changelogfile.Encode(r.entry.fileEntry())
	return err == nil && bytes.Equal(sum[:], r.hash)
}

// genericEffect is a state entry as the payload holds an effect: the
// decoded JSON object, numbers kept.
func genericEffect(op foldOp) (any, error) {
	raw, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	return out, dec.Decode(&out)
}

// restampChecksums stamps the checksum of every line in changed from its
// stored columns, as settleChecksums does at a commit.
func (t *txn) restampChecksums(changed map[int64]bool, report *CompactReport) error {
	if len(changed) == 0 {
		return nil
	}
	seqs := make([]int64, 0, len(changed))
	for seq := range changed {
		seqs = append(seqs, seq)
		report.From = min(report.From, seq)
	}
	rows, err := queryChecksumRows(t.ctx, t.tx, `WHERE seq = ANY($1) ORDER BY seq`, seqs)
	if err != nil {
		return err
	}
	for _, r := range rows {
		_, sum, err := changelogfile.Encode(r.entry.fileEntry())
		if err != nil {
			return fmt.Errorf("substrate/engine: seq %d does not encode after the compaction: %w", r.entry.Seq, err)
		}
		if _, err := t.exec(`UPDATE changelog SET hash = $2 WHERE seq = $1`, r.entry.Seq, sum[:]); err != nil {
			return err
		}
		report.Rewritten++
	}
	return nil
}

// markArchiveRewrite tells the archive pass which lines are not what its
// pieces hold: each seq the compaction rewrote or removed, stamped with this
// compaction's number, so a pass clears only the rows its snapshot saw
// (archive.go). The number is above every stamp a pass can have seen, from
// the record's count or the rows still there, so a mark is kept whether or
// not the archive has a record yet: the first pass of an archive may have
// read its snapshot before this compaction and create its record after it.
func (t *txn) markArchiveRewrite(seqs []int64) error {
	if len(seqs) == 0 {
		return nil
	}
	var rewrite int64
	if err := t.row(`
		SELECT greatest(
			coalesce((SELECT rewrites FROM archive_records), 0),
			coalesce((SELECT max(rewrite) FROM archive_rewritten), 0)) + 1`).Scan(&rewrite); err != nil {
		return err
	}
	if _, err := t.exec(`UPDATE archive_records SET rewrites = $1`, rewrite); err != nil {
		return err
	}
	_, err := t.exec(`
		INSERT INTO archive_rewritten (seq, rewrite) SELECT unnest($1::bigint[]), $2
		ON CONFLICT (repository, seq) DO UPDATE SET rewrite = EXCLUDED.rewrite`, seqs, rewrite)
	return err
}

// --- the operator's door ---

// CompactRepository compacts one repository's history up to upTo, every
// record, the head where upTo is 0 or above it: the operator's door to what
// the compaction pass will call, and the one a test calls. Like a rebuild,
// it appends no entry of its own.
func (s *service) CompactRepository(ctx context.Context, repository string, upTo int64) (CompactReport, error) {
	started := time.Now()
	if s.readOnly {
		return CompactReport{}, ErrDirectoryReadOnly
	}
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return CompactReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return CompactReport{}, err
	}
	// A note whose row was written back goes first (damage.go).
	if _, err := ds.lookAtDamage(ctx); err != nil {
		return CompactReport{}, err
	}
	report, err := ds.compact(ctx, upTo, nil)
	report.Took = time.Since(started)
	return report, err
}

// compact runs compactHistory in a transaction of its own and, once it has
// committed, empties the change tail's window, which may hold a line it
// changed.
func (ds *dataset) compact(ctx context.Context, upTo int64, scope func(eref) bool) (CompactReport, error) {
	if upTo <= 0 {
		upTo = 1<<63 - 1
	}
	var report CompactReport
	err := ds.withCompactionTx(ctx, true, func(t *txn) error {
		var err error
		report, err = t.compactHistory(upTo, scope)
		return err
	})
	if err != nil {
		return report, err
	}
	ds.compacted(report)
	return report, nil
}

// compacted is what follows a committed compaction that changed a line: the
// change tail's window may hold one, so it is emptied, and the streams read
// again. The archive's next pass reads the mark compactHistory left.
func (ds *dataset) compacted(report CompactReport) {
	if report.From > 0 {
		ds.tail.reset()
		ds.watch.nudge()
	}
}

// withCompactionTx runs fn in a transaction of its own, as a rebuild does: an
// operator's or the pass's work, which appends no entry. With lock it holds
// the changelog lock first, which every writer takes, and refuses a history
// in a newer dialect than this binary's.
func (ds *dataset) withCompactionTx(ctx context.Context, lock bool, fn func(t *txn) error) error {
	if lock {
		// A copy of the spool and its snapshot see no compaction between
		// them (spoolCopy).
		released, err := ds.spoolCopy.hold(ctx)
		if err != nil {
			return err
		}
		defer released()
	}
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	t := &txn{
		ctx: ctx, ds: ds, tx: tx, actor: substrate.ActorSystem, tier: substrate.TierMachine,
		now: nowUTC(), internal: true,
	}
	if lock {
		if err := t.lockKey(changelogLockKey); err != nil {
			return err
		}
		t.seqLocked = true
		if err := t.refuseNewerChangelogDialect(); err != nil {
			return err
		}
	}
	if err := fn(t); err != nil {
		return err
	}
	return tx.Commit()
}
