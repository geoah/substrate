package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A gated agent delete keeps its ifVersion through the hold: the policy door
// writes it on the request, and the accept is held to it, so a target edited
// while the request waited is not deleted under the reviewer.
func TestGatedDeleteCarriesIfVersionToTheAccept(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPolicy(t, ds, "gate-widget-deletes", map[string]any{
		"selector": map[string]any{
			"kinds":  []any{crewPackage + "/widget"},
			"agents": []any{crewPackage + "/editor"},
			"ops":    []any{policyOpDelete},
		},
		"action": "gate",
	})
	widget, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-doomed", Properties: map[string]any{"name": "keep me"},
	})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", toolArgs(t, map[string]any{
			"op": "delete", "kind": crewPackage + "/widget", "id": "w-doomed", "ifVersion": 1,
		})}}},
		fakeTurn{content: "held, waiting."},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "remove the widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got, err := ds.Get(ctx, crewPackage+"/widget", widget.ID); err != nil || got.DeletedAt != nil {
		t.Fatalf("a gated delete landed: %+v %v", got, err)
	}
	req := onlyPatchRequest(t, ds)
	if req.Properties["op"] != opDelete {
		t.Fatalf("request op = %v, want delete", req.Properties["op"])
	}
	if v, ok := asInt64(req.Properties[propIfVersion]); !ok || v != widget.Version {
		t.Fatalf("request ifVersion = %v, want the held write's %d", req.Properties[propIfVersion], widget.Version)
	}

	// The precondition is part of the reviewed envelope.
	_, err = ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{propIfVersion: 7},
	})
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("rewriting ifVersion on a proposed request: %v", err)
	}

	// The target moves while the request waits.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, crewPackage+"/widget", widget.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "edited since"},
	}); err != nil {
		t.Fatalf("edit target: %v", err)
	}
	_, err = ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	})
	if err == nil {
		t.Fatal("accepting a delete request whose target moved succeeded")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("accept: %v, want a conflict", err)
	}
	got, err := ds.Get(ctx, crewPackage+"/widget", widget.ID)
	if err != nil || got.DeletedAt != nil || got.Properties["name"] != "edited since" {
		t.Fatalf("a refused accept touched the target: %+v %v", got, err)
	}
	after, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Properties["decision"] != "proposed" {
		t.Fatalf("a refused accept moved the request: %v", after.Properties["decision"])
	}
	if _, ok := after.Annotations["substrate/conflict"]; !ok {
		t.Fatalf("the refusal is not annotated: %+v", after.Annotations)
	}
}
