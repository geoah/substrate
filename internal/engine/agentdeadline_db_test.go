package engine

// An agent run that reaches its deadline settles `overbudget` on its one
// thread, wherever the deadline lands: a deadline inside a model call no
// longer fails the delivery into a retry on a fresh thread. A run whose
// deadline comes inside the reserve at the top of a turn gets one last turn,
// opened by a nudge that is sent and never stored, with a turn and a tool call
// past its counters where those are spent. A run whose caller's context ends
// first still fails as an error.

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const deadlinePackage = "deadline.test.dev/deadline"

var deadlineNote = deadlinePackage + "/note"

// The final-phase agents' deadline, and how much of it is left when the test
// releases their slow tool. The reserve is a fifth of the window (4 s), so the
// next turn starts inside it with a second to spare for the anchor's error,
// and the final turn has about 3 s for one completion and one write.
const (
	finalDeadline    = 20 * time.Second
	finalReleaseLeft = 3 * time.Second
)

// provisionDeadlineAgents installs a note kind and four agents on one
// provider row:
//   - `overrunner`: a 5 s deadline and the write tool.
//   - `drafter`: the final-phase deadline, one turn and one tool call, the
//     write tool and `napper` as a sub-agent.
//   - `chronicler`: a chat agent with the final-phase deadline and `napper`,
//     on the one model that declares a context window (1000 tokens, reserve
//     200, keep 60), so its thread compacts.
//   - `napper`: no tools; the tests hold its model turn to make a slow tool.
func provisionDeadlineAgents(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "deadlinellm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-deadlinellm",
			"pricing": []any{
				map[string]any{"model": "dover", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "ddraft", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "dnap", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "dchron", "inputPer1M": "1", "outputPer1M": "5", "contextWindow": 1000},
			},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row deadlinellm: %v", err)
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(deadlinePackage, name, data)
	}
	finalSeconds := int(finalDeadline / time.Second)
	docs := []map[string]any{
		vocabulary.PackageManifest(deadlinePackage, 0),
		vocabulary.KindManifest(deadlinePackage, map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		agent("overrunner", map[string]any{
			"provider": "deadlinellm", "model": "dover",
			"tools":       []any{map[string]any{"function": vocabulary.HostFunctionWrite}},
			"permissions": map[string]any{"writes": []any{deadlineNote}},
			"budgets":     map[string]any{"deadlineSeconds": 5},
		}),
		agent("drafter", map[string]any{
			"provider": "deadlinellm", "model": "ddraft",
			"tools":       []any{map[string]any{"function": vocabulary.HostFunctionWrite}},
			"subagents":   []any{deadlinePackage + "/napper"},
			"permissions": map[string]any{"writes": []any{deadlineNote}},
			"budgets":     map[string]any{"deadlineSeconds": finalSeconds, "maxTurns": 1, "maxToolCalls": 1},
		}),
		agent("chronicler", map[string]any{
			"provider": "deadlinellm", "model": "dchron",
			"subagents":  []any{deadlinePackage + "/napper"},
			"compaction": map[string]any{"reserveTokens": 200, "keepRecentTokens": 60},
			"budgets":    map[string]any{"deadlineSeconds": finalSeconds},
		}),
		agent("napper", map[string]any{"provider": "deadlinellm", "model": "dnap"}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install deadline package: %v", err)
	}
	return ds, fake
}

// deadlineThreadsOf lists one deadline agent's threads, oldest first, each
// with its id under `__id`.
func deadlineThreadsOf(t *testing.T, ds *dataset, name string) []map[string]any {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT id, props FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", "agent")+` = $2
		ORDER BY created_at, id`, typeThread, vocabulary.RecordPath(kindAgent, deadlinePackage+"/"+name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		props := map[string]any{}
		if err := json.Unmarshal(raw, &props); err != nil {
			t.Fatal(err)
		}
		props["__id"] = id
		out = append(out, props)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// lastUserRowAt reads when the newest user row landed on an agent's one
// thread. The loop starts its deadline as soon as the run's user row has
// committed, so this plus the deadline is the run's deadline less that
// commit. It runs off the test goroutine, so it returns its error.
func lastUserRowAt(ds *dataset, name string) (time.Time, error) {
	ctx := context.Background()
	var thread string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT id FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", "agent")+` = $2`,
		typeThread, vocabulary.RecordPath(kindAgent, deadlinePackage+"/"+name)).Scan(&thread); err != nil {
		return time.Time{}, err
	}
	probe, err := json.Marshal(map[string]any{
		msgRelThread: referenceValueOf(vocabulary.RecordPath(typeThread, thread)),
		"role":       "user",
	})
	if err != nil {
		return time.Time{}, err
	}
	var at time.Time
	err = ds.db.QueryRowContext(ctx, `
		SELECT max(created_at) FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props @> $2::jsonb`,
		typeMessage, string(probe)).Scan(&at)
	return at, err
}

// heldTurn is a channel that blocks a scripted turn, and its release: safe to
// call twice, and called at cleanup, before the fake server's Close waits on
// the held handler.
func heldTurn(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	held := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	return held, release
}

// releaseNearTheDeadline releases a held slow tool once finalReleaseLeft of
// the agent's deadline is left, counted from the run's user row. It starts
// when the held turn arrives; the returned wait blocks until it released.
func releaseNearTheDeadline(ds *dataset, name string, arrived <-chan struct{}, release func()) (wait func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer release()
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			return
		}
		at, err := lastUserRowAt(ds, name)
		if err != nil {
			return
		}
		time.Sleep(time.Until(at.Add(finalDeadline - finalReleaseLeft)))
	}()
	return func() { <-done }
}

// noNudge fails when any message of a request carries the final-turn nudge.
func noNudge(t *testing.T, what string, req map[string]any) {
	t.Helper()
	for _, m := range requestMessages(req) {
		if c, _ := m["content"].(string); strings.Contains(c, finalTurnNudge) {
			t.Fatalf("%s carries the final-turn nudge: %v", what, m)
		}
	}
}

func TestAgentDeadlineInsideAModelCallSettlesOverbudgetOnOneThread(t *testing.T) {
	t.Parallel()
	// The first turn writes a note; the second is held past the 5 s
	// deadline. The delivery settles on its one thread as overbudget, keeps
	// the first turn's write and spend, and is neither retried nor parked.
	ctx := context.Background()
	ds, fake := provisionDeadlineAgents(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath(kindAgent, deadlinePackage+"/overrunner"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	held, _ := heldTurn(t)
	fake.script("dover",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", deadlineNote, "n-early", map[string]any{"name": "early"})}}},
		fakeTurn{content: "too late", release: held},
	)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-deadline", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	threads := deadlineThreadsOf(t, ds, "overrunner")
	if len(threads) != 1 {
		t.Fatalf("overrunner threads = %d, want 1: a deadline inside a model call retried the delivery", len(threads))
	}
	th := threads[0]
	if th["status"] != threadOverBudget || th["reason"] != reasonDeadline {
		t.Fatalf("thread status %v reason %v, want %s / %s", th["status"], th["reason"], threadOverBudget, reasonDeadline)
	}
	if got := intProp(th, "turns"); got != 2 {
		t.Fatalf("thread turns = %d, want 2", got)
	}
	// The first turn's usage (10 prompt, 5 completion at 1 and 5 USD per 1M)
	// stays on the thread; the call the deadline cut off reported none.
	if intProp(th, "promptTokens") != 10 || intProp(th, "completionTokens") != 5 {
		t.Fatalf("thread tokens = %v/%v, want 10/5", th["promptTokens"], th["completionTokens"])
	}
	if cost, _ := anyFloat(th["costUSD"]); math.Abs(cost-35e-6) > 1e-12 {
		t.Fatalf("thread costUSD = %v, want 3.5e-05", th["costUSD"])
	}
	if _, err := ds.Get(ctx, deadlineNote, "n-early"); err != nil {
		t.Fatalf("the first turn's note: %v", err)
	}
	if got := len(fake.requestsOf("dover")); got != 2 {
		t.Fatalf("model requests = %d, want 2", got)
	}
	if st := triggerStatusOf(t, ds, tr.ID); st.Parked != 0 {
		t.Fatalf("trigger parked %d deliveries, want 0", st.Parked)
	}
	var okRuns int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND `+referencePathSQL("props", "trigger")+` = $2 AND props->>'status' = 'ok'`,
		typeTriggerRun, vocabulary.RecordPath(typeTrigger, tr.ID)).Scan(&okRuns); err != nil {
		t.Fatal(err)
	}
	if okRuns != 1 {
		t.Fatalf("ok runs = %d, want 1", okRuns)
	}
}

