package engine

// A sub-agent that needs answers before it builds says so and ends; its
// caller asks the owner and calls it again naming the thread it answered
// with, and the sub-agent continues that conversation with what it already
// read and decided, rather than starting over. A call may continue only a
// thread its own conversation opened for that same sub-agent.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestASubagentContinuesTheThreadItAskedIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionRollupAgents(t)
	lead := rollupPackage + "/lead"

	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "draft the plan"})}}},
		fakeTurn{content: "the worker asked which color"},
	)
	fake.script("rworker", fakeTurn{content: "Which color should it be?"})
	first, err := ds.ChatAgent(ctx, substrate.ActorPerson, lead, substrate.ChatInput{Message: "start"}, nil)
	if err != nil || first.Status != threadOK {
		t.Fatalf("first turn: %+v %v", first, err)
	}
	asked, _, _, _ := writesOf(t, toolRowNamed(t, toolRowsOf(t, ds, first.Thread), "worker"))
	childThread(t, ds, asked, "worker", first.Thread)

	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{
			"input": "Answers to the questions you asked: blue", "thread": asked,
		})}}},
		fakeTurn{content: "built"},
	)
	fake.script("rworker", fakeTurn{content: "Built it in blue."})
	second, err := ds.ChatAgent(ctx, substrate.ActorPerson, lead, substrate.ChatInput{Thread: first.Thread, Message: "blue"}, nil)
	if err != nil || second.Status != threadOK {
		t.Fatalf("second turn: %+v %v", second, err)
	}

	// The second call continued the thread it named: no new worker thread,
	// and the worker's request carried the whole conversation.
	var threads []string
	for _, row := range toolRowsOf(t, ds, first.Thread) {
		if row["name"] == "worker" {
			if row["ok"] != true {
				t.Fatalf("a worker call failed: %v", row["content"])
			}
			id, _, _, _ := writesOf(t, row)
			threads = append(threads, id)
		}
	}
	if len(threads) != 2 || threads[1] != asked {
		t.Fatalf("worker threads %v, want the second call to continue %s", threads, asked)
	}
	requests := fake.requestsOf("rworker")
	sent, err := json.Marshal(requests[len(requests)-1])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"draft the plan", "Which color should it be?", "Answers to the questions you asked: blue"} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("the continued call's request lacks %q", want)
		}
	}
}

func TestASubagentContinuesOnlyItsOwnConversationsThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionRollupAgents(t)
	lead := rollupPackage + "/lead"

	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "draft the plan"})}}},
		fakeTurn{content: "asked"},
	)
	fake.script("rworker", fakeTurn{content: "Which color?"})
	first, err := ds.CallAgent(ctx, lead, "start")
	if err != nil || first.Status != threadOK {
		t.Fatalf("first call: %+v %v", first, err)
	}
	asked, _, _, _ := writesOf(t, toolRowNamed(t, toolRowsOf(t, ds, first.Thread), "worker"))

	// Another conversation names that thread, and then a thread of another
	// agent: both are refused before the worker runs.
	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "blue", "thread": asked})}}},
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "blue", "thread": first.Thread})}}},
		fakeTurn{content: "gave up"},
	)
	other, err := ds.CallAgent(ctx, lead, "again")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	var refusals []string
	for _, row := range toolRowsOf(t, ds, other.Thread) {
		if row["name"] == "worker" {
			if row["ok"] == true {
				t.Fatalf("a worker call continued a thread it does not own: %v", row["content"])
			}
			content, _ := row["content"].(string)
			refusals = append(refusals, content)
		}
	}
	if len(refusals) != 2 ||
		!strings.Contains(refusals[0], "was not opened from this conversation") ||
		!strings.Contains(refusals[1], "continue a thread only with the sub-agent that answered it") {
		t.Fatalf("refusals %q", refusals)
	}
	if got := len(fake.requestsOf("rworker")); got != 1 {
		t.Fatalf("the worker ran %d times, want once", got)
	}
}
