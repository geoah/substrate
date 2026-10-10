package commands

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reflectionHeader is the package document of the package issue 887 was
// filed against, under a repository's own authority.
const reflectionHeader = `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/reflection
data:
  authority: ada.example.com
  package: reflection
  version: 1
---
`

// reflectionAgent is one agent of that package with the description given.
func reflectionAgent(description string) string {
	return `kind: substrate.reamde.dev/core/agent
metadata:
  id: ada.example.com/reflection/reflector
data:
  authority: ada.example.com
  package: reflection
  description: ` + description + `
  prompt: Summarize the notes.
  provider: openai
  model: gpt-5-mini
`
}

// longDescription is 229 characters, the length that failed the apply in
// production against a limit of 200.
var longDescription = strings.Repeat("Reflect on the notes. ", 11)[:229]

// lengthProblem is the line the server's loader writes for it.
const lengthProblem = "agent ada.example.com/reflection/reflector: data.description: one short sentence (at most 200 chars), got 229"

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// problemsBlock is the `problems:` heading and its bullets as renderError
// prints err.
func problemsBlock(t *testing.T, err error) string {
	t.Helper()
	var buf bytes.Buffer
	renderError(&buf, err)
	out := buf.String()
	i := strings.Index(out, "  problems:\n")
	if i < 0 {
		t.Fatalf("no problems in:\n%s", out)
	}
	var b strings.Builder
	for j, line := range strings.SplitAfter(out[i:], "\n") {
		if j > 0 && !strings.HasPrefix(line, "    - ") {
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// The issue's case: the problems validate prints offline are byte for byte
// the ones `apply` prints from the server's 422 for the same file, and
// validate sends nothing.
func TestValidateMatchesTheApplyRefusal(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.vocabularyCheck = true
	path := writeManifest(t, reflectionHeader+reflectionAgent(longDescription))

	_, _, validateErr := h.run("validate", "-f", path)
	if validateErr == nil {
		t.Fatal("validate must refuse a description over its limit")
	}
	if len(h.fake.requests) != 0 {
		t.Fatalf("validate reached the server: %v", h.fake.requests)
	}
	_, _, applyErr := h.run("apply", "-f", path)
	if applyErr == nil {
		t.Fatal("apply must refuse a description over its limit")
	}
	if got := h.lastRequest(); got != "POST /api/v1/vocabulary/apply" {
		t.Fatalf("apply's refusal came from %q", got)
	}

	offline, server := problemsBlock(t, validateErr), problemsBlock(t, applyErr)
	if offline != server {
		t.Fatalf("validate printed:\n%s\napply printed:\n%s", offline, server)
	}
	if !strings.Contains(offline, "    - "+lengthProblem+"\n") {
		t.Fatalf("missing %q in:\n%s", lengthProblem, offline)
	}
}

// A file set without its package document is checked under a placeholder
// header, which validate names, and that alone exits non-zero.
func TestValidateNamesAMissingPackageHeader(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionAgent("Summarize the day's notes."))
	out, _, err := h.run("validate", "-f", path)
	if err == nil {
		t.Fatalf("a members-only file set must exit non-zero without --partial\n%s", out)
	}
	if !strings.Contains(err.Error(), "--partial") {
		t.Fatalf("the refusal should name --partial: %v", err)
	}
	if want := "  - package ada.example.com/reflection: no package document among the files, so its members were checked under a placeholder header\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	if _, _, err := h.run("validate", "--partial", "-f", path); err != nil {
		t.Fatalf("--partial accepts the partial check of a clean member: %v", err)
	}
}

// --partial still checks the members it can: the description over its
// limit is refused with the server's line.
func TestValidatePartialReportsTheLengthProblem(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionAgent(longDescription))
	_, _, err := h.run("validate", "--partial", "-f", path)
	var ae *apiError
	if !errors.As(err, &ae) {
		t.Fatalf("want the validation refusal, got %v", err)
	}
	if block := problemsBlock(t, err); !strings.Contains(block, "    - "+lengthProblem+"\n") {
		t.Fatalf("missing %q in:\n%s", lengthProblem, block)
	}
}

// A whole, clean package exits 0 and says what it checked.
func TestValidateCleanPackagePasses(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes."))
	out, _ := h.mustRun("validate", "-f", path)
	if want := "declarations: 2 documents checked in 1 package (ada.example.com/reflection)\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	if strings.Contains(out, "not checked offline") {
		t.Fatalf("a whole package skipped something:\n%s", out)
	}
}

