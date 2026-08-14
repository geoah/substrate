package engine

// Final review #2: the lifecycle fence covers AGENTS and NESTED cross-bundle
// callables, not just top-level functions. The one dataset-wide fence is held
// once at an invocation tree's root (through its last message, settlement and
// cursor write) and every nested host Call / function tool / sub-agent
// re-checks the callee's lifecycle under it. These are blocking fake-LLM/tool
// tests: an agent invocation drains before a disable returns, and a live
// bundle can neither invoke nor commit a disabled bundle's callable.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A disable of a bundled AGENT's bundle drains an in-flight agent invocation:
// the agent commits its thread and messages BEFORE disable returns, and the
// next admission refuses. The agent's model call blocks on a barrier, so the
// invocation is provably in flight (past admission, holding the fence) while
// the disable races it.
func TestFinalFenceDrainsBundledAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installGreeterBundle(t, ds, fake)
	const greeter = "abundle.bundles.substrate.reamde.dev/greeter"
	const abAuthority = "abundle.bundles.substrate.reamde.dev"

	release := make(chan struct{})
	arrived := make(chan struct{})
	fake.script("greet", fakeTurn{content: "hello", arrived: arrived, release: release})

	type callRes struct {
		thread string
		err    error
	}
	callDone := make(chan callRes, 1)
	go func() {
		res, err := ds.CallAgent(ctx, greeter, "hi")
		if err != nil {
			callDone <- callRes{"", err}
			return
		}
		callDone <- callRes{res.Thread, nil}
	}()

	// The agent is admitted and blocked inside its model turn — it holds the
	// shared fence, and its thread + user message have already landed.
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("agent never reached its model turn")
	}

	disableDone := make(chan error, 1)
	go func() { disableDone <- ds.DisableBundle(ctx, abAuthority) }()
	select {
	case err := <-disableDone:
		t.Fatalf("disable returned while an admitted agent was in flight: %v", err)
	case <-time.After(500 * time.Millisecond):
		// Blocked at the fence, as it must be.
	}

	// Release the model turn: the agent settles and commits, THEN disable lands.
	close(release)
	var res callRes
	select {
	case res = <-callDone:
		if res.err != nil {
			t.Fatalf("drained agent: %v", res.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("agent call never returned")
	}
	select {
	case err := <-disableDone:
		if err != nil {
			t.Fatalf("disable: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("disable never returned after the agent drained")
	}

	// The drained agent's thread settled (ok) before disable returned.
	thread, err := ds.Get(ctx, typeThread, res.thread)
	if err != nil {
		t.Fatalf("drained agent thread: %v", err)
	}
	if thread.Properties["status"] != threadOK {
		t.Fatalf("drained agent thread did not settle before disable returned: %+v", thread.Properties)
	}
	// And the next admission refuses.
	if _, err := ds.CallAgent(ctx, greeter, "hi again"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("post-disable agent call: %v", err)
	}
}

// A bundled agent TRIGGER delivery holds the fence through its cursor advance:
// a disable racing the delivery drains it (the agent's message lands, the
// cursor moves) before returning, and afterwards the trigger skips loudly.
func TestFinalFenceDrainsBundledAgentTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installGreeterBundle(t, ds, fake)
	const abAuthority = "abundle.bundles.substrate.reamde.dev"

	// A trigger on the bundle's config type delivering to the bundled agent.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/trigger", ID: "on-greet",
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{abAuthority + "/abconfig"}}},
			"callable": map[string]any{"kind": "core.substrate.reamde.dev/agent", "id": abAuthority + "/greeter"},
		},
	}); err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	// The change the trigger delivers on.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: abAuthority + "/abconfig", ID: "ab-cfg", Properties: map[string]any{"note": "x"},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}

	release := make(chan struct{})
	arrived := make(chan struct{})
	fake.script("greet", fakeTurn{content: "delivered", arrived: arrived, release: release})

	procDone := make(chan error, 1)
	go func() {
		_, err := ds.ProcessTriggers(ctx)
		procDone <- err
	}()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("agent delivery never reached its model turn")
	}

	disableDone := make(chan error, 1)
	go func() { disableDone <- ds.DisableBundle(ctx, abAuthority) }()
	select {
	case err := <-disableDone:
		t.Fatalf("disable returned while an admitted agent delivery was in flight: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	close(release)
	if err := <-procDone; err != nil {
		t.Fatalf("process triggers: %v", err)
	}
	select {
	case err := <-disableDone:
		if err != nil {
			t.Fatalf("disable: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("disable never returned after the delivery drained")
	}

	// A fresh pass after disable delivers nothing — the trigger loads its
	// callable unresolved and skips.
	before := threadCountOf(t, ds, abAuthority+"/greeter")
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("post-disable process: %v", err)
	}
	if after := threadCountOf(t, ds, abAuthority+"/greeter"); after != before {
		t.Fatalf("a disabled bundle's agent trigger delivered: threads %d -> %d", before, after)
	}
}

// installToolBundle stands up a bundle with one effectful function, plus a
// non-bundled agent that names the bundled function as a tool and a live
// llmprovider row for it — the cross-bundle function-tool fixture.
func installToolBundle(t *testing.T, ds *dataset, fake *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	const tbAuthority = "toolb.bundles.substrate.reamde.dev"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "userllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-userllm"},
	}); err != nil {
		t.Fatalf("put llmprovider row: %v", err)
	}
	writer := vocabulary.FunctionManifest(tbAuthority, "writer", map[string]any{
		"description": "writes one task",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
		"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-tool", "properties": {"title": "written"}}],
            "output": {"ok": True}}
