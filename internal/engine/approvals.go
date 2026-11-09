package engine

// Approvals (decision record 0254). An agent's call of a function declaring
// `approval: required` does not run: the loop lands an llm/approval holding
// what would run, with exactly what, and answers the agent that the owner is
// asked. The approval names the conversation the owner is in, the root of
// the call chain, so a sub-agent's call surfaces where the owner reads.
//
// The owner approves or declines it through one transition. Either commit
// hands the approval to settleApproval, which runs an approved call under the
// asking agent's own actor and recorded grant, exactly as a foreground call
// of it would have run, then writes the outcome onto the approval and an
// `approvalDecided` system row into the conversation in one transaction, and
// resumes the agent there. A call is never run twice: the run claims the
// approval (`running`) before the body starts, and one a restart interrupted
// ends `failed` at the next open and is reported as such.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// propApprovalState is the approval's one machine, and its states.
const (
	propApprovalState = "state"
	approvalPending   = "pending"
	approvalApproved  = "approved"
	approvalDenied    = "denied"
	approvalRunning   = "running"
	approvalDone      = "done"
	approvalFailed    = "failed"
)

// eventApprovalDecided is the `event` of the system row an approval's
// outcome writes into its conversation.
const eventApprovalDecided = "approvalDecided"

// approvalInterrupted is the error an approved call a restart ended reports.
const approvalInterrupted = "interrupted: the server stopped before the approved call finished, and it is not run again — call the tool again if it is still wanted"

// approvalFrozen are the properties the loop stamps at creation: what the
// owner read is what runs, so nothing changes them afterwards.
var approvalFrozen = []string{
	msgRelThread, "requestedIn", "agent", "function", "tool", "toolCallId", "arguments", "writes",
}

// approvalRunProps are the properties only the engine's run of the call
// writes.
var approvalRunProps = []string{"output", "error", "finishedAt", "reportedAt"}

// admitApproval validates the creating write: only the loop lands one, born
// pending, with nothing of a run on it.
func (t *txn) admitApproval(sp *applySpec) error {
	if !t.approvalLoop {
		return fmt.Errorf("%w: an approval is landed by the agent loop when an agent calls a function that needs one — nothing else creates it",
			substrate.ErrForbidden)
	}
	if want, ok := sp.states[propApprovalState]; ok && want != approvalPending {
		return fmt.Errorf("%w: an approval is born pending — %q skips the owner", substrate.ErrValidation, want)
	}
	for _, name := range approvalRunProps {
		if _, named := sp.props[name]; named {
			return fmt.Errorf("%w: %s is written by the run of an approved call, never at create", substrate.ErrValidation, name)
		}
	}
	return nil
}

// guardApproval holds every later write: the envelope is frozen, the run's
// properties and its three states are the engine's, and approving or
// declining is the owner's hand alone. A decision hands the approval to
// settleApproval once it commits.
func (t *txn) guardApproval(sp *applySpec) error {
	for _, name := range approvalFrozen {
		next, named := sp.props[name]
		if named && !jsonEqual(sp.existing.Props[name], next) {
			return fmt.Errorf("%w: %s is immutable on an approval — what the owner reads is what runs",
				substrate.ErrForbidden, name)
		}
	}
	if !t.approvalRun {
		for _, name := range approvalRunProps {
			next, named := sp.props[name]
			if named && !jsonEqual(sp.existing.Props[name], next) {
				return fmt.Errorf("%w: %s is written by the run of the approved call", substrate.ErrForbidden, name)
			}
		}
	}
	target, transitioning := sp.states[propApprovalState]
	cur := sp.existing.States[propApprovalState]
	if !transitioning || target == cur {
		return nil
	}
	switch target {
	case approvalApproved, approvalDenied:
		// An agent approving a call, its own or another's, would make "what
		// did the owner allow" a record of the agent: installed code and
		// agents run at bundle tier and never decide one.
		if t.tier == substrate.TierBundle || t.approvalRun {
			return fmt.Errorf("%w: an approval is decided by the owner, never by an agent or installed code", substrate.ErrForbidden)
		}
		id := sp.id
		ds := t.ds
		t.afterCommit = append(t.afterCommit, func() { ds.settleApproval(id) })
	default:
		if !t.approvalRun {
			return fmt.Errorf("%w: an approved call moves to %s by the engine's run of it, never by a write", substrate.ErrForbidden, target)
		}
	}
	return nil
}

