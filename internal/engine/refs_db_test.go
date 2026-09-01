package engine_test

// THE REFS INDEX as storage: what the migration leaves behind, what a
// re-derive preserves, and what a declaration change re-projects. The
// derivation itself is pure and tested without a database
// (refs_internal_test.go); this is the half that needs one.

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const refsAuthority = "refsindex.example.substrate.reamde.dev"

const (
	refsHub   = refsAuthority + "/hub"
	refsSpoke = refsAuthority + "/spoke"
)

// refsVocabulary installs a hub and a spoke naming it twice: once singly, once
// through a repeated reference, so a re-derive has an `ord` to move.
func refsVocabulary(t *testing.T, ds substrate.Dataset, withHub bool) error {
	t.Helper()
	spoke := map[string]any{
		"hubs": map[string]any{"type": "reference", "kind": refsHub, "repeated": true},
	}
	if withHub {
		spoke["hub"] = map[string]any{"type": "reference", "kind": refsHub}
	}
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.AuthorityManifest(refsAuthority, 0),
		vocabulary.KindManifest(refsAuthority,
			map[string]any{"singular": "hub", "plural": "hubs"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(refsAuthority,
			map[string]any{"singular": "spoke", "plural": "spokes"},
			map[string]any{"properties": spoke}),
	})
	return err
}

// refRows reads the index rows one record holds, in the index's own key order.
type refRowRead struct {
	property, path string
	ord            int
	dst            string
}

func refRows(t *testing.T, raw *sql.DB, kind, id string) []refRowRead {
	t.Helper()
	rows, err := raw.QueryContext(context.Background(),
		`SELECT property, path, ord, dst FROM refs
		 WHERE src_kind = $1 AND src = $2 ORDER BY property, path, ord`, kind, id)
	if err != nil {
		t.Fatalf("read the refs index: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []refRowRead
	for rows.Next() {
		var r refRowRead
		if err := rows.Scan(&r.property, &r.path, &r.ord, &r.dst); err != nil {
			t.Fatalf("scan a refs row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the refs index: %v", err)
	}
	return out
}

// TestMigrationLeavesTheRefsIndexAndNoEdges: 0010 drops the edges table and
// creates `refs` in its place, so a database this binary opened has exactly
// one of the two.
func TestMigrationLeavesTheRefsIndexAndNoEdges(t *testing.T) {
	t.Parallel()
	_, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()

	var refsThere, edgesThere bool
	if err := raw.QueryRowContext(ctx,
		`SELECT to_regclass(current_schema() || '.refs') IS NOT NULL,
		        to_regclass(current_schema() || '.edges') IS NOT NULL`).Scan(&refsThere, &edgesThere); err != nil {
		t.Fatalf("ask the catalog: %v", err)
	}
	if !refsThere {
		t.Fatal("migration 0010 did not create the refs index")
	}
	if edgesThere {
		t.Fatal("the edges table survived migration 0010")
	}
	// The primary key is what makes a re-derive replace exactly one record's
	// rows, so it is asserted rather than assumed.
	var cols string
	if err := raw.QueryRowContext(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		FROM pg_index i
		JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE i.indrelid = to_regclass(current_schema() || '.refs') AND i.indisprimary`).Scan(&cols); err != nil {
		t.Fatalf("read the refs primary key: %v", err)
	}
	if want := "repository,src_kind,src,property,path,ord"; cols != want {
		t.Fatalf("refs primary key = %q, want %q", cols, want)
	}
}

// A ROW CARRIES NO CLOCK (migration 0011). Every column is a function of the
// source record's folded properties and its kind's declaration, which is what
// makes the live table and the rebuilt one the same table; a `created_at` read
// the apply's clock on a re-projection and the entry's on a replay.
func TestTheRefsIndexStoresNoTimestamp(t *testing.T) {
	t.Parallel()
	_, raw, _ := newDatasetWithDB(t)

	var cols string
	if err := raw.QueryRowContext(context.Background(), `
		SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'refs'`).Scan(&cols); err != nil {
		t.Fatalf("read the refs columns: %v", err)
	}
	if want := "repository,src_kind,src,property,path,ord,dst_kind,dst,props"; cols != want {
		t.Fatalf("refs columns = %q, want %q", cols, want)
	}
}

// THE REVERSE READ'S INDEX COVERS ITS SORT. `incoming` matches on (repository,
// dst_kind, dst) and pages in (src_kind, src, property, path, ord) order, so
// the index carries the ordering key too: without it a hot target sorts its
// whole match set once per page.
//
// This is the migration's half — which columns the index has, in which order.
// Whether the reader's own statement plans onto it is
// TestIncomingPageIsAnIndexOrderedScan, which explains the production SQL.
func TestTheRefsIndexCoversTheIncomingSort(t *testing.T) {
	t.Parallel()
	_, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()

	var cols string
	if err := raw.QueryRowContext(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		FROM pg_index i
		JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE i.indrelid = to_regclass(current_schema() || '.refs')
		  AND i.indexrelid = to_regclass(current_schema() || '.refs_dst_idx')`).Scan(&cols); err != nil {
		t.Fatalf("read the refs dst index: %v", err)
	}
	if want := "repository,dst_kind,dst,src_kind,src,property,path,ord"; cols != want {
		t.Fatalf("refs_dst_idx = %q, want %q", cols, want)
	}
}

// THE REBUILT INDEX IS THE LIVE ONE, across a declaration change that
// re-projects it. A vocabulary apply re-derives affected records inside the
// apply, while a rebuild re-derives them from the replayed record entries; the
// two must land on the same rows, or the fold snapshot the containment test
// stands on is not reproducible.
func TestTheRebuiltIndexMatchesTheLiveOne(t *testing.T) {
	t.Parallel()
	svc, ds := newDataset(t)
	ctx := context.Background()
	if err := refsVocabulary(t, ds, true); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h1"})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s1", Properties: map[string]any{"hubs": []any{"h1"}},
	})
	// A tombstone holds rows the narrowing counts never see, which is where the
	// live path and the replay had the most room to disagree — and it is what
	// lets `hub` be dropped below, since no LIVE record carries it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s2", Properties: map[string]any{"hub": "h1"},
	})
	if _, err := ds.Delete(ctx, owner, refsSpoke, "s2"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Drop `hub` from the declaration: every spoke's rows are re-projected
	// inside the apply, under the apply's own clock.
	if err := refsVocabulary(t, ds, false); err != nil {
		t.Fatalf("drop the reference: %v", err)
	}

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); !bytes.Equal(before, after) {
		t.Fatal("the rebuilt refs index differs from the live one")
	}
}

