package engine_test

// DIALECT 2, the typed declarations: a repository whose declaration rows are the
// old `definition` blob opens under this binary with every row translated, once,
// atomically with the stamp — and a rebuild replays the translation instead of
// losing it.

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// declarationPlanter is the internal-write seam a dialect-1 store is stood up
// through (vocabularyplant_internal_test.go).
type declarationPlanter interface {
	PlantDeclarationRow(ctx context.Context, kindIdent, id string, props map[string]any) error
	DialectOneProps(kindIdent, id string, props map[string]any) map[string]any
}

func planter(t *testing.T, ds substrate.Dataset) declarationPlanter {
	t.Helper()
	p, ok := ds.(declarationPlanter)
	if !ok {
		t.Fatal("the dataset does not expose the declaration-planting seam")
	}
	return p
}

// declarationRows lists every declaration row a repository holds, by kind.
func declarationRows(t *testing.T, ds substrate.Dataset) map[string][]*substrate.Record {
	t.Helper()
	out := map[string][]*substrate.Record{}
	for _, kind := range []string{
		"core.substrate.reamde.dev/authority", "core.substrate.reamde.dev/actor",
		"core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/trait",
		"core.substrate.reamde.dev/propertytype", "core.substrate.reamde.dev/recordmapping",
		"core.substrate.reamde.dev/function", "core.substrate.reamde.dev/agent",
		"core.substrate.reamde.dev/bundle",
	} {
		page, err := ds.List(context.Background(), substrate.Query{
			Filter: substrate.Filter{Kinds: []string{kind}}, First: 500,
		})
		if err != nil {
			t.Fatalf("list %s: %v", kind, err)
		}
		if len(page.Records) == 0 {
			continue // this repository holds none of that kind
		}
		out[kind] = page.Records
	}
	return out
}

// plantDialectOne rewrites every declaration row into its dialect-1 shape and
// winds the stamp back, so the next open re-runs the ladder. It returns the
// number of rows planted.
func plantDialectOne(t *testing.T, ds substrate.Dataset, db *sql.DB, only func(kind, id string) bool) int {
	t.Helper()
	p := planter(t, ds)
	planted := 0
	for kind, rows := range declarationRows(t, ds) {
		for _, row := range rows {
			if only != nil && !only(kind, row.ID) {
				continue
			}
			old := p.DialectOneProps(kind, row.ID, row.Properties)
			if err := p.PlantDeclarationRow(context.Background(), kind, row.ID, old); err != nil {
				t.Fatalf("plant %s %s: %v", kind, row.ID, err)
			}
			planted++
		}
	}
	windBackDialect(t, db)
	return planted
}

// storedDialect reads the repository's stamped dialect.
func storedDialect(t *testing.T, db *sql.DB) int {
	t.Helper()
	var d int
	if err := db.QueryRow(`SELECT dialect FROM vocabulary_dialect`).Scan(&d); err != nil {
		t.Fatalf("read the stamped dialect: %v", err)
	}
	return d
}

