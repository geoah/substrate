package engine

// Replay compaction (docs/plans/compaction.md, phase 1). A continued thread
// replays its prose history on every continuation, and nothing else bounds
// it, so a long thread grows past its model's context window. The loop folds
// the older part of that history into one `summary` llm/message row, written
// under the agent's actor like every other row, and replay emits the summary
// in place of the rows its `covers` names. The covered rows are never touched:
// they stay the audit, and the console still shows them.
//
// The summary is its own role, never a `system` row with a marker, because
// three queries read `system` rows as resolutions and resume the thread on
// them (recheckResolutions, SweepResolutions, systemRowsInLastHour).
//
// A cut never separates an open obligation from what closes it: an
// unanswered tool call, or a pending interaction or change request the
// thread is waiting on. The rule is adk-go's (longestSelfContainedPrefix and
// skipBlockedHead), keyed on the record paths the transcript itself stamps,
// so the obligations are read from the rows alone.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// msgRoleSummary is the engine's fold of older turns: written by the loop,
// replayed in place of the rows it covers.
const msgRoleSummary = "summary"

// compactionAllowance is the wall clock one summarizer call may take, and
// what the lease is extended by before it starts. It runs on the invocation's
// own context rather than the loop's deadline, so a compaction at settle is
// never cut short by a run that spent its whole budget.
const compactionAllowance = 60 * time.Second

// The summarizer's input caps, adk-go's: one message never crowds out the
// rest, and the whole transcript stays inside any model this would run on.
const (
	compactionLineCap       = 2000
	compactionTranscriptCap = 200000
)

// compactionMinOutput is the floor under the summarizer's output ceiling
// (0.8 of the reserve), so a small reserve still leaves room for the format.
const compactionMinOutput = 256

// summaryFramingHead and summaryFramingTail wrap a summary row's content as
// the one user message replay emits for it. pi's framing: the model reads it
// as context it was handed, not as a turn the user wrote.
const (
	summaryFramingHead = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	summaryFramingTail = "\n</summary>"
)

// compactionSystemPrompt is the summarizer's system prompt, pi's verbatim.
const compactionSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

// compactionDurableFacts is adk-go's durable-facts instruction, shared by
// both prompts: identifiers and chosen options are what a rolling summary
// loses first, and what a user notices first when it does.
const compactionDurableFacts = `## Durable facts
- [Every concrete detail the user has stated: identifiers, names, dates, numbers, chosen options and the reasons given for them. Copy each one verbatim.]

The conversation may already start from a summary of a summary, so every durable fact present in it or in a previous summary MUST be carried forward unchanged, even if it is old and the recent turns are about something else. Never drop or generalize a durable fact to save space; drop narrative instead.`

// compactionPrompt is the first-time prompt: pi's checkpoint format plus the
// durable facts section.
const compactionPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

` + compactionDurableFacts + `

Keep each section concise. Preserve exact record ids, names, and error messages.`

// compactionUpdatePrompt folds new messages into a previous summary: pi's
// PRESERVE, ADD and UPDATE rules, with the durable facts carried forward.
const compactionUpdatePrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact record ids, names, and error messages
- PRESERVE every durable fact from the previous summary unchanged
- If something is no longer relevant, you may remove it, except a durable fact

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work, updated based on progress]

### Blocked
- [Current blockers, removed if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

` + compactionDurableFacts + `

Keep each section concise. Preserve exact record ids, names, and error messages.`

// errNothingToCompact is a plan that found no range: the history is too
// small, or every cut would split an open obligation.
var errNothingToCompact = errors.New("nothing to compact")

// threadRow is one stored llm/message row as replay and planning read it.
type threadRow struct {
	id    string
	props map[string]any
}

func (r threadRow) role() string {
	s, _ := r.props["role"].(string)
	return s
}

func (r threadRow) content() string {
	s, _ := r.props["content"].(string)
	return s
}

// summaryMessage is the one user message replay emits for a summary row.
func summaryMessage(content string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: summaryFramingHead + content + summaryFramingTail}
}

// summarySpan is one summary row resolved against the rows it sits among:
// its own position, and the positions of the first and last rows it covers.
type summarySpan struct {
	at, from, through int
}

