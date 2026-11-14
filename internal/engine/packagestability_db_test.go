package engine_test

// A package's stability (decision record 0256) on its row: the label a header
// states is an authored key of the package record, so a client reads it where
// it reads the description. The seed writes it at creation, the boot upgrade
// writes it onto a repository that predates the key, the apply and install
// doors store what the header says and nothing for a header that says
// nothing, and a rebuild hands it back. The loader's half (the closed value
// set, the key on a package header alone) is internal/vocabulary's
// stability_test.go.

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

const packageKindRef = "substrate.reamde.dev/core/package"

// storedStability is the label a package's record carries, and whether it
// carries one at all.
func storedStability(t *testing.T, ds substrate.Dataset, pkg string) (string, bool) {
	t.Helper()
	raw, has := mustGet(t, ds, packageKindRef, pkg).Properties["stability"]
	if !has || raw == nil {
		return "", false
	}
	label, ok := raw.(string)
	if !ok {
		t.Fatalf("%s: stored stability is %T, not a string", pkg, raw)
	}
	return label, true
}

// wantSeededStabilities asserts every seeded package's record carries the
// label its shipped header states. Core's and llm's are spelled out, because
// which label each holds is the promise (kinds_test.go holds the whole table)
// and a reader of the tree alone would agree with any header.
func wantSeededStabilities(t *testing.T, ds substrate.Dataset, when string) {
	t.Helper()
	shipped, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	for _, pkg := range kinds.SeedPackages {
		g, ok := shipped.PackageByName(pkg)
		if !ok {
			t.Fatalf("the seed does not load %s", pkg)
		}
		if got, _ := storedStability(t, ds, pkg); got != g.Stability {
			t.Errorf("%s: %s stores stability %q, and its header states %q", when, pkg, got, g.Stability)
		}
	}
	if got, _ := storedStability(t, ds, kinds.SeedCorePackage); got != substrate.StabilityStable {
		t.Errorf("%s: core stores stability %q, want %q", when, got, substrate.StabilityStable)
	}
	if got, _ := storedStability(t, ds, kinds.SeedLLMPackage); got != substrate.StabilityExperimental {
		t.Errorf("%s: llm stores stability %q, want %q", when, got, substrate.StabilityExperimental)
	}
	// The authority row publishes packages and holds no kind to a promise.
	if raw, has := mustGet(t, ds, "substrate.reamde.dev/core/authority", kinds.SeedAuthority).Properties["stability"]; has && raw != nil {
		t.Errorf("%s: the authority row stores stability %v", when, raw)
	}
}

func TestCreationWritesEachSeededPackagesStability(t *testing.T) {
	t.Parallel()
	svc, ds := newCoreDataset(t)
	wantSeededStabilities(t, ds, "after creation")

	// The label is a declaration key like any other: it rides the changelog,
	// so a rebuild from the segment files carries it.
	if _, err := svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	wantSeededStabilities(t, ds, "after the rebuild")
}

var reHeaderStability = regexp.MustCompile(`\n  stability: \S+\n`)

// lowerDeclaredVersion puts a declaration's own `version:` one lower: the
// version it stood at before the change this fixture takes back out.
func lowerDeclaredVersion(t *testing.T, doc string) string {
	t.Helper()
	line := reDeclaredVersion.FindString(doc)
	if line == "" {
		t.Fatal("the declaration no longer pins a version of its own")
	}
	v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "version:")))
	if err != nil || v < 2 {
		t.Fatalf("the declared version %q has no version before it", strings.TrimSpace(line))
	}
	return strings.Replace(doc, line, "\n  version: "+strconv.Itoa(v-1)+"\n", 1)
}