// reflectionNote is a kind of the package whose one reference is pinned at
// the kind given.
func reflectionNote(pin string) string {
	return `---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/reflection/note
data:
  authority: ada.example.com
  package: reflection
  names:
    singular: note
  properties:
    about:
      type: reference
      kind: ` + pin + `
`
}

// A package that passes is resolved against the files and the shipped seed,
// so a pin at a kind nothing declares is refused offline, and a pin at a
// core kind is not.
func TestValidateResolvesAgainstTheFilesAndTheSeed(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes.")+reflectionNote("ada.example.com/reflection/nothing"))
	_, _, err := h.run("validate", "-f", path)
	if err == nil {
		t.Fatal("a pin at an undeclared kind of the package must be refused")
	}
	if want := `data.properties.about.kind: unknown referent kind "ada.example.com/reflection/nothing"`; !strings.Contains(problemsBlock(t, err), want) {
		t.Fatalf("missing %q in:\n%s", want, problemsBlock(t, err))
	}

	path = writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes.")+reflectionNote("substrate.reamde.dev/llm/thread"))
	if out, _, err := h.run("validate", "-f", path); err != nil {
		t.Fatalf("a pin at a shipped kind resolves offline: %v\n%s", err, out)
	}
}

// A pin into a package neither among the files nor shipped cannot be
// resolved here, so validate names the reference it skipped and exits
// non-zero until --partial accepts that.
func TestValidateSkipsResolutionIntoAPackageNotAmongTheFiles(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes.")+reflectionNote("bob.example.com/people/person"))
	out, _, err := h.run("validate", "-f", path)
	if err == nil {
		t.Fatalf("an unresolved package must exit non-zero without --partial\n%s", out)
	}
	if want := "  - package bob.example.com/people: neither among the files nor shipped, so these references were not resolved: bob.example.com/people/person\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	if _, _, err := h.run("validate", "--partial", "-f", path); err != nil {
		t.Fatalf("--partial accepts the unresolved package: %v", err)
	}
}

// Only a reference the resolver follows can depend on another package. A
// description that opens with an identity is prose, so the package it names
// is no dependency and the run is whole.
func TestValidateIgnoresAPackageNamedInProse(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("bob.example.com/people/person describes the subject."))
	out, _ := h.mustRun("validate", "-f", path)
	if strings.Contains(out, "not checked offline") {
		t.Fatalf("a description invented a dependency:\n%s", out)
	}
}

