package scope

import (
	"strings"
	"testing"
)

func TestTheGrammarAdmitsThePatternsAndRefusesTheRest(t *testing.T) {
	admitted := []string{
		"read:*",
		"write:*",
		"read:ada.example.com/*",
		"read:ada.example.com/tasks/*",
		"write:ada.example.com/tasks/task",
		"read:~/tasks/*",
		"write:~/*",
		"read:substrate.reamde.dev/core/kind",
		"write:substrate.reamde.dev/*",
		// `~/core` is the owner's own package named core, not the built-in.
		"write:~/core/*",
	}
	for _, raw := range admitted {
		if _, err := Parse(raw); err != nil {
			t.Errorf("Parse(%q) = %v, want admitted", raw, err)
		}
	}
	refused := []string{
		"",
		"read",
		"read:",
		"admin:*",
		"READ:*",
		"read:task",
		"read:tasks/task",
		"read:~x/*",
		"read:a.example.com/~/x",
		"read:ada.example.com/tas*",
		"read:*/tasks/task",
		"read:substrate.reamde.dev/core/token",
		"write:substrate.reamde.dev/core/credential",
		"read:substrate.reamde.dev/core/secret",
		"read:substrate.reamde.dev/core/recoverykey",
		"write:substrate.reamde.dev/core/trigger",
		"write:substrate.reamde.dev/core/*",
		"write:substrate.reamde.dev/llm/provider",
		"write:substrate.reamde.dev/llm/*",
	}
	for _, raw := range refused {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) admitted, want refused", raw)
		}
	}
}

func TestARequestDropsDuplicatesAndOfflineAccessAndNamesSomething(t *testing.T) {
	got, err := ParseRequest("read:~/tasks/* offline_access read:~/tasks/*  write:~/tasks/task")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].String() != "read:~/tasks/*" || got[1].String() != "write:~/tasks/task" {
		t.Fatalf("ParseRequest = %v", got)
	}
	for _, param := range []string{"", "offline_access", "   "} {
		if _, err := ParseRequest(param); err == nil {
			t.Errorf("ParseRequest(%q) admitted, want refused: an app names what it needs", param)
		}
	}
	if _, err := ParseRequest(strings.Repeat("read:a.example.com/p/k ", 400)); err == nil {
		t.Error("an oversized scope parameter was admitted")
	}
	many := make([]string, 0, 40)
	for i := range 40 {
		many = append(many, "read:a.example.com/p/k"+strings.Repeat("x", i))
	}
	if _, err := ParseRequest(strings.Join(many, " ")); err == nil {
		t.Error("forty scopes were admitted")
	}
}

