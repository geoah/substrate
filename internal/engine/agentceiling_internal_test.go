package engine

// The sub-agent emit ceiling, once both sides of it may be globs
// (decision record 0080).

import (
	"slices"
	"testing"
)

func TestEffectiveEmitIntersectsGlobsBySubsumption(t *testing.T) {
	const (
		task = "ada.example.com/tasks/task"
		note = "ada.example.com/notes/note"
	)
	cases := map[string]struct {
		own, ceiling, want []string
	}{
		"a ceiling narrows an own glob to the ceiling": {
			own:     []string{"*"},
			ceiling: []string{"ada.example.com/tasks/*"},
			want:    []string{"ada.example.com/tasks/*"},
		},
		"an own kind survives a glob ceiling that covers it": {
			own:     []string{task, "bob.example.com/tasks/task"},
			ceiling: []string{"ada.example.com/*"},
			want:    []string{task},
		},
		"a glob own set narrows to the kinds the ceiling names": {
			own:     []string{"ada.example.com/*"},
			ceiling: []string{task, note},
			want:    []string{task, note},
		},
		"siblings meet as nothing": {
			own:     []string{"ada.example.com/tasks/*"},
			ceiling: []string{"ada.example.com/notes/*"},
			want:    []string{},
		},
		"two globs meet as the narrower": {
			own:     []string{"ada.example.com/*"},
			ceiling: []string{"*"},
			want:    []string{"ada.example.com/*"},
		},
		// The carve-out holds THROUGH the ceiling: a chain of globs can never
		// arrive at auth material, however wide either side is.
		"a glob chain never reaches an auth kind": {
			own:     []string{"*"},
			ceiling: []string{"substrate.reamde.dev/core/token"},
			want:    []string{},
		},
		// Naming it on both sides does grant it: the carve-out is on the glob.
		"a named auth kind passes a named ceiling": {
			own:     []string{"substrate.reamde.dev/core/token"},
			ceiling: []string{"substrate.reamde.dev/core/token"},
			want:    []string{"substrate.reamde.dev/core/token"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := effectiveEmit(c.own, c.ceiling, true)
			if len(got) != len(c.want) {
				t.Fatalf("effectiveEmit(%v, %v) = %v, want %v", c.own, c.ceiling, got, c.want)
			}
			for _, w := range c.want {
				if !slices.Contains(got, w) {
					t.Fatalf("effectiveEmit(%v, %v) = %v, missing %q", c.own, c.ceiling, got, w)
				}
			}
		})
	}
	// An unceilinged call keeps its own set verbatim, globs included.
	own := []string{"ada.example.com/*"}
	if got := effectiveEmit(own, nil, false); !slices.Equal(got, own) {
		t.Fatalf("unceilinged = %v, want %v", got, own)
	}
}