func TestAgentFinalPhaseGivesTheModelALastTurnToWrite(t *testing.T) {
	t.Parallel()
	// The drafter's first turn calls napper, whose model turn the test holds
	// until 3 s of the 20 s deadline are left: a slow tool. The next turn
	// starts inside the reserve, so the loop sends the nudge and runs one
	// last turn, although maxTurns (1) and maxToolCalls (1) are both spent,
	// and the write that turn makes lands before the run settles.
	ctx := context.Background()
	ds, fake := provisionDeadlineAgents(t)
	held, release := heldTurn(t)
	arrived := make(chan struct{})
	fake.script("ddraft",
		fakeTurn{calls: []fakeCall{{"napper", `{"input":"nap"}`}}},
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", deadlineNote, "n-final", map[string]any{"name": "final"})}}},
	)
	fake.script("dnap", fakeTurn{content: "rested", arrived: arrived, release: held})
	wait := releaseNearTheDeadline(ds, "drafter", arrived, release)
	res, err := ds.CallAgent(ctx, deadlinePackage+"/drafter", "draft")
	wait()
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOverBudget || res.Reason != reasonDeadlineLastTurn {
		t.Fatalf("result status %q reason %q, want %s / %s (a reason of max turns means the loop never entered the final phase)",
			res.Status, res.Reason, threadOverBudget, reasonDeadlineLastTurn)
	}
	if res.Turns != 2 || res.ToolCalls != 2 {
		t.Fatalf("result turns %d tool calls %d, want 2 and 2: the final phase reserves one of each", res.Turns, res.ToolCalls)
	}
	if _, err := ds.Get(ctx, deadlineNote, "n-final"); err != nil {
		t.Fatalf("the final turn's note: %v", err)
	}
	// The final request carries the nudge as its last message, and the
	// tools: the write is what the turn is for.
	reqs := fake.requestsOf("ddraft")
	if len(reqs) != 2 {
		t.Fatalf("drafter requests = %d, want 2", len(reqs))
	}
	msgs := requestMessages(reqs[1])
	if last := msgs[len(msgs)-1]; last["role"] != "user" || last["content"] != finalTurnNudge {
		t.Fatalf("final request's last message = %v, want the nudge", last)
	}
	if tools, _ := reqs[1]["tools"].([]any); len(tools) == 0 {
		t.Fatal("final request carries no tools")
	}
	// The nudge is sent, never stored.
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["content"] == finalTurnNudge {
			t.Fatalf("the thread stores the nudge: %v", m)
		}
	}
}

