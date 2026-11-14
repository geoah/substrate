package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// operatorDefaultsLLM is an OpenAI-wire chat endpoint standing in for the
// operator's: every completion answers "ok" and reports 10 prompt and 5
// completion tokens, so an operator-paid call settles a known cost.
func operatorDefaultsLLM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		if stream, _ := req["stream"].(bool); !stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cmpl", "object": "chat.completion",
				"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
				"usage":   usage,
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []map[string]any{
			{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "ok"}}}},
			{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}},
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

// The owner reads the engine's answer as it stands at
// GET /api/v1/operator/defaults: the site's name, the allowance with what an
// operator-paid call spent today, and an item the operator offers while the
// person's own key serves it.
//
// Serial: the fake endpoint is on loopback, which the server's dial gate
// refuses unless the process environment allows it, and t.Setenv is
// process-wide.
func TestOperatorDefaultsAnswersTheOwner(t *testing.T) {
	t.Setenv(egress.AllowVar, "127.0.0.0/8,::1/128")
	srv := operatorDefaultsLLM(t)
	cfg := &operatordefaults.Config{
		LLM: map[string]*operatordefaults.LLM{"chatllm": {
			ID: "chatllm", Wire: llm.WireOpenAI, BaseURL: srv.URL, APIKey: operatordefaults.NewSecret("operator-key-fixture"),
			Models:          []operatordefaults.Model{{Model: "chat", InputPer1M: "1", OutputPer1M: "5"}},
			MaxOutputTokens: 64,
		}},
		Budget: operatordefaults.Budget{CentsPerDay: 200},
	}
	env := newDBEnv(t,
		withEngine(engine.WithOperatorDefaults(cfg, "Example")),
		withConfig(func(c *Config) { c.OperatorDefaults = true }))
	ctx := t.Context()
	provider := vocabulary.LLMKind("provider")
	// The row names no key, so the operator's transport serves it and pays.
	env.put(t, provider, "chatllm", map[string]any{"wire": "openai", "baseURL": "https://llm.example.com/v1"})
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
  id: %[1]s/chatter
data:
  authority: %[2]s
  package: crew
  description: chatter under test
  prompt: You are chatter.
  provider: chatllm
  model: chat
`, crew, env.repository))
	if _, err := env.ds.CallAgent(ctx, crew+"/chatter", "go"); err != nil {
		t.Fatalf("the operator-paid call: %v", err)
	}
	// The person's own key: the row is served by it from here on, and the
	// operator still offers the slot.
	if _, err := env.ds.Patch(ctx, substrate.ActorPerson, provider, "chatllm", substrate.PatchInput{
		Properties: map[string]any{"apiKey": "row-own-key"},
	}); err != nil {
		t.Fatalf("key the row: %v", err)
	}

	rec := env.do(t, http.MethodGet, "/api/v1/operator/defaults", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.OperatorDefaults](t, rec)
	if got.Operator == nil || got.Operator.Name != "Example" || got.Operator.Allowance == nil {
		t.Fatalf("operator = %+v", got.Operator)
	}
	if a := got.Operator.Allowance; a.CentsPerDay != 200 || a.UsedCents < 1 || a.ResetsAt.IsZero() ||
		len(a.BySlot) != 1 || a.BySlot[0].Slot != substrate.SlotLLM || a.BySlot[0].Cents != a.UsedCents {
		t.Fatalf("allowance = %+v, want the call's spend under llm", a)
	}
	var item *substrate.OperatorDefault
	for i := range got.Items {
		if got.Items[i].ID == "llm/chatllm" {
			item = &got.Items[i]
		}
	}
	if item == nil || item.Source != substrate.SourceOwn || !item.OperatorOffers || item.Target != vocabulary.RecordPath(provider, "chatllm") {
		t.Fatalf("items = %+v, want llm/chatllm on its own key and offered by the operator", got.Items)
	}
}

// A scoped app token is refused at the route, and a full-access one reads it
// as the owner does (decision 0204).
func TestOperatorDefaultsRefusesAScopedAppToken(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	const client = "https://notes.example.org/client.json"
	scoped := env.mint(t, substrate.MintTokenInput{
		Label: "notes", ClientID: client, ClientName: "Notes",
		Scopes: []string{"read:" + env.repository + "/notes/note"},
	})
	rec := env.do(t, http.MethodGet, "/api/v1/operator/defaults", scoped, nil)
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)

	full := env.mint(t, substrate.MintTokenInput{Label: "notes", ClientID: client, ClientName: "Notes"})
	rec = env.do(t, http.MethodGet, "/api/v1/operator/defaults", full, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.OperatorDefaults](t, rec); got.Operator != nil || got.Items == nil {
		t.Fatalf("a deployment with no defaults file answered %+v", got)
	}
}

// A grant that reads neither the provider rows nor the model records is
// offered no models, while the owner's dataset offers the row's: what each
// row prices is read off both kinds.
func TestScopedDatasetOffersNoModelsWithoutBothKinds(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	env.put(t, vocabulary.LLMKind("provider"), "anthropic", map[string]any{
		"wire":    "anthropic",
		"pricing": []any{map[string]any{"model": "claude-test", "inputPer1M": "3", "outputPer1M": "15"}},
	})
	if offers, err := env.ds.ModelOffers(t.Context()); err != nil || len(offers["anthropic"]) != 1 {
		t.Fatalf("the owner's dataset offers %v (%v)", offers, err)
	}
	ds := newScopedDataset(env.ds, scope.Grant{}, substrate.Actor("app:example.com"))
	offers, err := ds.ModelOffers(t.Context())
	if err != nil || len(offers) != 0 {
		t.Fatalf("offers %v (%v), want none", offers, err)
	}
}
