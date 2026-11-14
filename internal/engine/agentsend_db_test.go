package engine

// A chat message that names its own id, and a continue that writes no
// message (decision 0312): the user row lands under the id its client chose,
// a request that repeats the id writes nothing and runs nothing, on a new
// chat, on an old one, across a restart and when two copies race, and a
// thread is continued from its stored rows alone.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const chatter = crewPackage + "/chatter"

// sendChat runs one chat request to its end and answers its events.
func sendChat(ds *dataset, in substrate.ChatInput) (*substrate.AgentResult, []substrate.AgentEvent, error) {
	log := newEventLog()
	res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, chatter, in, log.emit)
	return res, log.snapshot(), err
}

// userRows are the thread's user messages.
func userRows(t *testing.T, ds *dataset, thread string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "user" {
			out = append(out, m)
		}
	}
	return out
}

// wantIdleRepeat holds a repeated send's events to the thread event and the
// one idle line.
func wantIdleRepeat(t *testing.T, res *substrate.AgentResult, events []substrate.AgentEvent, err error, thread, status string) {
	t.Helper()
	if err != nil || res != nil {
		t.Fatalf("a repeated send = %+v, %v, want no result and no error", res, err)
	}
	if len(events) != 2 || events[0].Kind != substrate.AgentEventThread || events[0].Thread != thread ||
		events[1].Kind != substrate.AgentEventIdle || events[1].Thread != thread || events[1].Status != status {
		t.Fatalf("a repeated send's events = %+v, want thread %s then idle %s", events, thread, status)
	}
}

func TestAMessageIsWrittenUnderTheIdItsClientChose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "one"}, fakeTurn{content: "two"})

	res, _, err := sendChat(ds, substrate.ChatInput{Message: "first", MessageID: "c41a7e9b02"})
	if err != nil {
		t.Fatalf("a new chat under a chosen id: %v", err)
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: "c41a7e9b02"})
	if err != nil || row == nil {
		t.Fatalf("the message under its chosen id: %v, %v", row, err)
	}
	if row.Props["role"] != "user" || row.Props["content"] != "first" || intProp(row.Props, "turn") != 0 ||
		referenceID(row.Props[msgRelThread]) != res.Thread {
		t.Fatalf("the chosen row = %+v, want the user message at turn 0 of %s", row.Props, res.Thread)
	}

	res2, _, err := sendChat(ds, substrate.ChatInput{Thread: res.Thread, Message: "second", MessageID: "5b90e3c1f2"})
	if err != nil || res2.Reply != "two" {
		t.Fatalf("a second message under a chosen id = %+v, %v", res2, err)
	}
	msgs := threadMessages(t, ds, res.Thread)
	if len(msgs) != 4 {
		t.Fatalf("the thread holds %d rows, want user, assistant, user, assistant", len(msgs))
	}
	for i, m := range msgs {
		if intProp(m, "turn") != i {
			t.Fatalf("row %d has turn %v: the chosen row must take the thread's next ordinal", i, m["turn"])
		}
	}
	// The row is the person's, as a message with no chosen id is, though it
	// lands in the transaction the agent's claim runs in.
	fake.script("chat", fakeTurn{content: "three"})
	if _, _, err := sendChat(ds, substrate.ChatInput{Thread: res.Thread, Message: "third"}); err != nil {
		t.Fatal(err)
	}
	writer := func(id string) string {
		var actor string
		if err := ds.db.QueryRowContext(ctx, `SELECT actor FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`,
			typeMessage, id).Scan(&actor); err != nil {
			t.Fatalf("the writer of message %s: %v", id, err)
		}
		return actor
	}
	rows := threadMessagesWithIDs(t, ds, res.Thread)
	plain, reply := writer(rows[4]["__id"].(string)), writer(rows[5]["__id"].(string))
	if got := writer("5b90e3c1f2"); got != plain || got == reply {
		t.Fatalf("the chosen row's writer = %s, want the person's (%s), not the agent's (%s)", got, plain, reply)
	}
	// The model saw each message once, in order, the second after the first
	// exchange.
	reqs := fake.requestsOf("chat")
	if len(reqs) != 3 {
		t.Fatalf("%d model calls, want 3", len(reqs))
	}
	var said []string
	for _, m := range reqs[1]["messages"].([]any) {
		msg := m.(map[string]any)
		if msg["role"] != "system" {
			said = append(said, msg["role"].(string))
		}
	}
	if strings.Join(said, ",") != "user,assistant,user" {
		t.Fatalf("the second call's conversation = %v, want user, assistant, user", said)
	}
}

