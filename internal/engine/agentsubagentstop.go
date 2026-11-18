package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// subagentOutcome is what a sub-agent's run leaves for the call that ran it
// beside its result: the window its deadline gave it, and the tool call its
// run last failed on. A caller that reads only `status: overbudget` cannot
// tell a run that spent its turns from one whose one write waited out the
// clock, and told the owner the budget was too small when a declaration's
// index build had waited on another session for the whole window. The child
// loop writes it from its own goroutine and the caller reads it after
// runAgent returns, so nothing reads it while it is written.
type subagentOutcome struct {
	// window is the run's deadline less its start; callerBound is a window
	// the caller's own deadline cut shorter than the agent's budget.
	window      time.Duration
	callerBound bool
	// failedTool and failedError are the run's newest tool outcome where it
	// failed; a later call that succeeded clears them. failedIndex is the
	// kind whose index build that call was still waiting on when its time
	// ran out.
	failedTool, failedError, failedIndex string
}

// noteTool records one settled call of the run; index is the kind whose
// index build the call's time cut short, where it was one.
func (o *subagentOutcome) noteTool(name, out string, ok bool, index string) {
	if o == nil {
		return
	}
	if ok {
		o.failedTool, o.failedError, o.failedIndex = "", "", ""
		return
	}
	o.failedTool, o.failedError, o.failedIndex = name, toolErrorText(out), index
}

// toolErrorText is the error a toolError result carries, else the result.
func toolErrorText(out string) string {
	var v struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(out), &v) == nil && v.Error != "" {
		return v.Error
	}
	return out
}

// subagentResult is a sub-agent call's answer to the model that made it.
// A run that did not finish says why in `stopped`, a sentence the calling
// model can repeat to the owner as it stands, and names the call it failed
// on under `failedCall`, so a stop is never read as a budget the owner
// should raise when nothing about the budget ended it.
func subagentResult(sub *vocabulary.Agent, res *substrate.AgentResult, o *subagentOutcome) map[string]any {
	out := map[string]any{"reply": res.Reply, "thread": res.Thread, "status": res.Status}
	if res.Status == threadOK {
		return out
	}
	if res.Cause != "" {
		out["cause"] = res.Cause
	}
	if res.Reason != "" {
		out["reason"] = res.Reason
	}
	if o != nil && o.failedTool != "" {
		out["failedCall"] = map[string]any{"tool": o.failedTool, "error": o.failedError}
	}
	if s := subagentStopped(sub, res, o); s != "" {
		out["stopped"] = s
	}
	return out
}

// subagentStopped says why a sub-agent's run ended before it finished.
func subagentStopped(sub *vocabulary.Agent, res *substrate.AgentResult, o *subagentOutcome) string {
	name := sub.Name
	var b strings.Builder
	switch res.Cause {
	case substrate.CauseDeadline:
		had := ""
		if o != nil && o.window > 0 {
			whose := "its own time limit"
			if o.callerBound {
				whose = "the time left in your own run"
			}
			had = fmt.Sprintf("had %s, %s", wordsDuration(o.window), whose)
		}
		switch {
		case o != nil && o.failedIndex != "":
			// Said first and plainly: "ran out of time" was read as a
			// budget to raise, when the time went to the database.
			fmt.Fprintf(&b, "%s's %s call was still waiting for the database to build the index for %s when its time ran out, so nothing it was declaring was applied.",
				name, o.failedTool, o.failedIndex)
			if had != "" {
				fmt.Fprintf(&b, " It %s.", had)
			}
			fmt.Fprintf(&b, " An index build waits for the database's other work to finish; %s's time limit and budget did not cause the wait, and raising them is not the fix.", name)
		case had != "":
			fmt.Fprintf(&b, "%s ran out of time: it %s.", name, had)
		default:
			fmt.Fprintf(&b, "%s ran out of time.", name)
		}
	case substrate.CauseTurnLimit:
		fmt.Fprintf(&b, "%s used all %d of its turns.", name, sub.Budgets.MaxTurns)
	case substrate.CauseToolCallLimit:
		fmt.Fprintf(&b, "%s used all %d of its tool calls.", name, sub.Budgets.MaxToolCalls)
	default:
		switch res.Status {
		case threadStopped:
			fmt.Fprintf(&b, "%s was stopped before it finished.", name)
		case threadError:
			fmt.Fprintf(&b, "%s failed.", name)
		default:
			fmt.Fprintf(&b, "%s stopped before it finished.", name)
		}
		if res.Reason != "" {
			fmt.Fprintf(&b, " The run's reason: %s.", strings.TrimSuffix(res.Reason, "."))
		}
	}
	if o != nil && o.failedTool != "" && o.failedIndex == "" {
		fmt.Fprintf(&b, " Its last call, %s, failed: %s.", o.failedTool, strings.TrimSuffix(o.failedError, "."))
		if res.Cause == substrate.CauseDeadline && strings.Contains(o.failedError, "deadline exceeded") {
			b.WriteString(" That call was still running when the time ran out, so it is the call that took the time, and nothing it was doing was applied.")
		}
	}
	if res.Cause == substrate.CauseDeadline || res.Cause == substrate.CauseTurnLimit || res.Cause == substrate.CauseToolCallLimit {
		b.WriteString(" Its daily spend was not what stopped it.")
	}
	return b.String()
}

// wordsDuration is a window as a person says it: "5 min", "95 s", "2 min 30 s".
func wordsDuration(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s < 120:
		return fmt.Sprintf("%d s", s)
	case s%60 == 0:
		return fmt.Sprintf("%d min", s/60)
	default:
		return fmt.Sprintf("%d min %d s", s/60, s%60)
	}
}
