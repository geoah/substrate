package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/vocabulary"
)

// toolResult decodes one tool result the way the model reads it.
func toolResult(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("tool result %q: %v", out, err)
	}
	return m
}

// A query call refused for its arguments alone is refused before anything
// stored is read, and says the same call fails the same way. A call whose
// answer depends on the repository passes the arguments' check, whatever it
// names: only the read can tell a metric or a kind that is not there yet.
func TestParseQueryCallRefusesOnlyWhatTheArgumentsSay(t *testing.T) {
	const widget = "crew.test.dev/crew/widget"
	scope := queryScope{kinds: []string{widget, "substrate.reamde.dev/observations/*"}, rows: 100, requireKinds: true}
	window := map[string]any{"gte": "2026-01-01T00:00:00Z", "lt": "2026-02-01T00:00:00Z"}
	for name, x := range map[string]struct {
		args map[string]any
		want string
	}{
		"a kind group": {map[string]any{"kind": widget, "aggregate": map[string]any{
			"property": map[string]any{"name": "name"}, "range": window, "reducers": []any{"sum"}, "groupBy": []any{"kind"},
		}}, "groupBy kind was removed"},
		"an unknown reducer": {map[string]any{"kind": widget, "aggregate": map[string]any{
			"property": map[string]any{"name": "name"}, "range": window, "reducers": []any{"bogus"},
		}}, `"bogus" is not a reducer`},
		"a malformed filter":      {map[string]any{"filter": map[string]any{"at": map[string]any{"gte": "2026-01-01"}}}, "filter takes"},
		"count beside a ranking":  {map[string]any{"kind": widget, "q": "bolt", "count": true}, "count is not supported with q"},
		"a malformed order":       {map[string]any{"kind": widget, "orderBy": 7}, "orderBy is a list of {property, desc}"},
		"a get without a kind":    {map[string]any{"id": "w-1"}, "get needs a kind"},
		"a search naming no kind": {map[string]any{"q": "bolt"}, "q needs filter.kinds"},
	} {
		_, msg := parseQueryCall(scope, x.args)
		if !strings.Contains(msg, x.want) {
			t.Errorf("%s: parseQueryCall refused %q, want %q", name, msg, x.want)
			continue
		}
		// A dataset with nothing behind it: the refusal is answered before
		// anything is read.
		out, ok, rows := (&dataset{}).runQueryTool(context.Background(), scope, x.args)
		if got := toolResult(t, out); ok || rows != 0 || got["invalid"] != true || got["note"] != invalidCallNote {
			t.Errorf("%s: %s, want an invalid refusal", name, out)
		}
	}
	for name, args := range map[string]map[string]any{
		"a metric no record holds": {"aggregate": map[string]any{
			"metric": map[string]any{"ref": "substrate.reamde.dev/observations/metric/bolts"}, "latest": true,
		}},
		"a kind nobody declared":       {"kind": "crew.test.dev/crew/gadget", "first": 1, "count": true},
		"a kind outside the allowlist": {"kind": "substrate.reamde.dev/core/token", "first": 1},
		"a record that is not there":   {"kind": widget, "id": "w-404"},
	} {
		if call, msg := parseQueryCall(scope, args); msg != "" {
			t.Errorf("%s: refused by its arguments alone: %s", name, msg)
		} else if call.agg == nil && call.id == "" && len(call.q.Filter.Kinds) != 1 {
			t.Errorf("%s: the kind shorthand was not folded into the filter: %+v", name, call.q.Filter)
		}
	}
}

// An exact repeat of a read this run refused for its arguments alone is
// answered from the refusal without spending the read budget, whatever order
// the model wrote the arguments in. Another call reads, and so does the
// repeat of a call refused for anything else: a refusal of what is stored,
// or a timeout, is never kept.
func TestPrepareCallAnswersAnExactRepeatOfARefusedRead(t *testing.T) {
	l := &agentLoop{
		ag:     &vocabulary.Agent{Reads: &vocabulary.FunctionReads{Kinds: []string{"crew.test.dev/crew/widget"}, Calls: 10, Rows: 100}},
		byName: map[string]agentTool{"query": {builtin: vocabulary.AgentToolQuery}},
	}
	// Each call gives its share of the rows back at once, as a read of
	// nothing does, so the budget stays whole for the next.
	call := func(args string) *toolRun {
		c := &toolRun{call: llm.ToolCall{Name: "query", Arguments: args}}
		l.prepareCall(c, 1)
		l.settleRead(c, 0)
		return c
	}
	const grouped = `{"kind":"crew.test.dev/crew/widget","aggregate":{"groupBy":["kind"],"reducers":["sum"]}}`
	refusal := toolInvalid("aggregate.groupBy[0]: groupBy kind was removed")

	first := call(grouped)
	if first.settled {
		t.Fatalf("the first call was answered before it ran: %s", first.out)
	}
	l.noteRefusal(first, refusal)

	again := call(`{"aggregate":{"reducers":["sum"],"groupBy":["kind"]},"kind":"crew.test.dev/crew/widget"}`)
	if !again.settled || again.ok {
		t.Fatalf("the repeat ran: settled %v ok %v", again.settled, again.ok)
	}
	got := toolResult(t, again.out)
	if got["error"] != "aggregate.groupBy[0]: groupBy kind was removed" || got["invalid"] != true || got["repeated"] != true || got["note"] != repeatedCallNote {
		t.Fatalf("the repeat's answer: %v", got)
	}
	if l.readCalls != 1 {
		t.Fatalf("the repeat spent the read budget: %d calls, want 1", l.readCalls)
	}

	if other := call(`{"kind":"crew.test.dev/crew/widget","aggregate":{"groupBy":["source"],"reducers":["sum"]}}`); other.settled {
		t.Fatalf("a changed call was answered from the refusal: %s", other.out)
	}

	for name, x := range map[string]struct{ args, out string }{
		"a metric no record holds": {
			`{"aggregate":{"metric":{"ref":"substrate.reamde.dev/observations/metric/bolts"},"latest":true}}`,
			toolError("substrate: validation failed: [aggregate.metric.ref: substrate.reamde.dev/observations/metric/bolts names no metric]"),
		},
		"a timed-out read": {`{"kind":"crew.test.dev/crew/widget","q":"bolt"}`, toolError(fmt.Sprintf(toolTimeoutMessage, 30))},
	} {
		l.noteRefusal(call(x.args), x.out)
		if c := call(x.args); c.settled {
			t.Errorf("%s was answered from memory: %s", name, c.out)
		}
	}
}

// A model filling every optional argument sends false for a flag it does not
// mean, which sets nothing.
func TestArgSetReadsFalseAsUnset(t *testing.T) {
	for _, v := range []any{nil, "", float64(0), false, []any{}, map[string]any{}} {
		if argSet(v) {
			t.Errorf("argSet(%#v) = true", v)
		}
	}
	for _, v := range []any{"x", float64(1), true, []any{1}, map[string]any{"a": 1}} {
		if !argSet(v) {
			t.Errorf("argSet(%#v) = false", v)
		}
	}
}
