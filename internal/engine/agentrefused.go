package engine

import (
	"encoding/json"

	"github.com/geoah/substrate/internal/substrate"
)

// An exact repeat of a refused read.
//
// A `query` call refused for its input (toolInvalid) fails the same way until
// the input changes or something is written that the refusal depends on, and
// a model that read the refusal as a hiccup sent the identical call again.
// The loop keeps each such refusal for the run, with the changelog position
// read before the call ran, and answers an exact repeat from it without
// reading or spending the read budget while every entry past that position is
// a message row this run wrote. Any other write voids it, whoever made it: a
// refusal can depend on what is declared (the architect's apply lifts the
// refusal of a kind that did not exist) or on what is stored (a metric a
// background job or a sync creates lifts "names no metric").

// repeatedCallNote rides the answer to a repeat.
const repeatedCallNote = "this exact call was refused earlier in this run and nothing but this conversation has been written since, " +
	"so it was not run again: change what the error names, or answer from what you have"

// refusedRead is one kept refusal: its error, and where the changelog stood
// before the refused call ran.
type refusedRead struct {
	err string
	at  substrate.ChangelogHead
}

// refusalKey is the call's tool and arguments as one string. encoding/json
// writes a map's keys sorted, so the key does not depend on the order the
// model wrote them in.
func refusalKey(name string, args map[string]any) (string, bool) {
	b, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	return name + "\x00" + string(b), true
}

// priorRefusal answers the repeat of a call this run refused for its input,
// or "" when there is none or something but this run's messages was written
// since. c.readAt is where the changelog stands now.
func (l *agentLoop) priorRefusal(c *toolRun) string {
	key, ok := refusalKey(c.call.Name, c.args)
	if !ok {
		return ""
	}
	l.refusedMu.Lock()
	prior, found := l.refusedReads[key]
	quiet := found && l.onlyOwnRowsLocked(prior.at, c.readAt)
	l.refusedMu.Unlock()
	if !quiet {
		return ""
	}
	out, _ := json.Marshal(map[string]any{"error": prior.err, "invalid": true, "repeated": true, "note": repeatedCallNote})
	return string(out)
}

// onlyOwnRowsLocked reports whether every changelog entry after `from` up to
// `to` is a message row this run wrote. Committed seqs are gapless (each is
// the previous head plus one, under the changelog lock), so counting this
// run's seqs in the span is enough. A row whose write has not returned yet
// is not counted, which voids the refusal rather than keeping it.
func (l *agentLoop) onlyOwnRowsLocked(from, to substrate.ChangelogHead) bool {
	if from.Generation != to.Generation || to.Seq < from.Seq {
		return false
	}
	own := int64(0)
	for _, seq := range l.ownSeqs {
		if seq > from.Seq && seq <= to.Seq {
			own++
		}
	}
	return own == to.Seq-from.Seq
}

// noteRefusal keeps a call's refusal when it refused the input.
func (l *agentLoop) noteRefusal(c *toolRun, out string) {
	var r struct {
		Error   string `json:"error"`
		Invalid bool   `json:"invalid"`
	}
	if json.Unmarshal([]byte(out), &r) != nil || !r.Invalid {
		return
	}
	key, ok := refusalKey(c.call.Name, c.args)
	if !ok {
		return
	}
	l.refusedMu.Lock()
	defer l.refusedMu.Unlock()
	if l.refusedReads == nil {
		l.refusedReads = map[string]refusedRead{}
	}
	l.refusedReads[key] = refusedRead{err: r.Error, at: c.readAt}
}

// noteOwnRow counts the entry of a message row this run wrote as its own.
// Every other entry the write's transaction appended stays uncounted.
func (l *agentLoop) noteOwnRow(in substrate.PutInput, written []changeEntry) {
	l.refusedMu.Lock()
	defer l.refusedMu.Unlock()
	for _, e := range written {
		if e.kind == in.Kind && e.id == in.ID {
			l.ownSeqs = append(l.ownSeqs, e.seq)
		}
	}
}
