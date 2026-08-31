package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// THE REFS INDEX.
//
// A reference is a VALUE in the source record's own properties, and the `refs`
// table is the reverse projection of those values: which record points at
// which, from which site. It is derived storage, exactly as `records` is
// derived from the changelog — `deriveRefs` computes a record's whole row set
// from (folded properties, kind declaration) and `syncRefs` makes the stored
// rows equal that set. Nothing else writes the table.
//
// That is why there is no fold effect for it. A record's properties are in the
// delta the changelog carries; the index is a pure function of them, so a
// replay that reproduces the properties reproduces the index, and an effect
// describing the rows would be a second statement of the same fact, free to
// disagree with the first.
//
// ADDRESSING. See migration 0010 for the column contract. `property` is the
// kind's own top-level name, `path` the value address below it (dots joining
// object field names, list indices and keyed-map keys), `ord` the index inside
// a repeated reference. A single top-level reference is (name, "", 0).

// refRow is one derived reference: where it sits in the source record, what it
// points at, and the link data written beside it.
type refRow struct {
	Property string
	Path     string
	Ord      int
	Dst      eref
	// Props is the reference's declared link data (`properties:` on the
	// declaration), nil where it declares none.
	Props map[string]any
}

// key addresses the row inside its source record — the primary key's tail, and
// what a re-derive replaces.
func (r refRow) key() [3]string { return [3]string{r.Property, r.Path, strconv.Itoa(r.Ord)} }

// identity is what created_at is preserved across: the SITE and the TARGET,
// deliberately without `ord`. Reordering a repeated reference moves a value
// between ordinals without the record pointing anywhere new, and stamping a
// fresh creation on it would say the pointer was just made.
func (r refRow) identity() [4]string {
	return [4]string{r.Property, r.Path, r.Dst.Kind, r.Dst.ID}
}

// deriveRefs is the one function from a record's stored state to its rows in
// the refs index. PURE: no registry lookups beyond the declaration it is
// handed, no database, no clock. The live write path and the rebuild both call
// it, so the index a replay produces is the index the write produced.
//
// It reaches every SITE a reference is declared at, not only a kind's own
// properties: inside an object, inside a repeated object's elements, inside a
// keyed map's values, to the declared depth. A site it did not reach would be
// a pointer no reverse read could see.
//
// A value that is not a well-formed record path yields no row rather than a
// kindless one. Coercion and normalizeReference refuse those at the write, so
// reaching one here means a row predates a declaration change, and the index
// says nothing about it rather than something wrong.
func deriveRefs(ty *vocabulary.Kind, props map[string]any) []refRow {
	if ty == nil {
		return nil
	}
	var out []refRow
	for _, name := range ty.PropOrder {
		p := ty.Props[name]
		if !holdsReference(p) {
			continue
		}
		out = appendRefRows(out, name, nil, p, props[name])
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].key(), out[j].key()
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		return out[i].Ord < out[j].Ord
	})
	return out
}

// appendRefRows walks ONE declared site's value in its declared container. The
// `path` accumulator is the value address below the top-level property, which
// is why a list index and a map key are segments of it: two elements of a
// repeated object each hold their own copy of the same declared field, and an
// address that could not tell them apart would collide in the primary key.
func appendRefRows(out []refRow, property string, path []string, p *vocabulary.Property, v any) []refRow {
	if v == nil {
		return out
	}
	switch {
	case p.Keyed:
		m, ok := v.(map[string]any)
		if !ok {
			return out
		}
		for _, key := range sortedKeys(m) {
			out = appendRefValue(out, property, append(path, key), p, m[key], 0)
		}
		return out
	case p.Repeated:
		list, ok := v.([]any)
		if !ok {
			return out
		}
		for i, item := range list {
			// A repeated REFERENCE addresses its elements by `ord`; a repeated
			// OBJECT addresses them by a path segment, because the references
			// are inside the elements and each element holds a whole site set.
			if p.Datatype == vocabulary.DatatypeReference {
				out = appendRefValue(out, property, path, p, item, i)
				continue
			}
			out = appendRefValue(out, property, append(path, strconv.Itoa(i)), p, item, 0)
		}
		return out
	}
	return appendRefValue(out, property, path, p, v, 0)
}

// appendRefValue handles ONE value: the reference itself, or an object whose
// declared fields are walked in turn.
func appendRefValue(out []refRow, property string, path []string, p *vocabulary.Property, v any, ord int) []refRow {
	if v == nil {
		return out
	}
	if p.Datatype == vocabulary.DatatypeReference {
		dst, props, ok := splitReferenceValue(v)
		if !ok {
			return out
		}
		return append(out, refRow{
			Property: property, Path: strings.Join(path, "."), Ord: ord,
			Dst: dst, Props: props,
		})
	}
	m, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for _, fname := range sortedKeys(m) {
		f, declared := p.Fields[fname]
		if !declared || !holdsReference(f) {
			continue
		}
		out = appendRefRows(out, property, append(path, fname), f, m[fname])
	}
	return out
}

