package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAgentReachIncludesToolsSubagentsAndCycles(t *testing.T) {
	for _, via := range []string{"tools", "subagents", "toolsFrom"} {
		t.Run(via, func(t *testing.T) {
			e, _ := newCallEnv(t)
			props := map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{taskKind}}}}
			switch via {
			case "tools":
				props["tools"] = []any{map[string]any{"function": map[string]any{"ref": kindFunction + "/" + reportFn}}}
			case "subagents":
				props["subagents"] = []any{map[string]any{"ref": kindAgent + "/" + otherAgent}}
				e.seed(substrate.Record{Kind: kindAgent, ID: otherAgent, Properties: map[string]any{"tools": []any{map[string]any{"function": reportFn}}, "subagents": []any{talkedAgent}}}, nil)
			case "toolsFrom":
				props["toolsFrom"] = []any{map[string]any{"functions": reportFn}}
				e.seed(substrate.Record{Kind: kindFunction, ID: reportFn, Properties: map[string]any{"offer": map[string]any{"toolName": "report"}, "permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}, "agents": []any{talkedAgent}}}}, nil)
			}
			e.seed(substrate.Record{Kind: kindAgent, ID: talkedAgent, Properties: props}, nil)
			for _, covered := range []bool{false, true} {
				scopes := []string{"call:" + runnerFn, "read:" + taskKind}
				if covered {
					scopes = append(scopes, "read:"+personKind)
				}
				grant, err := scope.NewGrant(scopes)
				if err != nil {
					t.Fatal(err)
				}
				ds := newScopedDataset(e.svc.datasets[fakeRepository], grant, substrate.ActorAPI)
				got, err := ds.coversAgentReads(context.Background(), talkedAgent)
				if err != nil || got != covered {
					t.Fatalf("chat coverage = %v, %v, want %v", got, err, covered)
				}
				bearer := e.grant("call:"+runnerFn+" read:"+taskKind+" read:"+personKind, scopes)
				response := e.do(http.MethodPost, callPath(runnerFn), bearer, []byte(`{"input":{}}`), "application/json")
				want := http.StatusForbidden
				if covered {
					want = http.StatusOK
				}
				wantStatus(t, response, want)
			}
		})
	}
}

func TestAgentReachExemptsOnlyAskingAgentsInteractionWrites(t *testing.T) {
	e, _ := newCallEnv(t)
	for _, asks := range []bool{true, false} {
		props := map[string]any{"permissions": map[string]any{"writes": []any{vocabulary.KindLLMInteraction}, "reads": map[string]any{"kinds": []any{taskKind}}}}
		if asks {
			props["tools"] = []any{map[string]any{"function": vocabulary.HostFunctionAsk}, map[string]any{"function": vocabulary.HostFunctionQuery}}
		}
		rec := &substrate.Record{Kind: kindAgent, ID: talkedAgent, Properties: props}
		reach, err := resolveReach(context.Background(), e.svc.datasets[fakeRepository], rec)
		if err != nil {
			t.Fatal(err)
		}
		if (len(reach.writes) == 0) != asks || len(reach.reads) != 1 || reach.reads[0] != taskKind {
			t.Fatalf("asks %v: %+v", asks, reach)
		}
	}
}

func TestMissingKindReachRequiresExactScope(t *testing.T) {
	const missing = "absent.example/data/secret"
	for _, pattern := range []string{"absent.example/data/*", missing} {
		grant, err := scope.NewGrant([]string{"read:" + pattern})
		if err != nil {
			t.Fatal(err)
		}
		got := uncoveredReach(grant, functionReach{reads: []string{missing}}, nil)
		if (len(got) == 0) != (pattern == missing) {
			t.Fatalf("%s covers missing kind: %v", pattern, got)
		}
	}
}
