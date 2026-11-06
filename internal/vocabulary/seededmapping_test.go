package vocabulary_test

// A seeded mapping waits instead of refusing (decision record 0232,
// seededmapping.go). The fixture is a core package holding one kind and one
// mapping from a provider's mirror onto it, the shape of core's mappings onto
// `contact`, and each case changes what else the registry holds.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	seededCard    = "substrate.reamde.dev/core/card"
	seededMapping = "substrate.reamde.dev/core/crmperson"
	crmPerson     = "providers.example.com/crm/person"
)

const seededCore = `kind: substrate.reamde.dev/core/package
metadata:
  id: substrate.reamde.dev/core
data:
  authority: substrate.reamde.dev
  package: core
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/card
data:
  authority: substrate.reamde.dev
  package: core
  names:
    singular: card
  properties:
    name:
      type: string
    emails:
      type: email
      repeated: true
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: substrate.reamde.dev/core/crmperson
data:
  authority: substrate.reamde.dev
  package: core
  from: providers.example.com/crm/person
  to: substrate.reamde.dev/core/card
  property: card
  match:
    - from: email
      to: emails
  map:
    name:
      path: fullName
    emails:
      path: email
      merge: union
`

// crmProvider is the provider's mirror; fullName is the path the mapping
// reads, so a provider without it is an older version the mapping does not
// fit.
func crmProvider(fullName bool) string {
	doc := `kind: substrate.reamde.dev/core/package
metadata:
  id: providers.example.com/crm
data:
  authority: providers.example.com
  package: crm
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: providers.example.com/crm/person
data:
  authority: providers.example.com
  package: crm
  names:
    singular: person
  properties:
    email:
      type: email
`
	if fullName {
		doc += `    fullName:
      type: string
`
	}
	return doc
}

// ownersPeoplePackage maps the same mirror through the same slot onto a kind of the
// owner's own.
const ownersPeoplePackage = `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/people
data:
  authority: ada.example.com
  package: people
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/people/person
data:
  authority: ada.example.com
  package: people
  names:
    singular: person
  properties:
    emails:
      type: email
      repeated: true
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: ada.example.com/people/crmperson
data:
  authority: ada.example.com
  package: people
  from: providers.example.com/crm/person
  to: ada.example.com/people/person
  property: card
  match:
    - from: email
      to: emails
  map:
    emails:
      path: email
      merge: union
`

func loadSeeded(t *testing.T, files map[string]string) *vocabulary.Registry {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	reg, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("the registry refused: %v", err)
	}
	return reg
}

func installSeeded(t *testing.T, reg *vocabulary.Registry, body string) error {
	t.Helper()
	docs, err := vocabulary.ParseStream([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	packages, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return reg.InstallAll(packages)
}

// heldReason is why the seeded mapping waits, or "" when it is live.
func heldReason(reg *vocabulary.Registry) string {
	for _, h := range reg.HeldMappings() {
		if h.Mapping.Identity() == seededMapping {
			return h.Reason
		}
	}
	return ""
}

func liveOnto(reg *vocabulary.Registry, to string) []string {
	var ids []string
	for _, m := range reg.MappingsTo(to) {
		ids = append(ids, m.Identity())
	}
	return ids
}

func TestASeededMappingWaitsForItsProvider(t *testing.T) {
	reg := loadSeeded(t, map[string]string{"core/core.yaml": seededCore})
	if reason := heldReason(reg); !strings.Contains(reason, crmPerson+" is not installed") {
		t.Fatalf("with no provider, the mapping waits because %q", reason)
	}
	if ids := liveOnto(reg, seededCard); len(ids) != 0 {
		t.Fatalf("a waiting mapping is live: %v", ids)
	}

	// The install switches it on, and the mirror gets its slot.
	if err := installSeeded(t, reg, crmProvider(true)); err != nil {
		t.Fatalf("installing the provider: %v", err)
	}
	if reason := heldReason(reg); reason != "" {
		t.Fatalf("with the provider installed, the mapping still waits: %s", reason)
	}
	if ids := liveOnto(reg, seededCard); len(ids) != 1 || ids[0] != seededMapping {
		t.Fatalf("live mappings onto the card: %v", ids)
	}
	person, _ := reg.ByIdentity(crmPerson)
	if slot, ok := person.Props["card"]; !ok || slot.MappedBy != seededMapping {
		t.Fatalf("the mirror carries no slot from the seeded mapping: %+v", person.Props["card"])
	}

	// Removing the provider is not refused, and the mapping waits again.
	reg.Remove("providers.example.com/crm")
	if reason := heldReason(reg); !strings.Contains(reason, "is not installed") {
		t.Fatalf("after the provider left, the mapping waits because %q", reason)
	}
}

func TestASeededMappingWaitsForAProviderItDoesNotFit(t *testing.T) {
	reg := loadSeeded(t, map[string]string{
		"core/core.yaml": seededCore,
		"crm/crm.yaml":   crmProvider(false),
	})
	if reason := heldReason(reg); !strings.Contains(reason, "does not fit "+crmPerson) || !strings.Contains(reason, "fullName") {
		t.Fatalf("against an older provider, the mapping waits because %q", reason)
	}
	person, _ := reg.ByIdentity(crmPerson)
	if _, ok := person.Props["card"]; ok {
		t.Fatal("a waiting mapping gave the mirror its slot")
	}
}

func TestASeededMappingYieldsItsSlotToTheOwnersMapping(t *testing.T) {
	reg := loadSeeded(t, map[string]string{
		"core/core.yaml":     seededCore,
		"crm/crm.yaml":       crmProvider(true),
		"people/people.yaml": ownersPeoplePackage,
	})
	if reason := heldReason(reg); !strings.Contains(reason, "ada.example.com/people/crmperson already fills "+crmPerson+".card") {
		t.Fatalf("beside the owner's mapping on the same slot, the seeded one waits because %q", reason)
	}
	person, _ := reg.ByIdentity(crmPerson)
	if slot := person.Props["card"]; slot == nil || slot.MappedBy != "ada.example.com/people/crmperson" {
		t.Fatalf("the slot is not the owner's mapping's: %+v", slot)
	}
}

// A mapping of any other package still refuses where it does not fit: the
// wait is the seeded package's alone.
func TestAnOwnersMappingStillRefusesWithoutItsProvider(t *testing.T) {
	fsys := fstest.MapFS{"people/people.yaml": &fstest.MapFile{Data: []byte(ownersPeoplePackage)}}
	_, err := vocabulary.LoadFS(fsys)
	if err == nil || !strings.Contains(err.Error(), "which this repository does not have") {
		t.Fatalf("the owner's mapping without its provider: %v", err)
	}
}