// summarySpans resolves every summary row's `covers`. A summary whose ids are
// not both among the rows (a covered row was deleted), or whose range does
// not sit before it, cannot be placed, and is reported instead of replayed.
func summarySpans(rows []threadRow) (spans []summarySpan, unresolved []string) {
	index := make(map[string]int, len(rows))
	for i, r := range rows {
		index[r.id] = i
	}
	for i, r := range rows {
		if r.role() != msgRoleSummary {
			continue
		}
		covers, _ := r.props["covers"].(map[string]any)
		fromID, _ := covers["from"].(string)
		throughID, _ := covers["through"].(string)
		from, okFrom := index[fromID]
		through, okThrough := index[throughID]
		if !okFrom || !okThrough || from > through || through >= i {
			unresolved = append(unresolved, r.id)
			continue
		}
		spans = append(spans, summarySpan{at: i, from: from, through: through})
	}
	return spans, unresolved
}

// replayView builds the model-facing history from a thread's rows: user rows
// kept, assistant rows kept only as prose (a turn that called tools is
// dropped), tool rows dropped, system rows replayed as user content, and each
// summary emitted where its range starts, the range itself skipped. Where
// several summaries start at one row the latest wins: a later compaction
// folds the earlier summary into its own. Rows after a range, a resolution
// that landed while the summary was being written among them, replay as
// usual.
//
// origin[k] is the row the k-th message came from; for a summary message it
// is the summary row itself.
func replayView(rows []threadRow, log *slog.Logger) (msgs []llm.Message, origin []int, maxTurn int) {
	spans, unresolved := summarySpans(rows)
	if log != nil {
		for _, id := range unresolved {
			log.Warn("substrate: agent thread summary covers rows that are not in the thread, replaying them in full", "summary", id)
		}
	}
	startAt := map[int]summarySpan{}
	for _, s := range spans {
		if cur, ok := startAt[s.from]; !ok || s.at > cur.at {
			startAt[s.from] = s
		}
	}
	maxTurn = -1
	for _, r := range rows {
		if t, ok := anyFloat(r.props["turn"]); ok && int(t) > maxTurn {
			maxTurn = int(t)
		}
	}
	for i := 0; i < len(rows); {
		if s, ok := startAt[i]; ok {
			msgs = append(msgs, summaryMessage(rows[s.at].content()))
			origin = append(origin, s.at)
			i = s.through + 1
			continue
		}
		r := rows[i]
		content := r.content()
		switch r.role() {
		case "user":
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: content})
			origin = append(origin, i)
		case "assistant":
			if content != "" && r.props["toolCalls"] == nil {
				msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: content})
				origin = append(origin, i)
			}
		case msgRoleSystem:
			// The substrate's own turn (a proposal decision) replays as USER
			// content: no wire admits a mid-thread system role on the messages
			// array (the system slot is the agent's prompt), and the content
			// is a self-describing JSON envelope either way.
			if content != "" {
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: content})
				origin = append(origin, i)
			}
		}
		i++
	}
	return msgs, origin, maxTurn
}

// messageTokens estimates one message at four characters per token, plus a
// few for the wire's per-message framing.
func messageTokens(m llm.Message) int {
	return (len(m.Content)+3)/4 + 4
}

func messagesTokens(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += messageTokens(m)
	}
	return n
}

// estimateTokens is the estimated size of a request carrying msgs: the
// messages and the system prompt.
func (l *agentLoop) estimateTokens(msgs []llm.Message) int {
	return messagesTokens(msgs) + (len(l.system())+3)/4
}

// --- obligations -----------------------------------------------------------------

// The two record kinds whose pending state a thread waits on: a change
// request's decision and an interaction's answer both report back as a
// system row naming the record.
var obligationKinds = map[string]bool{
	vocabulary.KindLLMInteraction:     true,
	vocabulary.KindRecordPatchRequest: true,
}

// reHeldForReview finds the request path in a gated write's tool result
// (policy.go heldForReview): a fallback for a result whose `changes` lost it.
var reHeldForReview = regexp.MustCompile(`held for review as ([A-Za-z0-9][A-Za-z0-9._~:@/-]*)`)

