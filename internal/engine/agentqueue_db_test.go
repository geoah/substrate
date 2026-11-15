package engine

// A message sent while a chat turn runs (decision 0318): its row is written
// at once, the loop reads it before its next model call and after the last
// call's tool results, one the run ends without reading starts the next turn,
// and a stop, a failure or a restart leaves it in the thread, written once.

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	librarian   = crewPackage + "/librarian"
	listWidgets = `{"op":"list","kind":"` + crewPackage + `/widget"}`
)

// chatRun is one chat request running in the background.
type chatRun struct {
	log *eventLog
	end chan chatEnd
}

type chatEnd struct {
	res *substrate.AgentResult
	err error
}

func startChat(ds *dataset, agent string, in substrate.ChatInput) *chatRun {
	return startChatOn(context.Background(), ds, agent, in)
}

func startChatOn(ctx context.Context, ds *dataset, agent string, in substrate.ChatInput) *chatRun {
	r := &chatRun{log: newEventLog(), end: make(chan chatEnd, 1)}
	go func() {
		res, err := ds.ChatAgent(ctx, substrate.ActorPerson, agent, in, r.log.emit)
		r.end <- chatEnd{res, err}
	}()
	return r
}

// wait is the request's own end: its stream's result.
func (r *chatRun) wait(t *testing.T) (*substrate.AgentResult, error) {
	t.Helper()
	select {
	case got := <-r.end:
		return got.res, got.err
	case <-time.After(30 * time.Second):
		t.Fatal("the chat request did not return")
		return nil, nil
	}
}

// queue sends a message into the thread a turn runs on and waits until the
// server says its row is written.
func queue(t *testing.T, ds *dataset, agent, thread, id, text string) *chatRun {
	t.Helper()
	r := startChat(ds, agent, substrate.ChatInput{Thread: thread, Message: text, MessageID: id})
	waitUntil(t, "message "+id+" to be queued", func() bool { return eventOf(r.log, substrate.AgentEventMessageQueued, id) != nil })
	return r
}

// eventOf is the log's event of kind that names message id, or nil.
func eventOf(log *eventLog, kind, id string) *substrate.AgentEvent {
	for _, ev := range log.snapshot() {
		if ev.Kind == kind && ev.ID == id {
			return &ev
		}
	}
	return nil
}

// conversation is one model call's messages after the system prompt, as
// `role` or, for a user message, `user:<its text>`.
func conversation(t *testing.T, req map[string]any) []string {
	t.Helper()
	var out []string
	for _, raw := range req["messages"].([]any) {
		m := raw.(map[string]any)
		role, _ := m["role"].(string)
		switch role {
		case "system":
		case "user":
			text, _ := m["content"].(string)
			if parts, ok := m["content"].([]any); ok && len(parts) > 0 {
				text, _ = parts[0].(map[string]any)["text"].(string)
			}
			out = append(out, "user:"+text)
		default:
			out = append(out, role)
		}
	}
	return out
}

func wantConversation(t *testing.T, what string, req map[string]any, want ...string) {
	t.Helper()
	if got := conversation(t, req); !slices.Equal(got, want) {
		t.Fatalf("%s read\n  %v\nwant\n  %v", what, got, want)
	}
}

// rolesByTurn are the thread's rows in ordinal order, as `role` or
// `user:<text>`; it fails on an ordinal two rows share.
func rolesByTurn(t *testing.T, ds *dataset, thread string) []string {
	t.Helper()
	rows := threadMessages(t, ds, thread)
	sort.SliceStable(rows, func(i, j int) bool { return intProp(rows[i], "turn") < intProp(rows[j], "turn") })
	var out []string
	for i, m := range rows {
		if i > 0 && intProp(m, "turn") == intProp(rows[i-1], "turn") {
			t.Fatalf("two rows share ordinal %d: %+v and %+v", intProp(m, "turn"), rows[i-1], m)
		}
		role, _ := m["role"].(string)
		if role == "user" {
			role += ":" + m["content"].(string)
		}
		out = append(out, role)
	}
	return out
}

