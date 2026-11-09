package engine

// Approvals (decision record 0254): an agent's call of a function declaring
// `approval: required` lands an llm/approval instead of running; the owner's
// approve runs exactly that call and reports it into the conversation, a
// decline reports without running, a sub-agent's call is asked on the root
// conversation, nobody but the owner decides one, and a call a restart
// interrupted is never run again.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	apprPackage = "appr.test.dev/appr"
	apprSent    = apprPackage + "/sent"
	apprSend    = apprPackage + "/send"
)

func provisionApprovalAgents(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "apprllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-apprllm"},
	}); err != nil {
		t.Fatalf("put llm/provider row apprllm: %v", err)
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		data["provider"] = "apprllm"
		return vocabulary.AgentManifest(apprPackage, name, data)
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(apprPackage, 0),
		vocabulary.KindManifest(apprPackage, map[string]any{"singular": "sent"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		// The body stands in for a send: what it "sent" lands as a record, so
		// a test can tell whether it ran and with what.
		vocabulary.FunctionManifest(apprPackage, "send", map[string]any{
			"description": "sends a text",
			"runtime":     vocabulary.RuntimePython,
			"effect":      vocabulary.FunctionEffectIrreversible,
			"approval":    vocabulary.FunctionApprovalRequired,
			"arguments":   []any{map[string]any{"name": "text", "type": "string", "required": true}},
			"permissions": map[string]any{"writes": []any{apprSent}},
			"source": `
def main(input, host):
    text = input["args"]["text"]
    return {"effects": [{"action": "put", "kind": "` + apprSent + `", "id": host.ids.minted(text), "properties": {"text": text}}],
            "output": {"sent": text}}
`,
		}),
		// A function that reaches the send through its own body.
		vocabulary.FunctionManifest(apprPackage, "relay", map[string]any{
			"description": "relays a text through send",
			"runtime":     vocabulary.RuntimePython,
			"arguments":   []any{map[string]any{"name": "text", "type": "string", "required": true}},
			"permissions": map[string]any{"call": []any{apprSend}, "writes": []any{apprSent}},
			"source": `
def main(input, host):
    return {"output": host.functions.call("` + apprSend + `", {"text": input["args"]["text"]})}
`,
		}),
		agent("mailer", map[string]any{
			"model": "amail",
			"tools": []any{
				map[string]any{"function": apprSend},
				map[string]any{"function": apprPackage + "/relay"},
			},
			"permissions": map[string]any{"writes": []any{apprSent}},
		}),
		agent("boss", map[string]any{
			"model":       "aboss",
			"subagents":   []any{apprPackage + "/mailer"},
			"permissions": map[string]any{"writes": []any{apprSent}},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the approvals package: %v", err)
	}
	return ds, fake
}

// approvalsOf lists the approvals asked on one conversation.
func approvalsOf(t *testing.T, ds *dataset, thread string) []*erow {
	t.Helper()
	rows, err := ds.liveRowsOf(context.Background(), vocabulary.KindLLMApproval)
	if err != nil {
		t.Fatal(err)
	}
	var out []*erow
	for _, r := range rows {
		if propRefID(r.Props[msgRelThread]) == thread {
			out = append(out, r)
		}
	}
	return out
}

func approvalRow(t *testing.T, ds *dataset, id string) *erow {
	t.Helper()
	row, err := ds.loadApproval(context.Background(), eref{Kind: vocabulary.KindLLMApproval, ID: id})
	if err != nil || row == nil {
		t.Fatalf("approval %s: %v", id, err)
	}
	return row
}

// decidedRows are the conversation's approvalDecided envelopes.
func decidedRows(t *testing.T, ds *dataset, thread string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] != msgRoleSystem {
			continue
		}
		env := map[string]any{}
		if c, _ := m["content"].(string); json.Unmarshal([]byte(c), &env) == nil && env["event"] == eventApprovalDecided {
			out = append(out, env)
		}
	}
	return out
}

func decideApproval(t *testing.T, ds *dataset, actor substrate.Actor, id, state string) error {
	t.Helper()
	row := approvalRow(t, ds, id)
	version := row.Version
	_, err := ds.Patch(context.Background(), actor, vocabulary.KindLLMApproval, id, substrate.PatchInput{
		Properties: map[string]any{propApprovalState: state}, IfVersion: &version,
	})
	return err
}

// sentLive reports whether the send ran with text: its record lives under
// the id the body mints from the text.
func sentLive(t *testing.T, ds *dataset, text string) bool {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: apprSent, ID: vocabulary.DerivedMintedID(text)})
	return err == nil && row != nil && row.DeletedAt == nil
}

