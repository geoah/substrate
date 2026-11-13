package api

// The MCP door is paced with the routes, from the same allowance, once per
// request however many requests its tool makes in process. The tool's reads
// are the engine's, so this runs on it.

import (
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
