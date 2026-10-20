package engine

import (
	"context"
	"math"
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
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
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
