package engine_test

// The aggregate read's two paths held to each other (decision 0161): each
// case is a request the second review found answered differently from
// observed_rollups and from raw points, or wrongly on both, with the
// review's own numbers. Every case reads both paths and compares them row by
// row before it checks the answer.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// bothPaths runs one read from rollups where they serve and once from raw
// points alone, and fails unless the two answer the same rows.
func (f aggFix) bothPaths(filter substrate.Filter, a substrate.Aggregate) (rollups, raw *substrate.AggregateResult, rollupRows int64) {
	f.t.Helper()
	ctx, trace := engine.AggregateTraced(context.Background())
	rollups = f.agg(ctx, filter, a)
	raw = f.agg(engine.AggregateRaw(context.Background()), filter, a)
	sameRows(f.t, rollups, raw)
	return rollups, raw, trace.RollupRows()
}

// sameRows fails unless the two results hold the same rows, field by field.
func sameRows(t *testing.T, rollups, raw *substrate.AggregateResult) {
	t.Helper()
	a, _ := json.Marshal(rollups.Rows)
	b, _ := json.Marshal(raw.Rows)
	if string(a) != string(b) {
		t.Errorf("rollups and raw points answer differently:\nrollups %s\nraw     %s", a, b)
	}
}

func at(t time.Time) string { return t.Format(time.RFC3339) }

// statements is a pgx tracer that keeps the SQL of every statement the
// service sends, so a test can say a statement never ran.
type statements struct {
	mu   sync.Mutex
	list []string
}

func (s *statements) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list, d.SQL)
	return ctx
}

func (s *statements) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// mark is where the statements sent from now on start.
func (s *statements) mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.list)
}

// since counts the statements sent after mark that hold fragment.
func (s *statements) since(mark int, fragment string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, q := range s.list[mark:] {
		if strings.Contains(q, fragment) {
			n++
		}
	}
	return n
}

// The statement fragments the budget tests look for: the forms' discovery,
// the amount coverage taken from raw spans, and a property read's values.
const (
	discoverStatement = "SELECT DISTINCT s.n, f.form, x.code"
	coverageStatement = "sum(x.brk) OVER"
	propertyStatement = ")::text FROM records WHERE"
)

// A range holding no point answers in its streams' unit, where the metric
// declares none, as a range holding points does (a 70.5 kg reading held five
// minutes, read in the hour after it and five days later): the units are settled from
// every selected stream before the forms are found in the range. A unit
// asked for converts there too, where an empty range once had no measure and
// refused it as a code's.
func TestAggregateEmptyRangeKeepsItsUnits(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.mustPut(t, f.metric, "load", map[string]any{"name": "Load"})
	f.mustPut(t, f.observation, "load/1", map[string]any{
		"metric": f.metricRef("load"), "source": f.sourceRef("scale"), "form": "gauge",
		"value": quantity("70.5", "kg"), "at": at(start), "hold": "PT5M",
	})
	for _, x := range []struct {
		name, unit, want string
		from             time.Time
		rows             int
	}{
		{"a range with the point", "", "kg", start, 1},
		{"an empty range", "", "kg", start.AddDate(0, 0, 5), 0},
		{"an empty range in grams", "g", "g", start.AddDate(0, 0, 5), 0},
	} {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("load")},
			Range:    rng(x.from, x.from.Add(time.Hour)),
			Reducers: reducers("avg"),
			Unit:     x.unit,
		})
		if res.Unit != x.want || len(res.Rows) != x.rows {
			t.Errorf("%s: unit %q and %d rows, want %q and %d", x.name, res.Unit, len(res.Rows), x.want, x.rows)
		}
	}
}

// A form held only at a range's cut ends, or only in summaries, is found
// and read: one source's amount lies only in [00:07, 00:15), before the
// range's first whole rollup row, another's only in [01:45, 01:52), after
// its last, and a third source has summaries and no point, read with
// prefer-raw. Each answers its row.
func TestAggregateFormsAtTheCutEndsAndInSummaries(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		source string
		at     time.Duration
	}{{"early", 8 * time.Minute}, {"late", 110 * time.Minute}} {
		f.mustPut(t, f.observation, "steps/"+x.source, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef(x.source), "form": "amount",
			"value": quantity("5", "{steps}"), "at": at(start.Add(x.at)), "endsAt": at(start.Add(x.at + time.Minute)),
		})
	}
	res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start.Add(7*time.Minute), start.Add(112*time.Minute)),
		GroupBy:  []string{"source"},
		Reducers: reducers("count", "sum"),
	})
	if len(res.Rows) != 2 {
		t.Fatalf("cut ends: %+v, want a row per source", res.Rows)
	}
	for _, row := range res.Rows {
		if c, _ := val(row, "count"); c != "1" || row.Form != "amount" {
			t.Errorf("cut ends: %s counts %s %s, want 1 amount", row.Group.Source, c, row.Form)
		}
	}
	f.mustPut(t, f.observation, "hr/daily", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("ring"), "form": "summary",
		"statistic": "avg", "period": "day", "value": quantity("58", "/min"), "at": at(start), "endsAt": at(start.AddDate(0, 0, 1)),
	})
	sums := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		From:     "prefer-raw",
		Range:    rng(start, start.AddDate(0, 0, 1)),
		Reducers: reducers("avg"),
	})
	if len(sums.Rows) != 1 || sums.Rows[0].From != "summaries" || sums.Rows[0].Form != "summary" {
		t.Fatalf("summaries alone: %+v, want the day's summary row", sums.Rows)
	}
	if v, _ := val(sums.Rows[0], "avg"); v != "58" {
		t.Errorf("summaries alone: avg %s, want 58", v)
	}
}

// A read canceled once its statements have run stops inside the Go-side
// reduction, not after it: a gauge of 3,000 quarter hours, one rollup row
// each, read over its whole range and canceled as soon as the plan has read
// the rows, answers context.Canceled having merged fewer rows than the
// thousand steps between two checks, where it once merged all 3,000 and
// walked their held time before the first check.
func TestAggregateStopsWhenCanceledAfterItsStatements(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const n = 3000
	offsets := make([]int64, n)
	vals := make([]any, n)
	for i := range n {
		offsets[i] = int64(i) * (15 * time.Minute).Milliseconds()
		vals[i] = strconv.Itoa(60 + i%40)
	}
	end := start.Add(n * 15 * time.Minute)
	f.series("hr/long", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("strap"), "form": "gauge", "unit": "/min",
	}, start, end, offsets, vals)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, trace := engine.AggregateTraced(ctx)
	trace.AfterPlan(cancel)
	_, err := f.ds.Aggregate(ctx, substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		Range:    rng(start, end),
		Reducers: reducers("avg", "twa"),
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if trace.RollupRows() < n {
		t.Fatalf("the plan read %d rollup rows, want the %d the reduction would merge", trace.RollupRows(), n)
	}
	if merged := trace.MergedRows(); merged >= 1024 {
		t.Errorf("the canceled read merged %d of %d rollup rows before it stopped", merged, n)
	}
}