// The loop with a scripted model: a message that arrives while the call that
// asks for a tool is in flight is read after that tool's result; one that
// arrives during the next round is read after that round's; one that arrives
// while the final answer streams starts the next turn, with no one asking.
func TestAMessageSentWhileATurnRunsIsReadAtTheNextCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	arrived1, release1 := make(chan struct{}), make(chan struct{})
	arrived2, release2 := make(chan struct{}), make(chan struct{})
	hold3, release3 := heldTurn(t)
	arrived4, release4 := make(chan struct{}), make(chan struct{})
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{"query", listWidgets}}, arrived: arrived1, release: release1},
		fakeTurn{calls: []fakeCall{{"query", listWidgets}}, arrived: arrived2, release: release2},
		fakeTurn{pieces: []string{"found ", "none"}, content: "found none", hold: hold3},
		fakeTurn{content: "counted: none", arrived: arrived4, release: release4},
	)
	turn := startChat(ds, librarian, substrate.ChatInput{Message: "list widgets", MessageID: "m0"})
	waitArrived(t, arrived1, "the first call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread

	// Before a tool call: the call that will ask for the tool is in flight.
	q1 := queue(t, ds, librarian, thread, "q1", "only the red ones")
	if ev := eventOf(q1.log, substrate.AgentEventMessageQueued, "q1"); ev.Text != "only the red ones" || ev.Turn != 2 {
		t.Fatalf("the queued event = %+v, want the text at ordinal 2", ev)
	}
	if first := q1.log.snapshot()[0]; first.Kind != substrate.AgentEventThread || first.Thread != thread {
		t.Fatalf("a queued send's stream opened with %+v, want the thread event", first)
	}
	if got := threadProps(t, ds, thread)["status"]; got != threadRunning {
		t.Fatalf("thread status with a message queued = %v, want running", got)
	}
	if n := len(fake.requestsOf("lib")); n != 1 {
		t.Fatalf("%d model calls with a message queued, want the 1 in flight: a message adds no call", n)
	}
	// The call in flight goes on without it.
	if eventOf(q1.log, substrate.AgentEventMessageTaken, "q1") != nil {
		t.Fatal("the message was taken by a call that began before it was sent")
	}
	close(release1)
	waitArrived(t, arrived2, "the second call")
	wantConversation(t, "the call after the first tool result", fake.requestsOf("lib")[1],
		"user:list widgets", "assistant", "tool", "user:only the red ones")
	// Every stream of the turn is told, the first sender's and the second's.
	for _, log := range []*eventLog{turn.log, q1.log} {
		waitUntil(t, "the taken event", func() bool { return eventOf(log, substrate.AgentEventMessageTaken, "q1") != nil })
	}

	// Between two tool calls: sent during the second round.
	queue(t, ds, librarian, thread, "q2", "and the blue ones")
	close(release2)
	turn.log.waitFor(t, substrate.AgentEventDelta)
	wantConversation(t, "the call after the second tool result", fake.requestsOf("lib")[2],
		"user:list widgets", "assistant", "tool", "user:only the red ones", "assistant", "tool", "user:and the blue ones")

	// During the final answer: nothing reads it in this run.
	q3 := queue(t, ds, librarian, thread, "q3", "now count them")
	release3()
	res, err := turn.wait(t)
	if err != nil || res.Status != threadOK || res.Reply != "found none" || res.Turns != 3 {
		t.Fatalf("the steered run = %+v, %v, want ok after its own 3 turns", res, err)
	}
	if res, err := q3.wait(t); err != nil || res == nil || res.Reply != "found none" {
		t.Fatalf("the last message's stream ended %+v, %v, want the run's own done", res, err)
	}
	if eventOf(q3.log, substrate.AgentEventMessageTaken, "q3") != nil {
		t.Fatal("the run said it took a message sent during its final answer")
	}
	// The next turn is the thread's by the time the run says done, and its
	// stream says which message it starts from.
	following := newEventLog()
	attached := make(chan chatEnd, 1)
	go func() {
		res, err := ds.AttachAgentTurn(ctx, thread, following.emit)
		attached <- chatEnd{res, err}
	}()
	waitArrived(t, arrived4, "the next turn's call")
	waitUntil(t, "the next turn to say what it starts from", func() bool {
		return eventOf(following, substrate.AgentEventMessageTaken, "q3") != nil
	})
	close(release4)
	if next := <-attached; next.err != nil || next.res == nil || next.res.Reply != "counted: none" {
		t.Fatalf("the turn the last message started = %+v, %v", next.res, next.err)
	}
	waitUntil(t, "the next turn's settle", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK
	})
	wantConversation(t, "the next turn", fake.requestsOf("lib")[3],
		"user:list widgets", "assistant", "tool", "user:only the red ones", "assistant", "tool", "user:and the blue ones",
		"assistant", "user:now count them")

	// The stored rows read in the order things happened, each ordinal once:
	// a message sits after the reply that was streaming when it was sent.
	want := []string{
		"user:list widgets", "assistant", "user:only the red ones", "tool",
		"assistant", "user:and the blue ones", "tool",
		"assistant", "user:now count them", "assistant",
	}
	if got := rolesByTurn(t, ds, thread); !slices.Equal(got, want) {
		t.Fatalf("rows by ordinal\n  %v\nwant\n  %v", got, want)
	}
	// One run under its budgets, then the next: 3 turns and 1.
	if props := threadProps(t, ds, thread); intProp(props, "turns") != 4 || intProp(props, "toolCalls") != 2 {
		t.Fatalf("thread tally = %v turns, %v tool calls, want 4 and 2", props["turns"], props["toolCalls"])
	}
	if n := len(userRows(t, ds, thread)); n != 4 {
		t.Fatalf("%d user rows, want the 4 sent", n)
	}
}

// Several messages queued during one call are read together, in the order
// they were sent, and none of them twice.
func TestSeveralQueuedMessagesAreReadInOrder(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{"query", listWidgets}}, arrived: arrived, release: release},
		fakeTurn{content: "done"},
	)
	turn := startChat(ds, librarian, substrate.ChatInput{Message: "list widgets", MessageID: "m0"})
	waitArrived(t, arrived, "the first call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread
	queue(t, ds, librarian, thread, "q1", "first")
	queue(t, ds, librarian, thread, "q2", "second")
	queue(t, ds, librarian, thread, "q3", "third")
	close(release)
	if res, err := turn.wait(t); err != nil || res.Status != threadOK || res.Turns != 2 {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	reqs := fake.requestsOf("lib")
	if len(reqs) != 2 {
		t.Fatalf("%d model calls, want 2: queued messages add none", len(reqs))
	}
	wantConversation(t, "the second call", reqs[1],
		"user:list widgets", "assistant", "tool", "user:first", "user:second", "user:third")
	var taken []string
	for _, ev := range turn.log.snapshot() {
		if ev.Kind == substrate.AgentEventMessageTaken {
			taken = append(taken, ev.ID)
		}
	}
	if !slices.Equal(taken, []string{"q1", "q2", "q3"}) {
		t.Fatalf("taken events = %v, want each message once, in order", taken)
	}
}

// A message with the id of one already queued is that message: it writes
// nothing, and its stream is the running turn's.
func TestARepeatedMessageIDOnARunningThreadWritesNothing(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold}, fakeTurn{content: "and that"})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "hi", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread

	in := substrate.ChatInput{Thread: thread, Message: "and this", MessageID: "q1"}
	first := queue(t, ds, chatter, thread, in.MessageID, in.Message)
	again := startChat(ds, chatter, in)
	// The repeat attaches: it is told of the message as every stream is.
	waitUntil(t, "the repeat's replay", func() bool { return eventOf(again.log, substrate.AgentEventMessageQueued, "q1") != nil })
	if n := len(userRows(t, ds, thread)); n != 2 {
		t.Fatalf("%d user rows after a repeated queued send, want 2", n)
	}
	// The same id under another text is another message: refused.
	_, _, err := sendChat(ds, substrate.ChatInput{Thread: thread, Message: "something else", MessageID: "q1"})
	if !errors.Is(err, substrate.ErrMessageIDTaken) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a queued message's id under another text = %v, want the id refused as taken", err)
	}
	release()
	for _, r := range []*chatRun{turn, first, again} {
		if res, err := r.wait(t); err != nil || res == nil || res.Reply != "partial reply" {
			t.Fatalf("a stream of the turn ended %+v, %v", res, err)
		}
	}
	waitUntil(t, "the turn the message started", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && slices.Contains(assistantContents(t, ds, thread), "and that")
	})
	if n := len(fake.requestsOf("chat")); n != 2 {
		t.Fatalf("%d model calls, want 2: the message runs once", n)
	}
	// And once it has run, the same request again is one idle line.
	res, events, err := sendChat(ds, in)
	wantIdleRepeat(t, res, events, err, thread, threadOK)
	if n := len(userRows(t, ds, thread)); n != 2 {
		t.Fatalf("%d user rows after the repeat, want 2", n)
	}
}

