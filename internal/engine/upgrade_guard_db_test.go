package engine_test

// The boot-time shipped-vocabulary upgrade and `/vocabulary/apply` must refuse
// the SAME changes. They used to disagree: an operator applying a narrowing
// declaration by hand was refused with the live-row count, while the upgrade at
// open projected the very same change silently — leaving rows shaped one way
// under a declaration that said another, with nothing anywhere reporting it. A
// guard only one door honors is not a guard.
//
// Refusing the UPGRADE is not refusing the REPOSITORY: the open succeeds on the
// stored declarations, because the migration a guard demands has to run through
// the API that failing the open would have closed. So these assert both halves
// — the new shape does not land, AND the repository still works.
//
// These drive the upgrade the only way it can be driven: two Opens over ONE
// database, the second carrying a shipped tree that moved (seed_db_test.go's
// harness — `shippedTree` plays binary N, the patched copy binary N+1).

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const corePackage = "substrate.reamde.dev/core"

// coreKind is one shipped core declaration inside a copied tree.
func coreKind(tree, file string) string {
	return filepath.Join(tree, corePackage, file)
}

func patchShipped(t *testing.T, path string, replace func(string) string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(replace(string(raw))), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// pinVersion rewrites a declaration's own `version:`, whatever it stands at.
// A kind that pins one of its own (llmprovider, recordpatchpolicy) needs the
// line REPLACED by a test that wants an older pin, not a second key the parser
// would refuse.
func pinVersion(t *testing.T, doc, version string) string {
	t.Helper()
	at := reDeclaredVersion.FindStringIndex(doc)
	if at == nil {
		t.Fatal("the declaration no longer pins a version of its own")
	}
	return reDeclaredVersion.ReplaceAllString(doc, "\n  version: "+version+"\n")
}

// The declaration's OWN version line: at the `data:` block's indentation, so a
// property named `version` (the kind declares one) is never the match.
var reDeclaredVersion = regexp.MustCompile(`\n  version: \S+\n`)

// narrowLabel turns llmprovider's `label` from a string into an int — the same
// datatype change `/vocabulary/apply` refuses while a live row holds a string.
func narrowLabel(t *testing.T, doc string) string {
	t.Helper()
	const from = "    label:\n      type: string\n"
	if !strings.Contains(doc, from) {
		t.Fatal("llmprovider no longer declares `label` as a plain string")
	}
	return strings.Replace(doc, from, "    label:\n      type: int\n", 1)
}

// seededRepository creates a repository under the REAL tree and leaves ONE
// live llmprovider row in it — the row every narrowing below would strand.
// Written here rather than seeded: a repository holds no provider until its
// owner writes one.
func seededRepository(t *testing.T) (dsn string) {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "guarded",
		Properties: map[string]any{"label": "a label", "wire": "openai"},
	}); err != nil {
		t.Fatalf("put the live provider row: %v", err)
	}
	return dsn
}

// openMoved opens the database again under a tree whose core authority moved,
// and returns what the upgrade said.
func openMoved(t *testing.T, dsn, tree string) error {
	t.Helper()
	bumpPackageVersion(t, tree, corePackage, "99")
	svc, err := engine.Open(context.Background(), dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(tree))
	if err != nil {
		return err
	}
	defer func() { _ = svc.Close() }()
	_, err = svc.Dataset(context.Background(), "geoah")
	return err
}

func TestBootUpgradeRefusesANarrowingWithLiveRows(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		return narrowLabel(t, doc)
	})

	// The open SUCCEEDS — a repository whose rows a guard names must still be
	// reachable, or the migration it is asking for cannot be performed.
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	// …and the narrowing did NOT land: the repository still speaks the shape
	// its live rows are written in.
	stillSpeaksTheOldShape(t, dsn)
}

// stillSpeaksTheOldShape asserts the repository still WORKS under the stored
// declaration: a write the refused narrowing would have rejected is accepted.
// Structural poking at the declaration row would only prove the projection
// skipped; this proves the thing that matters, which is that the rows the guard
// named can still be reached and rewritten.
func stillSpeaksTheOldShape(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "another",
		Properties: map[string]any{"label": "still a string", "wire": "openai"},
	}); err != nil {
		t.Fatalf("the old shape must still be writable — the narrowing landed anyway: %v", err)
	}
}

func TestBootUpgradeAdmitsAnAdditiveChange(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	// The version moves and the declaration only GAINS a property. The guard
	// exists to stop a narrowing, never an ordinary upgrade.
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		return strings.Replace(doc, "  properties:\n",
			"  properties:\n    region:\n      type: string\n      description: an added, optional property\n", 1)
	})
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("an additive upgrade must land: %v", err)
	}
}

func TestBootUpgradeIgnoresAKindItDoesNotRewrite(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	// llmprovider narrows AND pins its own version, which the authority bump
	// below cannot lift — so the upgrade holds the stored declaration exactly
	// as it stands and never rewrites this kind, while every other core
	// declaration moves around it. A guard that classified every kind in a
	// touched authority, rather than the ones actually being rewritten, would
	// refuse this boot over a change nobody is making.
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		return pinVersion(t, narrowLabel(t, doc), "1")
	})
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a kind the upgrade will not rewrite must not refuse the boot: %v", err)
	}
}

// The boot upgrade takes the DEFAULT check `/vocabulary/apply` takes. A shipped
// property whose declared default no write could store would otherwise land at
// open and break every create of that kind afterwards, with the door that
// refuses it by hand never consulted.
func TestBootUpgradeRefusesAnUnstorableDefault(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		// llmprovider pins a version of its own, so the authority bump alone
		// would leave this declaration exactly where it stands and re-project
		// nothing, and the upgrade under test would never run.
		doc = pinVersion(t, doc, "99")
		return strings.Replace(doc, "  properties:\n",
			"  properties:\n    region:\n      type: string\n      pattern: \"^eu-\"\n"+
				"      default: us-east\n      description: a default the pattern refuses\n", 1)
	})
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	// The declaration did not land: the property the bad default rode in on is
	// not declared, so a write naming it is refused as undeclared.
	ctx := context.Background()
	svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "after",
		Properties: map[string]any{"label": "still writable", "wire": "openai", "region": "eu-west"},
	}); err == nil {
		t.Fatal("the declaration carrying the unstorable default must not have landed")
	}
	// …and the repository still works under the declaration it stored.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "after",
		Properties: map[string]any{"label": "still writable", "wire": "openai"},
	})
}
