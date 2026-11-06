package engine

// The host defaults (decision 0230): an empty, host-listed llm/provider row
// runs on the host's transport whole, the host's embeddings entry serves a
// repository whose rows claim none, and every host-paid call reserves against
// the host's daily ledger first. Each test here builds the host file by hand,
// so the transport points at an in-process fake; internal/hostdefaults holds
// the file's own refusals.

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
	"github.com/geoah/substrate/internal/hostdefaults"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// hostTestKey is the host's key in these fixtures; it is never a real key.
const hostTestKey = "host-key-fixture"

// testClock is a settable clock the host ledger reads.
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

// hostEmbedFake is an OpenAI-wire embeddings endpoint: a bag-of-words
// vector per input, so texts sharing words score high, and every bearer it
// saw. It reports no usage, as some gateways do.
type hostEmbedFake struct {
	mu    sync.Mutex
	auths []string
	srv   *httptest.Server
}

func newHostEmbedFake(t *testing.T) *hostEmbedFake {
	t.Helper()
	f := &hostEmbedFake{}
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

func (f *hostEmbedFake) authsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...)
}

// hostChatConfig is a host file serving the crew's chat row, chatllm, from
// llmURL, model `chat` at 1 and 5 USD per million tokens, and embeddings from
// embedURL where it is set.
func hostChatConfig(llmURL, embedURL string, cents int64) *hostdefaults.Config {
	entry := &hostdefaults.LLM{
		ID: "chatllm", Wire: llm.WireOpenAI, BaseURL: llmURL, APIKey: hostdefaults.NewSecret(hostTestKey),
		Models:          []hostdefaults.Model{{Model: "chat", InputPer1M: "1", OutputPer1M: "5"}},
		MaxOutputTokens: 64,
		Headers:         []hostdefaults.Header{{Name: "X-Host-Tag", Value: "host"}},
	}
	c := &hostdefaults.Config{LLM: map[string]*hostdefaults.LLM{"chatllm": entry}, Budget: hostdefaults.Budget{CentsPerDay: cents}}
	if embedURL != "" {
		c.LLM["vectors"] = &hostdefaults.LLM{
			ID: "vectors", Wire: llm.WireOpenAI, BaseURL: embedURL, APIKey: hostdefaults.NewSecret("host-embed-key-fixture"),
			Models:          []hostdefaults.Model{{Model: "unused", InputPer1M: "0", OutputPer1M: "0"}},
			MaxOutputTokens: 1,
			Embeddings:      &hostdefaults.Embeddings{Model: "text-embedding-3-small", Per1M: "0.02"},
		}
	}
	return c
}

