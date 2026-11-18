package engine

import (
	"context"
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
	o.noteTool("query", `{"record":null}`, true, "")
	o.noteTool("vocabulary", toolError("substrate/engine: create index for ~/trips/flight: timeout: context deadline exceeded"), false, "")
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

// A declaration whose index build was still waiting on the database when the
// architect's time ran out says so first, and that the time limit and the
// budget are not the fix: "ran out of time" alone had Moss tell the owner to
// raise a budget (#1496).
func TestASubagentWhoseIndexBuildTheDeadlineCutSaysTheDatabaseTookTheTime(t *testing.T) {
	t.Parallel()
	o := &subagentOutcome{window: 300 * time.Second}
	cut := &indexBuildCut{kind: "~/trips/flight", err: context.DeadlineExceeded}
	o.noteTool("vocabulary", toolError(cut.Error()), false, cut.kind)
	got := subagentResult(&vocabulary.Agent{Name: "architect"}, &substrate.AgentResult{
		Thread: "t1", Status: threadOverBudget, Cause: substrate.CauseDeadline, Reason: reasonDeadline,
	}, o)
	failed, _ := got["failedCall"].(map[string]any)
	if failed["tool"] != "vocabulary" || !strings.Contains(failed["error"].(string), "create index for ~/trips/flight: the index was still being built when the call ended, so nothing was applied") {
		t.Fatalf("failedCall = %v", got["failedCall"])
	}
	want := "architect's vocabulary call was still waiting for the database to build the index for ~/trips/flight when its time ran out, so nothing it was declaring was applied." +
		" It had 5 min, its own time limit." +
		" An index build waits for the database's other work to finish; architect's time limit and budget did not cause the wait, and raising them is not the fix." +
		" Its daily spend was not what stopped it."
	if s := got["stopped"].(string); s != want {
		t.Fatalf("stopped = %q\nwant      %q", s, want)
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
	o.noteTool("query", toolError("no such kind"), false, "")
	o.noteTool("query", `{"records":[]}`, true, "")
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
