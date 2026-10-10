package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// spendPackage carries the spend cap's agents: one capped, one capped at
// zero, one with no cap of its own, and one on a provider row with no price.
const spendPackage = "spend.test.dev/spend"

// openSpendDataset provisions a repository for the spend cap: two provider
// rows on a fake server, and spendPackage with capped's cap at 150 cents.
// The fake reports 10 prompt and 5 completion tokens a turn (promptTokens
// moves the first), and pricedllm charges 100000 USD per million prompt
// tokens and nothing for completions, so a default turn costs exactly 1 USD:
// 100 cents.
func openSpendDataset(t *testing.T, opts ...Option) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t, opts...)
	fake := newFakeLLM(t)
	priced := []any{}
	for _, model := range []string{"capped", "zero", "open"} {
		priced = append(priced, map[string]any{"model": model, "inputPer1M": "100000", "outputPer1M": "0"})
	}
	for id, props := range map[string]map[string]any{
		"pricedllm": {"pricing": priced},
		// No pricing at all: every model on this row is unpriced.
		"freellm": {},
	} {
		props["wire"], props["baseURL"], props["apiKey"] = "openai", fake.srv.URL, "row-key-"+id
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeProvider, ID: id, Properties: props}); err != nil {
			t.Fatalf("put llm/provider row %s: %v", id, err)
		}
	}
	applySpendPackage(t, ds, 150)
	return ds, fake
}

// applySpendPackage installs spendPackage with capped's cap at cappedCents.
// Applying it again is how a test raises the cap.
func applySpendPackage(t *testing.T, ds *dataset, cappedCents int) {
	t.Helper()
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(spendPackage, name, data)
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(spendPackage, 0),
		vocabulary.KindManifest(spendPackage, map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		agent("capped", map[string]any{
			"provider": "pricedllm", "model": "capped",
			"budgets": map[string]any{"spendCentsPerDay": cappedCents},
		}),
		agent("zero", map[string]any{
			"provider": "pricedllm", "model": "zero",
			"budgets": map[string]any{"spendCentsPerDay": 0},
		}),
		agent("open", map[string]any{"provider": "pricedllm", "model": "open"}),
		agent("unpriced", map[string]any{
			"provider": "freellm", "model": "free",
			"budgets": map[string]any{"spendCentsPerDay": 1},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install %s: %v", spendPackage, err)
	}
}

// putSpendTrigger points a record trigger on widget creates at one agent.
func putSpendTrigger(t *testing.T, ds *dataset, agent string) string {
	t.Helper()
	rec, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{spendPackage + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath(kindAgent, spendPackage+"/"+agent),
		},
	})
	if err != nil {
		t.Fatalf("put trigger for %s: %v", agent, err)
	}
	return rec.ID
}

// putSpendWidget creates one widget and returns the seq of its change.
func putSpendWidget(t *testing.T, ds *dataset, id string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: spendPackage + "/widget", ID: id, Properties: map[string]any{"name": id},
	}); err != nil {
		t.Fatalf("put widget %s: %v", id, err)
	}
	var seq int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT max(seq) FROM changelog WHERE kind = $1 AND record_id = $2`, spendPackage+"/widget", id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

func spendPass(t *testing.T, ds *dataset) {
	t.Helper()
	if _, err := ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("process: %v", err)
	}
}

func spendStatusOf(t *testing.T, ds *dataset, trigger string) substrate.TriggerStatus {
	t.Helper()
	statuses, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	for _, st := range statuses {
		if st.ID == trigger {
			return st
		}
	}
	t.Fatalf("no status for trigger %s", trigger)
	return substrate.TriggerStatus{}
}

func spendCursorOf(t *testing.T, ds *dataset, trigger string) int64 {
	t.Helper()
	var seq int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, trigger).Scan(&seq); err != nil {
		t.Fatalf("cursor of %s: %v", trigger, err)
	}
	return seq
}

func spendRunsOf(t *testing.T, ds *dataset, trigger string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM records WHERE kind = $1 AND `+referencePathSQL("props", "trigger")+` = $2`,
		typeTriggerRun, vocabulary.RecordPath(typeTrigger, trigger)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// heldAt is the hold an agent of spendPackage reads at its own cap.
