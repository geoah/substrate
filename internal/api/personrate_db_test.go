package api

// The MCP door is paced with the routes, from the same allowance, once per
// request however many requests its tool makes in process. The tool's reads
// are the engine's, so this runs on it.

import (
	"context"
	"net/http"
	"testing"
)

func TestTheDoorSharesThePersonsAllowance(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withConfig(func(c *Config) { c.RequestsPerPerson, c.RequestBurstPerPerson = 1, 2 }))
	_, assistant := mcpAssistantToken(t, env)
	if text, isErr := callTool(t, env.h, assistant, "overview", map[string]any{}); isErr {
		t.Fatalf("overview: %s", text)
	}
	wantStatus(t, rpc(t, env.h, assistant, "ping", nil), http.StatusOK)
	wantErrorCode(t, rpc(t, env.h, assistant, "ping", nil), http.StatusTooManyRequests, codeRateLimited)

	// The owner's own token acts for the same person.
	wantErrorCode(t, env.do(t, http.MethodGet, recordsPath+"?filter=%7B%7D", env.token, nil), http.StatusTooManyRequests, codeRateLimited)
}

// An app that spends its person's requests cannot keep its person from
// revoking it: a request of the person's own to manage their tokens is paced
// by a bucket no app's token reaches.
func TestAnAppThatSpentItsPersonsRequestsIsStillRevoked(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withConfig(func(c *Config) { c.RequestsPerPerson, c.RequestBurstPerPerson = 1, 2 }))
	appSecret := appScopedToken(t, env, "read:*")
	_, appToken, err := env.svc.Authenticate(context.Background(), appSecret)
	if err != nil {
		t.Fatal(err)
	}
	for {
		rec := env.do(t, http.MethodGet, recordsPath+"?filter=%7B%7D", appSecret, nil)
		if rec.Code == http.StatusTooManyRequests {
			break
		}
		wantStatus(t, rec, http.StatusOK)
	}
	// The person's other requests wait with the app's.
	wantErrorCode(t, env.do(t, http.MethodGet, recordsPath+"?filter=%7B%7D", env.token, nil), http.StatusTooManyRequests, codeRateLimited)
	// Their tokens they still manage.
	wantStatus(t, env.do(t, http.MethodGet, "/tokens", env.token, nil), http.StatusOK)
	wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+appToken.ID, env.token, nil), http.StatusOK)
	wantErrorCode(t, env.do(t, http.MethodGet, recordsPath+"?filter=%7B%7D", appSecret, nil), http.StatusUnauthorized, codeAuth)
}
