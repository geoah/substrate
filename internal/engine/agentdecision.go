package engine

// The resolution primitive. A transition declared with `notifies: <prop>`
// (vocabulary.Transition.Notifies, a reference property pinned to llmthread)
// reports itself into that thread: the same transaction writes ONE `system`
// llmmessage — the kind's envelope plus the transition's changelog entries —
// and, after commit, the thread RESUMES so the agent's next turn reacts to
// it. PR #72 hardcoded this for recordpatchrequest; the marker is the same
// mechanism, declared (docs/plans/thread-interactions.md). The message is an
// ordinary record: any reader of the thread (the console, GraphQL, the watch
// stream) sees the resolution without this package's help, and the loop's
// history replay hands it to the model as context.
//
// Resume is BOUNDED, because a resume is a paid agent turn: the agent's own
// `resume: never` withholds it (the row still lands), a transition performed
// by the thread's own agent never resumes that thread (the self-actor
// exclusion precedent), and a per-thread hourly budget caps how often any
// thread wakes. Delivery is at-least-once, not fire-and-forget: a resume
// that loses the lease mid-turn is recovered by the loop's settle-time
// re-check (agentloop.go), and a resume lost to a restart by the sweep
// (SweepResolutions), both keyed on rows the records already hold.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// msgRoleSystem is the substrate's own turn in a thread. The other roles are
// the chat wire's (agentloop.go); this one never travels to a provider as a
// system message — history replay hands it over as user content, because the
// wire's system slot belongs to the agent's prompt.
const msgRoleSystem = "system"

// The two verdicts, as the request declaration spells its states.
const (
	decisionAccepted = "accepted"
	decisionRejected = "rejected"
)

// resumeBudgetPerHour caps how often one thread wakes on resolutions: past
// it the rows still land and the sweep or the next continuation picks them
// up, but nothing turns a transition cycle into a token pump.
const resumeBudgetPerHour = 20

// resolutionNote is the marker transition apply performed: which reference
// property names the thread, which machine moved, and where it landed.
type resolutionNote struct {
	prop    string
	machine string
	state   string
}

// recordResolution writes the resolution's system message into the thread
// the marker names, inside the resolving transaction — the transition and
// its report land together or not at all. `wrote` is the transaction's own
// changelog entries: the record's transition, and whatever the transition's
// onEnter applied.
func (t *txn) recordResolution(ty *vocabulary.Kind, rec *erow, note *resolutionNote, wrote []changeEntry) error {
	threadID := referenceID(rec.Props[note.prop])
	if threadID == "" {
		return nil
	}
	env, err := t.resolutionEnvelope(ty, rec, note)
	if err != nil {
		return err
	}
	if err := t.putThreadSystemRow(threadID, env, wrote); err != nil {
		return err
	}
	ds, decider := t.ds, t.actor
	t.afterCommit = append(t.afterCommit, func() { ds.resumeNotifiedThread(threadID, decider) })
	return nil
}

// resolutionEnvelope is the system row's content: self-describing JSON,
// exactly like a trigger delivery's user message, so the model and any other
// reader parse the same bytes. The only universal key is `event`; the rest
// is the kind's own contract — the request's decision shape is grandfathered
// from PR #72, and a kind without an enrichment gets the generic form.
func (t *txn) resolutionEnvelope(ty *vocabulary.Kind, rec *erow, note *resolutionNote) (map[string]any, error) {
	if ty.Identity == vocabulary.KindRecordPatchRequest && note.machine == propDecision {
		return t.proposalDecisionEnvelope(rec, note.state)
	}
	if ty.Identity == vocabulary.KindLLMInteraction && note.machine == propInteractionState {
		env := map[string]any{
			"event":       "interactionDismissed",
			"interaction": vocabulary.RecordPath(ty.Identity, rec.ID),
		}
		if note.state == interactionAnswered {
			// The answers ride the envelope verbatim, so the model reads them
			// without a second query.
			env["event"] = "interactionAnswered"
			env["answers"] = rec.Props["answers"]
		}
		return env, nil
	}
	return map[string]any{
		"event":   "recordResolved",
		"record":  vocabulary.RecordPath(ty.Identity, rec.ID),
		"machine": note.machine,
		"state":   note.state,
	}, nil
}

