package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/mcp"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// assistantToken registers an assistant's token, as approving `mcp` for a
// program on the owner's device mints it.
func assistantToken(svc *fakeService) string {
	return svc.tokenWith(fakeRepository, func(i *substrate.TokenInfo) {
		i.Scopes, i.ClientID, i.ClientName, i.AppLocal = []string{"mcp"}, "https://claude.ai/oauth/claude-code-client-metadata", "Claude Code", true
	})
}

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

// Every method without a token is the 401 that starts a client's sign-in,
// naming the door's own metadata and the one scope that reaches it: both
// clients seen ask for every listed scope when the challenge names none,
// and Codex opens with a bearer-less GET.
func TestTheDoorChallengesEveryMethodWithoutAToken(t *testing.T) {
	env := newTestEnv(t)
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
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, prMetadataPath+mcpPath, "", nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[protectedResourceMetadata](t, rec)
	if got.Resource != "https://example.com/mcp" || len(got.AuthorizationServers) != 1 ||
		got.AuthorizationServers[0] != "https://example.com" || len(got.ScopesSupported) != 1 || got.ScopesSupported[0] != "mcp" {
		t.Fatalf("metadata = %+v", got)
	}
}

// The door takes an assistant's token alone: a full-access one or an app's
// is told to sign in for `mcp`, so its writes are never the owner's.
func TestTheDoorAdmitsAnAssistantsTokenAlone(t *testing.T) {
	env := newTestEnv(t)
	for name, tok := range map[string]string{
		"full access": env.svc.token(fakeRepository),
		"an app's":    appToken(env.svc, fakeRepository, "read:"+taskKind),
	} {
		rec := rpc(t, env.h, tok, "initialize", nil)
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) || !strings.Contains(wa, `scope="mcp"`) {
			t.Fatalf("%s token's challenge = %q", name, wa)
		}
	}
	wantStatus(t, rpc(t, env.h, assistantToken(env.svc), "initialize", nil), http.StatusOK)
}

// An assistant's token works at the door alone: a copy out of the
// assistant's config reaches no REST route, the token mint and the export
// among them.
func TestAnAssistantsTokenWorksAtTheDoorAlone(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/records"},
		{http.MethodGet, "/api/v1/" + taskKind + "/t1"},
		{http.MethodGet, "/tokens"},
		{http.MethodPost, "/tokens"},
		{http.MethodGet, "/api/v1/export"},
	} {
		rec := env.do(t, c.method, c.path, tok, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s with an assistant's token = %d %s, want 403", c.method, c.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "/mcp alone") && c.path != "/tokens" {
			t.Fatalf("%s %s refusal does not name the door: %s", c.method, c.path, rec.Body.String())
		}
	}
}