// A RE-DERIVE REPLACES THE WHOLE ROW SET, so the ordinals follow the values a
// repeated reference now holds rather than the ones it held before.
func TestReDeriveFollowsTheOrdinals(t *testing.T) {
	t.Parallel()
	ds, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()
	if err := refsVocabulary(t, ds, true); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	for _, id := range []string{"h1", "h2", "h3"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: id})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s1",
		Properties: map[string]any{"hub": "h1", "hubs": []any{"h1", "h2"}},
	})
	if got := refRows(t, raw, refsSpoke, "s1"); len(got) != 3 {
		t.Fatalf("the first write derived %d rows, want 3", len(got))
	}

	// h1 moves from ord 0 to ord 1, h2 leaves, h3 arrives.
	if _, err := ds.Patch(ctx, owner, refsSpoke, "s1", substrate.PatchInput{
		Properties: map[string]any{"hubs": []any{"h3", "h1"}},
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}

	after := refRows(t, raw, refsSpoke, "s1")
	if len(after) != 3 {
		t.Fatalf("the re-derive left %d rows, want 3: %+v", len(after), after)
	}
	// The ordinals followed the values.
	var order []string
	for _, r := range after {
		if r.property == "hubs" {
			order = append(order, r.dst)
		}
	}
	if len(order) != 2 || order[0] != "h3" || order[1] != "h1" {
		t.Fatalf("the repeated reference derived %v, want [h3 h1]", order)
	}
}

// A KIND WITH NO REFERENCE SITE ISSUES NO REFS STATEMENTS. Every write used to
// delete by source and re-derive, so a mail message — a kind that points at
// nothing — paid two statements per write on the substrate's hottest path.
//
// The proof is a STATEMENT-level trigger that raises: it fires once per
// statement even when the statement matches no row, which a row count could not
// tell from silence. The reference-bearing write at the end is what shows the
// instrument works rather than the trigger being unreachable.
func TestAReferenceFreeKindIssuesNoRefsStatements(t *testing.T) {
	t.Parallel()
	ds, _, dsn := newDatasetWithDB(t)
	ctx := context.Background()
	const note = refsAuthority + "/note"
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.AuthorityManifest(refsAuthority, 0),
		vocabulary.KindManifest(refsAuthority,
			map[string]any{"singular": "hub", "plural": "hubs"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(refsAuthority,
			map[string]any{"singular": "spoke", "plural": "spokes"},
			map[string]any{"properties": map[string]any{
				"hub": map[string]any{"type": "reference", "kind": refsHub},
			}}),
		vocabulary.KindManifest(refsAuthority,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h1"})

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open the schema owner's connection: %v", err)
	}
	defer func() { _ = admin.Close() }()
	if _, err := admin.ExecContext(ctx, `
		CREATE FUNCTION refs_statement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'a refs statement was issued'; END $$;
		CREATE TRIGGER refs_statement_guard AFTER INSERT OR DELETE ON refs
		FOR EACH STATEMENT EXECUTE FUNCTION refs_statement_guard();`); err != nil {
		t.Fatalf("arm the refs statement guard: %v", err)
	}

	// A kind with no reference site: the write must not reach the table.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: note, ID: "n1", Properties: map[string]any{"text": "hello"},
	}); err != nil {
		t.Fatalf("a reference-free write paid refs bookkeeping: %v", err)
	}
	// A kind with one: the same guard fires, so the silence above was the skip
	// and not a disarmed trigger.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s1", Properties: map[string]any{"hub": "h1"},
	}); err == nil {
		t.Fatal("the guard did not fire on a reference-bearing write")
	}
}

