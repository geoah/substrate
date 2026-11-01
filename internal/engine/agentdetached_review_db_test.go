package engine

// Regressions from the review of detached chat turns: a stop holds against
// every automatic resume, a stop reaches the sub-agent the turn is waiting
// on, a resolution landing mid-turn takes an ordinal of its own, a stuck
// write cannot hold a stopped turn, and the per-repository cap.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A resolution lands while the turn streams, then the owner stops the turn.
// Nothing resumes the stopped thread on its own: not the settle-time
// re-check and not the sweep. The resolution row takes an ordinal past the
// streaming reply's, and the owner's next message replays it.
func TestAStopHoldsAgainstAResolutionThatLandedMidTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-stop", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	hold, _ := heldTurn(t)
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/crew/widget","target":"w-stop","diff":{"properties":{"name":"better"}}}`}}},
		fakeTurn{content: "partial reply", hold: hold},
	)
	log := newEventLog()
	chatted := make(chan struct{})
	go func() {
		defer close(chatted)
		_, _ = ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Message: "change the widget"}, log.emit)
	}()
	delta := log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread

	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	sys := systemMessages(t, ds, thread)
	if len(sys) != 1 {
		t.Fatalf("system rows = %d", len(sys))
	}
	if sysTurn := intProp(sys[0], "turn"); sysTurn <= delta.Turn {
		t.Fatalf("the resolution row took turn %d, at or below the streaming reply's %d", sysTurn, delta.Turn)
	}

	res, err := ds.StopAgentTurn(ctx, substrate.ActorConsole, thread)
	if err != nil || res.Status != threadStopped {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	<-chatted
	holdFor(t, "the stop", func() bool {
		return threadProps(t, ds, thread)["status"] == threadStopped && len(fake.requestsOf("root")) == 2
	})
	if n, err := ds.SweepResolutions(ctx); err != nil || n != 0 {
		t.Fatalf("the sweep resumed %d thread(s), %v", n, err)
	}
	// The partial reply row carries the ordinal its deltas did.
	var replyTurn int
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "assistant" && m["content"] == "partia" {
			replyTurn = intProp(m, "turn")
		}
	}
	if replyTurn != delta.Turn {
		t.Fatalf("the partial reply row's turn = %d, its deltas carried %d", replyTurn, delta.Turn)
	}

	// The owner writes again: the next turn replays the resolution.
	fake.script("root", fakeTurn{content: "carried on"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Thread: thread, Message: "carry on"}, nil); err != nil {
		t.Fatalf("the owner's next message: %v", err)
	}
	reqs := fake.requestsOf("root")
	replayed := false
	msgs, _ := reqs[len(reqs)-1]["messages"].([]any)
	for _, m := range msgs {
		if mm, _ := m.(map[string]any); mm != nil && msgText(mm) == sys[0]["content"] {
			replayed = true
		}
	}
	if !replayed {
		body, _ := json.Marshal(msgs)
		t.Fatalf("the next turn's request does not replay the resolution row %q: %s", sys[0]["content"], body)
	}
}

// A stopped thread that a run of its own agent resolves something on is
// still not resumed by the sweep, which passes no decider.
func TestTheSweepLeavesAStoppedThreadAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-self", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	hold, _ := heldTurn(t)
	fake.script("self", fakeTurn{content: "working on it", hold: hold})
	log := newEventLog()
	chatted := make(chan struct{})
	go func() {
		defer close(chatted)
		_, _ = ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/selfjudge", substrate.ChatInput{Message: "stand by"}, log.emit)
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread
	if res, err := ds.StopAgentTurn(ctx, substrate.ActorConsole, thread); err != nil || res.Status != threadStopped {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	<-chatted
	time.Sleep(20 * time.Millisecond) // finishedAt strictly before the resolution row
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest, ID: "req-self",
		Properties: map[string]any{
			"diff":   map[string]any{"properties": map[string]any{"name": "better"}},
			"thread": thread,
			"target": vocabulary.RecordPath(crewPackage+"/widget", "w-self"),
		},
	}); err != nil {
		t.Fatalf("put request: %v", err)
	}
	fake.script("self",
		fakeTurn{calls: []fakeCall{{"write", decideArgs(t, "req-self", "accepted")}}},
		fakeTurn{content: "accepted it."},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/selfjudge", "work the inbox"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if n, err := ds.SweepResolutions(ctx); err != nil || n != 0 {
		t.Fatalf("the sweep resumed %d thread(s), %v", n, err)
	}
	holdFor(t, "the stopped thread", func() bool {
		return threadProps(t, ds, thread)["status"] == threadStopped && len(fake.requestsOf("self")) == 3
	})
}

