package engine

// A function body runs an agent (record 0106): the grant is
// `permissions.agents`, the body reads the agent's reply, the agent's rows
// carry the delivery's cause, and a chain that loops back through the agent
// is refused as recursion.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const relayPackage = "relay.test.dev/relay"

// installRelay adds a package whose functions run the crew's agents, and a
// looping agent whose tool is the function that runs it.
func installRelay(t *testing.T, ds *dataset, fake *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "loopllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-loopllm",
			"pricing": []any{map[string]any{"model": "loop", "inputPer1M": "1", "outputPer1M": "5"}},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row: %v", err)
	}
	scribe := crewPackage + "/scribe"
	docs := []map[string]any{
		vocabulary.PackageManifest(relayPackage, 0),
		vocabulary.FunctionManifest(relayPackage, "asker", map[string]any{
			"description": "asks the scribe and returns what it said",
			"runtime":     vocabulary.RuntimePython,
			"arguments":   []any{map[string]any{"name": "q", "type": "string", "required": true}},
			"permissions": map[string]any{"agents": []any{scribe}},
			"source": `
def main(input, host):
    return {"output": host.agents.call("` + scribe + `", input["args"]["q"])}
`,
		}),
		vocabulary.FunctionManifest(relayPackage, "sneak", map[string]any{
			"description": "asks the scribe without the grant",
			"runtime":     vocabulary.RuntimePython,
			"source": `
def main(input, host):
    return {"output": host.agents.call("` + scribe + `", "hello")}
`,
		}),
		vocabulary.FunctionManifest(relayPackage, "looper", map[string]any{
			"description": "runs the loopy agent, whose tool is this function",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"agents": []any{relayPackage + "/loopy"}},
			"source": `
def main(input, host):
    return {"output": host.agents.call("` + relayPackage + `/loopy", "go")}
`,
		}),
		vocabulary.FunctionManifest(relayPackage, "hasty", map[string]any{
			"description": "runs the slow agent under a short timeout",
			"runtime":     vocabulary.RuntimePython,
			"timeout":     "PT2S",
			"permissions": map[string]any{"agents": []any{relayPackage + "/slow"}},
			"source": `
def main(input, host):
    return {"output": host.agents.call("` + relayPackage + `/slow", "take your time")}
`,
		}),
		vocabulary.FunctionManifest(relayPackage, "flaky", map[string]any{
			"description": "asks the scribe, then fails",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"agents": []any{scribe}},
			"source": `
def main(input, host):
    host.agents.call("` + scribe + `", "note this")
    raise Exception("failed after the agent ran")
`,
		}),
		vocabulary.FunctionManifest(relayPackage, "curator", map[string]any{
			"description": "runs the editor, whose write tool creates widgets",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"agents": []any{crewPackage + "/editor"}},
			"source": `
def main(input, host):
    return {"output": host.agents.call("` + crewPackage + `/editor", "make a widget")}
`,
		}),
		vocabulary.AgentManifest(relayPackage, "slow", map[string]any{
			"description": "answers after the caller's deadline",
			"prompt":      "You are slow.",
			"provider":    "loopllm", "model": "slow",
		}),
		vocabulary.AgentManifest(relayPackage, "loopy", map[string]any{
			"description": "calls back the function that ran it",
			"prompt":      "You are loopy.",
			"provider":    "loopllm", "model": "loop",
			"tools": []any{map[string]any{"function": relayPackage + "/looper"}},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install relay: %v", err)
	}
}

func TestFunctionBodyRunsAnAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)

	fake.script("sub", fakeTurn{content: "the scribe's answer"})
	out, _, err := ds.CallFunction(ctx, relayPackage+"/asker", map[string]any{"q": "what is a widget?"})
	if err != nil {
		t.Fatalf("call asker: %v", err)
	}
	got, _ := out.(map[string]any)
	if got["reply"] != "the scribe's answer" || got["status"] != threadOK {
		t.Fatalf("the body read %+v, want the scribe's reply and status ok", out)
	}
	reqs := fake.requestsOf("sub")
	if len(reqs) != 1 || !strings.Contains(toolJSON(reqs[0]["messages"]), "what is a widget?") {
		t.Fatalf("the scribe was not asked the body's question: %+v", reqs)
	}
	threads := agentThreadsOf(t, ds, "scribe")
	if len(threads) != 1 || threads[0]["__id"] != got["thread"] || threads[0]["mode"] != "call" {
		t.Fatalf("scribe threads %+v, want one call-mode thread %v", threads, got["thread"])
	}

	// Without the grant the runner refuses before the agent runs.
	if _, _, err := ds.CallFunction(ctx, relayPackage+"/sneak", nil); err == nil ||
		!strings.Contains(err.Error(), "call allowlist") {
		t.Fatalf("an ungranted agent call: %v", err)
	}
	if n := len(agentThreadsOf(t, ds, "scribe")); n != 1 {
		t.Fatalf("an ungranted call opened a thread: %d scribe threads", n)
	}
}

