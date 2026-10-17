package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A thread keeps a preview of its last reply, so a thread list reads one
// kind. A turn that settles with no reply text leaves the previous preview.
//
// A stopped turn (the detached-turn work) settles with its partial text as
// the reply, which this same path writes; its case belongs with that change.
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
}
