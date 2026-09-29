package engine_test

// A SQL MIGRATION NEVER CHANGES WHAT THE FOLD HOLDS (docs/operations.md,
// "Upgrading the binary"). The changelog is the truth and `repository
// rebuild` replays it into the fold tables (rebuild.go), so a migration that
// edits a fold row on its own leaves a store whose next rebuild is a
// different store. For every embedded migration this stages a database one
// migration behind it that holds a repository's rows, applies the migration,
// opens the engine over it (the open applies the rest and runs every
// repository migration), and holds the fold the open left to the fold
// RebuildRepository makes of the same changelog. Every stage's open applies
// the later migrations too, so a faulty migration fails its own subtest and
// each one before it, and the last subtest that fails names it.
//
// A fixture migration that edits `records` and writes no changelog entry
// goes through the same stage and must FAIL the comparison, on `records`.
// That keeps the test from passing vacuously: a harness that stopped seeing
// the rows would pass every embedded migration and fail there.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// refoldFixture is the fixture migration. It is under testdata/ because the
// runner embeds migrations/ alone, so no database ever applies it.
const refoldFixture = "testdata/refold/9999_label_every_record.up.sql"

// refoldMarks are the fold columns compared by presence alone: the orphan and
// ambiguity marks, which a rebuild derives again and stamps with its own
// transaction's time (rebuild.go rederiveOffers). A migration that sets or
// clears a mark is seen; one that only moves a stamp is not.
var refoldMarks = map[string][]string{"records": {"orphaned_at", "ambiguous_at"}}

// refoldSeeded are the fold tables the seeded repository holds rows in. A
// stage whose fold has none in one of them compares nothing there, so it
// fails instead. trigger_failures and paged_cursors are compared too, and
// hold no row: a parked failure and a paged drain both need a function body
// to run. A migration that edits either of them seeds a row first.
var refoldSeeded = []string{
	"records", "refs", "annotations", "property_managers", "former_ids", "property_offers",
	"trigger_cursors", "trigger_schedule",
}

// refoldLags are the landed migrations that predate the rule and add a
// derived column empty: a row written before the migration keeps it empty
// until a recompute (or, for ambiguous_at, the source's next write) sets it,
// while a rebuild derives it for every row. They are frozen, so a stage whose
// rows predate one compares its table without that column. Of the three, the
// seeded repository exercises property_offers.source alone: it holds offers
// and no orphan or ambiguity mark. Nothing else is exempt, and a new
// migration is never added here.
var refoldLags = []struct{ migration, table, column string }{
	{"0003_orphaned_at", "records", "orphaned_at"},
	{"0004_property_offers_source", "property_offers", "source"},
	{"0006_ambiguous_at", "records", "ambiguous_at"},
}

// refoldSource is the seeded store every stage copies: the schema holding its
// rows, its data root, and the one repository in both.
type refoldSource struct {
	schema, root, repo string
}

func TestEveryMigrationLeavesAFoldTheRebuildReproduces(t *testing.T) {
	ctx := context.Background()
	src := seedRefoldSource(t)
	migrations, err := engine.Migrations()
	if err != nil {
		t.Fatalf("list the embedded migrations: %v", err)
	}
	if len(migrations) < 2 {
		t.Fatalf("the binary embeds %d migration(s); the loop below would check none", len(migrations))
	}
	versions := map[string]int{}
	for _, m := range migrations {
		versions[m.Name] = m.Version
	}
	for _, lag := range refoldLags {
		if versions[lag.migration] == 0 {
			t.Fatalf("the binary no longer embeds %s: delete its refoldLags entry, whose exemption is for that migration alone", lag.migration)
		}
	}
	// The first migration creates the tables, so no row exists for it to meet.
	for i, m := range migrations[1:] {
		prior := migrations[i]
		t.Run(m.Name, func(t *testing.T) {
			behind := prior.Version
			omit := map[string][]string{}
			for _, lag := range refoldLags {
				if behind < versions[lag.migration] {
					omit[lag.table] = append(omit[lag.table], lag.column)
				}
			}
			live, rebuilt := refoldStage(t, src, behind, omit, func(t *testing.T, dsn string) {
				if err := engine.MigrateThrough(ctx, dsn, m.Version); err != nil {
					t.Fatalf("apply %s: %v", m.Name, err)
				}
			})
			if tables, diff := refoldDifferences(live, rebuilt); len(tables) > 0 {
				t.Fatalf("a store at %s, upgraded from %s on, holds a fold that differs in %s from what a rebuild "+
					"makes of its changelog; the last subtest that fails names the migration at fault. "+
					"A SQL migration may not change what the fold holds: the data change belongs in a "+
					"repository migration (docs/operations.md).\nfirst difference, live then rebuilt:\n%s",
					prior.Name, m.Name, strings.Join(tables, ", "), diff)
			}
		})
	}
	t.Run("fixture_edits_records_alone", func(t *testing.T) {
		fixture, err := os.ReadFile(refoldFixture)
		if err != nil {
			t.Fatalf("read the fixture: %v", err)
		}
		last := migrations[len(migrations)-1].Version
		live, rebuilt := refoldStage(t, src, last, nil, func(t *testing.T, dsn string) {
			if _, err := rawDB(t, dsn).ExecContext(ctx, string(fixture)); err != nil {
				t.Fatalf("apply the fixture: %v", err)
			}
		})
		const label = `"fixture/migrated": true`
		if !strings.Contains(live["records"], label) {
			t.Fatal("the fixture labeled no record: the stage holds no live row for a migration to edit")
		}
		if strings.Contains(rebuilt["records"], label) {
			t.Fatal("the rebuild kept a label no changelog entry wrote: the replay did not replace the fold")
		}
		tables, _ := refoldDifferences(live, rebuilt)
		if len(tables) != 1 || tables[0] != "records" {
			t.Fatalf("the fixture edits records and writes no changelog entry, and the comparison found differences in %v, want [records]", tables)
		}
	})
}

