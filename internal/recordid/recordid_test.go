package recordid

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	for _, s := range []string{
		"8f2c41d07a", "01h2xcejqtf2nbrexx3vqjhp42", "a", "A.b_c~d:e@f/g-h",
		"~/tasks/task", "samples.substrate.reamde.dev/tasks/task", strings.Repeat("x", MaxLen),
	} {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"", "-a", ".a", "~a", "~/", "a b", "a%2Fb", "a#b", "a?b", "a&b", "a\nb", "é",
		strings.Repeat("x", MaxLen+1),
	} {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