// proposalDecisionEnvelope is the request kind's enrichment: the verdict,
// the target, and — after an accepted apply — the version the accept
// produced, which is what the agent quotes back.
func (t *txn) proposalDecisionEnvelope(req *erow, verdict string) (map[string]any, error) {
	op := requestOp(req.Props)
	env := map[string]any{
		"event":    "proposalDecision",
		"request":  vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, req.ID),
		"decision": verdict,
		"op":       op,
	}
	target, err := t.requestTarget(req)
	if err != nil {
		return nil, err
	}
	if target.ID != "" {
		env["target"] = vocabulary.RecordPath(target.Kind, target.ID)
		if verdict == decisionAccepted {
			if op == opDelete {
				env["deleted"] = true
			} else if fresh, err := t.loadRow(target, false); err != nil {
				return nil, err
			} else if fresh != nil {
				env["version"] = fresh.Version
			}
		}
	}
	return env, nil
}

// recordProposalConflict reports a FAILED accept back to the proposing
// thread: the transition rolled back (the diff no longer applies, most often
// because the target moved), the request stays proposed, and without this
// row the agent that was told "held for review" waits on a request that can
// never land as reviewed. Called from the conflict-annotation transaction
// (write.go patchWith), so the annotation and the report commit together.
func (t *txn) recordProposalConflict(req *erow, reason string) error {
	threadID := referenceID(req.Props["thread"])
	if threadID == "" {
		return nil
	}
	env := map[string]any{
		"event":   "proposalConflicted",
		"request": vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, req.ID),
		"op":      requestOp(req.Props),
		"reason":  reason,
	}
	if target, err := t.requestTarget(req); err != nil {
		return err
	} else if target.ID != "" {
		env["target"] = vocabulary.RecordPath(target.Kind, target.ID)
	}
	if err := t.putThreadSystemRow(threadID, env, t.entries); err != nil {
		return err
	}
	ds, decider := t.ds, t.actor
	t.afterCommit = append(t.afterCommit, func() { ds.resumeNotifiedThread(threadID, decider) })
	return nil
}

// requestTarget resolves what a request would change: the target edge on a
// patch/delete, the named identity on a create.
func (t *txn) requestTarget(req *erow) (eref, error) {
	if requestOp(req.Props) == opCreate {
		targetKind, _ := req.Props["targetKind"].(string)
		targetID, _ := req.Props["targetId"].(string)
		if targetKind != "" && targetID != "" {
			return eref{Kind: targetKind, ID: targetID}, nil
		}
		return eref{}, nil
	}
	return t.edgeTargetOf(req.ref(), propTarget)
}

// putThreadSystemRow writes one `system` llmmessage into a thread: the
// envelope as content, the resolution's changelog entries as `changes`, the
// next stored turn ordinal. The writer is the resolving actor — an owner
// decision writes under the owner's hand, a judge's under the policy's — so
// the row says who resolved.
func (t *txn) putThreadSystemRow(threadID string, env map[string]any, wrote []changeEntry) error {
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
	_, err = t.put(substrate.PutInput{Kind: typeMessage, ID: id, Properties: props})
	return err
}

// nextThreadTurn is the thread's next message ordinal — the max stored
// `turn` plus one. A continuation running CONCURRENTLY may mint the same
// ordinal from its in-memory counter; replay stays honest because history
// orders by creation, and the ordinal is display.
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

// resumeNotifiedThread continues the thread a resolution just reported into,
// after the resolving transaction committed: the system message is already a
// row, so the continuation's history replays it and the agent's next turn is
// its reaction. The invocation carries NO user message — the resolution row
// is the turn — and runs under the thread's own lease (claimThread), so a
// thread that is mid-turn refuses cleanly; the loop's settle-time re-check
// is what then picks the resolution up.
func (ds *dataset) resumeNotifiedThread(threadID string, decider substrate.Actor) {
	ds.spawn("resume notified thread", func(ctx context.Context) {
		if !ds.admitResume(ctx, threadID, decider) {
			return
		}
		ds.continueThread(ctx, threadID)
	})
}

