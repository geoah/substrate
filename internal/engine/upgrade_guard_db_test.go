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
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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
	if _, err := svc.CreateRepository(ctx, testdb.Username(t), testdb.Authority(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Username(t))
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
	svc, err := engine.OpenForTest(t, context.Background(), dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(tree))
	if err != nil {
		return err
	}
	defer func() { _ = svc.Close() }()
	_, err = svc.Dataset(context.Background(), testdb.Username(t))
	return err
}

func TestBootUpgradeRefusesANarrowingWithLiveRows(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	// llmprovider pins a version of its own, so the authority bump alone would
	// keep the stored declaration and never classify the retype: the pin moves
	// too, and the guard is the only thing that keeps the narrowing out.
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		return pinVersion(t, narrowLabel(t, doc), "99")
	})

	// The open SUCCEEDS — a repository whose rows a guard names must still be
	// reachable, or the migration it is asking for cannot be performed. The
	// refusal names the property and the count.
	refused := openMovedRefused(t, dsn, tree)
	wantRefusedUpgrade(t, refused, `property "label" changes kind string → int`, "1 live records")
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
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
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
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
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

// providerWrite opens the database under the real tree and writes one
// llmprovider row, answering what the write said.
func providerWrite(t *testing.T, dsn, id string, props map[string]any) error {
	t.Helper()
	ctx := context.Background()
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(shippedTree(t)))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: id, Properties: props,
	})
	return err
}

// openMovedRefused is openMoved with the engine log's `refused` attribute
// captured, so a test can read the guard line the refused upgrade writes: the
// count is the operator's whole interface to the migration it asks for.
func openMovedRefused(t *testing.T, dsn, tree string) string {
	t.Helper()
	bumpPackageVersion(t, tree, corePackage, "99")
	return openRefused(t, dsn, tree)
}

// openRefused is openMovedRefused without the version bump: the tree is opened
// as it stands, for a test that moved the version itself.
func openRefused(t *testing.T, dsn, tree string) string {
	t.Helper()
	var refused string
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == "refused" {
				refused = a.Value.String()
			}
			return a
		},
	}))
	svc, err := engine.OpenForTest(t, context.Background(), dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(tree), engine.WithLogger(log))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	defer func() { _ = svc.Close() }()
	if _, err := svc.Dataset(context.Background(), testdb.Username(t)); err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	return refused
}

// wantRefusedUpgrade asserts the boot refused the upgrade with the guard line.
func wantRefusedUpgrade(t *testing.T, refused string, fragments ...string) {
	t.Helper()
	if refused == "" {
		t.Fatal("the boot did not refuse the upgrade")
	}
	for _, f := range fragments {
		if !strings.Contains(refused, f) {
			t.Fatalf("the refusal must name %q, got: %s", f, refused)
		}
	}
}

// The value constraints at the boot door. Every shipped declaration below pins
// version 99 so the authority bump re-projects it; the guard, and nothing else,
// is what keeps the tightened shape out.
func TestBootUpgradeRefusesATightenedPatternWithLiveRows(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const from = "    label:\n      type: string\n"
		if !strings.Contains(doc, from) {
			t.Fatal("llmprovider no longer declares `label` as a plain string")
		}
		return pinVersion(t, strings.Replace(doc, from, from+"      pattern: \"^[a-z]+$\"\n", 1), "99")
	})
	// The seeded row's label carries a space, which the pattern refuses.
	refused := openMovedRefused(t, dsn, tree)
	wantRefusedUpgrade(t, refused, `property "label" changes its pattern to ^[a-z]+$`, "1 live records")
	stillSpeaksTheOldShape(t, dsn)
}

func TestBootUpgradeRefusesALoweredMaxWithLiveRows(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	// A float field at depth: `defaults.temperature` is bounded 0..2.
	numbers := map[string]any{"label": "numbers", "wire": "openai", "defaults": map[string]any{"temperature": 1.5}}
	if err := providerWrite(t, dsn, "numbers", numbers); err != nil {
		t.Fatalf("put the numbered row: %v", err)
	}
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const from = "          max: 2\n"
		if strings.Count(doc, from) != 1 {
			t.Fatal("llmprovider no longer bounds `defaults.temperature` at 2, once")
		}
		return pinVersion(t, strings.Replace(doc, from, "          max: 1\n", 1), "99")
	})
	refused := openMovedRefused(t, dsn, tree)
	wantRefusedUpgrade(t, refused, `object "defaults" field "temperature" requires values <= 1`, "1 live records")
	// The stored bound still admits the value the tightened one refused.
	if err := providerWrite(t, dsn, "again", numbers); err != nil {
		t.Fatalf("the old bound must still be writable, so the narrowing landed anyway: %v", err)
	}
}

