package api

// The Idempotency-Key header reaches the engine on the five operations whose
// effect the server assigns (docs/api.md "Idempotency and retries"), through
// the context the handler passes, and never on the agent chat stream. What
// the engine does with a key is idempotency_db_test.go's.

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// idempotencyRecorder is a repository that answers the five operations and
// the chat stream, and records the Idempotency-Key each one's context
// carried and the caller CallFunction was handed: what the handler passes
// is the claim, and only the handed context can show it.
type idempotencyRecorder struct {
	stubKinds

	mu     sync.Mutex
	keys   map[string]string
	caller substrate.Actor
}

func newIdempotencyRecorder() *idempotencyRecorder {
	return &idempotencyRecorder{stubKinds: newStubKinds(personKind), keys: map[string]string{}}
}

func (d *idempotencyRecorder) saw(ctx context.Context, method string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.keys[method] = substrate.IdempotencyKeyFrom(ctx)
}

func (d *idempotencyRecorder) key(method string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.keys[method]
}

func (d *idempotencyRecorder) Put(ctx context.Context, _ substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	d.saw(ctx, "Put")
	return &substrate.Record{ID: "r1", Kind: in.Kind, Version: 1, Created: true, Properties: in.Properties}, nil
}

func (d *idempotencyRecorder) Merge(ctx context.Context, _ substrate.Actor, _ substrate.MergeInput) (*substrate.Record, error) {
	d.saw(ctx, "Merge")
	return &substrate.Record{ID: "m1", Kind: corePackage + "/recordmerge", Version: 1, Created: true}, nil
}

func (d *idempotencyRecorder) Split(ctx context.Context, _ substrate.Actor, _ substrate.SplitInput) (*substrate.Record, error) {
	d.saw(ctx, "Split")
	return &substrate.Record{ID: "s1", Kind: corePackage + "/recordsplit", Version: 1, Created: true}, nil
}

func (d *idempotencyRecorder) CallFunction(ctx context.Context, caller substrate.Actor, _ string, _ any) (any, int, error) {
	d.saw(ctx, "CallFunction")
	d.mu.Lock()
	d.caller = caller
	d.mu.Unlock()
	return map[string]any{"ok": true}, 0, nil
}

func (d *idempotencyRecorder) CallAgent(ctx context.Context, _ string, _ any) (*substrate.AgentResult, error) {
	d.saw(ctx, "CallAgent")
	return &substrate.AgentResult{Reply: "done", Thread: "th1", Status: "ok"}, nil
}

func (d *idempotencyRecorder) ChatAgent(ctx context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	d.saw(ctx, "ChatAgent")
	res := &substrate.AgentResult{Reply: "done", Thread: "th1", Status: "ok"}
	emit(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
	return res, nil
}

// WatchSignal is a changelog that never moves: the chat stream subscribes
// to it, to read its token again when it moves (decision 0272).
func (d *idempotencyRecorder) WatchSignal(context.Context) <-chan int64 { return nil }

func TestIdempotencyKeyReachesTheFiveOperations(t *testing.T) {
	ds := newIdempotencyRecorder()
	env := newStubEnv(t, ds)
	const key = "attempt-7f3a"

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		status int
	}{
		{
			"create", "Put", recordsPath,
			map[string]any{"kind": personKind, "properties": map[string]any{"name": "Ada"}},
			http.StatusCreated,
		},
		{
			"merge", "Merge", "/api/v1/merge",
			map[string]any{"kind": personKind, "winner": "a", "loser": "b"},
			http.StatusCreated,
		},
		{
			"split", "Split", "/api/v1/split",
			map[string]any{"merge": "m1"},
			http.StatusCreated,
		},
		{
			"function call", "CallFunction", "/api/v1/substrate.reamde.dev/core/function/adder/call",
			map[string]any{"input": map[string]any{"title": "x"}},
			http.StatusOK,
		},
		{
			"agent call", "CallAgent", "/api/v1/substrate.reamde.dev/core/agent/helper/call",
			map[string]any{"input": "hi"},
			http.StatusOK,
		},
	}
	for _, tc := range cases {
		rec := env.do(t, http.MethodPost, tc.path, stubToken, tc.body, idempotencyHeader, key)
		wantStatus(t, rec, tc.status)
		if got := ds.key(tc.method); got != key {
			t.Fatalf("%s: the engine saw Idempotency-Key %q, want %q", tc.name, got, key)
		}
		// Without the header the operation carries no key.
		rec = env.do(t, http.MethodPost, tc.path, stubToken, tc.body)
		wantStatus(t, rec, tc.status)
		if got := ds.key(tc.method); got != "" {
			t.Fatalf("%s: a request without the header carried key %q", tc.name, got)
		}
	}

	// Chat is a stream: nothing to replay, so the handler never binds the key.
	rec := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/agent/helper/chat", stubToken,
		map[string]any{"message": "hi"}, idempotencyHeader, key)
	wantStatus(t, rec, http.StatusOK)
	if got := ds.key("ChatAgent"); got != "" {
		t.Fatalf("chat carried Idempotency-Key %q; the stream is excluded", got)
	}
}

// The X-Substrate-Actor header reaches CallFunction as its caller, the
// door a networked call's run row names (#645); no header is the API door.
func TestCallFunctionCallerIsTheRequestActor(t *testing.T) {
	ds := newIdempotencyRecorder()
	env := newStubEnv(t, ds)
	path := "/api/v1/substrate.reamde.dev/core/function/adder/call"
	body := map[string]any{"input": map[string]any{"title": "x"}}

	for _, tc := range []struct {
		header []string
		want   substrate.Actor
	}{
		{[]string{actorHeader, "console"}, substrate.ActorConsole},
		{nil, substrate.ActorAPI},
	} {
		rec := env.do(t, http.MethodPost, path, stubToken, body, tc.header...)
		wantStatus(t, rec, http.StatusOK)
		ds.mu.Lock()
		got := ds.caller
		ds.mu.Unlock()
		if got != tc.want {
			t.Fatalf("header %v: CallFunction saw caller %q, want %q", tc.header, got, tc.want)
		}
	}
}
