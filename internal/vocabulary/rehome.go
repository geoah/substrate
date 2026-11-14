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

// LocalizeAuthority returns a copy of docs moved from the `from` authority into
// the repository's own namespace (decision record 0259).
//
// Copying a sample runs it with `from` the sample's authority, and it reads
// that name where the grammar says a string names something, as admitting
// the repository's own declarations reads the repository's name: a
// declaration's id and its name keys (LocalizeDeclaration), its `authority`,
// an actor's authority, and, in the two texts a copy's code reads (a
// function's `source` and `modules`, an agent's `prompt`), a spelled kind
// reference (LocalizeKindRefs). Every other occurrence stays as written: a
// URL's host, a port, an email address, a word in prose. The records a
// closure carries beside its declarations are left as shipped; the install
// folds them where the write reads a name (BundleInstall.Origin). The
// authority document of the sample's authority, which describes a
// publisher, is not carried: the local namespace has none.
//
// Admitting the repository's own declarations runs it with `from` its name
// (home): that name is another spelling of `~` only where a declaration
// names something, so only those keys move (LocalizeDeclaration), and the
// owner's text (a description, a prompt, a function's source, a default)
// stays as written. The repository's own authority document stays too, and
// a record a closure carries beside its declarations moves only its id
// (NamedID); the write folds its properties as it folds any record's.
//
// Two `requiresAtLeast` entries that become one package are refused, as
// LocalizeDeclaration refuses them.
func LocalizeAuthority(docs []map[string]any, from, home string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		if d["kind"] == CoreKind(DocAuthority) {
			if id, _ := mapPath(d, "metadata", "id").(string); id == from {
				if from == home {
					out = append(out, d)
				}
				continue
			}
		}
		if from == home {
			own, err := localizeOwnDocument(d, home)
			if err != nil {
				return nil, fmt.Errorf("admitting %s: %w", mapPathString(d, "metadata", "id"), err)
			}
			out = append(out, own)
			continue
		}
		copied, err := localizeCopiedDocument(d, from)
		if err != nil {
			return nil, fmt.Errorf("moving %s into %s: %w", from, Local, err)
		}
		out = append(out, copied)
	}
	return out, nil
}

// localizeOwnDocument is one of the repository's own documents with its name,
// home, folded into `~` where it names something: a declaration's id and its
// name keys (LocalizeDeclaration), another record's id where its kind is
// named by a reference.
func localizeOwnDocument(d map[string]any, home string) (map[string]any, error) {
	kind, _ := d["kind"].(string)
	out := make(map[string]any, len(d))
	for k, v := range d {
		out[k] = v
	}
	if meta, ok := d["metadata"].(map[string]any); ok {
		if id, ok := meta["id"].(string); ok {
			m := make(map[string]any, len(meta))
			for k, v := range meta {
				m[k] = v
			}
			m["id"] = LocalizeID(kind, id, home)
			out["metadata"] = m
		}
	}
	if _, declaration := declarationKinds[kind]; declaration {
		if data, ok := d["data"]; ok {
			localized, err := LocalizeDeclaration(kind, data, home)
			if err != nil {
				return nil, err
			}
			out["data"] = localized
		}
	}
	return out, nil
}

// mapPathString is mapPath's string, or "".
func mapPathString(d map[string]any, path ...string) string {
	s, _ := mapPath(d, path...).(string)
	return s
}

// declarationKinds are the kinds of a declaration document: what
// LocalizeDeclaration reads by the dialect's keys.
var declarationKinds = map[string]bool{
	CoreKind(DocAuthority): true, CoreKind(DocPackage): true, CoreKind(DocKind): true,
	CoreKind(DocTrait): true, CoreKind(DocPropertyType): true, CoreKind(DocRecordMapping): true,
	CoreKind(DocFunction): true, CoreKind(DocAgent): true, CoreKind(DocActor): true,
	CoreKind(DocBundle): true,
}

// localizeCopiedDocument is one document of a sample's closure, copied out of
// the sample's authority, from: its names folded as the repository's own
// documents fold the repository's name (localizeOwnDocument), and the kind
// references spelled in the code texts moved to `~`.
func localizeCopiedDocument(d map[string]any, from string) (map[string]any, error) {
	out, err := localizeOwnDocument(d, from)
	if err != nil {
		return nil, err
	}
	kind, _ := d["kind"].(string)
	texts, ok := copiedCodeTexts[kind]
	if !ok {
		return out, nil
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		return out, nil
	}
	copied := make(map[string]any, len(data))
	for k, v := range data {
		copied[k] = v
	}
	for _, key := range texts {
		if v, ok := data[key]; ok {
			if copied[key], err = walkStrings(v, func(s string) string { return LocalizeKindRefs(s, from) }); err != nil {
				return nil, err
			}
		}
	}
	out["data"] = copied
	return out, nil
}

// copiedCodeTexts are the texts a copied declaration's code reads by kind
// reference: a function's source and its modules, and an agent's prompt,
// which tells it the kinds to query. A copy moves the kind references spelled
// in them, and nothing else in them (LocalizeKindRefs).
var copiedCodeTexts = map[string][]string{
	CoreKind(DocFunction): {"source", "modules"},
	CoreKind(DocAgent):    {"prompt"},
}

