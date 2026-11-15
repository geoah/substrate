package engine

// The resolution primitive. A transition declared with `notifies: <prop>`
// (vocabulary.Transition.Notifies, a reference property pinned to llm/thread)
// reports itself into that thread: the same transaction writes ONE `system`
// llm/message — the kind's envelope plus the transition's changelog entries —
// and, after commit, the thread RESUMES so the agent's next turn reacts to
// it. The marker is declared, never hardcoded per kind. The message is an
// ordinary record: any reader of the thread (the console, the records route, the
// watch stream) sees the resolution without this package's help, and the loop's
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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
// onEnter applied. The row carries the resolved item's holders, the
// continuer among them (itemHolders), so the resume it starts is held to
// the item's work and to whoever continued it.
func (t *txn) recordResolution(ty *vocabulary.Kind, rec *erow, note *resolutionNote, wrote []changeEntry) error {
	threadID := referenceID(rec.Props[note.prop])
	if threadID == "" {
		return nil
	}
	env, err := t.resolutionEnvelope(ty, rec, note)
	if err != nil {
		return err
	}
	held, err := t.itemHolders(ty, rec)
	if err != nil {
		return err
	}
	row, err := t.putThreadSystemRowID(threadID, env, wrote, held)
	if err != nil {
		return err
	}
	ds, decider := t.ds, t.actor
	t.afterCommit = append(t.afterCommit, func() { ds.resumeNotifiedThread(threadID, decider, row) })
	return nil
}

// resolutionEnvelope is the system row's content: self-describing JSON,
// exactly like a trigger delivery's user message, so the model and any other
// reader parse the same bytes. The only universal key is `event`; the rest
// is the kind's own contract: the request and the interaction kinds carry
// their own enrichment, and a kind without one gets the generic form.
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
	// An owner who adjusted the values before accepting tells the proposer
	// what was applied, which is no longer its own diff.
	if verdict == decisionAccepted {
		if adj, ok := req.Props[propAdjustedDiff]; ok && adj != nil {
			env[propAdjustedDiff] = adj
		}
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
	held, err := t.requestHolders(req)
	if err != nil {
		return err
	}
	row, err := t.putThreadSystemRowID(threadID, env, t.entries, held)
	if err != nil {
		return err
	}
	ds, decider := t.ds, t.actor
	t.afterCommit = append(t.afterCommit, func() { ds.resumeNotifiedThread(threadID, decider, row) })
	return nil
}

// requestTarget resolves what a request would change: the `target` reference on a
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
	return referenceTargetOf(req, propTarget), nil
}

// putThreadSystemRow writes one `system` llm/message into a thread: the
// envelope as content, the resolution's changelog entries as `changes`, the
// next stored turn ordinal. The writer is the resolving actor — an owner
// decision writes under the owner's hand, a judge's under the policy's — so
// the row says who resolved.
func (t *txn) putThreadSystemRow(threadID string, env map[string]any, wrote []changeEntry, held map[string]any) error {
	_, err := t.putThreadSystemRowID(threadID, env, wrote, held)
	return err
}

