package engine

// The allowance ledger against real HTTP answers (decision 0235): what a
// failed operator-paid call settles to, the title and compaction calls on an
// operator-served row, reservations racing for the last of the allowance, one
// repository's ledger against another's, and the embeddings stamp against a
// row that shares its name.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// lastReservation is the newest ledger row.
func lastReservation(t *testing.T, ds *dataset) struct {
	slot             string
	ceiling, settled float64
} {
	t.Helper()
	ledger := operatorLedger(t, ds)
	if len(ledger) == 0 {
		t.Fatal("the ledger is empty")
	}
	return ledger[len(ledger)-1]
}

// centsNear reports whether two cent amounts agree past a float sum's last bits.
func centsNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// An operator-paid call settles to the usage the endpoint reported, even when the
// call then fails; to nothing when the endpoint cannot have acted on it (a
// refusal, nothing listening); and at its ceiling when it may have done the
// work and reported nothing or part of its usage (a 5xx, an answer cut off
// midway, an answer that does not decode, a success without usage or with
// its prompt count alone). The same holds for the operator's embeddings.
func TestOperatorGateSettlesAFailedCallByWhatTheEndpointCanHaveBilled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithOperatorDefaults(&operatordefaults.Config{Budget: operatordefaults.Budget{CentsPerDay: 1000}}, "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	cutOff := func(w http.ResponseWriter, contentType, partial string) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s", contentType, len(partial)+1000, partial)
		_ = buf.Flush()
		_ = conn.Close()
	}
	reply := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}
	const usage = `{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`
	const choice = `"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]`
	// 3 prompt tokens at 1 USD and 2 completion tokens at 5 USD per million,
	// in cents.
	const usageCents = (3*1.0 + 2*5.0) / 1e6 * 100

	type settles int
	const (
		toNothing settles = iota
		toCeiling
		toUsage
	)
	llmCases := []struct {
		name   string
		stream bool
		serve  http.HandlerFunc
		want   settles
	}{
		{"a 401", false, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
		}, toNothing},
		{"nothing listening", false, nil, toNothing},
		{"a 500", false, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"down"}}`, http.StatusInternalServerError)
		}, toCeiling},
		{"an answer cut off midway", false, func(w http.ResponseWriter, _ *http.Request) {
			cutOff(w, "application/json", `{"id":"c",`)
		}, toCeiling},
		{"a streamed answer cut off midway", true, func(w http.ResponseWriter, _ *http.Request) {
			cutOff(w, "text/event-stream", "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"o\"}}]}\n\n")
		}, toCeiling},
		{"an answer that does not decode", false, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, `{"choices":[{"message":`)
		}, toCeiling},
		{"an answer without usage", false, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, `{"id":"c",`+choice+`}`)
		}, toCeiling},
		{"an answer with its prompt count alone", false, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, `{"id":"c",`+choice+`,"usage":{"prompt_tokens":3}}`)
		}, toCeiling},
		{"an answer with no choices and its usage", false, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, `{"id":"c","choices":[],"usage":`+usage+`}`)
		}, toUsage},
		{"a streamed answer cut off after its usage", true, func(w http.ResponseWriter, _ *http.Request) {
			cutOff(w, "text/event-stream",
				"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"+
					"data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":"+usage+"}\n\n")
		}, toUsage},
		{"an answer with its usage", false, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, `{"id":"c",`+choice+`,"usage":`+usage+`}`)
		}, toUsage},
	}
	check := func(t *testing.T, want settles, cents float64) {
		t.Helper()
		row := lastReservation(t, ds)
		switch want {
		case toNothing:
			if row.settled != 0 {
				t.Fatalf("settled %v of a %v ceiling, want nothing", row.settled, row.ceiling)
			}
		case toCeiling:
			if row.settled != row.ceiling {
				t.Fatalf("settled %v, want the ceiling %v", row.settled, row.ceiling)
			}
		case toUsage:
			if !centsNear(row.settled, cents) || row.settled >= row.ceiling {
				t.Fatalf("settled %v of a %v ceiling, want the reported %v", row.settled, row.ceiling, cents)
			}
		}
	}
	for _, tc := range llmCases {
		t.Run("llm "+tc.name, func(t *testing.T) {
			base := closedURL
			if tc.serve != nil {
				srv := httptest.NewServer(tc.serve)
				t.Cleanup(srv.Close)
				base = srv.URL
			}
			pc := operatorProviderConfig(&operatordefaults.LLM{
				ID: "chatllm", Wire: llm.WireOpenAI, BaseURL: base, APIKey: operatordefaults.NewSecret(operatorTestKey),
				Models:          []operatordefaults.Model{{Model: "chat", InputPer1M: "1", OutputPer1M: "5"}},
				MaxOutputTokens: 64,
			})
			inner, err := llm.New(pc.wire, pc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			var onDelta func(llm.Delta)
			if tc.stream {
				onDelta = func(llm.Delta) {}
			}
			before := len(operatorLedger(t, ds))
			_, _ = newOperatorLLMClient(ds, pc, inner).Complete(ctx, llm.Request{
				Model: "chat", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
			}, onDelta)
			if n := len(operatorLedger(t, ds)); n != before+1 {
				t.Fatalf("the call wrote %d reservations, want 1", n-before)
			}
			check(t, tc.want, usageCents)
		})
	}

	vector := func(width int) string { return strings.TrimSuffix(strings.Repeat("0.1,", width), ",") }
	// 7 tokens at 0.02 USD per million, in cents.
	const embedCents = 7 * 0.02 / 1e6 * 100
	embedCases := []struct {
		name  string
		serve http.HandlerFunc
		want  settles
	}{
		{"a 400", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"bad input"}}`, http.StatusBadRequest)
		}, toNothing},
		{"an answer cut off midway", func(w http.ResponseWriter, _ *http.Request) {
			cutOff(w, "application/json", `{"data":[{"index":0,`)
		}, toCeiling},
		{"an answer of the wrong width and its usage", func(w http.ResponseWriter, _ *http.Request) {
			reply(w, `{"data":[{"index":0,"embedding":[`+vector(8)+`]}],"usage":{"prompt_tokens":7,"total_tokens":7}}`)
		}, toUsage},
	}
	for _, tc := range embedCases {
		t.Run("embeddings "+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.serve)
			t.Cleanup(srv.Close)
			p, err := ds.openOperatorEmbedProvider(&operatordefaults.LLM{
				ID: "vectors", Wire: llm.WireOpenAI, BaseURL: srv.URL, APIKey: operatordefaults.NewSecret("operator-embed-key-fixture"),
				Embeddings: &operatordefaults.Embeddings{Model: "text-embedding-3-small", Per1M: "0.02"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Embed(ctx, []string{"hi"}); err == nil {
				t.Fatal("the request succeeded")
			}
			check(t, tc.want, embedCents)
		})
	}
}

