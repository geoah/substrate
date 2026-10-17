package engine

import (
	"strings"
	"testing"
)

func TestCleanTitle(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`"Trip to Berlin".`:             "Trip to Berlin",
		"\n\nTitle: Weekly  plan\nmore": "Weekly plan",
		"“Quoted”":                      "Quoted",
		"   ":                           "",
		strings.Repeat("x", 100):        strings.Repeat("x", threadSubjectRunes),
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
