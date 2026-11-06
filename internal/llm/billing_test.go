package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/egress"
)

// A caller paying per call (the host's ledger, internal/engine/hostspend.go)
// releases a reservation only on what these tests hold: a failure is marked
// egress.ErrUnprocessed only when the endpoint cannot have acted on the
// request, a Result carries Usage only when the reply reported it whole, and
// a RetryOnlyUnbilled client makes no attempt after one that may have been
// billed.

// openaiOneShot is a whole one-shot reply; usage, when set, is its usage.
func openaiOneShot(usage string) string {
	body := `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]`
	if usage != "" {
		body += `,"usage":` + usage
	}
	return body + "}"
}

// openaiStream is a whole streamed reply; usage, when set, rides its own
// chunk before [DONE].
func openaiStream(usage string) string {
	out := "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	if usage != "" {
		out += "data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":" + usage + "}\n\n"
	}
	return out + "data: [DONE]\n\n"
}

const openaiUsageJSON = `{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`

// cutOff answers 200 with a length the body never reaches and drops the
// connection: an answer the endpoint started and the client lost.
func cutOff(w http.ResponseWriter, contentType, partial string) {
	conn, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: %s\r\nRetry-After-Ms: 0\r\nContent-Length: %d\r\n\r\n%s",
		contentType, len(partial)+1000, partial)
	_ = buf.Flush()
	_ = conn.Close()
}

// answer writes body as a 200 of contentType.
func answer(w http.ResponseWriter, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	_, _ = fmt.Fprint(w, body)
}

// refuse answers status with an error body either wire reads.
func refuse(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After-Ms", "0")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"api_error","message":"refused %d"}}`, status)
}

// closedURL is a base URL nothing listens on: a request to it never writes
// its headers.
func closedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func completeAt(t *testing.T, wire Wire, baseURL string, stream bool) (*Result, error) {
	t.Helper()
	client, err := New(wire, Config{BaseURL: baseURL, APIKey: "sk-billing-fixture"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	var onDelta func(Delta)
	if stream {
		onDelta = func(Delta) {}
	}
	return client.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, onDelta)
}

// A failure is marked unprocessed when no request left or every answer was a
// redirect or a 4xx, on both wires, one-shot and streamed; a 5xx, an answer
// cut off midway and an answer that does not decode are not, because the
// endpoint may have done the work.
func TestAFailureSaysWhetherTheEndpointCanHaveActedOnIt(t *testing.T) {
	type wireCase struct {
		wire        Wire
		contentType func(stream bool) string
		garbage     func(stream bool) string
	}
	wires := []wireCase{
		{
			wire: WireOpenAI,
			contentType: func(stream bool) string {
				if stream {
					return "text/event-stream"
				}
				return "application/json"
			},
			garbage: func(stream bool) string {
				if stream {
					return "data: {\"choices\":[{\"delta\":\n\n"
				}
				return `{"choices":[{"message":`
			},
		},
		{
			wire: WireAnthropic,
			contentType: func(stream bool) string {
				if stream {
					return "text/event-stream"
				}
				return "application/json"
			},
			garbage: func(stream bool) string {
				if stream {
					return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":\n\n"
				}
				return `{"content":[{"type":`
			},
		},
	}
	cases := []struct {
		name        string
		unprocessed bool
		// serve answers attempt n (from 1); nil means nothing listens.
		serve func(w http.ResponseWriter, n int32, wc wireCase, stream bool)
	}{
		{"a 401", true, func(w http.ResponseWriter, _ int32, _ wireCase, _ bool) { refuse(w, http.StatusUnauthorized) }},
		{"a 400", true, func(w http.ResponseWriter, _ int32, _ wireCase, _ bool) { refuse(w, http.StatusBadRequest) }},
		{"a 429 on every attempt", true, func(w http.ResponseWriter, _ int32, _ wireCase, _ bool) { refuse(w, http.StatusTooManyRequests) }},
		{"a redirect", true, func(w http.ResponseWriter, _ int32, _ wireCase, _ bool) {
			w.Header().Set("Location", "https://elsewhere.example.com/v1")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}},
		{"nothing listening", true, nil},
		{"a 500", false, func(w http.ResponseWriter, _ int32, _ wireCase, _ bool) { refuse(w, http.StatusInternalServerError) }},
		{"a 500 and then a 429", false, func(w http.ResponseWriter, n int32, _ wireCase, _ bool) {
			if n == 1 {
				refuse(w, http.StatusInternalServerError)
				return
			}
			refuse(w, http.StatusTooManyRequests)
		}},
		{"an answer cut off midway", false, func(w http.ResponseWriter, _ int32, wc wireCase, stream bool) {
			cutOff(w, wc.contentType(stream), `{"id":"c",`)
		}},
		{"an answer that does not decode", false, func(w http.ResponseWriter, _ int32, wc wireCase, stream bool) {
			answer(w, wc.contentType(stream), wc.garbage(stream))
		}},
	}
	for _, wc := range wires {
		for _, tc := range cases {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s %s stream=%v", wc.wire, tc.name, stream), func(t *testing.T) {
					t.Parallel()
					base := closedURL(t)
					if tc.serve != nil {
						var attempts atomic.Int32
						srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
							tc.serve(w, attempts.Add(1), wc, stream)
						}))
						t.Cleanup(srv.Close)
						base = srv.URL
					}
					_, err := completeAt(t, wc.wire, base, stream)
					if err == nil {
						t.Fatal("the call succeeded")
					}
					if got := errors.Is(err, egress.ErrUnprocessed); got != tc.unprocessed {
						t.Fatalf("unprocessed = %v, want %v: %v", got, tc.unprocessed, err)
					}
				})
			}
		}
	}
}

