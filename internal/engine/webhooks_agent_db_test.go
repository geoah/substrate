package engine

// An agent callable's webhook fire: the pending entry the door recorded
// becomes the loop's claim, so a process that dies mid-loop leaves an
// in-flight claim for a hand and not a pending entry the next open would run
// again, the rule every interrupted agent delivery follows (decision 0064).

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAnAgentWebhookInterruptedMidLoopWaitsForAHand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": map[string]any{}},
			"callable": vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	// The first model turn holds until the test releases it; the second is
	// what the retry by hand finishes on.
	arrived := make(chan struct{})
	release := make(chan struct{})
	fake.script("keep",
		fakeTurn{calls: []fakeCall{{"keyecho", `{}`}}, arrived: arrived, release: release},
		fakeTurn{content: "kept"})
	req := substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    []byte(`{"say":"agent"}`),
	}
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan string, 1)
	go func() {
		fid, err := ds.svc.receiveWebhook(fctx, testdb.Repository(t), tr.ID, "", req, webhookFireInline)
		if err != nil {
			t.Errorf("receive: %v", err)
		}
		done <- fid
	}()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the loop never reached its first model turn")
	}
	// The loop is at its first model turn: the entry reads as the claim, and a
	// hand is refused while this process runs it.
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 || failures[0].LastError != inFlightError {
		t.Fatalf("failures under the running loop = %+v (%v), want the claim", failures, err)
	}
	if _, err := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID); err == nil {
		t.Fatal("a hand retried the claim of a running loop")
	}
	// The process dies mid-loop: the fire returns without a park.
	cancel()
	fid := <-done
	close(release)
	failures, err = ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 || failures[0].LastError != inFlightError || failures[0].FireID != fid {
		t.Fatalf("failures after the interrupted loop = %+v (%v), want the claim under fire %s", failures, err, fid)
	}
	if pending, err := ds.pendingWebhooks(ctx, 0); err != nil || len(pending) != 0 {
		t.Fatalf("the next dispatcher pass would resume %+v (%v); an interrupted agent delivery waits for a hand", pending, err)
	}
	// The claim rewrite is its own delivery entry, above the admission's and
	// below the loop's first write.
	var firstLoopSeq int64
	if err := ds.db.QueryRowContext(ctx, `SELECT min(seq) FROM changelog WHERE kind = $1`, typeThread).Scan(&firstLoopSeq); err != nil {
		t.Fatal(err)
	}
	var claimSeq int64
	if err := ds.db.QueryRowContext(ctx, `
		SELECT coalesce(max(seq), 0) FROM changelog
		WHERE op = $1 AND record_id = $2 AND payload::text LIKE $3`,
		string(substrate.OpDelivery), tr.ID, "%"+inFlightError+"%").Scan(&claimSeq); err != nil {
		t.Fatal(err)
	}
	if claimSeq <= failures[0].ID || claimSeq >= firstLoopSeq {
		t.Fatalf("claim rewrite at seq %d, want above the admission %d and below the loop's first write %d", claimSeq, failures[0].ID, firstLoopSeq)
	}

	// A hand retries it: the loop runs again under the same fire id and the
	// claim retires.
	if _, err := ds.RetryTriggerFailure(ctx, tr.ID, failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if left, err := ds.TriggerFailures(ctx, tr.ID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retry: %+v (%v)", left, err)
	}
	if got := len(agentThreadsOf(t, ds, "keeper")); got != 2 {
		t.Fatalf("keeper threads: %d, want the interrupted one and the retry's", got)
	}
}