// A read canceled as a reducer begins stops inside the reducer, before it has
// taken the bucket: 10,000 unsorted readings in one hour, read from raw points
// and canceled after the read's last check before a reducer, answer
// context.Canceled having assembled no row. Canceled at a last, the copy of
// the bucket's points stops it (finding the last point takes one); at a
// median after a standard deviation, whose copy it reuses, the percentile's
// own count and sort do; at a durationIn after a twa, whose spans it reuses,
// the spans reducer does. Each once took the whole bucket, and the row was
// made before the next check.
func TestAggregateStopsInsideAReducer(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const n = 10000
	offsets := make([]int64, n)
	vals := make([]any, n)
	for i := range n {
		offsets[i] = int64(i) * 100
		vals[i] = strconv.Itoa(40 + (i*7919)%120)
	}
	f.series("hr/dense", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("strap"), "form": "gauge", "unit": "/min",
	}, start, start.Add(n*100*time.Millisecond), offsets, vals)
	median := substrate.AggregateReducer{Name: "percentile", Percentile: ptr(0.5)}
	zone := substrate.AggregateReducer{Name: "durationIn", Between: []float64{100, 200}}
	for _, x := range []struct {
		name     string
		reducers []substrate.AggregateReducer
		at       int
	}{
		{"the bucket's copy", reducers("last"), 1},
		{"the percentile", []substrate.AggregateReducer{{Name: "stddev"}, median}, 2},
		{"the spans reducer", []substrate.AggregateReducer{{Name: "twa"}, zone}, 2},
	} {
		t.Run(x.name, func(t *testing.T) {
			a := substrate.Aggregate{
				Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
				Range:    rng(start, start.Add(time.Hour)),
				Reducers: x.reducers,
			}
			if res, err := f.ds.Aggregate(engine.AggregateRaw(context.Background()), substrate.AggregateQuery{Aggregate: &a}); err != nil || len(res.Rows) != 1 {
				t.Fatalf("uncanceled: %v", err)
			}
			ctx, cancel := context.WithCancel(engine.AggregateRaw(context.Background()))
			defer cancel()
			ctx, trace := engine.AggregateTraced(ctx)
			began := 0
			trace.BeforeReducer(func() {
				if began++; began == x.at {
					cancel()
				}
			})
			_, err := f.ds.Aggregate(ctx, substrate.AggregateQuery{Aggregate: &a})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if trace.Rows() != 0 {
				t.Errorf("the canceled read assembled %d row(s): its reducers ran to their end", trace.Rows())
			}
		})
	}
}

// The raw spans an amount's coverage is joined from, where two rollup rows'
// extents overlap, count against maxSamples before the statement reads them:
// two traces over [00:00, 00:35) and [00:15, 00:50), read for an aligned
// hour from rows, need their two spans. maxSamples 1 refuses the read and the
// statement never runs; maxSamples 2 answers 50 minutes.
func TestAggregateCoverageFallbackCountsAgainstMaxSamples(t *testing.T) {
	t.Parallel()
	log := &statements{}
	f := aggFix{obsIndex: openObservedIndex(t, engine.WithTestQueryTracer(log)), t: t}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id       string
		from, to time.Duration
	}{{"a", 0, 35 * time.Minute}, {"b", 15 * time.Minute, 50 * time.Minute}} {
		f.series("steps/phone/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount", "unit": "{steps}",
		}, start.Add(x.from), start.Add(x.to), []int64{0}, []any{"1"})
	}
	read := func(maxSamples int64) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:    rng(start, start.Add(time.Hour)),
			Reducers: reducers("sum"),
			Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
		}}
	}
	mark := log.mark()
	_, err := f.ds.Aggregate(context.Background(), read(1))
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "aggregate.budget.maxSamples") {
		t.Fatalf("under maxSamples 1: err = %v, want a refusal naming maxSamples", err)
	}
	if n := log.since(mark, coverageStatement); n != 0 {
		t.Errorf("the refused read ran the coverage statement %d time(s)", n)
	}
	q := read(2)
	res, err := f.ds.Aggregate(context.Background(), q)
	if err != nil {
		t.Fatalf("under maxSamples 2: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0].Coverage != "PT50M" {
		t.Errorf("under maxSamples 2: %+v, want one row covering 50 minutes", res.Rows)
	}
}

// Finding a range's forms reads the points at its cut ends and its
// summaries, and counts them against maxSamples before it reads them: three
// amounts inside [00:07, 00:15), the cut end of a range from 00:07, and
// three daily summaries read with prefer-raw, are refused under maxSamples 2
// before the discovery statement runs.
func TestAggregateFormDiscoveryCountsAgainstMaxSamples(t *testing.T) {
	t.Parallel()
	log := &statements{}
	f := aggFix{obsIndex: openObservedIndex(t, engine.WithTestQueryTracer(log)), t: t}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		from := start.Add(time.Duration(8+i) * time.Minute)
		f.mustPut(t, f.observation, "steps/cut/"+strconv.Itoa(i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity("1", "{steps}"), "at": at(from), "endsAt": at(from.Add(time.Minute)),
		})
		day := start.AddDate(0, 0, 2+i)
		f.mustPut(t, f.observation, "hr/day/"+strconv.Itoa(i), map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("watch"), "form": "summary",
			"statistic": "avg", "period": "day", "value": quantity("60", "/min"), "at": at(day), "endsAt": at(day.AddDate(0, 0, 1)),
		})
	}
	for _, x := range []struct {
		name string
		a    substrate.Aggregate
	}{
		{"cut end", substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:  rng(start.Add(7*time.Minute), start.Add(2*time.Hour)), Reducers: reducers("sum"),
		}},
		{"summaries", substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")}, From: "prefer-raw",
			Range: rng(start, start.AddDate(0, 0, 7)), Reducers: reducers("avg"),
		}},
	} {
		t.Run(x.name, func(t *testing.T) {
			tight := x.a
			tight.Budget = &substrate.AggregateBudget{MaxSamples: 2}
			mark := log.mark()
			_, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &tight})
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "aggregate.budget.maxSamples") {
				t.Fatalf("under maxSamples 2: err = %v, want a refusal naming maxSamples", err)
			}
			if n := log.since(mark, discoverStatement); n != 0 {
				t.Errorf("the refused read ran the discovery statement %d time(s)", n)
			}
			res, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &x.a})
			if err != nil || len(res.Rows) == 0 {
				t.Fatalf("within the default budget: %v, %+v", err, res)
			}
		})
	}
}

// maxSamples bounds the raw points of every statement of a read together,
// not each statement's: an amount from 00:08 in the cut end of a range from
// 00:07, and two overlapping traces over [00:15, 00:50) and [00:30, 01:00),
// cost 1 point to find the forms, 1 to reduce the cut end and 3 to join the
// coverage the rows cannot measure. Each fits under maxSamples 4 and the sum,
// 5, does not: the read is refused before the coverage statement runs, and
// answers under maxSamples 5.
func TestAggregateMaxSamplesAddsEveryStatement(t *testing.T) {
	t.Parallel()
	log := &statements{}
	f := aggFix{obsIndex: openObservedIndex(t, engine.WithTestQueryTracer(log)), t: t}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value string
		from, to  time.Duration
	}{{"cut", "5", 8 * time.Minute, 9 * time.Minute}, {"a", "1", 15 * time.Minute, 50 * time.Minute}, {"b", "1", 30 * time.Minute, time.Hour}} {
		f.series("steps/phone/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount", "unit": "{steps}",
		}, start.Add(x.from), start.Add(x.to), []int64{0}, []any{x.value})
	}
	read := func(maxSamples int64) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:    rng(start.Add(7*time.Minute), start.Add(time.Hour)),
			Reducers: reducers("sum"),
			Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
		}}
	}
	mark := log.mark()
	_, err := f.ds.Aggregate(context.Background(), read(4))
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "needs 5 raw points (3 of them to join") {
		t.Fatalf("under maxSamples 4: err = %v, want a refusal of 5 raw points, 3 of them the coverage's", err)
	}
	if n := log.since(mark, discoverStatement); n != 1 {
		t.Errorf("the refused read ran the discovery statement %d time(s), want once: its 1 point fits", n)
	}
	if n := log.since(mark, coverageStatement); n != 0 {
		t.Errorf("the refused read ran the coverage statement %d time(s)", n)
	}
	res, err := f.ds.Aggregate(context.Background(), read(5))
	if err != nil {
		t.Fatalf("under maxSamples 5: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0].Coverage != "PT46M" {
		t.Fatalf("under maxSamples 5: %+v, want one row covering 46 minutes", res.Rows)
	}
	if v, _ := val(res.Rows[0], "sum"); v != "7" {
		t.Errorf("under maxSamples 5: sum %s, want 7", v)
	}
}

