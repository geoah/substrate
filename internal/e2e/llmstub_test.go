package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
)

// llmStub answers the OpenAI wire (`POST /chat/completions`) for the story
// agents. Each agent names its own model id, and each model id maps to one
// RESPONDER: a pure function from the request (the whole message history) to
// the next turn. Responders instead of fixed scripts because the stories
// need runtime values a static script cannot know (the server assigns
// recordpatchrequest ids), while staying fully deterministic: the same
// history always answers the same turn.
//
// One responder answers BOTH wires: a request carrying `stream: true` (chat)
// gets the same turn back as SSE chunk frames, a request without it (call and
// every trigger delivery) gets one JSON body.
type llmStub struct {
	mu         sync.Mutex
	srv        *httptest.Server
	responders map[string]func(req llmReq) llmTurn
	requests   map[string]int // model -> how many completions it answered
}

type llmReq struct {
	Model    string       `json:"model"`
	Messages []llmMessage `json:"messages"`
	// Stream is the caller asking for SSE instead of one JSON body: the chat
	// surface streams and the call surface does not, so the same responder
	// answers both wires and no responder knows which one it is on.
	Stream bool `json:"stream"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type llmTurn struct {
	content string
	calls   []llmCall
}

type llmCall struct {
	name string
	args any // marshaled to the tool_call arguments JSON string
}

func newLLMStub() *llmStub {
	s := &llmStub{responders: map[string]func(llmReq) llmTurn{}, requests: map[string]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *llmStub) close()      { s.srv.Close() }
func (s *llmStub) url() string { return s.srv.URL }

func (s *llmStub) respond(model string, fn func(llmReq) llmTurn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responders[model] = fn
}

func (s *llmStub) count(model string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[model]
}

func (s *llmStub) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" {
		http.NotFound(w, r)
		return
	}
	var req llmReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	fn := s.responders[req.Model]
	s.requests[req.Model]++
	s.mu.Unlock()
	if fn == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":"no responder for model %s"}}`, req.Model)
		return
	}
	turn := fn(req)
	calls, err := turn.wireCalls()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":"unmarshalable scripted args: %v"}}`, err)
		return
	}
	if req.Stream {
		writeStreamedTurn(w, turn, calls)
		return
	}
	message := map[string]any{"role": "assistant", "content": turn.content}
	if len(calls) > 0 {
		message["tool_calls"] = calls
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "cmpl", "object": "chat.completion",
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": "stop"}},
		"usage":   stubUsage(),
	})
}

// stubUsage is the tally every turn reports, on both wires. It is fixed, so a
// case that prices a model computes the cost it expects from these numbers.
func stubUsage() map[string]any {
	return map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
}

// wireCalls renders a scripted turn's tool calls as the wire's objects. Both
// answers send the same ones, so a responder cannot behave differently
// depending on whether the caller streamed.
func (t llmTurn) wireCalls() ([]map[string]any, error) {
	if len(t.calls) == 0 {
		return nil, nil
	}
	calls := make([]map[string]any, 0, len(t.calls))
	for i, c := range t.calls {
		args, err := json.Marshal(c.args)
		if err != nil {
			return nil, err
		}
		calls = append(calls, map[string]any{
			"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
			"function": map[string]any{"name": c.name, "arguments": string(args)},
		})
	}
	return calls, nil
}

// writeStreamedTurn answers the streaming wire: SSE chunk frames, then a
// choice-less final chunk carrying the tally, then the terminator. The tally
// rides that last chunk because the loop's cost accounting only ever sees a
// stream's usage there. Content splits in two frames so a collector that
// drops a fragment shows up as a short reply rather than as nothing.
func writeStreamedTurn(w http.ResponseWriter, turn llmTurn, calls []map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	flusher, _ := w.(http.Flusher)
	chunk := func(payload map[string]any) {
		buf, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", buf)
		if flusher != nil {
			flusher.Flush()
		}
	}
	// Split on a byte index: every scripted content is ASCII, and a case that
	// writes otherwise would see its own mangled rune.
	if turn.content != "" {
		half := len(turn.content) / 2
		for _, part := range []string{turn.content[:half], turn.content[half:]} {
			if part == "" {
				continue
			}
			chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"content": part}},
			}})
		}
	}
	for _, call := range calls {
		chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}},
		}})
	}
	chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{}, "usage": stubUsage()})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// envelope decodes the delivery envelope a trigger hands the agent: the
// first user message of the thread.
func (r llmReq) envelope() map[string]any {
	for _, m := range r.Messages {
		if m.Role == "user" {
			var env map[string]any
			if json.Unmarshal([]byte(m.Content), &env) == nil {
				return env
			}
			return nil
		}
	}
	return nil
}

// deliveredRecord pulls the record out of the delivery envelope.
func (r llmReq) deliveredRecord() map[string]any {
	env := r.envelope()
	rec, _ := env["record"].(map[string]any)
	return rec
}

// assistantTurns counts the turns already taken, which is what makes a
// responder a deterministic script over a growing history.
func (r llmReq) assistantTurns() int {
	n := 0
	for _, m := range r.Messages {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

// lastToolResult decodes the newest tool answer with the given name, so a
// responder's next move can depend on what the tool actually said.
func (r llmReq) lastToolResult(name string) map[string]any {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "tool" && r.Messages[i].Name == name {
			var out map[string]any
			if json.Unmarshal([]byte(r.Messages[i].Content), &out) == nil {
				return out
			}
			return nil
		}
	}
	return nil
}
