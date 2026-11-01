package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// toolRun is one tool call of a response, from its reserved ordinal to its
// settled result. Everything a dispatch reports lands here and not on the
// loop, so the calls of one concurrent run never share a field.
type toolRun struct {
	call llm.ToolCall
	// turn is the tool row's ordinal, reserved in call order.
	turn int
	// seq is the call's place in the tool-call budget: the ordinal its
	// idempotency key and a sub-agent's delivery derive from.
	seq int
	// refused is a call the tool-call budget turned away before it started.
	refused bool

	tool agentTool
	args map[string]any
	// settled is a call whose result was decided before its body ran: an
	// unknown tool, arguments that are not an object, a spent read budget.
	settled bool
	// readShare is the rows a `query` call holds of the read budget while it
	// runs.
	readShare int

	out string
	ok  bool
	// changes collects the changelog entries the dispatch committed (a
	// mutate's writes, a propose's request row, a function tool's applied
	// effects), stamped onto the tool row as `changes`. A sub-agent call
	// collects nothing here: the child's own rows carry the child's writes.
	changes []changeEntry
	// writes is a sub-agent call's summary of what its child chain wrote,
	// stamped onto the tool row as `subagentWrites`; nil on every other call
	// and on a sub-agent call whose child thread never opened.
	writes map[string]any
	// job is a background call the dispatch accepted and did not start: the
	// loop stamps the tool row `working` and starts it once the row landed
	// (agentjobs.go). nil on every other call.
	job *pendingJob
	// panicked carries a panic out of the call's goroutine, so it surfaces
	// on the loop's own.
	panicked any
}

// partitionCalls splits one response's calls, in order, into the spans the
// loop dispatches one after another: a maximal stretch of concurrent-safe
// calls is one span, and every other call is a span of its own, so an
// ordered call waits for everything before it and everything after waits
// for it.
func partitionCalls(safe []bool) [][2]int {
	var spans [][2]int
	for i := 0; i < len(safe); {
		j := i + 1
		if safe[i] {
			for j < len(safe) && safe[j] {
				j++
			}
		}
		spans = append(spans, [2]int{i, j})
		i = j
	}
	return spans
}

// concurrentSafe answers, per call, whether it may run beside its
// neighbors: whether nothing it does can change what a sibling reads.
func (l *agentLoop) concurrentSafe(calls []llm.ToolCall) []bool {
	safe := make([]bool, len(calls))
	for i, tc := range calls {
		safe[i] = l.toolConcurrentSafe(tc.Name)
	}
	return safe
}

// toolConcurrentSafe is `query`; a function whose permissions grant no
// effect, no mutation and no call that could carry one, and whose declared
// effect is read (or undeclared, without egress); and a sub-agent
// whose effective emit is empty, which no tool below it can widen. `write`,
// `propose` and `ask` write, and run in order. A name the agent does not
// carry answers an error and touches nothing.
func (l *agentLoop) toolConcurrentSafe(name string) bool {
	tool, ok := l.byName[name]
	if !ok {
		return true
	}
	switch {
	case tool.builtin == vocabulary.AgentToolQuery:
		return true
	case tool.builtin != "":
		return false
	case tool.sub != nil:
		return len(effectiveEmit(tool.sub.Emit, l.emit, true)) == 0
	default:
		fn := tool.fn
		caps := fn.Caps
		if len(caps.Emit) != 0 || len(caps.Mutations) != 0 || len(caps.Call) != 0 || len(caps.Agents) != 0 {
			return false
		}
		// A declared effect beyond read reaches past the repository (a sent
		// message, a payment), where two calls' order is the owner's to
		// see. An undeclared one with egress may be the same, so only a
		// declared read keeps egress concurrent.
		switch fn.Effect {
		case vocabulary.FunctionEffectRead:
			return true
		case "":
			return len(caps.Network) == 0
		default:
			return false
		}
	}
}

// parallelTools is how many calls of one run dispatch at once.
func (l *agentLoop) parallelTools() int {
	return max(1, l.ag.Budgets.MaxParallelTools)
}

