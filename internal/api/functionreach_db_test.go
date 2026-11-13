package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAgentReachIncludesToolsSubagentsAndCycles(t *testing.T) {
	t.Parallel()
	for _, via := range []string{"tools", "subagents", "toolsFrom"} {
		t.Run(via, func(t *testing.T) {
			t.Parallel()
			env := newDBEnv(t, withSamples("tasks"), withAppFlow(), withEngine(engine.WithInsecureForeignPackages(true)))
			tasks, chat := env.repository+"/tasks", env.repository+"/chat"
			report, runner, talked := tasks+"/report", tasks+"/runner", chat+"/assistant"
			reportPerms := map[string]any{"reads": map[string]any{"kinds": []any{taskKind, personKind}}}
			props := map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{taskKind}}}}
			reportData := map[string]any{"permissions": reportPerms}
			docs := []map[string]any{vocabulary.PackageManifest(chat, 1)}
			switch via {
			case "tools":
				props["tools"] = []any{map[string]any{"function": report}}
			case "subagents":
				props["subagents"] = []any{otherAgent}
				docs = append(docs,
					vocabulary.PackageManifest("other.example.com/tools", 1),
					agentReachAgent("other.example.com/tools", "helper", map[string]any{
						"tools": []any{map[string]any{"function": report}}, "subagents": []any{talked},
					}))
			case "toolsFrom":
				props["toolsFrom"] = []any{map[string]any{"functions": report}}
				reportData = map[string]any{
					"offer":       map[string]any{"agents": true, "name": "report"},
					"effect":      vocabulary.FunctionEffectRead,
					"permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}, "agents": []any{talked}},
				}
			}
			docs = append(docs,
				agentReachAgent(chat, "assistant", props),
				vocabulary.PackageManifest(tasks, 1),
				agentReachFunction(tasks, "report", reportData),
				agentReachFunction(tasks, "runner", map[string]any{"permissions": map[string]any{"agents": []any{talked}}}),
			)
			appAgentsApply(t, env, docs...)
			for _, covered := range []bool{false, true} {
				scopes := []string{"call:" + runner, "read:" + taskKind}
				if covered {
					scopes = append(scopes, "read:"+personKind)
				}
				grant, err := scope.NewGrant(scopes)
				if err != nil {
					t.Fatal(err)
				}
				ds := newScopedDataset(env.ds, grant, substrate.ActorPerson)
				got, err := ds.coversAgentReads(context.Background(), talked)
				if err != nil || got != covered {
					t.Fatalf("chat coverage = %v, %v, want %v", got, err, covered)
				}
				bearer := appScopedToken(t, env, scopes...)
				response := appScopedDo(t, env, http.MethodPost, callPath(runner), bearer, []byte(`{"input":{}}`), "application/json")
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
	t.Parallel()
	env := newDBEnv(t)
	for _, asks := range []bool{true, false} {
		props := map[string]any{"permissions": map[string]any{"writes": []any{vocabulary.KindLLMInteraction}, "reads": map[string]any{"kinds": []any{taskKind}}}}
		if asks {
			props["tools"] = []any{map[string]any{"function": vocabulary.HostFunctionAsk}, map[string]any{"function": vocabulary.HostFunctionQuery}}
		}
		rec := &substrate.Record{Kind: kindAgent, ID: env.repository + "/chat/assistant", Properties: props}
		reach, err := resolveReach(context.Background(), env.ds, rec)
		if err != nil {
			t.Fatal(err)
		}
		if (len(reach.writes) == 0) != asks || len(reach.reads) != 1 || reach.reads[0] != taskKind {
			t.Fatalf("asks %v: %+v", asks, reach)
		}
	}
}