func heldAt(agent string, spent, capCents int) string {
	return fmt.Sprintf("spend cap reached: agent %s/%s spent %d of its %d cents in the last 24 hours (budgets.spendCentsPerDay)",
		spendPackage, agent, spent, capCents)
}

// A capped agent stops after its cap: the trigger reads held with the cap's
// reason, the cursor stays on the change still owed, nothing parks, no run
// row is written, the hold is logged once, and raising the cap delivers the
// change at the next pass (#880).
func TestASpendCapHoldsAnAgentTriggerUntilTheCapIsRaised(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	ds, fake := openSpendDataset(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	trigger := putSpendTrigger(t, ds, "capped")
	fake.script("capped", fakeTurn{content: "one"}, fakeTurn{content: "two"}, fakeTurn{content: "three"})

	// 100 cents a run: the second is admitted at 100 of 150.
	for _, id := range []string{"w1", "w2"} {
		putSpendWidget(t, ds, id)
		spendPass(t, ds)
	}
	if n := threadCountOf(t, ds, spendPackage+"/capped"); n != 2 {
		t.Fatalf("threads before the cap: %d, want 2", n)
	}

	// At 200 of 150 the third delivery is held, pass after pass.
	owed := putSpendWidget(t, ds, "w3")
	spendPass(t, ds)
	spendPass(t, ds)
	if n := threadCountOf(t, ds, spendPackage+"/capped"); n != 2 {
		t.Fatalf("a held trigger ran: %d threads", n)
	}
	if n := len(fake.requestsOf("capped")); n != 2 {
		t.Fatalf("model calls: %d, want 2", n)
	}
	st := spendStatusOf(t, ds, trigger)
	if want := heldAt("capped", 200, 150); st.Held != want {
		t.Fatalf("held %q, want %q", st.Held, want)
	}
	if st.Parked != 0 || st.InFlight != 0 {
		t.Fatalf("a hold parked or claimed: %+v", st)
	}
	if cursor := spendCursorOf(t, ds, trigger); cursor >= owed {
		t.Fatalf("the cursor %d moved past the held change %d", cursor, owed)
	}
	if runs := spendRunsOf(t, ds, trigger); runs != 2 {
		t.Fatalf("run rows: %d, want 2; a hold writes none", runs)
	}
	if n := strings.Count(logs.String(), "held at a spend cap"); n != 1 {
		t.Fatalf("the hold logged %d times over two passes, want once", n)
	}

	// Raised to 500, the held change is delivered at the next pass.
	applySpendPackage(t, ds, 500)
	spendPass(t, ds)
	if n := threadCountOf(t, ds, spendPackage+"/capped"); n != 3 {
		t.Fatalf("threads after raising the cap: %d, want 3", n)
	}
	if st := spendStatusOf(t, ds, trigger); st.Held != "" || st.Parked != 0 {
		t.Fatalf("after raising the cap: %+v", st)
	}
	if cursor := spendCursorOf(t, ds, trigger); cursor < owed {
		t.Fatalf("the cursor %d stayed before the delivered change %d", cursor, owed)
	}
	if !strings.Contains(logs.String(), "under its spend cap again") {
		t.Fatal("the resume was not logged")
	}
}

// A zero cap holds every run: a call, a chat, a hand's wake and a trigger
// delivery, before any model call and before any thread opens.
func TestAZeroSpendCapHoldsEveryRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	want := heldAt("zero", 0, 0)
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, errSpendHeld) || !errors.Is(err, substrate.ErrGuard) || !strings.HasSuffix(err.Error(), want) {
			t.Fatalf("%s at a zero cap: %v", what, err)
		}
	}
	_, err := ds.CallAgent(ctx, spendPackage+"/zero", "hello")
	refused("a call", err)
	_, err = ds.ChatAgent(ctx, substrate.ActorAPI, spendPackage+"/zero", "", "hello", nil)
	refused("a chat", err)

	trigger := putSpendTrigger(t, ds, "zero")
	putSpendWidget(t, ds, "w1")
	_, err = ds.WakeTrigger(ctx, trigger)
	refused("a wake", err)
	spendPass(t, ds)
	if st := spendStatusOf(t, ds, trigger); st.Held != want || st.Parked != 0 {
		t.Fatalf("status at a zero cap: %+v", st)
	}
	if n := threadCountOf(t, ds, spendPackage+"/zero"); n != 0 {
		t.Fatalf("threads at a zero cap: %d", n)
	}
	if n := len(fake.requestsOf("zero")); n != 0 {
		t.Fatalf("model calls at a zero cap: %d", n)
	}
}

