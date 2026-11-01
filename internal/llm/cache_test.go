package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Where each wire puts its cache marks, and that the prefix a run repeats is
// the same bytes on every call of the run: a cache matches a prefix byte for
// byte, so one reordered key re-bills the whole prompt at the write rate.

// rawServer records every request body verbatim and answers each with ok.
type rawServer struct {
	srv    *httptest.Server
	bodies [][]byte
}

func newRawServer(t *testing.T, wire Wire) *rawServer {
	t.Helper()
	s := &rawServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.bodies = append(s.bodies, body)
		if wire == WireAnthropic {
			anthropicMessageBody(w)
			return
		}
		jsonBody(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *rawServer) last(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(s.bodies[len(s.bodies)-1], &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	return body
}

// cacheTools are cards whose schemas are maps with many keys, the kind a map
// iteration order would scramble if anything marshaled them unsorted.
func cacheTools() []Tool {
	props := map[string]any{}
	for _, k := range []string{"zeta", "alpha", "mu", "beta", "omega", "kappa", "delta", "pi"} {
		props[k] = map[string]any{"type": "string", "description": k, "enum": []any{k + "1", k + "2"}}
	}
	return []Tool{
		{Name: "find", Description: "find records", Parameters: map[string]any{"type": "object", "properties": props, "required": []any{"alpha"}}},
		{Name: "write", Description: "write a record", Parameters: map[string]any{"type": "object", "properties": props}},
	}
}

// cacheRun is one run's two calls: the second repeats the first and appends
// the model's tool call and its answer.
func cacheRun(ttl string) (Request, Request) {
	first := Request{
		Model: "m", System: "you keep records",
		Messages: []Message{{Role: RoleUser, Content: "find the thing"}},
		Tools:    cacheTools(),
		Cache:    &Cache{Key: "thread-1", TTL: ttl},
	}
	second := first
	second.Messages = append(append([]Message(nil), first.Messages...),
		Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "toolu_1", Name: "find", Arguments: `{"alpha":"x"}`}}},
		Message{Role: RoleTool, ToolCallID: "toolu_1", ToolName: "find", Content: "found"},
	)
	return first, second
}

func complete(t *testing.T, c Client, req Request) {
	t.Helper()
	if _, err := c.Complete(context.Background(), req, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

// marks lists the JSON paths in v carrying cache_control, and the value.
func marks(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := map[string]string{}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if k == "cache_control" {
					b, _ := json.Marshal(child)
					out[path] = string(b)
					continue
				}
				walk(path+"."+k, child)
			}
		case []any:
			for i, child := range x {
				walk(path+"["+itoa(i)+"]", child)
			}
		}
	}
	walk("", v)
	return out
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func sameMarks(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("marks = %v, want %v", got, want)
	}
	for path, v := range want {
		if got[path] != v {
			t.Fatalf("marks = %v, want %v", got, want)
		}
	}
}

func TestAnthropicMarksTheToolsTheSystemAndTheTail(t *testing.T) {
	for name, tc := range map[string]struct {
		ttl  string
		mark string
	}{
		"an hour":                      {CacheTTL1h, `{"ttl":"1h","type":"ephemeral"}`},
		"five minutes sends no ttl":    {CacheTTL5m, `{"type":"ephemeral"}`},
		"the wire's default, unstated": {"", `{"type":"ephemeral"}`},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newRawServer(t, WireAnthropic)
			_, second := cacheRun(tc.ttl)
			complete(t, anthropicClientAt(t, srv.srv.URL), second)
			// Three of the wire's four breakpoints: the last tool, the
			// system prompt, and the last block of the last turn — here the
			// tool's answer, the newest thing the next call will repeat.
			sameMarks(t, marks(t, srv.bodies[0]), map[string]string{
				".tools[1]":               tc.mark,
				".system[0]":              tc.mark,
				".messages[2].content[0]": tc.mark,
			})
		})
	}
}

func TestAnthropicMarksNothingWhenTheRequestAsksForNoCache(t *testing.T) {
	srv := newRawServer(t, WireAnthropic)
	_, second := cacheRun(CacheTTL1h)
	second.Cache = nil
	complete(t, anthropicClientAt(t, srv.srv.URL), second)
	if bytes.Contains(srv.bodies[0], []byte("cache_control")) {
		t.Fatalf("an uncached request carries a mark: %s", srv.bodies[0])
	}
}

// Signed thinking cannot carry a breakpoint, so a tail ending in one is
// marked on the block before it.
func TestAnthropicMarksTheTailPastSignedThinking(t *testing.T) {
	turns := []anthropicTurn{{Role: RoleAssistant, Content: []any{
		newAnthropicText("said"),
		json.RawMessage(`{"type":"thinking","thinking":"","signature":"s"}`),
	}}}
	markTail(turns, &anthropicCacheControl{Type: "ephemeral"})
	if b := turns[0].Content[0].(anthropicText); b.CacheControl == nil {
		t.Fatalf("the text before the thinking is unmarked: %+v", turns[0].Content)
	}
}