// A stop is the later word: the run settles `stopped`, the queued message
// stays in the thread, and nothing starts until the person goes on.
func TestAStopKeepsAQueuedMessageForThePersonsNextWord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	hold, _ := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "tell me", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	q1 := queue(t, ds, chatter, thread, "q1", "not that one")

	res, err := ds.StopAgentTurn(ctx, substrate.ActorPerson, thread)
	if err != nil || res.Status != threadStopped {
		t.Fatalf("stop = %+v, %v", res, err)
	}
	if res, err := q1.wait(t); err != nil || res == nil || res.Status != threadStopped {
		t.Fatalf("the queued send's stream ended %+v, %v, want the stopped run's done", res, err)
	}
	holdFor(t, "the stopped thread with a message queued", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadStopped && len(fake.requestsOf("chat")) == 1
	})
	if got := rolesByTurn(t, ds, thread); !slices.Equal(got, []string{"user:tell me", "assistant", "user:not that one"}) {
		t.Fatalf("rows after the stop = %v", got)
	}

	// The person's Continue reads it: the conversation ends on their message,
	// so the engine adds no sentence of its own.
	fake.script("chat", fakeTurn{content: "the other one, then"})
	cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || cont.Status != threadOK || cont.Reply != "the other one, then" {
		t.Fatalf("continue past a stop with a message queued = %+v, %v", cont, err)
	}
	wantConversation(t, "the continued turn", fake.requestsOf("chat")[1], "user:tell me", "assistant", "user:not that one")
	if n := len(userRows(t, ds, thread)); n != 2 {
		t.Fatalf("%d user rows, want 2", n)
	}
}

// A run that fails keeps the queued message too, and its stream ends with
// the failure. Trying again reads it.
func TestAFailedRunKeepsAQueuedMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("chat", fakeTurn{status: 500, arrived: arrived, release: release})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "does it work", MessageID: "m0"})
	waitArrived(t, arrived, "the failing call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread
	q1 := queue(t, ds, chatter, thread, "q1", "and quickly")
	close(release)
	if _, err := turn.wait(t); err == nil {
		t.Fatal("the failed turn returned no error")
	}
	var failure *substrate.AgentFailure
	if _, err := q1.wait(t); !errors.As(err, &failure) {
		t.Fatalf("the queued send's stream ended %v, want the run's failure", err)
	}
	holdFor(t, "the failed thread with a message queued", func() bool {
		return threadProps(t, ds, thread)["status"] == threadError && len(fake.requestsOf("chat")) == 1
	})
	fake.script("chat", fakeTurn{content: "it does"})
	cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || cont.Reply != "it does" {
		t.Fatalf("try again = %+v, %v", cont, err)
	}
	wantConversation(t, "the retry", fake.requestsOf("chat")[1], "user:does it work", "user:and quickly")
}

// A budget that ends the run before it reads a queued message does not eat
// the message: it is the person's word to go on, so it starts the next turn
// under budgets of its own.
func TestAQueuedMessageStartsTheNextTurnWhenABudgetEndsTheRun(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	call := fakeTurn{calls: []fakeCall{{"annotate", `{"id":"01h2xcejqtf2nbrexx3vqjhp41"}`}}}
	last := call
	last.arrived, last.release = arrived, release
	// budgeter stops after two turns, both spent on tool calls.
	fake.script("budget", call, last, fakeTurn{content: "both are annotated"})
	turn := startChat(ds, crewPackage+"/budgeter", substrate.ChatInput{Message: "annotate twice", MessageID: "m0"})
	waitArrived(t, arrived, "the run's last allowed call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread
	queue(t, ds, crewPackage+"/budgeter", thread, "q1", "then tell me")
	close(release)
	res, err := turn.wait(t)
	if err != nil || res.Status != threadOverBudget || res.Cause != substrate.CauseTurnLimit || res.Turns != 2 {
		t.Fatalf("the budgeted run = %+v, %v, want overbudget at its own 2 turns", res, err)
	}
	waitUntil(t, "the turn the message started", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK
	})
	reqs := fake.requestsOf("budget")
	if len(reqs) != 3 {
		t.Fatalf("%d model calls, want 3", len(reqs))
	}
	wantConversation(t, "the next turn", reqs[2],
		"user:annotate twice", "assistant", "tool", "assistant", "tool", "user:then tell me")
}

// A message never ran twice and is never lost across a restart: the row is
// there, the thread says the run was cut, the same request again starts
// nothing, and the person's Continue reads it.
func TestAQueuedMessageHoldsAcrossARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, restart := restartableAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"never ", "finished"}, hold: hold})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "mid-turn", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	in := substrate.ChatInput{Thread: thread, Message: "and one more thing", MessageID: "q1"}
	queue(t, ds, chatter, thread, in.MessageID, in.Message)

	ds = restart()
	release()
	if got := threadProps(t, ds, thread)["status"]; got != threadStopped {
		t.Fatalf("thread status after the restart = %v, want stopped", got)
	}
	res, events, err := sendChat(ds, in)
	wantIdleRepeat(t, res, events, err, thread, threadStopped)
	holdFor(t, "the restarted server with a message queued", func() bool { return len(fake.requestsOf("chat")) == 1 })
	if got := rolesByTurn(t, ds, thread); !slices.Equal(got, []string{"user:mid-turn", "assistant", "user:and one more thing"}) {
		t.Fatalf("rows after the restart = %v", got)
	}
	fake.script("chat", fakeTurn{content: "here it is"})
	cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || cont.Reply != "here it is" {
		t.Fatalf("continue after the restart = %+v, %v", cont, err)
	}
	wantConversation(t, "the continued turn", fake.requestsOf("chat")[1], "user:mid-turn", "assistant", "user:and one more thing")
}

