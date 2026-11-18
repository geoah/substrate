package api

// Who push reaches, on the real engine: an app approved to write its own
// device registers it, and nothing else; a person reads their own devices
// alone; a card's actions are offered only to who may decide it now, at
// its version; a failure resolves while its thread is still in error.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// pushDeviceBody is the device put the app sends.
func pushDeviceBody(key string) map[string]any {
	return map[string]any{"properties": map[string]any{
		"name": "iPhone", "platform": "ios", "pushKey": key, "notifies": []any{"replies", "requests"},
	}}
}

// An app approved `write:substrate.reamde.dev/core/device` registers its
// own install through HTTP, and is refused another token's id; an app not
// approved for it registers nothing.
func TestAScopedAppRegistersItsOwnDeviceThroughItsGrant(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withAppFlow())
	grant := func(scopes string) (token, id string) {
		t.Helper()
		park := appScopedPark(t, env, appRequest(scopes))
		wantStatus(t, appScopedShow(t, env, park), http.StatusOK)
		rec := appScopedApprove(t, env, park, []string{scopes}, 30)
		wantStatus(t, rec, http.StatusOK)
		tok := appScopedExchange(t, env, tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
		wantStatus(t, tok, http.StatusOK)
		res := decodeJSON[tokenResponse](t, tok)
		if res.Scope != scopes || res.TokenID == "" {
			t.Fatalf("the token response: %+v", res)
		}
		return res.AccessToken, res.TokenID
	}
	bearer, id := grant("write:" + vocabulary.KindDevice)
	wantStatus(t, env.do(t, http.MethodPut, pushDevice+id, bearer, pushDeviceBody("v1.app")), http.StatusCreated)
	if got := env.get(t, vocabulary.KindDevice, id); got == nil || got.Properties["pushKey"] != "v1.app" {
		t.Fatalf("the app's device: %+v", got)
	}
	other, _ := pushToken(t, env, substrate.MintTokenInput{Label: "other"})
	wantErrorCode(t, env.do(t, http.MethodPut, pushDevice+other.ID, bearer, pushDeviceBody("v1.app")), http.StatusForbidden, codeForbidden)
	notes, notesID := grant("read:~/notes/*")
	rec := env.do(t, http.MethodPut, pushDevice+notesID, notes, pushDeviceBody("v1.notes"))
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("an app without the device scope registered: %d %s", rec.Code, rec.Body.String())
	}
	if env.get(t, vocabulary.KindDevice, notesID) != nil {
		t.Fatal("an app without the device scope wrote a device")
	}
}

// A person who is not an admin reads their own devices alone, by id or in
// a list, whatever their roles read; an admin reads them all.
func TestAPersonReadsTheirOwnDevicesAlone(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	wantStatus(t, env.do(t, http.MethodPut, pushDevice+viewer.tokenID, viewer.token, pushDeviceBody("v1.viewer")), http.StatusCreated)
	admin, adminBearer := pushToken(t, env, substrate.MintTokenInput{Label: "admin's phone"})
	wantStatus(t, env.do(t, http.MethodPut, pushDevice+admin.ID, adminBearer, pushDeviceBody("v1.admin")), http.StatusCreated)

	wantStatus(t, env.do(t, http.MethodGet, pushDevice+viewer.tokenID, viewer.token, nil), http.StatusOK)
	wantErrorCode(t, env.do(t, http.MethodGet, pushDevice+admin.ID, viewer.token, nil), http.StatusNotFound, codeNotFound)
	wantStatus(t, env.do(t, http.MethodGet, pushDevice+viewer.tokenID, adminBearer, nil), http.StatusOK)

	list := func(bearer, filter string) []string {
		t.Helper()
		rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+filter, bearer, nil)
		wantStatus(t, rec, http.StatusOK)
		var ids []string
		for _, r := range decodeJSON[substrate.Page](t, rec).Records {
			ids = append(ids, r.ID)
		}
		return ids
	}
	kinds := `{"kinds":["` + vocabulary.KindDevice + `"]}`
	if got := list(viewer.token, kinds); len(got) != 1 || got[0] != viewer.tokenID {
		t.Fatalf("the viewer lists %v, want their own device alone", got)
	}
	other := `{"kinds":["` + vocabulary.KindDevice + `"],"properties":{"person":{"eq":"` + vocabulary.RecordPath(vocabulary.CoreKind("actor"), "someone") + `"}}}`
	if got := list(viewer.token, other); len(got) != 0 {
		t.Fatalf("the viewer lists %v naming another person", got)
	}
	if got := list(adminBearer, kinds); len(got) != 2 {
		t.Fatalf("the admin lists %v, want both devices", got)
	}
	// A list naming no kind, and a ranked search naming none, hold no other
	// person's device: the kind is left out before anything is paged. The
	// admin's list naming no kind holds it, so the viewer's missing it is
	// the rule.
	unnamed := func(bearer string) map[string]bool {
		t.Helper()
		rec := env.do(t, http.MethodGet, "/api/v1/records?first=1000&filter={}", bearer, nil)
		wantStatus(t, rec, http.StatusOK)
		out := map[string]bool{}
		for _, r := range decodeJSON[substrate.Page](t, rec).Records {
			if r.Kind == vocabulary.KindDevice {
				out[r.ID] = true
			}
		}
		return out
	}
	if got := unnamed(adminBearer); !got[admin.ID] {
		t.Fatalf("the admin's list naming no kind holds devices %v: the viewer's case proves nothing", got)
	}
	if got := unnamed(viewer.token); len(got) != 0 {
		t.Fatalf("a viewer's list naming no kind holds devices %v", got)
	}
	rec := env.do(t, http.MethodGet, "/api/v1/records?q=iPhone&mode=lexical", viewer.token, nil)
	wantStatus(t, rec, http.StatusOK)
	for _, h := range decodeJSON[substrate.SearchResult](t, rec).Hits {
		if h.Record != nil && h.Record.Kind == vocabulary.KindDevice {
			t.Fatalf("a search naming no kind found a device: %+v", h.Record)
		}
	}
	if rec := env.do(t, http.MethodGet, "/api/v1/records?q=iPhone&mode=lexical&filter="+kinds, viewer.token, nil); rec.Code == http.StatusOK {
		t.Fatalf("a viewer's search naming the device kind answered %s", rec.Body.String())
	}
	// Which records devices point at says who has one: refused.
	byDevice := `{"kinds":["` + vocabulary.CoreKind("actor") + `"],"referencedBy":{"kind":"` + vocabulary.KindDevice + `","property":"person"}}`
	if rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+byDevice, viewer.token, nil); rec.Code == http.StatusOK {
		t.Fatalf("a list narrowed by devices answered %s", rec.Body.String())
	}
	byToken := `{"kinds":["` + vocabulary.KindDevice + `"],"referencing":{"ref":"` + vocabulary.RecordPath(vocabulary.KindDevice, admin.ID) + `"}}`
	if rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+byToken, viewer.token, nil); rec.Code == http.StatusOK {
		t.Fatalf("a list referencing another person's device answered %s", rec.Body.String())
	}
}

