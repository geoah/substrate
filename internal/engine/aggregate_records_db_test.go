package engine_test

// The records the aggregate read reduces or windows by (decision 0161): the
// records mode's windows and the property read's records, which several
// sources write one night or one run of.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// sleepNight writes one source's night: a health/sleep record over
// [start, start+span) and its stage amounts laid end to end from stagesAt.
func (f aggFix) sleepNight(source string, n int, start time.Time, span time.Duration, stagesAt time.Time, stages []stage) {
	f.t.Helper()
	home := strings.TrimSuffix(f.observation, "/observations/observation")
	f.mustPut(f.t, home+"/health/sleep", fmt.Sprintf("%s/%d", source, n), map[string]any{
		"source": f.sourceRef(source), "at": start.Format(time.RFC3339Nano), "endsAt": start.Add(span).Format(time.RFC3339Nano),
		"utcOffsetStart": 180, "utcOffsetEnd": 180,
	})
	at := stagesAt
	for i, s := range stages {
		f.mustPut(f.t, f.observation, fmt.Sprintf("%s/%d/stage/%d", source, n, i), map[string]any{
			"metric": f.metricRef("health/sleep_stage"), "source": f.sourceRef(source), "form": "amount",
			"value": quantity(strconv.Itoa(s.min), "min"), "code": s.code,
			"at": at.Format(time.RFC3339Nano), "endsAt": at.Add(time.Duration(s.min) * time.Minute).Format(time.RFC3339Nano),
			"utcOffset": 180,
		})
		at = at.Add(time.Duration(s.min) * time.Minute)
	}
}

type stage struct {
	code string
	min  int
}

// sleepKind is the repository's health/sleep kind.
func (f aggFix) sleepKind() string {
	return strings.TrimSuffix(f.observation, "/observations/observation") + "/health/sleep"
}

// Several sources write one night as a sleep record each, and the records
// mode makes a window of every one: three sources' nights are three windows
// and every stage counts three times. bucket.records.sources keeps the
// highest-ranked source's night where two overlap: the whoop's on the first
// night, the garmin's where the whoop has none, and an unranked source's
// alone where no ranked source wrote the night. A whoop night that starts
// before the range still drops the garmin night inside it, so two adjacent
// ranges never count one night twice.
func TestAggregateRecordWindowsKeepTheHighestRankedSource(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	stages := []stage{{"light", 60}, {"deep", 45}, {"rem", 30}}
	plus3 := time.FixedZone("", 3*3600)
	night := time.Date(2026, 9, 1, 23, 0, 0, 0, plus3)
	// Night 0: whoop, garmin and apple. Night 1: garmin and apple. Night 2:
	// apple alone. Night 3: whoop from 23:59:59.5 on the 3rd, garmin a second
	// later, after the range's start at midnight.
	for _, x := range []struct {
		source string
		n      int
		at     time.Time
	}{
		{"whoop", 0, night},
		{"garmin", 0, night.Add(2 * time.Minute)},
		{"apple", 0, night.Add(5 * time.Minute)},
		{"garmin", 1, night.AddDate(0, 0, 1)},
		{"apple", 1, night.AddDate(0, 0, 1).Add(time.Minute)},
		{"apple", 2, night.AddDate(0, 0, 2)},
		{"whoop", 3, time.Date(2026, 9, 4, 23, 59, 59, 500000000, plus3)},
		{"garmin", 3, time.Date(2026, 9, 5, 0, 0, 0, 500000000, plus3)},
	} {
		f.sleepNight(x.source, x.n, x.at, 3*time.Hour, x.at, stages)
	}
	read := func(gte, lt time.Time, sources *substrate.AggregateRecordSources) *substrate.AggregateResult {
		return f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:  rng(gte, lt),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, Sources: sources,
			}},
			Reducers: reducers("sum"), Unit: "min",
			Sources: &substrate.AggregateSources{Policy: "priority", Order: []string{"whoop", "garmin", "apple"}},
		})
	}
	windows := func(res *substrate.AggregateResult) string {
		var out []string
		for _, row := range res.Rows {
			v, _ := val(row, "sum")
			out = append(out, strings.TrimPrefix(row.Record.Ref, f.sleepKind()+"/")+"="+v)
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	refs := func(res *substrate.AggregateResult) string {
		var out []string
		for _, row := range res.Rows {
			out = append(out, strings.TrimPrefix(row.Record.Ref, f.sleepKind()+"/"))
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	priority := &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop", "garmin"}, Property: "source"}
	all := read(night.Add(-time.Hour), night.AddDate(0, 0, 3), nil)
	if got, want := refs(all), "apple/0 apple/1 apple/2 garmin/0 garmin/1 whoop/0"; got != want {
		t.Errorf("separate windows: %s, want %s", got, want)
	}
	if got, want := windows(read(night.Add(-time.Hour), night.AddDate(0, 0, 3), priority)), "apple/2=135 garmin/1=135 whoop/0=135"; got != want {
		t.Errorf("priority windows: %s, want %s", got, want)
	}
	// The whoop's night 3 starts before the range and is no window of it, and
	// the garmin's night inside the range overlaps it and is dropped.
	edge := time.Date(2026, 9, 5, 0, 0, 0, 0, plus3)
	if got := windows(read(edge, edge.AddDate(0, 0, 1), priority)); got != "" {
		t.Errorf("after the whoop's night 3 started: %s, want no window", got)
	}
	if got, want := windows(read(edge.AddDate(0, 0, -1), edge, priority)), "whoop/3=135"; got != want {
		t.Errorf("before: %s, want %s", got, want)
	}

	for _, x := range []struct {
		name string
		s    *substrate.AggregateRecordSources
		want string
	}{
		{"no property", &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop"}}, "aggregate.bucket.records.sources.property"},
		{"no order", &substrate.AggregateRecordSources{Policy: "priority", Property: "source"}, "aggregate.bucket.records.sources.order"},
		{"an order beside separate", &substrate.AggregateRecordSources{Order: []string{"whoop"}}, "separate keeps every record"},
		{"an unknown policy", &substrate.AggregateRecordSources{Policy: "summaries"}, `"summaries" is not a policy for records`},
		{"an undeclared property", &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop"}, Property: "device"}, `declares no property "device"`},
		{"a number", &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"1"}, Property: "disturbances"}, "name a string, an enum or a reference"},
	} {
		t.Run(x.name, func(t *testing.T) {
			f.refusedAgg(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
				Range:  rng(night, night.AddDate(0, 0, 1)),
				Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
					Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, Sources: x.s,
				}},
				Reducers: reducers("sum"),
			}}, x.want)
		})
	}
}

