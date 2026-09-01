package engine_test

// THE REFS INDEX as storage: what the migration leaves behind, what a
// re-derive preserves, and what a declaration change re-projects. The
// derivation itself is pure and tested without a database
// (refs_internal_test.go); this is the half that needs one.

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
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
func TestTheRefsIndexCoversTheIncomingSort(t *testing.T) {
	t.Parallel()
	ds, raw, _ := newDatasetWithDB(t)
	ctx := context.Background()
	if err := refsVocabulary(t, ds, true); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: refsHub, ID: "h1"})
	for _, id := range []string{"s1", "s2", "s3"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: refsSpoke, ID: id, Properties: map[string]any{"hub": "h1"},
		})
	}

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

	// And the planner uses it that way: the page's ORDER BY is the index's own
	// order, so the plan carries no sort node. Sequential scans are disabled
	// because a three-row table is faster to scan than to seek, which says
	// nothing about the shape a hot target meets.
	if _, err := raw.ExecContext(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := raw.QueryContext(ctx, `
		EXPLAIN SELECT r.property, r.path, r.ord, r.src_kind, r.src FROM refs r
		WHERE r.dst_kind = $1 AND r.dst = $2
		ORDER BY r.src_kind, r.src, r.property, r.path, r.ord LIMIT 2`, refsHub, "h1")
	if err != nil {
		t.Fatalf("explain the incoming page: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan the plan: %v", err)
		}
		plan += line + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the plan: %v", err)
	}
	if strings.Contains(plan, "Sort") {
		t.Fatalf("the incoming page sorts its match set:\n%s", plan)
	}
	if !strings.Contains(plan, "refs_dst_idx") {
		t.Fatalf("the incoming page did not read the index:\n%s", plan)
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