// An operator-paid call reserves one attempt's ceiling, so its client makes no
// attempt after one that may have been billed. An anthropic answer dropped
// mid-body, which the endpoint may have finished and billed, ends the call
// with its ceiling settled, and the retry that would have answered never
// leaves. A refusal (a 429) is still retried, and that call settles to the
// usage the answering attempt reported.
func TestOperatorCallMakesNoAttemptAfterOneThatMayHaveBeenBilled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithOperatorDefaults(&operatordefaults.Config{Budget: operatordefaults.Budget{CentsPerDay: 1000}}, "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now

	const answer = `{"id":"msg_1","type":"message","role":"assistant","model":"chat","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
	// 3 input tokens at 1 USD and 2 output tokens at 5 USD per million, in
	// cents.
	const usageCents = (3*1.0 + 2*5.0) / 1e6 * 100
	cases := []struct {
		name     string
		first    func(w http.ResponseWriter)
		attempts int32
		failed   bool
	}{
		{"an answer dropped mid-body", func(w http.ResponseWriter) {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_, _ = fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nRetry-After-Ms: 0\r\nContent-Length: 1000\r\n\r\n{\"id\":")
			_ = buf.Flush()
			_ = conn.Close()
		}, 1, true},
		{"a 429", func(w http.ResponseWriter) {
			w.Header().Set("Retry-After-Ms", "0")
			http.Error(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, http.StatusTooManyRequests)
		}, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					tc.first(w)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, answer)
			}))
			t.Cleanup(srv.Close)
			pc := operatorProviderConfig(&operatordefaults.LLM{
				ID: "claudellm", Wire: llm.WireAnthropic, BaseURL: srv.URL, APIKey: operatordefaults.NewSecret(operatorTestKey),
				Models:          []operatordefaults.Model{{Model: "chat", InputPer1M: "1", OutputPer1M: "5"}},
				MaxOutputTokens: 64,
			})
			inner, err := llm.New(pc.wire, pc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			before := len(operatorLedger(t, ds))
			_, err = newOperatorLLMClient(ds, pc, inner).Complete(ctx, llm.Request{
				Model: "chat", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
			}, nil)
			if failed := err != nil; failed != tc.failed {
				t.Fatalf("err = %v, want failed %v", err, tc.failed)
			}
			if n := attempts.Load(); n != tc.attempts {
				t.Fatalf("the endpoint saw %d attempts, want %d", n, tc.attempts)
			}
			if n := len(operatorLedger(t, ds)); n != before+1 {
				t.Fatalf("the call wrote %d reservations, want 1", n-before)
			}
			row := lastReservation(t, ds)
			if tc.failed && row.settled != row.ceiling {
				t.Fatalf("settled %v, want the ceiling %v the dropped attempt may have cost", row.settled, row.ceiling)
			}
			if !tc.failed && (!centsNear(row.settled, usageCents) || row.settled >= row.ceiling) {
				t.Fatalf("settled %v of a %v ceiling, want the answering attempt's %v", row.settled, row.ceiling, usageCents)
			}
		})
	}
}

// waitLedger polls until the ledger holds n rows, every one settled: a title
// is written by a background task after the turn returns.
func waitLedger(t *testing.T, ds *dataset, n int) []struct {
	slot             string
	ceiling, settled float64
} {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ledger := operatorLedger(t, ds)
		settled := len(ledger) == n
		for _, row := range ledger {
			settled = settled && row.settled >= 0
		}
		if settled {
			return ledger
		}
		if time.Now().After(deadline) {
			t.Fatalf("the ledger never held %d settled rows: %+v", n, ledger)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A chat's title call on an operator-served row reserves against the
// allowance ledger like the turn before it, and a title answer that reports no usage
// keeps its ceiling rather than settling to nothing.
func TestOperatorTitleCallGoesThroughTheGateAndKeepsItsCeilingWithoutUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	ds.mu.Lock()
	ds.skipThreadTitles = false
	ds.mu.Unlock()
	op.script("chat", fakeTurn{content: "hello from the operator"}, fakeTurn{content: "Greeting", noUsage: true})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	ledger := waitLedger(t, ds, 2)
	if reqs := op.requestsOf("chat"); len(reqs) != 2 || reqs[1]["tools"] != nil {
		t.Fatalf("the operator's endpoint saw %d requests, want the turn and a title without tools", len(reqs))
	}
	turn, title := ledger[0], ledger[1]
	if turn.slot != substrate.SlotLLM || turn.settled <= 0 || turn.settled >= turn.ceiling {
		t.Fatalf("the turn's reservation = %+v, want one settled to its usage", turn)
	}
	if title.slot != substrate.SlotLLM || title.settled != title.ceiling {
		t.Fatalf("the title's reservation = %+v, want its ceiling kept", title)
	}
}

// A compaction's summarizer call on an operator-served row reserves against
// the allowance ledger, and a summary answer that reports no usage keeps its ceiling
// while the summary still lands.
func TestOperatorCompactionCallGoesThroughTheGateAndKeepsItsCeilingWithoutUsage(t *testing.T) {
	t.Parallel()
	ds, _, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config {
		c := operatorChatConfig(u, "", 1000)
		c.LLM["compactllm"] = &operatordefaults.LLM{
			ID: "compactllm", Wire: llm.WireOpenAI, BaseURL: u, APIKey: operatordefaults.NewSecret(operatorTestKey),
			Models:          []operatordefaults.Model{{Model: "compact", InputPer1M: "1", OutputPer1M: "5", ContextWindow: 1000}},
			MaxOutputTokens: 4096,
		}
		return c
	})
	mustPatchInternal(t, ds, typeProvider, "compactllm", map[string]any{"apiKey": nil})
	op.script("compact", fakeTurn{content: "r1" + hundred, promptTokens: 900})
	res, _ := chat(t, ds, "", "q1"+hundred)
	op.script("compact", fakeTurn{content: "r2" + hundred, promptTokens: 900}, fakeTurn{content: "SUMMARY ONE", noUsage: true})
	_, events := chat(t, ds, res.Thread, "q2"+hundred)
	compacted := false
	for _, ev := range events {
		compacted = compacted || ev.Kind == substrate.AgentEventCompacted
	}
	reqs := op.requestsOf("compact")
	if !compacted || len(reqs) != 3 || !isSummarizerRequest(reqs[2]) {
		t.Fatalf("compacted %v after %d requests to the operator; want the summary as the third", compacted, len(reqs))
	}
	if n := len(summaryRows(compactionRows(t, ds, res.Thread))); n != 1 {
		t.Fatalf("%d summary rows, want 1", n)
	}
	ledger := operatorLedger(t, ds)
	if len(ledger) != 3 {
		t.Fatalf("ledger = %+v, want the two turns and the summary", ledger)
	}
	for _, turn := range ledger[:2] {
		if turn.settled <= 0 || turn.settled >= turn.ceiling {
			t.Fatalf("a turn's reservation = %+v, want one settled to its usage", turn)
		}
	}
	if s := ledger[2]; s.settled != s.ceiling {
		t.Fatalf("the summary's reservation = %+v, want its ceiling kept", s)
	}
}

// Calls racing for the last of the day's allowance are admitted one at a
// time: of eight that each need the room left for one, exactly one is. The
// race runs ten times, each on a fresh UTC day, so a lost lock shows.
func TestOperatorLedgerAdmitsOneOfCallsRacingForTheLastRoom(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithOperatorDefaults(&operatordefaults.Config{Budget: operatordefaults.Budget{CentsPerDay: 10}}, "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now
	const racers = 8
	for round := range 10 {
		clock.Set(time.Date(2026, 11, 6+round, 12, 0, 0, 0, time.UTC))
		if _, err := ds.reserveOperator(ctx, substrate.SlotLLM, 6); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var admitted, refused atomic.Int32
		var wg sync.WaitGroup
		for range racers {
			wg.Go(func() {
				<-start
				_, err := ds.reserveOperator(ctx, substrate.SlotLLM, 4)
				switch {
				case err == nil:
					admitted.Add(1)
				case errors.Is(err, substrate.ErrAllowance):
					refused.Add(1)
				default:
					t.Errorf("reserve: %v", err)
				}
			})
		}
		close(start)
		wg.Wait()
		if admitted.Load() != 1 || refused.Load() != racers-1 {
			t.Fatalf("round %d: %d admitted and %d refused, want 1 and %d", round, admitted.Load(), refused.Load(), racers-1)
		}
		if used, _, _, err := ds.operatorSpendToday(ctx); err != nil || used != 10 {
			t.Fatalf("round %d: used = %v, %v; want the whole 10", round, used, err)
		}
	}
}

// One repository's operator-paid spend neither counts against another's
// allowance nor shows in its operator defaults read.
func TestOperatorLedgerIsPerRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithOperatorDefaults(&operatordefaults.Config{Budget: operatordefaults.Budget{CentsPerDay: 10}}, "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now
	other := secondRepository(t, ds)
	if _, err := ds.reserveOperator(ctx, substrate.SlotLLM, 9); err != nil {
		t.Fatal(err)
	}
	if used, by, _, err := other.operatorSpendToday(ctx); err != nil || used != 0 || len(by) != 0 {
		t.Fatalf("the other repository's day = %v %v, %v; want nothing", used, by, err)
	}
	if _, err := other.reserveOperator(ctx, substrate.SlotLLM, 9); err != nil {
		t.Fatalf("the other repository's reservation = %v, want its own whole allowance", err)
	}
	got, err := other.OperatorDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Operator == nil || got.Operator.Allowance == nil || got.Operator.Allowance.UsedCents != 9 {
		t.Fatalf("the other repository's operator defaults read = %+v, want 9 cents used", got.Operator)
	}
	if used, _, _, err := ds.operatorSpendToday(ctx); err != nil || used != 9 {
		t.Fatalf("the first repository's day = %v, %v; want its own 9", used, err)
	}
	if rows := operatorLedger(t, other); len(rows) != 1 {
		t.Fatalf("the other repository reads %d ledger rows, want its own one", len(rows))
	}
}

// The operator's embeddings stamp is no row id, so a row named `operator`
// claiming the same model is another pair: moving from the operator's entry
// to that row, or
// back, leaves the old vectors stale and buys them again.
func TestOperatorEmbeddingsStampNeverMatchesARowNamedOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if vocabulary.ValidID(operatorEmbedProviderID) {
		t.Fatalf("the operator's stamp %q is a valid record id", operatorEmbedProviderID)
	}
	opEmb, rowEmb := newOperatorEmbedFake(t), newOperatorEmbedFake(t)
	ds := openInternalDataset(t, WithOperatorDefaults(operatorChatConfig("https://llm.example.com/v1", opEmb.srv.URL, 1000), "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now
	if err := enginetest.InstallShelf(ctx, ds); err != nil {
		t.Fatal(err)
	}
	book, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "shelf.test.dev/shelf/book", Properties: map[string]any{"title": "Atlas", "description": "saxophone almanac chapter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stamp := func() string {
		t.Helper()
		var provider, model string
		if err := ds.db.QueryRow(`SELECT provider, model FROM embeddings WHERE record_id = $1`, book.ID).Scan(&provider, &model); err != nil {
			t.Fatal(err)
		}
		if model != "text-embedding-3-small" {
			t.Fatalf("vector model %q", model)
		}
		return provider
	}
	drain := func() {
		t.Helper()
		if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 1 {
			t.Fatalf("drain = %d, %v; want 1", n, err)
		}
	}
	reconcile := func() int {
		t.Helper()
		n, err := ds.reconcileEmbeddings(ctx, ds.db, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	drain()
	if p := stamp(); p != operatorEmbedProviderID {
		t.Fatalf("the operator's vector is stamped %q", p)
	}

	// The person's own row named `operator` takes the job on the same model.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "operator", Properties: map[string]any{
		"wire": "openai", "baseURL": rowEmb.srv.URL, "apiKey": "row-embed-key-fixture", "embedModel": "text-embedding-3-small",
	}}); err != nil {
		t.Fatal(err)
	}
	if n := reconcile(); n != 1 {
		t.Fatalf("after the row took the job the reconcile queued %d, want the operator's vector", n)
	}
	drain()
	if p := stamp(); p != "operator" || len(rowEmb.authsSeen()) != 1 {
		t.Fatalf("the vector is stamped %q after %d requests to the row's endpoint; want the row's own", p, len(rowEmb.authsSeen()))
	}

	// Back to the operator's entry: the row's vectors are not the operator's.
	mustPatchInternal(t, ds, typeProvider, "operator", map[string]any{"embedModel": nil})
	if n := reconcile(); n != 1 {
		t.Fatalf("after the row let go the reconcile queued %d, want the row's vector", n)
	}
	drain()
	if p := stamp(); p != operatorEmbedProviderID || len(opEmb.authsSeen()) != 2 {
		t.Fatalf("the vector is stamped %q after %d requests to the operator's endpoint; want the operator's again", p, len(opEmb.authsSeen()))
	}
}
