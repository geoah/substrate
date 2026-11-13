package catalog

import (
	"strings"
	"testing"
)

// A release that adds a kind naming another in movedFrom is held for review
// by a refresh, whether or not the kind it names is one the repository holds
// (decision 0269): it is never an additive change.
func TestARenamingReleaseIsNeverAdditive(t *testing.T) {
	parse := func(body string, v int64) *Bundle {
		t.Helper()
		b, err := ParseClosure([]byte(body), "example.com/notes", v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := parse(publishedClosure("example.com/notes", 1), 1)
	memo := "---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/memo\ndata:\n  authority: example.com\n  package: notes\n  movedFrom: example.com/notes/note\n  names:\n    singular: memo\n  properties:\n    name:\n      type: string\n"
	next := parse(strings.Replace(publishedClosure("example.com/notes", 2), "installs: [example.com/notes/item]", "installs: [example.com/notes/item, example.com/notes/memo]", 1)+memo, 2)
	if narrows, additive := CompareClosures(old, next); narrows || additive {
		t.Fatalf("CompareClosures = narrows %v, additive %v; want a rename held for review, not narrowing", narrows, additive)
	}
	if got := next.KindRenames(); len(got) != 1 || got[0] != "kind example.com/notes/memo: movedFrom example.com/notes/note" {
		t.Fatalf("KindRenames = %q", got)
	}
}
