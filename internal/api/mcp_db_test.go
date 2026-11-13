package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// mcpAssistantToken mints an assistant's token on the engine, as approving
// `mcp` for a program on the owner's device mints it, and answers its record
// id beside its secret.
func mcpAssistantToken(t *testing.T, env *dbEnv) (id, secret string) {
	t.Helper()
	return appDeliveryMint(t, env.ds, substrate.MintTokenInput{
		Label: "Claude Code", Scopes: []string{"mcp"},
		ClientID: "https://claude.ai/oauth/claude-code-client-metadata", ClientName: "Claude Code", AppLocal: true,
	})
}

// The door takes an assistant's token alone: a full-access one or an app's
// is told to sign in for `mcp`, so its writes are never the owner's.
func TestTheDoorAdmitsAnAssistantsTokenAlone(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	for name, tok := range map[string]string{
		"full access": env.token,
		"an app's":    appScopedToken(t, env, "read:"+personKind),
	} {
		rec := rpc(t, env.h, tok, "initialize", nil)
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) || !strings.Contains(wa, `scope="mcp"`) {
			t.Fatalf("%s token's challenge = %q", name, wa)
		}
	}
	_, assistant := mcpAssistantToken(t, env)
	wantStatus(t, rpc(t, env.h, assistant, "initialize", nil), http.StatusOK)
}

// An assistant's token works at the door alone: a copy out of the
// assistant's config reaches no REST route, the token mint and the export
// among them.
func TestAnAssistantsTokenWorksAtTheDoorAlone(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	_, tok := mcpAssistantToken(t, env)
	before, err := env.ds.Tokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
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
	if after, err := env.ds.Tokens(context.Background()); err != nil || len(after) != len(before) {
		t.Fatalf("a refused mint left %d tokens from %d: %v", len(after), len(before), err)
	}
}

// The overview's reads, and the guide, are all on the door's routes: none
// of them is refused there.
func TestTheDoorsOverviewReadsItsRoutes(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	_, tok := mcpAssistantToken(t, env)
	text, isErr := callTool(t, env.h, tok, "overview", map[string]any{})
	if isErr || strings.Contains(text, "answered 403") || !strings.Contains(text, `"guide"`) {
		t.Fatalf("overview = %s", text)
	}
	if text, isErr = callTool(t, env.h, tok, "guide", map[string]any{"topic": "records"}); isErr || !strings.HasPrefix(text, "# ") {
		t.Fatalf("guide = %.200s", text)
	}
}

// A tool's request is an ordinary REST request in process, written as the
// assistant's own actor with its token as the principal.
func TestADoorToolWritesThroughTheAPIAsTheAssistant(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	tokID, tok := mcpAssistantToken(t, env)
	id := mintedID("t1")
	text, isErr := callTool(t, env.h, tok, "write", map[string]any{
		"op": "put", "kind": taskKind, "id": id, "properties": map[string]any{"name": "buy milk"},
	})
	if isErr {
		t.Fatalf("write = %s", text)
	}
	changes, err := env.ds.Changes(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{taskKind}, RecordID: id}, 10)
	if err != nil || len(changes) != 1 || changes[0].Actor != substrate.AppActorLocal || changes[0].Principal != tokID {
		t.Fatalf("the write's entries = %+v, %v: want one as %q by %q", changes, err, substrate.AppActorLocal, tokID)
	}
	// A create with a minted id is the write a retry could repeat, so its key
	// reaches the engine: the retry is the same record.
	create := map[string]any{"op": "put", "kind": taskKind, "properties": map[string]any{"name": "call mum"}, "idempotencyKey": "k-1"}
	first, isErr := callTool(t, env.h, tok, "write", create)
	if isErr {
		t.Fatalf("create = %s", first)
	}
	again, isErr := callTool(t, env.h, tok, "write", create)
	if isErr {
		t.Fatalf("the retried create = %s", again)
	}
	var a, b substrate.Record
	if json.Unmarshal([]byte(first), &a) != nil || json.Unmarshal([]byte(again), &b) != nil || a.ID == "" || a.ID != b.ID {
		t.Fatalf("a create and its retry under one key answered %s and %s, want one record", first, again)
	}
	if n := appScopedCount(t, env, taskKind); n != 2 {
		t.Fatalf("the repository holds %d tasks, want the two the writes named", n)
	}
	text, isErr = callTool(t, env.h, tok, "query", map[string]any{"kind": taskKind, "id": id})
	if isErr || !strings.Contains(text, "buy milk") {
		t.Fatalf("query = %s", text)
	}
	// A REST refusal is the tool's error, carrying the API's error object.
	text, isErr = callTool(t, env.h, tok, "query", map[string]any{"kind": taskKind, "id": "missing"})
	if !isErr || !strings.Contains(text, "404") {
		t.Fatalf("query(missing) = %s", text)
	}
}