// A card's actions are offered to who may decide it now: an editor in
// their own turn, never a viewer, not once the editor is demoted, and not
// for a version the card has moved past.
//
// Serial: the model is on loopback (TestAPushTravelsFromATurnToTheRelayAndResolves).
func TestActionsAreOfferedOnlyToWhoMayDecideTheCardNow(t *testing.T) {
	t.Setenv(egress.AllowVar, "127.0.0.0/8,::1/128")
	ctx := context.Background()
	model := pushLLM(t)
	env := newDBEnv(t)
	crew := pushCrew(t, env, model.URL)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	viewer := addPushPerson(t, env, "v", scope.RoleViewer)
	res, err := env.ds.ChatAgent(substrate.WithIdentity(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson), editor.identity),
		substrate.ActorPerson, crew+"/moss", substrate.ChatInput{Message: "please ask me"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := env.ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 5})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the card: %v %v", page, err)
	}
	card := page.Records[0]
	item := substrate.PushItem{Kind: card.Kind, ID: card.ID, Version: card.Version}
	actions := func(tokenID, bearer string, item substrate.PushItem) bool {
		t.Helper()
		signer, err := env.ds.(substrate.PushStore).PushSigner(ctx, tokenID)
		if err != nil {
			t.Fatal(err)
		}
		id, err := signer.SealEvent(substrate.PushEvent{Event: substrate.PushEventRequest, Thread: res.Thread, Item: item})
		if err != nil {
			t.Fatal(err)
		}
		rec := env.do(t, http.MethodGet, "/api/v1/push/events/"+id, bearer, nil)
		wantStatus(t, rec, http.StatusOK)
		got := decodeJSON[substrate.PushEventInfo](t, rec)
		if got.Actions == nil {
			t.Fatalf("a request answered no actions: %+v", got)
		}
		return *got.Actions
	}
	if !actions(editor.tokenID, editor.token, item) {
		t.Fatal("the editor may not decide the card their own turn raised")
	}
	if actions(viewer.tokenID, viewer.token, item) {
		t.Fatal("a viewer is offered a decision their roles do not write")
	}
	if err := env.svc.(engine.Operator).SetPersonRoles(ctx, editor.name, env.repository, scope.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if actions(editor.tokenID, editor.token, item) {
		t.Fatal("a demoted person is still offered the decision")
	}
	// A write moves the card on: what a notification of the old version
	// offers, the decision's version precondition would refuse.
	admin, adminBearer := pushToken(t, env, substrate.MintTokenInput{Label: "admin's phone"})
	if !actions(admin.ID, adminBearer, item) {
		t.Fatal("the admin may not decide the card")
	}
	if _, err := env.ds.Patch(ctx, substrate.ActorPerson, card.Kind, card.ID, substrate.PatchInput{Labels: map[string]any{"example.com/seen": "yes"}}); err != nil {
		t.Fatal(err)
	}
	moved := env.get(t, card.Kind, card.ID)
	if moved.Version == item.Version {
		t.Fatal("the label left the card's version where it was")
	}
	if actions(admin.ID, adminBearer, item) {
		t.Fatal("an older version of the card is still offered")
	}
	if !actions(admin.ID, adminBearer, substrate.PushItem{Kind: card.Kind, ID: card.ID, Version: moved.Version}) {
		t.Fatal("the card's current version is not offered")
	}
}

