package engine

import (
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
)

func TestReplyMentionsReadsRecordLinks(t *testing.T) {
	const task = "ada.example.com/tasks/task"
	const decl = "substrate.reamde.dev/core/kind"
	for _, tc := range []struct {
		name  string
		reply string
		want  []substrate.AgentMention
	}{
		{"none", "nothing linked, [a page](https://example.com)", nil},
		{"one", "see [Q4 budget](substrate://" + task + "/q4)", []substrate.AgentMention{{Kind: task, ID: "q4", Text: "Q4 budget"}}},
		{
			"legacy ref and scheme case", "[a](ref:" + task + "/a) [b](SUBSTRATE://" + task + "/b)",
			[]substrate.AgentMention{{Kind: task, ID: "a", Text: "a"}, {Kind: task, ID: "b", Text: "b"}},
		},
		{
			"duplicates keep the first", "[one](substrate://" + task + "/a) and [two](substrate://" + task + "/a)",
			[]substrate.AgentMention{{Kind: task, ID: "a", Text: "one"}},
		},
		{
			"angle target and title", `[x](<substrate://` + task + `/x> "the x")`,
			[]substrate.AgentMention{{Kind: task, ID: "x", Text: "x"}},
		},
		{
			"empty label reads as the id", "[](substrate://" + task + "/x)",
			[]substrate.AgentMention{{Kind: task, ID: "x", Text: "x"}},
		},
		{
			"escaped brackets in label", `[a \[b\]](substrate://` + task + `/x)`,
			[]substrate.AgentMention{{Kind: task, ID: "x", Text: "a [b]"}},
		},
		{"nested brackets are no link", `[a [b]](substrate://` + task + `/x)`, nil},
		{
			"escape in label", `[a\_b](substrate://` + task + `/x)`,
			[]substrate.AgentMention{{Kind: task, ID: "x", Text: "a_b"}},
		},
		{
			"a declaration id has slashes", "[task](substrate://" + decl + "/" + task + ")",
			[]substrate.AgentMention{{Kind: decl, ID: task, Text: "task"}},
		},
		{"code span", "`[a](substrate://" + task + "/a)` and ``[b](substrate://" + task + "/b)``", nil},
		{
			"unclosed code span is text", "a ` then [a](substrate://" + task + "/a)",
			[]substrate.AgentMention{{Kind: task, ID: "a", Text: "a"}},
		},
		{
			"fenced block", "```md\n[a](substrate://" + task + "/a)\n```\n~~~\n[b](substrate://" + task + "/b)\n~~~\n[c](substrate://" + task + "/c)",
			[]substrate.AgentMention{{Kind: task, ID: "c", Text: "c"}},
		},
		{"unclosed fence hides the rest", "```\n[a](substrate://" + task + "/a)", nil},
		{"not a record path", "[a](substrate://nodot/tasks/task/a) [b](substrate://" + task + ") [c](substrate://" + task + "/)", nil},
		{"bare path is prose", "see " + task + "/q4", nil},
		{"broken link", "[a](substrate://" + task + "/a", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := replyMentions(tc.reply); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("replyMentions(%q) = %+v, want %+v", tc.reply, got, tc.want)
			}
		})
	}
}

func TestReplyMentionsIsBounded(t *testing.T) {
	reply := ""
	for i := range maxReplyMentions + 5 {
		reply += "[r](substrate://ada.example.com/tasks/task/" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ") "
	}
	if got := len(replyMentions(reply)); got != maxReplyMentions {
		t.Fatalf("mentions: %d", got)
	}
}

func TestCarriedLooksPastTheModelsOwnTurns(t *testing.T) {
	const kind = "ada.example.com/tasks/task"
	seen := []llm.Message{
		{Role: llm.RoleUser, Content: "about substrate://" + kind + "/from-user"},
		{Role: llm.RoleAssistant, Content: kind + "/said-by-model"},
		{Role: llm.RoleTool, Content: `{"kind":"` + kind + `","id":"from-tool"}`},
		{Role: llm.RoleTool, Content: `{"kind":"other.example.com/x/y","id":"wrong-kind"}`},
	}
	for id, want := range map[string]bool{"from-user": true, "from-tool": true, "said-by-model": false, "wrong-kind": false} {
		if got := carried(seen, kind, id); got != want {
			t.Errorf("carried(%s) = %v, want %v", id, got, want)
		}
	}
}

func TestTheGuidanceIsStatic(t *testing.T) {
	// The block sits inside the cached prefix: nothing in it may vary.
	got := composeSystem("P\n", "C", engineGuidance, "")
	if got != "P\n\nC\n\n"+engineGuidance {
		t.Fatalf("composeSystem with guidance = %q", got)
	}
	if got := composeSystem("P\n", "C", "", "J"); got != "P\n\nC\n\nJ" {
		t.Fatalf("composeSystem without guidance = %q", got)
	}
}

func TestAProviderRowMayNotSetTheGuidance(t *testing.T) {
	if _, err := mergeParams("openai", map[string]any{"engineGuidance": false}, nil); err == nil {
		t.Fatal("a provider row's defaults turned the guidance off")
	}
	p, err := mergeParams("openai", map[string]any{"maxTokens": 10}, map[string]any{"engineGuidance": false})
	if err != nil || p.MaxTokens != 10 {
		t.Fatalf("an agent's own engineGuidance: %+v, %v", p, err)
	}
}
