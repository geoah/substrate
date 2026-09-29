package engine_test

// A stored package the loader parks (it parses and no longer admits under the
// binary) keeps the indexes its rows were written with: the fold derives a
// parked row's `fts` and refs rows from the package's stored declaration, so
// a rebuild and a boot import reproduce the fold of a package nobody edited
// (issue 461). The parked kinds still refuse every read and write, and a
// package that does not parse at all is named in the fold snapshot as the
// one divergence it expects.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	pfPackage = "parkedfold.bundles.substrate.reamde.dev/notes"
	pfNote    = pfPackage + "/note"
	pfTask    = "samples.substrate.reamde.dev/tasks/task"
)

// pfDocs is a one-kind bundle closure whose kind indexes a text property (the
// band a declaration alone decides) and holds a single and a repeated
// reference, so a parked row has bands and refs rows the unknown-kind
// derivation would not give it.
func pfDocs() []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(pfPackage, 0),
		vocabulary.ActorManifest(pfPackage, vocabulary.PackageActor(pfPackage)),
		vocabulary.BundleManifest(pfPackage, map[string]any{
			"description": "a bundle whose package a later binary parks", "installs": []any{pfNote},
		}),
		vocabulary.KindManifest(pfPackage, map[string]any{"singular": "note"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"remark": map[string]any{"type": "text"},
				"about":  map[string]any{"type": "reference", "kind": pfTask},
				"links":  map[string]any{"type": "reference", "kind": pfNote, "repeated": true},
			}}),
	}
}

// pfOpener opens services over one database and one data root, the shape of
// a restart, with whatever seam the caller adds.
func pfOpener(t *testing.T, dsn, root string) func(opts ...engine.Option) substrate.Service {
	return func(opts ...engine.Option) substrate.Service {
		t.Helper()
		all := append([]engine.Option{
			engine.WithKindsDir(engine.SeedKindsDir),
			engine.WithDataRoot(root),
			engine.WithCredentialKey(engine.TestCredentialKey),
		}, opts...)
		svc, err := engine.OpenForTest(t, context.Background(), dsn, all...)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
}

// pfWriteNotes installs the closure and writes two live notes and a
// tombstoned one, each holding references, and returns the dataset.
func pfWriteNotes(t *testing.T, svc substrate.Service) substrate.Dataset {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(pfPackage)), pfDocs(), nil,
		substrate.BundleInstall{}); err != nil {
		t.Fatalf("install the closure: %v", err)
	}
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: pfTask, Properties: map[string]any{"name": "the errand"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: pfNote, ID: "first", Properties: map[string]any{
		"name": "quokka notes", "remark": "a remark about wombats",
		"about": pfTask + "/" + task.ID,
	}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: pfNote, ID: "second", Properties: map[string]any{
		"name": "second", "remark": "numbats", "links": []any{pfNote + "/first"},
	}})
	gone := mustPut(t, ds, owner, substrate.PutInput{Kind: pfNote, ID: "gone", Properties: map[string]any{
		"name": "gone", "remark": "a tombstone indexes too", "links": []any{pfNote + "/first"},
	}})
	if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	return ds
}

// pfIndexes reads the `fts` text of every note row and the number of refs rows
// the notes hold out of one fold snapshot.
func pfIndexes(t *testing.T, snap []byte) (fts map[string]string, refs int) {
	t.Helper()
	var s struct {
		FTS []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
			FTS  string `json:"fts"`
		} `json:"fts"`
		Refs []struct {
			SrcKind string `json:"src_kind"`
		} `json:"refs"`
	}
	if err := json.Unmarshal(snap, &s); err != nil {
		t.Fatalf("read the snapshot: %v", err)
	}
	fts = map[string]string{}
	for _, r := range s.FTS {
		if r.Kind == pfNote {
			fts[r.ID] = r.FTS
		}
	}
	for _, r := range s.Refs {
		if r.SrcKind == pfNote {
			refs++
		}
	}
	return fts, refs
}

