package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

type reachRecords map[string]*substrate.Record

func (r reachRecords) Get(_ context.Context, kind, id string) (*substrate.Record, error) {
	if rec := r[vocabulary.RecordPath(kind, id)]; rec != nil {
		return rec, nil
	}
	return nil, substrate.ErrNotFound
}

func (r reachRecords) List(context.Context, substrate.Query) (*substrate.Page, error) {
	return &substrate.Page{}, nil
}

func TestResolvedReachFollowsExternalCallees(t *testing.T) {
	fnKind, agKind := vocabulary.CoreKind(vocabulary.DocFunction), vocabulary.CoreKind(vocabulary.DocAgent)
	const pkg = "example.com/demo"
	const external = "outside.example/data/read"
	const secret = "outside.example/data/secret"
	for _, edge := range []string{"trigger", "call", "agents", "tools", "subagents"} {
		t.Run(edge, func(t *testing.T) {
			b := &Bundle{ID: pkg}
			data := map[string]any{}
			kind := fnKind
			externalKind := fnKind
			switch edge {
			case "trigger":
				b.triggers = []TriggerFact{{ID: "scheduled", Callable: fnKind + "/" + external}}
			case "call":
				data["permissions"] = map[string]any{"call": []any{external}}
			case "agents":
				data["permissions"] = map[string]any{"agents": []any{external}}
				externalKind = agKind
			case "tools":
				kind = agKind
				data["tools"] = []any{map[string]any{"function": external}}
			case "subagents":
				kind = agKind
				externalKind = agKind
				data["subagents"] = []any{external}
			}
			if edge != "trigger" {
				b.vocabularyDocs = []map[string]any{{"kind": kind, "metadata": map[string]any{"id": pkg + "/run"}, "data": data}}
				b.readFacts()
			}
			rows := reachRecords{externalKind + "/" + external: {Kind: externalKind, ID: external, Properties: map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{secret}}}}}}
			got, err := b.ResolvedReach(context.Background(), rows, []*Bundle{b})
			if err != nil || len(got) != 1 || got[0].Pattern != secret || got[0].Write {
				t.Fatalf("reach = %+v, %v", got, err)
			}
			if _, err := b.ResolvedReach(context.Background(), reachRecords{}, []*Bundle{b}); !errors.Is(err, substrate.ErrNotFound) {
				t.Fatalf("missing callee accepted: %v", err)
			}
			// The candidate wins over an installed declaration with narrower grants.
			candidate := &Bundle{vocabularyDocs: []map[string]any{{"kind": externalKind, "metadata": map[string]any{"id": external}, "data": map[string]any{"permissions": map[string]any{"writes": []any{secret}}}}}}
			got, err = b.ResolvedReach(context.Background(), rows, []*Bundle{b, candidate})
			if err != nil || len(got) != 1 || !got[0].Write {
				t.Fatalf("candidate = %+v, %v", got, err)
			}
		})
	}
}
