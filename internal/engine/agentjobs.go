package engine

// Background jobs (decision record 0200). A function declaring
// `execution: background`, or a `tools:` or `subagents:` entry setting
// `background: true`, does not hold the loop: the call answers at once with
// its handle, its tool row carries `job: {state: working}`, and the work runs
// on the service's background context, bounded by the function's own timeout
// or the sub-agent's own budgets and never by the run that started it.
//
// Its end is one transaction: the tool row's `job` moves to where it ended
// and a `system` row `{event: toolCompleted, …}` lands in the thread. Delivery
// is the decision loop's (agentdecision.go): a loop still running on the
// thread reads the row after its next batch of tool results, a settled
// thread resumes under the resume budget and `resume: never`, and the
// resolution sweep is the retry. A job lives in this process alone: a stop
// cancels it, and one a restart interrupted settles `failed` at the next open
// (settleInterruptedJobs).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A job's states, as the message kind's `job.state` spells them.
const (
	jobWorking   = "working"
	jobCompleted = "completed"
	jobFailed    = "failed"
	jobCanceled  = "canceled"
)

// eventToolCompleted is the `event` of the system row a job's end writes.
const eventToolCompleted = "toolCompleted"

// jobInterrupted is the error a job a restart ended reports.
const jobInterrupted = "interrupted: the server stopped during the job — call the tool again if you still need it"

// jobRetention is how long a finished job stays in memory for `wait`: past
// it, the toolCompleted row in the thread is where its result is read.
const jobRetention = 10 * time.Minute

// agentJob is one background call this process runs or ran lately.
type agentJob struct {
	thread   string
	callID   string
	tool     string
	callable string
	turn     int
	// row is the call's tool row, whose `job` the end moves.
	row       string
	startedAt time.Time
	// hub is the job's stop: a thread's stop cancels it through stop, the
	// shutdown through the background context, and a sub-agent the job runs
	// settles `stopped` on its own bounded writes, as under a chat turn.
	hub  *turnHub
	done chan struct{}

	// Set before done closes.
	state     string
	ok        bool
	output    string
	systemRow string
}

// agentJobs is the dataset's registry of jobs, by thread and call id.
type agentJobs struct {
	mu       sync.Mutex
	byThread map[string]map[string]*agentJob
}

func (r *agentJobs) add(j *agentJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byThread == nil {
		r.byThread = map[string]map[string]*agentJob{}
	}
	if r.byThread[j.thread] == nil {
		r.byThread[j.thread] = map[string]*agentJob{}
	}
	r.byThread[j.thread][j.callID] = j
}

func (r *agentJobs) remove(j *agentJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := r.byThread[j.thread]
	if jobs[j.callID] == j {
		delete(jobs, j.callID)
	}
	if len(jobs) == 0 {
		delete(r.byThread, j.thread)
	}
}

func (r *agentJobs) get(thread, callID string) *agentJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byThread[thread][callID]
}

// stop cancels every working job of a thread on behalf of actor and reports
// how many it reached.
func (r *agentJobs) stop(thread string, actor substrate.Actor) int {
	r.mu.Lock()
	var working []*agentJob
	for _, j := range r.byThread[thread] {
		select {
		case <-j.done:
		default:
			working = append(working, j)
		}
	}
	r.mu.Unlock()
	for _, j := range working {
		j.hub.stop(actor)
	}
	return len(working)
}

// pendingJob is a background call the current dispatch accepted: the loop
// stamps its tool row `working` and starts it once the row has landed.
type pendingJob struct {
	tool agentTool
	args map[string]any
	// run is the call as the job dispatches it: the accepting call's
	// identity and budget ordinal (so its idempotency key is the one a
	// foreground call would carry), and where the dispatch reports what it
	// wrote.
	run *toolRun
}

// jobDelivery is a finished job's system row, posted to the loop running on
// its thread for the next batch of tool results.
type jobDelivery struct {
	row     string
	content string
}

// background reports whether a call of the tool runs as a job.
func (t agentTool) runsInBackground(ag *vocabulary.Agent) bool {
	switch {
	case t.builtin != "":
		return false
	case t.sub != nil:
		return ag.BackgroundSubagents[t.sub.Identity()]
	case t.fn != nil:
		return t.background || t.fn.Background()
	}
	return false
}

