package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
)

// The cross-collection change feed (the console's stream page): newest-first
// history pages plus, per change row, every enabled trigger's delivery
// state. Read-only over the changelog and the trigger bookkeeping — nothing
// here writes.

// hideDeliveryEntries adds the predicate every public read carries: a
// `delivery` entry is the engine's own bookkeeping (delivery.go) and never
// leaves it. A continuation still moves past the hidden rows, because a
// cursor is a seq: a client resuming below a hidden entry reads nothing
// twice and misses nothing, and the head it is held to counts every entry.
// The dispatcher's own read (functions.go changesPast) is the one reader that
// does not carry it, so its scan position covers the entries too.
func hideDeliveryEntries(b *builder) {
	b.add(`op <> ` + b.arg(string(substrate.OpDelivery)))
}

// buildChangeFilter appends a ChangeFilter's predicates; the caller owns the
// seq bound and the ordering.
func (ds *dataset) buildChangeFilter(b *builder, f substrate.ChangeFilter) error {
	resolveTypes := func(names []string) ([]string, error) {
		reg := ds.registry()
		idents := make([]string, 0, len(names))
		for _, name := range names {
			t, err := reg.Resolve(name)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
			}
			idents = append(idents, t.Identity)
		}
		return idents, nil
	}
	if len(f.Kinds) > 0 {
		idents, err := resolveTypes(f.Kinds)
		if err != nil {
			return err
		}
		b.add(`kind IN ` + b.jsonArray(idents))
	}
	if len(f.ExcludeKinds) > 0 {
		idents, err := resolveTypes(f.ExcludeKinds)
		if err != nil {
			return err
		}
		b.add(`kind NOT IN ` + b.jsonArray(idents))
	}
	if len(f.Ops) > 0 {
		ops := make([]string, 0, len(f.Ops))
		for _, o := range f.Ops {
			ops = append(ops, string(o))
		}
		b.add(`op IN ` + b.jsonArray(ops))
	}
	if len(f.ExcludeOps) > 0 {
		ops := make([]string, 0, len(f.ExcludeOps))
		for _, o := range f.ExcludeOps {
			ops = append(ops, string(o))
		}
		b.add(`op NOT IN ` + b.jsonArray(ops))
	}
	if len(f.Actors) > 0 {
		actors := make([]string, 0, len(f.Actors))
		for _, a := range f.Actors {
			actors = append(actors, string(a))
		}
		b.add(`actor IN ` + b.jsonArray(actors))
	}
	if len(f.ExcludeActors) > 0 {
		actors := make([]string, 0, len(f.ExcludeActors))
		for _, a := range f.ExcludeActors {
			actors = append(actors, string(a))
		}
		b.add(`actor NOT IN ` + b.jsonArray(actors))
	}
	if f.RecordID != "" {
		// A merge and a split each write ONE entry that changes two records:
		// the merge addresses the winner and tombstones the loser, the split
		// addresses the loser and rewrites the winner. Their payloads name
		// both (merge.go payloadWinner, payloadLoser), so the record scope
		// matches on those too; otherwise a client following the loser never
		// sees its removal and one following the winner never sees the split.
		// The match is the addressed pair only: the winner's later writes do
		// not follow a former id here, and the entry count is unchanged.
		//
		// Three flat arms, each with an index: changelog_record_idx for the
		// first, the partial changelog_pair_idx for the other two, whose WHERE
		// the op test must repeat as a LITERAL. Bound as a parameter, a generic
		// plan could not prove the partial index applicable and would walk the
		// changelog. The `->>` test itself is never an index condition under
		// row-level security (0001_init says why),
		// so the pair arms scan the repository's merge and split rows.
		id := b.arg(f.RecordID)
		pair := `op IN ('` + string(substrate.OpMerge) + `', '` + string(substrate.OpSplit) + `')`
		b.add(`(record_id = ` + id +
			` OR (` + pair + ` AND payload->>'` + payloadWinner + `' = ` + id + `)` +
			` OR (` + pair + ` AND payload->>'` + payloadLoser + `' = ` + id + `))`)
	}
	if f.Q != "" {
		// One substring over the row's text: metacharacters escaped so the
		// query is always a literal, payload cast to text so a value or a
		// property name both hit. Sequential at personal scale by design.
		// Matching the payload is safe BY CONSTRUCTION of the store: a
		// secret's delta value is an opaque ref and a digest is a one-way
		// comparator, so the searchable bytes are never material.
		p := b.arg("%" + escapeLike(f.Q) + "%")
		b.add(`(kind ILIKE ` + p + ` OR actor ILIKE ` + p +
			` OR record_id ILIKE ` + p + ` OR payload::text ILIKE ` + p + `)`)
	}
	return nil
}

