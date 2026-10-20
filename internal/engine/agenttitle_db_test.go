package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The thread title tests (decision record 0170): a chat opens with its first
// message as its subject, the title model replaces it once after the first
// turn, and an owner's rename is never overwritten.

const namerAgent = crewPackage + "/namer"

// openTitleDataset is openAgentDataset with thread titling on.
func openTitleDataset(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ds, fake := openAgentDataset(t)
	ds.mu.Lock()
	ds.skipThreadTitles = false
	ds.mu.Unlock()
	return ds, fake
}

func getThread(t *testing.T, ds *dataset, id string) *substrate.Record {
	t.Helper()
	rec, err := ds.Get(context.Background(), typeThread, id)
	if err != nil {
		t.Fatalf("get thread %s: %v", id, err)
	}
	return rec
}

// waitThread polls the thread until ok holds, bounded: the title is written
// by a background task after the turn returns.
func waitThread(t *testing.T, ds *dataset, id string, ok func(*substrate.Record) bool) *substrate.Record {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rec := getThread(t, ds, id)
		if ok(rec) {
			return rec
		}
		if time.Now().After(deadline) {
			t.Fatalf("thread %s never reached the expected state: %+v", id, rec.Properties)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func anyFloat2(v any) float64 {
	f, _ := anyFloat(v)
	return f
}

func subjectManager(rec *substrate.Record) string {
	return rec.PropertyMeta[propThreadSubject].Manager
}

func chatOnce(t *testing.T, ds *dataset, thread, message string) *substrate.AgentResult {
	t.Helper()
	res, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, namerAgent, thread, message, func(substrate.AgentEvent) {})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	return res
}

func TestANewChatThreadOpensWithASubjectFromItsMessage(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	long := strings.Repeat("ab ", 60)
	fake.script("name", fakeTurn{content: "sure"})
	res := chatOnce(t, ds, "", "\n\n   Plan   a\ttrip "+long+"\nsecond line")
	rec := getThread(t, ds, res.Thread)
	want := cutRunes("Plan a trip "+strings.TrimSpace(long), threadSubjectRunes)
	if got := rec.Properties[propThreadSubject]; got != want {
		t.Fatalf("subject %q, want %q", got, want)
	}
	if rec.Title != want {
		t.Fatalf("title %q, want the subject %q", rec.Title, want)
	}
	if m := subjectManager(rec); m != "agent:"+strings.ReplaceAll(namerAgent, "/", ":") {
		t.Fatalf("subject manager %q, want the agent's actor", m)
	}

	// A thread that is not a chat has no subject and titles as its agent.
	fake.script("name", fakeTurn{content: "called"})
	called, err := ds.CallAgent(context.Background(), namerAgent, "do it")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	other := getThread(t, ds, called.Thread)
	if _, ok := other.Properties[propThreadSubject]; ok {
		t.Fatalf("a call thread carries a subject: %+v", other.Properties)
	}
	agent, err := ds.Get(context.Background(), kindAgent, namerAgent)
	if err != nil {
		t.Fatal(err)
	}
	if other.Title == "" || other.Title != agent.Title {
		t.Fatalf("call thread title %q, want its agent's %q", other.Title, agent.Title)
	}
}

func TestTheFirstTurnTitlesTheThreadAndChargesTheRow(t *testing.T) {
	t.Parallel()
	ds, fake := openTitleDataset(t)
	fake.script("name", fakeTurn{content: "Berlin has a lot to see."},
		fakeTurn{content: `"Trip to Berlin".`, promptTokens: 10, cacheReadTokens: 6})
	res := chatOnce(t, ds, "", "what should I see in berlin")
	// The title call's usage (10 in, 6 of them cache reads, 5 out at 1 and
	// 5 USD per 1M, reads at the default tenth) lands on the row's tallies
	// beside the turn's own.
	rec := waitThread(t, ds, res.Thread, func(r *substrate.Record) bool {
		return r.Properties[propThreadSubject] == "Trip to Berlin"
	})
	if rec.Title != "Trip to Berlin" {
		t.Fatalf("title %q", rec.Title)
	}
	if m := subjectManager(rec); m != "agent:"+strings.ReplaceAll(namerAgent, "/", ":") {
		t.Fatalf("subject manager %q, want the agent's actor", m)
	}
	if got, _ := anyFloat(rec.Properties["totalTokens"]); got != 30 {
		t.Fatalf("totalTokens %v, want the turn's 15 plus the title's 15", got)
	}
	if got, _ := anyFloat(rec.Properties["cacheReadTokens"]); got != 6 {
		t.Fatalf("cacheReadTokens %v, want the title's 6", got)
	}
	if got, want := anyFloat2(rec.Properties["costUSD"]), (10*1.0+5*5.0+4*1.0+6*0.1+5*5.0)/1e6; got < want-1e-12 || got > want+1e-12 {
		t.Fatalf("costUSD %v, want %v", got, want)
	}
	reqs := fake.requestsOf("name")
	if len(reqs) != 2 {
		t.Fatalf("model calls %d, want the turn and the title", len(reqs))
	}
	if _, ok := reqs[1]["tools"]; ok {
		t.Fatalf("the title call offered tools: %v", reqs[1])
	}
	// No effort is sent, so the model may think by default: the call gets
	// the ceiling with room for it.
	if _, sent := reqs[1]["reasoning_effort"]; sent {
		t.Fatalf("the title call sent an effort the agent never named: %v", reqs[1]["reasoning_effort"])
	}
	if mt, _ := reqs[1]["max_completion_tokens"].(float64); int(mt) != titleReasoningMaxTokens {
		t.Fatalf("title call max tokens %v, want %d", reqs[1]["max_completion_tokens"], titleReasoningMaxTokens)
	}

	// A second turn makes no title call.
	fake.script("name", fakeTurn{content: "and more"})
	chatOnce(t, ds, res.Thread, "anything else?")
	time.Sleep(100 * time.Millisecond)
	if got := len(fake.requestsOf("name")); got != 3 {
		t.Fatalf("model calls %d after the second turn, want 3", got)
	}
	if s := getThread(t, ds, res.Thread).Properties[propThreadSubject]; s != "Trip to Berlin" {
		t.Fatalf("subject after the second turn %q", s)
	}
}

func TestAnOwnerRenameIsNeverOverwritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openTitleDataset(t)

	// The rename lands while the title call is in flight.
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("name", fakeTurn{content: "hello"}, fakeTurn{content: "Generated", arrived: arrived, release: release})
	res := chatOnce(t, ds, "", "first question")
	<-arrived
	if _, err := ds.Patch(ctx, substrate.ActorConsole, typeThread, res.Thread, substrate.PatchInput{
		Properties: map[string]any{propThreadSubject: "My rename"},
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	close(release)
	rec := waitThread(t, ds, res.Thread, func(r *substrate.Record) bool {
		v, _ := anyFloat(r.Properties["totalTokens"])
		return v == 30
	})
	if s := rec.Properties[propThreadSubject]; s != "My rename" || rec.Title != "My rename" {
		t.Fatalf("subject %q title %q, want the rename", s, rec.Title)
	}
	if m := subjectManager(rec); m != string(substrate.ActorConsole) {
		t.Fatalf("subject manager %q, want console", m)
	}

	// The rename after the generated title holds through a further turn.
	fake.script("name", fakeTurn{content: "hi"}, fakeTurn{content: "Second generated"})
	res2 := chatOnce(t, ds, "", "another chat")
	waitThread(t, ds, res2.Thread, func(r *substrate.Record) bool {
		return r.Properties[propThreadSubject] == "Second generated"
	})
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeThread, res2.Thread, substrate.PatchInput{
		Properties: map[string]any{propThreadSubject: "Kept"},
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	fake.script("name", fakeTurn{content: "again"})
	chatOnce(t, ds, res2.Thread, "go on")
	if rec := getThread(t, ds, res2.Thread); rec.Properties[propThreadSubject] != "Kept" {
		t.Fatalf("subject after a further turn %q, want Kept", rec.Properties[propThreadSubject])
	}
}

func TestATitleCallThatFailsKeepsTheProvisionalSubject(t *testing.T) {
	t.Parallel()
	ds, fake := openTitleDataset(t)
	for _, title := range []fakeTurn{{status: 500}, {content: ""}} {
		fake.script("name", fakeTurn{content: "fine"}, title)
		before := len(fake.requestsOf("name"))
		res := chatOnce(t, ds, "", "keep me")
		if res.Status != threadOK {
			t.Fatalf("turn status %q", res.Status)
		}
		deadline := time.Now().Add(10 * time.Second)
		for len(fake.requestsOf("name")) < before+2 {
			if time.Now().After(deadline) {
				t.Fatal("the title call was never made")
			}
			time.Sleep(20 * time.Millisecond)
		}
		// The empty reply still billed its usage; once that lands the task
		// is done. The failed call bills nothing and writes nothing.
		rec := getThread(t, ds, res.Thread)
		if title.status == 0 {
			rec = waitThread(t, ds, res.Thread, func(r *substrate.Record) bool {
				v, _ := anyFloat(r.Properties["totalTokens"])
				return v == 30
			})
		}
		if rec.Properties[propThreadSubject] != "keep me" || rec.Properties["status"] != threadOK {
			t.Fatalf("after a failed title call: %+v", rec.Properties)
		}
	}
}

func TestTitleModelOnTheProviderRowIsUsed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openTitleDataset(t)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeProvider, "namellm", substrate.PatchInput{Properties: map[string]any{
		"titleModel": "small",
		// The title model takes the row's effort, not the agent's; "none"
		// is the one effort that gets the bare title ceiling.
		"defaults": map[string]any{"reasoningEffort": "none"},
		"pricing": []any{
			map[string]any{"model": "name", "inputPer1M": "1", "outputPer1M": "5"},
			map[string]any{"model": "small", "inputPer1M": "100", "outputPer1M": "100"},
		},
	}}); err != nil {
		t.Fatalf("set titleModel: %v", err)
	}
	fake.script("name", fakeTurn{content: "hello"})
	fake.script("small", fakeTurn{content: "Small title"})
	res := chatOnce(t, ds, "", "hi there")
	rec := waitThread(t, ds, res.Thread, func(r *substrate.Record) bool {
		return r.Properties[propThreadSubject] == "Small title"
	})
	small := fake.requestsOf("small")
	if len(small) != 1 {
		t.Fatalf("title model calls %d", len(small))
	}
	if small[0]["reasoning_effort"] != "none" {
		t.Fatalf("title call effort %v, want the row's none", small[0]["reasoning_effort"])
	}
	if mt, _ := small[0]["max_completion_tokens"].(float64); int(mt) != titleMaxTokens {
		t.Fatalf("title call max tokens %v, want %d", small[0]["max_completion_tokens"], titleMaxTokens)
	}
	if got := len(fake.requestsOf("name")); got != 1 {
		t.Fatalf("thread model calls %d, want the turn alone", got)
	}
	// The turn at 1/5 per 1M, the title at 100/100 per 1M.
	want := (10*1.0+5*5.0)/1e6 + (10*100.0+5*100.0)/1e6
	if got, _ := anyFloat(rec.Properties["costUSD"]); got < want-1e-12 || got > want+1e-12 {
		t.Fatalf("costUSD %v, want %v", got, want)
	}
}

// The title call is spend like a turn: an admission on a snapshot read
// before it sees its charge once it commits, and a title whose thread was
// deleted keeps its charge as lost, because no row carries it.
func TestATitleCallCountsAgainstTheSpendCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	ds.mu.Lock()
	ds.skipThreadTitles = false
	ds.mu.Unlock()
	open := spendPackage + "/open"

	// Every run on the thread reports its end; the title's is the second.
	ended := make(chan *spendRun, 4)
	ds.spend.finishing = func(r *spendRun) { ended <- r }
	waitTitle := func() {
		t.Helper()
		for range 2 {
			select {
			case <-ended:
			case <-time.After(10 * time.Second):
				t.Fatal("the title run never ended")
			}
		}
	}

	// A default turn costs 100 cents on pricedllm, and so does a title.
	if got := spentOf(t, ds, "open", true); got != 0 {
		t.Fatalf("before: %d cents", got)
	}
	fake.script("open", fakeTurn{content: "hello"}, fakeTurn{content: "Greeting"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, open, "", "hi", nil); err != nil {
		t.Fatal(err)
	}
	waitTitle()
	if got := spentOf(t, ds, "open", false); got != 200 {
		t.Fatalf("on the snapshot read before the chat: %d cents, want the turn's 100 and the title's 100", got)
	}
	if got := spentOf(t, ds, "open", true); got != 200 {
		t.Fatalf("on a fresh snapshot: %d cents, want 200", got)
	}

	// The thread is deleted while its title call runs.
	arrived, release := make(chan struct{}), make(chan struct{})
	fake.script("open", fakeTurn{content: "hello"}, fakeTurn{content: "Gone", arrived: arrived, release: release})
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, open, "", "again", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-arrived
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeThread, res.Thread, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the thread: %v", err)
	}
	close(release)
	waitTitle()
	ds.spend.finishing = nil
	// 200 before, the deleted thread's turn 100 (a tombstone still counts),
	// and its title's 100 as a lost charge.
	if got := spentOf(t, ds, "open", true); got != 400 {
		t.Fatalf("after a title whose thread was deleted: %d cents, want 400", got)
	}
}
