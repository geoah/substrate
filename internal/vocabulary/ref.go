package vocabulary

import (
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// THE KIND REFERENCE GRAMMAR.
//
// A stored, addressable kind is named by a qualified reference:
//
//	<authority>/<package>/<name>   "samples.substrate.reamde.dev/tasks/task"
//
// Every kind carries an authority and a package (decisions 0042, 0047): the
// authority is a DNS name and therefore always carries a dot, the package and
// the name never do, and none of the three can carry a "/". A bare name
// (`task`) is not a stored identity but load-time SHORTHAND — a reference
// `kind:` pin, a trigger source, a permission allowlist entry — that resolves
// against the declaring PACKAGE to a qualified identity before it is stored or
// addressed. The helpers below still render and split the bare form because
// that shorthand relies on them; nothing stores it.
//
// A RECORD PATH is the qualified kind reference plus the id:
// "<authority>/<package>/<kind>/<id>". It is what a `reference` property
// points with, and it sits under ReferenceValueKey inside the object that IS
// the stored value (decision 0044). A bare path string is accepted at the
// write as shorthand and normalized to that object; nothing stores it and
// nothing serves it.

// ReferenceValueKey is the one reserved key of a reference value: `ref` holds
// the referent's record path and every other key is a declared link property.
// It is reserved in the declaration too, so a link property can never shadow
// the pointer itself.
const ReferenceValueKey = "ref"

// ReferenceTargetField is the second reserved link-property name. It is not a
// key of the stored value: the GraphQL object generated for a reference carries
// `ref` (the path) beside `target` (the referent record itself), and the
// declared link properties are written into that same object. A link property
// spelled `target` would replace the referent field with its own value, so it is
// refused in the declaration, where the author can rename it.
const ReferenceTargetField = "target"

// KindRef renders a kind reference from its parts. An empty authority and
// package render the bare shorthand form — there is no `local/` prefix.
func KindRef(authority, pkg, name string) string {
	if authority == "" && pkg == "" {
		return name
	}
	return authority + "/" + pkg + "/" + name
}

// PackageRef renders a package identity, "<authority>/<package>" — the id of a
// package declaration, the group a declaration is owned, versioned and
// quarantined in, and what a bundle's `requires:` names.
func PackageRef(authority, pkg string) string { return authority + "/" + pkg }

// SplitKindRef splits a kind reference into its authority, its package and its
// local name. A bare shorthand reference answers an empty authority and
// package.
func SplitKindRef(ref string) (authority, pkg, name string) {
	a, rest, ok := strings.Cut(ref, "/")
	if !ok {
		return "", "", ref
	}
	p, n, ok := strings.Cut(rest, "/")
	if !ok {
		return "", "", ref
	}
	return a, p, n
}

// SplitPackageRef splits a package identity into its authority and its package
// name. A string that is not two non-empty segments answers two empty strings.
func SplitPackageRef(ref string) (authority, pkg string) {
	a, p, ok := strings.Cut(ref, "/")
	if !ok || a == "" || p == "" || strings.Contains(p, "/") {
		return "", ""
	}
	return a, p
}

// KindName is the local name of a kind reference — "task" for both
// "samples.substrate.reamde.dev/tasks/task" and "task".
func KindName(ref string) string {
	_, _, name := SplitKindRef(ref)
	return name
}

// KindAuthority is the authority of a kind reference, "" when it is bare.
func KindAuthority(ref string) string {
	authority, _, _ := SplitKindRef(ref)
	return authority
}

// KindPackage is the package identity a kind reference lives in
// ("samples.substrate.reamde.dev/tasks"), "" when the reference is bare.
func KindPackage(ref string) string {
	authority, pkg, _ := SplitKindRef(ref)
	if authority == "" {
		return ""
	}
	return PackageRef(authority, pkg)
}

// Qualified reports whether a kind reference names an authority and a package.
func Qualified(ref string) bool {
	authority, _, _ := SplitKindRef(ref)
	return authority != ""
}

// ValidKindReference reports whether a string is a kind REFERENCE — bare
// (`task`) or authority-qualified (`samples.substrate.reamde.dev/tasks/task`) — and not a glob.
// It is ValidTypeGlob's non-glob half on purpose: a trigger source and a
// capability allowlist admit exactly the same spellings, so a repository-local
// kind can be watched and written by the same declaration. Both sides resolve
// the reference against the repository's registry before it does any work —
// the allowlists at load, a trigger source when its trigger is loaded.
func ValidKindReference(ref string) bool {
	return !strings.Contains(ref, "*") && ValidTypeGlob(ref)
}

// RecordPath renders a record path: the kind reference, then the id.
func RecordPath(kind, id string) string { return kind + "/" + id }

// SplitRecordPath splits a STORED record path into its kind reference and its
// id. Every kind carries an authority and a package (decisions 0042, 0047), so
// a stored reference value is always "<authority>/<package>/<kind>/<id>".
//
// The split rests on the KIND GRAMMAR above and on nothing else, so it is
// deterministic WITHOUT a registry: an authority always carries a dot
// (naming.go's authorityRE requires at least one dotted label) and a package
// and a kind NAME never do (wordRE admits letters and digits only). So the
// FIRST segment is the authority, the kind is segments one through three, and
// a dotless first segment is no path at all.
//
// The id is EVERYTHING after the kind, slashes included: a DECLARATION
// record's id is itself a kind reference, so
// "substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/tasks/task" is
// one six-segment path naming one record, not a malformed four-segment one.
//
// A string that is not a full path answers ok=false, which is how an AUTHORED
// bare id is told from a stored path: a declaration id like
// "samples.substrate.reamde.dev/tasks/task" has a dotted first segment and
// nothing left after its kind, so it fails here and the reader completes it
// from the pin.
func SplitRecordPath(path string) (kind, id string, ok bool) {
	authority, rest, split := strings.Cut(path, "/")
	if !split || authority == "" || rest == "" {
		return "", "", false
	}
	if !strings.Contains(authority, ".") {
		return "", "", false
	}
	pkg, rest, split := strings.Cut(rest, "/")
	if !split || pkg == "" || rest == "" {
		return "", "", false
	}
	name, remainder, split := strings.Cut(rest, "/")
	if !split || name == "" || remainder == "" {
		return "", "", false
	}
	return authority + "/" + pkg + "/" + name, remainder, true
}

// ReferentID reads a reference property's value as the referent's own id.
//
// IT READS BOTH SHAPES, and it has to. A STORED value is the object, with the
// full "<kind>/<id>" path under `ref`; a MANIFEST authors the bare id, or the
// path, as a plain string against a pinned declaration. The loader sees all
// three: the authored value when a closure is installed from files, the object
// when the same declaration is read back off its row. They have to mean the
// same thing, or a declaration would parse one way in a manifest and another
// way in the row it becomes, which is exactly how an agent would lose its
// provider and a mapping its `to` on the first read-back.
//
// A value that does not carry the pin's prefix is handed back UNTOUCHED, so the
// caller's own validator is what refuses it and names it. This never guesses:
// stripping the pin is the whole operation.
func ReferentID(v any, pin string) string {
	s := referentPath(v)
	if id, cut := strings.CutPrefix(s, pin+"/"); cut {
		return id
	}
	return s
}

// referentPath reads the path out of a reference value in either shape: the
// object every write stores, or the bare string a manifest authors.
func referentPath(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s, _ := t[ReferenceValueKey].(string)
		return s
	}
	return ""
}