func TestResolveCompletesTheHomeAuthorityOnly(t *testing.T) {
	for in, want := range map[string]string{
		"read:~/tasks/*":         "read:ada.example.com/tasks/*",
		"write:~/*":              "write:ada.example.com/*",
		"read:~/tasks/task":      "read:ada.example.com/tasks/task",
		"read:b.example.com/x/*": "read:b.example.com/x/*",
		"read:*":                 "read:*",
	} {
		s, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Resolve("ada.example.com").String(); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAStoredGrantIsCanonicalResolvedAndNeverEmpty(t *testing.T) {
	if err := CheckStored(nil); err != nil {
		t.Fatalf("nil (full access) refused: %v", err)
	}
	for _, bad := range [][]string{
		{},
		{"read:~/tasks/*"},
		{""},
		{"read:"},
		{"read:substrate.reamde.dev/core/token"},
		{"read:ada.example.com/tasks/*", "nonsense"},
	} {
		if err := CheckStored(bad); err == nil {
			t.Errorf("CheckStored(%q) admitted", bad)
		}
	}
	for _, bad := range [][]string{nil, {}} {
		if _, err := NewGrant(bad); err == nil {
			t.Errorf("NewGrant(%q) admitted: a grant names at least one scope", bad)
		}
	}
}

func TestAGrantReachesWhatItNamesAndNeverTheMachinery(t *testing.T) {
	g, err := NewGrant([]string{
		"read:ada.example.com/people/*",
		"write:ada.example.com/tasks/task",
		"read:substrate.reamde.dev/core/kind",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		kind          string
		internal      bool
		reads, writes bool
		why           string
	}{
		{"ada.example.com/people/person", false, true, false, "a read glob reads"},
		{"ada.example.com/tasks/task", false, true, true, "write includes read"},
		{"ada.example.com/tasks/project", false, false, false, "an exact scope reaches one kind"},
		{"substrate.reamde.dev/core/kind", true, true, false, "an exact scope reads an internal kind"},
		{"substrate.reamde.dev/core/token", true, false, false, "the auth kinds are never reached"},
	}
	for _, c := range cases {
		if got := g.Reads(c.kind, c.internal); got != c.reads {
			t.Errorf("Reads(%s) = %v, want %v: %s", c.kind, got, c.reads, c.why)
		}
		if got := g.Writes(c.kind, c.internal); got != c.writes {
			t.Errorf("Writes(%s) = %v, want %v: %s", c.kind, got, c.writes, c.why)
		}
	}

	everything, err := NewGrant([]string{"write:*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		kind          string
		internal      bool
		reads, writes bool
	}{
		{"ada.example.com/tasks/task", false, true, true},
		{"substrate.reamde.dev/observations/observation", false, true, true},
		// A glob never reaches an internal kind, nor writes core or llm.
		{"substrate.reamde.dev/core/trigger", true, false, false},
		{"substrate.reamde.dev/llm/provider", true, false, false},
		{"substrate.reamde.dev/console/view", true, false, false},
		{"substrate.reamde.dev/core/token", true, false, false},
		{"substrate.reamde.dev/core/secret", true, false, false},
	} {
		if got := everything.Reads(c.kind, c.internal); got != c.reads {
			t.Errorf("write:* Reads(%s) = %v, want %v", c.kind, got, c.reads)
		}
		if got := everything.Writes(c.kind, c.internal); got != c.writes {
			t.Errorf("write:* Writes(%s) = %v, want %v", c.kind, got, c.writes)
		}
	}
	// The auth kinds are never reached, even read as if they were not
	// internal: the guard is the kind, not the purpose.
	for _, k := range []string{"token", "credential", "secret", "recoverykey"} {
		kind := "substrate.reamde.dev/core/" + k
		if everything.Reads(kind, false) || everything.Writes(kind, false) {
			t.Errorf("write:* reached the auth kind %s", kind)
		}
	}
	// Even a non-internal kind of core or llm is never written.
	if everything.Writes("substrate.reamde.dev/core/unit", false) {
		t.Error("write:* wrote a core kind")
	}
}

func TestTheAgentVerbsTakeTheGrammarOverAgentIDs(t *testing.T) {
	admitted := []string{
		"agents:*",
		"decide:*",
		"agents:~/*",
		"agents:~/llm/*",
		"agents:~/llm/substrateEditor",
		"decide:ada.example.com/chat/assistant",
		"agents:samples.substrate.reamde.dev/llm/*",
		// An agent of a built-in package is named like any other.
		"agents:substrate.reamde.dev/core/judge",
	}
	for _, raw := range admitted {
		s, err := Parse(raw)
		if err != nil {
			t.Errorf("Parse(%q) = %v, want admitted", raw, err)
			continue
		}
		if !s.OverAgents() || s.String() != raw {
			t.Errorf("Parse(%q) = %+v, want an agent scope in its own spelling", raw, s)
		}
	}
	refused := []string{
		"agents:",
		"agents:assistant",
		"agents:chat/assistant",
		"agents:ada.example.com/chat/assist*",
		"agents:ada.example.com/Chat/assistant",
		"agents:ada.example.com/chat/assistant/extra",
		"agents:ada.example.com/chat/Assistant",
		"agents:~x/*",
		"decide:ada.example.com/~/x",
		"agents:*/chat/assistant",
		"Agents:*",
	}
	for _, raw := range refused {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) admitted, want refused", raw)
		}
	}
	s, err := Parse("decide:~/chat/*")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Resolve("ada.example.com").String(); got != "decide:ada.example.com/chat/*" {
		t.Errorf("Resolve = %q", got)
	}
	if err := CheckStored([]string{"agents:~/chat/*"}); err == nil {
		t.Error("a stored agents scope still naming ~ was admitted")
	}
}

func TestAnAgentScopeReachesItsAgentsAndNoKindWhole(t *testing.T) {
	g, err := NewGrant([]string{
		"agents:ada.example.com/chat/*",
		"decide:ada.example.com/chat/assistant",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		agent         string
		talks, decide bool
	}{
		{"ada.example.com/chat/assistant", true, true},
		{"ada.example.com/chat/writer", true, false},
		{"ada.example.com/other/assistant", false, false},
		{"ada.example.com.evil/chat/assistant", false, false},
	} {
		if got := g.TalksTo(c.agent); got != c.talks {
			t.Errorf("TalksTo(%s) = %v, want %v", c.agent, got, c.talks)
		}
		if got := g.Decides(c.agent); got != c.decide {
			t.Errorf("Decides(%s) = %v, want %v", c.agent, got, c.decide)
		}
	}
	if !g.Talks() || !g.DecidesAny() {
		t.Error("the grant does not report the verbs it holds")
	}
	// The agent verbs read no kind through the rules of read and write: what
	// they make readable is decided row by row.
	for _, kind := range []string{
		"substrate.reamde.dev/core/agent",
		"substrate.reamde.dev/llm/thread",
		"substrate.reamde.dev/llm/message",
		"substrate.reamde.dev/core/recordpatchrequest",
	} {
		if g.Reads(kind, true) || g.Writes(kind, true) {
			t.Errorf("an agent scope reached the kind %s whole", kind)
		}
	}
	reads, err := NewGrant([]string{"read:*"})
	if err != nil {
		t.Fatal(err)
	}
	if reads.Talks() || reads.TalksTo("ada.example.com/chat/assistant") || reads.DecidesAny() {
		t.Error("a read scope talked to an agent")
	}
}