func TestAgentFinalPhaseNudgeStaysOutOfLaterRuns(t *testing.T) {
	t.Parallel()
	// Run 1 of a chat thread enters the final phase and replies without a
	// tool. Run 2 continues the thread, and its settle folds run 1 into a
	// summary. Neither run 2's request nor the summarizer's transcript
	// carries the nudge: it told run 1 to finish, not the thread.
	ctx := context.Background()
	ds, fake := provisionDeadlineAgents(t)
	chronicler := deadlinePackage + "/chronicler"
	q1, q2 := "q1"+hundred, "q2"+hundred
	r1, r2 := "r1"+hundred, "r2"+hundred
	held, release := heldTurn(t)
	arrived := make(chan struct{})
	fake.script("dchron",
		fakeTurn{calls: []fakeCall{{"napper", `{"input":"nap"}`}}},
		fakeTurn{content: r1},
	)
	fake.script("dnap", fakeTurn{content: "rested", arrived: arrived, release: held})
	wait := releaseNearTheDeadline(ds, "chronicler", arrived, release)
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, chronicler, "", q1, func(substrate.AgentEvent) {})
	wait()
	if err != nil {
		t.Fatalf("chat 1: %v", err)
	}
	reqs := fake.requestsOf("dchron")
	if len(reqs) != 2 {
		t.Fatalf("run 1 requests = %d, want 2", len(reqs))
	}
	if msgs := requestMessages(reqs[1]); msgs[len(msgs)-1]["content"] != finalTurnNudge {
		t.Fatalf("run 1 never entered the final phase: status %q reason %q", res.Status, res.Reason)
	}
	if res.Status != threadOK {
		t.Fatalf("run 1 status %q reason %q, want ok: the last turn called no tool", res.Status, res.Reason)
	}

	fake.script("dchron", fakeTurn{content: r2, promptTokens: 900}, fakeTurn{content: "SUMMARY ONE"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, chronicler, res.Thread, q2, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat 2: %v", err)
	}
	reqs = fake.requestsOf("dchron")
	if len(reqs) != 4 || !isSummarizerRequest(reqs[3]) {
		t.Fatalf("run 2 made %d requests, want a turn and a summarizer call", len(reqs)-2)
	}
	// Both requests read run 1, so the absence below is not for want of
	// looking.
	continuation := requestMessages(reqs[2])
	if continuation[1]["content"] != q1 {
		t.Fatalf("run 2 did not replay run 1: %v", continuation)
	}
	noNudge(t, "run 2's request", reqs[2])
	transcript, _ := requestMessages(reqs[3])[1]["content"].(string)
	if !strings.Contains(transcript, q1) {
		t.Fatalf("the summarizer did not cover run 1: %q", transcript)
	}
	noNudge(t, "the summarizer's request", reqs[3])
}

