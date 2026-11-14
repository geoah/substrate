package engine

// A turn's stream a client can join twice (decision 0317): every piece of
// text says where it belongs, a chat thread's resumed run streams as a sent
// turn does, what a call streamed is kept when it fails, an assistant row
// says how its reply was cut short, and a cut reply is continued.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// placed is each turn's text as a client that writes every delta at its
// position reads it, and each turn's reasoning the same way.
func placed(t *testing.T, events []substrate.AgentEvent) (text, reasoning map[int]string) {
	t.Helper()
	runes := map[string]map[int][]rune{substrate.AgentEventDelta: {}, substrate.AgentEventReasoning: {}}
	next := map[string]map[int]int{substrate.AgentEventDelta: {}, substrate.AgentEventReasoning: {}}
	for _, ev := range events {
		by, ok := runes[ev.Kind]
		if !ok {
			continue
		}
		if ev.At == nil {
			t.Fatalf("a %s event carries no position: %+v", ev.Kind, ev)
		}
		// Live pieces are contiguous: each starts where the last one ended.
		if want := next[ev.Kind][ev.Turn]; *ev.At != want {
			t.Fatalf("a %s piece of turn %d is at %d, and %d code points came before it", ev.Kind, ev.Turn, *ev.At, want)
		}
		next[ev.Kind][ev.Turn] = *ev.At + len([]rune(ev.Text))
		by[ev.Turn] = applyAt(by[ev.Turn], ev)
	}
	text, reasoning = map[int]string{}, map[int]string{}
	for turn, r := range runes[substrate.AgentEventDelta] {
		text[turn] = string(r)
	}
	for turn, r := range runes[substrate.AgentEventReasoning] {
		reasoning[turn] = string(r)
	}
	return text, reasoning
}

// The proof the positions rest on: writing each piece at its `at`, in code
// points, reproduces every assistant row of the run exactly, with characters
// of several code points split across pieces, and the count starts again at
// 0 with each turn, a tool call between them or not.
func TestTextPlacedByPositionReproducesTheStoredRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	// A family (three people joined by two joiners), a flag (two regional
	// indicators) and an accented letter (a letter and a combining mark),
	// each cut between its code points.
	first := []string{"Looking 👩‍", "👩‍👧 for wid", "gets 🇬", "🇷 now."}
	second := []string{"Found none, e", "́t voilà", " — tha", "t is all."}
	fake.script("lib",
		fakeTurn{pieces: first, calls: []fakeCall{{name: "query", args: `{"op":"list","kind":"` + crewPackage + `/widget"}`}}},
		fakeTurn{pieces: second},
	)
	log := newEventLog()
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/librarian", substrate.ChatInput{Message: "list widgets"}, log.emit)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	text, _ := placed(t, log.snapshot())
	stored := map[int]string{}
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "assistant" {
			stored[intProp(m, "turn")], _ = m["content"].(string)
		}
	}
	if len(stored) != 2 || stored[1] != strings.Join(first, "") || stored[3] != strings.Join(second, "") {
		t.Fatalf("stored assistant rows = %v", stored)
	}
	if len(text) != len(stored) {
		t.Fatalf("text streamed for turns %v, and rows are stored for %v", text, stored)
	}
	for turn, want := range stored {
		if text[turn] != want {
			t.Fatalf("turn %d placed by position reads %q, and its row holds %q", turn, text[turn], want)
		}
	}
	// Positions count code points, not bytes: the two pieces before the
	// second turn's third are 13 and 9 code points, and 13 and 11 bytes (each
	// combining mark is one code point of two bytes).
	for _, ev := range log.snapshot() {
		if ev.Kind == substrate.AgentEventDelta && ev.Text == second[2] && *ev.At != 13+9 {
			t.Fatalf("the third piece is at %d, want 22 code points in (24 bytes)", *ev.At)
		}
	}
	// And not UTF-16 units: the first turn's third piece follows 10 and 11
	// code points, which are 11 and 13 UTF-16 units (each person is two).
	for _, ev := range log.snapshot() {
		if ev.Kind == substrate.AgentEventDelta && ev.Text == first[2] && *ev.At != 10+11 {
			t.Fatalf("the first turn's third piece is at %d, want 21 code points in (24 UTF-16 units)", *ev.At)
		}
	}
}