// admitResume applies the resume bounds: the agent's own `resume: never`,
// the self-exclusion (a thread's own agent resolving something never wakes
// its own thread — it is already awake, or chose to settle), and the hourly
// per-thread budget.
func (ds *dataset) admitResume(ctx context.Context, threadID string, decider substrate.Actor) bool {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
	if err != nil || row == nil || row.DeletedAt != nil {
		ds.svc.log.Warn("substrate: resume: thread does not resolve", "thread", threadID, "error", err)
		return false
	}
	agentID := referenceID(row.Props["agent"])
	ag, err := ds.registry().ResolveAgent(agentID)
	if err != nil {
		ds.svc.log.Warn("substrate: resume: agent does not resolve", "thread", threadID, "agent", agentID, "error", err)
		return false
	}
	if ag.Resume == vocabulary.AgentResumeNever {
		return false
	}
	if decider != "" && string(decider) == ag.Actor() {
		return false
	}
	n, err := ds.systemRowsInLastHour(ctx, threadID)
	if err != nil {
		ds.svc.log.Warn("substrate: resume: budget query failed", "thread", threadID, "error", err)
		return false
	}
	if n > resumeBudgetPerHour {
		ds.svc.log.Warn("substrate: resume: the thread's hourly resume budget is spent — the sweep will retry",
			"thread", threadID, "resolutions", n)
		return false
	}
	return true
}

// systemRowsInLastHour approximates the thread's resume pressure: every
// resolution writes exactly one system row, so counting them bounds how
// often a transition cycle can wake the agent.
func (ds *dataset) systemRowsInLastHour(ctx context.Context, threadID string) (int, error) {
	probe, err := json.Marshal(map[string]any{
		msgRelThread: vocabulary.RecordPath(typeThread, threadID),
		"role":       msgRoleSystem,
	})
	if err != nil {
		return 0, err
	}
	var n int
	err = ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props @> $2::jsonb
		  AND created_at > $3`,
		typeMessage, string(probe), nowUTC().Add(-time.Hour)).Scan(&n)
	return n, err
}

// continueThread runs one continuation of a settled thread: no user message,
// the stored rows are the whole context. Fire-and-forget by design — a
// refusal (mid-turn lease, disabled bundle) is logged, and the settle-time
// re-check or the sweep is the retry.
func (ds *dataset) continueThread(ctx context.Context, threadID string) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
	if err != nil || row == nil || row.DeletedAt != nil {
		return
	}
	agentID := referenceID(row.Props["agent"])
	ag, err := ds.registry().ResolveAgent(agentID)
	if err != nil {
		ds.svc.log.Warn("substrate: resume: agent does not resolve", "thread", threadID, "agent", agentID, "error", err)
		return
	}
	ctx, release, err := ds.admitCallable(ctx, ag.Authority, ag.Identity())
	if err != nil {
		ds.svc.log.Warn("substrate: resume: agent is not admissible", "thread", threadID, "agent", agentID, "error", err)
		return
	}
	defer release()
	mode, _ := row.Props["mode"].(string)
	if _, err := ds.runAgent(ctx, ag, agentInvocation{mode: mode, threadID: threadID}); err != nil {
		ds.svc.log.Warn("substrate: resume: the continuation failed", "thread", threadID, "agent", agentID, "error", err)
	}
}

// SweepResolutions resumes settled threads whose newest resolution row
// postdates their settlement — the resumes a restart or a lost lease
// dropped. Both facts are already records, so the sweep is one query; it
// runs on the same background cadence the trigger dispatcher does
// (cmd/substrated). N unconsumed resolutions on one thread coalesce into the
// one continuation, which replays them all.
func (ds *dataset) SweepResolutions(ctx context.Context) (int, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT t.id FROM records t
		WHERE t.kind = $1 AND t.deleted_at IS NULL
		  AND t.props->>'status' IN ('ok', 'overbudget')
		  AND EXISTS (
		    SELECT 1 FROM records m
		    WHERE m.kind = $2 AND m.deleted_at IS NULL
		      AND m.props->>'role' = 'system'
		      AND m.props->>'thread' = 'core.substrate.reamde.dev/llmthread/' || t.id
		      AND m.created_at > (t.props->>'finishedAt')::timestamptz
		  )`,
		typeThread, typeMessage)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var threads []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		threads = append(threads, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	resumed := 0
	for _, id := range threads {
		if !ds.admitResume(ctx, id, "") {
			continue
		}
		ds.continueThread(ctx, id)
		resumed++
	}
	return resumed, nil
}