// The repository's setting caps every agent's runs together, an agent under
// its own cap included; a hand's entry reads a raised value at once; and a
// value that is not a whole number holds every run, saying why.
func TestARepositorySpendCapHoldsEveryAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	fake.script("open", fakeTurn{content: "one"}, fakeTurn{content: "two"})
	setCap := func(value, typ string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: kindSetting, ID: spendSettingID,
			Properties: map[string]any{"value": value, "type": typ},
		}); err != nil {
			t.Fatalf("put the repository cap %q: %v", value, err)
		}
	}
	setCap("50", "int")
	if _, err := ds.CallAgent(ctx, spendPackage+"/open", "go"); err != nil {
		t.Fatalf("a call under the repository cap: %v", err)
	}
	want := "spend cap reached: the repository spent 100 of its 50 cents in the last 24 hours (setting substrate.reamde.dev/llm/spendCentsPerDay)"
	for _, agent := range []string{"open", "capped"} {
		_, err := ds.CallAgent(ctx, spendPackage+"/"+agent, "go")
		if !errors.Is(err, errSpendHeld) || !strings.HasSuffix(err.Error(), want) {
			t.Fatalf("%s at the repository cap: %v", agent, err)
		}
	}
	setCap("1000", "int")
	if _, err := ds.CallAgent(ctx, spendPackage+"/open", "go"); err != nil {
		t.Fatalf("a call after raising the repository cap: %v", err)
	}

	// A row written before the write refused a bad value still holds every
	// run, and says why.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{value}', '"lots"') WHERE kind = $1 AND id = $2`,
		kindSetting, spendSettingID); err != nil {
		t.Fatal(err)
	}
	_, err := ds.CallAgent(ctx, spendPackage+"/open", "go")
	if !errors.Is(err, errSpendHeld) || !strings.Contains(err.Error(), `spend cap unreadable: setting substrate.reamde.dev/llm/spendCentsPerDay holds "lots"`) {
		t.Fatalf("a call under an unreadable repository cap: %v", err)
	}
}

// The repository cap is held to what the engine reads it as, on put and on
// patch: type int and a whole number of cents from 0 to 2^53-1. An empty
// value is no cap and is admitted.
func TestARepositorySpendCapRefusesAValueItCouldNotRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openSpendDataset(t)
	put := func(props map[string]any) error {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kindSetting, ID: spendSettingID, Properties: props})
		return err
	}
	for _, props := range []map[string]any{
		{"value": "-1", "type": "int"},
		{"value": "9007199254740992", "type": "int"},
		{"value": "500", "type": "string"},
		{"value": "lots", "type": "string"},
		{"value": "500"},
	} {
		if err := put(props); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), spendSettingID) {
			t.Errorf("put %v: %v, want a validation error naming the setting", props, err)
		}
	}
	if err := put(map[string]any{"value": "", "type": "int"}); err != nil {
		t.Fatalf("an empty cap: %v", err)
	}
	if err := put(map[string]any{"value": "9007199254740991", "type": "int"}); err != nil {
		t.Fatalf("the largest cap: %v", err)
	}
	for _, props := range []map[string]any{{"value": "-1"}, {"value": "1.5"}, {"type": "string"}} {
		_, err := ds.Patch(ctx, substrate.ActorAPI, kindSetting, spendSettingID, substrate.PatchInput{Properties: props})
		if !errors.Is(err, substrate.ErrValidation) {
			t.Errorf("patch %v: %v, want a validation error", props, err)
		}
	}
	// A setting under another id keeps the ordinary int rule.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kindSetting, ID: "spend.test.dev/spend/offset", Properties: map[string]any{"value": "-1", "type": "int"},
	}); err != nil {
		t.Fatalf("another int setting at -1: %v", err)
	}
}

