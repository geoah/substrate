package engine

// BULK CREATION: a run of puts that each create a record no row, tombstone or
// former id names, sent to the database in a few statements instead of a
// dozen per record. The seed writes the shipped units this way at
// registration and at the open of a repository that holds none of them
// (unitseed.go): 365 records through the per-record path cost a registration
// about 1.4 s, nearly all of it round trips.
//
// ONLY THE STATEMENTS ARE BATCHED. Each put runs the ordinary put path
// (putSpec, apply), and each effect it folds goes through fold, foldOne and
// foldRecordOp or applyManager, the code a rebuild replays the entry through,
// so what a record, its unit uses, its refs and its managers become is
// decided by the one fold. Under t.bulk the fold's write leaves (upsertRecord,
// replaceUnitUses, afterUnitFold, syncRefs, applyManager) and appendChange
// queue the statement they would send for a record the run creates, with the
// answer that statement gives for an absent row, and flushBulk sends each
// table's queue as one statement: the created rows, their unit uses, the unit
// graph's revision, their refs (syncRefsOfRows), their managers, and the
// changelog entries with the seqs a per-record append would have taken. Every
// other effect, and a leaf the run cannot batch, runs its statement at once.
//
// A record the run creates is absent until the flush, so loadRow answers nil
// for it in Go, and a second fold of it in the same run is refused rather
// than read as a second creation.
//
// The run takes no per-record advisory lock: inTx holds the repository's
// changelog lock from the transaction's start, and every writer that could
// create, merge or delete one of these records takes it first.

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// bulkWrite is the queue of one bulk run (t.bulk).
type bulkWrite struct {
	// absent are the records the run creates, each known to have no row, no
	// tombstone and no former-id trail: loadRow answers nil for them and
	// checkID reads nothing.
	absent map[eref]bool
	// folded are the records whose creation is queued; a second fold of one
	// is refused.
	folded map[eref]bool

	records  []bulkRecord
	uses     struct{ kinds, ids, units []string }
	revision int
	refs     []bulkRefs
	managers []bulkManager
	entries  []bulkEntry
}

// bulkRecord is the INSERT upsertRecord would have sent for an absent row.
type bulkRecord struct {
	row                              *erow
	fts                              [3]string
	states, props, labels, finalizer []byte
}

// bulkRefs is one row's syncRefs, for syncRefsOfRows.
type bulkRefs struct {
	ty  *vocabulary.Kind
	row *erow
}

// bulkManager is the INSERT applyManager would have sent for a property of
// an absent record, which holds no manager row to conflict with.
type bulkManager struct {
	ref                        eref
	property, actor, tier, who string
}

// bulkEntry is the INSERT appendChange would have sent.
type bulkEntry struct {
	actor    substrate.Actor
	op       substrate.Op
	recordID string
	kind     string
	raw      []byte
	causedBy sql.NullInt64
}

// The stages a bulk run's flush reports to its test seam (WithTestBulkFault),
// once each table's statement has run.
const (
	bulkAfterRecords  = "records"
	bulkAfterUses     = "uses"
	bulkAfterRevision = "revision"
	bulkAfterRefs     = "refs"
	bulkAfterManagers = "managers"
	bulkAfterEntries  = "entries"
)

// bulkEligible reports whether a kind's creating puts may run in bulk: a
// kind whose write touches nothing beyond the record, its refs, its unit
// uses, its managers and its changelog entry. A kind the observed index
// derives from, a metric, a mapping's source or target, a kind with an
// embedded or sealed property, and the kinds apply admits by reading other
// rows are not.
func (t *txn) bulkEligible(ty *vocabulary.Kind) bool {
	if ty == nil || bindsObserved(ty) || bindsMetric(ty) {
		return false
	}
	switch ty.Identity {
	case typeTrigger, typeProvider, vocabulary.KindRecordPatchPolicy, vocabulary.KindRecordPatchRequest, vocabulary.KindLLMInteraction:
		return false
	}
	reg := t.declarations()
	if len(reg.MappingsFrom(ty.Identity)) > 0 || len(reg.MappingsTo(ty.Identity)) > 0 {
		return false
	}
	for _, name := range ty.PropOrder {
		p := ty.Props[name]
		if p.Embed || p.Sensitive() || p.Datatype == vocabulary.DatatypeBlobRef {
			return false
		}
	}
	return true
}