// ReferentIDs is ReferentID over a repeated reference's stored list.
func ReferentIDs(values []any, pin string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, ReferentID(v, pin))
	}
	return out
}

// CoreKind renders a core-package kind reference: the manifest envelope's own
// kinds all live there ("substrate.reamde.dev/core/kind").
func CoreKind(name string) string { return PackageCore + "/" + name }

// GraphQLName is the GraphQL object name a kind resolves to WITHOUT
// disambiguation, and the ONE place that base rule lives:
//
//   - a SHIPPED kind keeps its bare singular —
//     "substrate.reamde.dev/core/token" -> Token;
//   - an INSTALLED kind is PACKAGE-prefixed —
//     "samples.substrate.reamde.dev/tasks/task" -> Tasks_Task.
//
// The underscore keeps installed names in a namespace disjoint from the
// shipped ones, so a bundle can never rename a shipped kind's GraphQL name by
// colliding with it. Two authorities installing the SAME package name would
// still collide here, which GraphQLNames resolves over the whole set; a
// collision it cannot resolve is refused where a declaration lands
// (graphqlNameProblems, run by Finalize, Install and InstallAll alike), never
// silently renamed.
func GraphQLName(ref, source string) string {
	_, pkg, name := SplitKindRef(ref)
	base := titleCase(name)
	if base == "" {
		return ""
	}
	if source != SourceInstalled {
		return base
	}
	return titleCase(sanitizeName(pkg)) + "_" + base
}

