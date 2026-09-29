package engine

// A filter by pointer walks an index. A scalar reference property compiles
// to ONE equality on the path expression every reader reads it by
// (referencePathSQL, both stored spellings) against ONE bound text[], and the
// engine builds the btree on that expression for every (kind, scalar
// reference) it declares — nobody declares it. Left to the generic jsonb
// containment index, the planner prices a two-dozen-value `in` at the default
// jsonb selectivity — tens of thousands of rows for a handful — and on a large
// repository chooses a parallel seq scan that runs for tens of seconds. A
// repeated reference keeps its containment probes, and the reverse read
// (`referencing`) walks refs_dst_idx.

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const refIndexPackage = "refindex.test.dev/refindex"

// refIndexDataset declares a target kind and a pointer kind holding one
// scalar and one repeated reference at it, and plants three pointers.
func refIndexDataset(t *testing.T) (*dataset, string, string) {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	target := refIndexPackage + "/target"
	pointer := refIndexPackage + "/pointer"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "refindex", Authority: refIndexPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(refIndexPackage, 0),
			vocabulary.KindManifest(refIndexPackage, map[string]any{"singular": "target"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(refIndexPackage, map[string]any{"singular": "pointer"},
				map[string]any{"properties": map[string]any{
					"note":     map[string]any{"type": "string"},
					"target":   map[string]any{"type": "reference", "kind": target},
					"alsoSeen": map[string]any{"type": "reference", "kind": target, "repeated": true},
				}}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: target, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatalf("put target %s: %v", id, err)
		}
	}
	// Every pointer has also seen `a` (a repeated reference holds each record
	// once, so the one pointing at it names it once).
	for id, at := range map[string]string{"p1": "a", "p2": "b", "p3": "c"} {
		alsoSeen := []any{"a"}
		if at != "a" {
			alsoSeen = append(alsoSeen, at)
		}
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: pointer, ID: id,
			Properties: map[string]any{"note": id, "target": at, "alsoSeen": alsoSeen},
		}); err != nil {
			t.Fatalf("put pointer %s: %v", id, err)
		}
	}
	return ds, target, pointer
}

