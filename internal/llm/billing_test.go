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

// A caller paying per call (the allowance ledger,
// internal/engine/operatorspend.go) releases a reservation only on what these
// tests hold: a failure is marked egress.ErrUnprocessed only when the
// endpoint cannot have acted on the request, a Result's Usage is Partial
// unless the reply reported it whole, and a RetryOnlyUnbilled client makes no
// attempt after one that may have been billed.

// usageReport is how much of its usage a reply reported.
type usageReport int

const (
	reportedNone usageReport = iota
	reportedPart
	reportedWhole
)

// reportOf reads a Usage as a usageReport.
func reportOf(u *Usage) usageReport {
	switch {
	case u == nil:
		return reportedNone
	case u.Partial:
		return reportedPart
	}
	return reportedWhole
}

func (r usageReport) String() string {
	return [...]string{"none", "part", "whole"}[r]
}

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
// that reports one of its two counts returns a Partial Usage holding that
// count; a reply that reports both returns them whole.
func TestUsageIsPartialUnlessTheReplyReportedItWhole(t *testing.T) {
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
		want        usageReport
		// prompt and completion are the counts a reported Usage holds.
		prompt, completion int
	}{
		{"openai one-shot without usage", WireOpenAI, false, "application/json", openaiOneShot(""), reportedNone, 0, 0},
		{"openai one-shot with an empty usage", WireOpenAI, false, "application/json", openaiOneShot(`{}`), reportedNone, 0, 0},
		{"openai one-shot without completion_tokens", WireOpenAI, false, "application/json", openaiOneShot(`{"prompt_tokens":3}`), reportedPart, 3, 0},
		{"openai one-shot without prompt_tokens", WireOpenAI, false, "application/json", openaiOneShot(`{"completion_tokens":2}`), reportedPart, 0, 2},
		{"openai one-shot with usage", WireOpenAI, false, "application/json", openaiOneShot(openaiUsageJSON), reportedWhole, 3, 2},
		{"openai stream without usage", WireOpenAI, true, "text/event-stream", openaiStream(""), reportedNone, 0, 0},
		{"openai stream without completion_tokens", WireOpenAI, true, "text/event-stream", openaiStream(`{"prompt_tokens":3}`), reportedPart, 3, 0},
		{"openai stream with usage", WireOpenAI, true, "text/event-stream", openaiStream(openaiUsageJSON), reportedWhole, 3, 2},
		{"anthropic one-shot without usage", WireAnthropic, false, "application/json", anthropicOneShot(""), reportedNone, 0, 0},
		{"anthropic one-shot without output_tokens", WireAnthropic, false, "application/json", anthropicOneShot(`{"input_tokens":4}`), reportedPart, 4, 0},
		{"anthropic one-shot without input_tokens", WireAnthropic, false, "application/json", anthropicOneShot(`{"output_tokens":2}`), reportedPart, 0, 2},
		{"anthropic one-shot with usage", WireAnthropic, false, "application/json", anthropicOneShot(`{"input_tokens":4,"output_tokens":2}`), reportedWhole, 4, 2},
		{"anthropic stream without a final output count", WireAnthropic, true, "text/event-stream", anthropicEventStream(anthropicNoDelta...), reportedPart, 4, 0},
		{"anthropic stream with usage", WireAnthropic, true, "text/event-stream", anthropicEventStream(anthropicStreamOK...), reportedWhole, 1, 1},
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
			if got := reportOf(res.Usage); got != tc.want {
				t.Fatalf("usage = %+v, reported %v, want %v", res.Usage, got, tc.want)
			}
			if u := res.Usage; u != nil && (u.PromptTokens != tc.prompt || u.CompletionTokens != tc.completion) {
				t.Fatalf("usage = %+v, want %d prompt and %d completion tokens", u, tc.prompt, tc.completion)
			}
		})
	}
}

