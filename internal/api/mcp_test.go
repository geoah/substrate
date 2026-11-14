package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/mcp"
	"github.com/geoah/substrate/internal/substrate"
)

// The MCP door's handler half: the challenge, the metadata, the JSON-RPC
// framing and the routes a tool may reach, on the stub. What a tool's
// request does is the engine's (mcp_db_test.go).

// rpc sends one JSON-RPC message to the door.
func rpc(t *testing.T, h http.Handler, token, method string, params any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		msg["params"] = params
	}
	raw, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, mcpPath, bytes.NewReader(raw))
	req.Host = "substrate.example.com"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type rpcAnswer struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolAnswer struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// callTool calls one tool and answers its text and whether it was an error.
func callTool(t *testing.T, h http.Handler, token, name string, args any) (string, bool) {
	t.Helper()
	rec := rpc(t, h, token, "tools/call", map[string]any{"name": name, "arguments": args})
	wantStatus(t, rec, http.StatusOK)
	ans := decodeJSON[rpcAnswer](t, rec)
	if ans.Error != nil {
		t.Fatalf("tools/call %s: %+v", name, ans.Error)
	}
	var res toolAnswer
	if err := json.Unmarshal(ans.Result, &res); err != nil || len(res.Content) != 1 {
		t.Fatalf("tools/call %s result = %s", name, ans.Result)
	}
	return res.Content[0].Text, res.IsError
}

// mcpAssistantInfo is an assistant's token as the stub's bearer opens it:
// `mcp` alone, minted for a program on the owner's device.
var mcpAssistantInfo = substrate.TokenInfo{
	ID: "tok1", Label: "Claude Code", Scopes: []string{"mcp"},
	ClientID: "https://claude.ai/oauth/claude-code-client-metadata", ClientName: "Claude Code", AppLocal: true,
}

// mcpStubEnv serves the handler on a stub whose bearer is an assistant's
// token, for what the door does with a message before any route it calls
// reaches a dataset.
func mcpStubEnv(t *testing.T, cfg ...func(*Config)) *testEnv {
	t.Helper()
	return newStubEnvOn(t, &stubService{ds: stubDataset{}, info: mcpAssistantInfo}, cfg...)
}

// Every method without a token is the 401 that starts a client's sign-in,
// naming the door's own metadata and the one scope that reaches it: both
// clients seen ask for every listed scope when the challenge names none,
// and Codex opens with a bearer-less GET.
func TestTheDoorChallengesEveryMethodWithoutAToken(t *testing.T) {
	env := newStubEnv(t, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := env.do(t, method, mcpPath, "", nil)
		wantStatus(t, rec, http.StatusUnauthorized)
		wa := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(wa, `scope="mcp"`) || !strings.Contains(wa, `resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp"`) {
			t.Fatalf("%s challenge = %q", method, wa)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("%s Cache-Control = %q, want no-store", method, cc)
		}
	}
	rec := env.do(t, http.MethodPost, mcpPath, "nope", nil)
	wantStatus(t, rec, http.StatusUnauthorized)
	if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="invalid_token"`) {
		t.Fatalf("a dead token's challenge = %q", wa)
	}
	// A browser opening the address is sent to the page that explains it.
	browser := env.do(t, http.MethodGet, mcpPath, "", nil, "Accept", "text/html")
	wantStatus(t, browser, http.StatusSeeOther)
	if loc := browser.Header().Get("Location"); loc != mcpConsolePage {
		t.Fatalf("browser sent to %q", loc)
	}
}

func TestTheDoorsMetadataNamesTheDoorAndTheMCPScope(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet, prMetadataPath+mcpPath, "", nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[protectedResourceMetadata](t, rec)
	if got.Resource != "https://example.com/mcp" || len(got.AuthorizationServers) != 1 ||
		got.AuthorizationServers[0] != "https://example.com" || len(got.ScopesSupported) != 1 || got.ScopesSupported[0] != "mcp" {
		t.Fatalf("metadata = %+v", got)
	}
}

func TestTheDoorSpeaksTheProtocolStatelessly(t *testing.T) {
	env := mcpStubEnv(t)
	tok := stubToken

	init := decodeJSON[rpcAnswer](t, rpc(t, env.h, tok, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	var res struct {
		ProtocolVersion string         `json:"protocolVersion"`
		ServerInfo      map[string]any `json:"serverInfo"`
		Instructions    string         `json:"instructions"`
	}
	if err := json.Unmarshal(init.Result, &res); err != nil || res.ProtocolVersion != "2025-06-18" ||
		res.ServerInfo["name"] != "substrate" || !strings.HasPrefix(res.Instructions, "Call overview first") {
		t.Fatalf("initialize = %s", init.Result)
	}
	// A version the door does not speak is answered with its newest.
	init = decodeJSON[rpcAnswer](t, rpc(t, env.h, tok, "initialize", map[string]any{"protocolVersion": "2099-01-01"}))
	if !strings.Contains(string(init.Result), `"protocolVersion":"`+mcp.Versions[0]+`"`) {
		t.Fatalf("initialize(2099) = %s", init.Result)
	}

	// The stateless revision's first call is method-not-found before the
	// header rule, so the client falls back to initialize.
	disc := rpc(t, env.h, tok, "server/discover", nil, "MCP-Protocol-Version", "2026-07-28")
	wantStatus(t, disc, http.StatusOK)
	if a := decodeJSON[rpcAnswer](t, disc); a.Error == nil || a.Error.Code != -32601 {
		t.Fatalf("server/discover = %s", disc.Body.String())
	}
	wantStatus(t, rpc(t, env.h, tok, "tools/list", nil, "MCP-Protocol-Version", "2026-07-28"), http.StatusBadRequest)

	list := decodeJSON[rpcAnswer](t, rpc(t, env.h, tok, "tools/list", nil, "MCP-Protocol-Version", "2025-11-25"))
	var tools struct {
		Tools []struct {
			Name        string         `json:"name"`
			Annotations map[string]any `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &tools); err != nil || len(tools.Tools) != len(mcp.Tools()) {
		t.Fatalf("tools/list = %s", list.Result)
	}
	for _, tool := range tools.Tools {
		for _, hint := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
			if _, ok := tool.Annotations[hint]; !ok {
				t.Fatalf("tool %s carries no %s: a client reads it as the worst case", tool.Name, hint)
			}
		}
	}

	// A notification is accepted with nothing to say; a token's GET or
	// DELETE has no stream and no session to reach.
	note, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	req := httptest.NewRequest(http.MethodPost, mcpPath, bytes.NewReader(note))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusAccepted)
	wantStatus(t, env.do(t, http.MethodGet, mcpPath, tok, nil), http.StatusMethodNotAllowed)
	wantStatus(t, env.do(t, http.MethodDelete, mcpPath, tok, nil), http.StatusMethodNotAllowed)

	// A page on another origin cannot drive the door.
	wantStatus(t, rpc(t, env.h, tok, "ping", nil, "Origin", "https://evil.example.org"), http.StatusForbidden)
	wantStatus(t, rpc(t, env.h, tok, "ping", nil, "Origin", "https://substrate.example.com"), http.StatusOK)
}