// requireDeclaredIndexes fails unless the notes hold what only their
// declaration derives: the remark's words (the unknown-kind bands index the
// title and body alone) and the three refs rows.
func requireDeclaredIndexes(t *testing.T, when string, snap []byte) {
	t.Helper()
	fts, refs := pfIndexes(t, snap)
	if len(fts) != 3 {
		t.Fatalf("%s: the snapshot holds %d note rows, want 3", when, len(fts))
	}
	for id, word := range map[string]string{"first": "wombat", "second": "numbat", "gone": "tombston"} {
		if !strings.Contains(fts[id], word) {
			t.Fatalf("%s: note %s indexes %q, which lacks the remark's %q: its bands are not its declaration's", when, id, fts[id], word)
		}
	}
	if refs != 3 {
		t.Fatalf("%s: the notes hold %d refs rows, want 3", when, refs)
	}
}

// requireParkedKindRefused fails unless the parked kind is absent from the
// kinds and refused on a read, a list and a write.
func requireParkedKindRefused(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	kinds, err := ds.Kinds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if k.Identity == pfNote {
			t.Fatalf("the parked kind %s is listed among the kinds", pfNote)
		}
	}
	if _, err := ds.Get(ctx, pfNote, "first"); err == nil {
		t.Fatal("a parked kind's record must not be readable")
	}
	if _, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{pfNote}}}); err == nil {
		t.Fatal("a parked kind must not be listable")
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: pfNote, Properties: map[string]any{"name": "new"}}); err == nil {
		t.Fatal("a parked kind must not accept writes")
	}
}

// TestAParkedPackageRebuildsAndImportsToTheSameFold parks a package with live
// and tombstoned rows through the loader seam, as a binary whose contract
// tightened would, and holds the fold to what a rebuild and a boot import of
// the directory reproduce, `fts` and `refs` included.
func TestAParkedPackageRebuildsAndImportsToTheSameFold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	root := t.TempDir()
	open := pfOpener(t, dsn, root)

	svc := open()
	requireDeclaredIndexes(t, "while the package is live", foldOf(t, pfWriteNotes(t, svc)))
	_ = svc.Close()

	// The next binary refuses the package: the repository opens without it,
	// and its rows keep the indexes their declaration derived.
	svc2 := open(engine.WithTestInadmissible(pfPackage))
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open with the package parked: %v", err)
	}
	if st := bundleStatusFor(t, ds2, pfPackage); !st.Quarantined {
		t.Fatalf("the package should be parked: %+v", st)
	}
	requireParkedKindRefused(t, ds2)
	parked := foldOf(t, ds2)
	requireDeclaredIndexes(t, "parked", parked)

	if _, err := svc2.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := foldOf(t, ds2); string(rebuilt) != string(parked) {
		t.Fatalf("the rebuilt fold of a parked package is not the fold\n%s", firstDifference(parked, rebuilt))
	}
	requireParkedKindRefused(t, ds2)
	id := repositoryIDOf(t, ds2)
	_ = svc2.Close()

	// The boot import folds the directory into an empty database under the
	// same binary, so it parks the package too and must land on the same fold.
	root2 := copyRepositoryDir(t, root, id)
	svc3 := pfOpener(t, engine.MigratedDSN(t), root2)(engine.WithTestInadmissible(pfPackage))
	ds3, err := svc3.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if imported := foldOf(t, ds3); string(imported) != string(parked) {
		t.Fatalf("the imported fold of a parked package is not the fold\n%s", firstDifference(parked, imported))
	}
	requireParkedKindRefused(t, ds3)
}

// TestUninstallingAParkedPackageReindexesItsRows: a parked package's rows
// derive their indexes from its stored declaration only while it is stored.
// The uninstall removes the declaration and leaves the rows, so it re-derives
// them at the unknown-kind bands with no refs rows, which is what a rebuild
// after it computes.
func TestUninstallingAParkedPackageReindexesItsRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := pfOpener(t, dsn, t.TempDir())

	svc := open()
	pfWriteNotes(t, svc)
	_ = svc.Close()

	svc2 := open(engine.WithTestInadmissible(pfPackage))
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open with the package parked: %v", err)
	}
	if err := ds2.UninstallBundle(ctx, pfPackage); err != nil {
		t.Fatalf("uninstall the parked package: %v", err)
	}
	before := foldOf(t, ds2)
	fts, refs := pfIndexes(t, before)
	if len(fts) != 3 || strings.Contains(fts["first"], "wombat") || refs != 0 {
		t.Fatalf("after the uninstall the notes index %v with %d refs rows; want the unknown-kind bands and none", fts, refs)
	}
	if _, err := svc2.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the rebuilt fold after the uninstall is not the fold\n%s", firstDifference(before, after))
	}
}