// A record outside the range drops one inside it only where it is itself
// kept, and that turns on the records ranked above it, outside the range
// too. A whoop night Z, a garmin night Y overlapping it and an apple night X
// overlapping Y alone: Z drops Y, so X is kept, and a range holding X alone
// answers so, as a range holding all three does; without Z beside it, Y
// would drop X there, and the day before drops Y, so X would be counted on
// neither day. The records mode and the property read weigh alike.
func TestAggregateRecordSourcesWeighAChainAcrossTheRangeEdge(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	d := time.Date(2026, 9, 10, 0, 0, 0, 0, plus3)
	chain := []struct {
		id, source string
		at         time.Time
		span       time.Duration
	}{
		{"whoop/z", "whoop", d.Add(-3 * time.Hour), 2 * time.Hour},
		{"garmin/y", "garmin", d.Add(-2 * time.Hour), 150 * time.Minute},
		{"apple/x", "apple", d.Add(15 * time.Minute), 105 * time.Minute},
	}
	for _, r := range chain {
		f.mustPut(t, f.sleepKind(), r.id, map[string]any{
			"source": f.sourceRef(r.source), "at": r.at.Format(time.RFC3339), "endsAt": r.at.Add(r.span).Format(time.RFC3339),
		})
		f.workout(r.id, r.source, "running", r.at, r.span, "5")
	}
	// One stage in each night that no other night holds.
	f.stageAt("whoop/z/stage", "whoop", "light", d.Add(-3*time.Hour), 10*time.Minute)
	f.stageAt("garmin/y/stage", "garmin", "light", d, 10*time.Minute)
	f.stageAt("apple/x/stage", "apple", "light", d.Add(time.Hour), 10*time.Minute)
	whoopFirst := &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop", "garmin"}, Property: "source"}
	windows := func(gte, lt time.Time) string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:  rng(gte, lt),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, Sources: whoopFirst,
			}},
			Reducers: reducers("count"),
		})
		seen := map[string]bool{}
		var out []string
		for _, row := range res.Rows {
			if ref := strings.TrimPrefix(row.Record.Ref, f.sleepKind()+"/"); !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	distance := func(gte, lt time.Time) string {
		res := f.agg(context.Background(), substrate.Filter{Kinds: []string{f.workoutKind()}}, substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "distance", Sources: whoopFirst},
			Range:    rng(gte, lt), Reducers: reducers("count"),
		})
		if len(res.Rows) == 0 {
			return "0"
		}
		n, _ := val(res.Rows[0], "count")
		return n
	}
	for _, x := range []struct {
		name            string
		from, to        time.Time
		windows, counts string
	}{
		{"the whole chain", d.AddDate(0, 0, -1), d.AddDate(0, 0, 1), "apple/x whoop/z", "2"},
		{"the day X starts", d, d.AddDate(0, 0, 1), "apple/x", "1"},
		{"the day before", d.AddDate(0, 0, -1), d, "whoop/z", "1"},
	} {
		if got := windows(x.from, x.to); got != x.windows {
			t.Errorf("%s: windows %q, want %q", x.name, got, x.windows)
		}
		if got := distance(x.from, x.to); got != x.counts {
			t.Errorf("%s: workouts counted %s, want %s", x.name, got, x.counts)
		}
	}

	// maxRecords bounds the records the policy weighs around the range as
	// well as the ones the range matches: X and the two it reads beside it.
	bounded := func(maxRecords int) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:  rng(d, d.AddDate(0, 0, 1)),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, Sources: whoopFirst, MaxRecords: maxRecords,
			}},
			Reducers: reducers("count"),
		}}
	}
	f.refusedAgg(bounded(2), "aggregate.bucket.records.maxRecords")
	if _, err := f.ds.Aggregate(context.Background(), bounded(3)); err != nil {
		t.Errorf("maxRecords 3: %v", err)
	}
}