func TestTheDoorSpeaksTheProtocolStatelessly(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)

	init := decodeJSON[rpcAnswer](t, rpc(t, env.h, tok, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	var res struct {
		ProtocolVersion string         `json:"protocolVersion"`
		ServerInfo      map[string]any `json:"serverInfo"`
		Instructions    string         `json:"instructions"`
	}
	if err := json.Unmarshal(init.Result, &res); err != nil || res.ProtocolVersion != "2025-06-18" ||
		res.ServerInfo["name"] != "substrate" || !strings.HasPrefix(res.Instructions, "Read the kinds tool") {
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

// A tool's request is an ordinary REST request in process, written as the
// assistant's own actor with its token as the principal.
func TestADoorToolWritesThroughTheAPIAsTheAssistant(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)
	text, isErr := callTool(t, env.h, tok, "write", map[string]any{
		"op": "put", "kind": taskKind, "id": "t/1", "properties": map[string]any{"note": "buy milk"},
	})
	if isErr {
		t.Fatalf("write = %s", text)
	}
	d := env.svc.datasets[fakeRepository]
	if d.lastActor != substrate.AppActorLocal || d.lastPrincipal != env.svc.tokens[tok].info.ID || d.lastPut.ID != "t/1" {
		t.Fatalf("wrote %q as %q by %q", d.lastPut.ID, d.lastActor, d.lastPrincipal)
	}
	// A create with a minted id is the write a retry could repeat, so its key
	// reaches the engine.
	if text, isErr := callTool(t, env.h, tok, "write", map[string]any{
		"op": "put", "kind": taskKind, "properties": map[string]any{"note": "call mum"}, "idempotencyKey": "k-1",
	}); isErr || d.lastIdempotencyKey != "k-1" {
		t.Fatalf("create = %s with key %q", text, d.lastIdempotencyKey)
	}
	text, isErr = callTool(t, env.h, tok, "query", map[string]any{"kind": taskKind, "id": "t/1"})
	if isErr || !strings.Contains(text, "buy milk") {
		t.Fatalf("query = %s", text)
	}
	// A REST refusal is the tool's error, carrying the API's error object.
	text, isErr = callTool(t, env.h, tok, "query", map[string]any{"kind": taskKind, "id": "missing"})
	if !isErr || !strings.Contains(text, "404") {
		t.Fatalf("query(missing) = %s", text)
	}
}

// A kind the model sends is held to three segments, and the door's requests
// to the route patterns its tools use, so no path a model builds reaches the
// approval calls or any other route.
func TestADoorToolReachesTheRoutesItsToolsUseAlone(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)
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
	// A bodiless request the door makes reads as an empty body, as a
	// server's request does, never a nil one a handler would panic on.
	text, isErr := callTool(t, env.h, tok, "packages", map[string]any{"action": "install", "id": "providers.substrate.reamde.dev/google"})
	if strings.Contains(text, "500") || strings.Contains(text, "internal") {
		t.Fatalf("a bodiless install = %v %s", isErr, text)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/units", nil)
	req = req.WithContext(withMCPDoor(req.Context()))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusOK)
}

// The assistant reads what the owner reads but never writes the auth
// material, and never uninstalls or purges a package.
func TestAnAssistantNeverWritesTheAuthMaterialOrRemovesAPackage(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)
	for _, args := range []map[string]any{
		{"op": "delete", "kind": "substrate.reamde.dev/core/token", "id": "tok1"},
	} {
		text, isErr := callTool(t, env.h, tok, "write", args)
		// The record path itself refuses a token's delete (it is revoked at
		// /tokens); every other write is the assistant's dataset's refusal.
		if !isErr || !(strings.Contains(text, "403") || strings.Contains(text, "405")) {
			t.Fatalf("write %v = %s, want refused", args, text)
		}
	}
	d := env.svc.datasets[fakeRepository]
	if d.lastPut.Kind != "" || d.lastActor != "" {
		t.Fatalf("a refused write reached the dataset: %+v as %q", d.lastPut, d.lastActor)
	}
	// The dataset refuses every write of each auth kind, whatever route
	// reaches it.
	a := newAssistantDataset(d)
	ctx := context.Background()
	for kind := range vocabulary.AuthKinds {
		for name, err := range map[string]error{
			"put": func() error {
				_, err := a.Put(ctx, substrate.AppActorLocal, substrate.PutInput{Kind: kind, ID: "x"})
				return err
			}(),
			"patch": func() error {
				_, err := a.Patch(ctx, substrate.AppActorLocal, kind, "x", substrate.PatchInput{})
				return err
			}(),
			"delete": func() error {
				_, err := a.Delete(ctx, substrate.AppActorLocal, kind, "x", substrate.DeleteInput{})
				return err
			}(),
			"merge": func() error {
				_, err := a.Merge(ctx, substrate.AppActorLocal, substrate.MergeInput{Kind: kind, Winner: "x", Loser: "y"})
				return err
			}(),
		} {
			if !errors.Is(err, substrate.ErrForbidden) {
				t.Fatalf("%s of %s = %v, want forbidden", name, kind, err)
			}
		}
	}
	if _, _, err := a.MintToken(ctx, substrate.MintTokenInput{Label: "x"}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("mint = %v", err)
	}
	if _, err := a.Export(ctx); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("export = %v", err)
	}
	if _, err := a.StartOAuth(ctx, substrate.AppActorLocal, "x"); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("oauth start = %v", err)
	}
	// A bundle PATCH to uninstalled or purging reaches these two.
	if err := a.UninstallBundle(ctx, "providers.substrate.reamde.dev/google"); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("uninstall = %v", err)
	}
	if _, err := a.PurgeBundle(ctx, "providers.substrate.reamde.dev/google"); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("purge = %v", err)
	}
}