const (
	pfSources = "parkedfold.example.substrate.reamde.dev/sources"
	pfSource  = pfSources + "/source"
	pfCard    = pfPackage + "/card"
)

// pfMappedDocs is a bundle closure whose mapping reaches into a package that
// stays live: a link-only mapping from pfSource onto the closure's own card,
// so the source kind carries the mapping's subject slot and no offer rows
// exist.
func pfMappedDocs() []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(pfPackage, 0),
		vocabulary.ActorManifest(pfPackage, vocabulary.PackageActor(pfPackage)),
		vocabulary.BundleManifest(pfPackage, map[string]any{
			"description": "a bundle mapping from a package that stays live", "installs": []any{pfCard, pfPackage + "/sourcecard"},
		}),
		vocabulary.KindManifest(pfPackage, map[string]any{"singular": "card"},
			map[string]any{"displayTemplate": "{label}", "properties": map[string]any{
				"label": map[string]any{"type": "string"},
			}}),
		vocabulary.MappingManifest(pfPackage, "sourcecard", map[string]any{
			"from": pfSource, "to": pfCard, "property": "card",
		}),
	}
}

// pfSlotRows counts the refs rows the source kind holds in one snapshot.
func pfSlotRows(t *testing.T, snap []byte) int {
	t.Helper()
	var s struct {
		Refs []struct {
			SrcKind  string `json:"src_kind"`
			Property string `json:"property"`
		} `json:"refs"`
	}
	if err := json.Unmarshal(snap, &s); err != nil {
		t.Fatalf("read the snapshot: %v", err)
	}
	n := 0
	for _, r := range s.Refs {
		if r.SrcKind == pfSource && r.Property == "card" {
			n++
		}
	}
	return n
}

// TestAParkedMappingKeepsTheSlotRowOfALiveSourceKind: a mapping lives with
// its target, so a parked package can map from a kind of a package that stays
// live. The subject slot leaves the live registry with the package, and the
// source rows keep the refs row it projected, through a rebuild too; the
// uninstall of the parked package drops the row, and a rebuild agrees.
func TestAParkedMappingKeepsTheSlotRowOfALiveSourceKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := pfOpener(t, dsn, t.TempDir())

	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pfSources, 0),
		vocabulary.KindManifest(pfSources, map[string]any{"singular": "source"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name": map[string]any{"type": "string"},
			}}),
	}); err != nil {
		t.Fatalf("declare the source package: %v", err)
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(pfPackage)), pfMappedDocs(), nil,
		substrate.BundleInstall{}); err != nil {
		t.Fatalf("install the mapping closure: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: pfSource, ID: "linked", Properties: map[string]any{"name": "a source"}})
	if n := pfSlotRows(t, foldOf(t, ds)); n != 1 {
		t.Fatalf("the source holds %d slot rows while the mapping is live, want 1", n)
	}
	_ = svc.Close()

	svc2 := open(engine.WithTestInadmissible(pfPackage))
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open with the package parked: %v", err)
	}
	rb := svc2.(rebuilder)
	parked := foldOf(t, ds2)
	if n := pfSlotRows(t, parked); n != 1 {
		t.Fatalf("the source holds %d slot rows with the mapping parked, want 1", n)
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := foldOf(t, ds2); string(rebuilt) != string(parked) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(parked, rebuilt))
	}

	if err := ds2.UninstallBundle(ctx, pfPackage); err != nil {
		t.Fatalf("uninstall the parked package: %v", err)
	}
	uninstalled := foldOf(t, ds2)
	if n := pfSlotRows(t, uninstalled); n != 0 {
		t.Fatalf("the source holds %d slot rows after its mapping was uninstalled, want 0", n)
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild after the uninstall: %v", err)
	}
	if rebuilt := foldOf(t, ds2); string(rebuilt) != string(uninstalled) {
		t.Fatalf("the rebuilt fold after the uninstall is not the fold\n%s", firstDifference(uninstalled, rebuilt))
	}
}