// A record with no endsAt is an instant, which drops no record with a span:
// a band that wrote a night while it was still going on, before it had an
// end, ranks above the watch, yet the watch's whole night keeps its window.
// The unfinished night makes no window of its own.
func TestAggregateRecordSourcesKeepANightUnderAnUnfinishedOne(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	night := time.Date(2026, 9, 12, 23, 0, 0, 0, plus3)
	f.sleepNight("garmin", 0, night, 8*time.Hour, night, []stage{{"light", 60}, {"deep", 45}})
	f.mustPut(t, f.sleepKind(), "whoop/0", map[string]any{
		"source": f.sourceRef("whoop"), "at": night.Add(time.Hour).Format(time.RFC3339),
	})
	res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
		Range:  rng(night.Add(-time.Hour), night.AddDate(0, 0, 1)),
		Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
			Filter:  substrate.Filter{Kinds: []string{f.sleepKind()}},
			Sources: &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop", "garmin"}, Property: "source"},
		}},
		Reducers: reducers("sum"), Unit: "min",
	})
	if len(res.Rows) != 1 || res.Rows[0].Record == nil || res.Rows[0].Record.Ref != f.sleepKind()+"/garmin/0" {
		t.Fatalf("rows %+v, want the garmin night's one row", res.Rows)
	}
	wantNum(t, "garmin/0", res.Rows[0], "sum", 105, true)
}

// workout writes one source's workout of a sport over [at, at+span) with a
// distance in km.
func (f aggFix) workout(id, source, sport string, at time.Time, span time.Duration, km string) {
	f.t.Helper()
	home := strings.TrimSuffix(f.observation, "/observations/observation")
	f.mustPut(f.t, home+"/health/workout", id, map[string]any{
		"source": f.sourceRef(source), "sport": sport, "distance": quantity(km, "km"),
		"at": at.Format(time.RFC3339Nano), "endsAt": at.Add(span).Format(time.RFC3339Nano),
		"utcOffsetStart": 180, "utcOffsetEnd": 180, "duration": vocabularyDuration(span.Milliseconds()),
	})
}

// workoutKind is the repository's health/workout kind.
func (f aggFix) workoutKind() string {
	return strings.TrimSuffix(f.observation, "/observations/observation") + "/health/workout"
}

// A property read over workouts that three sources each wrote counts one
// run three times. property.sources keeps the highest-ranked source's
// workout where two overlap, and match keeps apart the overlapping workouts
// of two sports: a 3-hour activity a band detected does not drop the run and
// the walk a watch recorded inside it unless the two name one sport. A
// workout the range leaves out still drops the lower-ranked copy inside it.
func TestAggregatePropertyKeepsTheHighestRankedSource(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	day := func(d, h, m, s int) time.Time { return time.Date(2026, 9, d, h, m, s, 0, plus3) }
	f.workout("garmin/1", "garmin", "running", day(7, 7, 0, 0), time.Hour, "10")
	f.workout("whoop/1", "whoop", "running", day(7, 7, 1, 0), time.Hour, "9.5")
	f.workout("apple/1", "apple", "running", day(7, 7, 0, 30), 59*time.Minute, "10.2")
	f.workout("apple/2", "apple", "running", day(8, 7, 0, 0), 30*time.Minute, "5.1")
	f.workout("whoop/3", "whoop", "other", day(9, 6, 0, 0), 3*time.Hour, "12")
	f.workout("garmin/3", "garmin", "running", day(9, 6, 30, 0), time.Hour, "8")
	f.workout("garmin/4", "garmin", "walking", day(9, 8, 0, 0), 30*time.Minute, "2")
	f.workout("whoop/5", "whoop", "running", day(13, 23, 59, 59), time.Hour, "3")
	f.workout("garmin/5", "garmin", "running", day(14, 0, 0, 30), time.Hour, "3.1")
	read := func(gte, lt time.Time, sources *substrate.AggregateRecordSources) string {
		res := f.agg(context.Background(), substrate.Filter{Kinds: []string{f.workoutKind()}}, substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "distance", Sources: sources},
			Range:    rng(gte, lt), Reducers: reducers("sum", "count"), Unit: "km",
		})
		if len(res.Rows) == 0 {
			return ""
		}
		sum, _ := val(res.Rows[0], "sum")
		n, _ := val(res.Rows[0], "count")
		return sum + "/" + n
	}
	week, next := day(7, 0, 0, 0), day(14, 0, 0, 0)
	garminFirst := &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"garmin", "whoop"}, Property: "source"}
	whoopFirst := &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop", "garmin"}, Property: "source"}
	bySport := &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop", "garmin"}, Property: "source", Match: []string{"sport"}}
	for _, x := range []struct {
		name      string
		from, to  time.Time
		sources   *substrate.AggregateRecordSources
		sumAndCnt string
	}{
		{"every record", week, next, nil, "59.8/8"},
		// The whoop's run from 23:59:59 on the 13th is the garmin's run of
		// the 14th, which the next range counts.
		{"garmin first", week, next, garminFirst, "25.1/4"},
		{"whoop first", week, next, whoopFirst, "29.6/4"},
		{"whoop first by sport", week, next, bySport, "39.6/6"},
		{"after the range edge, whoop first", next, next.AddDate(0, 0, 1), whoopFirst, ""},
		{"after the range edge, garmin first", next, next.AddDate(0, 0, 1), garminFirst, "3.1/1"},
	} {
		if got := read(x.from, x.to, x.sources); got != x.sumAndCnt {
			t.Errorf("%s: sum/count = %q, want %q", x.name, got, x.sumAndCnt)
		}
	}
	f.refusedAgg(substrate.AggregateQuery{
		Filter: substrate.Filter{Kinds: []string{f.workoutKind()}},
		Aggregate: &substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "distance"},
			Sources:  &substrate.AggregateSources{Policy: "priority", Order: []string{"garmin"}},
			Range:    rng(week, next), Reducers: reducers("sum"),
		},
	}, "property.sources chooses among records")
	f.refusedAgg(substrate.AggregateQuery{
		Filter: substrate.Filter{Kinds: []string{f.workoutKind()}},
		Aggregate: &substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "distance", Sources: &substrate.AggregateRecordSources{
				Policy: "priority", Order: []string{"garmin"}, Property: "source", Match: []string{"distance"},
			}},
			Range: rng(week, next), Reducers: reducers("sum"),
		},
	}, "aggregate.property.sources.match[0]")
}

