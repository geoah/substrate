package api

// The Idempotency-Key header reaches the engine on the five operations whose
// effect the server assigns (docs/api.md "Idempotency and retries"), through
// the context the handler passes, and never on the agent chat stream.

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// callableFake is the fake dataset with the automation and agent seams the
// plain fake does not carry. The trigger verbs are promoted from the nil
// interface and never called; the three callables record the key they saw.
type callableFake struct {
	*fakeDataset
	substrate.AutomationOps

	mu   sync.Mutex
	keys map[string]string
}

func (c *callableFake) saw(method, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[method] = key
}

func (c *callableFake) key(method string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[method]
}

func (c *callableFake) CallFunction(ctx context.Context, _ string, _ any) (any, int, error) {
	c.saw("CallFunction", substrate.IdempotencyKeyFrom(ctx))
	return map[string]any{"ok": true}, 0, nil
}

func (c *callableFake) CallAgent(ctx context.Context, _ string, _ any) (*substrate.AgentResult, error) {
	c.saw("CallAgent", substrate.IdempotencyKeyFrom(ctx))
	return &substrate.AgentResult{Reply: "done", Thread: "th1", Status: "ok"}, nil
}

func (c *callableFake) ChatAgent(ctx context.Context, _ substrate.Actor, _, _, _ string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	c.saw("ChatAgent", substrate.IdempotencyKeyFrom(ctx))
	res := &substrate.AgentResult{Reply: "done", Thread: "th1", Status: "ok"}
	emit(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
	return res, nil
}

func TestIdempotencyKeyReachesTheFiveOperations(t *testing.T) {
	env := newTestEnv(t)
	plain := env.svc.datasets[fakeRepository]
	callable := &callableFake{fakeDataset: plain, keys: map[string]string{}}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return callable }
	tok := env.svc.token(fakeRepository)
	const key = "attempt-7f3a"

	person := "samples.substrate.reamde.dev/people/person"
	cases := []struct {
		name   string
		path   string
		body   any
		status int
		saw    func() string
	}{
		{
			"create", "/api/v1/" + person,
			map[string]any{"properties": map[string]any{"name": "Ada"}},
			http.StatusCreated,
			func() string { return plain.lastIdempotencyKey },
		},
		{
			"merge", "/api/v1/merge",
			map[string]any{"kind": person, "winner": "a", "loser": "b"},
			http.StatusCreated,
			func() string { return plain.lastIdempotencyKey },
		},
		{
			"split", "/api/v1/split",
			map[string]any{"merge": "m1"},
			http.StatusCreated,
			func() string { return plain.lastIdempotencyKey },
		},
		{
			"function call", "/api/v1/substrate.reamde.dev/core/function/adder/call",
			map[string]any{"input": map[string]any{"title": "x"}},
			http.StatusOK,
			func() string { return callable.key("CallFunction") },
		},
		{
			"agent call", "/api/v1/substrate.reamde.dev/core/agent/helper/call",
			map[string]any{"input": "hi"},
			http.StatusOK,
			func() string { return callable.key("CallAgent") },
		},
	}
	for _, tc := range cases {
		rec := env.do(t, http.MethodPost, tc.path, tok, tc.body, idempotencyHeader, key)
		wantStatus(t, rec, tc.status)
		if got := tc.saw(); got != key {
			t.Fatalf("%s: the engine saw Idempotency-Key %q, want %q", tc.name, got, key)
		}
		// Without the header the operation carries no key.
		plain.lastIdempotencyKey = "stale"
		rec = env.do(t, http.MethodPost, tc.path, tok, tc.body)
		wantStatus(t, rec, tc.status)
		if got := tc.saw(); got != "" {
			t.Fatalf("%s: a request without the header carried key %q", tc.name, got)
		}
	}

	// Chat is a stream: nothing to replay, so the handler never binds the key.
	rec := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/agent/helper/chat", tok,
		map[string]any{"message": "hi"}, idempotencyHeader, key)
	wantStatus(t, rec, http.StatusOK)
	if got := callable.key("ChatAgent"); got != "" {
		t.Fatalf("chat carried Idempotency-Key %q; the stream is excluded", got)
	}
}

// A repeat under one key answers the first record with its `201`, and the
// same key with another body is the closed error set's `409 conflict` whose
// message names the key, the code the docs promise.
func TestIdempotencyKeyRepeatReplaysAndMismatchIs409(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	path := "/api/v1/samples.substrate.reamde.dev/people/person"
	body := map[string]any{"properties": map[string]any{"name": "Ada"}}

	first := env.do(t, http.MethodPost, path, tok, body, idempotencyHeader, "reused")
	wantStatus(t, first, http.StatusCreated)
	repeat := env.do(t, http.MethodPost, path, tok, body, idempotencyHeader, "reused")
	wantStatus(t, repeat, http.StatusCreated)
	if a, b := decodeJSON[substrate.Record](t, first), decodeJSON[substrate.Record](t, repeat); a.ID != b.ID {
		t.Fatalf("the repeat created %s, want the first attempt's %s", b.ID, a.ID)
	}

	rec := env.do(t, http.MethodPost, path, tok,
		map[string]any{"properties": map[string]any{"name": "Grace"}}, idempotencyHeader, "reused")
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `Idempotency-Key "reused"`) {
		t.Fatalf("the conflict does not name the key: %q", msg)
	}
}
