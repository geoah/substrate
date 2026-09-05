package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The 500 block: functions, triggers and agents, over the repository the
// stories left behind. Everything this file declares lives under its OWN
// authority, so a case here can never move a story fixture; the one thing it
// borrows is the `storyllm` provider row, which already points at the run's
// scripted stub.
const (
	xfAuthority = "extras.e2e.example"
	xfPackage   = "extras"
	xfPkg       = xfAuthority + "/" + xfPackage

	xfParkKind       = xfPkg + "/triggerbait"
	xfParkCollection = "/api/v1/" + xfParkKind

	xfFunctionPath = "/api/v1/substrate.reamde.dev/core/function/"
	xfAgentPath    = "/api/v1/substrate.reamde.dev/core/agent/"

	xfThreadCollection  = "/api/v1/substrate.reamde.dev/core/llmthread"
	xfMessageCollection = "/api/v1/substrate.reamde.dev/core/llmmessage"

	// The host functions answer their FULL identity and never a bare name:
	// ResolveFunction keeps a built-in out of the bare-name candidates, so
	// these travel percent-encoded through the one {name} path segment.
	xfHostQuery   = "substrate.reamde.dev/core/query"
	xfHostPropose = "substrate.reamde.dev/core/propose"
	xfHostMutate  = "substrate.reamde.dev/core/mutate"

	// The trigger TRG-03 poisons and TRG-04 replays, and the record it fires on.
	xfParkTrigger = "x-park"
	xfBaitID      = "xf-bait"

	// The greeting AGN-02 reassembles out of the stream's deltas.
	xfGreeting = "Hello. This reply arrived in pieces."
)

func init() {
	registerCase(500, "FN-01", "A python function answers a direct call",
		"A function declared through vocabulary/apply runs on `…/function/{name}/call`: the bare name "+
			"resolves because it is unique, the full authority/name answers the same call percent-encoded, "+
			"and the body's return lands as the declared output with no effects.",
		xfCaseFunctionCall)
	registerCase(510, "FN-02", "A raising function is a failure the caller sees",
		"A body that raises answers 500 `function_failed` carrying the exception, never a 2xx and never "+
			"the 422 an input-schema violation would be; the direct call path writes no run record.",
		xfCaseFunctionFault)
	registerCase(520, "FN-03", "The host functions on the direct call API",
		"`query` reads a record through the call API under the token's own reach, while `propose` and "+
			"`mutate` are refused 403 because a direct call has no calling agent to bound their writes; "+
			"a bare host name is a 404 naming the full identity.",
		xfCaseHostFunctions)
	registerCase(530, "TRG-03", "A failing delivery parks, and a retry drains it",
		"A trigger whose function raises retries and parks the delivery with its error and attempt count; "+
			"the record is untouched. Fixing the function and retrying the parked failure re-runs it against "+
			"current state: the write lands and the parked list drains to empty.",
		xfCaseTriggerPark)
	registerCase(540, "TRG-04", "Replay resets the cursor and re-delivers",
		"A replay from 0 hands the trigger its whole backlog again: the delivery runs a second time and "+
			"writes a new run row, while the idempotent put leaves the record at the version it already had.",
		xfCaseTriggerReplay)
	registerCase(550, "AGN-02", "Agent chat streams ndjson",
		"`…/agent/{name}/chat` streams one AgentEvent per line: the thread id first, the assistant's turn "+
			"as deltas that reassemble to the whole reply, and one done carrying the settled result last. "+
			"The transcript persists as llmthread and llmmessage records the client can re-read.",
		xfCaseAgentChat)
	registerCase(560, "AGN-04", "Without a provider row the agent refuses at dispatch",
		"An agent naming an llmprovider row nobody wrote is admitted at declaration and refused at the "+
			"call, 422, naming the row it wanted.",
		xfCaseAgentNoProvider)
	registerCase(570, "AGN-05", "Cost lands from the model's usage block",
		"A provider row carrying a price for the model turns the stub's usage tally into the run's cost: "+
			"the result and the thread record both carry 15 tokens and the exact USD that prices them.",
		xfCaseAgentCost)
}

