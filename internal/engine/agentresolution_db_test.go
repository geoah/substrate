package engine

// The notifies marker's delivery bounds (docs/plans/thread-interactions.md
// phase 1): the agent's own `resume: never`, the self-exclusion, the
// settle-time re-check that recovers a mid-turn resolution, the sweep that
// recovers a dropped one, and the conflicted accept reporting back. The happy
// path (decide → system row → resume) is agentdecision_db_test.go, unchanged
// by the marker migration on purpose.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// holdFor asserts a condition STAYS true for a short window — the negative
// twin of waitUntil, for "the continuation must not come".
func holdFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !check() {
			t.Fatalf("%s did not hold", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func assistantContents(t *testing.T, ds *dataset, threadID string) []string {
	t.Helper()
	var out []string
	for _, m := range threadMessages(t, ds, threadID) {
		if m["role"] == "assistant" {
			if c, _ := m["content"].(string); c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

func TestResumeNeverWithholdsTheContinuation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-stoic", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.script("stoic",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-stoic","diff":{"properties":{"name":"better"}}}`}}},
		fakeTurn{content: "proposed."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/stoic", "change the widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// The resolution row lands either way; the continuation never comes: the
	// script has no third turn, so a resume would flip the thread to error.
	if len(systemMessages(t, ds, res.Thread)) != 1 {
		t.Fatal("the resolution row did not land")
	}
	holdFor(t, "resume: never", func() bool {
		threads := agentThreadsOf(t, ds, "stoic")
		return len(threads) == 1 && threads[0]["status"] == threadOK &&
			len(fake.requestsOf("stoic")) == 2
	})
}

func TestSelfResolutionDoesNotResumeOwnThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-selfish", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	// Run 1 mints the thread the request will point at.
	fake.script("self", fakeTurn{content: "standing by."})
	first, err := ds.CallAgent(ctx, crewAuthority+"/selfjudge", "stand by")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	// The request names run 1's thread, exactly as a propose from that thread
	// would have stamped it.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest, ID: "req-self",
		Properties: map[string]any{
			"diff":   map[string]any{"properties": map[string]any{"name": "better"}},
			"thread": first.Thread,
			"target": vocabulary.RecordPath(crewAuthority+"/widget", "w-selfish"),
		},
	}); err != nil {
		t.Fatalf("put request: %v", err)
	}
	// Run 2: the SAME agent accepts it. The decider and the thread's agent
	// are one actor, so thread 1 gets the row and no continuation.
	fake.script("self",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, "req-self", "accepted")}}},
		fakeTurn{content: "accepted it."},
	)
	if _, err := ds.CallAgent(ctx, crewAuthority+"/selfjudge", "work the inbox"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got, err := ds.Get(ctx, crewAuthority+"/widget", "w-selfish"); err != nil || got.Properties["name"] != "better" {
		t.Fatalf("the accept did not land: %+v %v", got, err)
	}
	if len(systemMessages(t, ds, first.Thread)) != 1 {
		t.Fatal("the resolution row did not land on the proposing thread")
	}
	holdFor(t, "the self-exclusion", func() bool {
		return len(fake.requestsOf("self")) == 3 // one for run 1, two for run 2, none for a resume
	})
}

func TestSettleRecheckConsumesMidTurnResolutions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-mid", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	arrived := make(chan struct{})
	release := make(chan struct{})
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-mid","diff":{"properties":{"name":"better"}}}`}}},
		// The barrier turn: the loop is MID-RUN (lease held) while the test
		// accepts the proposal, so the accept's resume loses the lease.
		fakeTurn{content: "still thinking.", arrived: arrived, release: release},
		// The settle-time re-check's continuation reads the resolution row.
		fakeTurn{content: "saw the decision."},
	)
	done := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "change the widget")
		done <- err
	}()
	<-arrived
	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("call: %v", err)
	}
	threads := agentThreadsOf(t, ds, "classifier")
	if len(threads) != 1 {
		t.Fatalf("classifier threads: %d", len(threads))
	}
	threadID := threads[0]["__id"].(string)
	waitUntil(t, "the re-check's continuation", func() bool {
		for _, c := range assistantContents(t, ds, threadID) {
			if c == "saw the decision." {
				return true
			}
		}
		return false
	})
}

func TestSweepResumesDroppedResolutions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "hello."})
	res, err := ds.CallAgent(ctx, crewAuthority+"/chatter", "say hello")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// A resolution row lands AFTER the thread settled, with no resume — the
	// restart-drop shape. The sweep is what finds it.
	env, _ := json.Marshal(map[string]any{"event": "recordResolved", "record": "x/y/z", "state": "done"})
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeMessage,
		Properties: map[string]any{
			"role": msgRoleSystem, "content": string(env), "turn": 99, "thread": res.Thread,
		},
	}); err != nil {
		t.Fatalf("put resolution row: %v", err)
	}
	fake.script("chat", fakeTurn{content: "picked it up."})
	n, err := ds.SweepResolutions(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d threads, want 1", n)
	}
	waitUntil(t, "the swept continuation", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "picked it up." {
				return true
			}
		}
		return false
	})
	// A second sweep finds nothing: the continuation's settle moved
	// finishedAt past the row.
	waitUntil(t, "the sweep going quiet", func() bool {
		n, err := ds.SweepResolutions(ctx)
		return err == nil && n == 0
	})
}

func TestConflictedAcceptReportsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-moved", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-moved","diff":{"properties":{"name":"better"}}}`}}},
		fakeTurn{content: "proposed."},
		// The conflict notification resumes the thread too: the agent should
		// hear that its held write can no longer land as reviewed.
		fakeTurn{content: "understood, re-reading."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "change the widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// The target moves under the pending request, so the accept's CAS fails.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, crewAuthority+"/widget", "w-moved", substrate.PatchInput{
		Properties: map[string]any{"name": "owner-took-it"},
	}); err != nil {
		t.Fatal(err)
	}
	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err == nil {
		t.Fatal("a stale accept landed")
	}
	system := systemMessages(t, ds, res.Thread)
	if len(system) != 1 {
		t.Fatalf("system messages: %d, want the conflict report", len(system))
	}
	content, _ := system[0]["content"].(string)
	var env map[string]any
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		t.Fatalf("the report is not an envelope: %q", content)
	}
	if env["event"] != "proposalConflicted" || !strings.Contains(content, "w-moved") {
		t.Fatalf("envelope: %+v", env)
	}
	// The request is still proposed, annotated, and the thread resumed.
	fresh, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
	if err != nil || fresh.Properties["decision"] != "proposed" {
		t.Fatalf("request after conflict: %+v %v", fresh, err)
	}
	waitUntil(t, "the conflict resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "understood, re-reading." {
				return true
			}
		}
		return false
	})
}
