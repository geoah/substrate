package engine

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
)

// A chat thread's heading (decision record 0170). The thread opens with its
// first message as its subject, the title model replaces that once after the
// first turn, and an owner's rename is never overwritten: the generated title
// is written only while the subject's manager row still names the agent's
// own actor, which a rename by any other actor moves.

const propThreadSubject = "subject"

// threadSubjectRunes bounds a subject, provisional or generated.
const threadSubjectRunes = 80

// titleExcerptRunes bounds each side of the conversation the title model
// reads: the input cost of a title stays flat however long the first reply
// was.
const titleExcerptRunes = 2000

// The title call's output ceilings. A title is at most eight words, so 32
// tokens hold it, and a call that sends the effort "none" gets that. Every
// other call gets room to think first, an unset effort included: a model
// with a reasoning default thinks unasked, counts the thinking against the
// same ceiling, and would otherwise stop on it with no title.
const (
	titleMaxTokens          = 32
	titleReasoningMaxTokens = 512
)

// titleAllowance bounds the title call. It runs after the turn, on the
// service's background context, so nothing waits on it but shutdown.
const titleAllowance = 20 * time.Second

const titleSystemPrompt = "You name conversations. Reply with a title for the " +
	"conversation: at most eight words, no quotes, no trailing period, in the " +
	"conversation's own language. Reply with the title alone."

// provisionalSubject is the subject a chat thread opens with: the first
// non-blank line of its first message, whitespace collapsed, cut to
// threadSubjectRunes. Only a chat thread minted by this run takes one: a
// trigger's first message is a JSON envelope, which reads as no heading.
func (l *agentLoop) provisionalSubject() string {
	if l.in.mode != agentModeChat || l.in.threadID != "" {
		return ""
	}
	for line := range strings.SplitSeq(l.in.user, "\n") {
		if s := cleanSubject(line); s != "" {
			return s
		}
	}
	return ""
}

// cleanSubject collapses whitespace, strips surrounding quotes and a
// trailing period, and cuts to threadSubjectRunes.
func cleanSubject(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"'`“”‘’«»", r)
	})
	return cutRunes(s, threadSubjectRunes)
}

// cutRunes cuts s to at most n runes, never inside one.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}

// cleanTitle is the title model's answer as a subject: its first non-blank
// line, cleaned, without a trailing period. Empty when it said nothing.
func cleanTitle(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "Title:")
		if s := strings.TrimRight(cleanSubject(line), "."); s != "" {
			return cleanSubject(s)
		}
	}
	return ""
}

// titleAfterFirstTurn schedules the title call when this run opened a chat
// thread and its first turn settled ok. It runs after the done event, as a
// background task, so the turn's stream and result never wait on it and its
// failure never reaches them.
func (l *agentLoop) titleAfterFirstTurn(status, reply string) {
	if status != threadOK || l.in.mode != agentModeChat || l.in.threadID != "" || l.historyLen != 0 {
		return
	}
	l.ds.mu.RLock()
	skip := l.ds.skipThreadTitles
	l.ds.mu.RUnlock()
	if skip || strings.TrimSpace(reply) == "" {
		return
	}
	l.ds.spawn("title thread", func(ctx context.Context) {
		l.titleThread(ctx, reply)
	})
}

