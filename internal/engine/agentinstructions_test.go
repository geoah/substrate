package engine

import (
	"fmt"
	"strings"
	"testing"
)

// The block is capped: the instructions that do not fit are counted, never
// cut mid-line, and the block still closes.
func TestRenderInstructionsHoldsItsCap(t *testing.T) {
	if got := renderInstructions(nil, 1024); got != "" {
		t.Fatalf("no instructions rendered %q", got)
	}
	lines := make([]string, 0, 40)
	for i := range 40 {
		lines = append(lines, fmt.Sprintf(`{"id":"i%02d","text":"%s"}`, i, strings.Repeat("x", 60)))
	}
	got := renderInstructions(lines, 1024)
	if len(got) > 1024 {
		t.Fatalf("block is %d bytes, over its 1024 cap", len(got))
	}
	if !strings.HasPrefix(got, instructionsOpen+"\n") || !strings.HasSuffix(got, instructionsClose) {
		t.Fatalf("block is not closed:\n%s", got)
	}
	if !strings.Contains(got, `{"id":"i00"`) || !strings.Contains(got, "more instructions left out") {
		t.Fatalf("want the oldest first and the rest counted:\n%s", got)
	}
}
