package engine

// llm/model records (decision record 0242): the models the seed ships, how a
// row's models are priced, one record per row and model, and what an agent
// on runsOn runs on.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func putModelRecord(t *testing.T, ds *dataset, id string, props map[string]any) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: typeModel, ID: id, Properties: props}); err != nil {
		t.Fatalf("put llm/model %s: %v", id, err)
	}
}

func putModelRecordErr(ds *dataset, id string, props map[string]any) error {
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: typeModel, ID: id, Properties: props})
	return err
}

func keyProviderRow(t *testing.T, ds *dataset, id string) {
	t.Helper()
	mustPatchInternal(t, ds, typeProvider, id, map[string]any{"apiKey": "test-key"})
}

// openModelsDataset is a repository as registration leaves it: the three
// seeded provider rows and the shipped models, which test fixtures leave out
// unless asked.
func openModelsDataset(t *testing.T, opts ...Option) *dataset {
	t.Helper()
	return openCoreDataset(t, append([]Option{WithLLMProviderSeed(true), WithRecordSeed(true)}, opts...)...)
}

func runsOnAgent(entries ...string) *vocabulary.Agent {
	return &vocabulary.Agent{Package: "ag.example.com/ag", Name: "runner", RunsOn: entries}
}

func TestTheSeedShipsTheModelsAndTheRowsCarryNoPrices(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openModelsDataset(t)
	for _, id := range []string{"openai:gpt-5", "anthropic:claude-opus-5-5", "anthropic:claude-haiku-5-5", "gemini:gemini-2.5-flash"} {
		row, err := ds.loadRowDB(ctx, eref{Kind: typeModel, ID: id})
		if err != nil || row == nil || row.DeletedAt != nil {
			t.Fatalf("shipped llm/model %s: %v, %v", id, row, err)
		}
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: "anthropic"})
	if err != nil || row == nil {
		t.Fatalf("seeded anthropic row: %v, %v", row, err)
	}
	if _, ok := row.Props["pricing"]; ok {
		t.Fatalf("the seeded row carries pricing: %v", row.Props["pricing"])
	}

	// A keyed seeded row prices its models from the shipped records, Opus
	// 5.5's stated read rate and Haiku 5.5's narrower window included.
	keyProviderRow(t, ds, "anthropic")
	pc, err := ds.resolveProvider(ctx, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	opus := pc.pricing["claude-opus-5-5"]
	if opus.inPer1M != 4 || opus.outPer1M != 20 || opus.cacheReadPer1M != 0.2 || pc.contextWindow("claude-opus-5-5") != 200000 {
		t.Fatalf("claude-opus-5-5 priced %+v, window %d", opus, pc.contextWindow("claude-opus-5-5"))
	}
	if pc.contextWindow("claude-haiku-5-5") != 100000 {
		t.Fatalf("claude-haiku-5-5 window %d, want 100000", pc.contextWindow("claude-haiku-5-5"))
	}

	// A second open finds every shipped record current and writes nothing.
	before := changelogHead(t, ds)
	if err := ds.ensureShippedRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if after := changelogHead(t, ds); after != before {
		t.Fatalf("an open with every shipped model current wrote %d entries", after-before)
	}
}