// expiringContext reports DeadlineExceeded once canceled: a caller whose own
// deadline passes at the moment the test picks. Value hides the embedded
// cancel context, so a context derived from this one cannot attach to it and
// takes this Err when it ends.
type expiringContext struct{ context.Context }

func (c expiringContext) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func (expiringContext) Value(any) any { return nil }

func TestAgentCallerContextEndingFirstStaysAnError(t *testing.T) {
	t.Parallel()
	// The caller's context ends while the model call is held, well inside
	// the drafter's 20 s deadline. That is the server or the caller going
	// away, not the run's deadline, whether the context says canceled or
	// deadline exceeded: the call fails with the model call's error instead
	// of settling overbudget, and the thread is left to the sweep as before
	// (its settle runs on the dead context).
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"canceled", func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}},
		{"deadline exceeded", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			return expiringContext{ctx}, cancel
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds, fake := provisionDeadlineAgents(t)
			held, _ := heldTurn(t)
			arrived := make(chan struct{})
			fake.script("ddraft", fakeTurn{content: "never", arrived: arrived, release: held})
			cctx, cancel := tc.ctx()
			t.Cleanup(cancel)
			go func() {
				select {
				case <-arrived:
				case <-time.After(30 * time.Second):
				}
				cancel()
			}()
			res, err := ds.CallAgent(cctx, deadlinePackage+"/drafter", "draft")
			if err == nil {
				t.Fatalf("call settled %+v, want an error", res)
			}
			if !strings.Contains(err.Error(), "drafter: llm:") {
				t.Fatalf("call error = %v, want the model call's error", err)
			}
			threads := deadlineThreadsOf(t, ds, "drafter")
			if len(threads) != 1 {
				t.Fatalf("drafter threads = %d, want 1", len(threads))
			}
			if threads[0]["status"] == threadOverBudget {
				t.Fatalf("thread status %v reason %v: a caller's context settled as the run's deadline", threads[0]["status"], threads[0]["reason"])
			}
		})
	}
}