// Reasoning counts apart from the reply, from 0 in the same turn.
func TestReasoningIsPlacedApartFromTheReply(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	fake.script("think", fakeTurn{reasoning: "weighing it 🤔 twice", pieces: []string{"The ans", "wer."}})
	log := newEventLog()
	res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, crewPackage+"/thinker", substrate.ChatInput{Message: "well?"}, log.emit)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	text, reasoning := placed(t, log.snapshot())
	msgs := threadMessages(t, ds, res.Thread)
	last := msgs[len(msgs)-1]
	turn := intProp(last, "turn")
	if text[turn] != "The answer." || text[turn] != last["content"] {
		t.Fatalf("the reply placed by position = %q, stored %q", text[turn], last["content"])
	}
	if reasoning[turn] != "weighing it 🤔 twice" || reasoning[turn] != last["reasoning"] {
		t.Fatalf("the reasoning placed by position = %q, stored %q", reasoning[turn], last["reasoning"])
	}
}

// An attach mid-turn replays from position 0 and goes on live: a client that
// applies the replay over what it already shows, and one that attaches a
// second time, both read the stored row.
func TestAnAttachIsSafeToApplyTwice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	hold, release := heldTurn(t)
	pieces := []string{"Tomatoes 🍅 like ", "a deep watering ", "twice a week."}
	fake.script("chat", fakeTurn{pieces: pieces, hold: hold})
	first := newEventLog()
	go func() {
		_, _ = ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "tomatoes?"}, first.emit)
	}()
	first.waitFor(t, substrate.AgentEventDelta)
	thread := first.snapshot()[0].Thread

	// Two more clients attach while the turn holds after its first piece.
	second, third := newEventLog(), newEventLog()
	done := make(chan struct{}, 2)
	for _, log := range []*eventLog{second, third} {
		go func() {
			_, _ = ds.AttachAgentTurn(ctx, thread, log.emit)
			done <- struct{}{}
		}()
		log.waitFor(t, substrate.AgentEventDelta)
	}
	release()
	<-done
	<-done
	first.waitFor(t, substrate.AgentEventDone)

	want := strings.Join(pieces, "")
	msgs := threadMessages(t, ds, thread)
	if got := msgs[len(msgs)-1]["content"]; got != want {
		t.Fatalf("stored reply = %q", got)
	}
	turn := intProp(msgs[len(msgs)-1], "turn")
	// One client applies everything it was ever sent, on one piece of text:
	// its own stream, then both attaches' replays and live pieces.
	var shown []rune
	for _, log := range []*eventLog{first, second, third} {
		for _, ev := range log.snapshot() {
			if ev.Kind == substrate.AgentEventDelta && ev.Turn == turn {
				shown = applyAt(shown, ev)
			}
		}
		if string(shown) != want {
			t.Fatalf("after a stream applied over what was shown the reply reads %q, want %q", string(shown), want)
		}
	}
	if ev := second.waitFor(t, substrate.AgentEventDelta); *ev.At != 0 || ev.Text != pieces[0] {
		t.Fatalf("the replay opens with %+v at %d, want the turn's first piece at 0", ev, *ev.At)
	}
}

// askedThread is a chat with the poller that ends on a question, and the
// interaction it left pending.
func askedThread(t *testing.T, ds *dataset, fake *fakeLLM) (string, *substrate.Record) {
	t.Helper()
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{content: "asked."},
	)
	res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{Message: "pick a color for me"}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	return res.Thread, onlyInteraction(t, ds)
}