// TestTypedDeclarationRungTranslatesEveryStoredDeclaration is the rung's own
// test: a repository holding a dialect-1 row for every schema kind opens with
// every row typed, the registry whole, and the callables still dispatching.
func TestTypedDeclarationRungTranslatesEveryStoredDeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// A repository with a row of EVERY schema kind: the vocabulary bundles bring
	// kinds, traits, property types and mappings; a bundle closure brings the
	// bundle document, its functions and its agents.
	importVocabulary(t, ds)
	// web brings functions and agents; github brings a mapping with rules, which
	// is the keyed container the translation has to keep as a container.
	installShippedBundle(t, ds, "web")
	installShippedBundle(t, ds, "github")

	// The fixture is what a pre-typed binary left: every declaration's content
	// back in a `definition` blob, the mirrors beside it, the stamp wound back.
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// EVERY schema kind, or the fixture does not mean what the test says.
	rows := declarationRows(t, ds)
	for _, kind := range []string{
		"core.substrate.reamde.dev/authority", "core.substrate.reamde.dev/actor",
		"core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/trait",
		"core.substrate.reamde.dev/propertytype", "core.substrate.reamde.dev/recordmapping",
		"core.substrate.reamde.dev/function", "core.substrate.reamde.dev/agent",
		"core.substrate.reamde.dev/bundle",
	} {
		if len(rows[kind]) == 0 {
			t.Fatalf("the fixture holds no %s row — every schema kind has to be in it", kind)
		}
	}
	if planted := plantDialectOne(t, ds, db, nil); planted < 20 {
		t.Fatalf("planted %d declaration rows — the fixture is too small to mean anything", planted)
	}
	_ = svc.Close()

	// The reopen runs the rung — and then the boot upgrade, which is what makes
	// this the projection-registry test too (M1): the rung leaves rows in the
	// typed shape while the STORED core declarations still declare the blob, so
	// every row the upgrade re-projects carries properties the stored `kind`
	// declaration does not declare. Only projectionKind (vocabularywrite.go),
	// which holds such a row to the declaration the projection is installing,
	// lets that land; with the live registry deciding, this open fails.
	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("reopen a dialect-1 store: %v", err)
	}
	if got := storedDialect(t, db); got != engine.MaxSchemaDialect() {
		t.Fatalf("stamped dialect = %d, want %d", got, engine.MaxSchemaDialect())
	}

	// Every row is typed: no blob, no mirrors, and the manifest's own keys are
	// properties.
	for kind, rows := range declarationRows(t, ds2) {
		for _, row := range rows {
			for _, gone := range []string{"definition", "name", "sourceYAML"} {
				if _, held := row.Properties[gone]; held {
					t.Fatalf("%s %s still carries %q: %v", kind, row.ID, gone, row.Properties)
				}
			}
		}
	}
	wantProps := map[string][]string{
		"core.substrate.reamde.dev/kind":          {"names", "properties"},
		"core.substrate.reamde.dev/agent":         {"prompt", "tools", "provider"},
		"core.substrate.reamde.dev/function":      {"runtime", "source", "emit"},
		"core.substrate.reamde.dev/bundle":        {"installs"},
		"core.substrate.reamde.dev/trait":         {"authority"},
		"core.substrate.reamde.dev/recordmapping": {"from", "to", "edge"},
	}
	typed := declarationRows(t, ds2)
	for kind, want := range wantProps {
		row := typed[kind][0]
		for _, key := range want {
			if _, held := row.Properties[key]; !held {
				t.Fatalf("%s %s carries no %q after the rung: %v", kind, row.ID, key, row.Properties)
			}
		}
	}
	// The keyed and repeated containers survive as containers, not as blobs: the
	// mapping's rules are one, and one of the mappings the fixture holds has them.
	var mapped bool
	for _, row := range typed["core.substrate.reamde.dev/recordmapping"] {
		rules, ok := row.Properties["map"].(map[string]any)
		if !ok {
			continue
		}
		mapped = true
		for name, rv := range rules {
			if _, isRule := rv.(map[string]any); !isRule {
				t.Fatalf("%s: map rule %q is %T, not the {path} object the declaration holds", row.ID, name, rv)
			}
		}
	}
	if !mapped {
		t.Fatal("no mapping in the fixture carries rules — the translation of the map form is untested")
	}

	// The registry is whole: the kinds resolve, and the CALLABLES the bundle
	// installed still dispatch. A trigger write is the dispatch proof that needs
	// no runner: admission resolves the callable against the registry rebuilt
	// from the translated rows, and refuses a trigger it cannot deliver.
	if _, err := ds2.KindByRef(ctx, "tasks.substrate.reamde.dev/task"); err != nil {
		t.Fatalf("a vocabulary kind stopped resolving: %v", err)
	}
	if _, err := ds2.KindByRef(ctx, "web.bundles.substrate.reamde.dev/page"); err != nil {
		t.Fatalf("a bundle's kind stopped resolving: %v", err)
	}
	for _, callable := range []struct{ kind, id string }{
		{"core.substrate.reamde.dev/function", "web.bundles.substrate.reamde.dev/findurls"},
		{"core.substrate.reamde.dev/agent", "web.bundles.substrate.reamde.dev/pageclassifier"},
	} {
		if _, err := ds2.Put(ctx, owner, substrate.PutInput{
			Kind: "core.substrate.reamde.dev/trigger", ID: "rung-" + localName(callable.id),
			Properties: map[string]any{
				"enabled": false,
				"source": map[string]any{"record": map[string]any{
					"kinds": []any{"web.bundles.substrate.reamde.dev/page"}, "ops": []any{"create"},
				}},
				"callable": map[string]any{"kind": callable.kind, "id": callable.id},
			},
		}); err != nil {
			t.Fatalf("a trigger naming the translated %s refuses — the callable did not survive: %v", callable.id, err)
		}
	}

	// IDEMPOTENT: a second open with the stamp wound back writes nothing.
	before := maxSeq(t, ds2)
	windBackDialect(t, db)
	_ = svc2.Close()
	svc3 := open()
	ds3, err := svc3.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	if after := maxSeq(t, ds3); after != before {
		t.Fatalf("re-running the rung appended %d entries — it is not content-gated", after-before)
	}
}