// titleThread asks the title model for the thread's subject and writes it,
// with the call's usage, unless the owner renamed the thread meanwhile. Any
// failure leaves the provisional subject: it is logged and never retried.
func (l *agentLoop) titleThread(ctx context.Context, reply string) {
	model, params := l.model, llm.Params{ReasoningEffort: l.params.ReasoningEffort}
	if l.provider.titleModel != "" && l.provider.titleModel != l.model {
		// The agent's effort was chosen for the agent's model; the title
		// model takes the row's own default, the word that row's models
		// accept.
		model = l.provider.titleModel
		defaults, err := mergeParams(l.provider.id, l.provider.defaults, nil)
		if err != nil {
			l.ds.svc.log.Warn("substrate: thread title skipped, the provider row's defaults do not merge",
				"thread", l.threadID, "error", err)
			return
		}
		params = llm.Params{ReasoningEffort: defaults.ReasoningEffort}
	}
	params.MaxTokens = titleReasoningMaxTokens
	if params.ReasoningEffort == "none" {
		params.MaxTokens = titleMaxTokens
	}
	// One user message carrying the exchange: a request that ends on an
	// assistant turn is a prefill on the anthropic wire, which the model
	// would continue instead of titling.
	var prompt strings.Builder
	prompt.WriteString("<conversation>\nuser: ")
	prompt.WriteString(cutRunes(l.in.user, titleExcerptRunes))
	prompt.WriteString("\nassistant: ")
	prompt.WriteString(cutRunes(reply, titleExcerptRunes))
	prompt.WriteString("\n</conversation>\n\nTitle this conversation.")

	// The call is a spend operation of its own on the thread, counted the
	// way a turn is (spend.go): a charge from its return, the row's lifetime
	// once the patch commits, and a lost charge when no patch does.
	base := 0.0
	if row, err := l.ds.loadRowDB(ctx, eref{Kind: typeThread, ID: l.threadID}); err == nil && row != nil {
		base, _ = anyFloat(row.Props["costUSD"])
	}
	run := l.ds.spend.open(l.ag.Identity(), l.threadID, base)
	defer l.ds.spend.finish(run)

	cctx, cancel := context.WithTimeout(ctx, titleAllowance)
	defer cancel()
	res, err := l.client.Complete(cctx, llm.Request{
		Model: model, System: titleSystemPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt.String()}},
		Params:   params,
	}, nil)
	if err != nil {
		l.ds.svc.log.Warn("substrate: thread title call failed; the thread keeps its first message as its subject",
			"thread", l.threadID, "model", model, "error", err)
		return
	}
	title := ""
	if len(res.ToolCalls) == 0 {
		title = cleanTitle(res.Content)
	}
	if title == "" {
		l.ds.svc.log.Warn("substrate: thread title call returned no title; the thread keeps its first message as its subject",
			"thread", l.threadID, "model", model, "stop", res.Stop)
	}
	var prompted, completed int
	if res.Usage != nil {
		prompted, completed = res.Usage.PromptTokens, res.Usage.CompletionTokens
	}
	price := l.provider.pricing[model]
	cost := (float64(prompted)*price.inPer1M + float64(completed)*price.outPer1M) / 1e6
	l.ds.spend.charge(run, cost)
	if title == "" && prompted+completed == 0 {
		return
	}
	lifetime, patched, err := l.writeTitle(ctx, title, prompted, completed, cost)
	if err != nil {
		l.ds.svc.log.Warn("substrate: thread title not written", "thread", l.threadID, "error", err)
		return
	}
	if patched {
		l.ds.spend.commit(run, lifetime)
	}
}

// writeTitle patches the generated subject and adds the call's usage onto
// the thread's tallies, so costUSD stays the thread's whole cost. The usage
// is billed whether or not the subject lands; the subject lands only while
// the agent still manages it. It reports the costUSD the patch wrote, and
// whether a patch was made at all: a deleted thread takes none.
func (l *agentLoop) writeTitle(ctx context.Context, title string, prompted, completed int, cost float64) (lifetime float64, patched bool, err error) {
	err = l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
		patched = false
		t.causedBy = l.in.causedBy
		// The order every put takes (claimThread says why).
		if err := t.lockRegistryDepShared(); err != nil {
			return err
		}
		ref := eref{Kind: typeThread, ID: l.threadID}
		if err := t.lockRecord(ref); err != nil {
			return err
		}
		row, err := t.loadRow(ref, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil || row.Kind != typeThread {
			return nil
		}
		num := func(key string) float64 {
			v, _ := anyFloat(row.Props[key])
			return v
		}
		props := map[string]any{
			"promptTokens":     int(num("promptTokens")) + prompted,
			"completionTokens": int(num("completionTokens")) + completed,
			"totalTokens":      int(num("totalTokens")) + prompted + completed,
			"costUSD":          num("costUSD") + cost,
		}
		if title != "" {
			managers, err := t.managersOf(ref)
			if err != nil {
				return err
			}
			if m, ok := managers[propThreadSubject]; ok && m.actor == string(l.actor) {
				props[propThreadSubject] = title
			}
		}
		if _, err := t.patch(ref, substrate.PatchInput{Properties: props}); err != nil {
			return err
		}
		patched, lifetime = true, num("costUSD")+cost
		return nil
	})
	return lifetime, patched && err == nil, err
}