func TestApprovedCallRunsExactlyAndReports(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("amail",
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"hello"}`}}},
		fakeTurn{content: "It is waiting for your approval."},
		fakeTurn{content: "Sent."},
	)
	res, err := ds.CallAgent(ctx, apprPackage+"/mailer", "send hello")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != threadOK {
		t.Fatalf("result %+v", res)
	}
	if sentLive(t, ds, "hello") {
		t.Fatal("the send ran before anyone approved it")
	}
	held := approvalsOf(t, ds, res.Thread)
	if len(held) != 1 {
		t.Fatalf("approvals on the thread = %d, want 1", len(held))
	}
	a := held[0]
	if a.States[propApprovalState] != approvalPending || propRefID(a.Props["function"]) != apprSend ||
		propRefID(a.Props["agent"]) != apprPackage+"/mailer" || a.Props["tool"] != "send" ||
		a.Props["requestedIn"] != nil {
		t.Fatalf("approval %v %v", a.States, a.Props)
	}
	if args, _ := a.Props["arguments"].(map[string]any); args["text"] != "hello" {
		t.Fatalf("arguments %v", a.Props["arguments"])
	}
	row := jobToolRow(t, ds, res.Thread, "call_0")
	out := map[string]any{}
	_ = json.Unmarshal([]byte(row["content"].(string)), &out)
	if out["approval"] != a.ID || !strings.Contains(out["note"].(string), "approvalDecided") {
		t.Fatalf("the tool result %v", out)
	}

	if err := decideApproval(t, ds, substrate.ActorAPI, a.ID, approvalApproved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitFor(t, "the resume's reply", func() bool {
		_, props := threadStatusOf(t, ds, res.Thread)
		return props[propThreadLastReply] == "Sent."
	})
	if !sentLive(t, ds, "hello") {
		got := approvalRow(t, ds, a.ID)
		t.Fatalf("the approved call did not run: %v %v", got.States, got.Props)
	}
	done := approvalRow(t, ds, a.ID)
	if done.States[propApprovalState] != approvalDone || propString(done, "reportedAt") == "" ||
		done.Props["decidedAt"] == nil {
		t.Fatalf("approval after the run %v %v", done.States, done.Props)
	}
	if o, _ := done.Props["output"].(map[string]any); o["sent"] != "hello" {
		t.Fatalf("approval output %v", done.Props["output"])
	}
	envs := decidedRows(t, ds, res.Thread)
	if len(envs) != 1 || envs[0]["decision"] != approvalApproved || envs[0]["ok"] != true ||
		envs[0]["tool"] != "send" || envs[0]["callable"] != apprSend {
		t.Fatalf("approvalDecided rows %v", envs)
	}
	reqs := fake.requestsOf("amail")
	if len(reqs) != 3 || !requestCarries(reqs[2], eventApprovalDecided) {
		t.Fatalf("the resume did not carry the outcome: %d requests", len(reqs))
	}
	// Decided once: a second decision is refused by the machine.
	if err := decideApproval(t, ds, substrate.ActorAPI, a.ID, approvalDenied); err == nil {
		t.Fatal("a done approval took a second decision")
	}
}

func TestDeclinedCallNeverRunsAndReports(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("amail",
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"nope"}`}}},
		fakeTurn{content: "It is waiting for your approval."},
		fakeTurn{content: "Understood, not sent."},
	)
	res, err := ds.CallAgent(ctx, apprPackage+"/mailer", "send nope")
	if err != nil {
		t.Fatal(err)
	}
	a := approvalsOf(t, ds, res.Thread)[0]
	if err := decideApproval(t, ds, substrate.ActorAPI, a.ID, approvalDenied); err != nil {
		t.Fatalf("decline: %v", err)
	}
	waitFor(t, "the resume's reply", func() bool {
		_, props := threadStatusOf(t, ds, res.Thread)
		return props[propThreadLastReply] == "Understood, not sent."
	})
	if sentLive(t, ds, "nope") {
		t.Fatal("a declined call ran")
	}
	got := approvalRow(t, ds, a.ID)
	if got.States[propApprovalState] != approvalDenied || propString(got, "reportedAt") == "" ||
		got.Props["output"] != nil {
		t.Fatalf("approval after the decline %v %v", got.States, got.Props)
	}
	if envs := decidedRows(t, ds, res.Thread); len(envs) != 1 || envs[0]["decision"] != approvalDenied {
		t.Fatalf("approvalDecided rows %v", envs)
	}
}