// stageAt writes one coded stage amount of a source over [at, at+span).
func (f aggFix) stageAt(id, source, code string, at time.Time, span time.Duration) {
	f.t.Helper()
	f.mustPut(f.t, f.observation, id, map[string]any{
		"metric": f.metricRef("health/sleep_stage"), "source": f.sourceRef(source), "form": "amount",
		"value": quantity(strconv.FormatFloat(span.Minutes(), 'f', -1, 64), "min"), "code": code,
		"at": at.Format(time.RFC3339Nano), "endsAt": at.Add(span).Format(time.RFC3339Nano), "utcOffset": 180,
	})
}

// A records-mode window takes an amount by the part of its span inside it:
// a stage that starts half a second before the night (another device's
// clock) counts all but that half second, not nothing, and a stage that runs
// past the night's end counts up to it, not whole. A five-hour amount that
// starts an hour before the window is found and cut too. Each row says how
// many of its amounts were cut in prorated, and its coverage is the time
// inside the window. Rollups serve no records-mode amount, so both paths
// answer the same.
func TestAggregateRecordWindowsTakeAnAmountByItsOverlap(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	start := time.Date(2026, 10, 4, 23, 0, 0, 500000000, plus3)
	end := time.Date(2026, 10, 5, 3, 0, 0, 0, plus3)
	f.mustPut(t, f.sleepKind(), "whoop/0", map[string]any{
		"source": f.sourceRef("whoop"), "at": start.Format(time.RFC3339Nano), "endsAt": end.Format(time.RFC3339Nano),
	})
	clock := func(h, m int) time.Time {
		d := 4
		if h < 12 {
			d = 5
		}
		return time.Date(2026, 10, d, h, m, 0, 0, plus3)
	}
	f.stageAt("garmin/awake", "garmin", "awake", clock(23, 0), 30*time.Minute)
	f.stageAt("garmin/light", "garmin", "light", clock(23, 30), 90*time.Minute)
	f.stageAt("garmin/deep", "garmin", "deep", clock(1, 0), time.Hour)
	f.stageAt("garmin/rem", "garmin", "rem", clock(2, 0), 90*time.Minute)
	f.stageAt("apple/asleep", "apple", "asleep", clock(22, 0), 5*time.Hour)
	res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
		Range:    rng(clock(20, 0), clock(6, 0)),
		Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}}},
		GroupBy:  []string{"source", "code"},
		Reducers: reducers("sum", "count"),
		Unit:     "min",
	})
	type want struct {
		sum      float64
		count    string
		prorated int64
		coverage string
	}
	wants := map[string]want{
		"apple asleep": {300 * (4*3600 - 0.5) / (5 * 3600), "1", 1, "PT3H59M59.5S"},
		"garmin awake": {30 * (1800 - 0.5) / 1800, "1", 1, "PT29M59.5S"},
		"garmin light": {90, "1", 0, "PT1H30M"},
		"garmin deep":  {60, "1", 0, "PT1H"},
		"garmin rem":   {60, "1", 1, "PT1H"},
	}
	seen := map[string]bool{}
	for _, row := range res.Rows {
		key := strings.TrimPrefix(row.Group.Source, f.source+"/") + " " + row.Group.Code
		w, ok := wants[key]
		if !ok {
			t.Errorf("unexpected row %s", key)
			continue
		}
		seen[key] = true
		wantNum(t, key, row, "sum", w.sum, true)
		if c, _ := val(row, "count"); c != w.count || row.Prorated != w.prorated || row.Coverage != w.coverage {
			t.Errorf("%s: count %s, prorated %d, coverage %s; want %s, %d, %s", key, c, row.Prorated, row.Coverage, w.count, w.prorated, w.coverage)
		}
	}
	for key := range wants {
		if !seen[key] {
			t.Errorf("no row for %s", key)
		}
	}

	// One amount across two windows apart counts its part in each, once.
	noon := time.Date(2026, 10, 6, 10, 0, 0, 0, plus3)
	for i, w := range [][2]time.Time{{noon, noon.Add(time.Hour)}, {noon.Add(90 * time.Minute), noon.Add(150 * time.Minute)}} {
		f.mustPut(t, f.sleepKind(), fmt.Sprintf("nap/%d", i), map[string]any{
			"source": f.sourceRef("whoop"), "at": w[0].Format(time.RFC3339), "endsAt": w[1].Format(time.RFC3339),
		})
	}
	f.stageAt("garmin/nap", "garmin", "light", noon.Add(30*time.Minute), 90*time.Minute)
	naps, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
		Range:    rng(noon, noon.Add(3*time.Hour)),
		Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}}},
		Reducers: reducers("sum", "count"),
		Unit:     "min",
	})
	if len(naps.Rows) != 2 {
		t.Fatalf("%d nap rows, want two", len(naps.Rows))
	}
	for _, row := range naps.Rows {
		wantNum(t, row.Record.Ref, row, "sum", 30, true)
		if c, _ := val(row, "count"); c != "1" || row.Prorated != 1 {
			t.Errorf("%s: count %s, prorated %d; want 1, 1", row.Record.Ref, c, row.Prorated)
		}
	}
}

