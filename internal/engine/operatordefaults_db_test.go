package engine

// The operator defaults (decision 0230): an empty, operator-listed
// llm/provider row runs on the operator's transport whole, the operator's
// embeddings entry serves a repository whose rows claim none, and every
// operator-paid call reserves against the allowance ledger first. Each test
// here builds the defaults file by hand, so the transport points at an
// in-process fake; internal/operatordefaults holds the file's own refusals.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// operatorTestKey is the operator's key in these fixtures; it is never a real
// key.
const operatorTestKey = "operator-key-fixture"

// testClock is a settable clock the allowance ledger reads.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// operatorEmbedFake is an OpenAI-wire embeddings endpoint: a bag-of-words
// vector per input, so texts sharing words score high, and every bearer it
// saw. It reports no usage, as some gateways do.
type operatorEmbedFake struct {
	mu    sync.Mutex
	auths []string
	srv   *httptest.Server
}

func newOperatorEmbedFake(t *testing.T) *operatorEmbedFake {
	t.Helper()
	f := &operatorEmbedFake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.mu.Unlock()
		type datum struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var out struct {
			Data []datum `json:"data"`
		}
		for i, text := range req.Input {
			vec := make([]float32, 1536)
			for _, word := range strings.Fields(strings.ToLower(text)) {
				h := 0
				for _, r := range word {
					h = (h*31 + int(r)) % len(vec)
				}
				vec[h]++
			}
			var sum float64
			for _, v := range vec {
				sum += float64(v * v)
			}
			if sum == 0 {
				vec[0], sum = 1, 1
			}
			for j := range vec {
				vec[j] /= float32(math.Sqrt(sum))
			}
			out.Data = append(out.Data, datum{Index: i, Embedding: vec})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *operatorEmbedFake) authsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...)
}

// operatorChatConfig is a defaults file serving the crew's chat row, chatllm,
// from llmURL, model `chat` at 1 and 5 USD per million tokens, and embeddings
// from embedURL where it is set, nested under the id `vectors`.
func operatorChatConfig(llmURL, embedURL string, cents int64) *operatordefaults.Config {
	entry := &operatordefaults.LLM{
		ID: "chatllm", Wire: llm.WireOpenAI, BaseURL: llmURL, APIKey: operatordefaults.NewSecret(operatorTestKey),
		Models:          []operatordefaults.Model{{Model: "chat", InputPer1M: "1", OutputPer1M: "5"}},
		MaxOutputTokens: 64,
		Headers:         []operatordefaults.Header{{Name: "X-Operator-Tag", Value: "operator"}},
	}
	c := &operatordefaults.Config{LLM: map[string]*operatordefaults.LLM{"chatllm": entry}, Budget: operatordefaults.Budget{CentsPerDay: cents}}
	if embedURL != "" {
		c.LLM["vectors"] = &operatordefaults.LLM{
			ID: "vectors", Wire: llm.WireOpenAI, BaseURL: embedURL, APIKey: operatordefaults.NewSecret("operator-embed-key-fixture"),
			Models:          []operatordefaults.Model{{Model: "unused", InputPer1M: "0", OutputPer1M: "0"}},
			MaxOutputTokens: 1,
			Embeddings:      &operatordefaults.Embeddings{Model: "text-embedding-3-small", Per1M: "0.02"},
		}
	}
	return c
}

// openOperatorDataset opens the agent fixture under a defaults file, the site
// named Example: the crew's rows point at the returned row fake, and the
// operator's transport at the second fake.
func openOperatorDataset(t *testing.T, cfg func(operatorURL string) *operatordefaults.Config) (*dataset, *fakeLLM, *fakeLLM, *testClock) {
	t.Helper()
	op := newFakeLLM(t)
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds := openInternalDataset(t, WithOperatorDefaults(cfg(op.srv.URL), "Example"))
	ds.svc.operatorNow = clock.Now
	rows := provisionAgents(t, ds)
	return ds, rows, op, clock
}

// emptyTheChatRow leaves chatllm without a key and with a transport of its
// own that must never be used: a base URL at the row fake, a header, request
// defaults and a cache opt-in.
func emptyTheChatRow(t *testing.T, ds *dataset, rowURL string) {
	t.Helper()
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{
		"apiKey": nil, "baseURL": rowURL,
		"headers":  []any{map[string]any{"name": "X-Row-Leak", "value": "row"}},
		"defaults": map[string]any{"temperature": 1.5},
		"cache":    map[string]any{"key": true},
	})
}

