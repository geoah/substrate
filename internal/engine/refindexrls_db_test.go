package engine

// A list filtered by a scalar reference reads refs, not the row's properties.
// Row level security holds every records read to its repository, and Postgres
// runs a clause it cannot prove leakproof only on the rows the policy admits:
// `->` and `->>` on jsonb are not leakproof, so the path expression never
// reached the kind's reference index and the filter read, and detoasted, every
// row of the kind. A Gmail mirror's per-thread reads cost a scan of every
// stored message, payloads included, each, and a history page that emptied a
// hundred threads took a scheduled fire past its minute (2026-10-06).

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAListByAScalarReferenceWalksTheRefsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, target, pointer := refIndexDataset(t)
	for i := range 300 {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: pointer, ID: "bulk" + strconv.Itoa(i), Properties: map[string]any{"target": "a"},
		}); err != nil {
			t.Fatalf("put bulk pointer %d: %v", i, err)
		}
	}
	for _, table := range []string{"refs", "records"} {
		if _, err := ds.svc.admin.ExecContext(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	list := func(filter substrate.Filter) string {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{First: 500, Filter: filter})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return recordIDs(page)
	}
	byTarget := func(c substrate.Cond) substrate.Filter {
		return substrate.Filter{Kinds: []string{pointer}, Properties: map[string]substrate.Cond{"target": c}}
	}

	// The SQL a list builds reads refs at the reference's own site, and the
	// plan reaches the target through refs_dst_idx with no row's properties
	// read to decide it. ds.db is the application's pool, under the policy.
	b := &builder{overRecords: true}
	if _, err := ds.buildFilter(ctx, ds.db, b, byTarget(substrate.Cond{In: []any{"b", "c"}})); err != nil {
		t.Fatalf("build filter: %v", err)
	}
	where := strings.Join(b.where, " AND ")
	if !strings.Contains(where, "FROM refs r") || strings.Contains(where, referencePathSQL("props", "target")) {
		t.Fatalf("the list's scalar reference filter does not read refs:\n%s", where)
	}
	plan := explain(t, ds, "records", where, b.args)
	if !strings.Contains(plan, "refs_dst_idx") || strings.Contains(plan, "props") {
		t.Fatalf("the plan does not reach the targets through refs_dst_idx alone:\n%s", plan)
	}

	// The rows: one target, two, none, and a filter naming no kind.
	if got := list(byTarget(substrate.Cond{Eq: "b"})); got != "p2" {
		t.Fatalf("target b answered %q, want p2", got)
	}
	if got := list(byTarget(substrate.Cond{In: []any{"b", "c"}})); got != "p2,p3" {
		t.Fatalf("target in [b, c] answered %q, want p2,p3", got)
	}
	if got := list(byTarget(substrate.Cond{Eq: "nowhere"})); got != "" {
		t.Fatalf("target nowhere answered %q, want nothing", got)
	}
	if got := list(substrate.Filter{Properties: map[string]substrate.Cond{
		"target": {Eq: vocabulary.RecordPath(target, "c")},
	}}); got != "p3" {
		t.Fatalf("a kindless filter on target c answered %q, want p3", got)
	}

	// A pointer stored under a target's former id answers a filter naming
	// the record it became.
	if _, err := ds.db.ExecContext(ctx,
		`INSERT INTO former_ids (record_kind, former_id, record_id) VALUES ($1, 'old-c', 'c')`, target); err != nil {
		t.Fatalf("plant a former id: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pointer, ID: "pold", Properties: map[string]any{"target": "old-c"},
	}); err != nil {
		t.Fatalf("put the pointer at the former id: %v", err)
	}
	if got := list(byTarget(substrate.Cond{Eq: "c"})); got != "p3,pold" {
		t.Fatalf("target c answered %q, want p3,pold", got)
	}

	// While a re-derivation still owes the kind's refs rows for the property,
	// the row's own properties decide: p2's refs rows are gone here, as a
	// declaration change the pass has not reached would leave them, and the
	// filter still finds it. Once nothing is owed, refs is the answer.
	if _, err := ds.db.ExecContext(ctx,
		`DELETE FROM refs WHERE src_kind = $1 AND src = 'p2'`, pointer); err != nil {
		t.Fatalf("drop p2's refs rows: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO index_reprojections (kind, refs, refs_properties, fts) VALUES ($1, true, ARRAY['target'], false)`,
		pointer); err != nil {
		t.Fatalf("owe a re-derivation: %v", err)
	}
	if got := list(byTarget(substrate.Cond{Eq: "b"})); got != "p2" {
		t.Fatalf("with refs owed, target b answered %q, want p2 off its properties", got)
	}
	if _, err := ds.db.ExecContext(ctx, `DELETE FROM index_reprojections WHERE kind = $1`, pointer); err != nil {
		t.Fatal(err)
	}
	if got := list(byTarget(substrate.Cond{Eq: "b"})); got != "" {
		t.Fatalf("with nothing owed, target b answered %q, want refs alone to decide", got)
	}
}
