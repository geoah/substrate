package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// The scoped dataset refuses the read itself too, so a route that ever moves
// into the scoped surface does not hand it out. The stub under it answers
// nothing, so a read that reached it would panic.
func TestScopedDatasetRefusesOperatorDefaults(t *testing.T) {
	ds := newScopedDataset(stubDataset{}, scope.Grant{}, substrate.Actor("app:example.com"))
	if _, err := ds.OperatorDefaults(t.Context()); err == nil {
		t.Fatal("the scoped dataset answered the operator defaults read")
	}
}

// Discovery lists operatorDefaults only where the server booted with a
// defaults file.
func TestDiscoveryListsOperatorDefaultsOnlyWithADefaultsFile(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		env := newStubEnv(t, nil, func(c *Config) { c.OperatorDefaults = loaded })
		rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
		wantStatus(t, rec, http.StatusOK)
		var doc struct {
			Features []featureInfo `json:"features"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		has := slices.ContainsFunc(doc.Features, func(f featureInfo) bool { return f.Name == "operatorDefaults" })
		if has != loaded {
			t.Fatalf("defaults file %v: operatorDefaults listed = %v", loaded, has)
		}
	}
}

// A call past the allowance answers 403 guard with the quota reason, so a
// client tells it from every other guard.
func TestAllowanceRefusalCarriesTheQuotaReason(t *testing.T) {
	status, p := problemFor(fmt.Errorf("agent x: llm: %w: today's allowance from Example is used up", substrate.ErrAllowance))
	if status != http.StatusForbidden || p.Code != codeGuard || p.Reason != substrate.ReasonQuota {
		t.Fatalf("problem = %d %+v", status, p)
	}
	if _, p := problemFor(fmt.Errorf("%w: spend cap", substrate.ErrGuard)); p.Reason != "" {
		t.Fatalf("a plain guard carries reason %q", p.Reason)
	}
}