func TestARepeatedSendWritesNothingAndRunsNothing(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "one"}, fakeTurn{content: "two"})

	opening := substrate.ChatInput{Message: "first", MessageID: "open-1"}
	res, _, err := sendChat(ds, opening)
	if err != nil {
		t.Fatal(err)
	}
	// The first send of a chat, repeated, opens no second thread.
	again, events, err := sendChat(ds, opening)
	wantIdleRepeat(t, again, events, err, res.Thread, threadOK)
	if threads := agentThreadsOf(t, ds, "chatter"); len(threads) != 1 {
		t.Fatalf("%d threads after the opening send was repeated, want 1", len(threads))
	}

	next := substrate.ChatInput{Thread: res.Thread, Message: "second", MessageID: "next-1"}
	if _, _, err := sendChat(ds, next); err != nil {
		t.Fatal(err)
	}
	again, events, err = sendChat(ds, next)
	wantIdleRepeat(t, again, events, err, res.Thread, threadOK)
	// And the opening one still answers, though it is no longer the newest.
	again, events, err = sendChat(ds, opening)
	wantIdleRepeat(t, again, events, err, res.Thread, threadOK)

	if rows := userRows(t, ds, res.Thread); len(rows) != 2 {
		t.Fatalf("%d user rows after three repeats, want the 2 that were sent", len(rows))
	}
	if n := len(fake.requestsOf("chat")); n != 2 {
		t.Fatalf("%d model calls after three repeats, want 2", n)
	}
}

func TestARepeatedSendAttachesToTheTurnItStarted(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{content: "partial reply", hold: hold})

	in := substrate.ChatInput{Message: "hi", MessageID: "held-1"}
	first, firstLog := make(chan *substrate.AgentResult, 1), newEventLog()
	go func() {
		res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, chatter, in, firstLog.emit)
		if err != nil {
			t.Errorf("the first send: %v", err)
		}
		first <- res
	}()
	firstLog.waitFor(t, substrate.AgentEventDelta)
	thread := firstLog.snapshot()[0].Thread

	repeat, repeatLog := make(chan *substrate.AgentResult, 1), newEventLog()
	go func() {
		res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, chatter, in, repeatLog.emit)
		if err != nil {
			t.Errorf("the repeated send: %v", err)
		}
		repeat <- res
	}()
	// The replay: the thread event first, then what the turn streamed so far.
	if ev := repeatLog.waitFor(t, substrate.AgentEventDelta); ev.Text != "partia" {
		t.Fatalf("the repeat's first delta = %q, want the turn's replay", ev.Text)
	}
	if ev := repeatLog.snapshot()[0]; ev.Kind != substrate.AgentEventThread || ev.Thread != thread {
		t.Fatalf("the repeat opened with %+v, want the thread event of %s", ev, thread)
	}
	release()
	a, b := <-first, <-repeat
	if a == nil || b == nil || a.Reply != "partial reply" || b.Reply != a.Reply || b.Thread != thread {
		t.Fatalf("the two streams settled %+v and %+v, want one turn's result twice", a, b)
	}
	if n := len(fake.requestsOf("chat")); n != 1 {
		t.Fatalf("%d model calls, want 1", n)
	}
	if rows := userRows(t, ds, thread); len(rows) != 1 {
		t.Fatalf("%d user rows, want 1", len(rows))
	}
}