func TestBootUpgradeRefusesARaisedDecimalMinWithLiveRows(t *testing.T) {
	t.Parallel()
	dsn := seededRepository(t)
	// A decimal field inside a repeated object: `pricing[].inputPer1M` is
	// bounded at 0.
	numbers := map[string]any{"label": "numbers", "wire": "openai", "pricing": []any{
		map[string]any{"model": "m", "inputPer1M": "0.50"},
	}}
	if err := providerWrite(t, dsn, "numbers", numbers); err != nil {
		t.Fatalf("put the numbered row: %v", err)
	}
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const from = "        inputPer1M:\n          type: decimal\n          min: 0\n"
		if !strings.Contains(doc, from) {
			t.Fatal("llmprovider no longer declares `pricing.inputPer1M` as a decimal bounded at 0")
		}
		return pinVersion(t, strings.Replace(doc, from, "        inputPer1M:\n          type: decimal\n          min: 1\n", 1), "99")
	})
	refused := openMovedRefused(t, dsn, tree)
	wantRefusedUpgrade(t, refused, `object "pricing" field "inputPer1M" requires values >= 1`, "1 live records")
	if err := providerWrite(t, dsn, "again", numbers); err != nil {
		t.Fatalf("the old bound must still be writable, so the narrowing landed anyway: %v", err)
	}
}

// The bundle door: an upgrade of an installed closure that tightens a pattern is
// refused with the count, the preview names the same blocker, and a pattern
// every stored value satisfies lands.
func TestBundleUpgradeRefusesATightenedPatternWithLiveRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := installMailBundle(t)
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbItemType, ID: "kept", Properties: map[string]any{"name": "keep me"},
	})
	closure := func(pattern string) []map[string]any {
		item := vocabulary.KindManifest(mbPackage,
			map[string]any{"singular": "mailitem"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string", "pattern": pattern},
			}})
		docs := mbDocs(nil, mbConfigTypeDoc(), mbAccountTypeDoc(), item, mbMessageTypeDoc(),
			mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource))
		docs[0] = vocabulary.PackageManifest(mbPackage, 2)
		return docs
	}
	want := `type ` + mbItemType + `: property "name" changes its pattern to ^[a-z]+$`

	planner, ok := ds.(substrate.BundleUpgradePlanner)
	if !ok {
		t.Fatal("dataset does not plan bundle upgrades")
	}
	plan, err := planner.PlanBundleUpgrade(ctx, closure("^[a-z]+$"))
	if err != nil {
		t.Fatalf("plan the upgrade: %v", err)
	}
	blocked := false
	for _, b := range plan.Blockers {
		if strings.Contains(b, want) && strings.Contains(b, "1 live records") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("the preview must carry the guard line %q, got %+v", want, plan.Blockers)
	}

	_, err = applier(t, ds).ApplyVocabularyDocuments(ctx, owner, closure("^[a-z]+$"))
	wantNarrowingGuard(t, err, want, "1 live records")

	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, closure("^[a-z ]+$")); err != nil {
		t.Fatalf("a pattern every stored value matches must land: %v", err)
	}
}

