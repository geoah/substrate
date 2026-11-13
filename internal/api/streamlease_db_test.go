package api

// An agent stream a full token opened ends with its token: the revoke is a
// changelog write, the stream reads its token again and ends with an error
// event whose code is `auth` (decision 0272).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// holdingChat runs a chat turn until its stream ends: a turn that holds
// without a model, which the engine cannot be made to run.
type holdingChat struct {
	substrate.Dataset
	started chan struct{}
}

func (d holdingChat) ChatAgent(ctx context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "tA"})
	close(d.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestAFullTokensAgentStreamEndsWithItsToken(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	started := make(chan struct{})
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return holdingChat{Dataset: ds, started: started} }
	_, info, err := env.svc.Authenticate(context.Background(), env.token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	other := env.mint(t, substrate.MintTokenInput{Label: "other"})

	req := httptest.NewRequest(http.MethodPost, helperChatPath, bytes.NewReader([]byte(`{"message":"hi"}`)))
	req.Header.Set("Authorization", "Bearer "+env.token)
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

	wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+info.ID, other, nil), http.StatusOK)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent stream ran on after its token was revoked")
	}
	var last substrate.AgentEvent
	for _, line := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		_ = json.Unmarshal([]byte(line), &last)
	}
	if last.Kind != substrate.AgentEventError || last.Code != codeAuth {
		t.Fatalf("the stream ended with %+v, want an error event with the code auth (body %s)", last, rec.Body.String())
	}
}