// Copies of one send that race: one turn runs, one row is written, and every
// copy is answered about the same thread.
func TestCopiesOfASendRaceToOneTurn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		old  bool
	}{{"a new chat", false}, {"an old chat", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds, fake := openAgentDataset(t)
			in := substrate.ChatInput{Message: "racing", MessageID: "race-1"}
			calls := 1
			if tc.old {
				fake.script("chat", fakeTurn{content: "before"})
				res, _, err := sendChat(ds, substrate.ChatInput{Message: "earlier"})
				if err != nil {
					t.Fatal(err)
				}
				in.Thread = res.Thread
				calls = 2
			}
			fake.script("chat", fakeTurn{content: "the one answer"})

			const copies = 3
			var wg sync.WaitGroup
			threads := make([]string, copies)
			errs := make([]error, copies)
			start := make(chan struct{})
			for i := range copies {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, events, err := sendChat(ds, in)
					errs[i] = err
					if len(events) > 0 && events[0].Kind == substrate.AgentEventThread {
						threads[i] = events[0].Thread
					}
				}()
			}
			close(start)
			wg.Wait()
			for i := range copies {
				if errs[i] != nil {
					t.Fatalf("copy %d: %v", i, errs[i])
				}
				if threads[i] == "" || threads[i] != threads[0] {
					t.Fatalf("the copies named threads %v, want one thread from each", threads)
				}
			}
			if all := agentThreadsOf(t, ds, "chatter"); len(all) != 1 {
				t.Fatalf("%d threads, want 1", len(all))
			}
			if n := len(fake.requestsOf("chat")); n != calls {
				t.Fatalf("%d model calls, want %d", n, calls)
			}
			sent := 0
			for _, m := range userRows(t, ds, threads[0]) {
				if m["content"] == "racing" {
					sent++
				}
			}
			if sent != 1 {
				t.Fatalf("the raced message was written %d times, want once", sent)
			}
		})
	}
}

func TestAMessageIDThatIsNotThisMessageIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "a"}, fakeTurn{content: "b"}, fakeTurn{content: "c"})
	one, _, err := sendChat(ds, substrate.ChatInput{Message: "first", MessageID: "taken-1"})
	if err != nil {
		t.Fatal(err)
	}
	two, _, err := sendChat(ds, substrate.ChatInput{Message: "other chat", MessageID: "taken-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sendChat(ds, substrate.ChatInput{Thread: one.Thread, Message: "later", MessageID: "taken-3"}); err != nil {
		t.Fatal(err)
	}
	var reply string
	for _, m := range threadMessagesWithIDs(t, ds, one.Thread) {
		if m["role"] == "assistant" {
			reply, _ = m["__id"].(string)
		}
	}
	for _, tc := range []struct {
		name string
		in   substrate.ChatInput
	}{
		{"another thread's message", substrate.ChatInput{Thread: one.Thread, Message: "other chat", MessageID: "taken-2"}},
		{"this thread's message with another text", substrate.ChatInput{Thread: one.Thread, Message: "changed", MessageID: "taken-1"}},
		{"an opening send with another text", substrate.ChatInput{Message: "changed", MessageID: "taken-1"}},
		{"a later message sent as an opening one", substrate.ChatInput{Message: "later", MessageID: "taken-3"}},
		{"a row that is no sent message", substrate.ChatInput{Thread: one.Thread, Message: "c", MessageID: reply}},
	} {
		_, events, err := sendChat(ds, tc.in)
		if !errors.Is(err, substrate.ErrConflict) || !errors.Is(err, errMessageIDTaken) || len(events) != 0 {
			t.Errorf("%s = %v with events %+v, want a conflict before the stream", tc.name, err, events)
		}
	}
	for _, id := range []string{"-leading", "has space", "per%cent", strings.Repeat("a", 129)} {
		if _, _, err := sendChat(ds, substrate.ChatInput{Message: "x", MessageID: id}); !errors.Is(err, substrate.ErrValidation) {
			t.Errorf("messageId %q = %v, want a validation refusal", id, err)
		}
	}
	// Nothing above wrote or ran anything.
	if n := len(fake.requestsOf("chat")); n != 3 {
		t.Fatalf("%d model calls, want the 3 sends", n)
	}
	if n := len(agentThreadsOf(t, ds, "chatter")); n != 2 {
		t.Fatalf("%d threads, want 2", n)
	}
	if rows := userRows(t, ds, two.Thread); len(rows) != 1 {
		t.Fatalf("the other chat holds %d user rows, want 1", len(rows))
	}
	// A deleted message's id stays spent.
	if _, err := ds.Delete(ctx, substrate.ActorPerson, typeMessage, "taken-3", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the message: %v", err)
	}
	if _, _, err := sendChat(ds, substrate.ChatInput{Thread: one.Thread, Message: "later", MessageID: "taken-3"}); !errors.Is(err, errMessageIDTaken) {
		t.Fatalf("a deleted message's id = %v, want it refused as taken", err)
	}
}

