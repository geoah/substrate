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
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const coreAuthority = "core.substrate.reamde.dev"

// coreKind is one shipped core declaration inside a copied tree.
func coreKind(tree, file string) string {
	return filepath.Join(tree, coreAuthority, file)
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
// llmprovider pins one of its own, so a test that wants an OLDER pin replaces
// it rather than adding a second key the parser would refuse.
func pinVersion(t *testing.T, doc, version string) string {
	t.Helper()
	at := reDeclaredVersion.FindStringIndex(doc)
	if at == nil {
		t.Fatal("llmprovider no longer pins a version of its own")
	}
	return reDeclaredVersion.ReplaceAllString(doc, "\n  version: "+version+"\n")
}

// The declaration's OWN version line: at the `data:` block's indentation, so a
// property named `version` (the kind declares one) is never the match.
var reDeclaredVersion = regexp.MustCompile(`\n  version: \S+\n`)

// narrowName turns llmprovider's `name` from a string into an int — the same
// datatype change `/vocabulary/apply` refuses while a live row holds a string.
func narrowName(t *testing.T, doc string) string {
	t.Helper()
	const from = "    name:\n      type: string\n"
	if !strings.Contains(doc, from) {
		t.Fatal("llmprovider no longer declares `name` as a plain string")
	}
	return strings.Replace(doc, from, "    name:\n      type: int\n", 1)
}

// seededRepository creates a repository under the REAL tree and leaves ONE
// live llmprovider row in it — the row every narrowing below would strand.
// Written here rather than seeded: a repository holds no provider until its
// owner writes one.
func seededRepository(t *testing.T) (dsn string) {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmprovider", ID: "guarded",
		Properties: map[string]any{"name": "a label", "wire": "openai"},
	}); err != nil {
		t.Fatalf("put the live provider row: %v", err)
	}
	return dsn
}

// openMoved opens the database again under a tree whose core authority moved,
// and returns what the upgrade said.
func openMoved(t *testing.T, dsn, tree string) error {
	t.Helper()
	bumpGroupVersion(t, tree, coreAuthority, "99")
	svc, err := engine.Open(context.Background(), dsn, engine.WithCredentialKey("test-cred-key"), engine.WithKindsDir(tree))
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
		return narrowName(t, doc)
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
	svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey("test-cred-key"), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmprovider", ID: "another",
		Properties: map[string]any{"name": "still a string", "wire": "openai"},
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
		return pinVersion(t, narrowName(t, doc), "1")
	})
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a kind the upgrade will not rewrite must not refuse the boot: %v", err)
	}
}

// oldLLMMessage is the llmmessage declaration as it stood before `thread`
// moved from an EDGE to a required REFERENCE — inline, because the point is a
// repository seeded under the OLD shape and then booted under this tree.
const oldLLMMessage = `kind: core.substrate.reamde.dev/kind
metadata:
  id: core.substrate.reamde.dev/llmmessage
data:
  authority: core.substrate.reamde.dev
  description: One turn of an agent thread, written by the loop as it runs.
  names:
    singular: llmmessage
    plural: llmmessages
  displayTemplate: "{role} #{turn}"
  properties:
    role:
      type: string
      description: user, assistant or tool
    content:
      type: text
      description: the turn's text
    turn:
      type: int
      description: the loop iteration this row belongs to, for ordering
  edges:
    thread:
      to: core.substrate.reamde.dev/llmthread
      required: true
      description: the thread this turn belongs to
`

// A relationship that MOVES — an edge becoming a required reference — has to
// announce itself. Dropping the edge is unguarded (edges are not diffed at
// all) and the reference is a NEW property name, so nothing classified it:
// the declaration would land quietly and leave every existing row pointing the
// old way, invisible to every read written against the new one. That is not a
// hypothetical — it is exactly what llmmessage does on this branch.
func TestBootUpgradeRefusesAnEdgeBecomingARequiredReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)

	// Seed under the OLD llmmessage: a thread, and a message holding the edge.
	before := shippedTree(t)
	patchShipped(t, coreKind(before, "llmmessage.yaml"), func(string) string {
		return oldLLMMessage
	})
	svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey("test-cred-key"), engine.WithKindsDir(before))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmthread", ID: "t1",
		Properties: map[string]any{
			"agent": vocabulary.RecordPath("core.substrate.reamde.dev/agent", "core.substrate.reamde.dev/chat"),
			"mode":  "chat",
		},
	}); err != nil {
		t.Fatalf("put a thread: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmmessage", ID: "m1",
		Properties: map[string]any{"role": "user", "content": "hi", "turn": 0},
		Edges:      []substrate.EdgeInput{{Rel: "thread", To: substrate.EdgeRef{ID: "t1"}}},
	}); err != nil {
		t.Fatalf("put a message under the old declaration: %v", err)
	}
	_ = svc.Close()

	// Boot under THIS tree, exactly as deploying this branch would. No version
	// bump needed: llmmessage pins its own version here and the stored
	// declaration carries the authority's older one, so the upgrade already
	// wants to run.
	svc2, err := engine.Open(ctx, dsn, engine.WithCredentialKey("test-cred-key"), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("open under the new tree: %v", err)
	}
	defer func() { _ = svc2.Close() }()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	// The new declaration did NOT land, so the message still reads under the
	// shape it was written with — which is the point: the row is reachable, so
	// it can be migrated or deleted.
	m, err := ds2.Get(ctx, "core.substrate.reamde.dev/llmmessage", "m1")
	if err != nil {
		t.Fatalf("the pre-migration message must still read: %v", err)
	}
	if len(m.Edges["thread"]) != 1 {
		t.Fatalf("the message lost the edge it was written with: %+v", m.Edges)
	}
	if _, moved := m.Properties["thread"]; moved {
		t.Fatal("the reference landed anyway — the guard did not hold")
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
	svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey("test-cred-key"), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmprovider", ID: "after",
		Properties: map[string]any{"name": "still writable", "wire": "openai", "region": "eu-west"},
	}); err == nil {
		t.Fatal("the declaration carrying the unstorable default must not have landed")
	}
	// …and the repository still works under the declaration it stored.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmprovider", ID: "after",
		Properties: map[string]any{"name": "still writable", "wire": "openai"},
	})
}
