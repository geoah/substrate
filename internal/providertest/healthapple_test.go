package providertest

// The healthapple suite: the sample adapter that writes the applehealth
// provider's mirror rows as the health sample's records. Each case installs
// the provider, imports health (and with it observations and the metric
// catalogue) and healthapple, writes mirror rows by running the provider's
// function over the research deliveries, and lets the dispatcher deliver
// those rows to the adapter through its record trigger.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	haObs         = "samples.substrate.reamde.dev/observations/"
	haTrace       = haObs + "trace"
	haObservation = haObs + "observation"
	haSource      = haObs + "source"
	haMetric      = haObs + "metric/health/"
	haSleep       = "samples.substrate.reamde.dev/health/sleep"
	haWorkout     = "samples.substrate.reamde.dev/health/workout"
	haAdapt       = "samples.substrate.reamde.dev/healthapple/adaptapplehealth"
)

// newHealthApple is a repository with the provider, the health sample, the
// adapter and one account, `iphone`.
func newHealthApple(t *testing.T) substrate.Dataset {
	t.Helper()
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, appleHealthDir, nil)
	importVocabulary(t, ds, "health", "healthapple")
	mustPut(t, ds, substrate.PutInput{
		Kind: ahAccount, ID: "iphone",
		Properties: map[string]any{"displayName": "Ada's iPhone"},
	})
	return ds
}

// deliverAt runs one delivery through the provider, received at `at`, and
// then the dispatcher, so the adapter sees every mirror row it changed.
func deliverAt(t *testing.T, ds substrate.Dataset, req map[string]any, fire, at string) {
	t.Helper()
	s := newStepper(t, ds, appleHealthIngest, ahConfig(t, ds))
	s.setEnvelope(ahEnvelope(req, fire, at))
	var resume any
	for range 40 {
		_, _, cur := s.step(resume)
		s.apply()
		if cur == nil {
			drainTriggers(t, ds)
			if n := parkedFailures(t, ds); n != 0 {
				t.Fatalf("%d deliveries parked", n)
			}
			return
		}
		resume = cur
	}
	t.Fatal("the provider delivery did not finish in 40 pages")
}

// adapt runs the adapter once over one mirror row as it stands, the way the
// record trigger delivers it, applying what it stages, and returns its output.
func adapt(t *testing.T, ds substrate.Dataset, kind, id string) map[string]any {
	t.Helper()
	row := mustGet(t, ds, kind, id)
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	s := newStepper(t, ds, haAdapt, nil)
	s.setEnvelope(map[string]any{
		"change": map[string]any{"op": "update", "kind": kind, "id": id},
		"record": record,
	})
	_, out, _ := s.step(nil)
	s.apply()
	m, _ := out.(map[string]any)
	return m
}

// outputsOf lists the adapter's outputs that point at one mirror row.
func outputsOf(t *testing.T, ds substrate.Dataset, origin string) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{haTrace, haObservation, haSleep, haWorkout},
			Referencing: &substrate.Referencing{Ref: origin},
		},
		First: 500,
	})
	if err != nil {
		t.Fatalf("list the outputs of %s: %v", origin, err)
	}
	return page.Records
}

func strs(v any) []string {
	list, _ := v.([]any)
	out := make([]string, len(list))
	for i, item := range list {
		switch v := item.(type) {
		case nil:
			out[i] = "null"
		case float64:
			out[i] = strconv.FormatFloat(v, 'f', -1, 64)
		default:
			out[i] = fmt.Sprint(v)
		}
	}
	return out
}

func unixOf(t *testing.T, s string) float64 {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return float64(at.Unix())
}

