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

// A record row carries the kind's label only inside its declaration: the
// decoder lifts it to KindInfo.Label, so `substratectl kinds -o json` prints a
// top-level `label` as the vocabulary read does (decision 0117). A kind that
// declares none decodes with no label.
func TestDecodeTypeInfoCarriesTheDeclaredLabel(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "providers.substrate.reamde.dev/slack/conversation",
		"kind": "substrate.reamde.dev/core/kind",
		"properties": {
			"authority": "providers.substrate.reamde.dev",
			"package": "slack",
			"names": {"singular": "conversation"},
			"label": {"singular": "Channel", "plural": "Channels"}
		}
	}`)
	ti, ok := decodeTypeInfo(raw)
	if !ok {
		t.Fatal("typed row did not decode")
	}
	if ti.Label == nil || ti.Label.Singular != "Channel" || ti.Label.Plural != "Channels" {
		t.Errorf("label = %+v, want Channel / Channels", ti.Label)
	}

	bare := json.RawMessage(`{"identity": "d.example.com/d/thing", "label": {"singular": "Thing", "plural": "Things"}}`)
	if ti, _ := decodeTypeInfo(bare); ti.Label == nil || ti.Label.Plural != "Things" {
		t.Errorf("bare KindInfo label = %+v", ti.Label)
	}

	none := json.RawMessage(`{"id": "d.example.com/d/thing", "properties": {"names": {"singular": "thing"}}}`)
	if ti, _ := decodeTypeInfo(none); ti.Label != nil {
		t.Errorf("undeclared label = %+v, want nil", ti.Label)
	}
}
