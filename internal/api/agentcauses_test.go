package api

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// agentCausesGoldenPath is in the console's tree, beside the reasons' golden
// and for the same reason: a vitest beside it holds the console's union of
// causes, and the sentence it has for each, to it.
const agentCausesGoldenPath = "../../web/console/src/lib/api/agentcauses.golden.json"

const agentsDocPath = "../../docs/agents.md"

// The golden is the closed set of causes a run settles with
// (substrate.AgentCauses), sorted. Written by `-update`, never by hand.
func TestAgentCausesMatchTheGolden(t *testing.T) {
	causes := slices.Sorted(slices.Values(substrate.AgentCauses))
	if len(slices.Compact(slices.Clone(causes))) != len(causes) {
		t.Fatalf("substrate.AgentCauses lists a cause twice: %v", causes)
	}
	encoded, err := json.MarshalIndent(causes, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	if *updateGolden {
		if err := os.WriteFile(agentCausesGoldenPath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", agentCausesGoldenPath)
		return
	}

	want, err := os.ReadFile(agentCausesGoldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with `go test ./internal/api/ -run TestAgentCauses -update`): %v", err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the causes have moved and %s is stale.\n\n"+
			"Regenerate it:\n"+
			"  go test ./internal/api/ -run TestAgentCauses -update\n\n"+
			"then make TurnCause in web/console/src/lib/api/agents.ts match — `pnpm test` in\n"+
			"web/console fails until it does.\n\ngot:\n%s\nwant:\n%s",
			agentCausesGoldenPath, encoded, want)
	}
}

// docs/agents.md states the closed set of causes in the paragraph that
// starts `The closed set of causes:`.
func TestAgentCausesDocsListTheClosedSet(t *testing.T) {
	raw, err := os.ReadFile(agentsDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentsDocPath, err)
	}
	const opening = "The closed set of causes:"
	var paragraph []string
	for line := range strings.Lines(string(raw) + "\n\n") {
		line = strings.TrimSpace(line)
		switch {
		case paragraph == nil && strings.HasPrefix(line, opening):
			paragraph = []string{line}
		case paragraph != nil && line != "":
			paragraph = append(paragraph, line)
		case paragraph != nil:
			got := []string{}
			for _, m := range backticked.FindAllStringSubmatch(strings.Join(paragraph, " "), -1) {
				got = append(got, m[1])
			}
			if want := slices.Sorted(slices.Values(substrate.AgentCauses)); !slices.Equal(got, want) {
				t.Errorf("%s lists the closed set of causes as\n  %v\nand the code's is\n  %v\n"+
					"Make the paragraph that starts %q list the code's, in that order, each in backticks.",
					agentsDocPath, got, want, opening)
			}
			return
		}
	}
	t.Fatalf("%s has no paragraph that starts %q", agentsDocPath, opening)
}