// dispatchBackground accepts a background call: the checks a caller can
// answer at once run here, so a call that can never run fails as a tool
// error now rather than as a toolCompleted row later.
func (l *agentLoop) dispatchBackground(c *toolRun) (string, bool) {
	tool, args := c.tool, c.args
	switch {
	case tool.sub != nil:
		if depth := l.in.depth + 1; depth > l.ag.Budgets.Depth {
			return toolError(fmt.Sprintf("agent depth cap: %s would run at depth %d (cap %d)", tool.sub.Identity(), depth, l.ag.Budgets.Depth)), false
		}
		if input, _ := args["input"].(string); input == "" {
			return toolError("input is required — the task for the sub-agent"), false
		}
	case tool.fn.Input != nil:
		var input any
		if args != nil {
			input = args
		}
		if err := vocabulary.CheckValue(tool.fn.Input, input); err != nil {
			return toolError("input: " + err.Error()), false
		}
	}
	c.job = &pendingJob{tool: tool, args: args, run: &toolRun{call: c.call, turn: c.turn, seq: c.seq, tool: tool, args: args}}
	return toolJSON(map[string]any{"job": c.call.ID, "status": jobWorking}), true
}

// jobProps is a tool row's `job` value.
func jobProps(state string, started, finished time.Time) map[string]any {
	props := map[string]any{"state": state, "startedAt": started.Format(time.RFC3339Nano)}
	if !finished.IsZero() {
		props["finishedAt"] = finished.Format(time.RFC3339Nano)
	}
	return props
}

// jobLoop is the loop a job dispatches through: this loop's identity, grants
// and call path, with the ordinal of the call that started it (so its
// idempotency key is the one a foreground call would carry), and nothing a
// later turn of this loop touches. Its tally is its own: a sub-agent job's
// spend is opened on the root thread's agent and thread, so it counts against
// the root's caps, and its end rolls the cost onto the root thread row.
func (l *agentLoop) jobLoop(hub *turnHub, sub bool) *agentLoop {
	in := l.in
	in.complete, in.onThread = nil, nil
	in.emit, in.hub, in.stopHub = nil, nil, hub
	in.notAfter = time.Time{}
	in.tally = &agentTally{effects: map[string]int{}}
	if root := l.in.tally.spend; sub && root != nil {
		in.tally.spend = l.ds.spend.open(root.agent, root.thread, 0)
	}
	return &agentLoop{
		ds: l.ds, ag: l.ag, provider: l.provider, model: l.model, params: l.params,
		client: l.client, in: in, actor: l.actor, threadID: l.threadID,
		emit: l.emit, byName: l.byName, toolCalls: l.toolCalls, replyTurn: -1,
	}
}

// startJob runs a job the loop accepted, once its tool row (row) landed.
func (l *agentLoop) startJob(tc llm.ToolCall, turn int, row string, started time.Time, p *pendingJob) {
	j := &agentJob{
		thread: l.threadID, callID: tc.ID, tool: tc.Name, callable: p.tool.callable(),
		turn: turn, row: row, startedAt: started,
		hub: newTurnHub(), done: make(chan struct{}),
	}
	jl := l.jobLoop(j.hub, p.tool.sub != nil)
	ds := l.ds
	ds.jobs.add(j)
	if !ds.spawn("agent background job", func(bg context.Context) { ds.runJob(bg, j, jl, p) }) {
		// The shutdown began: the row stays `working`, and the next open
		// settles it as interrupted.
		ds.jobs.remove(j)
		ds.spend.finish(jl.in.tally.spend)
	}
}

// runJob is the job's goroutine: the call, then its end.
func (ds *dataset) runJob(bg context.Context, j *agentJob, jl *agentLoop, p *pendingJob) {
	jctx, cancel := context.WithCancel(bg)
	defer cancel()
	j.hub.setRun(jctx, cancel)
	jl.runCtx = jctx
	out, ok := ds.runJobCall(jctx, jl, p)
	state := jobCompleted
	if !ok {
		state = jobFailed
	}
	if actor := j.hub.stopper(); actor != "" {
		state, ok, out = jobCanceled, false, toolError(reasonStoppedBy(actor))
	} else if bg.Err() != nil {
		// The shutdown: nothing is written on a closing pool, and the next
		// open settles the row as interrupted.
		ds.spend.finish(jl.in.tally.spend)
		ds.jobs.remove(j)
		close(j.done)
		return
	}
	ds.endJob(bg, j, jl, p.run, state, ok, jl.capToolResult(out))
}

