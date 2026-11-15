package engine

// A question answered in the person's own words (decision record 0327): a
// chat message that names the pending interaction resolves it in the
// transaction that writes the message, and the turn that reads the message
// is the one continuation. No resolution row lands and nothing resumes.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// waitingOn is the thread's `waitingOnPerson`.
func waitingOn(t *testing.T, ds *dataset, thread string) int {
	t.Helper()
	rec, err := ds.Get(context.Background(), typeThread, thread)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := anyFloat(rec.Properties[propThreadWaiting])
	return int(n)
}

// interactionNow reads an interaction again.
func interactionNow(t *testing.T, ds *dataset, id string) *substrate.Record {
	t.Helper()
	rec, err := ds.Get(context.Background(), vocabulary.KindLLMInteraction, id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// settlesAt reports that the model serving model is asked exactly calls
// times: now, after every resume the sweep would start, and a moment later.
func settlesAt(t *testing.T, ds *dataset, fake *fakeLLM, model string, calls int) {
	t.Helper()
	if _, err := ds.SweepResolutions(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := len(fake.requestsOf(model)); got != calls {
		t.Fatalf("the model was asked %d times, want %d: a turn nobody asked for ran, or one is missing", got, calls)
	}
}

// lastUserContext is the block the newest user message of a model call
// carries after its text.
func lastUserContext(t *testing.T, req map[string]any) string {
	t.Helper()
	msgs := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i].(map[string]any)
		if m["role"] != "user" {
			continue
		}
		if text, ok := m["content"].(string); ok {
			return text
		}
		var all []string
		for _, p := range m["content"].([]any) {
			text, _ := p.(map[string]any)["text"].(string)
			all = append(all, text)
		}
		return strings.Join(all, "\n")
	}
	t.Fatal("the call carries no user message")
	return ""
}

func TestAMessageThatNamesItsQuestionAnswersItInOneTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	if n := waitingOn(t, ds, thread); n != 1 {
		t.Fatalf("waitingOnPerson = %d before the answer, want 1", n)
	}
	fake.script("askm", fakeTurn{content: "teal it is."})
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{
		Thread: thread, Message: "neither: make it teal", MessageID: "r1", RepliesTo: interaction.ID,
	}, nil)
	if err != nil || res.Status != threadOK || res.Reply != "teal it is." {
		t.Fatalf("the reply's turn = %+v, %v", res, err)
	}
	got := interactionNow(t, ds, interaction.ID)
	if got.Properties["state"] != interactionReplied {
		t.Fatalf("state = %v, want %s", got.Properties["state"], interactionReplied)
	}
	if path := storedReferencePath(got.Properties[propRepliedIn]); path != vocabulary.RecordPath(typeMessage, "r1") {
		t.Fatalf("repliedIn = %q, want the message", path)
	}
	if path := storedReferencePath(got.Properties[propResolvedBy]); path != vocabulary.RecordPath(kindActor, actorID(substrate.ActorPerson)) {
		t.Fatalf("resolvedBy = %q, want the person's actor", path)
	}
	if at, _ := got.Properties["resolvedAt"].(string); at == "" {
		t.Fatal("a replied interaction carries no resolvedAt")
	}
	if got.Properties["answers"] != nil {
		t.Fatalf("a question answered in words carries answers: %v", got.Properties["answers"])
	}
	if n := waitingOn(t, ds, thread); n != 0 {
		t.Fatalf("waitingOnPerson = %d after the answer, want 0", n)
	}
	// The message is the report: no resolution row, and nothing to resume.
	if rows := systemMessages(t, ds, thread); len(rows) != 0 {
		t.Fatalf("a reply in words wrote %d system rows, want none", len(rows))
	}
	reqs := fake.requestsOf("askm")
	block := lastUserContext(t, reqs[len(reqs)-1])
	if !strings.Contains(block, "answers: substrate://"+vocabulary.RecordPath(vocabulary.KindLLMInteraction, interaction.ID)) {
		t.Fatalf("the model was not told the message answers the question:\n%s", block)
	}
	// The stored row holds the same block, so a replay sends the same bytes.
	for _, m := range threadMessagesWithIDs(t, ds, thread) {
		if m["__id"] == "r1" {
			if stored, _ := m[propMsgContext].(string); !strings.Contains(block, stored) || !strings.Contains(stored, "answers: substrate://") {
				t.Fatalf("the stored block is not the one sent:\n%s", stored)
			}
		}
	}
	// Two calls asked the question, one answered the reply, and no other
	// turn follows.
	settlesAt(t, ds, fake, "askm", 3)

	// A repeat of the send writes nothing, resolves nothing and runs nothing.
	version := got.Version
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{
		Thread: thread, Message: "neither: make it teal", MessageID: "r1", RepliesTo: interaction.ID,
	}, nil); err != nil {
		t.Fatalf("the repeated reply: %v", err)
	}
	if again := interactionNow(t, ds, interaction.ID); again.Version != version {
		t.Fatalf("a repeated reply moved the interaction from version %d to %d", version, again.Version)
	}
	settlesAt(t, ds, fake, "askm", 3)

	// The next turn replays the stored block as it was sent.
	fake.script("askm", fakeTurn{content: "you are welcome."})
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{
		Thread: thread, Message: "thanks", MessageID: "r2",
	}, nil); err != nil {
		t.Fatal(err)
	}
	reqs = fake.requestsOf("askm")
	replayed := false
	for _, raw := range reqs[len(reqs)-1]["messages"].([]any) {
		if m := raw.(map[string]any); m["role"] == "user" {
			if text := lastUserContext(t, map[string]any{"messages": []any{m}}); strings.Contains(text, "neither: make it teal") {
				replayed = strings.Contains(text, "answers: substrate://")
			}
		}
	}
	if !replayed {
		t.Fatal("the replay of the reply does not say which question it answered")
	}
}

