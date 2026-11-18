package engine

import (
	"errors"
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
	o.noteTool("query", `{"record":null}`, true, nil)
	o.noteTool("vocabulary", toolError("substrate/engine: create index for ~/trips/flight: timeout: context deadline exceeded"), false, nil)
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

// A declaration whose index work the architect's time cut says so first,
// naming the phase the end met: "ran out of time" alone had Moss tell the
// owner to raise a budget (#1496). It says nothing of why the work took the
// time, which is not known, and the model reads no driver error.
func TestASubagentWhoseIndexWorkTheDeadlineCutSaysWhereItStood(t *testing.T) {
	t.Parallel()
	driver := errors.New("read tcp 192.0.2.1:5432: i/o timeout: context deadline exceeded")
	for _, tc := range []struct {
		phase       indexCutPhase
		error, said string
	}{
		{
			indexCutPreparing,
			"substrate/engine: create index for ~/trips/flight: the call ended before the index was ready, so the declaration was not applied",
			"architect's vocabulary call was still preparing the index for ~/trips/flight when its time ran out, so nothing it was declaring was applied.",
		},
		{
			indexCutQueued,
			"substrate/engine: create index for ~/trips/flight: the call ended while it was waiting for another index build on the database to finish, so the declaration was not applied",
			"architect's vocabulary call was still waiting for another index build on the database to finish before it could build the index for ~/trips/flight when its time ran out, so nothing it was declaring was applied.",
		},
	} {
		cut := &indexBuildCut{kind: "~/trips/flight", phase: tc.phase, err: driver}
		if !errors.Is(cut, driver) {
			t.Fatalf("phase %d: the driver's error is not reachable through the cut", tc.phase)
		}
		o := &subagentOutcome{window: 300 * time.Second}
		o.noteTool("vocabulary", toolError(cut.Error()), false, cut)
		got := subagentResult(&vocabulary.Agent{Name: "architect"}, &substrate.AgentResult{
			Thread: "t1", Status: threadOverBudget, Cause: substrate.CauseDeadline, Reason: reasonDeadline,
		}, o)
		failed, _ := got["failedCall"].(map[string]any)
		if failed["tool"] != "vocabulary" || failed["error"] != tc.error {
			t.Fatalf("phase %d: failedCall = %v\nwant error %q", tc.phase, got["failedCall"], tc.error)
		}
		want := tc.said +
			" It had 5 min, its own time limit." +
			" The time went to the database's index work, not to architect's turns or tool calls." +
			" Its daily spend was not what stopped it."
		stopped := got["stopped"].(string)
		if stopped != want {
			t.Fatalf("phase %d: stopped = %q\nwant      %q", tc.phase, stopped, want)
		}
		for _, leak := range []string{"192.0.2.1", "i/o timeout", "deadline exceeded"} {
			if strings.Contains(stopped, leak) || strings.Contains(tc.error, leak) {
				t.Fatalf("phase %d: the model reads the driver's error (%q)", tc.phase, leak)
			}
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
	o.noteTool("query", toolError("no such kind"), false, nil)
	o.noteTool("query", `{"records":[]}`, true, nil)
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