// LocalizeKindRefs is text with every kind reference spelled under from
// moved to `~`: a whole mention of from followed by "/<package>/<name>",
// each a single lowercase word, with nothing of an address around it. A
// mention after "/", "@" or ":" (a URL's path, userinfo or an email address,
// a scheme or an actor), one followed by anything but "/" (a port, the end
// of a host), one with fewer than two words after it, and one whose name runs
// on into "/", "@", ":" or a name's characters (a full stop that ends a
// sentence is not one) stay as written.
func LocalizeKindRefs(text, from string) string {
	if from == "" || !strings.Contains(text, from) {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		at := strings.Index(text[i:], from)
		if at < 0 {
			b.WriteString(text[i:])
			break
		}
		at += i
		end := at + len(from)
		b.WriteString(text[i:at])
		if spelledKindRef(text, at, end) {
			b.WriteString(Local)
		} else {
			b.WriteString(from)
		}
		i = end
	}
	return b.String()
}

// spelledKindRef reports whether the mention at [at,end) of text is the
// authority of a kind reference standing on its own.
func spelledKindRef(text string, at, end int) bool {
	if !authorityBoundary(text, at, end) {
		return false
	}
	if at > 0 && strings.ContainsRune("/@:", rune(text[at-1])) {
		return false
	}
	i := end
	for range 2 {
		if i >= len(text) || text[i] != '/' {
			return false
		}
		n := lowerWord(text[i+1:])
		if n == 0 {
			return false
		}
		i += 1 + n
	}
	switch {
	case i == len(text):
		return true
	case text[i] == '.':
		// A sentence's full stop, not a name running on.
		return i+1 == len(text) || !isNameByte(text[i+1])
	default:
		return !isNameByte(text[i]) && !strings.ContainsRune("/@:", rune(text[i]))
	}
}

// lowerWord is the length of the single lowercase word s starts with: a
// letter, then letters and digits.
func lowerWord(s string) int {
	n := 0
	for n < len(s) && (s[n] >= 'a' && s[n] <= 'z' || n > 0 && s[n] >= '0' && s[n] <= '9') {
		n++
	}
	return n
}

// LocalizeDeclaration is the data of a declaration of kind (a declaration
// record kind, CoreKind(DocKind) and the rest) with the repository's name,
// home, folded into `~` where the dialect says a key names something
// (declarationPositions): a reference position, in every form the loader
// accepts there (a name, a pattern, a glob, a record path, a reference
// value's object, a list of any of them), `authority` when it is the name,
// and the packages `requiresAtLeast` keys. Every other value stays as
// written, whatever it looks like. The data of a kind no declaration has
// stays whole. Two `requiresAtLeast` entries that become one package are
// refused: the floors they set cannot both hold.
func LocalizeDeclaration(kind string, v any, home string) (any, error) {
	root, ok := declarationRoots[kind]
	if home == "" || !ok {
		return v, nil
	}
	return declarationWalker{home: home}.object(root, v)
}

// declarationWalker folds one repository's name through a declaration by
// the dialect's positions.
type declarationWalker struct{ home string }

// object walks v, an object of the key set keys or a list of them.
func (w declarationWalker) object(keys string, v any) (any, error) {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			var err error
			if out[i], err = w.object(keys, x); err != nil {
				return nil, err
			}
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			c, ok := classOf(keys, k)
			if !ok {
				// A key the dialect does not hold here: the loader refuses
				// it, and nothing in it is read as a name.
				out[k] = x
				continue
			}
			var err error
			switch c.pos {
			case posRef:
				out[k] = w.reference(x)
			case posAuthority:
				if s, ok := x.(string); ok && s == w.home {
					out[k] = Local
				} else {
					out[k] = w.reference(x)
				}
			case posPackages:
				out[k], err = w.keys(x)
			case posObject:
				out[k], err = w.object(c.keys, x)
			case posNamed:
				out[k], err = w.named(c.keys, x)
			default:
				out[k] = x
			}
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return v, nil
	}
}

// reference folds a value at a reference position: a name, a pattern or a
// record path under home (LocalizePath), a reference value's `ref`, beside
// link properties that stay, or a list of either.
func (w declarationWalker) reference(v any) any {
	switch t := v.(type) {
	case string:
		return LocalizePath(t, w.home)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = w.reference(x)
		}
		return out
	case map[string]any:
		r, ok := t[ReferenceValueKey].(string)
		if !ok {
			return v
		}
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = x
		}
		out[ReferenceValueKey] = LocalizePath(r, w.home)
		return out
	}
	return v
}

// named walks a map keyed by names the author chose: each key stays as
// written, and each value is an object of the key set keys.
func (w declarationWalker) named(keys string, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return v, nil
	}
	out := make(map[string]any, len(m))
	for k, x := range m {
		var err error
		if out[k], err = w.object(keys, x); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// keys is a map keyed by package identities, its keys folded. Two keys that
// fold to one package are refused rather than one of them dropped.
func (w declarationWalker) keys(v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return v, nil
	}
	out := make(map[string]any, len(m))
	from := make(map[string]string, len(m))
	for _, k := range sortedMapKeys(m) {
		key := LocalizeRef(k, w.home)
		if other, taken := from[key]; taken {
			return nil, fmt.Errorf("requiresAtLeast names %s twice, as %q and as %q: give it one floor", key, other, k)
		}
		from[key] = k
		out[key] = m[k]
	}
	return out, nil
}

// walkStrings rewrites every string in v, map keys included, failing where two
// keys of one map would become one.
func walkStrings(v any, rewrite func(string) string) (any, error) {
	switch t := v.(type) {
	case string:
		return rewrite(t), nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for _, k := range sortedMapKeys(t) {
			key := rewrite(k)
			if _, taken := out[key]; taken {
				return nil, fmt.Errorf("two keys become %q, so one would be dropped", key)
			}
			val, err := walkStrings(t[k], rewrite)
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			w, err := walkStrings(val, rewrite)
			if err != nil {
				return nil, err
			}
			out[i] = w
		}
		return out, nil
	default:
		return v, nil
	}
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