// explain renders the plan Postgres picks for a records predicate with seq
// scans priced out, so a small table still shows which index the planner CAN
// use: a predicate no index serves stays a (now very expensive) Seq Scan.
func explain(t *testing.T, ds *dataset, from, where string, args []any) string {
	t.Helper()
	ctx := context.Background()
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN (FORMAT TEXT) SELECT id FROM `+from+` WHERE `+where, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

func TestAScalarReferenceFilterIsOneEqualityOnItsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, target, pointer := refIndexDataset(t)

	// The kind declared no `indices:`, and its scalar reference has one all
	// the same: partial on the kind, on the path expression, stamped with its
	// own statement like a declared index so the apply reconciles it.
	name := "idx_" + derivedID(pointer, "ref", "target")
	var def string
	var comment sql.NullString
	if err := ds.db.QueryRowContext(ctx, `
		SELECT indexdef, obj_description(to_regclass($1), 'pg_class') FROM pg_indexes WHERE indexname = $1`,
		name).Scan(&def, &comment); err != nil {
		t.Fatalf("the reference index %s: %v", name, err)
	}
	upper := strings.ToUpper(def)
	if !strings.Contains(upper, "COALESCE") || !strings.Contains(def, "WHERE (kind = '"+pointer+"'") {
		t.Fatalf("the reference index is not the partial path-expression index: %s", def)
	}
	if !comment.Valid || !strings.HasPrefix(comment.String, "CREATE INDEX IF NOT EXISTS "+name) {
		t.Fatalf("the reference index carries no statement comment: %v", comment)
	}
	// The repeated reference gets none: its value is a list the expression
	// does not read, and its filter stays on the containment index.
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = $1`,
		"idx_"+derivedID(pointer, "ref", "alsoSeen")).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("a repeated reference was given a path-expression index")
	}

	// The SQL: one kind binds as a scalar, and the `in` list is ONE equality
	// on the index's expression against ONE text[] — no containment probe,
	// however many values.
	b := &builder{}
	if _, err := ds.buildFilter(ctx, ds.db, b, substrate.Filter{
		Kinds:      []string{pointer},
		Properties: map[string]substrate.Cond{"target": {In: []any{"a", "b"}}},
	}); err != nil {
		t.Fatalf("build filter: %v", err)
	}
	where := strings.Join(b.where, " AND ")
	wantEq := referencePathSQL("props", "target") + ` = ANY($2::text[])`
	if !strings.Contains(where, `kind = $1`) || !strings.Contains(where, wantEq) || strings.Contains(where, "@>") {
		t.Fatalf("the scalar reference filter did not compile to one indexed equality:\n%s", where)
	}
	paths, ok := b.args[1].([]string)
	if !ok || len(paths) != 2 || paths[0] != vocabulary.RecordPath(target, "a") || paths[1] != vocabulary.RecordPath(target, "b") {
		t.Fatalf("the bound path list: %#v", b.args[1])
	}
	// The plan walks the reference index, not the table.
	plan := explain(t, ds, "records", where, b.args)
	t.Logf("plan:\n%s", plan)
	if !strings.Contains(plan, name) {
		t.Fatalf("the plan does not use %s:\n%s", name, plan)
	}
	// And answers the right rows.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{pointer},
		Properties: map[string]substrate.Cond{"target": {In: []any{"a", "b"}}},
	}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := recordIDs(page); got != "p1,p2" {
		t.Fatalf("the list answered %q, want p1,p2", got)
	}
	// `eq` and a full path spell the same clause.
	b = &builder{}
	if _, err := ds.buildFilter(ctx, ds.db, b, substrate.Filter{
		Kinds:      []string{pointer},
		Properties: map[string]substrate.Cond{"target": {Eq: vocabulary.RecordPath(target, "c")}},
	}); err != nil {
		t.Fatalf("build filter: %v", err)
	}
	if where := strings.Join(b.where, " AND "); !strings.Contains(where, wantEq) {
		t.Fatalf("eq on a scalar reference did not compile to the indexed equality:\n%s", where)
	}

	// A REPEATED reference keeps the containment probes, both spellings.
	b = &builder{}
	if _, err := ds.buildFilter(ctx, ds.db, b, substrate.Filter{
		Kinds:      []string{pointer},
		Properties: map[string]substrate.Cond{"alsoSeen": {Eq: "b"}},
	}); err != nil {
		t.Fatalf("build filter: %v", err)
	}
	where = strings.Join(b.where, " AND ")
	if strings.Count(where, "props @> $") != 2 || strings.Contains(where, "= ANY($2") {
		t.Fatalf("the repeated reference filter is not the two containment probes:\n%s", where)
	}
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{pointer},
		Properties: map[string]substrate.Cond{"alsoSeen": {Eq: "a"}},
	}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := recordIDs(page); got != "p1,p2,p3" {
		t.Fatalf("the repeated list answered %q, want p1,p2,p3", got)
	}
}

// The reverse read stands on refs, not on props: its EXISTS walks
// refs_dst_idx by (repository, dst_kind, dst) and never probes a row's
// properties.
func TestTheReferencingArmWalksTheRefsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, target, pointer := refIndexDataset(t)
	b := &builder{}
	if _, err := ds.buildFilter(ctx, ds.db, b, substrate.Filter{
		Kinds:       []string{pointer},
		Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(target, "b")},
	}); err != nil {
		t.Fatalf("build filter: %v", err)
	}
	where := strings.Join(b.where, " AND ")
	if !strings.Contains(where, "FROM refs r") || strings.Contains(where, "@>") {
		t.Fatalf("the referencing arm does not read refs:\n%s", where)
	}
	plan := explain(t, ds, "records", where, b.args)
	if !strings.Contains(plan, "refs_dst_idx") || strings.Contains(plan, "records_props_idx") {
		t.Fatalf("the plan does not walk refs_dst_idx:\n%s", plan)
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(target, "b")},
	}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := recordIDs(page); got != "p2" {
		t.Fatalf("referencing b answered %q, want p2", got)
	}
}

