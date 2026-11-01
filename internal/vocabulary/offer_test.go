package vocabulary_test

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// offerAuthority is agAuthority with one more function whose data tail is
// fnData, and the classifier agent carrying agData.
func offerAuthority(fnData, agData string) string {
	return strings.Replace(agAuthority(`  description: classifies widgets
  prompt: You classify widgets.
  provider: default
  model: claude-opus-5
`+agData), "---\nkind: substrate.reamde.dev/core/agent\nmetadata:\n  id: ag.example.com/ag/sorter", `---
kind: substrate.reamde.dev/core/function
metadata:
  id: ag.example.com/ag/todayFeed
data:
  authority: ag.example.com
  package: ag
  description: what is on today
  runtime: python
  source: "def main(input, host): return {}"
`+fnData+`---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ag.example.com/ag/sorter`, 1)
}

func TestFunctionOfferLoads(t *testing.T) {
	for _, tc := range []struct {
		name, fnData, wantName string
	}{
		{"derived", "  effect: read\n  offer: {agents: true}\n", "ag_todayFeed"},
		{"named", "  effect: read\n  offer: {agents: true, name: today}\n", "today"},
		{"withheld", "  effect: read\n  offer: {agents: false}\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := loadAgAuthority(t, offerAuthority(tc.fnData, "  toolsFrom:\n    - {functions: \"*\"}\n"))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			fn, err := r.ResolveFunction("ag.example.com/ag/todayFeed")
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.wantName == "" && fn.Offer != nil:
				t.Fatalf("offer %+v, want none", fn.Offer)
			case tc.wantName != "" && (fn.Offer == nil || fn.Offer.ToolName != tc.wantName):
				t.Fatalf("offer %+v, want tool name %q", fn.Offer, tc.wantName)
			}
			ag, err := r.ResolveAgent("ag.example.com/ag/classifier")
			if err != nil {
				t.Fatal(err)
			}
			if !ag.TakesOffered("ag.example.com/ag/todayFeed") {
				t.Fatalf("toolsFrom %v does not take the function", ag.ToolsFrom)
			}
		})
	}
}

func TestFunctionOfferRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, fnData, want string
	}{
		{"no effect", "  offer: {agents: true}\n", "declares its effect"},
		{"no audience", "  effect: read\n  offer: {name: today}\n", "data.offer.agents"},
		{"not a map", "  effect: read\n  offer: true\n", "data.offer: a map"},
		{"bad name", "  effect: read\n  offer: {agents: true, name: \"to day\"}\n", "data.offer.name"},
		{"unknown key", "  effect: read\n  offer: {agents: true, description: x}\n", `unknown key "description"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadAgAuthority(t, offerAuthority(tc.fnData, ""))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAgentToolsFromPatterns(t *testing.T) {
	r, err := loadAgAuthority(t, offerAuthority("", `  toolsFrom:
    - {functions: ag.example.com/*}
    - {functions: other.example.com/feeds/*}
    - {functions: third.example.com/x/today}
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, _ := r.ResolveAgent("ag.example.com/ag/classifier")
	for ident, want := range map[string]bool{
		"ag.example.com/ag/todayFeed":   true,
		"other.example.com/feeds/a":     true,
		"other.example.com/other/a":     false,
		"third.example.com/x/today":     true,
		"third.example.com/x/yesterday": false,
	} {
		if got := ag.TakesOffered(ident); got != want {
			t.Fatalf("TakesOffered(%s) = %v, want %v", ident, got, want)
		}
	}
	for _, bad := range []string{"todayFeed", "ag.example.com/ag/to*", `""`} {
		_, err := loadAgAuthority(t, offerAuthority("", "  toolsFrom:\n    - {functions: "+bad+"}\n"))
		if err == nil || !strings.Contains(err.Error(), "data.toolsFrom[0]") {
			t.Fatalf("toolsFrom %s: err = %v", bad, err)
		}
	}
}

func TestOfferedToolNameIsPackageAndName(t *testing.T) {
	if got := vocabulary.OfferedToolName("feeds.example.com/feeds", "today"); got != "feeds_today" {
		t.Fatalf("got %q", got)
	}
}