// runJobCall is the call itself, on the job's own lifecycle lease: the
// run that started it may have released its own. A panic is the job's
// failure, so the row still ends.
func (ds *dataset) runJobCall(ctx context.Context, jl *agentLoop, p *pendingJob) (out string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ds.svc.log.Error("substrate: a background job panicked", "thread", jl.threadID, "panic", fmt.Sprint(r))
			out, ok = toolError("the job failed inside the server"), false
		}
	}()
	pkg, ident := "", ""
	if p.tool.sub != nil {
		pkg, ident = p.tool.sub.Package, p.tool.sub.Identity()
	} else {
		pkg, ident = p.tool.fn.Package, p.tool.fn.Identity()
	}
	actx, release, err := ds.admitCallable(ctx, pkg, ident)
	if err != nil {
		return toolError(err.Error()), false
	}
	defer release()
	if p.tool.sub != nil {
		return jl.dispatchSubAgent(actx, p.run, p.tool.sub, p.args)
	}
	return jl.dispatchFunction(actx, p.run, p.tool.fn, p.args)
}

// endJob writes the job's end in one transaction: the tool row's `job`, what
// the call wrote, a sub-agent's spend rolled onto the root thread, and the
// toolCompleted row. Then it delivers: to the loop running on the thread, or
// as a resume of the settled thread.
func (ds *dataset) endJob(bg context.Context, j *agentJob, jl *agentLoop, run *toolRun, state string, ok bool, out string) {
	defer ds.jobs.retire(j)
	finished := nowUTC()
	tally := jl.in.tally
	ctx, cancel := context.WithTimeout(context.WithoutCancel(bg), agentSettleTimeout)
	defer cancel()
	env := map[string]any{
		"event": eventToolCompleted, "toolCallId": j.callID, "name": j.tool, "ok": ok, "output": out,
	}
	if j.callable != "" {
		env["callable"] = j.callable
	}
	var systemRow string
	var rootCost float64
	var rolled bool
	err := ds.inTx(ctx, jl.actor, false, func(t *txn) error {
		rolled = false
		t.causedBy = jl.in.causedBy
		props := map[string]any{"job": jobProps(state, j.startedAt, finished)}
		if len(run.changes) > 0 {
			props["changes"] = changeProps(run.changes)
		}
		if run.writes != nil {
			props["subagentWrites"] = run.writes
		}
		if _, err := t.patch(eref{Kind: typeMessage, ID: j.row}, substrate.PatchInput{Properties: props}); err != nil {
			return err
		}
		if tally.spend != nil && (tally.cost > 0 || tally.prompt > 0 || tally.completion > 0) {
			cost, err := t.rollJobSpend(tally.spend.thread, tally)
			if err != nil {
				return err
			}
			rootCost, rolled = cost, true
		}
		var err error
		systemRow, err = t.putThreadSystemRowID(j.thread, env, run.changes)
		return err
	})
	if tally.spend != nil {
		if err == nil && rolled {
			ds.spend.commit(tally.spend, rootCost, false)
		}
		ds.spend.finish(tally.spend)
	}
	if err != nil {
		// The row stays `working`; the next open settles it as interrupted.
		ds.svc.log.Warn("substrate: a background job's end was not written", "thread", j.thread, "call", j.callID, "error", err)
		close(j.done)
		return
	}
	j.state, j.ok, j.output, j.systemRow = state, ok, out, systemRow
	close(j.done)
	ds.deliverJob(j, toolJSON(env))
}

// rollJobSpend adds a sub-agent job's tally to the root thread's counters,
// under the row lock as a settle does, and answers the row's new cost.
func (t *txn) rollJobSpend(thread string, tally *agentTally) (float64, error) {
	if err := t.lockRegistryDepShared(); err != nil {
		return 0, err
	}
	ref := eref{Kind: typeThread, ID: thread}
	if err := t.lockRecord(ref); err != nil {
		return 0, err
	}
	row, err := t.loadRow(ref, true)
	if err != nil || row == nil || row.DeletedAt != nil {
		return 0, err
	}
	base := func(key string) int {
		v, _ := anyFloat(row.Props[key])
		return int(v)
	}
	cost, _ := anyFloat(row.Props["costUSD"])
	cost += tally.cost
	_, err = t.patch(ref, substrate.PatchInput{Properties: map[string]any{
		"promptTokens":     base("promptTokens") + tally.prompt,
		"completionTokens": base("completionTokens") + tally.completion,
		"totalTokens":      base("totalTokens") + tally.prompt + tally.completion,
		"cacheReadTokens":  base("cacheReadTokens") + tally.cacheRead,
		"cacheWriteTokens": base("cacheWriteTokens") + tally.cacheWrite,
		"costUSD":          cost,
	}})
	return cost, err
}