// rowObligations is what one row opens and closes. An assistant row opens one
// obligation per tool call and its tool rows close them; a tool row whose
// dispatch created an interaction or a change request opens one keyed by the
// record's path; a system row naming such a record closes it.
func rowObligations(r threadRow, at int) (opens, closes []string) {
	switch r.role() {
	case "assistant":
		for n, c := range objectRows(r.props["toolCalls"]) {
			id, _ := c["id"].(string)
			if id == "" {
				// Nothing can answer a call without an id, so it stays open.
				id = fmt.Sprintf("#%d/%d", at, n)
			}
			opens = append(opens, "call:"+id)
		}
	case "tool":
		if id, _ := r.props["toolCallId"].(string); id != "" {
			closes = append(closes, "call:"+id)
		}
		for _, ch := range objectRows(r.props["changes"]) {
			kind, _ := ch["kind"].(string)
			id, _ := ch["id"].(string)
			if obligationKinds[kind] && id != "" {
				opens = append(opens, "rec:"+vocabulary.RecordPath(kind, id))
			}
		}
		if m := reHeldForReview.FindStringSubmatch(r.content()); m != nil {
			opens = append(opens, "rec:"+m[1])
		}
	case msgRoleSystem:
		var env map[string]any
		if json.Unmarshal([]byte(r.content()), &env) != nil {
			return nil, nil
		}
		// The resolution envelopes (agentdecision.go): an interaction names
		// itself under `interaction`, a generic resolution under `record`, and
		// a decided or conflicted request under `request`.
		for _, key := range []string{"interaction", "record", "request"} {
			if path, _ := env[key].(string); path != "" {
				closes = append(closes, "rec:"+path)
			}
		}
	}
	return opens, closes
}

// obligationWalk is one pass over rows[base:].
type obligationWalk struct {
	// balanced[i] says nothing opened in the walk is still open after row i.
	balanced []bool
	// open maps each obligation still open at the end to the row that opened
	// it.
	open map[string]int
	// firstOpen maps every key the walk saw opened to the first row that
	// opened it, closed later or not.
	firstOpen map[string]int
	// orphans are closes of keys the walk never opened, by row.
	orphans []orphanClose
}

type orphanClose struct {
	at  int
	key string
}

func walkObligations(rows []threadRow, base int) obligationWalk {
	w := obligationWalk{
		balanced:  make([]bool, len(rows)),
		open:      map[string]int{},
		firstOpen: map[string]int{},
	}
	for i := base; i < len(rows); i++ {
		opens, closes := rowObligations(rows[i], i)
		for _, k := range closes {
			if _, ok := w.open[k]; ok {
				delete(w.open, k)
			} else {
				w.orphans = append(w.orphans, orphanClose{at: i, key: k})
			}
		}
		for _, k := range opens {
			if _, ok := w.open[k]; !ok {
				w.open[k] = i
			}
			if _, ok := w.firstOpen[k]; !ok {
				w.firstOpen[k] = i
			}
		}
		w.balanced[i] = len(w.open) == 0
	}
	return w
}

// --- planning --------------------------------------------------------------------

// compactionPlan is one range to summarize: rows[fromIdx..throughIdx],
// inclusive, and the replay-view messages inside it.
type compactionPlan struct {
	fromIdx, throughIdx int
	// previous is the content of the latest summary inside the range, which
	// the new summary folds in; "" when the range holds none.
	previous string
	// covered are the range's replay-view messages in order, a folded
	// summary's own message excluded (it travels as previous).
	covered []llm.Message
	// speakers labels each covered message for the transcript: the role of
	// the row it came from.
	speakers []string
}