// --- the declarations ---------------------------------------------------

// xfWordCountSource is the smallest useful body: declared arguments arrive
// under `args`, the return's `output` is checked against `returns`.
const xfWordCountSource = `
def main(input, host):
    args = input.get("args") or {}
    return {"output": {"words": len((args.get("text") or "").split())}}
`

const xfAlwaysFailsSource = `
def main(input, host):
    raise RuntimeError("deliberate")
`

const xfImportBombSource = `
def main(input, host):
    raise RuntimeError("the import bomb went off")
`

// xfStampSource is the repaired body TRG-03 retries and TRG-04 replays. The
// put is idempotent on purpose: a second delivery of the same change must
// leave the record at the version the first one left it at.
const xfStampSource = `
def main(input, host):
    rec = (input.get("envelope") or {}).get("record") or {}
    rid = rec.get("id") or ""
    host.effects.put("` + xfParkKind + `", rid, properties={"seenBy": "importbomb"})
    return {"output": {"seen": rid}}
`

// xfDoc is one vocabulary document: the envelope's kind/metadata/data.
func xfDoc(kind, id string, data map[string]any) map[string]any {
	return map[string]any{"kind": kind, "metadata": map[string]any{"id": id}, "data": data}
}

// xfFunctionDoc declares one python function under the extras package.
func xfFunctionDoc(name string, data map[string]any) map[string]any {
	data["authority"] = xfAuthority
	data["package"] = xfPackage
	data["runtime"] = "python"
	data["timeout"] = "PT10S"
	return xfDoc("substrate.reamde.dev/core/function", xfPkg+"/"+name, data)
}

// xfAgentDoc declares one toolless agent: it answers in a single turn, so the
// budgets name the smallest loop the validator accepts (maxToolCalls has a
// declared minimum of 1 even for an agent that dispatches none).
func xfAgentDoc(name, provider, model, description, prompt string) map[string]any {
	return xfDoc("substrate.reamde.dev/core/agent", xfPkg+"/"+name, map[string]any{
		"authority":   xfAuthority,
		"package":     xfPackage,
		"description": description,
		"prompt":      prompt,
		"provider":    provider,
		"model":       model,
		"budgets":     map[string]any{"maxTurns": 2, "maxToolCalls": 1, "deadlineSeconds": 60},
	})
}

// xfTriggerBaitKind is the kind TRG-03's trigger fires on: a name to write and a
// stamp for the function to put back.
func xfTriggerBaitKind() map[string]any {
	return xfDoc("substrate.reamde.dev/core/kind", xfParkKind, map[string]any{
		"authority":       xfAuthority,
		"package":         xfPackage,
		"names":           map[string]any{"singular": "triggerbait"},
		"description":     "A record whose only job is to make a trigger fire.",
		"displayTemplate": "{name}",
		"properties": map[string]any{
			"name":   map[string]any{"type": "string", "description": "what this bait is called"},
			"seenBy": map[string]any{"type": "string", "description": "the callable that last stamped this record"},
		},
	})
}

