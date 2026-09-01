package engine_test

// THE REFS INDEX as storage: what the migration leaves behind, what a
// re-derive preserves, and what a declaration change re-projects. The
// derivation itself is pure and tested without a database
// (refs_internal_test.go); this is the half that needs one.

import (
	"context"
	"database/sql"
	"testing"
	"time"

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
	createdAt      time.Time
}

func refRows(t *testing.T, raw *sql.DB, kind, id string) []refRowRead {
	t.Helper()
	rows, err := raw.QueryContext(context.Background(),
		`SELECT property, path, ord, dst, created_at FROM refs
		 WHERE src_kind = $1 AND src = $2 ORDER BY property, path, ord`, kind, id)
	if err != nil {
		t.Fatalf("read the refs index: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []refRowRead
	for rows.Next() {
		var r refRowRead
		if err := rows.Scan(&r.property, &r.path, &r.ord, &r.dst, &r.createdAt); err != nil {
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

// A RE-DERIVE PRESERVES created_at for a pointer that came through unchanged.
// The match is on (property, path, dst) and NOT on `ord`: reordering a repeated
// reference moves a value between ordinals without the record pointing anywhere
// new, and re-stamping would say the pointer was just made.
func TestReDerivePreservesCreatedAt(t *testing.T) {
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
	born := map[string]time.Time{}
	for _, r := range refRows(t, raw, refsSpoke, "s1") {
		born[r.property+"|"+r.path+"|"+r.dst] = r.createdAt
	}
	if len(born) != 3 {
		t.Fatalf("the first write derived %d rows, want 3", len(born))
	}

	// Enough of a gap that a re-stamp would be visible.
	time.Sleep(5 * time.Millisecond)
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
	for _, r := range after {
		key := r.property + "|" + r.path + "|" + r.dst
		was, survived := born[key]
		switch {
		case survived && !r.createdAt.Equal(was):
			t.Fatalf("%s was re-stamped: %v -> %v", key, was, r.createdAt)
		case !survived && !r.createdAt.After(born["hub||h1"]):
			t.Fatalf("%s is new and carries an old creation: %v", key, r.createdAt)
		}
	}
	// And the ordinals followed the values.
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
