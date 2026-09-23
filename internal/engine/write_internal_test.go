package engine

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A write naming a hot column the kind never bound is refused, and the
// refusal quotes the exact `traits:` line that binds it. The temporal trait's
// own document declares only `at` and `endsAt`, so `dueAt` is discoverable
// from nowhere else than this message and the loader's reserved-name one.
func TestSplitPropsNamesTheTemporalBinding(t *testing.T) {
	for name, want := range map[string]string{
		"at":     "traits: [substrate.reamde.dev/core/temporal(point)]",
		"endsAt": "traits: [substrate.reamde.dev/core/temporal(range)]",
		"dueAt":  `traits: ["substrate.reamde.dev/core/temporal(point: dueAt)"]`,
	} {
		t.Run(name, func(t *testing.T) {
			todo := &vocabulary.Kind{Name: "todo", HotColumns: map[string]bool{}}
			_, _, _, err := splitProps(todo, map[string]any{name: "2026-08-08T00:00:00Z"})
			if err == nil {
				t.Fatalf("writing %s to a kind without the binding must be refused", name)
			}
			for _, part := range []string{"todo declares no " + name, "`" + want + "`"} {
				if !strings.Contains(err.Error(), part) {
					t.Fatalf("error = %q, want it to contain %q", err, part)
				}
			}

			bound := &vocabulary.Kind{Name: "todo", HotColumns: map[string]bool{name: true}}
			props, hot, _, err := splitProps(bound, map[string]any{name: "2026-08-08T00:00:00Z"})
			if err != nil {
				t.Fatalf("a bound %s must be accepted: %v", name, err)
			}
			if props != nil || !hot.mentions() {
				t.Fatalf("a bound %s lands in the hot column, not in properties: props=%v hot=%+v", name, props, hot)
			}
		})
	}
}