// xfApply admits the extras package and whatever documents one case needs.
// An apply is additive (it never prunes a declaration the batch omits), so
// each case declares only its own and the cases stay independent.
func xfApply(c *C, docs ...map[string]any) {
	c.t.Helper()
	batch := append([]map[string]any{
		xfDoc("substrate.reamde.dev/core/package", xfPkg, map[string]any{
			"authority": xfAuthority, "package": xfPackage, "version": 1,
		}),
	}, docs...)
	status, raw := c.do(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": batch}, nil)
	c.requiref(status == http.StatusOK, "vocabulary/apply of %s answered %d: %s", xfPkg, status, raw)
}

// --- the shared readers -------------------------------------------------

// xfError is a refusal's published code and message.
type xfError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func xfDecodeError(c *C, raw []byte) xfError {
	c.t.Helper()
	var out xfError
	c.requiref(json.Unmarshal(raw, &out) == nil, "undecodable error body: %s", raw)
	return out
}

// xfCallOut is what the call API answers: the callable's output and how many
// effects it applied.
type xfCallOut struct {
	Output  map[string]any `json:"output"`
	Effects int            `json:"effects"`
}

// xfCall posts one direct call. The name travels percent-encoded, which is
// what carries a full `{authority}/{name}` identity through the one path
// segment the route declares.
func xfCall(c *C, name string, input, out any) (int, []byte) {
	c.t.Helper()
	return c.do(http.MethodPost, xfFunctionPath+url.PathEscape(name)+"/call",
		map[string]any{"input": input}, out)
}

// xfFailure is one parked delivery (substrate.TriggerFailure).
type xfFailure struct {
	ID        int64  `json:"id"`
	Trigger   string `json:"trigger"`
	Seq       int64  `json:"seq"`
	RecordID  string `json:"recordId"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"lastError"`
	ParkedAt  string `json:"parkedAt"`
}

// xfQuietParked reads a trigger's parked list without recording a step, so a
// waitFor condition can poll it. A read failure is reported as no failures:
// the wait then times out naming what it was waiting for.
func xfQuietParked(c *C, trigger string) []xfFailure {
	var page struct {
		Items []xfFailure `json:"items"`
	}
	if err := c.r.fetch(triggerCollection+"/"+trigger+"/parked", &page); err != nil {
		return nil
	}
	return page.Items
}

// xfRunsFor counts the run rows one callable left behind, whatever their
// status: a trigger delivery writes one, a direct call writes none.
func xfRunsFor(c *C, callable string) int {
	c.t.Helper()
	recs, err := c.quietList(runCollection)
	c.requiref(err == nil, "listing the run records: %v", err)
	n := 0
	for _, rec := range recs {
		if refPath(rec.Properties["callable"]) == callable {
			n++
		}
	}
	return n
}

// xfAgentResult mirrors substrate.AgentResult, narrowed to what these cases
// assert on.
type xfAgentResult struct {
	Reply            string  `json:"reply"`
	Thread           string  `json:"thread"`
	Status           string  `json:"status"`
	Turns            int     `json:"turns"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	CostUSD          float64 `json:"costUSD"`
}

// xfChatEvent mirrors substrate.AgentEvent, narrowed the same way.
type xfChatEvent struct {
	Kind   string         `json:"kind"`
	Thread string         `json:"thread"`
	Text   string         `json:"text"`
	Error  string         `json:"error"`
	Result *xfAgentResult `json:"result"`
}

// xfThreadMessages reads one thread's persisted transcript. The `thread`
// property is a reference: a flat `<kind>/<id>` path string, so it filters by
// equality like any other string.
func xfThreadMessages(c *C, thread string) []record {
	c.t.Helper()
	filter := url.QueryEscape(`{"properties":{"thread":{"eq":"substrate.reamde.dev/core/llmthread/` + thread + `"}}}`)
	var page struct {
		Records []record `json:"records"`
	}
	status, raw := c.do(http.MethodGet, xfMessageCollection+"?first=50&filter="+filter, nil, &page)
	c.requiref(status == http.StatusOK, "listing thread %s's messages answered %d: %s", thread, status, raw)
	return page.Records
}

// --- FN-01 --------------------------------------------------------------

func xfCaseFunctionCall(c *C) {
	xfApply(c, xfFunctionDoc("wordcount", map[string]any{
		"description": "Count the words in a string.",
		"arguments": []map[string]any{
			{"name": "text", "type": "string", "required": true, "description": "the text to count"},
		},
		"returns": []map[string]any{
			{"name": "words", "type": "float", "required": true, "description": "how many words the text holds"},
		},
		"source": xfWordCountSource,
	}))
	c.stepf("declared the python function `%s/wordcount` through `vocabulary/apply`", xfPkg)

	var out xfCallOut
	status, raw := xfCall(c, "wordcount", map[string]any{"text": "one two three"}, &out)
	c.requiref(status == http.StatusOK, "the call answered %d: %s", status, raw)
	words, ok := out.Output["words"].(float64)
	c.requiref(ok && words == 3, "the call answered words=%v, want 3: %s", out.Output["words"], raw)
	c.requiref(out.Effects == 0, "a function that writes nothing reported %d effects", out.Effects)
	c.stepf("`wordcount` counted `one two three` as 3 words and applied 0 effects; the bare name resolved because it is the only one")

	// The same function under its full identity: one path segment, the slash
	// percent-encoded, which is how an ambiguous name is disambiguated.
	out = xfCallOut{}
	status, raw = xfCall(c, xfPkg+"/wordcount", map[string]any{"text": "four five"}, &out)
	c.requiref(status == http.StatusOK, "the call by full identity answered %d: %s", status, raw)
	words, _ = out.Output["words"].(float64)
	c.requiref(words == 2, "the call by full identity answered words=%v, want 2: %s", out.Output["words"], raw)
	c.stepf("`%s/wordcount`, percent-encoded into the one name segment, answers the same call", xfPkg)
}

// --- FN-02 --------------------------------------------------------------

func xfCaseFunctionFault(c *C) {
	xfApply(c, xfFunctionDoc("alwaysfails", map[string]any{
		"description": "A body that raises unconditionally.",
		"source":      xfAlwaysFailsSource,
	}))
	runsBefore := xfRunsFor(c, xfPkg+"/alwaysfails")

	status, raw := xfCall(c, "alwaysfails", map[string]any{}, nil)
	c.requiref(status == http.StatusInternalServerError,
		"a raising body answered %d, want 500: %s", status, raw)
	fail := xfDecodeError(c, raw)
	// Pinned exactly: the in-process conformance suite
	// (internal/testenv/conformance_db_test.go) holds this same code, and this
	// is the same contract over the live door.
	c.requiref(fail.Error.Code == "function_failed",
		"the refusal's code is %q, want function_failed: %s", fail.Error.Code, raw)
	c.requiref(strings.Contains(fail.Error.Message, "deliberate"),
		"the refusal does not carry the body's own exception: %s", fail.Error.Message)
	c.stepf("a body raising `RuntimeError(\"deliberate\")` answered 500 `function_failed` carrying the exception, not the 422 a bad input would be")

	// The observability side: `CallFunction` (internal/engine/runner.go) is
	// documented as "no cursor motion, no run row", and a direct call is not a
	// delivery. There is nothing to read, and this asserts that rather than
	// skipping it.
	c.requiref(xfRunsFor(c, xfPkg+"/alwaysfails") == runsBefore,
		"the direct call wrote a run record; the call API records no delivery")
	c.stepf("the failed call left NO run record behind: a direct call is not a delivery, and only a trigger delivery writes the run ledger")
}

// --- FN-03 --------------------------------------------------------------

func xfCaseHostFunctions(c *C) {
	// `query` is a read, and the caller is a token that owns the whole
	// repository: there is nothing narrower to hold it to, so it runs.
	var out xfCallOut
	status, raw := xfCall(c, xfHostQuery,
		map[string]any{"kind": taskKind, "id": "task-welcome-flow"}, &out)
	c.requiref(status == http.StatusOK, "calling %s answered %d: %s", xfHostQuery, status, raw)
	rec, _ := out.Output["record"].(map[string]any)
	c.requiref(rec != nil && rec["id"] == "task-welcome-flow" && rec["kind"] == taskKind,
		"%s answered the wrong record: %s", xfHostQuery, raw)
	c.stepf("`%s` read `task-welcome-flow` back through the call API, under the token's own reach", xfHostQuery)

	// The bare name is refused: a built-in is kept out of the bare-name
	// candidates, so a repository function may take the name `query` without
	// the host one shadowing it.
	status, raw = xfCall(c, "query", map[string]any{"kind": taskKind, "id": "task-welcome-flow"}, nil)
	c.requiref(status == http.StatusNotFound, "the bare name `query` answered %d, want 404: %s", status, raw)
	refusal := xfDecodeError(c, raw)
	c.requiref(strings.Contains(refusal.Error.Message, xfHostQuery),
		"the 404 does not name the full identity to use instead: %s", refusal.Error.Message)
	c.stepf("the bare name `query` is a 404 naming `%s`: a host function answers its full identity alone", xfHostQuery)

	// `propose` and `mutate` write, and their ceiling is the CALLING AGENT's
	// effective emit. A direct call has no calling agent, so there is no
	// ceiling to apply and inventing one would turn a reviewed write into an
	// unreviewed one. The inputs are WELL-FORMED on purpose: argument
	// validation answers first (422), and the gate under test is the one
	// behind it.
	inputs := map[string]map[string]any{
		xfHostPropose: {
			"op": "patch", "kind": taskKind, "target": "task-welcome-flow",
			"diff": map[string]any{"properties": map[string]any{"priority": "low"}}, "rationale": "a direct call must not file this",
		},
		xfHostMutate: {"query": `mutation { patch(kind: "` + taskKind + `", id: "task-welcome-flow", input: {}) { id } }`},
	}
	for _, name := range []string{xfHostPropose, xfHostMutate} {
		status, raw = xfCall(c, name, inputs[name], nil)
		c.requiref(status == http.StatusForbidden, "calling %s answered %d, want 403: %s", name, status, raw)
		refusal = xfDecodeError(c, raw)
		c.requiref(refusal.Error.Code == "forbidden", "%s was refused with code %q, want forbidden", name, refusal.Error.Code)
		c.requiref(strings.Contains(refusal.Error.Message, "CALLING AGENT") &&
			strings.Contains(refusal.Error.Message, "call the agent"),
			"the refusal of %s does not say whose grants bound it, or where it does work: %s", name, refusal.Error.Message)
	}
	c.stepf("`%s` and `%s` are both refused 403: a direct call carries no calling agent, so their writes have no ceiling and the refusal says to call an agent instead", xfHostPropose, xfHostMutate)
}

// --- TRG-03 -------------------------------------------------------------

func xfCaseTriggerPark(c *C) {
	xfApply(c, xfTriggerBaitKind(), xfFunctionDoc("importbomb", map[string]any{
		"description": "A delivery that raises, so its trigger parks it.",
		"permissions": map[string]any{"writes": []string{xfParkKind}},
		"source":      xfImportBombSource,
	}))
	c.putTrigger(xfParkTrigger, map[string]any{
		"enabled": true,
		"source": map[string]any{"record": map[string]any{
			"kinds": []string{xfParkKind}, "ops": []string{"create"},
		}},
		"callable": "substrate.reamde.dev/core/function/" + xfPkg + "/importbomb",
	})
	c.putRec(xfParkCollection, xfBaitID, map[string]any{"name": "The bait the bomb goes off on"})

	// The wake races the server's own dispatch tick, so the wait is on the
	// settled state: the delivery retries, then parks, whoever ran it.
	c.wake(xfParkTrigger)
	c.waitFor("the failing delivery to park", func() bool { return len(xfQuietParked(c, xfParkTrigger)) == 1 })

	var parked struct {
		Items []xfFailure `json:"items"`
	}
	status, raw := c.do(http.MethodGet, triggerCollection+"/"+xfParkTrigger+"/parked", nil, &parked)
	c.requiref(status == http.StatusOK, "reading the parked list answered %d: %s", status, raw)
	c.requiref(len(parked.Items) == 1, "the parked list holds %d failures, want exactly 1: %s", len(parked.Items), raw)
	failure := parked.Items[0]
	c.requiref(failure.RecordID == xfBaitID && failure.Seq > 0,
		"the parked failure names record %q at seq %d, want %q at a real seq", failure.RecordID, failure.Seq, xfBaitID)
	c.requiref(failure.Attempts == 3,
		"the delivery parked after %d attempts, want the declared 3 (internal/engine: triggerAttempts)", failure.Attempts)
	c.requiref(strings.Contains(failure.LastError, "the import bomb went off"),
		"the parked failure does not carry the body's own error: %s", failure.LastError)
	c.stepf("the delivery failed 3 times and parked as failure %d, carrying the record (`%s`), the changelog seq (%d) and the body's error", failure.ID, failure.RecordID, failure.Seq)

	bait := c.getRec(xfParkCollection, xfBaitID)
	c.requiref(bait.prop("seenBy") == "", "the parked delivery wrote %q onto the record; a failed body applies nothing", bait.prop("seenBy"))
	c.stepf("the record is untouched: a body that raises commits no effect")

	// Fix the function. The engine maintains the declaration's version, so a
	// changed source lands at stored+1 without anybody bumping it by hand.
	xfApply(c, xfFunctionDoc("importbomb", map[string]any{
		"description": "A delivery that stamps the record it was handed.",
		"permissions": map[string]any{"writes": []string{xfParkKind}},
		"source":      xfStampSource,
	}))
	c.stepf("re-applied `%s/importbomb` with a body that stamps instead of raising", xfPkg)

	var retried struct {
		Ran int `json:"ran"`
	}
	status, raw = c.do(http.MethodPost,
		fmt.Sprintf("%s/%s/parked/%d/retry", triggerCollection, xfParkTrigger, failure.ID), nil, &retried)
	c.requiref(status == http.StatusOK && retried.Ran == 1,
		"the retry answered %d, ran %d: %s", status, retried.Ran, raw)
	stillParked := len(xfQuietParked(c, xfParkTrigger))
	c.requiref(stillParked == 0,
		"the parked list still holds %d failures after a successful retry", stillParked)
	bait = c.getRec(xfParkCollection, xfBaitID)
	c.requiref(bait.prop("seenBy") == "importbomb",
		"the retried delivery did not stamp the record: seenBy is %q", bait.prop("seenBy"))
	c.stepf("the retry of failure %d re-ran the delivery against current state: the stamp landed and the parked list drained to 0", failure.ID)
}

// --- TRG-04 -------------------------------------------------------------

func xfCaseTriggerReplay(c *C) {
	runsBefore := c.quietRuns(xfParkTrigger)
	before := c.getRec(xfParkCollection, xfBaitID)
	c.requiref(before.prop("seenBy") == "importbomb",
		"TRG-04 replays TRG-03's healthy trigger, and the record is not stamped: %v", before.Properties)

	status, raw := c.do(http.MethodPost, triggerCollection+"/"+xfParkTrigger+"/replay",
		map[string]any{"from": 0}, nil)
	c.requiref(status == http.StatusOK, "the replay answered %d: %s", status, raw)
	c.wake(xfParkTrigger)
	c.waitFor("the replayed delivery to settle", func() bool { return c.quietRuns(xfParkTrigger) > runsBefore })
	runsAfter := c.quietRuns(xfParkTrigger)
	c.stepf("a replay from seq 0 handed the trigger its whole backlog again: OK runs went from %d to %d", runsBefore, runsAfter)

	after := c.getRec(xfParkCollection, xfBaitID)
	c.requiref(after.prop("seenBy") == "importbomb", "the re-delivery unstamped the record: seenBy is %q", after.prop("seenBy"))
	c.requiref(after.Version == before.Version,
		"the re-delivery moved `%s` from version %d to %d; a put of the values already there must leave the fold alone",
		xfBaitID, before.Version, after.Version)
	c.stepf("`%s` is still at version %d: the delivery ran again and the idempotent put changed nothing, so replay costs a run and not a rewrite", xfBaitID, after.Version)
}

// --- AGN-02 -------------------------------------------------------------

// xfGreeterResponder answers one content-only turn. AGN-02 asserts the deltas
// reassemble to exactly this, so the stub's chunking is under test too.
func xfGreeterResponder(llmReq) llmTurn { return llmTurn{content: xfGreeting} }

func xfCaseAgentChat(c *C) {
	c.r.stub.respond("chatgreeter", xfGreeterResponder)
	// `storyllm` already points at this run's stub; the agent is new.
	xfApply(c, xfAgentDoc("chatgreeter", "storyllm", "chatgreeter",
		"Greets whoever opens a thread, in one streamed turn.",
		"You greet the person who opened this thread in one short sentence."))

	status, raw := c.do(http.MethodPost, xfAgentPath+"chatgreeter/chat",
		map[string]any{"thread": "", "message": "hello"}, nil)
	c.requiref(status == http.StatusOK, "the chat answered %d: %s", status, raw)

	var events []xfChatEvent
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var ev xfChatEvent
		c.requiref(json.Unmarshal([]byte(line), &ev) == nil, "undecodable ndjson event: %s", line)
		// The error event is the loop failing after the 200 was already gone.
		// It is a failure, never a line to skip past.
		c.requiref(ev.Kind != "error", "the stream carried an error event: %s", ev.Error)
		events = append(events, ev)
	}
	c.requiref(len(events) >= 3, "the stream carried %d events, want a thread, at least one delta and a done: %s", len(events), raw)
	c.requiref(events[0].Kind == "thread" && events[0].Thread != "",
		"the first event is %q with thread %q, want the thread id first", events[0].Kind, events[0].Thread)
	last := events[len(events)-1]
	c.requiref(last.Kind == "done" && last.Result != nil,
		"the last event is %q, want a done carrying the result", last.Kind)

	deltas, streamed := 0, ""
	for _, ev := range events[1 : len(events)-1] {
		if ev.Kind == "delta" {
			deltas++
			streamed += ev.Text
		}
	}
	c.requiref(deltas >= 1, "no delta arrived between the thread event and the done")
	c.requiref(streamed == xfGreeting, "the deltas reassemble to %q, want %q", streamed, xfGreeting)
	c.requiref(last.Result.Reply == xfGreeting && last.Result.Thread == events[0].Thread && last.Result.Status == "ok",
		"the done's result: reply %q, thread %q, status %q", last.Result.Reply, last.Result.Thread, last.Result.Status)
	c.stepf("the stream delivered `thread` (`%s`), %d `delta` events reassembling to the whole reply, then one `done` whose result settles `ok` on the same thread",
		events[0].Thread, deltas)

	// The transcript is ordinary data: the same run re-read through the record
	// API, not a live-only artifact of the stream.
	thread := c.getRec(xfThreadCollection, events[0].Thread)
	c.requiref(thread.prop("status") == "ok" && thread.prop("mode") == "chat" && thread.prop("model") == "chatgreeter",
		"the llmthread record: status %q, mode %q, model %q", thread.prop("status"), thread.prop("mode"), thread.prop("model"))
	msgs := xfThreadMessages(c, events[0].Thread)
	roles := map[string]string{}
	for _, m := range msgs {
		roles[m.prop("role")] = m.prop("content")
	}
	c.requiref(roles["user"] == "hello" && roles["assistant"] == xfGreeting,
		"the persisted transcript holds %d messages: %v", len(msgs), roles)
	c.stepf("the thread persists as records: `llmthread` `%s` settled `ok` in mode `chat`, with the user's `hello` and the assistant's reply as `llmmessage` rows", events[0].Thread)
}

