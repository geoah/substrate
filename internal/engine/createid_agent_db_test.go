package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// openStrictAgentDataset is openAgentDataset under the production rule on
// who names a new record (decision 0228): no fixture ids.
func openStrictAgentDataset(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ds := openInternalDataset(t)
	return ds, provisionAgents(t, ds)
}

// An agent's direct write may not name a new record; without an id the
// write lands under a minted one.
func TestAnAgentWriteCreatesUnderAMintedID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openStrictAgentDataset(t)
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-made", map[string]any{"name": "named"})}}},
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "", map[string]any{"name": "minted"})}}},
		fakeTurn{content: "done."},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/editor", "make widgets")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !toolResultsContain(t, ds, res.Thread, "is not a minted id") {
		t.Fatalf("the named create was not refused: %+v", threadMessages(t, ds, res.Thread))
	}
	if _, err := ds.Get(ctx, crewPackage+"/widget", "w-made"); err == nil {
		t.Fatal("the agent named a new widget")
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{crewPackage + "/widget"}}})
	if err != nil || len(page.Records) != 1 || !vocabulary.MintedID(page.Records[0].ID) {
		t.Fatalf("widgets = %+v, %v; want the one minted", page, err)
	}
}

// A gated create with no id is held under a target id minted at the gate,
// and the owner's accept lands it there: the accept is not judged as the
// owner naming the record. A gated create under a slug is refused at the
// gate, before the owner is asked to accept something that cannot land.
func TestAGatedCreateMintsItsTargetAndTheAcceptLandsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openStrictAgentDataset(t)
	putPolicy(t, ds, "gate-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewPackage + "/widget"}, "agents": []any{crewPackage + "/editor"}},
		"action":   "gate",
	})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-held", map[string]any{"name": "named"})}}},
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "", map[string]any{"name": "wanted"})}}},
		fakeTurn{content: "held."},
		fakeTurn{content: "landed."},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !toolResultsContain(t, ds, res.Thread, "is not a minted id") {
		t.Fatalf("the named gated create was not refused: %+v", threadMessages(t, ds, res.Thread))
	}
	req := onlyPatchRequest(t, ds)
	target, _ := req.Properties["targetId"].(string)
	if req.Properties["op"] != "create" || !vocabulary.MintedID(target) {
		t.Fatalf("request: %+v", req.Properties)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got, err := ds.Get(ctx, crewPackage+"/widget", target); err != nil || got.Properties["name"] != "wanted" {
		t.Fatalf("the accepted create did not land at %s: %+v %v", target, got, err)
	}
}

// propose create without an id names a minted one; with a slug it is refused
// at propose. The owner's accept lands the minted one.
func TestAProposedCreateTakesAMintedTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openStrictAgentDataset(t)
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"create","kind":"crew.test.dev/crew/widget","id":"w-named","diff":{"properties":{"name":"named"}}}`}}},
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"create","kind":"crew.test.dev/crew/widget","diff":{"properties":{"name":"born"}}}`}}},
		fakeTurn{content: "proposed."},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/classifier", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !toolResultsContain(t, ds, res.Thread, "is not a minted id") {
		t.Fatalf("the named proposal was not refused: %+v", threadMessages(t, ds, res.Thread))
	}
	req := onlyPatchRequest(t, ds)
	target, _ := req.Properties["targetId"].(string)
	if !vocabulary.MintedID(target) {
		t.Fatalf("request target = %q", target)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got, err := ds.Get(ctx, crewPackage+"/widget", target); err != nil || got.Properties["name"] != "born" {
		t.Fatalf("the accepted create did not land: %+v %v", got, err)
	}
	// A person writing a create request names the target under the same rule.
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest,
		Properties: map[string]any{
			"op": "create", "targetKind": crewPackage + "/widget", "targetId": "w-by-hand",
			"diff": map[string]any{"properties": map[string]any{"name": "by hand"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "is not a minted id") {
		t.Fatalf("a hand-written create request under a slug: %v", err)
	}
	// A function's request names a composed id, and the owner's accept lands
	// it there: the id was judged against the function that proposed it,
	// not against the owner who accepts.
	fn := substrate.FunctionActor("providers.example.com", "sync", "pull")
	composed, err := ds.Put(ctx, fn, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest,
		Properties: map[string]any{
			"op": "create", "targetKind": crewPackage + "/widget", "targetId": "upstream-1047392",
			"diff": map[string]any{"properties": map[string]any{"name": "synced"}},
		},
	})
	if err != nil {
		t.Fatalf("a function's create request under a composed id: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, composed.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &composed.Version,
	}); err != nil {
		t.Fatalf("accept the function's request: %v", err)
	}
	if got, err := ds.Get(ctx, crewPackage+"/widget", "upstream-1047392"); err != nil || got.Properties["name"] != "synced" {
		t.Fatalf("the accepted composed create did not land: %+v %v", got, err)
	}
}