func TestARowsOwnEntryWinsAndARecordPricesTheRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "gw", Properties: map[string]any{
		"wire": "openai", "baseURL": "https://gw.example.com/v1", "apiKey": "test-key",
		"pricing": []any{
			// An entry with one rate prices the other at zero, as it always has.
			map[string]any{"model": "x", "inputPer1M": "1"},
			// A later entry for the same model wins.
			map[string]any{"model": "y", "inputPer1M": "1", "outputPer1M": "1"},
			map[string]any{"model": "y", "inputPer1M": "3", "outputPer1M": "3"},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	putModelRecord(t, ds, "gw:x", map[string]any{
		"provider": "gw", "model": "x",
		"pricing": map[string]any{"inputPer1M": "2", "outputPer1M": "2"}, "contextWindow": 1000,
	})
	putModelRecord(t, ds, "gw:z", map[string]any{
		"provider": "gw", "model": "z",
		"pricing": map[string]any{"inputPer1M": "5", "outputPer1M": "6"}, "contextWindow": 2000,
	})
	putModelRecord(t, ds, "gw:w", map[string]any{"provider": "gw", "model": "w", "contextWindow": 3000})

	pc, err := ds.resolveProvider(ctx, "gw")
	if err != nil {
		t.Fatal(err)
	}
	if x := pc.pricing["x"]; x.inPer1M != 1 || x.outPer1M != 0 || pc.contextWindow("x") != 1000 {
		t.Fatalf("x: the row's entry prices it and the record windows it; got %+v, window %d", x, pc.contextWindow("x"))
	}
	if y := pc.pricing["y"]; y.inPer1M != 3 {
		t.Fatalf("y: the later entry wins; got %+v", y)
	}
	if z := pc.pricing["z"]; z.inPer1M != 5 || z.outPer1M != 6 || pc.contextWindow("z") != 2000 {
		t.Fatalf("z: the record prices what no entry names; got %+v, window %d", z, pc.contextWindow("z"))
	}
	if _, priced := pc.pricing["w"]; priced || pc.contextWindow("w") != 3000 {
		t.Fatalf("w: a record without prices windows its model and prices nothing; priced %v, window %d", priced, pc.contextWindow("w"))
	}

	offers, err := ds.ModelOffers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"x", "y", "z"}; !slices.Equal(offers["gw"], want) {
		t.Fatalf("gw offers %v, want %v", offers["gw"], want)
	}

	// A record's pricing states both base rates or it is refused.
	err = putModelRecordErr(ds, "gw:half", map[string]any{
		"provider": "gw", "model": "half",
		"pricing": map[string]any{"inputPer1M": "1"},
	})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "outputPer1M") {
		t.Fatalf("a record with one base rate: %v", err)
	}
}

func TestOneModelRecordPerRowAndModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	for _, id := range []string{"gw", "x", "x:y"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeProvider, ID: id,
			Properties: map[string]any{"wire": "openai", "baseURL": "https://gw.example.com/v1"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	putModelRecord(t, ds, "a", map[string]any{"provider": "gw", "model": "m"})
	err := putModelRecordErr(ds, "b", map[string]any{"provider": "gw", "model": "m"})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), `llm/model "a" already describes "m"`) {
		t.Fatalf("a second record for gw and m: %v", err)
	}
	putModelRecord(t, ds, "b", map[string]any{"provider": "gw", "model": "n"})
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeModel, "b", substrate.PatchInput{Properties: map[string]any{"model": "m"}}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a patch onto a held pair: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeModel, "a", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	mustPatchInternal(t, ds, typeModel, "b", map[string]any{"model": "m"})
	// Writing the deleted record back is a write like any other.
	if err := putModelRecordErr(ds, "a", map[string]any{"provider": "gw", "model": "m"}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("restoring a record onto a held pair: %v", err)
	}

	// The pair is compared as stored: row `x:y` with model `z` and row `x`
	// with model `y:z` are two pairs, whatever an id built from them reads.
	putModelRecord(t, ds, "c", map[string]any{"provider": "x:y", "model": "z"})
	putModelRecord(t, ds, "d", map[string]any{"provider": "x", "model": "y:z"})
}