// GraphQLKind is one kind as the naming rule sees it: its identity and where
// its declaration came from.
type GraphQLKind struct {
	Identity string
	Source   string
}

// GraphQLNames is the naming rule over a WHOLE SET of kinds, and it is what
// both readers ask: the GraphQL schema builder (internal/gql) and the
// declaration-time collision check (load.go). Asking one function keeps the
// schema and the refusal from being two spellings of one rule.
//
// The base name is GraphQLName above. Its one ambiguity is two AUTHORITIES
// installing the same package name: "acme.example.com/tasks/task" and
// "samples.substrate.reamde.dev/tasks/task" both want Tasks_Task. There the
// FULL authority joins the name, dots folded to underscores, for EVERY kind of
// both packages (Acme_example_com_Tasks_Task), so a kind's name does not
// depend on which of its neighbors exist inside its own package. The result is
// order independent: the input is a set.
//
// The full authority and not its first label, because two authorities can
// share a label ("acme.example.com" and "acme.example.org"), and a tie-break
// that ties again is a name claimed twice. It is also what decision 0014
// reserved: no identifier is derived from a first label.
func GraphQLNames(kinds []GraphQLKind) map[string]string {
	authoritiesOf := map[string]map[string]bool{}
	for _, k := range kinds {
		if k.Source != SourceInstalled {
			continue
		}
		authority, pkg, _ := SplitKindRef(k.Identity)
		if authority == "" {
			continue
		}
		if authoritiesOf[pkg] == nil {
			authoritiesOf[pkg] = map[string]bool{}
		}
		authoritiesOf[pkg][authority] = true
	}
	out := make(map[string]string, len(kinds))
	for _, k := range kinds {
		name := GraphQLName(k.Identity, k.Source)
		if name == "" {
			continue
		}
		authority, pkg, _ := SplitKindRef(k.Identity)
		if k.Source == SourceInstalled && len(authoritiesOf[pkg]) > 1 {
			name = titleCase(sanitizeName(strings.ReplaceAll(authority, ".", "_"))) + "_" + name
		}
		out[k.Identity] = name
	}
	return out
}

// titleCase upper-cases the first rune and leaves the rest as declared, so a
// camelCase local name keeps its humps.
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// sanitizeName drops every character a GraphQL name may not carry, leaving
// letters, digits and the underscore an authority's folded dots become.
func sanitizeName(s string) string {
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// PackageActor is the writing hand a PACKAGE's own installed code carries:
// `bundle:<authority>:<package>`, the same hand an install writes its
// declarations under. A package declares it as an actor document so its tier
// and its mapping precedence are legible, but the string is derived here and
// never authored: two closures sharing an actor would share their attribution
// and their trigger self-exclusion (record 0025, amended by 0047).
func PackageActor(pkg string) string {
	authority, name := SplitPackageRef(pkg)
	return string(substrate.BundleActor(authority, name))
}
