package api

// Push on the real engine (docs/plans/push.md): the device put through the
// handler with the fixtures of testdata/push, the three routes, and a push
// from a turn through the dispatcher to a relay, whose event ids resolve
// for their own token alone.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/push"
	"github.com/geoah/substrate/internal/pushdispatch"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	pushClientID  = "https://apps.example.com/apps/chat/client.json"
	pushAuthority = "example.com"
	pushDevice    = "/api/v1/substrate.reamde.dev/core/device/"
)

// pushFixture reads one of testdata/push.
func pushFixture(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "push", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// pushDocuments is an authority reader over one development document that
// lists the chat app, naming relay for it when relay is not "".
func pushDocuments(t *testing.T, relay string) *authority.Reader {
	t.Helper()
	app := map[string]any{"client_id": pushClientID, "name": "Chat"}
	if relay != "" {
		app["push"] = map[string]any{"relay": relay}
	}
	raw, err := json.Marshal(map[string]any{"authority": pushAuthority, "apps": []any{app}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return authority.NewReader(authority.Options{LocalDocuments: map[string]string{pushAuthority: path}})
}

// pushToken mints a token and answers its record and its bearer.
func pushToken(t *testing.T, env *dbEnv, in substrate.MintTokenInput) (substrate.TokenInfo, string) {
	t.Helper()
	info, secret, err := env.ds.MintToken(context.Background(), in)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return info, secret
}

// chatToken is the chat app's full-access token, its publisher verified.
func chatToken(t *testing.T, env *dbEnv) (substrate.TokenInfo, string) {
	t.Helper()
	return pushToken(t, env, substrate.MintTokenInput{Label: "Chat", ClientID: pushClientID, ClientName: "Chat", AppAuthority: pushAuthority})
}

// sameKeys reports whether two JSON values have the same keys at every
// level, the shape a fixture pins.
func sameKeys(a, b any) bool {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok != bok {
		return false
	}
	if !aok {
		return true
	}
	if !slices.Equal(slices.Sorted(maps.Keys(am)), slices.Sorted(maps.Keys(bm))) {
		return false
	}
	for k := range am {
		if !sameKeys(am[k], bm[k]) {
			return false
		}
	}
	return true
}

// The device put through the real handler, with the fixtures the app
// copies: a create at the token's own id is 201, an update leaving
// `notifies` out is 200 and keeps it, and each refusal answers its status
// and code. The engine fills `token` and `person`; a person's other token
// deletes it, and so does an admin.
func TestADevicePutThroughTheHandler(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withConfig(func(c *Config) { c.PushDocuments = pushDocuments(t, "https://push.example.com") }))
	var fx struct {
		Path   string `json:"path"`
		Create struct {
			Body   json.RawMessage `json:"body"`
			Status int             `json:"status"`
		} `json:"create"`
		Update struct {
			Body   json.RawMessage `json:"body"`
			Status int             `json:"status"`
		} `json:"update"`
		Refusals []struct {
			Why        string          `json:"why"`
			Fresh      bool            `json:"fresh"`
			OtherToken bool            `json:"otherToken"`
			Body       json.RawMessage `json:"body"`
			Status     int             `json:"status"`
			Code       string          `json:"code"`
		} `json:"refusals"`
	}
	pushFixture(t, "device-put.json", &fx)
	put := func(tok substrate.TokenInfo, bearer string, body json.RawMessage) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, strings.ReplaceAll(fx.Path, "{tokenId}", tok.ID), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		env.h.ServeHTTP(rec, req)
		return rec
	}
	phone, phoneBearer := chatToken(t, env)
	fresh, freshBearer := chatToken(t, env)
	for _, r := range fx.Refusals {
		if !r.Fresh {
			continue
		}
		rec := put(fresh, freshBearer, r.Body)
		if rec.Code != r.Status || decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Code != r.Code {
			t.Fatalf("%s: %d %s, want %d %s", r.Why, rec.Code, rec.Body.String(), r.Status, r.Code)
		}
	}

	rec := put(phone, phoneBearer, fx.Create.Body)
	wantStatus(t, rec, fx.Create.Status)
	created := decodeJSON[map[string]any](t, rec)
	if created["id"] != phone.ID {
		t.Fatalf("the device answered id %v, want the token's %s", created["id"], phone.ID)
	}
	stored := env.get(t, vocabulary.KindDevice, phone.ID)
	if appScopedRefPath(stored.Properties["token"]) != vocabulary.RecordPath(vocabulary.CoreKind("token"), phone.ID) ||
		appScopedRefPath(stored.Properties["person"]) == "" || stored.Properties["seenAt"] == nil {
		t.Fatalf("the stored device: %v", stored.Properties)
	}
	rec = put(phone, phoneBearer, fx.Update.Body)
	wantStatus(t, rec, fx.Update.Status)
	if got := env.get(t, vocabulary.KindDevice, phone.ID).Properties["notifies"]; fmt.Sprint(got) != "[replies requests]" {
		t.Fatalf("an update without notifies left %v", got)
	}
	for _, r := range fx.Refusals {
		if r.Fresh {
			continue
		}
		tok, bearer := phone, phoneBearer
		if r.OtherToken {
			tok, _ = chatToken(t, env)
		}
		rec := put(tok, bearer, r.Body)
		if rec.Code != r.Status || decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Code != r.Code {
			t.Fatalf("%s: %d %s, want %d %s", r.Why, rec.Code, rec.Body.String(), r.Status, r.Code)
		}
	}
	// A create through the records route names its id, which it may not.
	rec = env.do(t, http.MethodPost, "/api/v1/records", phoneBearer, map[string]any{
		"kind": vocabulary.KindDevice, "id": phone.ID,
		"properties": map[string]any{"name": "iPhone", "platform": "ios", "pushKey": "v1.x", "notifies": []any{"replies"}},
	})
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("a create through the records route naming the id: %d %s", rec.Code, rec.Body.String())
	}

	// The app reads its record and deletes it at the same address; another
	// token of the person deletes it too.
	wantStatus(t, env.do(t, http.MethodGet, pushDevice+phone.ID, phoneBearer, nil), http.StatusOK)
	wantStatus(t, env.do(t, http.MethodDelete, pushDevice+phone.ID, phoneBearer, nil), http.StatusOK)
	wantStatus(t, put(phone, phoneBearer, fx.Create.Body), http.StatusOK)
	wantStatus(t, env.do(t, http.MethodDelete, pushDevice+phone.ID, env.token, nil), http.StatusOK)
}

