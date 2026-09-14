package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The window read's engine half (substrate/occurrence.go). Three reads on ONE
// repeatable-read snapshot: the rows after the key, every candidate series,
// and the overrides claiming a slot inside the window. Nothing here expands a
// rule — the engine stays expander-free (decision 0039 and its successor) —
// it hands the API layer the complete inputs a correct page needs, so a
// master and its overrides are never seen in two states.

// seriesPredicate is what makes a row a series: the recurring trait's two ways
// to name occurrences. The partial index records_recurring_idx (migration
// 0002) spells the same predicate, so the candidate read is index-backed.
const seriesPredicate = `(props ? 'recurrence' OR props ? 'rdates')`

// slotExpr is a temporal row's position on the timeline whatever column its
// binding chose: a `temporal(point: dueAt)` kind keeps its slot in due_at and
// leaves `at` null, so the two never both hold a value.
const slotExpr = `COALESCE(at, due_at)`

func (ds *dataset) Window(ctx context.Context, q substrate.WindowQuery) (*substrate.WindowPage, error) {
	if !q.From.Before(q.To) {
		return nil, fmt.Errorf("%w: the window is empty: from %s is not before to %s",
			substrate.ErrValidation, q.From.Format(time.RFC3339), q.To.Format(time.RFC3339))
	}
	first := q.First
	if first <= 0 {
		first = defaultPageSize
	}
	if first > maxPageSize {
		first = maxPageSize
	}
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The base filter is the caller's minus the `at` bound: the rows take the
	// bound on their own slot column below, the series must not, because a
	// series anchored years ago still produces occurrences in this window.
	base := q.Filter
	base.Properties = make(map[string]substrate.Cond, len(q.Filter.Properties))
	for name, c := range q.Filter.Properties {
		if name != substrate.PropAt {
			base.Properties[name] = c
		}
	}
	// One builder per query: a builder numbers its arguments as it renders,
	// and a query handed arguments it never references is a Postgres error.
	rb := &builder{}
	types, err := ds.buildFilter(ctx, tx, rb, base)
	if err != nil {
		return nil, err
	}
	temporal, atKinds, dueKinds, err := ds.temporalKinds(types)
	if err != nil {
		return nil, err
	}
	expand, err := ds.expandProperties(temporal, q.Expand)
	if err != nil {
		return nil, err
	}

	page := &substrate.WindowPage{
		Rows: []*substrate.Record{}, Series: []*substrate.Record{}, Overrides: []*substrate.Record{},
		Generation: ds.historyGeneration(),
	}
	var head sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(seq) FROM changelog`).Scan(&head); err != nil {
		return nil, err
	}
	page.Head = head.Int64

	// --- the rows: plain events and overrides in the window, after the key.
	rb.add(windowBound(rb, atKinds, dueKinds, q.From, q.To))
	rb.add(`NOT ` + seriesPredicate)
	if q.After != nil {
		rb.add(windowSeek(rb, q.After, q.Desc))
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	order := slotExpr + " " + dir + ", kind " + dir + ", id " + dir
	rowsSQL := `SELECT ` + recordCols + ` FROM records WHERE ` + strings.Join(rb.where, " AND ") +
		` ORDER BY ` + order + ` LIMIT ` + rb.arg(first+1)
	got, err := ds.scanRows(ctx, tx, rowsSQL, rb.args)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: window rows: %w", err)
	}
	if len(got) > first {
		page.More = true
		got = got[:first]
	}
	for _, row := range got {
		e, err := ds.hydrate(ctx, tx, row, q.WithAnnotations)
		if err != nil {
			return nil, err
		}
		page.Rows = append(page.Rows, e)
	}

	// --- the series: every candidate, whole. Only a kind that binds the
	// recurring trait can hold one; a temporal kind with a property that
	// merely shares a name is not a series.
	var seriesKinds []string
	for _, t := range temporal {
		if t.Implements(vocabulary.TraitRecurringCore) {
			seriesKinds = append(seriesKinds, t.Identity)
		}
	}
	if len(seriesKinds) > 0 {
		sb := &builder{}
		if _, err := ds.buildFilter(ctx, tx, sb, base); err != nil {
			return nil, err
		}
		sb.add(`kind IN ` + sb.jsonArray(seriesKinds))
		sb.add(seriesPredicate)
		seriesSQL := `SELECT ` + recordCols + ` FROM records WHERE ` + strings.Join(sb.where, " AND ") +
			` ORDER BY kind, id LIMIT ` + sb.arg(substrate.WindowSeriesBudget+1)
		rows, err := ds.scanRows(ctx, tx, seriesSQL, sb.args)
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: window series: %w", err)
		}
		if len(rows) > substrate.WindowSeriesBudget {
			return nil, fmt.Errorf("%w: more than %d series match this window read; narrow the filter",
				substrate.ErrValidation, substrate.WindowSeriesBudget)
		}
		for _, row := range rows {
			e, err := ds.hydrate(ctx, tx, row, false)
			if err != nil {
				return nil, err
			}
			page.Series = append(page.Series, e)
		}
	}

	// --- the overrides: whatever their kind and wherever their own `at`
	// went, a row whose recurrenceOf names one of the series and whose
	// originalAt falls in the window claims that slot. Found through the refs
	// index the way the reverse read is, restricted to kinds that bind the
	// override trait.
	if len(page.Series) > 0 {
		var overrideKinds []string
		for _, t := range ds.registry().Implementing(vocabulary.TraitOverrideCore) {
			overrideKinds = append(overrideKinds, t.Identity)
		}
		if len(overrideKinds) > 0 {
			paths := make([]string, 0, len(page.Series))
			for _, s := range page.Series {
				paths = append(paths, vocabulary.RecordPath(s.Kind, s.ID))
			}
			ob := &builder{}
			ob.add(`deleted_at IS NULL`)
			ob.add(`kind IN ` + ob.jsonArray(overrideKinds))
			ob.add(`EXISTS (SELECT 1 FROM refs r WHERE r.src_kind = records.kind AND r.src = records.id AND r.property = ` +
				ob.arg(vocabulary.PropRecurrenceOf) + ` AND (r.dst_kind || '/' || r.dst) IN ` + ob.jsonArray(paths) + `)`)
			originalAt := `(props->>` + sqlLiteral(vocabulary.PropOriginalAt) + `)::timestamptz`
			ob.add(originalAt + ` >= ` + ob.arg(q.From))
			ob.add(originalAt + ` < ` + ob.arg(q.To))
			overridesSQL := `SELECT ` + recordCols + ` FROM records WHERE ` + strings.Join(ob.where, " AND ") +
				` ORDER BY kind, id`
			rows, err := ds.scanRows(ctx, tx, overridesSQL, ob.args)
			if err != nil {
				return nil, fmt.Errorf("substrate/engine: window overrides: %w", err)
			}
			for _, row := range rows {
				e, err := ds.hydrate(ctx, tx, row, false)
				if err != nil {
					return nil, err
				}
				page.Overrides = append(page.Overrides, e)
			}
		}
	}

	if len(expand) > 0 && len(page.Rows) > 0 {
		if page.Included, err = ds.expandReferents(ctx, tx, expand, page.Rows); err != nil {
			return nil, err
		}
	}
	return page, nil
}

// temporalKinds narrows the filter's kinds to the ones that bind temporal
// (every implementor when the filter named none), and splits them by the
// column their slot occupies. A window over kinds none of which sits on the
// timeline is a validation error, not an empty page that looked complete.
func (ds *dataset) temporalKinds(types []*vocabulary.Kind) (temporal []*vocabulary.Kind, atKinds, dueKinds []string, err error) {
	if len(types) == 0 {
		types = ds.registry().Implementing(vocabulary.TraitTemporalCore)
	}
	for _, t := range types {
		col := slotColumnOf(t)
		switch col {
		case "at":
			atKinds = append(atKinds, t.Identity)
		case "due_at":
			dueKinds = append(dueKinds, t.Identity)
		default:
			continue
		}
		temporal = append(temporal, t)
	}
	if len(temporal) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: a window read needs kinds that bind temporal; none of the kinds in play does",
			substrate.ErrValidation)
	}
	return temporal, atKinds, dueKinds, nil
}

// slotColumnOf reads the row column a kind's temporal slot occupies, or ""
// when the kind binds no temporal trait.
func slotColumnOf(t *vocabulary.Kind) string {
	for _, b := range t.Traits {
		if b.Identity != vocabulary.TraitTemporalCore {
			continue
		}
		switch b.Columns[substrate.PropAt] {
		case substrate.PropDueAt:
			return "due_at"
		default:
			return "at"
		}
	}
	return ""
}

// windowBound renders the half-open [from, to) bound over each kind's own slot
// column, so the two indexes (records_at_idx, records_due_at_idx) each serve
// their half instead of a COALESCE nothing indexes.
func windowBound(b *builder, atKinds, dueKinds []string, from, to any) string {
	var arms []string
	if len(atKinds) > 0 {
		arms = append(arms, `(kind IN `+b.jsonArray(atKinds)+` AND at >= `+b.arg(from)+` AND at < `+b.arg(to)+`)`)
	}
	if len(dueKinds) > 0 {
		arms = append(arms, `(kind IN `+b.jsonArray(dueKinds)+` AND due_at >= `+b.arg(from)+` AND due_at < `+b.arg(to)+`)`)
	}
	return `(` + strings.Join(arms, " OR ") + `)`
}

// windowSeek is the keyset continuation past one (slot, kind, id): strictly
// after it in the walk's direction, so a page never repeats or skips a row.
func windowSeek(b *builder, k *substrate.WindowKey, desc bool) string {
	gt := ">"
	if desc {
		gt = "<"
	}
	at, kind, id := b.arg(k.At), b.arg(k.Kind), b.arg(k.ID)
	return `(` + slotExpr + ` ` + gt + ` ` + at +
		` OR (` + slotExpr + ` = ` + at + ` AND (kind ` + gt + ` ` + kind +
		` OR (kind = ` + kind + ` AND id ` + gt + ` ` + id + `))))`
}

// scanRows runs one recordCols projection and drains it fully before the
// caller hydrates: hydrate issues its own reads on the same snapshot
// connection, and a second query while a cursor is open is "conn busy".
func (ds *dataset) scanRows(ctx context.Context, x dbx, sqlText string, args []any) ([]*erow, error) {
	rows, err := x.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*erow
	for rows.Next() {
		var es recordScan
		if err := rows.Scan(es.dests()...); err != nil {
			return nil, err
		}
		out = append(out, es.finish())
	}
	return out, rows.Err()
}
