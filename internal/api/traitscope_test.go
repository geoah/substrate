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

// An app's token is answered the implementors of a trait that its grant
// reaches, each with its declaration, and no other.
func TestAnAppTokenListsTheImplementorsItsGrantReaches(t *testing.T) {
	e := newAppEnv(t)
	traitScope := "write:implements(" + temporalTrait + ")"
	app := e.grant(traitScope, []string{traitScope})
	wantStatus(t, e.do(http.MethodGet, recordsOf(t, taskKind), app, nil, ""), http.StatusOK)
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	// The person is listed among the implementors but its declaration binds
	// nothing, so the trait scope does not reach it.
	ds.traits[temporalTrait] = []string{taskKind, conversationKind, personKind}
	ds.mu.Unlock()

	route := "/api/v1/substrate.reamde.dev/core/trait/temporal/implementors"
	rec := e.do(http.MethodGet, route, app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.OperationalList[substrate.KindInfo]](t, rec)
	var ids []string
	for _, k := range got.Items {
		ids = append(ids, k.Identity)
		if k.Definition == nil {
			t.Fatalf("%s came without its declaration", k.Identity)
		}
	}
	slices.Sort(ids)
	if want := []string{conversationKind, taskKind}; !slices.Equal(ids, want) {
		t.Fatalf("implementors %v, want %v", ids, want)
	}

	// The declaration it answers is the kind's own data map.
	for _, k := range got.Items {
		if k.Identity == taskKind && k.Definition["traits"] == nil {
			t.Fatalf("the task's declaration lost its traits: %v", k.Definition)
		}
	}

	// Declared to bind the trait later, the person is listed from the next
	// request on; made internal, no trait scope lists it again.
	declare(ds, personKind, func(def map[string]any) { def["traits"] = []any{temporalTrait + "(point)"} })
	if got := implementorIDs(t, e, route, app); !slices.Equal(got, []string{conversationKind, personKind, taskKind}) {
		t.Fatalf("after the person binds the trait: %v", got)
	}
	declare(ds, personKind, func(def map[string]any) { def["purpose"] = "internal" })
	if got := implementorIDs(t, e, route, app); !slices.Equal(got, []string{conversationKind, taskKind}) {
		t.Fatalf("an internal implementor was listed: %v", got)
	}

	// A read-only trait scope lists the same kinds; a grant over an unrelated
	// kind lists none.
	readScope := "read:implements(" + temporalTrait + ")"
	reader := e.grant(readScope, []string{readScope})
	if got := implementorIDs(t, e, route, reader); !slices.Equal(got, []string{conversationKind, taskKind}) {
		t.Fatalf("a read-only trait scope's implementors: %v", got)
	}
	// The fake lists implementors from its own table, as the engine lists
	// them from declarations, so the person leaves it with the trait.
	ds.mu.Lock()
	ds.traits[temporalTrait] = []string{taskKind, conversationKind}
	ds.mu.Unlock()
	unrelated := "write:" + personKind
	if got := implementorIDs(t, e, route, e.grant(unrelated, []string{unrelated})); len(got) != 0 {
		t.Fatalf("an unrelated grant's implementors: %v", got)
	}

	narrow := "write:" + taskKind
	exact := e.grant(narrow, []string{narrow})
	rec = e.do(http.MethodGet, route, exact, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.OperationalList[substrate.KindInfo]](t, rec); len(got.Items) != 1 || got.Items[0].Identity != taskKind {
		t.Fatalf("an exact scope's implementors: %+v", got.Items)
	}
}

func implementorIDs(t *testing.T, e *appEnv, route, bearer string) []string {
	t.Helper()
	rec := e.do(http.MethodGet, route, bearer, nil, "")
	wantStatus(t, rec, http.StatusOK)
	var ids []string
	for _, k := range decodeJSON[substrate.OperationalList[substrate.KindInfo]](t, rec).Items {
		ids = append(ids, k.Identity)
	}
	slices.Sort(ids)
	return ids
}
