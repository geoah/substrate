package commands

import (
	"encoding/json"
	"testing"
)

// A typed declaration row authors `authority` and the `names` object and
// carries no `name`/`plural` properties: the decoder must read the names off
// the declaration and derive only the missing halves from the id, never
// clobbering an authored value. The dot-split this replaces read
// "core.substrate.reamde.dev/agent" as name "core" under authority
// "substrate.reamde.dev/agent".
func TestDecodeTypeInfoTypedRow(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "core.substrate.reamde.dev/agent",
		"kind": "core.substrate.reamde.dev/kind",
		"version": 3,
		"properties": {
			"authority": "core.substrate.reamde.dev",
			"version": 6,
			"names": {"singular": "agent", "plural": "agents"},
			"description": "one declared agent"
		}
	}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("typed row did not decode")
	}
	if ti.Identity != "core.substrate.reamde.dev/agent" {
		t.Errorf("identity = %q", ti.Identity)
	}
	if ti.Name != "agent" || ti.Authority != "core.substrate.reamde.dev" {
		t.Errorf("name/authority = %q / %q", ti.Name, ti.Authority)
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

// A row an older substrate wrote still carries the blob and the projected
// name/plural columns; both shapes must keep decoding.
func TestDecodeTypeInfoLegacyRow(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "tasks.substrate.reamde.dev/task",
		"properties": {
			"name": "task",
			"plural": "tasks",
			"authority": "tasks.substrate.reamde.dev",
			"definition": {"description": "one task", "names": {"singular": "task", "plural": "tasks"}}
		}
	}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("legacy row did not decode")
	}
	if ti.Name != "task" || ti.Plural != "tasks" || ti.Authority != "tasks.substrate.reamde.dev" {
		t.Errorf("name/plural/authority = %q / %q / %q", ti.Name, ti.Plural, ti.Authority)
	}
	if ti.Description != "one task" {
		t.Errorf("description = %q", ti.Description)
	}
}

// A bare repository-local kind id has no authority to derive.
func TestDecodeTypeInfoBareLocalKind(t *testing.T) {
	raw := json.RawMessage(`{"id": "task", "properties": {"names": {"singular": "task", "plural": "tasks"}}}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("bare row did not decode")
	}
	if ti.Name != "task" || ti.Authority != "" {
		t.Errorf("name/authority = %q / %q", ti.Name, ti.Authority)
	}
}