// --- AGN-04 -------------------------------------------------------------

func xfCaseAgentNoProvider(c *C) {
	xfApply(c, xfAgentDoc("orphanagent", "missingprovider", "orphanmodel",
		"An agent whose provider row nobody ever wrote.",
		"You answer in one short sentence."))
	c.stepf("the declaration was ADMITTED: an agent names its provider by id, and nothing resolves that id until a call does")

	status, raw := c.do(http.MethodPost, xfAgentPath+"orphanagent/call", map[string]any{"input": "hi"}, nil)
	c.requiref(status == http.StatusUnprocessableEntity, "the call answered %d, want 422: %s", status, raw)
	refusal := xfDecodeError(c, raw)
	c.requiref(refusal.Error.Code == "validation", "the refusal's code is %q, want validation: %s", refusal.Error.Code, raw)
	c.requiref(strings.Contains(refusal.Error.Message, `llmprovider row "missingprovider" does not resolve`),
		"the refusal does not name the missing row: %s", refusal.Error.Message)
	c.requiref(strings.Contains(refusal.Error.Message, "create it"),
		"the refusal names the missing row but not what to do about it: %s", refusal.Error.Message)
	c.stepf("the call was refused 422 naming the row it wanted: `llmprovider row \"missingprovider\" does not resolve`, and telling the owner to create it")
}