// The list form stays on the same index: several targets of one kind are one
// `dst = ANY` arm, targets of two kinds an OR of arms each naming dst_kind and
// dst, and both plans walk refs_dst_idx. The fixture carries a few hundred
// pointers at another target and fresh statistics, because on a handful of
// unanalyzed rows the planner prices a probe of every source row through
// refs_pkey as cheapest whatever the predicate is.
func TestTheReferencingListWalksTheRefsIndex(t *testing.T) {
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
	for name, tc := range map[string]struct {
		refs []string
		arm  string
		want string
	}{
		"one kind": {
			refs: []string{vocabulary.RecordPath(target, "b"), vocabulary.RecordPath(target, "c")},
			arm:  "r.dst = ANY(", want: "p2,p3",
		},
		"two kinds": {
			refs: []string{vocabulary.RecordPath(target, "b"), vocabulary.RecordPath(pointer, "p1")},
			arm:  ") OR (", want: "p2",
		},
	} {
		b := &builder{}
		if _, err := ds.buildFilter(ctx, ds.db, b, substrate.Filter{
			Kinds:       []string{pointer},
			Referencing: &substrate.Referencing{Refs: tc.refs},
		}); err != nil {
			t.Fatalf("%s: build filter: %v", name, err)
		}
		where := strings.Join(b.where, " AND ")
		if !strings.Contains(where, "FROM refs r") || !strings.Contains(where, tc.arm) {
			t.Fatalf("%s: the referencing arm is not the %q shape:\n%s", name, tc.arm, where)
		}
		plan := explain(t, ds, "records", where, b.args)
		if !strings.Contains(plan, "refs_dst_idx") {
			t.Fatalf("%s: the plan does not walk refs_dst_idx:\n%s", name, plan)
		}
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Referencing: &substrate.Referencing{Refs: tc.refs, Property: "target"},
		}})
		if err != nil {
			t.Fatalf("%s: list: %v", name, err)
		}
		if got := recordIDs(page); got != tc.want {
			t.Fatalf("%s: referencing %v answered %q, want %s", name, tc.refs, got, tc.want)
		}
	}
}

