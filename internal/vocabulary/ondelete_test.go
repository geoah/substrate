package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// `ownerRef` is declarable on a REFERENCE, not only on an edge (decision record
// 0032), and only on the one shape the sweep can follow: a kind's own
// single-valued pointer with `kind:` pinned at one kind.

// ownerRefDocs is one authority holding an account kind and a mirror kind whose
// `account` property is filled in per case.
func ownerRefDocs(account string) string {
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: own.example.com
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: own.example.com/account
data:
  authority: own.example.com
  names: {singular: account, plural: accounts}
  properties:
    label: {type: string}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: own.example.com/mirror
data:
  authority: own.example.com
  names: {singular: mirror, plural: mirrors}
  properties:
    account:
` + account
}

func loadOwnerRef(t *testing.T, account string) (*vocabulary.Registry, error) {
	t.Helper()
	return vocabulary.LoadFS(fstest.MapFS{
		"own.yaml": {Data: []byte(ownerRefDocs(account))},
	})
}

func TestOwnerRefOnAReference(t *testing.T) {
	r, err := loadOwnerRef(t, "      type: reference\n      kind: account\n      ownerRef: true\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mirror, ok := r.ByIdentity("own.example.com/mirror")
	if !ok {
		t.Fatal("mirror kind missing")
	}
	p, ok := mirror.Prop("account")
	if !ok {
		t.Fatal("account property missing")
	}
	if !p.OwnerRef {
		t.Fatal("ownerRef did not survive the parse")
	}
	// The pin resolves the same way an edge's `to:` does, and the sweep matches
	// on the resolved identity.
	if p.To != "own.example.com/account" {
		t.Fatalf("pin = %q, want the resolved identity", p.To)
	}
}

func TestOwnerRefRefusesTheShapesItCannotFollow(t *testing.T) {
	for name, tc := range map[string]struct{ account, want string }{
		"repeated": {
			account: "      type: reference\n      kind: account\n      repeated: true\n      ownerRef: true\n",
			want:    "names ONE owner",
		},
		"keyed": {
			account: "      type: reference\n      kind: account\n      keyed: true\n      ownerRef: true\n",
			want:    "names ONE owner",
		},
		"unpinned": {
			account: "      type: reference\n      ownerRef: true\n",
			want:    "needs `kind:` or `trait:` pinned",
		},
		"pinned at any": {
			account: "      type: reference\n      kind: any\n      ownerRef: true\n",
			want:    "needs `kind:` or `trait:` pinned",
		},
		"an object field": {
			account: "      type: object\n      fields:\n        held: {type: reference, kind: account, ownerRef: true}\n",
			want:    "never an object field",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadOwnerRef(t, tc.account)
			if err == nil {
				t.Fatal("expected the declaration to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// traitOwnerRefDocs is one authority holding a trait, an account kind that
// implements it, and a mirror kind whose `account` property is filled per case.
func traitOwnerRefDocs(account string) string {
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: own.example.com
data:
  version: 1
---
kind: core.substrate.reamde.dev/trait
metadata:
  id: own.example.com/connected
data:
  authority: own.example.com
  properties:
    tokenRef: secret
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: own.example.com/account
data:
  authority: own.example.com
  names: {singular: account, plural: accounts}
  traits: [connected]
  properties:
    tokenRef: {type: secret}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: own.example.com/mirror
data:
  authority: own.example.com
  names: {singular: mirror, plural: mirrors}
  properties:
    account:
` + account
}

// `ownerRef` is declarable on a TRAIT-pinned reference (0034): a
// provider-agnostic kind owns any record whose kind implements the trait, and
// the pin stays enumerable because the kinds implementing a trait are finite.
func TestOwnerRefOnATraitReference(t *testing.T) {
	r, err := vocabulary.LoadFS(fstest.MapFS{
		"own.yaml": {Data: []byte(traitOwnerRefDocs("      type: reference\n      trait: connected\n      ownerRef: true\n"))},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mirror, ok := r.ByIdentity("own.example.com/mirror")
	if !ok {
		t.Fatal("mirror kind missing")
	}
	p, ok := mirror.Prop("account")
	if !ok {
		t.Fatal("account property missing")
	}
	if !p.OwnerRef {
		t.Fatal("ownerRef did not survive the parse")
	}
	if p.To != "" {
		t.Fatalf("To = %q, want empty for a trait pin", p.To)
	}
	// The trait pin resolves to the full trait identity, the way a binding does.
	if p.ToTrait != "own.example.com/connected" {
		t.Fatalf("ToTrait = %q, want the resolved trait identity", p.ToTrait)
	}
}

// A reference pins one kind of thing: naming both `kind:` and `trait:` is
// refused rather than resolved to one of them.
func TestReferenceRefusesBothPins(t *testing.T) {
	_, err := vocabulary.LoadFS(fstest.MapFS{
		"own.yaml": {Data: []byte(traitOwnerRefDocs("      type: reference\n      kind: account\n      trait: connected\n      ownerRef: true\n"))},
	})
	if err == nil || !strings.Contains(err.Error(), "pins `kind:` OR `trait:`") {
		t.Fatalf("error = %v, want it to refuse both pins", err)
	}
}

// A non-reference property never takes the key at all: `ownerRef` is not in the
// ordinary property key set, so a string that claims one is refused as unknown
// rather than silently ignored.
func TestOwnerRefIsNotAStringPropertyKey(t *testing.T) {
	_, err := loadOwnerRef(t, "      type: string\n      ownerRef: true\n")
	if err == nil || !strings.Contains(err.Error(), "ownerRef") {
		t.Fatalf("error = %v, want it to refuse the key", err)
	}
}
