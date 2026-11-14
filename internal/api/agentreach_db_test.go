package api

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// agentReachFunction is a function of pkg whose body does nothing, declaring
// data beside its card.
func agentReachFunction(pkg, name string, data map[string]any) map[string]any {
	data["description"] = name + " under test"
	data["runtime"] = vocabulary.RuntimePython
	data["source"] = "\ndef main(input, host):\n    return {\"output\": {}}\n"
	return vocabulary.FunctionManifest(pkg, name, data)
}

// agentReachAgent is an agent of pkg on the fixture's provider row,
// declaring data beside its card.
func agentReachAgent(pkg, name string, data map[string]any) map[string]any {
	data["description"] = name + " under test"
	data["prompt"] = "You are " + name + "."
	data["provider"] = appAgentsLLMProvider
	data["model"] = name
	return vocabulary.AgentManifest(pkg, name, data)
}

func TestChatCoverageFollowsAnAgentsFunctionTool(t *testing.T) {
	t.Parallel()
	const tool = "other.example.com/tools/lookup"
	env := newDBEnv(t, withSamples("people"), withEngine(engine.WithInsecureForeignPackages(true)))
	talked := env.repository + "/chat/assistant"
	appAgentsApply(t, env,
		vocabulary.PackageManifest("other.example.com/tools", 1),
		agentReachFunction("other.example.com/tools", "lookup", map[string]any{
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}},
		}),
		vocabulary.PackageManifest(env.repository+"/chat", 1),
		agentReachAgent(env.repository+"/chat", "assistant", map[string]any{
			"tools": []any{map[string]any{"function": tool}},
		}),
	)
	for _, covered := range []bool{false, true} {
		scopes := []string{"agents:" + talked}
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
			t.Fatalf("coverage=%v, %v; want %v", got, err, covered)
		}
	}
}

func TestCallAndChatGlobsRespectInternalKinds(t *testing.T) {
	t.Parallel()
	const hidden = "other.example.com/tools/hidden"
	for _, pattern := range []string{"*", "substrate.reamde.dev/llm/*", "other.example.com/tools/*"} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			env := newDBEnv(t, withEngine(engine.WithInsecureForeignPackages(true)))
			tasks, chat := env.repository+"/tasks", env.repository+"/chat"
			// Declared under the full name, and named under `~`, which is how
			// a token's grant holds them (decision record 0259).
			summarize, talked := "~/tasks/summarize", "~/chat/assistant"
			appAgentsApply(t, env,
				vocabulary.PackageManifest("other.example.com/tools", 1),
				vocabulary.KindManifest("other.example.com/tools", map[string]any{"singular": "hidden"}, map[string]any{
					"purpose": vocabulary.PurposeInternal, "properties": map[string]any{"name": map[string]any{"type": "string"}},
				}),
				vocabulary.PackageManifest(tasks, 1),
				agentReachFunction(tasks, "summarize", map[string]any{
					"permissions": map[string]any{"reads": map[string]any{"kinds": []any{pattern}}},
				}),
				vocabulary.PackageManifest(chat, 1),
				agentReachAgent(chat, "assistant", map[string]any{"tools": []any{map[string]any{"function": summarize}}}),
			)
			for _, exact := range []bool{false, true} {
				scopes := []string{"call:" + summarize, "agents:" + talked, "read:*"}
				if exact {
					scopes = append(scopes, "read:"+hidden)
				}
				grant, err := scope.NewGrant(scopes)
				if err != nil {
					t.Fatal(err)
				}
				ds := newScopedDataset(env.ds, grant, substrate.ActorPerson)
				want := exact && pattern == "other.example.com/tools/*"
				_, _, err = ds.CallFunction(context.Background(), substrate.ActorPerson, summarize, nil)
				if (err == nil) != want {
					t.Fatalf("call %s exact %v: %v", pattern, exact, err)
				}
				got, err := ds.coversAgentReads(context.Background(), talked)
				if err != nil || got != want {
					t.Fatalf("chat %s exact %v: %v %v", pattern, exact, got, err)
				}
			}
		})
	}
}