// The records mode finds how far an amount may start before a window and
// still reach into it by reading the selected streams' spanned amounts from
// the first window to their newest, and those count against maxSamples: a
// night read long before a stream's later amounts takes them all, and a
// budget they pass refuses the read rather than scan past it.
func TestAggregateRecordWindowsChargeTheAmountReach(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	night := time.Date(2026, 8, 1, 23, 0, 0, 0, plus3)
	f.sleepNight("whoop", 0, night, 3*time.Hour, night, []stage{{"light", 60}, {"deep", 45}})
	for d := 1; d <= 30; d++ {
		f.stageAt(fmt.Sprintf("later/%d", d), "whoop", "light", night.AddDate(0, 0, d).Add(-10*time.Hour), 10*time.Minute)
	}
	query := func(maxSamples int64) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:  rng(night.Add(-time.Hour), night.Add(5*time.Hour)),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}},
			}},
			Reducers: reducers("count"),
			Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
		}}
	}
	f.refusedAgg(query(20), "finding how far amounts reach")
	res, err := f.ds.Aggregate(context.Background(), query(100))
	if err != nil {
		t.Fatalf("maxSamples 100: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows %+v, want the night's one", res.Rows)
	}
	if n, _ := val(res.Rows[0], "count"); n != "2" {
		t.Errorf("count = %s, want the night's two stages", n)
	}
}

// A property read reduces a duration in a duration unit, exactly, and gives
// its population standard deviation; property.groupBy splits it by the
// records' own properties, an enum (sport) and a reference (source), each
// row naming its values in group.properties. Weekly training time per sport
// is one read.
func TestAggregatePropertyReducesDurationsBySport(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, plus3) }
	f.workout("garmin/1", "garmin", "running", day(7, 7), 30*time.Minute, "5")
	f.workout("garmin/2", "garmin", "running", day(8, 7), 90*time.Minute, "15")
	f.workout("garmin/3", "garmin", "cycling", day(9, 7), 2*time.Hour, "40")
	f.workout("apple/4", "apple", "running", day(10, 7), 45*time.Minute, "8")
	read := func(groupBy []string, unit string, reds ...string) *substrate.AggregateResult {
		return f.agg(context.Background(), substrate.Filter{Kinds: []string{f.workoutKind()}}, substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "duration", GroupBy: groupBy},
			Range:    rng(day(7, 0), day(14, 0)),
			Bucket:   &substrate.AggregateBucket{Calendar: "week", Timezone: "Europe/Athens"},
			Reducers: reducers(reds...), Unit: unit,
		})
	}
	bySport := read([]string{"sport"}, "min", "sum", "count", "stddev")
	got := map[string]string{}
	for _, row := range bySport.Rows {
		sum, _ := val(row, "sum")
		cnt, _ := val(row, "count")
		sd, _ := val(row, "stddev")
		if row.Group == nil {
			t.Fatalf("row %+v has no group", row)
		}
		got[row.Group.Properties["sport"]] = sum + "/" + cnt + "/" + sd + " " + row.Values[0].Unit
	}
	// Running is 30, 90 and 45 minutes: a mean of 55 and a variance of 650.
	for sport, want := range map[string]string{"running": "165/3/25.49509756796392415 min", "cycling": "120/1/0 min"} {
		if got[sport] != want {
			t.Errorf("%s = %q, want %q", sport, got[sport], want)
		}
	}
	if len(got) != 2 || bySport.Unit != "min" || bySport.Exact {
		t.Errorf("rows %v, unit %q, exact %v; want two sports in min, inexact (a square root)", got, bySport.Unit, bySport.Exact)
	}
	total := read(nil, "", "sum")
	if v, _ := val(total.Rows[0], "sum"); len(total.Rows) != 1 || v != "17100" || total.Unit != "s" || !total.Exact {
		t.Errorf("total = %+v, want 17100 s, exact", total.Rows)
	}
	bySource := read([]string{"source", "sport"}, "h", "sum")
	gotSources := map[string]string{}
	for _, row := range bySource.Rows {
		v, _ := val(row, "sum")
		gotSources[strings.TrimPrefix(row.Group.Properties["source"], f.source+"/")+" "+row.Group.Properties["sport"]] = v
	}
	for k, want := range map[string]string{"garmin running": "2", "garmin cycling": "2", "apple running": "0.75"} {
		if gotSources[k] != want {
			t.Errorf("%s = %q, want %q (all %v)", k, gotSources[k], want, gotSources)
		}
	}
	for _, x := range []struct {
		name string
		a    substrate.Aggregate
		want string
	}{
		{"a unit that is no duration", substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "duration"}, Range: rng(day(7, 0), day(14, 0)), Reducers: reducers("sum"), Unit: "km",
		}, "not a duration unit"},
		{"a group of numbers", substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "duration", GroupBy: []string{"strain"}}, Range: rng(day(7, 0), day(14, 0)), Reducers: reducers("sum"),
		}, "aggregate.property.groupBy[0]"},
		{"a stream key in groupBy", substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "duration"}, GroupBy: []string{"source"}, Range: rng(day(7, 0), day(14, 0)), Reducers: reducers("sum"),
		}, "property.groupBy"},
	} {
		t.Run(x.name, func(t *testing.T) {
			f.refusedAgg(substrate.AggregateQuery{Filter: substrate.Filter{Kinds: []string{f.workoutKind()}}, Aggregate: &x.a}, x.want)
		})
	}
}