// A stop while the turn waits on a sub-agent's completion settles the child
// thread `stopped` too, not `running` under a lease nothing renews.
func TestAStopSettlesTheSubAgentThreadStopped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	childArrived := make(chan struct{})
	childHeld, _ := heldTurn(t)
	fake.script("root", fakeTurn{calls: []fakeCall{{"scribe", `{"input":"summarize the widget"}`}}})
	fake.script("sub", fakeTurn{content: "child reply", arrived: childArrived, release: childHeld})
	log := newEventLog()
	chatted := make(chan struct{})
	go func() {
		defer close(chatted)
		_, _ = ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Message: "delegate"}, log.emit)
	}()
	waitArrived(t, childArrived, "the sub-agent's completion")
	thread := log.waitFor(t, substrate.AgentEventThread).Thread
	var child string
	for _, tr := range agentThreadsOf(t, ds, "scribe") {
		child, _ = tr["__id"].(string)
	}
	res, err := ds.StopAgentTurn(ctx, substrate.ActorConsole, thread)
	if err != nil || res.Status != threadStopped {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	<-chatted
	props := threadProps(t, ds, child)
	if props["status"] != threadStopped || props["reason"] != reasonStoppedBy(substrate.ActorConsole) {
		t.Fatalf("child thread = %v (%v), want stopped by console", props["status"], props["reason"])
	}
	if _, live := ds.runningThreads.Load(child); live {
		t.Fatal("the child loop is still registered")
	}
}

// A shutdown while the turn waits on a sub-agent settles both threads.
func TestAShutdownSettlesTheSubAgentThreadStopped(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	childArrived := make(chan struct{})
	childHeld, _ := heldTurn(t)
	fake.script("root", fakeTurn{calls: []fakeCall{{"scribe", `{"input":"summarize"}`}}})
	fake.script("sub", fakeTurn{content: "child reply", arrived: childArrived, release: childHeld})
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Message: "delegate"}, log.emit)
	}()
	waitArrived(t, childArrived, "the sub-agent's completion")
	thread := log.waitFor(t, substrate.AgentEventThread).Thread
	ds.svc.stopBackground(backgroundDrainTimeout)
	for _, tr := range agentThreadsOf(t, ds, "scribe") {
		if tr["status"] != threadStopped || tr["reason"] != reasonShutdown {
			t.Fatalf("child thread = %v (%v)", tr["status"], tr["reason"])
		}
	}
	if got := threadProps(t, ds, thread); got["status"] != threadStopped {
		t.Fatalf("parent thread = %v (%v)", got["status"], got["reason"])
	}
}