// A reply that leaves its usage out returns no Usage, on both wires,
// one-shot and streamed, so it is never read as having cost nothing; a reply
// that reports it returns it.
func TestUsageIsNilUnlessTheReplyReportedItWhole(t *testing.T) {
	anthropicNoDelta := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":4,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_stop"}`,
	}
	anthropicOneShot := func(usage string) string {
		body := `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"`
		if usage != "" {
			body += `,"usage":` + usage
		}
		return body + "}"
	}
	cases := []struct {
		name        string
		wire        Wire
		stream      bool
		contentType string
		body        string
		reported    bool
	}{
		{"openai one-shot without usage", WireOpenAI, false, "application/json", openaiOneShot(""), false},
		{"openai one-shot with an empty usage", WireOpenAI, false, "application/json", openaiOneShot(`{}`), false},
		{"openai one-shot with usage", WireOpenAI, false, "application/json", openaiOneShot(openaiUsageJSON), true},
		{"openai stream without usage", WireOpenAI, true, "text/event-stream", openaiStream(""), false},
		{"openai stream with usage", WireOpenAI, true, "text/event-stream", openaiStream(openaiUsageJSON), true},
		{"anthropic one-shot without usage", WireAnthropic, false, "application/json", anthropicOneShot(""), false},
		{"anthropic one-shot without output_tokens", WireAnthropic, false, "application/json", anthropicOneShot(`{"input_tokens":4}`), false},
		{"anthropic one-shot with usage", WireAnthropic, false, "application/json", anthropicOneShot(`{"input_tokens":4,"output_tokens":2}`), true},
		{"anthropic stream without a final output count", WireAnthropic, true, "text/event-stream", anthropicEventStream(anthropicNoDelta...), false},
		{"anthropic stream with usage", WireAnthropic, true, "text/event-stream", anthropicEventStream(anthropicStreamOK...), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				answer(w, tc.contentType, tc.body)
			}))
			t.Cleanup(srv.Close)
			res, err := completeAt(t, tc.wire, srv.URL, tc.stream)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if res.Content != "ok" {
				t.Fatalf("content = %q", res.Content)
			}
			if got := res.Usage != nil; got != tc.reported {
				t.Fatalf("usage = %+v, want reported %v", res.Usage, tc.reported)
			}
		})
	}
}

