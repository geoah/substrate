package engine

import (
	"encoding/json"
)

// An exact repeat of a refused read.
//
// A `query` call refused for its arguments alone (parseQueryCall, which
// reads nothing stored) is refused the same way whatever the repository
// holds, and a model that read the refusal as a hiccup sent the identical
// call again. The loop keeps each such refusal for the run and answers an
// exact repeat from it, without reading or spending the read budget. A
// refusal of anything stored (a kind, metric or record that is not there, a
// kind outside the allowlist, a budget, a timeout, an index still being
// re-derived) is never kept: a write or the next call can lift it, and no
// position in the changelog says reliably whether one has. Nor is a time
// zone the server cannot load, which a zone file appearing lifts.

// repeatedCallNote rides the answer to a repeat.
const repeatedCallNote = "this exact call was refused earlier in this run for its arguments alone, so it was not run again: " +
	"change what the error names, or answer from what you have"

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

// priorRefusal answers the repeat of a call this run refused for its
// arguments, or "".
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

// noteRefusal keeps a call's refusal when it refused the arguments alone
// (toolInvalid).
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