// A sub-agent's call is asked on the conversation the owner is in, and the
// approved run writes under the sub-agent's own actor.
func TestSubagentApprovalIsAskedOnTheRootConversation(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("aboss",
		fakeTurn{calls: []fakeCall{{name: "mailer", args: `{"input":"send hi"}`}}},
		fakeTurn{content: "The mailer is waiting for your approval."},
		fakeTurn{content: "Sent hi."},
	)
	fake.script("amail",
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"hi"}`}}},
		fakeTurn{content: "Waiting for approval."},
	)
	res, err := ds.CallAgent(ctx, apprPackage+"/boss", "have the mailer send hi")
	if err != nil {
		t.Fatal(err)
	}
	held := approvalsOf(t, ds, res.Thread)
	if len(held) != 1 {
		t.Fatalf("approvals on the root conversation = %d, want 1", len(held))
	}
	child := propRefID(held[0].Props["requestedIn"])
	if child == "" || child == res.Thread {
		t.Fatalf("requestedIn = %q, want the mailer's own thread", child)
	}
	if err := decideApproval(t, ds, substrate.ActorAPI, held[0].ID, approvalApproved); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the root's resume", func() bool {
		_, props := threadStatusOf(t, ds, res.Thread)
		return props[propThreadLastReply] == "Sent hi."
	})
	row, err := ds.loadRowDB(ctx, eref{Kind: apprSent, ID: vocabulary.DerivedMintedID("hi")})
	if err != nil || row == nil {
		t.Fatalf("the approved call did not run: %v", err)
	}
	ag, _ := ds.registry().ResolveAgent(apprPackage + "/mailer")
	var wrote string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT actor FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`,
		apprSent, vocabulary.DerivedMintedID("hi")).Scan(&wrote); err != nil {
		t.Fatal(err)
	}
	if wrote != ag.Actor() {
		t.Fatalf("the approved call wrote as %q, want the mailer %q", wrote, ag.Actor())
	}
}

