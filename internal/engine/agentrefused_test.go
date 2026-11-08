package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
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

// A read the engine refused for its input says the same call fails the same
// way; one it failed, or cannot answer yet, says nothing of the kind, since
// the same call may outlive it.
func TestQueryFailureMarksOnlyARefusedInputInvalid(t *testing.T) {
	refused := toolResult(t, queryFailure(&substrate.ValidationError{Problems: []string{"aggregate.groupBy[0]: groupBy kind was removed"}}))
	if refused["invalid"] != true || refused["note"] != invalidCallNote {
		t.Fatalf("a refused input: %v", refused)
	}
	for _, err := range []error{substrate.ErrObservedRederiving, errors.New("connection reset"), fmt.Errorf("%w: busy", substrate.ErrUnavailable)} {
		if got := toolResult(t, queryFailure(err)); got["invalid"] != nil || got["note"] != nil {
			t.Errorf("%v answered as an invalid call: %v", err, got)
		}
	}
}

// An exact repeat of a read this run refused for its input is answered from
// the refusal without spending the read budget, whatever order the model
// wrote the arguments in. Another call, a refusal the same call may outlive,
// and a run that may have written since all read again.
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

	const slow = `{"kind":"crew.test.dev/crew/widget","q":"bolt"}`
	timedOut := call(slow)
	l.noteRefusal(timedOut, toolError(fmt.Sprintf(toolTimeoutMessage, 30)))
	if c := call(slow); c.settled {
		t.Fatalf("a timed-out read was answered from memory: %s", c.out)
	}

	l.forgetRefusals()
	if c := call(grouped); c.settled {
		t.Fatalf("a refusal outlived a call that may have written: %s", c.out)
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
