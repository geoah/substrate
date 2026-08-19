package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// THE GRAMMAR INVARIANT SplitRecordPath rests on: an authority always carries a
// dot, a kind NAME never does. Everything else in this file assumes it, and a
// validator that ever admitted a dotless authority or a dotted name would make
// a flat reference value ambiguous — silently, because the parse would keep
// answering, just wrongly. So it is asserted against the validators themselves
// rather than restated in a comment.
func TestKindGrammarSeparatesAuthorityFromName(t *testing.T) {
	for _, s := range []string{"core", "substrate", "a", "z9", "core-x"} {
		if vocabulary.ValidAuthority(s) {
			t.Errorf("ValidAuthority(%q) = true: a dotless authority makes <kind>/<id> ambiguous", s)
		}
	}
	for _, s := range []string{"core.substrate.reamde.dev", "example.com", "a.b"} {
		if !vocabulary.ValidAuthority(s) {
			t.Errorf("ValidAuthority(%q) = false", s)
		}
	}
	for _, s := range []string{"task", "llmprovider", "person2"} {
		if !vocabulary.ValidName(s) {
			t.Errorf("ValidName(%q) = false", s)
		}
	}
	for _, s := range []string{"my.kind", "a.b", "task/sub", "Task"} {
		if vocabulary.ValidName(s) {
			t.Errorf("ValidName(%q) = true: a dotted or slashed kind name makes <kind>/<id> ambiguous", s)
		}
	}
}

func TestSplitRecordPath(t *testing.T) {
	for name, tc := range map[string]struct {
		path string
		kind string
		id   string
		ok   bool
	}{
		"qualified kind": {
			path: "core.substrate.reamde.dev/llmprovider/claude",
			kind: "core.substrate.reamde.dev/llmprovider", id: "claude", ok: true,
		},
		// A declaration record's id is a kind reference, so its path has four
		// segments and the id keeps its own slash.
		"declaration record id": {
			path: "core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task",
			kind: "core.substrate.reamde.dev/kind", id: "tasks.substrate.reamde.dev/task", ok: true,
		},
		"qualified kind, id with slashes": {
			path: "core.substrate.reamde.dev/note/a/b/c",
			kind: "core.substrate.reamde.dev/note", id: "a/b/c", ok: true,
		},
		// Every kind carries an authority (decision 0042), so a dotless first
		// segment is no stored path: a bare "<name>/<id>" like "task/abc123"
		// never names a stored record.
		"dotless first segment is not a path": {path: "task/abc123"},
		// The authored short form of a reference to a declaration: a bare id
		// that LOOKS like a path but has no id left over. It must not parse, or
		// the pin could never complete it.
		"bare declaration id is not a path": {path: "tasks.substrate.reamde.dev/task"},
		"bare id":                           {path: "claude"},
		"empty":                             {path: ""},
		"leading slash":                     {path: "/claude"},
		"trailing slash":                    {path: "task/"},
		"authority alone":                   {path: "core.substrate.reamde.dev/"},
	} {
		t.Run(name, func(t *testing.T) {
			kind, id, ok := vocabulary.SplitRecordPath(tc.path)
			if ok != tc.ok || kind != tc.kind || id != tc.id {
				t.Fatalf("SplitRecordPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.path, kind, id, ok, tc.kind, tc.id, tc.ok)
			}
			if tc.ok && vocabulary.RecordPath(kind, id) != tc.path {
				t.Errorf("RecordPath does not round-trip %q", tc.path)
			}
		})
	}
}
