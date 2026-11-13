package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A quantity's value is a digit string on every door. The REST body decodes
// numbers as json.Number, and the engine refuses one as a quantity's value
// (decision 0012) rather than turning it back into digits; the string the
// writer sent is what is stored. A filter's operand is a quantity value too,
// and the engine reads it as the object the reader sent, converting it
// through the repository's units: 1.80 m is under 6 ft and over 5.
func TestQuantityValueIsADigitString(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	if err := enginetest.PutShippedUnits(context.Background(), env.ds, "m"); err != nil {
		t.Fatalf("put the length units: %v", err)
	}
	env.declare(t, fmt.Sprintf(`
kind: substrate.reamde.dev/core/package
metadata:
  id: %[1]s/body
data:
  authority: %[1]s
  package: body
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: %[1]s/body/measurement
data:
  authority: %[1]s
  package: body
  names:
    singular: measurement
  displayTemplate: "{name}"
  properties:
    name:
      type: string
    height:
      type: quantity
      unit: m
`, env.repository))
	kind := env.repository + "/body/measurement"
	id := mintedID("ada")
	path := "/api/v1/" + vocabulary.RecordPath(kind, id)

	rec := env.do(t, http.MethodPut, path, env.token, map[string]any{"properties": map[string]any{
		"name": "Ada", "height": map[string]any{"value": 1.8, "unit": "m"},
	}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "height", "written as a string")

	rec = env.do(t, http.MethodPut, path, env.token, map[string]any{"properties": map[string]any{
		"name": "Ada", "height": map[string]any{"value": "1.80", "unit": "m"},
	}})
	wantStatus(t, rec, http.StatusCreated)
	h, _ := env.get(t, kind, id).Properties["height"].(map[string]any)
	if h["value"] != "1.80" || h["unit"] != "m" {
		t.Fatalf("stored height = %#v, want the digits as written", h)
	}

	for feet, want := range map[string]int{"6": 1, "5": 0} {
		rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{
			Kinds: []string{kind},
			Properties: map[string]substrate.Cond{
				"height": {Lt: map[string]any{"value": feet, "unit": "[ft_i]"}},
			},
		}), env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.Page](t, rec).Records; len(got) != want {
			t.Fatalf("height under %s ft: %d records, want %d", feet, len(got), want)
		}
	}
}
