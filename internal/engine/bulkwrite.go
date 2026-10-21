package engine

// BULK CREATION: a run of puts that each create a record no row, tombstone or
// former id names, sent to the database in a few statements instead of a
// dozen per record. The seed writes the shipped units this way at
// registration and at the open of a repository that holds none of them
// (unitseed.go): 365 records through the per-record path cost a registration
// about 1.4 s, nearly all of it round trips.
//
// Nothing about what is written changes. Each put runs the ordinary put path
// (putSpec, apply), so its coercion, checks, title, managers and changelog
// payload are the ones a single put makes, and its effects go through fold
// and appendChange as always. Under t.bulk those two, and the reads the path
// makes of a record known absent, are answered in Go and queued; flushBulk
// then applies the queued effects as foldRecordOp and appendChange would
// have, one statement per table: the created rows, their refs and unit uses,
// their managers, the unit graph's revision, and the changelog entries with
// the seqs a per-record append would have taken. A rebuild replays those
// entries through foldOne one effect at a time and reaches the same tables
// (TestBulkSeedMatchesThePerRecordPath).
//
// The run takes no per-record advisory lock: inTx holds the repository's
// changelog lock from the transaction's start, and every writer that could
// create, merge or delete one of these records takes it first.

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// bulkWrite is the queue of one bulk run (t.bulk).
type bulkWrite struct {
	// absent are the records the run creates, each known to have no row, no
	// tombstone and no former-id trail: loadRow answers nil for them and
	// checkID reads nothing.
	absent map[eref]bool
	rows   []bulkRow
	// managers are the run's manager effects, in fold order.
	managers []foldOp
	entries  []bulkEntry
}

// bulkRow is one created row as foldRecordOp would have written it.
type bulkRow struct {
	ref eref
	ty  *vocabulary.Kind
	row *erow
	fts [3]string
}

// bulkEntry is one changelog entry as appendChange would have inserted it.
type bulkEntry struct {
	actor    substrate.Actor
	op       substrate.Op
	recordID string
	kind     string
	raw      []byte
	causedBy int64
}

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

// putBulk puts every input as a creation in one bulk run, or answers false
// with nothing written where the run cannot take them all: a kind that is
// not eligible, an input that names a record a row, a tombstone or a
// former id already answers, or a transaction with effects not yet appended.
func (t *txn) putBulk(ins []substrate.PutInput) (bool, error) {
	if t.bulk != nil || len(t.folded) > 0 || len(ins) == 0 {
		return false, nil
	}
	kinds := map[string]bool{}
	refs := make(map[eref]bool, len(ins))
	var ids, idKinds []string
	for _, in := range ins {
		ty, err := t.resolveType(in.Kind)
		if err != nil || in.ID == "" || !t.bulkEligible(ty) {
			return false, nil
		}
		ref := eref{Kind: ty.Identity, ID: in.ID}
		if refs[ref] {
			return false, nil
		}
		refs[ref] = true
		kinds[ty.Identity] = true
		ids, idKinds = append(ids, in.ID), append(idKinds, ty.Identity)
	}
	var held bool
	if err := t.row(`
		SELECT EXISTS (
			SELECT 1 FROM unnest($1::text[], $2::text[]) AS u(kind, id)
			WHERE EXISTS (SELECT 1 FROM records r WHERE r.kind = u.kind AND r.id = u.id)
			   OR EXISTS (SELECT 1 FROM former_ids f WHERE f.record_kind = u.kind AND f.former_id = u.id))`,
		idKinds, ids).Scan(&held); err != nil {
		return false, fmt.Errorf("substrate/engine: bulk put: %w", err)
	}
	if held {
		return false, nil
	}
	t.bulk = &bulkWrite{absent: refs}
	defer func() { t.bulk = nil }()
	for _, in := range ins {
		if _, err := t.put(in); err != nil {
			return false, err
		}
	}
	if err := t.flushBulk(); err != nil {
		return false, err
	}
	return true, nil
}