// The recorded-local calendar places each record by its own UTC offset,
// read from the property the request names: a night that ends at 01:30Z in
// Los Angeles (-07:00) is on its wake day there, the 21st, not the 22nd
// Athens would put it on, and each row is written at its records' offsets.
// A record without the offset is refused, naming it, unless
// bucket.fallbackTimezone places it, which the row counts in offsetless.
func TestAggregatePropertyPlacesARecordByItsOwnOffset(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	night := func(id string, end time.Time, offset any, asleep string) {
		props := map[string]any{
			"source": f.sourceRef("whoop"), "at": end.Add(-8 * time.Hour).Format(time.RFC3339), "endsAt": end.Format(time.RFC3339),
			"timeAsleep": asleep,
		}
		if offset != nil {
			props["utcOffsetEnd"] = offset
		}
		f.mustPut(t, f.sleepKind(), id, props)
	}
	night("athens", time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC), 180, "PT7H")
	night("la", time.Date(2026, 9, 22, 1, 30, 0, 0, time.UTC), -420, "PT1H")
	night("london", time.Date(2026, 9, 22, 6, 30, 0, 0, time.UTC), 60, "PT8H")
	plus3 := time.FixedZone("", 3*3600)
	read := func(bucket substrate.AggregateBucket) (*substrate.AggregateResult, error) {
		return f.ds.Aggregate(context.Background(), substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{f.sleepKind()}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: "timeAsleep", Anchor: "endsAt", UTCOffset: "utcOffsetEnd"},
				Range:    rng(time.Date(2026, 9, 21, 0, 0, 0, 0, plus3), time.Date(2026, 9, 23, 0, 0, 0, 0, plus3)),
				Bucket:   &bucket, Reducers: reducers("sum", "count"), Unit: "h",
			},
		})
	}
	rows := func(res *substrate.AggregateResult) string {
		var out []string
		for _, row := range res.Rows {
			sum, _ := val(row, "sum")
			cnt, _ := val(row, "count")
			out = append(out, fmt.Sprintf("%s..%s=%s/%s/%d", row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339), sum, cnt, row.Offsetless))
		}
		return strings.Join(out, " ")
	}
	res, err := read(substrate.AggregateBucket{Calendar: "day"})
	if err != nil {
		t.Fatalf("recorded-local: %v", err)
	}
	if got, want := rows(res), "2026-09-21T00:00:00+03:00..2026-09-22T00:00:00-07:00=8/2/0 2026-09-22T00:00:00+01:00..2026-09-22T22:00:00+01:00=8/1/0"; got != want {
		t.Errorf("recorded-local days:\n got %s\nwant %s", got, want)
	}
	athens, err := read(substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens"})
	if err == nil || !strings.Contains(err.Error(), "aggregate.property.utcOffset") {
		t.Errorf("an offset beside a timezone: %v, %+v; want a refusal naming property.utcOffset", err, athens)
	}

	night("unknown", time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC), nil, "PT6H")
	f.refusedAgg(substrate.AggregateQuery{
		Filter: substrate.Filter{Kinds: []string{f.sleepKind()}},
		Aggregate: &substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "timeAsleep", Anchor: "endsAt", UTCOffset: "utcOffsetEnd"},
			Range:    rng(time.Date(2026, 9, 21, 0, 0, 0, 0, plus3), time.Date(2026, 9, 23, 0, 0, 0, 0, plus3)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day"}, Reducers: reducers("sum"),
		},
	}, f.sleepKind()+"/unknown holds no utcOffsetEnd")
	res, err = read(substrate.AggregateBucket{Calendar: "day", FallbackTimezone: "Europe/Athens"})
	if err != nil {
		t.Fatalf("with a fallback zone: %v", err)
	}
	// The night without an offset ends at 08:00 in Athens on the 22nd, the
	// day's first record, so the day starts at Athens's midnight.
	if got, want := rows(res), "2026-09-21T00:00:00+03:00..2026-09-22T00:00:00-07:00=8/2/0 2026-09-22T00:00:00+03:00..2026-09-22T22:00:00+01:00=14/2/1"; got != want {
		t.Errorf("with a fallback zone:\n got %s\nwant %s", got, want)
	}
}

