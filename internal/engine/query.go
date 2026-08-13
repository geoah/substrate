package engine

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// dbx is the subset of database/sql both *sql.DB and *sql.Tx satisfy.
type dbx interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

const defaultPageSize = 50

const maxPageSize = 500

// Get reads one record by its full (type, id) identity. A former id — the id
// of a record a merge fused away — resolves to the canonical record WITHIN
// THE TYPE, which says so through CanonicalID (MODEL §4.1). The tombstone
// itself is still readable through a deleted filter; what an id must never do
// is silently name a record the graph has moved past.
func (ds *dataset) Get(ctx context.Context, typ, id string) (*substrate.Record, error) {
	ty, err := ds.resolveType(typ)
	if err != nil {
		return nil, err
	}
	canonical, err := ds.canonicalOf(ctx, ds.db, eref{Kind: ty.Identity, ID: id})
	if err != nil {
		return nil, err
	}
	row, err := scanRecord(ds.db.QueryRowContext(ctx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = $2`, canonical.Kind, canonical.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: record %s", substrate.ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	e, err := ds.hydrate(ctx, ds.db, row, true, true)
	if err != nil {
		return nil, err
	}
	// Single-record reads carry property provenance. Reverse
	// edges are a separate paged read: they are derived graph state, and an
	// record can have an unbounded number of them.
	if e.PropertyMeta, err = ds.propertyMeta(ctx, e); err != nil {
		return nil, err
	}
	if canonical.ID != id {
		e.CanonicalID = canonical.ID
	}
	return e, nil
}

// Incoming reads a page of reverse edges: every edge whose dst is
// this record, joined to its live source. A tombstoned source still holds its
// edge row until GC, but a deleted record no longer points at anything.
func (ds *dataset) Incoming(ctx context.Context, typ, id string, first int, after string) (*substrate.IncomingPage, error) {
	ty, err := ds.resolveType(typ)
	if err != nil {
		return nil, err
	}
	canonical, err := ds.canonicalOf(ctx, ds.db, eref{Kind: ty.Identity, ID: id})
	if err != nil {
		return nil, err
	}
	if first <= 0 {
		first = defaultPageSize
	}
	if first > maxPageSize {
		first = maxPageSize
	}
	var total int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM edges e JOIN records s ON s.kind = e.src_kind AND s.id = e.src
		WHERE e.dst_kind = $1 AND e.dst = $2 AND s.deleted_at IS NULL`,
		canonical.Kind, canonical.ID).Scan(&total); err != nil {
		return nil, err
	}
	// KEYSET continuation: the reverse-edge order is the fixed,
	// non-null triple (rel, source type, source id), so the `after` token
	// carries those three values and the next page seeks strictly past them
	// with a row-value comparison — no OFFSET, stable under concurrent writes.
	b := &builder{}
	dst := `e.dst_kind = ` + b.arg(canonical.Kind) + ` AND e.dst = ` + b.arg(canonical.ID)
	seek := ""
	if after != "" {
		tok, err := decodeKeyset(after)
		if err != nil {
			return nil, err
		}
		if tok.O != incomingOrder || len(tok.K) != 3 {
			return nil, fmt.Errorf("%w: bad cursor", substrate.ErrValidation)
		}
		if tok.K[0] == nil || tok.K[1] == nil || tok.K[2] == nil {
			return nil, fmt.Errorf("%w: bad cursor", substrate.ErrValidation)
		}
		seek = ` AND (e.rel, s.kind, s.id) > (` +
			b.arg(*tok.K[0]) + `, ` + b.arg(*tok.K[1]) + `, ` + b.arg(*tok.K[2]) + `)`
	}
	limitArg := b.arg(first + 1)
	rows, err := ds.db.QueryContext(ctx, `
		SELECT e.rel, s.id, s.kind, s.title
		FROM edges e JOIN records s ON s.kind = e.src_kind AND s.id = e.src
		WHERE `+dst+` AND s.deleted_at IS NULL`+seek+`
		ORDER BY e.rel, s.kind, s.id
		LIMIT `+limitArg, b.args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	page := &substrate.IncomingPage{Incoming: []substrate.IncomingEdge{}, Total: total}
	for rows.Next() {
		var in substrate.IncomingEdge
		if err := rows.Scan(&in.Rel, &in.From.ID, &in.From.Kind, &in.From.Title); err != nil {
			return nil, err
		}
		if len(page.Incoming) == first {
			last := page.Incoming[first-1]
			rel, sty, sid := last.Rel, last.From.Kind, last.From.ID
			page.Cursor = encodeKeyset(incomingOrder, []*string{&rel, &sty, &sid}, 0)
			break
		}
		page.Incoming = append(page.Incoming, in)
	}
	return page, rows.Err()
}

// incomingOrder is the fixed order signature stamped into an Incoming cursor,
// so a list cursor cannot be replayed against the reverse-edge reader.
const incomingOrder = "incoming:rel,srcType,srcId"

// propertyMeta assembles one record's per-property provenance: the manager
// ledger — actor AND tier, so a read can tell a bundle pin from an
// owner's or from a machine row recompute may replace — and the live
// mapping-projection offers whose value differs (JSON equality) from the
// stored one, the ALTERNATIVES the console shows beside a held value.
func (ds *dataset) propertyMeta(ctx context.Context, e *substrate.Record) (map[string]substrate.PropertyMeta, error) {
	out := map[string]substrate.PropertyMeta{}
	rows, err := ds.db.QueryContext(ctx,
		`SELECT property, actor, tier, updated_at FROM property_managers WHERE record_kind = $1 AND record_id = $2`,
		e.Kind, e.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m substrate.PropertyMeta
		var property string
		if err := rows.Scan(&property, &m.Manager, &m.Tier, &m.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		m.UpdatedAt = m.UpdatedAt.UTC()
		out[property] = m
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	rows, err = ds.db.QueryContext(ctx, `
		SELECT property, actor, value, updated_at FROM property_offers
		WHERE record_kind = $1 AND record_id = $2 ORDER BY property, actor`, e.Kind, e.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var property, actor string
		var raw []byte
		var at time.Time
		if err := rows.Scan(&property, &actor, &raw, &at); err != nil {
			return nil, err
		}
		var value any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &value)
		}
		// An offer's stored value bypasses recordOf, so a property that is
		// sensitive TODAY redacts here too: a stale offer minted before a
		// re-type must not hand out what the main value hides. Unresolvable
		// kinds fail closed the same way.
		if ty, err := ds.resolveType(e.Kind); err != nil || ty == nil {
			value = Redacted
		} else if p, ok := ty.Prop(property); ok && p.Sensitive() {
			value = Redacted
		}
		if jsonEqual(value, e.Properties[property]) {
			continue
		}
		m := out[property]
		m.Alternatives = append(m.Alternatives, substrate.PropertyAlternative{
			Actor: actor, Value: value, UpdatedAt: at.UTC(),
		})
		out[property] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, rows.Err()
}

// record is the in-transaction projection every mutation returns.
func (t *txn) record(row *erow, ty *vocabulary.Kind) (*substrate.Record, error) {
	return t.ds.hydrateWithType(t.ctx, t.tx, row, ty, true, true)
}

func (ds *dataset) hydrate(ctx context.Context, x dbx, row *erow, withEdges, withAnnotations bool) (*substrate.Record, error) {
	ty, _ := ds.registry().ByIdentity(row.Kind)
	return ds.hydrateWithType(ctx, x, row, ty, withEdges, withAnnotations)
}

func (ds *dataset) hydrateWithType(ctx context.Context, x dbx, row *erow, ty *vocabulary.Kind, withEdges, withAnnotations bool) (*substrate.Record, error) {
	e := recordOf(ty, row)
	// Former ids are the record's own discarded names, server-set.
	former, err := loadFormerIDs(ctx, x, row.ref())
	if err != nil {
		return nil, err
	}
	e.FormerIDs = former
	if withEdges {
		edges, err := loadEdges(ctx, x, row.ref())
		if err != nil {
			return nil, err
		}
		e.Edges = edges
	}
	if withAnnotations {
		ann, err := loadAnnotations(ctx, x, row.ref())
		if err != nil {
			return nil, err
		}
		if len(ann) > 0 {
			e.Annotations = ann
		}
	}
	// A blob-ref reads back as the blob's manifest, never the bytes inline:
	// the stored digest resolves to {digest, mimeType, size, status} through
	// the blob record.
	if err := ds.resolveBlobRefs(ctx, x, ty, e); err != nil {
		return nil, err
	}
	return e, nil
}

func loadAnnotations(ctx context.Context, x dbx, ref eref) (map[string]any, error) {
	rows, err := x.QueryContext(ctx,
		`SELECT key, value FROM annotations WHERE record_kind = $1 AND record_id = $2 ORDER BY key`,
		ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]any{}
	for rows.Next() {
		var k string
		var raw []byte
		if err := rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func loadEdges(ctx context.Context, x dbx, ref eref) (map[string][]substrate.EdgeTarget, error) {
	rows, err := x.QueryContext(ctx, `
		SELECT e.rel, e.props, t.id, t.kind, t.title
		FROM edges e JOIN records t ON t.kind = e.dst_kind AND t.id = e.dst
		WHERE e.src_kind = $1 AND e.src = $2
		ORDER BY e.rel, e.created_at, e.dst_kind, e.dst`, ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]substrate.EdgeTarget{}
	for rows.Next() {
		var rel string
		var raw []byte
		var tgt substrate.EdgeTarget
		if err := rows.Scan(&rel, &raw, &tgt.ID, &tgt.Kind, &tgt.Title); err != nil {
			return nil, err
		}
		props := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &props)
		}
		if len(props) > 0 {
			tgt.Properties = props
		}
		out[rel] = append(out[rel], tgt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// --- the one generic list query ---

type builder struct {
	args  []any
	where []string
}

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *builder) add(clause string) { b.where = append(b.where, clause) }

func (b *builder) jsonArray(vals []string) string {
	raw, _ := json.Marshal(vals)
	return `(SELECT jsonb_array_elements_text(` + b.arg(raw) + `::jsonb))`
}

func (ds *dataset) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	b := &builder{}
	if err := ds.buildFilter(b, q.Filter); err != nil {
		return nil, err
	}
	terms, err := ds.orderTerms(q.OrderBy)
	if err != nil {
		return nil, err
	}
	order := renderOrder(terms)
	first := q.First
	if first <= 0 {
		first = defaultPageSize
	}
	if first > maxPageSize {
		first = maxPageSize
	}
	// KEYSET continuation: the opaque `after` token carries the
	// last row's ORDER BY key values (plus the id tiebreak that is already the
	// final order term), so the next page SEEKS strictly past that row instead
	// of skipping OFFSET rows. The walk is O(1) per page and — because it names
	// a position in the order rather than a count — stable under concurrent
	// inserts and deletes: every row that exists for the whole walk is seen
	// exactly once. The token pins the resolved order it was minted for; a
	// cursor replayed against a different orderBy is refused, not silently
	// mis-seeked.
	// carriedHead is the FIRST page's head, threaded through the cursor so every
	// page of one walk reports the same head.
	var carriedHead int64
	if q.After != "" {
		tok, err := decodeKeyset(q.After)
		if err != nil {
			return nil, err
		}
		if tok.O != order {
			return nil, fmt.Errorf("%w: cursor does not match this orderBy", substrate.ErrValidation)
		}
		if len(tok.K) != len(terms) {
			return nil, fmt.Errorf("%w: bad cursor", substrate.ErrValidation)
		}
		b.add(seekPredicate(b, terms, tok.K))
		carriedHead = tok.H
	}
	where := "TRUE"
	if len(b.where) > 0 {
		where = strings.Join(b.where, " AND ")
	}
	// The order-key expressions are appended to the projection as text so the
	// last row's key values can be captured verbatim for the next cursor;
	// computing them in Go would risk a rendering mismatch (jsonb ->> vs
	// fmt), and any mismatch skips or repeats rows.
	keyCols := make([]string, len(terms))
	for i, t := range terms {
		// Aliased so a bare-column key (created_at, at, …) does not collide
		// with its own recordCols output name and make ORDER BY ambiguous.
		keyCols[i] = `(` + t.expr + `)::text AS __k` + strconv.Itoa(i)
	}
	limitArg := b.arg(first + 1)
	sqlText := listSQL(where, keyCols, order, limitArg)

	// A read-only REPEATABLE READ snapshot pins the page AND the head seq to
	// one point in time: head = MAX(seq) in this snapshot, so every
	// row the page can see has a change seq <= head, and `watch?from=head`
	// replays exactly the changes NOT in this snapshot — no gap, no dup. But
	// REPEATABLE READ pins ONE page, not the whole walk (each List opens a fresh
	// txn and snapshot), so the walk's head is the FIRST page's head carried
	// through the cursor (codex regress #3): a later page reports the same head
	// it began at, and a row inserted mid-walk lands strictly after that head —
	// caught by the watch, never lost. The keyset walk itself still sees every
	// row committed for the whole walk exactly once.
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	page := &substrate.Page{}
	if carriedHead != 0 {
		page.Head = carriedHead
	} else {
		var head sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT max(seq) FROM changelog`).Scan(&head); err != nil {
			return nil, err
		}
		page.Head = head.Int64
	}

	// The rows cursor is drained FULLY before any hydration: hydrate issues its
	// own reads on the same snapshot connection, and pgx forbids a second query
	// while this cursor is open ("conn busy").
	rows, err := tx.QueryContext(ctx, sqlText, b.args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: list: %w", err)
	}
	type scanned struct {
		row  *erow
		keys []*string
	}
	var got []scanned
	hasMore := false
	for rows.Next() {
		var es recordScan
		keyBufs := make([]sql.NullString, len(terms))
		dests := es.dests()
		for i := range keyBufs {
			dests = append(dests, &keyBufs[i])
		}
		if err := rows.Scan(dests...); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if len(got) == first {
			// The (first+1)th row only proves there is another page; the cursor
			// points AT the last RETURNED row, so this row is discarded.
			hasMore = true
			break
		}
		keys := make([]*string, len(terms))
		for i := range keyBufs {
			if keyBufs[i].Valid {
				v := keyBufs[i].String
				keys[i] = &v
			}
		}
		got = append(got, scanned{row: es.finish(), keys: keys})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if hasMore && len(got) > 0 {
		page.Cursor = encodeKeyset(order, got[len(got)-1].keys, page.Head)
	}
	for _, s := range got {
		e, err := ds.hydrate(ctx, tx, s.row, q.WithEdges, q.WithAnnotations)
		if err != nil {
			return nil, err
		}
		page.Records = append(page.Records, e)
	}
	return page, nil
}

// listSQL assembles the keyset list query. It carries a LIMIT and NEVER an
// OFFSET: continuation is the seek predicate already folded into
// `where`, so a deep page costs the same as a shallow one. keyCols are the
// aliased order-key projections that capture the last row's cursor values.
func listSQL(where string, keyCols []string, order, limitArg string) string {
	return `SELECT ` + recordCols + `, ` + strings.Join(keyCols, ", ") +
		` FROM records WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ` + limitArg
}

func (ds *dataset) buildFilter(b *builder, f substrate.Filter) error {
	reg := ds.registry()
	var types []*vocabulary.Kind
	seen := map[string]bool{}
	addType := func(t *vocabulary.Kind) {
		if !seen[t.Identity] {
			seen[t.Identity] = true
			types = append(types, t)
		}
	}
	for _, name := range f.Kinds {
		t, err := reg.Resolve(name)
		if err != nil {
			return fmt.Errorf("%w: %w", substrate.ErrValidation, err)
		}
		addType(t)
	}
	if f.Implements != "" {
		impl, err := reg.ImplementingStrict(f.Implements)
		if err != nil {
			return fmt.Errorf("%w: %w", substrate.ErrValidation, err)
		}
		if len(impl) == 0 {
			return fmt.Errorf("%w: no type implements %q", substrate.ErrValidation, f.Implements)
		}
		// Every predicate in a filter NARROWS: `types` and `implements` intersect,
		// they never union. Unioning them let a COLLECTION read — where the path
		// forces filter.types — answer with rows of other types
		// entirely, so `/tasks?filter={"implements":"…"}` returned every
		// implementor in the repository. `implements` alone still means "every
		// implementor", which is what the trait-records resource asks for.
		if len(types) == 0 {
			for _, t := range impl {
				addType(t)
			}
		} else {
			implements := make(map[string]bool, len(impl))
			for _, t := range impl {
				implements[t.Identity] = true
			}
			kept := types[:0]
			for _, t := range types {
				if implements[t.Identity] {
					kept = append(kept, t)
				}
			}
			if len(kept) == 0 {
				return fmt.Errorf("%w: no type in filter.types implements %q", substrate.ErrValidation, f.Implements)
			}
			types = kept
		}
	}
	if len(types) > 0 {
		idents := make([]string, 0, len(types))
		for _, t := range types {
			idents = append(idents, t.Identity)
		}
		b.add(`kind IN ` + b.jsonArray(idents))
	}
	if len(f.IDs) > 0 {
		b.add(`id IN ` + b.jsonArray(f.IDs))
	}
	switch {
	case f.Deleted == nil || !*f.Deleted:
		b.add(`deleted_at IS NULL`)
	default:
		b.add(`deleted_at IS NOT NULL`)
	}
	for _, name := range sortedKeys(f.Properties) {
		if err := ds.condProp(b, types, name, f.Properties[name]); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(f.Labels) {
		if err := condJSON(b, `labels`, name, f.Labels[name], ""); err != nil {
			return err
		}
	}
	if f.Edge != nil {
		if f.Edge.To == "" {
			return fmt.Errorf("%w: edge filter needs a target", substrate.ErrValidation)
		}
		// The target matches by id; ToType narrows it to one type when two
		// types hold the same id (identity is the (type, id) pair).
		clause := `EXISTS (SELECT 1 FROM edges x WHERE x.src_kind = records.kind AND x.src = records.id AND x.dst = ` + b.arg(f.Edge.To)
		if f.Edge.ToKind != "" {
			ty, err := reg.Resolve(f.Edge.ToKind)
			if err != nil {
				return fmt.Errorf("%w: %w", substrate.ErrValidation, err)
			}
			clause += ` AND x.dst_kind = ` + b.arg(ty.Identity)
		}
		if f.Edge.Rel != "" {
			clause += ` AND x.rel = ` + b.arg(f.Edge.Rel)
		}
		b.add(clause + `)`)
	}
	return nil
}

// recordColumns maps the filterable/orderable record columns onto their SQL
// names. One casing, everywhere: the wire spells them camelCase and the table
// keeps SQL's snake.
var recordColumns = map[string]string{
	substrate.PropAt: "at", substrate.PropEndsAt: "ends_at", substrate.PropDueAt: "due_at",
	"createdAt": "created_at", "updatedAt": "updated_at", "deletedAt": "deleted_at",
	"title": "title", "body": "body", "id": "id", "version": "version",
}

// snakeColumns names the deleted spelling of every camelCase column, so a
// caller that used the old one is told what replaced it instead of getting
// "cannot order by".
var snakeColumns = map[string]string{
	"ends_at": substrate.PropEndsAt, "due_at": substrate.PropDueAt,
	"created_at": "createdAt", "updated_at": "updatedAt", "deleted_at": "deletedAt",
}

func columnFor(name string) (string, error) {
	if col, ok := recordColumns[name]; ok {
		return col, nil
	}
	if camel, ok := snakeColumns[name]; ok {
		return "", fmt.Errorf("%w: %q is spelled %q", substrate.ErrValidation, name, camel)
	}
	return "", nil
}

func (ds *dataset) condProp(b *builder, types []*vocabulary.Kind, name string, c substrate.Cond) error {
	col, err := columnFor(name)
	if err != nil {
		return err
	}
	if col != "" {
		return condColumn(b, col, c)
	}
	// A state property filters like any other property; only its STORAGE is
	// the states column (MODEL §11.4).
	if ds.stateProp(types, name) {
		return condJSON(b, `states`, name, c, vocabulary.DatatypeString)
	}
	if ds.sensitiveProp(types, name) {
		return fmt.Errorf("%w: %s is sensitive and cannot be filtered", substrate.ErrValidation, name)
	}
	kind := vocabulary.Datatype("")
	for _, t := range types {
		if p, ok := t.Prop(name); ok {
			kind = p.Datatype
			if p.Repeated {
				kind = ""
			}
			break
		}
	}
	return condJSON(b, `props`, name, c, kind)
}

// stateProp reports whether name is a state property on any candidate type —
// every loaded type when the query names none, the same way sensitiveProp asks.
func (ds *dataset) stateProp(types []*vocabulary.Kind, name string) bool {
	if len(types) == 0 {
		types = ds.registry().Kinds()
	}
	for _, t := range types {
		if _, ok := t.StateProp(name); ok {
			return true
		}
	}
	return false
}

// sensitiveProp reports whether name is sensitive on any candidate type —
// every loaded type when the query names none. A filter or ordering over a
// redacted value is an oracle that reconstructs it one comparison at a time.
func (ds *dataset) sensitiveProp(types []*vocabulary.Kind, name string) bool {
	if len(types) == 0 {
		types = ds.registry().Kinds()
	}
	for _, t := range types {
		if p, ok := t.Prop(name); ok && p.Sensitive() {
			return true
		}
	}
	return false
}

// condColumn filters a hot column or a plain records column.
func condColumn(b *builder, col string, c substrate.Cond) error {
	expr := col
	for _, x := range []struct {
		op string
		v  any
	}{{"=", c.Eq}, {">", c.Gt}, {">=", c.Gte}, {"<", c.Lt}, {"<=", c.Lte}} {
		if x.v == nil {
			continue
		}
		v, err := columnValue(col, x.v)
		if err != nil {
			return err
		}
		b.add(expr + " " + x.op + " " + b.arg(v))
	}
	if len(c.In) > 0 {
		vals := make([]string, 0, len(c.In))
		for _, v := range c.In {
			vals = append(vals, fmt.Sprint(v))
		}
		b.add(expr + `::text IN ` + b.jsonArray(vals))
	}
	if c.Prefix != "" {
		b.add(expr + `::text LIKE ` + b.arg(likePrefix(c.Prefix)))
	}
	if c.Exists != nil {
		if *c.Exists {
			b.add(expr + ` IS NOT NULL`)
		} else {
			b.add(expr + ` IS NULL`)
		}
	}
	return nil
}

func columnValue(col string, v any) (any, error) {
	if !strings.HasSuffix(col, "at") {
		return fmt.Sprint(v), nil
	}
	switch t := v.(type) {
	case time.Time:
		return t.UTC(), nil
	case string:
		ts, err := parseTime(t)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", substrate.ErrValidation, col, err)
		}
		return ts.UTC(), nil
	default:
		return nil, fmt.Errorf("%w: %s expects a timestamp", substrate.ErrValidation, col)
	}
}

// condJSON filters one key of a jsonb column, casting by declared kind so
// ranges compare as numbers or instants rather than text.
func condJSON(b *builder, col, key string, c substrate.Cond, kind vocabulary.Datatype) error {
	// The key is bound LAZILY: a placeholder the WHERE never mentions leaves
	// Postgres unable to infer its type ("could not determine data type of
	// parameter $n"), so it is only appended when a clause actually uses it.
	keyArg := ""
	textExpr := func() string {
		if keyArg == "" {
			keyArg = b.arg(key)
		}
		return col + `->>(` + keyArg + `::text)`
	}
	cast := func(v any) (any, error) { return fmt.Sprint(v), nil }
	typedExpr := func() string { return textExpr() }
	switch kind {
	case vocabulary.DatatypeInt, vocabulary.DatatypeFloat:
		typedExpr = func() string { return `(` + textExpr() + `)::numeric` }
		cast = func(v any) (any, error) { return asFloat(v) }
	case vocabulary.DatatypeDatetime, vocabulary.DatatypeDate:
		typedExpr = func() string { return `(` + textExpr() + `)::timestamptz` }
		cast = func(v any) (any, error) {
			switch t := v.(type) {
			case time.Time:
				return t.UTC(), nil
			default:
				ts, err := parseTime(fmt.Sprint(v))
				if err != nil {
					return nil, fmt.Errorf("%w: %s: %w", substrate.ErrValidation, key, err)
				}
				return ts.UTC(), nil
			}
		}
	case vocabulary.DatatypeBool:
		typedExpr = func() string { return `(` + textExpr() + `)::boolean` }
		cast = func(v any) (any, error) { return v, nil }
	}
	for _, x := range []struct {
		op string
		v  any
	}{{"=", c.Eq}, {">", c.Gt}, {">=", c.Gte}, {"<", c.Lt}, {"<=", c.Lte}} {
		if x.v == nil {
			continue
		}
		v, err := cast(x.v)
		if err != nil {
			return err
		}
		b.add(typedExpr() + " " + x.op + " " + b.arg(v))
	}
	if len(c.In) > 0 {
		vals := make([]string, 0, len(c.In))
		for _, v := range c.In {
			vals = append(vals, fmt.Sprint(v))
		}
		b.add(textExpr() + ` IN ` + b.jsonArray(vals))
	}
	if c.Prefix != "" {
		b.add(textExpr() + ` LIKE ` + b.arg(likePrefix(c.Prefix)))
	}
	if c.Contains != nil {
		raw, err := json.Marshal([]any{c.Contains})
		if err != nil {
			return err
		}
		b.add(col + `->(` + b.arg(key) + `::text) @> ` + b.arg(raw) + `::jsonb`)
	}
	if c.Exists != nil {
		clause := `jsonb_exists(` + col + `, ` + b.arg(key) + `)`
		if !*c.Exists {
			clause = `NOT ` + clause
		}
		b.add(clause)
	}
	return nil
}

func likePrefix(p string) string {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(p)
	return esc + "%"
}

// orderTerm is one resolved sort key: the SQL expression and its direction.
// The final terms of any order are always the (`kind`, `id`) tiebreak — two
// NOT-NULL columns whose PAIR is the record's unique identity: an
// id is unique only WITHIN a type, so a cross-type walk (GraphQL records, a
// trait/implements query spanning types) needs `kind` beside `id` for a strict
// total order, the precondition keyset seeking relies on. Ordering by id alone
// left two kinds sharing an id and equal sort values non-deterministically
// ordered — a page boundary could skip or duplicate one (codex regress #2).
type orderTerm struct {
	expr string
	desc bool
}

// nonNull reports whether this term is one of the (kind, id) tiebreak columns
// — never null, so its seek needs no null branch and its ORDER BY needs no
// NULLS LAST. A caller ordering explicitly by `kind`/`id` also lands here,
// which is correct: both columns are NOT NULL.
func (t orderTerm) nonNull() bool { return t.expr == "id" || t.expr == "kind" }

// orderExpr resolves one order property to its SQL expression.
func (ds *dataset) orderExpr(property string) (string, error) {
	col, err := columnFor(property)
	if err != nil {
		return "", err
	}
	switch {
	case col != "":
		return col, nil
	case ds.stateProp(nil, property):
		// Built by concatenation, so escaped the same way indices.go builds its
		// jsonb paths: the ValidCamel load-time invariant already forbids a
		// quote, and sqlLiteral is the defense-in-depth that does not rely on it.
		return `states->>` + sqlLiteral(property), nil
	case vocabulary.ValidCamel(property):
		if ds.sensitiveProp(nil, property) {
			return "", fmt.Errorf("%w: %s is sensitive and cannot be ordered by",
				substrate.ErrValidation, property)
		}
		return `props->>` + sqlLiteral(property), nil
	default:
		return "", fmt.Errorf("%w: cannot order by %q", substrate.ErrValidation, property)
	}
}

// orderTerms resolves the query's orderBy to the full list of sort keys,
// including the trailing (`kind`, `id`) tiebreak that makes the order a STRICT
// TOTAL order over the (type, id) identity. The
// default order is newest-first (created_at DESC); the tiebreak takes the
// leading key's direction, as it always has.
func (ds *dataset) orderTerms(orders []substrate.Order) ([]orderTerm, error) {
	var terms []orderTerm
	tieDesc := true
	if len(orders) == 0 {
		terms = append(terms, orderTerm{expr: "created_at", desc: true})
	} else {
		for _, o := range orders {
			expr, err := ds.orderExpr(o.Property)
			if err != nil {
				return nil, err
			}
			terms = append(terms, orderTerm{expr: expr, desc: o.Desc})
		}
		tieDesc = orders[0].Desc
	}
	terms = append(terms, orderTerm{expr: "kind", desc: tieDesc}, orderTerm{expr: "id", desc: tieDesc})
	return terms, nil
}

// renderOrder renders the ORDER BY clause. Nullable keys sort NULLS LAST (the
// keyset seek is built to match); the (kind, id) tiebreak columns are never
// null.
func renderOrder(terms []orderTerm) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		dir := "ASC"
		if t.desc {
			dir = "DESC"
		}
		if t.nonNull() {
			parts[i] = t.expr + " " + dir
		} else {
			parts[i] = t.expr + " " + dir + " NULLS LAST"
		}
	}
	return strings.Join(parts, ", ")
}

// seekPredicate builds the WHERE fragment selecting rows strictly AFTER the
// cursor row in the ORDER BY order. For keys `k0 d0, k1 d1, …, id` and cursor
// values `v0, v1, …`, the predicate is the lexicographic disjunction
//
//	(after on k0) OR (k0 eq v0 AND after on k1) OR …
//
// where "after on k" for an ASC key is `k > v` (DESC: `k < v`), and — because
// every non-id key sorts NULLS LAST — a null sorts after any value, so a
// non-null cursor value also admits `k IS NULL`. A null cursor value has
// nothing strictly after it on that key alone (later keys break the tie
// through the equal-prefix, where equality on a null is `k IS NULL`), so that
// disjunct is dropped. The id tiebreak is unique and non-null, so the final
// disjunct is always a strict, total cut.
func seekPredicate(b *builder, terms []orderTerm, keys []*string) string {
	var ors []string
	for i := range terms {
		var ands []string
		for j := range i {
			ands = append(ands, eqKey(b, terms[j], keys[j]))
		}
		after := afterKey(b, terms[i], keys[i])
		if after == "" {
			continue
		}
		ands = append(ands, after)
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	if len(ors) == 0 {
		return "FALSE"
	}
	return "(" + strings.Join(ors, " OR ") + ")"
}

// eqKey and afterKey compare against the NATIVE expression (never a ::text
// cast), so the comparison uses the exact type — and therefore the exact
// ordering — the ORDER BY uses: text-cast comparison would sort a numeric or
// timestamp key lexically and skip or repeat rows. The captured value is a
// string, and Postgres coerces it to the expression's type from context.
func eqKey(b *builder, t orderTerm, v *string) string {
	if v == nil {
		return t.expr + " IS NULL"
	}
	return t.expr + " = " + b.arg(*v)
}

func afterKey(b *builder, t orderTerm, v *string) string {
	if v == nil {
		return ""
	}
	op := ">"
	if t.desc {
		op = "<"
	}
	cmp := t.expr + " " + op + " " + b.arg(*v)
	if t.nonNull() {
		return cmp
	}
	return `(` + cmp + ` OR ` + t.expr + ` IS NULL)`
}

// keyset is the decoded form of the opaque list/incoming continuation token.
// O pins the resolved order it was minted against (so a cursor cannot be
// replayed against a different orderBy); K carries the last row's key values,
// a nil element meaning that key was NULL. H carries the FIRST page's changelog
// head so the whole walk reports ONE head: the
// list→watch handoff must pin the snapshot the walk began at, not each page's
// own new snapshot, or an insert during paging is lost. Zero (an empty
// changelog first page) is omitted; the Incoming reader mints no head.
type keyset struct {
	O string    `json:"o"`
	K []*string `json:"k"`
	H int64     `json:"h,omitempty"`
}

func encodeKeyset(order string, keys []*string, head int64) string {
	raw, _ := json.Marshal(keyset{O: order, K: keys, H: head})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeKeyset(cur string) (*keyset, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil {
		return nil, fmt.Errorf("%w: bad cursor", substrate.ErrValidation)
	}
	var k keyset
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, fmt.Errorf("%w: bad cursor", substrate.ErrValidation)
	}
	return &k, nil
}

// --- changelog ---

func (ds *dataset) Changes(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	b := &builder{}
	b.add(`seq > ` + b.arg(after))
	if err := ds.buildChangeFilter(b, f); err != nil {
		return nil, err
	}
	return ds.queryChanges(ctx, b, `seq`, limit)
}
