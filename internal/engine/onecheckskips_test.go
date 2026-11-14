package engine

// EVERY PLACE A WRITE SKIPS THE ONE CHECK, LISTED (decision 0276). The one
// check (roles.go authorizeWrite) holds every hand's write to its rules and
// to the persons and apps it works for. Two things skip it, and nothing
// else: a named exemption (run.go exemptOp), the record of a run itself,
// and the engine's own hand (txn.internal), its bookkeeping. The guard keys
// on what no wrapper can hide: every reference to an exemption, an `exempt`
// set or a txn literal naming one, and `internal` given anything but the
// literal false, whether set, named in a literal or passed to a parameter
// named internal, whichever function declares it. Each function of this package that does any of these is listed here
// with what it writes, or, where it takes the engine's hand by naming the
// system's (systemhands_test.go), in that inventory with what it decides; a
// new one fails the test until it is listed, and one that goes fails it
// until its line does.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var oneCheckSkips = map[string]string{
	"actors.go:disableActor":                        "internal: an app's actor record disabled when its last token goes",
	"actors.go:ensureActor":                         "internal: the actor record of a hand that writes, the system's bookkeeping",
	"agentcompaction.go:extendLease":                "exempt (conversation): a thread's lease, extended while its compaction runs",
	"agentdecision.go:putThreadSystemRowID":         "exempt (conversation): a decision's resolution row in its thread",
	"agentjobs.go:endJob":                           "exempt (conversation): a job's tool row ended, and its report",
	"agentjobs.go:stopIdleThread":                   "internal: a stopped idle thread's settlement and its jobs', after mayContinue admitted the hand that stops it",
	"agentloop.go:claimThread":                      "exempt (conversation): a turn's claim of its thread",
	"agentloop.go:openThread":                       "exempt (conversation): the thread a run opens",
	"agentloop.go:putRow":                           "exempt (conversation): a message the loop posts, the user's among them",
	"agentloop.go:settle":                           "exempt (conversation): a thread settled at its turn's end",
	"agentpinned.go:continuePinned":                 "exempt (conversation): a thread's pinned block taken again",
	"agentpinned.go:dropPinned":                     "exempt (conversation): a thread's pinned block dropped",
	"agenttitle.go:writeTitle":                      "exempt (conversation): a thread's title",
	"alerts.go:writeAlert":                          "internal: an alert the engine raises",
	"appowned.go:TakeOverPackage":                   "internal: a package an app keeps taken into the person's hands, an admin's route (decision 0294)",
	"approvals.go:dispatchApproval":                 "exempt (conversation): the approval a call lands",
	"approvals.go:loadApproval":                     "internal: a read of an approval inside a transaction, which writes nothing",
	"approvals.go:reportApproval":                   "exempt (conversation): an approval's outcome and its report",
	"approvals.go:runApproval":                      "exempt (conversation): an approval's claim",
	"blobs.go:putBlobBytes":                         "internal: a blob's manifest record, which the engine writes for the upload its door admitted",
	"conversion.go:convertTableTo":                  "internal: a history conversion's rewrite",
	"dataset.go:inRawTx":                            "internal: the engine's raw transaction, for its own bookkeeping",
	"dataset.go:inTx":                               "internal as its caller says: the flag passed on to inTxOn, each caller that passes anything but false listed",
	"dataset.go:inTxOn":                             "internal as its caller says: the transaction takes the flag its caller passed",
	"functions.go:putSystemRun":                     "exempt (a trigger run's own record) and internal: a delivery's run record, which the managed properties and a system kind need the engine's hand for",
	"inputs.go:writeBindings":                       "internal: a bundle's input bindings",
	"interactions.go:dispatchAsk":                   "exempt (ask): the ask's question",
	"mapping.go:writeSubject":                       "internal: a mapping's subject record, which writes no guarded property (checkPropertyOwnership)",
	"merge.go:putIn":                                "internal: a merge's or a split's rows",
	"move.go:putMovedRow":                           "internal: a kind move's rewrite, the shipped upgrade's",
	"people.go:namePersonActor":                     "internal: a person's actor record, which only the engine writes",
	"people.go:setPersonRoles":                      "internal: the roles on a person's actor, held to the last-admin rule",
	"rebuild.go:RebuildRepository":                  "internal: the changelog replayed",
	"recordseed.go:ensureShippedRecords":            "internal: the shipped data records the seed fills",
	"repodir.go:insertEntries":                      "internal: an import's replay",
	"repodir.go:refold":                             "internal: the table's history replayed into the fold",
	"roles.go:ensureShippedRoles":                   "internal: the shipped role records",
	"run.go:runRecordTx":                            "exempt: a run's conversation or ask, named by its caller (exemptOp)",
	"unitseed.go:ensureShippedUnits":                "internal: the shipped units and conversions the seed fills",
	"vocabularywrite.go:applyVocabularyBatchLocked": "internal: a vocabulary apply's declarations, which mayWrite or behalfMayWrite hold to the one check before it opens",
	"vocabularywrite.go:clearShippedStamp":          "internal: a package row's shipped stamp cleared",
	"write.go:deleteWith":                           "internal as its caller says: a delete passes its caller's flag on",
	"write.go:patchInternal":                        "internal: the engine's own patch, for the callers listed here or in systemHands",
	"write.go:patchWith":                            "internal: the note a refused accept leaves on its change request; and, as its caller says, the flag passed on",
	"write.go:putInternal":                          "internal: the engine's own put, for the callers listed here or in systemHands",
	"write.go:putWith":                              "internal as its caller says: a put passes its caller's flag on",
	"write.go:recordTx":                             "internal: the transaction putInternal and patchInternal open",
}