// splitReferenceValue reads a stored reference value in EITHER shape: the flat
// path a declaration without link data stores, or the `{ref, <props>...}`
// object a declaration with `properties:` stores. It is the one reader of the
// two shapes, so nothing else has to know which a declaration carries.
func splitReferenceValue(v any) (eref, map[string]any, bool) {
	switch t := v.(type) {
	case string:
		kind, id, ok := vocabulary.SplitRecordPath(t)
		if !ok {
			return eref{}, nil, false
		}
		return eref{Kind: kind, ID: id}, nil, true
	case map[string]any:
		path, _ := t[vocabulary.ReferenceValueKey].(string)
		kind, id, ok := vocabulary.SplitRecordPath(path)
		if !ok {
			return eref{}, nil, false
		}
		var props map[string]any
		for _, k := range sortedKeys(t) {
			if k == vocabulary.ReferenceValueKey {
				continue
			}
			if props == nil {
				props = map[string]any{}
			}
			props[k] = t[k]
		}
		return eref{Kind: kind, ID: id}, props, true
	}
	return eref{}, nil, false
}

// referencePathOf reads a stored reference value as the referent's record path,
// whichever shape it carries. "" when the value is not a reference.
func referencePathOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s, _ := t[vocabulary.ReferenceValueKey].(string)
		return s
	}
	return ""
}

// syncRefs makes the stored index equal what the record's properties now say.
// Called inside the write transaction after the fold, so the index and the row
// it projects commit together or not at all.
//
// CREATED_AT SURVIVES A RE-DERIVE. A record whose declaration or whose other
// properties changed re-derives its whole row set, and a pointer that came
// through unchanged must keep the moment it was first written: the reverse
// view orders by it, and re-stamping would make every reference look as old as
// the last unrelated edit. The match is on (property, path, dst) and NOT on
// `ord`, so reordering a repeated reference keeps each target's own creation.
func (t *txn) syncRefs(ref eref, ty *vocabulary.Kind, props map[string]any) error {
	want := deriveRefs(ty, props)
	born, err := t.refCreatedAt(ref)
	if err != nil {
		return err
	}
	if _, err := t.exec(`DELETE FROM refs WHERE src_kind = $1 AND src = $2`, ref.Kind, ref.ID); err != nil {
		return fmt.Errorf("substrate/engine: refs of %s %s: %w", ref.Kind, ref.ID, err)
	}
	for _, r := range want {
		at, had := born[r.identity()]
		if !had {
			at = t.now
		}
		raw, err := json.Marshal(nonNilMap(r.Props))
		if err != nil {
			return err
		}
		if _, err := t.exec(`
			INSERT INTO refs (src_kind, src, property, path, ord, dst_kind, dst, props, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`,
			ref.Kind, ref.ID, r.Property, r.Path, r.Ord, r.Dst.Kind, r.Dst.ID, raw, at); err != nil {
			return fmt.Errorf("substrate/engine: refs of %s %s (%s): %w", ref.Kind, ref.ID, r.Property, err)
		}
	}
	return nil
}

// refCreatedAt reads what the record's current rows were born at, keyed by the
// identity a re-derive preserves. The EARLIEST wins where two ordinals of one
// repeated reference name the same record: the pointer has existed since the
// first of them.
func (t *txn) refCreatedAt(ref eref) (map[[4]string]time.Time, error) {
	rows, err := t.query(`SELECT property, path, dst_kind, dst, created_at FROM refs
		WHERE src_kind = $1 AND src = $2`, ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[[4]string]time.Time{}
	for rows.Next() {
		var k [4]string
		var at time.Time
		if err := rows.Scan(&k[0], &k[1], &k[2], &k[3], &at); err != nil {
			return nil, err
		}
		at = at.UTC()
		if cur, had := out[k]; !had || at.Before(cur) {
			out[k] = at
		}
	}
	return out, rows.Err()
}

// syncRefsOf re-derives one record's rows from what is STORED, resolving the
// kind itself. The door for every caller that changed a record without holding
// its coerced property map: the vocabulary projection, and the rebuild.
func (t *txn) syncRefsOf(ref eref) error {
	row, err := t.loadRow(ref, false)
	if err != nil || row == nil {
		return err
	}
	// A kind this binary no longer declares has no sites to walk, so its rows
	// go rather than being left as a projection of a declaration nothing reads.
	// `writeReg` is what makes this correct DURING a vocabulary apply: the
	// candidate declarations are what the committed rows must project against,
	// and the live registry does not hold them until the publish.
	ty, _ := t.declarations().ByIdentity(row.Kind)
	return t.syncRefs(ref, ty, row.Props)
}

// declarations is the registry this transaction's writes are held to: the
// candidate closure while a vocabulary apply is in flight, the live registry
// otherwise.
func (t *txn) declarations() *vocabulary.Registry {
	if t.writeReg != nil {
		return t.writeReg
	}
	return t.ds.registry()
}

// syncRefsOfKind re-derives every record of one kind. Its caller is the
// vocabulary write path: a declaration that adds, drops or re-points a
// reference changes what the same stored properties project to, and an index
// left alone would answer for a declaration that is gone.
func (t *txn) syncRefsOfKind(ident string) error {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 ORDER BY id`, ident)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, id := range ids {
		if err := t.syncRefsOf(eref{Kind: ident, ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// idsOf is the canonical id plus every id this record used to live under — the
// txn-scoped twin of the dataset reader. A reference value keeps whatever id was
// written, so a reverse lookup that asked only for the canonical one would miss
// every pointer older than a merge.
func (t *txn) idsOf(ref eref) ([]string, error) {
	ids := []string{ref.ID}
	rows, err := t.query(`SELECT former_id FROM former_ids WHERE record_kind = $1 AND record_id = $2 ORDER BY former_id`,
		ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var former string
		if err := rows.Scan(&former); err != nil {
			return nil, err
		}
		ids = append(ids, former)
	}
	return ids, rows.Err()
}