// The boot door takes the retired-name check the apply verb takes (decision
// 0055). A repository whose stored core header retired a kind name refuses a
// binary that ships a kind by that name, and a binary whose tree dropped the
// retirement: the upgrade is skipped, the open succeeds, the stored row keeps
// the reservation.
func TestBootUpgradeRefusesARetiredName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)

	// Binary N+1 retires `gadget` in core, an additive change that lands, and
	// the stored header carries it.
	retiring := shippedTree(t)
	patchShipped(t, packageHeader(retiring, corePackage), func(doc string) string {
		return retireShippedKind(t, doc, "gadget")
	})
	if err := openMoved(t, dsn, retiring); err != nil {
		t.Fatalf("retiring a name is additive and must land: %v", err)
	}
	storedRetired := func(when string) {
		t.Helper()
		svc := openTree(t, dsn, shippedTree(t))
		defer func() { _ = svc.Close() }()
		ds, err := svc.Dataset(ctx, testdb.Username(t))
		if err != nil {
			t.Fatalf("%s: dataset: %v", when, err)
		}
		header := mustGet(t, ds, "substrate.reamde.dev/core/package", corePackage)
		if got := fmt.Sprint(header.Properties["retired"]); got != "map[kinds:[gadget]]" {
			t.Fatalf("%s: stored core header retired = %v", when, header.Properties["retired"])
		}
		if _, err := ds.KindByRef(ctx, corePackage+"/gadget"); err == nil {
			t.Fatalf("%s: the retired kind is declared", when)
		}
	}
	storedRetired("after the retirement landed")

	// Binary N+2 ships a `gadget` kind under core, from a tree that no longer
	// carries the retirement (a tree carrying both is refused by the loader
	// before it opens anything): the upgrade is refused and the repository
	// opens on the stored declarations.
	reusing := shippedTree(t)
	addShippedKind(t, reusing, corePackage, "gadget")
	bumpPackageVersion(t, reusing, corePackage, "100")
	svc := openTree(t, dsn, reusing)
	if _, err := svc.Dataset(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	_ = svc.Close()
	storedRetired("after a binary reused the name")

	// Binary N+3 ships the tree without the block: a retirement is permanent,
	// so the upgrade is refused whole and the stored header keeps it.
	dropping := shippedTree(t)
	bumpPackageVersion(t, dropping, corePackage, "101")
	svc = openTree(t, dsn, dropping)
	if _, err := svc.Dataset(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	_ = svc.Close()
	storedRetired("after a binary dropped the retirement")

	// The declared-again branch on its own: a tree at the STORED header version
	// (99) adds gadget.yaml, a new declaration the upgrade would append while
	// leaving the header exactly as stored, list included. Nothing drops the
	// list, so the refusal has to come from the name being declared, and the
	// log says so.
	appending := shippedTree(t)
	addShippedKind(t, appending, corePackage, "gadget")
	bumpPackageVersion(t, appending, corePackage, "99")
	var logs bytes.Buffer
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(appending), engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := svc.Dataset(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	_ = svc.Close()
	if !strings.Contains(logs.String(), "kind substrate.reamde.dev/core/gadget: the name is retired in package substrate.reamde.dev/core; a retired name is never declared again") {
		t.Fatalf("the refusal did not name the declared-again branch:\n%s", logs.String())
	}
	storedRetired("after a binary appended the retired kind")
}

// The boot upgrade never prunes, so a tree that retires a kind name this
// repository still declares is refused: landing the header beside the kind row
// would leave a stored closure the next open refuses as retired-and-declared.
func TestBootUpgradeRefusesRetiringAHeldKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)

	// Binary N+1 ships core/gadget: it lands and the repository declares it.
	shipping := shippedTree(t)
	addShippedKind(t, shipping, corePackage, "gadget")
	if err := openMoved(t, dsn, shipping); err != nil {
		t.Fatalf("shipping a new kind must land: %v", err)
	}

	// Binary N+2 stops shipping gadget and retires the name while this
	// repository still holds the kind: refused, the open succeeds, the kind
	// still resolves and the header carries no retirement.
	retiring := shippedTree(t)
	patchShipped(t, packageHeader(retiring, corePackage), func(doc string) string {
		return retireShippedKind(t, doc, "gadget")
	})
	bumpPackageVersion(t, retiring, corePackage, "100")
	svc := openTree(t, dsn, retiring)
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	if _, err := ds.KindByRef(ctx, corePackage+"/gadget"); err != nil {
		t.Fatalf("the held kind must still resolve: %v", err)
	}
	header := mustGet(t, ds, "substrate.reamde.dev/core/package", corePackage)
	if got, stored := header.Properties["retired"]; stored {
		t.Fatalf("the retirement landed beside the held kind: %v", got)
	}
	_ = svc.Close()

	// The repository opens again under the same tree: the stored closure is
	// consistent, so nothing refuses the open.
	svc = openTree(t, dsn, retiring)
	if _, err := svc.Dataset(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("the repository must keep opening: %v", err)
	}
	_ = svc.Close()
}

// retireShippedKind adds `retired: {kinds: [name]}` to a shipped package
// header, the first document of the file.
func retireShippedKind(t *testing.T, doc, name string) string {
	t.Helper()
	header, rest, split := strings.Cut(doc, "\n---")
	at := rePackageVersion.FindStringIndex(header)
	if at == nil {
		t.Fatal("the package header no longer declares a version")
	}
	header = header[:at[1]] + "\n  retired:\n    kinds:\n      - " + name + header[at[1]:]
	if split {
		return header + "\n---" + rest
	}
	return header
}