// A message with no id is refused on a running thread, as before: only a
// message that can be told from its repeat is written into a turn. So is a
// message into a thread that is no chat turn's: a call's, and a sub-agent's.
func TestOnlyAChatTurnTakesAMessageAndOnlyOneThatNamesItsID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "hi"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	_, _, err := sendChat(ds, substrate.ChatInput{Thread: thread, Message: "no id"})
	if !errors.Is(err, substrate.ErrThreadBusy) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a message with no id into a running thread = %v, want ErrThreadBusy", err)
	}
	// Another agent's thread is not this agent's to write into.
	_, err = ds.ChatAgent(ctx, substrate.ActorPerson, librarian, substrate.ChatInput{Thread: thread, Message: "wrong agent", MessageID: "x2"}, nil)
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "belongs to agent") {
		t.Fatalf("a message for another agent into a running thread = %v, want it refused as that agent's thread", err)
	}
	release()
	if _, err := turn.wait(t); err != nil {
		t.Fatal(err)
	}

	// A call's thread: running, with no chat turn behind it.
	arrived, held := make(chan struct{}), make(chan struct{})
	fake.script("chat", fakeTurn{content: "called", arrived: arrived, release: held})
	called := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, chatter, "a call")
		called <- err
	}()
	waitArrived(t, arrived, "the call")
	var callThread string
	for _, tr := range agentThreadsOf(t, ds, "chatter") {
		if tr["mode"] == "call" {
			callThread, _ = tr["__id"].(string)
		}
	}
	_, _, err = sendChat(ds, substrate.ChatInput{Thread: callThread, Message: "into a call", MessageID: "c1"})
	if !errors.Is(err, substrate.ErrThreadBusy) {
		t.Fatalf("a message into a running call's thread = %v, want ErrThreadBusy", err)
	}
	close(held)
	if err := waitTurn(t, called); err != nil {
		t.Fatal(err)
	}

	// A sub-agent's thread, while its parent's chat turn waits on it.
	childArrived := make(chan struct{})
	childHeld, releaseChild := heldTurn(t)
	fake.script("root", fakeTurn{calls: []fakeCall{{"scribe", `{"input":"summarize the widget"}`}}}, fakeTurn{content: "delegated"})
	fake.script("sub", fakeTurn{content: "child reply", arrived: childArrived, release: childHeld})
	parent := startChat(ds, crewPackage+"/classifier", substrate.ChatInput{Message: "delegate", MessageID: "p0"})
	waitArrived(t, childArrived, "the sub-agent's call")
	var child string
	for _, tr := range agentThreadsOf(t, ds, "scribe") {
		child, _ = tr["__id"].(string)
	}
	_, err = ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/scribe", substrate.ChatInput{Thread: child, Message: "into the child", MessageID: "s1"}, nil)
	if !errors.Is(err, substrate.ErrThreadBusy) {
		t.Fatalf("a message into a running sub-agent's thread = %v, want ErrThreadBusy", err)
	}
	for _, id := range []string{"c1", "s1", "x2"} {
		if row, err := ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: id}); err != nil || row != nil {
			t.Fatalf("refused message %s left a row: %+v, %v", id, row, err)
		}
	}
	releaseChild()
	if res, err := parent.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the parent's turn = %+v, %v", res, err)
	}
}

// A queued message takes no chat-turn slot: the run it joins holds one. The
// turn it starts takes the slot that run gives back, and waits for it.
func TestAQueuedMessageTakesNoSlotAndItsTurnWaitsForOne(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold}, fakeTurn{content: "and that"})
	// The person's own request, so its turn and the turn its message starts
	// count together.
	ctx := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	person := ds.turnPerson(ctx)
	turn := startChatOn(ctx, ds, chatter, substrate.ChatInput{Message: "hi", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	// The person runs as many turns as they may, this one among them.
	fillSlots(t, ds, person, ds.svc.chatTurnsPerPerson-1)
	q1 := startChatOn(ctx, ds, chatter, substrate.ChatInput{Thread: thread, Message: "and this", MessageID: "q1"})
	waitUntil(t, "the message to be queued", func() bool { return eventOf(q1.log, substrate.AgentEventMessageQueued, "q1") != nil })
	release()
	if res, err := turn.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	waitUntil(t, "the turn the message started, on the slot the run gave back", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && slices.Contains(assistantContents(t, ds, thread), "and that")
	})
	if !ds.chatTurns.take(person, ds.svc.chatTurnsPerPerson) || ds.chatTurns.take(person, ds.svc.chatTurnsPerPerson) {
		t.Fatal("the next turn's slot was not given back, or one too many was")
	}
}

// The turn a queued message would start may be refused (here: the provider's
// key is gone by then). The message is not lost: the thread settled fine
// with it unanswered, and a continue takes that up, which it refuses for a
// thread whose last message was answered.
func TestContinueTakesUpAMessageNoTurnAnswered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "hi", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	queue(t, ds, chatter, thread, "q1", "and this")
	key := func(value any) {
		t.Helper()
		if _, err := ds.Patch(ctx, substrate.ActorPerson, typeProvider, "chatllm", substrate.PatchInput{
			Properties: map[string]any{"apiKey": value},
		}); err != nil {
			t.Fatalf("set the provider key: %v", err)
		}
	}
	key(nil)
	release()
	if res, err := turn.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	holdFor(t, "the thread whose next turn was refused", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK && len(fake.requestsOf("chat")) == 1
	})
	key("row-key-chatllm")
	fake.script("chat", fakeTurn{content: "and that"}, fakeTurn{content: "unused"})
	cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || cont.Status != threadOK || cont.Reply != "and that" {
		t.Fatalf("continue a thread with an unanswered message = %+v, %v", cont, err)
	}
	wantConversation(t, "the continued turn", fake.requestsOf("chat")[1], "user:hi", "assistant", "user:and this")
	// Answered now: nothing is left to continue.
	if _, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil); !errors.Is(err, substrate.ErrNothingToContinue) {
		t.Fatalf("continue an answered thread = %v, want ErrNothingToContinue", err)
	}
}

// A turn that asked the person a question has settled: it is not running, so
// a typed message is an ordinary send. It starts a turn that reads the
// message, and it neither answers nor dismisses the question.
func TestAMessageToAThreadWaitingOnAnAnswerStartsATurnAndLeavesTheQuestion(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	fake.script("askm", fakeTurn{content: "take your time."})
	res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, crewPackage+"/poller",
		substrate.ChatInput{Thread: thread, Message: "let me think about it", MessageID: "w1"}, nil)
	if err != nil || res.Status != threadOK || res.Reply != "take your time." {
		t.Fatalf("a message to a thread waiting on an answer = %+v, %v", res, err)
	}
	rec, err := ds.Get(context.Background(), vocabulary.KindLLMInteraction, interaction.ID)
	if err != nil || rec.Properties["state"] != interactionPending {
		t.Fatalf("the question after a typed message = %+v, %v, want it still pending", rec, err)
	}
}

