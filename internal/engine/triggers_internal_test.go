package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
)

// A trigger source may name a kind in either spelling — `task` and
// `tasks.substrate.reamde.dev/task` are ONE kind — and the changelog rows
// the matcher compares against carry the identity. So a source declared bare
// must fire on the qualified rows, exactly as the runner's reads allowlist
// resolves at its gate; before resolveKinds it validated and then never fired.
func TestTriggerSourceResolvesBareKinds(t *testing.T) {
	// `task` is an IMPORTED vocabulary bundle now — creation seeds core alone
	// — so the registry a trigger resolves against is the seed plus what the
	// repository imported.
	reg, err := enginetest.SeededRegistry("../../kinds/core.substrate.reamde.dev", "people", "tasks")
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	tr, err := parseTrigger("t1", map[string]any{
		"callable": map[string]any{"kind": kindFunction, "id": "x.substrate.reamde.dev/f"},
		"source": map[string]any{
			"record": map[string]any{"kinds": []any{"task"}, "ops": []any{"create"}},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tr.Record.matches("tasks.substrate.reamde.dev/task", "create") {
		t.Fatal("the bare pattern matched an identity before resolution — the test proves nothing")
	}
	tr.resolveKinds(reg)
	if !tr.Record.matches("tasks.substrate.reamde.dev/task", "create") {
		t.Fatalf("a trigger on kinds [task] did not fire for tasks.substrate.reamde.dev/task: %v", tr.Record.Kinds)
	}
	// A kind the registry does not know is left alone and matches nothing.
	tr2, err := parseTrigger("t2", map[string]any{
		"callable": map[string]any{"kind": kindFunction, "id": "x.substrate.reamde.dev/f"},
		"source": map[string]any{
			"record": map[string]any{"kinds": []any{"nosuchkind"}, "ops": []any{"create"}},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tr2.resolveKinds(reg)
	if tr2.Record.matches("tasks.substrate.reamde.dev/task", "create") {
		t.Fatal("an unknown kind matched something")
	}
}