// threadMessagesWithIDs is threadMessages with each row's id under `__id`.
func threadMessagesWithIDs(t *testing.T, ds *dataset, thread string) []map[string]any {
	t.Helper()
	l := &agentLoop{ds: ds, threadID: thread}
	rows, err := l.loadThreadRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		r.props["__id"] = r.id
		out = append(out, r.props)
	}
	return out
}

// A repeat is told from a first send by the stored row alone, so it holds
// across a restart: after a turn that settled, and after one the restart
// interrupted.
func TestARepeatedSendHoldsAcrossARestart(t *testing.T) {
	t.Parallel()
	ds, fake, restart := restartableAgentDataset(t)
	fake.script("chat", fakeTurn{content: "settled"})
	settled := substrate.ChatInput{Message: "before the restart", MessageID: "restart-1"}
	res, _, err := sendChat(ds, settled)
	if err != nil {
		t.Fatal(err)
	}

	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{content: "never finished", hold: hold})
	cut := substrate.ChatInput{Message: "mid-turn", MessageID: "restart-2"}
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(context.Background(), substrate.ActorPerson, chatter, cut, log.emit)
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	cutThread := log.snapshot()[0].Thread

	ds = restart()
	release()
	again, events, err := sendChat(ds, settled)
	wantIdleRepeat(t, again, events, err, res.Thread, threadOK)
	// The shutdown settled the held turn `stopped`; the repeat says so and
	// starts nothing.
	again, events, err = sendChat(ds, cut)
	wantIdleRepeat(t, again, events, err, cutThread, threadStopped)
	if n := len(fake.requestsOf("chat")); n != 2 {
		t.Fatalf("%d model calls across the restart, want the 2 first sends", n)
	}
	if n := len(agentThreadsOf(t, ds, "chatter")); n != 2 {
		t.Fatalf("%d threads across the restart, want 2", n)
	}
}

// lastRequestMessages are the non-system messages of the newest model call.
func lastRequestMessages(t *testing.T, fake *fakeLLM, model string) []map[string]any {
	t.Helper()
	reqs := fake.requestsOf(model)
	if len(reqs) == 0 {
		t.Fatalf("no %s model call", model)
	}
	var out []map[string]any
	for _, m := range reqs[len(reqs)-1]["messages"].([]any) {
		if msg := m.(map[string]any); msg["role"] != "system" {
			out = append(out, msg)
		}
	}
	return out
}

