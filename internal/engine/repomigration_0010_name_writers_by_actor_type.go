package engine

// Repository migration 10: every stored declaration's `writer:` names types of
// actor, and sits on a property alone.
//
// Decision record 0290 made a property's `writer:` name the types of actor
// that may write it, and retired the three words it took before: `owner` is
// `person`, `connector` is `[function, agent]`, `oauth` is `system`. The
// loader this binary carries refuses each old word, so a repository holding a
// declaration that still says one, its own or an installed provider's, would
// park that package at its first open. This rewrites them, once, before the
// stored vocabulary loads, by the fixed rule, wherever a property or a
// reference declares one. A field of an object takes none from dialect 5,
// since the write path holds the property's value whole and never read one,
// so one stored on a field is removed, which changes nothing a write did.
//
// Every stored kind and trait declaration is rewritten, live or tombstoned,
// whatever its package's admission: a package parked for another reason
// would otherwise keep the old word, and repairing that reason would meet a
// second one no migration fixes. Each row is rewritten in place through the
// fold, as an ordinary record write, keeping its tombstone and its version.
// An imported sample's origin digest is stamped again over its rewritten
// rows only where the copy still matched its stamp before the rewrite, as
// migration 3 does, so a copy its owner edited still reads as edited and a
// re-import still asks before it replaces the edits. Nothing a `writer:`
// decides is a projection, so no refs row or search band moves.

import (
	"fmt"
	"maps"

	"github.com/geoah/substrate/internal/vocabulary"
)

// rewrittenWriter is the fixed rule from an old word to actor types, a fresh
// value each call so no two rows share a list.
func rewrittenWriter(word string) (any, bool) {
	switch word {
	case "owner":
		return "person", true
	case "connector":
		return []any{"function", "agent"}, true
	case "oauth":
		return "system", true
	}
	return nil, false
}

// migrateNameWritersByActorType is the migration's Run.
func migrateNameWritersByActorType(t *txn) (string, error) {
	rows, err := t.query(`SELECT kind, id FROM records WHERE kind IN ($1, $2) ORDER BY kind, id`, kindKind, kindTrait)
	if err != nil {
		return "", err
	}
	var refs []eref
	for rows.Next() {
		var ref eref
		if err := rows.Scan(&ref.Kind, &ref.ID); err != nil {
			_ = rows.Close()
			return "", err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return "", err
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	// pristine is read for a package before the first of its rows moves.
	pristine := map[string]bool{}
	changed := map[string]int{}
	for _, ref := range refs {
		row, err := t.loadRow(ref, true)
		if err != nil {
			return "", err
		}
		if row == nil {
			continue
		}
		next, n := rewriteWriters(copyMapDeep(row.Props))
		if n == 0 {
			continue
		}
		authority, _ := row.Props["authority"].(string)
		name, _ := row.Props["package"].(string)
		pkg := authority + "/" + name
		if _, read := pristine[pkg]; !read {
			if pristine[pkg], err = t.closureMatchesOrigin(pkg); err != nil {
				return "", err
			}
		}
		if err := t.migrateRow(ref, next, false); err != nil {
			return "", fmt.Errorf("rewrite %s: %w", vocabulary.RecordPath(ref.Kind, ref.ID), err)
		}
		changed[pkg] += n
	}
	if len(changed) == 0 {
		return "no stored declaration names a writer by an old word or on a field", nil
	}
	words := 0
	for _, pkg := range sortedKeys(changed) {
		if pristine[pkg] {
			if err := t.restampOrigin(pkg); err != nil {
				return "", err
			}
		}
		words += changed[pkg]
	}
	return fmt.Sprintf("named %d writers by actor type across %d packages", words, len(changed)), nil
}

// rewriteWriters rewrites the `writer:` an old word fills on each property a
// declaration's data declares, and removes the one a field of an object
// property carries, at any depth. It walks the definitions by their places,
// so a property or a field named `writer` is still a definition and a default
// value is never read as one. It answers the data, copied where it changed,
// and how many it changed.
func rewriteWriters(data map[string]any) (map[string]any, int) {
	props, ok := data["properties"].(map[string]any)
	if !ok {
		return data, 0
	}
	next, n := rewriteDefinitions(props, false)
	if n == 0 {
		return data, 0
	}
	out := maps.Clone(data)
	out["properties"] = next
	return out, n
}

// rewriteDefinitions rewrites each definition of a map of names to
// definitions: a kind's or a trait's properties, or an object's fields where
// fields is set. A shorthand, a bare type name, declares no writer.
func rewriteDefinitions(defs map[string]any, fields bool) (map[string]any, int) {
	var out map[string]any
	n := 0
	for name, raw := range defs {
		def, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		next, m := rewriteDefinition(def, fields)
		if m == 0 {
			continue
		}
		if out == nil {
			out = maps.Clone(defs)
		}
		out[name] = next
		n += m
	}
	if out == nil {
		return defs, 0
	}
	return out, n
}

// rewriteDefinition rewrites one definition's own `writer:`, or removes it
// from a field, and does the same for its fields.
func rewriteDefinition(def map[string]any, field bool) (map[string]any, int) {
	var out map[string]any
	n := 0
	if _, has := def["writer"]; has && field {
		out = maps.Clone(def)
		delete(out, "writer")
		n++
	} else if word, ok := def["writer"].(string); ok {
		if to, old := rewrittenWriter(word); old {
			out = maps.Clone(def)
			out["writer"] = to
			n++
		}
	}
	if fields, ok := def["fields"].(map[string]any); ok {
		if next, m := rewriteDefinitions(fields, true); m > 0 {
			if out == nil {
				out = maps.Clone(def)
			}
			out["fields"] = next
			n += m
		}
	}
	if out == nil {
		return def, 0
	}
	return out, n
}
