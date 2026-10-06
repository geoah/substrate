// Package livespend is the live LLM suite's spend ledger: the requests and
// tokens one test binary's live cases bought, per provider, held to a request
// ceiling and a token ceiling.
//
// liveMaxTokens in internal/llm bounds one answer and cannot bound a pass: a
// case that loops, a retry added to an adapter or a wire added to the table
// multiplies requests that each stay small. The ledger counts the whole pass.
// Both halves charge every request before it is sent and record its usage
// after: the adapter suite (internal/llm) around the clients it builds, the
// agent chain (internal/engine) around every client the agent loop builds.
//
// It imports nothing from internal/llm: that package's own tests use it, and
// an import back would be a cycle.
package livespend

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// ErrOverBudget is the refusal Charge returns in place of a request.
var ErrOverBudget = errors.New("live spend: over budget")

// The markers ci:llm (.mise/llmlive.sh) lifts every summary between into the
// CI run summary.
const (
	MarkerStart = "<!-- live-spend -->"
	MarkerEnd   = "<!-- /live-spend -->"
)

// Ledger is one test binary's spend. The zero value is not usable; New builds
// one.
type Ledger struct {
	title       string
	maxRequests int
	maxTokens   int

	mu      sync.Mutex
	wires   map[string]*spend
	refused int
}

type spend struct {
	requests, prompt, completion int
}

// New is a ledger that admits at most maxRequests requests and refuses every
// request once the booked tokens pass maxTokens. The title heads its summary.
func New(title string, maxRequests, maxTokens int) *Ledger {
	return &Ledger{title: title, maxRequests: maxRequests, maxTokens: maxTokens, wires: map[string]*spend{}}
}

func (l *Ledger) wire(name string) *spend {
	s := l.wires[name]
	if s == nil {
		s = &spend{}
		l.wires[name] = s
	}
	return s
}

// totals must be called with mu held.
func (l *Ledger) totals() (requests, tokens int) {
	for _, s := range l.wires {
		requests += s.requests
		tokens += s.prompt + s.completion
	}
	return requests, tokens
}

// Charge admits one request on a wire before it is sent, or refuses it with
// ErrOverBudget when it would be the request past the ceiling or the tokens
// already booked passed theirs. A refused request is never sent, so the
// request ceiling costs nothing to enforce; the token ceiling can only be read
// after an answer reports its usage, so the request that crosses it is paid
// for and the next one is refused.
func (l *Ledger) Charge(wire string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	requests, tokens := l.totals()
	if requests >= l.maxRequests || tokens > l.maxTokens {
		l.refused++
		return fmt.Errorf("%w: %s request refused and not sent: %d requests and %d tokens booked, ceilings %d and %d",
			ErrOverBudget, wire, requests, tokens, l.maxRequests, l.maxTokens)
	}
	l.wire(wire).requests++
	return nil
}

// Record books the usage one admitted request reported.
func (l *Ledger) Record(wire string, prompt, completion int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.wire(wire)
	s.prompt += prompt
	s.completion += completion
}

// Book records spend measured after the fact: requests that were not charged
// through this ledger, and their tokens.
func (l *Ledger) Book(wire string, requests, prompt, completion int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.wire(wire)
	s.requests += requests
	s.prompt += prompt
	s.completion += completion
}

// Err is non-nil when the pass crossed a ceiling: a request was refused, more
// requests were booked than the ceiling admits, or the tokens passed theirs.
func (l *Ledger) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.errLocked()
}

func (l *Ledger) errLocked() error {
	requests, tokens := l.totals()
	var errs []error
	if l.refused > 0 {
		errs = append(errs, fmt.Errorf("%w: %d requests refused and not sent", ErrOverBudget, l.refused))
	} else if requests > l.maxRequests {
		errs = append(errs, fmt.Errorf("%w: %d requests booked past the ceiling of %d", ErrOverBudget, requests, l.maxRequests))
	}
	if tokens > l.maxTokens {
		errs = append(errs, fmt.Errorf("%w: %d tokens booked past the ceiling of %d", ErrOverBudget, tokens, l.maxTokens))
	}
	return errors.Join(errs...)
}

// Summary is the per-provider table between the markers, or "" when nothing
// was booked, so a pass where every live case skipped prints nothing.
func (l *Ledger) Summary() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.wires) == 0 && l.refused == 0 {
		return ""
	}
	names := make([]string, 0, len(l.wires))
	for name := range l.wires {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(MarkerStart + "\n")
	fmt.Fprintf(&b, "### Live LLM spend: %s\n\n", l.title)
	b.WriteString("| Provider | Requests | Prompt tokens | Completion tokens |\n")
	b.WriteString("| --- | ---: | ---: | ---: |\n")
	var total spend
	for _, name := range names {
		s := l.wires[name]
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", name, s.requests, s.prompt, s.completion)
		total.requests += s.requests
		total.prompt += s.prompt
		total.completion += s.completion
	}
	fmt.Fprintf(&b, "| total | %d | %d | %d |\n", total.requests, total.prompt, total.completion)
	fmt.Fprintf(&b, "\nCeilings: %d requests, %d tokens (prompt and completion together).\n", l.maxRequests, l.maxTokens)
	if l.refused > 0 {
		fmt.Fprintf(&b, "Refused and not sent: %d.\n", l.refused)
	}
	if err := l.errLocked(); err != nil {
		fmt.Fprintf(&b, "\n%s\n", strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	b.WriteString(MarkerEnd + "\n")
	return b.String()
}

// Exit is the end of a test binary's run: it writes the summary to w and
// returns the exit code, 1 in place of 0 when the pass crossed a ceiling. A
// TestMain passes m.Run's code through it.
func (l *Ledger) Exit(code int, w io.Writer) int {
	if s := l.Summary(); s != "" {
		_, _ = io.WriteString(w, s)
	}
	if code == 0 && l.Err() != nil {
		return 1
	}
	return code
}
