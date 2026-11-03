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
