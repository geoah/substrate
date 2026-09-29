package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

// refTarget is one kind's share of a reverse read's targets: the kind, and
// every id a stored pointer at one of its targets may spell, canonical and
// former alike.
type refTarget struct {
	kind string
	ids  []string
}

// referencingTargets resolves the targets a reverse read names, `ref` or
// `refs`: each one's canonical identity and every id it used to live under,
// grouped by kind in kind order. A merge repoints no stored value, so a match
// against the canonical id alone would miss every pointer written before the
// merge.
func (ds *dataset) referencingTargets(ctx context.Context, x dbx, ref *substrate.Referencing) ([]refTarget, error) {
	paths, err := ref.Targets()
	if err != nil {
		return nil, err
	}
	targets := make([]eref, 0, len(paths))
	for i, path := range paths {
		field := "referencing.ref"
		if ref.Refs != nil {
			field = fmt.Sprintf("referencing.refs[%d]", i)
		}
		kind, id, ok := vocabulary.SplitRecordPath(path)
		if !ok {
			return nil, fmt.Errorf("%w: %s must be a record path \"<kind>/<id>\", got %q",
				substrate.ErrValidation, field, path)
		}
		ty, err := ds.resolveType(kind)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", substrate.ErrValidation, field, err)
		}
		targets = append(targets, eref{Kind: ty.Identity, ID: id})
	}
	trail, err := trailIDs(ctx, x, targets)
	if err != nil {
		return nil, err
	}
	byKind := map[string][]string{}
	for _, e := range trail {
		byKind[e.Kind] = append(byKind[e.Kind], e.ID)
	}
	out := make([]refTarget, 0, len(byKind))
	for _, kind := range sortedKeys(byKind) {
		ids := byKind[kind]
		slices.Sort(ids)
		out = append(out, refTarget{kind: kind, ids: ids})
	}
	return out, nil
}

// trailIDs resolves targets to every record identity a stored pointer may
// spell and still resolve, on read, to one of their canonical records: each
// target's canonical identity, then every former id whose trail leads to one
// of those. A trail stays within its kind. Each hop is one statement for
// every target of every kind, so the cost is the trail's depth, not the
// target count. Both walks go hop by hop, bounded like canonicalOf, rather
// than as one join: a live merge flattens the trail, but the flattening is
// not a changelog effect, so a rebuilt repository can hold a chain (x names
// y, y names c), and a pointer stored as x resolves to c on read and must
// answer a reverse read of c.
func trailIDs(ctx context.Context, x dbx, targets []eref) ([]eref, error) {
	cur := make(map[eref]eref, len(targets))
	seen := make(map[eref]map[eref]bool, len(targets))
	var pending []eref
	for _, t := range targets {
		if _, dup := cur[t]; dup {
			continue
		}
		cur[t] = t
		seen[t] = map[eref]bool{t: true}
		pending = append(pending, t)
	}
	for hop := 0; hop < 8 && len(pending) > 0; hop++ {
		next, err := formerTargets(ctx, x, pending)
		if err != nil {
			return nil, err
		}
		pending = nil
		queued := map[eref]bool{}
		for t, at := range cur {
			to, moved := next[at]
			if !moved || seen[t][to] {
				continue
			}
			seen[t][to] = true
			cur[t] = to
			if !queued[to] {
				queued[to] = true
				pending = append(pending, to)
			}
		}
	}
	found := map[eref]bool{}
	var frontier []eref
	for _, at := range cur {
		if !found[at] {
			found[at] = true
			frontier = append(frontier, at)
		}
	}
	for hop := 0; hop < 8 && len(frontier) > 0; hop++ {
		formers, err := formersOf(ctx, x, frontier)
		if err != nil {
			return nil, err
		}
		frontier = nil
		for _, f := range formers {
			if !found[f] {
				found[f] = true
				frontier = append(frontier, f)
			}
		}
	}
	out := make([]eref, 0, len(found))
	for e := range found {
		out = append(out, e)
	}
	return out, nil
}

// trailArgs splits identities into the two parallel arrays a trail statement
// unnests as (kind, id) pairs.
func trailArgs(refs []eref) ([]string, []string) {
	kinds := make([]string, len(refs))
	ids := make([]string, len(refs))
	for i, r := range refs {
		kinds[i], ids[i] = r.Kind, r.ID
	}
	return kinds, ids
}

// formerTargets reads one hop of the trail forwards: for each identity that
// is a former id, the record it now names within its kind. An identity that
// is no former id is absent.
func formerTargets(ctx context.Context, x dbx, refs []eref) (map[eref]eref, error) {
	kinds, ids := trailArgs(refs)
	rows, err := x.QueryContext(ctx, `
		SELECT f.record_kind, f.former_id, f.record_id
		  FROM unnest($1::text[], $2::text[]) AS t(kind, id)
		  JOIN former_ids f ON f.record_kind = t.kind AND f.former_id = t.id`, kinds, ids)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[eref]eref{}
	for rows.Next() {
		var kind, former, record string
		if err := rows.Scan(&kind, &former, &record); err != nil {
			return nil, err
		}
		out[eref{Kind: kind, ID: former}] = eref{Kind: kind, ID: record}
	}
	return out, rows.Err()
}