// The state is the send's alone: a write of the record cannot claim a
// message nobody sent, and the link is the engine's.
func TestOnlyTheSendMovesAQuestionToReplied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	_, interaction := askedThread(t, ds, fake)
	_, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionReplied}, IfVersion: &interaction.Version,
	})
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a patch to replied = %v, want forbidden", err)
	}
	for _, name := range []string{propRepliedIn, propResolvedBy} {
		_, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
			Properties: map[string]any{name: "anything"}, IfVersion: &interaction.Version,
		})
		if err == nil {
			t.Fatalf("a write set %s, which the engine stamps", name)
		}
	}
	if got := interactionNow(t, ds, interaction.ID); got.Properties["state"] != interactionPending {
		t.Fatalf("state = %v after the refused writes", got.Properties["state"])
	}
}

// What a reply must name, and what happens to one that names a question
// that is not this thread's or no longer waits.
func TestAReplyNamesAPendingQuestionOfItsOwnThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	poller := crewPackage + "/poller"

	// A second thread of the same agent, with a question of its own.
	fake.script("askm", fakeTurn{calls: []fakeCall{{"ask", askArgs}}}, fakeTurn{content: "asked."})
	other, err := ds.ChatAgent(ctx, substrate.ActorPerson, poller, substrate.ChatInput{Message: "and another"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var elsewhere string
	if err := ds.db.QueryRowContext(ctx, `SELECT id FROM records WHERE kind = $1 AND id <> $2`,
		vocabulary.KindLLMInteraction, interaction.ID).Scan(&elsewhere); err != nil {
		t.Fatal(err)
	}
	calls := len(fake.requestsOf("askm"))

	for name, in := range map[string]substrate.ChatInput{
		"no thread":                  {Message: "x", MessageID: "v1", RepliesTo: interaction.ID},
		"no message id":              {Thread: thread, Message: "x", RepliesTo: interaction.ID},
		"a question of another chat": {Thread: thread, Message: "x", MessageID: "v2", RepliesTo: elsewhere},
		"no such question":           {Thread: thread, Message: "x", MessageID: "v3", RepliesTo: "nothing-here"},
		"not an id":                  {Thread: thread, Message: "x", MessageID: "v4", RepliesTo: "not an id"},
	} {
		if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, poller, in, nil); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: %v, want a validation refusal", name, err)
		}
	}
	// The two refusals that read the record say the same thing, so an id
	// tells nothing about another thread's questions.
	_, errOther := ds.ChatAgent(ctx, substrate.ActorPerson, poller, substrate.ChatInput{Thread: thread, Message: "x", MessageID: "v5", RepliesTo: elsewhere}, nil)
	_, errNone := ds.ChatAgent(ctx, substrate.ActorPerson, poller, substrate.ChatInput{Thread: thread, Message: "x", MessageID: "v5", RepliesTo: "nothing-here"}, nil)
	if strings.ReplaceAll(errOther.Error(), elsewhere, "ID") != strings.ReplaceAll(errNone.Error(), "nothing-here", "ID") {
		t.Fatalf("the refusals differ:\n  %v\n  %v", errOther, errNone)
	}
	// Nothing was written, claimed or run by any of them.
	if got := len(fake.requestsOf("askm")); got != calls {
		t.Fatalf("a refused reply ran a turn: %d calls, want %d", got, calls)
	}
	for _, id := range []string{"v1", "v2", "v3", "v4", "v5"} {
		if row, err := ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: id}); err != nil || row != nil {
			t.Fatalf("a refused reply wrote message %s: %v, %v", id, row, err)
		}
	}
	for _, id := range []string{thread, other.Thread} {
		if rec, _ := ds.Get(ctx, typeThread, id); rec.Properties["status"] != threadOK || waitingOn(t, ds, id) != 1 {
			t.Fatalf("thread %s after the refused replies = %v, waiting on %d", id, rec.Properties["status"], waitingOn(t, ds, id))
		}
	}

	// A question answered since is left as it was, and the message is sent
	// as any other: the answer's own resume and the message are one run or
	// two, and the question is resolved once.
	fake.script("askm", fakeTurn{content: "noted: red."}, fakeTurn{content: "and noted."})
	answer(t, ds, interaction)
	answered := interactionNow(t, ds, interaction.ID)
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, poller, substrate.ChatInput{
		Thread: thread, Message: "on second thought, teal", MessageID: "late", RepliesTo: interaction.ID,
	}, nil); err != nil {
		t.Fatalf("a reply to a question answered since: %v", err)
	}
	after := interactionNow(t, ds, interaction.ID)
	if after.Properties["state"] != interactionAnswered || after.Version != answered.Version || after.Properties[propRepliedIn] != nil {
		t.Fatalf("a late reply moved an answered question: %+v", after.Properties)
	}
	for _, m := range threadMessagesWithIDs(t, ds, thread) {
		if m["__id"] == "late" {
			if stored, _ := m[propMsgContext].(string); strings.Contains(stored, "answers:") {
				t.Fatalf("a message that answered nothing says it answered:\n%s", stored)
			}
		}
	}
}