// TestTheSnapshotNamesAPackageThatDoesNotParse: a stored package that does
// not parse has no declaration to derive its rows' indexes under, so the fold
// snapshot names it as the divergence a rebuild may show, and stops naming it
// once a re-apply stores a declaration that parses.
func TestTheSnapshotNamesAPackageThatDoesNotParse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := pfOpener(t, dsn, t.TempDir())

	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, lqDocs()); err != nil {
		t.Fatalf("install the legacy bundle: %v", err)
	}
	if strings.Contains(string(foldOf(t, ds)), "unparsed_packages") {
		t.Fatal("a snapshot with nothing parked names an unparsed package")
	}
	// The pre-refactor agent shape, as TestUnparseableStoredAgentQuarantines
	// InsteadOfBricking fabricates it.
	if _, err := rawDB(t, dsn).ExecContext(ctx,
		`UPDATE records SET props = (props - 'provider' - 'model') || '{"llm": "cheap"}'::jsonb
		 WHERE kind = $1 AND id = $2`, kindAgentID, lqAgent); err != nil {
		t.Fatalf("fabricate the pre-refactor agent declaration: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var snap struct {
		Unparsed []string `json:"unparsed_packages"`
	}
	if err := json.Unmarshal(foldOf(t, ds2), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Unparsed) != 1 || snap.Unparsed[0] != lqPackage {
		t.Fatalf("the snapshot names %v as unparsed, want [%s]", snap.Unparsed, lqPackage)
	}
	if _, err := ds2.ApplyVocabularyDocuments(ctx, owner, lqDocs()); err != nil {
		t.Fatalf("re-apply the corrected manifest: %v", err)
	}
	if strings.Contains(string(foldOf(t, ds2)), "unparsed_packages") {
		t.Fatal("the snapshot still names the package after a re-apply stored a declaration that parses")
	}
}

// TestABootUpgradeReindexesASourceKindAParkedMappingReshapes: a parked
// mapping from a SEEDED kind keeps that kind's slot row, until a shipped
// upgrade declares an ordinary property under the slot's name. The parked
// mapping's slot then collides and is gone from the parked view, so the boot
// re-derives the source rows without it, which is what a rebuild after the
// upgrade folds.
func TestABootUpgradeReindexesASourceKindAParkedMappingReshapes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const provider = llmPackage + "/provider"
	dsn := engine.MigratedDSN(t)
	tree := shippedTree(t)
	open := pfOpener(t, dsn, t.TempDir())

	svc := open(engine.WithKindsDir(tree))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	docs := pfMappedDocs()
	docs[len(docs)-1] = vocabulary.MappingManifest(pfPackage, "sourcecard", map[string]any{
		"from": provider, "to": pfCard, "property": "card",
	})
	if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(pfPackage)), docs, nil,
		substrate.BundleInstall{}); err != nil {
		t.Fatalf("install the mapping closure: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: provider, ID: "hub", Properties: map[string]any{"label": "the hub", "wire": "openai"},
	})
	slotRows := func(snap []byte) int {
		t.Helper()
		var s struct {
			Refs []struct {
				SrcKind  string `json:"src_kind"`
				Property string `json:"property"`
			} `json:"refs"`
		}
		if err := json.Unmarshal(snap, &s); err != nil {
			t.Fatalf("read the snapshot: %v", err)
		}
		n := 0
		for _, r := range s.Refs {
			if r.SrcKind == provider && r.Property == "card" {
				n++
			}
		}
		return n
	}
	if n := slotRows(foldOf(t, ds)); n != 1 {
		t.Fatalf("the provider holds %d slot rows while the mapping is live, want 1", n)
	}
	_ = svc.Close()

	patchShipped(t, llmKind(tree, "provider.yaml"), func(doc string) string {
		doc = replaceShipped(t, doc, "  properties:\n", "  properties:\n    card:\n      type: string\n")
		return pinVersion(t, doc, "99")
	})
	svc2 := open(engine.WithKindsDir(tree), engine.WithTestInadmissible(pfPackage))
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the upgrade with the package parked: %v", err)
	}
	if _, declared := declaredProps(t, ds2, provider)["card"]; !declared {
		t.Fatal("the upgrade did not land: llm/provider does not declare `card`")
	}
	upgraded := foldOf(t, ds2)
	if n := slotRows(upgraded); n != 0 {
		t.Fatalf("the provider holds %d slot rows after the upgrade declared `card` a string, want 0", n)
	}
	if _, err := svc2.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := foldOf(t, ds2); string(rebuilt) != string(upgraded) {
		t.Fatalf("the rebuilt fold is not the upgraded fold\n%s", firstDifference(upgraded, rebuilt))
	}
}
