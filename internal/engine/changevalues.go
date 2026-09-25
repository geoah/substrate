package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Before and after values on a change row (decision 0106).
//
// Nothing here is stored. An entry's effects already carry what each property
// BECAME (fold.go rowDelta), so the "after" side is a projection of the row's
// own effects. The "before" side is the value the record's previous effect on
// that property left, found by walking the record's earlier entries newest
// first: the changelog is the truth and this is a read of it, so the answer
// cannot disagree with the fold the way a stored copy could.
//
// The walk is bounded and honest about it. It stops at the record's creation
// (no value before it), and a before it cannot derive reads BeforeUnknown,
// never a guess: history written before entries carried values, a gap in the
// record's version sequence (an effect on the record rode an entry the walk
// does not find by the record's id), or a record whose previous write lies
// further back than the budget.

const (
	// valuesBatch is how many earlier entries one round reads per record.
	valuesBatch = 64
	// valuesBudget is how many earlier entries a record's walk reads in all
	// before the befores it still owes read unknown.
	valuesBudget = 1024
)

// valueAt is one property's value after an entry: present=false is cleared.
type valueAt struct {
	value   any
	present bool
	// column marks a value the fold moved through a built-in column (the
	// title, a declared body, the three instants) or the machine states: none
	// of them can hold a sensitive datatype, so none is redacted.
	column bool
}

// recordChange is what one entry did to one record, composed across the
// entry's effects on it in order.
type recordChange struct {
	// touched is an effect that moved the record's version (record,
	// tombstone, bump, purge); an entry that only names the record touches
	// nothing.
	touched bool
	created bool
	purged  bool
	// moved maps each public property name the entry changed to what it left.
	moved map[string]valueAt
	// first and last are the versions the record reached at the entry's first
	// and last version-moving effect on it; 0 where the effect predates the
	// stamp (decision 0061).
	first, last int64
}

// composeRecordChange reads what an entry's effects did to ref, in the public
// property names a record read renders (recordOf): the property map, the
// title where the kind does not render it from a template, a declared body,
// the three instants, and the machine states.
func composeRecordChange(ty *vocabulary.Kind, ops []foldOp, ref eref) recordChange {
	rc := recordChange{moved: map[string]valueAt{}}
	for _, op := range ops {
		if op.ref() != ref {
			continue
		}
		switch op.Kind {
		case foldRecord, foldTombstone, foldBump, foldPurge:
		default:
			continue
		}
		rc.touched = true
		if v := versionOf(op.Version); v > 0 {
			if rc.first == 0 {
				rc.first = v
			}
			rc.last = v
		}
		switch op.Kind {
		case foldPurge:
			rc.purged = true
		case foldRecord:
			if op.Delta == nil {
				continue
			}
			if op.Delta.Created {
				rc.created = true
			}
			movedBy(ty, op.Delta, rc.moved)
		}
	}
	return rc
}

// movedBy records a delta's changes under their public names.
func movedBy(ty *vocabulary.Kind, d *rowDelta, into map[string]valueAt) {
	for name, v := range d.Set {
		into[name] = valueAt{value: v, present: true}
	}
	for _, name := range d.Del {
		into[name] = valueAt{}
	}
	column := func(name string, v *string) {
		if v == nil {
			return
		}
		if *v == "" {
			into[name] = valueAt{column: true}
			return
		}
		into[name] = valueAt{value: *v, present: true, column: true}
	}
	// A title the kind renders from its own properties is derived storage
	// (decision 0016): the property it renders from is already in the set.
	if ty.DisplayTemplate == "" {
		column(substrate.PropTitle, d.Title)
	}
	if declaresBody(ty) {
		column(substrate.PropBody, d.Body)
	}
	column(substrate.PropAt, d.At)
	column(substrate.PropEndsAt, d.EndsAt)
	column(substrate.PropDueAt, d.DueAt)
	if d.States != nil {
		for name, state := range *d.States {
			into[name] = valueAt{value: state, present: true, column: true}
		}
	}
}

// render renders one value as a record read does (redactProps): a sensitive
// property's value is the marker, and its empty string stays empty.
//
// It fails closed, because the history outlives the declaration it was written
// under and the current one is all it can consult: a secret renamed, dropped,
// retyped or its kind redeclared still has its sealed ref (or its digest) in
// every earlier entry. So a property value is shown only where the current
// kind declares its name, or declares it as a `renamedFrom`, with a datatype
// that is not sensitive, AND the value does not have the shape of what a
// sensitive datatype stores. Anything else reads as the marker.
func (v valueAt) render(ty *vocabulary.Kind, name string) any {
	if v.column {
		return v.value
	}
	if s, isStr := v.value.(string); isStr && s == "" {
		return ""
	}
	p, ok := currentProp(ty, name)
	if !ok || p.Sensitive() || looksSealed(v.value) {
		return Redacted
	}
	return v.value
}