// retire forgets a finished job after jobRetention.
func (r *agentJobs) retire(j *agentJob) {
	time.AfterFunc(jobRetention, func() { r.remove(j) })
}

// deliverJob hands a finished job to its thread: the loop running on it reads
// the row after its next batch of tool results, and a thread with none
// resumes. A loop that settles before reading it finds the row in its
// settle-time re-check, and a resume lost after that is the sweep's.
func (ds *dataset) deliverJob(j *agentJob, content string) {
	if v, ok := ds.runningThreads.Load(j.thread); ok {
		if l, _ := v.(*agentLoop); l != nil {
			if !l.postJob(jobDelivery{row: j.systemRow, content: content}) {
				// The loop is past its settle-time re-check.
				ds.resumeNotifiedThread(j.thread, "")
				return
			}
			if l.hub != nil {
				okv := j.ok
				l.hub.publish(substrate.AgentEvent{
					Kind: substrate.AgentEventToolFinished, ID: j.callID, Tool: j.tool, Callable: j.callable,
					OK: &okv, Output: j.output, Turn: j.turn, Job: j.state,
				})
			}
			return
		}
	}
	ds.resumeNotifiedThread(j.thread, "")
}

// postJob queues a delivery for the loop's next batch, and reports false
// once the loop closed its inbox.
func (l *agentLoop) postJob(d jobDelivery) bool {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	if l.jobsClosed {
		return false
	}
	l.jobInbox = append(l.jobInbox, d)
	return true
}

// closeJobs closes the inbox and reports whether it held a row the run never
// handed the model. A job that ends after it is delivered as a resume.
func (l *agentLoop) closeJobs() bool {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	l.jobsClosed = true
	seen := map[string]bool{}
	for _, id := range l.consumed {
		seen[id] = true
	}
	left := false
	for _, d := range l.jobInbox {
		if !seen[d.row] {
			left = true
		}
	}
	l.jobInbox = nil
	return left
}

// consume marks a system row this run handed the model, so the settle-time
// re-check does not resume the thread for it again.
func (l *agentLoop) consume(row string) {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	l.consumed = append(l.consumed, row)
}

// consumedRows is every system row this run handed the model.
func (l *agentLoop) consumedRows() []string {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	return append([]string(nil), l.consumed...)
}

// deliverJobs appends the jobs that finished since the last batch to the
// run's messages, after its tool results, as the replay would hand their rows
// over: user content carrying the envelope.
func (l *agentLoop) deliverJobs(messages []llm.Message) []llm.Message {
	l.jobMu.Lock()
	inbox := l.jobInbox
	l.jobInbox = nil
	seen := map[string]bool{}
	for _, id := range l.consumed {
		seen[id] = true
	}
	for _, d := range inbox {
		if seen[d.row] {
			continue
		}
		seen[d.row] = true
		l.consumed = append(l.consumed, d.row)
		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: d.content})
	}
	l.jobMu.Unlock()
	return messages
}

// waitJobBound is how long a `wait` call may hold the loop: what it asked
// for, inside what is left of the run less the reserve the run's last turn
// keeps.
func waitJobBound(asked time.Duration, deadline, now time.Time) time.Duration {
	if deadline.IsZero() {
		return max(asked, 0)
	}
	left := deadline.Sub(now)
	bound := left - deadlineReserve(left)
	if asked > 0 {
		bound = min(bound, asked)
	}
	return max(bound, 0)
}

