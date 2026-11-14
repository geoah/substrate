package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/providersecret"
)

// vendorAnswer is one failure answer a vendor documents, and the class its
// wire sorts it into.
type vendorAnswer struct {
	name   string
	status int
	body   string
	// header is a response header the answer carries, as "Name: value".
	header     string
	want       FailureClass
	retryAfter time.Duration
}

// anthropicAnswers are the error shapes of Anthropic's Messages API
// (platform.claude.com/docs/en/api/errors), one per documented type, and the
// prose case the type does not say.
var anthropicAnswers = []vendorAnswer{
	{
		name: "400 invalid_request_error", status: 400, want: FailureRejected,
		body: `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: 99999 > 64000, which is the maximum allowed"}}`,
	},
	{
		name: "400 a prompt past the window", status: 400, want: FailureContextTooLong,
		body: `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 214290 tokens > 200000 maximum"}}`,
	},
	{
		name: "400 an account out of credit", status: 400, want: FailureBilling,
		body: `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`,
	},
	{
		name: "429 at the spend limit the organization set", status: 429, want: FailureBilling,
		body: `{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your organization's spend limit.","details":{"error_code":"enforced_spend_limit_reached"}}}`,
	},
	{
		name: "400 at the usage limit the workspace set", status: 400, want: FailureBilling,
		body: `{"type":"error","error":{"type":"invalid_request_error","message":"You have reached your specified workspace API usage limits. You will regain access on 2026-12-01 at 00:00 UTC."}}`,
	},
	{
		name: "401 authentication_error", status: 401, want: FailureKeyRefused,
		body: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
	},
	{
		name: "402 billing_error", status: 402, want: FailureBilling,
		body: `{"type":"error","error":{"type":"billing_error","message":"There is an issue with your billing or payment information."}}`,
	},
	{
		name: "403 permission_error", status: 403, want: FailureModelUnavailable,
		body: `{"type":"error","error":{"type":"permission_error","message":"Your API key does not have permission to use the specified resource."}}`,
	},
	{
		name: "404 not_found_error", status: 404, want: FailureModelUnavailable,
		body: `{"type":"error","error":{"type":"not_found_error","message":"model: claude-nothing-9"}}`,
	},
	{
		name: "413 request_too_large", status: 413, want: FailureContextTooLong,
		body: `{"type":"error","error":{"type":"request_too_large","message":"Request exceeds the maximum allowed number of bytes."}}`,
	},
	{
		name: "429 rate_limit_error", status: 429, header: "Retry-After: 20", want: FailureRateLimited, retryAfter: 20 * time.Second,
		body: `{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your organization's rate limit of 50 requests per minute."}}`,
	},
	{
		name: "429 with the wait in milliseconds", status: 429, header: "Retry-After-Ms: 1500", want: FailureRateLimited, retryAfter: 1500 * time.Millisecond,
		body: `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
	},
	{
		name: "500 api_error", status: 500, want: FailureUnavailable,
		body: `{"type":"error","error":{"type":"api_error","message":"An unexpected error has occurred internal to Anthropic's systems."}}`,
	},
	{
		name: "504 timeout_error", status: 504, want: FailureUnavailable,
		body: `{"type":"error","error":{"type":"timeout_error","message":"The request timed out while processing."}}`,
	},
	{
		name: "529 overloaded_error", status: 529, want: FailureOverloaded,
		body: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
	},
	{name: "502 from a gateway, no body", status: 502, want: FailureUnavailable, body: `bad gateway`},
}

// openaiAnswers are the error shapes of OpenAI's API
// (developers.openai.com/api/docs/guides/error-codes), and those of the
// gateways that speak its wire.
var openaiAnswers = []vendorAnswer{
	{
		name: "400 context_length_exceeded", status: 400, want: FailureContextTooLong,
		body: `{"error":{"message":"This model's maximum context length is 128000 tokens. However, your messages resulted in 130112 tokens.","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`,
	},
	{
		name: "400 a parameter the model refuses", status: 400, want: FailureRejected,
		body: `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'none' with this model.","type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}`,
	},
	{
		name: "401 invalid_api_key", status: 401, want: FailureKeyRefused,
		body: `{"error":{"message":"Incorrect API key provided.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`,
	},
	{
		name: "403 a region the vendor does not serve", status: 403, want: FailureRejected,
		body: `{"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","param":null,"type":"request_forbidden"}}`,
	},
	{
		name: "404 model_not_found", status: 404, want: FailureModelUnavailable,
		body: `{"error":{"message":"The model 'gpt-nothing' does not exist or you do not have access to it.","type":"invalid_request_error","param":null,"code":"model_not_found"}}`,
	},
	{
		name: "403 model_not_found for a project without the model", status: 403, want: FailureModelUnavailable,
		body: `{"error":{"message":"Project does not have access to model gpt-5","type":"invalid_request_error","param":null,"code":"model_not_found"}}`,
	},
	{
		name: "429 rate_limit_exceeded", status: 429, header: "Retry-After: 7", want: FailureRateLimited, retryAfter: 7 * time.Second,
		body: `{"error":{"message":"Rate limit reached for requests. Please try again in 7s.","type":"requests","param":null,"code":"rate_limit_exceeded"}}`,
	},
	{
		name: "429 insufficient_quota", status: 429, want: FailureBilling,
		body: `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","param":null,"code":"insufficient_quota"}}`,
	},
	{
		name: "429 credit_balance_exhausted", status: 429, want: FailureBilling,
		body: `{"error":{"message":"Your credit balance is exhausted.","type":"billing_error","param":null,"code":"credit_balance_exhausted"}}`,
	},
	{
		name: "429 project_spend_limit_exceeded", status: 429, want: FailureBilling,
		body: `{"error":{"message":"The project has reached its monthly spend limit.","type":"rate_limit_error","param":null,"code":"project_spend_limit_exceeded"}}`,
	},
	{
		name: "500 server_error", status: 500, want: FailureUnavailable,
		body: `{"error":{"message":"The server had an error while processing your request.","type":"server_error","param":null,"code":null}}`,
	},
	{
		name: "503 overloaded", status: 503, want: FailureOverloaded,
		body: `{"error":{"message":"The engine is currently overloaded, please try again later.","type":"server_error","param":null,"code":null}}`,
	},
	{
		name: "402 from a gateway out of credits", status: 402, want: FailureBilling,
		body: `{"error":{"code":402,"message":"Insufficient credits. Add more using the credits page."}}`,
	},
	{name: "429 from a gateway, by status alone", status: 429, want: FailureRateLimited, body: `too many requests`},
	{name: "504 from a gateway, no body", status: 504, want: FailureUnavailable, body: `gateway timeout`},
}

// The two sorting functions, held to the vendors' documented types and codes
// without a server in between.
func TestTheWiresSortAVendorsErrorTypesAndCodes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status                  int
		errType, detail, errMsg string
		want                    FailureClass
	}{
		{400, "invalid_request_error", "", "tools.0: unknown field", FailureRejected},
		{400, "invalid_request_error", "", "Your credit balance is too low", FailureBilling},
		{400, "invalid_request_error", "", "You have reached your specified API usage limits.", FailureBilling},
		{429, "rate_limit_error", "enforced_spend_limit_reached", "", FailureBilling},
		{401, "authentication_error", "", "", FailureKeyRefused},
		{402, "billing_error", "", "", FailureBilling},
		{403, "permission_error", "", "", FailureModelUnavailable},
		{404, "not_found_error", "", "", FailureModelUnavailable},
		{413, "request_too_large", "", "", FailureContextTooLong},
		{429, "rate_limit_error", "", "", FailureRateLimited},
		{500, "api_error", "", "", FailureUnavailable},
		{504, "timeout_error", "", "", FailureUnavailable},
		{529, "overloaded_error", "", "", FailureOverloaded},
		// A stream's error event: the stream itself answered 200.
		{200, "overloaded_error", "", "", FailureOverloaded},
		{200, "authentication_error", "", "", FailureKeyRefused},
		{403, "authentication_error", "", "", FailureKeyRefused},
		// The type on a status that refuses one request, not the key.
		{400, "authentication_error", "", "", FailureRejected},
		{200, "api_error", "", "", FailureUnavailable},
		{200, "a_type_from_next_year", "", "", FailureUnavailable},
		// A type this does not know falls to the status.
		{429, "a_type_from_next_year", "", "", FailureRateLimited},
		{418, "", "", "", FailureRejected},
	} {
		if got := anthropicFailure(tc.status, tc.errType, tc.detail, tc.errMsg); got != tc.want {
			t.Errorf("anthropicFailure(%d, %q, %q, %q) = %q, want %q", tc.status, tc.errType, tc.detail, tc.errMsg, got, tc.want)
		}
	}
	for _, tc := range []struct {
		status           int
		errType, errCode string
		want             FailureClass
	}{
		{401, "invalid_request_error", "invalid_api_key", FailureKeyRefused},
		{401, "", "", FailureKeyRefused},
		{429, "requests", "rate_limit_exceeded", FailureRateLimited},
		{429, "tokens", "rate_limit_exceeded", FailureRateLimited},
		{429, "insufficient_quota", "insufficient_quota", FailureBilling},
		{400, "invalid_request_error", "billing_hard_limit_reached", FailureBilling},
		{429, "", "credit_balance_exhausted", FailureBilling},
		{429, "", "organization_spend_limit_exceeded", FailureBilling},
		{429, "", "project_spend_limit_exceeded", FailureBilling},
		{429, "", "organization_usage_limit_exceeded", FailureBilling},
		{404, "invalid_request_error", "model_not_found", FailureModelUnavailable},
		{400, "invalid_request_error", "context_length_exceeded", FailureContextTooLong},
		{400, "invalid_request_error", "unsupported_value", FailureRejected},
		{500, "server_error", "", FailureUnavailable},
		{503, "server_error", "", FailureOverloaded},
		{529, "", "", FailureOverloaded},
		{408, "", "", FailureUnavailable},
		// An error event inside a stream carries no status.
		{0, "server_error", "", FailureUnavailable},
		{0, "requests", "rate_limit_exceeded", FailureRateLimited},
		{0, "", "", ""},
	} {
		if got := openaiFailure(tc.status, tc.errType, tc.errCode); got != tc.want {
			t.Errorf("openaiFailure(%d, %q, %q) = %q, want %q", tc.status, tc.errType, tc.errCode, got, tc.want)
		}
	}
}

