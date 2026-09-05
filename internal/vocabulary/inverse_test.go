package vocabulary_test

// `inverse:` names a relationship as the TARGET reads it — `thread` on a
// message is `messages` on the thread — so a reader standing on the referent
// can say what points at it in the model's own words.
//
// It is a LABEL, never an identifier. Nothing resolves through it, so a
// collision between two AUTHORITIES is legal and reads as two groups sharing a
// word. Only a collision inside ONE authority is refused: there it is the same
// author telling a reader two different things by one name.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// inverseFixture builds a one-package tree: a `thing`, and a `pointer` whose
// references are declared by the caller. The package takes the authority's
// first label, so the fixture names one package per authority.
func inverseFixture(authority, pointerBody string) string {
	pkg := packageOf(authority)
	return `kind: substrate.reamde.dev/core/package
metadata: {id: ` + authority + `/` + pkg + `}
data: {authority: ` + authority + `, package: ` + pkg + `, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + authority + `/` + pkg + `/thing}
data:
  authority: ` + authority + `
  package: ` + pkg + `
  names: {singular: thing, plural: things}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + authority + `/` + pkg + `/pointer}
data:
  authority: ` + authority + `
  package: ` + pkg + `
  names: {singular: pointer, plural: pointers}
` + pointerBody
}

// packageOf is the package a fixture authority publishes: its first label.
func packageOf(authority string) string {
	label, _, _ := strings.Cut(authority, ".")
	return label
}

// loadInverse loads a fixture and RETURNS the error, which is the point in
// this file: most of these cases are refusals.
func loadInverse(t *testing.T, files map[string]string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return vocabulary.LoadFS(fsys)
}

func TestInverseIsCarriedByAReference(t *testing.T) {
	r := loadFixture(t, map[string]string{"a.yaml": inverseFixture("a.example.com", `  properties:
    owner:
      type: reference
      kind: thing
      inverse: owned
      inverseDescription: what this thing owns
    seen:
      type: reference
      kind: thing
      inverse: seenBy
`)})
	p, _ := r.ByIdentity("a.example.com/a/pointer")
	if got := p.Props["owner"].Inverse; got != "owned" {
		t.Fatalf("inverse = %q", got)
	}
	if got := p.Props["owner"].InverseDescription; got != "what this thing owns" {
		t.Fatalf("inverse description = %q", got)
	}
	if got := p.Props["seen"].Inverse; got != "seenBy" {
		t.Fatalf("reference inverse = %q", got)
	}
}

func TestInverseMustBeSpelledLikeADeclaredName(t *testing.T) {
	_, err := loadInverse(t, map[string]string{"a.yaml": inverseFixture("a.example.com", `  properties:
    owner: {type: reference, kind: thing, inverse: not_camel}
`)})
	if err == nil || !strings.Contains(err.Error(), "inverse") {
		t.Fatalf("a snake-cased inverse must be refused, got %v", err)
	}
}

func TestInverseDescriptionWithoutAnInverseIsRefused(t *testing.T) {
	_, err := loadInverse(t, map[string]string{"a.yaml": inverseFixture("a.example.com", `  properties:
    owner: {type: reference, kind: thing, inverseDescription: describes nothing}
`)})
	if err == nil || !strings.Contains(err.Error(), "inverseDescription") {
		t.Fatalf("a description of an undeclared inverse must be refused, got %v", err)
	}
}

func TestInverseCollisionInsideOneAuthorityIsRefused(t *testing.T) {
	_, err := loadInverse(t, map[string]string{"a.yaml": inverseFixture("a.example.com", `  properties:
    owner: {type: reference, kind: thing, inverse: related}
    seen:
      type: reference
      kind: thing
      inverse: related
`)})
	if err == nil {
		t.Fatal("one authority claiming one inverse twice on one target must be refused")
	}
	for _, want := range []string{"related", "already claimed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got: %v", want, err)
		}
	}
}

func TestInverseCollisionAcrossAuthoritiesIsLegal(t *testing.T) {
	// Two bundles both calling their side `mentions` is not an error and must
	// never be one: an inverse resolves nothing, so the reader sees two groups
	// sharing a word, each named by the kind it comes from. Refusing it would
	// let whichever bundle installed first own a common word and break every
	// install that came after.
	shared := `kind: substrate.reamde.dev/core/package
metadata: {id: shared.example.com/shared}
data: {authority: shared.example.com, package: shared, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: shared.example.com/shared/note}
data:
  authority: shared.example.com
  package: shared
  names: {singular: note, plural: notes}
`
	pointerAt := func(authority, name string) string {
		pkg := packageOf(authority)
		return `kind: substrate.reamde.dev/core/package
metadata: {id: ` + authority + `/` + pkg + `}
data: {authority: ` + authority + `, package: ` + pkg + `, version: 1}
---
kind: substrate.reamde.dev/core/kind
metadata: {id: ` + authority + `/` + pkg + `/` + name + `}
data:
  authority: ` + authority + `
  package: ` + pkg + `
  names: {singular: ` + name + `, plural: ` + name + `s}
  properties:
    note:
      type: reference
      kind: shared.example.com/shared/note
      inverse: mentions
`
	}
	r := loadFixture(t, map[string]string{
		"shared.yaml": shared,
		"one.yaml":    pointerAt("one.example.com", "alpha"),
		"two.yaml":    pointerAt("two.example.com", "beta"),
	})
	for _, id := range []string{"one.example.com/one/alpha", "two.example.com/two/beta"} {
		k, ok := r.ByIdentity(id)
		if !ok {
			t.Fatalf("%s did not load", id)
		}
		if got := k.Props["note"].Inverse; got != "mentions" {
			t.Fatalf("%s inverse = %q", id, got)
		}
	}
}

func TestInverseOnAnUnconstrainedPointerCannotCollide(t *testing.T) {
	// `kind: any` names no referent kind, so two of them share no key to collide
	// on.
	r := loadFixture(t, map[string]string{"a.yaml": inverseFixture("a.example.com", `  properties:
    one:
      type: reference
      kind: any
      inverse: related
    two:
      type: reference
      kind: any
      inverse: related
`)})
	p, _ := r.ByIdentity("a.example.com/a/pointer")
	if p.Props["one"].Inverse != "related" || p.Props["two"].Inverse != "related" {
		t.Fatal("unconstrained pointers keep their labels")
	}
}

// One name is one property: a kind's `properties:` block is a YAML mapping, so
// two declarations of one name cannot both survive the parse, and the
// relationship a name points at is whatever that one declaration says.
func TestOneNameIsOnePointer(t *testing.T) {
	r := loadFixture(t, map[string]string{"a.yaml": inverseFixture("a.example.com", `  properties:
    owner:
      type: reference
      kind: thing
      inverse: owned
`)})
	p, _ := r.ByIdentity("a.example.com/a/pointer")
	owner, ok := p.Prop("owner")
	if !ok || owner.Datatype != vocabulary.DatatypeReference || owner.Inverse != "owned" {
		t.Fatalf("owner = %+v", owner)
	}
}
