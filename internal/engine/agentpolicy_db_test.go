package engine

// The policy door (docs/plans/thread-interactions.md phase 3): owner rules
// gate, refuse or allow bundle-tier writes inside the emit ceiling; a gated
// write converts into a recordpatchrequest with the thread and the governing
// policy stamped; the author's confirmation floor gates whatever policy
// says; and neither the door's rules nor a gated (or self-proposed) request
// are a bundle actor's to touch.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func putPolicy(t *testing.T, ds *dataset, id string, props map[string]any) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: id, Properties: props,
	})
	if err != nil {
		t.Fatalf("put policy %s: %v", id, err)
	}
	return e
}

func TestPolicyGatesAMutateIntoARequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	policy := putPolicy(t, ds, "gate-widgets", map[string]any{
		"selector": map[string]any{
			"kinds":  []any{crewAuthority + "/widget"},
			"agents": []any{crewAuthority + "/editor"},
		},
		"action": "gate",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-held", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held, waiting."},
		// The decision's resume.
		fakeTurn{content: "landed at last."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/editor", "make a widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// The write did NOT land; the tool result is honest about the hold.
	if _, err := ds.Get(ctx, crewAuthority+"/widget", "w-held"); err == nil {
		t.Fatal("a gated write landed")
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatal("a gated mutate reported ok")
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "held for review as") {
		t.Fatalf("the result does not name the hold: %s", content)
	}
	// The request carries the whole story: op create (the target was absent),
	// the thread, the governing policy pinned at its revision.
	req := onlyPatchRequest(t, ds)
	if req.Properties["op"] != "create" || req.Properties["targetId"] != "w-held" {
		t.Fatalf("request: %+v", req.Properties)
	}
	if got := req.Properties["thread"]; got != vocabulary.RecordPath(typeThread, res.Thread) {
		t.Fatalf("request thread = %v", got)
	}
	if got := req.Properties["policy"]; got != vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, policy.ID) {
		t.Fatalf("request policy = %v", got)
	}
	if rev, _ := anyFloat(req.Properties["policyRevision"]); int64(rev) != policy.Version {
		t.Fatalf("request policyRevision = %v, policy version %d", req.Properties["policyRevision"], policy.Version)
	}
	// The gated request's changelog entry rides the tool row.
	if entry, ok := changesName(changesOfRow(tool), vocabulary.KindRecordPatchRequest); !ok || entry["id"] != req.ID {
		t.Fatalf("the tool row carries no request change: %+v", tool["changes"])
	}
	// The owner accepts; the held write lands; the thread hears it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got, err := ds.Get(ctx, crewAuthority+"/widget", "w-held"); err != nil || got.Properties["name"] != "wanted" {
		t.Fatalf("the accepted gate did not land: %+v %v", got, err)
	}
	waitUntil(t, "the gate's resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "landed at last." {
				return true
			}
		}
		return false
	})
}

