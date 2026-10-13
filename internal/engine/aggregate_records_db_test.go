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
