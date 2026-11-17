package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A sub-agent whose declaration's index build waited out its window was
// reported to the owner as a budget too small: its caller read only
// `status: overbudget`. The answer now says what stopped it and which call
// it was in.
func TestASubagentStoppedByItsDeadlineSaysWhichCallTookTheTime(t *testing.T) {
	t.Parallel()
	sub := &vocabulary.Agent{Name: "architect"}
	o := &subagentOutcome{window: 300 * time.Second}
	o.noteTool("query", `{"record":null}`, true)
	o.noteTool("vocabulary", toolError("substrate/engine: create index for ~/trips/flight: timeout: context deadline exceeded"), false)
	got := subagentResult(sub, &substrate.AgentResult{
		Thread: "t1", Status: threadOverBudget, Cause: substrate.CauseDeadline, Reason: reasonDeadline,
	}, o)
	if got["cause"] != substrate.CauseDeadline || got["reason"] != reasonDeadline {
		t.Fatalf("cause %v reason %v", got["cause"], got["reason"])
	}
	failed, _ := got["failedCall"].(map[string]any)
	if failed["tool"] != "vocabulary" || !strings.Contains(failed["error"].(string), "create index for ~/trips/flight") {
		t.Fatalf("failedCall = %v", got["failedCall"])
	}
	stopped, _ := got["stopped"].(string)
	for _, want := range []string{
		"architect ran out of time: it had 5 min, its own time limit.",
		"Its last call, vocabulary, failed: substrate/engine: create index",
		"so it is the call that took the time",
		"Its daily spend was not what stopped it.",
	} {
		if !strings.Contains(stopped, want) {
			t.Fatalf("stopped = %q, want it to say %q", stopped, want)
		}
	}
}

func TestASubagentCutShortByItsCallerSaysTheCallersTimeEnded(t *testing.T) {
	t.Parallel()
	o := &subagentOutcome{window: 95 * time.Second, callerBound: true}
	got := subagentResult(&vocabulary.Agent{Name: "architect"}, &substrate.AgentResult{
		Status: threadOverBudget, Cause: substrate.CauseDeadline, Reason: reasonDeadline,
	}, o)
	if s := got["stopped"].(string); !strings.Contains(s, "it had 95 s, the time left in your own run.") {
		t.Fatalf("stopped = %q", s)
	}
	if _, ok := got["failedCall"]; ok {
		t.Fatalf("failedCall on a run with no failed call: %v", got)
	}
}

func TestALaterCallThatSucceededClearsTheFailedOne(t *testing.T) {
	t.Parallel()
	o := &subagentOutcome{}
	o.noteTool("query", toolError("no such kind"), false)
	o.noteTool("query", `{"records":[]}`, true)
	got := subagentResult(&vocabulary.Agent{Name: "librarian", Budgets: vocabulary.AgentBudgets{MaxTurns: 24}}, &substrate.AgentResult{
		Status: threadOverBudget, Cause: substrate.CauseTurnLimit,
	}, o)
	if _, ok := got["failedCall"]; ok {
		t.Fatalf("failedCall after a later success: %v", got)
	}
	if s := got["stopped"].(string); s != "librarian used all 24 of its turns. Its daily spend was not what stopped it." {
		t.Fatalf("stopped = %q", s)
	}
}

func TestAFinishedSubagentAnswersAsBefore(t *testing.T) {
	t.Parallel()
	got := subagentResult(&vocabulary.Agent{Name: "architect"}, &substrate.AgentResult{Reply: "done", Thread: "t", Status: threadOK}, &subagentOutcome{})
	if len(got) != 3 || got["reply"] != "done" || got["status"] != threadOK {
		t.Fatalf("ok result = %v, want reply, thread and status alone", got)
	}
}