// The push routes answer for the asking token: its public key and its
// app's relay, `no_relay` for a token that is no verified app's, 503 while
// the document cannot be read; and every route answers `push_off`, and
// discovery leaves `push` out, where the operator turned push off.
func TestThePushRoutesAnswerForTheAskingToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t, withConfig(func(c *Config) { c.PushDocuments = pushDocuments(t, "https://push.example.com/") }))
	phone, phoneBearer := chatToken(t, env)
	rec := env.do(t, http.MethodGet, "/api/v1/push/key", phoneBearer, nil)
	wantStatus(t, rec, http.StatusOK)
	key := decodeJSON[substrate.PushKey](t, rec)
	signer, err := env.ds.(substrate.PushStore).PushSigner(ctx, phone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if key.Relay != "https://push.example.com/" || key.PublicKey != pushwire.Encode(signer.PublicKey()) {
		t.Fatalf("the key answered %+v", key)
	}
	noRelay := func(bearer, why string) {
		t.Helper()
		rec := env.do(t, http.MethodGet, "/api/v1/push/key", bearer, nil)
		wantStatus(t, rec, http.StatusForbidden)
		if got := decodeJSON[substrate.ErrorEnvelope](t, rec).Error; got.Code != codeForbidden || got.Reason != substrate.ReasonNoRelay {
			t.Fatalf("%s: %+v, want forbidden no_relay", why, got)
		}
	}
	noRelay(env.token, "a password sign-in's token")
	_, unverified := pushToken(t, env, substrate.MintTokenInput{Label: "Chat", ClientID: pushClientID, ClientName: "Chat"})
	noRelay(unverified, "an unverified app's token")
	_, local := pushToken(t, env, substrate.MintTokenInput{Label: "Chat", ClientID: pushClientID, ClientName: "Chat", AppAuthority: pushAuthority, AppLocal: true})
	noRelay(local, "a local app's token")
	_, unlisted := pushToken(t, env, substrate.MintTokenInput{Label: "Other", ClientID: "https://apps.example.com/apps/other/client.json", ClientName: "Other", AppAuthority: pushAuthority})
	noRelay(unlisted, "an app its publisher's document does not list")
	_, elsewhere := pushToken(t, env, substrate.MintTokenInput{Label: "Chat", ClientID: pushClientID, ClientName: "Chat", AppAuthority: "unreadable.example.com"})
	rec = env.do(t, http.MethodGet, "/api/v1/push/key", elsewhere, nil)
	wantErrorCode(t, rec, http.StatusServiceUnavailable, codeUnavailable)
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}

	// The badge, for a token whose repository holds no chat.
	rec = env.do(t, http.MethodGet, "/api/v1/push/badge", phoneBearer, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.PushBadge](t, rec); got.Badge != 0 {
		t.Fatalf("an empty repository's badge is %d", got.Badge)
	}
	// An event id that does not open, and one sealed for another token.
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/push/events/"+strings.Repeat("A", 64), phoneBearer, nil), http.StatusNotFound, codeNotFound)
	other, _ := chatToken(t, env)
	otherSigner, err := env.ds.(substrate.PushStore).PushSigner(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := otherSigner.SealEvent(substrate.PushEvent{Event: substrate.PushEventRead, Thread: "8f2c41d07a", Alerts: []string{"CetD8NWemKv7dBg-jASmqA"}})
	if err != nil {
		t.Fatal(err)
	}
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/push/events/"+id, phoneBearer, nil), http.StatusNotFound, codeNotFound)

	// Discovery lists push while it is on.
	if !discoveryLists(t, env.testEnv, "push") {
		t.Fatal("discovery does not list push while it is on")
	}

	// Off.
	off := newDBEnv(t, withConfig(func(c *Config) {
		c.PushOff = true
		c.PushDocuments = pushDocuments(t, "https://push.example.com")
	}))
	_, offBearer := chatToken(t, off)
	for _, path := range []string{"/api/v1/push/key", "/api/v1/push/badge", "/api/v1/push/events/" + id} {
		rec := off.do(t, http.MethodGet, path, offBearer, nil)
		wantStatus(t, rec, http.StatusForbidden)
		if got := decodeJSON[substrate.ErrorEnvelope](t, rec).Error; got.Reason != substrate.ReasonPushOff {
			t.Fatalf("%s under push off: %+v", path, got)
		}
	}
	if discoveryLists(t, off.testEnv, "push") {
		t.Fatal("discovery lists push while it is off")
	}
}