// TestTypedDeclarationRungMigratesOnlyTheUntypedRows is the interruption case.
// The rewrite and the stamp share one transaction, so a crash between them is
// not reachable; what IS reachable is a store where some rows are typed and some
// are not — an operator's partial repair, or a rung that failed on a later
// authority and rolled back. The rung must move exactly the rest.
func TestTypedDeclarationRungMigratesOnlyTheUntypedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// HALF the kind rows: the tasks authority's alone.
	planted := plantDialectOne(t, ds, db, func(kind, id string) bool {
		return kind == "core.substrate.reamde.dev/kind" && strings.HasPrefix(id, "tasks.substrate.reamde.dev/")
	})
	if planted == 0 {
		t.Fatal("nothing planted")
	}
	typedBefore := map[string]map[string]any{}
	for _, row := range declarationRows(t, ds)["core.substrate.reamde.dev/kind"] {
		if _, blob := row.Properties["definition"]; blob {
			continue
		}
		typedBefore[row.ID] = row.Properties
	}
	_ = svc.Close()

	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("reopen a half-typed store: %v", err)
	}
	for _, row := range declarationRows(t, ds2)["core.substrate.reamde.dev/kind"] {
		if _, blob := row.Properties["definition"]; blob {
			t.Fatalf("%s was left untranslated: %v", row.ID, row.Properties)
		}
		if want, wasTyped := typedBefore[row.ID]; wasTyped {
			if !reflect.DeepEqual(want, row.Properties) {
				t.Fatalf("%s was already typed and the rung rewrote it:\n before %v\n after  %v",
					row.ID, want, row.Properties)
			}
		}
	}
}

// TestTypedDeclarationRungRebuildsIdentically is the rebuild-equivalence pin:
// the rung's entries are ordinary changelog entries, so replaying the whole log
// reproduces the migrated rows EXACTLY — the properties, the title, the body and
// the SEARCH BANDS, which are the one thing the fold reads a declaration for. An
// installed authority is in the fixture on purpose: nothing re-projects its rows
// after the rung, so a band the rung got wrong would stay wrong and only a
// rebuild would disagree.
func TestTypedDeclarationRungRebuildsIdentically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds)
	installShippedBundle(t, ds, "web")
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	plantDialectOne(t, ds, db, nil)
	_ = svc.Close()

	svc2 := open()
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	migrated := foldSnapshot(t, db)

	rebuilder, ok := svc2.(interface {
		RebuildRepository(context.Context, string) (engine.RebuildReport, error)
	})
	if !ok {
		t.Fatal("the service does not expose RebuildRepository")
	}
	if _, err := rebuilder.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	replayed := foldSnapshot(t, db)
	for key, want := range migrated {
		got, held := replayed[key]
		if !held {
			t.Fatalf("%s did not survive the replay", key)
		}
		if want != got {
			t.Fatalf("%s replayed differently:\n live   %s\n replay %s", key, want, got)
		}
	}
	if len(replayed) != len(migrated) {
		t.Fatalf("the replay holds %d declaration rows, the live fold %d", len(replayed), len(migrated))
	}
}

