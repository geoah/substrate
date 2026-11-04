package scope

import (
	"slices"
	"testing"
)

// A trait scope (decision 0225): what `*` would reach among the kinds whose
// declaration binds the trait.

const (
	completable = "substrate.reamde.dev/core/completable"
	temporal    = "substrate.reamde.dev/core/temporal"
)

func TestATraitScopeNamesOneTraitInFull(t *testing.T) {
	for _, raw := range []string{
		"read:implements(" + completable + ")",
		"write:implements(" + temporal + ")",
		"read:implements(~/tasks/done)",
		// Core's own traits are admissible: the scope reaches what `*` would,
		// which writes no core kind and reaches no auth kind.
		"write:implements(substrate.reamde.dev/core/accountconfig)",
	} {
		if _, err := Parse(raw); err != nil {
			t.Errorf("Parse(%q) = %v, want it admitted", raw, err)
		}
	}
	for _, raw := range []string{
		"read:implements(completable)",
		"read:implements(substrate.reamde.dev/core)",
		"read:implements(substrate.reamde.dev/core/*)",
		"read:implements(*)",
		"read:implements()",
		"read:implements(ada.example.com/~/done)",
		"read:implements(" + completable,
		"read:implements(" + completable + ")/x",
		"agents:implements(" + completable + ")",
		"decide:implements(" + completable + ")",
		"call:implements(" + completable + ")",
	} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) admitted it", raw)
		}
	}
}

func TestATraitScopeCompletesTheHomeAuthorityInsideItsTrait(t *testing.T) {
	s, err := Parse("read:implements(~/tasks/done)")
	if err != nil {
		t.Fatal(err)
	}
	if !s.HomeRelative() {
		t.Fatal("a trait scope naming ~ is not home-relative")
	}
	if CheckStored([]string{s.String()}) == nil {
		t.Fatal("a stored trait scope still naming ~ was admitted")
	}
	resolved := s.Resolve("ada.example.com")
	if resolved.String() != "read:implements(ada.example.com/tasks/done)" {
		t.Fatalf("Resolve = %q", resolved)
	}
	if err := CheckStored([]string{resolved.String()}); err != nil {
		t.Fatalf("the resolved trait scope was refused as stored: %v", err)
	}
	if trait, ok := resolved.Trait(); !ok || trait != "ada.example.com/tasks/done" {
		t.Fatalf("Trait() = %q, %v", trait, ok)
	}
	if _, ok := (Scope{Verb: Agents, Pattern: TraitPattern(completable)}).Trait(); ok {
		t.Fatal("an agents scope read as a trait scope")
	}
}

