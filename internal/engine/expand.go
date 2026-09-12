package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The two graph arms of the one list query: `referencing`, the reverse read
// as a predicate over the refs index, and `expand`, the forward hop that
// carries a page's referents beside it. Both stand on the refs index and the
// stored reference values, so a pointer of any declared shape (pinned or
// unpinned, single or repeated, a kind's own property or one nested inside an
// object) is visible to the reverse read, and the forward one reads exactly
// what a record stores.

// referencingTarget resolves the target a reverse read names: its canonical
// identity and every id it used to live under. A merge repoints no stored
// value, so a match against the canonical id alone would miss every pointer
// written before the merge.
func (ds *dataset) referencingTarget(ctx context.Context, x dbx, ref *substrate.Referencing) (eref, []string, error) {
	kind, id, ok := vocabulary.SplitRecordPath(ref.Ref)
	if !ok {
		return eref{}, nil, fmt.Errorf("%w: referencing.ref must be a record path \"<kind>/<id>\", got %q",
			substrate.ErrValidation, ref.Ref)
	}
	ty, err := ds.resolveType(kind)
	if err != nil {
		return eref{}, nil, fmt.Errorf("%w: referencing.ref: %w", substrate.ErrValidation, err)
	}
	canonical, err := ds.canonicalOf(ctx, x, eref{Kind: ty.Identity, ID: id})
	if err != nil {
		return eref{}, nil, err
	}
	ids, err := ds.idsOf(ctx, x, canonical)
	if err != nil {
		return eref{}, nil, err
	}
	return canonical, ids, nil
}

// referencingWhere renders the target half of a refs predicate: the target
// kind, the target id set and the optional property narrowing. A single id
// binds as a scalar so the planner can walk refs_dst_idx in order; only a
// record with a former-id trail takes the set form.
func referencingWhere(b *builder, canonical eref, ids []string, property string) string {
	var target string
	if len(ids) == 1 {
		target = `r.dst = ` + b.arg(ids[0])
	} else {
		target = `r.dst IN ` + b.jsonArray(ids)
	}
	where := `r.dst_kind = ` + b.arg(canonical.Kind) + ` AND ` + target
	if property != "" {
		where += ` AND r.property = ` + b.arg(property)
	}
	return where
}

// condReferencing adds the reverse-read predicate to a list: the row holds at
// least one reference at the target. It is a correlated EXISTS rather than a
// join so the list stays a list of distinct records and pages on the record
// order like every other filter arm.
func (ds *dataset) condReferencing(ctx context.Context, x dbx, b *builder, ref *substrate.Referencing) error {
	canonical, ids, err := ds.referencingTarget(ctx, x, ref)
	if err != nil {
		return err
	}
	b.add(`EXISTS (SELECT 1 FROM refs r WHERE r.src_kind = records.kind AND r.src = records.id AND ` +
		referencingWhere(b, canonical, ids, ref.Property) + `)`)
	return nil
}

// referenceSites reads, for each record on a referencing page, the sites at
// which it points at the target. A page is distinct records, and one source
// can point from two sites, so the sites ride beside the page rather than
// multiplying its rows. Read on the page's own snapshot.
func (ds *dataset) referenceSites(ctx context.Context, x dbx, ref *substrate.Referencing, records []*substrate.Record) (map[string][]substrate.ReferenceSite, error) {
	if len(records) == 0 {
		return nil, nil
	}
	canonical, ids, err := ds.referencingTarget(ctx, x, ref)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(records))
	for _, e := range records {
		paths = append(paths, vocabulary.RecordPath(e.Kind, e.ID))
	}
	b := &builder{}
	rows, err := x.QueryContext(ctx,
		`SELECT r.src_kind, r.src, r.property, r.path FROM refs r WHERE `+
			referencingWhere(b, canonical, ids, ref.Property)+
			` AND (r.src_kind || '/' || r.src) IN `+b.jsonArray(paths)+
			` ORDER BY r.src_kind, r.src, r.property, r.path, r.ord`, b.args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]substrate.ReferenceSite{}
	for rows.Next() {
		var srcKind, src string
		var site substrate.ReferenceSite
		if err := rows.Scan(&srcKind, &src, &site.Property, &site.Path); err != nil {
			return nil, err
		}
		key := vocabulary.RecordPath(srcKind, src)
		// A repeated reference holding the target twice is one site: the ord
		// distinguishes values, not places.
		if prev := out[key]; len(prev) > 0 && prev[len(prev)-1] == site {
			continue
		}
		out[key] = append(out[key], site)
	}
	return out, rows.Err()
}

