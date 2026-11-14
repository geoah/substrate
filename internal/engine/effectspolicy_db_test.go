package engine

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Installed code never writes a recordpatchpolicy (policy.go): a policy a
// function could write is a gate that function could open. An agent's own
// write is refused at its door (agentwrite.go); a function's effect is refused
// at decode, whatever its `writes` grant names, the policy kind itself or a
// glob that covers it.
func TestAFunctionEffectNeverWritesAPolicy(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	for _, grant := range []string{vocabulary.KindRecordPatchPolicy, "substrate.reamde.dev/core/*", "*"} {
		fn := &vocabulary.Function{
			Name: "rules", Package: tierPackage, Description: "test",
			Caps: vocabulary.FunctionCaps{Emit: []string{grant}},
		}
		for _, action := range []string{"put", "patch", "delete"} {
			effect := map[string]any{"action": action, "kind": vocabulary.KindRecordPatchPolicy, "id": "open-the-gate"}
			if action != "delete" {
				effect["properties"] = map[string]any{"action": "allow"}
			}
			_, err := ds.decodeEffect(fn, effect)
			if err == nil || !strings.Contains(err.Error(), "installed code never writes the door's own rules") {
				t.Fatalf("writes %q, %s of a policy: err = %v, want the refusal", grant, action, err)
			}
		}
	}

	// The same grants still reach every other kind they name.
	fn := &vocabulary.Function{
		Name: "rules", Package: tierPackage, Description: "test",
		Caps: vocabulary.FunctionCaps{Emit: []string{"substrate.reamde.dev/core/*"}},
	}
	if _, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": "substrate.reamde.dev/core/contact", "id": "x",
	}); err != nil {
		t.Fatalf("a glob over core no longer reaches a contact: %v", err)
	}
}