// A day of per-minute data: heart rate as hourly gauge traces per source at
// irregular offsets, steps as amount traces whose buckets end in a null where
// the next minute has no row, distance and energy converted into the metric's
// unit, the engine's stats on every trace, and one source record per device.
func TestHealthAppleMinuteTraces(t *testing.T) {
	ds := newHealthApple(t)
	const received = "2026-10-08T05:31:02Z"
	deliverAt(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-minutes", received)

	hr := mustGet(t, ds, haTrace, "applehealth/iphone/apple-watch/heart_rate/2026-10-07T07")
	p := hr.Properties
	if p["form"] != "gauge" || p["unit"] != "/min" || p["hold"] != "PT4M" || num(t, p["utcOffsetStart"]) != 180 ||
		storedRefPath(p["metric"]) != haMetric+"heart_rate" ||
		storedRefPath(p["source"]) != haSource+"/applehealth/iphone/apple-watch" ||
		num(t, p["revision"]) != unixOf(t, received) {
		t.Fatalf("heart rate trace = %v", p)
	}
	if got := refIDs(hr, "origins"); len(got) != 1 || got[0] != "iphone/heart_rate/minutes/2026-10-07T07" {
		t.Fatalf("origins = %v", got)
	}
	wantOffsets, wantValues := []string{}, []string{}
	for _, row := range ahMetrics(t, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"))["heart_rate"].Data {
		at := ahInstant(t, row["date"].(string))
		if at.Format("2006-01-02T15") != "2026-10-07T07" {
			continue
		}
		wantOffsets = append(wantOffsets, fmt.Sprint(at.Sub(time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC)).Milliseconds()))
		wantValues = append(wantValues, fmt.Sprint(row["Avg"]))
	}
	if strings.Join(strs(p["offsets"]), ",") != strings.Join(wantOffsets, ",") ||
		strings.Join(strs(p["values"]), ",") != strings.Join(wantValues, ",") {
		t.Fatalf("offsets %v values %v, want %v %v", p["offsets"], p["values"], wantOffsets, wantValues)
	}
	stats, _ := p["stats"].(map[string]any)
	if stats == nil || num(t, stats["count"]) != float64(len(wantValues)) {
		t.Fatalf("the engine's stats = %v, want a count of %d", p["stats"], len(wantValues))
	}

	// Steps: a merged `Watch|iPhone` label is the phone's merged source.
	steps := mustGet(t, ds, haTrace, "applehealth/iphone/health/steps/2026-10-07T04").Properties
	vals, offs := strs(steps["values"]), strs(steps["offsets"])
	if steps["form"] != "amount" || steps["unit"] != "{steps}" || steps["hold"] != nil ||
		vals[0] != "47.6988740604333" || vals[1] != "null" || offs[0] != "120000" || offs[1] != "180000" ||
		offs[2] != "420000" {
		t.Fatalf("steps trace = offsets %v values %v", offs[:4], vals[:4])
	}
	dist := mustGet(t, ds, haTrace, "applehealth/iphone/health/distance/2026-10-07T04").Properties
	if dist["unit"] != "m" || strs(dist["values"])[0] != "33.8662005829" {
		t.Fatalf("distance in m = %v", strs(dist["values"])[:2])
	}
	energy := mustGet(t, ds, haTrace, "applehealth/iphone/apple-watch/active_energy/2026-10-07T04").Properties
	if energy["unit"] != "kcal" || !strings.HasPrefix(strs(energy["values"])[0], "0.0843") {
		t.Fatalf("active energy in kcal = %v", strs(energy["values"])[:2])
	}

	watch := mustGet(t, ds, haSource, "applehealth/iphone/apple-watch").Properties
	merged := mustGet(t, ds, haSource, "applehealth/iphone/health").Properties
	if watch["name"] != adaWatch || watch["class"] != "watch" || storedRefPath(watch["account"]) != ahAccount+"/iphone" ||
		merged["name"] != "Apple Health on Ada's iPhone" || merged["class"] != "app" {
		t.Fatalf("sources = %v and %v", watch, merged)
	}
	// physical_effort has no health metric: its batches write nothing and say so.
	out := adapt(t, ds, ahBatch, "iphone/physical_effort/minutes/2026-10-07T04")
	if num(t, out["written"]) != 0 || !strings.Contains(fmt.Sprint(out["skipped"]), "physical_effort: no health metric maps it") {
		t.Fatalf("physical_effort output = %v", out)
	}
}

