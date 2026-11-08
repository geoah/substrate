package engine

import "encoding/json"

// An exact repeat of a refused read.
//
// A `query` call refused for its input (toolInvalid) fails the same way until
// the input changes, and a model that read the refusal as a hiccup sent the
// identical call again. The loop keeps each such refusal for the run and
// answers an exact repeat from it without reading or spending the read
// budget. A call that may write forgets them all first, because what a read
// refuses can depend on what is declared: the architect's apply lifts the
// refusal of a kind that did not exist.

// repeatedCallNote rides the answer to a repeat.
const repeatedCallNote = "this exact call was refused earlier in this run, and the run has written nothing since, " +
	"so it was not run again: change what the error names, or answer from what you have"

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
// or "".
func (l *agentLoop) priorRefusal(c *toolRun) string {
	key, ok := refusalKey(c.call.Name, c.args)
	if !ok {
		return ""
	}
	l.refusedMu.Lock()
	prior, found := l.refusedReads[key]
	l.refusedMu.Unlock()
	if !found {
		return ""
	}
	out, _ := json.Marshal(map[string]any{"error": prior, "invalid": true, "repeated": true, "note": repeatedCallNote})
	return string(out)
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
		l.refusedReads = map[string]string{}
	}
	l.refusedReads[key] = r.Error
}

// forgetRefusals drops every kept refusal.
func (l *agentLoop) forgetRefusals() {
	l.refusedMu.Lock()
	l.refusedReads = nil
	l.refusedMu.Unlock()
}
