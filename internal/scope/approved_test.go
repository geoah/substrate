package scope

import (
	"testing"
)

// What an approval covers of the code it installs (decision 0215): a write
// covers a read, a glob what it matches and a glob no wider, an internal
// kind its exact scope alone, the auth kinds nothing, a write of core or
// llm nothing but full access, and full access everything.
func TestAnApprovalCoversAReachAsAGrantCoversAScope(t *testing.T) {
	approved, err := ApprovalOf([]string{
		"write:ada.example.com/tasks/*", "read:acme.example.com/*", "read:substrate.reamde.dev/core/setting",
		"write:substrate.reamde.dev/health/sleep",
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		pattern         string
		write, internal bool
		want            bool
	}{
		"a kind under a write glob":           {pattern: "ada.example.com/tasks/task", write: true, want: true},
		"a read under a write glob":           {pattern: "ada.example.com/tasks/task", want: true},
		"a glob no wider than the held one":   {pattern: "ada.example.com/tasks/*", write: true, want: true},
		"a glob wider than the held one":      {pattern: "ada.example.com/*", want: false},
		"a glob under a read glob":            {pattern: "acme.example.com/stock/*", want: true},
		"a write under a read glob":           {pattern: "acme.example.com/stock/item", write: true, want: false},
		"an internal kind under a glob":       {pattern: "acme.example.com/stock/item", internal: true, want: false},
		"an internal kind by its exact scope": {pattern: "substrate.reamde.dev/core/setting", internal: true, want: true},
		"the exact write":                     {pattern: "substrate.reamde.dev/health/sleep", write: true, want: true},
		"a sibling of the exact write":        {pattern: "substrate.reamde.dev/health/workout", want: false},
		"an auth kind":                        {pattern: "substrate.reamde.dev/core/token", want: false},
		"a write of llm":                      {pattern: "substrate.reamde.dev/llm/memory", write: true, want: false},
		"nothing like it":                     {pattern: "x.example.com/y/z", want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := approved.Covers(c.pattern, c.write, Kind{Internal: c.internal}); got != c.want {
				t.Fatalf("Covers(%q, write %v, internal %v) = %v, want %v", c.pattern, c.write, c.internal, got, c.want)
			}
		})
	}
	// Full covers everything, the zero value nothing, and a grant what its
	// token does.
	full, err := ApprovalOf([]string{"full"})
	if err != nil || !full.Full() || !full.Covers("substrate.reamde.dev/llm/memory", true, Kind{Internal: true}) || !full.Covers("substrate.reamde.dev/core/token", false, Kind{Internal: true}) {
		t.Fatalf("full = %+v, %v", full, err)
	}
	if none, err := ApprovalOf(nil); err != nil || none.Covers("ada.example.com/tasks/task", false, Kind{Internal: false}) {
		t.Fatalf("an empty approval covers something, %v", err)
	}
	if (Approved{}).Covers("ada.example.com/tasks/task", false, Kind{Internal: false}) {
		t.Fatal("the zero value covers something")
	}
	g, _ := NewGrant([]string{"read:ada.example.com/tasks/*"})
	if from := ApprovalFrom(g); !from.Covers("ada.example.com/tasks/task", false, Kind{Internal: false}) || from.Covers("ada.example.com/tasks/task", true, Kind{Internal: false}) {
		t.Fatal("a grant's approval is not the grant's reach")
	}
	// An approval holds the stored grammar: no `full` beside another. The
	// repository's own kinds are `~`, which it stores as asked.
	if _, err := ApprovalOf([]string{"read:~/tasks/*"}); err != nil {
		t.Fatalf("ApprovalOf over the repository's own kinds refused: %v", err)
	}
	for _, bad := range [][]string{{"full", "read:ada.example.com/tasks/*"}, {"nonsense"}} {
		if _, err := ApprovalOf(bad); err == nil {
			t.Fatalf("ApprovalOf(%v) admitted", bad)
		}
	}
}

// The scope a block names is the one that would cover the reach, and full
// access where no scope can.
func TestTheCoveringScopeIsNamedForTheOwner(t *testing.T) {
	for _, c := range []struct {
		pattern string
		write   bool
		want    string
	}{
		{"ada.example.com/tasks/task", true, "write:ada.example.com/tasks/task"},
		{"ada.example.com/tasks/*", false, "read:ada.example.com/tasks/*"},
		{"substrate.reamde.dev/core/setting", false, "read:substrate.reamde.dev/core/setting"},
		{"substrate.reamde.dev/llm/memory", true, "full"},
		{"substrate.reamde.dev/core/token", false, "full"},
	} {
		if got := Covering(c.pattern, c.write); got != c.want {
			t.Errorf("Covering(%q, %v) = %q, want %q", c.pattern, c.write, got, c.want)
		}
	}
}

func TestCodeGlobsCannotUseScopeExclusions(t *testing.T) {
	approved, err := ApprovalOf([]string{"write:*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"*", "substrate.reamde.dev/*", "substrate.reamde.dev/core/*", "substrate.reamde.dev/llm/*"} {
		for _, write := range []bool{false, true} {
			if approved.Covers(pattern, write, Kind{Internal: false}) || Covering(pattern, write) != "full" {
				t.Fatalf("scope covered code glob %s, write %v", pattern, write)
			}
		}
	}
}
