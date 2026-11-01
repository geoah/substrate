package engine

// Detached chat turns (decision 0169): a chat turn runs to its own end
// whether or not its client stays, any client attaches to it by thread id
// and gets replay then live, a client stops it, and a stopped or shut-down
// turn settles `stopped` with what it streamed kept.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// threadProps reads one thread's properties by id.
func threadProps(t *testing.T, ds *dataset, id string) map[string]any {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: typeThread, ID: id})
	if err != nil || row == nil {
		t.Fatalf("thread %s: %v", id, err)
	}
	return row.Props
}

// eventLog collects a stream's events and signals each arrival.
type eventLog struct {
	mu     sync.Mutex
	events []substrate.AgentEvent
	signal chan struct{}
}

func newEventLog() *eventLog { return &eventLog{signal: make(chan struct{}, 1024)} }

func (e *eventLog) emit(ev substrate.AgentEvent) {
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
	e.signal <- struct{}{}
}

func (e *eventLog) snapshot() []substrate.AgentEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]substrate.AgentEvent(nil), e.events...)
}

// waitFor blocks until the log holds an event of kind, or fails.
func (e *eventLog) waitFor(t *testing.T, kind string) substrate.AgentEvent {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		for _, ev := range e.snapshot() {
			if ev.Kind == kind {
				return ev
			}
		}
		select {
		case <-e.signal:
		case <-deadline:
			t.Fatalf("no %s event; saw %+v", kind, e.snapshot())
		}
	}
}

func TestAChatTurnOutlivesTheClientThatStartedIt(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("chat", fakeTurn{content: "still here", arrived: arrived, release: release})

	cctx, cancel := context.WithCancel(context.Background())
	log := newEventLog()
	done := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(cctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, log.emit)
		done <- err
	}()
	waitArrived(t, arrived, "the chat turn")
	thread := log.waitFor(t, substrate.AgentEventThread).Thread
	// The client leaves mid-turn: its call returns, the turn does not.
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ChatAgent after the client left = %v, want context.Canceled", err)
	}
	if got := threadProps(t, ds, thread)["status"]; got != threadRunning {
		t.Fatalf("thread status after the client left = %v, want running", got)
	}
	close(release)

	res, err := ds.AttachAgentTurn(context.Background(), thread, nil)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if res.Status != threadOK || res.Reply != "still here" {
		t.Fatalf("settled result = %+v", res)
	}
	props := threadProps(t, ds, thread)
	if props["status"] != threadOK {
		t.Fatalf("thread status = %v reason %v, want ok", props["status"], props["reason"])
	}
	msgs := threadMessages(t, ds, thread)
	if last := msgs[len(msgs)-1]; last["role"] != "assistant" || last["content"] != "still here" {
		t.Fatalf("newest message = %+v, want the assistant reply", last)
	}
	if _, live := ds.runningThreads.Load(thread); live {
		t.Fatal("the settled turn is still in runningThreads")
	}
}

func TestAttachReplaysTheRunningTurnThenStreamsLive(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{name: "query", args: `{"op":"list","kind":"` + crewPackage + `/widget"}`}}},
		fakeTurn{content: "found none", arrived: arrived, release: release},
	)
	started := make(chan *substrate.AgentResult, 1)
	go func() {
		res, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/librarian", substrate.ChatInput{Message: "list widgets"}, nil)
		if err != nil {
			t.Errorf("chat: %v", err)
		}
		started <- res
	}()
	// The second completion is held: both tool events are behind it.
	waitArrived(t, arrived, "the chat turn")
	var thread string
	for _, tr := range agentThreadsOf(t, ds, "librarian") {
		thread, _ = tr["__id"].(string)
	}

	log := newEventLog()
	attached := make(chan *substrate.AgentResult, 1)
	go func() {
		res, err := ds.AttachAgentTurn(context.Background(), thread, log.emit)
		if err != nil {
			t.Errorf("attach: %v", err)
		}
		attached <- res
	}()
	log.waitFor(t, substrate.AgentEventToolFinished)
	replayed := log.snapshot()
	close(release)
	res := <-attached
	<-started
	if res == nil || res.Status != threadOK {
		t.Fatalf("attach result = %+v", res)
	}

	if len(replayed) != 3 || replayed[0].Kind != substrate.AgentEventThread || replayed[0].Thread != thread ||
		replayed[1].Kind != substrate.AgentEventToolStarted || replayed[2].Kind != substrate.AgentEventToolFinished {
		t.Fatalf("replay = %+v, want thread, toolStarted, toolFinished", replayed)
	}
	// user 0, the tool-calling assistant 1, the tool row 2, the reply 3.
	if replayed[1].Turn != 2 || replayed[2].Turn != 2 {
		t.Fatalf("tool event turns = %d, %d, want 2", replayed[1].Turn, replayed[2].Turn)
	}
	all := log.snapshot()
	live := all[len(replayed):]
	text := ""
	for _, ev := range live[:len(live)-1] {
		if ev.Kind != substrate.AgentEventDelta || ev.Turn != 3 {
			t.Fatalf("live event %+v, want a turn 3 delta", ev)
		}
		text += ev.Text
	}
	if text != "found none" || live[len(live)-1].Kind != substrate.AgentEventDone {
		t.Fatalf("live events = %+v", live)
	}
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "tool" && intProp(m, "turn") != 2 {
			t.Fatalf("tool row turn = %v, want the events' 2", m["turn"])
		}
	}

	if _, err := ds.AttachAgentTurn(context.Background(), thread, nil); !errors.Is(err, substrate.ErrNoRunningTurn) {
		t.Fatalf("attach after done = %v, want ErrNoRunningTurn", err)
	}
}