// openHostDataset opens the agent fixture under a host file: the crew's rows
// point at the returned row fake, and the host's transport at host.
func openHostDataset(t *testing.T, cfg func(hostURL string) *hostdefaults.Config) (*dataset, *fakeLLM, *fakeLLM, *testClock) {
	t.Helper()
	host := newFakeLLM(t)
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds := openInternalDataset(t, WithHostDefaults(cfg(host.srv.URL)))
	ds.svc.hostNow = clock.Now
	rows := provisionAgents(t, ds)
	return ds, rows, host, clock
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

// hostLedger reads every reservation: its service, ceiling and settled cost
// (-1 while open).
func hostLedger(t *testing.T, ds *dataset) []struct {
	service          string
	ceiling, settled float64
} {
	t.Helper()
	rows, err := ds.db.Query(`SELECT service, ceiling_cents::float8, coalesce(settled_cents, -1)::float8 FROM host_spend_reservations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []struct {
		service          string
		ceiling, settled float64
	}
	for rows.Next() {
		var r struct {
			service          string
			ceiling, settled float64
		}
		if err := rows.Scan(&r.service, &r.ceiling, &r.settled); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// An empty row the host lists runs on the host's transport whole: its key,
// its endpoint, its headers and its output ceiling, and none of what the
// row's own fields say. The call is charged to the host's ledger. Writing a
// key on the row moves it back to the person's own transport.
func TestHostTransportServesAnEmptyHostListedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, host, _ := openHostDataset(t, func(u string) *hostdefaults.Config { return hostChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)

	host.script("chat", fakeTurn{content: "hello from the host"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if n := len(rowFake.requestsOf("chat")); n != 0 {
		t.Fatalf("the row's own base URL received %d requests", n)
	}
	reqs := host.requestsOf("chat")
	if len(reqs) != 1 {
		t.Fatalf("the host's endpoint received %d requests, want 1", len(reqs))
	}
	for _, a := range host.authsSeen() {
		if a != "Bearer "+hostTestKey {
			t.Fatalf("the host's endpoint saw bearer %q", a)
		}
	}
	for _, h := range host.headersSeen() {
		if h.Get("X-Row-Leak") != "" {
			t.Fatal("the row's own header reached a host-paid request")
		}
		if h.Get("X-Host-Tag") != "host" {
			t.Fatal("the host entry's header did not ride the request")
		}
	}
	req := reqs[0]
	if _, set := req["temperature"]; set {
		t.Fatalf("the row's defaults reached a host-paid request: temperature %v", req["temperature"])
	}
	if _, set := req["prompt_cache_key"]; set {
		t.Fatal("the row's cache opt-in reached a host-paid request")
	}
	if got, _ := req["max_completion_tokens"].(float64); got != 64 {
		t.Fatalf("max_completion_tokens = %v, want the host's 64 imposed", req["max_completion_tokens"])
	}
	ledger := hostLedger(t, ds)
	if len(ledger) != 1 || ledger[0].service != substrate.HostServiceLLM || ledger[0].settled <= 0 || ledger[0].settled >= ledger[0].ceiling {
		t.Fatalf("ledger = %+v, want one llm reservation settled below its ceiling", ledger)
	}

	// The person's own key wins, and the host is not asked again.
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": "row-own-key"})
	rowFake.script("chat", fakeTurn{content: "hello from the row"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "again"}, func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat on the person's key: %v", err)
	}
	if n := len(host.requestsOf("chat")); n != 1 {
		t.Fatalf("the host's endpoint received %d requests after the row was keyed, want still 1", n)
	}
	if auths := rowFake.authsSeen(); len(auths) == 0 || auths[len(auths)-1] != "Bearer row-own-key" {
		t.Fatalf("the row's endpoint saw %v", auths)
	}
	if n := len(hostLedger(t, ds)); n != 1 {
		t.Fatalf("a call on the person's key wrote the host's ledger: %d rows", n)
	}
}

// A model the host does not serve is refused before anything is sent, naming
// the models it does serve and the way out.
func TestHostTransportRefusesAModelOffItsList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, host, _ := openHostDataset(t, func(u string) *hostdefaults.Config {
		c := hostChatConfig(u, "", 1000)
		c.LLM["chatllm"].Models = []hostdefaults.Model{{Model: "small", InputPer1M: "1", OutputPer1M: "1"}}
		return c
	})
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "serves only small") || !strings.Contains(err.Error(), "your own apiKey") {
		t.Fatalf("chat = %v, want the model refused naming the host's list", err)
	}
	if n := len(host.authsSeen()) + len(rowFake.authsSeen()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

// A host-listed row the person deleted stays deleted: nothing resolves it,
// the services read says how to have it back, and a row recreated under the
// same id without a key is the host's again.
func TestHostDoesNotReviveADeletedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, host, _ := openHostDataset(t, func(u string) *hostdefaults.Config { return hostChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeProvider, "chatllm", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("chat on a deleted row = %v", err)
	}
	if n := len(host.authsSeen()); n != 0 {
		t.Fatalf("the host's endpoint received %d requests for a deleted row", n)
	}
	got, err := ds.HostServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := hostItem(t, got, "llm/chatllm")
	if item.Source != substrate.HostSourceHost || item.Readiness != substrate.HostReadyNotSetUp || !strings.Contains(item.Message, "create it without an apiKey") {
		t.Fatalf("deleted row's service = %+v", item)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "chatllm", Properties: map[string]any{"wire": "openai"}}); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	pc, err := ds.resolveProvider(ctx, "chatllm")
	if err != nil || pc.host == nil || pc.cfg.APIKey != hostTestKey {
		t.Fatalf("the recreated keyless row = %+v, %v; want the host's transport", pc, err)
	}
}

func hostItem(t *testing.T, s substrate.HostServices, id string) substrate.HostService {
	t.Helper()
	for _, it := range s.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no service %s in %+v", id, s.Items)
	return substrate.HostService{}
}

// The ledger reserves a ceiling against the UTC day's sum, refuses a
// reservation that would pass the budget, settles to the reported cost, keeps
// the ceiling where none was reported, settles a reservation left open (a
// crash) at its ceiling after an hour and never lowers it after, and starts
// again at the next UTC day.
func TestHostLedgerReservesSettlesAndExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds := openInternalDataset(t, WithHostDefaults(&hostdefaults.Config{Budget: hostdefaults.Budget{CentsPerDay: 10}}))
	ds.svc.hostNow = clock.Now
	used := func() float64 {
		t.Helper()
		u, _, _, err := ds.hostSpendToday(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	reserve := func(c float64) *hostReservation {
		t.Helper()
		r, err := ds.reserveHost(ctx, substrate.HostServiceLLM, c)
		if err != nil {
			t.Fatalf("reserve %v: %v", c, err)
		}
		return r
	}
	r1, crashed := reserve(4), reserve(4)
	_, err := ds.reserveHost(ctx, substrate.HostServiceLLM, 4)
	if !errors.Is(err, substrate.ErrHostBudget) || !errors.Is(err, substrate.ErrGuard) ||
		!strings.Contains(err.Error(), "used 8 of the host's 10 cents") || !strings.Contains(err.Error(), "2026-11-07T00:00:00Z") {
		t.Fatalf("a reservation past the budget = %v", err)
	}
	ds.settleHost(ctx, r1, 1)
	if u := used(); u != 5 {
		t.Fatalf("used after a settle = %v, want 5", u)
	}
	r3 := reserve(4)

	// Nothing settles crashed: an hour on, the next reservation settles it,
	// and r3, at their ceilings, and a late settle does not lower them.
	clock.Set(clock.Now().Add(61 * time.Minute))
	reserve(0.5)
	ds.settleHost(ctx, crashed, 0)
	ds.settleHost(ctx, r3, 0)
	if u := used(); u != 9.5 {
		t.Fatalf("used after the expiry = %v, want 9.5", u)
	}
	for _, row := range hostLedger(t, ds)[:3] {
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

// At the host's limit a trigger is held, as decision 0149 holds one at the
// person's cap, and a direct call is refused before a thread opens, whoever
// makes it: an app's token reaches the same engine call.
func TestHostHoldsARunAtTheLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, rowFake, host, _ := openHostDataset(t, func(u string) *hostdefaults.Config { return hostChatConfig(u, "", 1) })
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
	if _, err := ds.reserveHost(ctx, substrate.HostServiceLLM, 0.99); err != nil {
		t.Fatal(err)
	}
	text, err := ds.spendHold(ctx, ag)
	if err != nil || !strings.HasPrefix(text, hostHeldPrefix) || !strings.Contains(text, "of the host's 1 cents") {
		t.Fatalf("hold at the limit = %q, %v", text, err)
	}
	_, err = ds.ChatAgent(ctx, substrate.Actor("app:notes.example.org"), crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	if !errors.Is(err, substrate.ErrHostBudget) || !strings.Contains(err.Error(), "resets at") {
		t.Fatalf("an app's chat at the limit = %v", err)
	}
	if n := len(host.authsSeen()); n != 0 {
		t.Fatalf("the host's endpoint received %d requests at the limit", n)
	}
	if n := threadCountOf(t, ds, crewPackage+"/chatter"); n != 0 {
		t.Fatalf("a refused chat opened %d threads", n)
	}
}

// The host's embeddings entry serves a repository whose rows claim none: the
// drain buys vectors on the host's key, stamped `(host)`, the semantic arm
// reads them, the import's reconcile keeps them as current, and at the limit
// a hybrid search answers by its words while a semantic one is refused. A row
// that claims embedModel without a key of its own is refused.
func TestHostEmbeddingsServeARepositoryWhoseRowsClaimNone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newHostEmbedFake(t)
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds := openInternalDataset(t, WithHostDefaults(hostChatConfig("https://llm.example.com/v1", emb.srv.URL, 1000)))
	ds.svc.hostNow = clock.Now
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
	if provider != hostEmbedProviderID || model != "text-embedding-3-small" {
		t.Fatalf("vector stamped %s/%s", provider, model)
	}
	for _, a := range emb.authsSeen() {
		if a != "Bearer host-embed-key-fixture" {
			t.Fatalf("the embeddings endpoint saw %q", a)
		}
	}
	if ledger := hostLedger(t, ds); len(ledger) == 0 || ledger[0].service != substrate.HostServiceEmbeddings {
		t.Fatalf("ledger = %+v, want an embeddings reservation", ledger)
	}
	res, err := ds.Search(ctx, substrate.SearchInput{Q: "saxophone almanac", Mode: substrate.SearchSemantic})
	if err != nil || len(res.Hits) == 0 || res.Hits[0].Record.ID != book.ID {
		t.Fatalf("semantic search = %+v, %v", res, err)
	}

	// The import's reconcile reads the same pair, so the host's vectors are
	// current and nothing is queued to be bought again.
	if n, err := ds.reconcileEmbeddings(ctx, ds.db, time.Now()); err != nil || n != 0 {
		t.Fatalf("reconcile queued %d, %v; want 0", n, err)
	}
	var vectors int
	if err := ds.db.QueryRow(`SELECT count(*) FROM embeddings`).Scan(&vectors); err != nil || vectors != 1 {
		t.Fatalf("%d vectors after the reconcile, %v", vectors, err)
	}

	// At the limit: hybrid answers by its words and says why, semantic is
	// refused with the host's words.
	spent, _, _, err := ds.hostSpendToday(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.reserveHost(ctx, substrate.HostServiceLLM, 1000-spent); err != nil {
		t.Fatal(err)
	}
	hybrid, err := ds.Search(ctx, substrate.SearchInput{Q: "saxophone almanac", Mode: substrate.SearchHybrid})
	if err != nil || !strings.Contains(hybrid.Note, "host's allowance") {
		t.Fatalf("hybrid at the limit = %+v, %v", hybrid, err)
	}
	if _, err := ds.Search(ctx, substrate.SearchInput{Q: "saxophone almanac", Mode: substrate.SearchSemantic}); !errors.Is(err, substrate.ErrHostBudget) {
		t.Fatalf("semantic at the limit = %v", err)
	}

	// A row that claims embedModel pays with its own key: a keyless one the
	// host lists is refused rather than run on the host's.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "chatllm", Properties: map[string]any{
		"wire": "openai", "baseURL": emb.srv.URL, "embedModel": "text-embedding-3-small",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.resolveEmbedProvider(ctx); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "pays for them with its own key") {
		t.Fatalf("a keyless claiming row = %v", err)
	}
}

// The services read: where each row's credential comes from, whether it
// works, the newest open alert of an agent on the row, the embeddings
// provider, and the day's budget.
func TestHostServicesReportsSourcesAndBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	emb := newHostEmbedFake(t)
	ds, rowFake, _, _ := openHostDataset(t, func(u string) *hostdefaults.Config { return hostChatConfig(u, emb.srv.URL, 200) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: "bare", Properties: map[string]any{"wire": "anthropic"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.reserveHost(ctx, substrate.HostServiceLLM, 36.2); err != nil {
		t.Fatal(err)
	}
	got, err := ds.HostServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host == nil || got.Host.Budget.CentsPerDay != 200 || got.Host.Budget.UsedCents != 37 ||
		!got.Host.Budget.ResetsAt.Equal(time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)) ||
		len(got.Host.Budget.ByService) != 1 || got.Host.Budget.ByService[0].Cents != 37 {
		t.Fatalf("host = %+v", got.Host)
	}
	if it := hostItem(t, got, "llm/chatllm"); it.Source != substrate.HostSourceHost || it.Readiness != substrate.HostReadyReady || len(it.Models) != 1 {
		t.Fatalf("chatllm = %+v", it)
	}
	if it := hostItem(t, got, "llm/rootllm"); it.Source != substrate.HostSourceOwn || it.Readiness != substrate.HostReadyReady || len(it.Models) != 0 {
		t.Fatalf("rootllm = %+v", it)
	}
	if it := hostItem(t, got, "llm/bare"); it.Source != substrate.HostSourceNone || it.Readiness != substrate.HostReadyNotSetUp {
		t.Fatalf("bare = %+v", it)
	}
	if it := hostItem(t, got, "embeddings"); it.Source != substrate.HostSourceHost || it.Readiness != substrate.HostReadyReady || it.Target != "" {
		t.Fatalf("embeddings = %+v", it)
	}

	// An open alert about the chat agent marks its row failing, with the
	// park's error.
	if err := ds.inTx(ctx, substrate.ActorSystem, false, func(tx *txn) error {
		return tx.raiseAlert(substrate.ActorSystem, alertRaise{
			key: "trigger.parked/hostdefaults-test", level: alertLevelError, summary: "Trigger x has parked deliveries",
			detail: "401 invalid api key", about: []string{vocabulary.RecordPath(kindAgent, crewPackage+"/chatter")},
		})
	}); err != nil {
		t.Fatal(err)
	}
	got, err = ds.HostServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if it := hostItem(t, got, "llm/chatllm"); it.Readiness != substrate.HostReadyFailing || it.Message != "401 invalid api key" {
		t.Fatalf("chatllm with an open alert = %+v", it)
	}
}

// Without a host file nothing changes: a keyless row refuses with the words
// it always did, the repository has no embeddings pair, the services read
// names no host, and nothing writes the ledger.
func TestNoHostFileKeepsTodaysBehavior(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": nil})
	_, err := ds.resolveProvider(ctx, "chatllm")
	if !errors.Is(err, substrate.ErrValidation) ||
		err.Error() != `substrate: validation failed: llm/provider row "chatllm" declares no apiKey — a row carries the key for the endpoint it names, and there is no host key` {
		t.Fatalf("a keyless row = %v", err)
	}
	if pair, err := ds.embedPair(ctx, ds.db); err != nil || pair != nil {
		t.Fatalf("embeddings pair = %+v, %v", pair, err)
	}
	if p, err := ds.resolveEmbedProvider(ctx); err != nil || p != nil {
		t.Fatalf("embeddings provider = %+v, %v", p, err)
	}
	got, err := ds.HostServices(ctx)
	if err != nil || got.Host != nil {
		t.Fatalf("services = %+v, %v", got, err)
	}
	if it := hostItem(t, got, "llm/chatllm"); it.Source != substrate.HostSourceNone {
		t.Fatalf("chatllm = %+v", it)
	}
	if it := hostItem(t, got, "embeddings"); it.Source != substrate.HostSourceNone || it.Readiness != substrate.HostReadyNotSetUp {
		t.Fatalf("embeddings = %+v", it)
	}
	fake.script("chat", fakeTurn{content: "ok"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("chat on a keyless row with no host file = %v", err)
	}
	var n int
	if err := ds.db.QueryRow(`SELECT count(*) FROM host_spend_reservations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ledger rows = %d, %v", n, err)
	}
}
