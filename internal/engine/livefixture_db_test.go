package engine

// THE LIVE CASES' FIXTURES HOLD WITHOUT A KEY (#847). TestLiveCoachPersonas
// and TestLiveAgentChainAcrossWires buy real completions, so they run only
// where the keys are, weekly, and a fixture that rotted failed there and
// nowhere else (the coach's routine under a chosen id, which its kind
// refuses). Each case's fixture is its own function, and the tests below run
// it as the case does, with the case's first call made against a local
// endpoint that answers it with an error neither wire retries: the fixture
// is built, the first call reaches the wire, nothing is bought.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// firstCallEndpoint is a provider's endpoint that refuses every completion,
// recording each request's path and the model it names.
type firstCallEndpoint struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []firstCallRequest
}

type firstCallRequest struct {
	path, model string
}

func newFirstCallEndpoint(t *testing.T) *firstCallEndpoint {
	t.Helper()
	f := &firstCallEndpoint{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.seen = append(f.seen, firstCallRequest{path: r.URL.Path, model: body.Model})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"the fixture run ends a case at its first completion"}}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// first is the first request the endpoint saw, failing the test when it saw
// none.
func (f *firstCallEndpoint) first(t *testing.T) firstCallRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		t.Fatal("the case's first call never reached its provider")
	}
	return f.seen[0]
}

func TestTheCoachPersonasSeedAndReachTheirFirstCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	endpoint := newFirstCallEndpoint(t)
	c := seedCoachPersonas(t, ds, endpoint.srv.URL, "fixture-key")
	// The call ends on the endpoint's refusal; what it answers is the
	// endpoint's, not the case's.
	_, _ = ds.CallAgent(ctx, c.agent, coachPersonaCalls[0].say)
	if got := endpoint.first(t); got.path != "/v1/messages" || got.model != liveAnthropicModel {
		t.Fatalf("the coach's first call went to %s naming %q, want /v1/messages naming %q", got.path, got.model, liveAnthropicModel)
	}
	after, err := ds.Get(ctx, c.routine, c.upper.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != c.upper.Version {
		t.Fatalf("the routine moved from version %d to %d", c.upper.Version, after.Version)
	}
}

func TestTheLiveAgentChainInstallsAndReachesItsFirstCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	endpoint := newFirstCallEndpoint(t)
	openai := liveChainWire{key: "fixture-key", baseURL: endpoint.srv.URL + "/v1", model: liveOpenAIModel}
	anthropic := liveChainWire{key: "fixture-key", baseURL: endpoint.srv.URL, model: liveAnthropicModel}
	installLiveChain(t, ds, openai, anthropic)
	_, _ = ds.CallAgent(ctx, crewPackage+"/conductor", "Run the procedure.")
	if got := endpoint.first(t); got.path != "/v1/messages" || got.model != liveAnthropicModel {
		t.Fatalf("the conductor's first call went to %s naming %q, want /v1/messages naming %q", got.path, got.model, liveAnthropicModel)
	}
}