// rootThread is the conversation a call chain belongs to: this loop's own
// thread at the root, the root's for every sub-agent under it.
func (l *agentLoop) rootThread() string {
	if l.in.root != "" {
		return l.in.root
	}
	return l.threadID
}

// dispatchApproval lands the approval a call of fn needs instead of running
// it. What a caller can answer at once is answered now (the arguments, the
// callee's lifecycle), so a call that could never run fails as a tool error
// rather than as a declined card later.
func (l *agentLoop) dispatchApproval(ctx context.Context, c *toolRun, fn *vocabulary.Function, args map[string]any) (string, bool) {
	var input any
	if args != nil {
		input = args
	}
	if fn.Input != nil {
		if err := vocabulary.CheckValue(fn.Input, input); err != nil {
			return toolError("input: " + err.Error()), false
		}
	}
	if _, _, err := l.ds.admitCallable(ctx, fn.Package, fn.Identity()); err != nil {
		return toolError(err.Error()), false
	}
	id, err := newID()
	if err != nil {
		return toolError(err.Error()), false
	}
	arguments := args
	if arguments == nil {
		arguments = map[string]any{}
	}
	props := map[string]any{
		msgRelThread: l.rootThread(),
		"agent":      l.ag.Identity(),
		"function":   fn.Identity(),
		"tool":       c.call.Name,
		"toolCallId": c.call.ID,
		"arguments":  arguments,
		"writes":     stringsToAny(l.emit),
	}
	if root := l.rootThread(); root != l.threadID {
		props["requestedIn"] = l.threadID
	}
	err = l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
		t.causedBy = l.in.causedBy
		t.changeSink = &c.changes
		t.approvalLoop = true
		_, err := t.put(substrate.PutInput{Kind: vocabulary.KindLLMApproval, ID: id, Properties: props})
		return err
	})
	if err != nil {
		return toolError(err.Error()), false
	}
	l.in.tally.addEffect("approval")
	return toolJSON(map[string]any{
		"approval": id,
		"status":   "waiting for the owner's approval",
		"note": "This call does not run until the owner approves it in the conversation, where they see " +
			"exactly these arguments. Do not call it again or ask the owner in words: tell them it is " +
			"waiting for their approval. The outcome arrives later as an approvalDecided message.",
	}), true
}

// settleApproval runs a decided approval's consequence on the background
// context: an approved call runs, and either outcome is reported.
func (ds *dataset) settleApproval(id string) {
	ds.spawn("settle approval", func(bg context.Context) { ds.runApproval(bg, id) })
}

// runApproval is settleApproval's work. An approval another settle already
// claimed, or one already reported, is left alone.
func (ds *dataset) runApproval(bg context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(bg), agentSettleTimeout+vocabulary.MaxRunTimeout)
	defer cancel()
	ref := eref{Kind: vocabulary.KindLLMApproval, ID: id}
	row, err := ds.loadApproval(ctx, ref)
	if err != nil || row == nil {
		if err != nil {
			ds.svc.log.Warn("substrate: reading an approval to settle failed", "approval", id, "error", err)
		}
		return
	}
	if propString(row, "reportedAt") != "" {
		return
	}
	actor := substrate.Actor(agentActorOf(ds, propRefID(row.Props["agent"])))
	switch row.States[propApprovalState] {
	case approvalDenied:
		ds.reportApproval(ctx, row, actor, approvalDenied, nil, nil)
	case approvalApproved:
		// The claim: approved -> running under the version read, so two
		// settles of one approval run its call once.
		claimed := false
		version := row.Version
		err := ds.inTx(ctx, actor, false, func(t *txn) error {
			t.approvalRun = true
			_, err := t.patch(ref, substrate.PatchInput{
				Properties: map[string]any{propApprovalState: approvalRunning}, IfVersion: &version,
			})
			claimed = err == nil
			return err
		})
		if err != nil || !claimed {
			return
		}
		out, ok, changes := ds.runApprovedCall(ctx, row)
		state := approvalDone
		if !ok {
			state = approvalFailed
		}
		ds.reportApproval(ctx, row, actor, state, &out, changes)
	}
}

