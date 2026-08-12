package engine

// The effect-layer CAS matrix (SDK-review synthesis #2, #10):
// ifAbsent+ifVersion is refused at decode rather than silently letting
// ifAbsent win, and a version keeps full int64 fidelity through the runner's
// UseNumber decode.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// TestPutIfAbsentIfVersionRejectedAtDecode: the two preconditions cannot
// combine — ifAbsent short-circuits ahead of the version check, so the pair
// silently drops the guard. Decode refuses it; a lone ifVersion or a lone
// ifAbsent still decodes.
func TestPutIfAbsentIfVersionRejectedAtDecode(t *testing.T) {
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	fn := &vocabulary.Function{
		Name: "fixer", Authority: tierAuthority, Description: "test",
		Caps: vocabulary.FunctionCaps{Emit: []string{typeTierProfile}},
	}

	_, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": typeTierProfile, "id": "x",
		"ifAbsent": true, "ifVersion": float64(3),
		"properties": map[string]any{"name": "v"},
	})
	if err == nil || !strings.Contains(err.Error(), "ifAbsent and ifVersion cannot combine") {
		t.Fatalf("ifAbsent+ifVersion: err = %v, want the combine refusal", err)
	}

	// A lone ifVersion decodes.
	if ef, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": typeTierProfile, "id": "x", "ifVersion": float64(3),
	}); err != nil || ef.IfVersion == nil || *ef.IfVersion != 3 {
		t.Fatalf("lone ifVersion decode = %+v, %v", ef, err)
	}
	// A lone ifAbsent decodes.
	if ef, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": typeTierProfile, "id": "x", "ifAbsent": true,
	}); err != nil || !ef.IfAbsent || ef.IfVersion != nil {
		t.Fatalf("lone ifAbsent decode = %+v, %v", ef, err)
	}
}

// TestIfVersionInt64Fidelity: a version past 2^53 survives asInt64 as itself
// when carried as json.Number (the runner's UseNumber decode), where the old
// float64 path would round it to a neighboring integer.
func TestIfVersionInt64Fidelity(t *testing.T) {
	const big = int64(9007199254740993) // 2^53 + 1, unrepresentable in float64

	if got, ok := asInt64(json.Number("9007199254740993")); !ok || got != big {
		t.Fatalf("asInt64(json.Number) = %d, %v, want %d", got, ok, big)
	}
	// The float path cannot represent it — the very reason UseNumber is needed.
	if got, _ := asInt64(float64(big)); got == big {
		t.Fatalf("float64 unexpectedly preserved %d — the fidelity risk is gone?", big)
	}
	// A decoded effect carrying a json.Number ifVersion keeps the exact value.
	var ef effect
	if err := decodeIfVersion(map[string]any{"ifVersion": json.Number("9007199254740993")}, &ef); err != nil {
		t.Fatalf("decodeIfVersion(json.Number): %v", err)
	}
	if ef.IfVersion == nil || *ef.IfVersion != big {
		t.Fatalf("decoded ifVersion = %v, want %d", ef.IfVersion, big)
	}
}