// bulkAbsent reports whether the running bulk run creates ref, which no row
// answers yet.
func (t *txn) bulkAbsent(ref eref) bool {
	return t.bulk != nil && t.bulk.absent[ref]
}

// foldBulk is fold's half of a bulk run: a creating record effect is applied
// to a fresh row in Go, as foldRecordOp applies it to the row it loads, and
// a manager effect on a record the run creates is queued. It answers false
// for any other effect, which the fold applies as always.
func (t *txn) foldBulk(op foldOp) (foldResult, bool, error) {
	ref := op.ref()
	if !t.bulkAbsent(ref) {
		return foldResult{}, false, nil
	}
	switch {
	case op.Kind == foldRecord && op.Delta != nil && op.Delta.Created:
		if err := t.beforeUnitFold(ref.Kind); err != nil {
			return foldResult{}, true, err
		}
		row := &erow{
			ID: ref.ID, Kind: ref.Kind,
			States: map[string]string{}, Props: map[string]any{}, Labels: map[string]any{},
			CreatedAt: t.now, UpdatedAt: t.now,
		}
		op.Delta.applyTo(row)
		row.Version = 1
		ty, _ := t.derivationView().ByIdentity(ref.Kind)
		t.bulk.rows = append(t.bulk.rows, bulkRow{ref: ref, ty: ty, row: row, fts: t.foldFTS(row)})
		return foldResult{changed: true, created: true, row: row, version: 1}, true, nil
	case op.Kind == foldManager && op.Actor != "":
		t.bulk.managers = append(t.bulk.managers, op)
		return foldResult{changed: true}, true, nil
	}
	return foldResult{}, false, nil
}

// appendBulk is appendChange's half of a bulk run: the entry is built as
// appendChange builds it and queued, and flushBulk inserts it.
func (t *txn) appendBulk(actor substrate.Actor, op substrate.Op, recordID, typ string, payload map[string]any) error {
	if len(t.folded) > 0 {
		if payload == nil {
			payload = map[string]any{}
		}
		payload[foldPayloadKey] = t.folded
		t.folded = nil
	}
	raw, err := jsonb(payload)
	if err != nil {
		return err
	}
	t.bulk.entries = append(t.bulk.entries, bulkEntry{actor: actor, op: op, recordID: recordID, kind: typ, raw: raw, causedBy: t.causedBy})
	return nil
}

// flushBulk applies a bulk run's queue: what foldRecordOp, the manager fold
// and appendChange would have written record by record, one statement per
// table.
func (t *txn) flushBulk() error {
	b := t.bulk
	if err := t.flushBulkRows(b.rows); err != nil {
		return err
	}
	if err := t.flushBulkManagers(b.managers); err != nil {
		return err
	}
	return t.flushBulkEntries(b.entries)
}