// A run still in flight counts: its charges are not on any settled thread
// yet, and two admissions made while it runs past the cap are both refused.
// Once it settles, the thread row carries the whole cost.
func TestASpendCapCountsARunStillInFlight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	trigger := putSpendTrigger(t, ds, "capped")
	arrived, release := make(chan struct{}), make(chan struct{})
	// A failure below must not leave the fake's handler blocked: the server's
	// own cleanup, registered earlier and so run later, waits for it.
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	fake.script("capped",
		// 20 prompt tokens: 200 cents, charged when the turn returns. The
		// tool does not exist, so the model reads an error and goes on.
		fakeTurn{calls: []fakeCall{{"lookup", `{}`}}, promptTokens: 20},
		fakeTurn{content: "done", arrived: arrived, release: release},
	)
	first := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, spendPackage+"/capped", "first")
		first <- err
	}()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the first run never reached its second turn")
	}

	want := heldAt("capped", 200, 150)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = ds.CallAgent(ctx, spendPackage+"/capped", "second") })
	}
	wg.Wait()
	for i, err := range errs {
		if !errors.Is(err, errSpendHeld) || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("admission %d while the first run is in flight: %v", i, err)
		}
	}
	putSpendWidget(t, ds, "w1")
	spendPass(t, ds)
	if st := spendStatusOf(t, ds, trigger); st.Held != want {
		t.Fatalf("held %q while the first run is in flight, want %q", st.Held, want)
	}

	unblock()
	if err := <-first; err != nil {
		t.Fatalf("the first run: %v", err)
	}
	// A pass reads the settled sum again: the thread row's 300 cents, counted
	// once.
	spendPass(t, ds)
	if st := spendStatusOf(t, ds, trigger); st.Held != heldAt("capped", 300, 150) {
		t.Fatalf("held %q after the first run settled", st.Held)
	}
	if n := threadCountOf(t, ds, spendPackage+"/capped"); n != 1 {
		t.Fatalf("threads: %d, want 1", n)
	}
}

// A model the provider row has no price for spends 0 against a cap, so a
// cap of one cent never holds it.
func TestAnUnpricedModelSpendsNothingAgainstACap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	fake.script("free", fakeTurn{content: "one"}, fakeTurn{content: "two"}, fakeTurn{content: "three"})
	for i := range 3 {
		res, err := ds.CallAgent(ctx, spendPackage+"/unpriced", "go")
		if err != nil {
			t.Fatalf("unpriced run %d: %v", i, err)
		}
		if res.CostUSD != 0 || res.TotalTokens == 0 {
			t.Fatalf("unpriced run %d: cost %v over %d tokens", i, res.CostUSD, res.TotalTokens)
		}
	}
	ag, err := ds.registry().ResolveAgent(spendPackage + "/unpriced")
	if err != nil {
		t.Fatal(err)
	}
	ds.spend.expire()
	if held, err := ds.spendHold(ctx, ag); err != nil || held != "" {
		t.Fatalf("an unpriced agent reads held %q (%v)", held, err)
	}
}