func TestStopSettlesAChatTurnWithItsPartialOutput(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	hold, _ := heldTurn(t)
	fake.script("chat", fakeTurn{content: "partial reply", hold: hold})
	log := newEventLog()
	chatted := make(chan *substrate.AgentResult, 1)
	go func() {
		res, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, log.emit)
		if err != nil {
			t.Errorf("chat: %v", err)
		}
		chatted <- res
	}()
	if ev := log.waitFor(t, substrate.AgentEventDelta); ev.Text != "partia" {
		t.Fatalf("first delta = %q", ev.Text)
	}
	thread := log.snapshot()[0].Thread

	res, err := ds.StopAgentTurn(context.Background(), substrate.ActorAPI, thread)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if res.Status != threadStopped || res.Reason != "stopped by api" || res.Reply != "partia" {
		t.Fatalf("stop result = %+v", res)
	}
	if got := <-chatted; got == nil || got.Status != threadStopped {
		t.Fatalf("the attached stream's result = %+v, want stopped", got)
	}
	if done := log.waitFor(t, substrate.AgentEventDone); done.Result.Status != threadStopped {
		t.Fatalf("done event = %+v", done)
	}
	props := threadProps(t, ds, thread)
	if props["status"] != threadStopped || props["reason"] != "stopped by api" {
		t.Fatalf("thread = %v (%v), want stopped by api", props["status"], props["reason"])
	}
	msgs := threadMessages(t, ds, thread)
	if last := msgs[len(msgs)-1]; last["role"] != "assistant" || last["content"] != "partia" {
		t.Fatalf("newest message = %+v, want the partial reply", last)
	}

	// The stopped thread admits the next turn: no 409.
	fake.script("chat", fakeTurn{content: "back"})
	if _, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Thread: thread, Message: "again"}, nil); err != nil {
		t.Fatalf("a turn after the stop: %v", err)
	}
	if got := threadProps(t, ds, thread); got["status"] != threadOK || got["reason"] != nil {
		t.Fatalf("thread after the next turn = %v (%v)", got["status"], got["reason"])
	}
}

func TestStopRefusesAThreadWithNoRunningTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "done"})
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.StopAgentTurn(ctx, substrate.ActorAPI, res.Thread); !errors.Is(err, substrate.ErrNoRunningTurn) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("stop on a settled thread = %v, want ErrNoRunningTurn", err)
	}
	if _, err := ds.StopAgentTurn(ctx, substrate.ActorAPI, "nosuchthread"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("stop on an unknown thread = %v, want ErrNotFound", err)
	}

	// A run no client streams (here a call) holds its thread: not stoppable.
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("chat", fakeTurn{content: "called", arrived: arrived, release: release})
	called := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, crewPackage+"/chatter", "a call")
		called <- err
	}()
	waitArrived(t, arrived, "the chat turn")
	var running string
	for _, tr := range agentThreadsOf(t, ds, "chatter") {
		if tr["status"] == threadRunning {
			running, _ = tr["__id"].(string)
		}
	}
	_, stopErr := ds.StopAgentTurn(ctx, substrate.ActorAPI, running)
	_, attachErr := ds.AttachAgentTurn(ctx, running, nil)
	close(release)
	if err := <-called; err != nil {
		t.Fatalf("call: %v", err)
	}
	if running == "" || !errors.Is(stopErr, substrate.ErrNoRunningTurn) || !errors.Is(attachErr, substrate.ErrNoRunningTurn) {
		t.Fatalf("stop / attach on a called run's thread %q = %v / %v, want ErrNoRunningTurn", running, stopErr, attachErr)
	}
}

