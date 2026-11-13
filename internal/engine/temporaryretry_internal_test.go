package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Which failures wait for the walk (decision 0266): a temporary failure of a
// function that can run no agent, while waits remain. A function granted an
// agent or a call, an agent trigger, and a body that ran an agent before it
// failed are set aside, the last even though agentRetryGate keeps the
// failure temporary under the error it wraps it in.
func TestOnlyAFunctionThatRunsNoAgentWaitsForTheWalk(t *testing.T) {
	ds := &dataset{svc: &service{retryNow: time.Now}}
	fn := func(caps vocabulary.FunctionCaps) *trigger {
		return &trigger{ID: "t", Callable: &vocabulary.Function{Caps: caps}}
	}
	temporary := fmt.Errorf("run: %w", runner.ErrTemporary)
	ranAnAgent := agentRetryGate(&agentThreads{first: "thread-1"}, temporary)
	if !runner.Temporary(ranAnAgent) {
		t.Fatal("the gate no longer keeps the failure temporary; this case tests nothing")
	}
	for _, c := range []struct {
		name     string
		tr       *trigger
		cause    error
		attempts int
		waits    bool
	}{
		{"a function's temporary failure", fn(vocabulary.FunctionCaps{}), temporary, 1, true},
		{"its last wait", fn(vocabulary.FunctionCaps{}), temporary, len(temporaryRetryGaps), true},
		{"past its waits", fn(vocabulary.FunctionCaps{}), temporary, len(temporaryRetryGaps) + 1, false},
		{"a failure of its own", fn(vocabulary.FunctionCaps{}), fmt.Errorf("run: the body is broken"), 1, false},
		{"a function granted an agent", fn(vocabulary.FunctionCaps{Agents: []string{"a.example.com/p/agent"}}), temporary, 1, false},
		{"a function granted a call", fn(vocabulary.FunctionCaps{Call: []string{"a.example.com/p/helper"}}), temporary, 1, false},
		{"an agent trigger", &trigger{ID: "t", Agent: &vocabulary.Agent{}}, temporary, 1, false},
		{"a body that ran an agent", fn(vocabulary.FunctionCaps{}), ranAnAgent, 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, _, waits := ds.parkedError(c.tr, c.cause, c.attempts, time.Now())
			if waits != c.waits || strings.HasPrefix(text, temporaryRetryMark) != c.waits {
				t.Fatalf("waits = %v, text %q; want waits %v", waits, text, c.waits)
			}
		})
	}
}