// The settled sum is over root threads whose finishedAt is inside the window:
// a run that started before the window and finished in it counts, one that
// finished before it does not, a running thread has settled nothing, and a
// sub-agent's thread is already on its root's.
func TestSpendSumsRootThreadsByWhenTheyFinished(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openSpendDataset(t)
	now := nowUTC()
	ago := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339Nano) }
	put := func(id, agent string, props map[string]any) {
		t.Helper()
		props["agent"] = spendPackage + "/" + agent
		props["provider"], props["model"] = "pricedllm", agent
		if _, ok := props["mode"]; !ok {
			props["mode"] = "call"
		}
		if _, ok := props["agentDepth"]; !ok {
			props["agentDepth"] = 0
		}
		if _, ok := props["status"]; !ok {
			props["status"] = threadOK
		}
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: typeThread, ID: id, Properties: props}); err != nil {
			t.Fatalf("put thread %s: %v", id, err)
		}
	}
	put("t-long", "open", map[string]any{"startedAt": ago(48 * time.Hour), "finishedAt": ago(time.Hour), "costUSD": 2.0})
	put("t-old", "open", map[string]any{"startedAt": ago(30 * time.Hour), "finishedAt": ago(26 * time.Hour), "costUSD": 5.0})
	put("t-running", "open", map[string]any{"startedAt": ago(time.Hour), "costUSD": 7.0, "status": threadRunning})
	put("t-child", "capped", map[string]any{
		"startedAt": ago(time.Hour), "finishedAt": ago(time.Hour), "costUSD": 3.0,
		"agentDepth": 1, "mode": agentModeSubagent, "parent": "t-long",
	})
	put("t-capped", "capped", map[string]any{"startedAt": ago(2 * time.Hour), "finishedAt": ago(time.Hour), "costUSD": 0.5})

	sums, err := ds.querySpendSums(ctx, now.Add(-spendWindow))
	if err != nil {
		t.Fatal(err)
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	if got := sums.byAgent[spendPackage+"/open"]; !near(got, 2.0) {
		t.Errorf("open spent %v, want 2: only the thread that finished in the window", got)
	}
	if got := sums.byAgent[spendPackage+"/capped"]; !near(got, 0.5) {
		t.Errorf("capped spent %v, want 0.5: its root thread and not the sub-agent's", got)
	}
	if !near(sums.all, 2.5) {
		t.Errorf("the repository spent %v, want 2.5", sums.all)
	}
	// The snapshot is per thread, so the ledger can tell a run it holds.
	if len(sums.threads) != 2 || sums.threads["t-long"].agent != spendPackage+"/open" || !near(sums.threads["t-capped"].usd, 0.5) {
		t.Errorf("snapshot threads %+v, want t-long and t-capped", sums.threads)
	}
}

// spentOf reads what one agent of spendPackage spent in the window, in
// cents; fresh drops the cached snapshot first, as a dispatcher pass does.
func spentOf(t *testing.T, ds *dataset, agent string, fresh bool) int64 {
	t.Helper()
	if fresh {
		ds.spend.expire()
	}
	ag, err := ds.registry().ResolveAgent(spendPackage + "/" + agent)
	if err != nil {
		t.Fatal(err)
	}
	usd, _, err := ds.spendOf(context.Background(), ag)
	if err != nil {
		t.Fatal(err)
	}
	return spentCents(usd)
}

// panicOnSecond panics on its second completion: a loop that dies after one
// charged turn.
type panicOnSecond struct {
	llm.Client
	calls int
}

func (p *panicOnSecond) Complete(ctx context.Context, req llm.Request, onDelta func(string)) (*llm.Result, error) {
	p.calls++
	if p.calls == 2 {
		panic("the second completion panics")
	}
	return p.Client.Complete(ctx, req, onDelta)
}

// A run whose settle never committed keeps its charges counted: no row will
// ever carry them. A context that ends (a stop) fails the settle, a panic
// skips it, and a thread deleted mid-run takes a settle that writes nothing.
// Each of the three charged 200 cents before it ended, and each stays
// counted after a fresh snapshot.
func TestASpendCapCountsARunWhoseSettleNeverCommitted(t *testing.T) {
	t.Parallel()
	ds, fake := openSpendDataset(t)
	open := spendPackage + "/open"
	// 20 prompt tokens: 200 cents. The tool does not exist, so the loop goes
	// on to a second turn.
	charged := fakeTurn{calls: []fakeCall{{"lookup", `{}`}}, promptTokens: 20}

	// A stop: the context ends while the second turn is out.
	arrived, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	fake.script("open", charged, fakeTurn{content: "never", arrived: arrived, release: release})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, open, "go")
		done <- err
	}()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the run never reached its second turn")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a run whose context ended settled")
	}
	unblock()
	if got := spentOf(t, ds, "open", true); got != 200 {
		t.Fatalf("after a stop: %d cents, want 200", got)
	}

	// A panic: the second completion never returns.
	ds.mu.Lock()
	ds.wrapLLMClient = func(_ llm.Wire, c llm.Client) llm.Client { return &panicOnSecond{Client: c} }
	ds.mu.Unlock()
	fake.script("open", charged)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the loop did not panic")
			}
		}()
		_, _ = ds.CallAgent(context.Background(), open, "go")
	}()
	ds.mu.Lock()
	ds.wrapLLMClient = nil
	ds.mu.Unlock()
	if got := spentOf(t, ds, "open", true); got != 400 {
		t.Fatalf("after a panic: %d cents, want 400", got)
	}

	// A deleted thread: the continuation's settle finds a tombstone and
	// writes nothing. The thread's first turn (100 cents) is on its row,
	// which a tombstone keeps counted; the continuation's 200 are on none.
	fake.script("open", fakeTurn{content: "first"})
	first, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, open, "", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	arrived2, release2 := make(chan struct{}), make(chan struct{})
	unblock2 := sync.OnceFunc(func() { close(release2) })
	t.Cleanup(unblock2)
	fake.script("open", fakeTurn{content: "second", arrived: arrived2, release: release2, promptTokens: 20})
	go func() {
		_, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, open, first.Thread, "more", nil)
		done <- err
	}()
	<-arrived2
	if _, err := ds.Delete(context.Background(), substrate.ActorAPI, typeThread, first.Thread, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	unblock2()
	if err := <-done; err != nil {
		t.Fatalf("the turn on a deleted thread: %v", err)
	}
	if got := spentOf(t, ds, "open", true); got != 700 {
		t.Fatalf("after a settle that wrote nothing: %d cents, want 700", got)
	}
}