// unlabeledTree is the shipped tree as a binary before the key shipped it:
// no seeded header states a stability, core's `package` kind declares no such
// property, and each of those declarations sits one version lower. A
// repository created from it is one the real tree has to upgrade.
func unlabeledTree(t *testing.T) string {
	t.Helper()
	tree := shippedTree(t)
	for _, pkg := range kinds.SeedPackages {
		_, word := vocabulary.SplitPackageRef(pkg)
		patchShipped(t, filepath.Join(tree, pkg, word+".yaml"), func(doc string) string {
			// The header document alone: a later document of the stream is a
			// declaration of its own. The cut takes the header's last newline,
			// which a version on its last line is matched by, so it goes back.
			header, rest, split := strings.Cut(doc, "\n---")
			header += "\n"
			if !reHeaderStability.MatchString(header) {
				t.Fatalf("%s no longer states a stability; retune this fixture", pkg)
			}
			header = lowerDeclaredVersion(t, reHeaderStability.ReplaceAllString(header, "\n"))
			if split {
				header += "---" + rest
			}
			return header
		})
	}
	patchShipped(t, coreKind(tree, "package.yaml"), func(doc string) string {
		from := strings.Index(doc, "\n    stability:\n")
		to := strings.Index(doc, "\n    retired:\n")
		if from < 0 || to < from {
			t.Fatal("the package kind no longer declares stability ahead of retired; retune this fixture")
		}
		return lowerDeclaredVersion(t, doc[:from]+doc[to:])
	})
	return tree
}

// A repository created before the key holds package rows with no label and a
// `package` kind that declares none. Its next open under this binary is the
// boot upgrade, which has to land the property and the labels in one pass:
// a header row carrying the key is admitted against the `package` kind the
// same pass writes, never the stored one.
func TestBootUpgradeWritesStabilityOntoAnExistingRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	const repository = "predates.example.com"

	before := openTree(t, dsn, unlabeledTree(t))
	if _, err := before.CreateRepository(ctx, repository); err != nil {
		t.Fatalf("create the repository: %v", err)
	}
	ds, err := before.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range kinds.SeedPackages {
		if label, has := storedStability(t, ds, pkg); has {
			t.Fatalf("%s already stores stability %q before the upgrade; the fixture is not the older tree", pkg, label)
		}
	}
	_ = before.Close()

	tree := shippedTree(t)
	after := openTree(t, dsn, tree)
	ds, err = after.Dataset(ctx, repository)
	if err != nil {
		t.Fatalf("open under the tree that states the labels: %v", err)
	}
	wantSeededStabilities(t, ds, "after the boot upgrade")
	upgraded := maxSeq(t, ds)
	_ = after.Close()

	again := openTree(t, dsn, tree)
	t.Cleanup(func() { _ = again.Close() })
	ds, err = again.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if got := maxSeq(t, ds); got != upgraded {
		t.Fatalf("a second open on the same tree appended %d entries", got-upgraded)
	}
	wantSeededStabilities(t, ds, "after a second open")
}

// stabilityHeader renders a package header stating the given label; the empty
// label renders the plain header.
func stabilityHeader(pkg, label string) map[string]any {
	doc := vocabulary.PackageManifest(pkg, 0)
	if label != "" {
		doc["data"].(map[string]any)["stability"] = label
	}
	return doc
}

