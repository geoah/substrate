package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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

// The units route lists the repository's graph from the dataset: its units
// with their roots and exact maps, its conversions, the revision a client
// caches by and what the seed skipped, beside the engine's table.
func TestUnitsRouteListsTheGraph(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1/units", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	table := decodeJSON[substrate.UnitTable](t, rec)
	if table.Revision != fakeUnits.Revision {
		t.Errorf("revision = %d, want %d", table.Revision, fakeUnits.Revision)
	}
	if len(table.Units) != 2 || table.Units[1].ID != "lb" || table.Units[1].Root != "kg" || table.Units[1].Factor != "0.45359237" || table.Units[1].Code != "[lb_av]" {
		t.Errorf("units = %+v", table.Units)
	}
	if len(table.Conversions) != 1 || table.Conversions[0] != fakeUnits.Conversions[0] {
		t.Errorf("conversions = %+v", table.Conversions)
	}
	if len(table.Unseeded) != 1 || table.Unseeded[0].ID != "st" || table.Unseeded[0].Reason == "" {
		t.Errorf("unseeded = %+v", table.Unseeded)
	}
	if len(table.Measures) == 0 || table.Aliases["bpm"] != "/min" {
		t.Errorf("the engine's table is gone: %d measures, aliases %v", len(table.Measures), table.Aliases)
	}
	// The fields are always on the wire, an empty list included.
	for _, key := range []string{`"units":`, `"conversions":`, `"revision":`, `"unseeded":`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("the answer lacks %s", key)
		}
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
		{
			map[string]string{"value": "72.4", "from": "kg", "to": "[lb_av]"},
			substrate.UnitConversion{Value: "159.61467782185136844", Unit: "[lb_av]", Measure: "mass", Exact: false},
		},
		{
			map[string]string{"value": "37", "from": "°C", "to": "°F"},
			substrate.UnitConversion{Value: "98.6", Unit: "[degF]", Measure: "temperature", Exact: true},
		},
		{
			map[string]string{"value": "0.3", "from": "Cel", "to": "[degF]", "measure": "temperature-difference"},
			substrate.UnitConversion{Value: "0.54", Unit: "[degF]", Measure: "temperature-difference", Exact: true},
		},
		{
			map[string]string{"value": "3", "from": "GJ", "to": "kW.h"},
			substrate.UnitConversion{Value: "833.33333333333333333", Unit: "kW.h", Measure: "energy", Exact: false},
		},
		{
			map[string]string{"value": "62", "from": "bpm", "to": "/s"},
			substrate.UnitConversion{Value: "1.0333333333333333333", Unit: "/s", Measure: "rate", Exact: false},
		},
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

// Under the unit graph switch discovery says so, and the conversion route
// converts through the dataset's graph (ConvertUnit), a difference by
// delta=true; the table's `measure` is refused there, and `delta` is refused
// on a deployment that reads the table.
func TestUnitConversionRouteThroughTheGraph(t *testing.T) {
	svc := newFakeService()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	env := &testEnv{svc: svc, clock: clock, h: New(Config{Service: svc, Now: clock.now, UnitGraph: true})}
	doc := decodeJSON[map[string]any](t, env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil))
	if units, _ := doc["units"].(map[string]any); units == nil || units["graph"] != true {
		t.Fatalf("discovery's units = %v, want graph true", doc["units"])
	}
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=lb&to=kg", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if c := decodeJSON[substrate.UnitConversion](t, rec); c.Value != "0.45359237" || c.Unit != "kg" || !c.Exact {
		t.Errorf("1 lb = %+v", c)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=Cel&to=degF&delta=true", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if c := decodeJSON[substrate.UnitConversion](t, rec); c.Value != "1.8" {
		t.Errorf("a difference of 1 Cel = %+v", c)
	}
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=Cel&to=degF&measure=temperature-difference", tok, nil), http.StatusBadRequest, codeBadRequest)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=Cel&to=degF&delta=yes", tok, nil), http.StatusBadRequest, codeBadRequest)
	wantStatus(t, env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=kg&to=K", tok, nil), http.StatusUnprocessableEntity)

	table := newTestEnv(t)
	doc = decodeJSON[map[string]any](t, table.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil))
	if units, _ := doc["units"].(map[string]any); units == nil || units["graph"] != false {
		t.Fatalf("discovery's units on the table = %v, want graph false", doc["units"])
	}
	wantErrorCode(t, table.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=Cel&to=[degF]&delta=true", table.svc.token(fakeRepository), nil), http.StatusBadRequest, codeBadRequest)
}
