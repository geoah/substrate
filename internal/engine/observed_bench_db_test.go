package engine_test

// The observed index benchmark: a home's sensors and three wearables written
// as closed chunks into a durable Postgres, then a correction burst, a
// rebuild with its background re-derivation, and the reads a dashboard and
// a health app send. It is gated on SUBSTRATE_BENCH_OBSERVED=1 and needs a
// database of its own on disk with durability on, never the test container:
//
//	docker run -d --name substrate-bench -p 127.0.0.1:5491:5432 -e POSTGRES_PASSWORD=bench pgvector/pgvector:pg16
//	SUBSTRATE_BENCH_OBSERVED=1 \
//	SUBSTRATE_BENCH_DSN='postgres://postgres:bench@127.0.0.1:5491/postgres?sslmode=disable' \
//	SUBSTRATE_BENCH_ROOT=/var/tmp/substrate-bench SUBSTRATE_BENCH_DAYS=60 \
//	go test -count=1 -v -timeout 3h -run '^TestObservedIndexBenchmark$' ./internal/engine/
//
// docs/plans/observations/benchmark.md records what it printed.

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// benchRepository is the repository the benchmark writes.
const benchRepository = "bench.example.com"

// heapPeak samples the process's live heap every few milliseconds while it
// runs; reset starts a new phase.
type heapPeak struct {
	peak atomic.Uint64
	stop chan struct{}
}

func startHeapPeak() *heapPeak {
	h := &heapPeak{stop: make(chan struct{})}
	go func() {
		var m runtime.MemStats
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-tick.C:
				runtime.ReadMemStats(&m)
				for {
					old := h.peak.Load()
					if m.HeapAlloc <= old || h.peak.CompareAndSwap(old, m.HeapAlloc) {
						break
					}
				}
			}
		}
	}()
	return h
}

func (h *heapPeak) reset() uint64 {
	runtime.GC()
	return h.peak.Swap(0)
}

// latencies holds a phase's per-call times.
type latencies []time.Duration

func (l latencies) pct(p float64) time.Duration {
	if len(l) == 0 {
		return 0
	}
	s := slices.Clone(l)
	slices.Sort(s)
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// statementTally is a pgx tracer that counts a phase's statements and the
// time they took, all of them and those that read or write an observed_
// table, which are the settle's.
type statementTally struct {
	all, observed     atomic.Int64
	allNs, observedNs atomic.Int64
}

type statementStart struct{}

type statementOpen struct {
	at       time.Time
	observed bool
}

func (s *statementTally) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, statementStart{}, statementOpen{at: time.Now(), observed: strings.Contains(d.SQL, "observed_")})
}

func (s *statementTally) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	open, ok := ctx.Value(statementStart{}).(statementOpen)
	if !ok {
		return
	}
	took := time.Since(open.at).Nanoseconds()
	s.all.Add(1)
	s.allNs.Add(took)
	if open.observed {
		s.observed.Add(1)
		s.observedNs.Add(took)
	}
}

// line is the tally per put since the last call, and resets it.
func (s *statementTally) line(puts int) string {
	all, observed := s.all.Swap(0), s.observed.Swap(0)
	allNs, observedNs := s.allNs.Swap(0), s.observedNs.Swap(0)
	if puts == 0 {
		return "no puts"
	}
	per := func(n int64) float64 { return float64(n) / float64(puts) }
	return fmt.Sprintf("%.1f statements a put taking %s, of them %.1f on observed_ tables taking %s",
		per(all), time.Duration(per(allNs)).Round(10*time.Microsecond),
		per(observed), time.Duration(per(observedNs)).Round(10*time.Microsecond))
}