// dispatchRun runs one span of calls and returns once every one settled.
// toolStarted goes out for the whole span before any call dispatches, and
// toolFinished in call order as each prefix of the span settles, so a
// client sees every running call and the results in the order the model
// asked. The calls of a span of more than one are concurrent-safe; up to
// budgets.maxParallelTools of them run at once.
func (l *agentLoop) dispatchRun(ctx context.Context, run []*toolRun) {
	live := 0
	for _, c := range run {
		if !c.refused {
			live++
			l.toolEvent(substrate.AgentEventToolStarted, c.call, c.turn, nil, "")
		}
	}
	finish := func(c *toolRun) {
		c.out = l.capToolResult(c.out)
		job := ""
		if c.job != nil {
			job = jobWorking
		}
		l.toolEventJob(substrate.AgentEventToolFinished, c.call, c.turn, &c.ok, c.out, job)
	}
	limit := l.parallelTools()
	if live <= 1 || limit == 1 {
		for _, c := range run {
			if !c.refused {
				l.prepareCall(c, 1)
				if !c.settled {
					c.out, c.ok = l.dispatch(ctx, c)
				}
			}
			finish(c)
		}
		return
	}
	done := make([]chan struct{}, len(run))
	for i := range done {
		done[i] = make(chan struct{})
	}
	// The launcher prepares each call in call order as a slot frees, so
	// the read budget is spent in the order the model asked and a later
	// `query` sees what an earlier one already gave back.
	go func() {
		slots := make(chan struct{}, limit)
		queries := 0
		for _, c := range run {
			if !c.refused && l.byName[c.call.Name].builtin == vocabulary.AgentToolQuery {
				queries++
			}
		}
		for i, c := range run {
			if c.refused {
				close(done[i])
				continue
			}
			slots <- struct{}{}
			l.prepareCall(c, min(queries, limit))
			if l.byName[c.call.Name].builtin == vocabulary.AgentToolQuery {
				queries--
			}
			if c.settled {
				<-slots
				close(done[i])
				continue
			}
			go func() {
				defer close(done[i])
				defer func() { <-slots }()
				defer func() {
					if r := recover(); r != nil {
						c.panicked = r
					}
				}()
				c.out, c.ok = l.dispatch(ctx, c)
			}()
		}
	}()
	for i, c := range run {
		<-done[i]
		finish(c)
	}
	for _, c := range run {
		if c.panicked != nil {
			panic(c.panicked)
		}
	}
}

// prepareCall resolves a call's tool and arguments, and spends a `query`
// call's read budget, before the call launches. pending is how many
// `query` calls of the span may hold a share of the rows at once, this one
// included: each takes its part of what is left, so concurrent reads never
// overrun the budget together. A call answered here is settled.
func (l *agentLoop) prepareCall(c *toolRun, pending int) {
	settle := func(out string) {
		c.out, c.ok, c.settled = out, false, true
	}
	tool, ok := l.byName[c.call.Name]
	if !ok {
		settle(toolError("unknown tool: " + c.call.Name))
		return
	}
	c.tool = tool
	if s := strings.TrimSpace(c.call.Arguments); s != "" {
		if err := json.Unmarshal([]byte(s), &c.args); err != nil {
			settle(toolError("arguments are not a JSON object: " + err.Error()))
			return
		}
	}
	if tool.builtin != vocabulary.AgentToolQuery {
		return
	}
	reads := l.ag.Reads
	if reads == nil {
		settle(toolError("query is not granted — the agent declares no reads"))
		return
	}
	if l.readCalls >= reads.Calls {
		settle(toolError(fmt.Sprintf("read budget exhausted (%d calls)", reads.Calls)))
		return
	}
	l.readCalls++
	l.readMu.Lock()
	defer l.readMu.Unlock()
	left := reads.Rows - l.readRows - l.readReserved
	if left <= 0 {
		settle(toolError(fmt.Sprintf("read budget exhausted (%d rows)", reads.Rows)))
		return
	}
	pending = max(1, pending)
	c.readShare = min(left, (left+pending-1)/pending)
	l.readReserved += c.readShare
}

// settleRead gives back a `query` call's share of the rows and counts what
// it read.
func (l *agentLoop) settleRead(c *toolRun, rows int) {
	l.readMu.Lock()
	defer l.readMu.Unlock()
	l.readReserved -= c.readShare
	l.readRows += rows
	c.readShare = 0
}
