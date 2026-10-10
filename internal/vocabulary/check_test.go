package vocabulary_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// twoPackagesWithLongDescriptions is two packages, each with declarations
// whose descriptions run past their limits. The names are chosen so that
// sorting every problem together would interleave the packages: `agent` sorts
// before `kind` across both.
func twoPackagesWithLongDescriptions(t *testing.T) []vocabulary.Document {
	t.Helper()
	long := strings.Repeat("a", 229)
	stream := `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/alpha
data:
  authority: ada.example.com
  package: alpha
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/alpha/note
data:
  authority: ada.example.com
  package: alpha
  names:
    singular: note
  properties:
    name:
      type: string
      description: ` + long + `
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ada.example.com/alpha/reflector
data:
  authority: ada.example.com
  package: alpha
  description: ` + long + `
  prompt: Summarize the notes.
  provider: openai
  model: gpt-5-mini
---
kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/beta
data:
  authority: ada.example.com
  package: beta
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ada.example.com/beta/reflector
data:
  authority: ada.example.com
  package: beta
  description: ` + long + `
  prompt: Summarize the notes.
  provider: openai
  model: gpt-5-mini
`
	docs, err := vocabulary.ParseStream([]byte(stream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return docs
}

// Each package's problems come sorted, as BuildPackages sorts them, and the
// packages come in name order: the order the engine's 422 and `substratectl
// validate` share. One BuildPackages call over every document would sort the
// two packages' problems together instead.
func TestBuildEachPackageReportsPackageByPackage(t *testing.T) {
	docs := twoPackagesWithLongDescriptions(t)
	// Reversed, so the order out cannot be the order in.
	for i, j := 0, len(docs)-1; i < j; i, j = i+1, j-1 {
		docs[i], docs[j] = docs[j], docs[i]
	}
	_, err := vocabulary.BuildEachPackage(docs, func(string) string { return vocabulary.SourceInstalled })
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a ValidationError, got %v", err)
	}
	want := []string{
		"agent ada.example.com/alpha/reflector: data.description is required — the agent is its own tool card",
		"agent ada.example.com/alpha/reflector: data.description: one short sentence (at most 200 chars), got 229",
		"kind ada.example.com/alpha/note: data.properties.name.description: one short sentence (at most 200 chars), got 229",
		"agent ada.example.com/beta/reflector: data.description is required — the agent is its own tool card",
		"agent ada.example.com/beta/reflector: data.description: one short sentence (at most 200 chars), got 229",
	}
	if !reflect.DeepEqual(ve.Problems, want) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(ve.Problems, "\n"), strings.Join(want, "\n"))
	}
}

// The origin each package builds with is the one sourceOf names for it: the
// `runtime: host` rule refuses a host function anywhere but `builtin`.
func TestBuildEachPackageAsksTheSourcePerPackage(t *testing.T) {
	stream := `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/tools
data:
  authority: ada.example.com
  package: tools
---
kind: substrate.reamde.dev/core/function
metadata:
  id: ada.example.com/tools/now
data:
  authority: ada.example.com
  package: tools
  description: Read the clock.
  runtime: host
`
	docs, err := vocabulary.ParseStream([]byte(stream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var asked []string
	_, err = vocabulary.BuildEachPackage(docs, func(pkg string) string {
		asked = append(asked, pkg)
		return vocabulary.SourceInstalled
	})
	if !reflect.DeepEqual(asked, []string{"ada.example.com/tools"}) {
		t.Fatalf("sourceOf asked for %v", asked)
	}
	if err == nil || !strings.Contains(err.Error(), "data.runtime: host") {
		t.Fatalf("an installed package declaring a host function must be refused, got %v", err)
	}
	if _, err := vocabulary.BuildEachPackage(docs, func(string) string { return vocabulary.SourceBuiltin }); err != nil {
		t.Fatalf("a builtin package may declare a host function, got %v", err)
	}
}
