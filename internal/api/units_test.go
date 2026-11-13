package api

import (
	"net/http"
	"testing"
)

// The units route sits behind the token every /api/v1 route needs and reads
// no parameter: a `measure` is refused by name before the graph is read.
func TestUnitsRouteRefusesBeforeTheGraph(t *testing.T) {
	env := newStubEnv(t, nil)
	wantErrorCode(t, env.do(t, http.MethodGet, "/api/v1/units", "", nil), http.StatusUnauthorized, codeAuth)
	rec := env.do(t, http.MethodGet, "/api/v1/units?measure=mass", stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	wantMessage(t, rec, `"measure"`)
}

// The conversion route's own refusals come before the graph: `measure` and
// `unit` are no parameters of it, delta is a boolean and the three units of
// a conversion are required. Discovery says nothing about units.
func TestUnitConversionRouteRefusesBeforeTheGraph(t *testing.T) {
	env := newStubEnv(t, nil)
	doc := decodeJSON[map[string]any](t, env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil))
	if _, held := doc["units"]; held {
		t.Errorf("discovery still answers units: %v", doc["units"])
	}
	for _, tc := range []struct {
		query string
		says  string
	}{
		{"value=1&from=Cel&to=degF&measure=temperature-difference", `"measure"`},
		{"value=1&from=Cel&to=degF&delta=yes", "delta"},
		{"value=1&from=kg", "to is required"},
		{"value=1&from=kg&to=g&unit=g", `"unit"`},
	} {
		rec := env.do(t, http.MethodGet, "/api/v1/units/convert?"+tc.query, stubToken, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		wantMessage(t, rec, tc.says)
	}
}
