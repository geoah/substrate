package api

// The routes a person who is not an admin reaches, held to their roles on
// the real engine (decision 0276): a bearer webhook acts for its token's
// person, a stream ends when that person's roles change, a person lists the
// kinds and searches the history of what they read, and a write their roles
// refuse says so.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A bearer webhook acts for its token's person: while they write the
// callable's kind the delivery lands, and once they are a viewer it is
// refused by their role and parks. The key door acts for nobody, so a keyed
// request still lands.
func TestABearerWebhookIsHeldToItsPersonsRoles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newAppDeliveryEnv(t, false)
	editor := addPerson(t, e.dbEnv, "e", scope.RoleEditor)
	_, app := appDeliveryMint(t, e.ds, substrate.MintTokenInput{
		Identity: editor.identity, Scopes: []string{"webhook:" + appHook}, ClientID: appClientID, ClientName: "Notes",
	})
	deliver := func(body string) *httptest.ResponseRecorder {
		return postHook(e.h, "/webhooks/"+e.repository+"/"+appHook, []byte(body), map[string]string{
			"Content-Type": "application/json", "Authorization": "Bearer " + app,
		})
	}
	wantStatus(t, deliver(`{"as":"editor"}`), http.StatusAccepted)
	e.waitEchoes(t, 1)

	if err := e.svc.(engine.Operator).SetPersonRoles(ctx, editor.name, e.repository, scope.RoleViewer); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, deliver(`{"as":"viewer"}`), http.StatusAccepted)
	deadline := time.Now().Add(30 * time.Second)
	for {
		rows := e.pending(t, appHook)
		if len(rows) == 1 && !rows[0].Pending && !rows[0].Running {
			if !strings.Contains(rows[0].LastError, "may not write") {
				t.Fatalf("the viewer's delivery parked with %q, want refused by their role", rows[0].LastError)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the viewer's delivery never parked: %+v", rows)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, echo := range e.echoes(t) {
		if echo.Properties["payload"] == `{"as":"viewer"}` {
			t.Fatal("a viewer's bearer delivery wrote the callable's record")
		}
	}

	wantStatus(t, postHook(e.h, "/webhooks/"+e.repository+"/"+appHook+"/"+appHookKey, []byte(`{"as":"key"}`), nil), http.StatusAccepted)
	e.waitEchoes(t, 2)
}

// watchEnds reads br until the stream ends, and fails on a line before it:
// a stream that ends at a role change sends no terminal frame.
func watchEnds(t *testing.T, br *bufio.Reader) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		line, err := br.ReadBytes('\n')
		if err == nil {
			err = fmt.Errorf("a line before the end: %s", line)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if strings.HasPrefix(err.Error(), "a line before the end") {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watch ran on after its person's roles changed")
	}
}

// A watch reads through the view its person's roles built when it opened:
// a change of those roles ends it with no terminal frame, as a drain does,
// and the client reconnects from its cursor onto a view of the roles held
// now, which carries less: an admin's carries a token's record, a viewer's
// none.
func TestAWatchEndsAtARoleChangeAndReopensNarrower(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	note := noteKind(t, env)
	bo := addPerson(t, env, "b", scope.RoleAdmin)
	srv := httptest.NewServer(env.h)
	defer srv.Close()

	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1", bo.token)
	defer stop()
	opened := readLine(t, br)
	if opened["bookmark"] == nil {
		t.Fatalf("the first line = %v, want the bookmark", opened)
	}
	generation, _ := opened["generation"].(string)
	env.mint(t, substrate.MintTokenInput{Label: "as an admin"})
	row := readLine(t, br)
	if row["kind"] != tokenType {
		t.Fatalf("the admin's watch carried %v, want the token's record", row)
	}
	cursor := int64(row["seq"].(float64))

	if err := env.svc.(engine.Operator).SetPersonRoles(ctx, bo.name, env.repository, scope.RoleViewer); err != nil {
		t.Fatal(err)
	}
	watchEnds(t, br)

	again, stopAgain := startWatch(t, srv, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", cursor, url.QueryEscape(generation)), bo.token)
	defer stopAgain()
	if line := readLine(t, again); line["bookmark"] == nil {
		t.Fatalf("the reconnect's first line = %v, want the bookmark", line)
	}
	env.mint(t, substrate.MintTokenInput{Label: "after"})
	env.put(t, note, "n1", map[string]any{"name": "after"})
	for {
		line := readLine(t, again)
		if line["kind"] == tokenType || line["kind"] == vocabulary.CoreKind("actor") {
			t.Fatalf("the viewer's watch carried a control kind's row: %v", line)
		}
		if line["recordId"] == "n1" {
			break
		}
	}
}

// An export is the whole repository, an admin's: one whose admin is made a
// viewer while it runs is cut before its last entry, as one whose token is
// revoked is.
func TestAnExportIsCutAtItsPersonsDemotionAndAtTheirTokensRevocation(t *testing.T) {
	t.Parallel()
	for name, cut := range map[string]func(t *testing.T, env *dbEnv, p rolePerson){
		"demoted": func(t *testing.T, env *dbEnv, p rolePerson) {
			if err := env.svc.(engine.Operator).SetPersonRoles(context.Background(), p.name, env.repository, scope.RoleViewer); err != nil {
				t.Fatal(err)
			}
		},
		"revoked": func(t *testing.T, env *dbEnv, p rolePerson) {
			wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+p.tokenID, p.token, nil), http.StatusOK)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newDBEnv(t)
			bo := addPerson(t, env, "b", scope.RoleAdmin)
			w := &revokingWriter{ResponseRecorder: httptest.NewRecorder(), revoke: func() {
				cut(t, env, bo)
				env.clock.advance(2 * leaseTransferEvery)
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
			req.Header.Set("Authorization", "Bearer "+bo.token)
			if !serveAborting(env, w, req) {
				t.Fatalf("the export ran to its end after its person was %s (%d bytes)", name, w.Body.Len())
			}
			if !w.revoked {
				t.Fatal("the export wrote no body before it was cut")
			}
			if archiveHoldsManifest(w.Body.Bytes()) {
				t.Fatal("the cut archive carries its last entry")
			}
		})
	}
}

// A viewer lists the kinds through the requests the console and
// `substratectl kinds` send: the declarations of the kinds their roles read,
// and no control kind's. A walk in small pages never meets an empty page
// before its end, where the CLI would stop.
func TestAViewerListsTheKindsTheyRead(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	filter := `{"kinds":["` + kindDeclarationRow + `"]}`
	walk := func(first int) (ids []string) {
		t.Helper()
		after := ""
		for range 1000 {
			q := url.Values{"first": {fmt.Sprint(first)}, "filter": {filter}}
			if after != "" {
				q.Set("after", after)
			}
			rec := env.do(t, http.MethodGet, recordsPath+"?"+q.Encode(), viewer.token, nil)
			wantStatus(t, rec, http.StatusOK)
			page := decodeJSON[substrate.Page](t, rec)
			for _, r := range page.Records {
				ids = append(ids, r.ID)
			}
			if page.Cursor == "" {
				return ids
			}
			if len(page.Records) == 0 {
				t.Fatalf("a page of %d came back empty before the end of the walk", first)
			}
			after = page.Cursor
		}
		t.Fatal("the walk never ended")
		return nil
	}
	for what, first := range map[string]int{"the console's and substratectl kinds'": 500, "a small page's": 3} {
		ids := walk(first)
		if !slices.Contains(ids, note) {
			t.Errorf("%s list of the kinds holds no %s: %v", what, note, ids)
		}
		for _, control := range []string{kindTrigger, vocabulary.CoreKind("role"), vocabulary.CoreKind("token")} {
			if slices.Contains(ids, control) {
				t.Errorf("%s list of the kinds holds the control kind %s", what, control)
			}
		}
	}
	rec := env.do(t, http.MethodGet, recordsPath+"?count=1&filter="+url.QueryEscape(filter), viewer.token, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
}

// A person searches the history of the kinds their roles read whole, and
// the search finds nothing of a kind they do not: a token's label.
func TestAPersonSearchesTheHistoryOfWhatTheyReadWhole(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	env.put(t, note, "n1", map[string]any{"name": "Zwicky's note"})
	env.mint(t, substrate.MintTokenInput{Label: "Zwicky's laptop"})
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	for who, token := range map[string]string{"the admin": env.token, "a viewer": viewer.token} {
		rec := env.do(t, http.MethodGet, "/api/v1/changes?first=50&q=zwicky", token, nil)
		wantStatus(t, rec, http.StatusOK)
		var found []string
		for _, c := range decodeJSON[struct {
			Changes []substrate.Change `json:"changes"`
		}](t, rec).Changes {
			found = append(found, c.Kind)
		}
		wantToken := who == "the admin"
		if !slices.Contains(found, note) || slices.Contains(found, tokenType) != wantToken {
			t.Fatalf("%s's history search found rows of %v, want the note's, and the token's %v", who, found, wantToken)
		}
	}
}

// A write a person's roles refuse says so: `not_permitted`. An app's write
// its scope refuses stays a scope refusal, without it.
func TestAViewersRefusedWriteSaysNotPermitted(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	rec := env.do(t, http.MethodPost, recordsPath, viewer.token, map[string]any{"kind": note, "properties": map[string]any{"name": "two"}})
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	var refused substrate.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &refused); err != nil || refused.Error.Reason != substrate.ReasonNotPermitted {
		t.Fatalf("a viewer's refused write answered %s, want the reason not_permitted", rec.Body.String())
	}
	app := appScopedToken(t, env, "read:"+note)
	rec = env.do(t, http.MethodPost, recordsPath, app, map[string]any{"kind": note, "properties": map[string]any{"name": "three"}})
	wantStatus(t, rec, http.StatusForbidden)
	var scoped substrate.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &scoped); err != nil || scoped.Error.Reason == substrate.ReasonNotPermitted {
		t.Fatalf("an app's write its scope refuses answered %s, want no not_permitted", rec.Body.String())
	}
}

