package vocabulary

import (
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// THE KIND REFERENCE GRAMMAR.
//
// A kind is named by a reference:
//
//	<authority>/<name>   a published kind — "tasks.substrate.reamde.dev/task"
//	<name>               a repository-local kind — "task"
//
// The authority is a DNS name and therefore always carries a dot; a bare name
// never does, and neither form can carry a "/" inside its parts. So the two
// forms are distinguishable by inspection, which is why bare and qualified
// kinds cannot collide and why a REST path can tell an
// authority segment from a plural one.
//
// A RECORD PATH is the kind reference plus the id: "<authority>/<kind>/<id>"
// for a qualified kind, "<kind>/<id>" for a bare one. It is the whole stored
// value of a `reference` property — one flat string, not a pair.

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
// "tasks.substrate.reamde.dev/task" and "task".
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
// (`task`) or authority-qualified (`tasks.substrate.reamde.dev/task`) — and not a glob.
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

// SplitRecordPath splits a record path into its kind reference and its id.
//
// The split rests on the KIND GRAMMAR above and on nothing else, so it is
// deterministic WITHOUT a registry: an authority always carries a dot
// (naming.go's authorityRE requires at least one dotted label) and a kind NAME
// never does (wordRE admits letters and digits only). So the FIRST segment
// decides — with a dot it is an authority and the kind is segments one and two,
// without one it is a repository-local kind and the kind is segment one.
//
// The id is EVERYTHING after the kind, slashes included: a DECLARATION record's
// id is itself a kind reference, so
// "core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task" is one
// four-segment path naming one record, not a malformed three-segment one.
//
// A string that is not a path answers ok=false, which is how an AUTHORED bare
// id is told from a full path: a declaration id like
// "tasks.substrate.reamde.dev/task" has a dotted first segment and nothing left
// after its kind, so it fails here and the reader completes it from the pin.
func SplitRecordPath(path string) (kind, id string, ok bool) {
	first, rest, split := strings.Cut(path, "/")
	if !split || first == "" || rest == "" {
		return "", "", false
	}
	if !strings.Contains(first, ".") {
		return first, rest, true
	}
	name, remainder, split := strings.Cut(rest, "/")
	if !split || name == "" || remainder == "" {
		return "", "", false
	}
	return first + "/" + name, remainder, true
}

// ReferentID reads a reference property's value as the referent's own id.
//
// A reference is stored as the full "<kind>/<id>" path, but a manifest AUTHORS
// the bare id against a pinned declaration, and the loader sees both: the
// authored value when a closure is installed from files, the canonical path
// when the same declaration is read back off its row. They have to mean the
// same thing, or a declaration would parse one way in a manifest and another
// way in the row it becomes.
//
// A value that does not carry the pin's prefix is handed back UNTOUCHED, so the
// caller's own validator is what refuses it and names it. This never guesses:
// stripping the pin is the whole operation.
func ReferentID(v any, pin string) string {
	s, _ := v.(string)
	if id, cut := strings.CutPrefix(s, pin+"/"); cut {
		return id
	}
	return s
}

// ReferentIDs is ReferentID over a repeated reference's stored list.
func ReferentIDs(values []any, pin string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, ReferentID(v, pin))
	}
	return out
}

// CoreKind renders a core-authority kind reference: the manifest envelope's
// own kinds all live there ("core.substrate.reamde.dev/kind").
func CoreKind(name string) string { return KindRef(AuthorityCore, name) }

// GraphQLName is the GraphQL object name a kind resolves to, and the ONE place
// the rule lives. The common case stays a readable name:
//
//   - a repository-local kind capitalizes — "task" -> Task;
//   - a SHIPPED kind keeps its bare singular — "people.substrate.reamde.dev/person" ->
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

// AuthorityActor is the writing hand an AUTHORITY's own installed code
// carries: `bundle:<authority>`, the same hand an install writes its
// declarations under. An authority declares it as an actor document so its
// tier and its mapping precedence are legible, but the string is derived here
// and never authored: two authorities sharing a first label shared this actor
// until record 0025, and with it their attribution and their trigger
// self-exclusion.
func AuthorityActor(authority string) string {
	return string(substrate.BundleActor(authority))
}
