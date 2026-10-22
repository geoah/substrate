package engine_test

// A logarithmic measure in the aggregate read: what adds or averages
// decibels is refused before any point is reduced, on every form and every
// read; what reads a level as it is answers.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// level writes one observation of a metric from source "meter": a gauge held
// an hour, an amount over the hour, a counter's running total or a day
// summary of a statistic.
func (f aggFix) level(id, metric, form, value, unit string, at time.Time, extra map[string]any) {
	f.t.Helper()
	props := map[string]any{
		"metric": f.metricRef(metric), "source": f.sourceRef("meter"), "form": form,
		"value": quantity(value, unit), "at": at.Format(time.RFC3339), "utcOffset": 0,
	}
	switch form {
	case "gauge":
		props["hold"] = "PT1H"
	case "amount", "summary":
		props["endsAt"] = at.Add(time.Hour).Format(time.RFC3339)
	}
	for k, v := range extra {
		props[k] = v
	}
	f.mustPut(f.t, f.observation, id, props)
}

// 61 dB[SPL] and 62 dB[SPL] are not 123 dB[SPL], and 60 and 80 dB[SPL] held
// an hour each average to 77.0 dB[SPL] of power, not 70: sum, increase and
// rate are refused on every logarithmic measure, and avg, twa and stddev on
// the levels of a power (sound-pressure-level, power-level), as reducers and
// as rolling reducers, on the metric, several-metric, summary and property
// reads. A level against no reference (a hearing threshold in dB) and an
// acidity still average, and max and percentile read any level.
func TestAggregateRefusesWhatAddsOrAveragesALevel(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	putShippedUnits(t, f.ds, "dBSPL", "dBm", "dB", "pH")
	for id, unit := range map[string]string{
		"noise": "dBSPL", "dose": "dBSPL", "signal": "dBm",
		"threshold": "dB", "gain": "dB", "acidity": "pH",
	} {
		f.mustPut(t, f.metric, id, map[string]any{"name": id, "unit": unitRef(unit)})
	}
	f.level("noise/1", "noise", "gauge", "60", "dB[SPL]", at, nil)
	f.level("noise/2", "noise", "gauge", "80", "dB[SPL]", at.Add(time.Hour), nil)
	f.level("noise/day", "noise", "summary", "70", "dB[SPL]", at, map[string]any{"statistic": "sum", "period": "day"})
	f.level("dose/1", "dose", "amount", "61", "dB[SPL]", at, nil)
	f.level("dose/2", "dose", "amount", "62", "dB[SPL]", at.Add(time.Hour), nil)
	f.level("signal/1", "signal", "counter", "-40", "dBm", at, nil)
	f.level("signal/2", "signal", "counter", "-30", "dBm", at.Add(time.Hour), nil)
	f.level("threshold/1", "threshold", "gauge", "20", "dB", at, nil)
	f.level("threshold/2", "threshold", "gauge", "30", "dB", at.Add(time.Hour), nil)
	f.level("gain/1", "gain", "amount", "3", "dB", at, nil)
	f.level("acidity/1", "acidity", "gauge", "6", "[pH]", at, nil)
	f.level("acidity/2", "acidity", "gauge", "7", "[pH]", at.Add(time.Hour), nil)

	day := rng(at.Add(-8*time.Hour), at.Add(16*time.Hour))
	read := func(metric string, a substrate.Aggregate) substrate.AggregateQuery {
		a.Metric = &substrate.AggregateMetric{Ref: f.metricPath(metric)}
		a.Range = day
		return substrate.AggregateQuery{Aggregate: &a}
	}
	hours := &substrate.AggregateBucket{Every: "PT1H"}
	for _, x := range []struct {
		label string
		q     substrate.AggregateQuery
		want  string
	}{
		{"gauge avg", read("noise", substrate.Aggregate{Reducers: reducers("max", "avg")}), "aggregate.reducers[1]: avg does not apply to " + f.metricPath("noise") + ": dBSPL is the level of a power"},
		{"gauge stddev", read("noise", substrate.Aggregate{Reducers: reducers("stddev")}), "aggregate.reducers[0]: stddev does not apply"},
		{"amount sum", read("dose", substrate.Aggregate{Reducers: reducers("sum")}), "aggregate.reducers[0]: sum does not apply to " + f.metricPath("dose") + ": dBSPL is logarithmic"},
		{"amount avg", read("dose", substrate.Aggregate{Reducers: reducers("avg")}), "aggregate.reducers[0]: avg does not apply"},
		{"summary sum", read("noise", substrate.Aggregate{From: "summaries", Reducers: reducers("sum")}), "aggregate.reducers[0]: sum does not apply"},
		{"prefer-raw sum", read("dose", substrate.Aggregate{From: "prefer-raw", Reducers: reducers("sum")}), "aggregate.reducers[0]: sum does not apply"},
		{"counter increase", read("signal", substrate.Aggregate{Reducers: reducers("increase")}), "aggregate.reducers[0]: increase does not apply to " + f.metricPath("signal") + ": dBm is logarithmic"},
		{"counter rate", read("signal", substrate.Aggregate{Reducers: reducers("rate")}), "aggregate.reducers[0]: rate does not apply"},
		{"level sum", read("gain", substrate.Aggregate{Reducers: reducers("sum")}), "aggregate.reducers[0]: sum does not apply to " + f.metricPath("gain") + ": dB is logarithmic"},
		{"rolling sum", read("noise", substrate.Aggregate{Bucket: hours, Reducers: reducers("max"), Rolling: &substrate.AggregateRolling{Buckets: 3, Reducers: reducers("max", "sum")}}), "aggregate.rolling.reducers[1]: a rolling sum does not apply"},
		{"rolling avg", read("noise", substrate.Aggregate{Bucket: hours, Reducers: reducers("max"), Rolling: &substrate.AggregateRolling{Buckets: 3}}), "aggregate.rolling.reducers[0]: a rolling avg does not apply"},
		{"rolling level sum", read("threshold", substrate.Aggregate{Bucket: hours, Reducers: reducers("avg"), Rolling: &substrate.AggregateRolling{Buckets: 3, Reducers: reducers("sum")}}), "aggregate.rolling.reducers[0]: a rolling sum does not apply"},
		{"several metrics", substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metrics: []substrate.AggregateMetricRead{{Ref: f.metricPath("threshold")}, {Ref: f.metricPath("noise")}},
			Range:   day, Reducers: reducers("avg"),
		}}, "aggregate.reducers[0]: in aggregate.metrics[1] (" + f.metricPath("noise") + "), avg does not apply"},
	} {
		t.Run(x.label, func(t *testing.T) {
			f := f
			f.t = t
			f.refusedAgg(x.q, x.want)
		})
	}

	for _, x := range []struct {
		label, metric, reducer, want string
	}{
		{"spl max", "noise", "max", "80"},
		{"spl percentile", "noise", "p50", "70"},
		{"level avg", "threshold", "avg", "25"},
		{"acidity avg", "acidity", "avg", "6.5"},
		{"amount max", "dose", "max", "62"},
	} {
		a := substrate.Aggregate{Reducers: reducers(x.reducer)}
		if x.reducer == "p50" {
			half := 0.5
			a.Reducers = []substrate.AggregateReducer{{Name: "percentile", Percentile: &half}}
		}
		res := f.agg(ctx, substrate.Filter{}, *read(x.metric, a).Aggregate)
		if len(res.Rows) != 1 || len(res.Rows[0].Values) != 1 || res.Rows[0].Values[0].Value == nil || *res.Rows[0].Values[0].Value != x.want {
			t.Errorf("%s: %+v, want one row of %s", x.label, res.Rows, x.want)
		}
	}
	res := f.agg(ctx, substrate.Filter{}, *read("noise", substrate.Aggregate{
		Bucket: hours, Reducers: reducers("max"), Rolling: &substrate.AggregateRolling{Buckets: 2, Reducers: reducers("max")},
	}).Aggregate)
	if len(res.Rows) != 2 {
		t.Errorf("rolling max: %+v, want two hours", res.Rows)
	}
}

