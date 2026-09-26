package engine

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Before and after values on a change row (decision 0107).
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
// does not find by the record's id), a previous write further back than
// the request's budget, or one behind a merge or split the pair read's share
// of that budget did not reach and the record's versions cannot rule out.

const (
	// valuesBatch is how many earlier entries one round reads per record.
	valuesBatch = 64
	// valuesBudget is how many earlier entries one request reads in all,
	// shared by every record's walk: a page of many records costs what a
	// page of one does. What is still owed when it runs out reads unknown.
	valuesBudget = 4096
	// valuesPairShare is the part of the budget the merge and split read may
	// spend, a quarter: a record merged and split a thousand times must not
	// leave the walks, which answer every other before, nothing to read.
	valuesPairShare = 4
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
	props []owedBefore
}

// owedBefore is one before a request owes and the name the walk finds it
// under: the property's own name, or for a rename the old name, which is
// where the record held the value before the entry moved it.
type owedBefore struct {
	name string
	pc   *substrate.PropertyChange
}

// renamesOf answers, new name to old, the renames an apply's rewrite of ref
// made in entry c (convert.go convertRecord writes them to the payload, old
// name to new). The rewrite is one entry per record, addressed to it, so only
// the addressed record's renames are read, and only a pair the entry's
// effects moved as a move is kept: the old name cleared and the new one set.
func renamesOf(c *substrate.Change, ref eref, moved map[string]valueAt) map[string]string {
	if c.RecordID != ref.ID || c.Kind != ref.Kind {
		return nil
	}
	var out map[string]string
	pair := func(from, to string) {
		old, oldMoved := moved[from]
		now, newMoved := moved[to]
		if from == "" || to == "" || from == to || !oldMoved || old.present || !newMoved || !now.present {
			return
		}
		if out == nil {
			out = map[string]string{}
		}
		out[to] = from
	}
	switch renamed := c.Payload[payloadRenamed].(type) {
	case map[string]any:
		for from, to := range renamed {
			s, _ := to.(string)
			pair(from, s)
		}
	case map[string]string:
		for from, to := range renamed {
			pair(from, to)
		}
	}
	return out
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
	// known holds, by seq, the entries the walk has without reading them by
	// the record's id: the page's own rows that touch the record (an effect
	// can ride an entry addressed to another record) and every merge or split
	// naming it, which one statement per request reads for every walk.
	known map[int64]earlierEntry
	// below is where the next round reads under; expect the version the next
	// older effect on the record must have reached, 0 when unknown.
	below  int64
	expect int64
	// pairFloor is where the record's merge and split history stops being
	// known: the pair read ran out of its share with a pair naming the record
	// still unread just under it. Under it only an unbroken run of versions
	// says no unread pair touched the record (visit). 0 is the whole history
	// read.
	pairFloor int64
	done      bool
}

func newRecordWalk(ref eref, ty *vocabulary.Kind) *recordWalk {
	return &recordWalk{ref: ref, ty: ty, pending: map[string][]*substrate.PropertyChange{}, known: map[int64]earlierEntry{}}
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
			// A rename is one change under the new name: the old name's
			// clear is the same move, not a removal of its own.
			renamed := renamesOf(c, ref, rc.moved)
			var gone map[string]bool
			if renamed != nil {
				gone = make(map[string]bool, len(renamed))
				for _, from := range renamed {
					gone[from] = true
				}
			}
			props := make([]substrate.PropertyChange, 0, len(rc.moved))
			for _, name := range sortedKeys(rc.moved) {
				if gone[name] {
					continue
				}
				pc := substrate.PropertyChange{Name: name, RenamedFrom: renamed[name]}
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
				w = newRecordWalk(ref, ty)
				walks[ref] = w
				order = append(order, w)
			}
			req := valueRequest{seq: c.Seq}
			for k := range a.Properties {
				pc := &a.Properties[k]
				name := pc.Name
				if pc.RenamedFrom != "" {
					name = pc.RenamedFrom
				}
				req.props = append(req.props, owedBefore{name: name, pc: pc})
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
				w.known[changes[i].Seq] = earlierEntry{seq: changes[i].Seq, ops: effects[i], fromPage: true}
			}
		}
	}
	top := int64(0)
	for _, w := range order {
		// Newest first, whichever direction the page was read in.
		sort.Slice(w.requests, func(a, b int) bool { return w.requests[a].seq > w.requests[b].seq })
		w.below = w.requests[0].seq + 1
		top = max(top, w.below)
	}
	budget := ds.svc.valuesBudget
	spent, err := ds.readPairs(ctx, order, top, max(1, budget/valuesPairShare))
	if err != nil {
		return err
	}
	return runWalks(order, budget-spent, func(active []*recordWalk, limit int) ([][]earlierEntry, error) {
		return ds.walkRound(ctx, active, limit)
	})
}

