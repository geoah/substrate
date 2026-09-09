package commands

import (
	"encoding/json"
	"testing"
)

// A typed declaration row authors `authority`, `package` and the `names`
// object and carries no `name` property: the decoder must read the name off
// the declaration and derive only the missing thirds from the id, never
// clobbering an authored value.
func TestDecodeTypeInfoTypedRow(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "substrate.reamde.dev/core/agent",
		"kind": "substrate.reamde.dev/core/kind",
		"version": 3,
		"properties": {
			"authority": "substrate.reamde.dev",
			"package": "core",
			"version": 6,
			"names": {"singular": "agent"},
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
	if ti.Version != 6 {
		t.Errorf("version = %d", ti.Version)
	}
	if ti.Description != "one declared agent" {
		t.Errorf("description = %q", ti.Description)
	}
}

// A row an older substrate wrote still carries the blob and the projected
// name column; both shapes must keep decoding.
func TestDecodeTypeInfoLegacyRow(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "samples.substrate.reamde.dev/tasks/task",
		"properties": {
			"name": "task",
			"authority": "samples.substrate.reamde.dev",
			"package": "tasks",
			"definition": {"description": "one task", "names": {"singular": "task"}}
		}
	}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("legacy row did not decode")
	}
	if ti.Name != "task" || ti.Authority != "samples.substrate.reamde.dev" || ti.Package != "tasks" {
		t.Errorf("name/authority/package = %q / %q / %q", ti.Name, ti.Authority, ti.Package)
	}
	if ti.Description != "one task" {
		t.Errorf("description = %q", ti.Description)
	}
}

// A bare repository-local kind id has no authority or package to derive.
func TestDecodeTypeInfoBareLocalKind(t *testing.T) {
	raw := json.RawMessage(`{"id": "task", "properties": {"names": {"singular": "task"}}}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("bare row did not decode")
	}
	if ti.Name != "task" || ti.Authority != "" || ti.Package != "" {
		t.Errorf("name/authority/package = %q / %q / %q", ti.Name, ti.Authority, ti.Package)
	}
}
