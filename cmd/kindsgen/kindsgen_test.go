// The generator's own suite covers the dialect the SHIPPED declarations do not
// exercise yet. Core declares no keyed map, no nested object past level two, no
// pattern and no refersTo today, so the conformance test in internal/corekinds
// cannot reach those paths — and they are exactly the paths the typed core is
// about to walk into. A synthetic declaration reaches them, and the generated
// package is COMPILED to prove the emission is more than plausible text.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The synthetic declaration wears an existing kind's name because the Go
// spelling of a kind is a table, not a derivation: a made-up name would fail
// buildPlan for the right reason and teach this test nothing.
const widerDialect = `kind: core.substrate.reamde.dev/kind
metadata:
  id: core.substrate.reamde.dev/blob
data:
  authority: core.substrate.reamde.dev
  description: a synthetic declaration exercising the wider dialect
  version: v1alpha1
  names:
    singular: blob
    plural: blobs
  properties:
    digest:
      type: string
      pattern: "^[0-9a-f]{64}$"
      required: true
    emit:
      type: string
      repeated: true
      refersTo: kind
      description: the kinds this may write
    version:
      type: string
      managed: true
    labels:
      type: string
      keyed: true
      keyPattern: camel
    inputs:
      type: object
      keyed: true
      keyPattern: kindRef
      description: one named configuration need per key
      fields:
        kind:
          type: reference
          to: any
        window:
          type: object
          fields:
            unit:
              type: enum
              values:
                - value: day
                  label: Days
                - week
            size:
              type: int
              min: 1
              max: 90
    moment:
      type: datetime
`

func generate(t *testing.T, document string) (goFiles map[string]string, ts string) {
	t.Helper()
	root := t.TempDir()
	kindsDir := filepath.Join(root, "kinds", "core.substrate.reamde.dev")
	if err := os.MkdirAll(kindsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kindsDir, "blob.yaml"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	goDir := filepath.Join(root, "out")
	tsFile := filepath.Join(root, "out.ts")
	if err := run(kindsDir, goDir, tsFile, "all"); err != nil {
		t.Fatalf("generating: %v", err)
	}
	entries, err := os.ReadDir(goDir)
	if err != nil {
		t.Fatal(err)
	}
	goFiles = map[string]string{}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(goDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		goFiles[e.Name()] = string(raw)
	}
	raw, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatal(err)
	}
	return goFiles, string(raw)
}

func TestWiderDialectGenerates(t *testing.T) {
	goFiles, ts := generate(t, widerDialect)
	blob := goFiles["blob.go"]
	for _, want := range []string{
		// keyed scalar and keyed object, each a map whose keys are held to their
		// declared contract.
		"Labels map[string]string",
		"Inputs map[string]BlobInputs",
		`d.key(p, mk, "camel")`,
		`d.key(p, mk, "kindRef")`,
		// an object field inside a keyed object: depth 3, named by its path.
		"type BlobInputsWindow struct",
		"Window *BlobInputsWindow",
		// an enum declared at depth 4, with its labels.
		"type BlobInputsWindowUnit string",
		// gofmt aligns a const block, so the spelling of the gap is its business.
		"BlobInputsWindowUnitDay",
		`BlobInputsWindowUnit = "day"`,
		`"day": "Days"`,
		// a reference field resolved through the declaration's to:.
		`d.reference(p, props[key], "any")`,
		// the declared bounds and pattern travel into the decoder.
		"Bounds{Min: bound(1), Max: bound(90)}",
		`regexp.MustCompile("^[0-9a-f]{64}$")`,
		// a required scalar is a VALUE, not a pointer, and its absence is refused.
		"Digest string",
		`if props["digest"] == nil {`,
		// the markers reach the doc, which is the only place a client can read
		// them without the declaration.
		"Names a kind in the registry.",
		"Managed: the engine stamps it",
		// keys are the admitted set, sorted
		`var BlobKeys = []string{"digest", "emit", "inputs", "labels", "moment", "version"}`,
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("generated Go is missing %q", want)
		}
	}
	for _, want := range []string{
		"labels?: Record<string, string>",
		"inputs?: Record<string, BlobInputs>",
		"digest: string",
		`export type BlobInputsWindowUnit =`,
		`"day": "Days",`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("generated TypeScript is missing %q", want)
		}
	}
}