func mustPatchInternal(t *testing.T, ds *dataset, kind, id string, props map[string]any) {
	t.Helper()
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, kind, id, substrate.PatchInput{Properties: props}); err != nil {
		t.Fatalf("patch %s/%s: %v", kind, id, err)
	}
}

// operatorLedger reads every reservation: its slot, ceiling and settled cost
// (-1 while open).
func operatorLedger(t *testing.T, ds *dataset) []struct {
	slot             string
	ceiling, settled float64
} {
	t.Helper()
	rows, err := ds.db.Query(`SELECT slot, ceiling_cents::float8, coalesce(settled_cents, -1)::float8 FROM operator_spend_reservations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []struct {
		slot             string
		ceiling, settled float64
	}
	for rows.Next() {
		var r struct {
			slot             string
			ceiling, settled float64
		}
		if err := rows.Scan(&r.slot, &r.ceiling, &r.settled); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// An empty row the operator lists runs on the operator's transport whole: its
// key, its endpoint, its headers and its output ceiling, and none of what the
// row's own fields say. The call is charged to the allowance ledger. Writing
// a key on the row moves it back to the person's own transport.
func TestOperatorTransportServesAnEmptyOperatorListedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)

	op.script("chat", fakeTurn{content: "hello from the operator"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if n := len(rowFake.requestsOf("chat")); n != 0 {
		t.Fatalf("the row's own base URL received %d requests", n)
	}
	reqs := op.requestsOf("chat")
	if len(reqs) != 1 {
		t.Fatalf("the operator's endpoint received %d requests, want 1", len(reqs))
	}
	for _, a := range op.authsSeen() {
		if a != "Bearer "+operatorTestKey {
			t.Fatalf("the operator's endpoint saw bearer %q", a)
		}
	}
	for _, h := range op.headersSeen() {
		if h.Get("X-Row-Leak") != "" {
			t.Fatal("the row's own header reached an operator-paid request")
		}
		if h.Get("X-Operator-Tag") != "operator" {
			t.Fatal("the operator entry's header did not ride the request")
		}
	}
	req := reqs[0]
	if _, set := req["temperature"]; set {
		t.Fatalf("the row's defaults reached an operator-paid request: temperature %v", req["temperature"])
	}
	if _, set := req["prompt_cache_key"]; set {
		t.Fatal("the row's cache opt-in reached an operator-paid request")
	}
	if got, _ := req["max_completion_tokens"].(float64); got != 64 {
		t.Fatalf("max_completion_tokens = %v, want the operator's 64 imposed", req["max_completion_tokens"])
	}
	ledger := operatorLedger(t, ds)
	if len(ledger) != 1 || ledger[0].slot != substrate.SlotLLM || ledger[0].settled <= 0 || ledger[0].settled >= ledger[0].ceiling {
		t.Fatalf("ledger = %+v, want one llm reservation settled below its ceiling", ledger)
	}

	// The person's own key wins, and the operator's endpoint is not asked
	// again.
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": "row-own-key"})
	rowFake.script("chat", fakeTurn{content: "hello from the row"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "again"}, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat on the person's key: %v", err)
	}
	if n := len(op.requestsOf("chat")); n != 1 {
		t.Fatalf("the operator's endpoint received %d requests after the row was keyed, want still 1", n)
	}
	if auths := rowFake.authsSeen(); len(auths) == 0 || auths[len(auths)-1] != "Bearer row-own-key" {
		t.Fatalf("the row's endpoint saw %v", auths)
	}
	if n := len(operatorLedger(t, ds)); n != 1 {
		t.Fatalf("a call on the person's key wrote the allowance ledger: %d rows", n)
	}
}

// A model the operator does not serve is refused before anything is sent,
// naming the site, the models it does serve and the way out.
func TestOperatorTransportRefusesAModelOffItsList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config {
		c := operatorChatConfig(u, "", 1000)
		c.LLM["chatllm"].Models = []operatordefaults.Model{{Model: "small", InputPer1M: "1", OutputPer1M: "1"}}
		return c
	})
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "runs on Example's key, which serves only small") ||
		!strings.Contains(err.Error(), "add your own key to the row") {
		t.Fatalf("chat = %v, want the model refused naming the site and its list", err)
	}
	if n := len(op.authsSeen()) + len(rowFake.authsSeen()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

// An operator-listed row the person deleted stays deleted: nothing resolves
// it, the operator defaults read says how to have it back, and a row
// recreated under the same id without a key is the operator's again.
func TestOperatorDoesNotReviveADeletedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeProvider, "chatllm", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("chat on a deleted row = %v", err)
	}
	if n := len(op.authsSeen()); n != 0 {
		t.Fatalf("the operator's endpoint received %d requests for a deleted row", n)
	}
	got, err := ds.OperatorDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := operatorItem(t, got, "llm/chatllm")
	if item.Source != substrate.SourceNone || !item.OperatorOffers || item.Readiness != substrate.ReadinessNotSetUp ||
		!strings.Contains(item.Message, "create it without an apiKey to use Example's key") {
		t.Fatalf("deleted row's item = %+v", item)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "chatllm", Properties: map[string]any{"wire": "openai"}}); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	pc, err := ds.resolveProvider(ctx, "chatllm")
	if err != nil || pc.operator == nil || pc.cfg.APIKey != operatorTestKey {
		t.Fatalf("the recreated keyless row = %+v, %v; want the operator's transport", pc, err)
	}
}

func operatorItem(t *testing.T, s substrate.OperatorDefaults, id string) substrate.OperatorDefault {
	t.Helper()
	for _, it := range s.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %s in %+v", id, s.Items)
	return substrate.OperatorDefault{}
}

// The ledger reserves a ceiling against the UTC day's sum, refuses a
// reservation that would pass the allowance, naming the site, settles to the reported cost, keeps
// the ceiling where none was reported, settles a reservation left open (a
// crash) at its ceiling after an hour and never lowers it after, and starts
// again at the next UTC day.
func TestOperatorLedgerReservesSettlesAndExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds := openInternalDataset(t, WithOperatorDefaults(&operatordefaults.Config{Budget: operatordefaults.Budget{CentsPerDay: 10}}, "Example"))
	ds.svc.operatorNow = clock.Now
	used := func() float64 {
		t.Helper()
		u, _, _, err := ds.operatorSpendToday(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	reserve := func(c float64) *operatorReservation {
		t.Helper()
		r, err := ds.reserveOperator(ctx, substrate.SlotLLM, c)
		if err != nil {
			t.Fatalf("reserve %v: %v", c, err)
		}
		return r
	}
	r1, crashed := reserve(4), reserve(4)
	_, err := ds.reserveOperator(ctx, substrate.SlotLLM, 4)
	if !errors.Is(err, substrate.ErrAllowance) || !errors.Is(err, substrate.ErrGuard) ||
		!strings.HasSuffix(err.Error(), "today's allowance from Example has 2 of 10 cents left, not enough for the next call; it resets at 00:00 UTC; add your own key to keep going") {
		t.Fatalf("a reservation past the allowance = %v", err)
	}
	ds.settleOperator(ctx, r1, 1)
	if u := used(); u != 5 {
		t.Fatalf("used after a settle = %v, want 5", u)
	}
	r3 := reserve(4)

	// Nothing settles crashed: an hour on, the next reservation settles it,
	// and r3, at their ceilings, and a late settle does not lower them.
	clock.Set(clock.Now().Add(61 * time.Minute))
	reserve(0.5)
	ds.settleOperator(ctx, crashed, 0)
	ds.settleOperator(ctx, r3, 0)
	if u := used(); u != 9.5 {
		t.Fatalf("used after the expiry = %v, want 9.5", u)
	}
	for _, row := range operatorLedger(t, ds)[:3] {
		if row.settled < 0 {
			t.Fatalf("a reservation is still open after the expiry: %+v", row)
		}
	}

	// A call that reports no usage keeps its ceiling, and so does one that
	// failed after it left; one the endpoint refused costs nothing, and the
	// mark is read through whatever wraps it.
	missing := reserve(0.125)
	ds.settleCall(ctx, missing, nil, 0, false)
	dropped := reserve(0.125)
	ds.settleCall(ctx, dropped, errors.New("connection reset by peer"), 0, false)
	refused := reserve(0.125)
	ds.settleCall(ctx, refused, fmt.Errorf("agent: %w", egress.Unprocessed(errors.New("401 refused"))), 0, false)
	if u := used(); u != 9.75 {
		t.Fatalf("used = %v, want 9.75", u)
	}

	// The next UTC day starts at nothing.
	clock.Set(time.Date(2026, 11, 7, 0, 0, 1, 0, time.UTC))
	if u := used(); u != 0 {
		t.Fatalf("used on the next day = %v", u)
	}
	reserve(9.5)
}

// At the allowance a trigger is held, as decision 0149 holds one at the
// person's cap, and a direct call is refused before a thread opens, whoever
// makes it: an app's token reaches the same engine call. Both say the
// allowance is used up, name the site and the way out.
func TestOperatorHoldsARunAtTheLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, "", 1) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	ag, err := ds.registry().ResolveAgent(crewPackage + "/chatter")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := ds.spendHold(ctx, ag); err != nil || text != "" {
		t.Fatalf("hold under the limit = %q, %v", text, err)
	}
	// 0.99 of the 1 cent is held: a completion of 64 tokens at 5 USD per
	// million (0.032 cents) no longer fits.
	if _, err := ds.reserveOperator(ctx, substrate.SlotLLM, 0.99); err != nil {
		t.Fatal(err)
	}
	const usedUp = "today's allowance from Example is used up; it resets at 00:00 UTC; add your own key to keep going"
	text, err := ds.spendHold(ctx, ag)
	if err != nil || text != usedUp {
		t.Fatalf("hold at the limit = %q, %v", text, err)
	}
	_, err = ds.ChatAgent(ctx, substrate.Actor("app:notes.example.org"), crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	if !errors.Is(err, substrate.ErrAllowance) || !strings.HasSuffix(err.Error(), ": "+usedUp) {
		t.Fatalf("an app's chat at the limit = %v", err)
	}
	if n := len(op.authsSeen()); n != 0 {
		t.Fatalf("the operator's endpoint received %d requests at the limit", n)
	}
	if n := threadCountOf(t, ds, crewPackage+"/chatter"); n != 0 {
		t.Fatalf("a refused chat opened %d threads", n)
	}
}

// The operator's embeddings entry serves a repository whose rows claim none:
// the drain buys vectors on the operator's key, stamped `(operator)`, the
// semantic arm reads them, the import's reconcile keeps them as current, and
// at the limit a hybrid search answers by its words while a semantic one is
// refused. A row that claims embedModel without a key of its own is refused.
func TestOperatorEmbeddingsServeARepositoryWhoseRowsClaimNone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newOperatorEmbedFake(t)
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds := openInternalDataset(t, WithOperatorDefaults(operatorChatConfig("https://llm.example.com/v1", emb.srv.URL, 1000), "Example"))
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
	if n, err := ds.ProcessEmbedQueue(ctx, 20); err != nil || n != 1 {
		t.Fatalf("drain = %d, %v", n, err)
	}
	var provider, model string
	if err := ds.db.QueryRow(`SELECT provider, model FROM embeddings WHERE record_id = $1`, book.ID).Scan(&provider, &model); err != nil {
		t.Fatal(err)
	}
	if provider != operatorEmbedProviderID || model != "text-embedding-3-small" {
		t.Fatalf("vector stamped %s/%s", provider, model)
	}
	for _, a := range emb.authsSeen() {
		if a != "Bearer operator-embed-key-fixture" {
			t.Fatalf("the embeddings endpoint saw %q", a)
		}
	}
	if ledger := operatorLedger(t, ds); len(ledger) == 0 || ledger[0].slot != substrate.SlotEmbeddings {
		t.Fatalf("ledger = %+v, want an embeddings reservation", ledger)
	}
	res, err := ds.Search(ctx, substrate.SearchInput{Q: "saxophone almanac", Mode: substrate.SearchSemantic})
	if err != nil || len(res.Hits) == 0 || res.Hits[0].Record.ID != book.ID {
		t.Fatalf("semantic search = %+v, %v", res, err)
	}

	// The import's reconcile reads the same pair, so the operator's vectors
	// are current and nothing is queued to be bought again.
	if n, err := ds.reconcileEmbeddings(ctx, ds.db, time.Now()); err != nil || n != 0 {
		t.Fatalf("reconcile queued %d, %v; want 0", n, err)
	}
	var vectors int
	if err := ds.db.QueryRow(`SELECT count(*) FROM embeddings`).Scan(&vectors); err != nil || vectors != 1 {
		t.Fatalf("%d vectors after the reconcile, %v", vectors, err)
	}

	// At the limit: hybrid answers by its words and says why, semantic is
	// refused naming the site.
	spent, _, _, err := ds.operatorSpendToday(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.reserveOperator(ctx, substrate.SlotLLM, 1000-spent); err != nil {
		t.Fatal(err)
	}
	hybrid, err := ds.Search(ctx, substrate.SearchInput{Q: "saxophone almanac", Mode: substrate.SearchHybrid})
	if err != nil || !strings.HasPrefix(hybrid.Note, "Today's allowance from Example is used up") {
		t.Fatalf("hybrid at the limit = %+v, %v", hybrid, err)
	}
	if _, err := ds.Search(ctx, substrate.SearchInput{Q: "saxophone almanac", Mode: substrate.SearchSemantic}); !errors.Is(err, substrate.ErrAllowance) ||
		!strings.Contains(err.Error(), "today's allowance from Example") {
		t.Fatalf("semantic at the limit = %v", err)
	}

	// A row that claims embedModel pays with its own key: a keyless one the
	// operator lists is refused rather than run on the operator's.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "chatllm", Properties: map[string]any{
		"wire": "openai", "baseURL": emb.srv.URL, "embedModel": "text-embedding-3-small",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.resolveEmbedProvider(ctx); !errors.Is(err, substrate.ErrValidation) ||
		!strings.Contains(err.Error(), "pays for them with its own key, and Example's embeddings serve only") {
		t.Fatalf("a keyless claiming row = %v", err)
	}
}

// The operator defaults read: where each row's key comes from, whether the
// operator's file offers it whatever the source and the allowance, whether
// it works, the newest open alert of an agent on the row, the embeddings
// provider and the row its card belongs to, and today's allowance.
func TestOperatorDefaultsReportsSourcesAndAllowance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newOperatorEmbedFake(t)
	ds, rowFake, _, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, emb.srv.URL, 200) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "bare", Properties: map[string]any{"wire": "anthropic"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.reserveOperator(ctx, substrate.SlotLLM, 36.2); err != nil {
		t.Fatal(err)
	}
	read := func() substrate.OperatorDefaults {
		t.Helper()
		got, err := ds.OperatorDefaults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read()
	if got.Operator == nil || got.Operator.Name != "Example" || got.Operator.Allowance == nil {
		t.Fatalf("operator = %+v", got.Operator)
	}
	if a := got.Operator.Allowance; a.CentsPerDay != 200 || a.UsedCents != 37 ||
		!a.ResetsAt.Equal(time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)) ||
		len(a.BySlot) != 1 || a.BySlot[0].Slot != substrate.SlotLLM || a.BySlot[0].Cents != 37 {
		t.Fatalf("allowance = %+v", a)
	}
	if it := operatorItem(t, got, "llm/chatllm"); it.Source != substrate.SourceOperator || !it.OperatorOffers ||
		it.Readiness != substrate.ReadinessReady || len(it.Models) != 1 {
		t.Fatalf("chatllm = %+v", it)
	}
	if it := operatorItem(t, got, "llm/rootllm"); it.Source != substrate.SourceOwn || it.OperatorOffers ||
		it.Readiness != substrate.ReadinessReady || len(it.Models) != 0 {
		t.Fatalf("rootllm = %+v", it)
	}
	if it := operatorItem(t, got, "llm/bare"); it.Source != substrate.SourceNone || it.OperatorOffers || it.Readiness != substrate.ReadinessNotSetUp {
		t.Fatalf("bare = %+v", it)
	}
	// The file lists vectors, which has no row: it offers the slot, and
	// nothing resolves it.
	if it := operatorItem(t, got, "llm/vectors"); it.Source != substrate.SourceNone || !it.OperatorOffers || it.Readiness != substrate.ReadinessNotSetUp {
		t.Fatalf("vectors = %+v", it)
	}
	// No row claims embedModel, so the operator's embeddings serve, and the
	// row they are nested under does not exist, so no card shows the line.
	if it := operatorItem(t, got, "embeddings"); it.Source != substrate.SourceOperator || !it.OperatorOffers ||
		it.Readiness != substrate.ReadinessReady || it.Target != "" {
		t.Fatalf("embeddings = %+v", it)
	}

	// The allowance used up changes neither where the key comes from nor
	// whether the operator offers it.
	if _, err := ds.reserveOperator(ctx, substrate.SlotLLM, 163); err != nil {
		t.Fatal(err)
	}
	got = read()
	if got.Operator.Allowance.UsedCents != 200 {
		t.Fatalf("allowance = %+v", got.Operator.Allowance)
	}
	if it := operatorItem(t, got, "llm/chatllm"); it.Source != substrate.SourceOperator || !it.OperatorOffers || it.Readiness != substrate.ReadinessReady {
		t.Fatalf("chatllm with the allowance used up = %+v", it)
	}

	// The row the embeddings entry is nested under, once it exists, is the
	// embeddings target.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "vectors", Properties: map[string]any{"wire": "openai"}}); err != nil {
		t.Fatal(err)
	}
	if it := operatorItem(t, read(), "embeddings"); it.Source != substrate.SourceOperator || it.Target != vocabulary.RecordPath(typeProvider, "vectors") {
		t.Fatalf("embeddings with its row = %+v", it)
	}

	// The person's own key on chatllm is the source, and the operator still
	// offers the slot: removing the key falls back to the operator's.
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": "row-own-key"})
	if it := operatorItem(t, read(), "llm/chatllm"); it.Source != substrate.SourceOwn || !it.OperatorOffers ||
		it.Readiness != substrate.ReadinessReady || len(it.Models) != 0 {
		t.Fatalf("chatllm on its own key = %+v", it)
	}

	// A keyed row claiming embedModel is the embeddings source and target,
	// and the operator still offers the slot.
	mustPatchInternal(t, ds, typeProvider, "rootllm", map[string]any{"embedModel": "text-embedding-3-small"})
	if it := operatorItem(t, read(), "embeddings"); it.Source != substrate.SourceOwn || !it.OperatorOffers ||
		it.Readiness != substrate.ReadinessReady || it.Target != vocabulary.RecordPath(typeProvider, "rootllm") {
		t.Fatalf("embeddings claimed by rootllm = %+v", it)
	}

	// An open alert about the chat agent marks its row failing, with the
	// park's error.
	if err := ds.inTx(ctx, substrate.ActorSystem, false, func(tx *txn) error {
		return tx.raiseAlert(substrate.ActorSystem, alertRaise{
			key: "trigger.parked/operatordefaults-test", level: alertLevelError, summary: "Trigger x has parked deliveries",
			detail: "401 invalid api key", about: []string{vocabulary.RecordPath(kindAgent, crewPackage+"/chatter")},
		})
	}); err != nil {
		t.Fatal(err)
	}
	if it := operatorItem(t, read(), "llm/chatllm"); it.Readiness != substrate.ReadinessFailing || it.Message != "401 invalid api key" {
		t.Fatalf("chatllm with an open alert = %+v", it)
	}
}

// Without a defaults file nothing changes but the words: a keyless row
// refuses naming this server, the repository has no embeddings pair, the
// operator defaults read names no operator and offers nothing, and nothing
// writes the ledger.
func TestNoOperatorDefaultsFileKeepsTodaysBehavior(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": nil})
	_, err := ds.resolveProvider(ctx, "chatllm")
	if !errors.Is(err, substrate.ErrValidation) ||
		err.Error() != `substrate: validation failed: llm/provider row "chatllm" declares no apiKey: a row carries the key for the endpoint it names, and this server supplies no key for a row of this id` {
		t.Fatalf("a keyless row = %v", err)
	}
	if pair, err := ds.embedPair(ctx, ds.db); err != nil || pair != nil {
		t.Fatalf("embeddings pair = %+v, %v", pair, err)
	}
	if p, err := ds.resolveEmbedProvider(ctx); err != nil || p != nil {
		t.Fatalf("embeddings provider = %+v, %v", p, err)
	}
	got, err := ds.OperatorDefaults(ctx)
	if err != nil || got.Operator != nil {
		t.Fatalf("operator defaults = %+v, %v", got, err)
	}
	if it := operatorItem(t, got, "llm/chatllm"); it.Source != substrate.SourceNone || it.OperatorOffers {
		t.Fatalf("chatllm = %+v", it)
	}
	if it := operatorItem(t, got, "embeddings"); it.Source != substrate.SourceNone || it.OperatorOffers ||
		it.Readiness != substrate.ReadinessNotSetUp || it.Target != "" {
		t.Fatalf("embeddings = %+v", it)
	}
	fake.script("chat", fakeTurn{content: "ok"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("chat on a keyless row with no defaults file = %v", err)
	}
	var n int
	if err := ds.db.QueryRow(`SELECT count(*) FROM operator_spend_reservations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ledger rows = %d, %v", n, err)
	}
}