// A call that fails after the endpoint reported its usage returns that usage
// beside the error: a reply with no choices, a stream cut off after its
// usage. A stream cut off before its final count returns its input count,
// Partial.
func TestAFailureAfterTheUsageKeepsIt(t *testing.T) {
	anthropicUpToDelta := anthropicEventStream(anthropicStreamOK[:5]...)
	anthropicUpToBlock := anthropicEventStream(anthropicStreamOK[:3]...)
	openaiUpToUsage := strings.TrimSuffix(openaiStream(openaiUsageJSON), "data: [DONE]\n\n")
	cases := []struct {
		name   string
		wire   Wire
		stream bool
		serve  func(w http.ResponseWriter)
		want   usageReport
	}{
		{"openai one-shot with no choices", WireOpenAI, false, func(w http.ResponseWriter) {
			answer(w, "application/json", `{"id":"c","object":"chat.completion","choices":[],"usage":`+openaiUsageJSON+`}`)
		}, reportedWhole},
		{"openai stream cut off after its usage", WireOpenAI, true, func(w http.ResponseWriter) {
			cutOff(w, "text/event-stream", openaiUpToUsage)
		}, reportedWhole},
		{"anthropic stream cut off after its final count", WireAnthropic, true, func(w http.ResponseWriter) {
			cutOff(w, "text/event-stream", anthropicUpToDelta)
		}, reportedWhole},
		{"anthropic stream cut off before its final count", WireAnthropic, true, func(w http.ResponseWriter) {
			cutOff(w, "text/event-stream", anthropicUpToBlock)
		}, reportedPart},
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
			var usage *Usage
			if res != nil {
				usage = res.Usage
			}
			if got := reportOf(usage); got != tc.want {
				t.Fatalf("usage beside the error = %+v, reported %v, want %v", usage, got, tc.want)
			}
		})
	}
}

// A client with RetryOnlyUnbilled (the operator's) makes no attempt after one
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
				if n := attempts.Load(); n != 2 || res.Content != "ok" || reportOf(res.Usage) != reportedWhole {
					t.Fatalf("content %q, usage %+v after %d attempts, want the second attempt's answer", res.Content, res.Usage, n)
				}
			})
		}
	}
}

