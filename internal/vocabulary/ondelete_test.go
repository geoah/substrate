package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// `onDelete: cascade` says the referent owns the record declaring it. It is
// declarable on the one shape the sweep can follow — a kind's own single-valued
// reference — and needs no pin, because the refs index finds the sources naming
// a referent whether or not the declaration says which kind they are.

// cascadeDocs is one authority holding an account kind and a mirror kind whose
// `account` property is filled in per case.
func cascadeDocs(account string) string {
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: own.example.com/own
data:
  authority: own.example.com
  package: own
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: own.example.com/own/account
data:
  authority: own.example.com
  package: own
  names: {singular: account, plural: accounts}
  properties:
    label: {type: string}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: own.example.com/own/mirror
data:
  authority: own.example.com
  package: own
  names: {singular: mirror, plural: mirrors}
  properties:
    account:
` + account
}

func loadCascade(t *testing.T, account string) (*vocabulary.Registry, error) {
	t.Helper()
	return vocabulary.LoadFS(fstest.MapFS{
		"own.yaml": {Data: []byte(cascadeDocs(account))},
	})
}

func TestCascadeOnAReference(t *testing.T) {
	r, err := loadCascade(t, "      type: reference\n      kind: account\n      onDelete: cascade\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mirror, ok := r.ByIdentity("own.example.com/own/mirror")
	if !ok {
		t.Fatal("mirror kind missing")
	}
	p, ok := mirror.Prop("account")
	if !ok {
		t.Fatal("account property missing")
	}
	if p.OnDelete != vocabulary.OnDeleteCascade || !p.Cascades() {
		t.Fatalf("onDelete = %q, want it to survive the parse", p.OnDelete)
	}
	// The pin resolves to a full identity, and the sweep matches on that.
	if p.To != "own.example.com/own/account" {
		t.Fatalf("pin = %q, want the resolved identity", p.To)
	}
}

// An UNPINNED cascade is legal: the refs index names the source rows whatever
// kind they point at, so the owner's sweep finds them without a pin.
func TestCascadeNeedsNoPin(t *testing.T) {
	for name, account := range map[string]string{
		"absent pin": "      type: reference\n      onDelete: cascade\n",
		"pinned any": "      type: reference\n      kind: any\n      onDelete: cascade\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, err := loadCascade(t, account)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			mirror, _ := r.ByIdentity("own.example.com/own/mirror")
			p, _ := mirror.Prop("account")
			if !p.Cascades() {
				t.Fatal("an unpinned cascade must be admitted")
			}
		})
	}
}

func TestCascadeRefusesTheShapesItCannotFollow(t *testing.T) {
	for name, tc := range map[string]struct{ account, want string }{
		"repeated": {
			account: "      type: reference\n      kind: account\n      repeated: true\n      onDelete: cascade\n",
			want:    "cascade names ONE owner",
		},
		"keyed": {
			account: "      type: reference\n      kind: account\n      keyed: true\n      onDelete: cascade\n",
			want:    "cascade names ONE owner",
		},
		"an object field": {
			account: "      type: object\n      fields:\n        held: {type: reference, kind: account, onDelete: cascade}\n",
			want:    "never an object field",
		},
		"an unknown behavior": {
			account: "      type: reference\n      kind: account\n      onDelete: detach\n",
			want:    `"detach" is not a behavior`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadCascade(t, tc.account)
			if err == nil {
				t.Fatal("expected the declaration to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// `ownerRef` was the key cascade replaced, so the refusal names the replacement
// rather than reporting an unknown key.
func TestOwnerRefNamesOnDelete(t *testing.T) {
	_, err := loadCascade(t, "      type: reference\n      kind: account\n      ownerRef: true\n")
	if err == nil {
		t.Fatal("expected `ownerRef` to be refused")
	}
	for _, want := range []string{`key "ownerRef" is deleted`, "`onDelete: cascade`"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must carry %q, got: %v", want, err)
		}
	}
}

// traitPinDocs is one authority holding a trait, an account kind that
// implements it, and a mirror kind whose `account` property is filled per case.
func traitPinDocs(account string) string {
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: own.example.com/own
data:
  authority: own.example.com
  package: own
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: own.example.com/own/connected
data:
  authority: own.example.com
  package: own
  properties:
    tokenRef: secret
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: own.example.com/own/account
data:
  authority: own.example.com
  package: own
  names: {singular: account, plural: accounts}
  traits: [connected]
  properties:
    tokenRef: {type: secret}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: own.example.com/own/mirror
data:
  authority: own.example.com
  package: own
  names: {singular: mirror, plural: mirrors}
  properties:
    account:
` + account
}

// A cascade is declarable on a TRAIT-pinned reference: a provider-agnostic kind
// owns any record whose kind implements the trait.
func TestCascadeOnATraitReference(t *testing.T) {
	r, err := vocabulary.LoadFS(fstest.MapFS{
		"own.yaml": {Data: []byte(traitPinDocs("      type: reference\n      trait: connected\n      onDelete: cascade\n"))},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mirror, ok := r.ByIdentity("own.example.com/own/mirror")
	if !ok {
		t.Fatal("mirror kind missing")
	}
	p, ok := mirror.Prop("account")
	if !ok {
		t.Fatal("account property missing")
	}
	if !p.Cascades() {
		t.Fatal("onDelete did not survive the parse")
	}
	if p.To != "" {
		t.Fatalf("To = %q, want empty for a trait pin", p.To)
	}
	// The trait pin resolves to the full trait identity, the way a binding does.
	if p.ToTrait != "own.example.com/own/connected" {
		t.Fatalf("ToTrait = %q, want the resolved trait identity", p.ToTrait)
	}
}

// A reference pins one kind of thing: naming both `kind:` and `trait:` is
// refused rather than resolved to one of them.
func TestReferenceRefusesBothPins(t *testing.T) {
	_, err := vocabulary.LoadFS(fstest.MapFS{
		"own.yaml": {Data: []byte(traitPinDocs("      type: reference\n      kind: account\n      trait: connected\n      onDelete: cascade\n"))},
	})
	if err == nil || !strings.Contains(err.Error(), "pins `kind:` OR `trait:`") {
		t.Fatalf("error = %v, want it to refuse both pins", err)
	}
}

// A non-reference property never takes the reference keys at all: they are not
// in the ordinary property key set, so a string that claims one is refused as
// unknown rather than silently ignored.
func TestReferenceKeysAreNotStringPropertyKeys(t *testing.T) {
	for _, key := range []string{"onDelete: cascade", "mustExist: true", "subject: true"} {
		t.Run(key, func(t *testing.T) {
			_, err := loadCascade(t, "      type: string\n      "+key+"\n")
			name, _, _ := strings.Cut(key, ":")
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error = %v, want it to refuse %q", err, name)
			}
		})
	}
}
