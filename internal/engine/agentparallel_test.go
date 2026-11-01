package engine

import (
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A stretch of concurrent-safe calls is one span; every ordered call is a
// span of its own, so it waits for what precedes it and holds what follows.
func TestPartitionCallsSplitsAtEveryOrderedCall(t *testing.T) {
	for _, tc := range []struct {
		safe []bool
		want [][2]int
	}{
		{safe: nil, want: nil},
		{safe: []bool{true}, want: [][2]int{{0, 1}}},
		{safe: []bool{false}, want: [][2]int{{0, 1}}},
		{safe: []bool{true, true, true}, want: [][2]int{{0, 3}}},
		{safe: []bool{false, false}, want: [][2]int{{0, 1}, {1, 2}}},
		{safe: []bool{true, true, false, true}, want: [][2]int{{0, 2}, {2, 3}, {3, 4}}},
		{safe: []bool{false, true, true, false, false, true}, want: [][2]int{{0, 1}, {1, 3}, {3, 4}, {4, 5}, {5, 6}}},
	} {
		if got := partitionCalls(tc.safe); !slices.Equal(got, tc.want) {
			t.Errorf("partitionCalls(%v) = %v, want %v", tc.safe, got, tc.want)
		}
	}
}

// `query`, a function granted no effect and a sub-agent left no emit run
// together; the writing built-ins, a function that may write or call, and a
// sub-agent that may write run in order.
func TestToolConcurrentSafeHoldsWritersInOrder(t *testing.T) {
	const widget = "crew.test.dev/crew/widget"
	l := &agentLoop{
		emit: []string{widget},
		byName: map[string]agentTool{
			"query":    {builtin: vocabulary.AgentToolQuery},
			"write":    {builtin: vocabulary.AgentToolWrite},
			"propose":  {builtin: vocabulary.AgentToolPropose},
			"ask":      {builtin: vocabulary.AgentToolAsk},
			"pure":     {fn: &vocabulary.Function{}},
			"writer":   {fn: &vocabulary.Function{Caps: vocabulary.FunctionCaps{Emit: []string{widget}}}},
			"merger":   {fn: &vocabulary.Function{Caps: vocabulary.FunctionCaps{Mutations: []string{"merge"}}}},
			"caller":   {fn: &vocabulary.Function{Caps: vocabulary.FunctionCaps{Call: []string{"crew.test.dev/crew/other"}}}},
			"runner":   {fn: &vocabulary.Function{Caps: vocabulary.FunctionCaps{Agents: []string{"crew.test.dev/crew/other"}}}},
			"fetcher":  {fn: &vocabulary.Function{Caps: vocabulary.FunctionCaps{Network: []string{"example.com"}}}},
			"reader":   {sub: &vocabulary.Agent{}},
			"outsider": {sub: &vocabulary.Agent{Emit: []string{"crew.test.dev/crew/task"}}},
			"scribe":   {sub: &vocabulary.Agent{Emit: []string{widget}}},
		},
	}
	want := map[string]bool{
		"query": true, "write": false, "propose": false, "ask": false,
		"pure": true, "writer": false, "merger": false, "caller": false, "runner": false, "fetcher": true,
		// The child's emit meets this loop's: what it may write is the
		// intersection, and only an empty one lets it run beside others.
		"reader": true, "outsider": true, "scribe": false,
		"missing": true,
	}
	for name, safe := range want {
		if got := l.toolConcurrentSafe(name); got != safe {
			t.Errorf("%s: concurrent-safe %v, want %v", name, got, safe)
		}
	}
}

// Each `query` of a span takes its part of the rows left, so the span's
// reads together never pass the budget; a share given back is there for the
// next call; and the calls budget is spent in call order.
func TestPrepareCallSharesTheReadBudget(t *testing.T) {
	l := &agentLoop{
		ag:     &vocabulary.Agent{Reads: &vocabulary.FunctionReads{Kinds: []string{"crew.test.dev/crew/widget"}, Calls: 3, Rows: 100}},
		byName: map[string]agentTool{"query": {builtin: vocabulary.AgentToolQuery}},
	}
	call := func() *toolRun { return &toolRun{call: llm.ToolCall{Name: "query", Arguments: `{"q":"x"}`}} }
	a, b := call(), call()
	l.prepareCall(a, 2)
	l.prepareCall(b, 1)
	if a.settled || b.settled || a.readShare != 50 || b.readShare != 50 {
		t.Fatalf("shares: a %d (settled %v), b %d (settled %v), want 50 and 50", a.readShare, a.settled, b.readShare, b.settled)
	}
	l.settleRead(a, 10)
	c := call()
	l.prepareCall(c, 1)
	if c.readShare != 40 {
		t.Fatalf("share after a gave 40 back: %d, want 40", c.readShare)
	}
	d := call()
	l.prepareCall(d, 1)
	if !d.settled || d.ok || d.out != toolError("read budget exhausted (3 calls)") {
		t.Fatalf("a fourth call: settled %v, out %s", d.settled, d.out)
	}
	l.settleRead(b, 50)
	l.settleRead(c, 40)
	if l.readRows != 100 || l.readReserved != 0 {
		t.Fatalf("readRows %d readReserved %d, want 100 and 0", l.readRows, l.readReserved)
	}
}

// A call the loop cannot run is settled before it launches, with the error
// the model reads.
func TestPrepareCallSettlesWhatCannotRun(t *testing.T) {
	l := &agentLoop{
		ag:     &vocabulary.Agent{},
		byName: map[string]agentTool{"query": {builtin: vocabulary.AgentToolQuery}},
	}
	for _, tc := range []struct {
		call llm.ToolCall
		want string
	}{
		{call: llm.ToolCall{Name: "nope"}, want: toolError("unknown tool: nope")},
		{call: llm.ToolCall{Name: "query"}, want: toolError("query is not granted — the agent declares no reads")},
	} {
		c := &toolRun{call: tc.call}
		l.prepareCall(c, 1)
		if !c.settled || c.ok || c.out != tc.want {
			t.Errorf("%s: settled %v ok %v out %s, want %s", tc.call.Name, c.settled, c.ok, c.out, tc.want)
		}
	}
	c := &toolRun{call: llm.ToolCall{Name: "query", Arguments: "[1]"}}
	l.prepareCall(c, 1)
	if !c.settled || c.ok {
		t.Fatalf("an array of arguments: settled %v ok %v", c.settled, c.ok)
	}
}