// runApprovedCall runs the call an approval holds, as the asking agent's
// foreground call would have run: under its actor, its recorded grant and
// the function's own lifecycle admission, with the idempotency key the
// approval names.
func (ds *dataset) runApprovedCall(ctx context.Context, row *erow) (out string, ok bool, changes []changeEntry) {
	defer func() {
		if r := recover(); r != nil {
			ds.svc.log.Error("substrate: an approved call panicked", "approval", row.ID, "panic", fmt.Sprint(r))
			out, ok = toolError("the approved call failed inside the server"), false
		}
	}()
	reg := ds.registry()
	ag, err := reg.ResolveAgent(propRefID(row.Props["agent"]))
	if err != nil {
		return toolError("the agent that asked is gone: " + err.Error()), false, nil
	}
	fn, err := reg.ResolveFunction(propRefID(row.Props["function"]))
	if err != nil {
		return toolError("the function is gone: " + err.Error()), false, nil
	}
	actx, release, err := ds.admitCallable(ctx, fn.Package, fn.Identity())
	if err != nil {
		return toolError(err.Error()), false, nil
	}
	defer release()
	args, _ := row.Props["arguments"].(map[string]any)
	thread := propRefID(row.Props["requestedIn"])
	if thread == "" {
		thread = propRefID(row.Props[msgRelThread])
	}
	l := &agentLoop{
		ds: ds, ag: ag, actor: substrate.Actor(ag.Actor()), threadID: thread,
		emit: anyStrings(row.Props["writes"]), replyTurn: -1,
		in: agentInvocation{
			delivery: "approval/" + row.ID,
			tally:    &agentTally{effects: map[string]int{}},
		},
	}
	run := &toolRun{}
	out, ok = l.dispatchFunction(actx, run, fn, args)
	return out, ok, run.changes
}

// reportApproval writes an approval's outcome in one transaction: its end
// state and what the call answered, and the approvalDecided row in its
// conversation. Then it delivers the row as a background job's end is: to a
// loop running on the conversation, or as a resume of it.
func (ds *dataset) reportApproval(ctx context.Context, row *erow, actor substrate.Actor, state string, out *string, changes []changeEntry) {
	ref := eref{Kind: vocabulary.KindLLMApproval, ID: row.ID}
	thread := propRefID(row.Props[msgRelThread])
	decision := approvalApproved
	if state == approvalDenied {
		decision = approvalDenied
	}
	env := map[string]any{
		"event":    eventApprovalDecided,
		"approval": vocabulary.RecordPath(vocabulary.KindLLMApproval, row.ID),
		"decision": decision,
		"tool":     propString(row, "tool"),
		"callable": propRefID(row.Props["function"]),
		"agent":    propRefID(row.Props["agent"]),
	}
	props := map[string]any{"reportedAt": nowUTC().Format(time.RFC3339Nano)}
	if out != nil {
		var parsed map[string]any
		_ = json.Unmarshal([]byte(*out), &parsed)
		ok := state == approvalDone
		env["ok"] = ok
		props["finishedAt"] = props["reportedAt"]
		if ok {
			env["output"] = parsed["output"]
			props["output"] = parsed["output"]
		} else {
			msg, _ := parsed["error"].(string)
			if msg == "" {
				msg = *out
			}
			env["error"] = msg
			props["error"] = msg
		}
	}
	var systemRow string
	err := ds.inTx(ctx, actor, false, func(t *txn) error {
		t.approvalRun = true
		if cur := row.States[propApprovalState]; state != cur && state != approvalDenied {
			props[propApprovalState] = state
		}
		if _, err := t.patch(ref, substrate.PatchInput{Properties: props}); err != nil {
			return err
		}
		var err error
		systemRow, err = t.putThreadSystemRowID(thread, env, changes)
		return err
	})
	if err != nil {
		// Left unreported: the next open reports it (settleInterruptedApprovals).
		ds.svc.log.Warn("substrate: an approval's outcome was not written", "approval", row.ID, "error", err)
		return
	}
	ds.deliverSystemRow(thread, systemRow, toolJSON(env))
}