// Between a settle's commit and the run leaving the running state, a fresh
// snapshot already holds the thread: the run counts once, not twice.
func TestASpendCapCountsARunOnceBetweenItsCommitAndItsEnd(t *testing.T) {
	t.Parallel()
	ds, fake := openSpendDataset(t)
	fake.script("open", fakeTurn{content: "one"})
	var between int64
	ds.spend.finishing = func(*spendRun) { between = spentOf(t, ds, "open", true) }
	if _, err := ds.CallAgent(context.Background(), spendPackage+"/open", "go"); err != nil {
		t.Fatal(err)
	}
	ds.spend.finishing = nil
	if between != 100 {
		t.Fatalf("between the commit and the end: %d cents, want 100", between)
	}
	if got := spentOf(t, ds, "open", false); got != 100 {
		t.Fatalf("after the end, on the same snapshot: %d cents, want 100", got)
	}
}

// A continued chat whose thread last finished before the window counts its
// whole thread once its turn settles, as the snapshot will: an admission
// right after the settle, on a snapshot read before it, sees the thread's
// lifetime cost and not only the new turn's.
func TestASpendCapCountsAContinuedChatsWholeThreadAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	capped := spendPackage + "/capped"
	fake.script("capped", fakeTurn{content: "long ago"}, fakeTurn{content: "today"})
	first, err := ds.ChatAgent(ctx, substrate.ActorAPI, capped, "", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The first turn finished 30 hours ago and cost 5 USD; this process has
	// long forgotten it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeThread, first.Thread, substrate.PatchInput{Properties: map[string]any{
		"finishedAt": nowUTC().Add(-30 * time.Hour).Format(time.RFC3339Nano), "costUSD": 5.0,
	}}); err != nil {
		t.Fatal(err)
	}
	ds.spend.mu.Lock()
	ds.spend.runs = nil
	ds.spend.mu.Unlock()
	if got := spentOf(t, ds, "capped", true); got != 0 {
		t.Fatalf("before the continuation: %d cents, want 0", got)
	}

	// Between the continuation's settle and its loop's return, a check on the
	// snapshot read before it already sees the whole thread.
	var between int64 = -1
	ds.spend.finishing = func(*spendRun) { between = spentOf(t, ds, "capped", false) }
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, capped, first.Thread, "again", nil); err != nil {
		t.Fatalf("the continuation, admitted at 0 of 150: %v", err)
	}
	ds.spend.finishing = nil
	if between != 600 {
		t.Fatalf("between the settle and the loop's return, on the cached snapshot: %d cents, want 600", between)
	}
	want := heldAt("capped", 600, 150)
	_, err = ds.ChatAgent(ctx, substrate.ActorAPI, capped, first.Thread, "and again", nil)
	if !errors.Is(err, errSpendHeld) || !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("an admission right after the settle: %v, want %q", err, want)
	}
	if got := spentOf(t, ds, "capped", true); got != 600 {
		t.Fatalf("on a fresh snapshot: %d cents, want 600", got)
	}
}