func TestAShutdownSettlesADetachedTurnStopped(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	held, _ := heldTurn(t)
	arrived := make(chan struct{})
	fake.script("chat", fakeTurn{content: "never", arrived: arrived, release: held})
	log := newEventLog()
	chatted := make(chan *substrate.AgentResult, 1)
	go func() {
		res, _ := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, log.emit)
		chatted <- res
	}()
	waitArrived(t, arrived, "the chat turn")
	thread := log.waitFor(t, substrate.AgentEventThread).Thread

	ds.svc.stopBackground(backgroundDrainTimeout)
	// stopBackground waited for the detached turn, so its settle committed.
	props := threadProps(t, ds, thread)
	if props["status"] != threadStopped || props["reason"] != reasonShutdown {
		t.Fatalf("thread after shutdown = %v (%v), want stopped by the shutdown", props["status"], props["reason"])
	}
	if res := <-chatted; res == nil || res.Status != threadStopped || res.Reason != reasonShutdown {
		t.Fatalf("the attached stream's result = %+v", res)
	}
	// A chat after the shutdown began opens nothing.
	if _, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "late"}, nil); !errors.Is(err, substrate.ErrUnavailable) {
		t.Fatalf("chat during shutdown = %v, want ErrUnavailable", err)
	}
}

func TestTheThreadStatusEnumAdmitsStopped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "done"})
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeThread, res.Thread, substrate.PatchInput{
		Properties: map[string]any{"status": threadStopped, "reason": reasonStoppedBy(substrate.ActorConsole)},
	}); err != nil {
		t.Fatalf("a stopped status does not validate: %v", err)
	}
}

// A reasoning model's thinking carries the reply's `turn`, like its deltas,
// and an attach mid-turn replays it as one merged event before the reply.
func TestAnAttachReplaysTheTurnsReasoningWithItsTurn(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{reasoning: "weighing the options", content: "the answer", hold: hold})
	log := newEventLog()
	chatted := make(chan struct{})
	go func() {
		defer close(chatted)
		_, _ = ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, log.emit)
	}()
	delta := log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread
	for _, ev := range log.snapshot() {
		if ev.Kind == substrate.AgentEventReasoning && ev.Turn != delta.Turn {
			t.Fatalf("a reasoning event carries turn %d, the reply's deltas %d", ev.Turn, delta.Turn)
		}
	}

	late := newEventLog()
	attached := make(chan struct{})
	go func() {
		defer close(attached)
		if _, err := ds.AttachAgentTurn(context.Background(), thread, late.emit); err != nil {
			t.Errorf("attach: %v", err)
		}
	}()
	late.waitFor(t, substrate.AgentEventDelta)
	replay := late.snapshot()
	release()
	<-attached
	<-chatted
	if len(replay) != 3 || replay[1].Kind != substrate.AgentEventReasoning ||
		replay[1].Text != "weighing the options" || replay[1].Turn != delta.Turn || replay[2].Turn != delta.Turn {
		t.Fatalf("replay = %+v, want thread, one reasoning event and the delta, all at turn %d", replay, delta.Turn)
	}
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "assistant" && intProp(m, "turn") != delta.Turn {
			t.Fatalf("the reply row's turn = %v, its events carried %d", m["turn"], delta.Turn)
		}
	}
}

// A settled thread holds no lease, and the owner's next message is a new turn
// at once, after a turn that called sub-agents too (#1038: the thread settled
// `ok` and kept `leaseUntil` minutes past it).
func TestASettledTurnLeavesNoLeaseAndTheNextMessageRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"scribe", `{"input":"first"}`}, {"scribe", `{"input":"second"}`}}},
		fakeTurn{content: "both summarized"},
		fakeTurn{content: "and again"},
	)
	fake.script("sub", fakeTurn{content: "one"}, fakeTurn{content: "two"})

	first, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Message: "delegate"}, nil)
	if err != nil || first.Status != threadOK {
		t.Fatalf("first turn = %+v, %v", first, err)
	}
	props := threadProps(t, ds, first.Thread)
	if props["status"] != threadOK {
		t.Fatalf("thread status = %v (%v), want ok", props["status"], props["reason"])
	}
	if lease, held := props["leaseUntil"]; held {
		t.Fatalf("the settled thread still holds leaseUntil %v", lease)
	}
	children := agentThreadsOf(t, ds, "scribe")
	if len(children) != 2 {
		t.Fatalf("scribe threads: %d, want 2", len(children))
	}
	for _, child := range children {
		if lease, held := child["leaseUntil"]; held || child["status"] != threadOK {
			t.Fatalf("sub-agent thread = %v, leaseUntil %v; want ok and no lease", child["status"], lease)
		}
	}

	second, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/classifier", substrate.ChatInput{Thread: first.Thread, Message: "once more"}, nil)
	if err != nil || second.Status != threadOK || second.Reply != "and again" {
		t.Fatalf("the next message on the settled thread = %+v, %v", second, err)
	}
	msgs := threadMessages(t, ds, first.Thread)
	var users []any
	for _, m := range msgs {
		if m["role"] == "user" {
			users = append(users, m["content"])
		}
	}
	if len(users) != 2 || users[1] != "once more" {
		t.Fatalf("user messages = %v, want both sends", users)
	}
	if lease, held := threadProps(t, ds, first.Thread)["leaseUntil"]; held {
		t.Fatalf("the second settle left leaseUntil %v", lease)
	}
}
