package llm

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/geoah/substrate/internal/llm/livespend"
)

// spendFake stands in for a provider: every completion answers with the same
// usage, and calls counts the requests that reached it.
type spendFake struct {
	usage Usage
	calls int
}

func (f *spendFake) Complete(context.Context, Request, func(string)) (*Result, error) {
	f.calls++
	u := f.usage
	return &Result{Content: "ok", Usage: &u}, nil
}

func spendRequest() Request {
	return Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}
}

// The live cases' meter, driven through a fake client: the request past the
// ceiling never reaches the provider, and the pass exits 1.
func TestSpendMeterStopsAtTheRequestCeiling(t *testing.T) {
	ledger := livespend.New("test", 2, 1000)
	fake := &spendFake{usage: Usage{PromptTokens: 10, CompletionTokens: 5}}
	client := liveMeter{"openai", ledger, fake}
	for i := range 2 {
		if _, err := client.Complete(context.Background(), spendRequest(), nil); err != nil {
			t.Fatalf("request %d of 2: %v", i+1, err)
		}
	}
	_, err := client.Complete(context.Background(), spendRequest(), nil)
	if !errors.Is(err, livespend.ErrOverBudget) {
		t.Fatalf("the third request of a two-request ceiling: %v, want ErrOverBudget", err)
	}
	if fake.calls != 2 {
		t.Fatalf("the provider saw %d requests, want 2: the refused one was sent", fake.calls)
	}
	if got := ledger.Exit(0, io.Discard); got != 1 {
		t.Fatalf("a pass past its request ceiling exits %d, want 1", got)
	}
}

func TestSpendMeterStopsAtTheTokenCeiling(t *testing.T) {
	ledger := livespend.New("test", 10, 100)
	fake := &spendFake{usage: Usage{PromptTokens: 80, CompletionTokens: 40}}
	client := liveMeter{"anthropic", ledger, fake}
	if _, err := client.Complete(context.Background(), spendRequest(), nil); err != nil {
		t.Fatalf("the first request: %v", err)
	}
	// 120 tokens booked against a ceiling of 100: the next request is refused.
	_, err := client.Complete(context.Background(), spendRequest(), nil)
	if !errors.Is(err, livespend.ErrOverBudget) {
		t.Fatalf("a request after the token ceiling: %v, want ErrOverBudget", err)
	}
	if fake.calls != 1 {
		t.Fatalf("the provider saw %d requests, want 1", fake.calls)
	}
	if got := ledger.Exit(0, io.Discard); got != 1 {
		t.Fatalf("a pass past its token ceiling exits %d, want 1", got)
	}
}

func TestSpendMeterWithinBothCeilingsPasses(t *testing.T) {
	ledger := livespend.New("test", 3, 100)
	fake := &spendFake{usage: Usage{PromptTokens: 20, CompletionTokens: 10}}
	client := liveMeter{"openai", ledger, fake}
	for i := range 3 {
		if _, err := client.Complete(context.Background(), spendRequest(), nil); err != nil {
			t.Fatalf("request %d of 3: %v", i+1, err)
		}
	}
	if fake.calls != 3 {
		t.Fatalf("the provider saw %d requests, want 3", fake.calls)
	}
	if got := ledger.Exit(0, io.Discard); got != 0 {
		t.Fatalf("a pass within both ceilings exits %d, want 0", got)
	}
}
