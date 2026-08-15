package engine

// The decision's report back to the thread that proposed it. A
// recordpatchrequest an agent's propose landed carries a `thread` reference;
// when the decision transition runs (write.go apply), the same transaction
// writes ONE `system` llmmessage into that thread — the verdict, the target,
// the version the accept produced, and the changelog entries the decision
// wrote — and, after commit, the thread RESUMES so the agent's next turn
// reacts to it. The message is an ordinary record: any reader of the thread
// (the console, GraphQL, the watch stream) sees the decision without this
// package's help, and the loop's history replay hands it to the model as
// context.
//
// Resume is ALWAYS-ON for now; making it the agent declaration's own choice
// is tracked work. It is also fire-and-forget and in-process: a refusal (the
// thread is mid-turn, the agent is gone, the bundle is disabled) is logged,
// never surfaced to the decider — the decision itself has already landed —
// and a server restart between commit and resume drops the resume, never the
// message.

import (
	"context"
	"encoding/json"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// msgRoleSystem is the substrate's own turn in a thread. The other roles are
// the chat wire's (agentloop.go); this one never travels to a provider as a
// system message — history replay hands it over as user content, because the
// wire's system slot belongs to the agent's prompt.
const msgRoleSystem = "system"

// The two verdicts, as the declaration spells its states.
const (
	decisionAccepted = "accepted"
	decisionRejected = "rejected"
)

// recordProposalDecision writes the decision's system message into the
// proposing thread, inside the deciding transaction — the decision and its
// report land together or not at all. `wrote` is the decision's own changelog
// entries: the request's patch, and (on accept) the apply's writes.
func (t *txn) recordProposalDecision(req *erow, verdict string, wrote []changeEntry) error {
	threadID := referenceID(req.Props["thread"])
	if threadID == "" {
		return nil
	}
	op := requestOp(req.Props)
	// The envelope is the message's content: self-describing JSON, exactly
	// like a trigger delivery's user message, so the model and any other
	// reader parse the same bytes.
	env := map[string]any{
		"event":    "proposalDecision",
		"request":  vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, req.ID),
		"decision": verdict,
		"op":       op,
	}
	var target eref
	if op == opCreate {
		targetKind, _ := req.Props["targetKind"].(string)
		targetID, _ := req.Props["targetId"].(string)
		if targetKind != "" && targetID != "" {
			target = eref{Kind: targetKind, ID: targetID}
		}
	} else {
		tgt, err := t.edgeTargetOf(req.ref(), propTarget)
		if err != nil {
			return err
		}
		target = tgt
	}
	if target.ID != "" {
		env["target"] = vocabulary.RecordPath(target.Kind, target.ID)
		if verdict == decisionAccepted {
			if op == opDelete {
				env["deleted"] = true
			} else if fresh, err := t.loadRow(target, false); err != nil {
				return err
			} else if fresh != nil {
				// Read AFTER applyEditDiff ran, so this is the version the
				// accept produced — what the agent quotes back.
				env["version"] = fresh.Version
			}
		}
	}
	content, err := json.Marshal(env)
	if err != nil {
		return err
	}
	turn, err := t.nextThreadTurn(threadID)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	props := map[string]any{
		"role": msgRoleSystem, "content": string(content),
		"turn": turn, msgRelThread: threadID,
	}
	if len(wrote) > 0 {
		props["changes"] = changeProps(wrote)
	}
	// The decider's own hand: an owner decision writes under the owner's
	// actor, a judge agent's under the agent's, so the row says who decided.
	if _, err := t.put(substrate.PutInput{Kind: typeMessage, ID: id, Properties: props}); err != nil {
		return err
	}
	ds := t.ds
	t.afterCommit = append(t.afterCommit, func() { ds.resumeDecidedThread(threadID) })
	return nil
}

// nextThreadTurn is the thread's next message ordinal — the max stored `turn`
// plus one, computed under the deciding transaction. The loop's own counter
// is not running here, and a resumed continuation re-derives its counter from
// the stored rows the same way (loadHistory), so the two never collide.
func (t *txn) nextThreadTurn(threadID string) (int64, error) {
	probe, err := json.Marshal(map[string]any{
		msgRelThread: vocabulary.RecordPath(typeThread, threadID),
	})
	if err != nil {
		return 0, err
	}
	var next int64
	err = t.row(`
		SELECT coalesce(max((props->>'turn')::bigint), -1) + 1 FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props @> $2::jsonb`,
		typeMessage, string(probe)).Scan(&next)
	return next, err
}

// resumeDecidedThread continues the thread whose proposal was just decided,
// after the deciding transaction committed: the system message is already a
// row, so the continuation's history replays it and the agent's next turn is
// its reaction. The invocation carries NO user message — the decision row is
// the turn — and runs under the thread's own lease (claimThread), so a thread
// that is mid-turn refuses cleanly and picks the decision up on its next
// continuation instead.
func (ds *dataset) resumeDecidedThread(threadID string) {
	go func() {
		ctx := context.Background()
		row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
		if err != nil || row == nil || row.DeletedAt != nil {
			ds.svc.log.Warn("substrate: decision resume: thread does not resolve",
				"thread", threadID, "error", err)
			return
		}
		agentID := referenceID(row.Props["agent"])
		ag, err := ds.registry().ResolveAgent(agentID)
		if err != nil {
			ds.svc.log.Warn("substrate: decision resume: agent does not resolve",
				"thread", threadID, "agent", agentID, "error", err)
			return
		}
		ctx, release, err := ds.admitCallable(ctx, ag.Authority, ag.Identity())
		if err != nil {
			ds.svc.log.Warn("substrate: decision resume: agent is not admissible",
				"thread", threadID, "agent", agentID, "error", err)
			return
		}
		defer release()
		mode, _ := row.Props["mode"].(string)
		if _, err := ds.runAgent(ctx, ag, agentInvocation{mode: mode, threadID: threadID}); err != nil {
			ds.svc.log.Warn("substrate: decision resume: the continuation failed",
				"thread", threadID, "agent", agentID, "error", err)
		}
	}()
}
