package vocabulary

import (
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// THE KIND REFERENCE GRAMMAR.
//
// A kind is named by a reference:
//
//	<authority>/<name>   a published kind — "tasks.substrate.geoah.me/task"
//	<name>               a repository-local kind — "task"
//
// The authority is a DNS name and therefore always carries a dot; a bare name
// never does, and neither form can carry a "/" inside its parts. So the two
// forms are distinguishable by inspection, which is why bare and qualified
// kinds cannot collide and why a REST path can tell an
// authority segment from a plural one.
//
// A RECORD reference is the kind reference plus the id: "<authority>/<kind>/<id>"
// for a qualified kind, "<kind>/<id>" for a bare one.

// KindRef renders a kind reference from its parts. An empty authority renders
// the bare form — there is no `local/` prefix.
func KindRef(authority, name string) string {
	if authority == "" {
		return name
	}
	return authority + "/" + name
}

// SplitKindRef splits a kind reference into its authority and its local name.
// A bare reference answers an empty authority.
func SplitKindRef(ref string) (authority, name string) {
	if a, n, ok := strings.Cut(ref, "/"); ok {
		return a, n
	}
	return "", ref
}

// KindName is the local name of a kind reference — "task" for both
// "tasks.substrate.geoah.me/task" and "task".
func KindName(ref string) string {
	_, name := SplitKindRef(ref)
	return name
}

// KindAuthority is the authority of a kind reference, "" when it is bare.
func KindAuthority(ref string) string {
	authority, _ := SplitKindRef(ref)
	return authority
}

// Qualified reports whether a kind reference names an authority.
func Qualified(ref string) bool { return strings.Contains(ref, "/") }

// ValidKindReference reports whether a string is a kind REFERENCE — bare
// (`task`) or authority-qualified (`tasks.substrate.geoah.me/task`) — and not a glob.
// It is ValidTypeGlob's non-glob half on purpose: a trigger source and a
// capability allowlist admit exactly the same spellings, so a repository-local
// kind can be watched and written by the same declaration. Both sides resolve
// the reference against the repository's registry before it does any work —
// the allowlists at load, a trigger source when its trigger is loaded.
func ValidKindReference(ref string) bool {
	return !strings.Contains(ref, "*") && ValidTypeGlob(ref)
}

// RecordRef renders a record reference: the kind reference, then the id.
func RecordRef(kind, id string) string { return kind + "/" + id }

// SplitRecordRef splits a record reference into its kind reference and id. A
// qualified record reference has three parts, a bare one has two; anything
// else is not a record reference and answers ok=false.
func SplitRecordRef(ref string) (kind, id string, ok bool) {
	parts := strings.Split(ref, "/")
	switch len(parts) {
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return "", "", false
		}
		return parts[0], parts[1], true
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return "", "", false
		}
		return parts[0] + "/" + parts[1], parts[2], true
	default:
		return "", "", false
	}
}

// CoreKind renders a core-authority kind reference: the manifest envelope's
// own kinds all live there ("core.substrate.reamde.dev/kind").
func CoreKind(name string) string { return KindRef(AuthorityCore, name) }

// GraphQLName is the GraphQL object name a kind resolves to, and the ONE place
// the rule lives. The common case stays a readable name:
//
//   - a repository-local kind capitalizes — "task" -> Task;
//   - a SHIPPED kind keeps its bare singular — "people.substrate.geoah.me/person" ->
//     Person;
//   - an INSTALLED (bundle) kind is authority-prefixed with the leading
//     label of its authority — "google.bundles.substrate.reamde.dev/person" ->
//     Google_Person.
//
// The underscore keeps installed names in a namespace disjoint from the bare
// ones, so a bundle can never rename a shipped kind's GraphQL name by
// colliding with it. Two kinds that still resolve to one name are refused at
// DECLARATION time (engine), not silently renamed.
func GraphQLName(ref, source string) string {
	authority, name := SplitKindRef(ref)
	base := titleCase(name)
	if base == "" {
		return ""
	}
	if source != SourceInstalled {
		return base
	}
	return titleCase(sanitizeName(leadingLabel(authority))) + "_" + base
}

// leadingLabel is an authority's first DNS label ("google" in
// "google.bundles.substrate.reamde.dev").
func leadingLabel(authority string) string {
	label, _, _ := strings.Cut(authority, ".")
	return label
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
// letters and digits.
func sanitizeName(s string) string {
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// AuthorityActor is the writing hand an AUTHORITY's own installed code carries:
// `connector:<first label>`. An authority declares it as an
// actor document so its tier and its mapping precedence are legible, but the
// name itself is the closed actor domain's, never a DNS name.
func AuthorityActor(authority string) string {
	return substrate.ConnectorActorPrefix + leadingLabel(authority)
}