// Whoever sends a message a turn reads has continued that turn, and the rest
// of it is held to them too (decision 0276): a person's turn that an app's
// message joins is held to the app's grant from the call that reads the
// message, and not before.
func TestATurnIsHeldToWhoeverItReadAMessageFrom(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	write := fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}}
	arrived, release := make(chan struct{}), make(chan struct{})
	first := write
	first.arrived, first.release = arrived, release
	r.fake.script("scribe", first, write, fakeTurn{content: "done."})
	ada := r.person(r.adaID, substrate.ActorPerson)
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := r.ds.ChatAgent(ada, substrate.ActorPerson, r.pkg+"/scribe", substrate.ChatInput{Message: "go", MessageID: "m0"}, log.emit)
		turn <- err
	}()
	waitArrived(t, arrived, "the person's turn")
	thread := log.waitFor(t, substrate.AgentEventThread).Thread

	// An app that may talk to the agent and read what it reads, and write
	// nothing, sends into the turn.
	app, appCtx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	joined := newEventLog()
	go func() {
		_, _ = r.ds.ChatAgent(appCtx, app, r.pkg+"/scribe", substrate.ChatInput{Thread: thread, Message: "and another", MessageID: "q1"}, joined.emit)
	}()
	waitUntil(t, "the app's message to be queued", func() bool { return eventOf(joined, substrate.AgentEventMessageQueued, "q1") != nil })
	close(release)
	if err := waitTurn(t, turn); err != nil {
		t.Fatal(err)
	}
	rows := toolRows(t, r.ds, thread)
	if len(rows) != 2 {
		t.Fatalf("%d tool rows, want the two writes", len(rows))
	}
	// The call the model made before it read the app's message is the
	// person's alone. The one after is the app's too, whose grant lacks it.
	if rows[0]["ok"] != true || !refusedRow(rows[1]) || r.notes(t) != 1 {
		t.Fatalf("the writes around the app's message = %v, %v with %d notes: want the first made and the second refused", rows[0], rows[1], r.notes(t))
	}
	// The message's own row names its sender, and the refused call's row
	// names the app's token among whom the turn is held to.
	if held := r.heldRaw(t, typeMessage, "q1"); !strings.Contains(held, token) {
		t.Fatalf("the queued message is held to %s, want the app's token %s", held, token)
	}
}

// A write whose commit landed and whose answer was lost (decision 0271) is a
// message all the same: the loop is told of it, reads it at its next call,
// and the request that wrote it is answered as a repeat of itself.
func TestAQueuedMessageWhoseCommitAnswerWasLostIsStillRead(t *testing.T) {
	t.Parallel()
	var lose atomic.Bool
	ds := openInternalDataset(t, WithTestCommitInDoubt(lose.Load))
	fake := provisionAgents(t, ds)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{"query", listWidgets}}, arrived: arrived, release: release},
		fakeTurn{content: "done"},
	)
	turn := startChat(ds, librarian, substrate.ChatInput{Message: "list widgets", MessageID: "m0"})
	waitArrived(t, arrived, "the first call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread

	// The run is held at its model call, so the one commit in doubt is the
	// message's.
	lose.Store(true)
	q1 := startChat(ds, librarian, substrate.ChatInput{Thread: thread, Message: "only the red ones", MessageID: "q1"})
	waitUntil(t, "the message to be queued", func() bool { return eventOf(q1.log, substrate.AgentEventMessageQueued, "q1") != nil })
	lose.Store(false)
	if n := len(userRows(t, ds, thread)); n != 2 {
		t.Fatalf("%d user rows after a commit in doubt, want the message's too", n)
	}
	close(release)
	if res, err := turn.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	if res, err := q1.wait(t); err != nil || res == nil || res.Status != threadOK {
		t.Fatalf("the request whose commit was in doubt ended %+v, %v, want the turn's done", res, err)
	}
	reqs := fake.requestsOf("lib")
	if len(reqs) != 2 {
		t.Fatalf("%d model calls, want 2", len(reqs))
	}
	wantConversation(t, "the call after the tool result", reqs[1],
		"user:list widgets", "assistant", "tool", "user:only the red ones")
	if n := len(userRows(t, ds, thread)); n != 2 {
		t.Fatalf("%d user rows, want 2: the message is written once", n)
	}
}

// The run's last turn under its deadline reads what was sent, before it is
// told the turn is its last: a reply written past a message's ordinal is one
// that read it, so no call leaves a message behind it unread.
func TestTheLastTurnUnderADeadlineReadsAQueuedMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionDeadlineAgents(t)
	chronicler := deadlinePackage + "/chronicler"
	held, release := heldTurn(t)
	arrived := make(chan struct{})
	fake.script("dchron",
		fakeTurn{calls: []fakeCall{{"napper", `{"input":"nap"}`}}},
		fakeTurn{content: "noted, briefly"},
	)
	fake.script("dnap", fakeTurn{content: "rested", arrived: arrived, release: held})
	wait := releaseNearTheDeadline(ds, "chronicler", arrived, release)
	turn := startChat(ds, chronicler, substrate.ChatInput{Message: "take a nap", MessageID: "m0"})
	waitArrived(t, arrived, "the slow tool")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread
	q1 := queue(t, ds, chronicler, thread, "q1", "and report back")
	wait()
	res, err := turn.wait(t)
	if err != nil || res.Status != threadOK || res.Reply != "noted, briefly" {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	reqs := fake.requestsOf("dchron")
	if len(reqs) != 2 {
		t.Fatalf("%d model calls, want 2", len(reqs))
	}
	msgs := requestMessages(reqs[1])
	if n := len(msgs); n < 3 || msgs[n-1]["content"] != finalTurnNudge || msgText(msgs[n-2]) != "and report back" {
		t.Fatalf("the last turn's request ended %v, want the message and then the word that the turn is the last", msgs[max(0, len(msgs)-2):])
	}
	if eventOf(q1.log, substrate.AgentEventMessageTaken, "q1") == nil {
		t.Fatal("the last turn read the message and no stream said so")
	}
	// Read, so it starts no turn of its own.
	holdFor(t, "the settled thread", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK && len(fake.requestsOf("dchron")) == 2
	})
	if _, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil); !errors.Is(err, substrate.ErrNothingToContinue) {
		t.Fatalf("continue after the last turn read the message = %v, want ErrNothingToContinue", err)
	}
}

