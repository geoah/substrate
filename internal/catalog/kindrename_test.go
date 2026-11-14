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

// A closure a document serves names no package in `~`, which is the
// installing repository's own namespace: the parse refuses it, naming each
// name, and the admission a publishing repository runs on its releases
// (AdmitClosure) refuses it and a renamed kind the same way.
func TestAClosureThatNamesTheLocalNamespaceIsRefused(t *testing.T) {
	note := "---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/memo\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: memo\n  properties:\n    task:\n      type: reference\n      kind: ~/tasks/task\n"
	body := strings.Replace(publishedClosure("example.com/notes", 1), "installs: [example.com/notes/item]", "installs: [example.com/notes/item, example.com/notes/memo]", 1) + note
	if _, err := ParseClosure([]byte(body), "example.com/notes", 1); err == nil || !strings.Contains(err.Error(), "~/tasks/task") {
		t.Fatalf("ParseClosure = %v, want the closure refused naming ~/tasks/task", err)
	}
	if _, _, err := AdmitClosure([]byte(body), "example.com/notes", 1); err == nil || !strings.Contains(err.Error(), "local namespace") {
		t.Fatalf("AdmitClosure = %v, want the local name refused", err)
	}
	memo := "---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/memo\ndata:\n  authority: example.com\n  package: notes\n  movedFrom: example.com/notes/note\n  names:\n    singular: memo\n  properties:\n    name:\n      type: string\n"
	renamed := strings.Replace(publishedClosure("example.com/notes", 1), "installs: [example.com/notes/item]", "installs: [example.com/notes/item, example.com/notes/memo]", 1) + memo
	if _, _, err := AdmitClosure([]byte(renamed), "example.com/notes", 1); err == nil || !strings.Contains(err.Error(), "movedFrom") && !strings.Contains(err.Error(), "rename") {
		t.Fatalf("AdmitClosure of a renaming closure = %v, want it refused", err)
	}
	vocab, data, err := AdmitClosure([]byte(publishedClosure("example.com/notes", 1)), "example.com/notes", 1)
	if err != nil || len(vocab) == 0 || len(data) != 0 {
		t.Fatalf("AdmitClosure of a plain closure = %d declarations, %d records, %v", len(vocab), len(data), err)
	}
}