func TestATraitScopeReachesWhatStarReachesAmongTheKindsThatBindIt(t *testing.T) {
	writes, err := NewGrant([]string{"write:" + TraitPattern(completable)})
	if err != nil {
		t.Fatal(err)
	}
	reads, err := NewGrant([]string{"read:" + TraitPattern(completable)})
	if err != nil {
		t.Fatal(err)
	}
	everything, err := NewGrant([]string{"write:*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name          string
		kind          Kind
		read, written bool
	}{
		{"a kind that binds it", Kind{Ref: "ada.example.com/tasks/task", Traits: []string{temporal, completable}}, true, true},
		{"a kind that binds it in another case", Kind{Ref: "ada.example.com/tasks/chore", Traits: []string{"substrate.reamde.dev/core/Completable"}}, true, true},
		{"a kind that binds another trait", Kind{Ref: "ada.example.com/notes/note", Traits: []string{temporal}}, false, false},
		{"a kind that binds none", Kind{Ref: "ada.example.com/people/person"}, false, false},
		{"an internal kind that binds it", Kind{Ref: "ada.example.com/tasks/log", Internal: true, Traits: []string{completable}}, false, false},
		{"a core kind that binds it", Kind{Ref: "substrate.reamde.dev/core/unit", Traits: []string{completable}}, true, false},
		{"an llm kind that binds it", Kind{Ref: "substrate.reamde.dev/llm/memory", Traits: []string{completable}}, true, false},
		{"an auth kind that binds it", Kind{Ref: "substrate.reamde.dev/core/token", Traits: []string{completable}}, false, false},
	} {
		if got := writes.Reads(c.kind); got != c.read {
			t.Errorf("%s: a write trait scope reads it = %v, want %v", c.name, got, c.read)
		}
		if got := writes.Writes(c.kind); got != c.written {
			t.Errorf("%s: a write trait scope writes it = %v, want %v", c.name, got, c.written)
		}
		if got := reads.Reads(c.kind); got != c.read {
			t.Errorf("%s: a read trait scope reads it = %v, want %v", c.name, got, c.read)
		}
		if reads.Writes(c.kind) {
			t.Errorf("%s: a read trait scope writes it", c.name)
		}
		// Never past what `*` reaches there.
		if (c.read && !everything.Reads(c.kind)) || (c.written && !everything.Writes(c.kind)) {
			t.Errorf("%s: the trait scope reaches past `*`", c.name)
		}
	}
	if writes.NamesPackage("ada.example.com/tasks") {
		t.Error("a trait scope names a package")
	}
}

func TestATraitScopeCoversOnlyItselfAndOnlyStarCoversIt(t *testing.T) {
	read := Scope{Verb: Read, Pattern: TraitPattern(completable)}
	write := Scope{Verb: Write, Pattern: TraitPattern(completable)}
	other := Scope{Verb: Read, Pattern: TraitPattern(temporal)}
	for _, c := range []struct {
		held, want Scope
		covers     bool
	}{
		{read, read, true},
		{write, read, true},
		{read, write, false},
		{Scope{Verb: Read, Pattern: "*"}, read, true},
		{Scope{Verb: Write, Pattern: "*"}, write, true},
		{Scope{Verb: Read, Pattern: "*"}, write, false},
		{Scope{Verb: Read, Pattern: "substrate.reamde.dev/*"}, read, false},
		{Scope{Verb: Read, Pattern: "substrate.reamde.dev/core/*"}, read, false},
		{read, other, false},
		{read, Scope{Verb: Read, Pattern: "ada.example.com/tasks/task"}, false},
		{read, Scope{Verb: Read, Pattern: "ada.example.com/*"}, false},
		{write, Scope{Verb: Read, Pattern: "*"}, false},
	} {
		if got := Covers(c.held, c.want, false); got != c.covers {
			t.Errorf("Covers(%s, %s) = %v, want %v", c.held, c.want, got, c.covers)
		}
	}
}

func TestATraitScopeCoversTheReachOfAnExactKindThatBindsIt(t *testing.T) {
	g, err := NewGrant([]string{"write:" + TraitPattern(completable)})
	if err != nil {
		t.Fatal(err)
	}
	binds := Kind{Traits: []string{completable}}
	if !g.ReachCovered(Write, "ada.example.com/tasks/task", binds) {
		t.Error("the reach of a kind that binds the trait is not covered")
	}
	if g.ReachCovered(Write, "ada.example.com/tasks/task", Kind{}) {
		t.Error("the reach of a kind that binds nothing is covered")
	}
	if g.ReachCovered(Read, "ada.example.com/tasks/task", Kind{Internal: true, Traits: []string{completable}}) {
		t.Error("the reach of an internal kind is covered")
	}
	if g.ReachCovered(Read, "ada.example.com/tasks/*", binds) {
		t.Error("a glob reach is covered by a trait scope")
	}
	approved, err := ApprovalOf([]string{"write:" + TraitPattern(completable)})
	if err != nil {
		t.Fatal(err)
	}
	if !approved.Covers("ada.example.com/tasks/task", true, binds) || approved.Covers("ada.example.com/tasks/*", false, binds) {
		t.Error("an approval of a trait scope covers what a grant of it would not")
	}
}

func TestAKindIsReadOffItsDeclaration(t *testing.T) {
	k := KindOf("ada.example.com/tasks/task", map[string]any{
		"purpose": "internal",
		"traits":  []any{temporal + "(point: dueAt)", completable, 7, "bare"},
	})
	if !k.Internal || !slices.Equal(k.Traits, []string{temporal, completable}) || k.Ref != "ada.example.com/tasks/task" {
		t.Fatalf("KindOf = %+v", k)
	}
	if k := KindOf("ada.example.com/tasks/task", map[string]any{"traits": []string{completable}}); k.Internal || !slices.Equal(k.Traits, []string{completable}) {
		t.Fatalf("KindOf over a []string = %+v", k)
	}
	if k := KindOf("ada.example.com/tasks/task", nil); k.Internal || len(k.Traits) != 0 {
		t.Fatalf("KindOf over no declaration = %+v", k)
	}
}