// dispatchWait is the `wait` host function: it holds the loop on the named
// jobs of this thread until each finished, the bound passed or the run ended,
// and answers what finished and what is still working. A job it answers is
// consumed: its toolCompleted row is not handed over again.
func (l *agentLoop) dispatchWait(ctx context.Context, args map[string]any) (string, bool) {
	raw, _ := args["jobs"].([]any)
	var ids []string
	for _, v := range raw {
		if s, _ := v.(string); s != "" {
			ids = append(ids, s)
		}
	}
	if len(ids) == 0 {
		return toolError("jobs is required — the job ids background calls answered with"), false
	}
	asked := time.Duration(0)
	if f, ok := anyFloat(args["timeoutSeconds"]); ok && f > 0 {
		asked = time.Duration(f * float64(time.Second))
	}
	deadline, _ := ctx.Deadline()
	timer := time.NewTimer(waitJobBound(asked, deadline, nowUTC()))
	defer timer.Stop()
	completed := []any{}
	working := []string{}
	var unknown []string
	expired := false
	for _, id := range ids {
		j := l.ds.jobs.get(l.threadID, id)
		if j == nil {
			unknown = append(unknown, id)
			continue
		}
		if !expired {
			select {
			case <-j.done:
			case <-timer.C:
				expired = true
			case <-ctx.Done():
				expired = true
			}
		}
		select {
		case <-j.done:
		default:
			working = append(working, id)
			continue
		}
		if j.state == "" {
			// Its end was not written: the next open reports it.
			working = append(working, id)
			continue
		}
		l.consume(j.systemRow)
		completed = append(completed, map[string]any{"job": id, "ok": j.ok, "state": j.state, "output": j.output})
	}
	out := map[string]any{"completed": completed, "working": working}
	if len(unknown) > 0 {
		out["unknown"] = unknown
		out["note"] = "an unknown job is not running in this thread now: a finished one's result is its toolCompleted message"
	}
	return toolJSON(out), true
}

// stopIdleThread is a stop of a thread with no running turn whose jobs it
// canceled: the thread settles `stopped`, so no job's end resumes it until
// its owner writes again.
func (ds *dataset) stopIdleThread(ctx context.Context, actor substrate.Actor, threadID string) (*substrate.AgentResult, error) {
	res := &substrate.AgentResult{Thread: threadID, Status: threadStopped, Reason: reasonStoppedBy(actor)}
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		if err := t.lockRegistryDepShared(); err != nil {
			return err
		}
		ref := eref{Kind: typeThread, ID: threadID}
		if err := t.lockRecord(ref); err != nil {
			return err
		}
		row, err := t.loadRow(ref, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return fmt.Errorf("%w: thread %s", substrate.ErrNotFound, threadID)
		}
		if status, _ := row.Props["status"].(string); status == threadRunning {
			// A continuation holds it: the jobs are stopped, the run is its own.
			res.Status, res.Reason = status, ""
			return nil
		}
		_, err = t.patch(ref, substrate.PatchInput{Properties: map[string]any{
			"status": threadStopped, "reason": res.Reason,
		}})
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// settleInterruptedJobs ends every tool row a stopped writer left `working`:
// at open no job of this repository runs, so each one's work was lost. The
// row moves to `failed` and its toolCompleted row says so, which the sweep
// then delivers like any other.
func (ds *dataset) settleInterruptedJobs(ctx context.Context) error {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, props FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->'job'->>'state' = $2
		ORDER BY id`, typeMessage, jobWorking)
	if err != nil {
		return err
	}
	type lost struct {
		id    string
		props map[string]any
	}
	var found []lost
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return err
		}
		props := map[string]any{}
		if err := json.Unmarshal(raw, &props); err != nil {
			_ = rows.Close()
			return err
		}
		found = append(found, lost{id: id, props: props})
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range found {
		thread := referenceID(f.props[msgRelThread])
		callID, _ := f.props["toolCallId"].(string)
		name, _ := f.props["name"].(string)
		started := nowUTC()
		if job, _ := f.props["job"].(map[string]any); job != nil {
			if s, _ := job["startedAt"].(string); s != "" {
				if at, perr := time.Parse(time.RFC3339Nano, s); perr == nil {
					started = at
				}
			}
		}
		out := toolError(jobInterrupted)
		env := map[string]any{"event": eventToolCompleted, "toolCallId": callID, "name": name, "ok": false, "output": out}
		if callable, _ := f.props["callable"].(string); callable != "" {
			env["callable"] = callable
		}
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			if _, err := t.patch(eref{Kind: typeMessage, ID: f.id}, substrate.PatchInput{Properties: map[string]any{
				"job": jobProps(jobFailed, started, t.now),
			}}); err != nil {
				return err
			}
			if thread == "" {
				return nil
			}
			_, err := t.putThreadSystemRowID(thread, env, nil)
			return err
		})
		if err != nil {
			return fmt.Errorf("settle interrupted job %s: %w", f.id, err)
		}
		ds.svc.log.Warn("substrate: a background job was interrupted by a stop, marked failed",
			"thread", thread, "call", strings.TrimSpace(callID))
	}
	return nil
}