// bulkRefusal says why putBulk declined a run, "" where it ran it. A test
// seam reads it (WithTestSeedPath).
type bulkRefusal string

const (
	bulkRan        bulkRefusal = ""
	bulkNested     bulkRefusal = "a bulk run or an unappended effect is open"
	bulkEmpty      bulkRefusal = "nothing to write"
	bulkIneligible bulkRefusal = "a kind that cannot run in bulk"
	bulkRepeated   bulkRefusal = "an input names a record twice"
	bulkHeld       bulkRefusal = "a row, a tombstone or a former id names an input"
	bulkOff        bulkRefusal = "the bulk run is off"
)

// putBulk puts every input as a creation in one bulk run, or answers why not
// with nothing written where the run cannot take them all: a kind that is
// not eligible, an input that names a record a row, a tombstone or a former
// id already answers, or a transaction with effects not yet appended.
func (t *txn) putBulk(ins []substrate.PutInput) (bulkRefusal, error) {
	switch {
	case t.bulk != nil || len(t.folded) > 0:
		return bulkNested, nil
	case len(ins) == 0:
		return bulkEmpty, nil
	}
	refs := make(map[eref]bool, len(ins))
	var ids, idKinds []string
	for _, in := range ins {
		ty, err := t.resolveType(in.Kind)
		if err != nil || in.ID == "" || !t.bulkEligible(ty) {
			return bulkIneligible, nil
		}
		ref := eref{Kind: ty.Identity, ID: in.ID}
		if refs[ref] {
			return bulkRepeated, nil
		}
		refs[ref] = true
		ids, idKinds = append(ids, in.ID), append(idKinds, ty.Identity)
	}
	var held bool
	if err := t.row(`
		SELECT EXISTS (
			SELECT 1 FROM unnest($1::text[], $2::text[]) AS u(kind, id)
			WHERE EXISTS (SELECT 1 FROM records r WHERE r.kind = u.kind AND r.id = u.id)
			   OR EXISTS (SELECT 1 FROM former_ids f WHERE f.record_kind = u.kind AND f.former_id = u.id))`,
		idKinds, ids).Scan(&held); err != nil {
		return "", fmt.Errorf("substrate/engine: bulk put: %w", err)
	}
	if held {
		return bulkHeld, nil
	}
	t.bulk = &bulkWrite{absent: refs, folded: map[eref]bool{}}
	defer func() { t.bulk = nil }()
	for _, in := range ins {
		if _, err := t.put(in); err != nil {
			return "", err
		}
	}
	if err := t.flushBulk(); err != nil {
		return "", err
	}
	return bulkRan, nil
}

// bulkAbsent reports whether the running bulk run creates ref, which no row
// answers yet.
func (t *txn) bulkAbsent(ref eref) bool {
	return t.bulk != nil && t.bulk.absent[ref]
}

// bulkLoad is loadRow's answer for a record the run creates: nil, the
// absent row, until the run has folded it once. A second fold would read
// nil again and create the row twice, so it is refused.
func (t *txn) bulkLoad(ref eref) (handled bool, err error) {
	if !t.bulkAbsent(ref) {
		return false, nil
	}
	if t.bulk.folded[ref] {
		return true, fmt.Errorf("substrate/engine: a bulk run folds %s %s a second time", ref.Kind, ref.ID)
	}
	return true, nil
}

// queueRecord takes upsertRecord's INSERT of a record the run creates; false
// where the statement must run now: no run, a record the run does not
// create, or a row with an instant column, which the batched INSERT does not
// carry.
func (t *txn) queueRecord(r *erow, fts [3]string, states, props, labels, finalizers []byte) bool {
	ref := eref{Kind: r.Kind, ID: r.ID}
	if !t.bulkAbsent(ref) {
		return false
	}
	t.bulk.folded[ref] = true
	if r.At != nil || r.EndsAt != nil || r.DueAt != nil {
		return false
	}
	t.bulk.records = append(t.bulk.records, bulkRecord{row: r, fts: fts, states: states, props: props, labels: labels, finalizer: finalizers})
	return true
}

// queueUses takes replaceUnitUses' INSERT for a record the run creates.
func (t *txn) queueUses(ref eref, uses []string) bool {
	if !t.bulkAbsent(ref) {
		return false
	}
	u := &t.bulk.uses
	for _, unit := range uses {
		u.kinds, u.ids, u.units = append(u.kinds, ref.Kind), append(u.ids, ref.ID), append(u.units, unit)
	}
	return true
}

