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

// PublishAnswer is what a publish does to a name at one position: moves it
// by the publish's own name rule, stops the publish while it names the
// package, or leaves it as written, as history (decision record 0289).
type PublishAnswer string

const (
	PublishMoves   PublishAnswer = "moves"
	PublishStops   PublishAnswer = "stops"
	PublishHistory PublishAnswer = "history"
)

// publishDeclarationAnswers is the publish's answer at every position of a
// declaration that names something (positions.go). A position class with no
// answer fails TestEveryNamingPositionHasAPublishAnswer.
var publishDeclarationAnswers = map[position]PublishAnswer{
	posRef:       PublishMoves,
	posAuthority: PublishMoves,
	posPackages:  PublishMoves,
	posCode:      PublishMoves,
	// An agent's prompt is its author's prose: the publish leaves it, and
	// its preview lists every prompt that still names the package
	// (PublishTexts).
	posPrompt: PublishHistory,
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
	// Every position that names something, by its class's answer: a record
	// path names a kind and an id, each moved; any other name is one ref
	// moves or leaves; the repository's own authority, where the document is
	// one of its own, becomes the publishing one; a code text's kind
	// references move by the rule a copy moves them with. Another package of
	// the repository's own that names every package of `~` (`~/*`) keeps
	// naming this one beside it.
	w := declarationWalker{
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
		code: func(s string) string {
			text, other := publishKindRefs(s, pkg, to, homes)
			if own {
				*others = append(*others, other...)
			}
			return text
		},
	}
	if !own {
		w.beside = func(s string) string { return publishBeside(s, pkg, to, homes) }
	}
	walked, err := w.answering(publishDeclarationAnswers).object(declarationRoots[kind], data)
	if err != nil {
		return nil, err
	}
	out["data"] = walked
	return out, nil
}

// answering is w with each of its folds kept where answers moves the names
// of its position class and dropped where they stay as history. A class the
// publish stops on is answered by PublishDocuments' problems, never here.
func (w declarationWalker) answering(answers map[position]PublishAnswer) declarationWalker {
	keep := func(pos position) bool { return answers[pos] == PublishMoves }
	same := func(s string) string { return s }
	if !keep(posRef) {
		w.fold, w.beside = same, nil
	}
	if !keep(posAuthority) {
		w.authority = same
	}
	if !keep(posCode) {
		w.code = nil
	}
	if !keep(posPrompt) {
		w.prompt = nil
	}
	return w
}

// PublishTexts lists the declarations, as record paths, whose author's
// text the publish leaves as written (a class it answers as history: an
// agent's prompt) and which name the package pkg (NamesPackage): after the
// publish they name a package that is gone from `~`, so the preview says
// which, for their author to read.
func PublishTexts(docs []map[string]any, pkg, to string, spellings []string) []string {
	var out []string
	for _, d := range docs {
		kind, _ := d["kind"].(string)
		root, ok := declarationRoots[kind]
		if !ok {
			continue
		}
		named := false
		look := func(s string) string {
			named = named || NamesPackage(s, pkg, to, spellings)
			return s
		}
		w := declarationWalker{fold: func(s string) string { return s }, authority: func(s string) string { return s }}
		if publishDeclarationAnswers[posPrompt] == PublishHistory {
			w.prompt = look
		}
		if publishDeclarationAnswers[posCode] == PublishHistory {
			w.code = look
		}
		_, _ = w.object(root, d["data"])
		if named {
			out = append(out, RecordPath(kind, mapPathString(d, "metadata", "id")))
		}
	}
	sort.Strings(out)
	return out
}

// NamesPackage reports whether v names the local package pkg: whether a
// string in it, or a key of a map in it, folded as a door folds a name
// (each of spellings to `~`), holds `~/<pkg>` whole or followed by `/`, at
// a name boundary inside a text. A spelling equal to to, the authority pkg
// is published under, names the published package and is skipped. It is
// the one test of a publish's preview listing and of its stops on a record
// still to be acted on.
func NamesPackage(v any, pkg, to string, spellings []string) bool {
	switch x := v.(type) {
	case string:
		return mentionsPackage(x, pkg, to, spellings)
	case []any:
		for _, e := range x {
			if NamesPackage(e, pkg, to, spellings) {
				return true
			}
		}
	case map[string]any:
		for k, e := range x {
			if mentionsPackage(k, pkg, to, spellings) || NamesPackage(e, pkg, to, spellings) {
				return true
			}
		}
	}
	return false
}

// mentionsPackage is NamesPackage for one string.
func mentionsPackage(s, pkg, to string, spellings []string) bool {
	for _, home := range append([]string{Local}, spellings...) {
		if home == "" || home == to {
			continue
		}
		name := home + "/" + pkg
		for i := 0; i < len(s); {
			at := strings.Index(s[i:], name)
			if at < 0 {
				break
			}
			at += i
			if packageMentionAt(s, at, at+len(name)) {
				return true
			}
			i = at + 1
		}
	}
	return false
}