// The turn that unread messages start reads all of them, so it is held to
// every sender, not to the first: an app's message that waits beside a
// person's holds that turn to the app's grant too.
func TestTheNextTurnIsHeldToEverySenderItStartsFrom(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	hold, release := heldTurn(t)
	write := fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}}
	r.fake.script("scribe", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold}, write, fakeTurn{content: "done."})
	ada := r.person(r.adaID, substrate.ActorPerson)
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := r.ds.ChatAgent(ada, substrate.ActorPerson, r.pkg+"/scribe", substrate.ChatInput{Message: "go", MessageID: "m0"}, log.emit)
		turn <- err
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread

	// During the final answer: the person's own message, then an app's that
	// may talk to the agent and write nothing.
	send := func(ctx context.Context, actor substrate.Actor, id string) {
		t.Helper()
		sent := newEventLog()
		go func() {
			_, _ = r.ds.ChatAgent(ctx, actor, r.pkg+"/scribe", substrate.ChatInput{Thread: thread, Message: "write it", MessageID: id}, sent.emit)
		}()
		waitUntil(t, "message "+id+" to be queued", func() bool { return eventOf(sent, substrate.AgentEventMessageQueued, id) != nil })
	}
	send(ada, substrate.ActorPerson, "q1")
	app, appCtx, _ := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	send(appCtx, app, "q2")
	release()
	if err := waitTurn(t, turn); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the turn the two messages started", func() bool {
		_, live := r.ds.runningThreads.Load(thread)
		return !live && len(r.fake.requestsOf("scribe")) == 3
	})
	rows := toolRows(t, r.ds, thread)
	if len(rows) != 1 || !refusedRow(rows[0]) || r.notes(t) != 0 {
		t.Fatalf("the next turn's write = %v with %d notes: want it refused by the app's grant", rows, r.notes(t))
	}
}

// replyRow is the id of the thread's newest assistant row.
func replyRow(t *testing.T, ds *dataset, thread string) string {
	t.Helper()
	newest, at := "", -1
	for _, m := range threadMessagesWithIDs(t, ds, thread) {
		if m["role"] == "assistant" && intProp(m, "turn") > at {
			newest, at = m["__id"].(string), intProp(m, "turn")
		}
	}
	if newest == "" {
		t.Fatalf("thread %s holds no reply", thread)
	}
	return newest
}

// loseTheRun makes the thread what a crash leaves: `running` under an
// expired lease, with no loop behind it and no `lastMessage` stamped by the
// settle that never ran. The next sweep settles it from its rows alone.
func loseTheRun(t *testing.T, ds *dataset, thread string) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.db.ExecContext(ctx, `UPDATE records SET props = (props - 'lastMessage') || '{"status":"running","leaseUntil":"2001-01-01T00:00:00Z"}'::jsonb
		WHERE kind = $1 AND id = $2`, typeThread, thread); err != nil {
		t.Fatal(err)
	}
	if err := ds.settleLostThreads(ctx); err != nil {
		t.Fatalf("settle the lost run: %v", err)
	}
	if status, _ := threadStatusOf(t, ds, thread); status != threadError {
		t.Fatalf("the lost run settled %s", status)
	}
}

// A message a run never read is its sender's own word with no answer: it is
// not what the chat is unread for. `lastMessage` keeps naming the newest
// row that is not such a message, at the loop's settle and at the recovery's
// after a crash, which has only the rows to go by; and a message is none of
// the things that wait on a person.
func TestAQueuedMessageIsNotTheThreadsLastMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "one"})
	res, _, err := sendChat(ds, substrate.ChatInput{Message: "hi", MessageID: "m0"})
	if err != nil {
		t.Fatal(err)
	}
	thread, read := res.Thread, replyRow(t, ds, res.Thread)
	if got := lastMessageOf(t, ds, thread); got != read {
		t.Fatalf("after a reply lastMessage is %s, want the reply %s", got, read)
	}

	// A turn stopped before it wrote anything, with a message sent meanwhile:
	// the thread holds two messages no reply came after, and still names the
	// reply its person has read.
	arrived, held := make(chan struct{}), make(chan struct{})
	fake.script("chat", fakeTurn{content: "never sent", arrived: arrived, release: held})
	turn := startChat(ds, chatter, substrate.ChatInput{Thread: thread, Message: "second", MessageID: "m1"})
	waitArrived(t, arrived, "the held call")
	queue(t, ds, chatter, thread, "q1", "not that one")
	if stopped, err := ds.StopAgentTurn(ctx, substrate.ActorPerson, thread); err != nil || stopped.Status != threadStopped {
		t.Fatalf("stop = %+v, %v", stopped, err)
	}
	close(held)
	if _, err := turn.wait(t); err != nil {
		t.Fatal(err)
	}
	if got := lastMessageOf(t, ds, thread); got != read {
		t.Fatalf("after a stop that wrote nothing, with a message queued, lastMessage is %s, want still the reply %s", got, read)
	}
	if n := waitingOf(t, ds, thread); n != 0 {
		t.Fatalf("a queued message counts as %d things waiting on a person", n)
	}
	// The same rows after a crash: no loop is left to say which were read.
	loseTheRun(t, ds, thread)
	if got := lastMessageOf(t, ds, thread); got != read {
		t.Fatalf("after the recovery's settle lastMessage is %s, want still the reply %s", got, read)
	}

	// A turn stopped mid-reply with a message queued: what streamed is the
	// news, and the message after it is not.
	hold, _ := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold})
	turn = startChat(ds, chatter, substrate.ChatInput{Thread: thread, Message: "third", MessageID: "m2"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	queue(t, ds, chatter, thread, "q2", "wait")
	if _, err := ds.StopAgentTurn(ctx, substrate.ActorPerson, thread); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := turn.wait(t); err != nil {
		t.Fatal(err)
	}
	partial := replyRow(t, ds, thread)
	if got := lastMessageOf(t, ds, thread); got != partial || partial == read {
		t.Fatalf("after a stop mid-reply lastMessage is %s, want the reply cut short %s", got, partial)
	}
	loseTheRun(t, ds, thread)
	if got := lastMessageOf(t, ds, thread); got != partial {
		t.Fatalf("after the recovery's settle lastMessage is %s, want the reply cut short %s", got, partial)
	}
	if n := waitingOf(t, ds, thread); n != 0 {
		t.Fatalf("queued messages count as %d things waiting on a person", n)
	}

	// A row from before rows carried an ordinal is no message waiting for an
	// answer: it does not come before the reply.
	if _, err := ds.putInternal(ctx, substrate.ActorSystem, substrate.PutInput{Kind: typeMessage, ID: fid("no-ordinal"), Properties: map[string]any{
		"role": "user", "content": "from before ordinals", "turn": 0, msgRelThread: thread,
	}}); err != nil {
		t.Fatalf("plant a row: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE records SET props = props - 'turn' WHERE kind = $1 AND id = $2`, typeMessage, fid("no-ordinal")); err != nil {
		t.Fatal(err)
	}
	loseTheRun(t, ds, thread)
	if got := lastMessageOf(t, ds, thread); got != partial {
		t.Fatalf("with a user row that carries no ordinal lastMessage is %s, want the reply %s", got, partial)
	}

	// It never moves back. A thread may name a message this rule passes
	// over (the upgrade seeded the newest row, whatever it was), and a mark
	// that reads through it could not follow it to an earlier one: a settle
	// that writes nothing newer leaves it where it is.
	if _, err := ds.db.ExecContext(ctx, `UPDATE records SET props = props || jsonb_build_object('lastMessage', jsonb_build_object('ref', $3::text))
		WHERE kind = $1 AND id = $2`, typeThread, thread, vocabulary.RecordPath(typeMessage, "q2")); err != nil {
		t.Fatal(err)
	}
	fake.script("chat", fakeTurn{status: 500})
	if _, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil); err == nil {
		t.Fatal("the failing continue answered no error")
	}
	if got := lastMessageOf(t, ds, thread); got != "q2" {
		t.Fatalf("after a settle that wrote nothing newer lastMessage went back to %s, want it still %s", got, "q2")
	}
	// A later reply takes its place.
	fake.script("chat", fakeTurn{content: "here"})
	if cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil); err != nil || cont.Reply != "here" {
		t.Fatalf("continue = %+v, %v", cont, err)
	}
	if got := lastMessageOf(t, ds, thread); got != replyRow(t, ds, thread) || got == partial {
		t.Fatalf("after a reply lastMessage is %s, want the reply %s", got, replyRow(t, ds, thread))
	}
}