// Re-running a row that did not change writes nothing; the corrected
// redelivery rewrites the run's hour under the same ids at a higher revision,
// adds Garmin's heart rate as its own source, and leaves every other hour's
// traces as they were.
func TestHealthAppleRedelivery(t *testing.T) {
	ds := newHealthApple(t)
	const first, second = "2026-10-08T05:31:02Z", "2026-10-08T08:15:44Z"
	deliverAt(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-first", first)
	before := map[string]int64{}
	for _, row := range listLive(t, ds, haTrace) {
		before[row.ID] = row.Version
	}
	if out := adapt(t, ds, ahBatch, "iphone/heart_rate/minutes/2026-10-07T07"); num(t, out["written"]) != 0 || num(t, out["unchanged"]) == 0 {
		t.Fatalf("an unchanged row re-run wrote %v", out)
	}
	deliverAt(t, ds, ahRequest(t, "delivery_4_redelivery_corrected.json"), "hook-again", second)

	changed := []string{}
	for _, row := range listLive(t, ds, haTrace) {
		v, seen := before[row.ID]
		switch {
		case !seen:
			changed = append(changed, "+"+row.ID)
		case v != row.Version:
			changed = append(changed, row.ID)
			if num(t, row.Properties["revision"]) != unixOf(t, second) {
				t.Fatalf("%s changed at revision %v", row.ID, row.Properties["revision"])
			}
		}
	}
	sort.Strings(changed)
	want := []string{
		"+applehealth/iphone/garmin-connect/heart_rate/2026-10-07T05",
		"applehealth/iphone/health/distance/2026-10-07T05",
		"applehealth/iphone/health/steps/2026-10-07T05",
		"applehealth/iphone/iphone/distance/2026-10-07T05",
		"applehealth/iphone/iphone/steps/2026-10-07T05",
	}
	if strings.Join(changed, " ") != strings.Join(want, " ") {
		t.Fatalf("changed traces = %v, want %v", changed, want)
	}
	// The run's minutes (08:05 to 08:41 local) left the iPhone's trace for the
	// merged one: past 08:05 the iPhone's trace holds nothing but the null
	// that ends its 08:04 bucket.
	phone := mustGet(t, ds, haTrace, "applehealth/iphone/iphone/steps/2026-10-07T05").Properties
	offsets, values := strs(phone["offsets"]), strs(phone["values"])
	for i, off := range offsets {
		ms, _ := strconv.Atoi(off)
		if ms >= 300000 && ms < 2460000 && values[i] != "null" {
			t.Fatalf("the iPhone's steps trace still holds %s steps at %d ms", values[i], ms)
		}
	}
	merged := strs(mustGet(t, ds, haTrace, "applehealth/iphone/health/steps/2026-10-07T05").Properties["offsets"])
	if !strings.Contains(","+strings.Join(merged, ",")+",", ",360000,") {
		t.Fatalf("the merged steps trace has no 08:06 minute: %v", merged)
	}
}

// A deleted mirror row deletes what was written from it and nothing else.
func TestHealthAppleDeletedBatchDeletesItsOutputs(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-minutes", "2026-10-08T05:31:02Z")
	gone := ahBatch + "/iphone/step_count/minutes/2026-10-07T04"
	kept := ahBatch + "/iphone/heart_rate/minutes/2026-10-07T04"
	if len(outputsOf(t, ds, gone)) == 0 || len(outputsOf(t, ds, kept)) == 0 {
		t.Fatal("the batches wrote nothing to delete")
	}
	if _, err := ds.Delete(context.Background(), substrate.ActorAPI, ahBatch, "iphone/step_count/minutes/2026-10-07T04", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the batch: %v", err)
	}
	drainTriggers(t, ds)
	if left := outputsOf(t, ds, gone); len(left) != 0 {
		t.Fatalf("%d outputs of the deleted batch survive, first %s", len(left), left[0].ID)
	}
	if len(outputsOf(t, ds, kept)) == 0 {
		t.Fatal("deleting one batch deleted another's outputs")
	}
}

// Half an hour of heart rate sent again from London: the same instants under
// +0100. The batch then holds two offsets in one hour, and the hour's trace
// splits where the offset changes.
func TestHealthAppleTwoTimeZonesSplitATrace(t *testing.T) {
	ds := newHealthApple(t)
	athens := ahRequest(t, "delivery_1_heart_rate_steps_minutes.json")
	deliverAt(t, ds, athens, "hook-athens", "2026-10-08T05:31:02Z")

	london := time.FixedZone("", 3600)
	var rows []map[string]any
	for _, row := range ahMetrics(t, athens)["heart_rate"].Data {
		at := ahInstant(t, row["date"].(string))
		if at.Before(time.Date(2026, 10, 7, 7, 30, 0, 0, time.UTC)) || !at.Before(time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)) {
			continue
		}
		row["date"] = at.In(london).Format(appDate)
		rows = append(rows, row)
	}
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"metrics": []any{
		map[string]any{"name": "heart_rate", "units": "count/min", "data": rows},
	}}})
	req := ahRequest(t, "delivery_1_heart_rate_steps_minutes.json")
	req["body"] = map[string]any{"text": string(body)}
	deliverAt(t, ds, req, "hook-london", "2026-10-08T09:00:00Z")

	var parts []*substrate.Record
	for _, row := range outputsOf(t, ds, ahBatch+"/iphone/heart_rate/minutes/2026-10-07T07") {
		if row.Kind == haTrace {
			parts = append(parts, row)
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].At.Before(*parts[j].At) })
	if len(parts) != 2 {
		t.Fatalf("%d traces for the hour, want 2", len(parts))
	}
	a, b := parts[0], parts[1]
	if a.ID != "applehealth/iphone/apple-watch/heart_rate/2026-10-07T07" || num(t, a.Properties["utcOffsetStart"]) != 180 ||
		num(t, b.Properties["utcOffsetStart"]) != 60 || !a.EndsAt.Equal(*b.At) ||
		!strings.HasPrefix(b.ID, "applehealth/iphone/apple-watch/heart_rate/2026-10-07T07:") {
		t.Fatalf("split = %s (%v, ends %v) and %s (%v, at %v)", a.ID, a.Properties["utcOffsetStart"], a.EndsAt,
			b.ID, b.Properties["utcOffsetStart"], b.At)
	}
	if len(strs(a.Properties["values"]))+len(strs(b.Properties["values"])) != 18 {
		t.Fatalf("the split lost points: %d and %d", len(strs(a.Properties["values"])), len(strs(b.Properties["values"])))
	}
}