// deliverSystemRow hands a system row written outside a running turn to its
// thread: the loop running on it reads the row after its next batch of tool
// results, and a thread with none resumes. A loop that settles first finds
// the row in its settle-time re-check, and a resume lost after that is the
// resolution sweep's.
func (ds *dataset) deliverSystemRow(thread, row, content string) {
	if v, ok := ds.runningThreads.Load(thread); ok {
		if l, _ := v.(*agentLoop); l != nil && l.postJob(jobDelivery{row: row, content: content}) {
			return
		}
	}
	ds.resumeNotifiedThread(thread, "")
}

// loadApproval reads one approval row, nil when it is gone.
func (ds *dataset) loadApproval(ctx context.Context, ref eref) (*erow, error) {
	var row *erow
	err := ds.inTx(ctx, substrate.ActorAPI, true, func(t *txn) error {
		r, err := t.loadRow(ref, false)
		if err == nil && r != nil && r.DeletedAt == nil {
			row = r
		}
		return err
	})
	return row, err
}

// settleInterruptedApprovals is the approvals' open-time sweep, beside the
// jobs': no run of this repository is in flight at open, so a call left
// approved or running was cut off by the last writer's stop. It ends failed
// and is never run again, since nothing tells whether its body already sent;
// every decided approval not yet reported is reported. The resolution sweep
// resumes the conversations these rows land in.
func (ds *dataset) settleInterruptedApprovals(ctx context.Context) error {
	rows, err := ds.liveRowsOf(ctx, vocabulary.KindLLMApproval)
	if err != nil {
		return err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, row := range rows {
		if propString(row, "reportedAt") != "" {
			continue
		}
		actor := substrate.Actor(agentActorOf(ds, propRefID(row.Props["agent"])))
		switch state := row.States[propApprovalState]; state {
		case approvalApproved, approvalRunning:
			out := toolError(approvalInterrupted)
			ds.reportApproval(ctx, row, actor, approvalFailed, &out, nil)
		case approvalDenied:
			ds.reportApproval(ctx, row, actor, approvalDenied, nil, nil)
		case approvalDone, approvalFailed:
			var out string
			if state == approvalDone {
				out = toolJSON(map[string]any{"output": row.Props["output"]})
			} else {
				out = toolError(propString(row, "error"))
			}
			ds.reportApproval(ctx, row, actor, state, &out, nil)
		}
	}
	return nil
}

// agentActorOf is an agent's actor by identity, or the agent actor its
// identity spells when the agent is no longer declared.
func agentActorOf(ds *dataset, ident string) string {
	if ag, err := ds.registry().ResolveAgent(ident); err == nil {
		return ag.Actor()
	}
	authority, pkg, name := vocabulary.SplitKindRef(ident)
	if authority == "" {
		return string(substrate.ActorAPI)
	}
	return string(substrate.AgentActor(authority, pkg, name))
}

// propRefID is the referent's id of a stored reference value, either shape.
func propRefID(v any) string {
	ref, _, ok := splitReferenceValue(v)
	if !ok {
		return ""
	}
	return ref.ID
}