// A read takes the forms its range holds, never the form of the stream's
// newest point: an amount early in the hour is counted though a gauge of the
// same stream came after it, a range holding both is refused as mixed, and a
// summary written later leaves a read from raw as it was.
func TestAggregateFormsComeFromTheRange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	steps := f.metricRef("health/steps")
	f.mustPut(t, f.observation, "phone/early", map[string]any{
		"metric": steps, "source": f.sourceRef("phone"), "form": "amount", "value": quantity("10", "{steps}"),
		"at": at(start), "endsAt": at(start.Add(10 * time.Minute)),
	})
	f.mustPut(t, f.observation, "phone/later", map[string]any{
		"metric": steps, "source": f.sourceRef("phone"), "form": "gauge", "value": quantity("20", "{steps}"),
		"at": at(start.Add(30 * time.Minute)),
	})
	quarter := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start, start.Add(15*time.Minute)),
		Reducers: reducers("count"),
	}
	check := func(stage string) {
		t.Helper()
		res, _, _ := f.bothPaths(substrate.Filter{}, quarter)
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows, want the amount's", stage, len(res.Rows))
		}
		if c, _ := val(res.Rows[0], "count"); c != "1" || res.Rows[0].Form != "amount" {
			t.Errorf("%s: count %s form %s, want 1 amount", stage, c, res.Rows[0].Form)
		}
	}
	check("a gauge after the amount")
	hour := quarter
	hour.Range = rng(start, start.Add(time.Hour))
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &hour}, "a row never mixes forms")
	f.mustPut(t, f.observation, "phone/daily", map[string]any{
		"metric": steps, "source": f.sourceRef("phone"), "form": "summary", "statistic": "sum", "period": "day",
		"value": quantity("100", "{steps}"), "at": at(start), "endsAt": at(start.Add(24 * time.Hour)),
	})
	check("a summary after both")
}

// An amount's coverage is the union of its spans on both paths. Two traces of
// one source over [00:00, 00:35) and [00:15, 00:50) cover 50 minutes, not 70,
// so prefer-raw passes the hour's 100-step summary over under its 0.9 floor
// only where raw points cover 54 minutes; and two amounts in one quarter hour
// at two offsets (two rollup groups) cover their union, an hour, not 90
// minutes.
func TestAggregateAmountCoverageIsAUnion(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id       string
		from, to time.Duration
	}{{"a", 0, 35 * time.Minute}, {"b", 15 * time.Minute, 50 * time.Minute}} {
		f.series("steps/phone/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount", "unit": "{steps}",
		}, start.Add(x.from), start.Add(x.to), []int64{0}, []any{"1"})
	}
	f.mustPut(t, f.observation, "phone/hour", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "summary", "statistic": "sum",
		"period": "hour", "value": quantity("100", "{steps}"), "at": at(start), "endsAt": at(start.Add(time.Hour)),
	})
	hour := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("sum"),
		From:     "prefer-raw",
	}
	res, _, _ := f.bothPaths(substrate.Filter{}, hour)
	if len(res.Rows) != 1 || res.Rows[0].From != "summaries" {
		t.Fatalf("prefer-raw: %+v, want the summary's row", res.Rows)
	}
	if v, _ := val(res.Rows[0], "sum"); v != "100" {
		t.Errorf("prefer-raw sum = %s, want the summary's 100", v)
	}
	hour.From = ""
	res, _, rollupRows := f.bothPaths(substrate.Filter{}, hour)
	if rollupRows == 0 {
		t.Error("the raw read took no rollup row")
	}
	if len(res.Rows) != 1 || res.Rows[0].Coverage != "PT50M" {
		t.Fatalf("from raw: %+v, want 50 minutes covered", res.Rows)
	}
	if v, _ := val(res.Rows[0], "sum"); v != "2" {
		t.Errorf("from raw sum = %s, want the two traces' 2", v)
	}
	// Two rollup groups of one quarter hour: offsets +03:00 and 00:00.
	next := start.Add(2 * time.Hour)
	for _, x := range []struct {
		id       string
		from, to time.Duration
		offset   int
	}{{"long", 0, time.Hour, 180}, {"short", 5 * time.Minute, 35 * time.Minute, 0}} {
		f.mustPut(t, f.observation, "watch/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("watch"), "form": "amount",
			"value": quantity("3", "{steps}"), "at": at(next.Add(x.from)), "endsAt": at(next.Add(x.to)), "utcOffset": x.offset,
		})
	}
	hour.Range = rng(next, next.Add(time.Hour))
	res, _, _ = f.bothPaths(substrate.Filter{}, hour)
	if len(res.Rows) != 1 || res.Rows[0].Coverage != "PT1H" {
		t.Errorf("two groups in one quarter hour: %+v, want an hour covered", res.Rows)
	}
}

// The bucket budget counts the series a read answers, codes included, before
// a point is read: two coded amounts grouped by code over four quarter hours
// with fill are eight rows, refused under maxBuckets 4 and answered in full
// under 8.
func TestAggregateBucketBudgetCountsCodes(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, code := range []string{"light", "deep"} {
		from := start.Add(time.Duration(i) * 20 * time.Minute)
		f.mustPut(t, f.observation, "stage/"+code, map[string]any{
			"metric": f.metricRef("health/sleep_stage"), "source": f.sourceRef("ring"), "form": "amount",
			"value": quantity("5", "min"), "code": code, "at": at(from), "endsAt": at(from.Add(5 * time.Minute)),
		})
	}
	read := func(maxBuckets int) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:    rng(start, start.Add(time.Hour)),
			Bucket:   &substrate.AggregateBucket{Every: "PT15M"},
			GroupBy:  []string{"code"},
			Reducers: reducers("sum"),
			Fill:     "zero",
			Budget:   &substrate.AggregateBudget{MaxBuckets: maxBuckets, MaxGroups: 2},
		}}
	}
	ctx, trace := engine.AggregateTraced(context.Background())
	if _, err := f.ds.Aggregate(ctx, read(4)); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "aggregate.budget.maxBuckets") {
		t.Fatalf("err = %v, want a refusal naming maxBuckets", err)
	}
	if trace.RawPoints() != 0 || trace.RollupRows() != 0 {
		t.Errorf("the refused read took %d raw points and %d rollup rows", trace.RawPoints(), trace.RollupRows())
	}
	q := read(8)
	res, _, _ := f.bothPaths(q.Filter, *q.Aggregate)
	if len(res.Rows) != 8 {
		t.Errorf("under maxBuckets 8: %d rows, want two codes in four buckets", len(res.Rows))
	}
}