func TestPolicyRefusesAndComposesMostRestrictive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	putPolicy(t, ds, "allow-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		"action":   "allow",
	})
	putPolicy(t, ds, "refuse-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		"action":   "refuse",
	})
	// Most restrictive wins: refuse over allow.
	verdict, rule, err := ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyRefuse || rule == nil || rule.id != "refuse-widgets" {
		t.Fatalf("verdict = %s %v %v", verdict, rule, err)
	}
	// A disabled policy matches nothing; the allow remains.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchPolicy, "refuse-widgets", substrate.PatchInput{
		Properties: map[string]any{"disabled": true},
	}); err != nil {
		t.Fatal(err)
	}
	verdict, rule, err = ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow || rule == nil || rule.id != "allow-widgets" {
		t.Fatalf("verdict after disable = %s %v %v", verdict, rule, err)
	}
	// No match at all is allow with nothing to audit.
	verdict, rule, err = ds.policyVerdict(ctx, taskKind, policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow || rule != nil {
		t.Fatalf("unmatched verdict = %s %v %v", verdict, rule, err)
	}
	// The request kind is never policed: it IS the gate.
	putPolicy(t, ds, "gate-everything", map[string]any{"action": "gate"})
	verdict, _, err = ds.policyVerdict(ctx, vocabulary.KindRecordPatchRequest, policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow {
		t.Fatalf("the gate gated itself: %s %v", verdict, err)
	}
}

func TestBundleHandsStayOffThePolicyKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("med",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "core.substrate.reamde.dev/recordpatchpolicy", id: "backdoor", properties: {action: "allow"}}) { id } }`,
		})}}},
		fakeTurn{content: "tried."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/meddler", "open the door")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatalf("installed code wrote a policy: %v", tool["content"])
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "owner's hand alone") {
		t.Fatalf("the refusal does not say whose hand: %s", content)
	}
	if _, err := ds.Get(ctx, vocabulary.KindRecordPatchPolicy, "backdoor"); err == nil {
		t.Fatal("the policy landed")
	}
}

func TestConfirmationFloorGatesFunctionEffects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("burn",
		fakeTurn{calls: []fakeCall{{"burn", `{}`}}},
		fakeTurn{content: "held, as declared."},
		fakeTurn{content: "burned at last."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/burner", "burn one")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// The floor held the effect: nothing landed, the batch failed honestly,
	// and the request exists with the thread stamped and NO policy (the
	// declaration itself demanded review).
	if _, err := ds.Get(ctx, taskKind, "t-burned"); err == nil {
		t.Fatal("a floored effect landed")
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatal("a floored effect reported ok")
	}
	req := onlyPatchRequest(t, ds)
	if req.Properties["op"] != "create" || req.Properties["targetId"] != "t-burned" {
		t.Fatalf("request: %+v", req.Properties)
	}
	if req.Properties["policy"] != nil {
		t.Fatalf("a floor cited a policy: %v", req.Properties["policy"])
	}
	if got := req.Properties["thread"]; got != vocabulary.RecordPath(typeThread, res.Thread) {
		t.Fatalf("request thread = %v", got)
	}
	// Accepting materializes the held effect.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got, err := ds.Get(ctx, taskKind, "t-burned"); err != nil || got.Properties["name"] != "burned" {
		t.Fatalf("the accepted effect did not land: %+v %v", got, err)
	}
}

func TestVoluntaryProposalsStaySelfAcceptable(t *testing.T) {
	t.Parallel()
	// The documented contract the gated guard deliberately does NOT change
	// (core_db_test.go's ceiling test): a bundle actor whose emit covers the
	// target may accept its own VOLUNTARY proposal, because it could have
	// written the target directly and nothing escalates. The gate is where
	// self-decision is refused (TestGatedRequestsRefuseBundleDecisions).
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-mine", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	arrived := make(chan struct{})
	release := make(chan struct{})
	fake.script("self",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-mine","diff":{"properties":{"name":"mine"}}}`}}},
		fakeTurn{content: "waiting for the id.", arrived: arrived, release: release},
	)
	done := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, crewAuthority+"/selfjudge", "take the widget")
		done <- err
	}()
	<-arrived
	req := onlyPatchRequest(t, ds)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("call: %v", err)
	}
	fake.script("self",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, req.ID, "accepted")}}},
		fakeTurn{content: "approved my own."},
	)
	res2, err := ds.CallAgent(ctx, crewAuthority+"/selfjudge", "approve your proposal")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	tool := lastToolMessage(t, ds, res2.Thread)
	if tool["ok"] != true {
		t.Fatalf("a within-emit self-accept refused: %v", tool["content"])
	}
	if got, err := ds.Get(ctx, crewAuthority+"/widget", "w-mine"); err != nil || got.Properties["name"] != "mine" {
		t.Fatalf("the self-accepted diff did not land: %+v %v", got, err)
	}
}

func TestGatedRequestsRefuseBundleDecisions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPolicy(t, ds, "gate-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		"action":   "gate",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-gated", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held."},
	)
	if _, err := ds.CallAgent(ctx, crewAuthority+"/editor", "make a widget"); err != nil {
		t.Fatalf("editor call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	// The arbiter's emit covers widgets and requests, and it is NOT the
	// proposer — but the request is policy-gated, so installed code keeps out.
	fake.script("arbiter",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, req.ID, "accepted")}}},
		fakeTurn{content: "tried."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/arbiter", "work the inbox")
	if err != nil {
		t.Fatalf("arbiter call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatal("installed code decided a policy-gated request")
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "a policy gated this request") {
		t.Fatalf("the refusal does not name the gate: %s", content)
	}
	// The owner still can.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("owner accept: %v", err)
	}
	if _, err := ds.Get(ctx, crewAuthority+"/widget", "w-gated"); err != nil {
		t.Fatalf("the accepted gate did not land: %v", err)
	}
}