func TestEveryPlaceAWriteSkipsTheOneCheckIsListed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]bool{}
	var parsed []*ast.File
	var names []string
	// Every function's parameter named internal, by position, and how many
	// parameters it takes, so a method of another type with the same name
	// and another arity is not taken for it: a call that passes it anything
	// but false may take the engine's hand, whatever wraps it.
	type internalParam struct{ at, params int }
	internalAt := map[string][]internalParam{}
	// The exemptions (run.go exemptOp): naming one is the only way to take
	// it, however a caller passes it on.
	exemptions := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		parsed, names = append(parsed, file), append(names, f)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				i, at := 0, -1
				for _, field := range d.Type.Params.List {
					for _, n := range field.Names {
						if n.Name == "internal" {
							at = i
						}
						i++
					}
					if len(field.Names) == 0 {
						i++
					}
				}
				if at >= 0 {
					internalAt[d.Name.Name] = append(internalAt[d.Name.Name], internalParam{at: at, params: i})
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					if id, ok := vs.Type.(*ast.Ident); ok && id.Name == "exemptOp" {
						for _, n := range vs.Names {
							exemptions[n.Name] = true
						}
					}
				}
			}
		}
	}
	if len(exemptions) == 0 || len(internalAt) == 0 {
		t.Fatalf("read no exemptions (%v) or no parameter named internal (%v): the guard reads nothing", exemptions, internalAt)
	}
	for k, file := range parsed {
		f := names[k]
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			where := f + ":" + fn.Name.Name
			ast.Inspect(fn, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					for i, lhs := range x.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok {
							continue
						}
						switch sel.Sel.Name {
						case "exempt":
							found[where] = true
						case "internal":
							if i < len(x.Rhs) && !literalFalse(x.Rhs[i]) {
								found[where] = true
							}
						}
					}
				case *ast.CallExpr:
					// The engine's hand by argument: a transaction opened with
					// it, a write given it, and the two writes that take it.
					name := ""
					switch f := x.Fun.(type) {
					case *ast.SelectorExpr:
						name = f.Sel.Name
					case *ast.Ident:
						name = f.Name
					}
					if name == "putInternal" || name == "patchInternal" {
						found[where] = true
					}
					for _, p := range internalAt[name] {
						if len(x.Args) == p.params && !literalFalse(x.Args[p.at]) {
							found[where] = true
						}
					}
				case *ast.Ident:
					if exemptions[x.Name] {
						found[where] = true
					}
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); !ok || id.Name != "txn" {
						return true
					}
					for _, el := range x.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						k, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if k.Name == "exempt" {
							found[where] = true
						}
						if k.Name == "internal" && !literalFalse(kv.Value) {
							found[where] = true
						}
					}
				}
				return true
			})
		}
	}
	for _, site := range slices.Sorted(func(yield func(string) bool) {
		for k := range found {
			if !yield(k) {
				return
			}
		}
	}) {
		_, listed := oneCheckSkips[site]
		if _, system := systemHands[site]; !listed && !system {
			t.Errorf("%s skips the one check (an exemption or the engine's own hand) and is not in oneCheckSkips: list it with what it writes, or hold the write to the check", site)
		}
	}
	for site := range oneCheckSkips {
		if !found[site] {
			t.Errorf("oneCheckSkips lists %s, which no longer skips the one check: take its line out", site)
		}
	}
}

// literalFalse reports whether e is the literal false: anything else in the
// place of `internal` (true, a variable, an expression) may take the
// engine's hand, and is a site to list.
func literalFalse(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "false"
}
