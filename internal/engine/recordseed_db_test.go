package engine_test

// The record seed (decision record 0229): the records a seeded package ships
// beside its declarations land at registration and at every open after the
// shipped upgrade, and an owner's edit or deletion stands.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	collectionViewKind = "substrate.reamde.dev/console/collectionview"
	memoryDeclaration  = "substrate.reamde.dev/core/kind/substrate.reamde.dev/llm/memory"
	contactDeclaration = "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/contact"
)

// shippedViews is the views file the console package ships, read from the
// tree so a change to it does not break these tests.
func shippedViews(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(shippedTree(t), vocabulary.PackageConsole, "views.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// shippedViewIDs is the id of every view the console package ships.
func shippedViewIDs(t *testing.T) []string {
	t.Helper()
	_, recs, err := vocabulary.ParseShipped(shippedViews(t))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		ids = append(ids, rec.ID)
	}
	return ids
}

// writeViews replaces a tree's shipped views file.
func writeViews(t *testing.T, tree string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tree, vocabulary.PackageConsole, "views.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func viewVersions(t *testing.T, ds substrate.Dataset, ids ...string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, id := range ids {
		rec, err := ds.Get(context.Background(), collectionViewKind, id)
		if err != nil {
			t.Fatalf("read the %s view: %v", id, err)
		}
		out[id] = rec.Version
	}
	return out
}

// A fresh repository holds the shipped views as they ship, and the next open
// finds nothing to write: a seeded row covers its shipped record, references
// included.
func TestRecordSeedWritesTheShippedViewsAtRegistration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	name := testdb.Repository(t)
	tree := shippedTree(t)
	svc := openTree(t, dsn, tree, engine.WithRecordSeed(true))
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	active, err := ds.Get(ctx, collectionViewKind, "memory-active")
	if err != nil {
		t.Fatalf("read the shipped Active view: %v", err)
	}
	want := map[string]any{
		"appliesTo": map[string]any{"ref": memoryDeclaration},
		"name":      "Active",
		"where":     map[string]any{"active": map[string]any{"eq": true}},
		"default":   true,
	}
	for k, v := range want {
		if !reflect.DeepEqual(active.Properties[k], v) {
			t.Errorf("the Active view's %s is %v, want %v", k, active.Properties[k], v)
		}
	}
	people, err := ds.Get(ctx, collectionViewKind, "contact-1-people")
	if err != nil {
		t.Fatalf("read the shipped People view: %v", err)
	}
	if got := people.Properties["appliesTo"]; !reflect.DeepEqual(got, map[string]any{"ref": contactDeclaration}) {
		t.Errorf("the People view applies to %v, want %s", got, contactDeclaration)
	}
	ids := shippedViewIDs(t)
	before := viewVersions(t, ds, ids...)
	_ = svc.Close()

	svc = openTree(t, dsn, tree, engine.WithRecordSeed(true))
	defer func() { _ = svc.Close() }()
	ds, err = svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if after := viewVersions(t, ds, ids...); !reflect.DeepEqual(after, before) {
		t.Fatalf("the open after registration wrote the views again: versions %v, then %v", before, after)
	}
}

// A repository created by a binary whose console package had no
// collectionview kind receives the kind through the shipped upgrade and the
// views through the seed right after it. The binary that did not declare the
// kind skipped the records it could not write and still created the
// repository.
func TestRecordSeedCatchesUpARepositoryCreatedWithoutTheKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	name := testdb.Repository(t)
	older := treeAtConsole(t, console3)
	writeViews(t, older, shippedViews(t))
	svc := openTree(t, dsn, older, engine.WithRecordSeed(true))
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create on a binary without the kind: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, collectionViewKind, "memory-active"); err == nil {
		t.Fatal("a binary without the collectionview kind wrote a view")
	}
	_ = svc.Close()

	svc = openTree(t, dsn, shippedTree(t), engine.WithRecordSeed(true))
	defer func() { _ = svc.Close() }()
	ds, err = svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range shippedViewIDs(t) {
		rec, err := ds.Get(ctx, collectionViewKind, id)
		if err != nil {
			t.Fatalf("the open after the upgrade did not seed %s: %v", id, err)
		}
		if rec.DeletedAt != nil {
			t.Fatalf("%s is deleted", id)
		}
	}
}

// A newer binary's views refresh a view only the seed wrote and add a new
// one, and leave the view the owner edited as the owner left it. A view the
// owner deleted stays deleted at every later open.
func TestRecordSeedRefreshesOnlyWhatNoOtherHandWrote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	name := testdb.Repository(t)
	svc := openTree(t, dsn, shippedTree(t), engine.WithRecordSeed(true))
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, collectionViewKind, "memory-active", substrate.PatchInput{
		Properties: map[string]any{"name": "Current"},
	}); err != nil {
		t.Fatalf("the owner renames the Active view: %v", err)
	}
	_ = svc.Close()

	// --- the newer binary: both shipped views renamed, and a third view.
	newer := shippedTree(t)
	views := string(shippedViews(t))
	views = strings.Replace(views, "name: Active", "name: Now", 1)
	views = strings.Replace(views, "name: Retired", "name: Taken back", 1)
	views += `---
kind: substrate.reamde.dev/console/collectionview
metadata:
  id: memory-agent
data:
  properties:
    appliesTo:
      ref: ` + memoryDeclaration + `
    name: From agents
    where:
      source:
        eq: agent
`
	writeViews(t, newer, []byte(views))
	svc = openTree(t, dsn, newer, engine.WithRecordSeed(true))
	ds, err = svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	names := func() map[string]string {
		out := map[string]string{}
		for _, id := range []string{"memory-active", "memory-retired", "memory-agent"} {
			rec, err := ds.Get(ctx, collectionViewKind, id)
			if err != nil {
				t.Fatalf("read %s: %v", id, err)
			}
			out[id], _ = rec.Properties["name"].(string)
			if rec.DeletedAt != nil {
				out[id] = "(deleted)"
			}
		}
		return out
	}
	want := map[string]string{"memory-active": "Current", "memory-retired": "Taken back", "memory-agent": "From agents"}
	if got := names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the newer binary's open the views are named %v, want %v", got, want)
	}

	if _, err := ds.Delete(ctx, substrate.ActorPerson, collectionViewKind, "memory-retired", substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner deletes the Retired view: %v", err)
	}
	_ = svc.Close()
	svc = openTree(t, dsn, newer, engine.WithRecordSeed(true))
	defer func() { _ = svc.Close() }()
	ds, err = svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	want["memory-retired"] = "(deleted)"
	if got := names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the owner's deletion and a reopen the views are named %v, want %v", got, want)
	}
}

// A shipped view's where is any JSON to the engine: the console checks it.
// The declared shape still holds the rest, so a view naming no kind is
// refused.
func TestCollectionViewNeedsItsKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	name := testdb.Repository(t)
	if _, err := svc.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds, err := svc.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: collectionViewKind, ID: "nameless",
		Properties: map[string]any{"name": "Open", "where": map[string]any{"active": map[string]any{"eq": true}}},
	})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a view naming no kind was admitted: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: collectionViewKind, ID: "sorted",
		Properties: map[string]any{"appliesTo": memoryDeclaration, "name": "By text", "sort": "text"},
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a sort with no direction was admitted: %v", err)
	}
}
