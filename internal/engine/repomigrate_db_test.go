package engine_test

// REPOSITORY MIGRATIONS against a real store (decision record 0099): the
// first one rewrites stored bare names, the ledger records it, a second open
// is silent, a rebuild reproduces it, and a divergent ledger refuses the open.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	rmPackage = "ada.example.com/shapes"
	rmAlpha   = rmPackage + "/alpha"
	rmBeta    = rmPackage + "/beta"
	rmRanked  = rmPackage + "/ranked"
	rmKind    = "substrate.reamde.dev/core/kind"
	rmSplit   = "substrate.reamde.dev/core/recordsplit"
	rmMerge   = "substrate.reamde.dev/core/recordmerge"
)

// rmOpen opens the engine over one database and one data root, so a reopen
// meets the same repository directory the first open wrote.
func rmOpen(t *testing.T, dsn, root string) substrate.Service {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(root), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(engine.SeedKindsDir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return svc
}

func rmLedger(t *testing.T, db *sql.DB) map[int]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT version, name FROM repository_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]string{}
	for rows.Next() {
		var v int
		var name string
		if err := rows.Scan(&v, &name); err != nil {
			t.Fatal(err)
		}
		out[v] = name
	}
	return out
}

// rmClearLedger empties a repository's ledger through the maint pool: the app
// role may only read and append it, which is the point of the grant.
func rmClearLedger(t *testing.T, dsn, repo string) {
	t.Helper()
	maint, err := engine.OpenScopedDB(dsn, repo, engine.RoleMaint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = maint.Close() }()
	if _, err := maint.ExecContext(context.Background(), `DELETE FROM repository_migrations WHERE repository = $1`, repo); err != nil {
		t.Fatalf("clear the ledger: %v", err)
	}
}

