package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// parityStream is one generated stream: the metric it is written under and
// its form.
type parityStream struct {
	metric, form string
	offsetless   bool
}

// writeParityStreams writes three days of generated readings for one seed:
// a gauge in two units of one measure with gaps, nulls, holds and a clock
// change; a coded gauge; an amount on a +05:30 trip with nulls and codes;
// a counter that resets; a gauge written without an offset; and, under each
// metric's own tiers, corrections and deletions. A censored value sends its
// group to raw points before any row is merged (planGroup), which the
// aggregate tests of censored values hold.
func writeParityStreams(t *testing.T, f aggFix, rng *rand.Rand, seed int64) []parityStream {
	t.Helper()
	ctx := context.Background()
	start := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	hours := 72
	tierSets := [][]any{{"15m", "1h", "1d"}, {"5m", "1h"}, {"15m", "1d"}, {"1h"}, {"5m", "15m", "1h", "1d"}}
	metric := func(name, measures, unit, form string) string {
		id := fmt.Sprintf("parity%d/%s", seed, name)
		props := map[string]any{"name": name, "form": form, "rollups": tierSets[rng.Intn(len(tierSets))]}
		if measures != "" {
			props["measures"] = measures
		}
		if unit != "" {
			props["unit"] = unit
		}
		f.mustPut(t, f.metric, id, props)
		return id
	}
	berlin := func(at time.Time) int {
		if at.Before(time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)) {
			return 120
		}
		return 60
	}
	var out []parityStream
	temp := metric("temperature", "temperature", "Cel", "gauge")
	doors := metric("door", "", "", "gauge")
	steps := metric("steps", "", "{steps}", "amount")
	meter := metric("meter", "energy", "kW.h", "counter")
	cellar := metric("cellar", "temperature", "Cel", "gauge")
	out = append(out, parityStream{temp, "gauge", false}, parityStream{doors, "gauge", false},
		parityStream{steps, "amount", false}, parityStream{meter, "counter", false}, parityStream{cellar, "gauge", true})
	total := 1000
	var written []string
	for h := range hours {
		if rng.Intn(9) == 0 {
			continue
		}
		at := start.Add(time.Duration(h) * time.Hour)
		trace := func(id string, props map[string]any, step func() time.Duration, value func(i int) any) {
			var offsets []int64
			var vals []any
			for o := time.Duration(0); o < time.Hour; o += step() {
				offsets = append(offsets, o.Milliseconds())
				vals = append(vals, value(len(vals)))
			}
			props["at"], props["endsAt"] = at.Format(time.RFC3339), at.Add(time.Hour).Format(time.RFC3339)
			props["offsets"] = anySlice(offsets)
			key := "values"
			if _, ok := props["unit"]; !ok {
				key = "codes"
			}
			props[key] = vals
			f.mustPut(t, f.trace, id, props)
			written = append(written, id)
		}
		step := func(lo, hi int) func() time.Duration {
			return func() time.Duration { return time.Duration(lo+rng.Intn(hi-lo+1)) * time.Minute }
		}
		unit, scale := "Cel", 1.0
		if h%5 == 0 {
			unit, scale = "[degF]", 1.8
		}
		props := map[string]any{"metric": f.metricRef(temp), "source": f.sourceRef("attic"), "form": "gauge", "unit": unit, "utcOffset": berlin(at)}
		if rng.Intn(3) == 0 {
			props["hold"] = fmt.Sprintf("PT%dM", 2+rng.Intn(10))
		}
		trace(fmt.Sprintf("p%d/temp/%d", seed, h), props, step(1, 9), func(int) any {
			if rng.Intn(14) == 0 {
				return nil
			}
			c := 18 + rng.Float64()*6
			if scale != 1 {
				c = c*scale + 32
			}
			return strconv.FormatFloat(math.Round(c*100)/100, 'f', -1, 64)
		})
		codes := []string{"open", "closed", "ajar"}
		trace(fmt.Sprintf("p%d/door/%d", seed, h), map[string]any{
			"metric": f.metricRef(doors), "source": f.sourceRef("hall"), "form": "gauge", "utcOffset": berlin(at),
		}, step(4, 20), func(int) any { return codes[rng.Intn(len(codes))] })
		trace(fmt.Sprintf("p%d/steps/%d", seed, h), map[string]any{
			"metric": f.metricRef(steps), "source": f.sourceRef("phone"), "form": "amount", "unit": "{steps}", "utcOffset": 330,
		}, step(2, 12), func(int) any {
			if rng.Intn(10) == 0 {
				return nil
			}
			return strconv.Itoa(rng.Intn(200))
		})
		trace(fmt.Sprintf("p%d/meter/%d", seed, h), map[string]any{
			"metric": f.metricRef(meter), "source": f.sourceRef("meter"), "form": "counter", "unit": "kW.h",
			"resetRule": "monotonic", "utcOffset": berlin(at),
		}, step(5, 15), func(int) any {
			if rng.Intn(30) == 0 {
				total = rng.Intn(5)
			} else {
				total += rng.Intn(40)
			}
			return strconv.Itoa(total) + "." + strconv.Itoa(rng.Intn(10))
		})
		trace(fmt.Sprintf("p%d/cellar/%d", seed, h), map[string]any{
			"metric": f.metricRef(cellar), "source": f.sourceRef("cellar"), "form": "gauge", "unit": "Cel",
		}, step(10, 30), func(int) any { return strconv.FormatFloat(10+float64(rng.Intn(400))/100, 'f', -1, 64) })
	}
	// A coded amount, single records across the days.
	for i := range 12 {
		at := start.Add(time.Duration(rng.Intn(hours*60)) * time.Minute)
		f.mustPut(t, f.observation, fmt.Sprintf("p%d/nap/%d", seed, i), map[string]any{
			"metric": f.metricRef(steps), "source": f.sourceRef("watch"), "form": "amount", "code": []string{"walk", "run"}[i%2],
			"value": quantity(strconv.Itoa(10+rng.Intn(90)), "{steps}"), "at": at.Format(time.RFC3339),
			"endsAt": at.Add(time.Duration(5+rng.Intn(50)) * time.Minute).Format(time.RFC3339), "utcOffset": 330,
		})
	}
	// Corrections and deletions.
	for range len(written) / 8 {
		id := written[rng.Intn(len(written))]
		if rng.Intn(2) == 0 {
			if _, err := f.ds.Delete(ctx, owner, f.trace, id, substrate.DeleteInput{}); err != nil {
				t.Fatalf("delete %s: %v", id, err)
			}
			continue
		}
		rec, err := f.ds.Get(ctx, f.trace, id)
		if err != nil {
			continue
		}
		props := rec.Properties
		key := "values"
		if _, ok := props[key]; !ok {
			key = "codes"
		}
		vals, _ := props[key].([]any)
		if len(vals) > 2 {
			props[key] = vals[:len(vals)-1]
			props["offsets"] = props["offsets"].([]any)[:len(vals)-1]
		}
		props["revision"] = 2
		f.mustPut(t, f.trace, id, props)
	}
	return out
}

