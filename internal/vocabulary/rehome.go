package vocabulary

import (
	"fmt"
	"sort"
	"strings"
)

// Rehoming a closure: the one walk that moves a set of decoded documents from
// one authority to another. It is what a SAMPLE import runs before the
// documents reach ordinary admission (decision record 0048), since a sample is
// authored under a placeholder authority and lands under the repository's own,
// and what `substratectl apply -f --as` runs client-side over the same files.
//
// It walks the DECODED documents rather than the bytes, so nothing depends on
// how a manifest was quoted, folded or ordered, and every string in them is
// rewritten: an id, a declared `authority`, a reference pin, an entry in
// `installs`/`requires`/`writes`, a trigger selector, a mapping's `from`/`to`,
// an actor, and the authority a function's source or an agent's instruction
// spells inside its own text. A closure whose prose still named the placeholder
// would tell the reader to go and write a kind that does not exist.

// RehomeAuthority returns a copy of docs with every mention of the `from`
// authority replaced by `to`. The originals are untouched: the catalog holds
// one parsed copy of each shipped closure and serves every repository from it.
//
// A mention is the authority as a whole name, bounded by something that cannot
// continue a DNS name, so `samples.substrate.reamde.dev` inside
// `notsamples.substrate.reamde.dev` or `samples.substrate.reamde.dev.example`
// is left alone. [AuthorityMentions] applies the same boundary, so what the
// walk leaves is never what the refusal reports.
//
// It fails on a KEY COLLISION: a map holding both a key that mentions `from`
// and the key it would be rewritten to has two entries for one name, and
// writing them into one map would silently drop whichever landed first. That
// is a closure to fix, not a document to admit.
func RehomeAuthority(docs []map[string]any, from, to string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		rehomed, err := rehomeValue(d, from, to)
		if err != nil {
			return nil, err
		}
		m, _ := rehomed.(map[string]any)
		out = append(out, m)
	}
	return out, nil
}

// AuthorityMentions names the documents that still mention an authority, by
// `metadata.id` (or their kind where a document carries no id). The import
// refuses on a non-empty answer: a document that reached admission still
// spelling the placeholder would declare vocabulary under an authority the
// repository does not own.
func AuthorityMentions(docs []map[string]any, authority string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range docs {
		if !mentionsAuthority(d, authority) {
			continue
		}
		name, _ := mapPath(d, "metadata", "id").(string)
		if name == "" {
			name, _ = d["kind"].(string)
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func rehomeValue(v any, from, to string) (any, error) {
	switch t := v.(type) {
	case string:
		return replaceAuthority(t, from, to), nil
	case map[string]any:
		out := make(map[string]any, len(t))
		// Sorted, so a collision is reported against the same key whichever
		// order the map happens to iterate in.
		for _, k := range sortedMapKeys(t) {
			// A key carries an authority where a map is keyed by kind
			// reference (a closure's kindDescriptions, a mapping's rules).
			key := replaceAuthority(k, from, to)
			if _, taken := out[key]; taken {
				return nil, fmt.Errorf("rehoming %s to %s: two keys become %q (%q is already there), so one would be dropped",
					from, to, key, key)
			}
			val, err := rehomeValue(t[k], from, to)
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			rehomed, err := rehomeValue(val, from, to)
			if err != nil {
				return nil, err
			}
			out[i] = rehomed
		}
		return out, nil
	default:
		return v, nil
	}
}

// mentionsAuthority applies the SAME boundary test the rewrite does, so a
// string the walk deliberately left alone is not then reported as a mention:
// `notsamples.substrate.reamde.dev` is another authority's name, not this one's.
func mentionsAuthority(v any, authority string) bool {
	switch t := v.(type) {
	case string:
		return namesAuthority(t, authority)
	case map[string]any:
		for k, val := range t {
			if namesAuthority(k, authority) || mentionsAuthority(val, authority) {
				return true
			}
		}
	case []any:
		for _, val := range t {
			if mentionsAuthority(val, authority) {
				return true
			}
		}
	}
	return false
}

// namesAuthority reports whether s spells `authority` as a whole name at least
// once.
func namesAuthority(s, authority string) bool {
	if authority == "" {
		return false
	}
	for i := 0; ; {
		at := strings.Index(s[i:], authority)
		if at < 0 {
			return false
		}
		at += i
		end := at + len(authority)
		if authorityBoundary(s, at, end) {
			return true
		}
		i = end
	}
}

// replaceAuthority rewrites every WHOLE mention of `from` in s. The boundary
// test is the one an authority has: a mention neither continues into another
// label nor is continued from one.
func replaceAuthority(s, from, to string) string {
	if from == "" || !strings.Contains(s, from) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		at := strings.Index(s[i:], from)
		if at < 0 {
			b.WriteString(s[i:])
			break
		}
		at += i
		end := at + len(from)
		b.WriteString(s[i:at])
		if authorityBoundary(s, at, end) {
			b.WriteString(to)
		} else {
			b.WriteString(from)
		}
		i = end
	}
	return b.String()
}

// authorityBoundary reports whether the mention at [at,end) in s stands on its
// own rather than inside a longer name.
func authorityBoundary(s string, at, end int) bool {
	if at > 0 && isNameByte(s[at-1]) {
		return false
	}
	if end < len(s) && isNameByte(s[end]) {
		return false
	}
	return true
}

// isNameByte reports whether b may continue a DNS-style authority name.
func isNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '-', b == '_':
		return true
	}
	return false
}

// mapPath reads a nested map value by key path, or nil.
func mapPath(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func sortedMapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