// rmDeclaration is one stored kind's declaration properties, as a put would
// carry them back.
func rmDeclaration(t *testing.T, ds substrate.Dataset, id string) *substrate.Record {
	t.Helper()
	for _, r := range declarationRows(t, ds)[rmKind] {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no stored kind row %s", id)
	return nil
}

func TestRepositoryMigrationQualifiesStoredBareNames(t *testing.T) {
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	root := t.TempDir()
	repo := testdb.Repository(t)

	svc := rmOpen(t, dsn, root)
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	// A package of the repository's own, spelled in full as admission
	// demands: a trait, a kind binding it, and a kind pinning the other at
	// three depths and binding core's temporal.
	docs := []map[string]any{
		vocabulary.PackageManifest(rmPackage, 1),
		{
			"kind":     "substrate.reamde.dev/core/trait",
			"metadata": map[string]any{"id": rmRanked},
			"data":     map[string]any{"authority": "ada.example.com", "package": "shapes", "properties": map[string]any{"score": "int"}},
		},
		vocabulary.KindManifest(rmPackage, map[string]any{"singular": "alpha"}, map[string]any{
			"traits":     []any{rmRanked},
			"properties": map[string]any{"score": map[string]any{"type": "int"}},
		}),
		vocabulary.KindManifest(rmPackage, map[string]any{"singular": "beta"}, map[string]any{
			"traits": []any{"substrate.reamde.dev/core/temporal(point)"},
			"properties": map[string]any{
				"target": map[string]any{"type": "reference", "kind": rmAlpha},
				"held":   map[string]any{"type": "reference", "trait": rmRanked},
				"bag": map[string]any{"type": "object", "fields": map[string]any{
					"inner": map[string]any{"type": "reference", "kind": rmAlpha},
				}},
			},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply: %v", err)
	}
	betaBefore, err := ds.KindByRef(ctx, rmBeta)
	if err != nil {
		t.Fatal(err)
	}
	splitBefore, err := ds.KindByRef(ctx, rmSplit)
	if err != nil {
		t.Fatal(err)
	}

	// Plant the rows as the binary before decision 0098 stored them: bare
	// wherever the old shorthand allowed it, core's recordsplit included.
	bare := func(id string, edit func(props map[string]any)) {
		t.Helper()
		props := rmDeclaration(t, ds, id).Properties
		edit(props)
		if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, id, props); err != nil {
			t.Fatalf("plant %s: %v", id, err)
		}
	}
	bare(rmAlpha, func(p map[string]any) { p["traits"] = []any{"ranked"} })
	bare(rmBeta, func(p map[string]any) {
		p["traits"] = []any{"temporal(point)"}
		props := p["properties"].(map[string]any)
		props["target"].(map[string]any)["kind"] = "alpha"
		props["held"].(map[string]any)["trait"] = "ranked"
		props["bag"].(map[string]any)["fields"].(map[string]any)["inner"].(map[string]any)["kind"] = "alpha"
	})
	bare(rmSplit, func(p map[string]any) {
		p["properties"].(map[string]any)["merge"].(map[string]any)["kind"] = "recordmerge"
	})
	raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	// The first open already ran the migration over a repository with nothing
	// bare and recorded it, as every fresh repository's first open does. A
	// repository from before the migration existed has no such row, so the
	// ledger is cleared to stand for one.
	if got := rmLedger(t, raw); !reflect.DeepEqual(got, map[int]string{1: "qualify_bare_declaration_names", 2: "qualify_bare_selector_kinds"}) {
		t.Fatalf("a fresh repository's first open recorded %v", got)
	}
	rmClearLedger(t, dsn, repo)
	planted := changelogHead(t, raw)
	_ = svc.Close()

	// The next open runs the migration before the stored vocabulary loads,
	// so the repository opens at all.
	svc2 := rmOpen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("a repository holding bare stored names did not open: %v", err)
	}
	if got := rmLedger(t, raw); !reflect.DeepEqual(got, map[int]string{1: "qualify_bare_declaration_names", 2: "qualify_bare_selector_kinds"}) {
		t.Fatalf("ledger = %v", got)
	}
	migrated := changelogHead(t, raw)
	if migrated <= planted {
		t.Fatalf("the migration appended nothing: head %d before, %d after", planted, migrated)
	}
	beta, err := ds2.KindByRef(ctx, rmBeta)
	if err != nil {
		t.Fatal(err)
	}
	props := beta.Definition["properties"].(map[string]any)
	if got := props["target"].(map[string]any)["kind"]; got != rmAlpha {
		t.Errorf("target.kind = %v, want %s", got, rmAlpha)
	}
	if got := props["held"].(map[string]any)["trait"]; got != rmRanked {
		t.Errorf("held.trait = %v, want %s", got, rmRanked)
	}
	if got := props["bag"].(map[string]any)["fields"].(map[string]any)["inner"].(map[string]any)["kind"]; got != rmAlpha {
		t.Errorf("bag.inner.kind = %v, want %s", got, rmAlpha)
	}
	if got, _ := beta.Definition["traits"].([]any); len(got) != 1 || got[0] != "substrate.reamde.dev/core/temporal(point)" {
		t.Errorf("beta traits = %v", beta.Definition["traits"])
	}
	alpha, err := ds2.KindByRef(ctx, rmAlpha)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := alpha.Definition["traits"].([]any); len(got) != 1 || got[0] != rmRanked {
		t.Errorf("alpha traits = %v", alpha.Definition["traits"])
	}
	split, err := ds2.KindByRef(ctx, rmSplit)
	if err != nil {
		t.Fatal(err)
	}
	if got := split.Definition["properties"].(map[string]any)["merge"].(map[string]any)["kind"]; got != rmMerge {
		t.Errorf("recordsplit.merge.kind = %v, want %s", got, rmMerge)
	}
	// Every row keeps the version the store gave it: a rewrite of a spelling
	// is not an edit the API made.
	if beta.Version != betaBefore.Version || split.Version != splitBefore.Version {
		t.Errorf("versions moved: beta %d -> %d, recordsplit %d -> %d", betaBefore.Version, beta.Version, splitBefore.Version, split.Version)
	}
	_ = svc2.Close()

	// A second open finds the ledger row and appends nothing.
	svc3 := rmOpen(t, dsn, root)
	if _, err := svc3.Dataset(ctx, repo); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if head := changelogHead(t, raw); head != migrated {
		t.Fatalf("the second open appended: head %d, was %d", head, migrated)
	}
	if got := rmLedger(t, raw); len(got) != 2 {
		t.Fatalf("ledger after the second open = %v", got)
	}
	// A rebuild replays the plant and then the migration's writes, and the
	// fold comes back qualified.
	if _, err := svc3.(engine.Operator).RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	ds3, err := svc3.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := ds3.KindByRef(ctx, rmBeta)
	if err != nil {
		t.Fatal(err)
	}
	if got := rebuilt.Definition["properties"].(map[string]any)["target"].(map[string]any)["kind"]; got != rmAlpha {
		t.Errorf("after the rebuild target.kind = %v", got)
	}
	_ = svc3.Close()
}

func TestRepositoryMigrationsRefuseADivergentLedger(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		rows  [][2]any
		newer bool
		says  string
	}{
		"a version this binary does not carry": {rows: [][2]any{{1, "qualify_bare_declaration_names"}, {99, "from_the_future"}}, newer: true, says: "99 (from_the_future)"},
		"a name that differs":                  {rows: [][2]any{{1, "something_else"}}, says: `recorded "something_else"`},
	} {
		t.Run(name, func(t *testing.T) {
			dsn := engine.MigratedDSN(t)
			root := t.TempDir()
			repo := testdb.Repository(t)
			svc := rmOpen(t, dsn, root)
			if _, err := svc.CreateRepository(ctx, repo); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			if _, err := svc.Dataset(ctx, repo); err != nil {
				t.Fatal(err)
			}
			raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = raw.Close() })
			rmClearLedger(t, dsn, repo)
			for _, r := range tc.rows {
				if _, err := raw.ExecContext(ctx, `INSERT INTO repository_migrations (version, name) VALUES ($1, $2)`, r[0], r[1]); err != nil {
					t.Fatalf("write the ledger: %v", err)
				}
			}
			_ = svc.Close()

			svc2 := rmOpen(t, dsn, root)
			t.Cleanup(func() { _ = svc2.Close() })
			_, err = svc2.Dataset(ctx, repo)
			if err == nil {
				t.Fatal("a repository with a divergent ledger opened")
			}
			if tc.newer != errors.Is(err, engine.ErrRepositoryMigrationsNewer) {
				t.Fatalf("ErrRepositoryMigrationsNewer = %v, want %v: %v", !tc.newer, tc.newer, err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal does not say %q: %v", tc.says, err)
			}
		})
	}
}