// foldSnapshot is every declaration row's WHOLE folded state — properties,
// title, body, states and the search bands — as one comparable string per row.
// The fts column is in it because it is the fold's one registry-dependent
// column: a rung that indexed a row under a declaration other than the one it
// stored would show up here and nowhere else.
func foldSnapshot(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`
		SELECT kind, id, props::text, title, body, states::text, coalesce(fts::text, '')
		FROM records
		WHERE kind LIKE 'core.substrate.reamde.dev/%' AND deleted_at IS NULL
		  AND kind IN ('core.substrate.reamde.dev/authority', 'core.substrate.reamde.dev/actor',
		               'core.substrate.reamde.dev/kind', 'core.substrate.reamde.dev/trait',
		               'core.substrate.reamde.dev/propertytype', 'core.substrate.reamde.dev/recordmapping',
		               'core.substrate.reamde.dev/function', 'core.substrate.reamde.dev/agent',
		               'core.substrate.reamde.dev/bundle')
		ORDER BY kind, id`)
	if err != nil {
		t.Fatalf("read the fold: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var kind, id, props, title, body, states, fts string
		if err := rows.Scan(&kind, &id, &props, &title, &body, &states, &fts); err != nil {
			t.Fatalf("scan the fold: %v", err)
		}
		out[kind+" "+id] = strings.Join([]string{props, title, body, states, fts}, "\x1f")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the fold: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("the repository holds no declaration rows")
	}
	// A snapshot of empty bands would compare equal for the wrong reason.
	indexed := false
	for _, row := range out {
		if fields := strings.Split(row, "\x1f"); fields[len(fields)-1] != "" {
			indexed = true
			break
		}
	}
	if !indexed {
		t.Fatal("no declaration row carries search bands — the fts comparison would prove nothing")
	}
	return out
}

// propsByRow reads every declaration row's properties, keyed by kind+id.
func propsByRow(t *testing.T, ds substrate.Dataset) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for kind, rows := range declarationRows(t, ds) {
		for _, row := range rows {
			out[kind+"\x00"+row.ID] = row.Properties
		}
	}
	return out
}

// TestTypedDeclarationRungRefusesADowngrade pins the half of the atomicity an
// older binary meets: the stamp the rung wrote is above what a dialect-1 binary
// speaks, and such a binary refuses the open instead of reading the typed rows
// as declarations with nothing in them.
func TestTypedDeclarationRungRefusesADowngrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := svc.Dataset(ctx, "geoah"); err != nil {
		t.Fatal(err)
	}
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	if got := storedDialect(t, db); got != engine.MaxSchemaDialect() {
		t.Fatalf("a fresh repository stamped %d, want %d", got, engine.MaxSchemaDialect())
	}
	// A store one dialect ahead of the binary — which is what a dialect-1 binary
	// meets after this rung ran: the refusal is by name, and it is a service
	// condition rather than an auth failure.
	if _, err := db.ExecContext(ctx, `UPDATE vocabulary_dialect SET dialect = $1`,
		engine.MaxSchemaDialect()+1); err != nil {
		t.Fatalf("bump the stored dialect: %v", err)
	}
	_ = svc.Close()
	svc2 := open()
	if _, err := svc2.Dataset(ctx, "geoah"); !errors.Is(err, engine.ErrVocabularyDialectNewer) {
		t.Fatalf("a store ahead of the binary must refuse by name, got %v", err)
	}
}

