package engine

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const fanPackage = "fan.test.dev/fan"

// installFanAgents installs fanPackage: `fanout` (query, write and two
// sub-agents, the default maxParallelTools) and `serial` (the same, at 1).
// The sub-agents `scout` and `spotter` declare no writes, so they run beside
// other calls; each names its own model so the fake server holds each at
// its own barrier.
func installFanAgents(t *testing.T, ds *dataset) {
	t.Helper()
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(fanPackage, name, data)
	}
	caller := func(name, model string, budgets map[string]any) map[string]any {
		return agent(name, map[string]any{
			"provider": "rootllm", "model": model,
			"tools": []any{
				map[string]any{"function": vocabulary.HostFunctionQuery},
				map[string]any{"function": vocabulary.HostFunctionWrite},
			},
			"subagents": []any{fanPackage + "/scout", fanPackage + "/spotter"},
			"permissions": map[string]any{
				"reads":  map[string]any{"kinds": []any{fanPackage + "/note"}},
				"writes": []any{fanPackage + "/note"},
			},
			"budgets": budgets,
		})
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(fanPackage, 0),
		vocabulary.KindManifest(fanPackage, map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		caller("fanout", "fanout", map[string]any{}),
		caller("serial", "serial", map[string]any{"maxParallelTools": 1}),
		agent("scout", map[string]any{"provider": "subllm", "model": "scout"}),
		agent("spotter", map[string]any{"provider": "subllm", "model": "spotter"}),
	}); err != nil {
		t.Fatalf("install %s: %v", fanPackage, err)
	}
}

// arrives fails the test when ch is not closed within a generous bound.
func arrives(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// stillOpen fails the test when ch closes within a short window: a call
// that must not have started yet.
func stillOpen(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s started early", what)
	case <-time.After(300 * time.Millisecond):
	}
}

// toolEvents is the tool events' kinds and call ids, in emit order.
func toolEvents(events []substrate.AgentEvent) [][2]string {
	var out [][2]string
	for _, ev := range events {
		if ev.Kind == substrate.AgentEventToolStarted || ev.Kind == substrate.AgentEventToolFinished {
			out = append(out, [2]string{ev.Kind, ev.ID})
		}
	}
	return out
}

type agentOutcome struct {
	res *substrate.AgentResult
	err error
}

// TestAgentRunsIndependentToolCallsConcurrently holds two sub-agent calls
// that write nothing on the fake model server's barriers: both reach the
// model before either is released, so they are in flight at once. The one
// asked second finishes first, and the tool rows and their events still
// read in the order the model asked.
func TestAgentRunsIndependentToolCallsConcurrently(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installFanAgents(t, ds)

	scoutIn, scoutGo := make(chan struct{}), make(chan struct{})
	spotIn, spotGo := make(chan struct{}), make(chan struct{})
	fake.script("fanout",
		fakeTurn{calls: []fakeCall{
			{"scout", `{"input":"look left"}`},
			{"spotter", `{"input":"look right"}`},
			{"query", `{"op":"list","kind":"` + fanPackage + `/note"}`},
		}},
		fakeTurn{content: "fanned out"},
	)
	fake.script("scout", fakeTurn{content: "left is clear", arrived: scoutIn, release: scoutGo})
	fake.script("spotter", fakeTurn{content: "right is clear", arrived: spotIn, release: spotGo})

	events := newEventLog()
	out := make(chan agentOutcome, 1)
	go func() {
		res, err := ds.ChatAgent(ctx, substrate.ActorPerson, fanPackage+"/fanout", substrate.ChatInput{Message: "look around"}, events.emit)
		out <- agentOutcome{res, err}
	}()
	arrives(t, scoutIn, "scout at the model")
	arrives(t, spotIn, "spotter at the model while scout is held")

	close(spotGo)
	waitFor(t, "spotter to settle while scout is held", func() bool {
		th := agentThreadsIn(t, ds, fanPackage, "spotter")
		return len(th) == 1 && th[0]["status"] == threadOK
	})
	close(scoutGo)
	got := <-out
	if got.err != nil || got.res.Reply != "fanned out" || got.res.ToolCalls != 3 {
		t.Fatalf("fanout: %+v, %v", got.res, got.err)
	}

	var names []string
	turns := map[string]int{}
	for _, m := range threadMessages(t, ds, got.res.Thread) {
		if m["role"] == "tool" {
			name, _ := m["name"].(string)
			names = append(names, name)
			turns[name] = intProp(m, "turn")
		}
	}
	if len(names) != 3 || !(turns["scout"] < turns["spotter"] && turns["spotter"] < turns["query"]) {
		t.Fatalf("tool rows %v at turns %v, want scout < spotter < query", names, turns)
	}
	want := [][2]string{
		{substrate.AgentEventToolStarted, "call_0"},
		{substrate.AgentEventToolStarted, "call_1"},
		{substrate.AgentEventToolStarted, "call_2"},
		{substrate.AgentEventToolFinished, "call_0"},
		{substrate.AgentEventToolFinished, "call_1"},
		{substrate.AgentEventToolFinished, "call_2"},
	}
	if ev := toolEvents(events.snapshot()); len(ev) != len(want) || !slices.Equal(ev, want) {
		t.Fatalf("tool events %v, want %v", ev, want)
	}
	// The results go back to the model in the order it asked.
	msgs, _ := fake.requestsOf("fanout")[1]["messages"].([]any)
	var ids []string
	for _, raw := range msgs {
		if m, _ := raw.(map[string]any); m["role"] == "tool" {
			id, _ := m["tool_call_id"].(string)
			ids = append(ids, id)
		}
	}
	if !slices.Equal(ids, []string{"call_0", "call_1", "call_2"}) {
		t.Fatalf("tool results replayed as %v", ids)
	}
}