// A decision on a change request whose accept would write auth material is
// refused before the patch reaches the request.
func TestAnAssistantNeverDecidesARequestThatWritesAuthMaterial(t *testing.T) {
	inner := newFakeService().addRepository(fakeRepository)
	inner.records["r1"] = &substrate.Record{
		ID: "r1", Kind: vocabulary.KindRecordPatchRequest,
		Properties: map[string]any{"target": map[string]any{"ref": "substrate.reamde.dev/core/token/tok1"}, "decision": "proposed"},
	}
	inner.records["r2"] = &substrate.Record{
		ID: "r2", Kind: vocabulary.KindRecordPatchRequest,
		Properties: map[string]any{"op": "create", "targetKind": "substrate.reamde.dev/core/secret", "targetId": "s1"},
	}
	a := newAssistantDataset(inner)
	for _, id := range []string{"r1", "r2"} {
		_, err := a.Patch(context.Background(), substrate.AppActorLocal, vocabulary.KindRecordPatchRequest, id, substrate.PatchInput{Properties: map[string]any{"decision": "accepted"}})
		if err == nil || !strings.Contains(err.Error(), "auth material") {
			t.Fatalf("deciding request %s = %v, want refused", id, err)
		}
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

// The whole flow for an assistant: the challenge names `mcp`, the request
// for the door's resource with no scope reads as `mcp`, the page selects
// until-revoked, and the token stores `["mcp"]` and works at the door.
func TestAnAssistantSignsInForTheDoor(t *testing.T) {
	e := newAppEnv(t)
	params := appRequest("")
	delete(params, "scope")
	params["resource"] = "https://substrate.example.com/mcp"
	id := e.park(params)
	view := e.show(id)
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(got.Scopes) != 1 || got.Scopes[0].Scope != "mcp" || got.DefaultLifetimeDays != 0 {
		t.Fatalf("an assistant's request = %+v, default %d", got.Scopes, got.DefaultLifetimeDays)
	}
	rec := e.approveShown(id, []string{"mcp"}, 0)
	wantStatus(t, rec, http.StatusOK)
	form := tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect))
	form.Set("resource", "https://substrate.example.com/mcp")
	tok := e.exchange(form)
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	if res.Scope != "mcp" || res.ExpiresIn != nil {
		t.Fatalf("token response = %+v", res)
	}
	if minted := e.svc.tokens[res.AccessToken].info; len(minted.Scopes) != 1 || minted.Scopes[0] != "mcp" || minted.ClientName != "Notes" {
		t.Fatalf("minted = %+v, want scopes [mcp]", minted)
	}
	wantStatus(t, rpc(t, e.h, res.AccessToken, "initialize", nil), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records", res.AccessToken, nil, ""), http.StatusForbidden)
}

// The door's resource takes the `mcp` scope alone, and `mcp` sets nothing
// up and is asked for alone.
func TestTheDoorsResourceTakesTheMCPScopeAlone(t *testing.T) {
	e := newAppEnv(t)
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
		rec := e.do(http.MethodGet, authorizeURL(params), "", nil, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("authorize %v = %d, want the 400 page", c, rec.Code)
		}
	}
	form := tokenForm("nope")
	form.Set("resource", "https://substrate.example.com/elsewhere")
	rec := e.exchange(form)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_target") {
		t.Fatalf("token with a foreign resource = %d %s", rec.Code, rec.Body.String())
	}
}

// A pasted token for an assistant is minted by the owner with scopes
// ["mcp"]; any other scopes are the authorization flow's.
func TestTheOwnerMintsAnAssistantsTokenForAPastedHeader(t *testing.T) {
	env := newTestEnv(t)
	owner := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodPost, "/tokens", owner, map[string]any{"label": "Cursor", "scopes": []string{"mcp"}})
	wantStatus(t, rec, http.StatusCreated)
	minted := decodeJSON[substrate.MintedToken](t, rec)
	if len(minted.Token.Scopes) != 1 || minted.Token.Scopes[0] != "mcp" {
		t.Fatalf("minted = %+v", minted.Token)
	}
	rec = env.do(t, http.MethodPost, "/tokens", owner, map[string]any{"label": "x", "scopes": []string{"read:*"}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
}

// The history tool pages through the real route: each page's cursor and
// generation, passed back, read the next page and nothing twice.
func TestTheHistoryToolReadsTwoPagesThroughTheRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)
	ds := env.svc.datasets[fakeRepository]
	seedRun(ds, 5, substrate.ActorAPI, runTask, substrate.OpPut, map[string]any{"created": true}, "")
	type page struct {
		Changes []struct {
			Seq int64 `json:"seq"`
		} `json:"changes"`
		Cursor     int64  `json:"cursor"`
		Generation string `json:"generation"`
	}
	read := func(args map[string]any) page {
		t.Helper()
		text, isErr := callTool(t, env.h, tok, "history", args)
		var p page
		if isErr || json.Unmarshal([]byte(text), &p) != nil {
			t.Fatalf("history %v = %s", args, text)
		}
		return p
	}
	first := read(map[string]any{"first": 3})
	if len(first.Changes) != 3 || first.Cursor == 0 || first.Generation == "" {
		t.Fatalf("first page = %+v", first)
	}
	second := read(map[string]any{"first": 3, "before": first.Cursor, "generation": first.Generation})
	if len(second.Changes) != 2 || second.Changes[0].Seq >= first.Changes[2].Seq {
		t.Fatalf("second page = %+v after %+v", second, first)
	}
}