// Every attempt of one anthropic call that reported usage counts toward the
// call's. On the person's own key a one-shot answer that broke after its
// usage object arrived is retried, and the usage of the attempt that answered
// adds to it, Partial when either report was; an attempt that reported nothing
// adds nothing and leaves the sum whole. A dropped body whose report is
// ambiguous reports nothing. A RetryOnlyUnbilled client (the operator's)
// still makes the one billable attempt, and returns its usage beside the
// failure, or none, so the ledger keeps its ceiling.
func TestEveryAttemptThatReportedUsageCountsTowardTheCall(t *testing.T) {
	const head = `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"`
	body := func(usage string) string {
		if usage == "" {
			return head
		}
		return head + `,"usage":` + usage
	}
	answerWith := func(usage string) func(w http.ResponseWriter) {
		return func(w http.ResponseWriter) { answer(w, "application/json", body(usage)+"}") }
	}
	// droppedAfter answers a body that breaks after usage, or before any
	// usage when usage is empty.
	droppedAfter := func(usage string) func(w http.ResponseWriter) {
		return func(w http.ResponseWriter) { cutOff(w, "application/json", body(usage)) }
	}
	refused := func(w http.ResponseWriter) { refuse(w, http.StatusInternalServerError) }
	const (
		// first is 7 prompt tokens (1 read, 2 written) and 2 completion
		// tokens; second is 14 (3 read, 4 written) and 3.
		first  = `{"input_tokens":4,"cache_read_input_tokens":1,"cache_creation_input_tokens":2,"output_tokens":2}`
		second = `{"input_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":3}`
	)
	firstOnly := &Usage{PromptTokens: 7, CompletionTokens: 2, CacheReadTokens: 1, CacheWriteTokens: 2}
	secondOnly := &Usage{PromptTokens: 14, CompletionTokens: 3, CacheReadTokens: 3, CacheWriteTokens: 4}
	cases := []struct {
		name     string
		operator bool
		serve    []func(w http.ResponseWriter)
		failed   bool
		want     *Usage
	}{
		{
			"an answer dropped after its usage, then an answer", false,
			[]func(http.ResponseWriter){droppedAfter(first), answerWith(second)},
			false, &Usage{PromptTokens: 21, CompletionTokens: 5, CacheReadTokens: 4, CacheWriteTokens: 6, Attempts: 2},
		},
		{
			"two answers dropped after their usage, then an answer", false,
			[]func(http.ResponseWriter){droppedAfter(first), droppedAfter(first), answerWith(second)},
			false, &Usage{PromptTokens: 28, CompletionTokens: 7, CacheReadTokens: 5, CacheWriteTokens: 8, Attempts: 3},
		},
		{
			"an answer dropped before its usage, then an answer", false,
			[]func(http.ResponseWriter){droppedAfter(""), answerWith(second)},
			false, secondOnly,
		},
		{
			"an answer dropped mid-usage, then an answer", false,
			[]func(http.ResponseWriter){func(w http.ResponseWriter) {
				cutOff(w, "application/json", head+`,"usage":{"input_tokens":4,"output_tokens":2`)
			}, answerWith(second)},
			false, secondOnly,
		},
		{
			"an answer dropped after part of its usage, then an answer", false,
			[]func(http.ResponseWriter){droppedAfter(`{"input_tokens":4}`), answerWith(second)},
			false, &Usage{PromptTokens: 18, CompletionTokens: 3, CacheReadTokens: 3, CacheWriteTokens: 4, Partial: true, Attempts: 2},
		},
		{
			"an answer dropped after its usage, then an answer without usage", false,
			[]func(http.ResponseWriter){droppedAfter(first), answerWith("")},
			false, &Usage{PromptTokens: 7, CompletionTokens: 2, CacheReadTokens: 1, CacheWriteTokens: 2, Partial: true, Attempts: 2},
		},
		{
			"an answer dropped after a repeated usage, then an answer", false,
			[]func(http.ResponseWriter){droppedAfter(first + `,"usage":` + first), answerWith(second)},
			false, secondOnly,
		},
		{
			"an answer not served as JSON dropped after its usage, then an answer", false,
			[]func(http.ResponseWriter){func(w http.ResponseWriter) { cutOff(w, "text/plain", body(first)) }, answerWith(second)},
			false, secondOnly,
		},
		{
			"an answer dropped after its usage, then two 500s", false,
			[]func(http.ResponseWriter){droppedAfter(first), refused, refused},
			true, firstOnly,
		},
		{
			"a 500, then an answer", false,
			[]func(http.ResponseWriter){refused, answerWith(second)},
			false, secondOnly,
		},
		{
			"the operator's: an answer dropped after its usage", true,
			[]func(http.ResponseWriter){droppedAfter(first), answerWith(second)},
			true, firstOnly,
		},
		{
			"the operator's: an answer dropped after a repeated usage", true,
			[]func(http.ResponseWriter){droppedAfter(first + `,"usage":` + first), answerWith(second)},
			true, nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := int(attempts.Add(1))
				if n > len(tc.serve) {
					t.Errorf("attempt %d, want %d at most", n, len(tc.serve))
					refuse(w, http.StatusBadRequest)
					return
				}
				tc.serve[n-1](w)
			}))
			t.Cleanup(srv.Close)
			client, err := New(WireAnthropic, Config{BaseURL: srv.URL, APIKey: "sk-billing-fixture", RetryOnlyUnbilled: tc.operator})
			if err != nil {
				t.Fatal(err)
			}
			res, err := client.Complete(context.Background(), Request{
				Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, nil)
			if failed := err != nil; failed != tc.failed {
				t.Fatalf("err = %v, want failed %v", err, tc.failed)
			}
			wantAttempts := len(tc.serve)
			if tc.operator {
				wantAttempts = 1
			}
			if n := int(attempts.Load()); n != wantAttempts {
				t.Fatalf("%d attempts, want %d", n, wantAttempts)
			}
			if !tc.failed && res.Content != "ok" {
				t.Fatalf("content = %q, want the answering attempt's", res.Content)
			}
			var usage *Usage
			if res != nil {
				usage = res.Usage
			}
			if !sameUsage(usage, tc.want) {
				t.Fatalf("usage = %+v, want %+v", usage, tc.want)
			}
		})
	}
}

