package engine

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The cost stamp prices cached prompt tokens at the provider row's cache
// rates, or at the default ratios of its input rate when the row states none
// (decision 0181), and the thread records the cached counts beside
// promptTokens.

// cachedTurn reports 100 prompt tokens, 60 of them read from the cache and
// 20 written to it, and the fake's fixed 5 completion tokens.
func cachedTurn(content string, calls ...fakeCall) fakeTurn {
	return fakeTurn{content: content, calls: calls, promptTokens: 100, cacheReadTokens: 60, cacheWriteTokens: 20}
}

func putPricedProvider(t *testing.T, ds *dataset, fake *fakeLLM, id string, pricing map[string]any) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: id,
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-" + id,
			"pricing": []any{pricing},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row %s: %v", id, err)
	}
}

func TestAThreadRecordsCacheCountsAndPricesThemAtTheRowsRates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	// namer's row states both cache rates; scribe's states none.
	putPricedProvider(t, ds, fake, "namellm", map[string]any{
		"model": "name", "inputPer1M": "1", "outputPer1M": "5", "cacheReadPer1M": "0.2", "cacheWritePer1M": "2",
	})
	putPricedProvider(t, ds, fake, "subllm", map[string]any{"model": "sub", "inputPer1M": "1", "outputPer1M": "5"})
	fake.script("name", cachedTurn("named"))
	fake.script("sub", cachedTurn("scribed"))

	for _, tc := range []struct {
		agent string
		want  float64
	}{
		{namerAgent, (20*1 + 60*0.2 + 20*2 + 5*5) / 1e6},
		{crewPackage + "/scribe", (20*1 + 60*0.1 + 20*1.25 + 5*5) / 1e6},
	} {
		res, err := ds.CallAgent(ctx, tc.agent, "go")
		if err != nil {
			t.Fatalf("call %s: %v", tc.agent, err)
		}
		th := getThread(t, ds, res.Thread).Properties
		if intProp(th, "promptTokens") != 100 || intProp(th, "cacheReadTokens") != 60 ||
			intProp(th, "cacheWriteTokens") != 20 || intProp(th, "totalTokens") != 105 {
			t.Fatalf("%s thread tallies = %+v", tc.agent, th)
		}
		if got := anyFloat2(th["costUSD"]); math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("%s thread costUSD = %v, want %v", tc.agent, got, tc.want)
		}
		if math.Abs(res.CostUSD-tc.want) > 1e-12 {
			t.Fatalf("%s result costUSD = %v, want %v", tc.agent, res.CostUSD, tc.want)
		}
	}
}

func TestARootThreadRollsUpItsSubAgentsCacheCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("root",
		cachedTurn("", fakeCall{"scribe", `{"input":"summarize"}`}),
		fakeTurn{content: "done", promptTokens: 100, cacheReadTokens: 90},
	)
	fake.script("sub", cachedTurn("the summary"))
	res, err := ds.CallAgent(ctx, crewPackage+"/classifier", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	root := getThread(t, ds, res.Thread).Properties
	if intProp(root, "promptTokens") != 300 || intProp(root, "cacheReadTokens") != 60+90+60 || intProp(root, "cacheWriteTokens") != 40 {
		t.Fatalf("root thread tallies = %+v", root)
	}
	children := agentThreadsOf(t, ds, "scribe")
	if len(children) != 1 {
		t.Fatalf("scribe threads: %d", len(children))
	}
	if child := children[0]; intProp(child, "cacheReadTokens") != 60 || intProp(child, "cacheWriteTokens") != 20 {
		t.Fatalf("child thread tallies = %+v", child)
	}
	// Every row is priced 1 and 5 with the default ratios.
	want := ((20*1+60*0.1+20*1.25)*2 + (10*1 + 90*0.1) + 3*5*5) / 1e6
	if got := anyFloat2(root["costUSD"]); math.Abs(got-want) > 1e-12 {
		t.Fatalf("root costUSD = %v, want %v", got, want)
	}
}

