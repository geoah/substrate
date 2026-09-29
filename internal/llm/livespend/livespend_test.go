package livespend

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLedgerRefusesTheRequestPastTheCeiling(t *testing.T) {
	l := New("test", 2, 1000)
	for i := range 2 {
		if err := l.Charge("openai"); err != nil {
			t.Fatalf("request %d of 2: %v", i+1, err)
		}
	}
	if err := l.Charge("anthropic"); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("the third request of a two-request ceiling: %v, want ErrOverBudget", err)
	}
	if err := l.Err(); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("Err after a refusal: %v, want ErrOverBudget", err)
	}
	if got := l.Exit(0, io.Discard); got != 1 {
		t.Fatalf("a pass with a refused request exits %d, want 1", got)
	}
}

func TestLedgerRefusesEveryRequestOnceTheTokensPassTheCeiling(t *testing.T) {
	l := New("test", 10, 100)
	if err := l.Charge("openai"); err != nil {
		t.Fatal(err)
	}
	l.Record("openai", 90, 20)
	if err := l.Charge("openai"); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("a request after 110 of 100 tokens: %v, want ErrOverBudget", err)
	}
	if got := l.Exit(0, io.Discard); got != 1 {
		t.Fatalf("a pass past its token ceiling exits %d, want 1", got)
	}
}

func TestLedgerBookedSpendIsHeldToBothCeilings(t *testing.T) {
	// Book is the after-the-fact door: nothing is refused, so crossing either
	// ceiling there must still fail the pass.
	for _, tc := range []struct {
		name                         string
		requests, prompt, completion int
	}{
		{"requests", 5, 10, 10},
		{"tokens", 1, 80, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := New("test", 4, 100)
			l.Book("anthropic", tc.requests, tc.prompt, tc.completion)
			if err := l.Err(); !errors.Is(err, ErrOverBudget) {
				t.Fatalf("Err: %v, want ErrOverBudget", err)
			}
			if got := l.Exit(0, io.Discard); got != 1 {
				t.Fatalf("exit %d, want 1", got)
			}
		})
	}
}

func TestLedgerWithinBothCeilingsKeepsTheExitCode(t *testing.T) {
	l := New("test", 4, 100)
	l.Book("openai", 4, 60, 40)
	if err := l.Err(); err != nil {
		t.Fatalf("a pass exactly at both ceilings: %v", err)
	}
	if got := l.Exit(0, io.Discard); got != 0 {
		t.Fatalf("exit %d, want 0", got)
	}
	// A failing test's code is never replaced.
	if got := l.Exit(3, io.Discard); got != 3 {
		t.Fatalf("exit %d, want 3", got)
	}
}

func TestLedgerSummaryIsOneTablePerProvider(t *testing.T) {
	l := New("adapter suite", 24, 20000)
	for _, w := range []string{"openai", "anthropic", "openai"} {
		if err := l.Charge(w); err != nil {
			t.Fatal(err)
		}
	}
	l.Record("openai", 30, 5)
	l.Record("openai", 20, 4)
	l.Record("anthropic", 40, 6)

	var out strings.Builder
	if got := l.Exit(0, &out); got != 0 {
		t.Fatalf("exit %d, want 0", got)
	}
	want := MarkerStart + "\n" +
		"### Live LLM spend: adapter suite\n\n" +
		"| Provider | Requests | Prompt tokens | Completion tokens |\n" +
		"| --- | ---: | ---: | ---: |\n" +
		"| anthropic | 1 | 40 | 6 |\n" +
		"| openai | 2 | 50 | 9 |\n" +
		"| total | 3 | 90 | 15 |\n" +
		"\nCeilings: 24 requests, 20000 tokens (prompt and completion together).\n" +
		MarkerEnd + "\n"
	if out.String() != want {
		t.Fatalf("summary:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestLedgerWithNothingBookedPrintsNothing(t *testing.T) {
	// Every hermetic run of a package that holds live cases ends here: nothing
	// bought, so no table and the code untouched.
	l := New("test", 1, 1)
	var out strings.Builder
	if got := l.Exit(0, &out); got != 0 || out.Len() != 0 {
		t.Fatalf("exit %d, output %q; want 0 and nothing", got, out.String())
	}
}
