package llm

// The live suite's SPEND LEDGER: what one pass bought, and the two ceilings it
// may not cross.
//
// The per-request cap (liveMaxTokens) bounds one answer; it cannot bound a
// pass. A case that loops, a table that grows a wire, a retry added to an
// adapter — each multiplies requests while every individual request stays
// obediently small, and the bill is the product. So the ledger counts the
// whole pass and refuses past a ceiling.
//
// The request ceiling is enforced BEFORE the call, so crossing it costs
// nothing: the ledger returns an error instead of buying the completion. The
// token ceiling can only be checked after the fact — tokens are what the
// answer reports — so it fails the run at the end, after the money is already
// spent. That asymmetry is why the request ceiling is the tight one.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	// liveMaxRequests is the whole pass's request budget, across every wire.
	// The suite makes nine; the headroom is for a case or a wire being added,
	// not for a loop.
	liveMaxRequests = 24

	// liveTokenCeiling is the pass's cumulative token budget, prompt and
	// completion together. Nine requests capped at 256 completion tokens each
	// land near 4k, so crossing 20k means something is asking for far more
	// than this suite ever should.
	liveTokenCeiling = 20_000
)

// liveLedger is the pass's ledger. Package-level because the thing being
// bounded is the PASS, not any one case: a per-test budget cannot see a
// second test spending the same money.
var liveLedger = &liveSpend{
	requests:   map[string]int{},
	prompt:     map[string]int{},
	completion: map[string]int{},
}

type liveSpend struct {
	mu         sync.Mutex
	requests   map[string]int
	prompt     map[string]int
	completion map[string]int
}

// charge books one request against the budget and reports whether it may
// proceed. It counts the refused request too, so the summary shows the
// overrun rather than a number sitting exactly on the ceiling.
func (l *liveSpend) charge(wire string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests[wire]++
	total := 0
	for _, n := range l.requests {
		total += n
	}
	if total > liveMaxRequests {
		return fmt.Errorf(
			"live spend: request %d crosses the pass ceiling of %d and was NOT sent — "+
				"a case is looping, or the suite grew and %s needs raising deliberately",
			total, liveMaxRequests, "liveMaxRequests")
	}
	return nil
}

func (l *liveSpend) record(wire string, u *Usage) {
	if u == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prompt[wire] += u.PromptTokens
	l.completion[wire] += u.CompletionTokens
}

func (l *liveSpend) totals() (requests, tokens int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.requests {
		requests += n
	}
	for _, n := range l.prompt {
		tokens += n
	}
	for _, n := range l.completion {
		tokens += n
	}
	return requests, tokens
}

// summary is the per-provider table the CI job lifts into its run summary. The
// markers are what `ci:llm` greps for, so it can append exactly this block to
// $GITHUB_STEP_SUMMARY without knowing anything about Go's test output.
//
// It reports the ADAPTER half only. The engine's chain case lives in another
// package and cannot reach this ledger; what it spent is asserted on the
// thread rows it writes, which is the accounting that matters there.
func (l *liveSpend) summary() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.requests) == 0 {
		return ""
	}
	wires := make([]string, 0, len(l.requests))
	for w := range l.requests {
		wires = append(wires, w)
	}
	sort.Strings(wires)

	var b strings.Builder
	b.WriteString("<!-- live-spend -->\n")
	b.WriteString("### Live LLM spend (adapter suite)\n\n")
	b.WriteString("| Provider | Requests | Prompt tokens | Completion tokens |\n")
	b.WriteString("| --- | ---: | ---: | ---: |\n")
	for _, w := range wires {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", w, l.requests[w], l.prompt[w], l.completion[w])
	}
	b.WriteString("<!-- /live-spend -->\n")
	return b.String()
}

// liveMeter is a Client that books every completion before delegating. Client
// is one method, so metering a wire costs exactly this.
type liveMeter struct {
	wire string
	Client
}

func (m liveMeter) Complete(ctx context.Context, req Request, onDelta func(string)) (*Result, error) {
	if err := liveLedger.charge(m.wire); err != nil {
		return nil, err
	}
	res, err := m.Client.Complete(ctx, req, onDelta)
	if res != nil {
		liveLedger.record(m.wire, res.Usage)
	}
	return res, err
}

// TestMain prints the pass's spend and holds it to the token ceiling. It is a
// no-op for every hermetic case in this package: nothing bought, nothing
// booked, empty summary.
func TestMain(m *testing.M) {
	code := m.Run()
	if s := liveLedger.summary(); s != "" {
		fmt.Print(s)
		requests, tokens := liveLedger.totals()
		fmt.Printf("live spend: %d requests, %d tokens\n", requests, tokens)
		if tokens > liveTokenCeiling {
			fmt.Printf("live spend: %d tokens crosses the pass ceiling of %d\n", tokens, liveTokenCeiling)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}
