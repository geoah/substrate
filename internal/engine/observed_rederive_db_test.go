package engine_test

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// tiersHeld lists the tiers, in seconds, observed_rollups holds rows of for
// a metric.
func tiersHeld(t *testing.T, r obsIndex, metric string) []int {
	t.Helper()
	rows, err := r.db.Query(`SELECT DISTINCT tier FROM test_rollup_tiers WHERE metric = $1 ORDER BY tier`, r.metric+"/"+metric)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Ints(out)
	return out
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A metric whose `rollups` change has the rows of its streams derived again
// behind the write, in the background, and reads of that metric are refused
// until that is done: then its rows are in the new tiers alone
// and answer what the raw points do. Removing the metric's `rollups` puts its
// streams back on the default tiers the same way; deleting the metric is
// refused while values point at it (decision 0186). Another metric's rows
// are left as they were.
func TestObservedMetricRollupsChangeRederivesItsRows(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	utc := func(time.Time) int { return 0 }
	f.tierHours("attic", "air_temperature", "gauge", "Cel", from, from.Add(30*time.Hour), utc)
	f.tierHours("phone", "health/steps", "amount", "{steps}", from, from.Add(30*time.Hour), utc)
	defaults := []int{900, 3600, 86400}
	if got := tiersHeld(t, f.obsIndex, "air_temperature"); !sameInts(got, defaults) {
		t.Fatalf("the default tiers hold %v, want %v", got, defaults)
	}
	steps := observedTables(t, f.db)
	read := func(name string) {
		t.Helper()
		f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")}, Range: rng(from, from.Add(30*time.Hour)),
			Bucket: &substrate.AggregateBucket{Every: "PT1H"}, Reducers: reducers("avg", "max", "count"),
		})
	}
	f.rollups("air_temperature", "5m", "1d")
	engine.DrainIndexReprojection(t, f.ds)
	if got := tiersHeld(t, f.obsIndex, "air_temperature"); !sameInts(got, []int{300, 86400}) {
		t.Errorf("after the metric named 5m and 1d its rows are in %v", got)
	}
	if got := tiersHeld(t, f.obsIndex, "health/steps"); !sameInts(got, defaults) {
		t.Errorf("another metric's rows moved to %v", got)
	}
	read("5m and 1d")
	checkRollupsMatchSpecTiers(t, f, map[string][]time.Duration{f.metricPath("air_temperature"): {5 * time.Minute, 24 * time.Hour}})
	_, err := f.ds.Delete(context.Background(), owner, f.metric, "air_temperature", substrate.DeleteInput{})
	if !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("deleting a metric values point at: err = %v, want a conflict", err)
	}
	if _, err := f.patch(t, f.metric, "air_temperature", map[string]any{"rollups": nil}); err != nil {
		t.Fatal(err)
	}
	engine.DrainIndexReprojection(t, f.ds)
	if got := observedTables(t, f.db); got != steps {
		t.Errorf("back on the default tiers the index is not the one first written\n%s", firstDifference([]byte(steps), []byte(got)))
	}
}

