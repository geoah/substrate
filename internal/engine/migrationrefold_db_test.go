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
// A stage is staged twice once the repository ledger exists: with the
// ledger empty, as a repository older than every repository migration has
// it, and with the source's ledger rows for the repository migrations a
// binary at that stage could have run, as an installation upgraded in place
// holds them. The second open runs no repository migration the installation
// already ran, so a migration whose effect a repository migration would mask
// is held to the fold the installation really keeps. A last stage at the newest
// migration keeps the whole ledger and runs nothing, so the fold the source's
// own writes left is held to the rebuild with nothing re-derived first.
//
// A stage behind the migration that gives repositories ids holds the source
// keyed by its name, as a release before ids left it: every `repository`
// column and the control-plane row's id the name, the data key wrapped under
// the name, the directory named by it with a format-1 manifest. Its open
// converts it (repositoryid.go) before anything else reads it.
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
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/changelogfile"
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
	"records", "refs", "annotations", "property_marks", "former_ids", "property_offers",
	"trigger_cursors", "trigger_schedule", "observed_segments", "observed_rollups", "observed_latest",
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

// refoldUnitGraph is the migration that creates the unit graph's revision
// counter (decision 0186). A store behind it was written by a binary that
// declared no unit kinds, so it held no unit or conversion row for the
// counter to count; the source holds them because its observed history names
// units. A stage behind it compares no unit_graph row: the counter it starts
// at zero counts nothing a real store of that age held.
const refoldUnitGraph = "0016_unit_graph"

// refoldPropertyMarks is the migration that replaces each property's
// manager and tier with its mark (decision 0284). A store behind it held the
// managers' table and a history in the changelog dialect before the marks, and the marks
// conversion rebuilds its fold at the boot after it, marks and offers
// included; the source's history is in the marks' dialect already, which no
// store of that age held, so nothing converts it there. A stage behind it
// compares, and requires, none of refoldMarkTables.
const refoldPropertyMarks = "0026_property_marks"

// refoldMarkTables are the tables refoldPropertyMarks replaces empty.
var refoldMarkTables = []string{"property_marks", "property_offers"}

// refoldLedgerTable is the migration that creates the repository ledger.
const refoldLedgerTable = "0005_repository_migrations"

// refoldRepositoryIDs is the migration that lets a repository's id differ
// from its name. A stage behind it holds the source keyed by its name.
const refoldRepositoryIDs = "0020_repository_ids"

// refoldLedgerFloors names, for every repository migration, the SQL
// migration that landed with it: a binary that ran it had applied that one,
// so a stage at or past it keeps the repository migration's ledger row. A new
// repository migration adds its entry here; the test fails until it does.
var refoldLedgerFloors = map[string]string{
	"qualify_bare_declaration_names":         "0005_repository_migrations",
	"qualify_bare_selector_kinds":            "0005_repository_migrations",
	"name_units_by_unit_records":             "0016_unit_graph",
	"name_applet_runtimes":                   "0019_operator_spend_reservations",
	"remove_reply_relay_contacts":            "0019_operator_spend_reservations",
	"key_the_repository_record_by_its_id":    "0020_repository_ids",
	"start_paged_drains_over":                "0021_identity_columns_byte_order",
	"purge_the_recovery_key":                 "0023_secrets_left_behind",
	"link_actors":                            "0024_changelog_on_behalf_of",
	"name_writers_by_actor_type":             "0024_changelog_on_behalf_of",
	"drop_a_body_its_kind_does_not_declare":  "0025_records_drop_body",
	"move_the_sign_in_out_of_the_repository": "0027_identities",
}

