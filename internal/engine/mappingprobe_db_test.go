package engine

// A mapping probe finds its candidates through records_props_idx. Row level
// security runs a clause it cannot prove leakproof only on the rows its policy
// admits, and jsonb `->>` and `@>` are not leakproof, so a probe written
// against `records` read every row of its target kind inside the write
// transaction, under the changelog lock. An exact probe reads
// records_containing (migration 0012) instead, which applies the policy's
// predicate itself.

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	probePackage = "probe.example.substrate.reamde.dev/probe"
	probeSubject = probePackage + "/subject"
	probeOther   = probePackage + "/other"
)

// probeVocabulary declares a subject with a scalar and a repeated string
// property, and a second kind carrying the same scalar.
func probeVocabulary() []map[string]any {
	props := map[string]any{
		"url":    map[string]any{"type": "string"},
		"emails": map[string]any{"type": "string", "repeated": true},
	}
	return []map[string]any{
		vocabulary.PackageManifest(probePackage, 0),
		vocabulary.KindManifest(probePackage, map[string]any{"singular": "subject"}, map[string]any{"properties": props}),
		vocabulary.KindManifest(probePackage, map[string]any{"singular": "other"}, map[string]any{"properties": props}),
	}
}

// rowProbe is the probe as it was written before records_containing: the
// predicate on the row, under the policy. It is the oracle the indexed probe
// must agree with.
func rowProbe(t *testing.T, ds *dataset, kind, prop string, repeated bool, v string) []string {
	t.Helper()
	ctx := context.Background()
	q := `SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL AND props->>$2 = $3 ORDER BY id`
	var arg any = v
	if repeated {
		q = `SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL AND props->$2 @> $3::jsonb ORDER BY id`
		arg = fmt.Sprintf(`[%q]`, v)
	}
	rows, err := ds.db.QueryContext(ctx, q, kind, prop, arg)
	if err != nil {
		t.Fatalf("row probe: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func indexedProbe(t *testing.T, ds *dataset, kind, prop string, repeated bool, v string) []string {
	t.Helper()
	var out []string
	if err := ds.inTx(context.Background(), substrate.ActorAPI, true, func(tx *txn) error {
		var err error
		out, err = tx.probeCandidates(kind, &vocabulary.Property{Name: prop, Repeated: repeated}, []string{v}, false)
		return err
	}); err != nil {
		t.Fatalf("probe %s=%s: %v", prop, v, err)
	}
	return out
}

func TestAnExactProbeAnswersWhatTheRowPredicateDid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	other := secondRepository(t, ds)
	for _, d := range []*dataset{ds, other} {
		if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, probeVocabulary()); err != nil {
			t.Fatalf("apply the probe vocabulary: %v", err)
		}
	}
	put := func(d *dataset, kind, id, url string, emails ...string) {
		t.Helper()
		props := map[string]any{"url": url}
		if emails != nil {
			items := make([]any, len(emails))
			for i, e := range emails {
				items[i] = e
			}
			props["emails"] = items
		}
		if _, err := d.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	put(ds, probeSubject, "b", "u-1", "shared@example.com", "B@example.com")
	put(ds, probeSubject, "a", "u-1", "a@example.com", "shared@example.com")
	put(ds, probeSubject, "c", "u-2")
	put(ds, probeSubject, "gone", "u-3", "gone@example.com")
	if _, err := ds.Delete(ctx, substrate.ActorAPI, probeSubject, "gone", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	put(ds, probeOther, "o", "u-2", "a@example.com")
	put(other, probeSubject, "elsewhere", "u-1", "a@example.com")

	for _, tc := range []struct {
		prop     string
		repeated bool
		value    string
		want     []string
	}{
		{"url", false, "u-1", []string{"a", "b"}},
		{"url", false, "u-2", []string{"c"}},
		{"url", false, "U-1", nil},
		{"url", false, "u-3", nil},
		{"emails", true, "shared@example.com", []string{"a", "b"}},
		{"emails", true, "a@example.com", []string{"a"}},
		{"emails", true, "b@example.com", nil},
		{"emails", true, "gone@example.com", nil},
		// A probe whose shape is not the stored value's finds nothing either
		// way: a scalar compared with a list, and a list with a scalar.
		{"emails", false, "a@example.com", nil},
		{"url", true, "u-1", nil},
	} {
		got := indexedProbe(t, ds, probeSubject, tc.prop, tc.repeated, tc.value)
		if !slices.Equal(got, tc.want) {
			t.Errorf("probe %s=%q (repeated %v) = %v, want %v", tc.prop, tc.value, tc.repeated, got, tc.want)
		}
		if row := rowProbe(t, ds, probeSubject, tc.prop, tc.repeated, tc.value); !slices.Equal(got, row) {
			t.Errorf("probe %s=%q (repeated %v) = %v, the row predicate found %v", tc.prop, tc.value, tc.repeated, got, row)
		}
	}
	if got := indexedProbe(t, other, probeSubject, "url", false, "u-1"); !slices.Equal(got, []string{"elsewhere"}) {
		t.Errorf("the other repository's probe = %v, want its own record alone", got)
	}
}

func TestAnExactProbeSeeksThePropsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds := openCoreDataset(t, WithTestQueryTracer(log))
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, probeVocabulary()); err != nil {
		t.Fatalf("apply the probe vocabulary: %v", err)
	}
	// Enough rows of the probed kind that a read of the kind shows in the
	// plan as the rows it filtered out.
	const n = 1000
	if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		for i := range n {
			if _, err := tx.put(substrate.PutInput{
				Kind: probeSubject, ID: fmt.Sprintf("s%04d", i),
				Properties: map[string]any{
					"url":    fmt.Sprintf("u-%d", i),
					"emails": []any{fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("q%d@example.com", i)},
				},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	analyzeTables(t, ds)

	// The function's own statement, planned the way a SQL function plans it
	// (generic, its arguments unknown) under the role that owns it.
	var body, owner string
	var definer bool
	if err := ds.svc.maint.QueryRowContext(ctx, `
		SELECT prosrc, pg_get_userbyid(proowner), prosecdef FROM pg_proc
		WHERE proname = 'records_containing' AND pronamespace = current_schema()::regnamespace`).Scan(&body, &owner, &definer); err != nil {
		t.Fatalf("read records_containing: %v", err)
	}
	if owner != roleMaint || !definer {
		t.Fatalf("records_containing owner=%s definer=%v, want %s and SECURITY DEFINER", owner, definer, roleMaint)
	}
	plan := func(q loggedQuery) string {
		t.Helper()
		tx, err := ds.svc.maint.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		for _, stmt := range []string{
			`SELECT set_config('substrate.repository', ` + sqlLiteral(ds.scope.Repository) + `, true)`,
			`SET LOCAL plan_cache_mode = force_generic_plan`,
			`PREPARE probe_body(text, jsonb) AS ` + body,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
		defer func() { _, _ = tx.ExecContext(ctx, `DEALLOCATE probe_body`) }()
		// The probe's own arguments are the last two the driver was handed,
		// after its result-format option.
		args := q.args[len(q.args)-2:]
		return explainPlan(t, ctx, tx, `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) EXECUTE probe_body(`+
			sqlLiteral(fmt.Sprintf("%s", args[0]))+`, `+sqlLiteral(fmt.Sprintf("%s", args[1]))+`)`)
	}

	for _, tc := range []struct {
		prop     string
		repeated bool
		value    string
		want     string
	}{
		{"url", false, "u-421", "s0421"},
		{"emails", true, "q421@example.com", "s0421"},
	} {
		var got []string
		q := log.sent(t, "records_containing", func() {
			got = indexedProbe(t, ds, probeSubject, tc.prop, tc.repeated, tc.value)
		})
		if !slices.Equal(got, []string{tc.want}) {
			t.Fatalf("probe %s=%q = %v, want [%s]", tc.prop, tc.value, got, tc.want)
		}
		assertSeeksRefs(t, "the "+tc.prop+" probe", plan(q), 2, "records_props_idx")
	}
}
