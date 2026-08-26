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
type llmStub struct {
	mu         sync.Mutex
	srv        *httptest.Server
	responders map[string]func(req llmReq) llmTurn
	requests   map[string]int // model -> how many completions it answered
}

type llmReq struct {
	Model    string       `json:"model"`
	Messages []llmMessage `json:"messages"`
}

type llmMessage struct {
	Role    string `json:"role"`
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
	w.Header().Set("Content-Type", "application/json")
	if fn == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":"no responder for model %s"}}`, req.Model)
		return
	}
	turn := fn(req)
	message := map[string]any{"role": "assistant", "content": turn.content}
	if len(turn.calls) > 0 {
		calls := make([]map[string]any, 0, len(turn.calls))
		for i, c := range turn.calls {
			args, err := json.Marshal(c.args)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, `{"error":{"message":"unmarshalable scripted args: %v"}}`, err)
				return
			}
			calls = append(calls, map[string]any{
				"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
				"function": map[string]any{"name": c.name, "arguments": string(args)},
			})
		}
		message["tool_calls"] = calls
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "cmpl", "object": "chat.completion",
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
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