// refoldSource is the seeded store every stage copies: the schema holding its
// rows, its data root, and the one repository in both, by its name (repo) and
// its id. namedDEK is its data key wrapped under the name, the binding a
// release before ids wrapped it under.
type refoldSource struct {
	schema, root, repo, id string
	namedDEK               []byte
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
	if versions[refoldUnitGraph] == 0 {
		t.Fatalf("the binary no longer embeds %s, whose unit_graph exemption is for that migration alone", refoldUnitGraph)
	}
	if versions[refoldPropertyMarks] == 0 {
		t.Fatalf("the binary no longer embeds %s, whose marks exemption is for that migration alone", refoldPropertyMarks)
	}
	if versions[refoldRepositoryIDs] == 0 {
		t.Fatalf("the binary no longer embeds %s, behind which a stage is keyed by the name", refoldRepositoryIDs)
	}
	for name, floor := range refoldLedgerFloors {
		if versions[floor] == 0 {
			t.Fatalf("refoldLedgerFloors names %s for repository migration %s, and the binary embeds no such migration", floor, name)
		}
	}
	omitBehind := func(behind int) map[string][]string {
		omit := map[string][]string{}
		for _, lag := range refoldLags {
			if behind < versions[lag.migration] {
				omit[lag.table] = append(omit[lag.table], lag.column)
			}
		}
		return omit
	}
	// The first migration creates the tables, so no row exists for it to meet.
	for i, m := range migrations[1:] {
		prior := migrations[i]
		upgrade := func(t *testing.T, dsn string) {
			if err := engine.MigrateThrough(ctx, dsn, m.Version); err != nil {
				t.Fatalf("apply %s: %v", m.Name, err)
			}
		}
		stage := func(t *testing.T, ledger bool) {
			t.Helper()
			live, rebuilt := refoldStage(t, src, prior.Version, omitBehind(prior.Version), ledger, upgrade)
			if prior.Version < versions[refoldUnitGraph] {
				delete(live, "unit_graph")
				delete(rebuilt, "unit_graph")
			}
			if prior.Version < versions[refoldPropertyMarks] {
				for _, table := range refoldMarkTables {
					delete(live, table)
					delete(rebuilt, table)
				}
			}
			if tables, diff := refoldDifferences(live, rebuilt); len(tables) > 0 {
				t.Fatalf("a store at %s, upgraded from %s on, holds a fold that differs in %s from what a rebuild "+
					"makes of its changelog; the last subtest that fails names the migration at fault. "+
					"A SQL migration may not change what the fold holds: the data change belongs in a "+
					"repository migration (docs/operations.md).\nfirst difference, live then rebuilt:\n%s",
					prior.Name, m.Name, strings.Join(tables, ", "), diff)
			}
		}
		t.Run(m.Name, func(t *testing.T) {
			stage(t, false)
			if prior.Version < versions[refoldLedgerTable] {
				return
			}
			t.Run("ledger_kept", func(t *testing.T) { stage(t, true) })
		})
	}
	t.Run("head_ledger_kept", func(t *testing.T) {
		last := migrations[len(migrations)-1]
		live, rebuilt := refoldStage(t, src, last.Version, nil, true, func(*testing.T, string) {})
		if tables, diff := refoldDifferences(live, rebuilt); len(tables) > 0 {
			t.Fatalf("a store at %s whose repository ran every repository migration holds a fold that differs in %s "+
				"from what a rebuild makes of its changelog: the fold's own writes and the rebuild disagree.\n"+
				"first difference, live then rebuilt:\n%s", last.Name, strings.Join(tables, ", "), diff)
		}
	})
	t.Run("fixture_edits_records_alone", func(t *testing.T) {
		fixture, err := os.ReadFile(refoldFixture)
		if err != nil {
			t.Fatalf("read the fixture: %v", err)
		}
		last := migrations[len(migrations)-1].Version
		live, rebuilt := refoldStage(t, src, last, nil, false, func(t *testing.T, dsn string) {
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
// repository's history into it: the seeded core, llm, console and observations
// declarations, the
// tasks sample with the people and scheduling packages it requires, task
// records created, patched, labeled, annotated, referenced, deleted and put
// back (writeSomeHistory), people fed by three mapped sources, with offers
// beside an owner's hold, a merge and a source collected by the sweep
// (writeMappedHistory), a record trigger and a schedule trigger, whose
// creation writes their cursor and fire state, and the environment catalogue's
// metrics with one of every observed shape (writeObservedHistory), so the observed
// index holds points, rollups and latest rows. Both triggers are disabled
// and no dispatch runs, so the scan position a dispatch moves outside the
// ledger (functions.go advanceCursor) never moves. Every stage copies these
// rows, so they are written once.
func seedRefoldSource(t *testing.T) refoldSource {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	root := t.TempDir()
	// A store from releases before the erasure, whose purges kept their
	// history, as the migrations' own stores did.
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(root), engine.WithTestPurgeKeepsHistory())
	if err != nil {
		t.Fatalf("open the source: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	repo := testdb.Repository(t)
	info, err := svc.CreateRepository(ctx, repo)
	if err != nil {
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
	writeObservedHistory(t, ds)
	// The stages copy the rows and the directory, so the source's process
	// must have written its last line and released the repository.
	if err := svc.Close(); err != nil {
		t.Fatalf("close the source: %v", err)
	}
	var wrapped []byte
	if err := rawDB(t, dsn).QueryRowContext(ctx, `SELECT dek FROM repositories WHERE id = $1`, info.ID).Scan(&wrapped); err != nil {
		t.Fatalf("read the source's wrapped DEK: %v", err)
	}
	dek, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, wrapped, engine.DEKAAD(info.ID))
	if err != nil {
		t.Fatalf("unwrap the source's DEK: %v", err)
	}
	named, err := engine.SealWithKey(engine.TestCredentialKeyBytes, dek, engine.DEKAAD(repo))
	if err != nil {
		t.Fatal(err)
	}
	return refoldSource{schema: currentSchema(t, rawDB(t, dsn)), root: root, repo: repo, id: info.ID, namedDEK: named}
}

// installRefoldTriggers installs a package with one function and two
// disabled triggers onto it, a record trigger and an hourly schedule, so the
// delivery ledger holds a cursor and a fire state. The body never runs.
func installRefoldTriggers(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	const pkg = "refold.test.dev/refold"
	callable := vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/noop")
	err := enginetest.Install(context.Background(), ds, substrate.ActorPerson, enginetest.Manifest{
		Name: "refold", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
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
// both snapshots. ledger keeps the source's ledger rows of the repository
// migrations a binary at `behind` could have run (refoldLedgerFloors), so the
// open runs only the ones after them.
func refoldStage(t *testing.T, src refoldSource, behind int, omit map[string][]string, ledger bool, upgrade func(t *testing.T, dsn string)) (live, rebuilt map[string]string) {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	if err := engine.MigrateThrough(ctx, dsn, behind); err != nil {
		t.Fatalf("migrate the stage through %d: %v", behind, err)
	}
	keyed := behind >= refoldVersion(t, refoldRepositoryIDs)
	key := src.id
	if !keyed {
		key = src.repo
	}
	db := rawDB(t, dsn)
	if n := copyRefoldRows(t, db, src.schema, currentSchema(t, db), key); n == 0 {
		t.Fatal("the stage holds no record: it would compare two empty folds")
	}
	if !keyed {
		if _, err := db.ExecContext(ctx, `UPDATE repositories SET dek = $1`, src.namedDEK); err != nil {
			t.Fatalf("wrap the stage's DEK under the name: %v", err)
		}
	}
	if ledger && copyRefoldLedger(t, db, src.schema, currentSchema(t, db), behind, key) == 0 {
		t.Fatal("the stage kept no ledger row: it would stage what the empty ledger already does")
	}
	upgrade(t, dsn)

	// A keyed stage restores nothing (the archive is its table's); one
	// behind ids has no archive and a directory named by the name, as a
	// release before ids left it.
	root := copyRepositoryDir(t, src.root, src.id)
	if !keyed {
		root = t.TempDir()
		legacyDirectory(t, filepath.Join(src.root, "archive"), root, src.id)
		keyRefoldDirByName(t, root, src)
	}
	svc := reopenWith(t, dsn, root)
	ds, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the staged repository: %v", err)
	}
	// A stage behind the search index's table reindexes at this open, and a
	// stage behind the observed index's tables derives it behind the open
	// (checkObservedIndex), so the fold is read once both have settled.
	<-engine.SearchReindexDone(ds)
	engine.DrainIndexReprojection(t, ds)
	live = refoldSnapshot(t, db, omit)
	marksAt := 0
	if migrations, err := engine.Migrations(); err == nil {
		for _, m := range migrations {
			if m.Name == refoldPropertyMarks {
				marksAt = m.Version
			}
		}
	}
	for _, table := range refoldSeeded {
		if behind < marksAt && slices.Contains(refoldMarkTables, table) {
			continue
		}
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
	engine.DrainIndexReprojection(t, ds)
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
// the stage's open runs them all, unless the stage keeps the ledger
// (copyRefoldLedger). Every `repository` column and the control-plane row's
// id are written as key: the source's id, or its name for a stage behind
// refoldRepositoryIDs. It returns the records copied.
func copyRefoldRows(t *testing.T, db *sql.DB, from, to, key string) int64 {
	t.Helper()
	// A table a later migration replaced takes none of the source's rows: one
	// it turned into a view holds none of its own in the source, and one
	// whose stage shape requires a column the source no longer has could
	// not take them. 0026 is both: it drops the old manager and offer tables'
	// rows, a fold table and a derived one, which the marks and the rebuild
	// give again.
	tables := refoldColumnQuery(t, db, `
		SELECT s.table_name FROM information_schema.tables s
		JOIN information_schema.tables f
		  ON f.table_schema = $2 AND f.table_name = s.table_name AND f.table_type = 'BASE TABLE'
		WHERE s.table_schema = $1 AND s.table_type = 'BASE TABLE'
		  AND NOT EXISTS (
		      SELECT 1 FROM information_schema.columns c
		      WHERE c.table_schema = $1 AND c.table_name = s.table_name
		        AND c.is_nullable = 'NO' AND c.column_default IS NULL AND c.is_generated = 'NEVER'
		        AND NOT EXISTS (
		            SELECT 1 FROM information_schema.columns g
		            WHERE g.table_schema = $2 AND g.table_name = s.table_name AND g.column_name = c.column_name))
		ORDER BY s.table_name`, to, from)
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
		selected := make([]string, len(cols))
		for i, c := range cols {
			quoted[i] = pgx.Identifier{c}.Sanitize()
			selected[i] = quoted[i]
			if c == "repository" || (table == "repositories" && c == "id") {
				selected[i] = refoldLiteral(t, key)
			}
		}
		res, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s`,
			pgx.Identifier{to, table}.Sanitize(), strings.Join(quoted, ", "), strings.Join(selected, ", "), pgx.Identifier{from, table}.Sanitize()))
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

// copyRefoldLedger copies the source's repository ledger rows of the
// repository migrations whose floor (refoldLedgerFloors) is at or before
// `behind` into the stage, under key as copyRefoldRows writes it, and returns
// how many it kept.
func copyRefoldLedger(t *testing.T, db *sql.DB, from, to string, behind int, key string) int {
	t.Helper()
	migrations, err := engine.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]int{}
	for _, m := range migrations {
		versions[m.Name] = m.Version
	}
	rows, err := db.Query(fmt.Sprintf(`SELECT repository, version, name, applied_at FROM %s ORDER BY repository, version`,
		pgx.Identifier{from, "repository_migrations"}.Sanitize()))
	if err != nil {
		t.Fatalf("read the source's ledger: %v", err)
	}
	type entry struct {
		repository, name string
		version          int
		appliedAt        time.Time
	}
	var all []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.repository, &e.version, &e.name, &e.appliedAt); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	kept := 0
	for _, e := range all {
		floor, ok := refoldLedgerFloors[e.name]
		if !ok {
			t.Fatalf("repository migration %d (%s) has no entry in refoldLedgerFloors: name the SQL migration it landed with", e.version, e.name)
		}
		if versions[floor] > behind {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (repository, version, name, applied_at) VALUES ($1, $2, $3, $4)`,
			pgx.Identifier{to, "repository_migrations"}.Sanitize()), key, e.version, e.name, e.appliedAt); err != nil {
			t.Fatalf("copy ledger row %d into the stage: %v", e.version, err)
		}
		kept++
	}
	return kept
}

// keyRefoldDirByName puts the copy of the source's directory under root back
// into the shape a release before ids left it in: named by the name, its
// manifest format 1 with the data key wrapped under the name.
func keyRefoldDirByName(t *testing.T, root string, src refoldSource) {
	t.Helper()
	from, err := changelogfile.RepoDir(root, src.id)
	if err != nil {
		t.Fatal(err)
	}
	to, err := changelogfile.RepoDir(root, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(to, changelogfile.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var m changelogfile.Manifest
	if err := m.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	m.Format, m.ID, m.DEK = changelogfile.ManifestFormatBeforeIDs, "", src.namedDEK
	if err := changelogfile.WriteManifest(to, m); err != nil {
		t.Fatalf("write the format-1 manifest: %v", err)
	}
}

// refoldVersion is the version of the embedded migration named name.
func refoldVersion(t *testing.T, name string) int {
	t.Helper()
	migrations, err := engine.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.Name == name {
			return m.Version
		}
	}
	t.Fatalf("the binary embeds no migration %s", name)
	return 0
}

// refoldLiteral is key as a SQL string literal. A key is a repository id or an
// authority, neither of which holds a quote; one that did is refused rather
// than escaped.
func refoldLiteral(t *testing.T, key string) string {
	t.Helper()
	if !vocabulary.ValidRepositoryID(key) && !vocabulary.ValidRepositoryAuthority(key) {
		t.Fatalf("%q is neither a repository id nor an authority", key)
	}
	return "'" + key + "'"
}

// refoldSnapshot reads every fold table whole: each row as JSON, every column
// but `repository` and the ones omit names, sorted, one array per table.
// Whole rows rather than FoldSnapshot's column lists, so a column a later
// migration adds to a fold table is compared from the day it lands.
func refoldSnapshot(t *testing.T, db *sql.DB, omit map[string][]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range engine.FoldTables() {
		from := pgx.Identifier{table}.Sanitize()
		row := `to_jsonb(x) - 'repository'`
		switch table {
		case "observed_streams":
			// A stream's id is the engine's own and a rebuild may allocate
			// another, its extent a bound that only grows, and a stream
			// whose records are gone keeps its row: the segments, rollups
			// and latest rows below name their streams by key.
			continue
		case "observed_segments", "observed_rollups", "observed_latest":
			from = `(SELECT t.*, jsonb_build_array(s.metric, s.kind, s.source, s.about, s.channel, s.statistic) AS stream_key
				FROM ` + from + ` t JOIN observed_streams s ON s.id = t.stream)`
			row += ` - 'stream'`
		}
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
			row, from)).Scan(&doc); err != nil {
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