// THE IMPORT PATH. A repository directory from another installation is folded
// at boot under whatever of its closure this binary admits, and its ledger
// stays behind in the source database, so the migration runs at the imported
// repository's first open, over rows the source never migrated. The refs
// index and the search bands were derived during the import under a registry
// that could not admit the bare closure, so the migration re-derives them: the
// imported repository's projections equal the source's, and a search finds
// the record.
func TestRepositoryMigrationRunsOnAnImportedDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, _ := newDatasetWithDSN(t)
	repo := testdb.Repository(t)
	docs := []map[string]any{
		vocabulary.PackageManifest(rmPackage, 1),
		vocabulary.KindManifest(rmPackage, map[string]any{"singular": "alpha"}, map[string]any{
			"properties": map[string]any{"label": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(rmPackage, map[string]any{"singular": "beta"}, map[string]any{
			"properties": map[string]any{
				"target": map[string]any{"type": "reference", "kind": rmAlpha, "mustExist": true},
				"notes":  map[string]any{"type": "string", "fts": true},
			},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply: %v", err)
	}
	a1 := mustPut(t, ds, owner, substrate.PutInput{Kind: rmAlpha, Properties: map[string]any{"label": "the one pointed at"}})
	b1 := mustPut(t, ds, owner, substrate.PutInput{Kind: rmBeta, Properties: map[string]any{
		"target": vocabulary.RecordPath(rmAlpha, a1.ID), "notes": "orchard ledger reconciled",
	}})
	// The source installation stored its declarations bare, as the binary
	// before decision 0098 did, and never opened them again under this one.
	bare := func(id string, edit func(props map[string]any)) {
		t.Helper()
		props := rmDeclaration(t, ds, id).Properties
		edit(props)
		if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, id, props); err != nil {
			t.Fatalf("plant %s: %v", id, err)
		}
	}
	bare(rmBeta, func(p map[string]any) {
		p["properties"].(map[string]any)["target"].(map[string]any)["kind"] = "alpha"
	})
	bare(rmSplit, func(p map[string]any) {
		p["properties"].(map[string]any)["merge"].(map[string]any)["kind"] = "recordmerge"
	})
	sourceRefs, sourceFTS := rmProjections(t, foldOf(t, ds))
	if len(sourceRefs) == 0 || sourceFTS[rmBeta+"/"+b1.ID] == "" {
		t.Fatalf("the source holds no refs or no bands for beta: refs %d, fts %q", len(sourceRefs), sourceFTS[rmBeta+"/"+b1.ID])
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The copy, under a database that has never seen it.
	root2 := copyRepositoryDir(t, root, id)
	dsn2 := engine.MigratedDSN(t)
	svc2 := mustReopen(t, dsn2, root2)
	raw2, err := engine.OpenScopedDB(dsn2, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw2.Close() })
	if got := rmLedger(t, raw2); len(got) != 0 {
		t.Fatalf("the boot import wrote a ledger before the repository opened: %v", got)
	}
	ds2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("the imported repository did not open: %v", err)
	}
	if got := rmLedger(t, raw2); !reflect.DeepEqual(got, map[int]string{1: "qualify_bare_declaration_names", 2: "qualify_bare_selector_kinds"}) {
		t.Fatalf("ledger after the first open = %v", got)
	}
	beta, err := ds2.KindByRef(ctx, rmBeta)
	if err != nil {
		t.Fatal(err)
	}
	if got := beta.Definition["properties"].(map[string]any)["target"].(map[string]any)["kind"]; got != rmAlpha {
		t.Errorf("target.kind = %v, want %s", got, rmAlpha)
	}
	split, err := ds2.KindByRef(ctx, rmSplit)
	if err != nil {
		t.Fatal(err)
	}
	if got := split.Definition["properties"].(map[string]any)["merge"].(map[string]any)["kind"]; got != rmMerge {
		t.Errorf("recordsplit.merge.kind = %v, want %s", got, rmMerge)
	}
	// The projections the import could not derive are the source's again.
	refs, fts := rmProjections(t, foldOf(t, ds2))
	if !reflect.DeepEqual(refs, sourceRefs) {
		t.Errorf("refs after the import and the migration differ from the source:\n%v\nwant\n%v", refs, sourceRefs)
	}
	if !reflect.DeepEqual(fts, sourceFTS) {
		t.Errorf("search bands after the import and the migration differ from the source:\n%v\nwant\n%v", fts, sourceFTS)
	}
	hits, err := searchHits(ds2.Search(ctx, substrate.SearchInput{Q: "orchard ledger", Mode: substrate.SearchLexical}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Record.ID != b1.ID {
		t.Fatalf("search hits = %v, want %s", hitIDs(hits), b1.ID)
	}
	_ = svc2.Close()
}

// rmProjections reads the refs section of a fold snapshot whole, and the
// search bands of this test's two kinds keyed by record path: the two
// projections a declaration decides for a row.
func rmProjections(t *testing.T, snapshot []byte) ([]map[string]any, map[string]string) {
	t.Helper()
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(snapshot, &sections); err != nil {
		t.Fatalf("decode the fold snapshot: %v", err)
	}
	var refs []map[string]any
	if err := json.Unmarshal(sections["refs"], &refs); err != nil {
		t.Fatalf("decode refs: %v", err)
	}
	var rows []struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		FTS  string `json:"fts"`
	}
	if err := json.Unmarshal(sections["fts"], &rows); err != nil {
		t.Fatalf("decode fts: %v", err)
	}
	fts := map[string]string{}
	for _, r := range rows {
		if r.Kind == rmAlpha || r.Kind == rmBeta {
			fts[r.Kind+"/"+r.ID] = r.FTS
		}
	}
	return refs, fts
}

// THE SELECTOR MIGRATION (migration 2). A trigger source and a policy
// selector stored bare, as a binary before decision record 0101 admitted
// them, open spelled in full and appended through the changelog; a word no
// single kind carries stays as it is; a second open appends nothing; a
// rebuild reproduces the rewrite.
func TestRepositoryMigrationQualifiesStoredSelectorKinds(t *testing.T) {
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	root := t.TempDir()
	repo := testdb.Repository(t)

	svc := rmOpen(t, dsn, root)
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	// One kind to select, and a function for the trigger to call.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(rmPackage, 1),
		vocabulary.KindManifest(rmPackage, map[string]any{"singular": "alpha"}, map[string]any{
			"properties": map[string]any{"score": map[string]any{"type": "int"}},
		}),
		vocabulary.FunctionManifest(rmPackage, "note", map[string]any{
			"description": "notes a change",
			"runtime":     vocabulary.RuntimePython,
			"source":      "def main(input, host):\n    return {}\n",
		}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	trigger, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger", ID: "on-alpha",
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{rmAlpha}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", rmPackage+"/note"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	policy, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-alpha",
		Properties: map[string]any{
			"selector": map[string]any{"kinds": []any{rmAlpha, "*"}, "ops": []any{"put"}},
			"action":   "gate",
		},
	})
	if err != nil {
		t.Fatalf("put policy: %v", err)
	}

	// Plant both rows as the binary before 0101 stored them: the kind spelled
	// bare, and on the policy a second word no kind carries, which the old
	// resolution left alone too.
	plant := func(rec *substrate.Record, edit func(props map[string]any)) {
		t.Helper()
		props := rec.Properties
		edit(props)
		if err := planter(t, ds).PlantDeclarationRow(ctx, rec.Kind, rec.ID, props); err != nil {
			t.Fatalf("plant %s: %v", rec.ID, err)
		}
	}
	plant(trigger, func(p map[string]any) {
		p["source"].(map[string]any)["record"].(map[string]any)["kinds"] = []any{"alpha"}
	})
	plant(policy, func(p map[string]any) {
		p["selector"].(map[string]any)["kinds"] = []any{"alpha", "nosuch", "*"}
	})
	raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	rmClearLedger(t, dsn, repo)
	planted := changelogHead(t, raw)
	_ = svc.Close()

	svc2 := rmOpen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("a repository holding bare stored selectors did not open: %v", err)
	}
	if got := rmLedger(t, raw); got[2] != "qualify_bare_selector_kinds" {
		t.Fatalf("ledger = %v, want migration 2 recorded", got)
	}
	migrated := changelogHead(t, raw)
	if migrated <= planted {
		t.Fatalf("the migration appended nothing: head %d before, %d after", planted, migrated)
	}
	kindsOf := func(ds substrate.Dataset, kind, id string, path ...string) []any {
		t.Helper()
		rec, err := ds.Get(ctx, kind, id)
		if err != nil {
			t.Fatalf("get %s/%s: %v", kind, id, err)
		}
		var cur any = rec.Properties
		for _, key := range path {
			cur = cur.(map[string]any)[key]
		}
		list, _ := cur.([]any)
		return list
	}
	if got := kindsOf(ds2, trigger.Kind, trigger.ID, "source", "record", "kinds"); !reflect.DeepEqual(got, []any{rmAlpha}) {
		t.Errorf("trigger kinds = %v, want [%s]", got, rmAlpha)
	}
	if got := kindsOf(ds2, policy.Kind, policy.ID, "selector", "kinds"); !reflect.DeepEqual(got, []any{rmAlpha, "nosuch", "*"}) {
		t.Errorf("policy kinds = %v, want [%s nosuch *]", got, rmAlpha)
	}
	_ = svc2.Close()

	// A second open finds the ledger row and appends nothing.
	svc3 := rmOpen(t, dsn, root)
	if _, err := svc3.Dataset(ctx, repo); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if head := changelogHead(t, raw); head != migrated {
		t.Fatalf("the second open appended: head %d, was %d", head, migrated)
	}
	// A rebuild replays the plant and then the migration's writes.
	if _, err := svc3.(engine.Operator).RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	ds3, err := svc3.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := kindsOf(ds3, trigger.Kind, trigger.ID, "source", "record", "kinds"); !reflect.DeepEqual(got, []any{rmAlpha}) {
		t.Errorf("after the rebuild trigger kinds = %v", got)
	}
	_ = svc3.Close()
}