func TestAgentToolCallingItsRunnerIsRecursion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)

	fake.script("loop",
		fakeTurn{calls: []fakeCall{{"looper", `{}`}}},
		fakeTurn{content: "gave up"},
	)
	out, _, err := ds.CallFunction(ctx, relayPackage+"/looper", nil)
	if err != nil {
		t.Fatalf("call looper: %v", err)
	}
	thread, _ := out.(map[string]any)["thread"].(string)
	msgs := threadMessages(t, ds, thread)
	var tool map[string]any
	for _, m := range msgs {
		if m["role"] == "tool" {
			tool = m
		}
	}
	if tool == nil || tool["ok"] != false || !strings.Contains(tool["content"].(string), "recursion is refused") {
		t.Fatalf("the looping tool call was not refused as recursion: %+v", msgs)
	}
}

// The caller's timeout is the agent's deadline too, and the thread still
// settles when it passes: the loop's writes do not ride the canceled
// context of the body's runner.
func TestAgentRunSettlesAtTheCallersDeadline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)

	release := make(chan struct{})
	defer close(release)
	fake.script("slow", fakeTurn{content: "too late", release: release})
	if _, _, err := ds.CallFunction(ctx, relayPackage+"/hasty", nil); err == nil {
		t.Fatal("a body whose agent outlived its timeout succeeded")
	}
	var status string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT props->>'status' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", "agent")+` = $2`,
		typeThread, vocabulary.RecordPath(kindAgent, relayPackage+"/slow")).Scan(&status); err != nil {
		t.Fatalf("the slow agent's thread: %v", err)
	}
	if status == threadRunning {
		t.Fatal("the thread was left running past the caller's deadline")
	}
}

func TestAgentRunFromADeliveryCarriesItsCause(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", relayPackage+"/looper"),
		},
	}); err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	fake.script("loop", fakeTurn{content: "done"})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-cause", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	var widgetSeq int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT seq FROM changelog WHERE record_id = 'w-cause' ORDER BY seq LIMIT 1`).Scan(&widgetSeq); err != nil {
		t.Fatal(err)
	}
	if n := threadCountOf(t, ds, relayPackage+"/loopy"); n != 1 {
		t.Fatalf("the delivery ran loopy %d times, want once", n)
	}
	var uncaused int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog
		WHERE actor = $1 AND (caused_by IS NULL OR caused_by <> $2)`,
		string(substrate.AgentActor(vocabulary.SplitKindRef(relayPackage+"/loopy"))), widgetSeq).Scan(&uncaused); err != nil {
		t.Fatal(err)
	}
	if uncaused != 0 {
		t.Fatalf("%d of loopy's rows do not name the widget change %d as their cause", uncaused, widgetSeq)
	}
}

func TestAgentsGrantIsResolvedAtLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	for name, perms := range map[string]map[string]any{
		"unknown agent": {"agents": []any{crewPackage + "/ghost"}},
		"both lists":    {"agents": []any{crewPackage + "/scribe"}, "call": []any{crewPackage + "/scribe"}},
		"bare name":     {"agents": []any{"scribe"}},
	} {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
			vocabulary.PackageManifest("bad.test.dev/bad", 0),
			vocabulary.FunctionManifest("bad.test.dev/bad", "caller", map[string]any{
				"description": "names an agent it cannot run",
				"runtime":     vocabulary.RuntimePython,
				"permissions": perms,
				"source":      "def main(input, host):\n    return {}\n",
			}),
		})
		if err == nil || !strings.Contains(err.Error(), "permissions.agents") {
			t.Fatalf("%s: the grant was admitted or refused for another reason: %v", name, err)
		}
	}
}

// An agent's input becomes a committed message and an LLM request, so a
// body that passes an injected secret to an agent is refused before the
// agent runs.
func TestAgentCallRefusesASecretInItsInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	const (
		pkg    = "vaultrelay.test.dev/vaultrelay"
		cfg    = pkg + "/relayconfig"
		secret = "sk-relay-supersecret-35"
	)
	scribe := crewPackage + "/scribe"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "a bundle whose function hands its secret to an agent",
			"inputs":      map[string]any{"connector": map[string]any{"kind": cfg, "inject": "functions"}},
			"installs":    []any{cfg, pkg + "/leaker"},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "relayconfig"},
			map[string]any{"properties": map[string]any{"apiToken": map[string]any{"type": "secret"}}}),
		vocabulary.FunctionManifest(pkg, "leaker", map[string]any{
			"description": "passes its injected secret to the scribe",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"agents": []any{scribe}},
			"source": `
