package commands

import (
	"encoding/json"
	"testing"
)

// A typed declaration row authors `authority`, `package` and the `names`
// object and carries no `name`/`plural` properties: the decoder must read the
// names off the declaration and derive only the missing thirds from the id,
// never clobbering an authored value.
func TestDecodeTypeInfoTypedRow(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "substrate.reamde.dev/core/agent",
		"kind": "substrate.reamde.dev/core/kind",
		"version": 3,
		"properties": {
			"authority": "substrate.reamde.dev",
			"package": "core",
			"version": 6,
			"names": {"singular": "agent", "plural": "agents"},
			"description": "one declared agent"
		}
	}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("typed row did not decode")
	}
	if ti.Identity != "substrate.reamde.dev/core/agent" {
		t.Errorf("identity = %q", ti.Identity)
	}
	if ti.Name != "agent" || ti.Authority != "substrate.reamde.dev" || ti.Package != "core" {
		t.Errorf("name/authority/package = %q / %q / %q", ti.Name, ti.Authority, ti.Package)
	}
	if ti.Plural != "agents" {
		t.Errorf("plural = %q", ti.Plural)
	}
	if ti.Version != 6 {
		t.Errorf("version = %d", ti.Version)
	}
	if ti.Description != "one declared agent" {
		t.Errorf("description = %q", ti.Description)
	}
}
