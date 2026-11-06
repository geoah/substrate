package engine

// A key the vendor refused (401 or 403) reads `failing` on the operator
// defaults read with the vendor and the status, for every repository when it
// is the operator's key and for its own repository alone when it is a
// person's, until a call the vendor accepts. A chat on a refused key ends with
// the plain refusal under ErrGuard, never a validation failure carrying the
// vendor's words, and the embed drain stops calling a refused key every
// minute.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
)

// refusedKeyBody is OpenAI's body for a key it refuses. It quotes no key: the
// scrub has its own tests (agentkeyleak_db_test.go).
const refusedKeyBody = `{"error":{"message":"Incorrect API key provided.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`

// readinessOf reads one item's readiness and message off ds's operator
// defaults read.
func readinessOf(t *testing.T, ds *dataset, id string) (string, string) {
	t.Helper()
	got, err := ds.OperatorDefaults(context.Background())
	if err != nil {
		t.Fatalf("operator defaults: %v", err)
	}
	it := operatorItem(t, got, id)
	return it.Readiness, it.Message
}

func wantReadiness(t *testing.T, ds *dataset, id, readiness, message string) {
	t.Helper()
	gotReadiness, gotMessage := readinessOf(t, ds, id)
	if gotReadiness != readiness || gotMessage != message {
		t.Fatalf("%s reads %q %q, want %q %q", id, gotReadiness, gotMessage, readiness, message)
	}
}

// chatOn runs one chat turn of the crew's chatter and returns its error.
func chatOn(ds *dataset) error {
	_, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, crewPackage+"/chatter",
		substrate.ChatInput{Message: "hi"}, func(substrate.AgentEvent) {})
	return err
}

// The operator's key is one fact for the deployment: a 401 on it reads
// failing in every repository the operator serves, the chat that met it ends
// with the plain refusal under ErrGuard, and the next call the vendor
// accepts, from any repository, reads ready everywhere.
func TestOperatorKeyRefusedFailsEveryRepositoryUntilACallIsAccepted(t *testing.T) {
	t.Parallel()
	ds, rowFake, op, _ := openOperatorDataset(t, func(u string) *operatordefaults.Config { return operatorChatConfig(u, "", 1000) })
	emptyTheChatRow(t, ds, rowFake.srv.URL)
	other := secondRepository(t, ds)
	if err := enginetest.ImportVocabulary(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	emptyTheChatRow(t, other, provisionAgents(t, other).srv.URL)
	for _, d := range []*dataset{ds, other} {
		wantReadiness(t, d, "llm/chatllm", substrate.ReadinessReady, "")
	}

	op.script("chat", fakeTurn{status: http.StatusUnauthorized, errBody: refusedKeyBody})
	err := chatOn(ds)
	const refusal = "Chatllm refused Example's key (401). Add your own key on the Chatllm card under Providers."
	if !errors.Is(err, substrate.ErrGuard) || errors.Is(err, substrate.ErrValidation) || err.Error() != refusal {
		t.Fatalf("chat = %v, want the plain refusal under ErrGuard and not ErrValidation", err)
	}
	for _, d := range []*dataset{ds, other} {
		wantReadiness(t, d, "llm/chatllm", substrate.ReadinessFailing, "Chatllm refused the key (401)")
	}

	op.script("chat", fakeTurn{content: "the key works again"})
	if err := chatOn(other); err != nil {
		t.Fatalf("chat in the other repository: %v", err)
	}
	for _, d := range []*dataset{ds, other} {
		wantReadiness(t, d, "llm/chatllm", substrate.ReadinessReady, "")
	}
}

// A person's own key is their repository's: a 403 on it reads failing there
// and nowhere else, even where another repository's row of the same id points
// at the same vendor. A call the vendor accepts reads ready again, and so does
// a new key written over a refused one, before any call.
func TestOwnKeyRefusedFailsOnlyItsRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	other := secondRepository(t, ds)
	if _, err := other.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "chatllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-chatllm"},
	}); err != nil {
		t.Fatalf("put the other repository's row: %v", err)
	}

	fake.script("chat", fakeTurn{status: http.StatusForbidden, errBody: refusedKeyBody})
	err := chatOn(ds)
	const refusal = "Chatllm refused this key (403). Add a working key on the Chatllm card under Providers."
	if !errors.Is(err, substrate.ErrGuard) || errors.Is(err, substrate.ErrValidation) || err.Error() != refusal {
		t.Fatalf("chat = %v, want the plain refusal under ErrGuard and not ErrValidation", err)
	}
	wantReadiness(t, ds, "llm/chatllm", substrate.ReadinessFailing, "Chatllm refused the key (403)")
	wantReadiness(t, other, "llm/chatllm", substrate.ReadinessReady, "")

	fake.script("chat", fakeTurn{content: "accepted"})
	if err := chatOn(ds); err != nil {
		t.Fatalf("chat: %v", err)
	}
	wantReadiness(t, ds, "llm/chatllm", substrate.ReadinessReady, "")

	fake.script("chat", fakeTurn{status: http.StatusUnauthorized, errBody: refusedKeyBody})
	if err := chatOn(ds); !errors.Is(err, substrate.ErrGuard) {
		t.Fatalf("chat = %v, want the refusal", err)
	}
	wantReadiness(t, ds, "llm/chatllm", substrate.ReadinessFailing, "Chatllm refused the key (401)")
	mustPatchInternal(t, ds, typeProvider, "chatllm", map[string]any{"apiKey": "row-key-replaced"})
	wantReadiness(t, ds, "llm/chatllm", substrate.ReadinessReady, "")
}