// TestGeneratedPackageCompiles is why the wider dialect is trustworthy: the
// emitted package is built, not eyeballed. It needs no module cache — the
// generated code imports the standard library and nothing else, which is the
// leaf rule paying for itself.
func TestGeneratedPackageCompiles(t *testing.T) {
	goFiles, _ := generate(t, widerDialect)
	dir := t.TempDir()
	mod := "module corekindsgen\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range goFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated package does not compile: %v\n%s", err, out)
	}
}

// TestRefusals is the rule that a dialect feature the reader does not
// understand FAILS the run. Generating past one would emit a type that silently
// means something else, which no review of a generated file catches.
func TestRefusals(t *testing.T) {
	cases := map[string]struct{ document, problem string }{
		"an unknown property key": {
			document: propertyDocument("digest:\n      type: string\n      coerce: true\n"),
			problem:  `unknown key "coerce"`,
		},
		"an unknown datatype": {
			document: propertyDocument("digest:\n      type: uuid\n"),
			problem:  `datatype "uuid"`,
		},
		"both containers at once": {
			document: propertyDocument("digest:\n      type: string\n      keyed: true\n      repeated: true\n"),
			problem:  "keyed and repeated are the two containers",
		},
		"a key contract without a keyed map": {
			document: propertyDocument("digest:\n      type: string\n      keyPattern: camel\n"),
			problem:  "keyPattern is the contract",
		},
		"refersTo on something that is not a string": {
			document: propertyDocument("size:\n      type: int\n      refersTo: kind\n"),
			problem:  "refersTo marks what a STRING value names",
		},
		"an enum without values": {
			document: propertyDocument("mode:\n      type: enum\n"),
			problem:  "an enum declares its values",
		},
		"a field nested past the bound": {
			document: propertyDocument(nestedTooDeep),
			problem:  "fields nest 4 levels deep at most",
		},
		"a secret as a field": {
			document: propertyDocument("wrap:\n      type: object\n      fields:\n        key:\n          type: secret\n"),
			problem:  "secret is its own property, never a field",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			kindsDir := filepath.Join(root, "core.substrate.reamde.dev")
			if err := os.MkdirAll(kindsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(kindsDir, "blob.yaml"), []byte(tc.document), 0o644); err != nil {
				t.Fatal(err)
			}
			err := run(kindsDir, filepath.Join(root, "out"), filepath.Join(root, "out.ts"), "all")
			if err == nil {
				t.Fatalf("generated past %s", name)
			}
			if !strings.Contains(err.Error(), tc.problem) {
				t.Errorf("the refusal does not name the problem:\n got %v\nwant %q", err, tc.problem)
			}
		})
	}
}

// TestUnknownKindRefuses holds the naming table's own rule: a kind with no Go
// spelling stops the run rather than inventing one.
func TestUnknownKindRefuses(t *testing.T) {
	root := t.TempDir()
	kindsDir := filepath.Join(root, "core.substrate.reamde.dev")
	if err := os.MkdirAll(kindsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	document := strings.NewReplacer("blob", "widget", "blobs", "widgets").Replace(propertyDocument("digest:\n      type: string\n"))
	if err := os.WriteFile(filepath.Join(kindsDir, "widget.yaml"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(kindsDir, filepath.Join(root, "out"), filepath.Join(root, "out.ts"), "all")
	if err == nil || !strings.Contains(err.Error(), "no Go spelling") {
		t.Fatalf("a kind with no Go spelling generated anyway: %v", err)
	}
}

const nestedTooDeep = `deep:
      type: object
      fields:
        two:
          type: object
          fields:
            three:
              type: object
              fields:
                four:
                  type: object
                  fields:
                    five:
                      type: string
`

func propertyDocument(properties string) string {
	return `kind: core.substrate.reamde.dev/kind
metadata:
  id: core.substrate.reamde.dev/blob
data:
  authority: core.substrate.reamde.dev
  names:
    singular: blob
    plural: blobs
  properties:
    ` + properties
}