// A cap lets runs through again once what it counted leaves the window, with
// no run in between to end and prune anything: a 50-cent repository cap
// admits a 100-cent run, holds the next, and 25 hours later admits again.
// Both a settled run and one whose settle never committed expire.
func TestASpendCapForgetsSpendThatLeftTheWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	clock := &TestClock{}
	ds.spend.now = clock.Now
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kindSetting, ID: spendSettingID, Properties: map[string]any{"value": "50", "type": "int"},
	}); err != nil {
		t.Fatal(err)
	}
	open := spendPackage + "/open"

	// A settled run of 100 cents, and a lost one of 200: its context ends
	// while its second turn is out.
	fake.script("open", fakeTurn{content: "one"})
	if _, err := ds.CallAgent(ctx, open, "go"); err != nil {
		t.Fatalf("a call under the cap: %v", err)
	}
	arrived, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	// Raised for the lost run only, so it is admitted; lowered again below.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kindSetting, ID: spendSettingID, Properties: map[string]any{"value": "1000", "type": "int"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.script("open", fakeTurn{calls: []fakeCall{{"lookup", `{}`}}, promptTokens: 20},
		fakeTurn{content: "never", arrived: arrived, release: release})
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(runCtx, open, "go")
		done <- err
	}()
	<-arrived
	cancel()
	<-done
	unblock()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kindSetting, ID: spendSettingID, Properties: map[string]any{"value": "50", "type": "int"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ds.CallAgent(ctx, open, "go")
	if !errors.Is(err, errSpendHeld) || !strings.Contains(err.Error(), "the repository spent 300 of its 50 cents") {
		t.Fatalf("a call at the cap: %v", err)
	}

	// 25 hours on, nothing ran and nothing ended: both have left the window.
	clock.Advance(25 * time.Hour)
	if got := spentOf(t, ds, "open", false); got != 0 {
		t.Fatalf("25 hours later: %d cents, want 0", got)
	}
	fake.script("open", fakeTurn{content: "again"})
	if _, err := ds.CallAgent(ctx, open, "go"); err != nil {
		t.Fatalf("a call 25 hours later: %v", err)
	}
}

// GC keeps a tombstoned thread whose settle is inside the window: the row is
// the durable copy of what it cost. A continuation whose thread is deleted
// mid-run and collected keeps both the thread's earlier settled cost and the
// run's own lost charges counted, after the ledger's copy of the settle has
// aged out. A thread that settled before the window is collected as before.
func TestGCKeepsAThreadThatSettledInsideTheSpendWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openSpendDataset(t)
	clock := &TestClock{}
	ds.spend.now = clock.Now
	open := spendPackage + "/open"

	fake.script("open", fakeTurn{content: "first"})
	first, err := ds.ChatAgent(ctx, substrate.ActorAPI, open, "", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	arrived, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	fake.script("open", fakeTurn{content: "second", arrived: arrived, release: release, promptTokens: 20})
	done := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(ctx, substrate.ActorAPI, open, first.Thread, "more", nil)
		done <- err
	}()
	<-arrived
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeThread, first.Thread, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatalf("the turn on a deleted thread: %v", err)
	}

	// An old thread, settled 30 hours ago and deleted, is collectable.
	fake.script("open", fakeTurn{content: "old"})
	old, err := ds.ChatAgent(ctx, substrate.ActorAPI, open, "", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeThread, old.Thread, substrate.PatchInput{Properties: map[string]any{
		"finishedAt": nowUTC().Add(-30 * time.Hour).Format(time.RFC3339Nano),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeThread, old.Thread, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}

	// Past the ledger's two minutes for a settled run, so only the row can
	// carry the first turn's cost.
	clock.Advance(3 * spendViewMaxAge)
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: first.Thread}); err != nil || row == nil || row.DeletedAt == nil {
		t.Fatalf("the thread that settled in the window: %+v %v, want its tombstone kept", row, err)
	}
	if row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: old.Thread}); err != nil || row != nil {
		t.Fatalf("the thread that settled before the window: %+v %v, want it collected", row, err)
	}
	// 100 cents on the kept row, 200 lost by the continuation; the old
	// thread's 100 left the window with it.
	if got := spentOf(t, ds, "open", true); got != 300 {
		t.Fatalf("after GC, on a fresh snapshot: %d cents, want 300", got)
	}
}
