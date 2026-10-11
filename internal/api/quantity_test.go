package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A quantity's value is a digit string on every door. The REST body decodes
// numbers as json.Number, and the engine refuses one as a quantity's value
// (decision 0012); the fake holds the same line, so this proves what arrives
// at the dataset is the string the writer sent, and that a number is refused
// rather than turned back into digits.
func TestQuantityValueIsADigitString(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	rec := env.do(t, http.MethodPut, peoplePath+"/ada", tok, map[string]any{"properties": map[string]any{
		"name": "Ada", "height": map[string]any{"value": 1.8, "unit": "m"},
	}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "height", "not a JSON number")

	rec = env.do(t, http.MethodPut, peoplePath+"/ada", tok, map[string]any{"properties": map[string]any{
		"name": "Ada", "height": map[string]any{"value": "1.80", "unit": "m"},
	}})
	wantStatus(t, rec, http.StatusCreated)
	h, _ := ds.lastPut.Properties["height"].(map[string]any)
	if h["value"] != "1.80" || h["unit"] != "m" {
		t.Fatalf("the dataset saw height = %#v, want the digits as written", ds.lastPut.Properties["height"])
	}

	// A filter's operand is a quantity value too, and reaches the dataset as
	// the object the reader sent.
	rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{
		Kinds: []string{personKind},
		Properties: map[string]substrate.Cond{
			"height": {Lt: map[string]any{"value": "6", "unit": "[ft_i]"}},
		},
	}), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	got, _ := ds.lastQuery.Filter.Properties["height"].Lt.(map[string]any)
	if got["value"] != "6" || got["unit"] != "[ft_i]" {
		t.Fatalf("the dataset saw lt = %#v", ds.lastQuery.Filter.Properties["height"].Lt)
	}
}