// TestAgentOrderedCallWaitsForTheCallsBeforeIt puts a write between two
// calls that could run together: the write waits for the first to settle,
// and the second does not start until the write landed.
func TestAgentOrderedCallWaitsForTheCallsBeforeIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installFanAgents(t, ds)

	scoutIn, scoutGo := make(chan struct{}), make(chan struct{})
	spotIn, spotGo := make(chan struct{}), make(chan struct{})
	fake.script("fanout",
		fakeTurn{calls: []fakeCall{
			{"scout", `{"input":"look left"}`},
			{"write", toolArgs(t, map[string]any{
				"op": "put", "kind": fanPackage + "/note", "id": fid("n-between"),
				"input": map[string]any{"properties": map[string]any{"name": "between"}},
			})},
			{"spotter", `{"input":"look right"}`},
		}},
		fakeTurn{content: "in order"},
	)
	fake.script("scout", fakeTurn{content: "left", arrived: scoutIn, release: scoutGo})
	fake.script("spotter", fakeTurn{content: "right", arrived: spotIn, release: spotGo})

	out := make(chan agentOutcome, 1)
	go func() {
		res, err := ds.CallAgent(ctx, fanPackage+"/fanout", "go")
		out <- agentOutcome{res, err}
	}()
	arrives(t, scoutIn, "scout at the model")
	stillOpen(t, spotIn, "spotter, behind the write")
	if _, err := ds.Get(ctx, fanPackage+"/note", fid("n-between")); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the write ran before the call ahead of it settled: %v", err)
	}
	close(scoutGo)
	arrives(t, spotIn, "spotter after the write")
	if _, err := ds.Get(ctx, fanPackage+"/note", fid("n-between")); err != nil {
		t.Fatalf("spotter started before the write ahead of it landed: %v", err)
	}
	close(spotGo)
	got := <-out
	if got.err != nil || got.res.Reply != "in order" || got.res.Effects != 1 {
		t.Fatalf("fanout: %+v, %v", got.res, got.err)
	}
}

// TestAgentMaxParallelToolsOneRunsCallsInTurn: at 1, the second of two
// calls that could run together waits for the first.
func TestAgentMaxParallelToolsOneRunsCallsInTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installFanAgents(t, ds)

	scoutIn, scoutGo := make(chan struct{}), make(chan struct{})
	spotIn, spotGo := make(chan struct{}), make(chan struct{})
	fake.script("serial",
		fakeTurn{calls: []fakeCall{
			{"scout", `{"input":"look left"}`},
			{"spotter", `{"input":"look right"}`},
		}},
		fakeTurn{content: "one at a time"},
	)
	fake.script("scout", fakeTurn{content: "left", arrived: scoutIn, release: scoutGo})
	fake.script("spotter", fakeTurn{content: "right", arrived: spotIn, release: spotGo})

	out := make(chan agentOutcome, 1)
	go func() {
		res, err := ds.CallAgent(ctx, fanPackage+"/serial", "go")
		out <- agentOutcome{res, err}
	}()
	arrives(t, scoutIn, "scout at the model")
	stillOpen(t, spotIn, "spotter, with scout held")
	close(scoutGo)
	arrives(t, spotIn, "spotter after scout")
	close(spotGo)
	got := <-out
	if got.err != nil || got.res.Reply != "one at a time" {
		t.Fatalf("serial: %+v, %v", got.res, got.err)
	}
}

