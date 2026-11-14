package vocabulary

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Publishing one of the repository's own packages (decision record 0289)
// moves it out of the local namespace to the authority its owner proved:
// `~/recipes` becomes `ada.example.com/recipes`, and every name that pointed
// at it follows. It is the one move between authorities, and the repository
// that owns the kinds makes it; a publisher's kinds keep their names from then
// on (decision record 0269).

// PublishDocuments is the local namespace's documents with package pkg moved
// to the authority to. docs are every declaration document of `~`;
// spellings are the names that spell `~` here now (a name another of the
// repository's packages is published under is not one), which a declaration
// or a code text may write `~` with.
//
// The package's own documents move whole: an id, the declaring `authority`,
// every name at a name key, and a kind of the package
// spelled in a function's source and modules or an agent's prompt. Another
// package of the repository's own follows wherever it names the package: a
// reference pinned at one of its kinds, a grant, a requirement. The author's
// text stays as written, as it does when the local namespace admits it.
//
// It answers the documents that move or change, and a line for each name in
// the package's own documents of another package in `~`: a published
// package is installed where that package does not exist, so it is published
// first or the name goes.
func PublishDocuments(docs []map[string]any, pkg, to string, spellings []string) ([]map[string]any, []string, error) {
	homes := append([]string{Local}, spellings...)
	var out []map[string]any
	var problems []string
	for _, d := range docs {
		own := documentOf(d, pkg, homes)
		var others []string
		moved, err := publishDocument(d, pkg, to, homes, own, &others)
		if err != nil {
			return nil, nil, fmt.Errorf("publishing %s: %s: %w", pkg, mapPathString(d, "metadata", "id"), err)
		}
		for _, o := range others {
			problems = append(problems, fmt.Sprintf("%s %s names %s, another package of this repository's own: publish it first, or stop naming it",
				mapPathString(d, "kind"), mapPathString(moved, "metadata", "id"), o))
		}
		if own || !reflect.DeepEqual(moved, d) {
			out = append(out, moved)
		}
	}
	sort.Strings(problems)
	return out, slices.Compact(problems), nil
}

// documentOf reports whether d declares into the local package pkg.
func documentOf(d map[string]any, pkg string, homes []string) bool {
	a, _ := mapPath(d, "data", "authority").(string)
	p, _ := mapPath(d, "data", "package").(string)
	return slices.Contains(homes, a) && p == pkg
}

// publishDocument is d with pkg moved to to, collecting into others every
// other local package d names when d is one of pkg's own.
func publishDocument(d map[string]any, pkg, to string, homes []string, own bool, others *[]string) (map[string]any, error) {
	ref := func(s string) string {
		moved, other := publishRef(s, pkg, to, homes)
		if own && other != "" {
			*others = append(*others, other)
		}
		return moved
	}
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
			if NamedID(kind) {
				m["id"] = ref(id)
			}
			out["metadata"] = m
		}
	}
	data, ok := d["data"].(map[string]any)
	if !ok || !declarationKinds[kind] {
		return out, nil
	}
	// A record path names a kind and an id, each moved; any other name at a
	// reference position is one ref moves or leaves. The repository's own
	// authority, where the document is one of its own, becomes the
	// publishing one.
	walked, err := declarationWalker{
		fold: func(s string) string {
			if kind, id, ok := SplitRecordPath(s); ok {
				return RecordPath(ref(kind), ref(id))
			}
			return ref(s)
		},
		authority: func(s string) string {
			if own && slices.Contains(homes, s) {
				return to
			}
			return s
		},
	}.object(declarationRoots[kind], data)
	if err != nil {
		return nil, err
	}
	moved, _ := walked.(map[string]any)
	for _, key := range copiedCodeTexts[kind] {
		v, ok := data[key]
		if !ok {
			continue
		}
		if moved[key], err = walkStrings(v, func(s string) string {
			text, other := publishKindRefs(s, pkg, to, homes)
			if own {
				*others = append(*others, other...)
			}
			return text
		}); err != nil {
			return nil, err
		}
	}
	out["data"] = moved
	return out, nil
}

// publishRef is ref moved to to when it names pkg under one of homes (a
// package, a kind, a record path, a glob), and unchanged otherwise. other is
// the package ref names when that is another package under one of homes.
func publishRef(ref, pkg, to string, homes []string) (moved, other string) {
	for _, home := range homes {
		rest, ok := strings.CutPrefix(ref, home+"/")
		if !ok || home == "" {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		if name == pkg {
			return to + "/" + rest, ""
		}
		return ref, Local + "/" + name
	}
	return ref, ""
}

// PublishedRef is ref moved from the local package pkg to the authority to
// when it names that package (a package, a kind, a record path, a glob), and
// unchanged otherwise.
func PublishedRef(ref, pkg, to string) string {
	moved, _ := publishRef(ref, pkg, to, []string{Local})
	return moved
}

// publishKindRefs is text with every kind reference of pkg spelled under one
// of homes moved to to, by the rule LocalizeKindRefs reads a code text with,
// and the other local packages it names.
func publishKindRefs(text, pkg, to string, homes []string) (string, []string) {
	var others []string
	for _, home := range homes {
		if home == "" || !strings.Contains(text, home) {
			continue
		}
		var b strings.Builder
		for i := 0; i < len(text); {
			at := strings.Index(text[i:], home)
			if at < 0 {
				b.WriteString(text[i:])
				break
			}
			at += i
			end := at + len(home)
			b.WriteString(text[i:at])
			name := ""
			if spelledKindRef(text, at, end) {
				name = text[end+1 : end+1+lowerWord(text[end+1:])]
			}
			switch {
			case name == pkg:
				b.WriteString(to)
			case name != "":
				others = append(others, Local+"/"+name)
				b.WriteString(home)
			default:
				b.WriteString(home)
			}
			i = end
		}
		text = b.String()
	}
	return text, others
}