func (t *txn) flushBulkRows(rows []bulkRow) error {
	if len(rows) == 0 {
		return nil
	}
	var ids, kinds, titles, bodies, states, props, labels, finalizers, ftsA, ftsB, ftsC []string
	var kindVersions []int64
	var useKinds, useIDs, useUnits []string
	unitFolds := 0
	byKind := map[string][]*erow{}
	tys := map[string]*vocabulary.Kind{}
	var order []string
	for _, r := range rows {
		row := r.row
		if row.At != nil || row.EndsAt != nil || row.DueAt != nil {
			return fmt.Errorf("substrate/engine: bulk put of %s %s: a row with an instant column is no bulk row", r.ref.Kind, r.ref.ID)
		}
		for _, v := range []struct {
			dst *[]string
			src any
		}{{&states, row.States}, {&props, row.Props}, {&labels, row.Labels}, {&finalizers, nonNilStrings(row.Finalizers)}} {
			raw, err := jsonb(v.src)
			if err != nil {
				return err
			}
			*v.dst = append(*v.dst, string(raw))
		}
		ids, kinds, titles, bodies = append(ids, row.ID), append(kinds, row.Kind), append(titles, row.Title), append(bodies, row.Body)
		ftsA, ftsB, ftsC = append(ftsA, r.fts[0]), append(ftsB, r.fts[1]), append(ftsC, r.fts[2])
		kindVersions = append(kindVersions, row.KindVersion)
		for _, u := range rowUnitUses(r.ty, row.Props) {
			useKinds, useIDs, useUnits = append(useKinds, row.Kind), append(useIDs, row.ID), append(useUnits, u)
		}
		if isUnitGraphKind(row.Kind) {
			unitFolds++
		}
		t.noteMetricRules(r.ty, r.ref, row)
		if r.ty == nil || declaresReference(r.ty) {
			if _, seen := byKind[row.Kind]; !seen {
				order = append(order, row.Kind)
			}
			byKind[row.Kind] = append(byKind[row.Kind], row)
			tys[row.Kind] = r.ty
		}
	}
	// upsertRecord's INSERT, for rows no conflict can meet: a row the run
	// believed absent and found is an error, never a silent update.
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
		return fmt.Errorf("substrate/engine: bulk put of %d records: %w", len(rows), err)
	}
	if len(useUnits) > 0 {
		if _, err := t.exec(`
			INSERT INTO unit_uses (kind, record_id, unit)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[])`, useKinds, useIDs, useUnits); err != nil {
			return fmt.Errorf("substrate/engine: unit uses of %d records: %w", len(rows), err)
		}
	}
	if unitFolds > 0 {
		// afterUnitFold once per row, in one statement: the revision moves by
		// one for every unit or conversion row the run created.
		t.unitTouched = true
		t.unitLocal = nil
		if _, err := t.exec(`
			INSERT INTO unit_graph (revision) VALUES ($1)
			ON CONFLICT (repository) DO UPDATE SET revision = unit_graph.revision + $1`, unitFolds); err != nil {
			return fmt.Errorf("substrate/engine: move the unit graph revision: %w", err)
		}
	}
	for _, kind := range order {
		if err := t.syncRefsOfRows(kind, tys[kind], byKind[kind]); err != nil {
			return err
		}
	}
	return nil
}

func (t *txn) flushBulkManagers(ops []foldOp) error {
	if len(ops) == 0 {
		return nil
	}
	var kinds, ids, props, actors, tiers, principals []string
	for _, op := range ops {
		if op.UpdatedAt != nil {
			return fmt.Errorf("substrate/engine: bulk manager of %s %s carries its own time", op.Ref, op.ID)
		}
		kinds, ids, props = append(kinds, op.Ref), append(ids, op.ID), append(props, op.Property)
		actors, tiers, principals = append(actors, op.Actor), append(tiers, op.Tier), append(principals, op.Principal)
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

// flushBulkEntries inserts the run's entries at the seqs appendChange would
// have given them, one after another from the head, and records each as
// appendChange does.
func (t *txn) flushBulkEntries(entries []bulkEntry) error {
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
		payloads, caused = append(payloads, string(e.raw)), append(caused, e.causedBy)
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
		seq := got[i].seq
		if seq > t.maxSeq {
			t.maxSeq = seq
		}
		causedBy := sql.NullInt64{Int64: e.causedBy, Valid: e.causedBy > 0}
		if e.op != substrate.OpDelivery {
			t.entries = append(t.entries, changeEntry{seq: seq, op: e.op, kind: e.kind, id: e.recordID})
		}
		t.pending = append(t.pending, pendingEntry{
			Seq: seq, TS: t.now, Actor: string(e.actor), Principal: t.principal,
			Op: string(e.op), RecordID: e.recordID, Kind: e.kind,
			CausedBy: causedBy.Int64, CausedByOK: causedBy.Valid,
			PayloadText: got[i].text,
		})
	}
	return nil
}
