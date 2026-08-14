package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/vocabulary"
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
		"callable": vocabulary.RecordPath(kindFunction, "x.substrate.reamde.dev/f"),
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
		"callable": vocabulary.RecordPath(kindFunction, "x.substrate.reamde.dev/f"),
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

// THE ONE RELEASED REFERENCE VALUE. `trigger.callable` shipped as a
// `{kind, id}` pair before a reference became one flat path, and a trigger is a
// DATA row: the rung walks declaration rows alone, so nothing re-projects a
// stored trigger. Two things therefore have to hold at once — a stored pair
// still dispatches, and any rewrite of that row canonicalizes it — and this
// pins both, so neither can be dropped as "the old shape".
func TestTriggerCallableReadsTheReleasedPairAndTheFlatPath(t *testing.T) {
	const id = "x.substrate.reamde.dev/f"
	source := map[string]any{
		"record": map[string]any{"kinds": []any{"task"}, "ops": []any{"create"}},
	}
	for name, callable := range map[string]any{
		"the flat path":           vocabulary.RecordPath(kindFunction, id),
		"the released {kind, id}": map[string]any{"kind": kindFunction, "id": id},
	} {
		t.Run(name, func(t *testing.T) {
			tr, err := parseTrigger("t1", map[string]any{"callable": callable, "source": source})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tr.CallableKind != callableKindFunction || tr.CallableID != id {
				t.Fatalf("callable = %s/%s, want function/%s", tr.CallableKind, tr.CallableID, id)
			}
		})
	}

	// The rewrite half: coercion is what turns a stored pair into the path, so
	// the row canonicalizes the next time anything writes it.
	pin := &vocabulary.Property{Datatype: vocabulary.DatatypeReference, To: kindFunction}
	got, err := coerceReference(pin, map[string]any{"kind": kindFunction, "id": id})
	if err != nil {
		t.Fatalf("coerce the released pair: %v", err)
	}
	if want := vocabulary.RecordPath(kindFunction, id); got != want {
		t.Fatalf("the released pair coerced to %#v, want the flat path %q", got, want)
	}
}
