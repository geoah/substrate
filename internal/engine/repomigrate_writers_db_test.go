package engine_test

// Repository migration 10 (decision 0290): a stored declaration that still
// names a writer by a word from before actor types is rewritten at the first
// open under this binary, before the stored vocabulary loads, so its package
// loads rather than parks.

import (
	"context"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

// Each old word is rewritten by the fixed rule wherever it fills a `writer:`:
// a property's and a reference's, a property named `writer` included; a
// field's `writer:`, which never had an effect, is removed; and a default
// value that holds the word is left as it is. The rewrite goes through the
// changelog, keeps the row's version, runs once, and a rebuild reproduces it.
// It reaches a package parked for a reason of its own, which loads once that
// reason is repaired, and a tombstoned declaration, which stays tombstoned.
// An imported sample still matching its stamp is stamped again over the
// rewrite; one its owner edited still reads as edited.
func TestRepositoryMigrationNamesStoredWritersByActorType(t *testing.T) {
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	root := t.TempDir()
	repo := testdb.Repository(t)
	const (
		pkg     = "ada.example.com/writers"
		note    = pkg + "/note"
		gone    = pkg + "/gone"
		parked  = "ada.example.com/parked"
		item    = parked + "/item"
		missing = "ada.example.com/missing"
	)

	svc := rmOpen(t, dsn, root)
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{
				"cursor": map[string]any{"type": "string", "writer": "person"},
				"writer": map[string]any{"type": "string", "writer": "person"},
				"bag": map[string]any{"type": "object", "writer": []any{"function", "agent"}, "fields": map[string]any{
					"inner":  map[string]any{"type": "string"},
					"writer": map[string]any{"type": "string"},
				}},
				"shape":  map[string]any{"type": "json", "default": map[string]any{"writer": "owner"}},
				"target": map[string]any{"type": "reference", "kind": note, "writer": "system"},
			},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "gone"}, map[string]any{
			"properties": map[string]any{"cursor": map[string]any{"type": "string", "writer": "person"}},
		}),
		vocabulary.PackageManifest(parked, 1),
		vocabulary.KindManifest(parked, map[string]any{"singular": "item"}, map[string]any{
			"properties": map[string]any{
				"cursor": map[string]any{"type": "string", "writer": "person"},
				"link":   map[string]any{"type": "reference", "kind": note},
			},
		}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	before, err := ds.KindByRef(ctx, note)
	if err != nil {
		t.Fatal(err)
	}

	// A declaration tombstoned with the old word in it, first: the delete
	// validates the rest of the stored vocabulary.
	goneProps := rmDeclaration(t, ds, gone).Properties
	goneProps["properties"].(map[string]any)["cursor"].(map[string]any)["writer"] = "owner"
	if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, gone, goneProps); err != nil {
		t.Fatalf("plant the gone kind: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, rmKind, gone, substrate.DeleteInput{}); err != nil {
		t.Fatalf("tombstone the gone kind: %v", err)
	}

	// Plant the row as a binary before actor types stored it.
	props := rmDeclaration(t, ds, note).Properties
	decl := props["properties"].(map[string]any)
	decl["cursor"].(map[string]any)["writer"] = "owner"
	decl["writer"].(map[string]any)["writer"] = "owner"
	fields := decl["bag"].(map[string]any)["fields"].(map[string]any)
	fields["inner"].(map[string]any)["writer"] = "connector"
	fields["writer"].(map[string]any)["writer"] = "connector"
	decl["bag"].(map[string]any)["writer"] = "connector"
	decl["target"].(map[string]any)["writer"] = "oauth"
	if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, note, props); err != nil {
		t.Fatalf("plant: %v", err)
	}
	// A package that parks at the next open for a reason of its own: its
	// kind names one no package declares.
	itemProps := rmDeclaration(t, ds, item).Properties
	itemDecl := itemProps["properties"].(map[string]any)
	itemDecl["cursor"].(map[string]any)["writer"] = "owner"
	itemDecl["link"].(map[string]any)["kind"] = missing + "/thing"
	if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, item, itemProps); err != nil {
		t.Fatalf("plant the parked package: %v", err)
	}

	// Two imported samples whose copies say `owner`: notes as its import
	// stamped it, ink edited by its owner since.
	c, err := catalog.Load(catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatal(err)
	}
	sampleKind := map[string]string{}
	for _, word := range []string{"notes", "ink"} {
		if _, _, err := c.Import(ctx, substrate.ActorPerson, "samples.substrate.reamde.dev/"+word, ds); err != nil {
			t.Fatalf("import %s: %v", word, err)
		}
		// A sample lands in the local namespace (decision record 0259).
		pkg := vocabulary.Local + "/" + word
		for _, r := range declarationRows(t, ds)[rmKind] {
			if r.Properties["authority"] == vocabulary.Local && r.Properties["package"] == word {
				sampleKind[word] = r.ID
				break
			}
		}
		if sampleKind[word] == "" {
			t.Fatalf("the %s sample landed no kind", word)
		}
		row := rmDeclaration(t, ds, sampleKind[word]).Properties
		row["properties"].(map[string]any)["reviewer"] = map[string]any{"type": "string", "writer": "owner"}
		if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, sampleKind[word], row); err != nil {
			t.Fatalf("plant %s: %v", word, err)
		}
		if err := ds.(interface {
			StampOriginDigest(context.Context, string) error
		}).StampOriginDigest(ctx, pkg); err != nil {
			t.Fatalf("stamp %s: %v", pkg, err)
		}
	}
	edited := rmDeclaration(t, ds, sampleKind["ink"]).Properties
	edited["description"] = "Edited by its owner."
	if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, sampleKind["ink"], edited); err != nil {
		t.Fatalf("edit ink: %v", err)
	}
	modified := func(t *testing.T, ds substrate.Dataset) map[string]bool {
		t.Helper()
		out := map[string]bool{}
		for _, word := range []string{"notes", "ink"} {
			st, err := ds.BundleStatus(ctx, repo+"/"+word)
			if err != nil {
				t.Fatalf("status of %s: %v", word, err)
			}
			out[word] = st.Modified
		}
		return out
	}
	if got := modified(t, ds); !reflect.DeepEqual(got, map[string]bool{"notes": false, "ink": true}) {
		t.Fatalf("before the upgrade the copies read modified %v, want ink alone", got)
	}
	raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	// A repository from before the migration has no row of it, nor of any
	// after it.
	engine.MakeMigrationPending(t, dsn, repo, 10)
	planted := changelogHead(t, raw)
	_ = svc.Close()

	written := func(t *testing.T, def map[string]any) {
		t.Helper()
		got := def["properties"].(map[string]any)
		bag := got["bag"].(map[string]any)["fields"].(map[string]any)
		for name, c := range map[string]struct {
			at   any
			want any
		}{
			"cursor":        {got["cursor"].(map[string]any)["writer"], "person"},
			"writer":        {got["writer"].(map[string]any)["writer"], "person"},
			"bag":           {got["bag"].(map[string]any)["writer"], []any{"function", "agent"}},
			"bag.inner":     {bag["inner"].(map[string]any)["writer"], nil},
			"bag.writer":    {bag["writer"].(map[string]any)["writer"], nil},
			"target":        {got["target"].(map[string]any)["writer"], "system"},
			"shape.default": {got["shape"].(map[string]any)["default"], map[string]any{"writer": "owner"}},
		} {
			if !reflect.DeepEqual(c.at, c.want) {
				t.Errorf("%s writer = %#v, want %#v", name, c.at, c.want)
			}
		}
	}

	svc2 := rmOpen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("a repository holding the old writer words did not open: %v", err)
	}
	if got := rmLedger(t, raw)[10]; got != "name_writers_by_actor_type" {
		t.Fatalf("the ledger does not record migration 10: %v", rmLedger(t, raw))
	}
	migrated := changelogHead(t, raw)
	if migrated <= planted {
		t.Fatalf("the migration appended nothing: head %d before, %d after", planted, migrated)
	}
	after, err := ds2.KindByRef(ctx, note)
	if err != nil {
		t.Fatalf("the rewritten kind does not load: %v", err)
	}
	written(t, after.Definition)
	if after.Version != before.Version {
		t.Errorf("the rewrite moved the kind's version: %d -> %d", before.Version, after.Version)
	}
	for word, kind := range sampleKind {
		k, err := ds2.KindByRef(ctx, kind)
		if err != nil {
			t.Fatalf("the %s sample's kind does not load: %v", word, err)
		}
		if got := k.Definition["properties"].(map[string]any)["reviewer"].(map[string]any)["writer"]; got != "person" {
			t.Errorf("the %s sample's reviewer writer = %#v, want person", word, got)
		}
	}
	if got := modified(t, ds2); !reflect.DeepEqual(got, map[string]bool{"notes": false, "ink": true}) {
		t.Errorf("after the upgrade the copies read modified %v, want ink alone: the rewrite restamps a copy that matched its stamp and keeps an edited one edited", got)
	}
	// The parked package parks, for its own reason, with its writer
	// rewritten all the same.
	if _, err := ds2.KindByRef(ctx, item); err == nil {
		t.Fatal("the package whose kind names an undeclared one loaded")
	}
	if got := rmDeclaration(t, ds2, item).Properties["properties"].(map[string]any)["cursor"].(map[string]any)["writer"]; got != "person" {
		t.Errorf("the parked package's stored writer = %#v, want person", got)
	}
	// The tombstoned declaration stays tombstoned, rewritten.
	var goneWriter string
	var goneDeleted bool
	if err := raw.QueryRowContext(ctx, `SELECT props->'properties'->'cursor'->>'writer', deleted_at IS NOT NULL FROM records WHERE kind = $1 AND id = $2`, rmKind, gone).Scan(&goneWriter, &goneDeleted); err != nil {
		t.Fatal(err)
	}
	if goneWriter != "person" || !goneDeleted {
		t.Errorf("the tombstoned declaration reads writer %q, deleted %v, want person and still deleted", goneWriter, goneDeleted)
	}
	_ = svc2.Close()

	// A second open finds the ledger row and appends nothing.
	svc3 := rmOpen(t, dsn, root)
	t.Cleanup(func() { _ = svc3.Close() })
	if _, err := svc3.Dataset(ctx, repo); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if head := changelogHead(t, raw); head != migrated {
		t.Fatalf("the second open appended: head %d, was %d", head, migrated)
	}
	// A rebuild replays the plant and the migration's writes.
	if _, err := svc3.(engine.Operator).RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	ds3, err := svc3.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := ds3.KindByRef(ctx, note)
	if err != nil {
		t.Fatalf("the rebuilt kind does not load: %v", err)
	}
	written(t, rebuilt.Definition)

	// The parked package's own reason repaired: the kind it names declared,
	// it loads at the next open with nothing else in its way.
	if _, err := ds3.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(missing, 1),
		vocabulary.KindManifest(missing, map[string]any{"singular": "thing"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare the missing package: %v", err)
	}
	_ = svc3.Close()
	svc4 := rmOpen(t, dsn, root)
	t.Cleanup(func() { _ = svc4.Close() })
	ds4, err := svc4.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := ds4.KindByRef(ctx, item)
	if err != nil {
		t.Fatalf("the repaired package does not load: %v", err)
	}
	if got := repaired.Definition["properties"].(map[string]any)["cursor"].(map[string]any)["writer"]; got != "person" {
		t.Errorf("the repaired package's writer = %#v, want person", got)
	}
}
