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
		b.add(`record_id = ` + b.arg(f.RecordID))
	}
	if f.Q != "" {
		// One substring over the row's text: metacharacters escaped so the
		// query is always a literal, payload cast to text so a value or a
		// property name both hit. Sequential at personal scale by design.
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
		SELECT seq, ts, actor, op, record_id, kind, payload FROM changelog
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
		out = append(out, c)
	}
	return out, rows.Err()
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
		if ch.Kind == typeRun {
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