// The boot door converts a shipped rename at open, in the transaction that
// projects the declaration (decision 0063): the live row reads the value under
// the new name, the old name is undeclared, and the open neither refused nor
// skipped the upgrade.
func TestBootUpgradeConvertsAShippedRename(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)
	tree := shippedTree(t)
	renameShippedLabel(t, tree)
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a shipped rename must land at open: %v", err)
	}

	svc := openTree(t, dsn, tree)
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	got := mustGet(t, ds, "substrate.reamde.dev/core/llmprovider", "guarded")
	if got.Properties["displayLabel"] != "a label" || got.Properties["label"] != nil {
		t.Fatalf("the boot did not move the value: %v", got.Properties)
	}
	// Landed, the preview reports the rename as done: nothing to write.
	planner, ok := ds.(substrate.ShippedUpgradePlanner)
	if !ok {
		t.Fatal("dataset does not plan the shipped upgrade")
	}
	plans, err := planner.PlanShippedUpgrade(ctx)
	if err != nil {
		t.Fatalf("plan the shipped upgrade: %v", err)
	}
	for _, p := range plans {
		if p.Upgrade.Available || len(p.Upgrade.Steps) > 0 {
			t.Fatalf("the landed rename still shows as pending: %+v", p)
		}
	}
	if got.Title != "a label (openai)" {
		t.Fatalf("the title did not render under the renamed template: %q", got.Title)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "renamed",
		Properties: map[string]any{"displayLabel": "written under the new name", "wire": "openai"},
	})
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "stale",
		Properties: map[string]any{"label": "the old name", "wire": "openai"},
	}); err == nil {
		t.Fatal("the old name must be undeclared once the rename landed")
	}
}

// renameShippedLabel is the shipped rename the boot tests drive: llmprovider's
// `label` becomes `displayLabel`, the kind's own template follows, and the
// declaration pins version 99 so the authority bump re-projects it.
func renameShippedLabel(t *testing.T, tree string) {
	t.Helper()
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const from = "    label:\n      type: string\n"
		if !strings.Contains(doc, from) {
			t.Fatal("llmprovider no longer declares `label` as a plain string")
		}
		doc = strings.Replace(doc, from, "    displayLabel:\n      type: string\n      renamedFrom: label\n", 1)
		const tmpl = `displayTemplate: "{label|localName} ({wire})"`
		if !strings.Contains(doc, tmpl) {
			t.Fatal("llmprovider no longer titles itself from `label`")
		}
		doc = strings.Replace(doc, tmpl, `displayTemplate: "{displayLabel|localName} ({wire})"`, 1)
		return pinVersion(t, doc, "99")
	})
}

// addShippedMirror adds a provider-mirror shape to the shipped core tree: a
// `gadget` kind with an EMPTY subject slot (an unpinned, optional `person`
// reference marked `subject: true`, the shape a provider ships so the package
// owning the subject kind can declare the mapping, record 49) and one string
// property named by prop.
func addShippedMirror(t *testing.T, tree, prop string) {
	t.Helper()
	doc := "kind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + corePackage + "/gadget\ndata:\n" +
		"  authority: substrate.reamde.dev\n  package: core\n  names:\n    singular: gadget\n" +
		"  properties:\n    " + prop + ":\n      type: string\n"
	if prop != "login" {
		doc += "      renamedFrom: login\n"
	}
	doc += "    person:\n      type: reference\n      subject: true\n      mustExist: true\n"
	if err := os.WriteFile(filepath.Join(tree, corePackage, "gadget.yaml"), []byte(doc), 0o600); err != nil {
		t.Fatalf("add the shipped mirror kind: %v", err)
	}
}