// The thread title's call reaches the vendor apart from the turn, after it:
// its refusal reads failing though the turn itself was accepted.
func TestTitleCallRefusedReadsFailing(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ds.mu.Lock()
	ds.skipThreadTitles = false
	ds.mu.Unlock()
	fake.script("chat", fakeTurn{content: "hello"}, fakeTurn{status: http.StatusUnauthorized, errBody: refusedKeyBody})
	if err := chatOn(ds); err != nil {
		t.Fatalf("chat: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		readiness, message := readinessOf(t, ds, "llm/chatllm")
		if readiness == substrate.ReadinessFailing {
			if message != "Chatllm refused the key (401)" {
				t.Fatalf("message = %q", message)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("llm/chatllm still reads %q after the title call was refused", readiness)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A compaction's summarizer call is refused after the turn that triggered it
// was accepted: the thread settles, and the row reads failing.
func TestCompactionCallRefusedReadsFailing(t *testing.T) {
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
	op.script("compact", fakeTurn{content: "r2" + hundred, promptTokens: 900},
		fakeTurn{status: http.StatusUnauthorized, errBody: refusedKeyBody})
	chat(t, ds, res.Thread, "q2"+hundred)
	if reqs := op.requestsOf("compact"); len(reqs) != 3 || !isSummarizerRequest(reqs[2]) {
		t.Fatalf("the operator saw %d requests; want the summary as the third", len(reqs))
	}
	wantReadiness(t, ds, "llm/compactllm", substrate.ReadinessFailing, "Compactllm refused the key (401)")
}

// The embed drain's refusal reads failing on the embeddings slot of every
// repository the operator's entry serves, and the drain backs off: one call
// per backoff step for the whole deployment, a minute after the first
// refusal and doubling, rather than one per repository per pass. A call the
// vendor accepts reads ready, and every queue drains again.
func TestOperatorEmbedRefusalFailsEmbeddingsAndBacksOffTheDrain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	good := newOperatorEmbedFake(t)
	var refuse atomic.Bool
	var calls atomic.Int32
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if refuse.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(refusedKeyBody))
			return
		}
		good.srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(vendor.Close)
	ds := openInternalDataset(t, WithOperatorDefaults(operatorChatConfig("https://llm.example.com/v1", vendor.URL, 1000), "Example"))
	clock := &testClock{now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	ds.svc.operatorNow = clock.Now
	other := secondRepository(t, ds)
	if err := enginetest.ImportVocabulary(ctx, other, "people"); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*dataset{ds, other} {
		if err := enginetest.InstallShelf(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: "shelf.test.dev/shelf/book", Properties: map[string]any{"title": "Atlas", "description": "saxophone almanac chapter"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	drain := func(d *dataset, wantN int, wantErr bool, wantCalls int32) {
		t.Helper()
		n, err := d.ProcessEmbedQueue(ctx, 20)
		if n != wantN || (err != nil) != wantErr || calls.Load() != wantCalls {
			t.Fatalf("drain = %d, %v after %d calls to the vendor; want %d, error %v, %d calls", n, err, calls.Load(), wantN, wantErr, wantCalls)
		}
	}
	at := func(d time.Duration) { clock.Set(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC).Add(d)) }

	refuse.Store(true)
	drain(ds, 0, true, 1)
	for _, d := range []*dataset{ds, other} {
		wantReadiness(t, d, substrate.SlotEmbeddings, substrate.ReadinessFailing, "Vectors refused the key (401)")
	}
	drain(other, 0, false, 1)
	at(30 * time.Second)
	drain(ds, 0, false, 1)

	// The first step ends: one drain asks again, and the step doubles.
	at(61 * time.Second)
	drain(other, 0, true, 2)
	drain(ds, 0, false, 2)
	at(61*time.Second + 61*time.Second)
	drain(ds, 0, false, 2)

	refuse.Store(false)
	at(61*time.Second + 121*time.Second)
	drain(ds, 1, false, 3)
	for _, d := range []*dataset{ds, other} {
		wantReadiness(t, d, substrate.SlotEmbeddings, substrate.ReadinessReady, "")
	}
	drain(other, 1, false, 4)
}