// A bodiless request the door makes reads as an empty body, as a server's
// request does, never a nil one a handler would panic on.
func TestADoorToolsBodilessRequestReadsAsEmpty(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	_, tok := mcpAssistantToken(t, env)
	text, isErr := callTool(t, env.h, tok, "packages", map[string]any{"action": "add", "id": "providers.substrate.reamde.dev/google"})
	if strings.Contains(text, "500") || strings.Contains(text, "internal") {
		t.Fatalf("a bodiless add = %v %s", isErr, text)
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
	t.Parallel()
	env := newDBEnv(t)
	tokID, tok := mcpAssistantToken(t, env)
	text, isErr := callTool(t, env.h, tok, "write", map[string]any{"op": "delete", "kind": tokenType, "id": tokID})
	// The record path itself refuses a token's delete (it is revoked at
	// /tokens); every other write is the assistant's dataset's refusal.
	if !isErr || !(strings.Contains(text, "403") || strings.Contains(text, "405")) {
		t.Fatalf("write delete of a token = %s, want refused", text)
	}
	if got := env.get(t, tokenType, tokID); got == nil || got.DeletedAt != nil {
		t.Fatalf("a refused delete took the token: %+v", got)
	}
	// The dataset refuses every write of each auth kind, whatever route
	// reaches it, before the engine is asked.
	a := newAssistantDataset(env.ds)
	ctx := context.Background()
	tokens, err := env.ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
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
		if got := env.get(t, kind, "x"); got != nil {
			t.Fatalf("a refused write stored %s/x: %+v", kind, got)
		}
	}
	if _, _, err := a.MintToken(ctx, substrate.MintTokenInput{Label: "x"}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("mint = %v", err)
	}
	if after, err := env.ds.Tokens(ctx); err != nil || len(after) != len(tokens) {
		t.Fatalf("a refused mint left %d tokens from %d: %v", len(after), len(tokens), err)
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
	t.Parallel()
	env := newDBEnv(t)
	target, _ := appDeliveryMint(t, env.ds, substrate.MintTokenInput{Label: "target"})
	env.put(t, vocabulary.KindRecordPatchRequest, "r1", map[string]any{
		"target": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(tokenType, target)}, "decision": "proposed",
		"diff": map[string]any{"properties": map[string]any{"label": "taken"}},
	})
	env.put(t, vocabulary.KindRecordPatchRequest, "r2", map[string]any{
		"op": "create", "targetKind": "substrate.reamde.dev/core/secret", "targetId": "s1", "decision": "proposed",
		"diff": map[string]any{"properties": map[string]any{"displayName": "a key"}},
	})
	a := newAssistantDataset(env.ds)
	for _, id := range []string{"r1", "r2"} {
		v := env.get(t, vocabulary.KindRecordPatchRequest, id).Version
		_, err := a.Patch(context.Background(), substrate.AppActorLocal, vocabulary.KindRecordPatchRequest, id, substrate.PatchInput{
			Properties: map[string]any{"decision": "accepted"}, IfVersion: &v,
		})
		if err == nil || !strings.Contains(err.Error(), "auth material") {
			t.Fatalf("deciding request %s = %v, want refused", id, err)
		}
		if got := env.get(t, vocabulary.KindRecordPatchRequest, id); got.Properties["decision"] != "proposed" {
			t.Fatalf("the refused decision moved request %s to %v", id, got.Properties["decision"])
		}
	}
}