func TestObservedIndexBenchmark(t *testing.T) {
	if os.Getenv("SUBSTRATE_BENCH_OBSERVED") != "1" {
		t.Skip("set SUBSTRATE_BENCH_OBSERVED=1, SUBSTRATE_BENCH_DSN and SUBSTRATE_BENCH_ROOT to run the observed index benchmark")
	}
	ctx := context.Background()
	admin := os.Getenv("SUBSTRATE_BENCH_DSN")
	root := os.Getenv("SUBSTRATE_BENCH_ROOT")
	if admin == "" || root == "" {
		t.Fatal("the benchmark needs SUBSTRATE_BENCH_DSN (a durable Postgres of its own) and SUBSTRATE_BENCH_ROOT (a directory on disk)")
	}
	days := 60
	if v := os.Getenv("SUBSTRATE_BENCH_DAYS"); v != "" {
		days, _ = strconv.Atoi(v)
	}
	sensors := 20
	if v := os.Getenv("SUBSTRATE_BENCH_SENSORS"); v != "" {
		sensors, _ = strconv.Atoi(v)
	}
	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adminDB.Close() }()
	var settings []string
	for _, guc := range []string{"fsync", "synchronous_commit", "full_page_writes", "shared_buffers", "data_directory", "server_version"} {
		var v string
		if err := adminDB.QueryRow(`SELECT current_setting($1)`, guc).Scan(&v); err != nil {
			t.Fatal(err)
		}
		settings = append(settings, guc+"="+v)
		if (guc == "fsync" || guc == "synchronous_commit" || guc == "full_page_writes") && v != "on" {
			t.Fatalf("%s is %s: the benchmark measures a durable database", guc, v)
		}
	}
	t.Logf("postgres: %s", strings.Join(settings, " "))
	for _, q := range []string{`DROP DATABASE IF EXISTS substrate_bench WITH (FORCE)`, `CREATE DATABASE substrate_bench`} {
		if _, err := adminDB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	dsn := strings.Replace(admin, "/postgres?", "/substrate_bench?", 1)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	tally := &statementTally{}
	svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(root), engine.WithTestQueryTracer(tally))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = svc.Close() }()
	if _, err := svc.CreateRepository(ctx, benchRepository); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, benchRepository)
	if err != nil {
		t.Fatal(err)
	}
	installCatalogues(t, ds, "health", "environment")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	trace, observation := kindTrace, kindObservation
	metricRef := func(id string) map[string]any { return map[string]any{"ref": kindMetric + "/" + id} }
	sourceRef := func(id string) map[string]any { return map[string]any{"ref": kindSource + "/" + id} }
	put := func(kind, id string, props map[string]any) time.Duration {
		started := time.Now()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
		return time.Since(started)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	offsetAt := func(at time.Time) int {
		_, off := at.In(berlin).Zone()
		return off / 60
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, berlin).UTC()
	end := start.AddDate(0, 0, days)

	// The home: sensors of four metrics, a reading a minute, one closed
	// trace per sensor and hour, its id the sensor and the hour.
	type sensor struct {
		id, metric, form, unit string
	}
	var home20 []sensor
	kinds := []sensor{
		{"temp", "environment/air_temperature", "gauge", "Cel"},
		{"humidity", "environment/relative_humidity", "gauge", "%"},
		{"power", "environment/power", "gauge", "W"},
		{"energy", "environment/energy", "counter", "kW.h"},
	}
	for i := range sensors {
		k := kinds[i%len(kinds)]
		home20 = append(home20, sensor{fmt.Sprintf("%s-%02d", k.id, i), k.metric, k.form, k.unit})
	}
	gen := rand.New(rand.NewSource(1))
	totals := map[string]float64{}
	sensorChunk := func(s sensor, at time.Time, rev int) map[string]any {
		values := make([]any, 60)
		for i := range values {
			minute := float64(at.Hour()*60 + i)
			switch s.form {
			case "counter":
				totals[s.id] += 0.004 + gen.Float64()*0.01
				values[i] = strconv.FormatFloat(totals[s.id], 'f', 4, 64)
			default:
				v := 20 + 3*float64(len(s.id)%5) + 2*float64(int(minute)%37)/37 + gen.Float64()
				values[i] = strconv.FormatFloat(v+float64(rev-1), 'f', 2, 64)
			}
		}
		props := map[string]any{
			"metric": metricRef(s.metric), "source": sourceRef(s.id), "form": s.form, "unit": s.unit,
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339), "every": "PT1M",
			"values": values, "utcOffset": offsetAt(at), "revision": rev,
		}
		switch s.form {
		case "gauge":
			props["hold"] = "PT2M"
		case "counter":
			props["resetRule"] = "monotonic"
		}
		return props
	}
	// Three wearables: heart rate from all three, a reading every five
	// minutes; steps from two, a minute each; sleep stages from two, one
	// night trace of 30-second epochs. Their streams overlap in time.
	wearables := []string{"watch", "ring", "strap"}
	hrChunk := func(src string, at time.Time) map[string]any {
		values := make([]any, 12)
		for i := range values {
			values[i] = strconv.Itoa(55 + gen.Intn(40) + len(src))
		}
		return map[string]any{
			"metric": metricRef("health/heart_rate"), "source": sourceRef(src), "form": "gauge", "unit": "/min", "hold": "PT5M",
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339), "every": "PT5M",
			"values": values, "utcOffset": offsetAt(at),
		}
	}
	stepsChunk := func(src string, at time.Time) map[string]any {
		values := make([]any, 60)
		for i := range values {
			if at.In(berlin).Hour() < 7 {
				values[i] = "0"
			} else {
				values[i] = strconv.Itoa(gen.Intn(120))
			}
		}
		return map[string]any{
			"metric": metricRef("health/steps"), "source": sourceRef(src), "form": "amount", "unit": "{steps}",
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339), "every": "PT1M",
			"values": values, "utcOffset": offsetAt(at),
		}
	}
	nightTrace := func(src string, at time.Time) map[string]any {
		codes := make([]any, 960)
		stages := []string{"light", "deep", "rem", "awake"}
		for i := range codes {
			codes[i] = stages[(i/40+gen.Intn(2))%len(stages)]
		}
		return map[string]any{
			"metric": metricRef("health/sleep_stage"), "source": sourceRef(src), "form": "gauge", "every": "PT30S",
			"at": at.Format(time.RFC3339), "endsAt": at.Add(8 * time.Hour).Format(time.RFC3339),
			"codes": codes, "utcOffset": offsetAt(at),
		}
	}

	walLSN := func() string {
		var lsn string
		if err := adminDB.QueryRow(`SELECT pg_current_wal_lsn()::text`).Scan(&lsn); err != nil {
			t.Fatal(err)
		}
		return lsn
	}
	walSince := func(from string) int64 {
		var n int64
		if err := adminDB.QueryRow(`SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)::bigint`, from).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	heap := startHeapPeak()
	defer close(heap.stop)
	var report strings.Builder
	logf := func(format string, args ...any) {
		fmt.Fprintf(&report, format+"\n", args...)
	}
	logf("workload: %d days from %s, %d sensors at a reading a minute in hourly traces, 3 wearables", days, start.Format(time.DateOnly), sensors)

	// --- write ---
	heap.reset()
	wal := walLSN()
	tally.line(0)
	var sensorPuts, wearPuts latencies
	writeStarted := time.Now()
	for at := start; at.Before(end); at = at.Add(time.Hour) {
		for _, s := range home20 {
			sensorPuts = append(sensorPuts, put(trace, s.id+"/"+at.Format("2006-01-02T15"), sensorChunk(s, at, 1)))
		}
		for i, src := range wearables {
			wearPuts = append(wearPuts, put(trace, "hr/"+src+"/"+at.Format("2006-01-02T15"), hrChunk(src, at)))
			if i < 2 {
				wearPuts = append(wearPuts, put(trace, "steps/"+src+"/"+at.Format("2006-01-02T15"), stepsChunk(src, at)))
			}
			if i > 0 && at.In(berlin).Hour() == 23 {
				wearPuts = append(wearPuts, put(trace, "sleep/"+src+"/"+at.Format("2006-01-02"), nightTrace(src, at)))
			}
		}
		if at.In(berlin).Hour() == 0 && at.In(berlin).Day() == 1 {
			t.Logf("wrote through %s (%s)", at.Format(time.DateOnly), time.Since(writeStarted).Round(time.Second))
		}
	}
	writeTook, writeWAL, writePeak := time.Since(writeStarted), walSince(wal), heap.reset()
	logf("write: %d sensor puts p50 %s p95 %s; %d wearable puts p50 %s p95 %s; took %s; WAL %d bytes; peak heap growth %d KiB",
		len(sensorPuts), sensorPuts.pct(0.5), sensorPuts.pct(0.95), len(wearPuts), wearPuts.pct(0.5), wearPuts.pct(0.95),
		writeTook.Round(time.Second), writeWAL, writePeak/1024)
	logf("write statements: %s", tally.line(len(sensorPuts)+len(wearPuts)))

	// --- sizes ---
	var readings, available, records int64
	if err := db.QueryRow(`SELECT coalesce(sum(points), 0), coalesce(sum(available), 0), count(*) FROM observed_segments`).
		Scan(&readings, &available, &records); err != nil {
		t.Fatal(err)
	}
	logf("readings: %d in %d records", readings, records)
	logf("| table | rows | total bytes | heap bytes | bytes per reading |")
	logf("| --- | ---: | ---: | ---: | ---: |")
	for _, table := range []string{"records", "changelog", "observed_streams", "observed_segments", "observed_rollups", "observed_latest"} {
		var rows, total, heapBytes int64
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT pg_total_relation_size($1::regclass), pg_relation_size($1::regclass)`, table).Scan(&total, &heapBytes); err != nil {
			t.Fatal(err)
		}
		logf("| %s | %d | %d | %d | %.1f |", table, rows, total, heapBytes, float64(total)/float64(readings))
	}
	var tiers []string
	// pg_column_size of a row is its tuple's data, header excluded; the
	// record id columns are what a compact first and last point would save.
	rows, err := db.Query(`SELECT tier, count(*), sum(pg_column_size(r.*)),
		sum(coalesce(pg_column_size(first_record_id), 0) + coalesce(pg_column_size(last_record_id), 0))
		FROM observed_rollups r GROUP BY tier ORDER BY tier`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tier, n, data, ids int64
		if err := rows.Scan(&tier, &n, &data, &ids); err != nil {
			t.Fatal(err)
		}
		tiers = append(tiers, fmt.Sprintf("%ds: %d rows, %d bytes of row data, %d of them record ids", tier, n, data, ids))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	logf("rollup rows by tier: %s", strings.Join(tiers, "; "))
	var segmentFiles int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(path, "changelog") {
			if info, err := d.Info(); err == nil {
				segmentFiles += info.Size()
			}
		}
		return nil
	})
	logf("changelog segment files: %d bytes (%.1f per reading)", segmentFiles, float64(segmentFiles)/float64(readings))

	// --- reads ---
	read := func(name string, n int, q func(i int) substrate.AggregateQuery, raw bool) {
		var l latencies
		var rowsOut int
		for i := range n {
			c := ctx
			if raw {
				c = engine.AggregateRaw(ctx)
			}
			started := time.Now()
			res, err := ds.Aggregate(c, q(i))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			l = append(l, time.Since(started))
			rowsOut = len(res.Rows)
		}
		logf("| %s | %d | %d | %s | %s |", name, n, rowsOut, l.pct(0.5), l.pct(0.95))
	}
	metricOf := func(s sensor) string { return kindMetric + "/" + s.metric }
	bySource := func(s sensor) substrate.Filter {
		return substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: kindSource + "/" + s.id}}}
	}
	gauges := slices.DeleteFunc(slices.Clone(home20), func(s sensor) bool { return s.form != "gauge" })
	monthAgo, weekAgo := end.AddDate(0, 0, -30), end.AddDate(0, 0, -7)
	runReads := func(stage string) {
		heap.reset()
		logf("reads %s:", stage)
		logf("| read | runs | rows | p50 | p95 |")
		logf("| --- | ---: | ---: | ---: | ---: |")
		read("daily means per sensor, whole range, Berlin days", len(gauges), func(i int) substrate.AggregateQuery {
			s := gauges[i]
			return substrate.AggregateQuery{Filter: bySource(s), Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: metricOf(s)}, Range: rng(start, end),
				Bucket: &substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Berlin"}, Reducers: reducers("avg", "min", "max"),
			}}
		}, false)
		read("the same, forced raw (one sensor)", 1, func(int) substrate.AggregateQuery {
			s := gauges[0]
			return substrate.AggregateQuery{Filter: bySource(s), Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: metricOf(s)}, Range: rng(start, end),
				Bucket: &substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Berlin"}, Reducers: reducers("avg", "min", "max"),
			}}
		}, true)
		read("hourly means per sensor, last 30 days", len(gauges), func(i int) substrate.AggregateQuery {
			s := gauges[i]
			return substrate.AggregateQuery{Filter: bySource(s), Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: metricOf(s)}, Range: rng(monthAgo, end),
				Bucket: &substrate.AggregateBucket{Every: "PT1H"}, Reducers: reducers("avg"),
			}}
		}, false)
		read("15-minute means per sensor, last 7 days", len(gauges), func(i int) substrate.AggregateQuery {
			s := gauges[i]
			return substrate.AggregateQuery{Filter: bySource(s), Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: metricOf(s)}, Range: rng(weekAgo, end),
				Bucket: &substrate.AggregateBucket{Every: "PT15M"}, Reducers: reducers("avg"),
			}}
		}, false)
		read("daily steps across sources by priority, last 30 days", 5, func(int) substrate.AggregateQuery {
			return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: kindMetric + "/health/steps"}, Range: rng(monthAgo, end),
				Bucket: &substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Berlin"}, Reducers: reducers("sum"),
				Sources: &substrate.AggregateSources{Policy: substrate.SourcesPriority, Order: []string{kindSource + "/watch", kindSource + "/ring"}},
			}}
		}, false)
		read("daily heart rate p90 of one source, last 30 days", 5, func(int) substrate.AggregateQuery {
			return substrate.AggregateQuery{
				Filter: substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: kindSource + "/watch"}}},
				Aggregate: &substrate.Aggregate{
					Metric: &substrate.AggregateMetric{Ref: kindMetric + "/health/heart_rate"}, Range: rng(monthAgo, end),
					Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Berlin"},
					Reducers: []substrate.AggregateReducer{{Name: substrate.ReducerPercentile, Percentile: new(0.9)}},
				},
			}
		}, false)
		logf("reads peak heap growth: %d KiB", heap.reset()/1024)
	}
	runReads("after the write")

	// --- correction burst: every sensor's day rewritten at revision 2 ---
	wal = walLSN()
	tally.line(0)
	var corrections latencies
	day := end.AddDate(0, 0, -3)
	for at := day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Hour) {
		for _, s := range home20 {
			corrections = append(corrections, put(trace, s.id+"/"+at.Format("2006-01-02T15"), sensorChunk(s, at, 2)))
		}
	}
	logf("correction burst: %d puts p50 %s p95 %s; WAL %d bytes; peak heap growth %d KiB",
		len(corrections), corrections.pct(0.5), corrections.pct(0.95), walSince(wal), heap.reset()/1024)
	logf("correction statements: %s", tally.line(len(corrections)))
	// A patch the index does not read: the fingerprint holds.
	var notes latencies
	for i := range 50 {
		s := home20[i%len(home20)]
		started := time.Now()
		if _, err := ds.Patch(ctx, substrate.ActorAPI, trace, s.id+"/"+day.Add(time.Duration(i%24)*time.Hour).Format("2006-01-02T15"),
			substrate.PatchInput{Properties: map[string]any{"quality": "valid"}}); err != nil {
			t.Fatalf("patch: %v", err)
		}
		notes = append(notes, time.Since(started))
	}
	logf("patches the index does not read: %d, p50 %s p95 %s", len(notes), notes.pct(0.5), notes.pct(0.95))
	logf("patch statements: %s", tally.line(len(notes)))

	// --- rebuild and its background re-derivation ---
	wal = walLSN()
	rebuildStarted := time.Now()
	rep, err := svc.(engine.Operator).RebuildRepository(ctx, benchRepository)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuildTook, rebuildWAL, rebuildPeak := time.Since(rebuildStarted), walSince(wal), heap.reset()
	wal = walLSN()
	derive := time.Now()
	engine.DrainIndexReprojection(t, ds)
	deriveTook := time.Since(derive)
	logf("rebuild: %d entries, %d records, took %s, WAL %d bytes, peak heap growth %d KiB",
		rep.Entries, rep.Records, rebuildTook.Round(time.Millisecond), rebuildWAL, rebuildPeak/1024)
	logf("background re-derivation after the rebuild: took %s, WAL %d bytes, peak heap growth %d KiB",
		deriveTook.Round(time.Millisecond), walSince(wal), heap.reset()/1024)
	runReads("after the rebuild")
	t.Logf("\n%s", report.String())
	_ = observation
}