// A counter read in a unit it was not stored in: its extremes are in the
// unit asked for on both paths while its steps take the stored digits, and a
// bucket whose one reading has none before it has no increase on either path
// (null), not a measured change of zero.
func TestAggregateCounterInAnotherUnit(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value, unit string
		at              time.Duration
	}{{"first", "1", "kW.h", 0}, {"second", "2000", "W.h", 30 * time.Minute}} {
		f.mustPut(t, f.observation, "meter/"+x.id, map[string]any{
			"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "counter", "resetRule": "monotonic",
			"value": quantity(x.value, x.unit), "at": at(start.Add(x.at)),
		})
	}
	read := func(to time.Duration) substrate.AggregateRow {
		t.Helper()
		res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("energy")},
			Range:    rng(start, start.Add(to)),
			Reducers: reducers("min", "max", "increase"),
			Unit:     "W.h",
		})
		if rollupRows == 0 {
			t.Error("the read took no rollup row")
		}
		if len(res.Rows) != 1 {
			t.Fatalf("%d rows, want one", len(res.Rows))
		}
		return res.Rows[0]
	}
	lone := read(15 * time.Minute)
	if lo, _ := val(lone, "min"); lo != "1000" {
		t.Errorf("the lone kW.h reading's min = %s W.h, want 1000", lo)
	}
	if v, ok := val(lone, "increase"); ok {
		t.Errorf("a reading with none before it has increase %s, want none", v)
	}
	hour := read(time.Hour)
	if lo, _ := val(hour, "min"); lo != "1000" {
		t.Errorf("min = %s W.h, want 1000", lo)
	}
	if hi, _ := val(hour, "max"); hi != "2000" {
		t.Errorf("max = %s W.h, want 2000", hi)
	}
	if v, _ := val(hour, "increase"); v != "2000" {
		t.Errorf("increase = %s W.h, want 2000: a reading in another unit starts over", v)
	}
}

// A recorded-local day starts and ends at the offsets of its first and last
// points on both paths, however those fall in quarter hours: an amount and a
// gauge at 12:01Z (+03:00) and 12:10Z (-04:00), one rollup slot, make a day
// from midnight +03:00 to midnight -04:00, 31 hours, not 17. And prefer-raw
// measures a day's coverage over the 31 hours it reports: 25 held hours are
// not 0.9 of it, so the day's summary serves it.
func TestAggregateRecordedLocalDayFromRealPoints(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	noon := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value string
		at        time.Duration
		offset    int
	}{{"a", "10", time.Minute, 180}, {"b", "20", 10 * time.Minute, -240}} {
		f.mustPut(t, f.observation, "travel/steps/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("travel"), "form": "amount",
			"value": quantity(x.value, "{steps}"), "at": at(noon.Add(x.at)), "endsAt": at(noon.Add(x.at + 5*time.Minute)),
			"utcOffset": x.offset,
		})
		f.mustPut(t, f.observation, "travel/hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("travel"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(noon.Add(x.at)), "hold": "PT5M", "utcOffset": x.offset,
		})
	}
	athensDay := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	travel := substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath("travel")}}}
	for _, x := range []struct{ metric, reducer string }{{"health/steps", "sum"}, {"health/heart_rate", "twa"}} {
		res, _, _ := f.bothPaths(travel, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath(x.metric)},
			Range:    rng(athensDay, athensDay.AddDate(0, 0, 2)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("count", x.reducer),
		})
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows, want the one day", x.metric, len(res.Rows))
		}
		row := res.Rows[0]
		if row.Start.Format(time.RFC3339) != "2026-09-01T00:00:00+03:00" || row.End.Format(time.RFC3339) != "2026-09-02T00:00:00-04:00" {
			t.Errorf("%s: the day runs %s to %s, want midnight +03:00 to midnight -04:00", x.metric,
				row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339))
		}
	}
	// A gauge held 24 hours at +03:00 and then 1 hour at -04:00 of the same
	// local day, and the day's summary.
	trip := substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath("trip")}}}
	for _, x := range []struct {
		id, value string
		at        time.Time
		offset    int
	}{{"1", "60", athensDay, 180}, {"2", "80", time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC), -240}} {
		f.mustPut(t, f.observation, "trip/hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("trip"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(x.at), "utcOffset": x.offset,
		})
	}
	f.mustPut(t, f.observation, "trip/hr/day", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("trip"), "form": "summary",
		"statistic": "avg", "period": "day", "value": quantity("70", "/min"),
		"at": at(athensDay), "endsAt": at(athensDay.AddDate(0, 0, 1)), "utcOffset": 180,
	})
	res, _, _ := f.bothPaths(trip, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		From:     "prefer-raw",
		Range:    rng(athensDay, athensDay.AddDate(0, 0, 2)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day"},
		Reducers: reducers("avg"),
	})
	if len(res.Rows) == 0 || res.Rows[0].From != "summaries" {
		t.Fatalf("prefer-raw: %+v, want the first day from its summary", res.Rows)
	}
	if v, _ := val(res.Rows[0], "avg"); v != "70" {
		t.Errorf("the first day's avg = %s, want the summary's 70", v)
	}
}

// A recorded-local day is read from raw points where a point's offset is not
// whole quarter hours: at +00:01 the quarter hour from 23:45Z holds an
// amount at 23:58Z, on September 1 locally, and one at 23:59Z, on September
// 2, and a gauge's two readings fall either side of local midnight the same
// way. A row's quarter hour placed both on September 1; the raw points place
// each on its own day, and both paths now answer that.
func TestAggregateRecordedLocalDayAtAnOddOffset(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	midnight := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value string
		at        time.Duration
	}{{"before", "3", -2 * time.Minute}, {"after", "4", -time.Minute}} {
		f.mustPut(t, f.observation, "odd/steps/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("odd"), "form": "amount",
			"value": quantity(x.value, "{steps}"), "at": at(midnight.Add(x.at)), "endsAt": at(midnight.Add(x.at + 30*time.Second)),
			"utcOffset": 1,
		})
	}
	for _, x := range []struct {
		id, value string
		at        time.Duration
	}{{"before", "60", -4 * time.Minute}, {"after", "80", -30 * time.Second}} {
		f.mustPut(t, f.observation, "odd/hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("odd"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": midnight.Add(x.at).Format(time.RFC3339), "hold": "PT2M", "utcOffset": 1,
		})
	}
	days := rng(midnight.AddDate(0, 0, -1), midnight.AddDate(0, 0, 1))
	for _, x := range []struct {
		metric, reducer string
		want            [2]string
	}{{"health/steps", "sum", [2]string{"3", "4"}}, {"health/heart_rate", "count", [2]string{"1", "1"}}} {
		res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath(x.metric)},
			Range:    days,
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers(x.reducer),
		})
		if len(res.Rows) != 2 {
			t.Fatalf("%s: %d rows, want September 1 and 2", x.metric, len(res.Rows))
		}
		for i, row := range res.Rows {
			if v, _ := val(row, x.reducer); v != x.want[i] {
				t.Errorf("%s on the day from %s: %s = %s, want %s", x.metric, row.Start.Format(time.RFC3339), x.reducer, v, x.want[i])
			}
		}
	}
}