// The whole flow for an assistant: the challenge names `mcp`, the request
// for the door's resource with no scope reads as `mcp`, the page selects
// until-revoked, and the token stores `["mcp"]` and works at the door.
func TestAnAssistantSignsInForTheDoor(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withAppFlow())
	params := appRequest("")
	delete(params, "scope")
	params["resource"] = "https://substrate.example.com/mcp"
	id := appScopedPark(t, env, params)
	view := appScopedShow(t, env, id)
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(got.Scopes) != 1 || got.Scopes[0].Scope != "mcp" || got.DefaultLifetimeDays != 0 {
		t.Fatalf("an assistant's request = %+v, default %d", got.Scopes, got.DefaultLifetimeDays)
	}
	rec := appScopedApprove(t, env, id, []string{"mcp"}, 0)
	wantStatus(t, rec, http.StatusOK)
	form := tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect))
	form.Set("resource", "https://substrate.example.com/mcp")
	tok := appScopedExchange(t, env, form)
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	if res.Scope != "mcp" || res.ExpiresIn != nil {
		t.Fatalf("token response = %+v", res)
	}
	tokens, err := env.ds.Tokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var minted *substrate.TokenInfo
	for i := range tokens {
		if tokens[i].ClientID == appClientID {
			minted = &tokens[i]
		}
	}
	if minted == nil || len(minted.Scopes) != 1 || minted.Scopes[0] != "mcp" || minted.ClientName != "Notes" || minted.ExpiresAt != nil {
		t.Fatalf("minted = %+v, want scopes [mcp] until revoked", minted)
	}
	wantStatus(t, rpc(t, env.h, res.AccessToken, "initialize", nil), http.StatusOK)
	wantStatus(t, appScopedDo(t, env, http.MethodGet, "/api/v1/records", res.AccessToken, nil, ""), http.StatusForbidden)
}

// A pasted token for an assistant is minted by the owner with scopes
// ["mcp"]; any other scopes are the authorization flow's.
func TestTheOwnerMintsAnAssistantsTokenForAPastedHeader(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	rec := env.do(t, http.MethodPost, "/tokens", env.token, map[string]any{"label": "Cursor", "scopes": []string{"mcp"}})
	wantStatus(t, rec, http.StatusCreated)
	minted := decodeJSON[substrate.MintedToken](t, rec)
	if len(minted.Token.Scopes) != 1 || minted.Token.Scopes[0] != "mcp" {
		t.Fatalf("minted = %+v", minted.Token)
	}
	wantStatus(t, rpc(t, env.h, minted.Secret, "initialize", nil), http.StatusOK)
	before, err := env.ds.Tokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, http.MethodPost, "/tokens", env.token, map[string]any{"label": "x", "scopes": []string{"read:*"}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if after, err := env.ds.Tokens(context.Background()); err != nil || len(after) != len(before) {
		t.Fatalf("a refused mint left %d tokens from %d: %v", len(after), len(before), err)
	}
}

// The history tool pages through the real route: each page's cursor and
// generation, passed back, read the next page and nothing twice.
func TestTheHistoryToolReadsTwoPagesThroughTheRoute(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	_, tok := mcpAssistantToken(t, env)
	for i := range 5 {
		env.put(t, taskKind, mintedID("run"+string(rune('a'+i))), map[string]any{"name": "a task"})
	}
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
	kinds := []string{taskKind}
	first := read(map[string]any{"first": 3, "kinds": kinds})
	if len(first.Changes) != 3 || first.Cursor == 0 || first.Generation == "" {
		t.Fatalf("first page = %+v", first)
	}
	second := read(map[string]any{"first": 3, "kinds": kinds, "before": first.Cursor, "generation": first.Generation})
	if len(second.Changes) != 2 || second.Changes[0].Seq >= first.Changes[2].Seq {
		t.Fatalf("second page = %+v after %+v", second, first)
	}
}