// Under a source policy the recorded-local calendar refuses only a kept
// record without an offset: a lower-ranked source's night that the policy
// drops is never placed, so its missing offset refuses nothing. Kept, it
// refuses the read, naming it.
func TestAggregatePropertyRefusesOnlyAKeptRecordWithoutAnOffset(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	end := time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)
	f.mustPut(t, f.sleepKind(), "whoop/0", map[string]any{
		"source": f.sourceRef("whoop"), "at": end.Add(-8 * time.Hour).Format(time.RFC3339), "endsAt": end.Format(time.RFC3339),
		"timeAsleep": "PT7H", "utcOffsetEnd": 180,
	})
	f.mustPut(t, f.sleepKind(), "googlefit/0", map[string]any{
		"source": f.sourceRef("googlefit"), "at": end.Add(-8 * time.Hour).Format(time.RFC3339), "endsAt": end.Add(10 * time.Minute).Format(time.RFC3339),
		"timeAsleep": "PT6H50M",
	})
	plus3 := time.FixedZone("", 3*3600)
	query := func(order ...string) substrate.AggregateQuery {
		var sources *substrate.AggregateRecordSources
		if len(order) > 0 {
			sources = &substrate.AggregateRecordSources{Policy: "priority", Order: order, Property: "source"}
		}
		return substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{f.sleepKind()}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: "timeAsleep", Anchor: "endsAt", UTCOffset: "utcOffsetEnd", Sources: sources},
				Range:    rng(time.Date(2026, 9, 21, 0, 0, 0, 0, plus3), time.Date(2026, 9, 22, 0, 0, 0, 0, plus3)),
				Bucket:   &substrate.AggregateBucket{Calendar: "day"}, Reducers: reducers("sum", "count"), Unit: "h",
			},
		}
	}
	res, err := f.ds.Aggregate(context.Background(), query("whoop", "googlefit"))
	if err != nil {
		t.Fatalf("the whoop's night first: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows %+v, want one day", res.Rows)
	}
	sum, _ := val(res.Rows[0], "sum")
	n, _ := val(res.Rows[0], "count")
	if sum != "7" || n != "1" {
		t.Errorf("sum/count = %s/%s, want the whoop's night alone, 7/1", sum, n)
	}
	f.refusedAgg(query(), f.sleepKind()+"/googlefit/0 holds no utcOffsetEnd")
	f.refusedAgg(query("googlefit", "whoop"), f.sleepKind()+"/googlefit/0 holds no utcOffsetEnd")
}

// A records-mode row is written at its record's own offsets where the
// request names the properties holding them: a night begun in London
// (+01:00) and ended in Athens (+03:00), read with a range written at
// +03:00, starts at 23:00+01:00 and ends at 07:00+03:00, where it happened.
// Without utcOffsets, or for a record holding none, a row is written at the
// range's offset.
func TestAggregateRecordWindowsAreWrittenAtTheRecordsOffsets(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 14, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC)
	f.mustPut(t, f.sleepKind(), "travel", map[string]any{
		"source": f.sourceRef("whoop"), "at": start.Format(time.RFC3339), "endsAt": end.Format(time.RFC3339),
		"utcOffsetStart": 60, "utcOffsetEnd": 180,
	})
	f.mustPut(t, f.sleepKind(), "unknown", map[string]any{
		"source": f.sourceRef("whoop"), "at": start.AddDate(0, 0, 1).Format(time.RFC3339), "endsAt": end.AddDate(0, 0, 1).Format(time.RFC3339),
	})
	for _, day := range []int{0, 1} {
		f.stageAt(fmt.Sprintf("stage/%d", day), "whoop", "light", start.AddDate(0, 0, day).Add(time.Hour), time.Hour)
	}
	plus3 := time.FixedZone("", 3*3600)
	read := func(offsets *substrate.AggregateRecordOffsets) string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:  rng(time.Date(2026, 9, 14, 0, 0, 0, 0, plus3), time.Date(2026, 9, 17, 0, 0, 0, 0, plus3)),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, UTCOffsets: offsets,
			}},
			Reducers: reducers("count"),
		})
		var out []string
		for _, row := range res.Rows {
			out = append(out, row.Start.Format(time.RFC3339)+".."+row.End.Format(time.RFC3339))
		}
		return strings.Join(out, " ")
	}
	if got, want := read(&substrate.AggregateRecordOffsets{Start: "utcOffsetStart", End: "utcOffsetEnd"}),
		"2026-09-14T23:00:00+01:00..2026-09-15T07:00:00+03:00 2026-09-16T01:00:00+03:00..2026-09-16T07:00:00+03:00"; got != want {
		t.Errorf("at the records' offsets:\n got %s\nwant %s", got, want)
	}
	if got, want := read(&substrate.AggregateRecordOffsets{Start: "utcOffsetStart"}),
		"2026-09-14T23:00:00+01:00..2026-09-15T05:00:00+01:00 2026-09-16T01:00:00+03:00..2026-09-16T07:00:00+03:00"; got != want {
		t.Errorf("at the start's offset:\n got %s\nwant %s", got, want)
	}
	if got, want := read(nil),
		"2026-09-15T01:00:00+03:00..2026-09-15T07:00:00+03:00 2026-09-16T01:00:00+03:00..2026-09-16T07:00:00+03:00"; got != want {
		t.Errorf("at the range's offset:\n got %s\nwant %s", got, want)
	}
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
		Range:  rng(start, end),
		Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
			Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, UTCOffsets: &substrate.AggregateRecordOffsets{Start: "timeAsleep"},
		}},
		Reducers: reducers("count"),
	}}, "aggregate.bucket.records.utcOffsets.start")
}

