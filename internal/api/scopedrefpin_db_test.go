package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// refPinDeclare declares, under the repository's own authority, a holder kind
// implementing a trait, a mirror kind whose record mapping leads onto the
// holder, and two kinds whose `holder` reference the engine resolves through
// that mapping: one pinned to the holder kind, one to the trait. It writes the
// mirror m1, whose mapping mints its holder. It answers the package.
func refPinDeclare(t *testing.T, env *dbEnv) string {
	t.Helper()
	pkg := env.repository + "/refpin"
	env.declare(t, `
kind: substrate.reamde.dev/core/package
metadata:
  id: `+pkg+`
data:
  authority: `+env.repository+`
  package: refpin
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: `+pkg+`/named
data:
  authority: `+env.repository+`
  package: refpin
  description: a thing with a name
  properties:
    name: string
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+pkg+`/holder
data:
  authority: `+env.repository+`
  package: refpin
  names:
    singular: holder
  traits:
    - `+pkg+`/named
  properties:
    name:
      type: string
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+pkg+`/mirror
data:
  authority: `+env.repository+`
  package: refpin
  names:
    singular: mirror
  properties:
    name:
      type: string
    holder:
      type: reference
      kind: `+pkg+`/holder
      subject: true
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: `+pkg+`/mirrorholder
data:
  authority: `+env.repository+`
  package: refpin
  from: `+pkg+`/mirror
  to: `+pkg+`/holder
  property: holder
  match:
    - from: name
      to: name
  map:
    name:
      path: name
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+pkg+`/note
data:
  authority: `+env.repository+`
  package: refpin
  names:
    singular: note
  properties:
    holder:
      type: reference
      kind: `+pkg+`/holder
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: `+pkg+`/badge
data:
  authority: `+env.repository+`
  package: refpin
  names:
    singular: badge
  properties:
    holder:
      type: reference
      trait: `+pkg+`/named
`)
	env.put(t, pkg+"/mirror", "m1", map[string]any{"name": "Ada"})
	return pkg
}

// The engine resolves a path of a kind the pin does not admit through a
// record mapping onto the pinned kind, so a readable mirror's path would
// land the write on, and title it from, a record of a kind the grant cannot
// read.
func TestScopedRefPinConcreteNeedsTheMappedTargetReadable(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	pkg := refPinDeclare(t, env)
	note, mirror, holder := pkg+"/note", pkg+"/mirror", pkg+"/holder"
	put := func(app, id string) int {
		body := []byte(`{"properties":{"holder":"` + mirror + `/m1"}}`)
		return appScopedDo(t, env, http.MethodPut, "/api/v1/"+note+"/"+id, app, body, "application/json").Code
	}
	readsMirror := appScopedToken(t, env, "write:"+note, "read:"+mirror)
	if got := put(readsMirror, mintedID("x1")); got != http.StatusForbidden {
		t.Fatalf("a readable path under a pin the grant cannot read answered %d, want 403", got)
	}
	if env.get(t, note, mintedID("x1")) != nil {
		t.Fatal("the refused write stored the note")
	}
	both := appScopedToken(t, env, "write:"+note, "read:"+mirror, "read:"+holder)
	if got := put(both, mintedID("x2")); got != http.StatusOK && got != http.StatusCreated {
		t.Fatalf("the same write by a grant that reads the pin answered %d", got)
	}
	stored := env.get(t, note, mintedID("x2"))
	if kind, _, _ := vocabulary.SplitRecordPath(appScopedRefPath(stored.Properties["holder"])); kind != holder {
		t.Fatalf("the note's holder = %v, want the mirror's mapped holder", stored.Properties["holder"])
	}
}

func TestScopedRefPinTraitNeedsEveryImplementerReadable(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	pkg := refPinDeclare(t, env)
	badge, mirror, holder := pkg+"/badge", pkg+"/mirror", pkg+"/holder"
	// A holder's id is the engine's: the mirror's mapping minted Ada's.
	ada := appScopedRefPath(env.get(t, mirror, "m1").Properties["holder"])
	put := func(app, id, value string) int {
		body := []byte(`{"properties":{"holder":"` + value + `"}}`)
		return appScopedDo(t, env, http.MethodPut, "/api/v1/"+badge+"/"+id, app, body, "application/json").Code
	}
	readsMirror := appScopedToken(t, env, "write:"+badge, "read:"+mirror)
	if got := put(readsMirror, mintedID("b1"), mirror+"/m1"); got != http.StatusForbidden {
		t.Fatalf("a path that does not implement the trait, under a grant that cannot read the implementer, answered %d, want 403", got)
	}
	if env.get(t, badge, mintedID("b1")) != nil {
		t.Fatal("the refused write stored the badge")
	}
	holders := appScopedToken(t, env, "write:"+badge, "read:"+holder)
	if got := put(holders, mintedID("b2"), ada); got != http.StatusOK && got != http.StatusCreated {
		t.Fatalf("a readable implementer's path answered %d", got)
	}
	both := appScopedToken(t, env, "write:"+badge, "read:"+mirror, "read:"+holder)
	if got := put(both, mintedID("b3"), mirror+"/m1"); got != http.StatusOK && got != http.StatusCreated {
		t.Fatalf("a mirror's path under a grant reading every implementer answered %d", got)
	}
	stored := env.get(t, badge, mintedID("b3"))
	if kind, _, _ := vocabulary.SplitRecordPath(appScopedRefPath(stored.Properties["holder"])); kind != holder {
		t.Fatalf("the badge's holder = %v, want the mirror's mapped holder", stored.Properties["holder"])
	}
}