// The batched trail is every identity a pointer may spell and still resolve,
// on read, to a target's canonical record: canonicalOf decides both halves.
// It holds on the trails a live merge never leaves but a rebuild can, a chain
// (the flattening UPDATE is no changelog effect) and a cycle, and across
// kinds in one statement without a trail crossing from one kind to another.
func TestReferencingTrailIsEveryIDResolvingToATarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, target, pointer := refIndexDataset(t)
	at := func(kind string, ids ...string) []eref {
		out := make([]eref, 0, len(ids))
		for _, id := range ids {
			out = append(out, eref{Kind: kind, ID: id})
		}
		return out
	}
	trails := map[eref]string{
		{Kind: target, ID: "x"}: "y", {Kind: target, ID: "y"}: "c",
		{Kind: target, ID: "p"}: "q", {Kind: target, ID: "q"}: "p",
		// The same former id in another kind names another record.
		{Kind: pointer, ID: "x"}: "p1",
	}
	for former, record := range trails {
		if _, err := ds.db.ExecContext(ctx,
			`INSERT INTO former_ids (record_kind, former_id, record_id) VALUES ($1, $2, $3)`,
			former.Kind, former.ID, record); err != nil {
			t.Fatalf("plant former id %s: %v", former.key(), err)
		}
	}
	canonical := func(e eref) eref {
		t.Helper()
		c, err := ds.canonicalOf(ctx, ds.db, e)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	universe := append(at(target, "a", "b", "c", "x", "y", "p", "q", "nowhere"), at(pointer, "x", "p1")...)
	keys := func(es []eref) []string {
		out := make([]string, 0, len(es))
		for _, e := range es {
			out = append(out, e.key())
		}
		sort.Strings(out)
		return out
	}
	resolving := func(targets []eref) []string {
		t.Helper()
		canon := map[eref]bool{}
		for _, e := range targets {
			canon[canonical(e)] = true
		}
		var out []eref
		for _, e := range universe {
			if canon[e] || canon[canonical(e)] {
				out = append(out, e)
			}
		}
		return keys(out)
	}
	for _, targets := range [][]eref{
		at(target, "x"), at(target, "y"), at(target, "c"), at(target, "x", "b"),
		at(target, "p"), at(target, "q", "x", "a"), at(target, "nowhere"),
		at(pointer, "x"), append(at(target, "x"), at(pointer, "x")...),
	} {
		got, err := trailIDs(ctx, ds.db, targets)
		if err != nil {
			t.Fatalf("trail %v: %v", keys(targets), err)
		}
		if want := resolving(targets); !slices.Equal(keys(got), want) {
			t.Fatalf("trail %v = %v, want %v, every identity canonicalOf resolves to a target",
				keys(targets), keys(got), want)
		}
	}
	// End to end: a pointer stored under x, two hops from c, answers a
	// reverse read of c.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pointer, ID: "px", Properties: map[string]any{"target": "x"},
	}); err != nil {
		t.Fatalf("put the pointer at x: %v", err)
	}
	var dst string
	if err := ds.db.QueryRowContext(ctx,
		`SELECT dst FROM refs WHERE src_kind = $1 AND src = 'px' AND property = 'target'`, pointer).Scan(&dst); err != nil {
		t.Fatalf("read the pointer's refs row: %v", err)
	}
	if dst != "x" {
		t.Fatalf("the refs row holds %q; the case needs a pointer indexed under x", dst)
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Referencing: &substrate.Referencing{Refs: []string{vocabulary.RecordPath(target, "c")}, Property: "target"},
	}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := recordIDs(page); got != "p3,px" {
		t.Fatalf("referencing c answered %q, want p3 and the pointer stored under x", got)
	}
}

// A declaration that already indexes exactly the reference's expression is
// not doubled: the declared index is the reference index.
func TestADeclaredReferenceIndexIsNotDoubled(t *testing.T) {
	t.Parallel()
	kind := &vocabulary.Kind{
		Identity:  "dup.test.dev/dup/row",
		PropOrder: []string{"owner", "seen"},
		Props: map[string]*vocabulary.Property{
			"owner": {Datatype: vocabulary.DatatypeReference, To: "dup.test.dev/dup/who"},
			"seen":  {Datatype: vocabulary.DatatypeReference, To: "dup.test.dev/dup/who", Repeated: true},
		},
		Indices: [][]string{{"owner"}},
	}
	stmts, err := indexStatements([]*vocabulary.Kind{kind})
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 || stmts[0].name != "idx_"+derivedID(kind.Identity, "0") {
		t.Fatalf("a declared [owner] index was doubled by the reference index: %+v", stmts)
	}
	// Declared on something else, the reference gets its own.
	kind.Indices = [][]string{{"owner", "seen"}}
	if stmts, err = indexStatements([]*vocabulary.Kind{kind}); err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 || stmts[1].name != "idx_"+derivedID(kind.Identity, "ref", "owner") {
		t.Fatalf("the reference index is missing beside a compound declaration: %+v", stmts)
	}
	want := `CREATE INDEX IF NOT EXISTS ` + stmts[1].name + ` ON records (repository, (` +
		referencePathSQL("props", "owner") + `)) WHERE kind = 'dup.test.dev/dup/row'`
	if stmts[1].stmt != want {
		t.Fatalf("the reference index statement:\n got %s\nwant %s", stmts[1].stmt, want)
	}
}

// recordIDs is a page's ids, sorted: the fixture plants its pointers in map
// order, so the list's own (created_at) order is not the assertion.
func recordIDs(page *substrate.Page) string {
	ids := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}