// Each vendor's documented answer comes out of its wire's Complete with its
// class and the wait it named, one-shot and streamed alike, through the
// rebuild that scrubs the key.
func TestAFailedCallCarriesItsClassThroughEachWire(t *testing.T) {
	t.Parallel()
	const key = "zq-class-key-fixture"
	for wire, answers := range map[Wire][]vendorAnswer{WireAnthropic: anthropicAnswers, WireOpenAI: openaiAnswers} {
		for _, a := range answers {
			for _, streamed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/streamed=%v", wire, a.name, streamed), func(t *testing.T) {
					t.Parallel()
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						// No retry: the anthropic adapter retries a 429 and a
						// 5xx unless the endpoint says not to.
						w.Header().Set("X-Should-Retry", "false")
						if name, value, ok := strings.Cut(a.header, ": "); ok {
							w.Header().Set(name, value)
						}
						w.WriteHeader(a.status)
						// The answer quotes the key, as a vendor's may.
						body := strings.Replace(a.body, `"message":"`, `"message":"`+key+` `, 1)
						if body == a.body {
							body += " " + key
						}
						_, _ = w.Write([]byte(body))
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
					class, wait := FailureOf(err)
					if class != a.want || wait != a.retryAfter {
						t.Fatalf("FailureOf = %q after %v, want %q after %v: %v", class, wait, a.want, a.retryAfter, err)
					}
					if a.want == FailureContextTooLong && a.status == 400 && !errors.Is(err, ErrContextTooLong) {
						t.Fatalf("a prompt past the window lost the mark the loop compacts on: %v", err)
					}
					if strings.Contains(err.Error(), key) {
						t.Fatalf("the error carries the key: %v", err)
					}
				})
			}
		}
	}
}