// --- AGN-05 -------------------------------------------------------------

// xfPricedResponder answers one content-only turn; what AGN-05 reads is the
// usage block the stub attaches to every turn, not this text.
func xfPricedResponder(llmReq) llmTurn {
	return llmTurn{content: "Ten tokens in, five out."}
}

func xfCaseAgentCost(c *C) {
	c.r.stub.respond("pricedagent", xfPricedResponder)
	// A provider row of its own: `storyllm` carries no pricing and the
	// stories read its threads, so the prices land on a new row instead.
	// $1 per prompt token and $2 per completion token, which the stub's fixed
	// 10/5 tally turns into an exact 10*1 + 5*2 = 20.
	c.putRec(providerCollection, "pricedllm", map[string]any{
		"label": "the e2e scripted stub, with prices", "wire": "openai",
		"baseURL": c.r.stub.url(), "apiKey": "priced-key",
		"pricing": []map[string]any{
			{"model": "pricedagent", "inputPer1M": "1000000", "outputPer1M": "2000000"},
		},
	})
	xfApply(c, xfAgentDoc("pricedagent", "pricedllm", "pricedagent",
		"Answers once, so the run's tally is exactly one turn's usage.",
		"You answer in one short sentence."))

	var res xfAgentResult
	status, raw := c.do(http.MethodPost, xfAgentPath+"pricedagent/call",
		map[string]any{"input": "what did that cost"}, &res)
	c.requiref(status == http.StatusOK, "the agent call answered %d: %s", status, raw)
	c.requiref(res.Status == "ok" && res.Turns == 1, "the run settled %q after %d turns", res.Status, res.Turns)
	c.requiref(res.PromptTokens == 10 && res.CompletionTokens == 5 && res.TotalTokens == 15,
		"the tally is %d prompt + %d completion = %d, want the stub's 10 + 5 = 15",
		res.PromptTokens, res.CompletionTokens, res.TotalTokens)
	// Exact, not approximate: the prices are whole per-token dollars, so the
	// float arithmetic (engine: (prompt*in + completion*out) / 1e6) is exact
	// and a drifting result means the pricing table stopped applying.
	c.requiref(res.CostUSD == 20,
		"costUSD is %v, want 20 (10 prompt tokens at $1 and 5 completion tokens at $2)", res.CostUSD)
	c.stepf("the run's tally is 10 + 5 = 15 tokens, priced from the row's own table into costUSD 20")

	thread := c.getRec(xfThreadCollection, res.Thread)
	tokens, _ := thread.Properties["totalTokens"].(float64)
	cost, _ := thread.Properties["costUSD"].(float64)
	c.requiref(tokens == 15 && cost == 20,
		"the llmthread record stamped totalTokens %v and costUSD %v, want 15 and 20", thread.Properties["totalTokens"], thread.Properties["costUSD"])
	c.stepf("the `llmthread` record `%s` carries the same stamp: 15 tokens, costUSD 20, readable long after the call returned", res.Thread)
}
