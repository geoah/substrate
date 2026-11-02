package testenv_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

// coachModel answers the OpenAI wire for the coach: a turn whose history
// holds no tool result asks to store one memory, the next replies; and it
// keeps every system prompt it was sent.
type coachModel struct {
	mu      sync.Mutex
	srv     *httptest.Server
	memory  string
	systems []string
}

func newCoachModel(t *testing.T, memoryKind string) *coachModel {
	m := &coachModel{memory: memoryKind}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *coachModel) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	sawTool := false
	for _, msg := range req.Messages {
		if msg["role"] == "system" {
			m.systems = append(m.systems, fmt.Sprint(msg["content"]))
		}
		if msg["role"] == "tool" {
			sawTool = true
		}
	}
	m.mu.Unlock()
	message := map[string]any{"role": "assistant", "content": "Noted: three days a week."}
	finish := "stop"
	if !sawTool && strings.Contains(fmt.Sprint(req.Messages[len(req.Messages)-1]["content"]), "remember") {
		args, _ := json.Marshal(map[string]any{
			"op": "put", "kind": m.memory, "id": "schedule-1",
			"input": map[string]any{"properties": map[string]any{"text": "Trains three days a week, 60 minutes.", "topic": "schedule", "source": "coach"}},
		})
		message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
			"id": "call-1", "type": "function", "function": map[string]any{"name": "write", "arguments": string(args)},
		}}}
		finish = "tool_calls"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "c", "object": "chat.completion", "model": "coach",
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	})
}

func (m *coachModel) lastSystem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.systems) == 0 {
		return ""
	}
	return m.systems[len(m.systems)-1]
}

