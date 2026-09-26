package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// An empty token takes the separator joining it to its neighbor with it, so
// a heading never opens on ": " or closes on " +". Literal text is the
// author's and stays; a value is the writer's and is never touched.
func TestTemplateEmptyTokenDropsItsSeparator(t *testing.T) {
	for _, tc := range []struct {
		template string
		props    map[string]string
		want     string
	}{
		// The case that forced it: a merge request with no decision yet.
		{"{decision}: {winner} + {loser}", map[string]string{"winner": "Grace Hopper", "loser": "Grace B. Hopper"}, "Grace Hopper + Grace B. Hopper"},
		{"{decision}: {winner} + {loser}", map[string]string{"decision": "proposed", "winner": "Grace Hopper"}, "proposed: Grace Hopper"},
		{"{decision}: {winner} + {loser}", map[string]string{"decision": "proposed", "loser": "Grace B. Hopper"}, "proposed: Grace B. Hopper"},
		{"{decision}: {winner} + {loser}", map[string]string{"decision": "proposed", "winner": "A", "loser": "B"}, "proposed: A + B"},
		{"{decision}: {winner} + {loser}", nil, ""},

		{"{a} {b}", map[string]string{"b": "B"}, "B"},
		{"{a} {b}", map[string]string{"a": "A"}, "A"},
		{"{a}: {b}", map[string]string{"b": "B"}, "B"},
		{"{a}: {b}", map[string]string{"a": "A"}, "A"},
		{"{a} — {b}", map[string]string{"a": "A"}, "A"},
		{"{a} — {b}", map[string]string{"b": "B"}, "B"},
		{"{winner} ← {loser}", map[string]string{"loser": "L"}, "L"},
		{"{a} {b} {c}", map[string]string{"a": "A", "c": "C"}, "A C"},
		{"{a}: {b}.", map[string]string{"a": "A"}, "A"},

		// A separator between two surviving neighbors is kept once, never
		// twice and never zero times.
		{"{authority}/{package}/{localName}", map[string]string{"authority": "x.dev", "localName": "task"}, "x.dev/task"},

		// A bracket pair around an empty token goes with it.
		{"{label} ({wire})", map[string]string{"label": "Label"}, "Label"},
		{"{label} ({wire})", map[string]string{"wire": "w"}, "(w)"},
		{"{a} ({b}) - {c}", map[string]string{"a": "A", "c": "C"}, "A - C"},

		// A sigil before a token is the token's.
		{"{state} #{localName}", map[string]string{"state": "open"}, "open"},
		{"{state} #{localName}", map[string]string{"localName": "42"}, "#42"},
		{"{state} #{localName}", map[string]string{"state": "open", "localName": "42"}, "open #42"},

		// Words in a literal are the author's: only the separator goes.
		{"Issue {x}", nil, "Issue"},
		{"Issue {x}", map[string]string{"x": "7"}, "Issue 7"},
		{"Beeper at {apiBase}", nil, "Beeper at"},
		{"Q3: {plan}", nil, "Q3"},
		{"{a}: Issue {b}", map[string]string{"b": "7"}, "Issue 7"},

		// Alternatives are one token: it is empty only when every one is.
		{"{a|b}: {c}", map[string]string{"b": "B", "c": "C"}, "B: C"},
		{"{a|b}: {c}", map[string]string{"c": "C"}, "C"},

		// Nothing empty, nothing touched: literals and values alike.
		{"C++", nil, "C++"},
		{"Q3: plan", nil, "Q3: plan"},
		{"{a}", map[string]string{"a": ": Q3: plan +"}, ": Q3: plan +"},
		{"{a} + {b}", map[string]string{"a": "C++", "b": "-5 degrees"}, "C++ + -5 degrees"},
		{"Slack connection", nil, "Slack connection"},
	} {
		tpl, err := vocabulary.ParseTemplate(tc.template)
		if err != nil {
			t.Fatalf("%q: %v", tc.template, err)
		}
		if got := tpl.Render(testResolver{props: tc.props}); got != tc.want {
			t.Errorf("%q over %v = %q, want %q", tc.template, tc.props, got, tc.want)
		}
	}
}
