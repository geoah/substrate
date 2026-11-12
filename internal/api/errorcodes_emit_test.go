package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// codeConstants is every `code*` constant errors.go declares, by name.
func codeConstants(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatalf("parse errors.go: %v", err)
	}
	declared := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if strings.HasPrefix(name.Name, "code") {
					declared[name.Name] = true
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("errors.go declares no code constant: this test reads nothing")
	}
	return declared
}

// codeTakers are the emits that are handed their code as an argument, each
// with the argument's place: writeError, which checks it as it runs, and the
// MCP door's own answer, whose every caller names a constant.
var codeTakers = map[string]int{"writeError": 2, "failed": 1}

// emissions is what one source file writes into error envelopes.
type emissions struct {
	// problems are the places a code or a reason is written that is not a
	// declared constant.
	problems []string
	// emitted counts, per code constant, the places it is written: the Code
	// of an ErrorPayload literal, the code handed to a code taker, and a
	// return statement (a mapping hands its code to the emit that called it).
	emitted map[string]int
	// payloads and calls are how many ErrorPayload literals and code-taker
	// calls the file holds, so a scan that found nothing is told apart from
	// a file with nothing to find.
	payloads, calls int
}

// scanEmissions reads one parsed file of the package for the places an
// error code or a reason is written.
func scanEmissions(fset *token.FileSet, file *ast.File, declared map[string]bool) emissions {
	out := emissions{emitted: map[string]int{}}
	problem := func(pos token.Pos, msg string) {
		out.problems = append(out.problems, fset.Position(pos).String()+": "+msg)
	}
	emit := func(expr ast.Expr) bool {
		id, ok := expr.(*ast.Ident)
		if ok && declared[id.Name] {
			out.emitted[id.Name]++
		}
		return ok && declared[id.Name]
	}
	// Inside a code taker, the payload's Code is the argument it was handed.
	handed := map[ast.Node]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		if _, taker := codeTakers[fn.Name.Name]; !taker {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if kv, ok := n.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Code" {
					if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "code" {
						handed[kv.Value] = true
					}
				}
			}
			return true
		})
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ErrorPayload" {
				return true
			}
			out.payloads++
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					problem(node.Pos(), "an ErrorPayload literal without field names: name its Code")
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "Code":
					if !handed[kv.Value] && !emit(kv.Value) {
						problem(kv.Value.Pos(), "an ErrorPayload's Code is not one of the code constants in errors.go")
					}
				case "Reason":
					if _, literal := kv.Value.(*ast.BasicLit); literal {
						problem(kv.Value.Pos(), "an ErrorPayload's Reason is a string literal: name a Reason constant of internal/substrate")
					}
				}
			}
		case *ast.CallExpr:
			fn, ok := node.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			at, taker := codeTakers[fn.Name]
			if !taker || len(node.Args) <= at {
				return true
			}
			out.calls++
			arg := node.Args[at]
			_, named := arg.(*ast.Ident)
			switch {
			case emit(arg):
			case fn.Name == "writeError" && named:
				// A variable: writeError checks it as it runs.
			default:
				problem(arg.Pos(), fn.Name+" is handed a code that is not a code constant from errors.go")
			}
		case *ast.AssignStmt:
			// A payload built first and given its code or reason afterwards.
			for i, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Code" && sel.Sel.Name != "Reason") || i >= len(node.Rhs) {
					continue
				}
				if _, literal := node.Rhs[i].(*ast.BasicLit); literal {
					problem(node.Rhs[i].Pos(), "a "+sel.Sel.Name+" is assigned a string literal: name a constant")
				} else if sel.Sel.Name == "Code" {
					emit(node.Rhs[i])
				}
			}
		case *ast.ReturnStmt:
			for _, result := range node.Results {
				emit(result)
			}
		}
		return true
	})
	return out
}

// The closed set is about what a handler WRITES, and the list in errors.go
// only says what is declared. So every place a code is written is held to
// the constants: the `Code` of an ErrorPayload literal names one, an emit
// that takes its code as an argument is never handed a string literal (a
// code that reaches writeError through a variable is checked there, as it
// runs; nothing else is handed one), and no `Code` or `Reason` is assigned a
// literal afterwards. And a constant nothing writes is a member of the set
// no client can ever meet: every one is written somewhere. What this cannot
// see is an envelope spelled as raw JSON or a map, which review holds.
func TestEveryEmittedCodeIsADeclaredConstant(t *testing.T) {
	declared := codeConstants(t)
	emitted := map[string]int{}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	fset := token.NewFileSet()
	payloads, calls := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		found := scanEmissions(fset, file, declared)
		for _, p := range found.problems {
			t.Errorf("%s: a code or a reason a handler writes is a member of its closed set", p)
		}
		for code, n := range found.emitted {
			emitted[code] += n
		}
		payloads += found.payloads
		calls += found.calls
	}
	if payloads == 0 || calls == 0 {
		t.Fatalf("found %d ErrorPayload literals and %d writeError calls: this test reads nothing", payloads, calls)
	}
	for name := range declared {
		if emitted[name] == 0 {
			t.Errorf("%s is declared and listed and no handler writes it: a code nothing answers with is not in the closed set", name)
		}
	}
}