// The mapping half of the boot door: a repository's own mapping FROM a shipped
// mirror kind reads the renamed property as a map path. The boot builds its
// candidate from the stored packages plus the shipped ones and compiles it, so
// the stale path refuses the boot with the loader's own line, the open
// succeeds, and the rename lands once the mapping moves, rewriting the mirror
// record the mapping projects from.
func TestBootUpgradeRefusesARenameAStoredMappingReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)
	ownerPackage := testdb.Authority(t) + "/handles"
	const gadget = corePackage + "/gadget"

	// Binary N+1 ships the mirror; the owner declares the subject kind and the
	// mapping from it, and writes one mirror record.
	shipping := shippedTree(t)
	addShippedMirror(t, shipping, "login")
	if err := openMoved(t, dsn, shipping); err != nil {
		t.Fatalf("shipping the mirror kind must land: %v", err)
	}
	mapping := func(path string) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(ownerPackage, 0),
			vocabulary.KindManifest(ownerPackage, map[string]any{"singular": "handleowner"},
				map[string]any{"properties": map[string]any{"handle": map[string]any{"type": "string"}}}),
			vocabulary.MappingManifest(ownerPackage, "gadgethandleowner", map[string]any{
				"from": gadget, "to": ownerPackage + "/handleowner", "property": "person",
				"map": map[string]any{"handle": map[string]any{"path": path}},
			}),
		}
	}
	declare := func(t *testing.T, tree, path string) substrate.Dataset {
		t.Helper()
		svc := openTree(t, dsn, tree)
		t.Cleanup(func() { _ = svc.Close() })
		ds, err := svc.Dataset(ctx, testdb.Username(t))
		if err != nil {
			t.Fatalf("dataset: %v", err)
		}
		if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mapping(path)); err != nil {
			t.Fatalf("declare the mapping: %v", err)
		}
		return ds
	}
	ds := declare(t, shipping, "login")
	mirror := mustPut(t, ds, owner, substrate.PutInput{
		Kind: gadget, ID: "g1", Properties: map[string]any{"login": "geoah"},
	})
	subjectKind, subjectID, _ := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, gadget, mirror.ID), "person"))
	if got := mustGet(t, ds, subjectKind, subjectID); got.Properties["handle"] != "geoah" {
		t.Fatalf("the mapping did not project the handle: %v", got.Properties)
	}

	// Binary N+2 renames `login` to `username`: the stored mapping's path no
	// longer resolves against the candidate, so the boot refuses, naming the
	// mapping, and the mirror record keeps its value under the old name.
	renaming := shippedTree(t)
	addShippedMirror(t, renaming, "username")
	bumpPackageVersion(t, renaming, corePackage, "100")
	refused := openRefused(t, dsn, renaming)
	wantRefusedUpgrade(t, refused, "recordmapping "+ownerPackage+"/gadgethandleowner", `declares no property "login"`)
	svc := openTree(t, dsn, renaming)
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if got := mustGet(t, ds, gadget, mirror.ID); got.Properties["login"] != "geoah" || got.Properties["username"] != nil {
		t.Fatalf("a refused boot rename moved the value: %v", got.Properties)
	}
	_ = svc.Close()

	// The mapping cannot move first: its path type-checks against the stored
	// declaration, which still says `login`. The way out is to delete the
	// mapping, let the next open land the rename, and declare the mapping
	// again on the new name.
	svc = openTree(t, dsn, shippedTree(t))
	ds, err = svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mapping("username")); err == nil {
		t.Fatal("a mapping path onto the new name must not compile before the rename landed")
	}
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/recordmapping", ownerPackage+"/gadgethandleowner", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the mapping: %v", err)
	}
	_ = svc.Close()
	svc = openTree(t, dsn, renaming)
	if _, err := svc.Dataset(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("dataset: %v", err)
	}
	_ = svc.Close()
	declare(t, renaming, "username")
	svc = openTree(t, dsn, renaming)
	defer func() { _ = svc.Close() }()
	ds, err = svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if got := mustGet(t, ds, gadget, mirror.ID); got.Properties["username"] != "geoah" || got.Properties["login"] != nil {
		t.Fatalf("the rename did not land once the mapping moved: %v", got.Properties)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: gadget, ID: "g1", Properties: map[string]any{"username": "george"}})
	if got := mustGet(t, ds, subjectKind, subjectID); got.Properties["handle"] != "george" {
		t.Fatalf("the redeclared mapping does not project from the new name: %v", got.Properties)
	}
}

// addShippedGadget adds a core `gadget` kind pinned at its own version, whose
// displayTemplate reads an llmprovider property through a reference pinned at
// llmprovider: the shape whose stored and embedded declarations can disagree.
func addShippedGadget(t *testing.T, tree, version, tmpl string) {
	t.Helper()
	doc := "kind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + corePackage + "/gadget\ndata:\n" +
		"  authority: substrate.reamde.dev\n  package: core\n  version: " + version + "\n" +
		"  names:\n    singular: gadget\n" +
		"  displayTemplate: \"" + tmpl + "\"\n" +
		"  properties:\n    note:\n      type: string\n" +
		"    provider:\n      type: reference\n      kind: substrate.reamde.dev/core/llmprovider\n"
	if err := os.WriteFile(filepath.Join(tree, corePackage, "gadget.yaml"), []byte(doc), 0o600); err != nil {
		t.Fatalf("add the shipped gadget kind: %v", err)
	}
}