// A failure resolves, in the shape of testdata/push/event-failed.json,
// while its thread is still settled in error, and not once a later turn
// settled it ok.
//
// Serial: the model is on loopback.
func TestAFailureResolvesWhileItsThreadIsInError(t *testing.T) {
	t.Setenv(egress.AllowVar, "127.0.0.0/8,::1/128")
	ctx := context.Background()
	model := pushLLM(t)
	env := newDBEnv(t)
	crew := pushCrew(t, env, model.URL)
	phone, bearer := pushToken(t, env, substrate.MintTokenInput{Label: "phone"})
	person := substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
	ok, err := env.ds.ChatAgent(person, substrate.ActorPerson, crew+"/moss", substrate.ChatInput{Message: "hello"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.ds.ChatAgent(person, substrate.ActorPerson, crew+"/moss", substrate.ChatInput{Message: "please fail", Thread: ok.Thread}, nil); err == nil {
		t.Fatal("a failing model call answered no error")
	}
	thread := env.get(t, vocabulary.KindLLMThread, ok.Thread)
	if thread.Properties["status"] != "error" {
		t.Fatalf("the thread settled %v", thread.Properties["status"])
	}
	signer, err := env.ds.(substrate.PushStore).PushSigner(ctx, phone.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := signer.SealEvent(substrate.PushEvent{
		Event: substrate.PushEventFailed, Thread: ok.Thread,
		Item: substrate.PushItem{Kind: vocabulary.KindLLMThread, ID: ok.Thread, Version: thread.Version},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := env.do(t, http.MethodGet, "/api/v1/push/events/"+id, bearer, nil)
	wantStatus(t, rec, http.StatusOK)
	var shape, want map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	pushFixture(t, "event-failed.json", &want)
	if !sameKeys(shape, want) || shape["event"] != "failed" || shape["item"].(map[string]any)["kind"] != vocabulary.KindLLMThread {
		t.Fatalf("the failure resolved as %v, want the shape of %v", shape, want)
	}
	// A later write that leaves it in error moves its version and not the
	// failure: it still resolves.
	if _, err := env.ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMThread, ok.Thread, substrate.PatchInput{Properties: map[string]any{"subject": "renamed"}}); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, env.do(t, http.MethodGet, "/api/v1/push/events/"+id, bearer, nil), http.StatusOK)
	if _, err := env.ds.ChatAgent(person, substrate.ActorPerson, crew+"/moss", substrate.ChatInput{Message: "fine now", Thread: ok.Thread}, nil); err != nil {
		t.Fatal(err)
	}
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/push/events/"+id, bearer, nil), http.StatusNotFound, codeNotFound)
}

// pushCrew declares the agent the push tests chat with, on the model at
// base, and answers its package.
func pushCrew(t *testing.T, env *dbEnv, base string) string {
	t.Helper()
	env.put(t, vocabulary.LLMKind("provider"), "pushllm", map[string]any{"wire": "openai", "baseURL": base, "apiKey": "row-key-pushllm"})
	crew := env.repository + "/crew"
	env.declare(t, fmt.Sprintf(`
kind: substrate.reamde.dev/core/package
metadata:
  id: %[1]s
data:
  authority: %[2]s
  package: crew
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: %[1]s/moss
data:
  authority: %[2]s
  package: crew
  description: moss under test
  prompt: You are moss.
  provider: pushllm
  model: chat
  tools:
    - function: %[3]s
  permissions:
    writes:
      - %[4]s
`, crew, env.repository, vocabulary.HostFunctionAsk, vocabulary.KindLLMInteraction))
	return crew
}

// addPushPerson is addPerson for a second person of one env: the admin
// addPerson adds first is there already.
func addPushPerson(t *testing.T, env *dbEnv, label, role string) rolePerson {
	t.Helper()
	ctx := context.Background()
	name := testdb.RepositoryLabel(t) + label + ".example.com"
	if _, err := env.svc.Register(ctx, substrate.RegisterInput{Identity: name, Repository: name, Password: rolesPassword, SkipTOTP: true}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	if err := env.svc.(engine.Operator).AddPerson(ctx, name, env.repository, role); err != nil {
		t.Fatalf("add %s as %s: %v", name, role, err)
	}
	minted, err := env.svc.Login(ctx, substrate.LoginInput{Identity: name, Repository: env.repository, Password: rolesPassword, Label: "session"})
	if err != nil {
		t.Fatalf("sign %s in: %v", name, err)
	}
	return rolePerson{name: name, identity: minted.Token.Identity, token: minted.Secret, tokenID: minted.Token.ID}
}