// currentProp resolves a name the history carries to the property the kind
// declares under it now: the name itself, else the property that names it as
// its `renamedFrom`.
func currentProp(ty *vocabulary.Kind, name string) (*vocabulary.Property, bool) {
	if p, ok := ty.Prop(name); ok {
		return p, true
	}
	for _, p := range ty.Props {
		if p.RenamedFrom == name {
			return p, true
		}
	}
	return nil, false
}

// looksSealed reports a value shaped like what a secret (a sealed ref) or a
// digest (a lowercase hex SHA-256) stores: under a declaration that no longer
// says so, the shape is the one witness left that it once was sensitive.
func looksSealed(v any) bool {
	s, ok := v.(string)
	return ok && (strings.HasPrefix(s, secretRefPrefix) || reDigest.MatchString(s))
}

// valueRequest is one (row, affected record) whose befores the walk owes.
type valueRequest struct {
	seq   int64
	props []*substrate.PropertyChange
}

// recordWalk is one record's walk back through its earlier entries.
type recordWalk struct {
	ref eref
	ty  *vocabulary.Kind
	// requests, newest first; next is the first not yet reached.
	requests []valueRequest
	next     int
	// pending holds the befores owed by reached requests, by property name.
	pending map[string][]*substrate.PropertyChange
	// page holds the page's own rows that touch the record, by seq: an
	// effect can ride an entry addressed to another record, which the
	// record's own walk would not find.
	page map[int64][]foldOp
	// below is where the next round reads under; expect the version the next
	// older effect on the record must have reached, 0 when unknown.
	below  int64
	expect int64
	read   int
	done   bool
}

// deriveValues fills Properties on every affected record of changes whose
// kind is declared and which the entry moved a property of. effects holds each
// row's decoded effects, index-aligned with changes (nil for a row with none).
func (ds *dataset) deriveValues(ctx context.Context, changes []substrate.Change, effects [][]foldOp) error {
	reg := ds.registry()
	walks := map[eref]*recordWalk{}
	var order []*recordWalk
	for i := range changes {
		c := &changes[i]
		for j := range c.Affected {
			a := &c.Affected[j]
			ref := eref{Kind: a.Kind, ID: a.ID}
			ty, ok := reg.ByIdentity(a.Kind)
			if !ok {
				continue
			}
			rc := composeRecordChange(ty, effects[i], ref)
			if len(rc.moved) == 0 {
				continue
			}
			props := make([]substrate.PropertyChange, 0, len(rc.moved))
			for _, name := range sortedKeys(rc.moved) {
				pc := substrate.PropertyChange{Name: name}
				if v := rc.moved[name]; v.present {
					pc.After = v.render(ty, name)
				}
				props = append(props, pc)
			}
			a.Properties = props
			if rc.created {
				// Nothing precedes a creation: every before is absent.
				continue
			}
			w := walks[ref]
			if w == nil {
				w = &recordWalk{ref: ref, ty: ty, pending: map[string][]*substrate.PropertyChange{}, page: map[int64][]foldOp{}}
				walks[ref] = w
				order = append(order, w)
			}
			req := valueRequest{seq: c.Seq}
			for k := range a.Properties {
				req.props = append(req.props, &a.Properties[k])
			}
			w.requests = append(w.requests, req)
		}
	}
	if len(order) == 0 {
		return nil
	}
	for i := range changes {
		for _, w := range order {
			if composeRecordChange(w.ty, effects[i], w.ref).touched {
				w.page[changes[i].Seq] = effects[i]
			}
		}
	}
	for _, w := range order {
		// Newest first, whichever direction the page was read in.
		sort.Slice(w.requests, func(a, b int) bool { return w.requests[a].seq > w.requests[b].seq })
		w.below = w.requests[0].seq + 1
	}
	for {
		var active []*recordWalk
		for _, w := range order {
			if !w.done {
				active = append(active, w)
			}
		}
		if len(active) == 0 {
			return nil
		}
		if err := ds.walkRound(ctx, active); err != nil {
			return err
		}
	}
}

// earlierEntry is one entry a walk read back.
type earlierEntry struct {
	seq      int64
	ops      []foldOp
	opaque   bool
	fromPage bool
}

