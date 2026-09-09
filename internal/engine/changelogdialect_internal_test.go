package engine

// The changelog dialect names a VOCABULARY: the ops an entry may carry and the
// fold effects its payload may spell (changelogdialect.go). Nothing in the
// compiler ties the constant to that vocabulary, so this test does. It reads
// the two files the wire values are declared in and holds them to the lists
// below, so ADDING an op or an effect kind fails here until somebody decides
// whether an older binary's fold would refuse or misread it, and bumps
// maxChangelogDialect in the same commit if it would.
//
// The lists are wire values, not identifiers: renaming the Go constant is free,
// changing what lands in a payload is not.

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

// dialectSixOps and dialectSixEffects are the changelog vocabulary since
// dialect 2, where references absorbed edges (decision 0044): `link` and
// `unlink` stopped being ops and `edge`/`unedge`/`edge1` stopped being
// effects. A store holding any of the five is stamped below the floor and
// refused at the open (changelogdialect.go minChangelogDialect); one whose
// stamp says otherwise is refused at the fold by name (fold.go foldRefuses).
// Dialect 3 keeps this vocabulary unchanged: its rung is the
// `txn` frame on every entry and the checksum over it (decision 0057), not a
// spelling. Dialect 4 keeps it unchanged too: its rung is the `kindVersion`
// key on the record delta (decision 0060, TestTheKindVersionStampIsDialectFour),
// and dialect 5 its rung is the `updatedAt` key on the manager effect
// (decision 0063, TestTheManagerStampIsDialectFive),
// so the lists hold.
// Dialect 6 (maxChangelogDialect) adds the delivery ledger: the `delivery` op
// and the seven effects a trigger's bookkeeping folds through (decision 0064).
var (
	dialectSixOps = []string{
		"put", "patch", "delete", "merge", "split", "gc", "delivery",
	}
	dialectSixEffects = []string{
		"record", "tombstone", "purge", "bump",
		"annotation", "manager", "former", "resync",
		"cursor", "schedule", "park", "unpark", "page", "unpage", "forget",
	}
)

func TestChangelogDialectCoversTheChangelogVocabulary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what     string
		file     string
		typeName string
		want     []string
	}{
		{"changelog ops", "../substrate/change.go", "Op", dialectSixOps},
		{"fold effects", "fold.go", "foldKind", dialectSixEffects},
	} {
		got := declaredStrings(t, c.file, c.typeName)
		if len(got) != len(c.want) {
			t.Fatalf("%s: %v, dialect %d covers %v. Decide whether an older binary's fold could refuse or misread the difference, then update this list (and maxChangelogDialect if it could)",
				c.what, got, maxChangelogDialect, c.want)
		}
		have := map[string]bool{}
		for _, v := range got {
			have[v] = true
		}
		for _, v := range c.want {
			if !have[v] {
				t.Fatalf("%s: %q is gone from the source but still listed at dialect %d: a spelling history may hold cannot simply be dropped",
					c.what, v, maxChangelogDialect)
			}
		}
	}
}

