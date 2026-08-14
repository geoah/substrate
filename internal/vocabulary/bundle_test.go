package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// bnAuthority renders a minimal one-kind bundle closure on the given authority.
func bnAuthority(authority string) string {
	name, _, _ := strings.Cut(authority, ".")
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: ` + authority + `
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/bundle
metadata:
  id: ` + authority + `/` + name + `
data:
  authority: ` + authority + `
  description: one kind, so the closure is whole
  installs:
    - ` + authority + `/widget
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ` + authority + `/widget
data:
  authority: ` + authority + `
  names: {singular: widget, plural: ` + name + `widgets}
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

// What the fixed `.bundles.` suffix used to guarantee by construction — one
// first label, one authority — is now checked, because two free authorities can
// reach for one label. Both would write as `bundle:llm`, and the trigger loop
// guard keys on exactly that name, so the second is refused at declaration time
// rather than silently swallowing the first's writes.
func TestTwoAuthoritiesCannotShareABundleName(t *testing.T) {
	_, err := loadBnAuthorities(
		bnAuthority("llm.examples.substrate.reamde.dev"),
		bnAuthority("llm.bundles.substrate.reamde.dev"),
	)
	if err == nil || !strings.Contains(err.Error(), "bundle name llm is claimed by") {
		t.Fatalf("a shared first label must refuse: %v", err)
	}
}