// A call that fails after the endpoint reported its whole usage returns that
// usage beside the error: a reply with no choices, a stream cut off after its
// usage. A stream cut off before its final count returns none.
func TestAFailureAfterTheUsageKeepsIt(t *testing.T) {
	anthropicUpToDelta := anthropicEventStream(anthropicStreamOK[:5]...)
	anthropicUpToBlock := anthropicEventStream(anthropicStreamOK[:3]...)
	openaiUpToUsage := strings.TrimSuffix(openaiStream(openaiUsageJSON), "data: [DONE]\n\n")
	cases := []struct {
		name     string
		wire     Wire
		stream   bool
		serve    func(w http.ResponseWriter)
		reported bool
	}{
		{"openai one-shot with no choices", WireOpenAI, false, func(w http.ResponseWriter) {
			answer(w, "application/json", `{"id":"c","object":"chat.completion","choices":[],"usage":`+openaiUsageJSON+`}`)
		}, true},
		{"openai stream cut off after its usage", WireOpenAI, true, func(w http.ResponseWriter) {
			cutOff(w, "text/event-stream", openaiUpToUsage)
		}, true},
		{"anthropic stream cut off after its final count", WireAnthropic, true, func(w http.ResponseWriter) {
			cutOff(w, "text/event-stream", anthropicUpToDelta)
		}, true},
		{"anthropic stream cut off before its final count", WireAnthropic, true, func(w http.ResponseWriter) {
			cutOff(w, "text/event-stream", anthropicUpToBlock)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tc.serve(w) }))
			t.Cleanup(srv.Close)
			res, err := completeAt(t, tc.wire, srv.URL, tc.stream)
			if err == nil {
				t.Fatal("the call succeeded")
			}
			if errors.Is(err, egress.ErrUnprocessed) {
				t.Fatalf("a failure after an answer is marked unprocessed: %v", err)
			}
			if got := res != nil && res.Usage != nil; got != tc.reported {
				t.Fatalf("usage beside the error = %+v, want reported %v", res, tc.reported)
			}
		})
	}
}

// A client with RetryOnlyUnbilled (the host's) makes no attempt after one
// that may have been billed, so a call never spends past the one attempt its
// caller reserved for: an answer dropped mid-body, a 5xx and a connection
// dropped after the request went out each end the call, and the failure is
// not marked unprocessed. A refusal (a 429) is still retried. Without the
// flag every one of them is retried and the second attempt answers.
func TestARetryOnlyUnbilledClientMakesNoAttemptAfterABillableOne(t *testing.T) {
	const anthropicAnswer = `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`
	dropAfterRequest := func(w http.ResponseWriter) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}
	cases := []struct {
		name     string
		stream   bool
		first    func(w http.ResponseWriter)
		billable bool
	}{
		{"an answer dropped mid-body", false, func(w http.ResponseWriter) { cutOff(w, "application/json", `{"id":"msg_1",`) }, true},
		{"a 500", false, func(w http.ResponseWriter) { refuse(w, http.StatusInternalServerError) }, true},
		{"a 529 before a stream", true, func(w http.ResponseWriter) { refuse(w, 529) }, true},
		{"a connection dropped after the request", false, dropAfterRequest, true},
		{"a 429", false, func(w http.ResponseWriter) { refuse(w, http.StatusTooManyRequests) }, false},
		{"a 429 before a stream", true, func(w http.ResponseWriter) { refuse(w, http.StatusTooManyRequests) }, false},
	}
	for _, tc := range cases {
		for _, only := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s retryOnlyUnbilled=%v", tc.name, only), func(t *testing.T) {
				t.Parallel()
				var attempts atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					switch {
					case attempts.Add(1) == 1:
						tc.first(w)
					case tc.stream:
						answer(w, "text/event-stream", anthropicEventStream(anthropicStreamOK...))
					default:
						answer(w, "application/json", anthropicAnswer)
					}
				}))
				t.Cleanup(srv.Close)
				client, err := New(WireAnthropic, Config{BaseURL: srv.URL, APIKey: "sk-billing-fixture", RetryOnlyUnbilled: only})
				if err != nil {
					t.Fatal(err)
				}
				var onDelta func(Delta)
				if tc.stream {
					onDelta = func(Delta) {}
				}
				res, err := client.Complete(context.Background(), Request{
					Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
				}, onDelta)
				if only && tc.billable {
					if err == nil || errors.Is(err, egress.ErrUnprocessed) {
						t.Fatalf("err = %v, want a failure the endpoint may have billed", err)
					}
					if n := attempts.Load(); n != 1 {
						t.Fatalf("%d attempts, want the billable one alone", n)
					}
					return
				}
				if err != nil {
					t.Fatalf("complete: %v", err)
				}
				if n := attempts.Load(); n != 2 || res.Content != "ok" || res.Usage == nil {
					t.Fatalf("content %q, usage %+v after %d attempts, want the second attempt's answer", res.Content, res.Usage, n)
				}
			})
		}
	}
}