// An error event inside a stream that answered 200 is sorted by its type:
// Anthropic sends `overloaded_error` this way under load.
func TestAStreamsErrorEventCarriesItsClass(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n")
		_, _ = fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	}))
	t.Cleanup(srv.Close)
	c, err := New(WireAnthropic, Config{BaseURL: srv.URL + "/", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Params: Params{MaxTokens: 16},
	}, func(Delta) {})
	if class, _ := FailureOf(err); class != FailureOverloaded {
		t.Fatalf("FailureOf = %q, want overloaded: %v", class, err)
	}
}

// A refusal of the key that arrives as an error event inside a stream that
// answered 200 is a refused key like any other: the class, and the mark the
// engine's table of refused keys reads, both survive the scrubbed rebuild.
func TestAKeyRefusedInsideAStreamIsStillARefusedKey(t *testing.T) {
	t.Parallel()
	const key = "zq-stream-key-fixture"
	for wire, events := range map[Wire]string{
		WireAnthropic: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n" +
			"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"invalid x-api-key " + key + "\"}}\n\n",
		WireOpenAI: "data: {\"error\":{\"message\":\"Incorrect API key provided: " + key + "\",\"type\":\"invalid_request_error\",\"param\":null,\"code\":\"invalid_api_key\"}}\n\n",
	} {
		t.Run(string(wire), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, events)
			}))
			t.Cleanup(srv.Close)
			c, err := New(wire, Config{BaseURL: srv.URL + "/", APIKey: key})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Complete(context.Background(), Request{
				Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Params: Params{MaxTokens: 16},
			}, func(Delta) {})
			if err == nil {
				t.Fatal("no error")
			}
			if class, _ := FailureOf(err); class != FailureKeyRefused {
				t.Fatalf("FailureOf = %q, want key_refused: %v", class, err)
			}
			if got := providersecret.KeyRefusal(err); got != http.StatusUnauthorized {
				t.Fatalf("KeyRefusal = %d, want the 401 the refusal would have been: %v", got, err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("the error carries the key: %v", err)
			}
		})
	}
}