// Generated streams answer the same through their rollup rows, in whatever
// tiers their metrics name, and through the raw points alone: every form,
// each bucket mode, aligned and unaligned ranges and a tight maxGap. Floats
// a coarser tier added in another order may differ in their last bits, and
// nothing else.
func TestObservedRollupsAnswerAsRawPoints(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	var served int64
	reads := 0
	for seed := int64(1); seed <= 3; seed++ {
		gen := rand.New(rand.NewSource(seed))
		streams := writeParityStreams(t, f, gen, seed)
		start := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
		end := start.Add(72 * time.Hour)
		for _, s := range streams {
			reds := map[string][]string{
				"gauge":   {"avg", "min", "max", "count", "first", "last", "stddev"},
				"amount":  {"sum", "count", "min", "max"},
				"counter": {"increase", "rate", "count"},
			}[s.form]
			if s.metric == fmt.Sprintf("parity%d/door", seed) {
				reds = []string{"count", "first", "last", "timeInState"}
			}
			buckets := []substrate.AggregateBucket{
				{Every: "PT15M"},
				{Every: "PT1H"},
				{Every: "P1D"},
				{Every: "PT6H", Origin: "2026-10-23T00:05:00Z"},
				{Calendar: "day", Timezone: "Europe/Berlin"},
				{Calendar: "hour", Timezone: "Asia/Kolkata"},
			}
			if !s.offsetless {
				buckets = append(buckets, substrate.AggregateBucket{Calendar: "day"})
			}
			ranges := [][2]time.Time{{start, end}, {start.Add(7 * time.Minute), end.Add(-53 * time.Minute)}}
			for _, b := range buckets {
				for _, rg := range ranges {
					for _, maxGap := range []string{"", "PT20M"} {
						if maxGap != "" && s.form != "gauge" {
							continue
						}
						bucket := b
						a := substrate.Aggregate{
							Metric:   &substrate.AggregateMetric{Ref: f.metricPath(s.metric)},
							Range:    rng(rg[0], rg[1]),
							Bucket:   &bucket,
							Reducers: reducers(reds...),
							MaxGap:   maxGap,
						}
						ctx, trace := engine.AggregateTraced(context.Background())
						rollups := f.agg(ctx, substrate.Filter{}, a)
						raw := f.agg(engine.AggregateRaw(context.Background()), substrate.Filter{}, a)
						name := fmt.Sprintf("seed %d %s %+v %v maxGap %q", seed, s.metric, b, rg, maxGap)
						sameRowsWithin(t, name, rollups, raw)
						served += trace.RollupRows()
						reads++
					}
				}
			}
		}
	}
	if served == 0 {
		t.Fatal("no read was served from rollup rows: the comparison proved nothing")
	}
	t.Logf("%d reads, %d rollup rows served", reads, served)
}

// sameRowsWithin fails unless two results hold the same rows, every number
// within a relative 1e-9 of the other's and everything else equal.
func sameRowsWithin(t *testing.T, name string, a, b *substrate.AggregateResult) {
	t.Helper()
	ja, _ := json.Marshal(a.Rows)
	jb, _ := json.Marshal(b.Rows)
	var va, vb any
	_ = json.Unmarshal(ja, &va)
	_ = json.Unmarshal(jb, &vb)
	if path, ok := closeJSON(va, vb, "rows"); !ok {
		t.Fatalf("%s: rollups and raw points answer differently at %s:\nrollups %s\nraw     %s", name, path, ja, jb)
	}
}

func closeJSON(a, b any, path string) (string, bool) {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return path, false
		}
		for k, v := range x {
			if p, ok := closeJSON(v, y[k], path+"."+k); !ok {
				return p, false
			}
		}
		return "", true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return path, false
		}
		for i := range x {
			if p, ok := closeJSON(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); !ok {
				return p, false
			}
		}
		return "", true
	case string:
		y, ok := b.(string)
		if !ok {
			return path, false
		}
		if x == y {
			return "", true
		}
		fx, ex := strconv.ParseFloat(x, 64)
		fy, ey := strconv.ParseFloat(y, 64)
		if ex == nil && ey == nil && math.Abs(fx-fy) <= 1e-9*math.Max(1, math.Abs(fy)) {
			return "", true
		}
		return path, false
	}
	if fmt.Sprint(a) != fmt.Sprint(b) {
		return path, false
	}
	return "", true
}