// A permission glob covers kinds the registry does not hold yet and the
// resolver never looks it up (record 0080), so the package it names is no
// dependency either.
func TestValidateIgnoresAPermissionGlob(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes.")+`  permissions:
    writes: [bob.example.com/people/*]
`)
	out, _ := h.mustRun("validate", "-f", path)
	if strings.Contains(out, "not checked offline") {
		t.Fatalf("a permission glob invented a dependency:\n%s", out)
	}
}

// A reference skipped for want of its package hides nothing else: under
// --partial an undeclared pin into the files' own package is still refused,
// beside a prose mention and beside a real pin into an absent package alike.
func TestValidatePartialStillRefusesALocalError(t *testing.T) {
	localPin := `data.properties.about.kind: unknown referent kind "ada.example.com/reflection/nothing"`
	for name, extra := range map[string]string{
		"prose mention": "",
		"absent package": `---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/reflection/subject
data:
  authority: ada.example.com
  package: reflection
  names:
    singular: subject
  properties:
    person:
      type: reference
      kind: bob.example.com/people/person
`,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			path := writeManifest(t, reflectionHeader+
				reflectionAgent("bob.example.com/people/person describes the subject.")+
				reflectionNote("ada.example.com/reflection/nothing")+extra)
			_, _, err := h.run("validate", "--partial", "-f", path)
			var ae *apiError
			if !errors.As(err, &ae) {
				t.Fatalf("--partial must still refuse the undeclared local pin, got %v", err)
			}
			block := problemsBlock(t, err)
			if !strings.Contains(block, localPin) {
				t.Fatalf("missing %q in:\n%s", localPin, block)
			}
			if strings.Contains(block, "bob.example.com") {
				t.Fatalf("a reference into the absent package was refused:\n%s", block)
			}
		})
	}
}

// The resolver trims a trait binding and drops its remap before looking it
// up, so a padded binding into an absent package is a skipped reference
// named by its identity, never an `unknown trait` refusal.
func TestValidateSkipsAPaddedTraitIntoAPackageNotAmongTheFiles(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes.")+`---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/reflection/note
data:
  authority: ada.example.com
  package: reflection
  names:
    singular: note
  traits:
    - " bob.example.com/people/titled(name)"
  properties:
    name:
      type: string
`)
	out, _, err := h.run("validate", "--partial", "-f", path)
	if err != nil {
		t.Fatalf("the padded trait binding was refused: %v\n%s", err, out)
	}
	if want := "  - package bob.example.com/people: neither among the files nor shipped, so these references were not resolved: bob.example.com/people/titled\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// A mapping endpoint written as a stored reference value is the kind inside
// it (vocabulary.ReferentID), so a mapping from an absent package's kind is
// a skipped reference to that kind, not a read of shipped core.
func TestValidateSkipsAMappingFromAPackageNotAmongTheFiles(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, reflectionHeader+reflectionAgent("Summarize the day's notes.")+`---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/reflection/note
data:
  authority: ada.example.com
  package: reflection
  names:
    singular: note
  properties:
    name:
      type: string
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: ada.example.com/reflection/personnote
data:
  authority: ada.example.com
  package: reflection
  from:
    ref: substrate.reamde.dev/core/kind/bob.example.com/people/person
  to: ada.example.com/reflection/note
  property: person
  match:
    - from: name
      to: name
`)
	out, _, err := h.run("validate", "--partial", "-f", path)
	if err != nil {
		t.Fatalf("the mapping from an absent package was refused: %v\n%s", err, out)
	}
	if want := "  - package bob.example.com/people: neither among the files nor shipped, so these references were not resolved: bob.example.com/people/person\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// Files declaring into a seeded package are refused by name, whatever
// --partial says: checking them would build core from the files' part of it
// and refuse every other package's pin into the rest.
func TestValidateRefusesAChangeToASeededPackage(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, `kind: substrate.reamde.dev/core/package
metadata:
  id: substrate.reamde.dev/core
data:
  authority: substrate.reamde.dev
  package: core
---
`+reflectionHeader+reflectionAgent("Summarize the day's notes.")+reflectionNote("substrate.reamde.dev/core/function"))
	_, _, err := h.run("validate", "--partial", "-f", path)
	if err == nil {
		t.Fatal("a change to a seeded package must be refused")
	}
	var buf bytes.Buffer
	renderError(&buf, err)
	got := buf.String()
	if want := "the files declare into substrate.reamde.dev/core, seeded with the substrate: validate cannot check a change to a seeded package offline"; !strings.Contains(got, want) {
		t.Fatalf("missing %q in:\n%s", want, got)
	}
	if strings.Contains(got, "unknown referent kind") {
		t.Fatalf("the shipped core was replaced:\n%s", got)
	}
}

// A declaration naming neither data.authority nor data.package is a change
// apply completes from the stored row, which validate does not read.
func TestValidateSkipsAPartialDeclaration(t *testing.T) {
	h := newHarness(t)
	path := writeManifest(t, `kind: substrate.reamde.dev/core/agent
metadata:
  id: ada.example.com/reflection/reflector
data:
  hiddenFromChat: true
`)
	out, _, err := h.run("validate", "-f", path)
	if err == nil {
		t.Fatalf("a partial declaration must exit non-zero without --partial\n%s", out)
	}
	if want := "  - agent ada.example.com/reflection/reflector: names neither data.authority nor data.package, so apply completes it from the stored declaration first\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	if want := "declarations: 0 documents checked\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// Every shipped sample and provider, checked together so each reference
// lands among the files, passes whole: the offline check refuses nothing the
// server admits from the tree.
func TestValidatePassesEveryShippedPackage(t *testing.T) {
	h := newHarness(t)
	var args []string
	for _, root := range []string{"../../../samples", "../../../kinds/providers.substrate.reamde.dev"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && filepath.Ext(path) == ".yaml" {
				args = append(args, "-f", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(args) == 0 {
		t.Fatal("no shipped manifests found")
	}
	out, _ := h.mustRun(append([]string{"validate"}, args...)...)
	if strings.Contains(out, "not checked offline") {
		t.Fatalf("the shipped tree skipped something:\n%s", out)
	}
}