// A stop while the stopped turn's own message write is stuck: the write
// gives up within its budget, the failure is logged, and the thread still
// settles `stopped`.
func TestAStopSettlesWhileAMessageWriteIsStuck(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ds.agentWriteTimeout = 300 * time.Millisecond
	stuck := make(chan struct{})
	ds.agentRowWrite = func(ctx context.Context, props map[string]any) error {
		if props["role"] == "assistant" && props["content"] == "partia" {
			close(stuck)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	hold, _ := heldTurn(t)
	fake.script("chat", fakeTurn{content: "partial reply", hold: hold})
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, log.emit)
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread
	stopped := make(chan *substrate.AgentResult, 1)
	go func() {
		res, err := ds.StopAgentTurn(context.Background(), substrate.ActorAPI, thread)
		if err != nil {
			t.Errorf("stop: %v", err)
		}
		stopped <- res
	}()
	select {
	case <-stuck:
	case <-time.After(30 * time.Second):
		t.Fatal("the partial reply write never started")
	}
	var res *substrate.AgentResult
	select {
	case res = <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop waited on a stuck write past its budget")
	}
	if res == nil || res.Status != threadStopped {
		t.Fatalf("stop = %+v", res)
	}
	if got := threadProps(t, ds, thread)["status"]; got != threadStopped {
		t.Fatalf("thread = %v, want stopped", got)
	}
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "assistant" {
			t.Fatalf("the stuck partial reply landed: %+v", m)
		}
	}
}

// The repository runs a bounded number of chat turns at once; one more is
// refused with a conflict that says why, and a slot frees when a turn
// settles.
func TestRunningChatTurnsAreCapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	held, release := heldTurn(t)
	arrivals := make([]chan struct{}, maxRunningChatTurns)
	for i := range arrivals {
		arrivals[i] = make(chan struct{})
		fake.script("chat", fakeTurn{content: "busy", arrived: arrivals[i], release: held})
	}
	done := make(chan error, maxRunningChatTurns)
	for range maxRunningChatTurns {
		go func() {
			_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "work"}, nil)
			done <- err
		}()
	}
	for _, a := range arrivals {
		waitArrived(t, a, "a running chat turn")
	}
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "one more"}, nil)
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "chat turns are already running") {
		t.Fatalf("a turn past the cap = %v, want a conflict naming the cap", err)
	}
	release()
	for range maxRunningChatTurns {
		if err := <-done; err != nil {
			t.Fatalf("a capped turn: %v", err)
		}
	}
	fake.script("chat", fakeTurn{content: "room again"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "now"}, nil); err != nil {
		t.Fatalf("a turn after the others settled: %v", err)
	}
}

// A stop that lands while the turn writes its final reply decides the
// settle: the thread settles `stopped` with the reply it got, and the
// resolution that landed meanwhile starts no continuation.
func TestAStopDuringTheFinalWriteSettlesStoppedAndResumesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: "w-late", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	writing, release := make(chan struct{}), make(chan struct{})
	ds.agentRowWrite = func(ctx context.Context, props map[string]any) error {
		if props["role"] == "assistant" && props["content"] == "final answer" {
			close(writing)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/crew/widget","target":"w-late","diff":{"properties":{"name":"better"}}}`}}},
		fakeTurn{content: "final answer"},
	)
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Message: "change the widget"}, log.emit)
	}()
	waitArrived(t, writing, "the final reply write")
	thread := log.snapshot()[0].Thread
	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	stopped := make(chan *substrate.AgentResult, 1)
	go func() {
		res, _ := ds.StopAgentTurn(ctx, substrate.ActorConsole, thread)
		stopped <- res
	}()
	waitUntil(t, "the stop to reach the turn", func() bool {
		v, ok := ds.runningThreads.Load(thread)
		return ok && v.(*agentLoop).hub.stopper() != ""
	})
	close(release)
	res := <-stopped
	if res == nil || res.Status != threadStopped || res.Reply != "final answer" {
		t.Fatalf("stop = %+v, want stopped with the final reply", res)
	}
	holdFor(t, "the stop", func() bool {
		return threadProps(t, ds, thread)["status"] == threadStopped && len(fake.requestsOf("root")) == 2
	})
	msgs := threadMessages(t, ds, thread)
	if last := msgs[len(msgs)-1]; last["content"] != "final answer" && msgs[len(msgs)-2]["content"] != "final answer" {
		t.Fatalf("the final reply row is missing: %+v", msgs)
	}
}

