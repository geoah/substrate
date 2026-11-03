package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	refPinBadgeKind = "test.example.com/nest/badge"
	refPinTrait     = "test.example.com/nest/named"
)

// withRefPinBadge declares a kind whose `holder` reference is pinned to a
// trait only the person kind implements.
func (e *appEnv) withRefPinBadge() {
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.types = append(ds.types, substrate.KindInfo{
		Identity: refPinBadgeKind, Name: "badge", Authority: "test.example.com", Package: "nest", Version: 1, Source: "installed",
		Definition: map[string]any{"properties": map[string]any{
			"holder": map[string]any{"type": "reference", "trait": refPinTrait},
		}},
	})
	ds.traits[refPinTrait] = []string{personKind}
}

// The engine resolves a path of a kind the pin does not admit through a
// record mapping onto the pinned kind, so a readable mirror's path would
// land the write on, and title it from, a record of a kind the grant cannot
// read.
func TestScopedRefPinConcreteNeedsTheMappedTargetReadable(t *testing.T) {
	e := newAppEnv(t)
	put := func(app string) int {
		body := `{"properties":{"author":"` + taskKind + `/c1"}}`
		return e.do(http.MethodPut, "/api/v1/"+messageKind+"/x1", app, []byte(body), "application/json").Code
	}
	mirror := e.grant("write:"+messageKind+" read:"+taskKind, []string{"write:" + messageKind, "read:" + taskKind})
	if got := put(mirror); got != http.StatusForbidden {
		t.Fatalf("a readable path under a pin the grant cannot read answered %d, want 403", got)
	}
	both := e.grant("write:"+messageKind+" read:"+taskKind+" read:"+personKind,
		[]string{"write:" + messageKind, "read:" + taskKind, "read:" + personKind})
	if got := put(both); got != http.StatusOK && got != http.StatusCreated {
		t.Fatalf("the same write by a grant that reads the pin answered %d", got)
	}
}

func TestScopedRefPinTraitNeedsEveryImplementerReadable(t *testing.T) {
	e := newAppEnv(t)
	e.withRefPinBadge()
	put := func(app, value string) int {
		body := `{"properties":{"holder":"` + value + `"}}`
		return e.do(http.MethodPut, "/api/v1/"+refPinBadgeKind+"/b1", app, []byte(body), "application/json").Code
	}
	mirror := e.grant("write:"+refPinBadgeKind+" read:"+taskKind, []string{"write:" + refPinBadgeKind, "read:" + taskKind})
	if got := put(mirror, taskKind+"/c1"); got != http.StatusForbidden {
		t.Fatalf("a path that does not implement the trait, under a grant that cannot read the implementer, answered %d, want 403", got)
	}
	people := e.grant("write:"+refPinBadgeKind+" read:"+personKind, []string{"write:" + refPinBadgeKind, "read:" + personKind})
	if got := put(people, personKind+"/p1"); got != http.StatusOK && got != http.StatusCreated {
		t.Fatalf("a readable implementer's path answered %d", got)
	}
	both := e.grant("write:"+refPinBadgeKind+" read:"+taskKind+" read:"+personKind,
		[]string{"write:" + refPinBadgeKind, "read:" + taskKind, "read:" + personKind})
	if got := put(both, taskKind+"/c1"); got != http.StatusOK && got != http.StatusCreated {
		t.Fatalf("a mirror's path under a grant reading every implementer answered %d", got)
	}
}
