package scope

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func shippedRoles(t *testing.T, ids ...string) Roles {
	t.Helper()
	var held []Role
	for _, id := range ids {
		i := 0
		for i < len(ShippedRoles) && ShippedRoles[i].ID != id {
			i++
		}
		if i == len(ShippedRoles) {
			t.Fatalf("no shipped role %q", id)
		}
		held = append(held, ShippedRoles[i].Role)
	}
	return NewRoles(held, "ada.example.com")
}

// A pattern reaches a control kind or sign-in material only by naming it,
// a glob or a trait reaches every other kind, internal ones included, and
// `admin` reaches everything by being `admin`. No rule writes an actor or a
// role, the one naming them included.
func TestARoleReachesAControlKindOnlyByNamingIt(t *testing.T) {
	// The repository's own kinds are `~` (decision record 0259).
	note := Kind{Ref: "~/notes/note", Traits: []string{"~/notes/titled"}}
	thread := Kind{Ref: vocabulary.LLMKind("thread"), Internal: true}
	trigger := Kind{Ref: vocabulary.CoreKind("trigger"), Control: true}
	token := Kind{Ref: vocabulary.CoreKind("token"), Control: true}
	actor := Kind{Ref: vocabulary.CoreKind("actor"), Control: true}
	role := Kind{Ref: vocabulary.CoreKind("role"), Control: true}
	// A sign-in kind is held as one whether or not its caller marked it.
	unmarked := Kind{Ref: vocabulary.CoreKind("credential")}
	named := NewRoles([]Role{{ID: "triggers", Rules: []Rule{
		{Verbs: []Verb{Write}, Kinds: []string{vocabulary.CoreKind("trigger"), vocabulary.CoreKind("actor"), vocabulary.CoreKind("role"), "~/notes/*"}},
		{Verbs: []Verb{Read}, Kinds: []string{TraitPattern("~/notes/titled"), vocabulary.CoreKind("*")}},
	}}}, "ada.example.com")
	// The full name is another spelling of `~` in a rule as everywhere.
	fullName := NewRoles([]Role{{ID: "notes", Rules: []Rule{{Verbs: []Verb{Write}, Kinds: []string{"ada.example.com/notes/*"}}}}}, "ada.example.com")

	cases := []struct {
		name  string
		roles Roles
		verb  Verb
		kind  Kind
		want  bool
	}{
		{"admin writes a trigger", shippedRoles(t, RoleAdmin), Write, trigger, true},
		{"admin writes a token", shippedRoles(t, RoleAdmin), Write, token, true},
		{"admin writes a role", shippedRoles(t, RoleAdmin), Write, role, true},
		{"editor writes a note", shippedRoles(t, RoleEditor), Write, note, true},
		{"editor writes an internal kind", shippedRoles(t, RoleEditor), Write, thread, true},
		{"editor writes no trigger", shippedRoles(t, RoleEditor), Write, trigger, false},
		{"editor reads no trigger", shippedRoles(t, RoleEditor), Read, trigger, false},
		{"editor reads no sign-in kind", shippedRoles(t, RoleEditor), Read, unmarked, false},
		{"viewer reads a note", shippedRoles(t, RoleViewer), Read, note, true},
		{"viewer writes no note", shippedRoles(t, RoleViewer), Write, note, false},
		{"viewer and editor together write a note", shippedRoles(t, RoleViewer, RoleEditor), Write, note, true},
		{"a rule naming a trigger writes it", named, Write, trigger, true},
		{"a rule naming a trigger reads it", named, Read, trigger, true},
		{"a rule naming an actor writes none", named, Write, actor, false},
		{"a rule naming a role writes none", named, Write, role, false},
		{"a home glob writes a note", named, Write, note, true},
		{"a glob over core reads no token", named, Read, token, false},
		{"a trait reads a note", named, Read, Kind{Ref: "~/other/memo", Traits: []string{"~/notes/titled"}}, true},
		{"a rule naming the repository's full name writes its own kind", fullName, Write, note, true},
		{"a rule naming another authority writes no own kind", NewRoles([]Role{{ID: "other", Rules: []Rule{{Verbs: []Verb{Write}, Kinds: []string{"ada.example.com.evil/notes/*"}}}}}, "ada.example.com"), Write, note, false},
		{"no roles reach nothing", NewRoles(nil, "ada.example.com"), Read, note, false},
	}
	for _, c := range cases {
		if got := c.roles.Reaches(c.verb, c.kind); got != c.want {
			t.Errorf("%s: Reaches(%s, %s) = %v, want %v", c.name, c.verb, c.kind.Ref, got, c.want)
		}
	}
}

// Code a person starts is held to a glob it reads or writes only where the
// person's rules hold the whole glob and name every control kind it matches.
func TestARoleCoversAGlobOnlyWhereItNamesEveryControlKindInIt(t *testing.T) {
	controls := []string{vocabulary.CoreKind("trigger"), vocabulary.CoreKind("token")}
	note := Kind{Ref: "ada.example.com/notes/note"}
	cases := []struct {
		name    string
		roles   Roles
		verb    Verb
		pattern string
		want    bool
	}{
		{"admin covers everything", shippedRoles(t, RoleAdmin), Write, "*", true},
		{"editor covers its own package", shippedRoles(t, RoleEditor), Write, "ada.example.com/notes/*", true},
		{"editor covers no glob over a control kind", shippedRoles(t, RoleEditor), Read, "*", false},
		{"editor covers no glob over core", shippedRoles(t, RoleEditor), Read, vocabulary.CoreKind("*"), false},
		{"viewer covers no write", shippedRoles(t, RoleViewer), Write, "ada.example.com/notes/*", false},
		{"viewer covers a read of the note", shippedRoles(t, RoleViewer), Read, note.Ref, true},
	}
	for _, c := range cases {
		if got := c.roles.Covers(c.verb, c.pattern, note, controls); got != c.want {
			t.Errorf("%s: Covers(%s, %s) = %v, want %v", c.name, c.verb, c.pattern, got, c.want)
		}
	}
}

// A repository's override of how long a kind's history is kept is a control
// kind of the declarations class (decision 0276): shortening it removes
// history, so an editor's glob does not reach it and an admin does.
func TestARetentionOverrideIsAControlKind(t *testing.T) {
	ref := vocabulary.CoreKind("retention")
	if got := vocabulary.ControlClassOf(&vocabulary.Kind{Identity: ref}, false); got != vocabulary.ControlDeclarations {
		t.Fatalf("the class of %s = %q, want %q", ref, got, vocabulary.ControlDeclarations)
	}
	k := Kind{Ref: ref, Internal: true, Control: true}
	if shippedRoles(t, RoleEditor).Reaches(Write, k) || !shippedRoles(t, RoleAdmin).Reaches(Write, k) {
		t.Fatal("an editor writes the retention override, or an admin does not")
	}
}