// answer answers the pending interaction, which resumes its thread.
func answer(t *testing.T, ds *dataset, interaction *substrate.Record) {
	t.Helper()
	if _, err := ds.Patch(context.Background(), substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{
			"state": interactionAnswered,
			"answers": []any{
				map[string]any{"question": "color", "selected": []any{"red"}},
				map[string]any{"question": "sure", "selected": []any{"yes"}},
			},
		},
		IfVersion: &interaction.Version,
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
}

// attachResumed attaches to the resumed turn of thread once it runs.
func attachResumed(t *testing.T, ds *dataset, thread string) (*eventLog, <-chan *substrate.AgentResult) {
	t.Helper()
	waitUntil(t, "the resumed turn", func() bool {
		_, err := ds.runningTurn(context.Background(), thread)
		return err == nil
	})
	log := newEventLog()
	out := make(chan *substrate.AgentResult, 1)
	go func() {
		res, err := ds.AttachAgentTurn(context.Background(), thread, log.emit)
		if err != nil {
			t.Errorf("attach to the resumed turn: %v", err)
		}
		out <- res
	}()
	return log, out
}

// The run an answer resumes on a chat thread streams: an attach replays it
// and follows it live to its done, as it does a sent turn.
func TestAResumedChatRunStreams(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	hold, release := heldTurn(t)
	fake.script("askm", fakeTurn{pieces: []string{"noted: red", ", and yes."}, hold: hold})
	answer(t, ds, interaction)

	log, attached := attachResumed(t, ds, thread)
	if ev := log.waitFor(t, substrate.AgentEventDelta); ev.Text != "noted: red" || *ev.At != 0 {
		t.Fatalf("the resumed turn's first piece = %+v", ev)
	}
	if ev := log.snapshot()[0]; ev.Kind != substrate.AgentEventThread || ev.Thread != thread {
		t.Fatalf("the attach opened with %+v", ev)
	}
	if got := threadProps(t, ds, thread)["status"]; got != threadRunning {
		t.Fatalf("thread status while the resumed turn streams = %v", got)
	}
	release()
	res := <-attached
	if res == nil || res.Status != threadOK || res.Reply != "noted: red, and yes." {
		t.Fatalf("the resumed turn settled %+v", res)
	}
	text, _ := placed(t, log.snapshot())
	msgs := threadMessages(t, ds, thread)
	last := msgs[len(msgs)-1]
	if text[intProp(last, "turn")] != last["content"] {
		t.Fatalf("the resumed turn placed by position = %v, stored %q", text, last["content"])
	}
	if _, live := ds.runningThreads.Load(thread); live {
		t.Fatal("the settled resumed turn is still in runningThreads")
	}
}

// A stop reaches a resumed run: it settles `stopped`, keeping what it
// streamed as a row that says the stop cut it.
func TestAStopStopsAResumedChatRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	hold, _ := heldTurn(t)
	fake.script("askm", fakeTurn{pieces: []string{"noted: red", ", and yes."}, hold: hold})
	answer(t, ds, interaction)
	log, attached := attachResumed(t, ds, thread)
	log.waitFor(t, substrate.AgentEventDelta)

	res, err := ds.StopAgentTurn(ctx, substrate.ActorPerson, thread)
	if err != nil || res.Status != threadStopped || res.Cause != substrate.CauseRequested || res.Reply != "noted: red" {
		t.Fatalf("stop of a resumed turn = %+v, %v", res, err)
	}
	if got := <-attached; got == nil || got.Status != threadStopped {
		t.Fatalf("the attached stream's result = %+v", got)
	}
	msgs := threadMessages(t, ds, thread)
	last := msgs[len(msgs)-1]
	if last["role"] != "assistant" || last["content"] != "noted: red" || last[propCutBy] != cutByStop {
		t.Fatalf("the stopped turn's row = %+v, want the partial reply cut by the stop", last)
	}
}

// fillSlots takes n of person's chat-turn slots, as n running turns would.
func fillSlots(t *testing.T, ds *dataset, person string, n int) {
	t.Helper()
	for range n {
		if !ds.chatTurns.take(person, ds.svc.chatTurnsPerPerson) {
			t.Fatal("the person's slots were not free")
		}
	}
}

// resumed reports whether the thread holds the resumed turn's reply.
func resumed(t *testing.T, ds *dataset, thread, reply string) bool {
	t.Helper()
	return slices.Contains(assistantContents(t, ds, thread), reply)
}

// A resumed run takes one of its person's chat-turn slots. With all of them
// taken nothing is claimed and nothing runs, at the resume and at the sweep;
// the turn that settles and frees a slot starts it, with no sweep.
func TestAResumedChatRunWaitsForASlot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	// The person the resume is held to runs as many turns as they may.
	person := ds.turnPerson(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson))
	fillSlots(t, ds, person, ds.svc.chatTurnsPerPerson)
	time.Sleep(20 * time.Millisecond) // finishedAt strictly before the resolution row
	fake.script("askm", fakeTurn{content: "noted: red, and yes."})
	answer(t, ds, interaction)
	holdFor(t, "the resume waiting for a slot", func() bool {
		return threadProps(t, ds, thread)["status"] == threadOK && len(fake.requestsOf("askm")) == 2
	})
	if _, err := ds.SweepResolutions(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	holdFor(t, "the resume past a sweep with every slot taken", func() bool {
		return len(fake.requestsOf("askm")) == 2
	})
	// One of the person's turns settles: its give starts the resume.
	ds.resumeWaiting(ds.chatTurns.give(person))
	waitUntil(t, "the resume once a slot is free", func() bool {
		return resumed(t, ds, thread, "noted: red, and yes.")
	})
	waitUntil(t, "the resumed turn's settle", func() bool {
		_, live := ds.runningThreads.Load(thread)
		return !live && threadProps(t, ds, thread)["status"] == threadOK
	})
	// The slot it took is given back: the person holds the others alone.
	if !ds.chatTurns.take(person, ds.svc.chatTurnsPerPerson) || ds.chatTurns.take(person, ds.svc.chatTurnsPerPerson) {
		t.Fatal("the resumed turn's slot was not given back, or one too many was")
	}
}