func TestContinueTakesUpAStoppedTurnWithNoUserMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	hold, _ := heldTurn(t)
	fake.script("chat", fakeTurn{content: "partial reply", hold: hold})
	log := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "tell me"}, log.emit)
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread
	if _, err := ds.StopAgentTurn(ctx, substrate.ActorPerson, thread); err != nil {
		t.Fatalf("stop: %v", err)
	}

	fake.script("chat", fakeTurn{content: "l reply, finished"})
	clog := newEventLog()
	res, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, clog.emit)
	if err != nil {
		t.Fatalf("continue a stopped thread: %v", err)
	}
	if res.Status != threadOK || res.Reply != "l reply, finished" || res.Thread != thread {
		t.Fatalf("the continuation settled %+v", res)
	}
	events := clog.snapshot()
	if events[0].Kind != substrate.AgentEventThread || events[0].Thread != thread || events[len(events)-1].Kind != substrate.AgentEventDone {
		t.Fatalf("the continuation streamed %+v, want thread first and done last", events)
	}
	// The stored rows are the whole conversation: the person's one message,
	// the reply the stop cut, the continuation.
	msgs := threadMessages(t, ds, thread)
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m["role"].(string))
	}
	if strings.Join(roles, ",") != "user,assistant,assistant" {
		t.Fatalf("the thread's rows = %v, want no row for the continue itself", roles)
	}
	// The request ended on the engine's own sentence, because the rows end
	// on the assistant's text, and the sentence was not stored.
	sent := lastRequestMessages(t, fake, "chat")
	if last := sent[len(sent)-1]; last["role"] != "user" || last["content"] != continueNudge {
		t.Fatalf("the continuation's request ended with %+v, want the engine's nudge", last)
	}
	if before := sent[len(sent)-2]; before["role"] != "assistant" || before["content"] != "partia" {
		t.Fatalf("the continuation's request held %+v before the nudge, want the cut reply", before)
	}
	if got := threadProps(t, ds, thread); got["status"] != threadOK || got["reason"] != nil {
		t.Fatalf("thread after the continuation = %v (%v)", got["status"], got["reason"])
	}

	// A finished turn has nothing to continue.
	_, err = ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if !errors.Is(err, substrate.ErrNothingToContinue) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("continue a finished thread = %v, want ErrNothingToContinue", err)
	}
	if _, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, "nosuchthread", nil); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("continue an unknown thread = %v, want ErrNotFound", err)
	}
	if n := len(fake.requestsOf("chat")); n != 2 {
		t.Fatalf("%d model calls, want the turn and its one continuation", n)
	}
}

func TestContinueTriesAFailedTurnAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{status: 500})
	_, _, err := sendChat(ds, substrate.ChatInput{Message: "does it work"})
	if err == nil {
		t.Fatal("a turn whose model call failed returned no error")
	}
	threads := agentThreadsOf(t, ds, "chatter")
	thread, _ := threads[0]["__id"].(string)
	if threads[0]["status"] != threadError {
		t.Fatalf("thread = %v, want error", threads[0]["status"])
	}

	fake.script("chat", fakeTurn{content: "it does"})
	res, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || res.Status != threadOK || res.Reply != "it does" {
		t.Fatalf("continue a failed thread = %+v, %v", res, err)
	}
	// The rows ended on the person's message, so the request is that
	// conversation and nothing more.
	sent := lastRequestMessages(t, fake, "chat")
	if len(sent) != 1 || sent[0]["role"] != "user" || !strings.Contains(toolJSON(sent[0]["content"]), "does it work") {
		t.Fatalf("the retry's request = %+v, want the one user message", sent)
	}
	if rows := userRows(t, ds, thread); len(rows) != 1 {
		t.Fatalf("%d user rows after the retry, want 1", len(rows))
	}
}

func TestContinueRefusesAThreadATurnRunsOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	fake.script("chat", fakeTurn{content: "partial reply", hold: hold})
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "hi"}, log.emit)
		turn <- err
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	thread := log.snapshot()[0].Thread

	_, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if !errors.Is(err, substrate.ErrThreadBusy) || !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("continue a running thread = %v, want ErrThreadBusy", err)
	}
	// A message into it is busy the same way.
	_, _, err = sendChat(ds, substrate.ChatInput{Thread: thread, Message: "and this"})
	if !errors.Is(err, substrate.ErrThreadBusy) {
		t.Fatalf("a message into a running thread = %v, want ErrThreadBusy", err)
	}
	release()
	if err := waitTurn(t, turn); err != nil {
		t.Fatal(err)
	}
}

