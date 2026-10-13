package engine

// The public door in front of an agent callable: the bound on what the
// agent reads as its user message.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// putWebhookTrigger writes a webhook trigger on callable, the record path of
// a function or an agent, and returns its id.
func putWebhookTrigger(t *testing.T, ds *dataset, callable string, webhook map[string]any) string {
	t.Helper()
	tr, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"webhook": webhook},
			"callable": callable,
		},
	})
	if err != nil {
		t.Fatalf("put trigger on %s: %v", callable, err)
	}
	return tr.ID
}

// An agent reads a webhook request in its user message, which becomes an
// llm/message record and a model turn, so the door holds what rides inline
// to 1 MiB when the callable is an agent: a body, or multipart inline
// values. Past it the request is refused as too large and nothing is
// stored. A function callable still takes the door's 8 MiB.
func TestAnAgentWebhookIsHeldToOneMiB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	agent := putWebhookTrigger(t, ds, vocabulary.RecordPath(kindAgent, crewPackage+"/keeper"), map[string]any{})
	function := putWebhookTrigger(t, ds, vocabulary.RecordPath(kindFunction, crewPackage+"/keyecho"), map[string]any{})
	authority := testdb.Repository(t)
	body := func(n int) substrate.WebhookRequest {
		return substrate.WebhookRequest{
			Method: "POST", ContentType: "text/plain",
			Headers: map[string]string{"content-type": "text/plain"},
			Body:    bytes.Repeat([]byte("x"), n),
		}
	}
	form := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data", Headers: map[string]string{},
		Parts: []substrate.WebhookPart{
			{Name: "a", Value: string(bytes.Repeat([]byte("a"), 600<<10))},
			{Name: "b", Value: string(bytes.Repeat([]byte("b"), 600<<10))},
		},
	}
	stored := func(trigger string) int {
		t.Helper()
		rows, err := ds.TriggerFailures(ctx, trigger)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	for name, req := range map[string]substrate.WebhookRequest{
		"a body one byte past 1 MiB": body(1<<20 + 1),
		"two inline parts of 600 KiB": form,
	} {
		_, err := ds.svc.receiveWebhook(ctx, authority, agent, "", req, webhookFireHeld)
		if !errors.Is(err, substrate.ErrTooLarge) {
			t.Fatalf("%s to an agent: err = %v, want ErrTooLarge", name, err)
		}
	}
	if n := stored(agent); n != 0 {
		t.Fatalf("the refused requests left %d ledger rows", n)
	}
	if _, err := ds.svc.receiveWebhook(ctx, authority, agent, "", body(1<<20), webhookFireHeld); err != nil {
		t.Fatalf("a body of 1 MiB to an agent: %v", err)
	}
	if _, err := ds.svc.receiveWebhook(ctx, authority, function, "", body(2<<20), webhookFireHeld); err != nil {
		t.Fatalf("a body of 2 MiB to a function: %v", err)
	}
	if a, f := stored(agent), stored(function); a != 1 || f != 1 {
		t.Fatalf("pending rows: agent %d, function %d, want one each", a, f)
	}
}