// The scan itself, on source it must refuse and source it must not count.
func TestScanEmissionsRefusesWhatIsNotAConstant(t *testing.T) {
	declared := map[string]bool{"codeKnown": true, "codeOther": true}
	for name, tc := range map[string]struct {
		body string
		want string
		// emitted is what the body writes, when it is not refused.
		emitted map[string]int
	}{
		"a constant in a literal":          {body: `_ = substrate.ErrorPayload{Code: codeKnown}`, emitted: map[string]int{"codeKnown": 1}},
		"a constant handed to writeError":  {body: `writeError(w, 400, codeKnown, "m")`, emitted: map[string]int{"codeKnown": 1}},
		"a variable handed to writeError":  {body: `writeError(w, 400, code, "m")`},
		"a constant returned by a mapping": {body: `return codeOther`, emitted: map[string]int{"codeOther": 1}},
		"a constant only compared":         {body: `if x == codeKnown { return }`},
		"a constant only logged":           {body: `log(codeKnown)`},

		"a literal handed to writeError":   {body: `writeError(w, 400, "teapot", "m")`, want: "not a code constant"},
		"a variable handed to failed":      {body: `failed(400, code, "m")`, want: "not a code constant"},
		"a literal Code":                   {body: `_ = substrate.ErrorPayload{Code: "teapot"}`, want: "not one of the code constants"},
		"an unknown identifier as Code":    {body: `_ = substrate.ErrorPayload{Code: codeMadeUp}`, want: "not one of the code constants"},
		"a literal Reason":                 {body: `_ = substrate.ErrorPayload{Code: codeKnown, Reason: "made_up"}`, want: "Reason is a string literal"},
		"a Code assigned afterwards":       {body: `p.Code = "teapot"`, want: "Code is assigned a string literal"},
		"a Reason assigned afterwards":     {body: `p.Reason = "made_up"`, want: "Reason is assigned a string literal"},
		"a payload without field names":    {body: `_ = substrate.ErrorPayload{codeKnown, "", "m", nil, nil, nil, ""}`, want: "without field names"},
		"a constant assigned to a payload": {body: `p.Code = codeKnown`, emitted: map[string]int{"codeKnown": 1}},
	} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "fixture.go", "package api\nfunc fixture() {\n"+tc.body+"\n}\n", 0)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		got := scanEmissions(fset, file, declared)
		problems := strings.Join(got.problems, "\n")
		if (tc.want == "") != (problems == "") || !strings.Contains(problems, tc.want) {
			t.Errorf("%s: problems = %q, want %q", name, problems, tc.want)
		}
		if tc.want != "" {
			continue
		}
		for code := range declared {
			if got.emitted[code] != tc.emitted[code] {
				t.Errorf("%s: %s counted %d times as written, want %d", name, code, got.emitted[code], tc.emitted[code])
			}
		}
	}
}

// writeError is the one emit that takes its code as a value, so it refuses
// one outside the set as it runs: the client is answered `internal`.
func TestWriteErrorAnswersInternalForACodeOutsideTheSet(t *testing.T) {
	for code, want := range map[string]struct {
		status int
		code   string
	}{
		codeConflict: {http.StatusConflict, codeConflict},
		"teapot":     {http.StatusInternalServerError, codeInternal},
		"":           {http.StatusInternalServerError, codeInternal},
	} {
		rec := httptest.NewRecorder()
		writeError(rec, http.StatusConflict, code, "the message")
		var got substrate.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("code %q: decode %q: %v", code, rec.Body.String(), err)
		}
		if rec.Code != want.status || got.Error.Code != want.code {
			t.Errorf("code %q: answered %d %q, want %d %q", code, rec.Code, got.Error.Code, want.status, want.code)
		}
		if want.code == codeInternal && strings.Contains(rec.Body.String(), "the message") {
			t.Errorf("code %q: the refusal's message was sent under `internal`: %s", code, rec.Body.String())
		}
	}
}