// planCompaction chooses the range a compaction summarizes. The tail kept
// verbatim reaches back at least to where keepTokens of estimated history is
// reached, and begins at a user row, so it starts a run and never separates a
// question from its answer. The cut must also leave no obligation open: the
// range is a prefix of the history that is complete on its own.
//
// When an obligation opened early never closes (a pending ask in the oldest
// run), no prefix is complete, so the head stays raw instead: the range
// starts at the first run after the row that opened it (or at a summary that
// already starts after that row, folding it in), is held to the same
// rule over the rows from there, and may not reach a row that closes an
// obligation the skipped head opened. Nothing satisfying all of it means no
// compaction this time.
func planCompaction(rows []threadRow, view []llm.Message, origin []int, keepTokens int) (*compactionPlan, bool) {
	n := len(view)
	if n == 0 || len(origin) != n {
		return nil, false
	}
	keepFrom, sum := -1, 0
	for k := n - 1; k >= 0; k-- {
		sum += messageTokens(view[k])
		if sum >= keepTokens {
			keepFrom = k
			break
		}
	}
	if keepFrom < 0 {
		return nil, false
	}
	spans, _ := summarySpans(rows)
	spanAt := make(map[int]summarySpan, len(spans))
	for _, s := range spans {
		spanAt[s.at] = s
	}
	realUser := func(k int) bool {
		return view[k].Role == llm.RoleUser && rows[origin[k]].role() == "user"
	}

	full := walkObligations(rows, 0)
	if plan, ok := planFrom(rows, view, origin, spanAt, realUser, 0, full.balanced, len(rows), keepFrom); ok {
		return plan, true
	}
	if len(full.open) == 0 {
		return nil, false
	}
	// The blocked head: skip past each still-open obligation in the order it
	// opened, and try the history after it.
	openers := make([]int, 0, len(full.open))
	for _, at := range full.open {
		openers = append(openers, at)
	}
	sort.Ints(openers)
	tried := map[int]bool{}
	for _, opener := range openers {
		// The range starts at the first run after the opener, or at an
		// earlier summary that already starts after it: that summary folds
		// into the new one, so a head that stays blocked does not stack one
		// more summary message per compaction.
		s, base := -1, 0
		for k := range view {
			if origin[k] <= opener {
				continue
			}
			if sp, ok := spanAt[origin[k]]; ok && sp.from > opener {
				s, base = k, sp.from
				break
			}
			if realUser(k) {
				s, base = k, origin[k]
				break
			}
		}
		if s < 0 || tried[s] {
			continue
		}
		tried[s] = true
		w := walkObligations(rows, base)
		// A range may not reach a row that closes what the skipped head
		// opened: the model would keep a question whose answer went into the
		// summary.
		limit := len(rows)
		for _, oc := range w.orphans {
			if at, ok := full.firstOpen[oc.key]; ok && at < base && oc.at < limit {
				limit = oc.at
			}
		}
		if plan, ok := planFrom(rows, view, origin, spanAt, realUser, s, w.balanced, limit, keepFrom); ok {
			return plan, true
		}
	}
	return nil, false
}

// planFrom picks the cut for a range starting at view[start]: the first
// candidate at or after keepFrom, else the last one before it. A candidate
// is a real user row after start whose preceding rows leave nothing open,
// and whose row index is at most limit.
func planFrom(rows []threadRow, view []llm.Message, origin []int, spanAt map[int]summarySpan,
	realUser func(int) bool, start int, balanced []bool, limit, keepFrom int,
) (*compactionPlan, bool) {
	candidate := func(k int) bool {
		if k <= start || !realUser(k) || origin[k] > limit {
			return false
		}
		return origin[k] == 0 || balanced[origin[k]-1]
	}
	cut := -1
	for k := max(keepFrom, start+1); k < len(view); k++ {
		if candidate(k) {
			cut = k
			break
		}
	}
	if cut < 0 {
		for k := keepFrom - 1; k > start; k-- {
			if candidate(k) {
				cut = k
				break
			}
		}
	}
	if cut < 0 {
		return nil, false
	}
	plan := &compactionPlan{fromIdx: origin[start], throughIdx: origin[cut] - 1}
	latest := -1
	for k := start; k < cut; k++ {
		if s, ok := spanAt[origin[k]]; ok {
			// A summary inside the range folds into the new one: the new
			// range starts where the old one did, so replay never emits the
			// old summary and then its old tail again.
			plan.fromIdx = min(plan.fromIdx, s.from)
			if s.at > latest {
				latest = s.at
				plan.previous = rows[s.at].content()
			}
			continue
		}
		plan.covered = append(plan.covered, view[k])
		plan.speakers = append(plan.speakers, rows[origin[k]].role())
	}
	if len(plan.covered) == 0 {
		return nil, false
	}
	// A summary row ending the range replays as nothing, so the range stops
	// at the last row it actually replaces.
	for plan.throughIdx > plan.fromIdx && rows[plan.throughIdx].role() == msgRoleSummary {
		plan.throughIdx--
	}
	return plan, true
}

// --- the summarizer call ---------------------------------------------------------