// A resolution that lands while its own thread's turn runs, with the person
// at their cap, is not lost: the turn's settle starts the resume, which
// waits for the slot the turn is about to give back if it must. The sweep
// could never find it, since the row is older than the settle.
func TestAResolutionMidTurnAtTheCapStillResumes(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	// The person's own request, so its turn and its resume count together.
	ctx := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	person := ds.turnPerson(ctx)
	hold, release := heldTurn(t)
	fake.script("askm",
		fakeTurn{pieces: []string{"still ", "thinking."}, hold: hold},
		fakeTurn{content: "noted mid-turn."},
	)
	log := newEventLog()
	turn := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{Thread: thread, Message: "one moment"}, log.emit)
		turn <- err
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	// Every other slot of the person's is taken: this turn holds the last.
	fillSlots(t, ds, person, ds.svc.chatTurnsPerPerson-1)
	answer(t, ds, interaction)
	release()
	if err := waitTurn(t, turn); err != nil {
		t.Fatalf("the turn: %v", err)
	}
	waitUntil(t, "the resume of a resolution that landed mid-turn", func() bool {
		return resumed(t, ds, thread, "noted mid-turn.")
	})
}

// A resume that waits on a thread whose last turn failed is started when a
// slot frees too: the sweep looks only at threads that settled ok or over
// budget, so nothing else would start it.
func TestAResumeThatWaitsOnAFailedThreadStartsWhenASlotFrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	fake.script("askm", fakeTurn{status: 500})
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{Thread: thread, Message: "and?"}, nil); err == nil {
		t.Fatal("the failing turn returned no error")
	}
	if got := threadProps(t, ds, thread)["status"]; got != threadError {
		t.Fatalf("thread = %v, want error", got)
	}
	person := ds.turnPerson(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson))
	fillSlots(t, ds, person, ds.svc.chatTurnsPerPerson)
	fake.script("askm", fakeTurn{content: "noted after the failure."})
	answer(t, ds, interaction)
	holdFor(t, "the resume waiting for a slot", func() bool { return len(fake.requestsOf("askm")) == 3 })
	if _, err := ds.SweepResolutions(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	holdFor(t, "the resume past a sweep with every slot taken", func() bool { return len(fake.requestsOf("askm")) == 3 })
	ds.resumeWaiting(ds.chatTurns.give(person))
	waitUntil(t, "the resume on the failed thread", func() bool {
		return resumed(t, ds, thread, "noted after the failure.")
	})
}