// A kind the model sends is held to three segments, and the door's requests
// to the route patterns its tools use, so no path a model builds reaches the
// approval calls or any other route. Each is refused before a dataset is
// reached: the stub has none to reach. What a tool's own route answers is
// the engine's (TestADoorToolsBodilessRequestReadsAsEmpty).
func TestADoorToolReachesTheRoutesItsToolsUseAlone(t *testing.T) {
	env := mcpStubEnv(t)
	tok := stubToken
	for _, kind := range []string{"authorizations", "authorizations/abc/x", "a/b", "../../tokens"} {
		text, isErr := callTool(t, env.h, tok, "query", map[string]any{"kind": kind, "id": "x"})
		if !isErr || !strings.Contains(text, "full reference") {
			t.Fatalf("query(kind %q) = %s", kind, text)
		}
	}
	// A request the door makes is held to the tools' route patterns, so even
	// one it built wrong lands nowhere else.
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/authorizations/abc"},
		{http.MethodDelete, "/api/v1/authorizations/abc"},
		{http.MethodGet, "/api/v1/export"},
		{http.MethodPost, "/api/v1/oauth/start"},
		{http.MethodPost, "/api/v1/vocabulary/takeover"},
		{http.MethodPost, "/api/v1/repository/domain"},
		{http.MethodGet, "/tokens"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req = req.WithContext(withMCPDoor(req.Context()))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s from the door = %d %s, want 403", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// The door's resource takes the `mcp` scope alone, and `mcp` sets nothing
// up and is asked for alone.
func TestTheDoorsResourceTakesTheMCPScopeAlone(t *testing.T) {
	env := newStubEnv(t, nil, func(c *Config) {
		c.ClientDocuments = &memDocs{docs: map[string]authorize.Document{
			appClientID: {ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Notes", URI: "https://notes.example.org"},
		}}
	})
	// The app's documents name the server at substrate.example.com, so the
	// door's own resource is https://substrate.example.com/mcp.
	serve := func(method, target, body, contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host = "substrate.example.com"
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range []map[string]string{
		{"scope": "full", "resource": "https://substrate.example.com/mcp"},
		{"scope": "read:" + taskKind, "resource": "https://substrate.example.com/mcp"},
		{"scope": "mcp read:" + taskKind},
		{"scope": "mcp", "authorization_details": `[{"type":"package","package":"x.example.org/notes"}]`},
		{"scope": "mcp", "resource": "https://substrate.example.com/elsewhere"},
	} {
		params := appRequest(c["scope"])
		for k, v := range c {
			if k != "scope" {
				params[k] = v
			}
		}
		rec := serve(http.MethodGet, authorizeURL(params), "", "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("authorize %v = %d, want the 400 page", c, rec.Code)
		}
	}
	form := tokenForm("nope")
	form.Set("resource", "https://substrate.example.com/elsewhere")
	rec := serve(http.MethodPost, "/oauth/token", form.Encode(), "application/x-www-form-urlencoded")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_target") {
		t.Fatalf("token with a foreign resource = %d %s", rec.Code, rec.Body.String())
	}
}

// A webhook's key is a credential handed out in the clear: an assistant
// never reads it back, and never filters or orders triggers on it.
func TestAnAssistantNeverReadsAWebhookKey(t *testing.T) {
	rec := &substrate.Record{ID: "hook", Kind: kindTrigger, Properties: map[string]any{
		"source": map[string]any{"webhook": map[string]any{"key": "s3cret", "path": "x"}},
	}}
	got := withoutKey(rec)
	if strings.Contains(mustJSON(t, got), "s3cret") {
		t.Fatalf("trigger read back with its key: %s", mustJSON(t, got))
	}
	if !strings.Contains(mustJSON(t, rec), "s3cret") {
		t.Fatal("cutting the key changed the record it was cut from")
	}
	if err := refuseTriggerSource(substrate.Filter{Properties: map[string]substrate.Cond{"source.webhook.key": {}}}, nil); err == nil {
		t.Fatal("a filter on a trigger's source was admitted")
	}
	changes := withoutTriggerPayloads([]substrate.Change{{Kind: kindTrigger, Payload: map[string]any{"source": "s3cret"}}})
	if changes[0].Payload != nil {
		t.Fatal("a trigger's change carried its payload")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