// formersOf reads one hop of the trail backwards: every former id recorded
// against one of the identities, within its kind.
func formersOf(ctx context.Context, x dbx, refs []eref) ([]eref, error) {
	kinds, ids := trailArgs(refs)
	rows, err := x.QueryContext(ctx, `
		SELECT f.record_kind, f.former_id
		  FROM unnest($1::text[], $2::text[]) AS t(kind, id)
		  JOIN former_ids f ON f.record_kind = t.kind AND f.record_id = t.id`, kinds, ids)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []eref
	for rows.Next() {
		var e eref
		if err := rows.Scan(&e.Kind, &e.ID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// referencingWhere renders the target half of a refs predicate: per target
// kind, the kind and its id set, OR'd across kinds, then the optional
// property narrowing. A single id binds as a scalar so the planner can walk
// refs_dst_idx in order; a kind with several ids (a former-id trail, or
// several targets) takes the set form, which the same index still serves.
// Every arm names dst_kind and dst, the index's leading columns after the
// repository, so no arm reads the refs table outside the index.
func referencingWhere(b *builder, targets []refTarget, property string) string {
	arms := make([]string, 0, len(targets))
	for _, t := range targets {
		var dst string
		if len(t.ids) == 1 {
			dst = `r.dst = ` + b.arg(t.ids[0])
		} else {
			dst = `r.dst = ANY(` + b.textArray(t.ids) + `)`
		}
		arms = append(arms, `r.dst_kind = `+b.arg(t.kind)+` AND `+dst)
	}
	var where string
	switch len(arms) {
	case 0:
		where = `FALSE`
	case 1:
		where = arms[0]
	default:
		where = `((` + strings.Join(arms, `) OR (`) + `))`
	}
	if property != "" {
		where += ` AND r.property = ` + b.arg(property)
	}
	return where
}

// condReferencing adds the reverse-read predicate to a list: the row holds at
// least one reference at a target. It is a correlated EXISTS rather than a
// join so the list stays a list of distinct records, one row however many
// targets it points at, and pages on the record order like every other
// filter arm.
func (ds *dataset) condReferencing(ctx context.Context, x dbx, b *builder, ref *substrate.Referencing) error {
	targets, err := ds.referencingTargets(ctx, x, ref)
	if err != nil {
		return err
	}
	b.add(`EXISTS (SELECT 1 FROM refs r WHERE r.src_kind = records.kind AND r.src = records.id AND ` +
		referencingWhere(b, targets, ref.Property) + `)`)
	return nil
}

// referenceSites reads, for each record on a referencing page, the sites at
// which it points at a target. A page is distinct records, and one source
// can point from two sites, so the sites ride beside the page rather than
// multiplying its rows. Read on the page's own snapshot.
func (ds *dataset) referenceSites(ctx context.Context, x dbx, ref *substrate.Referencing, records []*substrate.Record) (map[string][]substrate.ReferenceSite, error) {
	if len(records) == 0 {
		return nil, nil
	}
	targets, err := ds.referencingTargets(ctx, x, ref)
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
			referencingWhere(b, targets, ref.Property)+
			` AND (r.src_kind || '/' || r.src) = ANY(`+b.textArray(paths)+`)`+
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
// expanded properties, keyed by the path AS WRITTEN so a reader joins them by
// the value it holds.
func (ds *dataset) expandReferents(ctx context.Context, x dbx, names []string, records []*substrate.Record) (map[string]*substrate.Record, error) {
	var paths []string
	for _, e := range records {
		for _, name := range names {
			paths = append(paths, referencePathsOf(e.Properties[name])...)
		}
	}
	return ds.loadReferents(ctx, x, paths)
}

// loadReferents loads the records a list of paths names, once each and in one
// query per kind, on the caller's snapshot. LIVE rows only: a merged-away id
// still has its tombstone under that id, and answering it here would hide the
// winner. A path the query does not answer is read again through the
// former-id trail, so a pointer at a merged-away id resolves to the canonical
// record. A path neither answers with a live record is dangling and has no
// entry.
//
// The two callers are the page's forward hop (expandReferents above) and the
// judge's evidence (judge.go judgeReferents), which share the cap: neither
// may turn one read into an unbounded one.
func (ds *dataset) loadReferents(ctx context.Context, x dbx, paths []string) (map[string]*substrate.Record, error) {
	byKind := map[string][]string{}
	var order []string
	seen := map[string]bool{}
	for _, path := range paths {
		kind, id, ok := vocabulary.SplitRecordPath(path)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		order = append(order, path)
		byKind[kind] = append(byKind[kind], id)
	}
	if len(order) == 0 {
		return nil, nil
	}
	if len(order) > maxExpanded {
		return nil, fmt.Errorf("%w: expanding would load %d referents, more than %d; read fewer records or expand fewer properties",
			substrate.ErrValidation, len(order), maxExpanded)
	}
	included := make(map[string]*substrate.Record, len(order))
	for _, kind := range sortedKeys(byKind) {
		b := &builder{}
		rows, err := x.QueryContext(ctx,
			`SELECT `+recordCols+` FROM records WHERE kind = `+b.arg(kind)+` AND id = ANY(`+b.textArray(byKind[kind])+`)`+
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