// While the pass a metric's `rollups` change queued is held, a read naming
// that metric is refused as unavailable, and so is a several-metric read
// naming it among others and a latest read selecting by filter alone, which
// may reach it. A read of another metric of the same kind answers, as does
// a latest read naming that other metric. Once the pass is done the
// changed metric answers again.
func TestObservedMetricRollupsChangeRefusesThatMetricAlone(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	f := aggFix{obsIndex: openObservedIndex(t, engine.WithTestReprojectionHook(func(ctx context.Context, kind string) error {
		if !armed.Load() {
			return nil
		}
		select {
		case held <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})), t: t}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	utc := func(time.Time) int { return 0 }
	f.tierHours("attic", "air_temperature", "gauge", "Cel", from, from.Add(6*time.Hour), utc)
	f.tierHours("phone", "health/steps", "amount", "{steps}", from, from.Add(6*time.Hour), utc)
	engine.DrainIndexReprojection(t, f.ds)
	bucketed := func(metric string) substrate.Aggregate {
		return substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)}, Range: rng(from, from.Add(6*time.Hour)),
			Bucket: &substrate.AggregateBucket{Every: "PT1H"}, Reducers: reducers("max"),
		}
	}
	reads := []struct {
		label     string
		a         substrate.Aggregate
		available bool
	}{
		{"the changed metric", bucketed("air_temperature"), false},
		{"another metric of the kind", bucketed("health/steps"), true},
		{"several metrics, the changed one among them", substrate.Aggregate{
			Metrics: []substrate.AggregateMetricRead{
				{Ref: f.metricPath("health/steps"), Reducers: reducers("sum")},
				{Ref: f.metricPath("air_temperature"), Reducers: reducers("max")},
			},
			Range: rng(from, from.Add(6*time.Hour)), Bucket: &substrate.AggregateBucket{Every: "PT1H"},
		}, false},
		{"latest by filter alone", substrate.Aggregate{Latest: true}, false},
		{"latest of another metric", substrate.Aggregate{Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")}, Latest: true}, true},
	}
	armed.Store(true)
	f.rollups("air_temperature", "5m", "1d")
	select {
	case <-held:
	case <-time.After(time.Minute):
		t.Fatal("the pass behind the rollups change never started")
	}
	for _, r := range reads {
		_, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &r.a})
		switch {
		case r.available && err != nil:
			t.Errorf("%s while the pass is held: %v, want an answer", r.label, err)
		case !r.available && !errors.Is(err, substrate.ErrUnavailable):
			t.Errorf("%s while the pass is held: err = %v, want unavailable", r.label, err)
		}
	}
	close(release)
	engine.DrainIndexReprojection(t, f.ds)
	for _, r := range reads {
		if _, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &r.a}); err != nil {
			t.Errorf("%s once the pass is done: %v", r.label, err)
		}
	}
}

// checkRollupsMatchSpecTiers is checkRollupsMatchSpec for metrics that name
// their own tiers.
func checkRollupsMatchSpecTiers(t *testing.T, f aggFix, tiers map[string][]time.Duration) {
	t.Helper()
	checkRollupsMatchSpecWith(t, f.ds, f.db, func(metric string) []time.Duration {
		if w, ok := tiers[metric]; ok {
			return w
		}
		return specTiers
	})
}

// A rebuild leaves the observed index to the background pass behind it, and
// an aggregate read over a kind the pass still owes is refused as
// unavailable, not answered from a half-derived index; once the pass is done
// the read answers what it did before the rebuild.
func TestObservedRebuildRefusesReadsUntilTheIndexIsDerived(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	f := aggFix{obsIndex: openObservedIndex(t, engine.WithTestReprojectionHook(func(ctx context.Context, kind string) error {
		if !armed.Load() {
			return nil
		}
		select {
		case held <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})), t: t}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.tierHours("attic", "air_temperature", "gauge", "Cel", from, from.Add(6*time.Hour), func(time.Time) int { return 0 })
	q := substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")}, Range: rng(from, from.Add(6*time.Hour)),
		Bucket: &substrate.AggregateBucket{Every: "PT1H"}, Reducers: reducers("avg"),
	}}
	before := f.agg(context.Background(), substrate.Filter{}, *q.Aggregate)
	armed.Store(true)
	if _, err := f.svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	select {
	case <-held:
	case <-time.After(time.Minute):
		t.Fatal("the pass behind the rebuild never started")
	}
	if _, err := f.ds.Aggregate(context.Background(), q); !errors.Is(err, substrate.ErrUnavailable) {
		t.Errorf("a read while the pass is held: err = %v, want unavailable", err)
	}
	close(release)
	engine.DrainIndexReprojection(t, f.ds)
	sameRows(t, f.agg(context.Background(), substrate.Filter{}, *q.Aggregate), before)
}

// `repository reindex --observed` queues every row of every observed kind
// for the background pass, which derives the index again from the records:
// rollup rows, latest rows and segments a fault moved come back as the
// records derive them, the latest row's count included.
func TestObservedReindexRepairsTheIndex(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeObservedHistory(t, ds)
	db := rawDB(t, dsn)
	want := observedTables(t, db)
	for _, q := range []string{
		`UPDATE observed_rollups SET n = n + 100, sum = sum + 1`,
		`UPDATE observed_latest SET points = points + 5, value = value + 1`,
		`UPDATE observed_segments SET points = points + 1`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	report, err := svc.(engine.Operator).ReindexObserved(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if len(report.Kinds) != 2 || report.Records == 0 {
		t.Errorf("reindex queued %+v, want the sample's two observed kinds and their records", report)
	}
	engine.DrainIndexReprojection(t, ds)
	if got := observedTables(t, db); got != want {
		t.Fatalf("the re-derived index is not the one the records derive\n%s", firstDifference([]byte(want), []byte(got)))
	}
}
