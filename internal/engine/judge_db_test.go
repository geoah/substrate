package engine

// The judge (docs/agents.md, "The policy door"): the engine runs a
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
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-judged", map[string]any{"name": "wanted"})}}},
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
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-wary", map[string]any{"name": "wanted"})}}},
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
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-advised", map[string]any{"name": "wanted"})}}},
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
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-sloppy", map[string]any{"name": "wanted"})}}},
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
	// The arbiter carries write: naming it as a judge fails closed.
	putPolicy(t, ds, "armed-judge", map[string]any{
		"selector":   map[string]any{"kinds": []any{crewPackage + "/widget"}},
		"action":     "gate",
		"judge":      crewPackage + "/arbiter",
		"mode":       "enforce",
		"autoAccept": 0.1,
	})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-armed", map[string]any{"name": "wanted"})}}},
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

// systemOf reads the system prompt one completion request carried: the wire
// puts it first as a system message.
func systemOf(t *testing.T, req map[string]any) string {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("a completion request with no messages: %+v", req)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("the first message is not the system prompt: %+v", first)
	}
	content, _ := first["content"].(string)
	return content
}

// TestTheEngineTellsTheJudgeItsReplyContract: the reply shape is the ENGINE's
// (judge.go decodes it), so the engine says it — the judge agent's own prompt
// first, the contract under it — and a judge that fences the object anyway
// (issue #555: Haiku does, having been told not to) is read, not recorded as
// a parse error.
func TestTheEngineTellsTheJudgeItsReplyContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	gatePolicyWithJudge(t, ds, "contract-widgets", map[string]any{"autoAccept": 0.9})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-fenced", map[string]any{"name": "wanted"})}}},
		fakeTurn{content: "held."},
		fakeTurn{content: "through."},
	)
	fake.script("vjudge", fakeTurn{content: haikuFencedVerdict})
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedAccepted || audit["verdict"] != "accept" {
		t.Fatalf("a fenced verdict did not decide: %+v", audit)
	}
	requests := fake.requestsOf("vjudge")
	if len(requests) != 1 {
		t.Fatalf("judge completions: %d", len(requests))
	}
	system := systemOf(t, requests[0])
	if !strings.HasPrefix(system, "You are verdictor.") {
		t.Fatalf("the judge's own prompt does not come first: %q", system)
	}
	if !strings.Contains(system, judgeReplyContract) {
		t.Fatalf("the judge was never told its reply contract: %q", system)
	}
	// The contract rides the INVOCATION, not the declaration: the agent row
	// still carries only its author's prompt.
	ag, err := ds.registry().ResolveAgent(crewPackage + "/verdictor")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ag.Prompt, "JSON") {
		t.Fatalf("the engine rewrote the judge's declaration: %q", ag.Prompt)
	}
}

// TestOrdinaryRunsCarryTheAgentsPromptAlone: the suffix is the judge mode's,
// so nothing else in the loop grew a paragraph it did not ask for.
func TestOrdinaryRunsCarryTheAgentsPromptAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "hello."})
	if _, err := ds.CallAgent(ctx, crewPackage+"/chatter", "hi"); err != nil {
		t.Fatalf("call: %v", err)
	}
	requests := fake.requestsOf("chat")
	if len(requests) != 1 {
		t.Fatalf("completions: %d", len(requests))
	}
	if system := systemOf(t, requests[0]); system != "You are chatter." {
		t.Fatalf("an ordinary run's system prompt: %q", system)
	}
}

// evidenceWidget writes the record a gated diff will cite: prose the judge
// must be able to read, and a secret it must not.
func evidenceWidget(t *testing.T, ds *dataset) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-evidence",
		Properties: map[string]any{"name": "the evidence text", "token": "hunter2"},
	}); err != nil {
		t.Fatalf("put the evidence: %v", err)
	}
}

// TestJudgeReadsTheDiffsReferentsWhenThePolicyOptsIn: a tool-less judge
// cannot follow a reference, so `expandReferents` hands it what the diff
// points at (issue #556) — redacted exactly as a read redacts, so the
// referent's prose travels and its secret does not.
func TestJudgeReadsTheDiffsReferentsWhenThePolicyOptsIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	evidenceWidget(t, ds)
	gatePolicyWithJudge(t, ds, "evidenced-widgets", map[string]any{
		"autoAccept":      0.9,
		"expandReferents": true,
	})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-cited", map[string]any{
			"name": "derived", "source": crewPackage + "/widget/w-evidence",
		})}}},
		fakeTurn{content: "held."},
		fakeTurn{content: "through."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.95,"rationale":"the source says so"}`},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget from the evidence"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	if audit := judgedAnnotation(t, ds, req.ID); audit["outcome"] != judgedAccepted {
		t.Fatalf("audit: %+v", audit)
	}
	requests := fake.requestsOf("vjudge")
	if len(requests) != 1 {
		t.Fatalf("judge completions: %d", len(requests))
	}
	raw, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	sent := string(raw)
	if !strings.Contains(sent, "referents") || !strings.Contains(sent, "the evidence text") {
		t.Fatalf("the judge never saw the record the diff cites: %s", sent)
	}
	if strings.Contains(sent, "hunter2") {
		t.Fatalf("the judge was handed a secret: %s", sent)
	}
	// The marker travels JSON-escaped inside the user turn, so the word is
	// what the assertion can hold: the property is THERE, redacted.
	if !strings.Contains(sent, "redacted") {
		t.Fatalf("the referent's sensitive property is missing its redaction marker: %s", sent)
	}
}

// TestJudgeWithoutTheDialNeverSeesTheReferent: the expansion is the owner's
// opt-in, so the default envelope carries the pointer and nothing behind it.
func TestJudgeWithoutTheDialNeverSeesTheReferent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	evidenceWidget(t, ds)
	gatePolicyWithJudge(t, ds, "blind-widgets", map[string]any{"autoAccept": 0.9})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-blind", map[string]any{
			"name": "derived", "source": crewPackage + "/widget/w-evidence",
		})}}},
		fakeTurn{content: "held."},
		fakeTurn{content: "through."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.95,"rationale":"nothing to check it against"}`},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget from the evidence"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	if audit := judgedAnnotation(t, ds, req.ID); audit["outcome"] != judgedAccepted {
		t.Fatalf("audit: %+v", audit)
	}
	requests := fake.requestsOf("vjudge")
	if len(requests) != 1 {
		t.Fatalf("judge completions: %d", len(requests))
	}
	raw, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	sent := string(raw)
	// The pointer is in the diff either way; the record behind it is not.
	if !strings.Contains(sent, "w-evidence") {
		t.Fatalf("the diff's own pointer went missing: %s", sent)
	}
	if strings.Contains(sent, "referents") || strings.Contains(sent, "the evidence text") {
		t.Fatalf("the referent arrived without the dial: %s", sent)
	}
}