// Whose rules hold a run's work is the engine's bookkeeping, and it names
// tokens: the view a person who is not an admin reads through cuts it from
// every record it returns and from its history, and an admin reads it
// whole.
func TestThePersonsViewCutsWhoseRulesHoldARun(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	chat := env.repository + "/chat"
	env.put(t, llmPackage+"/provider", appAgentsLLMProvider, map[string]any{"wire": "openai", "baseURL": "https://llm.example.com", "apiKey": "row-key"})
	appAgentsApply(t, env,
		vocabulary.PackageManifest(chat, 1),
		agentReachAgent(chat, "wide", map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{"*"}}}}),
	)
	env.put(t, kindThread, "t1", map[string]any{
		"agent": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindAgent, chat+"/wide")}, "subject": "first", "mode": "chat", "status": "ok",
		"provider": appAgentsLLMProvider, "model": "wide", "agentDepth": 0, "startedAt": "2026-01-01T00:00:00Z",
	})
	read := func(token string) (record map[string]any, history string) {
		t.Helper()
		rec := env.do(t, http.MethodGet, recordPath(kindThread, "t1"), token, nil)
		wantStatus(t, rec, http.StatusOK)
		record = decodeJSON[map[string]any](t, rec)
		rec = env.do(t, http.MethodGet, "/api/v1/changes?first=10&values=1&recordKind="+url.QueryEscape(kindThread)+"&recordId=t1", token, nil)
		wantStatus(t, rec, http.StatusOK)
		return record, rec.Body.String()
	}
	record, history := read(env.token)
	if props, _ := record["properties"].(map[string]any); props["heldTo"] == nil || !strings.Contains(history, `"heldTo"`) {
		t.Fatalf("the admin's read of the thread carries no heldTo: %v %s", record, history)
	}
	record, history = read(editor.token)
	if props, _ := record["properties"].(map[string]any); props == nil || props["heldTo"] != nil {
		t.Fatalf("the editor's read of the thread: %v, want it without heldTo", record)
	}
	if strings.Contains(history, `"heldTo":{`) {
		t.Fatalf("the editor's history of the thread carries heldTo's value: %s", history)
	}
}