// compactionTranscript renders the covered messages as labeled text, one
// entry per message, so the summarizer reads a conversation instead of
// continuing one. A continuation line is indented, so no message can start a
// line that reads as another speaker's label.
func compactionTranscript(msgs []llm.Message, speakers []string) string {
	lines := make([]string, 0, len(msgs))
	for i, m := range msgs {
		label := "[User]: "
		switch {
		case i < len(speakers) && speakers[i] == msgRoleSystem:
			label = "[Substrate]: "
		case m.Role == llm.RoleAssistant:
			label = "[Assistant]: "
		}
		content := m.Content
		if n := utf8.RuneCountInString(content); n > compactionLineCap {
			r := []rune(content)
			content = string(r[:compactionLineCap]) + fmt.Sprintf(" [... %d more characters truncated]", n-compactionLineCap)
		}
		lines = append(lines, label+strings.ReplaceAll(content, "\n", "\n  "))
	}
	// Oldest lines go first: the newest ones sit next to the kept tail, and
	// a previous summary already carries what the oldest said.
	total := 0
	for _, line := range lines {
		total += utf8.RuneCountInString(line) + 1
	}
	for len(lines) > 1 && total > compactionTranscriptCap {
		total -= utf8.RuneCountInString(lines[0]) + 1
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// compactionWindow reads the thread's compaction settings: the model's
// context window from the provider row, and the agent's reserve and keep.
// A model without a window, a disabled agent, or a reserve that leaves no
// room under the window, never compacts.
func (l *agentLoop) compactionWindow() (window, reserve, keep int, ok bool) {
	c := l.ag.Compaction
	window = l.provider.pricing[l.model].contextWindow
	if !c.Enabled || window <= 0 || c.ReserveTokens >= window {
		return 0, 0, 0, false
	}
	return window, c.ReserveTokens, c.KeepRecentTokens, true
}

// compact summarizes one planned range and writes the summary row. It writes
// nothing when the summarizer fails, stops on its output ceiling, calls a
// tool or returns nothing: a partial summary replayed in place of real rows
// would lose them for good. The call's usage is charged either way, since the
// provider billed it.
func (l *agentLoop) compact(ctx context.Context, plan *compactionPlan, tokensBefore int) error {
	rows := l.rows
	fromID, throughID := rows[plan.fromIdx].id, rows[plan.throughIdx].id
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].role() != msgRoleSummary {
			continue
		}
		// The latest summary already ends here: a retried compaction of the
		// same range is a no-op.
		if covers, _ := rows[i].props["covers"].(map[string]any); covers["through"] == throughID {
			return nil
		}
		break
	}
	var prompt strings.Builder
	prompt.WriteString("<conversation>\n")
	prompt.WriteString(compactionTranscript(plan.covered, plan.speakers))
	prompt.WriteString("\n</conversation>\n\n")
	instructions := compactionPrompt
	if plan.previous != "" {
		prompt.WriteString("<previous-summary>\n" + plan.previous + "\n</previous-summary>\n\n")
		instructions = compactionUpdatePrompt
	}
	prompt.WriteString(instructions)

	params := l.params
	params.MaxTokens = max(compactionMinOutput, l.ag.Compaction.ReserveTokens*4/5)
	cctx, cancel := context.WithTimeout(ctx, compactionAllowance)
	defer cancel()
	res, err := l.client.Complete(cctx, llm.Request{
		Model: l.model, System: compactionSystemPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt.String()}},
		Params:   params,
	}, nil)
	if err != nil {
		return fmt.Errorf("summarizer: %w", err)
	}
	l.charge(res.Usage)
	switch {
	case res.Stop == llm.StopLength:
		return errors.New("summarizer stopped on its output ceiling, so the summary is incomplete")
	case len(res.ToolCalls) > 0:
		return errors.New("summarizer called a tool instead of writing the summary")
	case strings.TrimSpace(res.Content) == "":
		return errors.New("summarizer returned no summary")
	}
	props := map[string]any{
		"role": msgRoleSummary, "content": res.Content,
		"covers":       map[string]any{"from": fromID, "through": throughID},
		"tokensBefore": tokensBefore, "model": l.model,
		"promptTokens": 0, "completionTokens": 0,
	}
	if res.Usage != nil {
		props["promptTokens"], props["completionTokens"] = res.Usage.PromptTokens, res.Usage.CompletionTokens
	}
	id, err := l.putMessageID(ctx, l.actor, props)
	if err != nil {
		return err
	}
	l.rows = append(l.rows, threadRow{id: id, props: props})
	covered := plan.throughIdx - plan.fromIdx + 1
	l.event(substrate.AgentEvent{
		Kind: substrate.AgentEventCompacted, Thread: l.threadID,
		TokensBefore: tokensBefore, Covered: covered,
	})
	l.ds.svc.log.Info("substrate: agent thread compacted", "thread", l.threadID,
		"covered", covered, "tokensBefore", tokensBefore)
	return nil
}

// extendLease pushes the thread's lease d past now, plus the slack, so a
// compaction at the end of a run that spent its whole deadline is never taken
// over as a crashed turn. It never shortens a lease.
func (l *agentLoop) extendLease(ctx context.Context, d time.Duration) error {
	until := nowUTC().Add(d + agentLeaseSlack)
	if !until.After(l.leaseAt) {
		return nil
	}
	if err := l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
		t.causedBy = l.in.causedBy
		_, err := t.patch(eref{Kind: typeThread, ID: l.threadID}, substrate.PatchInput{Properties: map[string]any{
			"leaseUntil": until.Format(time.RFC3339Nano),
		}})
		return err
	}); err != nil {
		return err
	}
	l.leaseAt = until
	return nil
}

