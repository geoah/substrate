package api

import (
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A trait scope (decision 0225) reaches what `*` would among the kinds whose
// declaration binds the trait when each request is checked.

const (
	temporalTrait    = "substrate.reamde.dev/core/temporal"
	conversationKind = "samples.substrate.reamde.dev/messaging/conversationmessage"
)

// declare rewrites one of the fake registry's declarations in place, as an
// upgrade of the kind would.
func declare(ds *fakeDataset, kind string, edit func(def map[string]any)) {
	for i, k := range ds.types {
		if k.Identity == kind {
			def := maps.Clone(k.Definition)
			edit(def)
			ds.types[i].Definition = def
			return
		}
	}
}

func TestATraitScopeReachesTheKindsThatBindItWhenEachRequestIsChecked(t *testing.T) {
	e := newAppEnv(t)
	scopes := "write:implements(" + temporalTrait + ")"
	app := e.grant(scopes, []string{scopes})

	// The task binds the trait with a variant and a remap, which say how it
	// binds it, not what.
	wantStatus(t, e.do(http.MethodPut, "/api/v1/"+taskKind+"/t1", app, []byte(`{"properties":{"note":"x"}}`), "application/json"), http.StatusCreated)
	wantStatus(t, e.do(http.MethodGet, recordsOf(t, taskKind), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, recordsOf(t, conversationKind), app, nil, ""), http.StatusOK)
	// A person binds nothing, so the token does not know the kind.
	wantStatus(t, e.do(http.MethodGet, recordsOf(t, personKind), app, nil, ""), http.StatusNotFound)
	if rec := e.do(http.MethodPut, "/api/v1/"+personKind+"/p1", app, []byte(`{"properties":{"name":"x"}}`), "application/json"); rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
		t.Fatalf("a trait scope wrote a kind that binds nothing: %d", rec.Code)
	}

	// Declared to bind the trait after the token was minted, the person is
	// reached from the next request on.
	ds := e.svc.datasets[fakeRepository]
	declare(ds, personKind, func(def map[string]any) { def["traits"] = []any{temporalTrait + "(point)"} })
	wantStatus(t, e.do(http.MethodGet, recordsOf(t, personKind), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodPut, "/api/v1/"+personKind+"/p1", app, []byte(`{"properties":{"name":"x"}}`), "application/json"), http.StatusCreated)

	// An internal kind is reached by no trait scope, whatever it binds.
	declare(ds, personKind, func(def map[string]any) { def["purpose"] = "internal" })
	wantStatus(t, e.do(http.MethodGet, recordsOf(t, personKind), app, nil, ""), http.StatusNotFound)
}

func TestTheApprovalPageListsTheKindsATraitScopeReachesAndSaysItGrows(t *testing.T) {
	e := newAppEnv(t)
	id := e.park(appRequest("read:implements(" + temporalTrait + ")"))
	view := e.show(id)
	wantStatus(t, view, http.StatusOK)
	page := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(page.Scopes) != 1 {
		t.Fatalf("scopes = %+v, want the trait scope alone", page.Scopes)
	}
	row := page.Scopes[0]
	if row.Scope != "read:implements("+temporalTrait+")" || row.Pattern != "implements("+temporalTrait+")" || !row.Glob {
		t.Fatalf("row = %+v, want the trait scope, marked as covering kinds added later", row)
	}
	var kinds []string
	for _, k := range row.Kinds {
		kinds = append(kinds, k.Identity)
	}
	if !slices.Contains(kinds, taskKind) || !slices.Contains(kinds, conversationKind) || slices.Contains(kinds, personKind) {
		t.Fatalf("kinds = %v, want the kinds that bind the trait today and no other", kinds)
	}
}