// The turn waiting messages start is held to the senders its starter read,
// so it claims the thread only while those very messages wait there. Where
// another turn has read them and left one of another sender's, it runs
// nothing: that turn's own settle starts what its message asks for.
func TestTheNextTurnRunsOnlyForTheMessagesItWasStartedFor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{status: 500})
	if _, _, err := sendChat(ds, substrate.ChatInput{Message: "hi", MessageID: "m0"}); err == nil {
		t.Fatal("the failing turn answered no error")
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: "m0"})
	if err != nil || row == nil {
		t.Fatalf("the message: %v", err)
	}
	thread := referenceID(row.Props[msgRelThread])
	ag, err := ds.registry().ResolveAgent(chatter)
	if err != nil {
		t.Fatal(err)
	}
	start := func(status string, startsFrom ...string) error {
		t.Helper()
		if _, err := ds.db.ExecContext(ctx, `UPDATE records SET props = props || jsonb_build_object('status', $3::text) WHERE kind = $1 AND id = $2`,
			typeThread, thread, status); err != nil {
			t.Fatal(err)
		}
		held := substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
		in := agentInvocation{mode: agentModeChat, threadID: thread, resume: true, unanswered: true, startsFrom: startsFrom}
		return ds.resumeChatTurn(held, ag, in, deferredResume{thread: thread, resolution: "m0", sent: true}, nil)
	}
	// Other messages than the ones waiting, and a thread that did not settle
	// on its own: nothing runs.
	for what, err := range map[string]error{
		"a turn started for another message":      start(threadOK, "another"),
		"a turn started for one message of two":   start(threadOK),
		"a turn started on a thread that failed":  start(threadError, "m0"),
		"a turn started on a thread a stop holds": start(threadStopped, "m0"),
	} {
		if !errors.Is(err, errNothingUnanswered) && !errors.Is(err, errResumeStopped) {
			t.Errorf("%s = %v, want it refused", what, err)
		}
	}
	if n := len(fake.requestsOf("chat")); n != 1 {
		t.Fatalf("%d model calls, want only the first turn's", n)
	}
	// The very messages, on a thread that settled on its own: it runs.
	fake.script("chat", fakeTurn{content: "hello"})
	if err := start(threadOK, "m0"); err != nil {
		t.Fatalf("the turn started for the message that waits: %v", err)
	}
	if !slices.Contains(assistantContents(t, ds, thread), "hello") {
		t.Fatal("the turn did not answer")
	}
}

// A run that ends on its own with a message unread names its own reply, and
// the turn the message starts then names its: the chat is unread for a
// reply each time, never for the message between them.
func TestTheNextTurnMovesLastMessageToItsReply(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	arrived, held := make(chan struct{}), make(chan struct{})
	fake.script("chat",
		fakeTurn{pieces: []string{"first ", "reply"}, hold: hold},
		fakeTurn{content: "second reply", arrived: arrived, release: held},
	)
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "hi", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	queue(t, ds, chatter, thread, "q1", "and this")
	release()
	if res, err := turn.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	// The next turn is held at its call: the thread still names the first
	// run's reply, though the message's ordinal is past it.
	waitArrived(t, arrived, "the next turn's call")
	first := replyRow(t, ds, thread)
	if got := lastMessageOf(t, ds, thread); got != first {
		t.Fatalf("with the message's turn running lastMessage is %s, want the first reply %s", got, first)
	}
	close(held)
	waitUntil(t, "the next turn's settle", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK
	})
	if second := replyRow(t, ds, thread); second == first || lastMessageOf(t, ds, thread) != second {
		t.Fatalf("after the next turn lastMessage is %s, want its reply %s", lastMessageOf(t, ds, thread), second)
	}
}

// The turn a message asked for is started from memory by the settle that
// left it unread. A process that ends between the two leaves an `ok` thread
// with an unanswered message, which the next process finds from the rows at
// its first sweep and answers, once.
func TestAnUnansweredMessageGetsItsTurnAfterARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, restart := restartableAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold})
	turn := startChat(ds, chatter, substrate.ChatInput{Message: "hi", MessageID: "m0"})
	turn.log.waitFor(t, substrate.AgentEventDelta)
	thread := turn.log.snapshot()[0].Thread
	queue(t, ds, chatter, thread, "q1", "and this")
	// The turn the message starts is refused in this process (no key), which
	// is what a process that ended before starting it leaves behind.
	key := func(ds *dataset, value any) {
		t.Helper()
		if _, err := ds.Patch(ctx, substrate.ActorPerson, typeProvider, "chatllm", substrate.PatchInput{
			Properties: map[string]any{"apiKey": value},
		}); err != nil {
			t.Fatalf("set the provider key: %v", err)
		}
	}
	key(ds, nil)
	release()
	if res, err := turn.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	holdFor(t, "the thread whose next turn did not start", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK && len(fake.requestsOf("chat")) == 1
	})
	key(ds, "row-key-chatllm")

	ds = restart()
	fake.script("chat", fakeTurn{content: "and that"})
	if n, err := ds.SweepResolutions(ctx); err != nil || n != 1 {
		t.Fatalf("the first sweep after the restart started %d turns, %v: want the message's", n, err)
	}
	waitUntil(t, "the turn the message asked for", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && slices.Contains(assistantContents(t, ds, thread), "and that")
	})
	wantConversation(t, "the turn started at the sweep", fake.requestsOf("chat")[1], "user:hi", "assistant", "user:and this")
	// Answered, and looked for once: a second sweep starts nothing.
	if n, err := ds.SweepResolutions(ctx); err != nil || n != 0 {
		t.Fatalf("a second sweep started %d turns, %v", n, err)
	}
	if n := len(fake.requestsOf("chat")); n != 2 {
		t.Fatalf("%d model calls, want 2", n)
	}
}