func TestTheSeedLeavesAShippedModelToTheRecordHoldingItsPair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openModelsDataset(t)
	// Deleted by a shipping hand, so only the pair keeps the seed from
	// writing it back.
	if _, err := ds.Delete(ctx, substrate.ActorSystem, typeModel, "openai:gpt-5", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	putModelRecord(t, ds, "mine", map[string]any{
		"provider": "openai", "model": "gpt-5",
		"pricing": map[string]any{"inputPer1M": "9", "outputPer1M": "9"},
	})
	var shipped vocabulary.ShippedRecord
	for _, rec := range ds.svc.shippedRecords {
		if rec.Kind == typeModel && rec.ID == "openai:gpt-5" {
			shipped = rec
		}
	}
	if shipped.ID == "" {
		t.Fatal("openai:gpt-5 is not shipped")
	}
	owed, err := ds.shippedRecordOwed(ctx, shipped)
	if err != nil || owed {
		t.Fatalf("owed %v, %v: the owner's record holds the pair", owed, err)
	}
	if err := ds.inTx(ctx, substrate.ActorSeed, true, func(t *txn) error { return t.seedShippedRecord(shipped) }); err != nil {
		t.Fatalf("the seed under the lock: %v", err)
	}
	if row, err := ds.loadRowDB(ctx, eref{Kind: typeModel, ID: "openai:gpt-5"}); err != nil || row == nil || row.DeletedAt == nil {
		t.Fatalf("the shipped record came back: %v, %v", row, err)
	}
	keyProviderRow(t, ds, "openai")
	pc, err := ds.resolveProvider(ctx, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if pc.pricing["gpt-5"].inPer1M != 9 {
		t.Fatalf("gpt-5 priced %+v, want the owner's record", pc.pricing["gpt-5"])
	}
}

func TestRunsOnTakesTheFirstModelANewThreadCanRunOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openModelsDataset(t)
	keyProviderRow(t, ds, "openai")
	ag := runsOnAgent("gone", "anthropic:claude-opus-5-5", "openai:gpt-5")
	choice, err := ds.chooseRunsOn(ctx, ag, "")
	if err != nil {
		t.Fatal(err)
	}
	if choice != (agentChoice{provider: "openai", model: "gpt-5", entry: "openai:gpt-5"}) {
		t.Fatalf("chose %+v: a missing record and a keyless row move to the next entry", choice)
	}
	pc, model, err := ds.resolveAgentProvider(ctx, ag, "")
	if err != nil || pc.id != "openai" || model != "gpt-5" || pc.pricing["gpt-5"].inPer1M != 1.25 {
		t.Fatalf("dispatch opened %v %q (%v)", pc, model, err)
	}
	if id, model, err := ds.agentRowAndModel(ctx, ag, ""); err != nil || id != "openai" || model != "gpt-5" {
		t.Fatalf("the hold and the diagnostics read %q %q (%v)", id, model, err)
	}

	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeProvider, "openai", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	_, err = ds.chooseRunsOn(ctx, ag, "")
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("no entry available: %v", err)
	}
	for _, want := range []string{`llm/model "gone" does not resolve`, `llm/provider row "anthropic" has no key`, `llm/provider row "openai" does not resolve`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal names each entry; %q is missing from: %v", want, err)
		}
	}
	if id, _, err := ds.agentRowAndModel(ctx, ag, ""); err != nil || id != "" {
		t.Fatalf("an agent with nothing to run on is nobody's row: %q (%v)", id, err)
	}
}

func TestRunsOnPassesOverAModelTheOperatorDoesNotServe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := &operatordefaults.Config{
		LLM: map[string]*operatordefaults.LLM{"anthropic": {
			ID: "anthropic", Wire: llm.WireAnthropic, BaseURL: "https://api.anthropic.com/",
			APIKey:          operatordefaults.NewSecret("operator-key-fixture"),
			Models:          []operatordefaults.Model{{Model: "claude-sonnet-5-5", InputPer1M: "2", OutputPer1M: "10"}},
			MaxOutputTokens: 8000,
		}},
		Budget: operatordefaults.Budget{CentsPerDay: 200},
	}
	ds := openModelsDataset(t, WithOperatorDefaults(cfg, "Example"))
	ag := runsOnAgent("anthropic:claude-opus-5-5", "anthropic:claude-sonnet-5-5")
	choice, err := ds.chooseRunsOn(ctx, ag, "")
	if err != nil || choice.model != "claude-sonnet-5-5" || choice.provider != "anthropic" {
		t.Fatalf("chose %+v (%v): the operator serves only Sonnet 5.5", choice, err)
	}
	pc, model, err := ds.resolveAgentProvider(ctx, ag, "")
	if err != nil || pc.operator == nil || model != "claude-sonnet-5-5" {
		t.Fatalf("dispatch opened %v %q (%v), want the operator's transport", pc, model, err)
	}
	offers, err := ds.ModelOffers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude-sonnet-5-5"}; !slices.Equal(offers["anthropic"], want) {
		t.Fatalf("the operator's row offers %v, want only its list %v", offers["anthropic"], want)
	}
	only := runsOnAgent("anthropic:claude-opus-5-5")
	if _, err := ds.chooseRunsOn(ctx, only, ""); err == nil || !strings.Contains(err.Error(), "serves only claude-sonnet-5-5") {
		t.Fatalf("an agent on a model the operator does not serve: %v", err)
	}
}