func TestContinueGivesAnOverBudgetRunAFreshBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	// budgeter stops after two turns: both spent on tool calls.
	call := fakeTurn{calls: []fakeCall{{"annotate", `{"id":"01h2xcejqtf2nbrexx3vqjhp41"}`}}}
	fake.script("budget", call, call)
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/budgeter", substrate.ChatInput{Message: "annotate twice"}, nil)
	if err != nil || res.Status != threadOverBudget {
		t.Fatalf("the budgeted turn = %+v, %v, want overbudget", res, err)
	}
	fake.script("budget", fakeTurn{content: "both are annotated"})
	cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, res.Thread, nil)
	if err != nil || cont.Status != threadOK || cont.Reply != "both are annotated" {
		t.Fatalf("continue an overbudget thread = %+v, %v", cont, err)
	}
	// The rows ended on a tool result, which the model answers as it stands.
	sent := lastRequestMessages(t, fake, "budget")
	if last := sent[len(sent)-1]; last["role"] != "tool" {
		t.Fatalf("the continuation's request ended with %+v, want the last tool result", last)
	}
}

// A hand that may not continue a thread learns nothing about a message on it
// from a guessed id: not the thread, not the agent, not whether the text
// matches. The refusal is the one a put at a taken id would be.
func TestAGuessedMessageIDSaysNothingAboutAThreadTheHandMayNotContinue(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	r.fake.script("proposer", fakeTurn{content: "one."}, fakeTurn{content: "two."})
	opening := substrate.ChatInput{Message: "ada's own words", MessageID: "guess-1"}
	res, err := r.ds.ChatAgent(r.adaCtx(), substrate.ActorPerson, r.pkg+"/proposer", opening, nil)
	if err != nil {
		t.Fatal(err)
	}
	later := substrate.ChatInput{Thread: res.Thread, Message: "and more", MessageID: "guess-2"}
	if _, err := r.ds.ChatAgent(r.adaCtx(), substrate.ActorPerson, r.pkg+"/proposer", later, nil); err != nil {
		t.Fatal(err)
	}

	// The app talks to scribe alone, and guesses ids of the proposer's chat.
	app, ctx, _ := r.app(t, "guesser.example.com", "agents:"+r.pkg+"/scribe")
	var said []string
	for _, in := range []substrate.ChatInput{
		{Message: "ada's own words", MessageID: "guess-1"},
		{Message: "a wrong guess", MessageID: "guess-1"},
		{Message: "and more", MessageID: "guess-2"},
		{Message: "and more", MessageID: "guess-2", Thread: "some-other-thread"},
	} {
		log := newEventLog()
		_, err := r.ds.ChatAgent(ctx, app, r.pkg+"/scribe", in, log.emit)
		if !errors.Is(err, errMessageIDTaken) || len(log.snapshot()) != 0 {
			// A named thread the app may not continue is refused before the
			// id is read at all.
			if in.Thread != "" && errors.Is(err, substrate.ErrForbidden) {
				continue
			}
			t.Fatalf("%+v = %v with events %+v, want the id refused as taken and no stream", in, err, log.snapshot())
		}
		if strings.Contains(err.Error(), res.Thread) || strings.Contains(err.Error(), "proposer") || strings.Contains(err.Error(), "text") {
			t.Fatalf("%+v was refused with %q, which says where the id is", in, err)
		}
		said = append(said, strings.ReplaceAll(err.Error(), in.MessageID, "<id>"))
	}
	for _, s := range said {
		if s != said[0] {
			t.Fatalf("the refusals differ, so they tell the guesses apart: %q", said)
		}
	}
	if n := len(r.fake.requestsOf("scribe")); n != 0 {
		t.Fatalf("%d model calls for the guesses, want none", n)
	}
	// Ada, who may continue it, is told what is wrong with her own request.
	_, err = r.ds.ChatAgent(r.adaCtx(), substrate.ActorPerson, r.pkg+"/proposer", substrate.ChatInput{Message: "changed", MessageID: "guess-1"}, nil)
	if !errors.Is(err, errMessageIDTaken) || !strings.Contains(err.Error(), "another text") {
		t.Fatalf("ada's own mismatch = %v, want it named", err)
	}
}
