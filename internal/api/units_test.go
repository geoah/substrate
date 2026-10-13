package api

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The unit table route serves every measure with its codes, print symbols and
// exact factors, behind the token every /api/v1 route needs.
func TestUnitTableRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)

	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units", "", nil), http.StatusUnauthorized, codeAuth)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units?measure=mass", tok, nil), http.StatusBadRequest, codeBadRequest)

	rec := env.do(t, http.MethodGet, "/api/v1/units", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	table := decodeJSON[substrate.UnitTable](t, rec)
	codes := map[string]substrate.UnitCode{}
	measures := map[string]substrate.UnitMeasure{}
	for _, m := range table.Measures {
		measures[m.Name] = m
		for _, c := range m.Units {
			codes[m.Name+" "+c.Code] = c
		}
	}
	for key, want := range map[string]substrate.UnitCode{
		"mass [lb_av]":                 {Code: "[lb_av]", Print: "lb", Factor: "45359237/100000000"},
		"temperature Cel":              {Code: "Cel", Print: "°C", Factor: "1", Offset: "5463/20"},
		"temperature-difference Cel":   {Code: "Cel", Print: "°C", Factor: "1"},
		"angle rad":                    {Code: "rad", Print: "rad"},
		"amount-fraction mmol/mol":     {Code: "mmol/mol", Print: "mmol/mol", Factor: "1/1000"},
		"score {score}":                {Code: "{score}", Print: "score", Factor: "1"},
		"reactive-power V.A{reactive}": {Code: "V.A{reactive}", Print: "var", Factor: "1"},
	} {
		if got := codes[key]; got != want {
			t.Errorf("%s = %+v, want %+v", key, got, want)
		}
	}
	if m := measures["mass"]; m.Base != "kg" || m.Scale != "ratio" || m.Display != "kg" || m.Dimension != "M" {
		t.Errorf("mass = %+v", m)
	}
	if table.Aliases["bpm"] != "/min" || table.Aliases["°C"] != "Cel" {
		t.Errorf("aliases = %v", table.Aliases)
	}
}

// The conversion route converts one value exactly through the engine's table,
// answers the measure it converted in and whether it rounded, and refuses what
// the table refuses with the table's reason.
func TestUnitConversionRoute(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	convert := func(params map[string]string) string {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		return "/api/v1/units/convert?" + q.Encode()
	}
	for _, tc := range []struct {
		params map[string]string
		want   substrate.UnitConversion
	}{
		{map[string]string{"value": "72.4", "from": "kg", "to": "[lb_av]"},
			substrate.UnitConversion{Value: "159.61467782185136844", Unit: "[lb_av]", Measure: "mass", Exact: false}},
		{map[string]string{"value": "37", "from": "°C", "to": "°F"},
			substrate.UnitConversion{Value: "98.6", Unit: "[degF]", Measure: "temperature", Exact: true}},
		{map[string]string{"value": "0.3", "from": "Cel", "to": "[degF]", "measure": "temperature-difference"},
			substrate.UnitConversion{Value: "0.54", Unit: "[degF]", Measure: "temperature-difference", Exact: true}},
		{map[string]string{"value": "3", "from": "GJ", "to": "kW.h"},
			substrate.UnitConversion{Value: "833.33333333333333333", Unit: "kW.h", Measure: "energy", Exact: false}},
		{map[string]string{"value": "62", "from": "bpm", "to": "/s"},
			substrate.UnitConversion{Value: "1.0333333333333333333", Unit: "/s", Measure: "rate", Exact: false}},
	} {
		rec := env.do(t, http.MethodGet, convert(tc.params), tok, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.UnitConversion](t, rec); got != tc.want {
			t.Errorf("%v = %+v, want %+v", tc.params, got, tc.want)
		}
	}
	for _, tc := range []struct {
		params map[string]string
		status int
		code   string
		says   string
	}{
		{map[string]string{"value": "1", "from": "kg", "to": "J"}, http.StatusUnprocessableEntity, codeValidation, "inside one measure"},
		{map[string]string{"value": "48", "from": "mmol/mol", "to": "%"}, http.StatusUnprocessableEntity, codeValidation, "inside one measure"},
		{map[string]string{"value": "1e3", "from": "kg", "to": "g"}, http.StatusUnprocessableEntity, codeValidation, "value"},
		{map[string]string{"value": "1", "from": "kgs", "to": "g"}, http.StatusUnprocessableEntity, codeValidation, "from"},
		{map[string]string{"value": "1", "from": "kg", "to": "g", "measure": "weight"}, http.StatusUnprocessableEntity, codeValidation, "not a measure"},
		{map[string]string{"value": "1", "from": "kg"}, http.StatusBadRequest, codeBadRequest, "to is required"},
		{map[string]string{"value": "1", "from": "kg", "to": "g", "unit": "g"}, http.StatusBadRequest, codeBadRequest, `"unit"`},
	} {
		rec := env.do(t, http.MethodGet, convert(tc.params), tok, nil)
		wantErrorCode(t, rec, tc.status, tc.code)
		wantMessage(t, rec, tc.says)
	}
}
