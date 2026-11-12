package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// errorReasonsGoldenPath is in the console's tree, beside the codes' golden
// and for the same reason: a vitest beside it holds the console's union of
// reasons to it.
const errorReasonsGoldenPath = "../../web/console/src/lib/api/errorreasons.golden.json"

const substrateErrorsPath = "../substrate/errors.go"

// A Reason constant the list does not carry is a reason the API would drop
// and no client was told about: the console's union and the docs are held to
// the list, never to the constants.
func TestErrorReasonsListEveryConstant(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), substrateErrorsPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", substrateErrorsPath, err)
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
				if !strings.HasPrefix(name.Name, "Reason") || i >= len(vs.Values) {
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
		t.Fatalf("%s declares no Reason constant: this test reads nothing", substrateErrorsPath)
	}
	for name, value := range declared {
		if !slices.Contains(substrate.ErrorReasons, value) {
			t.Errorf("%s = %q is missing from substrate.ErrorReasons: add it, then regenerate the golden with "+
				"`go test ./internal/api/ -run TestErrorReasons -update` and list it in docs/api.md", name, value)
		}
	}
	seen := map[string]bool{}
	for _, reason := range substrate.ErrorReasons {
		if seen[reason] {
			t.Errorf("substrate.ErrorReasons lists %q twice", reason)
		}
		seen[reason] = true
	}
	if len(seen) != len(declared) {
		t.Errorf("substrate.ErrorReasons has %d reasons and %s declares %d constants: the list is the constants and nothing else",
			len(seen), substrateErrorsPath, len(declared))
	}
}

// The golden is the closed set as the console reads it, sorted. Written by
// `-update`, never by hand.
func TestErrorReasonsMatchTheGolden(t *testing.T) {
	reasons := slices.Sorted(slices.Values(substrate.ErrorReasons))
	encoded, err := json.MarshalIndent(reasons, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	if *updateGolden {
		if err := os.WriteFile(errorReasonsGoldenPath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", errorReasonsGoldenPath)
		return
	}

	want, err := os.ReadFile(errorReasonsGoldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with `go test ./internal/api/ -run TestErrorReasons -update`): %v", err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the reasons have moved and %s is stale.\n\n"+
			"Regenerate it:\n"+
			"  go test ./internal/api/ -run TestErrorReasons -update\n\n"+
			"then make ErrorReason in web/console/src/lib/api/types.ts match — `pnpm test` in\n"+
			"web/console fails until it does.\n\ngot:\n%s\nwant:\n%s",
			errorReasonsGoldenPath, encoded, want)
	}
}

// docs/api.md states the closed set of reasons on one line, which may wrap:
// the paragraph that starts `The closed set of reasons:`.
func TestErrorReasonsDocsListTheClosedSet(t *testing.T) {
	raw, err := os.ReadFile(apiDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", apiDocPath, err)
	}
	const opening = "The closed set of reasons:"
	var paragraph []string
	for line := range strings.Lines(string(raw) + "\n\n") {
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
			if want := slices.Sorted(slices.Values(substrate.ErrorReasons)); !slices.Equal(got, want) {
				t.Errorf("%s lists the closed set of reasons as\n  %v\nand the code's is\n  %v\n"+
					"Make the paragraph that starts %q list the code's, in that order, each in backticks.",
					apiDocPath, got, want, opening)
			}
			return
		}
	}
	t.Fatalf("%s has no paragraph that starts %q: the Errors section lists the closed set of reasons there", apiDocPath, opening)
}

// A validation refusal's reason arrives as a value from the engine, so the
// mapping is where the set is held as it runs: a reason outside it is not
// sent, and the client acts on the code.
func TestAReasonOutsideTheSetIsNotSent(t *testing.T) {
	for reason, want := range map[string]string{
		substrate.ReasonStaleRevision: substrate.ReasonStaleRevision,
		"made_up":                     "",
		"":                            "",
	} {
		_, payload := problemFor(&substrate.ValidationError{Problems: []string{"revision: stale"}, Reason: reason})
		if payload.Code != codeValidation || payload.Reason != want {
			t.Errorf("reason %q: answered %q with reason %q, want %q with %q", reason, payload.Code, payload.Reason, codeValidation, want)
		}
	}
}
