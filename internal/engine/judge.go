package engine

// The judge (docs/plans/thread-interactions.md phase 4): when a policy that
// gated a request (or matched a voluntary propose) names one, the ENGINE
// runs it and the ENGINE decides — the judge only ever recommends. The judge
// is an ordinary agent run TOOL-LESS over a typed projection of exactly what
// a human reviewer reads (the frozen envelope, the proposer's identity, the
// policy's criteria, and — only when the policy's `context: thread` opted in
// — the proposing thread's recent turns, delimited as data). Its whole
// contract is the structured reply {verdict, confidence, rationale}.
//
// Routing fails closed at every gap: sub-threshold confidence, an escalate
// verdict, malformed output, transport failure, a request that moved under
// the judging (the decision CAS's on the version read before the judge ran)
// — all leave the request PROPOSED for the owner, the verdict riding it as a
// recommendation. Only `enforce` mode with confidence past the owner's own
// thresholds decides, and the decision runs under the POLICY's actor,
// bounded by this request's target kind — never by the judge's emit, which
// stays empty: a judge is advice, the policy is the grant.
//
// Every invocation, failures included, writes its verdict onto the request
// as the engine-owned `policy/verdict` annotation; the changelog versions
// annotation writes immutably, and the judge's own thread is the run record
// with its cost tallied, so the audit is complete with no extra kind.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// annPolicyVerdict is the engine-owned audit annotation on a judged request.
const annPolicyVerdict = "policy/verdict"

// The verdict vocabulary a judge replies with.
const (
	judgeVerdictAccept   = "accept"
	judgeVerdictReject   = "reject"
	judgeVerdictEscalate = "escalate"
)

// The audit outcomes.
const (
	judgedAccepted  = "accepted"
	judgedRejected  = "rejected"
	judgedEscalated = "escalated"
	judgedAdvised   = "advised"
	judgedError     = "error"
)

// judgeContextTurns bounds how much thread the `context: thread` dial hands
// the judge: the most recent prose turns, as data.
const judgeContextTurns = 6