// TestFreshRepositoryIsTypedFromBirth pins the seed: a repository created by
// this binary holds no dialect-1 row at all, so the rung has nothing to do for
// it and never will.
func TestFreshRepositoryIsTypedFromBirth(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installShippedBundle(t, ds, "web")
	for kind, rows := range declarationRows(t, ds) {
		for _, row := range rows {
			for _, gone := range []string{"definition", "name", "plural", "sourceYAML", "functions", "subagents"} {
				if _, held := row.Properties[gone]; held {
					t.Fatalf("a freshly seeded %s row carries the retired %q: %v", kind, gone, row.Properties)
				}
			}
			if _, held := row.Properties["authority"]; !held && kind != "core.substrate.reamde.dev/authority" {
				t.Fatalf("%s %s carries no authority: %v", kind, row.ID, row.Properties)
			}
		}
	}
	// The declarations a reader browses are the manifest's own keys.
	kindRow := declarationRows(t, ds)["core.substrate.reamde.dev/kind"][0]
	keys := sortedPropKeys(kindRow.Properties)
	if !containsAll(keys, []string{"authority", "names", "properties", "version"}) {
		t.Fatalf("a seeded kind row carries %v", keys)
	}
}

// localName is an id's last segment, for a test-local trigger id.
func localName(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func sortedPropKeys(props map[string]any) []string {
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// TestBootUpgradeDeliversTheTypedFlip is the projection-registry test (M1) in the
// shape this release needs it: a repository whose stored core `kind` declaration
// is the PRE-TYPED one, at a version below the tree's. The boot upgrade
// re-projects that declaration, and the row it writes carries the properties the
// NEW declaration declares — `names`, `properties`, `edges` — none of which the
// stored one does. It can only land because a projected row is held to the
// declaration the projection is installing (vocabularywrite.go projectionKind);
// against the live registry it is refused and the open fails, which is the boot
// this stage would otherwise brick.
func TestBootUpgradeDeliversTheTypedFlip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// The stored core `kind` declaration as the binary before this one shipped
	// it: a `definition` blob and the projected mirrors, at its own older version.
	row := mustGet(t, ds, "core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/kind")
	old := map[string]any{}
	for k, v := range row.Properties {
		old[k] = v
	}
	old["version"] = "v1alpha2"
	old["properties"] = map[string]any{
		"name":      map[string]any{"type": "string"},
		"authority": map[string]any{"type": "string"},
		"version":   map[string]any{"type": "string"},
		"plural":    map[string]any{"type": "string"},
		"source": map[string]any{
			"type": "enum",
			"values": []any{
				map[string]any{"value": "builtin", "label": ""},
				map[string]any{"value": "installed", "label": ""},
			},
		},
		"definition": map[string]any{"type": "json"},
	}
	if err := planter(t, ds).PlantDeclarationRow(ctx,
		"core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/kind", old); err != nil {
		t.Fatalf("plant the pre-typed kind declaration: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("the boot upgrade could not deliver the typed core: %v", err)
	}
	upgraded := mustGet(t, ds2, "core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/kind")
	declared, _ := upgraded.Properties["properties"].(map[string]any)
	if _, typed := declared["names"]; !typed {
		t.Fatalf("the stored kind declaration did not move to the typed one: %v", declared)
	}
	if v, _ := upgraded.Properties["version"].(string); v == "v1alpha2" {
		t.Fatalf("the declaration is still at the planted version: %v", upgraded.Properties)
	}
}

// TestTypedDeclarationRungRefusesAnUntranslatableRow is the structural half of
// the atomicity: the rung stamps the dialect only when NO live declaration row
// still carries a `definition`, so a row it cannot translate fails the rung —
// and the open — rather than surviving the stamp as the one row nobody can read
// twice the same way.
func TestTypedDeclarationRungRefusesAnUntranslatableRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds)
	installShippedBundle(t, ds, "web")
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	plantDialectOne(t, ds, db, nil)
	// One stored closure this binary can no longer PARSE: the pre-refactor `llm`
	// key on an agent, which the loader refuses by name. Nothing can translate its
	// rows, so nothing may stamp over them.
	if _, err := db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{definition,llm}', '"cheap"')
		WHERE kind = $1 AND id = $2`,
		"core.substrate.reamde.dev/agent", "web.bundles.substrate.reamde.dev/pageclassifier"); err != nil {
		t.Fatalf("break one stored closure: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	_, err = svc2.Dataset(ctx, "geoah")
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("the rung must refuse an untranslatable row by name, got %v", err)
	}
	if !strings.Contains(err.Error(), "web.bundles.substrate.reamde.dev") {
		t.Fatalf("the refusal must name the closure: %v", err)
	}
	// NOTHING WAS STAMPED: the rewrite and the stamp are one transaction, so the
	// store is still dialect-1 and a later open can try again.
	var stamped int
	switch err := db.QueryRowContext(ctx, `SELECT dialect FROM vocabulary_dialect`).Scan(&stamped); {
	case errors.Is(err, sql.ErrNoRows): // never stamped, which is the fixture
	case err != nil:
		t.Fatalf("read the stamp: %v", err)
	case stamped >= engine.MaxSchemaDialect():
		t.Fatalf("the failed rung stamped dialect %d", stamped)
	}

	// The same repository opens under the posture it had before: with the store
	// already stamped, the rung does not run, and the broken closure quarantines
	// exactly as it did before this release.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO vocabulary_dialect (dialect) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET dialect = EXCLUDED.dialect`,
		engine.MaxSchemaDialect()); err != nil {
		t.Fatalf("stamp the dialect by hand: %v", err)
	}
	_ = svc2.Close()
	svc3 := open()
	if _, err := svc3.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("with the rung skipped the repository must still open: %v", err)
	}
}

