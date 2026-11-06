// Package mcp is the substrate's Model Context Protocol door (decision 0230):
// the JSON-RPC an assistant such as Claude Code or Codex speaks, and the
// tools it calls. It is the Streamable HTTP transport's stateless form: one
// message in, one answer out, no session and no server stream, so nothing
// here outlives a request.
//
// Every tool is a sequence of REST requests made through API, which the
// HTTP layer answers in process with the caller's own bearer. The routes'
// validation, errors, idempotency and attribution are therefore the API's
// exactly, and no rule of docs/for-agents.md is restated here.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// Versions are the protocol revisions this door answers, newest first. An
// initialize naming another is answered with the newest. The stateless
// 2026-07-28 revision is not among them until it is final: its
// server/discover answers method-not-found, and both clients seen fall back
// to initialize on that.
var Versions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

// MaxResult bounds one tool result. Codex cuts a result at 10,000 tokens and
// Claude Code warns past 10,000, so an answer past this is refused with the
// way to page rather than cut mid-JSON.
const MaxResult = 32 << 10

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// API is the substrate's REST API as the door reaches it.
type API interface {
	Do(ctx context.Context, req Request) Response
}

// Request is one REST request under the API's version root.
type Request struct {
	Method string
	// Path is under /api/v1 and starts with a slash. Every id and kind
	// reference in it is the caller's to escape (Escape).
	Path  string
	Query url.Values
	// Body is sent as JSON when non-nil; Raw is sent as it is, with
	// ContentType, and wins over Body.
	Body        any
	Raw         []byte
	ContentType string
	// IdempotencyKey names a write the model may repeat.
	IdempotencyKey string
	// Timeout bounds the request; zero is the HTTP layer's own.
	Timeout time.Duration
}

// Response is a REST answer.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Server answers the protocol.
type Server struct {
	name, version string
	tools         []Tool
}

// NewServer is the door with the substrate's tool set.
func NewServer(name, version string) *Server {
	return &Server{name: name, version: version, tools: Tools()}
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Handle answers one POSTed message. It returns the HTTP status and the
// JSON to write; a nil body is 202 Accepted, the answer to a notification
// or to a client's response. versionHeader is the request's
// MCP-Protocol-Version, "" when absent.
func (s *Server) Handle(ctx context.Context, api API, body []byte, versionHeader string) (int, []byte) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return answerError(http.StatusBadRequest, nil, codeInvalidRequest, "a batch is not accepted: send one JSON-RPC message per request")
	}
	var m message
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return answerError(http.StatusBadRequest, nil, codeParse, "the body is not one JSON-RPC message")
	}
	if m.JSONRPC != "2.0" {
		return answerError(http.StatusBadRequest, m.ID, codeInvalidRequest, `jsonrpc must be "2.0"`)
	}
	// A response to a request this server never sends, or a notification:
	// nothing to answer.
	if m.Method == "" || len(m.ID) == 0 || string(m.ID) == "null" {
		return http.StatusAccepted, nil
	}
	// Before the header rule: a client trying the stateless revision sends
	// its version in the header, and must read method-not-found to fall
	// back to initialize.
	if m.Method == "server/discover" {
		return answerError(http.StatusOK, m.ID, codeMethodNotFound, "server/discover is not served: use initialize")
	}
	if m.Method != "initialize" && versionHeader != "" && !slices.Contains(Versions, versionHeader) {
		return answerError(http.StatusBadRequest, m.ID, codeInvalidRequest,
			fmt.Sprintf("protocol version %q is not one this server speaks: %v", versionHeader, Versions))
	}
	switch m.Method {
	case "initialize":
		return answer(m.ID, s.initialize(m.Params))
	case "ping":
		return answer(m.ID, map[string]any{})
	case "tools/list":
		return answer(m.ID, map[string]any{"tools": s.listTools()})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil || p.Name == "" {
			return answerError(http.StatusOK, m.ID, codeInvalidParams, "tools/call names a tool and its arguments")
		}
		i := slices.IndexFunc(s.tools, func(t Tool) bool { return t.Name == p.Name })
		if i < 0 {
			return answerError(http.StatusOK, m.ID, codeInvalidParams, "no tool named "+p.Name)
		}
		return answer(m.ID, s.call(ctx, api, s.tools[i], p.Arguments))
	default:
		return answerError(http.StatusOK, m.ID, codeMethodNotFound, "method "+m.Method+" is not served")
	}
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	version := Versions[0]
	if slices.Contains(Versions, p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		"instructions":    Instructions,
	}
}

func (s *Server) listTools() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"title":       t.Title,
			"description": t.Description,
			"inputSchema": t.Input,
			// Every hint is explicit: MCP reads a missing destructiveHint
			// as true, and Codex asks before any tool not marked read-only.
			"annotations": map[string]any{
				"title":           t.Title,
				"readOnlyHint":    t.Hints.ReadOnly,
				"destructiveHint": t.Hints.Destructive,
				"idempotentHint":  t.Hints.Idempotent,
				"openWorldHint":   false,
			},
		})
	}
	return out
}

// call runs one tool and shapes its result: one text block of compact JSON,
// with no structuredContent, since Codex shows the model that alone when it
// is present.
func (s *Server) call(ctx context.Context, api API, t Tool, args json.RawMessage) map[string]any {
	out, err := t.Run(ctx, api, args)
	if err != nil {
		return toolResult(err.Error(), true)
	}
	text, err := encode(out)
	if err != nil {
		return toolResult("the answer could not be encoded: "+err.Error(), true)
	}
	if len(text) > MaxResult {
		return toolResult(fmt.Sprintf("the answer is %d KiB, past the %d KiB one result carries: ask again with a smaller `first` and page with `after`, or read one record at a time",
			len(text)>>10, MaxResult>>10), true)
	}
	return toolResult(text, false)
}

func encode(v any) (string, error) {
	if raw, ok := v.(json.RawMessage); ok {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func answer(id json.RawMessage, result any) (int, []byte) {
	b, _ := json.Marshal(response{JSONRPC: "2.0", ID: id, Result: result})
	return http.StatusOK, b
}

func answerError(status int, id json.RawMessage, code int, msg string) (int, []byte) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	b, _ := json.Marshal(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
	return status, b
}
