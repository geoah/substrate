package engine

import (
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A background function is bounded by its own declared timeout (decision
// record 0200), so the job loop must not carry the agent's foreground
// toolTimeoutSeconds into toolBound.
func TestAgentJobLoopDropsForegroundToolTimeout(t *testing.T) {
	t.Parallel()
	ag := &vocabulary.Agent{Budgets: vocabulary.AgentBudgets{ToolTimeoutSeconds: 5}}
	l := &agentLoop{ag: ag, in: agentInvocation{tally: &agentTally{}}}
	jl := l.jobLoop(newTurnHub(), false)
	fn := &vocabulary.Function{Timeout: 120 * time.Second}
	if got, want := toolBound(jl.ag.Budgets.ToolTimeoutSeconds, 0, fn, time.Time{}, time.Now()), 120*time.Second+functionToolStartGrace; got != want {
		t.Fatalf("job bound for a function declaring 120s: %v, want %v", got, want)
	}
	if ag.Budgets.ToolTimeoutSeconds != 5 {
		t.Fatalf("the foreground agent's toolTimeoutSeconds changed to %d", ag.Budgets.ToolTimeoutSeconds)
	}
}