// TestTypedDeclarationRungHoldsRowsToTheRepositorysOwnDeclarations is the
// validation half: a repository whose stored meta-kind declaration is AHEAD of
// the binary's keeps it (the boot upgrade never downgrades), so its rows are
// held to ITS declaration and not to the one this binary ships. Holding them to
// the binary's would refuse a repository for being newer than the substrate
// reading it.
func TestTypedDeclarationRungHoldsRowsToTheRepositorysOwnDeclarations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	p := planter(t, ds)

	// The store as a NEWER binary left it: core's `kind` declaration one version
	// ahead, declaring a `lifecycle` property this binary knows nothing about, and
	// one kind row carrying it.
	kindRow := mustGet(t, ds, "core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/kind")
	ahead := map[string]any{}
	for k, v := range kindRow.Properties {
		ahead[k] = v
	}
	declared, _ := ahead["properties"].(map[string]any)
	if declared == nil {
		t.Fatalf("the kind declaration carries no properties: %v", ahead)
	}
	newer := map[string]any{}
	for k, v := range declared {
		newer[k] = v
	}
	newer["lifecycle"] = map[string]any{"type": "string", "managed": true}
	ahead["properties"] = newer
	ahead["version"] = "v1alpha99"
	if err := p.PlantDeclarationRow(ctx, "core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/kind", ahead); err != nil {
		t.Fatalf("plant the newer kind declaration: %v", err)
	}
	taskRow := mustGet(t, ds, "core.substrate.reamde.dev/kind", "tasks.substrate.reamde.dev/task")
	withLifecycle := map[string]any{}
	for k, v := range taskRow.Properties {
		withLifecycle[k] = v
	}
	withLifecycle["lifecycle"] = "beta"
	if err := p.PlantDeclarationRow(ctx, "core.substrate.reamde.dev/kind", "tasks.substrate.reamde.dev/task", withLifecycle); err != nil {
		t.Fatalf("plant the newer binary's stamped property: %v", err)
	}
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	windBackDialect(t, db)
	_ = svc.Close()

	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("a repository ahead of this binary must still migrate: %v", err)
	}
	migrated := mustGet(t, ds2, "core.substrate.reamde.dev/kind", "tasks.substrate.reamde.dev/task")
	if migrated.Properties["lifecycle"] != "beta" {
		t.Fatalf("the newer binary's stamped property did not survive: %v", migrated.Properties)
	}
	stillAhead := mustGet(t, ds2, "core.substrate.reamde.dev/kind", "core.substrate.reamde.dev/kind")
	if v, _ := stillAhead.Properties["version"].(string); v != "v1alpha99" {
		t.Fatalf("the newer stored declaration was downgraded to %q", v)
	}
}
