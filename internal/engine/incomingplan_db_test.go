package engine

// THE REVERSE READ'S PLAN, explained on the statement `Incoming` actually
// sends. This test is INTERNAL to the package for exactly that reason: it
// builds the page SQL through incomingPageSQL, the one function the reader
// itself calls, and runs it on the dataset's own pool, so the role, the row
// level security predicate and the join are production's. A hand-written
// approximation of the query would have kept passing while the real one sorted.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	planPackage = "incomingplan.example.substrate.reamde.dev/incomingplan"
	planHub     = planPackage + "/hub"
	planSpoke   = planPackage + "/spoke"
)

// planDataset provisions a repository holding one hub and a few spokes that
// point at it, and hands back the dataset behind the substrate.Dataset
// interface so the internals under test are reachable.
func planDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svc, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		WithCredentialKey(TestCredentialKey))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	generic, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := generic.(*dataset)
	if !ok {
		t.Fatalf("the service handed back a %T", generic)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(planPackage, 0),
		vocabulary.KindManifest(planPackage,
			map[string]any{"singular": "hub", "plural": "hubs"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(planPackage,
			map[string]any{"singular": "spoke", "plural": "spokes"},
			map[string]any{"properties": map[string]any{
				"hub": map[string]any{"type": "reference", "kind": planHub},
			}}),
	}); err != nil {
		t.Fatalf("install the vocabulary: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: planHub, ID: "h1"}); err != nil {
		t.Fatalf("write the hub: %v", err)
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: planSpoke, ID: id, Properties: map[string]any{"hub": "h1"},
		}); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}
	return ds
}

// explainPlan runs EXPLAIN on one statement with sequential scans disabled, in
// a transaction of its own so the setting is `SET LOCAL` and never escapes onto
// a pooled connection. Seqscan is off because a four-row table is faster to
// scan than to seek, which says nothing about the shape a target with tens of
// thousands of pointers meets — the question here is whether the index CAN
// answer the page in key order, not what the planner picks at this size.
func explainPlan(t *testing.T, ds *dataset, query string, args []any) string {
	t.Helper()
	ctx := context.Background()
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan the plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the plan: %v", err)
	}
	return plan.String()
}

// THE PAGE IS AN INDEX-ORDERED SCAN when the target answers to one id, which is
// every record that has never been merged into. refs_dst_idx carries the match
// key and then the ordering key, so the page reads rows in ORDER BY order and
// stops at the limit: no sort node, and nothing proportional to the fan-in.
//
// The target predicate has to be a SCALAR for that. `r.dst IN (SELECT
// jsonb_array_elements_text($n))` is a semi-join against a set the planner
// cannot size, and it will not walk the index in key order for it — a target
// with tens of thousands of pointers top-N sorts its whole match set on every
// page.
func TestIncomingPageIsAnIndexOrderedScan(t *testing.T) {
	t.Parallel()
	ds := planDataset(t)
	target := eref{Kind: planHub, ID: "h1"}

	b := &builder{}
	query := ds.incomingPageSQL(b, target, []string{"h1"}, substrate.IncomingOptions{}, nil, 26)
	plan := explainPlan(t, ds, query, b.args)

	if strings.Contains(plan, "Sort") {
		t.Fatalf("the page sorts its match set:\n%s", plan)
	}
	if !strings.Contains(plan, "refs_dst_idx") {
		t.Fatalf("the page did not read refs_dst_idx:\n%s", plan)
	}
	// The narrowed page and the second page take the same shape: the property
	// filter and the keyset seek are index columns too.
	b = &builder{}
	query = ds.incomingPageSQL(b, target, []string{"h1"},
		substrate.IncomingOptions{Property: "hub", FromKind: planSpoke},
		&incomingSeek{srcKind: planSpoke, src: "s1", property: "hub"}, 26)
	if plan := explainPlan(t, ds, query, b.args); strings.Contains(plan, "Sort") {
		t.Fatalf("a narrowed continuation page sorts:\n%s", plan)
	}
}

// A FORMER-ID TRAIL KEEPS THE SET FORM, and the set form may sort. It is the
// price of correctness — a pointer written before a merge names an id the
// record no longer answers to, and only the set matches those rows — and it is
// rare by construction: the trail grows one id per merge, and a record that has
// been merged into is not the fan-in hot spot. Pinned so the trade is a
// decision somebody made rather than a plan nobody looked at.
func TestIncomingPageOverAFormerIDTrailMatchesEveryID(t *testing.T) {
	t.Parallel()
	ds := planDataset(t)
	target := eref{Kind: planHub, ID: "h1"}

	b := &builder{}
	query := ds.incomingPageSQL(b, target, []string{"h1", "h0"}, substrate.IncomingOptions{}, nil, 26)
	plan := explainPlan(t, ds, query, b.args)
	if !strings.Contains(plan, "refs") {
		t.Fatalf("the multi-id page did not read the index at all:\n%s", plan)
	}
	// And it answers: the rows a merge left behind are what the set is for.
	rows, err := ds.db.QueryContext(context.Background(), query, b.args...)
	if err != nil {
		t.Fatalf("run the multi-id page: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var n int
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the multi-id page: %v", err)
	}
	if n != 3 {
		t.Fatalf("the multi-id page returned %d rows, want the 3 spokes", n)
	}
}