// The boot's candidate is EXACTLY the projected set: a stored declaration
// newer than the shipped one is kept, so the guards compile against the kept
// stored declaration and not the embedded one. Here the stored `gadget` (200)
// already reads `{provider.displayLabel}` while the binary embeds an older
// gadget (150) still reading `{provider.label}`; a candidate built from the
// shipped package whole would refuse a valid rename on the embedded template.
func TestBootUpgradeCompilesTheKeptStoredDeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)

	// Binary N+1 ships gadget at 200, already on the new name.
	ahead := shippedTree(t)
	addShippedGadget(t, ahead, "200", "{provider.displayLabel}")
	if err := openMoved(t, dsn, ahead); err != nil {
		t.Fatalf("shipping the gadget kind must land: %v", err)
	}

	// Binary N+2 renames llmprovider's label and embeds gadget at 150 on the
	// OLD name: gadget is kept at its stored 200, and the rename must land.
	renaming := shippedTree(t)
	renameShippedLabel(t, renaming)
	addShippedGadget(t, renaming, "150", "{provider.label}")
	if refused := openRefused(t, dsn, renaming); refused != "" {
		t.Fatalf("a kept stored declaration must not refuse the boot on its embedded twin: %s", refused)
	}
	svc := openTree(t, dsn, renaming)
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if got := mustGet(t, ds, "substrate.reamde.dev/core/llmprovider", "guarded"); got.Properties["displayLabel"] != "a label" {
		t.Fatalf("the rename did not land: %v", got.Properties)
	}
	gadget := mustGet(t, ds, "substrate.reamde.dev/core/kind", corePackage+"/gadget")
	if v, _ := vocabulary.VersionValue(gadget.Properties["version"]); v != 200 {
		t.Fatalf("the kept gadget is stored at version %d, want 200", v)
	}
	if gadget.Properties["displayTemplate"] != "{provider.displayLabel}" {
		t.Fatalf("the kept gadget's template is %v, want the stored one", gadget.Properties["displayTemplate"])
	}
	card := mustPut(t, ds, owner, substrate.PutInput{
		Kind: corePackage + "/gadget", ID: "g1",
		Properties: map[string]any{"provider": "substrate.reamde.dev/core/llmprovider/guarded"},
	})
	if card.Title != "a label" {
		t.Fatalf("the kept template does not render the renamed property: %q", card.Title)
	}
}

// The boot door reads the STORED closure, not the shipped tree alone: a
// repository's own kind whose displayTemplate reads the renamed property through
// a reference pinned at the shipped kind refuses the boot rename, the open
// succeeds on the stored declarations, and the rename lands once the template
// moves. Without the check the boot would rename, the stored template would
// render an empty title, and a stored mapping onto the property would fail to
// re-resolve and quarantine the user's package for a change the tree made.
func TestBootUpgradeRefusesARenameAStoredTemplateReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)
	viewerPackage := testdb.Authority(t) + "/dash"
	viewer := func(tmpl string) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(viewerPackage, 0),
			vocabulary.KindManifest(viewerPackage, map[string]any{"singular": "providercard"},
				map[string]any{
					"displayTemplate": tmpl,
					"properties": map[string]any{
						"provider": map[string]any{"type": "reference", "kind": "substrate.reamde.dev/core/llmprovider"},
					},
				}),
		}
	}
	declare := func(t *testing.T, tmpl string) {
		t.Helper()
		svc := openTree(t, dsn, shippedTree(t))
		defer func() { _ = svc.Close() }()
		ds, err := svc.Dataset(ctx, testdb.Username(t))
		if err != nil {
			t.Fatalf("dataset: %v", err)
		}
		if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, viewer(tmpl)); err != nil {
			t.Fatalf("declare the viewer kind: %v", err)
		}
	}
	declare(t, "{provider.label}")

	tree := shippedTree(t)
	renameShippedLabel(t, tree)
	refused := openMovedRefused(t, dsn, tree)
	wantRefusedUpgrade(t, refused,
		`kind `+viewerPackage+`/providercard: displayTemplate {provider.label} reads property "label" of substrate.reamde.dev/core/llmprovider, which this apply renames to "displayLabel"; rewrite the template first`)
	stillSpeaksTheOldShape(t, dsn)

	// The owner moves the template; the next open lands the rename.
	declare(t, "{provider.displayLabel}")
	svc := openTree(t, dsn, tree)
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	got := mustGet(t, ds, "substrate.reamde.dev/core/llmprovider", "guarded")
	if got.Properties["displayLabel"] != "a label" || got.Properties["label"] != nil {
		t.Fatalf("the rename did not land once the template moved: %v", got.Properties)
	}
	card := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       viewerPackage + "/providercard",
		Properties: map[string]any{"provider": "substrate.reamde.dev/core/llmprovider/guarded"},
	})
	if card.Title != "a label" {
		t.Fatalf("the stored template does not read the renamed property: %q", card.Title)
	}
}

