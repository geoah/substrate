package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// errorCodesGoldenPath is in the console's tree for the reason the wire
// golden is: a vitest beside it imports the file and holds the console's
// union of server codes to it.
const errorCodesGoldenPath = "../../web/console/src/lib/api/errorcodes.golden.json"

const apiDocPath = "../../docs/api.md"

// A code constant a handler can write and the list does not carry is a code
// no client was told about: the console's union and the docs are held to the
// list, never to the constants.
func TestErrorCodesListEveryConstant(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatalf("parse errors.go: %v", err)
	}
	declared := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "code") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("%s is not a string literal: the closed set is read off the source", name.Name)
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", name.Name, err)
				}
				declared[name.Name] = value
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("errors.go declares no code constant: this test reads nothing")
	}

	for name, value := range declared {
		if !slices.Contains(errorCodes, value) {
			t.Errorf("%s = %q is missing from errorCodes (errors.go): add it, then regenerate the golden with "+
				"`go test ./internal/api/ -run TestErrorCodes -update` and list it in docs/api.md", name, value)
		}
	}
	seen := map[string]bool{}
	for _, code := range errorCodes {
		if seen[code] {
			t.Errorf("errorCodes lists %q twice", code)
		}
		seen[code] = true
	}
	if len(seen) != len(declared) {
		t.Errorf("errorCodes has %d codes and errors.go declares %d constants: the list is the constants and nothing else", len(seen), len(declared))
	}
}

// The golden is the closed set as the console reads it, sorted. Written by
// `-update`, never by hand.
func TestErrorCodesMatchTheGolden(t *testing.T) {
	codes := slices.Sorted(slices.Values(errorCodes))
	encoded, err := json.MarshalIndent(codes, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	if *updateGolden {
		if err := os.WriteFile(errorCodesGoldenPath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", errorCodesGoldenPath)
		return
	}

	want, err := os.ReadFile(errorCodesGoldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with `go test ./internal/api/ -run TestErrorCodes -update`): %v", err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the error codes have moved and %s is stale.\n\n"+
			"Regenerate it:\n"+
			"  go test ./internal/api/ -run TestErrorCodes -update\n\n"+
			"then make ServerErrorCode in web/console/src/lib/api/types.ts match — `pnpm test` in\n"+
			"web/console fails until it does.\n\ngot:\n%s\nwant:\n%s",
			errorCodesGoldenPath, encoded, want)
	}
}

var backticked = regexp.MustCompile("`([^`]+)`")

// docs/api.md states the closed set on one line, which may wrap: the
// paragraph that starts `The closed set:`. It is the set, in the golden's
// order, and nothing else.
func TestErrorCodesDocsListTheClosedSet(t *testing.T) {
	raw, err := os.ReadFile(apiDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", apiDocPath, err)
	}
	const opening = "The closed set:"
	var paragraph []string
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		switch {
		case paragraph == nil && strings.HasPrefix(line, opening):
			paragraph = []string{line}
		case paragraph != nil && line != "":
			paragraph = append(paragraph, line)
		case paragraph != nil:
			got := []string{}
			for _, m := range backticked.FindAllStringSubmatch(strings.Join(paragraph, " "), -1) {
				got = append(got, m[1])
			}
			if want := slices.Sorted(slices.Values(errorCodes)); !slices.Equal(got, want) {
				t.Errorf("%s lists the closed set as\n  %v\nand the code's is\n  %v\n"+
					"Make the paragraph that starts %q list the code's, in that order, each in backticks.",
					apiDocPath, got, want, opening)
			}
			return
		}
	}
	t.Fatalf("%s has no paragraph that starts %q, followed by a blank line: the Errors section lists the closed set there", apiDocPath, opening)
}