// A reply that reported its prompt count and left its completion count out
// still costs its prompt: the thread records the count it has, prices it,
// and the person's spend cap counts it. The reply's answer lands as usual.
func TestAThreadCountsAReplyThatReportedPartOfItsUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPricedProvider(t, ds, fake, "namellm", map[string]any{"model": "name", "inputPer1M": "1", "outputPer1M": "5"})
	fake.script("name", fakeTurn{content: "named", promptTokens: 100, noCompletionCount: true})
	res, err := ds.CallAgent(ctx, namerAgent, "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	const want = 100 * 1.0 / 1e6
	th := getThread(t, ds, res.Thread).Properties
	if intProp(th, "promptTokens") != 100 || intProp(th, "completionTokens") != 0 {
		t.Fatalf("thread tallies = %+v, want the 100 prompt tokens reported", th)
	}
	if got := anyFloat2(th["costUSD"]); math.Abs(got-want) > 1e-12 {
		t.Fatalf("thread costUSD = %v, want %v", got, want)
	}
	ag, err := ds.registry().ResolveAgent(namerAgent)
	if err != nil {
		t.Fatal(err)
	}
	ds.spend.expire()
	if spent, _, err := ds.spendOf(ctx, ag); err != nil || math.Abs(spent-want) > 1e-12 {
		t.Fatalf("the spend cap reads %v (%v), want %v", spent, err, want)
	}
}

// A reply the client refuses after the endpoint reported its usage (one with
// no choices) fails the run and still costs that usage: the failed thread
// records and prices it, and the person's spend cap counts it.
func TestAFailedThreadCountsTheUsageReportedBesideItsFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPricedProvider(t, ds, fake, "namellm", map[string]any{"model": "name", "inputPer1M": "1", "outputPer1M": "5"})
	fake.script("name", fakeTurn{noChoices: true})
	if _, err := ds.CallAgent(ctx, namerAgent, "go"); err == nil {
		t.Fatal("a reply with no choices succeeded")
	}
	threads := agentThreadsOf(t, ds, "namer")
	if len(threads) != 1 {
		t.Fatalf("namer threads: %d, want 1", len(threads))
	}
	// The fake reports 10 prompt and 5 completion tokens.
	const want = (10*1.0 + 5*5.0) / 1e6
	th := threads[0]
	if intProp(th, "promptTokens") != 10 || intProp(th, "completionTokens") != 5 {
		t.Fatalf("failed thread tallies = %+v, want the usage reported beside the failure", th)
	}
	if got := anyFloat2(th["costUSD"]); math.Abs(got-want) > 1e-12 {
		t.Fatalf("failed thread costUSD = %v, want %v", got, want)
	}
	ag, err := ds.registry().ResolveAgent(namerAgent)
	if err != nil {
		t.Fatal(err)
	}
	ds.spend.expire()
	if spent, _, err := ds.spendOf(ctx, ag); err != nil || math.Abs(spent-want) > 1e-12 {
		t.Fatalf("the spend cap reads %v (%v), want %v", spent, err, want)
	}
}

// On the person's own key an anthropic-wire answer that broke after it
// reported its usage is retried, and the thread and the person's spend cap
// count both attempts: the dropped one billed its tokens as surely as the one
// that answered.
func TestAThreadCountsEveryBilledAttemptOfARetriedCompletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	const head = `{"id":"msg_1","type":"message","role":"assistant","model":"name","content":[{"type":"text","text":"named"}],"stop_reason":"end_turn"`
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			// A 200 whose body breaks after its usage object.
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			partial := head + `,"usage":{"input_tokens":40,"output_tokens":20}`
			_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nRetry-After-Ms: 0\r\nContent-Length: %d\r\n\r\n%s",
				len(partial)+1000, partial)
			_ = buf.Flush()
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, head+`,"usage":{"input_tokens":70,"output_tokens":30}}`)
	}))
	t.Cleanup(srv.Close)
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "namellm",
		Properties: map[string]any{
			"wire": "anthropic", "baseURL": srv.URL, "apiKey": "row-key-namellm",
			"pricing": []any{map[string]any{"model": "name", "inputPer1M": "1", "outputPer1M": "5"}},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row namellm: %v", err)
	}
	res, err := ds.CallAgent(ctx, namerAgent, "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if n := attempts.Load(); n != 2 {
		t.Fatalf("the endpoint saw %d attempts, want the dropped one and its retry", n)
	}
	const want = ((40+70)*1.0 + (20+30)*5.0) / 1e6
	th := getThread(t, ds, res.Thread).Properties
	if intProp(th, "promptTokens") != 110 || intProp(th, "completionTokens") != 50 {
		t.Fatalf("thread tallies = %+v, want both attempts' 110 prompt and 50 completion tokens", th)
	}
	if got := anyFloat2(th["costUSD"]); math.Abs(got-want) > 1e-12 {
		t.Fatalf("thread costUSD = %v, want %v", got, want)
	}
	ag, err := ds.registry().ResolveAgent(namerAgent)
	if err != nil {
		t.Fatal(err)
	}
	ds.spend.expire()
	if spent, _, err := ds.spendOf(ctx, ag); err != nil || math.Abs(spent-want) > 1e-12 {
		t.Fatalf("the spend cap reads %v (%v), want %v", spent, err, want)
	}
}