// escapeLike makes a user string a literal LIKE operand.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// queryChanges runs one changelog page over the builder's predicates.
func (ds *dataset) queryChanges(ctx context.Context, b *builder, order string, limit int) ([]substrate.Change, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT seq, ts, actor, op, record_id, kind, payload, hash FROM changelog
		WHERE `+strings.Join(b.where, " AND ")+`
		ORDER BY `+order+` LIMIT `+b.arg(limit), b.args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.Change
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		if c.Op == substrate.OpDelivery {
			// The ledger's own entry (delivery.go) moves no record and has
			// no event: it reaches only the dispatcher's read (functions.go
			// changesPast), and it leaves with neither effects nor `affected`.
			c.Payload = nil
		} else {
			projectAffected(&c)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// projectAffected turns a row's stored replay effects into the public change
// event and takes the effects off the row: `affected` names every record the
// entry moved, with the version each reached and whether it was tombstoned or
// purged, and nothing else of the fold leaves the engine (decision 0061). The
// stored row is untouched: this shapes the READ, and a rebuild reads the
// table's own rows through foldEntry, never through here. Taking the effects
// off is also what keeps a sensitive value out of the feed: the fold carries
// a secret's opaque ref or a digest, and the event carries no value of any
// property.
//
// One element per (kind, id), in first-touch order; a later effect on the same
// record within the entry updates its version and deletion status, so a
// record tombstoned and then purged in one entry reads once, deleted. A purge
// leaves no row and so no version. Annotation, manager, former-id and resync
// effects move no record of their own: the record they hang off is bumped or
// rewritten by an effect beside them, or is the entry's addressed record.
//
// An entry that recorded no record-moving effect (written before the fold
// carried them, or a rejection that moved nothing) names its addressed record
// with no version, deleted when the op is a delete or a collection.
func projectAffected(c *substrate.Change) {
	effects, _ := c.Payload[foldPayloadKey].([]any)
	if _, held := c.Payload[foldPayloadKey]; held {
		delete(c.Payload, foldPayloadKey)
		if len(c.Payload) == 0 {
			c.Payload = nil
		}
	}
	var out []substrate.AffectedRecord
	index := map[eref]int{}
	for _, e := range effects {
		op, ok := e.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := op["kind"].(string)
		switch foldKind(kind) {
		case foldRecord, foldTombstone, foldPurge, foldBump:
		default:
			continue
		}
		ref := eref{Kind: stringOf(op["ref"]), ID: stringOf(op["id"])}
		if ref.Kind == "" || ref.ID == "" {
			continue
		}
		i, seen := index[ref]
		if !seen {
			i = len(out)
			index[ref] = i
			out = append(out, substrate.AffectedRecord{Kind: ref.Kind, ID: ref.ID})
		}
		switch foldKind(kind) {
		case foldRecord:
			out[i].Version, out[i].Deleted = versionOf(op["version"]), false
		case foldTombstone:
			out[i].Version, out[i].Deleted = versionOf(op["version"]), true
		case foldPurge:
			out[i].Version, out[i].Deleted = 0, true
		case foldBump:
			// No live write emits a bump today; the fold keeps the effect
			// replayable (fold.go), and a history that holds one moved the
			// record's version, so the event names it.
			out[i].Version = versionOf(op["version"])
		}
	}
	if len(out) == 0 {
		out = []substrate.AffectedRecord{{
			Kind: c.Kind, ID: c.RecordID,
			Deleted: c.Op == substrate.OpDelete || c.Op == substrate.OpGC,
		}}
	}
	c.Affected = out
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// versionOf reads an effect's stored version. The payload is decoded number
// preserving (scanChange), so the value is a json.Number, spelled `1` by the
// table's jsonb and `1E0` by the segment file's canonical JSON; both parse. A
// float64 is the shape a plain decode would hand over, accepted so the
// projection does not depend on which decoder ran.
func versionOf(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return int64(f)
		}
	case float64:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

// ChangesBefore reads history newest-first: rows with seq < before, at most
// limit of them. before <= 0 means "from the head" — the feed's first page.
func (ds *dataset) ChangesBefore(ctx context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	b := &builder{}
	if before > 0 {
		b.add(`seq < ` + b.arg(before))
	} else {
		b.add(`TRUE`)
	}
	hideDeliveryEntries(b)
	if err := ds.buildChangeFilter(b, f); err != nil {
		return nil, err
	}
	return ds.queryChanges(ctx, b, `seq DESC`, limit)
}

// ChangeTriggers computes, for each given change, every runnable enabled
// trigger's stance on it, keyed by seq. A trigger the row cannot fire —
// source mismatch, the row IS the callable's own write, or a run row — is
// omitted. Parked wins over processed: a parked seq sits behind the cursor
// by construction (park-and-advance), and the failure row is the truer
// answer.
func (ds *dataset) ChangeTriggers(ctx context.Context, changes []substrate.Change) (map[int64][]substrate.ChangeTrigger, error) {
	out := make(map[int64][]substrate.ChangeTrigger, len(changes))
	if len(changes) == 0 {
		return out, nil
	}
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return nil, err
	}
	var live []loadedTrigger
	for _, lt := range triggers {
		if lt.Err == nil && lt.Enabled && lt.Record != nil && lt.runnable() {
			live = append(live, lt)
		}
	}
	if len(live) == 0 {
		return out, nil
	}
	cursors, head, err := ds.cursorSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	parked, err := ds.parkedAt(ctx, changes)
	if err != nil {
		return nil, err
	}
	for _, ch := range changes {
		if ch.Kind == typeTriggerRun {
			continue
		}
		op := runner.OpOf(ch)
		for _, lt := range live {
			if ch.Actor == substrate.Actor(lt.callableActor()) || !lt.Record.matches(ch.Kind, op) {
				continue
			}
			ct := substrate.ChangeTrigger{
				Trigger: lt.ID, Callable: lt.CallableID, State: substrate.ChangeTriggerPending,
			}
			// A trigger with no cursor row has not dispatched yet; it will
			// initialize AT HEAD (ensureCursor), so like TriggerStatuses it
			// reads as a cursor at head — already past every stored row.
			cursor, ok := cursors[lt.ID]
			if !ok {
				cursor = head
			}
			switch lastErr, isParked := parked[parkKey{lt.ID, ch.Seq}]; {
			case isParked:
				ct.State = substrate.ChangeTriggerParked
				ct.Error = lastErr
			case cursor >= ch.Seq:
				ct.State = substrate.ChangeTriggerProcessed
			}
			out[ch.Seq] = append(out[ch.Seq], ct)
		}
	}
	return out, nil
}

// cursorSnapshot reads every trigger cursor plus the changelog head in one
// pass, so a whole page's states come from two queries.
func (ds *dataset) cursorSnapshot(ctx context.Context) (map[string]int64, int64, error) {
	var head int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return nil, 0, err
	}
	rows, err := ds.db.QueryContext(ctx, `SELECT trigger_id, seq FROM trigger_cursors`)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	cursors := map[string]int64{}
	for rows.Next() {
		var id string
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			return nil, 0, err
		}
		cursors[id] = seq
	}
	return cursors, head, rows.Err()
}

type parkKey struct {
	trigger string
	seq     int64
}

// parkedAt loads the failure rows naming any of the given seqs: (trigger,
// seq) → the last error, one query for the batch.
func (ds *dataset) parkedAt(ctx context.Context, changes []substrate.Change) (map[parkKey]string, error) {
	seqs := make([]int64, 0, len(changes))
	for _, ch := range changes {
		seqs = append(seqs, ch.Seq)
	}
	raw, err := json.Marshal(seqs)
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT trigger_id, seq, last_error FROM trigger_failures
		WHERE fire_id = '' AND seq IN (SELECT jsonb_array_elements_text($1::jsonb)::bigint)`, raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[parkKey]string{}
	for rows.Next() {
		var k parkKey
		var lastErr string
		if err := rows.Scan(&k.trigger, &k.seq, &lastErr); err != nil {
			return nil, err
		}
		out[k] = lastErr
	}
	return out, rows.Err()
}