// A connection that drops mid-answer, and one that never opens, are the
// provider being unavailable; the caller leaving is no failure of the
// provider's at all.
func TestAFailedConnectionIsUnavailableAndALeavingCallerIsNot(t *testing.T) {
	t.Parallel()
	for _, wire := range []Wire{WireOpenAI, WireAnthropic} {
		t.Run(string(wire), func(t *testing.T) {
			t.Parallel()
			dropped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Should-Retry", "false")
				w.WriteHeader(http.StatusOK)
				if wire == WireOpenAI {
					_, _ = fmt.Fprint(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"par\"}}]}\n\n")
				} else {
					_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n")
				}
				w.(http.Flusher).Flush()
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}))
			t.Cleanup(dropped.Close)
			request := Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Params: Params{MaxTokens: 16}}
			c, err := New(wire, Config{BaseURL: dropped.URL + "/", APIKey: "k"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Complete(context.Background(), request, func(Delta) {})
			if class, _ := FailureOf(err); err == nil || class != FailureUnavailable {
				t.Fatalf("a dropped stream = %q, want unavailable: %v", class, err)
			}

			gone := httptest.NewServer(http.NotFoundHandler())
			url := gone.URL
			gone.Close()
			c, err = New(wire, Config{BaseURL: url + "/", APIKey: "k"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Complete(context.Background(), request, func(Delta) {})
			if class, _ := FailureOf(err); err == nil || class != FailureUnavailable {
				t.Fatalf("a refused connection = %q, want unavailable: %v", class, err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			c, err = New(wire, Config{BaseURL: dropped.URL + "/", APIKey: "k"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Complete(ctx, request, func(Delta) {})
			if class, _ := FailureOf(err); err == nil || class != "" {
				t.Fatalf("a canceled call = %q, want no class: %v", class, err)
			}
		})
	}
}
