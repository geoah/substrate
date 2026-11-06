package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/providersecret"
)

// keyAnswer is one failure answer a vendor sends, and whether it refuses the
// key: every 401, and a 403 only where its error names the key.
type keyAnswer struct {
	name    string
	status  int
	body    string
	refused bool
}

// keyAnswers are the vendors' documented failure shapes. Each body quotes the
// key so the test also holds the scrub.
func keyAnswers(key string) []keyAnswer {
	return []keyAnswer{
		{
			"openai 401 invalid_api_key", http.StatusUnauthorized,
			`{"error":{"message":"Incorrect API key provided: ` + key + `","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`, true,
		},
		{
			"anthropic 401 authentication_error", http.StatusUnauthorized,
			`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key ` + key + `"}}`, true,
		},
		{"401 with no error body", http.StatusUnauthorized, `unauthorized ` + key, true},
		{
			"anthropic 403 authentication_error", http.StatusForbidden,
			`{"type":"error","error":{"type":"authentication_error","message":"refused ` + key + `"}}`, true,
		},
		{
			"gateway 403 invalid_api_key", http.StatusForbidden,
			`{"error":{"message":"refused ` + key + `","type":"invalid_request_error","code":"invalid_api_key"}}`, true,
		},
		{
			"openai 403 region", http.StatusForbidden,
			`{"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}`, false,
		},
		{
			"openai 403 missing scope", http.StatusForbidden,
			`{"error":{"message":"You have insufficient permissions for this operation. Missing scopes: model.request.","type":"invalid_request_error","param":null,"code":null}}`, false,
		},
		{
			"anthropic 403 permission_error", http.StatusForbidden,
			`{"type":"error","error":{"type":"permission_error","message":"Your API key does not have permission to use the specified resource."}}`, false,
		},
		{
			"openrouter 403 moderation", http.StatusForbidden,
			`{"error":{"code":403,"message":"Your chosen model requires moderation and your input was flagged"}}`, false,
		},
		{"403 with no error body", http.StatusForbidden, `forbidden`, false},
		{
			"400", http.StatusBadRequest,
			`{"type":"error","error":{"type":"authentication_error","message":"refused ` + key + `"}}`, false,
		},
		{"429", http.StatusTooManyRequests, `{"error":{"message":"slow down","type":"rate_limit_error"}}`, false},
		{"500", http.StatusInternalServerError, `{"error":{"message":"oops","type":"api_error"}}`, false},
	}
}

// An answer that refuses the key is marked with its status on both wires,
// through the scrubbed rebuild, one-shot and streamed alike, so the engine
// can tell a refused key from any other failure. A 403 that refuses one
// request (a region, a model, a missing permission, moderation) carries no
// mark, and the mark carries no key.
func TestBothWiresMarkARefusedKeyWithItsStatus(t *testing.T) {
	t.Parallel()
	const key = "zq-refused-key-fixture"
	for _, wire := range []Wire{WireOpenAI, WireAnthropic} {
		for _, a := range keyAnswers(key) {
			for _, streamed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/streamed=%v", wire, a.name, streamed), func(t *testing.T) {
					t.Parallel()
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						// No retry: the anthropic adapter retries a 429 and a
						// 5xx unless the endpoint says not to.
						w.Header().Set("X-Should-Retry", "false")
						w.WriteHeader(a.status)
						_, _ = w.Write([]byte(a.body))
					}))
					t.Cleanup(srv.Close)
					c, err := New(wire, Config{BaseURL: srv.URL + "/", APIKey: key})
					if err != nil {
						t.Fatal(err)
					}
					var onDelta func(Delta)
					if streamed {
						onDelta = func(Delta) {}
					}
					_, err = c.Complete(context.Background(), Request{
						Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Params: Params{MaxTokens: 16},
					}, onDelta)
					if err == nil {
						t.Fatal("no error")
					}
					want := 0
					if a.refused {
						want = a.status
					}
					if got := providersecret.KeyRefusal(err); got != want {
						t.Fatalf("KeyRefusal = %d, want %d: %v", got, want, err)
					}
					if strings.Contains(err.Error(), key) {
						t.Fatalf("the error carries the key: %v", err)
					}
				})
			}
		}
	}
}