// packageMentionAt reports whether the package name at [at,end) of s
// stands on its own: no name runs into it, and after it comes the end, a
// `/`, or anything that does not continue a name (a sentence's full stop
// included).
func packageMentionAt(s string, at, end int) bool {
	if at > 0 && (isNameByte(s[at-1]) || s[at-1] == '~') {
		return false
	}
	switch {
	case end == len(s), s[end] == '/':
		return true
	case s[end] == '.':
		return end+1 == len(s) || !isNameByte(s[end+1])
	}
	return !isNameByte(s[end])
}

// publishBeside is the published package's glob, to add beside a pattern
// over every package under one of homes (`~/*`), and "" for any other
// name: such a pattern reached the package before the publish, and keeps
// reaching it after.
func publishBeside(s, pkg, to string, homes []string) string {
	for _, home := range homes {
		if home != "" && s == home+"/*" {
			return to + "/" + pkg + "/*"
		}
	}
	return ""
}

// PublishedBeside is publishBeside for the substrate's own records, whose
// names are stored under `~`.
func PublishedBeside(s, pkg, to string) string { return publishBeside(s, pkg, to, []string{Local}) }

// PublishedName is a name moved from the local package pkg to the authority
// to, as a declaration's names move: a record path's kind and its id each,
// any other name whole (PublishedRef).
func PublishedName(s, pkg, to string) string {
	if kind, id, ok := SplitRecordPath(s); ok {
		return RecordPath(PublishedRef(kind, pkg, to), PublishedRef(id, pkg, to))
	}
	return PublishedRef(s, pkg, to)
}

// PublishedCode is a code text with the kind references it spells of the
// local package pkg, under `~` or one of spellings, moved to the authority
// to, by the rule a declaration's code moves by (publishKindRefs).
func PublishedCode(text, pkg, to string, spellings ...string) string {
	moved, _ := publishKindRefs(text, pkg, to, append([]string{Local}, spellings...))
	return moved
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

// LocalNamesIn lists every name in the local namespace that a published
// closure's declarations spell: at any position that names something, and
// as a kind reference in a code text. A published closure is installed in
// another repository, where `~` is that repository's own, so an installing
// server refuses one that names it.
func LocalNamesIn(docs []map[string]any) []string {
	var found []string
	note := func(s string) string {
		if s == Local || IsLocal(s) {
			found = append(found, s)
		} else if kind, _, ok := SplitRecordPath(s); ok && IsLocal(kind) {
			found = append(found, s)
		}
		return s
	}
	for _, d := range docs {
		kind, _ := d["kind"].(string)
		root, ok := declarationRoots[kind]
		if !ok {
			continue
		}
		_, _ = declarationWalker{
			fold: note, authority: note,
			code: func(s string) string {
				_, named := publishKindRefs(s, "", "", []string{Local})
				found = append(found, named...)
				return s
			},
		}.object(root, d["data"])
	}
	sort.Strings(found)
	return slices.Compact(found)
}

// PackagesNamedIn lists the packages under an authority (not `~`) that
// docs' declarations name at a position that names something: a reference
// pin, a grant, a requirement, a reference value at a declaration. An
// installing server must hold each before the closure compiles, so a
// publish that writes a package's bundle requires them.
func PackagesNamedIn(docs []map[string]any) []string {
	var found []string
	note := func(s string) string {
		if pkg := namedPackage(s); pkg != "" {
			found = append(found, pkg)
		}
		return s
	}
	for _, d := range docs {
		kind, _ := d["kind"].(string)
		root, ok := declarationRoots[kind]
		if !ok {
			continue
		}
		_, _ = declarationWalker{fold: note, authority: func(s string) string { return s }}.object(root, d["data"])
	}
	sort.Strings(found)
	return slices.Compact(found)
}

// namedPackage is the package a name names: a declaration's record path by
// its id, a record path by its kind, and any other name, a kind, a
// callable, a package or a pattern under one, by its first two segments;
// "" for one under `~`, a bare word, or a pattern over a whole authority.
func namedPackage(name string) string {
	if kind, id, ok := SplitRecordPath(name); ok {
		if NamedID(kind) {
			name = id
		} else {
			name = kind
		}
	}
	authority, rest, ok := strings.Cut(name, "/")
	if !ok || authority == Local || !strings.Contains(authority, ".") {
		return ""
	}
	pkg, _, _ := strings.Cut(rest, "/")
	if pkg == "" || pkg == "*" {
		return ""
	}
	return authority + "/" + pkg
}