// Dialect 4 is the `record` delta carrying `kindVersion` (decision 0060). A
// dialect 3 binary does not refuse the key: foldOpsOf decodes without
// DisallowUnknownFields, so it replays the entry, drops the stamp and folds
// the row to 0 with nothing saying so. The rung is what makes that binary
// refuse at the gate, so the constant and the key are pinned together: a
// binary that writes the key stamps 4, and one that stops writing it may not
// keep the number.
func TestTheKindVersionStampIsDialectFour(t *testing.T) {
	t.Parallel()
	if maxChangelogDialect < 4 {
		t.Fatalf("maxChangelogDialect = %d; the kindVersion stamp is rung 4", maxChangelogDialect)
	}
	if err := admitChangelogDialect("geoah", 4, 3); !errors.Is(err, ErrChangelogDialectNewer) {
		t.Fatalf("a dialect 3 binary admitted a repository stamped 4: %v", err)
	}
	raw, err := json.Marshal(rowDelta{KindVersion: 4})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"kindVersion":4}` {
		t.Fatalf("the record delta spells the stamp as %s; dialect 4 is the key `kindVersion`", raw)
	}
}

// Dialect 5 is the `manager` effect carrying `updatedAt` (decision 0063): a
// property rename moves a manager row with its original stamp, and a dialect
// 4 binary, which decodes the key as absent and stamps the replay's own time,
// must refuse a store stamped 5 rather than fold it differently from the
// author. A store stamped 4 opens under this binary.
func TestTheManagerStampIsDialectFive(t *testing.T) {
	t.Parallel()
	if maxChangelogDialect < 5 {
		t.Fatalf("maxChangelogDialect = %d; the manager stamp is rung 5", maxChangelogDialect)
	}
	if err := admitChangelogDialect("geoah", maxChangelogDialect, 4); !errors.Is(err, ErrChangelogDialectNewer) {
		t.Fatalf("a dialect 4 binary admitted a repository stamped 5: %v", err)
	}
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(foldOp{Kind: foldManager, Ref: "k", ID: "r", Property: "p", Actor: "api", Tier: "owner", UpdatedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"updatedAt":"2026-09-08T12:00:00Z"`) {
		t.Fatalf("the manager effect spells its stamp as %s; dialect 5 is the key `updatedAt`", raw)
	}
	// Without a stamp the key is absent, so every manager effect written
	// before the rung still decodes to "the transaction's clock".
	raw, err = json.Marshal(foldOp{Kind: foldManager, Ref: "k", ID: "r", Property: "p", Actor: "api", Tier: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "updatedAt") {
		t.Fatalf("a manager effect without a stamp spells one: %s", raw)
	}
}

// declaredStrings returns the string values of every constant in file declared
// with the named type.
func declaredStrings(t *testing.T, file, typeName string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []string
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != typeName {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out = append(out, lit.Value[1:len(lit.Value)-1])
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no %s constants — did the type move?", file, typeName)
	}
	return out
}

// A repository this binary stamps (6) is refused by a binary whose maximum is
// 5, 4, 3 or 2: the delivery ledger's op and effects are unknown to their
// fold, and v0.46.0 and v0.47.0 (maximum 2) would also re-stamp every
// `changelog.hash` without `txn` at boot.
func TestChangelogDialectSixIsRefusedByAnOlderBinary(t *testing.T) {
	t.Parallel()
	if maxChangelogDialect != 6 {
		t.Fatalf("maxChangelogDialect = %d; the delivery ledger is rung 6", maxChangelogDialect)
	}
	for _, older := range []int{2, 3, 4, 5} {
		err := admitChangelogDialect("geoah", maxChangelogDialect, older)
		if !errors.Is(err, ErrChangelogDialectNewer) {
			t.Fatalf("a dialect %d binary admitted a repository stamped 6: %v", older, err)
		}
		if !strings.Contains(err.Error(), "dialect 6, this binary replays <= ") {
			t.Fatalf("the refusal must name both numbers: %v", err)
		}
	}
	if err := admitChangelogDialect("geoah", maxChangelogDialect, maxChangelogDialect); err != nil {
		t.Fatalf("a repository stamped at the maximum must open: %v", err)
	}
}

// The floor is the other half of the gate: every step that brought a store
// stamped below minChangelogDialect forward was retired together, so such a
// store is refused rather than opened into a fold this binary cannot
// complete. The refusal names the release that still adopted it, because
// booting that one once is the whole remedy. An unstamped 0 passes here: it
// is a store no binary has claimed, which the open-time gate tells apart from
// a pre-stamp history by probing the changelog.
func TestAStoreBelowTheFloorIsRefused(t *testing.T) {
	t.Parallel()
	if minChangelogDialect > maxChangelogDialect {
		t.Fatalf("the floor %d is above the maximum %d", minChangelogDialect, maxChangelogDialect)
	}
	for stored := 1; stored < minChangelogDialect; stored++ {
		err := admitChangelogDialect("geoah", stored, maxChangelogDialect)
		if !errors.Is(err, ErrChangelogDialectRetired) {
			t.Fatalf("a store stamped %d was admitted: %v", stored, err)
		}
		if !strings.Contains(err.Error(), lastAdoptingRelease) {
			t.Fatalf("the refusal must name the release that still adopted the store: %v", err)
		}
	}
	if err := admitChangelogDialect("geoah", 0, maxChangelogDialect); err != nil {
		t.Fatalf("an unstamped repository is judged by the open-time probe, not here: %v", err)
	}
}