// A message write that fails settles the thread `error`, so it does not
// stay `running` under its lease.
func TestAFailedMessageWriteSettlesTheThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	ds.agentRowWrite = func(_ context.Context, props map[string]any) error {
		if props["role"] == "assistant" {
			return errors.New("the disk is full")
		}
		return nil
	}
	fake.script("chat", fakeTurn{content: "never lands"})
	log := newEventLog()
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, log.emit); err == nil {
		t.Fatal("a turn whose reply could not be written succeeded")
	}
	thread := log.snapshot()[0].Thread
	props := threadProps(t, ds, thread)
	if props["status"] != threadError || !strings.Contains(fmt.Sprint(props["reason"]), "the disk is full") {
		t.Fatalf("thread = %v (%v), want error naming the write", props["status"], props["reason"])
	}
}

// A chat's function tool that runs an agent (`host.agents.call`) is part of
// the turn: a stop cancels that agent's model call, its thread settles
// `stopped`, and the stop answers without waiting for the model.
func TestAStopReachesAnAgentAFunctionToolRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installRelay(t, ds, fake)
	editorArrived := make(chan struct{})
	held, _ := heldTurn(t)
	fake.script("steward", fakeTurn{calls: []fakeCall{{"curator", `{}`}}})
	fake.script("edit", fakeTurn{content: "a widget", arrived: editorArrived, release: held})
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(ctx, substrate.ActorAPI, relayPackage+"/steward", substrate.ChatInput{Message: "make one"}, log.emit)
	}()
	waitArrived(t, editorArrived, "the editor's completion")
	thread := log.snapshot()[0].Thread
	stopped := make(chan *substrate.AgentResult, 1)
	go func() {
		res, _ := ds.StopAgentTurn(ctx, substrate.ActorConsole, thread)
		stopped <- res
	}()
	select {
	case res := <-stopped:
		if res == nil || res.Status != threadStopped {
			t.Fatalf("stop = %+v", res)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the stop waited on the agent the function tool runs")
	}
	editors := agentThreadsOf(t, ds, "editor")
	if len(editors) != 1 || editors[0]["status"] != threadStopped {
		t.Fatalf("editor threads = %+v, want one stopped", editors)
	}
}

// A stop during the settle-time compaction: the summarizer is canceled, and
// the thread settles `stopped` with the reply it got, never `ok`.
func TestAStopDuringSettleTimeCompactionSettlesStopped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	q1, q2 := "q1"+hundred, "q2"+hundred
	r1, r2 := "r1"+hundred, "r2"+hundred
	fake.script("compact", fakeTurn{content: r1, promptTokens: 900})
	first, err := ds.ChatAgent(ctx, substrate.ActorAPI, compactor, substrate.ChatInput{Message: q1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	summarizing := make(chan struct{})
	held, _ := heldTurn(t)
	fake.script("compact",
		fakeTurn{content: r2, promptTokens: 900},
		fakeTurn{content: "SUMMARY", arrived: summarizing, release: held},
	)
	chatted := make(chan *substrate.AgentResult, 1)
	go func() {
		res, _ := ds.ChatAgent(ctx, substrate.ActorAPI, compactor, substrate.ChatInput{Thread: first.Thread, Message: q2}, nil)
		chatted <- res
	}()
	waitArrived(t, summarizing, "the settle-time summarizer")
	res, err := ds.StopAgentTurn(ctx, substrate.ActorConsole, first.Thread)
	if err != nil || res.Status != threadStopped || res.Reply != r2 {
		t.Fatalf("stop = %+v, %v; want stopped keeping the reply", res, err)
	}
	if got := <-chatted; got == nil || got.Status != threadStopped {
		t.Fatalf("the chat's own result = %+v", got)
	}
	props := threadProps(t, ds, first.Thread)
	if props["status"] != threadStopped || props["reason"] != reasonStoppedBy(substrate.ActorConsole) {
		t.Fatalf("thread = %v (%v), want stopped by console", props["status"], props["reason"])
	}
	if ds.admitResume(ctx, first.Thread, "") {
		t.Fatal("a resume would be admitted on the stopped thread")
	}
}