func TestAContinuedThreadStaysOnItsOwnModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openModelsDataset(t)
	keyProviderRow(t, ds, "openai")
	keyProviderRow(t, ds, "anthropic")
	thread := &erow{ID: "t1", Props: map[string]any{
		"provider": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(typeProvider, "openai")},
		"model":    "gpt-5",
	}}
	ag := runsOnAgent("anthropic:claude-opus-5-5", "openai:gpt-5")
	choice, err := ds.continuedRunsOn(ctx, ag, thread)
	if err != nil || choice.provider != "openai" || choice.model != "gpt-5" {
		t.Fatalf("a thread that ran on gpt-5 chose %+v (%v), not its own model", choice, err)
	}
	dropped := runsOnAgent("anthropic:claude-opus-5-5")
	if _, err := ds.continuedRunsOn(ctx, dropped, thread); err == nil || !strings.Contains(err.Error(), "no longer runs on") {
		t.Fatalf("a thread on a model the agent dropped: %v", err)
	}
	mustPatchInternal(t, ds, typeProvider, "openai", map[string]any{"apiKey": nil})
	if _, err := ds.continuedRunsOn(ctx, ag, thread); err == nil || !strings.Contains(err.Error(), "cannot run now") {
		t.Fatalf("a thread whose model lost its key moves to no other: %v", err)
	}
}

// The console moves an agent of the owner's from provider + model to runsOn
// in one patch: runsOn set, the other two cleared, under one version.
func TestAnAgentMovesToRunsOnInOnePatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openModelsDataset(t)
	pkg := testdb.Repository(t) + "/crew"
	id := pkg + "/helper"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.AgentManifest(pkg, "helper", map[string]any{
			"description": "Helps.", "prompt": "Help.", "provider": "anthropic", "model": "claude-opus-5-5",
		}),
	}); err != nil {
		t.Fatalf("declare the agent: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, kindAgent, id, substrate.PatchInput{Properties: map[string]any{
		"runsOn":   []any{"anthropic:claude-opus-5-5", "anthropic:claude-sonnet-5-5"},
		"provider": nil, "model": nil,
	}}); err != nil {
		t.Fatalf("move the agent to runsOn: %v", err)
	}
	ag, err := ds.registry().ResolveAgent(id)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"anthropic:claude-opus-5-5", "anthropic:claude-sonnet-5-5"}; !slices.Equal(ag.RunsOn, want) || ag.Provider != "" || ag.Model != "" {
		t.Fatalf("runsOn %v, provider %q, model %q; want %v alone", ag.RunsOn, ag.Provider, ag.Model, want)
	}
	// Naming both is refused, so a half-done move never lands.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, kindAgent, id, substrate.PatchInput{Properties: map[string]any{
		"provider": "anthropic", "model": "claude-opus-5-5",
	}}); err == nil || !strings.Contains(err.Error(), "name exactly one of provider, providerInput and runsOn") {
		t.Fatalf("provider beside runsOn: %v", err)
	}
}