// A records-mode window is [at, endsAt): WHOOP stamps a cycle's recovery at
// the instant the night before it ends, so the night's window leaves it out,
// and widening the window by a minute after its end takes it, the night and
// its recovery on one row (docs/observations.md, a night and its recovery).
func TestAggregateRecordWindowTakesItsRecoveryWhenWidened(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 10, 9, 21, 30, 0, 0, time.UTC)
	end := time.Date(2026, 10, 10, 5, 45, 0, 160000000, time.UTC)
	f.mustPut(t, f.sleepKind(), "whoop/night", map[string]any{
		"source": f.sourceRef("whoop"), "at": start.Format(time.RFC3339Nano), "endsAt": end.Format(time.RFC3339Nano),
	})
	f.mustPut(t, f.observation, "whoop/recovery", map[string]any{
		"metric": f.metricRef("health/whoop_recovery"), "source": f.sourceRef("whoop"), "form": "summary",
		"statistic": "vendor", "period": "night", "value": quantity("78", "%"),
		"at": end.Format(time.RFC3339Nano), "endsAt": end.Add(24 * time.Hour).Format(time.RFC3339Nano), "utcOffset": 180,
	})
	read := func(around *substrate.AggregateAround) *substrate.AggregateResult {
		return f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/whoop_recovery")},
			From:   "summaries",
			Range:  rng(start.Add(-time.Hour), end),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, Around: around,
			}},
			Reducers: reducers("last"),
		})
	}
	if res := read(nil); len(res.Rows) != 0 {
		t.Errorf("the night's own window: %d rows, want none (the recovery is stamped at its end)", len(res.Rows))
	}
	res := read(&substrate.AggregateAround{After: "PT1M"})
	if len(res.Rows) != 1 || res.Rows[0].Record == nil || res.Rows[0].Record.Ref != f.sleepKind()+"/whoop/night" {
		t.Fatalf("widened: %+v, want the night's one row", res.Rows)
	}
	if v, _ := val(res.Rows[0], "last"); v != "78" {
		t.Errorf("the night's recovery = %s, want 78", v)
	}
}

// maxBuckets counts the windows the records mode answers, after its source
// policy drops the nights a higher-ranked source wrote too: six sleep
// records of three nights are three windows under priority, read under
// maxBuckets 3 and refused under 2. maxRecords still bounds every record
// the read weighs.
func TestAggregateRecordWindowsCountTheWindowsKeptAgainstMaxBuckets(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	stages := []stage{{"light", 60}, {"deep", 45}}
	plus3 := time.FixedZone("", 3*3600)
	night := time.Date(2026, 9, 1, 23, 0, 0, 0, plus3)
	for n := range 3 {
		for i, source := range []string{"whoop", "garmin"} {
			at := night.AddDate(0, 0, n).Add(time.Duration(i) * time.Minute)
			f.sleepNight(source, n, at, 2*time.Hour, at, stages)
		}
	}
	read := func(maxBuckets, maxRecords int) (*substrate.AggregateResult, error) {
		return f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:  rng(night.Add(-time.Hour), night.AddDate(0, 0, 3)),
			Bucket: &substrate.AggregateBucket{Records: &substrate.AggregateRecords{
				Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}, MaxRecords: maxRecords,
				Sources: &substrate.AggregateRecordSources{Policy: "priority", Order: []string{"whoop", "garmin"}, Property: "source"},
			}},
			Reducers: reducers("sum"), Unit: "min",
			Sources: &substrate.AggregateSources{Policy: "priority", Order: []string{"whoop", "garmin"}},
			Budget:  &substrate.AggregateBudget{MaxBuckets: maxBuckets},
		}})
	}
	res, err := read(3, 0)
	if err != nil || len(res.Rows) != 3 {
		t.Fatalf("priority under maxBuckets 3: %v, %v; want the three whoop nights", rowsOf(res), err)
	}
	if _, err := read(2, 0); err == nil || !strings.Contains(err.Error(), "aggregate.budget.maxBuckets") {
		t.Errorf("priority under maxBuckets 2: %v, want a refusal of three windows", err)
	}
	if _, err := read(3, 5); err == nil || !strings.Contains(err.Error(), "aggregate.bucket.records.maxRecords") {
		t.Errorf("six records under maxRecords 5: %v, want a refusal naming maxRecords", err)
	}
}