`,
	})
	docs := []map[string]any{
		vocabulary.AuthorityManifest(tbAuthority, ""),
		vocabulary.BundleManifest(tbAuthority, map[string]any{
			"description": "the tool bundle",
			"installs":    []any{tbAuthority + "/tbconfig", tbAuthority + "/writer"},
		}),
		vocabulary.KindManifest(tbAuthority,
			map[string]any{"singular": "tbconfig", "plural": "tbconfigs"},
			map[string]any{"properties": map[string]any{
				"note": map[string]any{"type": "string"},
			}}),
		writer,
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install tool bundle: %v", err)
	}
	const uAuthority = "userc.test.dev"
	user := vocabulary.AgentManifest(uAuthority, "user", map[string]any{
		"description": "uses the bundled writer tool", "prompt": "You write.",
		"provider": "userllm", "model": "user",
		"tools":       []any{map[string]any{"callable": tbAuthority + "/writer"}},
		"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.AuthorityManifest(uAuthority, ""), user,
	}); err != nil {
		t.Fatalf("install user agent: %v", err)
	}
}

// A live (non-bundled) agent's function tool that targets a DISABLED bundle's
// function is refused under the root's held fence — the tool result carries
// the lifecycle refusal and no effect lands.
func TestFinalCrossBundleAgentToolRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installToolBundle(t, ds, fake)
	const tbAuthority = "toolb.bundles.substrate.reamde.dev"

	if err := ds.DisableBundle(ctx, tbAuthority); err != nil {
		t.Fatalf("disable tool bundle: %v", err)
	}
	fake.script("user",
		fakeTurn{calls: []fakeCall{{"writer", `{}`}}},
		fakeTurn{content: "gave up"},
	)
	res, err := ds.CallAgent(ctx, "userc.test.dev/user", "write it")
	if err != nil {
		t.Fatalf("call user agent: %v", err)
	}
	if res.Reply != "gave up" {
		t.Fatalf("user reply: %q", res.Reply)
	}
	refused := false
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "disabled") {
			refused = true
		}
	}
	if !refused {
		t.Fatal("the agent's transcript carries no lifecycle refusal for the cross-bundle tool")
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-tool"); err == nil {
		t.Fatal("the disabled bundle's function landed an effect through the agent tool")
	}
}

// installCallerBundle adds a live bundle whose function host-Calls the tool
// bundle's writer — the cross-bundle host Call fixture. Requires
// installToolBundle first (for writer + task type).
func installCallerBundle(t *testing.T, ds *dataset) {
	t.Helper()
	ctx := context.Background()
	const cbAuthority = "callerb.bundles.substrate.reamde.dev"
	caller := vocabulary.FunctionManifest(cbAuthority, "caller", map[string]any{
		"description": "host-calls the bundled writer",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{
			"writes": []any{"tasks.substrate.reamde.dev/task"},
			"call":   []any{"toolb.bundles.substrate.reamde.dev/writer"},
		},
		"source": `
def main(input, host):
    out = host.call("toolb.bundles.substrate.reamde.dev/writer", {})
    return {"effects": [], "output": {"called": out}}
`,
	})
	docs := []map[string]any{
		vocabulary.AuthorityManifest(cbAuthority, ""),
		vocabulary.BundleManifest(cbAuthority, map[string]any{
			"description": "the caller bundle",
			"installs":    []any{cbAuthority + "/cbconfig", cbAuthority + "/caller"},
		}),
		vocabulary.KindManifest(cbAuthority,
			map[string]any{"singular": "cbconfig", "plural": "cbconfigs"},
			map[string]any{"properties": map[string]any{
				"note": map[string]any{"type": "string"},
			}}),
		caller,
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install caller bundle: %v", err)
	}
}

// A live bundle's function host-Calling a DISABLED bundle's function is
// refused: the nested Call re-checks the callee's lifecycle under the root's
// held fence, so the callee never runs and its effects never commit.
func TestFinalCrossBundleHostCallRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installToolBundle(t, ds, fake)
	installCallerBundle(t, ds)
	const tbAuthority = "toolb.bundles.substrate.reamde.dev"
	const callerFn = "callerb.bundles.substrate.reamde.dev/caller"

	// The callee's bundle is disabled while the CALLER's bundle stays live: the
	// nested host Call must refuse under the root's held fence, so the callee
	// never runs and its effect never commits.
	if err := ds.DisableBundle(ctx, tbAuthority); err != nil {
		t.Fatalf("disable tool bundle: %v", err)
	}
	_, _, err := ds.CallFunction(ctx, callerFn, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("cross-bundle call into a disabled bundle: %v", err)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-tool"); err == nil {
		t.Fatal("the disabled callee committed an effect through a live host Call")
	}
}
