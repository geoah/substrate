package engine_test

// A NAME UNDER WHICH A REPOSITORY HOLDS A PACKAGE THAT IS NOT ITS OWN DOES NOT
// SPELL `~` THERE (repositorynames.go). Each door that gives a repository a
// name refuses, early, one it holds a published package under, and an
// install from a former name in force is refused until the name lapses. With
// those refusals lifted, the state they keep away is held by the spellings:
// a publisher's names keep their meaning.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// publishedClosure is a package publisher publishes, as an install sends it:
// one kind, `task`, titled by its name.
func publishedClosure(publisher, pkg string) []map[string]any {
	full := publisher + "/" + pkg
	return []map[string]any{
		vocabulary.PackageManifest(full, 1),
		vocabulary.KindManifest(full, map[string]any{"singular": "task"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.BundleManifest(full, map[string]any{"description": "a package " + publisher + " publishes", "installs": []any{full + "/task"}}),
	}
}

func installPublished(ctx context.Context, ds substrate.Dataset, publisher, pkg string) error {
	_, err := ds.InstallBundleClosure(ctx, substrate.ActorPerson, publishedClosure(publisher, pkg), nil, substrate.BundleInstall{Published: true})
	return err
}

// holdsPackage reports whether ds holds the live package id.
func holdsPackage(t *testing.T, ds substrate.Dataset, id string) bool {
	t.Helper()
	_, err := ds.Get(context.Background(), vocabulary.CoreKind(vocabulary.DocPackage), id)
	if err != nil && !errors.Is(err, substrate.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestARenameOntoAPublisherOfAPackageItHoldsIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var clock atomic.Pointer[time.Time]
	at := func(when time.Time) { clock.Store(&when) }
	start := time.Now().UTC().Truncate(time.Second)
	at(start)
	svc, _ := newService(t, engine.WithTestNamesClock(func() time.Time { return *clock.Load() }))
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	ada, pub := name("ada"), name("pub")
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: ada, Repository: ada, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	login := func(repository string) substrate.LoginInput {
		return substrate.LoginInput{Identity: ada, Repository: repository, Password: testPassword}
	}
	if err := installPublished(ctx, ds, pub, "tasks"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.RenameRepository(ctx, login(ada), pub)
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), pub+"/tasks") {
		t.Fatalf("a rename onto the publisher of a package the repository holds: %v, want it refused naming the package", err)
	}
	if _, err := svc.Dataset(ctx, pub); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("%s resolves after a refused rename: %v", pub, err)
	}
	if !holdsPackage(t, ds, pub+"/tasks") {
		t.Fatal("the package went with the refused rename")
	}
	// Uninstalled, it is no longer held, and the name is the repository's
	// to take: the publisher's authority row the uninstall leaves changes no
	// name's meaning.
	if err := ds.UninstallBundle(ctx, pub+"/tasks"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameRepository(ctx, login(ada), pub); err != nil {
		t.Fatalf("the rename once the package is uninstalled: %v", err)
	}

	// Its former name answers for 90 days: a package that name publishes
	// is refused until then, saying until when.
	err = installPublished(ctx, ds, ada, "notes")
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "until "+start.Add(90*24*time.Hour).Format(time.RFC3339)) {
		t.Fatalf("an install from a former name in force: %v, want it refused saying until when", err)
	}
	if holdsPackage(t, ds, ada+"/notes") || holdsPackage(t, ds, "~/notes") {
		t.Fatal("a refused install landed")
	}
	at(start.Add(90 * 24 * time.Hour))
	if err := installPublished(ctx, ds, ada, "notes"); err != nil {
		t.Fatalf("an install from a former name that lapsed: %v", err)
	}
	if !holdsPackage(t, ds, ada+"/notes") {
		t.Fatal("the install from a lapsed former name did not land under its publisher")
	}
}

// An import under another name takes no name its archive holds a package
// under.
func TestAnImportUnderAPublisherOfAPackageItsArchiveHoldsIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	stranger, pub := name("stranger"), name("pub")
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: stranger, Repository: stranger, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, stranger)
	if err != nil {
		t.Fatal(err)
	}
	if err := installPublished(ctx, ds, pub, "tasks"); err != nil {
		t.Fatal(err)
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	root := engine.DataRootOf(svc)
	saved := t.TempDir()
	copyDir(t, root+"/archive", saved)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{Identity: stranger, Repository: stranger, Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	copyDir(t, saved, root+"/archive")
	listed, err := engine.ListArchiveIDs(ctx, svc, id)
	if err != nil || len(listed) == 0 {
		t.Fatalf("the stranger's archives: %v (%v)", listed, err)
	}
	op := svc.(engine.Operator)
	_, err = op.ImportArchive(ctx, id, listed[0], pub)
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), pub+"/tasks") {
		t.Fatalf("an import under the publisher of a package its archive holds: %v, want it refused naming the package", err)
	}
	if _, err := svc.Dataset(ctx, pub); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("%s resolves after a refused import: %v", pub, err)
	}
	if _, err := op.ImportArchive(ctx, id, listed[0], name("other")); err != nil {
		t.Fatalf("an import under a name the archive holds nothing under: %v", err)
	}
}