// A value held into a recorded-local read at an offset that is not whole
// quarter hours is split at its own local midnight on both paths: `open` at
// +00:01 from 23:44Z, read from 23:45Z, holds until `closed` at 23:59:30Z
// (+00:00), 840 s on September 1 and 30 s on September 2, where the rollups
// once put all 870 s in the day the slot's start names; and at -00:01 from
// 23:44Z until `closed` at 00:14:30Z, local midnight at 00:01Z, 960 s and
// 810 s. The only rows read are at +00:00, so the value carried in decides
// the path.
func TestAggregateRecordedLocalDayCarriedAtAnOddOffset(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	f.mustPut(t, f.metric, "door", map[string]any{"name": "Door"})
	midnight := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		source string
		offset int
		closed time.Duration
		want   [2]string
	}{
		{"ahead", 1, -30 * time.Second, [2]string{"840", "30"}},
		{"behind", -1, 14*time.Minute + 30*time.Second, [2]string{"960", "810"}},
	} {
		t.Run(x.source, func(t *testing.T) {
			f := aggFix{obsIndex: f.obsIndex, t: t}
			for _, p := range []struct {
				code   string
				at     time.Time
				offset int
			}{{"open", midnight.Add(-16 * time.Minute), x.offset}, {"closed", midnight.Add(x.closed), 0}} {
				f.mustPut(t, f.observation, "door/"+x.source+"/"+p.code, map[string]any{
					"metric": f.metricRef("door"), "source": f.sourceRef(x.source), "form": "gauge", "code": p.code,
					"at": p.at.Format(time.RFC3339), "utcOffset": p.offset,
				})
			}
			res, _, _ := f.bothPaths(substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath(x.source)}}}, substrate.Aggregate{
				Metric:   &substrate.AggregateMetric{Ref: f.metricPath("door")},
				Range:    rng(midnight.Add(-15*time.Minute), midnight.Add(15*time.Minute)),
				Bucket:   &substrate.AggregateBucket{Calendar: "day"},
				Reducers: []substrate.AggregateReducer{{Name: "timeInState", States: []string{"open"}}},
			})
			if len(res.Rows) != 2 {
				t.Fatalf("%d rows, want September 1 and 2: %+v", len(res.Rows), res.Rows)
			}
			for i, row := range res.Rows {
				got := "null"
				if v := row.Values[0].Value; v != nil {
					got = *v
				}
				if got != x.want[i] || row.Values[0].Unit != "s" {
					t.Errorf("the day from %s: open %s %s, want %s s", row.Start.Format(time.RFC3339), got, row.Values[0].Unit, x.want[i])
				}
			}
		})
	}
}

// Points at one instant are first and last by record id on both paths:
// `a-reversed` holds 2000 W.h and `z-reversed` 1 kW.h at 00:05, two rollup
// groups by unit, and in W.h the first is 2000 and the last 1000.
func TestAggregateFirstAndLastBreakTiesByRecord(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct{ id, value, unit string }{{"a-reversed", "2000", "W.h"}, {"z-reversed", "1", "kW.h"}} {
		f.mustPut(t, f.observation, x.id, map[string]any{
			"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "gauge",
			"value": quantity(x.value, x.unit), "at": at(start.Add(5 * time.Minute)),
		})
	}
	res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("energy")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("first", "last"),
		Unit:     "W.h",
	})
	if rollupRows == 0 {
		t.Error("the read took no rollup row")
	}
	if len(res.Rows) != 1 {
		t.Fatalf("%d rows", len(res.Rows))
	}
	first, _ := val(res.Rows[0], "first")
	last, _ := val(res.Rows[0], "last")
	if first != "2000" || last != "1000" {
		t.Errorf("first/last = %s/%s, want 2000/1000", first, last)
	}
}

// `eq` and `in` on one stream key admit what both admit, as the list's filter
// does: `eq` source A beside `in` [source B] reads no stream, where it once
// read both, and the same holds for a code.
func TestAggregateFilterConditionsIntersect(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, src := range []string{"a", "b"} {
		f.mustPut(t, f.observation, "door/"+src, map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef(src), "form": "gauge",
			"code": []string{"open", "closed"}[i], "at": at(start.Add(time.Duration(i) * time.Minute)),
		})
	}
	read := func(name string, c substrate.Cond) []substrate.AggregateRow {
		t.Helper()
		filter := substrate.Filter{Kinds: []string{f.observation}, Properties: map[string]substrate.Cond{name: c}}
		res, _, _ := f.bothPaths(filter, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
			Range:    rng(start, start.Add(time.Hour)),
			Reducers: reducers("count"),
		})
		return res.Rows
	}
	both := substrate.Cond{Eq: f.sourcePath("a"), In: []any{f.sourcePath("b")}}
	page, err := f.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds: []string{f.observation}, Properties: map[string]substrate.Cond{"source": both},
	}})
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("the list under eq A and in [B]: %d records (%v), want none", len(page.Records), err)
	}
	if rows := read("source", both); len(rows) != 0 {
		t.Errorf("eq A and in [B] on source: %d rows, want none", len(rows))
	}
	overlap := substrate.Cond{Eq: f.sourcePath("a"), In: []any{f.sourcePath("a"), f.sourcePath("b")}}
	if rows := read("source", overlap); len(rows) != 1 || rows[0].Sources[0] != f.sourcePath("a") {
		t.Errorf("eq A and in [A, B] on source: %+v, want A's row alone", rows)
	}
	if rows := read("code", substrate.Cond{Eq: "open", In: []any{"closed"}}); len(rows) != 0 {
		t.Errorf("eq open and in [closed] on code: %d rows, want none", len(rows))
	}
}

// locf carries a coded stream's previous row whole: `open` at 00:15 and
// `closed` at 01:15, each held a minute, leave the third hour `closed`, for
// a last and for the time in each state alike.
func TestAggregateLocfCarriesThePreviousCode(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, code := range []string{"open", "closed"} {
		f.mustPut(t, f.observation, "door/"+code, map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef("contact"), "form": "gauge", "code": code,
			"hold": "PT1M", "at": at(start.Add(time.Duration(i)*time.Hour + 15*time.Minute)),
		})
	}
	read := func(red substrate.AggregateReducer, unit string) substrate.AggregateRow {
		t.Helper()
		res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
			Range:    rng(start, start.Add(3*time.Hour)),
			Bucket:   &substrate.AggregateBucket{Every: "PT1H"},
			Reducers: []substrate.AggregateReducer{red},
			Fill:     "locf",
			Unit:     unit,
		})
		if len(res.Rows) != 3 || !res.Rows[2].Synthetic {
			t.Fatalf("%s: %+v, want three hours, the third filled", red.String(), res.Rows)
		}
		return res.Rows[2]
	}
	if last := read(substrate.AggregateReducer{Name: "last"}, ""); len(last.Values) != 1 || last.Values[0].Code != "closed" {
		t.Errorf("the filled hour's last = %+v, want closed", last.Values)
	}
	states := read(substrate.AggregateReducer{Name: "timeInState"}, "min")
	if len(states.Values) != 1 || states.Values[0].Code != "closed" || states.Values[0].Value == nil || *states.Values[0].Value != "1" {
		t.Errorf("the filled hour's time in state = %+v, want closed for 1 min", states.Values)
	}
}

