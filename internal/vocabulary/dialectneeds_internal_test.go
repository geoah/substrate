package vocabulary

// The keys a package's need is read from include those read outside
// checkKeys: a quantity bound's, which the quantity parse reads, and a
// shipped record's, which the tree's split reads (shippedRecordFrom). These
// tests enter a key of each at a dialect past this server's for their
// length. Package-level keyIntroducedAt: no t.Parallel.

import (
	"maps"
	"strings"
	"testing"
	"testing/fstest"
)

// introducing enters key of the named set at dialect at for the test.
func introducing(t *testing.T, set, key string, at int64) {
	t.Helper()
	prev := keyIntroducedAt
	next := maps.Clone(prev)
	next[set] = maps.Clone(prev[set])
	if next[set] == nil {
		next[set] = map[string]int64{}
	}
	next[set][key] = at
	keyIntroducedAt = next
	t.Cleanup(func() { keyIntroducedAt = prev })
}

const neededHeader = `kind: substrate.reamde.dev/core/package
metadata:
  id: g.example.com/g
data:
  authority: g.example.com
  package: g
  version: 1
  dialect: 3
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: g.example.com/g/thing
data:
  authority: g.example.com
  package: g
  names:
    singular: thing
  properties:
    weight:
      type: quantity
      unit: kg
      min: {value: "0", unit: kg}
`

func TestAQuantityBoundsKeysCountTowardTheDialectNeeded(t *testing.T) {
	fsys := fstest.MapFS{"a.yaml": &fstest.MapFile{Data: []byte(neededHeader)}}
	if _, err := LoadFS(fsys); err != nil {
		t.Fatalf("a quantity with a bound: %v", err)
	}
	introducing(t, "quantityBoundKeys", "unit", Dialect+1)
	_, err := LoadFS(fsys)
	if err == nil || !strings.Contains(err.Error(), "the package states dialect 3, and its declarations use `unit`, which dialect 4 introduced") {
		t.Fatalf("got %v, want the bound's unit named as dialect 4's", err)
	}
}

func TestAShippedRecordsKeysCountTowardItsPackagesDialect(t *testing.T) {
	tree := fstest.MapFS{"a.yaml": &fstest.MapFile{Data: []byte(neededHeader + `---
kind: g.example.com/g/thing
metadata:
  id: heavy
data:
  properties:
    weight: {value: "3", unit: kg}
`)}}
	r, _, err := LoadShipped(tree)
	if err != nil {
		t.Fatalf("a tree whose record's keys every dialect reads: %v", err)
	}
	if g, _ := r.PackageByName("g.example.com/g"); g.NeedsDialect != 3 {
		t.Fatalf("needs %d, want the header's own 3", g.NeedsDialect)
	}
	introducing(t, "shippedRecordDataKeys", "properties", Dialect+1)
	_, _, err = LoadShipped(tree)
	if err == nil || !strings.Contains(err.Error(), "its shipped record g.example.com/g/thing/heavy uses `properties`, which dialect 4 introduced") {
		t.Fatalf("got %v, want the record's key named as dialect 4's", err)
	}
}

// built is the packages a stream builds, before the registry resolves
// anything across them: where a package's stated dialect is held.
func built(t *testing.T, body string) error {
	t.Helper()
	docs, err := ParseStream([]byte(body))
	if err != nil {
		return err
	}
	_, err = BuildPackages(docs, SourceBuiltin)
	return err
}

// A document's envelope and metadata keys are read before the loader
// (documentFrom), and count toward the package the document builds.
func TestADocumentsEnvelopeKeysCountTowardTheDialectNeeded(t *testing.T) {
	if err := built(t, neededHeader); err != nil {
		t.Fatalf("a package whose keys every dialect reads: %v", err)
	}
	introducing(t, "metadataKeys", "id", Dialect+1)
	err := built(t, neededHeader)
	if err == nil || !strings.Contains(err.Error(), "the package states dialect 3, and its declarations use `id`, which dialect 4 introduced") {
		t.Fatalf("got %v, want metadata's id named as dialect 4's", err)
	}
}

// An agent's `params:` is a registered set (agentParamKeys), read by
// ParseAgentParams, and counts toward the need through the loader.
func TestAnAgentsParamsCountTowardTheDialectNeeded(t *testing.T) {
	body := neededHeader + `---
kind: substrate.reamde.dev/core/agent
metadata:
  id: g.example.com/g/classifier
data:
  authority: g.example.com
  package: g
  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  params: {temperature: 0.5}
`
	if err := built(t, body); err != nil {
		t.Fatalf("an agent with a param: %v", err)
	}
	introducing(t, "agentParamKeys", "temperature", Dialect+1)
	err := built(t, body)
	if err == nil || !strings.Contains(err.Error(), "its declarations use `temperature`, which dialect 4 introduced") {
		t.Fatalf("got %v, want the param named as dialect 4's", err)
	}
}