// expandProperties resolves the `expand` names against the kinds the filter
// admits (every kind when it names none): each must be a reference property
// one of them declares, single or repeated. A name none declares is refused,
// naming what could have been expanded, because a silently ignored expansion
// reads as a dangling graph.
func (ds *dataset) expandProperties(types []*vocabulary.Kind, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if len(types) == 0 {
		types = ds.registry().Kinds()
	}
	expandable := map[string]bool{}
	for _, t := range types {
		for _, name := range t.PropOrder {
			p := t.Props[name]
			if p != nil && p.Datatype == vocabulary.DatatypeReference && !p.Keyed {
				expandable[name] = true
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		if !expandable[name] {
			return nil, fmt.Errorf("%w: expand: %q is not a reference property of the kinds this list admits; expandable: %s",
				substrate.ErrValidation, name, strings.Join(sortedKeys(expandable), ", "))
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// maxExpanded caps how many referents one page may carry beside it. It is the
// page cap, so a page and its expansion together are at most two pages.
const maxExpanded = maxPageSize

// expandReferents loads the referents the page's records name under the
// expanded properties, once each, keyed by the path AS WRITTEN so a reader
// joins them by the value it holds. Loading is one query per referent kind on
// the page's snapshot, LIVE rows only: a merged-away id still has its
// tombstone under that id, and answering it here would hide the winner. A
// path the query does not answer is read again by Get, which follows the
// former-id trail, so a pointer at a merged-away id resolves to the canonical
// record. A path neither answers with a live record is dangling and has no
// entry.
func (ds *dataset) expandReferents(ctx context.Context, x dbx, names []string, records []*substrate.Record) (map[string]*substrate.Record, error) {
	byKind := map[string][]string{}
	var order []string
	seen := map[string]bool{}
	for _, e := range records {
		for _, name := range names {
			for _, path := range referencePathsOf(e.Properties[name]) {
				kind, id, ok := vocabulary.SplitRecordPath(path)
				if !ok || seen[path] {
					continue
				}
				seen[path] = true
				order = append(order, path)
				byKind[kind] = append(byKind[kind], id)
			}
		}
	}
	if len(order) == 0 {
		return nil, nil
	}
	if len(order) > maxExpanded {
		return nil, fmt.Errorf("%w: expand would load %d referents, more than %d; lower first or expand fewer properties",
			substrate.ErrValidation, len(order), maxExpanded)
	}
	included := make(map[string]*substrate.Record, len(order))
	for _, kind := range sortedKeys(byKind) {
		b := &builder{}
		rows, err := x.QueryContext(ctx,
			`SELECT `+recordCols+` FROM records WHERE kind = `+b.arg(kind)+` AND id IN `+b.jsonArray(byKind[kind])+
				` AND deleted_at IS NULL`,
			b.args...)
		if err != nil {
			return nil, err
		}
		// Drained before hydration: hydrate reads on the same connection.
		var got []*erow
		for rows.Next() {
			row, err := scanRecord(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			got = append(got, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		for _, row := range got {
			e, err := ds.hydrate(ctx, x, row, false)
			if err != nil {
				return nil, err
			}
			included[vocabulary.RecordPath(e.Kind, e.ID)] = e
		}
	}
	for _, path := range order {
		if _, ok := included[path]; ok {
			continue
		}
		// A path the batch did not answer is a former id or a dangling
		// pointer. The trail is walked on the same snapshot, and a winner it
		// reaches is read there too, so an expansion never carries a row the
		// page's head has not seen.
		kind, id, _ := vocabulary.SplitRecordPath(path)
		canonical, err := ds.canonicalOf(ctx, x, eref{Kind: kind, ID: id})
		if err != nil {
			return nil, err
		}
		if canonical.ID == id {
			continue
		}
		row, err := scanRecord(x.QueryRowContext(ctx,
			`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
			canonical.Kind, canonical.ID))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		e, err := ds.hydrate(ctx, x, row, false)
		if err != nil {
			return nil, err
		}
		included[path] = e
	}
	return included, nil
}

// referencePathsOf reads every referent path a stored reference value names:
// a single value, or each element of a repeated one, in either stored shape
// (a bare path, or the object keyed by `ref`).
func referencePathsOf(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if p := referencePathOf(item); p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		if p := referencePathOf(t); p != "" {
			return []string{p}
		}
	}
	return nil
}

// filterDigest is the filter's identity for a cursor: its canonical JSON,
// hashed. The digest is not a secret; it is only a compact way to say which
// predicate a page was cut from, so a token cannot be replayed under another.
func filterDigest(f substrate.Filter) string {
	raw, _ := json.Marshal(f)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}