// A property read counts its groups before it reads a value: a EUR and a JPY
// spend are two groups, refused under maxGroups 1, and two groups in two
// months are four cells, refused under maxBuckets 3, each before the
// statement that reads the values runs; the read within the budget runs it
// once.
func TestAggregatePropertyBudgetsBeforeTheScan(t *testing.T) {
	t.Parallel()
	log := &statements{}
	_, ds := newDataset(t, engine.WithTestQueryTracer(log))
	ctx := context.Background()
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{vocabularyPackage(moneyPackage), spendManifest()}); err != nil {
		t.Fatalf("install the spend kind: %v", err)
	}
	for i, x := range []struct {
		amount   int64
		currency string
	}{{1005, "EUR"}, {2, "JPY"}} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: moneyPackage + "/spend", ID: strconv.Itoa(i), Properties: map[string]any{
			"price": moneyValue(x.amount, x.currency), "paidAt": "2026-09-03T10:00:00Z",
		}})
	}
	read := func(budget substrate.AggregateBudget) error {
		_, err := ds.Aggregate(ctx, substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{moneyPackage + "/spend"}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: "price", Anchor: "paidAt"},
				Range:    rng(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
				Bucket:   &substrate.AggregateBucket{Calendar: "month", Timezone: "UTC"},
				Reducers: reducers("sum"),
				Budget:   &budget,
			},
		})
		return err
	}
	for _, x := range []struct {
		budget substrate.AggregateBudget
		want   string
	}{
		{substrate.AggregateBudget{MaxGroups: 1}, "aggregate.budget.maxGroups"},
		{substrate.AggregateBudget{MaxBuckets: 3}, "aggregate.budget.maxBuckets"},
	} {
		mark := log.mark()
		if err := read(x.budget); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), x.want) {
			t.Errorf("budget %+v: err = %v, want a refusal naming %s", x.budget, err, x.want)
		}
		if n := log.since(mark, propertyStatement); n != 0 {
			t.Errorf("budget %+v: the refused read ran the value statement %d time(s)", x.budget, n)
		}
	}
	mark := log.mark()
	if err := read(substrate.AggregateBudget{MaxGroups: 2, MaxBuckets: 4}); err != nil {
		t.Errorf("within the budget: %v", err)
	}
	if n := log.since(mark, propertyStatement); n != 1 {
		t.Errorf("within the budget the value statement ran %d time(s), want once", n)
	}
}

// A min or a max answers the stored digits, compared exactly, on both paths
// and in a downsample's columns, and the read says it is exact:
// 9007199254740993 and 9007199254740992 are one float64, which once answered
// 9007199254740990 with exact true.
func TestAggregateExtremesKeepTheirDigits(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range []string{"9007199254740993", "9007199254740992"} {
		from := start.Add(time.Duration(1+20*i) * time.Minute)
		f.mustPut(t, f.observation, "big/"+strconv.Itoa(i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("counter"), "form": "amount",
			"value": quantity(v, "{steps}"), "at": at(from), "endsAt": at(from.Add(time.Minute)),
		})
	}
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("min", "max"),
	}
	rollups, raw, _ := f.bothPaths(substrate.Filter{}, a)
	for _, res := range []*substrate.AggregateResult{rollups, raw} {
		if len(res.Rows) != 1 {
			t.Fatalf("%d rows", len(res.Rows))
		}
		lo, _ := val(res.Rows[0], "min")
		hi, _ := val(res.Rows[0], "max")
		if lo != "9007199254740992" || hi != "9007199254740993" || !res.Exact {
			t.Errorf("min/max = %s/%s, exact %v; want the stored digits, exact", lo, hi, res.Exact)
		}
	}
	a.Reducers = nil
	cols, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &a, Downsample: &substrate.Downsample{Points: 1}})
	if err != nil {
		t.Fatalf("downsample: %v", err)
	}
	if len(cols.Rows) != 1 || !cols.Exact {
		t.Fatalf("downsample: %+v, exact %v", cols.Rows, cols.Exact)
	}
	if lo, _ := val(cols.Rows[0], "min"); lo != "9007199254740992" {
		t.Errorf("the column's min = %s, want 9007199254740992", lo)
	}
	if hi, _ := val(cols.Rows[0], "max"); hi != "9007199254740993" {
		t.Errorf("the column's max = %s, want 9007199254740993", hi)
	}
}

// A converted min or max is chosen by its converted digits on both paths and
// in a downsample's columns: -273.14999999999999 Cel and 0.000000000000001 K
// read in K are 0.00000000000001 and 0.000000000000001, while their floats
// are 0 and 1e-15, so ordering the floats once answered them the wrong way
// round with exact true.
func TestAggregateConvertedExtremesCompareTheirDigits(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value, unit string
		at              time.Duration
	}{{"celsius", "-273.14999999999999", "Cel", time.Minute}, {"kelvin", "0.000000000000001", "K", 20 * time.Minute}} {
		f.mustPut(t, f.observation, "cold/"+x.id, map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef("cryostat"), "form": "gauge",
			"value": quantity(x.value, x.unit), "at": at(start.Add(x.at)),
		})
	}
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
		Range:    rng(start, start.Add(time.Hour)),
		Reducers: reducers("min", "max"),
		Unit:     "K",
	}
	const lo, hi = "0.000000000000001", "0.00000000000001"
	rollups, raw, rollupRows := f.bothPaths(substrate.Filter{}, a)
	if rollupRows == 0 {
		t.Error("the read took no rollup row")
	}
	for name, res := range map[string]*substrate.AggregateResult{"rollups": rollups, "raw": raw} {
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows", name, len(res.Rows))
		}
		gotLo, _ := val(res.Rows[0], "min")
		gotHi, _ := val(res.Rows[0], "max")
		if gotLo != lo || gotHi != hi || !res.Exact {
			t.Errorf("%s: min/max = %s/%s, exact %v; want %s/%s, exact", name, gotLo, gotHi, res.Exact, lo, hi)
		}
	}
	a.Reducers = nil
	cols, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &a, Downsample: &substrate.Downsample{Points: 1}})
	if err != nil {
		t.Fatalf("downsample: %v", err)
	}
	if len(cols.Rows) != 1 {
		t.Fatalf("downsample: %+v", cols.Rows)
	}
	gotLo, _ := val(cols.Rows[0], "min")
	gotHi, _ := val(cols.Rows[0], "max")
	if gotLo != lo || gotHi != hi || !cols.Exact {
		t.Errorf("the column's min/max = %s/%s, exact %v; want %s/%s, exact", gotLo, gotHi, cols.Exact, lo, hi)
	}
}

// A downsample answers at most `points` columns when its origin moves back to
// a quarter hour: [00:01, 01:01) in one column is one column, not two.
func TestAggregateDownsampleKeepsItsColumnCount(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC)
	for i, minute := range []time.Duration{0, 59} {
		from := start.Add(minute * time.Minute)
		f.mustPut(t, f.observation, "col/"+strconv.Itoa(i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity("5", "{steps}"), "at": at(from), "endsAt": at(from.Add(time.Minute)),
		})
	}
	for _, points := range []int{1, 2, 3} {
		res, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{
			Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
				Range:  rng(start, start.Add(time.Hour)),
			},
			Downsample: &substrate.Downsample{Points: points},
		})
		if err != nil {
			t.Fatalf("%d points: %v", points, err)
		}
		if len(res.Rows) == 0 || len(res.Rows) > points {
			t.Errorf("%d points: %d columns (%s wide)", points, len(res.Rows), res.Bucket.Every)
		}
	}
}

// The records mode's maxRecords counts the records the filter matches: two
// instants with no endsAt (empty windows) and then a night, under
// maxRecords 1, are refused, where the empty windows once used up the rows
// read and the night went unread without a word.
func TestAggregateMaxRecordsCountsEveryMatch(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	sleep := strings.TrimSuffix(f.observation, "/observations/observation") + "/health/sleep"
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.mustPut(t, sleep, "a-instant", map[string]any{"at": at(start)})
	f.mustPut(t, sleep, "b-instant", map[string]any{"at": at(start.Add(time.Minute))})
	f.mustPut(t, sleep, "c-night", map[string]any{"at": at(start.Add(time.Hour)), "endsAt": at(start.Add(3 * time.Hour))})
	f.mustPut(t, f.observation, "steps/night", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
		"value": quantity("10", "{steps}"), "at": at(start.Add(90 * time.Minute)), "endsAt": at(start.Add(100 * time.Minute)),
	})
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:  rng(start, start.Add(24*time.Hour)),
		Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
			Filter: substrate.Filter{Kinds: []string{sleep}}, MaxRecords: 1,
		}},
		Reducers: reducers("sum"),
	}}, "aggregate.bucket.records.maxRecords")
}