func TestAPackageRecordCarriesTheStabilityItsHeaderStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newCoreDataset(t)
	pkg := testdb.Repository(t) + "/shelf"
	label := map[string]any{"label": map[string]any{"type": "string"}}
	book := vocabulary.KindManifest(pkg, map[string]any{"singular": "book"}, map[string]any{"properties": label})
	apply := func(docs ...map[string]any) error {
		_, err := ds.ApplyVocabularyDocuments(ctx, owner, docs)
		return err
	}
	version := func() int64 {
		v, _ := vocabulary.VersionValue(mustGet(t, ds, packageKindRef, pkg).Properties["version"])
		return v
	}

	// A header that states nothing stores nothing: the row never claims a
	// promise its author did not make.
	if err := apply(stabilityHeader(pkg, ""), book); err != nil {
		t.Fatalf("apply the package with no stability: %v", err)
	}
	if got, has := storedStability(t, ds, pkg); has {
		t.Fatalf("a header that states none stored stability %q", got)
	}
	unstated := version()

	// The label is the header's own content, so stating one moves the header.
	if err := apply(stabilityHeader(pkg, substrate.StabilityBeta)); err != nil {
		t.Fatalf("state a stability: %v", err)
	}
	if got, _ := storedStability(t, ds, pkg); got != substrate.StabilityBeta {
		t.Fatalf("stored stability = %q, want %q", got, substrate.StabilityBeta)
	}
	stated := version()
	if stated <= unstated {
		t.Fatalf("stating a stability left the package at version %d (was %d)", stated, unstated)
	}
	if err := apply(stabilityHeader(pkg, substrate.StabilityBeta)); err != nil {
		t.Fatalf("re-apply the same header: %v", err)
	}
	if got := version(); got != stated {
		t.Fatalf("re-applying the same label moved the package version %d -> %d", stated, got)
	}

	// A label outside the closed set is refused at load, and the row keeps
	// what it had.
	err := apply(stabilityHeader(pkg, "alpha"))
	if err == nil || !strings.Contains(err.Error(), `"alpha" is not a stability`) {
		t.Fatalf("an unknown label must refuse, naming it: %v", err)
	}
	if got, _ := storedStability(t, ds, pkg); got != substrate.StabilityBeta {
		t.Fatalf("a refused label changed the stored one to %q", got)
	}

	// A change elsewhere in the package travels without its header: the engine
	// brings the stored header in at the next version, label included.
	pen := vocabulary.KindManifest(pkg, map[string]any{"singular": "pen"}, map[string]any{"properties": label})
	if err := apply(pen); err != nil {
		t.Fatalf("add a kind without the header: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", pkg+"/pen", substrate.DeleteInput{}); err != nil {
		t.Fatalf("drop the kind again: %v", err)
	}
	if got, _ := storedStability(t, ds, pkg); got != substrate.StabilityBeta {
		t.Fatalf("a change without the header left stability %q, want %q", got, substrate.StabilityBeta)
	}

	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got, _ := storedStability(t, ds, pkg); got != substrate.StabilityBeta {
		t.Fatalf("after the rebuild: stored stability = %q, want %q", got, substrate.StabilityBeta)
	}

	// Withdrawing the label is writing the header without it: the put merges,
	// so the projection has to clear the key rather than leave the old promise
	// standing on the row.
	if err := apply(stabilityHeader(pkg, "")); err != nil {
		t.Fatalf("withdraw the stability: %v", err)
	}
	if got, has := storedStability(t, ds, pkg); has {
		t.Fatalf("a header without the key left stability %q on the row", got)
	}
}

// The two catalog doors ride InstallBundleClosure: a published closure lands
// under its publisher's authority, a sample's copy under the repository's own
// with its provenance stamped. Each stores the label its header states, and
// the copy still reads as unmodified, because the digest it is compared by is
// taken over the rows as stored, label included.
func TestAnInstalledClosureKeepsItsPackagesStability(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	closure := func(pkg, stability string) []map[string]any {
		return []map[string]any{
			stabilityHeader(pkg, stability),
			vocabulary.BundleManifest(pkg, map[string]any{
				"description": "one kind", "installs": []any{pkg + "/gizmo"},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "gizmo"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		}
	}

	const published = "acme.example.com/mirror"
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(published)),
		closure(published, substrate.StabilityStable), nil, substrate.BundleInstall{Published: true}); err != nil {
		t.Fatalf("install the published closure: %v", err)
	}
	if got, _ := storedStability(t, ds, published); got != substrate.StabilityStable {
		t.Errorf("the published package stores stability %q, want %q", got, substrate.StabilityStable)
	}

	copied := testdb.Repository(t) + "/gizmo"
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(copied)),
		closure(copied, substrate.StabilityBeta), nil,
		substrate.BundleInstall{Origin: "samples.example.com/gizmo", OriginVersion: 3}); err != nil {
		t.Fatalf("install the copy: %v", err)
	}
	if got, _ := storedStability(t, ds, copied); got != substrate.StabilityBeta {
		t.Errorf("the copy stores stability %q, want %q", got, substrate.StabilityBeta)
	}
	ops, ok := ds.(interface {
		BundleStatus(context.Context, string) (substrate.BundleStatus, error)
	})
	if !ok {
		t.Fatal("dataset computes no bundle status")
	}
	st, err := ops.BundleStatus(ctx, copied)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.Modified {
		t.Error("a copy whose header states a stability reads as modified the moment it lands")
	}
}