// The tools and the system prompt are the same bytes on both calls of a run,
// and the first call's conversation is a prefix of the second's once the
// moving tail mark is set aside.
func TestAnthropicRepeatsTheRunsPrefixByteForByte(t *testing.T) {
	srv := newRawServer(t, WireAnthropic)
	client := anthropicClientAt(t, srv.srv.URL)
	first, second := cacheRun(CacheTTL1h)
	complete(t, client, first)
	a := srv.last(t)
	complete(t, client, second)
	b := srv.last(t)
	for _, key := range []string{"tools", "system"} {
		if !bytes.Equal(a[key], b[key]) {
			t.Fatalf("%s differs between the calls of one run:\n%s\n%s", key, a[key], b[key])
		}
	}
	var am, bm []json.RawMessage
	if err := json.Unmarshal(a["messages"], &am); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b["messages"], &bm); err != nil {
		t.Fatal(err)
	}
	if len(bm) <= len(am) {
		t.Fatalf("the second call did not grow: %d then %d turns", len(am), len(bm))
	}
	unmark := func(raw json.RawMessage) []byte {
		return bytes.ReplaceAll(raw, []byte(`,"cache_control":{"type":"ephemeral","ttl":"1h"}`), nil)
	}
	for i := range am {
		if !bytes.Equal(unmark(am[i]), unmark(bm[i])) {
			t.Fatalf("turn %d differs:\n%s\n%s", i, am[i], bm[i])
		}
	}
	// The marshal is not trusted to be stable by one sample: a map ranged
	// unsorted anywhere under the schemas would show within a few tries.
	for range 20 {
		complete(t, client, second)
		c := srv.last(t)
		if !bytes.Equal(b["tools"], c["tools"]) {
			t.Fatalf("tools differ between identical requests:\n%s\n%s", b["tools"], c["tools"])
		}
	}
}

func openaiClientAt(t *testing.T, baseURL string, cfg Config) Client {
	t.Helper()
	cfg.BaseURL, cfg.APIKey = baseURL, "k"
	c, err := New(WireOpenAI, cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func TestOpenAISendsTheCacheFieldsOnlyWhereTheRowOptsIn(t *testing.T) {
	mark := `{"ttl":"1h","type":"ephemeral"}`
	for name, tc := range map[string]struct {
		cfg   Config
		cache bool
		key   string
		marks map[string]string
	}{
		"no opt-in sends nothing": {cache: true, marks: map[string]string{}},
		"the key": {
			cfg: Config{CacheKey: true}, cache: true, key: `"thread-1"`, marks: map[string]string{},
		},
		// LiteLLM reads a whole message's cache_control and a tool's own,
		// and moves each onto the Anthropic block it builds.
		"the breakpoints": {
			cfg: Config{CacheBreakpoints: true}, cache: true,
			marks: map[string]string{".tools[1]": mark, ".messages[0]": mark, ".messages[3]": mark},
		},
		"both": {
			cfg: Config{CacheKey: true, CacheBreakpoints: true}, cache: true, key: `"thread-1"`,
			marks: map[string]string{".tools[1]": mark, ".messages[0]": mark, ".messages[3]": mark},
		},
		"a request that asks for no cache": {
			cfg: Config{CacheKey: true, CacheBreakpoints: true}, marks: map[string]string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newRawServer(t, WireOpenAI)
			_, second := cacheRun(CacheTTL1h)
			if !tc.cache {
				second.Cache = nil
			}
			complete(t, openaiClientAt(t, srv.srv.URL, tc.cfg), second)
			body := srv.last(t)
			if got := string(body["prompt_cache_key"]); got != tc.key {
				t.Fatalf("prompt_cache_key = %s, want %s", got, tc.key)
			}
			sameMarks(t, marks(t, srv.bodies[0]), tc.marks)
		})
	}
}

// The tools are the same bytes across the calls of a run on this wire too,
// mark included.
func TestOpenAIRepeatsTheRunsToolsByteForByte(t *testing.T) {
	srv := newRawServer(t, WireOpenAI)
	client := openaiClientAt(t, srv.srv.URL, Config{CacheKey: true, CacheBreakpoints: true})
	first, second := cacheRun(CacheTTL5m)
	complete(t, client, first)
	a := srv.last(t)
	complete(t, client, second)
	b := srv.last(t)
	if !bytes.Equal(a["tools"], b["tools"]) {
		t.Fatalf("tools differ between the calls of one run:\n%s\n%s", a["tools"], b["tools"])
	}
	if !strings.Contains(string(b["tools"]), `"cache_control":{"type":"ephemeral"}`) {
		t.Fatalf("a five-minute mark carries a ttl or is missing: %s", b["tools"])
	}
}
