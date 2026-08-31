package engine_test

// The one migration behind core version 10: `recordpatchpolicy.selector.ops`
// was a free string and is now an enum of the door's three verbs. A repository
// that already holds a policy is the thing that migration can strand, so this
// walks it both ways. A selector spelled in the door's own words (or leaving
// `ops` out, which means all three) upgrades and keeps matching; one spelled in
// another vocabulary is refused with the count, and the stored declarations
// stand.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// opsEnumBlock is the shipped `selector.ops` declaration, verbatim. Reverting
// it by REPLACEMENT rather than hand-writing a whole v9 document keeps the rest
// of the declaration exactly what shipped, so the migration under test is the
// one field and not a dozen properties a hand-written stand-in left out.
const opsEnumBlock = `        ops:
          type: enum
          repeated: true
          values:
            - put
            - patch
            - delete
`

// opsStringBlock is what stood there before core's version 10.
const opsStringBlock = `        ops:
          type: string
          repeated: true
`

// oldPolicyTree is the shipped tree with `selector.ops` back to a free string
// and core one version behind, which is the binary every stored policy was
// written under.
func oldPolicyTree(t *testing.T) string {
	t.Helper()
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "recordpatchpolicy.yaml"), func(doc string) string {
		if !strings.Contains(doc, opsEnumBlock) {
			t.Fatal("recordpatchpolicy no longer declares selector.ops as the enum this reverts")
		}
		// The kind pins a version of its own now; back then it took the
		// authority's, so the fixture puts it back on 9 with everything else
		// under that authority. Left at its shipped pin, the old tree and the
		// new one would declare the same version and no upgrade would run.
		return pinVersion(t, strings.Replace(doc, opsEnumBlock, opsStringBlock, 1), "9")
	})
	bumpGroupVersion(t, tree, coreAuthority, "9")
	return tree
}

// policySelectorOps reads a repository's live declaration for the field, so the
// assertion is about the ROWS the upgrade wrote and not the tree it read.
func policySelectorOps(t *testing.T, ds substrate.Dataset) []string {
	t.Helper()
	ty, err := ds.KindByRef(context.Background(), vocabulary.KindRecordPatchPolicy)
	if err != nil {
		t.Fatalf("the policy kind: %v", err)
	}
	at := ty.Definition
	for _, key := range []string{"properties", "selector", "fields", "ops"} {
		next, _ := at[key].(map[string]any)
		if next == nil {
			t.Fatalf("the declaration has no %q: %v", key, at)
		}
		at = next
	}
	values, _ := at["values"].([]any)
	out := make([]string, 0, len(values))
	for _, v := range values {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func TestPolicySelectorOpsUpgradeToTheEnum(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)

	old := openTree(t, dsn, oldPolicyTree(t))
	if _, err := old.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds, err := old.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	if ops := policySelectorOps(t, ds); len(ops) != 0 {
		t.Fatalf("the old declaration declares values %v", ops)
	}
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-puts",
		Properties: map[string]any{
			"selector": map[string]any{"ops": []any{"put", "patch"}},
			"action":   "gate",
		},
	})
	// AN OMITTED `ops` IS THE CANONICAL SPELLING: "empty means all three", and
	// `selector: {}` with action gate is the documented "gate every agent
	// write". A value count that read an absent field as a value outside the
	// new set would strand exactly the policies that are most correct.
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-everything",
		Properties: map[string]any{"selector": map[string]any{}, "action": "gate"},
	})
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-selectorless",
		Properties: map[string]any{"action": "gate"},
	})
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	// The shipped tree opens the same repository: the enum lands, and the
	// stored policy is untouched by it.
	next := openTree(t, dsn, shippedTree(t))
	ds, err = next.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	if ops := policySelectorOps(t, ds); strings.Join(ops, ",") != "put,patch,delete" {
		t.Fatalf("after the upgrade the field declares %v", ops)
	}
	// The rest of core's version rode the same projection: `trigger` pins
	// none of its own, so it carries the authority's.
	if v := kindVersion(t, ds, coreAuthority+"/trigger"); v != 15 {
		t.Fatalf("trigger is at version %d, want 15", v)
	}
	rec, err := ds.Get(ctx, vocabulary.KindRecordPatchPolicy, "gate-puts")
	if err != nil {
		t.Fatalf("the stored policy: %v", err)
	}
	sel, _ := rec.Properties["selector"].(map[string]any)
	if got, _ := sel["ops"].([]any); len(got) != 2 || got[0] != "put" || got[1] != "patch" {
		t.Fatalf("the upgrade rewrote the stored selector: %v", sel)
	}
}

// A selector holding a word from another vocabulary is what the guard is for:
// the upgrade is refused rather than landing a declaration the row violates,
// and refusing the upgrade never refuses the repository.
func TestPolicySelectorOpsUpgradeRefusedByAStrandedSelector(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)

	old := openTree(t, dsn, oldPolicyTree(t))
	if _, err := old.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds, err := old.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// The word the request a gate produces carries, which never matched a
	// write and which the enum cannot admit.
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-creates",
		Properties: map[string]any{
			"selector": map[string]any{"ops": []any{"create"}},
			"action":   "gate",
		},
	})
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	next := openTree(t, dsn, shippedTree(t))
	ds, err = next.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("a refused upgrade must still open the repository: %v", err)
	}
	if ops := policySelectorOps(t, ds); len(ops) != 0 {
		t.Fatalf("the stranded upgrade landed anyway: %v", ops)
	}
	if _, err := ds.Get(ctx, vocabulary.KindRecordPatchPolicy, "gate-creates"); err != nil {
		t.Fatalf("the stranding policy: %v", err)
	}
	// ONE ROW WITHHOLDS THE WHOLE AUTHORITY. The skip is per projection, not
	// per kind, so core's other version-12 declarations do not land either.
	// An owner reading the log needs the blast radius to be the authority,
	// and a narrower skip is a change this assertion sees.
	if v := kindVersion(t, ds, coreAuthority+"/trigger"); v != 9 {
		t.Fatalf("trigger is at version %d; the refusal withholds all of core, not one kind", v)
	}
}

// kindVersion reads one live declaration's stored version.
func kindVersion(t *testing.T, ds substrate.Dataset, ref string) int64 {
	t.Helper()
	ty, err := ds.KindByRef(context.Background(), ref)
	if err != nil {
		t.Fatalf("the %s kind: %v", ref, err)
	}
	return ty.Version
}