// A call that fails after it streamed keeps what it streamed: the text is
// the turn's assistant row, written before the thread settles `error`, and
// the row says a failure cut it. A continue then goes on from it.
func TestWhatACallStreamedIsKeptWhenItFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{pieces: []string{"Three paragraphs ", "in, the provider ", "never sent"}, dropAfter: 2})
	log := newEventLog()
	_, err := ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "go on"}, log.emit)
	if cause, _ := failureOf(t, err); cause != substrate.CauseProviderUnavailable {
		t.Fatalf("a dropped call carries %q, want provider_unavailable: %v", cause, err)
	}
	thread := log.snapshot()[0].Thread
	props := threadProps(t, ds, thread)
	if props["status"] != threadError || props[propThreadLastReply] != "Three paragraphs in, the provider" {
		t.Fatalf("thread = %v, lastReply %q", props["status"], props[propThreadLastReply])
	}
	msgs := threadMessages(t, ds, thread)
	last := msgs[len(msgs)-1]
	if last["role"] != "assistant" || last["content"] != "Three paragraphs in, the provider " || last[propCutBy] != cutByFailure {
		t.Fatalf("the failed turn's row = %+v, want what streamed, cut by the failure", last)
	}
	text, _ := placed(t, log.snapshot())
	if text[intProp(last, "turn")] != last["content"] {
		t.Fatalf("what streamed = %v, what is kept = %q", text, last["content"])
	}

	// Try again goes on from the kept text, on a new row.
	fake.script("chat", fakeTurn{content: "dropped the call, and here is the rest."})
	res, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, thread, nil)
	if err != nil || res.Status != threadOK {
		t.Fatalf("continue after a dropped call = %+v, %v", res, err)
	}
	sent := lastRequestMessages(t, fake, "chat")
	if n := len(sent); sent[n-1]["content"] != continueNudge || sent[n-2]["content"] != "Three paragraphs in, the provider " {
		t.Fatalf("the retry's request ended %+v", sent[max(0, len(sent)-2):])
	}
	msgs = threadMessages(t, ds, thread)
	if last := msgs[len(msgs)-1]; last[propCutBy] != nil || last["content"] != "dropped the call, and here is the rest." {
		t.Fatalf("the continuation's row = %+v, want a finished reply with no cutBy", last)
	}
}

// A reply the model's output limit cut says so on its row, settles ok as it
// did, and is the one finished thread a continue takes up.
func TestAReplyCutAtTheModelsLimitSaysSoAndIsContinued(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		finish, cut string
	}{{"length", cutByLength}, {"content_filter", cutByFilter}} {
		t.Run(tc.finish, func(t *testing.T) {
			t.Parallel()
			ds, fake := openAgentDataset(t)
			fake.script("chat", fakeTurn{content: "It stops mid-sen", finish: tc.finish})
			res, err := ds.ChatAgent(ctx, substrate.ActorPerson, chatter, substrate.ChatInput{Message: "tell me"}, nil)
			if err != nil || res.Status != threadOK {
				t.Fatalf("a cut reply = %+v, %v, want ok", res, err)
			}
			msgs := threadMessages(t, ds, res.Thread)
			if last := msgs[len(msgs)-1]; last[propCutBy] != tc.cut {
				t.Fatalf("the cut reply's row carries cutBy %v, want %s", last[propCutBy], tc.cut)
			}
			fake.script("chat", fakeTurn{content: "tence, and ends here."})
			cont, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, res.Thread, nil)
			if err != nil || cont.Status != threadOK || cont.Reply != "tence, and ends here." {
				t.Fatalf("continue a cut reply = %+v, %v", cont, err)
			}
			// The continuation finished, so there is nothing more to take up.
			if _, err := ds.ContinueAgentTurn(ctx, substrate.ActorPerson, res.Thread, nil); !errors.Is(err, substrate.ErrNothingToContinue) {
				t.Fatalf("continue a finished continuation = %v, want ErrNothingToContinue", err)
			}
		})
	}
}

// Only a reply the model did not finish is marked.
func TestCutOfMapsTheWiresStopReasons(t *testing.T) {
	t.Parallel()
	for stop, want := range map[string]string{
		"": "", "end": "", "toolCall": "", "other": "",
		"length": cutByLength, "filter": cutByFilter,
	} {
		if got := cutOf(stop); got != want {
			t.Errorf("cutOf(%q) = %q, want %q", stop, got, want)
		}
	}
}

// A run whose deadline passes inside a model call keeps what the call had
// streamed, as a stop does: the thread settles overbudget with the text as
// its reply, and the row says the run ended the call.
func TestADeadlineInsideACallKeepsWhatItStreamed(t *testing.T) {
	t.Parallel()
	ds, fake := provisionDeadlineAgents(t)
	hold, _ := heldTurn(t)
	fake.script("dover", fakeTurn{pieces: []string{"As far as it got", " before the deadline."}, hold: hold})
	res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, deadlinePackage+"/overrunner", substrate.ChatInput{Message: "take your time"}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Status != threadOverBudget || res.Cause != substrate.CauseDeadline || res.Reply != "As far as it got" {
		t.Fatalf("a run its deadline cut = %+v, want overbudget, deadline, the streamed text", res)
	}
	msgs := threadMessages(t, ds, res.Thread)
	if last := msgs[len(msgs)-1]; last["role"] != "assistant" || last["content"] != "As far as it got" || last[propCutBy] != cutByStop {
		t.Fatalf("the cut turn's row = %+v", last)
	}
}