// convertShippedProvider is the shipped backfill and remap the boot tests
// drive: llmprovider's optional `label` becomes required with a default, and
// the `azure` wire is respelled `azureopenai`. The declaration pins version 99
// so the authority bump re-projects it.
func convertShippedProvider(t *testing.T, tree string) {
	t.Helper()
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const label = "    label:\n      type: string\n"
		if !strings.Contains(doc, label) {
			t.Fatal("llmprovider no longer declares `label` as a plain string")
		}
		doc = strings.Replace(doc, label, "    label:\n      type: string\n      required: true\n      default: unnamed\n", 1)
		const azure = "        - value: azure\n          label: Azure OpenAI\n"
		if !strings.Contains(doc, azure) {
			t.Fatal("llmprovider no longer declares the `azure` wire")
		}
		doc = strings.Replace(doc, azure, "        - value: azureopenai\n          label: Azure OpenAI\n          renamedFrom: azure\n", 1)
		return pinVersion(t, doc, "99")
	})
}

// The boot door converts a shipped backfill and a shipped remap at open, in
// the transaction that projects the declaration (decision 0066): the row that
// lacked the label reads the default, the row on the old wire reads the new
// spelling, and the open neither refused nor skipped the upgrade.
func TestBootUpgradeConvertsAShippedBackfillAndRemap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)
	const provider = "substrate.reamde.dev/core/llmprovider"
	// Beside the seeded row (a label, the openai wire): one without a label
	// on the wire the tree respells.
	{
		svc := openTree(t, dsn, shippedTree(t))
		ds, err := svc.Dataset(ctx, testdb.Username(t))
		if err != nil {
			t.Fatalf("dataset: %v", err)
		}
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: provider, ID: "bare", Properties: map[string]any{"wire": "azure"},
		})
		_ = svc.Close()
	}
	tree := shippedTree(t)
	convertShippedProvider(t, tree)
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a shipped backfill and remap must land at open: %v", err)
	}

	svc := openTree(t, dsn, tree)
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	bare := mustGet(t, ds, provider, "bare")
	if bare.Properties["label"] != "unnamed" || bare.Properties["wire"] != "azureopenai" {
		t.Fatalf("the boot did not convert the row: %v", bare.Properties)
	}
	if guarded := mustGet(t, ds, provider, "guarded"); guarded.Properties["label"] != "a label" || guarded.Properties["wire"] != "openai" {
		t.Fatalf("the boot rewrote a row no step touched: %v", guarded.Properties)
	}
	// Landed, the preview reports nothing pending.
	planner, ok := ds.(substrate.ShippedUpgradePlanner)
	if !ok {
		t.Fatal("dataset does not plan the shipped upgrade")
	}
	plans, err := planner.PlanShippedUpgrade(ctx)
	if err != nil {
		t.Fatalf("plan the shipped upgrade: %v", err)
	}
	for _, p := range plans {
		if p.Upgrade.Available {
			t.Fatalf("the landed conversion still shows as pending: %+v", p)
		}
	}
	// The repository speaks the new shape: a create without a label takes the
	// default, and the old wire spelling is undeclared.
	if got := mustPut(t, ds, owner, substrate.PutInput{
		Kind: provider, ID: "later", Properties: map[string]any{"wire": "openai"},
	}); got.Properties["label"] != "unnamed" {
		t.Fatalf("a create did not take the default: %v", got.Properties)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: provider, ID: "stale", Properties: map[string]any{"label": "x", "wire": "azure"},
	}); err == nil {
		t.Fatal("the old wire spelling must be undeclared once the remap landed")
	}
}