// putThreadSystemRowID is putThreadSystemRow answering the row's id. The row
// reports an item, and carries held, that item's holders, whatever hand's
// transaction writes it: the resume it starts is the item's work (decision
// 0276). None is an item asked before this release: the row carries no
// `heldTo`, so no resume runs from it, and it says so.
func (t *txn) putThreadSystemRowID(threadID string, env map[string]any, wrote []changeEntry, held map[string]any) (string, error) {
	if held == nil {
		if _, said := env["error"]; !said {
			env["error"] = errAskAgain.Error()
		}
	}
	content, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	turn, err := t.nextThreadTurn(threadID)
	if err != nil {
		return "", err
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	props := map[string]any{
		"role": msgRoleSystem, "content": string(content),
		"turn": turn, msgRelThread: threadID,
	}
	if len(wrote) > 0 {
		props["changes"] = changeProps(wrote)
	}
	// The resume this row starts reads the time it landed (decision 0196).
	renderTurnContext(nowUTC(), zoneOf(t.loadRow(eref{Kind: kindSetting, ID: timezoneSettingID}, false)), nil).stamp(props)
	if err := t.requireThreadKinds(); err != nil {
		return "", err
	}
	// The row is the run's record of the resolution, whichever hand's
	// transaction writes it.
	was, wasReport := t.exempt, t.report
	t.exempt, t.report = exemptConversation, &reportHolders{held: held}
	_, err = t.put(substrate.PutInput{Kind: typeMessage, ID: id, Properties: props})
	t.exempt, t.report = was, wasReport
	if err != nil {
		return "", err
	}
	return id, nil
}

// requireThreadKinds refuses a thread write on a repository whose boot upgrade
// has not landed the agent runtime's kinds. The loop addresses them by
// constant, so writing "somewhere else" would put a turn into a dormant kind
// under ordinals nothing else counts. A `notifies:` transition is the one write
// that can reach here before the upgrade, and it must say what is missing
// rather than land half of itself (records 0077, 0078).
func (t *txn) requireThreadKinds() error {
	reg := t.declarations()
	for _, ident := range []string{typeThread, typeMessage} {
		if _, ok := reg.ByIdentity(ident); !ok {
			return fmt.Errorf("%w: this repository does not declare %s: the shipped vocabulary upgrade that moves the agent runtime's kinds has not landed here, and a thread cannot be written to until it does",
				substrate.ErrValidation, ident)
		}
	}
	return nil
}

// nextThreadTurn is the thread's next message ordinal: the max stored
// `turn` plus one, or, while a loop of this process runs on the thread, an
// ordinal it reserves past every one it has drawn. Replay orders by
// creation; the ordinal is what a client pairs streamed events with rows by,
// so it is unique within a thread.
func (t *txn) nextThreadTurn(threadID string) (int64, error) {
	next, err := t.storedNextTurn(threadID)
	if err != nil {
		return 0, err
	}
	// A loop running on the thread holds ordinals no row carries yet (the
	// reply it is streaming, the tool call it is running): the row takes one
	// past them, so no two rows of a thread share an ordinal.
	if v, ok := t.ds.runningThreads.Load(threadID); ok {
		if l, _ := v.(*agentLoop); l != nil {
			next = int64(l.reserveTurnAfter(int(next)))
		}
	}
	return next, nil
}

// storedNextTurn is the ordinal after the highest one a stored row of the
// thread carries.
func (t *txn) storedNextTurn(threadID string) (int64, error) {
	b := &builder{}
	kind := b.arg(typeMessage)
	thread, err := threadMessagesWhere(t.ctx, t.tx, b, "e", threadID)
	if err != nil {
		return 0, err
	}
	var next int64
	err = t.row(`
		SELECT coalesce(max((e.props->>'turn')::bigint), -1) + 1 FROM records e
		WHERE e.kind = `+kind+` AND e.deleted_at IS NULL AND `+thread,
		b.args...).Scan(&next)
	return next, err
}

// resumeNotifiedThread continues the thread a resolution just reported into,
// after the resolving transaction committed: the system message (row) is
// already a row, so the continuation's history replays it and the agent's
// next turn is its reaction. The invocation carries NO user message — the
// resolution row is the turn — and runs under the thread's own lease
// (claimThread), so a thread that is mid-turn refuses cleanly; the loop's
// settle-time re-check is what then picks the resolution up.
func (ds *dataset) resumeNotifiedThread(threadID string, decider substrate.Actor, row string) {
	ds.spawn("resume notified thread", func(ctx context.Context) {
		if !ds.admitResume(ctx, threadID, decider) {
			return
		}
		ds.continueThread(ctx, threadID, row)
	})
}

// admitResume applies the resume bounds: the agent's own `resume: never`,
// the self-exclusion (a thread's own agent resolving something never wakes
// its own thread — it is already awake, or chose to settle), and the hourly
// per-thread budget.
func (ds *dataset) admitResume(ctx context.Context, threadID string, decider substrate.Actor) bool {
	// Shutdown reaches here as a canceled context. Every query below would then
	// fail and be logged as a thread that does not resolve, which is a false
	// alarm: the resume is dropped, and the sweep recovers it after the restart.
	if ctx.Err() != nil {
		return false
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
	if err != nil || row == nil || row.DeletedAt != nil {
		ds.svc.log.Warn("substrate: resume: thread does not resolve", "thread", threadID, "error", err)
		return false
	}
	// A stopped thread holds until its owner writes again, whoever resolved
	// what: the owner's next message replays the resolution rows.
	if status, _ := row.Props["status"].(string); status == threadStopped {
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
	b := &builder{}
	kind := b.arg(typeMessage)
	thread, err := threadMessagesWhere(ctx, ds.db, b, "e", threadID)
	if err != nil {
		return 0, err
	}
	var n int
	err = ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records e
		WHERE e.kind = `+kind+` AND e.deleted_at IS NULL AND `+thread+`
		  AND e.props->>'role' = `+b.arg(msgRoleSystem)+` AND e.created_at > `+b.arg(nowUTC().Add(-time.Hour)),
		b.args...).Scan(&n)
	return n, err
}

// continueThread runs one continuation of a settled thread: no user message,
// the stored rows are the whole context. Fire-and-forget by design — a
// refusal (mid-turn lease, disabled bundle) is logged, and the settle-time
// re-check or the sweep is the retry. The continuation is held to whom the
// resolution row (resolution) that starts the turn names in its `heldTo`.
//
// A chat thread's continuation runs as a detached chat turn does (decision
// 0317, resumeChatTurn): it streams into a hub, so an attach shows it and a
// stop stops it, and it holds one of its person's chat-turn slots.
func (ds *dataset) continueThread(ctx context.Context, threadID, resolution string) {
	ds.continueThreadFrom(ctx, deferredResume{thread: threadID, resolution: resolution}, nil)
}

// continueThreadFrom is continueThread for the turn r names: one a
// resolution row starts, or one a message sent while the last turn ran
// starts (r.sent, agentqueue.go), which is held to that message's sender.
// opened, when set, is called once the turn holds the thread.
func (ds *dataset) continueThreadFrom(ctx context.Context, r deferredResume, opened func()) {
	threadID := r.thread
	ctx, err := ds.heldBy(ctx, typeMessage, r.resolution)
	if err != nil {
		ds.svc.log.Warn("substrate: resume: whom the turn acts for was not read", "thread", threadID, "error", err)
		return
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
	if err != nil || row == nil || row.DeletedAt != nil {
		return
	}
	var startsFrom []string
	if r.sent {
		// The turn reads every message that waits, so it is held to each of
		// their senders, from before its first read (its pinned context) on.
		if ctx, startsFrom, err = ds.heldToUnanswered(ctx, threadID); err != nil {
			ds.svc.log.Warn("substrate: resume: whom the waiting messages act for was not read", "thread", threadID, "error", err)
			return
		}
	}
	agentID := referenceID(row.Props["agent"])
	ag, err := ds.registry().ResolveAgent(agentID)
	if err != nil {
		ds.svc.log.Warn("substrate: resume: agent does not resolve", "thread", threadID, "agent", agentID, "error", err)
		return
	}
	ctx, release, err := ds.admitCallable(ctx, ag.Package, ag.Identity())
	if err != nil {
		ds.svc.log.Warn("substrate: resume: agent is not admissible", "thread", threadID, "agent", agentID, "error", err)
		return
	}
	defer release()
	mode, _ := row.Props["mode"].(string)
	in := agentInvocation{mode: mode, threadID: threadID, resume: true, unanswered: r.sent, startsFrom: startsFrom}
	if mode == agentModeChat {
		err = ds.resumeChatTurn(ctx, ag, in, r, opened)
	} else {
		_, err = ds.runAgent(ctx, ag, in)
	}
	if errors.Is(err, errResumeStopped) || errors.Is(err, errResumeWaits) || errors.Is(err, errNothingUnanswered) {
		return
	}
	if r.sent && errors.Is(err, substrate.ErrThreadBusy) {
		// Another turn took the thread first, and replays the message.
		return
	}
	if err != nil && ctx.Err() == nil {
		// A canceled context is the shutdown taking the turn down, not a
		// continuation that failed on its own; the sweep is the retry.
		ds.svc.log.Warn("substrate: resume: the continuation failed", "thread", threadID, "agent", agentID, "error", err)
	}
}

// errResumeWaits ends a chat thread's continuation that found its person's
// chat-turn slots all taken: nothing was claimed, and the resume is kept
// with the slots (personSlots.takeOrWait) to be started again when one of
// that person's turns settles, or at the next sweep.
var errResumeWaits = errors.New("the person's chat turns are all running: the resume waits for one to settle")

// resumeChatTurn runs a chat thread's continuation as a detached chat turn
// (decision 0317): the loop publishes into a hub the thread's attaches and
// stops reach, runs on a context a stop and the shutdown cancel, and writes
// and settles past that cancellation, so a stop or a shutdown settles it
// `stopped` with what it streamed kept, as a sent turn's does. It holds one
// of its person's chat-turn slots from before the claim to after the settle;
// with none free it runs nothing and waits for one (errResumeWaits): from
// names the row the turn starts from, kept to start it again. opened, when
// set, is called once the turn holds the thread. It returns when the turn
// has settled: the caller is already a background task of the service.
//
// The wait is kept in memory, as the settle-time re-check is: a resume that
// waits when the process stops is one the sweep finds again only where the
// thread settled before its resolution landed.
func (ds *dataset) resumeChatTurn(ctx context.Context, ag *vocabulary.Agent, in agentInvocation, from deferredResume, opened func()) error {
	person, limit := ds.turnPerson(ctx), ds.svc.chatTurnsPerPerson
	if !ds.chatTurns.takeOrWait(person, limit, from) {
		ds.svc.log.Info("substrate: resume: the person's chat turns are all running, the resume waits for one to settle",
			"thread", in.threadID, "limit", limit)
		return errResumeWaits
	}
	defer func() { ds.resumeWaiting(ds.chatTurns.give(person)) }()
	hub := newTurnHub()
	in.hub = hub
	runCtx, l, err := ds.prepareAgentRun(ctx, ag, in)
	if err != nil {
		hub.finish(nil, err)
		return err
	}
	if opened != nil {
		opened()
	}
	// The run keeps the context's values (whom it is held to, the fence's
	// lease, the spend admission) and drops its cancellation.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(runCtx))
	defer cancel()
	hub.setRun(runCtx, cancel)
	defer context.AfterFunc(ds.svc.bg.ctx, cancel)()
	res, err := (*substrate.AgentResult)(nil), errors.New("the resumed chat turn ended without settling")
	defer func() { hub.finish(res, err) }()
	res, err = l.run(runCtx)
	return err
}

// resumeWaiting starts again the resumes that waited for a chat-turn slot,
// each through the admission every resume passes: a thread stopped
// meanwhile stays stopped, and one whose person's turns are still all
// running waits again.
func (ds *dataset) resumeWaiting(waited []deferredResume) {
	for _, w := range waited {
		if w.sent {
			// A person's message starts it: no resume bound applies.
			ds.startQueuedTurn(w.thread, w.resolution)
			continue
		}
		ds.resumeNotifiedThread(w.thread, "", w.resolution)
	}
}

// turnPerson is the person whose chat-turn slots a turn on ctx counts
// under: the identity a request's token acts for, and for work with no
// request behind it (a resumed turn) the person it is held to, which is the
// same key that person's own sends count under. Work no person stands
// behind counts under the repository.
func (ds *dataset) turnPerson(ctx context.Context) string {
	if id := substrate.IdentityFrom(ctx); id != "" {
		return id
	}
	switch behalf := substrate.OnBehalfOfFrom(ctx); {
	case behalf == substrate.ActorPerson:
		if first, _ := ds.firstPerson.Load().(string); first != "" {
			return first
		}
	case strings.HasPrefix(string(behalf), substrate.PersonActorPrefix):
		return strings.TrimPrefix(string(behalf), substrate.PersonActorPrefix)
	}
	return ds.scope.Repository
}

// SweepResolutions resumes settled threads whose newest resolution row
// postdates their settlement — the resumes a restart or a lost lease
// dropped. Both facts are already records, so the sweep is one query
// (unconsumedResolutions); it runs on its own background cadence
// (cmd/substrated resumeInterval). N unconsumed resolutions on one thread
// coalesce into the one continuation, which replays them all.
//
// It settles the threads a lost run left `running` first
// (settleLostThreads), so the sweep is the one recovery pass for both.
func (ds *dataset) SweepResolutions(ctx context.Context) (int, error) {
	if err := ds.settleLostThreads(ctx); err != nil {
		// A failed settle must not hold back the resumes below.
		ds.svc.log.Error("substrate: settling lost agent threads failed", "error", err)
	}
	// Once per open: a chat thread a restart left with a message no turn
	// answered gets the turn the message asked for (agentqueue.go).
	started := ds.startUnansweredTurns(ctx)
	threads, err := ds.unconsumedResolutions(ctx)
	if err != nil {
		return started, err
	}
	resumed := started
	for _, id := range threads {
		if !ds.admitResume(ctx, id, "") {
			continue
		}
		first, err := ds.firstUnconsumedResolution(ctx, id)
		if err != nil {
			ds.svc.log.Warn("substrate: resume: the thread's resolutions were not read", "thread", id, "error", err)
			continue
		}
		if first == "" {
			continue
		}
		ds.continueThread(ctx, id, first)
		resumed++
	}
	// The resumes that wait for a slot are tried again here too: the give
	// that would start them comes only when one of their person's turns
	// settles, and a slot can also free by a limit that was raised.
	ds.resumeWaiting(ds.chatTurns.drainWaiting())
	return resumed, nil
}

// firstUnconsumedResolution is the oldest live system row of a settled
// thread written after it settled: the record the sweep's continuation
// starts from, "" when there is none.
func (ds *dataset) firstUnconsumedResolution(ctx context.Context, threadID string) (string, error) {
	var finished string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT COALESCE(props->>'finishedAt', '') FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		typeThread, threadID).Scan(&finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	at, err := time.Parse(time.RFC3339Nano, finished)
	if err != nil {
		return "", fmt.Errorf("thread %s settled at %q: %w", threadID, finished, err)
	}
	return ds.firstResolution(ctx, threadID, at, nil)
}

// firstResolution is the oldest live system row of the thread written after
// `after` and not among consumed: the first record of the turn a
// continuation runs. "" when there is none.
func (ds *dataset) firstResolution(ctx context.Context, threadID string, after time.Time, consumed []string) (string, error) {
	b := &builder{}
	kind := b.arg(typeMessage)
	thread, err := threadMessagesWhere(ctx, ds.db, b, "e", threadID)
	if err != nil {
		return "", err
	}
	var id string
	err = ds.db.QueryRowContext(ctx, `
		SELECT e.id FROM records e
		WHERE e.kind = `+kind+` AND e.deleted_at IS NULL AND `+thread+`
		  AND e.props->>'role' = `+b.arg(msgRoleSystem)+` AND e.created_at > `+b.arg(after)+`
		  AND NOT (e.id = ANY(`+b.textArray(consumed)+`))
		ORDER BY e.created_at, e.id LIMIT 1`,
		b.args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// unconsumedResolutions lists, in id order, the settled threads holding a
// live system row written after they settled.
//
// It must stay linear in the rows. Under row level security no index reaches
// the thread pointer on a message's `props`, because `->` and `->>` are not
// leakproof (refs.go refsAtSiteSQL), so matching messages to threads on it
// leaves the planner a nested loop that reads every message once per thread,
// which it takes whenever it underestimates the two sides: on a repository of
// 23k threads and 118k messages that was a quarter of an hour per pass, and
// every concurrent index build (indices.go) waited out its snapshot. The
// threads are matched to their messages through refs instead, whose columns
// are plain text with exact statistics, and each message is read by primary
// key. The message is a scalar subquery because Postgres never flattens one:
// as a join, the created_at comparison becomes an index condition on
// records_created_at_idx and the planner drives each thread from every record
// written after it settled. The settle time is cast once per thread in the
// CTE, and a message whose created_at fails the comparison never has its
// properties read.
//
// While a re-derivation of the messages' `thread` refs rows is owed, only
// the rows' own properties say where they point: the kind's system rows are
// read once, grouped by thread, and each thread is looked up by primary key.
func (ds *dataset) unconsumedResolutions(ctx context.Context) ([]string, error) {
	pending, err := refsPending(ctx, ds.db, []string{typeMessage}, msgRelThread)
	if err != nil {
		return nil, err
	}
	query, args := `
		WITH settled AS MATERIALIZED (
		  SELECT id, (props->>'finishedAt')::timestamptz AS finished_at
		  FROM records
		  WHERE kind = $1 AND deleted_at IS NULL AND props->>'status' IN ($3, $4)
		)
		SELECT s.id FROM settled s
		WHERE EXISTS (
		  SELECT 1 FROM refs r
		  WHERE r.dst_kind = $1 AND r.dst = s.id AND r.src_kind = $2
		    AND r.property = $6 AND r.path = '' AND r.ord = 0
		    AND (SELECT true FROM records m
		         WHERE m.kind = $2 AND m.id = r.src AND m.deleted_at IS NULL
		           AND m.created_at > s.finished_at AND m.props->>'role' = $5)
		)
		ORDER BY s.id`,
		[]any{typeThread, typeMessage, threadOK, threadOverBudget, msgRoleSystem, msgRelThread}
	if pending {
		query, args = `
		WITH resolved AS MATERIALIZED (
		  SELECT substr(m.thread, length($6::text) + 1) AS id, max(m.created_at) AS newest
		  FROM (
		    SELECT `+referencePathSQL("props", msgRelThread)+` AS thread, created_at
		    FROM records
		    WHERE kind = $2 AND deleted_at IS NULL AND props->>'role' = $5
		  ) m
		  WHERE starts_with(m.thread, $6::text)
		  GROUP BY 1
		)
		SELECT t.id FROM resolved r
		JOIN records t ON t.kind = $1 AND t.id = r.id
		WHERE t.deleted_at IS NULL AND t.props->>'status' IN ($3, $4)
		  AND r.newest > (t.props->>'finishedAt')::timestamptz
		ORDER BY t.id`,
			[]any{typeThread, typeMessage, threadOK, threadOverBudget, msgRoleSystem, vocabulary.RecordPath(typeThread, "")}
	}
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var threads []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		threads = append(threads, id)
	}
	return threads, rows.Err()
}