// A COACH FROM APPROVAL TO REVOCATION, END TO END (decision 0206): an app's
// authorization request carries an `agent` detail; the owner approves it on
// the real approval endpoint; the app's token declares training and the
// coach under its approval, and reads the coach's row; the owner chats with
// the coach, which stores a memory the next chat pins; the owner revokes the
// app, and the coach refuses.
func TestAnAppsCoachFromApprovalToRevocation(t *testing.T) {
	// The stub model listens on loopback, which the server's dial gate
	// blocks unless the operator allows it.
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8")
	e := testenv.Start(t)
	home := e.Repository
	training, coachPkg := home+"/training", home+"/trainingcoach"
	memory, routine := training+"/memory", training+"/routine"
	agent := coachPkg + "/workoutCoach"
	model := newCoachModel(t, memory)

	// The owner's AI provider, priced for the coach's model.
	if status, body := e.Do(http.MethodPut, "/api/v1/substrate.reamde.dev/llm/provider/coachllm", map[string]any{"properties": map[string]any{
		"wire": "openai", "baseURL": model.srv.URL, "apiKey": "sk-test-secret-key",
		"pricing": []any{map[string]any{"model": "coach", "inputPer1M": "1", "outputPer1M": "5"}},
	}}); status/100 != 2 {
		t.Fatalf("put the provider: %d %s", status, body)
	}

	// The app asks, with a loopback client id nothing fetches.
	const clientID, redirect, verifier = "http://127.0.0.1/substrate-workouts", "http://127.0.0.1:43123/callback", "coach-verifier-0123456789-abcdefghijklmnopqrstuvwxyz"
	sum := sha256.Sum256([]byte(verifier))
	details, _ := json.Marshal([]map[string]any{{
		"type": "agent", "agent": "~/trainingcoach/workoutCoach",
		"reads": []string{"~/training/routine", "~/training/memory"}, "writes": []string{"~/training/routine", "~/training/memory"},
		"spendCentsPerDay": 100,
	}})
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"scope":                 {"declare:~/training declare:~/trainingcoach write:~/training/* read:substrate.reamde.dev/llm/provider"},
		"authorization_details": {string(details)},
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(e.URL + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	requestID, ok := strings.CutPrefix(resp.Header.Get("Location"), "/authorize/")
	if resp.StatusCode != http.StatusSeeOther || !ok {
		t.Fatalf("authorize: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	console := map[string]string{"Content-Type": "application/json", "X-Substrate-Actor": "console"}
	status, raw, _ := e.DoRaw(http.MethodGet, "/api/v1/authorizations/"+requestID, nil, console)
	var view substrate.AuthorizationRequest
	if status != http.StatusOK || json.Unmarshal(raw, &view) != nil || len(view.Setup) != 1 || view.Setup[0].Agent == nil ||
		view.Setup[0].Agent.Agent != agent || view.Setup[0].Agent.SpendCentsPerDay != 100 {
		t.Fatalf("the approval page: %d %s", status, raw)
	}
	approval, _ := json.Marshal(map[string]any{
		"scopes": []string{"declare:" + training, "declare:" + coachPkg, "write:" + training + "/*", "read:substrate.reamde.dev/llm/provider"}, "lifetimeDays": 30,
	})
	status, raw, _ = e.DoRaw(http.MethodPost, "/api/v1/authorizations/"+requestID, approval, console)
	var answer substrate.AuthorizationAnswer
	if status != http.StatusOK || json.Unmarshal(raw, &answer) != nil {
		t.Fatalf("approve: %d %s", status, raw)
	}
	back, _ := url.Parse(answer.Redirect)
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {redirect}, "code_verifier": {verifier},
	}
	resp, err = http.PostForm(e.URL+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	tokRaw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var tok struct {
		AccessToken string                          `json:"access_token"`
		TokenID     string                          `json:"token_id"`
		Details     []substrate.AuthorizationDetail `json:"authorization_details"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(tokRaw, &tok) != nil || len(tok.Details) != 1 || tok.Details[0].Agent != agent {
		t.Fatalf("token: %d %s", resp.StatusCode, tokRaw)
	}
	app := e.WithToken(tok.AccessToken)

	// The app declares training with its memory kind, then the coach.
	app.MustJSON(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": []map[string]any{
		vocabulary.PackageManifest(training, 0),
		vocabulary.BundleManifest(training, map[string]any{"installs": []any{routine, memory}}),
		vocabulary.KindManifest(training, map[string]any{"singular": "routine"}, map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(training, map[string]any{"singular": "memory"}, map[string]any{"properties": map[string]any{
			"text": map[string]any{"type": "text"}, "topic": map[string]any{"type": "string"}, "source": map[string]any{"type": "string"},
			"active": map[string]any{"type": "bool", "default": true},
		}}),
	}}, nil)
	coach := map[string]any{"documents": []map[string]any{
		vocabulary.PackageManifest(coachPkg, 0),
		vocabulary.BundleManifest(coachPkg, map[string]any{"requires": []any{training}, "installs": []any{agent}}),
		vocabulary.AgentManifest(coachPkg, "workoutCoach", map[string]any{
			"description": "the coach", "prompt": "You coach.", "provider": "coachllm", "model": "coach", "purpose": "primary",
			"tools": []any{
				map[string]any{"function": "substrate.reamde.dev/core/query"},
				map[string]any{"function": "substrate.reamde.dev/core/write"},
				map[string]any{"function": "substrate.reamde.dev/core/ask"},
			},
			"context": map[string]any{"pinned": []any{map[string]any{
				"kind": memory, "filter": `{"properties": {"active": {"eq": true}}}`, "orderBy": "createdAt:desc,id", "first": 60,
				"fields": []any{"text", "topic"},
			}}},
			"permissions": map[string]any{
				"reads":  map[string]any{"kinds": []any{routine, memory}},
				"writes": []any{routine, memory, "substrate.reamde.dev/llm/interaction"},
			},
			"budgets": map[string]any{"spendCentsPerDay": 100},
		}),
	}}
	app.MustJSON(http.MethodPost, "/api/v1/vocabulary/apply", coach, nil)

	// The app reads its coach's row, and sees the provider without its key.
	agentPath := "/api/v1/substrate.reamde.dev/core/agent/" + url.PathEscape(agent)
	if status, body := app.Do(http.MethodGet, agentPath, nil); status != http.StatusOK || !strings.Contains(string(body), "You coach.") {
		t.Fatalf("the app reads its coach: %d %s", status, body)
	}
	if status, body := app.Do(http.MethodGet, "/api/v1/substrate.reamde.dev/llm/provider/coachllm", nil); status != http.StatusOK || strings.Contains(string(body), "sk-test-secret-key") {
		t.Fatalf("the app reads the provider: %d %s; want it without the key", status, body)
	}

	// The owner chats with the coach: it stores a memory, which the next
	// chat pins.
	call := func(input string) (int, []byte) {
		return e.Do(http.MethodPost, agentPath+"/call", map[string]any{"input": input})
	}
	if status, body := call("Please remember I train three days a week."); status != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("the first chat: %d %s", status, body)
	}
	if status, body := e.Do(http.MethodGet, "/api/v1/"+memory+"/schedule-1", nil); status != http.StatusOK || !strings.Contains(string(body), "three days a week") {
		t.Fatalf("the coach's memory: %d %s", status, body)
	}
	if status, body := call("What should I do today?"); status != http.StatusOK {
		t.Fatalf("the second chat: %d %s", status, body)
	}
	if !strings.Contains(model.lastSystem(), "Trains three days a week") {
		t.Fatalf("the second chat's prompt does not pin the memory: %s", model.lastSystem())
	}

	// The app removes its coach, which takes the agent and leaves the
	// memory, and puts it back under the approval it holds.
	bundlePath := "/api/v1/substrate.reamde.dev/core/bundle/" + url.PathEscape(coachPkg)
	if status, body := app.Do(http.MethodPatch, bundlePath, map[string]any{"properties": map[string]any{"uninstalled": true}}); status/100 != 2 {
		t.Fatalf("the app removes its coach: %d %s", status, body)
	}
	if status, _ := call("Hello?"); status != http.StatusNotFound {
		t.Fatalf("the removed coach answered %d, want 404", status)
	}
	if status, _ := e.Do(http.MethodGet, "/api/v1/"+memory+"/schedule-1", nil); status != http.StatusOK {
		t.Fatalf("the memory went with the coach: %d", status)
	}
	trainingBundle := "/api/v1/substrate.reamde.dev/core/bundle/" + url.PathEscape(training)
	if status, _ := app.Do(http.MethodPatch, trainingBundle, map[string]any{"properties": map[string]any{"uninstalled": true}}); status != http.StatusForbidden {
		t.Fatalf("the app removed its training kinds: %d, want 403", status)
	}
	app.MustJSON(http.MethodPost, "/api/v1/vocabulary/apply", coach, nil)

	// The owner revokes the app: the coach stops, and says why.
	if status, body := e.Do(http.MethodDelete, "/api/v1/substrate.reamde.dev/core/token/"+url.PathEscape(tok.TokenID), nil); status/100 != 2 {
		t.Fatalf("revoke the app: %d %s", status, body)
	}
	if status, body := call("Still there?"); status != http.StatusForbidden || !strings.Contains(string(body), "holds no live approval") {
		t.Fatalf("the coach after revocation: %d %s", status, body)
	}
}