// A CONTAINER FLIP ABOVE A NESTED REFERENCE re-projects the kind. deriveRefs
// addresses a nested pointer through its ANCESTORS — an object property that
// gains `repeated: true` moves the pointer inside it from `callable` to
// `0.callable` — so the declaration change is one the kind's stored records
// must be re-derived against. A tombstone is where it shows: the narrowing
// counts never see one, so nothing else would have touched its rows and they
// would answer at an address the declaration no longer describes.
func TestAContainerFlipAboveAReferenceReDerivesTombstones(t *testing.T) {
	t.Parallel()
	ds, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()
	const agent = refsAuthority + "/agent"
	nested := func(repeated bool) error {
		tool := map[string]any{
			"type": "object",
			"fields": map[string]any{
				"callable": map[string]any{"type": "reference", "kind": refsHub},
			},
		}
		if repeated {
			tool["repeated"] = true
		}
		_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
			vocabulary.AuthorityManifest(refsAuthority, 0),
			vocabulary.KindManifest(refsAuthority,
				map[string]any{"singular": "hub", "plural": "hubs"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(refsAuthority,
				map[string]any{"singular": "agent", "plural": "agents"},
				map[string]any{"properties": map[string]any{"tool": tool}}),
		})
		return err
	}
	if err := nested(false); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h1"})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: agent, ID: "a1",
		Properties: map[string]any{"tool": map[string]any{"callable": "h1"}},
	})
	if got := refRows(t, raw, agent, "a1"); len(got) != 1 || got[0].path != "callable" {
		t.Fatalf("the nested pointer derived %+v, want one row at `callable`", got)
	}
	if _, err := ds.Delete(ctx, owner, agent, "a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err := nested(true); err != nil {
		t.Fatalf("make the object repeated: %v", err)
	}
	// The stored value is the object the old declaration described, which the
	// new one does not read, so the re-derive leaves nothing rather than a row
	// addressed as if the flip had not happened.
	if got := refRows(t, raw, agent, "a1"); len(got) != 0 {
		t.Fatalf("the flip left %+v, want the old address gone", got)
	}
}

// A DELETE keeps the rows and a re-write replaces them: the index mirrors the
// records table, tombstones included, which is what lets a rebuild re-derive
// every row and land on what the live path wrote.
func TestDeleteKeepsTheIndexAndAReWriteReplacesIt(t *testing.T) {
	t.Parallel()
	ds, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()
	if err := refsVocabulary(t, ds, true); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h1"})
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h2"})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s1", Properties: map[string]any{"hub": "h1"},
	})

	if _, err := ds.Delete(ctx, owner, refsSpoke, "s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := refRows(t, raw, refsSpoke, "s1"); len(got) != 1 || got[0].dst != "h1" {
		t.Fatalf("a tombstone lost its rows: %+v", got)
	}

	// Clearing the property clears the row; the write is the only thing that
	// ever removes one.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s1", Properties: map[string]any{"hub": nil},
	})
	if got := refRows(t, raw, refsSpoke, "s1"); len(got) != 0 {
		t.Fatalf("clearing the property left %+v", got)
	}
}

// A VOCABULARY APPLY re-derives the affected kinds' rows. The narrowing guards
// refuse every change that would strand a LIVE value, so what reaches the
// re-derive is the additive case and the TOMBSTONES the counts deliberately do
// not see: a tombstoned record holding a value for a reference the new
// declaration drops would otherwise leave rows projecting a declaration that no
// longer exists.
func TestVocabularyApplyReprojectsTheIndex(t *testing.T) {
	t.Parallel()
	ds, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()
	if err := refsVocabulary(t, ds, true); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h1"})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: refsSpoke, ID: "s1", Properties: map[string]any{"hub": "h1", "hubs": []any{"h1"}},
	})
	if _, err := ds.Delete(ctx, owner, refsSpoke, "s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := refRows(t, raw, refsSpoke, "s1"); len(got) != 2 {
		t.Fatalf("the tombstone holds %+v, want both rows", got)
	}

	// Drop `hub`. No LIVE record holds it, so the narrowing count is zero and
	// the apply is admitted; the tombstone's row must still go.
	if err := refsVocabulary(t, ds, false); err != nil {
		t.Fatalf("drop the reference: %v", err)
	}
	got := refRows(t, raw, refsSpoke, "s1")
	if len(got) != 1 || got[0].property != "hubs" {
		t.Fatalf("the re-projection left %+v, want only the repeated reference", got)
	}
}
