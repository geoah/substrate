package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

type catalogueRef struct {
	name, to        string
	repeated, keyed bool
}

// catalogueKind builds a kind the way the loader leaves one: PropOrder is
// sorted by name (vocabulary/load.go), never declaration order.
func catalogueKind(ident, purpose, description string, refs ...catalogueRef) *vocabulary.Kind {
	i := strings.LastIndexByte(ident, '/')
	k := &vocabulary.Kind{
		Name: ident[i+1:], Package: ident[:i], Identity: ident,
		Purpose: purpose, Description: description,
		Props: map[string]*vocabulary.Property{
			"title": {Name: "title", Datatype: vocabulary.DatatypeString},
		},
	}
	for _, r := range refs {
		p := &vocabulary.Property{Name: r.name, Datatype: vocabulary.DatatypeReference, Repeated: r.repeated, Keyed: r.keyed}
		if trait, ok := strings.CutPrefix(r.to, "trait:"); ok {
			p.ToTrait = trait
		} else {
			p.To = r.to
		}
		k.Props[r.name] = p
	}
	for name := range k.Props {
		k.PropOrder = append(k.PropOrder, name)
	}
	sort.Strings(k.PropOrder)
	return k
}

// The registry hands kinds sorted by identity; the list must not depend on
// that, so the fixture is shuffled.
func catalogueRegistry() []*vocabulary.Kind {
	return []*vocabulary.Kind{
		catalogueKind("example.com/tasks/task", "", "A thing to do, e.g. a chore. It has a due date and an owner.",
			catalogueRef{name: "project", to: "example.com/tasks/project"},
			catalogueRef{name: "owner", to: "example.com/people/person"},
			catalogueRef{name: "links", to: "any", repeated: true},
			catalogueRef{name: "byRole", to: "example.com/tasks/project", keyed: true},
			catalogueRef{name: "cursor", to: "example.com/tasks/syncstate"}),
		catalogueKind("example.com/people/person", "", "A person you know."),
		catalogueKind("example.com/tasks/comment", "supporting", "A comment on a task.",
			catalogueRef{name: "task", to: "example.com/tasks/task"},
			catalogueRef{name: "account", to: "trait:substrate.reamde.dev/core/accountconfig"}),
		catalogueKind("example.com/tasks/syncstate", "internal", "Where the task sync stopped."),
		catalogueKind("example.com/tasks/project", "", "A group of tasks."),
		catalogueKind("substrate.reamde.dev/core/token", "internal", "A bearer token."),
	}
}

func TestKindsCatalogueRendersTheGrant(t *testing.T) {
	got := kindsCatalogue(catalogueRegistry(), []string{"example.com/*"})
	want := catalogueHeader + "\n\n```" + `
example.com/people
- example.com/people/person: A person you know.

example.com/tasks
- example.com/tasks/comment (supporting): A comment on a task. References: account -> any kind implementing substrate.reamde.dev/core/accountconfig, task -> example.com/tasks/task.
- example.com/tasks/project: A group of tasks.
- example.com/tasks/task: A thing to do, e.g. a chore. References: byRole{} -> example.com/tasks/project, links[] -> any kind, owner -> example.com/people/person, project -> example.com/tasks/project.
` + "```"
	if got != want {
		t.Fatalf("kinds list:\n%s\n\nwant:\n%s", got, want)
	}
}

// A reference to a kind the list does not carry is left out: the agent would
// read it as absent.
func TestKindsCatalogueListsOnlyReferencesItCanFollow(t *testing.T) {
	got := kindsCatalogue(catalogueRegistry(), []string{"example.com/tasks/*"})
	if strings.Contains(got, "owner ->") || strings.Contains(got, "cursor ->") {
		t.Fatalf("a reference to an unlisted kind is rendered:\n%s", got)
	}
	if !strings.Contains(got, "links[] -> any kind, project -> example.com/tasks/project.") {
		t.Fatalf("a followable reference is missing:\n%s", got)
	}
}

// An internal kind is listed where the allowlist names it, never where only a
// glob reaches it; an auth kind is never reached by a glob at all.
func TestKindsCatalogueListsInternalKindsOnlyWhenNamed(t *testing.T) {
	globbed := kindsCatalogue(catalogueRegistry(), []string{"*"})
	if strings.Contains(globbed, "syncstate") || strings.Contains(globbed, "core/token") {
		t.Fatalf("a glob listed an internal or auth kind:\n%s", globbed)
	}
	named := kindsCatalogue(catalogueRegistry(), []string{"example.com/tasks/syncstate", "example.com/people/*"})
	if !strings.Contains(named, "- example.com/tasks/syncstate (internal): Where the task sync stopped.") {
		t.Fatalf("a named internal kind is missing:\n%s", named)
	}
	if strings.Contains(named, "tasks/task") {
		t.Fatalf("a kind outside the grant is listed:\n%s", named)
	}
	if kindsCatalogue(catalogueRegistry(), nil) != "" {
		t.Fatal("no reads grant rendered a kinds list")
	}
	if kindsCatalogue(catalogueRegistry(), []string{"nowhere.example.com/*"}) != "" {
		t.Fatal("a grant reaching no kind rendered a kinds list")
	}
}