// A chat on runsOn, run end to end on the fake: the first turn stamps the
// thread with the entry it took, a continued turn sends that model again
// after the list changed, a new thread takes the new first entry, and a
// continued turn whose model lost its key refuses instead of moving.
func TestARunsOnThreadStaysOnItsModelAcrossTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	for _, row := range []string{"first", "second"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeProvider, ID: row,
			Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-" + row},
		}); err != nil {
			t.Fatal(err)
		}
	}
	putModelRecord(t, ds, "first:m1", map[string]any{"provider": "first", "model": "m1"})
	putModelRecord(t, ds, "second:m2", map[string]any{"provider": "second", "model": "m2"})
	pkg := testdb.Repository(t) + "/crew"
	id := pkg + "/talker"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.AgentManifest(pkg, "talker", map[string]any{
			"description": "Talks.", "prompt": "Talk.", "runsOn": []any{"first:m1", "second:m2"},
		}),
	}); err != nil {
		t.Fatalf("declare the agent: %v", err)
	}
	chat := func(thread, message string) (*substrate.AgentResult, error) {
		return ds.ChatAgent(ctx, substrate.ActorAPI, id, substrate.ChatInput{Thread: thread, Message: message}, nil)
	}

	fake.script("m1", fakeTurn{content: "one"})
	res, err := chat("", "hi")
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	thread, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: res.Thread})
	if err != nil || thread == nil {
		t.Fatalf("the thread: %v, %v", thread, err)
	}
	if row, model := vocabulary.ReferentID(thread.Props["provider"], typeProvider), thread.Props["model"]; row != "first" || model != "m1" {
		t.Fatalf("the thread is stamped %q %v, want the entry it took", row, model)
	}

	mustPatchInternal(t, ds, kindAgent, id, map[string]any{"runsOn": []any{"second:m2", "first:m1"}})
	// The allowance hold asks what the run takes: the thread's own row for
	// a continued thread, the list's first for a new one.
	ag, err := ds.registry().ResolveAgent(id)
	if err != nil {
		t.Fatal(err)
	}
	if row, model, err := ds.agentRowAndModel(ctx, ag, res.Thread); err != nil || row != "first" || model != "m1" {
		t.Fatalf("the hold for the continued thread reads %q %q (%v)", row, model, err)
	}
	if row, _, err := ds.agentRowAndModel(ctx, ag, ""); err != nil || row != "second" {
		t.Fatalf("the hold for a new thread reads %q (%v)", row, err)
	}
	fake.script("m1", fakeTurn{content: "two"})
	if _, err := chat(res.Thread, "again"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if got := len(fake.requestsOf("m1")); got != 2 || len(fake.requestsOf("m2")) != 0 {
		t.Fatalf("m1 sent %d times and m2 %d, want the continued thread on m1", got, len(fake.requestsOf("m2")))
	}

	fake.script("m2", fakeTurn{content: "three"})
	if _, err := chat("", "a new one"); err != nil {
		t.Fatalf("a new thread: %v", err)
	}
	if got := len(fake.requestsOf("m2")); got != 1 {
		t.Fatalf("m2 sent %d times, want the new thread on the list's first entry", got)
	}

	mustPatchInternal(t, ds, typeProvider, "first", map[string]any{"apiKey": nil})
	if _, err := chat(res.Thread, "once more"); err == nil || !strings.Contains(err.Error(), "cannot run now") {
		t.Fatalf("a continued thread whose model lost its key: %v", err)
	}
	if got := len(fake.requestsOf("m2")); got != 1 {
		t.Fatalf("m2 sent %d times: the continued thread moved to another model", got)
	}
}

// A shipped price the binary corrects reaches a record only the seed wrote,
// a rate it stops stating included, and the next open finds it current.
func TestTheSeedRefreshesAShippedPriceAndDropsARateItNoLongerStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openModelsDataset(t)
	shipped := slices.Clone(ds.svc.shippedRecords)
	i := slices.IndexFunc(shipped, func(r vocabulary.ShippedRecord) bool {
		return r.Kind == typeModel && r.ID == "anthropic:claude-opus-5-5"
	})
	if i < 0 {
		t.Fatal("anthropic:claude-opus-5-5 is not shipped")
	}
	props := maps.Clone(shipped[i].Properties)
	pricing := maps.Clone(props["pricing"].(map[string]any))
	delete(pricing, "cacheReadPer1M")
	pricing["inputPer1M"] = "3"
	props["pricing"] = pricing
	shipped[i].Properties = props
	ds.svc.shippedRecords = shipped

	if err := ds.ensureShippedRecords(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeModel, ID: "anthropic:claude-opus-5-5"})
	if err != nil || row == nil {
		t.Fatalf("the record: %v, %v", row, err)
	}
	got, _ := row.Props["pricing"].(map[string]any)
	if _, stated := got["cacheReadPer1M"]; stated || got["inputPer1M"] != "3" {
		t.Fatalf("pricing %v, want the corrected input rate and no read rate", got)
	}
	before := changelogHead(t, ds)
	if err := ds.ensureShippedRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if after := changelogHead(t, ds); after != before {
		t.Fatalf("the open after the correction wrote %d entries", after-before)
	}
}

// Two writes of one row and model at once: one lands, the other is refused.
func TestTwoWritesOfOnePairLandOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "gw",
		Properties: map[string]any{"wire": "openai", "baseURL": "https://gw.example.com/v1"},
	}); err != nil {
		t.Fatal(err)
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			errs[i] = putModelRecordErr(ds, fmt.Sprintf("r%d", i), map[string]any{"provider": "gw", "model": "m"})
		})
	}
	wg.Wait()
	landed := 0
	for _, err := range errs {
		switch {
		case err == nil:
			landed++
		case !errors.Is(err, substrate.ErrValidation):
			t.Fatalf("a refusal that is not the pair's: %v", err)
		}
	}
	if landed != 1 {
		t.Fatalf("%d of the two writes landed (%v), want one", landed, errs)
	}
}
