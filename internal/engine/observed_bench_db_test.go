package engine_test

// The benchmark gate of the observed index (the plan's finding 14): the whole
// write and rebuild path measured together, on real tables, before the rollup
// layout is frozen. It is gated on SUBSTRATE_BENCH_OBSERVED=1 and skipped
// otherwise, because it writes a month of three streams and takes minutes:
//
//	SUBSTRATE_BENCH_OBSERVED=1 go test -count=1 -v -run '^TestObservedIndexBenchmark$' ./internal/engine/
//
// docs/plans/observations/implementation/benchmark.md records what it printed.

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const benchDays = 30

var benchStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

type benchPhase struct {
	name                     string
	records, points          int
	rollups                  int
	changelogBytes, walBytes int64
	took                     time.Duration
}

func TestObservedIndexBenchmark(t *testing.T) {
	if os.Getenv("SUBSTRATE_BENCH_OBSERVED") != "1" {
		t.Skip("set SUBSTRATE_BENCH_OBSERVED=1 to run the observed index benchmark")
	}
	r := openObservedIndex(t)
	ctx := context.Background()
	repo := testdb.Repository(t)
	app, err := engine.SharedScopedDB(r.svc, repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	dir, err := changelogfile.RepoDir(engine.DataRootOf(r.svc), repo)
	if err != nil {
		t.Fatal(err)
	}
	segments := changelogfile.ChangelogDir(dir)

	trace := func(metric, unit, form, every string, at time.Time, n int, value func(i int) string, rev int) map[string]any {
		values := make([]any, n)
		for i := range values {
			values[i] = value(i)
		}
		props := map[string]any{
			"metric": r.metricRef(metric), "source": r.sourceRef("garmin-forerunner-265"), "form": form,
			"unit": unit, "at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339),
			"every": every, "values": values, "utcOffsetStart": 180, "utcOffsetEnd": 180, "revision": rev,
		}
		if form == "gauge" {
			props["hold"] = "PT2M"
		}
		return props
	}
	// The three streams of one hour: a 1-minute heart rate, a 15-minute step
	// amount, and for two hours a day a 1-second power trace. Each put's wall
	// time is added to its stream's.
	perStream := map[string]time.Duration{}
	perStreamPuts := map[string]int{}
	hour := func(at time.Time, rev int) (n, points int) {
		seed := at.Unix()/3600 + int64(rev)
		put := func(kind, id string, props map[string]any, pts int) {
			started := time.Now()
			if _, err := r.ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
				t.Fatalf("put %s: %v", id, err)
			}
			stream := strings.Split(id, "/")[1]
			perStream[stream] += time.Since(started)
			perStreamPuts[stream]++
			n++
			points += pts
		}
		stamp := at.Format("2006-01-02T15")
		put(r.trace, "garmin/hr/"+stamp, trace("health/heart_rate", "/min", "gauge", "PT1M", at, 60,
			func(i int) string { return fmt.Sprint(55 + (int64(i)*7+seed)%40) }, rev), 60)
		put(r.trace, "garmin/steps/"+stamp, trace("health/steps", "{steps}", "amount", "PT15M", at, 4,
			func(i int) string { return fmt.Sprint((int64(i)*131 + seed*17) % 900) }, rev), 4)
		if h := at.Hour(); h == 7 || h == 18 {
			put(r.trace, "garmin/power/"+stamp, trace("health/running_power", "W", "gauge", "PT1S", at, 3600,
				func(i int) string { return fmt.Sprintf("%d.%d", 180+(int64(i)*13+seed)%140, i%10) }, rev), 3600)
		}
		return n, points
	}

	var phases []benchPhase
	measure := func(name string, run func() (records, points int)) {
		t.Helper()
		wal0 := walLSN(t, r.db)
		started := time.Now()
		records, points := run()
		took := time.Since(started)
		phases = append(phases, benchPhase{
			name: name, records: records, points: points,
			rollups:        r.count(t, `SELECT count(*) FROM observed_rollups`),
			changelogBytes: dirBytes(t, segments),
			walBytes:       walSince(t, r.db, wal0),
			took:           took,
		})
	}

	measure(fmt.Sprintf("write %d days", benchDays), func() (int, int) {
		var records, points int
		for h := range benchDays * 24 {
			n, p := hour(benchStart.Add(time.Duration(h)*time.Hour), 1)
			records, points = records+n, points+p
		}
		return records, points
	})
	var streams strings.Builder
	fmt.Fprintf(&streams, "\n| Stream, write phase | Puts | Points per put | Mean wall time per put |\n| --- | ---: | ---: | ---: |\n")
	for _, s := range []struct {
		name   string
		points int
	}{{"hr", 60}, {"steps", 4}, {"power", 3600}} {
		mean := perStream[s.name] / time.Duration(max(perStreamPuts[s.name], 1))
		fmt.Fprintf(&streams, "| %s | %d | %d | %s |\n", s.name, perStreamPuts[s.name], s.points, mean.Round(10*time.Microsecond))
	}
	// The footprint and the reads, on the month as written: no dead rows, and
	// the planner's statistics fresh.
	for _, table := range []string{"records", "observed_points", "observed_rollups", "observed_latest"} {
		if _, err := r.db.Exec(`VACUUM ANALYZE ` + table); err != nil {
			t.Fatal(err)
		}
	}
	var footprint strings.Builder
	fmt.Fprintf(&footprint, "\n| Table, after the write | Rows | Total bytes | Bytes per row, with indexes | Bytes per row, heap tuple |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, table := range []string{"records", "observed_points", "observed_rollups", "observed_latest"} {
		var bytes int64
		var tuple float64
		if err := r.db.QueryRow(`SELECT pg_total_relation_size($1::regclass), coalesce((SELECT avg(pg_column_size(x.*)) FROM `+table+` x), 0)`,
			table).Scan(&bytes, &tuple); err != nil {
			t.Fatal(err)
		}
		rows := r.count(t, `SELECT count(*) FROM `+table)
		fmt.Fprintf(&footprint, "| %s | %d | %d | %d | %.0f |\n", table, rows, bytes, bytes/int64(max(rows, 1)), tuple)
	}
	var reads strings.Builder
	from, to := benchStart, benchStart.Add(benchDays*24*time.Hour)
	fmt.Fprintf(&reads, "\n| Read, %d days, daily mean per stream | Rows read | Wall time |\n| --- | ---: | ---: |\n", benchDays)
	for _, m := range []string{"health/heart_rate", "health/running_power"} {
		metric := r.metric + "/" + m
		rows, took := timedRead(t, app, `
			SELECT date_trunc('day', bucket15) AS day, sum(sum) / sum(n), sum(n)
			FROM observed_rollups WHERE metric = $1 AND bucket15 >= $2 AND bucket15 < $3
			GROUP BY 1 ORDER BY 1`, metric, from, to)
		fmt.Fprintf(&reads, "| %s from rollups | %d | %s |\n", m, rows, took)
		rows, took = timedRead(t, app, `
			SELECT date_trunc('day', ts) AS day, avg(value), count(*)
			FROM observed_points WHERE metric = $1 AND ts >= $2 AND ts < $3 AND value IS NOT NULL
			GROUP BY 1 ORDER BY 1`, metric, from, to)
		fmt.Fprintf(&reads, "| %s from raw points | %d | %s |\n", m, rows, took)
	}
	measure("rewrite one day, revision 2", func() (int, int) {
		var records, points int
		day := benchStart.Add(14 * 24 * time.Hour)
		for h := range 24 {
			n, p := hour(day.Add(time.Duration(h)*time.Hour), 2)
			records, points = records+n, points+p
		}
		return records, points
	})
	total := r.count(t, `SELECT count(*) FROM observed_points`)
	before := observedTables(t, r.db)
	measure("rebuild", func() (int, int) {
		report, err := r.svc.(rebuilder).RebuildRepository(ctx, repo)
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		return int(report.Records), total
	})
	if after := observedTables(t, r.db); after != before {
		t.Fatal("the rebuild did not reproduce the observed tables")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n| Phase | Records | Points | Rollup rows | Changelog bytes | WAL bytes | Wall time |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, p := range phases {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %s |\n",
			p.name, p.records, p.points, p.rollups, p.changelogBytes, p.walBytes, p.took.Round(time.Millisecond))
	}
	b.WriteString(streams.String())
	b.WriteString(footprint.String())
	b.WriteString(reads.String())
	t.Log(b.String())
	fmt.Println(b.String())
}

// timedRead runs one read on the app role's pool, row level security in
// force, and answers how many rows came back and how long it took, the
// fastest of three runs.
func timedRead(t *testing.T, db *sql.DB, q string, args ...any) (int, time.Duration) {
	t.Helper()
	best := time.Duration(1 << 62)
	n := 0
	for range 3 {
		started := time.Now()
		rows, err := db.Query(q, args...)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		n = 0
		for rows.Next() {
			n++
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(started))
	}
	return n, best.Round(10 * time.Microsecond)
}

func walLSN(t *testing.T, db *sql.DB) string {
	t.Helper()
	var lsn string
	if err := db.QueryRow(`SELECT pg_current_wal_lsn()::text`).Scan(&lsn); err != nil {
		t.Fatal(err)
	}
	return lsn
}

// walSince is the WAL the whole cluster wrote since lsn: every test database
// in the container, so the benchmark runs alone.
func walSince(t *testing.T, db *sql.DB, lsn string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)::bigint`, lsn).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dirBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		n += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