// THE STATE THE DOORS KEEP AWAY, FROM AN IMPORT. An archive holding a
// publisher's package, imported under the publisher's name with the early
// refusal lifted: the name does not spell `~` while the package is held, so
// its kind is the published one, a reference stored in full keeps pointing
// at it, the repository's own kinds answer under `~`, and the name spelling
// one of them names nothing.
func TestAnImportUnderAPublisherItHoldsKeepsThePublishersNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithTestNameDoorsOpen())
	label := testdb.RepositoryLabel(t)
	name := func(s string) string { return label + "-" + s + ".example.com" }
	stranger, pub := name("stranger"), name("pub")
	if _, err := svc.Register(ctx, substrate.RegisterInput{Identity: stranger, Repository: stranger, Password: testPassword, SkipTOTP: true}); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, stranger)
	if err != nil {
		t.Fatal(err)
	}
	if err := installPublished(ctx, ds, pub, "tasks"); err != nil {
		t.Fatal(err)
	}
	task := pub + "/tasks/task"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest("~/notes", 0),
		vocabulary.KindManifest("~/notes", map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"about": map[string]any{"type": "reference", "kind": task}},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	tid, nid := vocabulary.DerivedMintedID("held", "task"), vocabulary.DerivedMintedID("held", "note")
	mustPut(t, ds, owner, substrate.PutInput{Kind: task, ID: tid, Properties: map[string]any{"name": "published"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "~/notes/note", ID: nid, Properties: map[string]any{
		"about": map[string]any{"ref": task + "/" + tid},
	}})
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	root := engine.DataRootOf(svc)
	saved := t.TempDir()
	copyDir(t, root+"/archive", saved)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{Identity: stranger, Repository: stranger, Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	copyDir(t, saved, root+"/archive")
	listed, err := engine.ListArchiveIDs(ctx, svc, id)
	if err != nil || len(listed) == 0 {
		t.Fatalf("the stranger's archives: %v (%v)", listed, err)
	}
	if _, err := svc.(engine.Operator).ImportArchive(ctx, id, listed[0], pub); err != nil {
		t.Fatalf("the import under %s with its refusal lifted: %v", pub, err)
	}
	ds, err = svc.Dataset(ctx, pub)
	if err != nil {
		t.Fatal(err)
	}
	if info := ds.Repository(); info.Authority != pub || slices.Contains(info.LocalNames(), pub) {
		t.Fatalf("the imported repository is %s and spells ~ with %v, want %s, which spells nothing", info.Authority, info.LocalNames(), pub)
	}
	got, err := ds.Get(ctx, task, tid)
	if err != nil || got.Kind != task {
		t.Fatalf("%s/%s reads %+v, %v: want the published kind", task, tid, got, err)
	}
	note, err := ds.Get(ctx, "~/notes/note", nid)
	if err != nil {
		t.Fatalf("the repository's own note: %v", err)
	}
	if about, _ := note.Properties["about"].(map[string]any); about["ref"] != task+"/"+tid {
		t.Errorf("the note's reference reads %v, want %s/%s", note.Properties["about"], task, tid)
	}
	if _, err := ds.Get(ctx, pub+"/notes/note", nid); err == nil {
		t.Errorf("%s/notes/note folded to ~/notes/note under a name the repository holds a package under", pub)
	}
	put := mustPut(t, ds, owner, substrate.PutInput{Kind: task, ID: vocabulary.DerivedMintedID("held", "second"), Properties: map[string]any{"name": "written after"}})
	if put.Kind != task {
		t.Errorf("a write of %s landed as %s", task, put.Kind)
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{task}}})
	if err != nil || len(page.Records) != 2 || page.Records[0].Kind != task {
		t.Errorf("the list of %s: %+v, %v", task, page, err)
	}
}
