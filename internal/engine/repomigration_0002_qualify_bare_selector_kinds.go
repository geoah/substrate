package engine

// Repository migration 2: every stored trigger source and policy selector
// names its kinds in full.
//
// Before decision record 0101 the engine resolved a bare word in a trigger's
// `source.record.kinds` and a policy's `selector.kinds` at load, to the one
// kind anywhere carrying it. That resolution is gone: the matcher compares a
// pattern against the changelog row's identity, so a row stored bare would
// now match nothing. A trigger that never fires is a liveness bug and a policy
// that never matches is an open door, and the policy's is silent. This
// rewrites each bare entry, once, to the identity the previous binary
// resolved it to, so what the row meant is what it now says.
//
// The old rule is reproduced from the stored declaration rows alone, the way
// migration 1 reproduces its own: the one kind anywhere carrying the word,
// with no preference for any package, because a trigger or policy row belongs
// to none. A word with two candidates or none is left as it is and logged:
// the previous binary resolved it to nothing either, so the row matched
// nothing before and matches nothing after, and the trigger load reports it.
//
// The rows are rewritten through the fold, as ordinary record writes: the row
// and the changelog entry describing it land together, so a rebuild
// reproduces the rewrite. No manager row moves: the spelling is the engine's,
// not a writer's opinion, and a pin at the machine tier would take the
// property out of the owner's hand.

import (
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// selectorSite is one stored kind list the old load-time resolution read: the
// record kind carrying it and the path to the list inside the row's
// properties.
type selectorSite struct {
	kind string
	path []string
}

var selectorSites = []selectorSite{
	{kind: typeTrigger, path: []string{"source", "record", "kinds"}},
	{kind: vocabulary.KindRecordPatchPolicy, path: []string{"selector", "kinds"}},
}

func migrateQualifyBareSelectorKinds(t *txn) (string, error) {
	ds := t.ds
	docs, _, err := ds.storedDocumentsBySource(t.ctx, nil)
	if err != nil {
		return "", err
	}
	ix := indexDeclaredNames(docs)
	var rows, words, left int
	for _, site := range selectorSites {
		ids, err := t.liveIDs(site.kind)
		if err != nil {
			return "", err
		}
		for _, id := range ids {
			row, err := t.loadRow(eref{Kind: site.kind, ID: id}, true)
			if err != nil {
				return "", err
			}
			if row == nil {
				continue
			}
			list, ok := listAt(row.Props, site.path)
			if !ok {
				continue
			}
			qualified, n, bare := qualifySelectorKinds(list, ix)
			for _, word := range bare {
				ds.svc.log.Warn("substrate: a repository migration left a stored selector's bare kind as it is, because no single declared kind carries the word; the row matched nothing before and matches nothing now",
					"repository", logSafeID(ds.info.ID), "record", logSafeID(vocabulary.RecordPath(site.kind, id)), "word", logSafeText(word))
			}
			left += len(bare)
			if n == 0 {
				continue
			}
			before := row.clone()
			row.Props = withListAt(row.Props, site.path, qualified)
			res, err := t.foldRow(before, row, false, false)
			if err != nil {
				return "", err
			}
			if !res.changed {
				continue
			}
			if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, id, site.kind,
				map[string]any{"properties": []string{site.path[0]}}); err != nil {
				return "", err
			}
			rows++
			words += n
		}
	}
	if rows == 0 && left == 0 {
		return "no bare kind in any stored trigger source or policy selector", nil
	}
	return fmt.Sprintf("wrote the full identity into %d bare kinds across %d trigger and policy rows, and left %d that no single kind carries", words, rows, left), nil
}

// qualifySelectorKinds rewrites each bare pattern in a selector's kind list to
// the one declared kind carrying the word, the rule the previous binary
// applied at load. A glob and a full reference are left alone, and so is a
// word with no single candidate, which is returned so the caller can say so.
func qualifySelectorKinds(list []any, ix declaredNames) (out []any, changed int, bare []string) {
	out = make([]any, 0, len(list))
	for _, v := range list {
		pat, isString := v.(string)
		if !isString || pat == "*" || strings.HasSuffix(pat, "/*") || vocabulary.Qualified(pat) || !vocabulary.ValidTypeGlob(pat) {
			out = append(out, v)
			continue
		}
		if full := ix.uniqueKind(pat); full != "" {
			out = append(out, full)
			changed++
			continue
		}
		out = append(out, v)
		bare = append(bare, pat)
	}
	return out, changed, bare
}

// liveIDs lists the live rows of one kind, collected before any write so no
// cursor is open on the transaction's connection while it writes.
func (t *txn) liveIDs(kind string) ([]string, error) {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// listAt reads the list at path inside props, or reports there is none.
func listAt(props map[string]any, path []string) ([]any, bool) {
	var cur any = props
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	list, ok := cur.([]any)
	return list, ok
}

// withListAt returns props with the list at path replaced, copying every map
// along the path so the row loaded before the write, which shares the nested
// maps, still reads as it was and the fold sees the difference.
func withListAt(props map[string]any, path []string, list []any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = v
	}
	if len(path) == 1 {
		out[path[0]] = list
		return out
	}
	inner, _ := props[path[0]].(map[string]any)
	out[path[0]] = withListAt(inner, path[1:], list)
	return out
}