// A reply sent while the run that asked still goes joins that run (decision
// 0318), and resolves its question at once. A second question the run asks
// stays pending: a message answers the one it names.
func TestAReplySentWhileTheAskingRunGoesIsReadByThatRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{calls: []fakeCall{{"ask", `{"questions":[{"id":"size","prompt":"Which size?"}]}`}}, arrived: arrived, release: release},
		fakeTurn{content: "teal, then. And the size?"},
	)
	run := startChat(ds, crewPackage+"/poller", substrate.ChatInput{Message: "pick for me", MessageID: "m0"})
	waitArrived(t, arrived, "the call after the first question")
	thread := run.log.waitFor(t, substrate.AgentEventThread).Thread
	first := onlyInteraction(t, ds)

	reply := startChat(ds, crewPackage+"/poller", substrate.ChatInput{
		Thread: thread, Message: "teal", MessageID: "r1", RepliesTo: first.ID,
	})
	waitUntil(t, "the reply to be queued", func() bool { return eventOf(reply.log, substrate.AgentEventMessageQueued, "r1") != nil })
	// Resolved with the row, before any call reads it.
	if got := interactionNow(t, ds, first.ID); got.Properties["state"] != interactionReplied ||
		storedReferencePath(got.Properties[propRepliedIn]) != vocabulary.RecordPath(typeMessage, "r1") {
		t.Fatalf("the question after its reply was queued: %+v", got.Properties)
	}
	// A repeat while the run goes resolves nothing again.
	version := interactionNow(t, ds, first.ID).Version
	repeat := startChat(ds, crewPackage+"/poller", substrate.ChatInput{
		Thread: thread, Message: "teal", MessageID: "r1", RepliesTo: first.ID,
	})
	waitUntil(t, "the repeat to attach", func() bool { return len(repeat.log.snapshot()) > 0 })
	if got := interactionNow(t, ds, first.ID); got.Version != version {
		t.Fatalf("a repeated reply moved the question from version %d to %d", version, got.Version)
	}
	close(release)
	res, err := run.wait(t)
	if err != nil || res.Status != threadOK || res.Reply != "teal, then. And the size?" {
		t.Fatalf("the run = %+v, %v", res, err)
	}
	if eventOf(run.log, substrate.AgentEventMessageTaken, "r1") == nil {
		t.Fatal("the run never read the reply")
	}
	reqs := fake.requestsOf("askm")
	if block := lastUserContext(t, reqs[len(reqs)-1]); !strings.Contains(block, "answers: substrate://"+vocabulary.RecordPath(vocabulary.KindLLMInteraction, first.ID)) {
		t.Fatalf("the call that read the reply was not told what it answers:\n%s", block)
	}
	// The second question still waits, and the count says one.
	if n := waitingOn(t, ds, thread); n != 1 {
		t.Fatalf("waitingOnPerson = %d, want the second question alone", n)
	}
	var pending int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE kind = $1 AND states->>'state' = $2`,
		vocabulary.KindLLMInteraction, interactionPending).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("%d questions pending, %v: want the second alone", pending, err)
	}
	if rows := systemMessages(t, ds, thread); len(rows) != 0 {
		t.Fatalf("%d system rows, want none", len(rows))
	}
	settlesAt(t, ds, fake, "askm", 3)
}

// A reply sent while the final answer streams was not read by that run: it
// starts the next turn itself, once, and that turn is the one continuation.
func TestAReplyTheAskingRunNeverReadStartsOneTurn(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{content: "asked.", arrived: arrived, release: release},
		fakeTurn{content: "teal it is."},
	)
	run := startChat(ds, crewPackage+"/poller", substrate.ChatInput{Message: "pick for me", MessageID: "m0"})
	waitArrived(t, arrived, "the final answer's call")
	thread := run.log.waitFor(t, substrate.AgentEventThread).Thread
	interaction := onlyInteraction(t, ds)
	reply := startChat(ds, crewPackage+"/poller", substrate.ChatInput{
		Thread: thread, Message: "teal", MessageID: "r1", RepliesTo: interaction.ID,
	})
	waitUntil(t, "the reply to be queued", func() bool { return eventOf(reply.log, substrate.AgentEventMessageQueued, "r1") != nil })
	close(release)
	if res, err := run.wait(t); err != nil || res.Status != threadOK {
		t.Fatalf("the asking run = %+v, %v", res, err)
	}
	waitUntil(t, "the reply's own turn", func() bool {
		for _, c := range assistantContents(t, ds, thread) {
			if c == "teal it is." {
				return true
			}
		}
		return false
	})
	waitUntil(t, "the reply's turn to settle", func() bool {
		rec, err := ds.Get(context.Background(), typeThread, thread)
		return err == nil && rec.Properties["status"] == threadOK
	})
	if got := interactionNow(t, ds, interaction.ID); got.Properties["state"] != interactionReplied || waitingOn(t, ds, thread) != 0 {
		t.Fatalf("the question = %v, waiting on %d", got.Properties["state"], waitingOn(t, ds, thread))
	}
	settlesAt(t, ds, fake, "askm", 3)
}

// A client from before a message could name its question sends the words
// and then dismisses the card. The dismissal lands while the turn that
// answers the words runs: its row is written and read at the next turn, and
// it starts no turn of its own.
func TestADismissalAfterThePersonsWordStartsNoSecondTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("askm", fakeTurn{content: "teal it is.", arrived: arrived, release: release})
	run := startChat(ds, crewPackage+"/poller", substrate.ChatInput{Thread: thread, Message: "neither: teal", MessageID: "w1"})
	waitArrived(t, arrived, "the turn that answers the words")
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionDismissed}, IfVersion: &interaction.Version,
	}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	close(release)
	if res, err := run.wait(t); err != nil || res.Reply != "teal it is." {
		t.Fatalf("the turn = %+v, %v", res, err)
	}
	// The dismissal is in the thread, for the next turn's replay.
	if rows := systemMessages(t, ds, thread); len(rows) != 1 || !strings.Contains(rows[0]["content"].(string), "interactionDismissed") {
		t.Fatalf("the dismissal's row: %v", rows)
	}
	if n := waitingOn(t, ds, thread); n != 0 {
		t.Fatalf("waitingOnPerson = %d, want 0", n)
	}
	settlesAt(t, ds, fake, "askm", 3)
	fake.script("askm", fakeTurn{content: "anything else?"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{Thread: thread, Message: "thanks", MessageID: "w2"}, nil); err != nil {
		t.Fatal(err)
	}
	reqs := fake.requestsOf("askm")
	if got := conversation(t, reqs[len(reqs)-1]); len(got) < 2 || !strings.Contains(strings.Join(got, "\n"), "interactionDismissed") {
		t.Fatalf("the next turn did not read the dismissal: %v", got)
	}
}

// The rule is narrow. A question dismissed while the run that asked it still
// goes, with no word from the person since, resumes as before; so does one
// dismissed in a settled thread, whatever was said before.
func TestADismissalWithNoLaterWordStillResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	// While the asking run goes.
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{content: "asked.", arrived: arrived, release: release},
		fakeTurn{content: "dropped, then."},
	)
	run := startChat(ds, crewPackage+"/poller", substrate.ChatInput{Message: "pick for me", MessageID: "m0"})
	waitArrived(t, arrived, "the final answer's call")
	thread := run.log.waitFor(t, substrate.AgentEventThread).Thread
	interaction := onlyInteraction(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionDismissed}, IfVersion: &interaction.Version,
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if _, err := run.wait(t); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the dismissal's resume", func() bool {
		for _, c := range assistantContents(t, ds, thread) {
			if c == "dropped, then." {
				return true
			}
		}
		return false
	})
}

func TestADismissalInASettledThreadStillResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	thread, interaction := askedThread(t, ds, fake)
	// The person speaks, the turn settles, and only then is the card
	// dismissed: nothing is running to answer for it.
	fake.script("askm", fakeTurn{content: "take your time."}, fakeTurn{content: "dropped, then."})
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{Thread: thread, Message: "let me think", MessageID: "w1"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionDismissed}, IfVersion: &interaction.Version,
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the dismissal's resume", func() bool {
		for _, c := range assistantContents(t, ds, thread) {
			if c == "dropped, then." {
				return true
			}
		}
		return false
	})
}

// A question a sub-agent asked is its own thread's: a message into the
// conversation above it cannot answer it, and a message into the sub-agent's
// thread does, in one turn of that thread.
func TestASubagentsQuestionIsAnsweredInItsOwnThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	const pkg = "askers.test.dev/askers"
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "leadllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-leadllm"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.AgentManifest(pkg, "lead", map[string]any{
			"description": "delegates to the poller", "prompt": "You delegate.",
			"provider": "leadllm", "model": "lead", "subagents": []any{crewPackage + "/poller"},
			"permissions": map[string]any{"writes": []any{vocabulary.KindLLMInteraction}},
		}),
	}); err != nil {
		t.Fatalf("install the lead: %v", err)
	}
	fake.script("lead",
		fakeTurn{calls: []fakeCall{{"poller", `{"input":"ask them which color"}`}}},
		fakeTurn{content: "my helper has a question for you."},
	)
	fake.script("askm", fakeTurn{calls: []fakeCall{{"ask", askArgs}}}, fakeTurn{content: "asked."})
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, pkg+"/lead", substrate.ChatInput{Message: "pick a color", MessageID: "m0"}, nil)
	if err != nil {
		t.Fatalf("the lead's turn: %v", err)
	}
	interaction := onlyInteraction(t, ds)
	sub := storedReferencePath(interaction.Properties["thread"])
	_, subThread, _ := vocabulary.SplitRecordPath(sub)
	if subThread == "" || subThread == res.Thread {
		t.Fatalf("the question's thread = %q, want the sub-agent's own", sub)
	}
	// Into the conversation above: refused, and nothing is written there.
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, pkg+"/lead", substrate.ChatInput{
		Thread: res.Thread, Message: "teal", MessageID: "up1", RepliesTo: interaction.ID,
	}, nil); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a reply from the conversation above = %v, want a validation refusal", err)
	}
	if got := interactionNow(t, ds, interaction.ID); got.Properties["state"] != interactionPending {
		t.Fatalf("the question after the refused reply = %v", got.Properties["state"])
	}
	// Into the thread that asked: one turn of that thread.
	fake.script("askm", fakeTurn{content: "teal it is."})
	got, err := ds.ChatAgent(ctx, substrate.ActorPerson, crewPackage+"/poller", substrate.ChatInput{
		Thread: subThread, Message: "teal", MessageID: "sub1", RepliesTo: interaction.ID,
	}, nil)
	if err != nil || got.Reply != "teal it is." {
		t.Fatalf("the reply in the sub-agent's thread = %+v, %v", got, err)
	}
	if rec := interactionNow(t, ds, interaction.ID); rec.Properties["state"] != interactionReplied || waitingOn(t, ds, subThread) != 0 {
		t.Fatalf("the question = %v, its thread waits on %d", rec.Properties["state"], waitingOn(t, ds, subThread))
	}
	settlesAt(t, ds, fake, "askm", 3)
	settlesAt(t, ds, fake, "lead", 2)
}