// compactHistory compacts the run's replayed history, the rows this run
// loaded at open, and splices the new view in front of the run's own
// messages: the new user turn and any tool exchanges since.
func (l *agentLoop) compactHistory(ctx context.Context, messages []llm.Message, keep, tokensBefore int) ([]llm.Message, error) {
	plan, ok := planCompaction(l.rows, messages[:l.historyLen], l.origin, keep)
	if !ok {
		return nil, errNothingToCompact
	}
	if err := l.extendLease(ctx, compactionAllowance); err != nil {
		return nil, err
	}
	if err := l.compact(ctx, plan, tokensBefore); err != nil {
		return nil, err
	}
	view, origin, _ := replayView(l.rows, l.ds.svc.log)
	next := append(view, messages[l.historyLen:]...)
	l.historyLen, l.origin = len(view), origin
	return next, nil
}

// compactAtOpen is the open-time check: a continuation whose replayed
// history already crowds the window (a compaction at the last settle failed,
// or the window shrank) compacts before its first completion. A failure
// leaves the history as it was.
func (l *agentLoop) compactAtOpen(ctx context.Context, messages []llm.Message) []llm.Message {
	if l.in.threadID == "" {
		return messages
	}
	window, reserve, keep, ok := l.compactionWindow()
	if !ok {
		return messages
	}
	est := l.estimateTokens(messages)
	if est <= window-reserve {
		return messages
	}
	next, err := l.compactHistory(ctx, messages, keep, est)
	if !errors.Is(err, errNothingToCompact) {
		// The run's deadline starts after this check, but the lease was taken
		// at the claim: move it past the whole run again, or a summarizer call
		// longer than the slack leaves the run's tail under an expired lease,
		// which settleLostThreads reads as a crashed turn.
		if lerr := l.extendLease(ctx, time.Duration(l.ag.Budgets.DeadlineSeconds)*time.Second); lerr != nil {
			l.ds.svc.log.Warn("substrate: agent thread lease refresh after compaction failed", "thread", l.threadID, "error", lerr)
		}
	}
	if err != nil {
		l.ds.svc.log.Warn("substrate: agent thread compaction at open failed, continuing with the whole history",
			"thread", l.threadID, "error", err)
		return messages
	}
	return next
}

// compactOnOverflow is the provider-overflow path: the completion refused
// the context, so the history is compacted and the loop retries once.
func (l *agentLoop) compactOnOverflow(ctx context.Context, messages []llm.Message) ([]llm.Message, error) {
	_, _, keep, ok := l.compactionWindow()
	if !ok {
		return nil, errors.New("compaction is off for this agent or model")
	}
	return l.compactHistory(ctx, messages, keep, l.estimateTokens(messages))
}

// compactAtSettle is the primary check point: at the end of a chat or
// resumed run, if the next continuation would replay more than the window
// leaves free, compact now, so the user's next turn does not wait on it. The
// size is the first completion's measured context (system, replayed history
// and the new user turn) plus the final reply. A failure is logged and the
// thread still settles; the open-time check is the retry.
func (l *agentLoop) compactAtSettle(ctx context.Context, reply string) {
	if l.in.mode != agentModeChat && l.in.threadID == "" {
		return
	}
	window, reserve, keep, ok := l.compactionWindow()
	if !ok || l.firstContext+messageTokens(llm.Message{Content: reply}) <= window-reserve {
		return
	}
	err := func() error {
		if err := l.extendLease(ctx, compactionAllowance); err != nil {
			return err
		}
		rows, err := l.loadThreadRows(ctx)
		if err != nil {
			return err
		}
		l.rows = rows
		view, origin, _ := replayView(rows, l.ds.svc.log)
		plan, ok := planCompaction(rows, view, origin, keep)
		if !ok {
			return errNothingToCompact
		}
		return l.compact(ctx, plan, l.firstContext)
	}()
	switch {
	case errors.Is(err, errNothingToCompact):
		l.ds.svc.log.Debug("substrate: agent thread is near its window but has nothing to compact", "thread", l.threadID)
	case err != nil:
		l.ds.svc.log.Warn("substrate: agent thread compaction at settle failed", "thread", l.threadID, "error", err)
	}
}
