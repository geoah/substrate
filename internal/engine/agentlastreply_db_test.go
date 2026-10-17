package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A thread keeps a preview of its last reply, so a thread list reads one
// kind. A turn that settles with no reply text leaves the previous preview,
// and a stopped turn's preview is the text it streamed before the stop.
func TestSettleKeepsTheLastReplyOnTheThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	const budgeter = crewPackage + "/budgeter"
	chat := func(thread, message string) *substrate.AgentResult {
		t.Helper()
		res, err := ds.ChatAgent(ctx, substrate.ActorAPI, budgeter, thread, message, func(substrate.AgentEvent) {})
		if err != nil {
			t.Fatalf("chat: %v", err)
		}
		return res
	}
	lastReply := func(thread string) any {
		t.Helper()
		return getThread(t, ds, thread).Properties[propThreadLastReply]
	}

	long := strings.Repeat("word ", 100)
	fake.script("budget", fakeTurn{content: "  first\n\nanswer " + long})
	first := chat("", "hello")
	if first.Status != threadOK {
		t.Fatalf("first turn %+v", first)
	}
	want := cutRunes("first answer "+strings.TrimSpace(long), lastReplyRunes)
	if got := lastReply(first.Thread); got != want {
		t.Fatalf("lastReply %q, want %q", got, want)
	}

	// maxTurns is 2: two tool-calling turns end the run overbudget with no
	// reply, and the preview stays.
	call := fakeTurn{calls: []fakeCall{{"nosuchtool", `{}`}}}
	fake.script("budget", call, call)
	over := chat(first.Thread, "keep going")
	if over.Status != threadOverBudget || over.Reply != "" {
		t.Fatalf("second turn %+v", over)
	}
	if got := lastReply(first.Thread); got != want {
		t.Fatalf("lastReply after an overbudget turn %q, want the previous %q", got, want)
	}

	fake.script("budget", fakeTurn{content: "the newest"})
	chat(first.Thread, "again")
	if got := lastReply(first.Thread); got != "the newest" {
		t.Fatalf("lastReply %q, want the newest reply", got)
	}

	// A stop mid-reply: the preview is the partial text.
	hold, _ := heldTurn(t)
	fake.script("budget", fakeTurn{content: "cut short here", hold: hold})
	log := newEventLog()
	chatted := make(chan struct{})
	go func() {
		defer close(chatted)
		_, _ = ds.ChatAgent(ctx, substrate.ActorAPI, budgeter, first.Thread, "once more", log.emit)
	}()
	log.waitFor(t, substrate.AgentEventDelta)
	stopped, err := ds.StopAgentTurn(ctx, substrate.ActorAPI, first.Thread)
	if err != nil || stopped.Status != threadStopped || stopped.Reply != "cut sho" {
		t.Fatalf("stop = %+v, %v", stopped, err)
	}
	<-chatted
	if got := lastReply(first.Thread); got != "cut sho" {
		t.Fatalf("lastReply after a stop %q, want the partial %q", got, "cut sho")
	}
}