// walkRound reads the next batch of earlier entries for every active walk in
// one statement and steps each walk through its own.
func (ds *dataset) walkRound(ctx context.Context, active []*recordWalk) error {
	ids := make([]string, len(active))
	below := make([]int64, len(active))
	for i, w := range active {
		ids[i], below[i] = w.ref.ID, w.below
	}
	// The same three arms as the record scope (buildChangeFilter), for the
	// same indexes: the record's own entries, and a merge or split that names
	// it as either side. The pair arms repeat the op test as a literal so the
	// partial changelog_pair_idx applies.
	pair := `c.op IN ('` + string(substrate.OpMerge) + `', '` + string(substrate.OpSplit) + `')`
	rows, err := ds.db.QueryContext(ctx, `
		SELECT r.i, c.seq, c.op, c.record_id, c.kind, c.payload
		FROM unnest($1::text[], $2::bigint[]) WITH ORDINALITY AS r(id, below, i)
		CROSS JOIN LATERAL (
			SELECT seq, op, record_id, kind, payload FROM changelog c
			WHERE c.seq < r.below AND c.op <> $3
			  AND (c.record_id = r.id
			       OR (`+pair+` AND c.payload->>'`+payloadWinner+`' = r.id)
			       OR (`+pair+` AND c.payload->>'`+payloadLoser+`' = r.id))
			ORDER BY c.seq DESC LIMIT $4) c
		ORDER BY r.i, c.seq DESC`,
		ids, below, string(substrate.OpDelivery), valuesBatch)
	if err != nil {
		return err
	}
	read := make([][]earlierEntry, len(active))
	for rows.Next() {
		var (
			i                  int
			seq                int64
			op, recordID, kind string
			raw                []byte
		)
		if err := rows.Scan(&i, &seq, &op, &recordID, &kind, &raw); err != nil {
			_ = rows.Close()
			return err
		}
		w := active[i-1]
		e := earlierEntry{seq: seq}
		payload, err := decodeNumberPreserving(raw)
		if err == nil {
			e.ops, err = foldOpsOf(substrate.Change{Seq: seq, Payload: payload})
		}
		if err != nil {
			e.opaque = true
		}
		// An entry that names properties and carries no effects was written
		// before entries held values: what it set is not in the changelog.
		if _, named := payload["properties"]; named && e.ops == nil && recordID == w.ref.ID && kind == w.ref.Kind {
			e.opaque = true
		}
		read[i-1] = append(read[i-1], e)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i, w := range active {
		w.step(read[i])
	}
	return nil
}

// step walks one batch of a record's earlier entries, newest first, merged
// with the page's own rows in the same span.
func (w *recordWalk) step(batch []earlierEntry) {
	exhausted := len(batch) < valuesBatch
	floor := int64(0)
	if !exhausted {
		floor = batch[len(batch)-1].seq
	}
	w.read += len(batch)
	seen := map[int64]bool{}
	for _, e := range batch {
		seen[e.seq] = true
	}
	for seq, ops := range w.page {
		if seq < w.below && seq >= floor && !seen[seq] {
			batch = append(batch, earlierEntry{seq: seq, ops: ops, fromPage: true})
		}
	}
	sort.Slice(batch, func(a, b int) bool { return batch[a].seq > batch[b].seq })
	for _, e := range batch {
		if w.done {
			return
		}
		w.visit(e)
	}
	w.below = floor
	if exhausted || w.read >= valuesBudget {
		w.giveUp()
	}
}

// visit applies one earlier entry to the walk: its values answer the befores
// newer requests owe, then its own request (if the page holds one) starts
// owing.
func (w *recordWalk) visit(e earlierEntry) {
	if e.opaque {
		w.giveUp()
		return
	}
	rc := composeRecordChange(w.ty, e.ops, w.ref)
	if rc.touched {
		if w.expect > 0 && rc.last > 0 && rc.last != w.expect {
			// A version the walk never saw: an effect on the record rode an
			// entry it did not read, so a value found further back may be stale.
			w.giveUp()
			return
		}
		for name, v := range rc.moved {
			for _, pc := range w.pending[name] {
				if v.present {
					pc.Before = v.render(w.ty, name)
				}
			}
			delete(w.pending, name)
		}
		if rc.created || rc.purged {
			// Nothing of this record precedes a creation, and a purge ends the
			// lifetime a later creation starts over: what is still owed was
			// absent.
			w.pending = map[string][]*substrate.PropertyChange{}
		}
		w.expect = 0
		if rc.first > 1 {
			w.expect = rc.first - 1
		}
	}
	for w.next < len(w.requests) && w.requests[w.next].seq == e.seq {
		for _, pc := range w.requests[w.next].props {
			w.pending[pc.Name] = append(w.pending[pc.Name], pc)
		}
		w.next++
	}
	if rc.created || rc.purged {
		// Anything the walk still owes lies in a lifetime that already ended.
		w.giveUp()
		return
	}
	if len(w.pending) == 0 && w.next == len(w.requests) {
		w.done = true
	}
}

// giveUp marks every before the walk still owes, reached or not, unknown.
func (w *recordWalk) giveUp() {
	for _, pcs := range w.pending {
		for _, pc := range pcs {
			pc.BeforeUnknown = true
		}
	}
	for ; w.next < len(w.requests); w.next++ {
		for _, pc := range w.requests[w.next].props {
			pc.BeforeUnknown = true
		}
	}
	w.pending = nil
	w.done = true
}

// effectsOf decodes a scanned row's effects for deriveValues, before the
// projection takes them off the row.
func effectsOf(c substrate.Change) []foldOp {
	if c.Op == substrate.OpDelivery {
		return nil
	}
	ops, err := foldOpsOf(c)
	if err != nil {
		return nil
	}
	return ops
}
