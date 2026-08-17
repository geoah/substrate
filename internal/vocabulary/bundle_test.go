package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// bnAuthority renders a minimal one-kind bundle closure on the given
// authority, shipping a kind called `widget`.
func bnAuthority(authority string) string { return bnAuthorityKind(authority, "widget") }

// bnAuthorityKind is the same closure with the kind named, so two authorities
// can share a first label without claiming one GraphQL name.
func bnAuthorityKind(authority, kind string) string {
	name, _, _ := strings.Cut(authority, ".")
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: ` + authority + `
data:
  version: 1
---
kind: core.substrate.reamde.dev/bundle
metadata:
  id: ` + authority + `/` + name + `
data:
  authority: ` + authority + `
  description: one kind, so the closure is whole
  installs:
    - ` + authority + `/` + kind + `
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ` + authority + `/` + kind + `
data:
  authority: ` + authority + `
  names: {singular: ` + kind + `, plural: ` + name + kind + `s}
  properties:
    name: {type: string}
`
}

func loadBnAuthorities(bodies ...string) (*vocabulary.Registry, error) {
	fsys := fstest.MapFS{}
	for i, body := range bodies {
		fsys[string(rune('a'+i))+".yaml"] = &fstest.MapFile{Data: []byte(body)}
	}
	return vocabulary.LoadFS(fsys)
}

// An extension bundle's authority is NOT a name rule: any legal DNS-style
// authority owns one, and its first label is the bundle's name whatever the
// labels behind it say. Only the bare org-domain shape means something, because
// that one is built `builtin` — so it stays checked, and everything else is
// admitted on its shape alone.
func TestBundleAuthorityIsAnyLegalAuthority(t *testing.T) {
	for _, authority := range []string{
		"llm.examples.substrate.reamde.dev",
		"web.bundles.substrate.reamde.dev",
		"harvest.tools.example.com",
		"scraper.example.com",
	} {
		r, err := loadBnAuthorities(bnAuthority(authority))
		if err != nil {
			t.Fatalf("%s: %v", authority, err)
		}
		g, ok := r.AuthorityByName(authority)
		if !ok || g.Bundle == nil {
			t.Fatalf("%s: no bundle", authority)
		}
		name, _, _ := strings.Cut(authority, ".")
		if g.Bundle.Name != name {
			t.Errorf("%s: name %q, want %q", authority, g.Bundle.Name, name)
		}
		if g.Bundle.Vocabulary {
			t.Errorf("%s: reads as shipped vocabulary", authority)
		}
	}
}

// The bare org-domain label is the one shape that GRANTS — a vocabulary bundle
// is built `builtin` whichever door it came through — so it is still told apart
// from every other authority, and still refuses anything but kinds.
func TestVocabularyAuthorityStaysTheGrantingShape(t *testing.T) {
	r, err := loadBnAuthorities(bnAuthority("places.substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	g, ok := r.AuthorityByName("places.substrate.reamde.dev")
	if !ok || g.Bundle == nil {
		t.Fatal("no bundle")
	}
	if !g.Bundle.Vocabulary {
		t.Error("a bare org-domain authority must read as a vocabulary bundle")
	}
	if !vocabulary.ValidVocabularyAuthority("places.substrate.reamde.dev") ||
		vocabulary.ValidBundleAuthority("places.substrate.reamde.dev") {
		t.Error("the two owned-authority shapes must stay disjoint")
	}
}

// The first label is the bundle's name, the actor an install writes under and
// every installed kind's GraphQL prefix, so it has to be a plain word — a
// hyphen there is refused at the authority, not silently sanitized downstream.
func TestBundleAuthorityFirstLabelMustBeAWord(t *testing.T) {
	_, err := loadBnAuthorities(bnAuthority("my-llm.examples.example.com"))
	if err == nil || !strings.Contains(err.Error(), "FIRST label") {
		t.Fatalf("a hyphenated first label must refuse: %v", err)
	}
}

// Two authorities may share a first label. An install writes under
// `bundle:<authority>` (record 0025), so the two are two writers: two
// attributions, two sets of manager rows, and neither one's writes read as the
// other's trigger echo. This used to be refused at declaration time
// (bundleNameProblems), which is why the label is asserted shared here.
func TestTwoAuthoritiesMayShareABundleName(t *testing.T) {
	one := "llm.examples.substrate.reamde.dev"
	two := "llm.bundles.substrate.reamde.dev"
	r, err := loadBnAuthorities(bnAuthorityKind(one, "widget"), bnAuthorityKind(two, "gadget"))
	if err != nil {
		t.Fatalf("a shared first label must load: %v", err)
	}
	g1, _ := r.AuthorityByName(one)
	g2, _ := r.AuthorityByName(two)
	if g1.Bundle.Name != "llm" || g2.Bundle.Name != "llm" {
		t.Fatalf("the labels must still be shared: %q and %q", g1.Bundle.Name, g2.Bundle.Name)
	}
	a1, a2 := vocabulary.AuthorityActor(one), vocabulary.AuthorityActor(two)
	if a1 == a2 {
		t.Fatalf("two authorities share the actor %q", a1)
	}
	if a1 != "bundle:"+one || a2 != "bundle:"+two {
		t.Fatalf("actors %q and %q — an actor carries the full authority", a1, a2)
	}
}

// One GraphQL name is still one kind: what two authorities sharing a first
// label may NOT do is claim the same installed GraphQL name, and the refusal
// names both kinds.
func TestOneGraphQLNameIsStillOneKind(t *testing.T) {
	_, err := loadBnAuthorities(
		bnAuthorityKind("llm.examples.substrate.reamde.dev", "widget"),
		bnAuthorityKind("llm.bundles.substrate.reamde.dev", "widget"),
	)
	if err == nil || !strings.Contains(err.Error(), "graphql name Widget is claimed by") {
		t.Fatalf("one GraphQL name claimed twice must refuse: %v", err)
	}
}
