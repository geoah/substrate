package engine_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// Two repositories write the same observed records, same kinds, same ids,
// same metric paths, with different values. Under the app role each sees
// its own streams, segments, rollup rows and latest rows alone, can neither
// change nor delete the other's, and is refused a row forged into the
// other's; each repository's reads answer its own values, before and after
// the other is rebuilt and its index derived again behind the rebuild.
func TestObservedIndexIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	names := []string{"alpha.example.com", "beta.example.com"}
	datasets := map[string]substrate.Dataset{}
	ids := map[string]string{}
	for i, name := range names {
		info, err := svc.CreateRepository(ctx, name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids[name] = info.ID
		ds, err := svc.Dataset(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		datasets[name] = ds
		importVocabulary(t, ds, "observations")
		for h := range 3 {
			values := []any{}
			for j := range 6 {
				values = append(values, fmt.Sprintf("%d.%d", 20+10*i+h, j))
			}
			mustPut(t, ds, owner, substrate.PutInput{Kind: obsSampleTrace, ID: fmt.Sprintf("shared/%d", h), Properties: map[string]any{
				"metric": map[string]any{"ref": obsSampleMetric + "/air_temperature"}, "form": "gauge", "unit": "Cel",
				"at": fmt.Sprintf("2026-10-07T%02d:00:00Z", 6+h), "endsAt": fmt.Sprintf("2026-10-07T%02d:00:00Z", 7+h),
				"every": "PT10M", "values": values, "utcOffset": 180,
			}})
		}
	}
	latest := func(name string) string {
		t.Helper()
		res, err := datasets[name].Aggregate(ctx, substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: obsSampleMetric + "/air_temperature"}, Latest: true,
		}})
		if err != nil {
			t.Fatalf("%s latest: %v", name, err)
		}
		if len(res.Rows) != 1 || len(res.Rows[0].Values) != 1 || res.Rows[0].Values[0].Value == nil {
			t.Fatalf("%s latest: %+v", name, res.Rows)
		}
		return *res.Rows[0].Values[0].Value + fmt.Sprintf("/%d", res.Rows[0].Count)
	}
	want := map[string]string{"alpha.example.com": "22.5/18", "beta.example.com": "32.5/18"}
	tables := []string{"observed_streams", "observed_segments", "observed_rollups", "observed_latest"}
	check := func(stage string) {
		t.Helper()
		for _, name := range names {
			if got := latest(name); got != want[name] {
				t.Errorf("%s: %s reads %s, want %s", stage, name, got, want[name])
			}
			raw, err := engine.OpenScopedDB(dsn, ids[name], engine.RoleApp)
			if err != nil {
				t.Fatalf("open a scoped pool: %v", err)
			}
			other := ids[names[0]]
			if name == names[0] {
				other = ids[names[1]]
			}
			for _, table := range tables {
				var own, foreign int
				if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&own); err != nil {
					t.Fatalf("%s: %v", table, err)
				}
				if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE repository = $1`, other).Scan(&foreign); err != nil {
					t.Fatalf("%s: %v", table, err)
				}
				if own == 0 || foreign != 0 {
					t.Errorf("%s: %s sees %d rows of %s and %d of the other repository's", stage, name, own, table, foreign)
				}
				for _, q := range []string{`UPDATE ` + table + ` SET repository = repository WHERE repository = $1`, `DELETE FROM ` + table + ` WHERE repository = $1`} {
					res, err := raw.ExecContext(ctx, q, other)
					if err != nil {
						t.Fatalf("%s: %v", q, err)
					}
					if n, _ := res.RowsAffected(); n != 0 {
						t.Errorf("%s: %s touched %d of the other repository's rows", stage, q, n)
					}
				}
			}
			var segments int
			if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM observed_segments WHERE record_id LIKE 'shared/%'`).Scan(&segments); err != nil {
				t.Fatal(err)
			}
			if segments != 3 {
				t.Errorf("%s: %s sees %d segments at the shared ids, want its own 3", stage, name, segments)
			}
			for _, q := range []string{
				`INSERT INTO observed_streams (repository, metric, kind) VALUES ($1, 'm', 'k')`,
				`INSERT INTO observed_segments (repository, kind, record_id, stream, form, first_ts, last_ts, end_ts, points, available,
					utc_offset_min, offset_known, codes, fingerprint) VALUES ($1, 'k', 'forged', 1, 'gauge', now(), now(), now(), 0, 0, 0, true, '{}', '\x00')`,
				`INSERT INTO observed_rollups (repository, stream, form, tier, bucket, code, unit, delta, utc_offset_min, offset_known,
					n, bounded, covered_ms, max_gap_ms, start_gap_ms, end_gap_ms) VALUES ($1, 1, 'gauge', 900, now(), '', '', false, 0, true, 0, 0, 0, 0, 0, 0)`,
				`INSERT INTO observed_latest (repository, stream, ts, ts_end, code, unit, delta, form, comparator, utc_offset_min,
					offset_known, record_id, points) VALUES ($1, 1, now(), now(), '', '', false, 'gauge', '', 0, true, 'forged', 1)`,
			} {
				_, err := raw.ExecContext(ctx, q, other)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Errorf("%s: a row forged into the other repository: err = %v, want a row-security violation (42501)", stage, err)
				}
			}
			_ = raw.Close()
		}
	}
	check("as written")
	if _, err := svc.(rebuilder).RebuildRepository(ctx, ids[names[0]]); err != nil {
		t.Fatalf("rebuild %s: %v", names[0], err)
	}
	engine.DrainIndexReprojection(t, datasets[names[0]])
	check("after the first repository's rebuild")
}