// TestAgentToolEventsAndRowsSayWhenEachCallRan holds two sub-agent calls of
// one stretch and lets the second finish well before the first: its
// toolFinished goes out after the first's and both rows are written once
// the stretch settled, yet the event and the row's `ran` each say the
// call's own end, so a client times each call right, live or read back.
func TestAgentToolEventsAndRowsSayWhenEachCallRan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installFanAgents(t, ds)

	scoutIn, scoutGo := make(chan struct{}), make(chan struct{})
	spotIn, spotGo := make(chan struct{}), make(chan struct{})
	fake.script("fanout",
		fakeTurn{calls: []fakeCall{
			{"scout", `{"input":"look left"}`},
			{"spotter", `{"input":"look right"}`},
		}},
		fakeTurn{content: "fanned out"},
	)
	fake.script("scout", fakeTurn{content: "left is clear", arrived: scoutIn, release: scoutGo})
	fake.script("spotter", fakeTurn{content: "right is clear", arrived: spotIn, release: spotGo})

	events := newEventLog()
	out := make(chan agentOutcome, 1)
	go func() {
		res, err := ds.ChatAgent(ctx, substrate.ActorPerson, fanPackage+"/fanout", substrate.ChatInput{Message: "look around"}, events.emit)
		out <- agentOutcome{res, err}
	}()
	arrives(t, scoutIn, "scout at the model")
	arrives(t, spotIn, "spotter at the model")
	close(spotGo)
	waitFor(t, "spotter to settle while scout is held", func() bool {
		th := agentThreadsIn(t, ds, fanPackage, "spotter")
		return len(th) == 1 && th[0]["status"] == threadOK
	})
	const gap = 300 * time.Millisecond
	time.Sleep(gap)
	close(scoutGo)
	got := <-out
	if got.err != nil || got.res.Reply != "fanned out" {
		t.Fatalf("fanout: %+v, %v", got.res, got.err)
	}

	started := map[string]time.Time{}
	finished := map[string][2]time.Time{}
	for _, ev := range events.snapshot() {
		switch ev.Kind {
		case substrate.AgentEventToolStarted:
			if ev.StartedAt == nil || ev.FinishedAt != nil {
				t.Fatalf("toolStarted %s: startedAt %v, finishedAt %v", ev.ID, ev.StartedAt, ev.FinishedAt)
			}
			started[ev.ID] = *ev.StartedAt
		case substrate.AgentEventToolFinished:
			if ev.StartedAt == nil || ev.FinishedAt == nil {
				t.Fatalf("toolFinished %s carries no times", ev.ID)
			}
			finished[ev.ID] = [2]time.Time{*ev.StartedAt, *ev.FinishedAt}
		}
	}
	for _, id := range []string{"call_0", "call_1"} {
		s, f := finished[id][0], finished[id][1]
		if s.Before(started[id]) || f.Before(s) {
			t.Fatalf("%s: announced %v, ran %v to %v", id, started[id], s, f)
		}
	}
	if scout, spotter := finished["call_0"][1], finished["call_1"][1]; scout.Sub(spotter) < gap {
		t.Fatalf("scout finished %v, spotter %v: want spotter at least %v sooner", scout, spotter, gap)
	}

	rows := map[string]map[string]any{}
	for _, m := range threadMessages(t, ds, got.res.Thread) {
		if m["role"] == "tool" {
			id, _ := m["toolCallId"].(string)
			rows[id] = m
		}
	}
	for id, want := range finished {
		ran, _ := rows[id]["ran"].(map[string]any)
		s, _ := ran["startedAt"].(string)
		f, _ := ran["finishedAt"].(string)
		gotS, errS := time.Parse(time.RFC3339Nano, s)
		gotF, errF := time.Parse(time.RFC3339Nano, f)
		if errS != nil || errF != nil || !gotS.Equal(want[0]) || !gotF.Equal(want[1]) {
			t.Fatalf("%s row ran %v, want %v to %v", id, ran, want[0], want[1])
		}
	}
}