// Only the owner decides, only the loop creates, and what runs is frozen.
func TestApprovalIsTheOwnersAlone(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("amail",
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"x"}`}}},
		fakeTurn{content: "waiting"},
	)
	res, err := ds.CallAgent(ctx, apprPackage+"/mailer", "send x")
	if err != nil {
		t.Fatal(err)
	}
	a := approvalsOf(t, ds, res.Thread)[0]
	ag, _ := ds.registry().ResolveAgent(apprPackage + "/mailer")
	if err := decideApproval(t, ds, substrate.Actor(ag.Actor()), a.ID, approvalApproved); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an agent approving: %v, want forbidden", err)
	}
	if err := decideApproval(t, ds, substrate.ActorAPI, a.ID, approvalDone); !errors.Is(err, substrate.ErrForbidden) &&
		!errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("the owner marking it done: %v, want refused", err)
	}
	version := a.Version
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMApproval, a.ID, substrate.PatchInput{
		Properties: map[string]any{"arguments": map[string]any{"text": "changed"}}, IfVersion: &version,
	}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("changing the arguments: %v, want forbidden", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindLLMApproval, ID: "forged",
		Properties: map[string]any{
			msgRelThread: res.Thread, "agent": apprPackage + "/mailer", "function": apprSend, "tool": "send",
		},
	}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a hand-made approval: %v, want forbidden", err)
	}
	if got := approvalRow(t, ds, a.ID); got.States[propApprovalState] != approvalPending {
		t.Fatalf("the approval moved: %v", got.States)
	}
}

// A body is no way around the card: an agent's call of a function whose body
// calls the send is refused at the host call.
func TestHostCallOfAnApprovalFunctionIsRefusedUnderAnAgent(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("amail",
		fakeTurn{calls: []fakeCall{{name: "relay", args: `{"text":"sneaky"}`}}},
		fakeTurn{content: "done"},
	)
	res, err := ds.CallAgent(ctx, apprPackage+"/mailer", "relay sneaky")
	if err != nil {
		t.Fatal(err)
	}
	row := jobToolRow(t, ds, res.Thread, "call_0")
	if row["ok"] != false || !strings.Contains(row["content"].(string), "waits for the owner's approval") {
		t.Fatalf("the relay's tool row %v", row)
	}
	if sentLive(t, ds, "sneaky") {
		t.Fatal("the send ran through another body")
	}
}

// A call a restart cut off is never run again: the open's sweep ends it
// failed and reports it, and a decision nobody reported is reported.
func TestInterruptedApprovalSettlesFailedAndNeverRuns(t *testing.T) {
	ds, fake := provisionApprovalAgents(t)
	ctx := context.Background()
	fake.script("amail",
		fakeTurn{calls: []fakeCall{{name: "send", args: `{"text":"cut"}`}}},
		fakeTurn{content: "waiting"},
	)
	res, err := ds.CallAgent(ctx, apprPackage+"/mailer", "send cut")
	if err != nil {
		t.Fatal(err)
	}
	a := approvalsOf(t, ds, res.Thread)[0]
	// Approved and claimed as a run would, with the settle the commit would
	// schedule left out: the state a stop between the claim and the end
	// leaves behind.
	version := a.Version
	if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		_, err := tx.patch(eref{Kind: vocabulary.KindLLMApproval, ID: a.ID}, substrate.PatchInput{
			Properties: map[string]any{propApprovalState: approvalApproved}, IfVersion: &version,
		})
		tx.afterCommit = nil
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		tx.approvalRun = true
		_, err := tx.patch(eref{Kind: vocabulary.KindLLMApproval, ID: a.ID}, substrate.PatchInput{
			Properties: map[string]any{propApprovalState: approvalRunning},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := ds.settleInterruptedApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	got := approvalRow(t, ds, a.ID)
	if got.States[propApprovalState] != approvalFailed || !strings.Contains(propString(got, "error"), "interrupted") ||
		propString(got, "reportedAt") == "" {
		t.Fatalf("approval after the sweep %v %v", got.States, got.Props)
	}
	if sentLive(t, ds, "cut") {
		t.Fatal("the sweep ran an interrupted call")
	}
	if envs := decidedRows(t, ds, res.Thread); len(envs) != 1 || envs[0]["ok"] != false {
		t.Fatalf("approvalDecided rows %v", envs)
	}
	// A second sweep reports nothing twice.
	if err := ds.settleInterruptedApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if envs := decidedRows(t, ds, res.Thread); len(envs) != 1 {
		t.Fatalf("a second sweep reported again: %v", envs)
	}
}