// discoveryLists reports whether discovery lists the feature name.
func discoveryLists(t *testing.T, env *testEnv, name string) bool {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	var doc struct {
		Features []featureInfo `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, f := range doc.Features {
		if f.Name == name {
			return true
		}
	}
	return false
}

// pushLLM is an OpenAI-wire chat endpoint that asks a question when the
// person's message says "ask" and replies otherwise, and names a thread
// when asked to.
func pushLLM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream   bool             `json:"stream"`
			Tools    []any            `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]
		said, _ := json.Marshal(last["content"])
		if len(req.Tools) > 0 && last["role"] != "tool" && strings.Contains(string(said), "fail") {
			http.Error(w, `{"error":{"message":"the model failed"}}`, http.StatusBadRequest)
			return
		}
		delta := map[string]any{"content": "Here is the summary you asked for."}
		finish := "stop"
		if len(req.Tools) > 0 && last["role"] != "tool" && strings.Contains(string(said), "ask") {
			args := `{"questions":[{"id":"sure","prompt":"Are you sure?"}]}`
			delta = map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": fmt.Sprintf("call-%d", len(req.Messages)), "type": "function",
				"function": map[string]any{"name": "ask", "arguments": args},
			}}}
			finish = "tool_calls"
		}
		usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		if !req.Stream {
			msg := map[string]any{"role": "assistant"}
			maps.Copy(msg, delta)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cmpl", "object": "chat.completion",
				"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
				"usage":   usage,
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []map[string]any{
			{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta}}},
			{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}},
			{"object": "chat.completion.chunk", "choices": []any{}, "usage": usage},
		} {
			raw, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// pushRelay is a relay that verifies each push's signature against the
// public key the server answered for the device's token and keeps it.
type pushRelay struct {
	mu   sync.Mutex
	pub  ed25519.PublicKey
	got  []pushwire.PushRequest
	errs []string
}

func (p *pushRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.URL.Path != pushwire.PathPush {
		p.errs = append(p.errs, "path "+r.URL.Path)
	}
	if err := pushwire.Verify(r.Header.Get(pushwire.SignatureHeader), p.pub, time.Now(), body); err != nil {
		p.errs = append(p.errs, "signature: "+err.Error())
	}
	req, err := pushwire.DecodePush(body)
	if err != nil {
		p.errs = append(p.errs, "body: "+err.Error())
	}
	p.got = append(p.got, req)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"sent"}`))
}

func (p *pushRelay) take() ([]pushwire.PushRequest, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	got, errs := p.got, p.errs
	p.got, p.errs = nil, nil
	return got, errs
}

// A push travels from a turn to the relay and back: a reply, then two
// cards on one thread, each signed for the device's token, with a badge the
// badge route answers too; each event id resolves for its own token
// through its view now, a card the token may decide with actions and one a
// read-only app's token may not without; and a card decided on another
// device takes its alert back with a `read` naming its fingerprint.
//
// Serial: the model is on loopback, which the engine's dial refuses unless
// the process environment allows it, and t.Setenv is process-wide.
func TestAPushTravelsFromATurnToTheRelayAndResolves(t *testing.T) {
	t.Setenv(egress.AllowVar, "127.0.0.0/8,::1/128")
	ctx := context.Background()
	model := pushLLM(t)
	relay := &pushRelay{}
	relaySrv := httptest.NewTLSServer(relay)
	t.Cleanup(relaySrv.Close)
	docs := pushDocuments(t, relaySrv.URL)
	var clockMu sync.Mutex
	clock := time.Now()
	now := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	advance := func(d time.Duration) time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		clock = clock.Add(d)
		return clock
	}
	dispatcher := pushdispatch.New(pushdispatch.Options{
		View: func(ctx context.Context, whole substrate.Dataset, tok substrate.TokenInfo) (pushdispatch.View, error) {
			return NewTokenView(ctx, whole, tok)
		},
		Documents: docs,
		Sender:    push.New(push.Options{HTTP: egress.CredentialClient(relaySrv.Client().Transport, push.Timeout)}),
		Now:       now,
		Manual:    true,
	})
	env := newDBEnv(t, withEngine(engine.WithPushSink(dispatcher)), withConfig(func(c *Config) { c.PushDocuments = docs }))
	dispatcher.Start(env.svc)
	t.Cleanup(dispatcher.Close)

	provider := vocabulary.LLMKind("provider")
	env.put(t, provider, "pushllm", map[string]any{"wire": "openai", "baseURL": model.URL, "apiKey": "row-key-pushllm"})
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

	phone, phoneBearer := chatToken(t, env)
	rec := env.do(t, http.MethodGet, "/api/v1/push/key", phoneBearer, nil)
	wantStatus(t, rec, http.StatusOK)
	raw, err := pushwire.Decode(decodeJSON[substrate.PushKey](t, rec).PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	relay.pub = raw
	rec = env.do(t, http.MethodPut, pushDevice+phone.ID, phoneBearer, map[string]any{"properties": map[string]any{
		"name": "iPhone", "platform": "ios", "pushKey": "v1.phone", "notifies": []any{"replies", "requests"},
	}})
	wantStatus(t, rec, http.StatusCreated)

	person := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	chat := func(thread, message string) string {
		t.Helper()
		res, err := env.ds.ChatAgent(person, substrate.ActorPerson, crew+"/moss", substrate.ChatInput{Message: message, Thread: thread}, nil)
		if err != nil {
			t.Fatalf("chat: %v", err)
		}
		return res.Thread
	}
	sent := func(want int) []pushwire.PushRequest {
		t.Helper()
		dispatcher.Tick(advance(pushdispatch.Hold + time.Second))
		got, errs := relay.take()
		if len(errs) > 0 {
			t.Fatalf("the relay refused: %v", errs)
		}
		if len(got) != want {
			t.Fatalf("the relay got %d pushes, want %d: %+v", len(got), want, got)
		}
		return got
	}
	resolve := func(bearer, eventID string) (substrate.PushEventInfo, map[string]any) {
		t.Helper()
		rec := env.do(t, http.MethodGet, "/api/v1/push/events/"+eventID, bearer, nil)
		wantStatus(t, rec, http.StatusOK)
		var shape map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &shape); err != nil {
			t.Fatal(err)
		}
		return decodeJSON[substrate.PushEventInfo](t, rec), shape
	}
	fixtureShape := func(name string) (substrate.PushEventInfo, map[string]any) {
		t.Helper()
		var shape map[string]any
		pushFixture(t, name, &shape)
		var info substrate.PushEventInfo
		pushFixture(t, name, &info)
		return info, shape
	}

	// A reply.
	thread := chat("", "summarize the week")
	got := sent(1)
	reply := got[0]
	if reply.Event != substrate.PushEventReply || reply.Thread != thread || reply.PushKey != "v1.phone" || reply.Badge != 1 {
		t.Fatalf("the reply push: %+v", reply)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/push/badge", phoneBearer, nil)
	wantStatus(t, rec, http.StatusOK)
	if b := decodeJSON[substrate.PushBadge](t, rec).Badge; b != reply.Badge {
		t.Fatalf("the badge route answers %d, the push set %d", b, reply.Badge)
	}
	info, shape := resolve(phoneBearer, reply.EventID)
	want, wantShape := fixtureShape("event-reply.json")
	if info.Event != want.Event || info.Thread != thread || info.Item == nil || info.Item.Kind != want.Item.Kind || !sameKeys(shape, wantShape) {
		t.Fatalf("the reply resolved as %s, want the shape of %v", shape, wantShape)
	}

	// Two cards on one thread: two pushes, two event ids.
	chat(thread, "please ask me")
	first := sent(1)[0]
	chat(thread, "ask again")
	second := sent(1)[0]
	if first.Event != substrate.PushEventRequest || second.Event != substrate.PushEventRequest || first.EventID == second.EventID || first.Thread != thread {
		t.Fatalf("the two cards: %+v %+v", first, second)
	}
	card1, shape1 := resolve(phoneBearer, first.EventID)
	want1, wantShape1 := fixtureShape("event-request-card-1.json")
	if card1.Actions == nil || *card1.Actions != *want1.Actions || card1.Item.Kind != want1.Item.Kind || !sameKeys(shape1, wantShape1) {
		t.Fatalf("card 1 resolved as %s, want the shape of %v", shape1, wantShape1)
	}
	card2, _ := resolve(phoneBearer, second.EventID)
	if card2.Item == nil || card2.Item.ID == card1.Item.ID {
		t.Fatalf("the two cards resolve to one item: %+v %+v", card1.Item, card2.Item)
	}
	// A read-only app's token reads the second card and may not decide it.
	reader, readerBearer := pushToken(t, env, substrate.MintTokenInput{
		Label: "Reader", ClientID: "https://apps.example.com/apps/reader/client.json", ClientName: "Reader",
		Scopes: []string{"read:" + vocabulary.KindLLMThread, "read:" + vocabulary.KindLLMInteraction, "read:" + vocabulary.KindLLMMessage},
	})
	readerSigner, err := env.ds.(substrate.PushStore).PushSigner(ctx, reader.ID)
	if err != nil {
		t.Fatal(err)
	}
	readerEvent, err := readerSigner.SealEvent(substrate.PushEvent{Event: substrate.PushEventRequest, Thread: thread, Item: *card2.Item})
	if err != nil {
		t.Fatal(err)
	}
	other, shape2 := resolve(readerBearer, readerEvent)
	want2, wantShape2 := fixtureShape("event-request-card-2.json")
	if other.Actions == nil || *other.Actions != *want2.Actions || !sameKeys(shape2, wantShape2) {
		t.Fatalf("card 2 for a read-only app resolved as %s, want the shape of %v", shape2, wantShape2)
	}
	// The phone's event ids are its own.
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/push/events/"+first.EventID, readerBearer, nil), http.StatusNotFound, codeNotFound)

	// Card 1 decided on another device: a `read` takes its alert back, and
	// card 1 no longer offers actions.
	interaction := env.get(t, card1.Item.Kind, card1.Item.ID)
	rec = env.do(t, http.MethodPatch, "/api/v1/"+card1.Item.Kind+"/"+card1.Item.ID, env.token, map[string]any{
		"properties": map[string]any{"state": "answered", "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		"ifVersion":  interaction.Version,
	})
	wantStatus(t, rec, http.StatusOK)
	dispatcher.Tick(advance(pushdispatch.ReadHold + time.Second))
	reads, errs := relay.take()
	if len(errs) > 0 {
		t.Fatalf("the relay refused: %v", errs)
	}
	var read *pushwire.PushRequest
	for i := range reads {
		if reads[i].Event == substrate.PushEventRead {
			read = &reads[i]
		}
	}
	if read == nil {
		t.Fatalf("no read was sent: %+v", reads)
	}
	covered, shapeRead := resolve(phoneBearer, read.EventID)
	_, wantShapeRead := fixtureShape("event-read.json")
	if covered.Covers == nil || !slices.Equal(covered.Covers.Alerts, []string{pushwire.EventFingerprint(first.EventID)}) || !sameKeys(shapeRead, wantShapeRead) {
		t.Fatalf("the read covers %s, want card 1's fingerprint alone", shapeRead)
	}
	if again, _ := resolve(phoneBearer, first.EventID); again.Actions == nil || *again.Actions {
		t.Fatalf("a decided card still offers actions: %+v", again)
	}

	// The device's token revoked: nothing more is sent to it. The answer
	// resumed the thread, so the next message waits for that turn to settle.
	wantStatus(t, env.do(t, http.MethodDelete, "/api/v1/"+vocabulary.CoreKind("token")+"/"+phone.ID, env.token, nil), http.StatusOK)
	for deadline := time.Now().Add(30 * time.Second); env.get(t, vocabulary.KindLLMThread, thread).Properties["status"] == "running"; {
		if time.Now().After(deadline) {
			t.Fatal("the turn the answer resumed never settled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	chat(thread, "one more")
	sent(0)
}