// A 200 carrying x-should-retry: true is retried before its body is read,
// streamed or one-shot, so the usage it carried is never seen and the call
// counts only the retry's, as docs/agents.md says.
func TestA200ThatAsksForARetryIsRetriedUnread(t *testing.T) {
	const oneShot = `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`
	for _, tc := range []struct {
		stream            bool
		contentType, body string
		want              *Usage
	}{
		{false, "application/json", oneShot, &Usage{PromptTokens: 4, CompletionTokens: 2}},
		{true, "text/event-stream", anthropicEventStream(anthropicStreamOK...), &Usage{PromptTokens: 1, CompletionTokens: 1}},
	} {
		t.Run(fmt.Sprintf("stream=%v", tc.stream), func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("X-Should-Retry", "true")
					w.Header().Set("Retry-After-Ms", "0")
				}
				answer(w, tc.contentType, tc.body)
			}))
			t.Cleanup(srv.Close)
			res, err := completeAt(t, WireAnthropic, srv.URL, tc.stream)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if n := attempts.Load(); n != 2 || res.Content != "ok" || !sameUsage(res.Usage, tc.want) {
				t.Fatalf("content %q, usage %+v after %d attempts, want %+v after 2", res.Content, res.Usage, n, tc.want)
			}
		})
	}
}

// sameUsage reports whether two usages hold the same counts, nil only beside
// nil.
func sameUsage(a, b *Usage) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// A dropped one-shot body counts its usage only where every byte that arrived
// agrees with the whole body's decoder: valid JSON so far, a top-level type
// "message", and one usage object, whole, naming each count once. Anything
// else reports nothing, so the operator's ledger keeps its ceiling and the
// person's tally counts short, never over.
func TestADroppedBodyCountsOnlyAUsageItReportedOnce(t *testing.T) {
	const (
		head  = `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"`
		usage = `{"input_tokens":4,"output_tokens":2}`
		later = `{"input_tokens":40,"output_tokens":20}`
	)
	reported := &Usage{PromptTokens: 4, CompletionTokens: 2}
	cases := []struct {
		name    string
		payload string
		want    *Usage
	}{
		{"cut after its usage", head + `,"usage":` + usage, reported},
		{"cut after a comma", head + `,"usage":` + usage + `,`, reported},
		{"cut inside a later key", head + `,"usage":` + usage + `,"stop_seq`, reported},
		{"cut inside a later string", head + `,"usage":` + usage + `,"stop_sequence":"\u00`, reported},
		{"cut inside a later array", head + `,"usage":` + usage + `,"more":[{"type":"text","text":"he`, reported},
		{"whole, then whitespace", head + `,"usage":` + usage + "}\r\n", reported},
		{"type after its usage", `{"usage":` + usage + `,"type":"message"`, reported},
		{
			"usage with keys no count reads",
			head + `,"usage":{"input_tokens":4,"cache_creation":{"ephemeral_5m_input_tokens":1},"output_tokens":2,"service_tier":"standard"}`,
			reported,
		},

		{"the review's prefix: a second usage and no type", `{"usage":` + usage + `,"usage":` + later, nil},
		{"a second usage", head + `,"usage":` + usage + `,"usage":` + later, nil},
		{"a second usage in capitals", head + `,"usage":` + usage + `,"USAGE":` + later, nil},
		{"a second usage cut off", head + `,"usage":` + usage + `,"usage":{"input_tok`, nil},
		{"a count twice", head + `,"usage":{"input_tokens":4,"input_tokens":40,"output_tokens":2}`, nil},
		{"a count twice in capitals", head + `,"usage":{"input_tokens":4,"output_tokens":2,"Output_Tokens":20}`, nil},
		{"a cache count twice", head + `,"usage":{"input_tokens":4,"cache_read_input_tokens":1,"cache_read_input_tokens":9,"output_tokens":2}`, nil},
		{"a syntax error after usage", head + `,"usage":` + usage + `,"content":[}`, nil},
		{"no comma after usage", head + `,"usage":` + usage + ` "stop_sequence":null`, nil},
		{"a comma before the close", head + `,"usage":` + usage + `,}`, nil},
		{"a bad escape after usage", head + `,"usage":` + usage + `,"stop_sequence":"\x"`, nil},
		{"bytes after the close", head + `,"usage":` + usage + `}]`, nil},
		{"no type", `{"id":"msg_1","usage":` + usage, nil},
		{"an error's type", `{"type":"error","usage":` + usage, nil},
		{"a type that is no string", `{"type":1,"usage":` + usage, nil},
		{"a second type", head + `,"usage":` + usage + `,"type":"error"`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := anthropicDroppedUsage([]byte(tc.payload)); !sameUsage(got, tc.want) {
				t.Fatalf("usage = %+v, want %+v", got, tc.want)
			}
		})
	}
}
