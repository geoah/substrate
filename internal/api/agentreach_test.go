package api

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestChatCoverageFollowsAnAgentsFunctionTool(t *testing.T) {
	e, _ := newAgentEnv(t)
	const tool = "other.example.com/tools/lookup"
	e.seed(substrate.Record{Kind: kindAgent, ID: talkedAgent, Properties: map[string]any{
		"tools": []any{map[string]any{"function": tool}},
	}}, nil)
	e.seed(substrate.Record{Kind: vocabulary.CoreKind(vocabulary.DocFunction), ID: tool, Properties: map[string]any{
		"permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}},
	}}, nil)
	for _, covered := range []bool{false, true} {
		scopes := []string{"agents:" + talkedAgent}
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
			t.Fatalf("coverage=%v, %v; want %v", got, err, covered)
		}
	}
}

func TestCallAndChatGlobsRespectInternalKinds(t *testing.T) {
	const hidden = "other.example.com/tools/hidden"
	for _, pattern := range []string{"*", "substrate.reamde.dev/llm/*", "other.example.com/tools/*"} {
		e, calls := newCallEnv(t)
		base := e.svc.datasets[fakeRepository]
		base.types = append(base.types, substrate.KindInfo{Identity: hidden, Definition: map[string]any{"purpose": vocabulary.PurposeInternal}})
		perms := map[string]any{"reads": map[string]any{"kinds": []any{pattern}}}
		e.seed(substrate.Record{Kind: kindFunction, ID: summarizeFn, Properties: map[string]any{"permissions": perms}}, nil)
		e.seed(substrate.Record{Kind: kindAgent, ID: talkedAgent, Properties: map[string]any{"tools": []any{map[string]any{"function": summarizeFn}}}}, nil)
		for _, exact := range []bool{false, true} {
			scopes := []string{"call:" + summarizeFn, "agents:" + talkedAgent, "read:*"}
			if exact {
				scopes = append(scopes, "read:"+hidden)
			}
			grant, err := scope.NewGrant(scopes)
			if err != nil {
				t.Fatal(err)
			}
			ds := newScopedDataset(&callDataset{mintingDataset: &mintingDataset{fakeDataset: base, svc: e.svc}, rec: calls}, grant, substrate.ActorAPI)
			want := exact && pattern == "other.example.com/tools/*"
			_, _, err = ds.CallFunction(context.Background(), substrate.ActorAPI, summarizeFn, nil)
			if (err == nil) != want {
				t.Fatalf("call %s exact %v: %v", pattern, exact, err)
			}
			got, err := ds.coversAgentReads(context.Background(), talkedAgent)
			if err != nil || got != want {
				t.Fatalf("chat %s exact %v: %v %v", pattern, exact, got, err)
			}
		}
	}
}