// A property read holds a declared unit's scale before its scan, and a
// quantity with no declared unit at the scale its first value's unit has.
func TestAggregatePropertyRefusesWhatAddsALevel(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	putShippedUnits(t, ds, "dBSPL")
	const pkg = "levels.example.substrate.reamde.dev/levels"
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabularyPackage(pkg),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "reading"}, map[string]any{
			"properties": map[string]any{
				"loudness": map[string]any{"type": "quantity", "unit": "dBSPL"},
				"level":    map[string]any{"type": "quantity"},
				"takenAt":  map[string]any{"type": "datetime"},
			},
		}),
	}); err != nil {
		t.Fatalf("install the reading kind: %v", err)
	}
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for i, v := range []string{"61", "62"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: pkg + "/reading", ID: v, Properties: map[string]any{
			"loudness": quantity(v, "dB[SPL]"), "level": quantity(v, "dB[SPL]"),
			"takenAt": at.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		}})
	}
	read := func(prop string, names ...string) (*substrate.AggregateResult, error) {
		return ds.Aggregate(context.Background(), substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{pkg + "/reading"}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: prop, Anchor: "takenAt"},
				Range:    rng(at.Add(-time.Hour), at.Add(3*time.Hour)),
				Reducers: reducers(names...),
			},
		})
	}
	for _, x := range []struct{ prop, reducer string }{{"loudness", "sum"}, {"loudness", "avg"}, {"level", "sum"}, {"level", "stddev"}} {
		_, err := read(x.prop, "count", x.reducer)
		if err == nil || !strings.Contains(err.Error(), "aggregate.reducers[1]: "+x.reducer+" does not apply to "+x.prop+": dBSPL") {
			t.Errorf("%s of %s: %v, want a 422 naming the reducer and the unit", x.reducer, x.prop, err)
		}
	}
	res, err := read("loudness", "count", "max")
	if err != nil {
		t.Fatalf("count and max of loudness: %v", err)
	}
	if v, _ := val(res.Rows[0], "max"); len(res.Rows) != 1 || v != "62" {
		t.Errorf("max of loudness: %+v, want one row of 62", res.Rows)
	}
}
