package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// A declared title template that reads through a reference copies the
// referent's title or property into the record's title, so an app declares
// one only over a reference whose every possible target it reads.
func TestScopedRefPinDeclaredTemplateReadsOnlyReadableTargets(t *testing.T) {
	e := newAppEnv(t)
	e.withRefPinBadge()
	pkg := fakeRepository + "/training"
	scopes := "declare:~/training write:~/training/*"
	granted := []string{"declare:" + pkg, "write:" + pkg + "/*"}
	blind := e.grant(scopes, granted)
	reader := e.grant(scopes+" read:"+personKind, append(granted, "read:"+personKind))
	apply := func(app, template string) *httptest.ResponseRecorder {
		doc := map[string]any{
			"kind": corePackage + "/kind", "metadata": map[string]any{"id": pkg + "/note"},
			"data": map[string]any{
				"displayTemplate": template,
				"properties": map[string]any{
					"text":    map[string]any{"type": "string"},
					"person":  map[string]any{"type": "reference", "kind": personKind},
					"people":  map[string]any{"type": "reference", "kind": personKind, "repeated": true},
					"named":   map[string]any{"type": "reference", "trait": refPinTrait},
					"about":   map[string]any{"type": "reference", "kind": "any"},
					"anybody": map[string]any{"type": "reference"},
				},
			},
		}
		raw, _ := json.Marshal(map[string]any{"documents": []any{doc}})
		return e.do(http.MethodPost, "/api/v1/vocabulary/apply", app, raw, "application/json")
	}
	for _, template := range []string{"{person.privateNote}", "{person}", "{people[].privateNote}", "{text|person}", "{named.privateNote}"} {
		if rec := apply(blind, template); rec.Code != http.StatusForbidden {
			t.Errorf("%s by a grant that cannot read the target: %d %s, want 403", template, rec.Code, rec.Body.String())
		}
		if rec := apply(reader, template); rec.Code != http.StatusOK {
			t.Errorf("%s by a grant that reads the target: %d %s, want 200", template, rec.Code, rec.Body.String())
		}
	}
	for _, template := range []string{"{about}", "{anybody.name}"} {
		if rec := apply(reader, template); rec.Code != http.StatusForbidden {
			t.Errorf("%s over an unpinned reference: %d %s, want 403", template, rec.Code, rec.Body.String())
		}
	}
	if rec := apply(blind, "{text}"); rec.Code != http.StatusOK {
		t.Errorf("a template reading no reference: %d %s, want 200", rec.Code, rec.Body.String())
	}
}