// Weigh-ins, body fat, BMI, blood pressure and glucose are one observation per
// reading; lean body mass has no metric and is skipped.
func TestHealthAppleBodyObservations(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_3_body_measurements.json"), "hook-body", "2026-10-10T05:05:20Z")

	weight := mustGet(t, ds, haObservation, "applehealth/iphone/garmin-connect/body_mass/2026-09-11T04:02:41Z").Properties
	if weight["form"] != "gauge" || obj(t, weight["value"], "value") != "72.26" || obj(t, weight["value"], "unit") != "kg" ||
		num(t, weight["utcOffset"]) != 180 || storedRefPath(weight["origin"]) != ahBatch+"/iphone/weight_body_mass/seconds/2026-09-11T04" {
		t.Fatalf("weigh-in = %v", weight)
	}
	if c := mustGet(t, ds, haSource, "applehealth/iphone/garmin-connect").Properties["class"]; c != "service" {
		t.Fatalf("Garmin Connect's class = %v", c)
	}
	bmi := mustGet(t, ds, haObservation, "applehealth/iphone/garmin-connect/bmi/2026-09-11T04:02:41Z").Properties
	if obj(t, bmi["value"], "unit") != "kg/m2" {
		t.Fatalf("BMI = %v", bmi["value"])
	}
	sys := mustGet(t, ds, haObservation, "applehealth/iphone/garmin-connect/systolic_blood_pressure/2026-09-12T05:10:30Z").Properties
	dia := mustGet(t, ds, haObservation, "applehealth/iphone/garmin-connect/diastolic_blood_pressure/2026-09-12T05:10:30Z").Properties
	if obj(t, sys["value"], "value") != "118" || obj(t, dia["value"], "value") != "76" || obj(t, sys["value"], "unit") != "mm[Hg]" ||
		sys["hold"] != "PT0S" || len(sys["components"].([]any)) != 2 {
		t.Fatalf("blood pressure = %v / %v", sys, dia)
	}
	glucose := mustGet(t, ds, haObservation, "applehealth/iphone/iphone/blood_glucose_mass/2026-09-20T05:12:00Z").Properties
	if obj(t, glucose["value"], "unit") != "mg/dL" || obj(t, glucose["qualifiers"].([]any)[0], "code") != "before-meal" {
		t.Fatalf("glucose = %v", glucose)
	}
	for _, row := range listLive(t, ds, haObservation) {
		if strings.Contains(row.ID, "lean") {
			t.Fatalf("lean body mass was written as %s", row.ID)
		}
	}
	out := adapt(t, ds, ahBatch, "iphone/lean_body_mass/seconds/2026-09-11T04")
	if !strings.Contains(fmt.Sprint(out["skipped"]), "lean_body_mass: no health metric maps it") {
		t.Fatalf("lean body mass output = %v", out)
	}
}