// Two authorities may publish packages of one name: each group is headed by
// its authority and package, and the order is the full reference, so the
// bytes do not depend on the input order.
func TestKindsCatalogueKeepsAuthoritiesApart(t *testing.T) {
	kinds := []*vocabulary.Kind{
		catalogueKind("z.example.com/llm/thread", "", "Z's thread."),
		catalogueKind("a.example.com/llm/thread", "", "A's thread."),
		catalogueKind("a.example.com/llm/message", "", "A's message."),
	}
	got := kindsCatalogue(kinds, []string{"*"})
	reversed := kindsCatalogue([]*vocabulary.Kind{kinds[2], kinds[1], kinds[0]}, []string{"*"})
	if got != reversed {
		t.Fatal("the input order changed the bytes")
	}
	want := "\na.example.com/llm\n- a.example.com/llm/message: A's message.\n- a.example.com/llm/thread: A's thread.\n\nz.example.com/llm\n- z.example.com/llm/thread: Z's thread.\n"
	if !strings.Contains(got, want) {
		t.Fatalf("groups:\n%s", got)
	}
}

func TestKindsCatalogueCutsTheDescriptionToItsFirstSentence(t *testing.T) {
	long := strings.Repeat("ü", 300) + ". Second sentence."
	got := firstSentence(long, catalogueDescriptionRunes)
	if n := len([]rune(got)); n != catalogueDescriptionRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("cut to %d runes: %q", n, got)
	}
	for in, want := range map[string]string{
		"One. Two.":                  "One.",
		"Version 1.2 is\nhere":       "Version 1.2 is here",
		"A note, e.g. a memo. Then.": "A note, e.g. a memo.",
		"Really? Yes.":               "Really?",
		"Ends with a period.":        "Ends with a period.",
		"Ünïcode. Élan follows.":     "Ünïcode.",
	} {
		if got := firstSentence(in, 120); got != want {
			t.Errorf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}

// Past the size cap the remaining kinds are listed by reference alone, primary
// kinds stay described before supporting ones, and the tail is capped too.
func TestKindsCatalogueBoundsItsSize(t *testing.T) {
	var kinds []*vocabulary.Kind
	for i := range 4000 {
		purpose := ""
		if i%2 == 1 {
			purpose = vocabulary.PurposeSupporting
		}
		kinds = append(kinds, catalogueKind(
			"example.com/big/k"+strings.Repeat("y", i/26)+string(rune('a'+i%26)),
			purpose, strings.Repeat("words ", 30)+"."))
	}
	got := kindsCatalogue(kinds, []string{"*"})
	head, tail, cut := strings.Cut(got, "The list reached its size limit;")
	if !cut {
		t.Fatalf("4000 kinds did not reach the cap (%d bytes)", len(got))
	}
	if len(head) > catalogueMaxBytes {
		t.Fatalf("the described part is %d bytes, over the %d cap", len(head), catalogueMaxBytes)
	}
	if len(tail) > catalogueTailBytes+200 {
		t.Fatalf("the tail is %d bytes, over the %d cap", len(tail), catalogueTailBytes)
	}
	if !strings.Contains(tail, "\n- and ") {
		t.Fatal("a capped tail does not say how many it left out")
	}
	if strings.Contains(head, "(supporting)") {
		t.Fatal("a supporting kind is described while primary kinds fell to the tail")
	}
	if strings.Contains(tail, "words") {
		t.Fatal("a kind past the cap carries its description")
	}
}

func TestComposeSystemJoinsTheNonEmptyParts(t *testing.T) {
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"P\n", "", ""}, "P\n"},
		{[]string{"P\n", "C", ""}, "P\n\nC"},
		{[]string{"P", "C", "J"}, "P\n\nC\n\nJ"},
		{[]string{"", "C", "J"}, "C\n\nJ"},
		{[]string{"", "", ""}, ""},
	} {
		if got := composeSystem(tc.parts...); got != tc.want {
			t.Errorf("composeSystem(%q) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}
