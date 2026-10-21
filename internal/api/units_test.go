package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The units route lists the repository's graph from the dataset: its units
// with their roots and exact maps, its conversions, the revision a client
// caches by and what the seed skipped, behind the token every /api/v1 route
// needs.
func TestUnitsRouteListsTheGraph(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units", "", nil), http.StatusUnauthorized, codeAuth)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units?measure=mass", tok, nil), http.StatusBadRequest, codeBadRequest)

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
	// The fields are always on the wire, an empty list included, and the
	// old table's are gone.
	body := rec.Body.String()
	for _, key := range []string{`"units":`, `"conversions":`, `"revision":`, `"unseeded":`} {
		if !strings.Contains(body, key) {
			t.Errorf("the answer lacks %s", key)
		}
	}
	for _, key := range []string{`"measures":`, `"aliases":{`} {
		if strings.Contains(body, key) {
			t.Errorf("the answer still carries %s", key)
		}
	}
}

// The conversion route converts through the dataset's graph (ConvertUnit), a
// difference by delta=true; `measure` is no parameter of it, and discovery
// says nothing about units.
func TestUnitConversionRoute(t *testing.T) {
	env := newTestEnv(t)
	doc := decodeJSON[map[string]any](t, env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil))
	if _, held := doc["units"]; held {
		t.Errorf("discovery still answers units: %v", doc["units"])
	}
	tok := env.svc.token(fakeRepository)
	rec := env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=lb&to=kg", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if c := decodeJSON[substrate.UnitConversion](t, rec); c.Value != "0.45359237" || c.Unit != "kg" || !c.Exact {
		t.Errorf("1 lb = %+v", c)
	}
	if strings.Contains(rec.Body.String(), `"measure"`) {
		t.Errorf("the conversion still answers a measure: %s", rec.Body.String())
	}
	rec = env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=Cel&to=degF&delta=true", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if c := decodeJSON[substrate.UnitConversion](t, rec); c.Value != "1.8" {
		t.Errorf("a difference of 1 Cel = %+v", c)
	}
	for _, tc := range []struct {
		query  string
		status int
		code   string
		says   string
	}{
		{"value=1&from=Cel&to=degF&measure=temperature-difference", http.StatusBadRequest, codeBadRequest, `"measure"`},
		{"value=1&from=Cel&to=degF&delta=yes", http.StatusBadRequest, codeBadRequest, "delta"},
		{"value=1&from=kg", http.StatusBadRequest, codeBadRequest, "to is required"},
		{"value=1&from=kg&to=g&unit=g", http.StatusBadRequest, codeBadRequest, `"unit"`},
		{"value=1&from=kg&to=K", http.StatusUnprocessableEntity, codeValidation, "the fake converts"},
	} {
		rec := env.do(t, http.MethodGet, "/api/v1/units/convert?"+tc.query, tok, nil)
		wantErrorCode(t, rec, tc.status, tc.code)
		wantMessage(t, rec, tc.says)
	}
}