// A records-mode window is not clipped to the range, on either path: a sleep
// record over [00:00, 02:00) read with a range of [00:00, 01:00) takes an
// amount at 01:30 and a gauge's value held after 01:00, and an `around`
// widening reaches past the range at both ends.
func TestAggregateRecordWindowsReachPastTheRange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	sleep := strings.TrimSuffix(f.observation, "/observations/observation") + "/health/sleep"
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.mustPut(t, sleep, "night", map[string]any{"at": at(start), "endsAt": at(start.Add(2 * time.Hour))})
	f.mustPut(t, sleep, "nap", map[string]any{"at": at(start.Add(30 * time.Minute)), "endsAt": at(start.Add(45 * time.Minute))})
	for _, x := range []struct {
		id, value string
		from, to  time.Duration
	}{{"before", "5", -5 * time.Minute, 0}, {"after", "10", 90 * time.Minute, 100 * time.Minute}} {
		f.mustPut(t, f.observation, "steps/"+x.id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity(x.value, "{steps}"), "at": at(start.Add(x.from)), "endsAt": at(start.Add(x.to)),
		})
	}
	for _, x := range []struct {
		id, value string
		at        time.Duration
	}{{"evening", "60", -time.Hour}, {"night", "80", 90 * time.Minute}} {
		f.mustPut(t, f.observation, "hr/"+x.id, map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("phone"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(start.Add(x.at)),
		})
	}
	read := func(metric, id string, around *substrate.AggregateAround, reds ...string) substrate.AggregateRow {
		t.Helper()
		res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)},
			Range:  rng(start, start.Add(time.Hour)),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{sleep}, IDs: []string{id}}, Around: around,
			}},
			Reducers: reducers(reds...),
		})
		if rollupRows == 0 {
			t.Errorf("%s over %s read no rollup row; the windows are whole rows", metric, id)
		}
		if len(res.Rows) != 1 {
			t.Fatalf("%s over %s: %d rows, want one", metric, id, len(res.Rows))
		}
		return res.Rows[0]
	}
	if v, _ := val(read("health/steps", "night", nil, "sum"), "sum"); v != "10" {
		t.Errorf("the night's steps = %s, want the 10 at 01:30", v)
	}
	widened := &substrate.AggregateAround{Before: "PT1H", After: "PT1H"}
	if v, _ := val(read("health/steps", "nap", widened, "sum"), "sum"); v != "15" {
		t.Errorf("the widened nap's steps = %s, want 5 before the range and 10 after it", v)
	}
	night := read("health/heart_rate", "night", nil, "twa", "count")
	if v, _ := val(night, "twa"); v != "65" {
		t.Errorf("the night's time-weighted heart rate = %s, want 65 (60 held 90 minutes, 80 for 30)", v)
	}
	if c, _ := val(night, "count"); c != "1" {
		t.Errorf("the night's count = %s, want the point at 01:30", c)
	}
	nap := read("health/heart_rate", "nap", widened, "twa")
	if v, _ := val(nap, "twa"); !close9(num(t, v), (60*120+80*15)/135.0) {
		t.Errorf("the widened nap's time-weighted heart rate = %s, want %v", v, (60*120+80*15)/135.0)
	}
}

// A gauge's value carried into a recorded-local read from an offset west of
// the range's keeps every day of the range on the rollup path: daily body
// mass at 12:00Z (+00:00) read for five days at +03:00 carries 09-20's
// reading into 21:00Z, the evening of a day the range does not name, and the
// fifth day's reading still answers its row on both paths.
func TestAggregateRecordedLocalCarryFromTheWestKeepsEveryDay(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	noon := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for d := range 9 {
		f.mustPut(t, f.observation, "mass/"+strconv.Itoa(d), map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity(strconv.Itoa(70+d), "kg"), "at": at(noon.AddDate(0, 0, d)), "utcOffset": 0,
		})
	}
	plus3 := time.FixedZone("", 3*3600)
	from := time.Date(2026, 9, 21, 0, 0, 0, 0, plus3)
	res, _, rollupRows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(from, from.AddDate(0, 0, 5)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day"},
		Reducers: reducers("count", "first", "twa"),
	})
	if rollupRows == 0 {
		t.Fatalf("the read took no rollup row; it must reach the rollup path")
	}
	last := res.Rows[len(res.Rows)-1]
	if c, _ := val(last, "count"); c != "1" || last.Start.Format(time.DateOnly) != "2026-09-25" {
		t.Errorf("the range's last day: %s count %s, want 2026-09-25 with its reading", last.Start.Format(time.RFC3339), c)
	}
	if v, _ := val(last, "first"); v != "77" {
		t.Errorf("the range's last day's first = %q, want 77", v)
	}
}

// A recorded-local row's start, end and prefer-raw choice are its own group's,
// whatever other groups the read selects: London steps (+01:00) cover 22
// hours of 2026-09-24 beside the day's summary, and one Athens amount
// (+03:00) at 00:30 local is the day's earliest point. Read together or
// filtered to one source, each source's row runs midnight to midnight at its
// own offset, and London's 22 hours of 24 clear the 0.9 floor and answer from
// raw points. A shared day once ran from +03:00 to +01:00, 26 hours, and sent
// London to its summary.
func TestAggregateRecordedLocalRowsKeepTheirOwnEnds(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	london := time.FixedZone("", 3600)
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, london)
	for h := range 22 {
		f.mustPut(t, f.observation, "london/"+strconv.Itoa(h), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("london"), "form": "amount",
			"value": quantity("500", "{steps}"), "at": at(day.Add(time.Duration(h) * time.Hour)),
			"endsAt": at(day.Add(time.Duration(h+1) * time.Hour)), "utcOffset": 60,
		})
	}
	f.mustPut(t, f.observation, "london/day", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("london"), "form": "summary",
		"statistic": "sum", "period": "day", "value": quantity("12000", "{steps}"),
		"at": at(day), "endsAt": at(day.AddDate(0, 0, 1)), "utcOffset": 60,
	})
	athensAt := time.Date(2026, 9, 24, 0, 30, 0, 0, time.FixedZone("", 3*3600))
	f.mustPut(t, f.observation, "athens/1", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("athens"), "form": "amount",
		"value": quantity("40", "{steps}"), "at": at(athensAt), "endsAt": at(athensAt.Add(10 * time.Minute)), "utcOffset": 180,
	})
	plus3 := time.FixedZone("", 3*3600)
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		From:     "prefer-raw",
		Range:    rng(time.Date(2026, 9, 23, 0, 0, 0, 0, plus3), time.Date(2026, 9, 26, 0, 0, 0, 0, plus3)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day"},
		Reducers: reducers("sum"),
	}
	type shown struct{ start, end, from, sum string }
	rowOf := func(res *substrate.AggregateResult, source string) shown {
		t.Helper()
		for _, row := range res.Rows {
			if len(row.Sources) == 1 && row.Sources[0] == f.sourcePath(source) {
				v, _ := val(row, "sum")
				return shown{row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339), row.From, v}
			}
		}
		t.Fatalf("no row from %s: %+v", source, res.Rows)
		return shown{}
	}
	together, _, _ := f.bothPaths(substrate.Filter{}, a)
	for _, x := range []struct {
		source string
		want   shown
	}{
		{"london", shown{"2026-09-24T00:00:00+01:00", "2026-09-25T00:00:00+01:00", "raw", "11000"}},
		{"athens", shown{"2026-09-24T00:00:00+03:00", "2026-09-25T00:00:00+03:00", "raw", "40"}},
	} {
		alone, _, _ := f.bothPaths(substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath(x.source)}}}, a)
		if got := rowOf(alone, x.source); got != x.want {
			t.Errorf("%s alone: %+v, want %+v", x.source, got, x.want)
		}
		if got := rowOf(together, x.source); got != x.want {
			t.Errorf("%s beside the other source: %+v, want %+v", x.source, got, x.want)
		}
	}
}

