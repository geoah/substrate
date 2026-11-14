package llm

// EVERY LIVE CASE'S FIXTURE HOLDS WITHOUT A KEY (#847). The live cases buy
// real completions, so they run only where the keys are, weekly, and a case
// whose setup rotted fails there and nowhere else. This runs every one of
// them up to its first completion, on every run: the completion goes through
// the wire's real client, request built and checked as the live run builds
// it, to a local endpoint that answers it with an error, and the case stops
// there, skipped. Nothing is bought and no key is read.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// liveFixtureRun is the test that runs the live cases up to their first
// completion; a case is in that run when its test's name starts with it.
const liveFixtureRun = "TestEveryLiveCaseReachesItsFirstCall"

// liveFixture is that run's local endpoint, set before its cases start.
var liveFixture *liveFixtureEndpoint

// liveFixtureOf answers the endpoint a case's completions go to in the
// fixture run, and nil in any other.
func liveFixtureOf(t *testing.T) *liveFixtureEndpoint {
	if !strings.HasPrefix(t.Name(), liveFixtureRun+"/") {
		return nil
	}
	return liveFixture
}

// liveFixtureEndpoint answers every completion with an error neither wire
// retries, and counts what reached it, by case.
type liveFixtureEndpoint struct {
	srv     *httptest.Server
	mu      sync.Mutex
	got     int
	reached map[string]bool
}

func newLiveFixtureEndpoint(t *testing.T) *liveFixtureEndpoint {
	t.Helper()
	f := &liveFixtureEndpoint{reached: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.got++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"the fixture run ends a case at its first completion"}}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// url is the base URL a wire's client sends to: the OpenAI wire's carries
// its version, as its own endpoint's does.
func (f *liveFixtureEndpoint) url(wire Wire) string {
	if wire == WireOpenAI {
		return f.srv.URL + "/v1"
	}
	return f.srv.URL
}

func (f *liveFixtureEndpoint) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got
}

// liveFirstCall ends a case at its first completion: it sends it through the
// wire's real client to the fixture's endpoint, fails the case when nothing
// reached it, and otherwise records the case and skips the rest of it.
type liveFirstCall struct {
	t       *testing.T
	fixture *liveFixtureEndpoint
	Client
}

func (c liveFirstCall) Complete(ctx context.Context, req Request, onDelta func(Delta)) (*Result, error) {
	before := c.fixture.requests()
	_, err := c.Client.Complete(ctx, req, onDelta)
	if c.fixture.requests() == before {
		c.t.Fatalf("the case's first completion never reached the wire: %v", err)
	}
	name := strings.Split(c.t.Name(), "/")[1]
	c.fixture.mu.Lock()
	c.fixture.reached[name] = true
	c.fixture.mu.Unlock()
	c.t.SkipNow()
	return nil, nil
}

// liveCases is every live case of the package, by name.
var liveCases = map[string]func(*testing.T){
	"TestLiveCompletion":                              TestLiveCompletion,
	"TestLiveToolRoundTrip":                           TestLiveToolRoundTrip,
	"TestLiveStreaming":                               TestLiveStreaming,
	"TestLiveAnthropicConsecutiveUserTurns":           TestLiveAnthropicConsecutiveUserTurns,
	"TestLiveAnthropicReadsTheRunsPrefixFromTheCache": TestLiveAnthropicReadsTheRunsPrefixFromTheCache,
	"TestLiveOpenAIAcceptsThePromptCacheKey":          TestLiveOpenAIAcceptsThePromptCacheKey,
	"TestLiveReplayedToolExchange":                    TestLiveReplayedToolExchange,
}

func TestEveryLiveCaseReachesItsFirstCall(t *testing.T) {
	declared := liveCasesDeclared(t)
	for _, name := range declared {
		if liveCases[name] == nil {
			t.Errorf("%s is a live case liveCases does not list: add it, so its fixture runs here", name)
		}
	}
	for name := range liveCases {
		if !slices.Contains(declared, name) {
			t.Errorf("liveCases lists %s, which no test file declares", name)
		}
	}
	liveFixture = newLiveFixtureEndpoint(t)
	names := make([]string, 0, len(liveCases))
	for name := range liveCases {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t.Run(name, liveCases[name])
	}
	for _, name := range names {
		if !liveFixture.reached[name] {
			t.Errorf("%s never reached its first completion", name)
		}
	}
}

// liveCasesDeclared is every TestLive function the package's test files
// declare.
func liveCasesDeclared(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "TestLive") {
				out = append(out, fn.Name.Name)
			}
		}
	}
	slices.Sort(out)
	return out
}