// The turn two waiting messages start reads its pinned context under both
// senders, not under the first alone: where the second sender's token is
// gone by then, nothing that token's work could not read reaches the model.
func TestTheNextTurnReadsItsPinnedContextUnderEverySender(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	const pinned = "pinned-for-the-person"
	if _, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: r.note, ID: fid("pinned"), Properties: map[string]any{"name": pinned}}); err != nil {
		t.Fatal(err)
	}
	hold, release := heldTurn(t)
	r.fake.script("notepinner", fakeTurn{pieces: []string{"partial ", "reply"}, hold: hold}, fakeTurn{content: "next."})
	ada := r.adaCtx()
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := r.ds.ChatAgent(ada, substrate.ActorPerson, r.pkg+"/notepinner", substrate.ChatInput{Message: "go", MessageID: "m0"}, log.emit)
		turn <- err
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread
	if reqs := r.fake.requestsOf("notepinner"); len(reqs) != 1 || !requestCarries(reqs[0], pinned) {
		t.Fatal("the person's turn did not pin the note: the test proves nothing")
	}
	send := func(ctx context.Context, actor substrate.Actor, id string) {
		t.Helper()
		sent := newEventLog()
		go func() {
			_, _ = r.ds.ChatAgent(ctx, actor, r.pkg+"/notepinner", substrate.ChatInput{Thread: thread, Message: "and more", MessageID: id}, sent.emit)
		}()
		waitUntil(t, "message "+id+" to be queued", func() bool { return eventOf(sent, substrate.AgentEventMessageQueued, id) != nil })
	}
	send(ada, substrate.ActorPerson, "q1")
	app, appCtx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	send(appCtx, app, "q2")
	// The app's token goes before the turn its message waits for starts.
	if _, err := r.ds.Delete(r.adaCtx(), substrate.ActorPerson, kindToken, token, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	release()
	if err := waitTurn(t, turn); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the turn the two messages started", func() bool {
		_, live := r.ds.runningThreads.Load(thread)
		return !live && len(r.fake.requestsOf("notepinner")) == 2
	})
	if requestCarries(r.fake.requestsOf("notepinner")[1], pinned) {
		t.Fatal("the next turn replayed the block the person's turn pinned, to a turn held to a token that is gone")
	}
}

// A write left in doubt whose row cannot be read at once stays in doubt, in
// the queue. The next call settles it by reading the row before it goes on,
// so the message is read like any other.
func TestAMessageInDoubtIsSettledByTheNextCall(t *testing.T) {
	t.Parallel()
	var lose, unreadable atomic.Bool
	ds := openInternalDataset(t, WithTestCommitInDoubt(lose.Load))
	ds.queuedRowRead = func(ctx context.Context, id string) (*erow, error) {
		if unreadable.Load() {
			return nil, errors.New("the row could not be read")
		}
		return ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: id})
	}
	fake := provisionAgents(t, ds)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("lib",
		fakeTurn{calls: []fakeCall{{"query", listWidgets}}, arrived: arrived, release: release},
		fakeTurn{content: "done"},
	)
	turn := startChat(ds, librarian, substrate.ChatInput{Message: "list widgets", MessageID: "m0"})
	waitArrived(t, arrived, "the first call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread

	lose.Store(true)
	unreadable.Store(true)
	q1 := startChat(ds, librarian, substrate.ChatInput{Thread: thread, Message: "only the red ones", MessageID: "q1"})
	// The request is answered as a repeat of itself: the row is there.
	q1.log.waitFor(t, substrate.AgentEventThread)
	lose.Store(false)
	if eventOf(turn.log, substrate.AgentEventMessageQueued, "q1") != nil {
		t.Fatal("a message in doubt was announced before its row was read")
	}
	unreadable.Store(false)
	close(release)
	if res, err := turn.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	wantConversation(t, "the call after the tool result", fake.requestsOf("lib")[1],
		"user:list widgets", "assistant", "tool", "user:only the red ones")
	if eventOf(turn.log, substrate.AgentEventMessageQueued, "q1") == nil || eventOf(turn.log, substrate.AgentEventMessageTaken, "q1") == nil {
		t.Fatal("the message was read and the stream did not say it was queued and taken")
	}
}

// Where the row of a message in doubt still cannot be read at the next call,
// the run ends there: a reply written now would take an ordinal past a
// message it may not have read, and the message would read as answered.
func TestARunEndsRatherThanReplyPastAMessageItCannotRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var lose, unreadable atomic.Bool
	ds := openInternalDataset(t, WithTestCommitInDoubt(lose.Load))
	ds.queuedRowRead = func(ctx context.Context, id string) (*erow, error) {
		if unreadable.Load() {
			return nil, errors.New("the row could not be read")
		}
		return ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: id})
	}
	fake := provisionAgents(t, ds)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("lib", fakeTurn{calls: []fakeCall{{"query", listWidgets}}, arrived: arrived, release: release})
	turn := startChat(ds, librarian, substrate.ChatInput{Message: "list widgets", MessageID: "m0"})
	waitArrived(t, arrived, "the first call")
	thread := turn.log.waitFor(t, substrate.AgentEventThread).Thread

	lose.Store(true)
	unreadable.Store(true)
	q1 := startChat(ds, librarian, substrate.ChatInput{Thread: thread, Message: "only the red ones", MessageID: "q1"})
	q1.log.waitFor(t, substrate.AgentEventThread)
	lose.Store(false)
	close(release)
	var failure *substrate.AgentFailure
	if _, err := turn.wait(t); !errors.As(err, &failure) || failure.Cause != substrate.CauseInternal {
		t.Fatalf("the run with a message it could not read ended %v, want an internal failure", err)
	}
	if status, _ := threadStatusOf(t, ds, thread); status != threadError {
		t.Fatalf("the thread settled %s, want error", status)
	}
	if n := len(fake.requestsOf("lib")); n != 1 {
		t.Fatalf("%d model calls, want 1: no call was made past the message", n)
	}
	// Nothing reads as an answer to it: Try again reads it.
	unreadable.Store(false)
	fake.script("lib", fakeTurn{content: "the red ones"})
	cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || cont.Reply != "the red ones" {
		t.Fatalf("try again = %+v, %v", cont, err)
	}
	wantConversation(t, "the retry", fake.requestsOf("lib")[1],
		"user:list widgets", "assistant", "tool", "user:only the red ones")
}