// seedRefoldSource opens a store in a schema of its own and writes one
// repository's history into it: the seeded core and llm declarations, the
// tasks sample with the people and scheduling packages it requires, task
// records created, patched, labeled, annotated, referenced, deleted and put
// back (writeSomeHistory), people fed by three mapped sources, with offers
// beside an owner's hold, a merge and a source collected by the sweep
// (writeMappedHistory), and a record trigger and a schedule trigger, whose
// creation writes their cursor and fire state. Both triggers are disabled
// and no dispatch runs, so the scan position a dispatch moves outside the
// ledger (functions.go advanceCursor) never moves. Every stage copies these
// rows, so they are written once.
func seedRefoldSource(t *testing.T) refoldSource {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	root := t.TempDir()
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(root))
	if err != nil {
		t.Fatalf("open the source: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	repo := testdb.Repository(t)
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds, "tasks")
	writeSomeHistory(t, ds)
	installPeopleSourcesWithDir(t, ds)
	writeMappedHistory(t, ds)
	installRefoldTriggers(t, ds)
	// The stages copy the rows and the directory, so the source's process
	// must have written its last line and released the repository.
	if err := svc.Close(); err != nil {
		t.Fatalf("close the source: %v", err)
	}
	return refoldSource{schema: currentSchema(t, rawDB(t, dsn)), root: root, repo: repo}
}

// installRefoldTriggers installs a package with one function and two
// disabled triggers onto it, a record trigger and an hourly schedule, so the
// delivery ledger holds a cursor and a fire state. The body never runs.
func installRefoldTriggers(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	const pkg = "refold.test.dev/refold"
	callable := vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/noop")
	err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "refold", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.FunctionManifest(pkg, "noop", map[string]any{
				"description": "the triggers' callable; no dispatch runs it",
				"runtime":     vocabulary.RuntimePython,
				"source":      "def main(input, host):\n    return {}\n",
			}),
		},
		Triggers: []enginetest.Trigger{
			{ID: "on-task", Properties: map[string]any{
				"enabled":  false,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{"samples.substrate.reamde.dev/tasks/task"}}},
				"callable": callable,
			}},
			{ID: "hourly", Properties: map[string]any{
				"enabled": false,
				"source": map[string]any{"schedule": map[string]any{
					"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": "2026-01-01T00:00:00Z",
				}},
				"callable": callable,
			}},
		},
	})
	if err != nil {
		t.Fatalf("install the refold triggers: %v", err)
	}
}

