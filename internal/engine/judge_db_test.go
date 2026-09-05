package engine

// The judge (docs/plans/thread-interactions.md phase 4): the engine runs a
// tool-less agent over gated (and judge-matched proposed) requests and
// decides ONLY between the owner's thresholds, under the policy's own actor;
// everything else — low confidence, escalate verdicts, malformed output, a
// judge with hands, advise mode — fails closed into the owner's review with
// the verdict riding the request as the engine-owned audit annotation.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// judgedAnnotation waits for and returns the request's policy/verdict audit.
func judgedAnnotation(t *testing.T, ds *dataset, requestID string) map[string]any {
	t.Helper()
	var audit map[string]any
	waitUntil(t, "the judge's audit", func() bool {
		req, err := ds.Get(context.Background(), vocabulary.KindRecordPatchRequest, requestID)
		if err != nil {
			return false
		}
		a, ok := req.Annotations[annPolicyVerdict].(map[string]any)
		if !ok {
			return false
		}
		audit = a
		return true
	})
	return audit
}

func gatePolicyWithJudge(t *testing.T, ds *dataset, id string, extra map[string]any) *substrate.Record {
	t.Helper()
	props := map[string]any{
		"selector": map[string]any{"kinds": []any{crewPackage + "/widget"}},
		"action":   "gate",
		"judge":    crewPackage + "/verdictor",
		"criteria": "small honest changes yes, deletions never",
		"mode":     "enforce",
	}
	for k, v := range extra {
		props[k] = v
	}
	return putPolicy(t, ds, id, props)
}

func TestJudgeAcceptsWithinThresholds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	policy := gatePolicyWithJudge(t, ds, "judged-widgets", map[string]any{
		"autoAccept": 0.9,
		"context":    "thread",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/crew/widget", id: "w-judged", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held, waiting on the judge."},
		fakeTurn{content: "the judge let it through."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.97,"rationale":"small and honest"}`},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedAccepted || audit["verdict"] != "accept" {
		t.Fatalf("audit: %+v", audit)
	}
	if audit["policy"] != vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, policy.ID) {
		t.Fatalf("audit policy: %v", audit["policy"])
	}
	// The held write landed, decided by the POLICY's actor.
	waitUntil(t, "the judged accept landing", func() bool {
		got, err := ds.Get(ctx, crewPackage+"/widget", "w-judged")
		return err == nil && got.Properties["name"] == "wanted"
	})
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{RecordID: req.ID, Ops: []substrate.Op{substrate.OpPatch}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	decidedByPolicy := false
	for _, c := range changes {
		if c.Actor == substrate.Actor("policy:"+policy.ID) {
			decidedByPolicy = true
		}
	}
	if !decidedByPolicy {
		t.Fatalf("no decision under the policy's actor in %+v", changes)
	}
	// The judge SAW the thread context the policy opted into: its one
	// completion request carries the editor's prose as data.
	requests := fake.requestsOf("vjudge")
	if len(requests) != 1 {
		t.Fatalf("judge completions: %d", len(requests))
	}
	if raw, _ := json.Marshal(requests[0]); !strings.Contains(string(raw), "make a widget") {
		t.Fatalf("the judge never saw the opted-in thread context")
	}
	// The proposer heard the verdict: resumed with the third scripted turn.
	waitUntil(t, "the proposer's resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "the judge let it through." {
				return true
			}
		}
		return false
	})
}

func TestJudgeEscalatesBelowThresholdAndTheOwnerDecides(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	gatePolicyWithJudge(t, ds, "wary-widgets", map[string]any{"autoAccept": 0.9, "autoRefuse": 0.9})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/crew/widget", id: "w-wary", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.55,"rationale":"probably fine"}`},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedEscalated {
		t.Fatalf("audit: %+v", audit)
	}
	fresh, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
	if err != nil || fresh.Properties["decision"] != "proposed" {
		t.Fatalf("request after escalation: %+v %v", fresh.Properties, err)
	}
	// The recommendation did not spend the owner's authority: the owner
	// decides, and the decision still works.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &fresh.Version,
	}); err != nil {
		t.Fatalf("owner accept after escalation: %v", err)
	}
}

func TestJudgeAdvisesWhenAskedTo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	gatePolicyWithJudge(t, ds, "advisory-widgets", map[string]any{
		"autoAccept": 0.5,
		"mode":       "advise",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/crew/widget", id: "w-advised", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.99,"rationale":"looks perfect"}`},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedAdvised {
		t.Fatalf("an advising judge decided: %+v", audit)
	}
	if fresh, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID); err != nil || fresh.Properties["decision"] != "proposed" {
		t.Fatalf("request after advice: %+v %v", fresh.Properties, err)
	}
	if _, err := ds.Get(ctx, crewPackage+"/widget", "w-advised"); err == nil {
		t.Fatal("an advised write landed")
	}
}

func TestJudgeFailuresFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	gatePolicyWithJudge(t, ds, "sloppy-widgets", map[string]any{"autoAccept": 0.5})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/crew/widget", id: "w-sloppy", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held."},
	)
	// The judge padded its answer with prose: strict decode refuses, the
	// request escalates, nothing lands.
	fake.script("vjudge",
		fakeTurn{content: `Sure! Here is my verdict: {"verdict":"accept","confidence":0.99}`},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedError {
		t.Fatalf("audit: %+v", audit)
	}
	if note, _ := audit["note"].(string); !strings.Contains(note, "not the verdict object") {
		t.Fatalf("the audit does not say what broke: %v", audit["note"])
	}
	if _, err := ds.Get(ctx, crewPackage+"/widget", "w-sloppy"); err == nil {
		t.Fatal("a write landed on a broken judge")
	}
}

func TestJudgesWithHandsAreRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	// The arbiter carries mutate: naming it as a judge fails closed.
	putPolicy(t, ds, "armed-judge", map[string]any{
		"selector":   map[string]any{"kinds": []any{crewPackage + "/widget"}},
		"action":     "gate",
		"judge":      crewPackage + "/arbiter",
		"mode":       "enforce",
		"autoAccept": 0.1,
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/crew/widget", id: "w-armed", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held."},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedError {
		t.Fatalf("audit: %+v", audit)
	}
	if note, _ := audit["note"].(string); !strings.Contains(note, "carries tools") {
		t.Fatalf("the audit does not name the hands: %v", audit["note"])
	}
}

func TestVoluntaryProposalsAreJudgedWhenThePolicyMatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-polite", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	gatePolicyWithJudge(t, ds, "judge-widgets", map[string]any{"autoAccept": 0.9})
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/crew/widget","target":"w-polite","diff":{"properties":{"name":"better"}}}`}}},
		fakeTurn{content: "proposed."},
		fakeTurn{content: "the judge accepted my proposal."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.95,"rationale":"a small honest change"}`},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/classifier", "tidy the widget politely")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedAccepted {
		t.Fatalf("audit: %+v", audit)
	}
	waitUntil(t, "the judged proposal landing", func() bool {
		got, err := ds.Get(ctx, crewPackage+"/widget", "w-polite")
		return err == nil && got.Properties["name"] == "better"
	})
	waitUntil(t, "the proposer's resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "the judge accepted my proposal." {
				return true
			}
		}
		return false
	})
}