// queueRevision takes afterUnitFold's move of the graph's revision: moves
// add, so the run's moves are one statement whatever record made them.
func (t *txn) queueRevision() bool {
	if t.bulk == nil {
		return false
	}
	t.bulk.revision++
	return true
}

// queueRefs takes syncRefs for a record the run creates.
func (t *txn) queueRefs(ref eref, ty *vocabulary.Kind, props map[string]any) bool {
	if !t.bulkAbsent(ref) {
		return false
	}
	t.bulk.refs = append(t.bulk.refs, bulkRefs{ty: ty, row: &erow{ID: ref.ID, Kind: ref.Kind, Props: props}})
	return true
}

// queueManager takes applyManager's INSERT for a record the run creates,
// stamped with the transaction's clock, which is every live write's.
func (t *txn) queueManager(ref eref, property string, actor substrate.Actor, tier substrate.Tier, principal string, at time.Time) bool {
	if actor == "" || !at.Equal(t.now) || !t.bulkAbsent(ref) {
		return false
	}
	t.bulk.managers = append(t.bulk.managers, bulkManager{ref: ref, property: property, actor: string(actor), tier: string(tier), who: principal})
	return true
}

// flushBulk sends a bulk run's queues, one statement per table.
func (t *txn) flushBulk() error {
	b := t.bulk
	for _, step := range []struct {
		stage string
		run   func(*bulkWrite) error
	}{
		{bulkAfterRecords, t.flushBulkRecords},
		{bulkAfterUses, t.flushBulkUses},
		{bulkAfterRevision, t.flushBulkRevision},
		{bulkAfterRefs, t.flushBulkRefs},
		{bulkAfterManagers, t.flushBulkManagers},
		{bulkAfterEntries, t.flushBulkEntries},
	} {
		if err := step.run(b); err != nil {
			return err
		}
		if err := t.ds.svc.bulkFault(step.stage); err != nil {
			return err
		}
	}
	return nil
}

// flushBulkRecords is upsertRecord's INSERT for every queued row, for rows
// no conflict can meet: a row the run believed absent and found is an
// error, never a silent update.
func (t *txn) flushBulkRecords(b *bulkWrite) error {
	if len(b.records) == 0 {
		return nil
	}
	var ids, kinds, titles, bodies, states, props, labels, finalizers, ftsA, ftsB, ftsC []string
	var kindVersions []int64
	for _, r := range b.records {
		row := r.row
		ids, kinds, titles, bodies = append(ids, row.ID), append(kinds, row.Kind), append(titles, row.Title), append(bodies, row.Body)
		states, props, labels, finalizers = append(states, string(r.states)), append(props, string(r.props)), append(labels, string(r.labels)), append(finalizers, string(r.finalizer))
		ftsA, ftsB, ftsC = append(ftsA, r.fts[0]), append(ftsB, r.fts[1]), append(ftsC, r.fts[2])
		kindVersions = append(kindVersions, row.KindVersion)
	}
	if _, err := t.exec(`
		INSERT INTO records (
			id, kind, title, body, states, at, ends_at, due_at, props, labels,
			finalizers, fts, version, kind_version, created_at, updated_at
		)
		SELECT u.id, u.kind, u.title, u.body, u.states::jsonb, NULL, NULL, NULL, u.props::jsonb, u.labels::jsonb,
			ARRAY(SELECT jsonb_array_elements_text(u.finalizers::jsonb)),
			setweight(to_tsvector('english', u.a), 'A') ||
			setweight(to_tsvector('english', u.b), 'B') ||
			setweight(to_tsvector('english', u.c), 'C'),
			1, u.kind_version, $1, $1
		FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[],
			$10::text[], $11::text[], $12::text[], $13::bigint[])
			AS u(id, kind, title, body, states, props, labels, finalizers, a, b, c, kind_version)`,
		t.now, ids, kinds, titles, bodies, states, props, labels, finalizers, ftsA, ftsB, ftsC, kindVersions); err != nil {
		return fmt.Errorf("substrate/engine: bulk put of %d records: %w", len(b.records), err)
	}
	return nil
}

// flushBulkUses is replaceUnitUses' INSERT for every queued row.
func (t *txn) flushBulkUses(b *bulkWrite) error {
	if len(b.uses.units) == 0 {
		return nil
	}
	if _, err := t.exec(`
		INSERT INTO unit_uses (kind, record_id, unit)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[])`, b.uses.kinds, b.uses.ids, b.uses.units); err != nil {
		return fmt.Errorf("substrate/engine: unit uses of a bulk put: %w", err)
	}
	return nil
}