// An agent stream whose person's roles change ends `unavailable`, which a
// client opens again, never `auth`, which would sign it out.
func TestAnAgentStreamEndsAtARoleChangeWithoutSigningOut(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	started := make(chan struct{})
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return holdingChat{Dataset: ds, started: started} }
	bo := addPerson(t, env, "b", scope.RoleAdmin)
	req := httptest.NewRequest(http.MethodPost, helperChatPath, strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Authorization", "Bearer "+bo.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		env.h.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat turn never started")
	}
	if err := env.svc.(engine.Operator).SetPersonRoles(context.Background(), bo.name, env.repository, scope.RoleEditor); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent stream ran on after its person's roles changed")
	}
	// Before its first event went out the answer is an ordinary refusal,
	// after it an error event: either says unavailable.
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"unavailable"`) || strings.Contains(body, `"code":"auth"`) {
		t.Fatalf("the stream ended with %d %s, want the code unavailable", rec.Code, body)
	}
}

// A person pins and releases the values of a kind their roles write, as
// themselves, and made a viewer is refused by their role.
func TestAPersonPinsWhatTheirRolesWrite(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	env.put(t, note, "one", map[string]any{"name": "one"})
	bo := addPerson(t, env, "b", scope.RoleEditor)
	body := map[string]any{"properties": []any{"name"}}
	for _, verb := range []string{"pin", "release"} {
		wantStatus(t, env.do(t, http.MethodPost, recordPath(note, "one")+"/"+verb, bo.token, body), http.StatusOK)
		// The write is the person's: its line names their token.
		rec := env.do(t, http.MethodGet, "/api/v1/changes?before=0&first=1&recordKind="+url.QueryEscape(note)+"&recordId=one", env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if page := decodeJSON[substrate.ChangePage](t, rec); len(page.Changes) != 1 || page.Changes[0].Token != bo.tokenID {
			t.Fatalf("the %s's line: %+v, want it to name the person's token %s", verb, page.Changes, bo.tokenID)
		}
	}
	if err := env.svc.(engine.Operator).SetPersonRoles(context.Background(), bo.name, env.repository, scope.RoleViewer); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"pin", "release"} {
		rec := env.do(t, http.MethodPost, recordPath(note, "one")+"/"+verb, bo.token, body)
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
		if p := decodeJSON[substrate.ErrorEnvelope](t, rec).Error; p.Reason != substrate.ReasonNotPermitted {
			t.Fatalf("a viewer's %s answered %+v, want not_permitted", verb, p)
		}
	}
}

// The person's view passes no forged `heldTo` on: an editor's patch over
// HTTP that sets, changes or drops it on a thread is refused, and the
// thread's holders are what the engine wrote.
func TestAPersonsViewWritesNoHeldTo(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	chat := env.repository + "/chat"
	env.put(t, llmPackage+"/provider", appAgentsLLMProvider, map[string]any{"wire": "openai", "baseURL": "https://llm.example.com", "apiKey": "row-key"})
	appAgentsApply(t, env,
		vocabulary.PackageManifest(chat, 1),
		agentReachAgent(chat, "wide", map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{"*"}}}}),
	)
	env.put(t, kindThread, "t1", map[string]any{
		"agent": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindAgent, chat+"/wide")}, "subject": "first", "mode": "chat", "status": "ok",
		"provider": appAgentsLLMProvider, "model": "wide", "agentDepth": 0, "startedAt": "2026-01-01T00:00:00Z",
	})
	before := env.get(t, kindThread, "t1").Properties["heldTo"]
	if before == nil {
		t.Fatal("the thread holds no heldTo: the test proves nothing")
	}
	forged := map[string]any{"onBehalfOf": map[string]any{vocabulary.ReferenceValueKey: "substrate.reamde.dev/core/actor/forged"}}
	for what, v := range map[string]any{"change": forged, "drop": nil} {
		rec := env.do(t, http.MethodPatch, recordPath(kindThread, "t1"), editor.token, map[string]any{"properties": map[string]any{"heldTo": v}})
		if rec.Code/100 == 2 {
			t.Errorf("an editor's patch to %s a thread's heldTo answered %d", what, rec.Code)
		}
		if after := env.get(t, kindThread, "t1").Properties["heldTo"]; fmt.Sprint(after) != fmt.Sprint(before) {
			t.Fatalf("an editor's patch to %s it moved the thread's heldTo: %v, then %v", what, before, after)
		}
	}
}