// refoldStage stages one upgrade and returns the fold twice: as the open left
// it, and as RebuildRepository left it. The stage is a schema that ran the
// embedded migrations through `behind` and holds the source's rows: the rows
// this binary writes, in the tables an older binary built. A migration aimed
// at a shape this binary no longer writes therefore meets nothing here until
// the seed plants that shape (PlantDeclarationRow writes a row as an older
// binary left it, changelog entry included). upgrade then runs the step
// under test, and the engine opens over the schema and a copy of the
// source's repository directory. omit names columns, by table, left out of
// both snapshots.
func refoldStage(t *testing.T, src refoldSource, behind int, omit map[string][]string, upgrade func(t *testing.T, dsn string)) (live, rebuilt map[string]string) {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	if err := engine.MigrateThrough(ctx, dsn, behind); err != nil {
		t.Fatalf("migrate the stage through %d: %v", behind, err)
	}
	db := rawDB(t, dsn)
	if n := copyRefoldRows(t, db, src.schema, currentSchema(t, db)); n == 0 {
		t.Fatal("the stage holds no record: it would compare two empty folds")
	}
	upgrade(t, dsn)

	svc := reopenWith(t, dsn, copyRepositoryDir(t, src.root, src.repo))
	ds, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the staged repository: %v", err)
	}
	// A stage behind the search index's table reindexes at this open, and the
	// fold is read once that has settled.
	<-engine.SearchReindexDone(ds)
	live = refoldSnapshot(t, db, omit)
	for _, table := range refoldSeeded {
		if live[table] == "[]" {
			t.Fatalf("the staged fold holds no %s row: the comparison would hold nothing there", table)
		}
	}
	report, err := svc.(rebuilder).RebuildRepository(ctx, src.repo)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if report.Entries == 0 || report.Records == 0 {
		t.Fatalf("the rebuild replayed nothing: %+v", report)
	}
	rebuilt = refoldSnapshot(t, db, omit)
	if err := svc.Close(); err != nil {
		t.Fatalf("close the stage: %v", err)
	}
	return live, rebuilt
}

// copyRefoldRows copies every row of the source schema into the stage, table
// by table, over the columns both have: a column or a table a later migration
// adds is not in the stage yet, and takes its default when that migration
// runs. schema_migrations is the stage's own. repository_migrations stays
// empty, as it is for a repository older than every repository migration, so
// the stage's open runs them all. It returns the records copied.
func copyRefoldRows(t *testing.T, db *sql.DB, from, to string) int64 {
	t.Helper()
	tables := refoldColumnQuery(t, db, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
		ORDER BY table_name`, to)
	var records int64
	for _, table := range tables {
		if table == "schema_migrations" || table == "repository_migrations" {
			continue
		}
		cols := refoldColumnQuery(t, db, `
			SELECT s.column_name FROM information_schema.columns s
			JOIN information_schema.columns f
			  ON f.table_schema = $2 AND f.table_name = s.table_name AND f.column_name = s.column_name
			WHERE s.table_schema = $1 AND s.table_name = $3 AND s.is_generated = 'NEVER'
			ORDER BY s.ordinal_position`, to, from, table)
		if len(cols) == 0 {
			continue
		}
		quoted := make([]string, len(cols))
		for i, c := range cols {
			quoted[i] = pgx.Identifier{c}.Sanitize()
		}
		list := strings.Join(quoted, ", ")
		res, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s`,
			pgx.Identifier{to, table}.Sanitize(), list, list, pgx.Identifier{from, table}.Sanitize()))
		if err != nil {
			t.Fatalf("copy %s into the stage: %v", table, err)
		}
		if table == "records" {
			if records, err = res.RowsAffected(); err != nil {
				t.Fatal(err)
			}
		}
	}
	return records
}

// refoldSnapshot reads every fold table whole: each row as JSON, every column
// but `repository` and the ones omit names, sorted, one array per table.
// Whole rows rather than FoldSnapshot's column lists, so a column a later
// migration adds to a fold table is compared from the day it lands.
func refoldSnapshot(t *testing.T, db *sql.DB, omit map[string][]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range engine.FoldTables() {
		row := `to_jsonb(x) - 'repository'`
		for _, col := range omit[table] {
			row = fmt.Sprintf(`%s - '%s'`, row, col)
		}
		for _, col := range refoldMarks[table] {
			if slices.Contains(omit[table], col) {
				continue
			}
			row = fmt.Sprintf(`(%s - '%s') || jsonb_build_object('%s', x.%s IS NOT NULL)`,
				row, col, col, pgx.Identifier{col}.Sanitize())
		}
		var doc string
		if err := db.QueryRow(fmt.Sprintf(
			`SELECT coalesce(jsonb_agg(r ORDER BY r::text), '[]'::jsonb)::text FROM (SELECT %s AS r FROM %s x) s`,
			row, pgx.Identifier{table}.Sanitize())).Scan(&doc); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		out[table] = doc
	}
	return out
}

// refoldDifferences names every table whose snapshot differs, sorted, and
// where the first of them parts.
func refoldDifferences(live, rebuilt map[string]string) (tables []string, first string) {
	for table := range live {
		if live[table] != rebuilt[table] {
			tables = append(tables, table)
		}
	}
	sort.Strings(tables)
	if len(tables) > 0 {
		first = tables[0] + ": " + firstByteDifference([]byte(live[tables[0]]), []byte(rebuilt[tables[0]]))
	}
	return tables, first
}

func currentSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	return schema
}

// refoldColumnQuery runs a query of one text column and returns its values.
func refoldColumnQuery(t *testing.T, db *sql.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