// flushBulkRevision is afterUnitFold's statement once for the run's moves.
func (t *txn) flushBulkRevision(b *bulkWrite) error {
	if b.revision == 0 {
		return nil
	}
	return t.moveUnitRevision(b.revision)
}

// flushBulkRefs is syncRefs for every queued row, through syncRefsOfRows a
// kind at a time.
func (t *txn) flushBulkRefs(b *bulkWrite) error {
	byKind := map[string][]*erow{}
	tys := map[string]*vocabulary.Kind{}
	var order []string
	for _, r := range b.refs {
		kind := r.row.Kind
		if _, seen := byKind[kind]; !seen {
			order = append(order, kind)
			tys[kind] = r.ty
		} else if tys[kind] != r.ty {
			return fmt.Errorf("substrate/engine: a bulk run derives the refs of %s under two declarations", kind)
		}
		byKind[kind] = append(byKind[kind], r.row)
	}
	for _, kind := range order {
		if err := t.syncRefsOfRows(kind, tys[kind], byKind[kind]); err != nil {
			return err
		}
	}
	return nil
}

// flushBulkManagers is applyManager's INSERT for every queued manager.
func (t *txn) flushBulkManagers(b *bulkWrite) error {
	if len(b.managers) == 0 {
		return nil
	}
	var kinds, ids, props, actors, tiers, principals []string
	for _, m := range b.managers {
		kinds, ids, props = append(kinds, m.ref.Kind), append(ids, m.ref.ID), append(props, m.property)
		actors, tiers, principals = append(actors, m.actor), append(tiers, m.tier), append(principals, m.who)
	}
	if _, err := t.exec(`
		INSERT INTO property_managers (record_kind, record_id, property, actor, tier, principal, updated_at)
		SELECT u.kind, u.id, u.property, u.actor, u.tier, u.principal, $1
		FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[])
			AS u(kind, id, property, actor, tier, principal)`,
		t.now, kinds, ids, props, actors, tiers, principals); err != nil {
		return fmt.Errorf("substrate/engine: managers of a bulk put: %w", err)
	}
	return nil
}

// flushBulkEntries is appendChange's INSERT for every queued entry, at the
// seqs one append after another would have taken, each recorded as
// appendChange records it.
func (t *txn) flushBulkEntries(b *bulkWrite) error {
	entries := b.entries
	if len(entries) == 0 {
		return nil
	}
	if err := t.lockChangelog(); err != nil {
		return err
	}
	var actors, ops, ids, kinds, payloads []string
	var caused []int64
	for _, e := range entries {
		actors, ops, ids, kinds = append(actors, string(e.actor)), append(ops, string(e.op)), append(ids, e.recordID), append(kinds, e.kind)
		payloads, caused = append(payloads, string(e.raw)), append(caused, e.causedBy.Int64)
	}
	rows, err := t.query(`
		INSERT INTO changelog (seq, ts, actor, principal, op, record_id, kind, payload, caused_by)
		SELECT (SELECT coalesce(max(seq), 0) FROM changelog) + u.ord, $1, u.actor, $2, u.op, u.id, u.kind, u.payload::jsonb, NULLIF(u.caused, 0)
		FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::bigint[]) WITH ORDINALITY
			AS u(actor, op, id, kind, payload, caused, ord)
		RETURNING seq, payload::text`,
		t.now, t.principal, actors, ops, ids, kinds, payloads, caused)
	if err != nil {
		return fmt.Errorf("substrate/engine: the changelog entries of a bulk put: %w", err)
	}
	type stored struct {
		seq  int64
		text []byte
	}
	var got []stored
	for rows.Next() {
		var s stored
		if err := rows.Scan(&s.seq, &s.text); err != nil {
			_ = rows.Close()
			return err
		}
		got = append(got, s)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(got) != len(entries) {
		return fmt.Errorf("substrate/engine: a bulk put inserted %d changelog entries of %d", len(got), len(entries))
	}
	sort.Slice(got, func(i, j int) bool { return got[i].seq < got[j].seq })
	for i, e := range entries {
		t.appended(e.actor, e.op, e.recordID, e.kind, e.causedBy, got[i].seq, got[i].text)
	}
	return nil
}