// judgeVerdict is the judge's structured reply, decoded strictly.
type judgeVerdict struct {
	Verdict    string  `json:"verdict"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

// maybeJudge schedules the judge over a fresh request when the governing
// rule names one. Called after the creating transaction committed (the gate
// conversion, and a voluntary propose a judge-bearing policy matched).
func (ds *dataset) maybeJudge(requestID string, rule *policyRule) {
	if rule == nil || rule.judge == "" {
		return
	}
	snapshot := *rule
	go ds.judgeRequest(context.Background(), requestID, &snapshot)
}

// judgeRequest runs one evaluation: read the request at a version, run the
// judge, route, audit. Idempotent by construction — the decision CAS's on
// the read version and a decided request is left alone — so a duplicate
// invocation converges.
func (ds *dataset) judgeRequest(ctx context.Context, requestID string, rule *policyRule) {
	req, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, requestID)
	if err != nil {
		ds.svc.log.Warn("substrate: judge: request does not resolve", "request", requestID, "error", err)
		return
	}
	if decision, _ := req.Properties["decision"].(string); decision != "" && decision != "proposed" {
		return
	}
	verdict, judgeThread, jerr := ds.runJudge(ctx, req, rule)
	outcome := judgedEscalated
	note := ""
	switch {
	case jerr != nil:
		outcome, note = judgedError, jerr.Error()
	case rule.mode != "enforce":
		outcome = judgedAdvised
	case verdict.Verdict == judgeVerdictAccept && rule.autoAccept != nil && verdict.Confidence >= *rule.autoAccept:
		outcome = judgedAccepted
	case verdict.Verdict == judgeVerdictReject && rule.autoRefuse != nil && verdict.Confidence >= *rule.autoRefuse:
		outcome = judgedRejected
	}
	if outcome == judgedAccepted || outcome == judgedRejected {
		if err := ds.decideAsPolicy(ctx, req, rule, outcome); err != nil {
			// The decision lost (the request moved, the apply conflicted):
			// fail closed into review, with the loss on the record.
			outcome, note = judgedEscalated, err.Error()
		}
	}
	audit := map[string]any{
		"policy":         vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, rule.id),
		"policyRevision": rule.version,
		"judge":          rule.judge,
		"outcome":        outcome,
		"requestVersion": req.Version,
	}
	if judgeThread != "" {
		audit["thread"] = vocabulary.RecordPath(typeThread, judgeThread)
	}
	if jerr == nil {
		audit["verdict"] = verdict.Verdict
		audit["confidence"] = verdict.Confidence
		audit["rationale"] = verdict.Rationale
	}
	if note != "" {
		audit["note"] = note
	}
	// The audit rides the request as an engine-owned annotation; the
	// changelog entry beside it is what makes the write immutable history.
	if aerr := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if _, err := t.putAnnotation(eref{Kind: vocabulary.KindRecordPatchRequest, ID: req.ID}, annPolicyVerdict, audit); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpPatch, req.ID,
			vocabulary.KindRecordPatchRequest, map[string]any{"policyVerdict": audit})
	}); aerr != nil {
		ds.svc.log.Warn("substrate: judge: the audit write failed", "request", requestID, "error", aerr)
	}
}

// runJudge invokes the judge agent over the typed projection and decodes its
// structured reply. The judge must be TOOL-LESS: a judge that could write
// while judging would be an authorization surface with hands.
func (ds *dataset) runJudge(ctx context.Context, req *substrate.Record, rule *policyRule) (judgeVerdict, string, error) {
	var v judgeVerdict
	ag, err := ds.registry().ResolveAgent(rule.judge)
	if err != nil {
		return v, "", fmt.Errorf("judge %s does not resolve: %w", rule.judge, err)
	}
	if len(ag.Tools) > 0 || len(ag.Agents) > 0 {
		return v, "", fmt.Errorf("judge %s carries tools — a judge reads the envelope and replies, nothing else", rule.judge)
	}
	envelope := map[string]any{
		"event":   "judgeRequest",
		"request": vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, req.ID),
		"op":      requestOp(req.Properties),
		"diff":    req.Properties["diff"],
	}
	if rationale, _ := req.Properties["rationale"].(string); rationale != "" {
		envelope["rationale"] = rationale
	}
	if tk, _ := req.Properties["targetKind"].(string); tk != "" {
		envelope["targetKind"] = tk
		envelope["targetId"] = req.Properties["targetId"]
	}
	for _, e := range req.Edges[propTarget] {
		envelope["target"] = vocabulary.RecordPath(e.Kind, e.ID)
		break
	}
	if rule.criteria != "" {
		envelope["criteria"] = rule.criteria
	}
	threadID := referenceID(req.Properties["thread"])
	if threadID != "" {
		agentID, err := ds.threadAgent(ctx, threadID)
		if err == nil && agentID != "" {
			envelope["proposer"] = agentID
		}
		// The owner's dial: thread context is more signal AND more injection
		// surface, so it travels only where the policy opted in — as data.
		if rule.context == "thread" {
			turns, err := ds.recentProseTurns(ctx, threadID, judgeContextTurns)
			if err == nil && len(turns) > 0 {
				envelope["thread"] = turns
			}
		}
	}
	user, err := json.Marshal(envelope)
	if err != nil {
		return v, "", err
	}
	ctx, release, err := ds.admitCallable(ctx, ag.Authority, ag.Identity())
	if err != nil {
		return v, "", err
	}
	defer release()
	res, err := ds.runAgent(ctx, ag, agentInvocation{mode: "judge", user: string(user)})
	if err != nil {
		return v, "", err
	}
	if res.Status != threadOK {
		return v, res.Thread, fmt.Errorf("the judge's run settled %s (%s)", res.Status, res.Reason)
	}
	if err := decodeJudgeVerdict(res.Reply, &v); err != nil {
		return v, res.Thread, err
	}
	return v, res.Thread, nil
}

// decodeJudgeVerdict reads the judge's reply STRICTLY: one JSON object,
// a declared verdict, confidence in [0,1]. A model that padded its answer
// with prose fails here and the request escalates — never a lenient parse
// that guesses what an authorization surface meant.
func decodeJudgeVerdict(reply string, v *judgeVerdict) error {
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(reply)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("the judge's reply is not the verdict object: %w", err)
	}
	switch v.Verdict {
	case judgeVerdictAccept, judgeVerdictReject, judgeVerdictEscalate:
	default:
		return fmt.Errorf("the judge's verdict %q is not accept, reject or escalate", v.Verdict)
	}
	if v.Confidence < 0 || v.Confidence > 1 {
		return fmt.Errorf("the judge's confidence %v is outside [0,1]", v.Confidence)
	}
	return nil
}

// decideAsPolicy performs the engine's decision under the POLICY's actor:
// bounded by THIS request's target kind (the policy governed this request;
// the grant is the match), CAS'd on the version the judge read, and marked
// policyDecision so the gated-request guard admits exactly this hand.
func (ds *dataset) decideAsPolicy(ctx context.Context, req *substrate.Record, rule *policyRule, outcome string) error {
	decision := decisionRejected
	if outcome == judgedAccepted {
		decision = decisionAccepted
	}
	targetKind, err := ds.requestTargetKind(ctx, req)
	if err != nil {
		return err
	}
	version := req.Version
	// The rationale rides the policy/verdict audit the engine writes beside
	// this decision; the decision itself carries only the transition, CAS'd
	// on the version the judge read.
	in := substrate.PatchInput{
		Properties: map[string]any{propDecision: decision},
		IfVersion:  &version,
	}
	actor := substrate.Actor("policy:" + rule.id)
	_, err = ds.patchBounded(ctx, actor, vocabulary.KindRecordPatchRequest, req.ID, in,
		&effectCeiling{emit: []string{targetKind}, policyDecision: true})
	return err
}

// requestTargetKind resolves the kind an accept would write: targetKind on a
// create, the target edge's kind otherwise.
func (ds *dataset) requestTargetKind(ctx context.Context, req *substrate.Record) (string, error) {
	if tk, _ := req.Properties["targetKind"].(string); tk != "" {
		return tk, nil
	}
	for _, e := range req.Edges[propTarget] {
		return e.Kind, nil
	}
	return "", fmt.Errorf("%w: the request names no target to bound the decision by", substrate.ErrValidation)
}

// threadAgent reads which agent a thread belongs to.
func (ds *dataset) threadAgent(ctx context.Context, threadID string) (string, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
	if err != nil || row == nil {
		return "", err
	}
	return referenceID(row.Props["agent"]), nil
}

// recentProseTurns reads a thread's last prose turns (user and assistant
// content, never tool payloads) as data for the judge's envelope.
func (ds *dataset) recentProseTurns(ctx context.Context, threadID string, limit int) ([]map[string]any, error) {
	probe, err := json.Marshal(map[string]any{
		msgRelThread: vocabulary.RecordPath(typeThread, threadID),
	})
	if err != nil {
		return nil, err
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT props->>'role', props->>'content' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props @> $2::jsonb
		  AND props->>'role' IN ('user', 'assistant')
		  AND coalesce(props->>'content', '') <> ''
		ORDER BY created_at DESC, id DESC LIMIT $3`,
		typeMessage, string(probe), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		// Newest-first from the query, oldest-first for the reader.
		out = append([]map[string]any{{"role": role, "content": content}}, out...)
	}
	return out, rows.Err()
}