// runWalks steps every walk through the rounds read returns until each is
// done or the budget, shared by all of them, is spent.
func runWalks(order []*recordWalk, budget int, read func(active []*recordWalk, limit int) ([][]earlierEntry, error)) error {
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
		// Each active walk reads its share of what is left, never more than a
		// batch; a share under one entry is a spent budget.
		limit := min(valuesBatch, budget/len(active))
		if limit < 1 {
			for _, w := range active {
				w.giveUp()
			}
			return nil
		}
		batches, err := read(active, limit)
		if err != nil {
			return err
		}
		for i, w := range active {
			budget -= len(batches[i])
			w.step(batches[i], limit)
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

// earlierOf decodes one changelog row a walk read back.
func (w *recordWalk) earlierOf(seq int64, recordID, kind string, raw []byte) earlierEntry {
	e := earlierEntry{seq: seq}
	payload, err := decodeNumberPreserving(raw)
	if err == nil {
		e.ops, err = foldOpsOf(substrate.Change{Seq: seq, Payload: payload})
	}
	if err != nil {
		// Only the record's own kind makes an undecodable entry opaque. The
		// record arm matches by id alone, so another kind's record under the
		// same id reaches the walk too; an effect of that entry on this
		// record would show as a gap in its versions, which visit refuses.
		e.ops = nil
		e.opaque = kind == w.ref.Kind
		return e
	}
	// An entry that names properties and carries no effects was written
	// before entries held values: what it set is not in the changelog.
	if _, named := payload["properties"]; named && e.ops == nil && recordID == w.ref.ID && kind == w.ref.Kind {
		e.opaque = true
	}
	return e
}

// pairOp is the op test the pair reads spell as a literal, so the partial
// changelog_pair_idx applies: bound as a parameter, a generic plan could not
// prove the partial index applicable and would walk the changelog.
var pairOp = `op IN ('` + string(substrate.OpMerge) + `', '` + string(substrate.OpSplit) + `')`

// readPairs gives each walk the merges and splits under top that name its
// record as either side, newest first and at most limit of them, in one
// statement for the whole request. The `->>` test is never an index condition
// under row-level security (0001_init says why), so this scans the
// repository's merge and split rows through the partial changelog_pair_idx:
// once per request, where an arm in every walk's round would scan them once
// per record per round. It returns how many rows it read, which the request's
// budget pays for.
//
// A read that reached its limit sets each walk's pairFloor from the newest
// pair it left unread that names the walk's record, so only a before that an
// unread pair could have moved reads unknown; a walk no unread pair names goes
// on whole.
func (ds *dataset) readPairs(ctx context.Context, order []*recordWalk, top int64, limit int) (int, error) {
	byID := map[string][]*recordWalk{}
	ids := make([]string, 0, len(order))
	for _, w := range order {
		if byID[w.ref.ID] == nil {
			ids = append(ids, w.ref.ID)
		}
		byID[w.ref.ID] = append(byID[w.ref.ID], w)
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT seq, record_id, kind, payload, payload->>'`+payloadWinner+`', payload->>'`+payloadLoser+`'
		FROM changelog
		WHERE `+pairOp+` AND seq < $2
		  AND (payload->>'`+payloadWinner+`' = ANY($1) OR payload->>'`+payloadLoser+`' = ANY($1))
		ORDER BY seq DESC LIMIT $3`,
		ids, top, limit)
	if err != nil {
		return 0, err
	}
	n, oldest := 0, int64(0)
	for rows.Next() {
		var (
			seq            int64
			recordID, kind string
			raw            []byte
			winner, loser  sql.NullString
		)
		if err := rows.Scan(&seq, &recordID, &kind, &raw, &winner, &loser); err != nil {
			_ = rows.Close()
			return 0, err
		}
		n, oldest = n+1, seq
		for _, id := range []sql.NullString{winner, loser} {
			if !id.Valid {
				continue
			}
			for _, w := range byID[id.String] {
				if _, held := w.known[seq]; !held {
					w.known[seq] = w.earlierOf(seq, recordID, kind, raw)
				}
			}
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if n < limit {
		return n, nil
	}
	return n, ds.markUnreadPairs(ctx, byID, ids, oldest)
}

// markUnreadPairs sets pairFloor on every walk whose record a merge or split
// under below names: the pairs the limited read left. It reads one seq per
// record and no payload, so its answer is bounded by the page, not the
// history.
func (ds *dataset) markUnreadPairs(ctx context.Context, byID map[string][]*recordWalk, ids []string, below int64) error {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, max(seq) FROM (
			SELECT seq, unnest(ARRAY[payload->>'`+payloadWinner+`', payload->>'`+payloadLoser+`']) AS id
			FROM changelog
			WHERE `+pairOp+` AND seq < $2
			  AND (payload->>'`+payloadWinner+`' = ANY($1) OR payload->>'`+payloadLoser+`' = ANY($1))
		) p
		WHERE id = ANY($1)
		GROUP BY id`,
		ids, below)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			id  string
			seq int64
		)
		if err := rows.Scan(&id, &seq); err != nil {
			_ = rows.Close()
			return err
		}
		for _, w := range byID[id] {
			w.pairFloor = seq + 1
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return rows.Err()
}

// walkRound reads the next batch of at most limit earlier entries for every
// active walk in one statement, from the record's own entries, and returns
// them index-aligned with active. changelog_record_seq_idx leads with the
// record and ends with seq, so each walk's batch is a backward index range,
// never a sort of the record's whole history.
func (ds *dataset) walkRound(ctx context.Context, active []*recordWalk, limit int) ([][]earlierEntry, error) {
	ids := make([]string, len(active))
	below := make([]int64, len(active))
	for i, w := range active {
		ids[i], below[i] = w.ref.ID, w.below
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT r.i, c.seq, c.record_id, c.kind, c.payload
		FROM unnest($1::text[], $2::bigint[]) WITH ORDINALITY AS r(id, below, i)
		CROSS JOIN LATERAL (
			SELECT seq, record_id, kind, payload FROM changelog c
			WHERE c.record_id = r.id AND c.seq < r.below AND c.op <> $3
			ORDER BY c.seq DESC LIMIT $4) c
		ORDER BY r.i, c.seq DESC`,
		ids, below, string(substrate.OpDelivery), limit)
	if err != nil {
		return nil, err
	}
	read := make([][]earlierEntry, len(active))
	for rows.Next() {
		var (
			i              int
			seq            int64
			recordID, kind string
			raw            []byte
		)
		if err := rows.Scan(&i, &seq, &recordID, &kind, &raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		w := active[i-1]
		read[i-1] = append(read[i-1], w.earlierOf(seq, recordID, kind, raw))
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return read, rows.Err()
}

// step walks one batch of a record's earlier entries, newest first, merged
// with the entries it already holds in the same span. A batch shorter than
// the limit it was read under is the end of the record's own entries.
func (w *recordWalk) step(batch []earlierEntry, limit int) {
	exhausted := len(batch) < limit
	floor := int64(0)
	if !exhausted {
		floor = batch[len(batch)-1].seq
	}
	seen := map[int64]bool{}
	for _, e := range batch {
		seen[e.seq] = true
	}
	for seq, e := range w.known {
		if seq < w.below && seq >= floor && !seen[seq] {
			batch = append(batch, e)
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
	if exhausted {
		w.giveUp()
	}
}

// visit applies one earlier entry to the walk: its values answer the befores
// newer requests owe, then its own request (if the page holds one) starts
// owing.
func (w *recordWalk) visit(e earlierEntry) {
	var rc recordChange
	if e.opaque {
		// What the entry did is unknowable, and so is every before still
		// owed. A request older than it owes a before that lies further back,
		// so the walk goes on for those, with no version to hold the next
		// entry to.
		w.forget()
		w.expect = 0
	} else {
		rc = composeRecordChange(w.ty, e.ops, w.ref)
	}
	if rc.touched {
		gap := w.expect > 0 && rc.last > 0 && rc.last != w.expect
		// Under the pair floor an unread merge or split may have touched the
		// record, and only the version this entry reached, meeting the one the
		// walk expects, says none did.
		blind := e.seq < w.pairFloor && (w.expect == 0 || rc.last == 0)
		if gap || blind {
			// A version the walk never saw: an effect on the record rode an
			// entry it did not read, newer than this one, so a value found
			// from here back may be stale for what is owed now. A request at
			// or below this entry is older than that effect and still
			// derivable.
			w.forget()
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
		for _, o := range w.requests[w.next].props {
			w.pending[o.name] = append(w.pending[o.name], o.pc)
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

// forget marks the befores the reached requests owe unknown; the requests
// not yet reached keep theirs owed.
func (w *recordWalk) forget() {
	for _, pcs := range w.pending {
		for _, pc := range pcs {
			pc.BeforeUnknown = true
		}
	}
	w.pending = map[string][]*substrate.PropertyChange{}
}

// giveUp marks every before the walk still owes, reached or not, unknown.
func (w *recordWalk) giveUp() {
	for _, pcs := range w.pending {
		for _, pc := range pcs {
			pc.BeforeUnknown = true
		}
	}
	for ; w.next < len(w.requests); w.next++ {
		for _, o := range w.requests[w.next].props {
			o.pc.BeforeUnknown = true
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