// A shipped remap onto a wire the stored declaration still admits would make
// two providers' rows one set. The boot never runs a lossy step (decision
// 0067): it refuses, the open succeeds on the stored declarations, and the
// preview names the refusal with the plan behind it.
func TestBootUpgradeRefusesAShippedLossyRemap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)
	// A row on the wire the tree renames: lossiness is judged over the live
	// records, so the collapse needs a record to lose.
	{
		svc := openTree(t, dsn, shippedTree(t))
		ds, err := svc.Dataset(ctx, testdb.Username(t))
		if err != nil {
			t.Fatalf("dataset: %v", err)
		}
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: "substrate.reamde.dev/core/llmprovider", ID: "bare", Properties: map[string]any{"wire": "azure"},
		})
		_ = svc.Close()
	}
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const azure = "        - value: azure\n          label: Azure OpenAI\n"
		if !strings.Contains(doc, azure) {
			t.Fatal("llmprovider no longer declares the `azure` wire")
		}
		const openai = "        - value: openai\n          label: OpenAI\n"
		if !strings.Contains(doc, openai) {
			t.Fatal("llmprovider no longer declares the `openai` wire")
		}
		doc = strings.Replace(doc, azure, "", 1)
		doc = strings.Replace(doc, openai, "        - value: openai\n          label: OpenAI\n          renamedFrom: azure\n", 1)
		return pinVersion(t, doc, "99")
	})
	refused := openMovedRefused(t, dsn, tree)
	wantRefusedUpgrade(t, refused, `property "wire" value "azure" rewritten to "openai" on 1 live records, a value live records already hold`, "never runs a lossy step")
	stillSpeaksTheOldShape(t, dsn)

	svc := openTree(t, dsn, tree)
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	planner, ok := ds.(substrate.ShippedUpgradePlanner)
	if !ok {
		t.Fatal("dataset does not plan the shipped upgrade")
	}
	plans, err := planner.PlanShippedUpgrade(ctx)
	if err != nil {
		t.Fatalf("plan the shipped upgrade: %v", err)
	}
	var named, planned bool
	for _, p := range plans {
		for _, b := range p.Upgrade.Blockers {
			named = named || strings.Contains(b, "lossy conversion is refused")
		}
		// The plan rides the preview too: the lossy step with its count, the
		// hash and the head a door that could confirm would name.
		for _, s := range p.Upgrade.Steps {
			planned = planned || (s.Step == substrate.StepRemap && s.From == "azure" && s.To == "openai" && s.Records == 1 && s.Lossy)
		}
		if p.Upgrade.Available && (!p.Upgrade.Lossy || p.Upgrade.PlanHash == "" || p.Upgrade.ChangelogSeq == 0) {
			t.Fatalf("the preview carries no plan for the refused upgrade: %+v", p.Upgrade)
		}
	}
	if !named || !planned {
		t.Fatalf("the preview does not name the lossy remap (named=%v planned=%v): %+v", named, planned, plans)
	}
}

// The same shipped remap onto a retained wire is lossless while no live record
// holds the target: the boot runs it unattended and the row reads the new
// spelling, because lossiness is judged over the records and never the
// declaration (decision 0067).
func TestBootUpgradeConvertsARemapOntoARetainedValueNobodyHolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)
	const provider = "substrate.reamde.dev/core/llmprovider"
	// The one live row moves onto the wire the tree renames, so nothing holds
	// `openai` when the remap lands on it.
	{
		svc := openTree(t, dsn, shippedTree(t))
		ds, err := svc.Dataset(ctx, testdb.Username(t))
		if err != nil {
			t.Fatalf("dataset: %v", err)
		}
		if _, err := ds.Patch(ctx, owner, provider, "guarded", substrate.PatchInput{Properties: map[string]any{"wire": "azure"}}); err != nil {
			t.Fatalf("move the row onto the old wire: %v", err)
		}
		_ = svc.Close()
	}
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		const azure = "        - value: azure\n          label: Azure OpenAI\n"
		const openai = "        - value: openai\n          label: OpenAI\n"
		if !strings.Contains(doc, azure) || !strings.Contains(doc, openai) {
			t.Fatal("llmprovider no longer declares the `azure` and `openai` wires")
		}
		doc = strings.Replace(doc, azure, "", 1)
		doc = strings.Replace(doc, openai, "        - value: openai\n          label: OpenAI\n          renamedFrom: azure\n", 1)
		return pinVersion(t, doc, "99")
	})
	if err := openMoved(t, dsn, tree); err != nil {
		t.Fatalf("a remap onto a value nobody holds must land at open: %v", err)
	}
	svc := openTree(t, dsn, tree)
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if got := mustGet(t, ds, provider, "guarded"); got.Properties["wire"] != "openai" {
		t.Fatalf("the boot did not convert the row: %v", got.Properties)
	}
}