// A recorded-local row the range cuts says so: London steps (+01:00) every
// hour of 2026-09-20 to 09-22, read for the day 2026-09-21 at +03:00, fall on
// the evening of 09-20 from 22:00 and on 09-21 until 22:00, local time. Each
// row's start or end is the range's own instant, at the row's offset, and
// not the whole local day.
func TestAggregateRecordedLocalRowsTheRangeCutsSaySo(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	london := time.FixedZone("", 3600)
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, london)
	for h := range 72 {
		f.mustPut(t, f.observation, "london/"+strconv.Itoa(h), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("london"), "form": "amount",
			"value": quantity("10", "{steps}"), "at": at(start.Add(time.Duration(h) * time.Hour)),
			"endsAt": at(start.Add(time.Duration(h)*time.Hour + 30*time.Minute)), "utcOffset": 60,
		})
	}
	plus3 := time.FixedZone("", 3*3600)
	from := time.Date(2026, 9, 21, 0, 0, 0, 0, plus3)
	res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(from, from.AddDate(0, 0, 1)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day"},
		Reducers: reducers("count", "sum"),
	})
	want := [][3]string{
		{"2026-09-20T22:00:00+01:00", "2026-09-21T00:00:00+01:00", "2"},
		{"2026-09-21T00:00:00+01:00", "2026-09-21T22:00:00+01:00", "22"},
	}
	if len(res.Rows) != len(want) {
		t.Fatalf("%d rows, want %d: %+v", len(res.Rows), len(want), res.Rows)
	}
	for i, row := range res.Rows {
		c, _ := val(row, "count")
		if got := [3]string{row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339), c}; got != want[i] {
			t.Errorf("row %d: %v, want %v", i, got, want[i])
		}
	}
}

// prefer-raw serves a bucket from raw points only where it holds one: heart
// rate every three hours on 2030-01-01 and none after, beside a day summary
// of 70 on 2030-01-02. The last reading holds through 01-02, which is held
// time over the whole day, but no point of 01-02 is in it, so the summary
// serves the day.
func TestAggregatePreferRawNeedsAPointInTheBucket(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 24; h += 3 {
		f.mustPut(t, f.observation, "hr/"+strconv.Itoa(h), map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("strap"), "form": "gauge",
			"value": quantity(strconv.Itoa(60+h), "/min"), "at": at(day.Add(time.Duration(h) * time.Hour)),
		})
	}
	f.mustPut(t, f.observation, "hr/day2", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("strap"), "form": "summary",
		"statistic": "avg", "period": "day", "value": quantity("70", "/min"),
		"at": at(day.AddDate(0, 0, 1)), "endsAt": at(day.AddDate(0, 0, 2)),
	})
	res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		From:     "prefer-raw",
		Range:    rng(day, day.AddDate(0, 0, 2)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
		Reducers: reducers("count", "avg"),
	})
	if len(res.Rows) != 2 {
		t.Fatalf("%d rows, want two days: %+v", len(res.Rows), res.Rows)
	}
	if row := res.Rows[0]; row.From != "raw" {
		t.Errorf("2030-01-01, eight readings: from %s, want raw", row.From)
	}
	row := res.Rows[1]
	if v, _ := val(row, "avg"); row.From != "summaries" || v != "70" {
		t.Errorf("2030-01-02, no reading: from %s, avg %q, want the summary's 70", row.From, v)
	}
}

// A value in another measure refuses only a read that takes it, on both
// paths, naming its unit: heart rate from a watch in /min, a 70 kg hand entry
// in 2020 from another source, and a 72 kg reading on the watch dated
// 2026-10-05, its stream's newest. A read of 2026-10-01 holds neither and
// answers the watch's readings. A read of 2026-10-05 holds the watch's kg
// reading; a read of 2026 from a third source holds its 2020 kg reading, which
// has no hold and still holds; a counter reading in kg beside one in kW.h is
// the first and last of its group. Each is a 422.
func TestAggregateRefusesOnlyTheValuesItTakesInAnotherMeasure(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	hr := func(id, source, value, unit string, at time.Time, hold string) {
		props := map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef(source), "form": "gauge",
			"value": quantity(value, unit), "at": at.Format(time.RFC3339),
		}
		if hold != "" {
			props["hold"] = hold
		}
		f.mustPut(t, f.observation, id, props)
	}
	for h := 8; h <= 12; h++ {
		hr("watch/"+strconv.Itoa(h), "watch", strconv.Itoa(60+h), "/min", day.Add(time.Duration(h)*time.Hour), "PT5M")
	}
	hr("watch/mass", "watch", "72", "kg", day.AddDate(0, 0, 4), "PT5M")
	hr("hand/mass", "hand", "70", "kg", time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC), "PT5M")
	hr("old/mass", "old", "70", "kg", time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC), "")
	for _, x := range []struct{ source, value, unit string }{{"meter", "1", "kW.h"}, {"scale", "5", "kg"}} {
		f.mustPut(t, f.observation, x.source+"/energy", map[string]any{
			"metric": f.metricRef("energy"), "source": f.sourceRef(x.source), "form": "counter", "resetRule": "monotonic",
			"value": quantity(x.value, x.unit), "at": at(day.Add(9 * time.Hour)),
		})
	}
	sources := func(ids ...string) substrate.Filter {
		var in []any
		for _, id := range ids {
			in = append(in, f.sourcePath(id))
		}
		return substrate.Filter{Properties: map[string]substrate.Cond{"source": {In: in}}}
	}
	heartRate := func(from time.Time) substrate.Aggregate {
		return substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
			Range:    rng(from, from.AddDate(0, 0, 1)),
			Reducers: reducers("count", "avg"),
		}
	}
	res, _, _ := f.bothPaths(sources("watch", "hand"), heartRate(day))
	if len(res.Rows) != 1 || res.Unit != "/min" {
		t.Fatalf("2026-10-01: unit %s and %d rows, want /min and the watch's one: %+v", res.Unit, len(res.Rows), res.Rows)
	}
	if c, _ := val(res.Rows[0], "count"); c != "5" {
		t.Errorf("2026-10-01: count %s, want the watch's 5 readings", c)
	}
	energy := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("energy")},
		Range:    rng(day, day.AddDate(0, 0, 1)),
		Reducers: reducers("first", "last"),
		GroupBy:  []string{"source"},
		Unit:     "W.h",
	}
	for _, x := range []struct {
		name   string
		filter substrate.Filter
		a      substrate.Aggregate
	}{
		{"the watch's kg reading", sources("watch"), heartRate(day.AddDate(0, 0, 4))},
		{"a kg reading held into the range", sources("old"), heartRate(day)},
		{"a counter reading in kg", substrate.Filter{}, energy},
	} {
		for _, path := range []struct {
			name string
			ctx  context.Context
		}{{"rollups", context.Background()}, {"raw", engine.AggregateRaw(context.Background())}} {
			res, err := f.ds.Aggregate(path.ctx, substrate.AggregateQuery{Filter: x.filter, Aggregate: &x.a})
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "kg") {
				t.Errorf("%s, %s: %v, %+v; want a 422 naming kg", x.name, path.name, err, res)
			}
		}
	}
}