def main(input, host):
    token = input["config"]["inputs"]["connector"]["properties"]["apiToken"]
    return {"output": host.agents.call("` + scribe + `", {"token": token})}
`,
		}),
	}); err != nil {
		t.Fatalf("install the bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: cfg, Properties: map[string]any{"apiToken": secret},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	_, _, err := ds.CallFunction(ctx, pkg+"/leaker", nil)
	// The refusal reaches the body as a host-call error and returns in its
	// traceback, so it is matched by text.
	if err == nil || !strings.Contains(err.Error(), errSecretInAgentInput.Error()) {
		t.Fatalf("a secret passed to an agent: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the refusal leaked the secret: %v", err)
	}
	if n := len(agentThreadsOf(t, ds, "scribe")); n != 0 {
		t.Fatalf("the scribe ran on a secret: %d threads", n)
	}
}

// A function never sees the writes of the agents it runs: without that, a
// function watching a kind its agent writes wakes again on every such row.
func TestFunctionIsNotDeliveredItsAgentsWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", relayPackage+"/curator"),
		},
	}); err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", "w-by-editor", map[string]any{"name": "made"})}}},
		fakeTurn{content: "made it"},
	)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-seed", Properties: map[string]any{"name": "seed"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	for range 2 {
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	if _, err := ds.Get(ctx, crewPackage+"/widget", "w-by-editor"); err != nil {
		t.Fatalf("the editor's widget: %v", err)
	}
	if n := len(agentThreadsOf(t, ds, "editor")); n != 1 {
		t.Fatalf("the curator ran the editor %d times, want once", n)
	}
}

// A keyed function call whose body ran an agent and then failed keeps its
// key bound to the agent's thread: the repeat is 409 naming the thread, and
// the agent does not run twice.
func TestKeyedFunctionCallBindsToTheAgentThread(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)
	ctx := substrate.WithIdempotencyKey(context.Background(), "flaky-key-1")

	fake.script("sub", fakeTurn{content: "noted"})
	if _, _, err := ds.CallFunction(ctx, relayPackage+"/flaky", nil); err == nil {
		t.Fatal("the flaky body succeeded")
	}
	threads := agentThreadsOf(t, ds, "scribe")
	if len(threads) != 1 {
		t.Fatalf("the first attempt opened %d scribe threads, want one", len(threads))
	}
	_, _, err := ds.CallFunction(ctx, relayPackage+"/flaky", nil)
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), threads[0]["__id"].(string)) {
		t.Fatalf("the repeat under the key: %v, want a conflict naming thread %v", err, threads[0]["__id"])
	}
	if n := len(agentThreadsOf(t, ds, "scribe")); n != 1 {
		t.Fatalf("the repeat ran the scribe again: %d threads", n)
	}
}

// A delivery whose body ran an agent and then failed parks on that attempt
// instead of retrying, so the agent's committed writes are not repeated.
func TestDeliveryParksAfterItsAgentRan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)

	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", relayPackage+"/flaky"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	fake.script("sub", fakeTurn{content: "noted"})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-flaky", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := len(agentThreadsOf(t, ds, "scribe")); n != 1 {
		t.Fatalf("the delivery ran the scribe %d times, want once", n)
	}
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(failures) != 1 || failures[0].Attempts != 1 || !strings.Contains(failures[0].LastError, "parks instead of retrying") {
		t.Fatalf("parked failures %+v, want one after a single attempt", failures)
	}
}
