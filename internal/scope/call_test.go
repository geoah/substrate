package scope

import "testing"

// The call verb (decision 0219): the kind grammar over function ids, never a
// function of the core or llm package, and a reach check that holds a
// function's declared reads and writes to the grant's own scopes.

func TestACallScopeTakesTheKindGrammarOverFunctionsAndNeverCoreOrLLM(t *testing.T) {
	for _, raw := range []string{
		"call:*",
		"call:~/tasks/*",
		"call:a.example.com/*",
		"call:a.example.com/tasks/summarize",
		// A wider glob is admissible; Calls skips the two packages.
		"call:substrate.reamde.dev/*",
		"call:substrate.reamde.dev/health/convertlab",
	} {
		if _, err := Parse(raw); err != nil {
			t.Errorf("Parse(%q) = %v, want admitted", raw, err)
		}
	}
	for _, raw := range []string{
		"call:",
		"call:summarize",
		"call:tasks/summarize",
		"call:a.example.com/tasks/Summarize",
		"call:a.example.com/tasks/summ*",
		"call:a.example.com/~/x",
		"call:substrate.reamde.dev/core/query",
		"call:substrate.reamde.dev/core/*",
		"call:substrate.reamde.dev/llm/*",
		"call:substrate.reamde.dev/llm/embed",
	} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) admitted, want refused", raw)
		}
	}
	got, err := ParseRequest("call:~/tasks/* read:~/tasks/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].String() != "call:~/tasks/*" || got[0].Localize("ada.example.com").String() != "call:~/tasks/*" {
		t.Fatalf("ParseRequest = %v", got)
	}
	if err := CheckStored([]string{"call:ada.example.com/tasks/*"}); err != nil {
		t.Fatalf("a stored call scope was refused: %v", err)
	}
	if err := CheckStored([]string{"call:~/tasks/*"}); err != nil {
		t.Fatalf("a stored call scope over the repository's own functions refused: %v", err)
	}
}

func TestAGrantCallsTheFunctionsItsPatternsMatchAndNeverCoreOrLLM(t *testing.T) {
	g, err := NewGrant([]string{"call:a.example.com/tasks/*", "call:substrate.reamde.dev/*", "read:*"})
	if err != nil {
		t.Fatal(err)
	}
	for fn, want := range map[string]bool{
		"a.example.com/tasks/summarize":           true,
		"a.example.com/notes/summarize":           false,
		"substrate.reamde.dev/health/convertlab":  true,
		"substrate.reamde.dev/core/query":         false,
		"substrate.reamde.dev/llm/embed":          false,
		"":                                        false,
		"other.example.com/tasks/summarize":       false,
		"a.example.com/tasks/summarize/extra/seg": false,
	} {
		if got := g.Calls(fn); got != want {
			t.Errorf("Calls(%q) = %v, want %v", fn, got, want)
		}
	}
	if !g.CallsAny() {
		t.Fatal("CallsAny = false for a grant holding call scopes")
	}
	reader, _ := NewGrant([]string{"read:*"})
	if reader.CallsAny() || reader.Calls("a.example.com/tasks/summarize") {
		t.Fatal("a read scope calls a function")
	}
	if !g.Covers(Scope{Verb: Call, Pattern: "a.example.com/tasks/summarize"}, false) ||
		g.Covers(Scope{Verb: Call, Pattern: "a.example.com/*"}, false) {
		t.Fatal("Covers does not read a call glob as covering the functions under it and nothing wider")
	}
}

func TestReachCoveredHoldsAFunctionsReachToTheGrant(t *testing.T) {
	g, err := NewGrant([]string{"read:a.example.com/tasks/*", "write:a.example.com/notes/note"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		verb     Verb
		pattern  string
		internal bool
		want     bool
	}{
		{Read, "a.example.com/tasks/task", false, true},
		{Read, "a.example.com/tasks/*", false, true},
		// A write scope covers the function's read of the same kind.
		{Read, "a.example.com/notes/note", false, true},
		{Write, "a.example.com/notes/note", false, true},
		{Write, "a.example.com/tasks/task", false, false},
		{Read, "a.example.com/*", false, false},
		// A glob never reaches an internal kind, so it covers no read of one.
		{Read, "a.example.com/tasks/secret", true, false},
		{Read, "other.example.com/tasks/task", false, false},
	}
	for _, c := range cases {
		if got := g.ReachCovered(c.verb, c.pattern, Kind{Internal: c.internal}); got != c.want {
			t.Errorf("ReachCovered(%s, %q, %v) = %v, want %v", c.verb, c.pattern, c.internal, got, c.want)
		}
	}
	// No scope writes core or llm, so a function writing them, or a glob
	// that reaches them, is never covered, whatever the grant holds.
	everything, _ := NewGrant([]string{"write:*"})
	for _, pattern := range []string{"*", "substrate.reamde.dev/*", "substrate.reamde.dev/core/*", "substrate.reamde.dev/core/trigger", "substrate.reamde.dev/llm/provider"} {
		if everything.ReachCovered(Write, pattern, Kind{Internal: false}) {
			t.Errorf("write:* covers a function's write of %q", pattern)
		}
	}
	if !everything.ReachCovered(Write, "a.example.com/*", Kind{Internal: false}) || everything.ReachCovered(Read, "*", Kind{Internal: false}) {
		t.Fatal("write:* does not cover a function's write of a.example.com/* and read of *")
	}
}
