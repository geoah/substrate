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
		"write:substrate.reamde.dev/core/trigger",
		"write:substrate.reamde.dev/core/*",
		"write:substrate.reamde.dev/llm/provider",
		"write:substrate.reamde.dev/llm/*",
		// An applet runs under whoever opens it, so no app writes one.
		"write:substrate.reamde.dev/applets/applet",
		"write:substrate.reamde.dev/applets/*",
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

// The repository's full name is another spelling of `~`, and a scope is
// stored in the `~` one; every other authority stays as it is asked
// (decision record 0259).
func TestLocalizeFoldsTheRepositorysNameIntoTheLocalNamespaceOnly(t *testing.T) {
	for in, want := range map[string]string{
		"read:ada.example.com/tasks/*":    "read:~/tasks/*",
		"write:ada.example.com/*":         "write:~/*",
		"read:ada.example.com/tasks/task": "read:~/tasks/task",
		"read:~/tasks/*":                  "read:~/tasks/*",
		"read:b.example.com/x/*":          "read:b.example.com/x/*",
		"read:ada.example.com.evil/x/*":   "read:ada.example.com.evil/x/*",
		"read:*":                          "read:*",
	} {
		s, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Localize("ada.example.com").String(); got != want {
			t.Errorf("Localize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAStoredGrantIsCanonicalResolvedAndNeverEmpty(t *testing.T) {
	if err := CheckStored(nil); err != nil {
		t.Fatalf("nil (full access) refused: %v", err)
	}
	if err := CheckStored([]string{"read:~/tasks/*", "write:~/tasks/task"}); err != nil {
		t.Fatalf("a stored scope over the repository's own kinds refused: %v", err)
	}
	for _, bad := range [][]string{
		{},
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
		if got := g.Reads(Kind{Ref: c.kind, Internal: c.internal}); got != c.reads {
			t.Errorf("Reads(%s) = %v, want %v: %s", c.kind, got, c.reads, c.why)
		}
		if got := g.Writes(Kind{Ref: c.kind, Internal: c.internal}); got != c.writes {
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
		{"substrate.reamde.dev/applets/applet", true, false, false},
		{"substrate.reamde.dev/console/view", true, false, false},
		{"substrate.reamde.dev/core/token", true, false, false},
		{"substrate.reamde.dev/core/secret", true, false, false},
	} {
		if got := everything.Reads(Kind{Ref: c.kind, Internal: c.internal}); got != c.reads {
			t.Errorf("write:* Reads(%s) = %v, want %v", c.kind, got, c.reads)
		}
		if got := everything.Writes(Kind{Ref: c.kind, Internal: c.internal}); got != c.writes {
			t.Errorf("write:* Writes(%s) = %v, want %v", c.kind, got, c.writes)
		}
	}
	// The auth kinds are never reached, even read as if they were not
	// internal: the guard is the kind, not the purpose.
	for _, k := range []string{"token", "credential", "secret"} {
		kind := "substrate.reamde.dev/core/" + k
		if everything.Reads(Kind{Ref: kind, Internal: false}) || everything.Writes(Kind{Ref: kind, Internal: false}) {
			t.Errorf("write:* reached the auth kind %s", kind)
		}
	}
	// Even a non-internal kind of core or llm is never written.
	if everything.Writes(Kind{Ref: "substrate.reamde.dev/core/unit", Internal: false}) {
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
	full, err := Parse("decide:ada.example.com/chat/*")
	if err != nil {
		t.Fatal(err)
	}
	if got := full.Localize("ada.example.com").String(); got != s.String() {
		t.Errorf("Localize = %q, want %q", got, s)
	}
	if err := CheckStored([]string{"agents:~/chat/*"}); err != nil {
		t.Errorf("a stored agents scope over the repository's own agents refused: %v", err)
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
		if g.Reads(Kind{Ref: kind, Internal: true}) || g.Writes(Kind{Ref: kind, Internal: true}) {
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

func TestAWebhookScopeIsStoredNeverRequestedAndReachesOneTrigger(t *testing.T) {
	for _, raw := range []string{"webhook:*", "webhook:", "webhook:ada.example.com/*", "webhook:apple health"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) admitted, want refused: a webhook scope names one trigger's id", raw)
		}
	}
	// Only an approved webhook detail puts the verb on a token: an app that
	// writes it into `scope` is refused, whatever else it asks.
	if _, err := ParseRequest("read:~/tasks/* webhook:applehealth-webhook"); err == nil {
		t.Error("a request asked for webhook: in scope and was admitted")
	}
	g, err := NewGrant([]string{"write:providers.example.com/apple/*", WebhookScope("applehealth-webhook").String()})
	if err != nil {
		t.Fatalf("a stored webhook scope does not parse: %v", err)
	}
	if !g.Delivers("applehealth-webhook") {
		t.Error("the grant does not deliver to the trigger it names")
	}
	if g.Delivers("whoop-webhook") || g.Delivers("") {
		t.Error("the grant delivered to a trigger it does not name")
	}
	// It reads and writes no kind whole, the trigger kind least of all.
	if g.Reads(Kind{Ref: "substrate.reamde.dev/core/trigger", Internal: true}) || g.Writes(Kind{Ref: "substrate.reamde.dev/core/trigger", Internal: true}) {
		t.Error("a webhook scope reached the trigger kind")
	}
	only, err := NewGrant([]string{"webhook:applehealth-webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if only.Reads(Kind{Ref: "providers.example.com/apple/contact", Internal: false}) {
		t.Error("a webhook scope read a kind")
	}
}

// A grant names a package through a scope that reaches into it: a read or
// write over its kinds, or an agents or decide scope over its agents, since
// a package of agents alone is reached by nothing else (decision 0214). A
// webhook scope names one trigger.
func TestAGrantNamesAPackageThroughTheScopesThatReachIntoIt(t *testing.T) {
	const pkg = "providers.example.com/applehealth"
	for _, stored := range [][]string{
		{"read:providers.example.com/applehealth/account"},
		{"write:providers.example.com/applehealth/*"},
		{"read:providers.example.com/*"},
		{"read:*"},
		{"agents:providers.example.com/applehealth/*"},
		{"decide:*"},
	} {
		g, err := NewGrant(stored)
		if err != nil {
			t.Fatalf("NewGrant(%v): %v", stored, err)
		}
		if !g.NamesPackage(pkg) {
			t.Errorf("%v does not name %s", stored, pkg)
		}
	}
	for _, stored := range [][]string{
		// Another package of the same authority, one whose word only
		// starts with this one's, and another authority.
		{"read:providers.example.com/apple/contact"},
		{"write:providers.example.com/apple/*"},
		{"read:providers.example.com/applehealthx/*"},
		{"read:example.com/*"},
		{"agents:providers.example.com/apple/*"},
		// A webhook scope names one trigger, never a package.
		{"webhook:applehealth-webhook"},
	} {
		g, err := NewGrant(stored)
		if err != nil {
			t.Fatalf("NewGrant(%v): %v", stored, err)
		}
		if g.NamesPackage(pkg) {
			t.Errorf("%v names %s, want not", stored, pkg)
		}
	}
	g, _ := NewGrant([]string{"read:*"})
	if g.NamesPackage("") {
		t.Error("a grant named the empty package")
	}
}

// A scope already held is one the grant covers, whatever its spelling: the
// same verb or a write for a read, over a pattern at least as wide, and an
// internal kind only through its exact scope.
func TestAGrantCoversWhatItHoldsByReachNotSpelling(t *testing.T) {
	const kind = "providers.example.com/notes/note"
	cases := []struct {
		held     []string
		want     string
		internal bool
		covers   bool
	}{
		{[]string{"read:" + kind}, "read:" + kind, false, true},
		{[]string{"write:" + kind}, "read:" + kind, false, true},
		{[]string{"read:" + kind}, "write:" + kind, false, false},
		{[]string{"read:*"}, "read:providers.example.com/notes/*", false, true},
		{[]string{"read:providers.example.com/*"}, "read:providers.example.com/notes/*", false, true},
		{[]string{"write:providers.example.com/notes/*"}, "read:" + kind, false, true},
		{[]string{"read:providers.example.com/notes/*"}, "read:providers.example.com/*", false, false},
		{[]string{"read:providers.example.com/notes/*"}, "read:providers.example.com/notesx/*", false, false},
		{[]string{"read:providers.example.com/*"}, "read:example.com/notes/*", false, false},
		// A glob never reaches an internal kind, so it never covers one.
		{[]string{"read:*"}, "read:" + kind, true, false},
		{[]string{"read:" + kind}, "read:" + kind, true, true},
		{[]string{"write:" + kind}, "read:" + kind, true, true},
		// The agent verbs keep their own grammar, and a read covers none.
		{[]string{"agents:*"}, "agents:ada.example.com/chat/assistant", true, true},
		{[]string{"agents:ada.example.com/chat/*"}, "agents:ada.example.com/*", false, false},
		{[]string{"read:*"}, "agents:ada.example.com/chat/assistant", false, false},
		{[]string{"agents:*"}, "decide:*", false, false},
		// A webhook scope covers its own trigger alone.
		{[]string{"webhook:applehealth-webhook"}, "webhook:applehealth-webhook", false, true},
		{[]string{"webhook:applehealth-webhook"}, "webhook:whoop-webhook", false, false},
		{[]string{"read:*", "write:*"}, "webhook:applehealth-webhook", false, false},
	}
	for _, c := range cases {
		g, err := NewGrant(c.held)
		if err != nil {
			t.Fatalf("NewGrant(%v): %v", c.held, err)
		}
		want, err := Parse(c.want)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.want, err)
		}
		if got := g.Covers(want, c.internal); got != c.covers {
			t.Errorf("%v covers %s (internal %v) = %v, want %v", c.held, c.want, c.internal, got, c.covers)
		}
	}
}

// A trigger id is one predicate on every surface that names one, and a
// refusal names the verbs where they apply: seven stored, six requested.
func TestAVerbRefusalNamesTheVerbsItsSurfaceTakes(t *testing.T) {
	if _, err := Parse("webhook:a/b"); err == nil {
		t.Error("a webhook scope naming a path was admitted")
	}
	_, err := Parse("delete:*")
	if err == nil || !strings.Contains(err.Error(), "read, write, agents, decide, call or webhook") {
		t.Errorf("a stored scope's unknown verb = %v, want the six verbs named", err)
	}
	_, err = ParseRequest("delete:*")
	if err == nil || !strings.Contains(err.Error(), "read, write, agents, decide or call") || strings.Contains(err.Error(), "webhook") {
		t.Errorf("a request's unknown verb = %v, want the five requestable verbs named", err)
	}
}

// DECLARE IS RETIRED (decision 0214): a request naming it is refused with
// words pointing at a `package` detail, whatever package it names, and a
// stored token carrying it fails closed rather than reading as a narrower
// grant, so the app signs in again under the new grammar.
func TestADeclareScopeIsRefusedInARequestAndFailsClosedWhenStored(t *testing.T) {
	for _, raw := range []string{"declare:~/training", "declare:ada.example.com/training", "declare:*", "declare:~"} {
		_, err := ParseRequest(raw + " write:~/training/*")
		if err == nil || !strings.Contains(err.Error(), "retired") || !strings.Contains(err.Error(), "`package` authorization detail") {
			t.Errorf("ParseRequest(%q) = %v, want a refusal pointing at a package detail", raw, err)
		}
	}
	for _, stored := range [][]string{
		{"declare:ada.example.com/training"},
		{"write:ada.example.com/training/*", "declare:ada.example.com/training"},
	} {
		if err := CheckStored(stored); err == nil || !strings.Contains(err.Error(), "retired") {
			t.Errorf("CheckStored(%v) = %v, want a refusal naming the retired verb", stored, err)
		}
		if _, err := NewGrant(stored); err == nil {
			t.Errorf("NewGrant(%v) read a token carrying declare as a grant", stored)
		}
	}
}

func TestFullIsRequestedAloneAndNeverStored(t *testing.T) {
	got, err := ParseRequest("full offline_access full")
	if err != nil {
		t.Fatalf("ParseRequest(full) = %v", err)
	}
	if len(got) != 1 || got[0] != FullScope || got[0].String() != "full" || !RequestsFull(got) {
		t.Fatalf("ParseRequest(full) = %v, want the full scope once", got)
	}
	// Anything beside it is either covered by it or a misread of it, and the
	// owner approves it whole: the request is refused, not narrowed.
	for _, raw := range []string{"full read:*", "agents:* full"} {
		if _, err := ParseRequest(raw); err == nil {
			t.Errorf("ParseRequest(%q) admitted, want full refused beside another scope", raw)
		}
	}
	for _, raw := range []string{"full:", "full:*", "Full"} {
		if _, err := ParseRequest(raw); err == nil {
			t.Errorf("ParseRequest(%q) admitted, want refused: full is the bare word", raw)
		}
	}
	// A full approval mints a token without scopes, so a stored `full` is a
	// damaged token and refuses rather than reads as a grant.
	if _, err := Parse("full"); err == nil {
		t.Error("Parse(full) admitted, want refused: it is never stored")
	}
	if err := CheckStored([]string{"full"}); err == nil {
		t.Error("CheckStored([full]) admitted")
	}
	if _, err := NewGrant([]string{"full"}); err == nil {
		t.Error("NewGrant([full]) admitted")
	}
	if RequestsFull([]Scope{{Verb: Read, Pattern: "*"}}) {
		t.Error("read:* read as full")
	}
}

// An assistant's scope (decision 0230) is asked for alone like `full`, but
// it IS stored, as `["mcp"]`, so a binary that does not know the word fails
// the token closed; and it holds no grant, so a path that reads one for it
// refuses the token.
func TestMCPIsRequestedAloneStoredAloneAndHoldsNoGrant(t *testing.T) {
	got, err := ParseRequest("mcp offline_access mcp")
	if err != nil {
		t.Fatalf("ParseRequest(mcp) = %v", err)
	}
	if len(got) != 1 || got[0] != MCPScope || got[0].String() != "mcp" || !RequestsMCP(got) || !RequestsWhole(got) || RequestsFull(got) {
		t.Fatalf("ParseRequest(mcp) = %v, want the mcp scope once", got)
	}
	for _, raw := range []string{"mcp read:*", "agents:* mcp", "mcp full", "full mcp"} {
		if _, err := ParseRequest(raw); err == nil {
			t.Errorf("ParseRequest(%q) admitted, want mcp refused beside another scope", raw)
		}
	}
	for _, raw := range []string{"mcp:", "mcp:*", "MCP"} {
		if _, err := ParseRequest(raw); err == nil {
			t.Errorf("ParseRequest(%q) admitted, want refused: mcp is the bare word", raw)
		}
	}
	if !IsMCP([]string{"mcp"}) || IsMCP(nil) || IsMCP([]string{"mcp", "read:*"}) || IsMCP([]string{"read:*"}) {
		t.Error("IsMCP misreads a stored list")
	}
	if err := CheckStored([]string{"mcp"}); err != nil {
		t.Errorf("CheckStored([mcp]) = %v, want admitted: the token stores it", err)
	}
	for _, stored := range [][]string{{"mcp", "read:example.org/*"}, {"read:example.org/*", "mcp"}, {"mcp", "mcp"}} {
		if err := CheckStored(stored); err == nil {
			t.Errorf("CheckStored(%v) admitted, want mcp stored alone", stored)
		}
	}
	if _, err := NewGrant([]string{"mcp"}); err == nil {
		t.Error("NewGrant([mcp]) admitted, want refused: an assistant's token holds no grant")
	}
	approved, err := ApprovalOf([]string{"mcp"})
	if err != nil || !approved.Full() {
		t.Errorf("ApprovalOf([mcp]) = %v, %v; want it whole", approved, err)
	}
}

// A package of agents alone, such as a coach, is named by the scope that
// talks to them, so the app may learn whether it is set up; a webhook scope
// names one trigger and never a package.
func TestAnAgentScopeNamesItsPackage(t *testing.T) {
	g, err := NewGrant([]string{"agents:substrate.reamde.dev/trainingcoach/*", "decide:example.org/notes/scribe", "webhook:example.org-hook"})
	if err != nil {
		t.Fatal(err)
	}
	for pkg, want := range map[string]bool{
		"substrate.reamde.dev/trainingcoach": true,
		"example.org/notes":                  true,
		"substrate.reamde.dev/training":      false,
		"example.org":                        false,
	} {
		if got := g.NamesPackage(pkg); got != want {
			t.Errorf("NamesPackage(%q) = %v, want %v", pkg, got, want)
		}
	}
}
