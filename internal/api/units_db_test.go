package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The units route lists the repository's graph: its units with their roots
// and exact maps, its conversions, the revision a client caches by and what
// the seed skipped. The repository holds an owner's stone spelled as the
// shipped st is, so the open that seeds the shipped units skips st and says
// why.
func TestUnitsRouteListsTheGraph(t *testing.T) {
	t.Parallel()
	env := upgradeReopenedEnv(t, func(ctx context.Context, ds substrate.Dataset) {
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
			Kind: vocabulary.KindUnit, ID: "stone", Properties: map[string]any{"name": "stone", "code": "[stone_av]"},
		}); err != nil {
			t.Fatalf("put the owner's stone: %v", err)
		}
	}, engine.WithUnitSeed(true))

	rec := env.do(t, http.MethodGet, "/api/v1/units", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	table := decodeJSON[substrate.UnitTable](t, rec)
	if table.Revision <= 0 {
		t.Errorf("revision = %d, want the graph's", table.Revision)
	}
	var lb *substrate.RepositoryUnit
	for i := range table.Units {
		if table.Units[i].ID == "lb" {
			lb = &table.Units[i]
		}
	}
	// The root is the smallest id of the component, which with the shipped
	// mass units seeded is the gram.
	if lb == nil || lb.Root != "g" || lb.Factor != "453.59237" || lb.Code != "[lb_av]" {
		t.Errorf("lb = %+v", lb)
	}
	want := substrate.RepositoryConversion{ID: "lb-kg", From: "lb", To: "kg", Factor: "0.45359237"}
	var found bool
	for _, c := range table.Conversions {
		found = found || c == want
	}
	if !found {
		t.Errorf("conversions lack %+v", want)
	}
	var st *substrate.UnseededRecord
	for i := range table.Unseeded {
		if table.Unseeded[i].Kind == vocabulary.KindUnit && table.Unseeded[i].ID == "st" {
			st = &table.Unseeded[i]
		}
	}
	if st == nil || !strings.Contains(st.Reason, "[stone_av]") {
		t.Errorf("unseeded = %+v, want st skipped naming the spelling", table.Unseeded)
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

// The conversion route converts through the repository's graph, a difference
// by delta=true, and answers no measure; a pair the graph does not join is a
// validation error naming the units.
func TestUnitConversionRoute(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	if err := enginetest.PutShippedUnits(t.Context(), env.ds, "kg", "Cel"); err != nil {
		t.Fatal(err)
	}
	rec := env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=lb&to=kg", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if c := decodeJSON[substrate.UnitConversion](t, rec); c.Value != "0.45359237" || c.Unit != "kg" || !c.Exact {
		t.Errorf("1 lb = %+v", c)
	}
	if strings.Contains(rec.Body.String(), `"measure"`) {
		t.Errorf("the conversion still answers a measure: %s", rec.Body.String())
	}
	rec = env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=Cel&to=degF&delta=true", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if c := decodeJSON[substrate.UnitConversion](t, rec); c.Value != "1.8" {
		t.Errorf("a difference of 1 Cel = %+v", c)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/units/convert?value=1&from=kg&to=K", env.token, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "kg", "K")
}