// Sleep segments are coded stage amounts, one per segment, each the stage
// and its minutes; the night is one sleep record written by the batch its
// start falls in. A workout is a health workout attributed to the device its
// heart rate rows name.
func TestHealthAppleSleepAndWorkouts(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_2_sleep_and_workouts.json"), "hook-night", "2026-10-08T05:20:13Z")

	core := mustGet(t, ds, haObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-10-06T19:51:00Z").Properties
	if core["form"] != "amount" || core["code"] != "light" || obj(t, core["value"], "value") != "27" ||
		obj(t, core["value"], "unit") != "min" || core["endsAt"] != "2026-10-06T20:18:00Z" {
		t.Fatalf("a Core segment = %v", core)
	}
	stages := map[string]int{}
	for _, row := range listLive(t, ds, haObservation) {
		if storedRefPath(row.Properties["metric"]) == haMetric+"sleep_stage" {
			stages[fmt.Sprint(row.Properties["code"])]++
		}
	}
	if stages["light"] == 0 || stages["deep"] == 0 || stages["rem"] == 0 || stages["awake"] == 0 || stages["in_bed"] == 0 {
		t.Fatalf("stage amounts by code = %v", stages)
	}
	nights := listLive(t, ds, haSleep)
	if len(nights) != 2 {
		t.Fatalf("%d sleep records, want one per night", len(nights))
	}
	night := mustGet(t, ds, haSleep, "applehealth/iphone/health/sleep/2026-10-06T19:51:00Z").Properties
	if night["endsAt"] != "2026-10-07T03:55:49Z" || night["timeInBed"] == nil {
		t.Fatalf("the night = %v", night)
	}

	run := mustGet(t, ds, haWorkout, "applehealth/iphone/EB0B1D81-50F6-4AB6-8841-28B68D3DE5C6").Properties
	if run["sport"] != "running" || run["sportSource"] != "Running" ||
		storedRefPath(run["source"]) != haSource+"/applehealth/iphone/garmin-connect" ||
		obj(t, run["distance"], "value") != "6.1" || obj(t, run["distance"], "unit") != "km" ||
		obj(t, run["averageHeartRate"], "unit") != "/min" || run["duration"] != "PT36M19S" ||
		storedRefPath(run["origin"]) != ahWorkout+"/iphone/EB0B1D81-50F6-4AB6-8841-28B68D3DE5C6" {
		t.Fatalf("the run = %v", run)
	}
	lift := mustGet(t, ds, haWorkout, "applehealth/iphone/E956E694-D171-4E4D-922B-A0B57DC8E5FB").Properties
	if lift["sport"] != "strength" || storedRefPath(lift["source"]) != haSource+"/applehealth/iphone/apple-watch" {
		t.Fatalf("the strength workout = %v", lift)
	}
}

// The app's day buckets are its own aggregates: summaries, never raw points,
// so a raw read beside the minutes never counts them twice. Heart rate gives
// avg, min and max; steps a sum; resting heart rate Apple's own daily value;
// an aggregated night a sleep record with its stage totals.
func TestHealthAppleDailySummaries(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_0_daily_30d.json"), "hook-daily", "2026-10-10T05:02:11Z")

	if n := countLive(t, ds, haTrace); n != 0 {
		t.Fatalf("a daily delivery wrote %d traces", n)
	}
	steps := mustGet(t, ds, haObservation, "applehealth/iphone/health/steps/2026-09-10T21:00:00Z/sum").Properties
	if steps["form"] != "summary" || steps["statistic"] != "sum" || steps["period"] != "day" ||
		steps["periodId"] != "2026-09-11" || obj(t, steps["value"], "value") != "11187" ||
		steps["endsAt"] != "2026-09-11T21:00:00Z" {
		t.Fatalf("daily steps = %v", steps)
	}
	for _, stat := range []string{"avg", "min", "max"} {
		mustGet(t, ds, haObservation, "applehealth/iphone/apple-watch/heart_rate/2026-09-10T21:00:00Z/"+stat)
	}
	rest := mustGet(t, ds, haObservation, "applehealth/iphone/health/resting_heart_rate/2026-09-10T21:00:00Z/vendor").Properties
	if rest["statistic"] != "vendor" || obj(t, rest["value"], "value") != "49" || obj(t, rest["value"], "unit") != "/min" {
		t.Fatalf("resting heart rate = %v", rest)
	}
	night := mustGet(t, ds, haSleep, "applehealth/iphone/apple-watch/sleep/2026-09-10T20:04:41Z").Properties
	if night["light"] == nil || night["deep"] == nil || night["rem"] == nil || night["timeAsleep"] == nil {
		t.Fatalf("an aggregated night = %v", night)
	}
	deep := mustGet(t, ds, haObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-09-10T20:04:41Z/deep").Properties
	if deep["code"] != "deep" || obj(t, deep["value"], "value") != "91" {
		t.Fatalf("the night's deep total = %v", deep)
	}
	out := adapt(t, ds, ahBatch, "iphone/walking_asymmetry_percentage/days/2026-09")
	if !strings.Contains(fmt.Sprint(out["skipped"]), "walking_asymmetry_percentage: no health metric maps it") {
		t.Fatalf("walking asymmetry output = %v", out)
	}
}
