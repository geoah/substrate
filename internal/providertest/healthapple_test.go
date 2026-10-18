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
	"maps"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const haAdapt = "samples.substrate.reamde.dev/healthapple/adaptapplehealth"

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

// adapt runs the adapter once over one mirror row as its record trigger
// delivers it, applying what it stages, and returns its output.
func adapt(t *testing.T, ds substrate.Dataset, kind, id string) map[string]any {
	t.Helper()
	return adaptRow(t, ds, haAdapt, kind, id)
}

// outputsOf lists the adapter's outputs that point at one mirror row.
func outputsOf(t *testing.T, ds substrate.Dataset, origin string) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{obsTrace, obsObservation, healthSleep, healthWorkout},
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

// A day of per-minute data: heart rate as hourly gauge traces per source at
// irregular offsets, steps as amount traces whose buckets end in a null where
// the next minute has no row, distance and energy converted into the metric's
// unit, the engine's stats on every trace, and one source record per device.
func TestHealthAppleMinuteTraces(t *testing.T) {
	ds := newHealthApple(t)
	const received = "2026-10-08T05:31:02Z"
	deliverAt(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-minutes", received)

	hr := mustGet(t, ds, obsTrace, "applehealth/iphone/apple-watch/heart_rate/2026-10-07T07")
	p := hr.Properties
	if p["form"] != "gauge" || p["unit"] != "/min" || p["hold"] != "PT4M" || asFloat(t, p["utcOffset"]) != 180 ||
		storedRefPath(p["metric"]) != healthMetrics+"heart_rate" ||
		storedRefPath(p["source"]) != obsSource+"/applehealth/iphone/apple-watch" ||
		asFloat(t, p["revision"]) != float64(asInstant(t, received).Unix()) {
		t.Fatalf("heart rate trace = %v", p)
	}
	if got := refIDs(hr, "origins"); len(got) != 1 || got[0] != "applehealth/iphone/heart_rate/minutes/2026-10-07T07" {
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
	if stats == nil || asFloat(t, stats["count"]) != float64(len(wantValues)) {
		t.Fatalf("the engine's stats = %v, want a count of %d", p["stats"], len(wantValues))
	}

	// Steps: a merged `Watch|iPhone` label is the phone's merged source.
	steps := mustGet(t, ds, obsTrace, "applehealth/iphone/health/steps/2026-10-07T04").Properties
	vals, offs := strs(steps["values"]), strs(steps["offsets"])
	if steps["form"] != "amount" || steps["unit"] != "{steps}" || steps["hold"] != nil ||
		vals[0] != "47.6988740604333" || vals[1] != "null" || offs[0] != "120000" || offs[1] != "180000" ||
		offs[2] != "420000" {
		t.Fatalf("steps trace = offsets %v values %v", offs[:4], vals[:4])
	}
	dist := mustGet(t, ds, obsTrace, "applehealth/iphone/health/distance/2026-10-07T04").Properties
	if dist["unit"] != "m" || strs(dist["values"])[0] != "33.8662005829" {
		t.Fatalf("distance in m = %v", strs(dist["values"])[:2])
	}
	energy := mustGet(t, ds, obsTrace, "applehealth/iphone/apple-watch/active_energy/2026-10-07T04").Properties
	if energy["unit"] != "kcal" || !strings.HasPrefix(strs(energy["values"])[0], "0.0843") {
		t.Fatalf("active energy in kcal = %v", strs(energy["values"])[:2])
	}

	watch := mustGet(t, ds, obsSource, "applehealth/iphone/apple-watch").Properties
	merged := mustGet(t, ds, obsSource, "applehealth/iphone/health").Properties
	if watch["name"] != adaWatch || watch["class"] != "watch" || storedRefPath(watch["account"]) != ahAccount+"/iphone" ||
		merged["name"] != "Apple Health on Ada's iPhone" || merged["class"] != "app" {
		t.Fatalf("sources = %v and %v", watch, merged)
	}
	// physical_effort has no health metric: its batches write nothing and say so.
	out := adapt(t, ds, ahBatch, "applehealth/iphone/physical_effort/minutes/2026-10-07T04")
	if asFloat(t, out["written"]) != 0 || !strings.Contains(fmt.Sprint(out["skipped"]), "physical_effort: no health metric maps it") {
		t.Fatalf("physical_effort output = %v", out)
	}
	assertCatalogued(t, ds)
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
	for _, row := range listLive(t, ds, obsTrace) {
		before[row.ID] = row.Version
	}
	if out := adapt(t, ds, ahBatch, "applehealth/iphone/heart_rate/minutes/2026-10-07T07"); asFloat(t, out["written"]) != 0 || asFloat(t, out["unchanged"]) == 0 {
		t.Fatalf("an unchanged row re-run wrote %v", out)
	}
	deliverAt(t, ds, ahRequest(t, "delivery_4_redelivery_corrected.json"), "hook-again", second)

	changed := []string{}
	for _, row := range listLive(t, ds, obsTrace) {
		v, seen := before[row.ID]
		switch {
		case !seen:
			changed = append(changed, "+"+row.ID)
		case v != row.Version:
			changed = append(changed, row.ID)
			if asFloat(t, row.Properties["revision"]) != float64(asInstant(t, second).Unix()) {
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
	phone := mustGet(t, ds, obsTrace, "applehealth/iphone/iphone/steps/2026-10-07T05").Properties
	offsets, values := strs(phone["offsets"]), strs(phone["values"])
	for i, off := range offsets {
		ms, _ := strconv.Atoi(off)
		if ms >= 300000 && ms < 2460000 && values[i] != "null" {
			t.Fatalf("the iPhone's steps trace still holds %s steps at %d ms", values[i], ms)
		}
	}
	merged := strs(mustGet(t, ds, obsTrace, "applehealth/iphone/health/steps/2026-10-07T05").Properties["offsets"])
	if !strings.Contains(","+strings.Join(merged, ",")+",", ",360000,") {
		t.Fatalf("the merged steps trace has no 08:06 minute: %v", merged)
	}
}

// A deleted mirror row deletes what was written from it and nothing else.
func TestHealthAppleDeletedBatchDeletesItsOutputs(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-minutes", "2026-10-08T05:31:02Z")
	gone := ahBatch + "/applehealth/iphone/step_count/minutes/2026-10-07T04"
	kept := ahBatch + "/applehealth/iphone/heart_rate/minutes/2026-10-07T04"
	if len(outputsOf(t, ds, gone)) == 0 || len(outputsOf(t, ds, kept)) == 0 {
		t.Fatal("the batches wrote nothing to delete")
	}
	if _, err := ds.Delete(context.Background(), substrate.ActorAPI, ahBatch, "applehealth/iphone/step_count/minutes/2026-10-07T04", substrate.DeleteInput{}); err != nil {
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
	for _, row := range outputsOf(t, ds, ahBatch+"/applehealth/iphone/heart_rate/minutes/2026-10-07T07") {
		if row.Kind == obsTrace {
			parts = append(parts, row)
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].At.Before(*parts[j].At) })
	if len(parts) != 2 {
		t.Fatalf("%d traces for the hour, want 2", len(parts))
	}
	a, b := parts[0], parts[1]
	if a.ID != "applehealth/iphone/apple-watch/heart_rate/2026-10-07T07" || asFloat(t, a.Properties["utcOffset"]) != 180 ||
		asFloat(t, b.Properties["utcOffset"]) != 60 || !a.EndsAt.Equal(*b.At) ||
		!strings.HasPrefix(b.ID, "applehealth/iphone/apple-watch/heart_rate/2026-10-07T07:") {
		t.Fatalf("split = %s (%v, ends %v) and %s (%v, at %v)", a.ID, a.Properties["utcOffset"], a.EndsAt,
			b.ID, b.Properties["utcOffset"], b.At)
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

	weight := mustGet(t, ds, obsObservation, "applehealth/iphone/garmin-connect/body_mass/2026-09-11T04:02:41Z").Properties
	if weight["form"] != "gauge" || obj(t, weight["value"], "value") != "72.26" || obj(t, weight["value"], "unit") != "kg" ||
		asFloat(t, weight["utcOffset"]) != 180 || storedRefPath(weight["origin"]) != ahBatch+"/applehealth/iphone/weight_body_mass/seconds/2026-09-11T04" {
		t.Fatalf("weigh-in = %v", weight)
	}
	if c := mustGet(t, ds, obsSource, "applehealth/iphone/garmin-connect").Properties["class"]; c != "service" {
		t.Fatalf("Garmin Connect's class = %v", c)
	}
	bmi := mustGet(t, ds, obsObservation, "applehealth/iphone/garmin-connect/bmi/2026-09-11T04:02:41Z").Properties
	if obj(t, bmi["value"], "unit") != "kg/m2" {
		t.Fatalf("BMI = %v", bmi["value"])
	}
	sys := mustGet(t, ds, obsObservation, "applehealth/iphone/garmin-connect/systolic_blood_pressure/2026-09-12T05:10:30Z").Properties
	dia := mustGet(t, ds, obsObservation, "applehealth/iphone/garmin-connect/diastolic_blood_pressure/2026-09-12T05:10:30Z").Properties
	if obj(t, sys["value"], "value") != "118" || obj(t, dia["value"], "value") != "76" || obj(t, sys["value"], "unit") != "mm[Hg]" ||
		sys["hold"] != "PT0S" || len(sys["components"].([]any)) != 2 {
		t.Fatalf("blood pressure = %v / %v", sys, dia)
	}
	glucose := mustGet(t, ds, obsObservation, "applehealth/iphone/iphone/blood_glucose_mass/2026-09-20T05:12:00Z").Properties
	if obj(t, glucose["value"], "unit") != "mg/dL" || obj(t, glucose["qualifiers"].([]any)[0], "code") != "before-meal" {
		t.Fatalf("glucose = %v", glucose)
	}
	for _, row := range listLive(t, ds, obsObservation) {
		if strings.Contains(row.ID, "lean") {
			t.Fatalf("lean body mass was written as %s", row.ID)
		}
	}
	out := adapt(t, ds, ahBatch, "applehealth/iphone/lean_body_mass/seconds/2026-09-11T04")
	if !strings.Contains(fmt.Sprint(out["skipped"]), "lean_body_mass: no health metric maps it") {
		t.Fatalf("lean body mass output = %v", out)
	}
	assertCatalogued(t, ds)
}

// HealthKit's own unit spellings reach the engine's table as the app sends
// them: a weigh-in in `lb` and glucose in HealthKit's molar-mass annotated
// mmol/L are read by the table's aliases, and a height in `ft` is a foot and
// a weigh-in in `st` a stone, which the adapter maps itself because UCUM's
// `ft` is the femtotonne and its `st` the stere.
func TestHealthAppleReadsHealthKitUnits(t *testing.T) {
	ds := newHealthApple(t)
	body, _ := json.Marshal(map[string]any{"data": map[string]any{"metrics": []any{
		map[string]any{"name": "weight_body_mass", "units": "lb", "data": []any{
			map[string]any{"qty": 160, "date": "2026-09-11 07:02:41 +0300", "source": "Garmin Connect"},
		}},
		map[string]any{"name": "blood_glucose", "units": "mmol<180.1558800000541>/L", "data": []any{
			map[string]any{"qty": 5.1, "date": "2026-09-20 08:12:00 +0300", "mealTime": "Before Meal", "source": "Ada’s iPhone"},
		}},
		map[string]any{"name": "height", "units": "ft", "data": []any{
			map[string]any{"qty": 5.75, "date": "2026-09-21 08:00:00 +0300", "source": "Ada’s iPhone"},
		}},
	}}})
	req := ahRequest(t, "delivery_3_body_measurements.json")
	req["body"] = map[string]any{"text": string(body)}
	deliverAt(t, ds, req, "hook-units", "2026-10-10T05:05:20Z")
	stone, _ := json.Marshal(map[string]any{"data": map[string]any{"metrics": []any{
		map[string]any{"name": "weight_body_mass", "units": "st", "data": []any{
			map[string]any{"qty": 11.5, "date": "2026-09-12 07:02:41 +0300", "source": "Garmin Connect"},
		}},
	}}})
	req = ahRequest(t, "delivery_3_body_measurements.json")
	req["body"] = map[string]any{"text": string(stone)}
	deliverAt(t, ds, req, "hook-stone", "2026-10-10T05:06:20Z")

	for _, c := range []struct{ id, value, unit string }{
		{"applehealth/iphone/garmin-connect/body_mass/2026-09-11T04:02:41Z", "72.5747792", "kg"},
		{"applehealth/iphone/garmin-connect/body_mass/2026-09-12T04:02:41Z", "73.02837157", "kg"},
		{"applehealth/iphone/iphone/blood_glucose/2026-09-20T05:12:00Z", "5.1", "mmol/L"},
		{"applehealth/iphone/iphone/height/2026-09-21T05:00:00Z", "1.7526", "m"},
	} {
		v := mustGet(t, ds, obsObservation, c.id).Properties["value"]
		if obj(t, v, "value") != c.value || obj(t, v, "unit") != c.unit {
			t.Errorf("%s = %v, want %s %s", c.id, v, c.value, c.unit)
		}
	}
	assertCatalogued(t, ds)
}

// Sleep segments are coded stage amounts, one per segment, each the stage
// and its minutes; the night is one sleep record written by the batch its
// start falls in. A workout is a health workout attributed to the device its
// heart rate rows name.
func TestHealthAppleSleepAndWorkouts(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_2_sleep_and_workouts.json"), "hook-night", "2026-10-08T05:20:13Z")

	core := mustGet(t, ds, obsObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-10-06T19:51:00Z").Properties
	if core["form"] != "amount" || core["code"] != "light" || obj(t, core["value"], "value") != "27" ||
		obj(t, core["value"], "unit") != "min" || core["endsAt"] != "2026-10-06T20:18:00Z" {
		t.Fatalf("a Core segment = %v", core)
	}
	stages := map[string]int{}
	for _, row := range listLive(t, ds, obsObservation) {
		if storedRefPath(row.Properties["metric"]) == healthMetrics+"sleep_stage" {
			stages[fmt.Sprint(row.Properties["code"])]++
		}
	}
	// The iPhone's In Bed segment spans the whole night under the watches'
	// stages: written as a stage it would count the night twice, so it is
	// the night's timeInBed and no stage.
	if stages["light"] == 0 || stages["deep"] == 0 || stages["rem"] == 0 || stages["awake"] == 0 || stages["in_bed"] != 0 {
		t.Fatalf("stage amounts by code = %v, want no in_bed", stages)
	}
	if out := adapt(t, ds, ahBatch, "applehealth/iphone/sleep_analysis/seconds/2026-10-06T19"); !strings.Contains(fmt.Sprint(out["skipped"]), "In Bed") {
		t.Fatalf("the sleep batch's output %v does not count the In Bed segment", out)
	}
	nights := listLive(t, ds, healthSleep)
	if len(nights) != 2 {
		t.Fatalf("%d sleep records, want one per night", len(nights))
	}
	// A night told by segments is the source most of its first hour's stages
	// come from (the watch and Garmin Connect tie; the first by name wins),
	// spans the time in bed, and carries what the night fields every segment
	// repeats say: the time in bed and how long it took to fall asleep. Its
	// stage totals are the stage amounts, which several batches hold.
	night := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-10-06T19:51:00Z").Properties
	if storedRefPath(night["source"]) != obsSource+"/applehealth/iphone/apple-watch" || night["at"] != "2026-10-06T19:29:00Z" ||
		night["endsAt"] != "2026-10-07T03:56:49Z" || night["isNap"] != false ||
		night["timeInBed"] != "PT8H27M49S" || night["latency"] != "PT22M" {
		t.Errorf("the night = %v, want the watch's, over the time in bed, 22 minutes to fall asleep, no nap", night)
	}

	run := mustGet(t, ds, healthWorkout, "applehealth/iphone/EB0B1D81-50F6-4AB6-8841-28B68D3DE5C6").Properties
	if run["sport"] != "running" || run["sportSource"] != "Running" ||
		storedRefPath(run["source"]) != obsSource+"/applehealth/iphone/garmin-connect" ||
		obj(t, run["distance"], "value") != "6.1" || obj(t, run["distance"], "unit") != "km" ||
		obj(t, run["averageHeartRate"], "unit") != "/min" || run["duration"] != "PT36M19S" ||
		run["activeEnergy"] == nil || run["totalEnergy"] != nil ||
		storedRefPath(run["origin"]) != ahWorkout+"/applehealth/iphone/EB0B1D81-50F6-4AB6-8841-28B68D3DE5C6" {
		t.Fatalf("the run = %v", run)
	}
	lift := mustGet(t, ds, healthWorkout, "applehealth/iphone/E956E694-D171-4E4D-922B-A0B57DC8E5FB").Properties
	if lift["sport"] != "strength" || storedRefPath(lift["source"]) != obsSource+"/applehealth/iphone/apple-watch" {
		t.Fatalf("the strength workout = %v", lift)
	}
	// Each is titled on the clock the app wrote it in, +03:00.
	for name, want := range map[string][2]any{
		"night": {night["name"], "Night of Tue 6 Oct"}, "run": {run["name"], "Running, Wed 7 Oct 08:05"},
		"lift": {lift["name"], "Strength, Wed 7 Oct 18:32"},
	} {
		if want[0] != want[1] {
			t.Errorf("the %s is named %v, want %q", name, want[0], want[1])
		}
	}
	assertCatalogued(t, ds)
}

// Adapter outputs are read-only to the owner: the adapter writes every
// property it owns on each delivery and replay, so an edit to its output is
// undone the next time the mirror row is delivered. A correction is the
// owner's own record from the owner's own source, ranked above the provider
// in the source priority policy: a `priority` read answers the owner's value,
// a `separate` read shows both, and a replay of the adapter leaves the
// correction and the ranking in place.
func TestHealthAppleOwnerCorrectionOutranksTheAdapter(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_3_body_measurements.json"), "hook-body", "2026-10-10T05:05:20Z")
	ctx := context.Background()
	const at = "2026-09-11T04:02:41Z"
	bodyMass := healthMetrics + "body_mass"
	adapted := "applehealth/iphone/garmin-connect/body_mass/" + at
	if v := obj(t, mustGet(t, ds, obsObservation, adapted).Properties["value"], "value"); v != "72.26" {
		t.Fatalf("the adapter wrote %v, want the app's 72.26", v)
	}

	// An edit to the adapter's output lasts until the row is delivered again.
	mustPatch := func(id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Patch(ctx, substrate.ActorAPI, obsObservation, id, substrate.PatchInput{Properties: props}); err != nil {
			t.Fatalf("patch %s: %v", id, err)
		}
	}
	mustPatch(adapted, map[string]any{"value": map[string]any{"value": "71.8", "unit": "kg"}})
	replayAdapter(t, ds, "healthapple-on-mirror")
	if v := obj(t, mustGet(t, ds, obsObservation, adapted).Properties["value"], "value"); v != "72.26" {
		t.Fatalf("an owner's edit to the adapter's output survived a replay: %v", v)
	}

	// The correction: the owner's scale, ranked first for body mass, and its
	// reading at the same instant.
	mustPut(t, ds, substrate.PutInput{Kind: obsSource, ID: "owner/scale", Properties: map[string]any{
		"name": "My scale", "class": "scale", "priorities": map[string]any{bodyMass: 1},
	}})
	ownerSource := obsSource + "/owner/scale"
	correction := mustPut(t, ds, substrate.PutInput{Kind: obsObservation, ID: "owner/body_mass/" + at, Properties: map[string]any{
		"metric": map[string]any{"ref": bodyMass}, "source": map[string]any{"ref": ownerSource},
		"form": "gauge", "hold": "PT0S", "value": map[string]any{"value": "71.8", "unit": "kg"},
		"at": at, "utcOffset": 180,
	}})
	read := func(policy string) *substrate.AggregateResult {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{
			"metric":   map[string]any{"ref": bodyMass},
			"range":    map[string]any{"gte": "2026-09-11T00:00:00Z", "lt": "2026-09-12T00:00:00Z"},
			"reducers": []any{"max"}, "sources": map[string]any{"policy": policy},
		})
		var a substrate.Aggregate
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		res, err := ds.Aggregate(ctx, substrate.AggregateQuery{Filter: substrate.Filter{Kinds: []string{obsObservation}}, Aggregate: &a})
		if err != nil {
			t.Fatalf("aggregate with sources.policy %s: %v", policy, err)
		}
		return res
	}
	values := func(res *substrate.AggregateResult) []string {
		var out []string
		for _, row := range res.Rows {
			for _, v := range row.Values {
				if v.Value != nil {
					out = append(out, *v.Value)
				}
			}
		}
		sort.Strings(out)
		return out
	}
	check := func(when string) {
		t.Helper()
		if got := values(read("priority")); len(got) != 1 || got[0] != "71.8" {
			t.Fatalf("%s: a priority read answered %v, want the owner's 71.8 alone", when, got)
		}
		if got := values(read("separate")); len(got) != 2 || got[0] != "71.8" || got[1] != "72.26" {
			t.Fatalf("%s: a separate read answered %v, want the owner's 71.8 and the app's 72.26", when, got)
		}
	}
	check("after the correction")

	replayAdapter(t, ds, "healthapple-on-mirror")
	check("after a replay of the adapter")
	if now := mustGet(t, ds, obsObservation, correction.ID); now.Version != correction.Version {
		t.Fatalf("the replay rewrote the owner's correction: version %d, was %d", now.Version, correction.Version)
	}
	if v := obj(t, mustGet(t, ds, obsObservation, adapted).Properties["value"], "value"); v != "72.26" {
		t.Fatalf("the adapter's output reads %v after the replay, want the app's 72.26", v)
	}
}

// Every output names the adapter version that wrote it, and an upgrade of the
// adapter alone, replayed over mirror rows the app did not resend, rewrites
// each output at the revision it holds (decision record 0162).
func TestHealthAppleAdapterUpgradeRewritesUnchangedMirrors(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-minutes", "2026-10-08T05:31:02Z")
	deliverAt(t, ds, ahRequest(t, "delivery_2_sleep_and_workouts.json"), "hook-night", "2026-10-08T05:20:13Z")
	before := adapterOutputs(t, ds)
	assertAdapterStamp(t, before, healthAppleAdapter)
	next := upgradeAdapter(t, ds, healthAppleAdapter)
	replayAdapter(t, ds, "healthapple-on-mirror")
	assertAdapterRewrite(t, ds, next, before)
}

// The app's day buckets are its own aggregates: summaries, never raw points,
// so a raw read beside the minutes never counts them twice. Heart rate gives
// avg, min and max; steps a sum; resting heart rate Apple's own daily value;
// an aggregated night a sleep record with its stage totals.
func TestHealthAppleDailySummaries(t *testing.T) {
	ds := newHealthApple(t)
	deliverAt(t, ds, ahRequest(t, "delivery_0_daily_30d.json"), "hook-daily", "2026-10-10T05:02:11Z")

	if n := countLive(t, ds, obsTrace); n != 0 {
		t.Fatalf("a daily delivery wrote %d traces", n)
	}
	steps := mustGet(t, ds, obsObservation, "applehealth/iphone/health/steps/day/2026-09-10T21:00:00Z/sum").Properties
	if steps["form"] != "summary" || steps["statistic"] != "sum" || steps["period"] != "day" ||
		steps["periodId"] != "2026-09-11" || obj(t, steps["value"], "value") != "11187" ||
		steps["endsAt"] != "2026-09-11T21:00:00Z" {
		t.Fatalf("daily steps = %v", steps)
	}
	for _, stat := range []string{"avg", "min", "max"} {
		mustGet(t, ds, obsObservation, "applehealth/iphone/apple-watch/heart_rate/day/2026-09-10T21:00:00Z/"+stat)
	}
	rest := mustGet(t, ds, obsObservation, "applehealth/iphone/health/resting_heart_rate/day/2026-09-10T21:00:00Z/vendor").Properties
	if rest["statistic"] != "vendor" || obj(t, rest["value"], "value") != "49" || obj(t, rest["value"], "unit") != "/min" {
		t.Fatalf("resting heart rate = %v", rest)
	}
	// An aggregated night spans the time in bed (22:47:41 to 06:50:03), and
	// its inBed total is timeInBed, never an in_bed stage beside the four.
	night := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-09-10T20:04:41Z").Properties
	if night["light"] == nil || night["deep"] == nil || night["rem"] == nil || night["timeAsleep"] == nil ||
		night["at"] != "2026-09-10T19:47:41Z" || night["endsAt"] != "2026-09-11T03:50:03Z" ||
		night["latency"] != "PT17M" || night["isNap"] != false {
		t.Fatalf("an aggregated night = %v", night)
	}
	// totalSleep 7.6477... h over inBed 8.0327... h.
	if obj(t, night["efficiency"], "value") != "95.21" {
		t.Errorf("the aggregated night's efficiency = %v, want 95.21 %%", night["efficiency"])
	}
	deep := mustGet(t, ds, obsObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-09-10T20:04:41Z/deep").Properties
	if deep["code"] != "deep" || obj(t, deep["value"], "value") != "91" {
		t.Fatalf("the night's deep total = %v", deep)
	}
	for _, row := range listLive(t, ds, obsObservation) {
		if row.Properties["code"] == "in_bed" {
			t.Fatalf("%s is an in_bed amount", row.ID)
		}
	}
	out := adapt(t, ds, ahBatch, "applehealth/iphone/walking_asymmetry_percentage/days/2026-09")
	if !strings.Contains(fmt.Sprint(out["skipped"]), "walking_asymmetry_percentage: no health metric maps it") {
		t.Fatalf("walking asymmetry output = %v", out)
	}
	assertCatalogued(t, ds)
}

// haDelivery is a fixture's request carrying the given metrics at one
// grouping, as the app's automation sends them.
func haDelivery(t *testing.T, grouping string, metrics ...map[string]any) map[string]any {
	t.Helper()
	req := ahRequest(t, "delivery_1_heart_rate_steps_minutes.json")
	body, err := json.Marshal(map[string]any{"data": map[string]any{"metrics": metrics}})
	if err != nil {
		t.Fatal(err)
	}
	req["body"] = map[string]any{"text": string(body)}
	req["headers"].(map[string]any)["automation-aggregation"] = grouping
	return req
}

// TestHealthAppleMapsUnstagedSleepAndNaps: sleep a device did not stage
// (Asleep, Unspecified) is the `asleep` code, counted in the night's time
// asleep; a short daytime sleep is a nap and a long one a night; and a dense
// gauge from the minutes grouping carries its metric's hold.
func TestHealthAppleMapsUnstagedSleepAndNaps(t *testing.T) {
	ds := newHealthApple(t)
	segment := func(from, to, value string) map[string]any {
		return map[string]any{
			"startDate": from, "endDate": to, "value": value, "source": adaWatch,
			"sleepStart": "2026-10-07 14:10:00 +0300", "sleepEnd": "2026-10-07 14:50:00 +0300",
			"inBedStart": "2026-10-07 14:05:00 +0300", "inBedEnd": "2026-10-07 14:55:00 +0300", "inBed": 0.833333333,
		}
	}
	deliverAt(t, ds, haDelivery(t, "seconds", map[string]any{"name": "sleep_analysis", "units": "hr", "data": []any{
		segment("2026-10-07 14:10:00 +0300", "2026-10-07 14:30:00 +0300", "Asleep"),
		segment("2026-10-07 14:30:00 +0300", "2026-10-07 14:50:00 +0300", "Unspecified"),
	}}), "hook-nap", "2026-10-07T12:00:00Z")
	deliverAt(t, ds, haDelivery(t, "minutes",
		map[string]any{"name": "respiratory_rate", "units": "count/min", "data": []any{
			map[string]any{"qty": 14.2, "date": "2026-10-07 03:00:00 +0300", "source": adaWatch},
			map[string]any{"qty": 14.6, "date": "2026-10-07 03:01:00 +0300", "source": adaWatch},
		}},
		map[string]any{"name": "blood_oxygen_saturation", "units": "%", "data": []any{
			map[string]any{"qty": 96, "date": "2026-10-07 03:00:00 +0300", "source": adaWatch},
		}},
	), "hook-minutes", "2026-10-07T12:00:01Z")

	stages := map[string]string{}
	for _, row := range listLive(t, ds, obsObservation) {
		if storedRefPath(row.Properties["metric"]) == healthMetrics+"sleep_stage" {
			stages[fmt.Sprint(row.Properties["code"])] = fmt.Sprint(obj(t, row.Properties["value"], "value"))
		}
	}
	if want := map[string]string{"asleep": "20"}; !maps.Equal(stages, want) {
		t.Errorf("stage amounts %v, want the two unstaged segments as asleep", stages)
	}
	n := 0
	for _, row := range listLive(t, ds, obsObservation) {
		if row.Properties["code"] == "asleep" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d asleep amounts, want one per unstaged segment", n)
	}
	nap := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-10-07T11:10:00Z").Properties
	if nap["isNap"] != true || nap["latency"] != "PT5M" || nap["timeInBed"] != "PT50M" || nap["name"] != "Nap, Wed 7 Oct 14:05" {
		t.Errorf("the afternoon sleep = %v, want a nap of 50 minutes in bed, 5 to fall asleep", nap)
	}
	assertCatalogued(t, ds)
}

// TestHealthAppleKeepsIdsApart: an id composed from a long device name is
// hashed under the record id limit rather than refused, and an hours and a
// days automation that both send a heart rate bucket starting at local
// midnight write two summaries, one per period.
func TestHealthAppleKeepsIdsApart(t *testing.T) {
	ds := newHealthApple(t)
	long := "Ada’s Withings Body Plus Bluetooth Wireless Blood Pressure Cuff Model X Pro"
	deliverAt(t, ds, haDelivery(t, "seconds", map[string]any{"name": "blood_pressure", "units": "mmHg", "data": []any{
		map[string]any{"date": "2026-10-07 08:05:37 +0300", "systolic": 121, "diastolic": 79, "source": long},
	}}), "hook-cuff", "2026-10-07T06:00:00Z")
	var pressures []*substrate.Record
	for _, row := range listLive(t, ds, obsObservation) {
		if strings.HasSuffix(storedRefPath(row.Properties["metric"]), "_blood_pressure") {
			pressures = append(pressures, row)
		}
	}
	if len(pressures) != 2 {
		t.Fatalf("%d blood pressure observations, want systolic and diastolic", len(pressures))
	}
	for _, row := range pressures {
		if len(row.ID) > 128 || strings.HasPrefix(row.ID, "applehealth/") {
			t.Errorf("%s is %d bytes, want the hashed id", row.ID, len(row.ID))
		}
	}
	row := func(grouping string) map[string]any {
		return map[string]any{"name": "heart_rate", "units": "count/min", "data": []any{
			map[string]any{"date": "2026-10-07 00:00:00 +0300", "Min": 50, "Avg": 60, "Max": 70, "source": adaWatch},
		}}
	}
	deliverAt(t, ds, haDelivery(t, "hours", row("hours")), "hook-hours", "2026-10-07T06:00:01Z")
	deliverAt(t, ds, haDelivery(t, "days", row("days")), "hook-days", "2026-10-07T06:00:02Z")
	for _, period := range []string{"hour", "day"} {
		p := mustGet(t, ds, obsObservation, "applehealth/iphone/apple-watch/heart_rate/"+period+"/2026-10-06T21:00:00Z/avg").Properties
		if p["period"] != period {
			t.Errorf("the %s summary covers period %v", period, p["period"])
		}
	}
}

// haRead is one aggregate read over the adapter's observations.
func haRead(t *testing.T, ds substrate.Dataset, read map[string]any) *substrate.AggregateResult {
	t.Helper()
	raw, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	var a substrate.Aggregate
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	res, err := ds.Aggregate(context.Background(), substrate.AggregateQuery{
		Filter: substrate.Filter{Kinds: []string{obsObservation}}, Aggregate: &a,
	})
	if err != nil {
		t.Fatalf("aggregate %s: %v", raw, err)
	}
	return res
}

// Two phones whose account ids differ only where a composed part escapes a
// character (`phone/a`, `phone-a`) send the same hour of heart rate: the
// adapter writes a trace and a source per phone, each from its own batch,
// and deleting one phone's batch leaves the other's trace.
func TestHealthAppleAccountIDsStayApart(t *testing.T) {
	ds := newHealthApple(t)
	for _, id := range []string{"phone/a", "phone-a"} {
		mustPut(t, ds, substrate.PutInput{Kind: ahAccount, ID: id, Properties: map[string]any{"displayName": id}})
	}
	deliverAt(t, ds, forAccount(ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-10-01 10:05:00 +0000", "2026-10-01 10:15:00 +0000")), "phone/a"), "hook-slash", "2026-10-01T11:00:00Z")
	deliverAt(t, ds, forAccount(ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-10-01 10:25:00 +0000")), "phone-a"), "hook-dash", "2026-10-01T11:00:01Z")
	traces := map[string]string{
		"applehealth/phone~2Fa/apple-watch/heart_rate/2026-10-01T10": "applehealth/phone~2Fa/heart_rate/minutes/2026-10-01T10",
		"applehealth/phone-a/apple-watch/heart_rate/2026-10-01T10":   "applehealth/phone-a/heart_rate/minutes/2026-10-01T10",
	}
	for id, origin := range traces {
		trace := mustGet(t, ds, obsTrace, id)
		if got := refIDs(trace, "origins"); len(got) != 1 || got[0] != origin {
			t.Fatalf("%s comes from %v, want %s alone", id, got, origin)
		}
	}
	for _, src := range []string{"applehealth/phone~2Fa/apple-watch", "applehealth/phone-a/apple-watch"} {
		mustGet(t, ds, obsSource, src)
	}
	if _, err := ds.Delete(context.Background(), substrate.ActorAPI, ahBatch, "applehealth/phone-a/heart_rate/minutes/2026-10-01T10", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete phone-a's batch: %v", err)
	}
	drainTriggers(t, ds)
	if !live(t, ds, obsTrace, "applehealth/phone~2Fa/apple-watch/heart_rate/2026-10-01T10") {
		t.Fatal("deleting phone-a's batch deleted phone/a's trace")
	}
}

// After the owner switches distance from kilometers to miles mid-hour, the
// batch's earlier rows keep their own unit, and the hour's trace converts
// each row from the unit it was sent in: 0.1 km is 100 m, 0.2 mi 321.8688 m.
func TestHealthAppleConvertsEachRowFromItsOwnUnit(t *testing.T) {
	ds := newHealthApple(t)
	rows := func(qty float64, dates ...string) []map[string]any {
		out := []map[string]any{}
		for _, d := range dates {
			out = append(out, map[string]any{"qty": qty, "date": d, "source": adaWatch})
		}
		return out
	}
	deliverAt(t, ds, ahMetricRequest(t, "minutes", "walking_running_distance", "km", rows(0.1,
		"2026-10-01 10:05:00 +0000", "2026-10-01 10:10:00 +0000")), "hook-km", "2026-10-01T10:15:00Z")
	deliverAt(t, ds, ahMetricRequest(t, "minutes", "walking_running_distance", "mi", rows(0.2,
		"2026-10-01 10:20:00 +0000", "2026-10-01 10:25:00 +0000")), "hook-mi", "2026-10-01T10:30:00Z")
	trace := mustGet(t, ds, obsTrace, "applehealth/iphone/apple-watch/distance/2026-10-01T10").Properties
	var meters []float64
	for _, v := range strs(trace["values"]) {
		if v == "null" {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("trace value %q: %v", v, err)
		}
		meters = append(meters, f)
	}
	want := []float64{100, 100, 321.8688, 321.8688}
	if len(meters) != len(want) {
		t.Fatalf("the distance trace holds %v m, want %v", meters, want)
	}
	for i := range want {
		if diff := meters[i] - want[i]; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("the distance trace holds %v m, want %v", meters, want)
		}
	}
}

// One watch sends a night twice: as the app's aggregated night (Summarize
// Data on) and as its stage segments (Summarize Data off). The segments are
// the night's stage amounts; the totals are left out, whichever delivery
// comes first, so a sum by code counts each minute of the night once.
func TestHealthAppleSleepTotalsYieldToSegments(t *testing.T) {
	ds := newHealthApple(t)
	night := map[string]any{
		"sleepStart": "2026-10-06 23:00:00 +0300", "sleepEnd": "2026-10-07 06:00:00 +0300",
		"inBedStart": "2026-10-06 22:50:00 +0300", "inBedEnd": "2026-10-07 06:05:00 +0300",
		"source": adaWatch,
	}
	aggregated := maps.Clone(night)
	maps.Copy(aggregated, map[string]any{
		"date": "2026-10-07 00:00:00 +0300", "totalSleep": 6.5, "core": 4.0, "deep": 1.0, "rem": 1.5,
		"awake": 0.5, "asleep": 0.0, "inBed": 7.25,
	})
	segment := func(from, to, value string) map[string]any {
		row := maps.Clone(night)
		maps.Copy(row, map[string]any{"startDate": from, "endDate": to, "value": value})
		return row
	}
	segments := []any{
		segment("2026-10-06 23:00:00 +0300", "2026-10-07 03:00:00 +0300", "Core"),
		segment("2026-10-07 03:00:00 +0300", "2026-10-07 04:00:00 +0300", "Deep"),
		segment("2026-10-07 04:00:00 +0300", "2026-10-07 05:30:00 +0300", "REM"),
		segment("2026-10-07 05:30:00 +0300", "2026-10-07 06:00:00 +0300", "Awake"),
	}
	sums := func(when string) {
		t.Helper()
		res := haRead(t, ds, map[string]any{
			"metric":   map[string]any{"ref": healthMetrics + "sleep_stage"},
			"range":    map[string]any{"gte": "2026-10-06T19:00:00Z", "lt": "2026-10-07T04:00:00Z"},
			"groupBy":  []any{"code"},
			"reducers": []any{"sum"}, "unit": "min",
		})
		got := map[string]float64{}
		for _, row := range res.Rows {
			for _, v := range row.Values {
				if v.Value == nil || row.Group == nil {
					continue
				}
				f, err := strconv.ParseFloat(*v.Value, 64)
				if err != nil {
					t.Fatal(err)
				}
				got[row.Group.Code] += f
			}
		}
		want := map[string]float64{"light": 240, "deep": 60, "rem": 90, "awake": 30}
		if !maps.Equal(got, want) {
			t.Fatalf("%s: minutes by stage %v, want the segments' %v counted once", when, got, want)
		}
	}

	deliverAt(t, ds, haDelivery(t, "days", map[string]any{"name": "sleep_analysis", "units": "hr", "data": []any{aggregated}}),
		"hook-night", "2026-10-07T04:00:00Z")
	deliverAt(t, ds, haDelivery(t, "seconds", map[string]any{"name": "sleep_analysis", "units": "hr", "data": segments}),
		"hook-segments", "2026-10-07T04:00:01Z")
	sums("the segments after the night")
	// The night's row adapted again, after its segments are stored.
	out := adapt(t, ds, ahBatch, "applehealth/iphone/sleep_analysis/days/2026-10")
	if !strings.Contains(fmt.Sprint(out["skipped"]), "stage totals left out") {
		t.Fatalf("the night adapted after its segments = %v, want its totals left out", out)
	}
	sums("the night adapted again after the segments")
	sleep := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-10-06T20:00:00Z").Properties
	if sleep["deep"] != "PT1H" || sleep["timeAsleep"] != "PT6H30M" {
		t.Fatalf("the night = %v, want the aggregated row's stage durations", sleep)
	}
}

// The provider keeps an epoch date as its decimal text and reads a date
// without an offset on the account's clock; the adapter reads both the same
// way. Steps at epoch seconds become a trace, and heart rate at 08:00 with
// no offset on an Europe/Athens account is 05:00 UTC, written at +03:00.
func TestHealthAppleReadsEpochAndZonelessDates(t *testing.T) {
	ds := newHealthApple(t)
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, ahAccount, "iphone", substrate.PatchInput{
		Properties: map[string]any{"timezone": "Europe/Athens"},
	}); err != nil {
		t.Fatalf("set the account's zone: %v", err)
	}
	deliverAt(t, ds, ahMetricRequest(t, "seconds", "step_count", "count", []map[string]any{
		{"qty": 1.5, "date": 1790157600, "source": adaWatch},
		{"qty": 2.5, "date": 1790157660, "source": adaWatch},
		{"qty": 3.5, "date": 1790157720.5, "source": adaWatch},
	}), "hook-epoch", "2026-09-23T11:00:00Z")
	steps := mustGet(t, ds, obsTrace, "applehealth/iphone/apple-watch/steps/2026-09-23T10").Properties
	var counted []string
	for _, v := range strs(steps["values"]) {
		if v != "null" {
			counted = append(counted, v)
		}
	}
	if strings.Join(counted, ",") != "1.5,2.5,3.5" || asFloat(t, steps["utcOffset"]) != 180 {
		t.Fatalf("the epoch steps trace holds %v at offset %v, want 1.5, 2.5 and 3.5 at +180", counted, steps["utcOffset"])
	}

	deliverAt(t, ds, ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-07-01 08:00:00", "2026-07-01 08:10:00")), "hook-local", "2026-07-01T06:00:00Z")
	beat := mustGet(t, ds, obsTrace, "applehealth/iphone/apple-watch/heart_rate/2026-07-01T05").Properties
	if beat["at"] != "2026-07-01T05:00:00Z" || asFloat(t, beat["utcOffset"]) != 180 {
		t.Fatalf("the zoneless heart rate trace starts %v at offset %v, want 05:00 UTC at +180", beat["at"], beat["utcOffset"])
	}
	if n := countLive(t, ds, obsTrace); n != 2 {
		t.Fatalf("%d traces, want the steps hour and the heart rate hour alone", n)
	}
}

// watchNight is the aggregated row (Summarize Data on) of one night of Ada's
// watch, asleep from sleepStart to 06:00 on 7 Oct at +03:00: 4 hours of
// core, one deep, an hour and a half of REM and half an hour awake.
func watchNight(sleepStart string) map[string]any {
	return map[string]any{
		"date": "2026-10-07 00:00:00 +0300", "source": adaWatch,
		"sleepStart": sleepStart, "sleepEnd": "2026-10-07 06:00:00 +0300",
		"inBedStart": "2026-10-06 22:40:00 +0300", "inBedEnd": "2026-10-07 06:05:00 +0300",
		"totalSleep": 6.5, "core": 4.0, "deep": 1.0, "rem": 1.5, "awake": 0.5, "asleep": 0.0, "inBed": 7.4166666667,
	}
}

// watchSegments is the same night as stage segments (Summarize Data off),
// each naming the night it belongs to where named is set.
func watchSegments(named bool) []any {
	segment := func(from, to, value string) map[string]any {
		row := map[string]any{"startDate": from, "endDate": to, "value": value, "source": adaWatch}
		if named {
			maps.Copy(row, map[string]any{
				"sleepStart": "2026-10-06 23:00:00 +0300", "sleepEnd": "2026-10-07 06:00:00 +0300",
				"inBedStart": "2026-10-06 22:40:00 +0300", "inBedEnd": "2026-10-07 06:05:00 +0300",
			})
		}
		return row
	}
	return []any{
		segment("2026-10-06 23:00:00 +0300", "2026-10-07 03:00:00 +0300", "Core"),
		segment("2026-10-07 03:00:00 +0300", "2026-10-07 04:00:00 +0300", "Deep"),
		segment("2026-10-07 04:00:00 +0300", "2026-10-07 05:30:00 +0300", "REM"),
		segment("2026-10-07 05:30:00 +0300", "2026-10-07 06:00:00 +0300", "Awake"),
	}
}

// deliverSleep sends sleep_analysis rows at one time grouping.
func deliverSleep(t *testing.T, ds substrate.Dataset, grouping, fire, at string, rows ...any) {
	t.Helper()
	deliverAt(t, ds, haDelivery(t, grouping, map[string]any{"name": "sleep_analysis", "units": "hr", "data": rows}), fire, at)
}

// wantStageMinutes asserts the minutes a raw sum by code reads for the
// night of 6 to 7 October.
func wantStageMinutes(t *testing.T, ds substrate.Dataset, when string, want map[string]float64) {
	t.Helper()
	n := 0
	for _, row := range listLive(t, ds, obsObservation) {
		if storedRefPath(row.Properties["metric"]) == healthMetrics+"sleep_stage" {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("%s: no stage amounts, want %v", when, want)
	}
	res := haRead(t, ds, map[string]any{
		"metric":   map[string]any{"ref": healthMetrics + "sleep_stage"},
		"range":    map[string]any{"gte": "2026-10-06T18:00:00Z", "lt": "2026-10-07T04:00:00Z"},
		"groupBy":  []any{"code"},
		"reducers": []any{"sum"}, "unit": "min",
	})
	got := map[string]float64{}
	for _, row := range res.Rows {
		for _, v := range row.Values {
			if v.Value == nil || row.Group == nil {
				continue
			}
			f, err := strconv.ParseFloat(*v.Value, 64)
			if err != nil {
				t.Fatal(err)
			}
			got[row.Group.Code] += f
		}
	}
	if !maps.Equal(got, want) {
		t.Fatalf("%s: minutes by stage %v, want %v", when, got, want)
	}
}

// theNight is the stage minutes of watchNight and of watchSegments alike.
var theNight = map[string]float64{"light": 240, "deep": 60, "rem": 90, "awake": 30}

// An aggregated night whose start the app corrects, from 23:00 to 22:50,
// keeps its stage totals: they move to the corrected start, and the totals
// at the old start are not taken for segments that tell the night.
func TestHealthAppleCorrectedNightStartKeepsItsTotals(t *testing.T) {
	ds := newHealthApple(t)
	deliverSleep(t, ds, "days", "hook-night", "2026-10-07T04:00:00Z", watchNight("2026-10-06 23:00:00 +0300"))
	wantStageMinutes(t, ds, "the night", theNight)
	deliverSleep(t, ds, "days", "hook-corrected", "2026-10-07T05:00:00Z", watchNight("2026-10-06 22:50:00 +0300"))
	wantStageMinutes(t, ds, "the corrected night", theNight)
	if !live(t, ds, obsObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-10-06T19:50:00Z/deep") {
		t.Fatal("the deep total is not at the corrected start")
	}
	if n := countLive(t, ds, healthSleep); n != 1 {
		t.Fatalf("%d sleep records after the correction, want 1", n)
	}
}

// Segments that name no night (no sleepStart or sleepEnd) still tell the
// night their stretch overlaps: the totals of the source's aggregated night
// over them are left out, so a sum by code counts each minute once.
func TestHealthAppleSegmentsWithoutNightFieldsReplaceTotals(t *testing.T) {
	ds := newHealthApple(t)
	deliverSleep(t, ds, "days", "hook-night", "2026-10-07T04:00:00Z", watchNight("2026-10-06 23:00:00 +0300"))
	deliverSleep(t, ds, "seconds", "hook-segments", "2026-10-07T04:00:01Z", watchSegments(false)...)
	wantStageMinutes(t, ds, "unnamed segments after the night", theNight)
	if live(t, ds, obsObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-10-06T20:00:00Z/deep") {
		t.Fatal("the night's deep total survives beside the segments")
	}
}

// Totals, then the night's segments, then the segment batches deleted: the
// night's totals come back, because the night is recomputed from the rows
// that survive, and the sleep record keeps the aggregated row's durations
// throughout.
func TestHealthAppleRemovedSegmentsRestoreTotals(t *testing.T) {
	ds := newHealthApple(t)
	deliverSleep(t, ds, "days", "hook-night", "2026-10-07T04:00:00Z", watchNight("2026-10-06 23:00:00 +0300"))
	deliverSleep(t, ds, "seconds", "hook-segments", "2026-10-07T04:00:01Z", watchSegments(true)...)
	wantStageMinutes(t, ds, "the segments after the night", theNight)
	if live(t, ds, obsObservation, "applehealth/iphone/apple-watch/sleep_stage/2026-10-06T20:00:00Z/deep") {
		t.Fatal("the night's deep total survives beside the segments")
	}
	n := 0
	for _, row := range listLive(t, ds, ahBatch) {
		if row.Properties["grouping"] != "seconds" {
			continue
		}
		if _, err := ds.Delete(context.Background(), substrate.ActorAPI, ahBatch, row.ID, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete %s: %v", row.ID, err)
		}
		n++
	}
	if n != 4 {
		t.Fatalf("deleted %d segment batches, want the night's 4 hours", n)
	}
	drainTriggers(t, ds)
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}
	wantStageMinutes(t, ds, "the night after its segments went", theNight)
	sleep := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-10-06T20:00:00Z").Properties
	if sleep["deep"] != "PT1H" || sleep["timeAsleep"] != "PT6H30M" {
		t.Fatalf("the night = %v, want the aggregated row's durations", sleep)
	}
}

// One night as an aggregated row and as segments that name it, delivered
// in either order: the night is one sleep record, owned by the aggregated
// row's batch, with its durations right after each delivery.
func TestHealthAppleSleepKeepsItsDurationsInEitherOrder(t *testing.T) {
	const days = ahBatch + "/applehealth/iphone/sleep_analysis/days/2026-10"
	check := func(t *testing.T, ds substrate.Dataset, when string) {
		t.Helper()
		if n := countLive(t, ds, healthSleep); n != 1 {
			t.Fatalf("%s: %d sleep records, want 1", when, n)
		}
		sleep := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-10-06T20:00:00Z").Properties
		if sleep["deep"] != "PT1H" || sleep["rem"] != "PT1H30M" || sleep["timeAsleep"] != "PT6H30M" ||
			sleep["efficiency"] == nil || storedRefPath(sleep["origin"]) != days {
			t.Fatalf("%s: the night = %v, want the aggregated row's durations from %s", when, sleep, days)
		}
		wantStageMinutes(t, ds, when, theNight)
	}
	t.Run("night first", func(t *testing.T) {
		ds := newHealthApple(t)
		deliverSleep(t, ds, "days", "hook-night", "2026-10-07T04:00:00Z", watchNight("2026-10-06 23:00:00 +0300"))
		check(t, ds, "the night alone")
		deliverSleep(t, ds, "seconds", "hook-segments", "2026-10-07T04:00:01Z", watchSegments(true)...)
		check(t, ds, "the segments after the night")
	})
	t.Run("segments first", func(t *testing.T) {
		ds := newHealthApple(t)
		deliverSleep(t, ds, "seconds", "hook-segments", "2026-10-07T04:00:00Z", watchSegments(true)...)
		if sleep := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-10-06T20:00:00Z").Properties; sleep["deep"] != nil {
			t.Fatalf("the segments alone wrote durations %v", sleep)
		}
		deliverSleep(t, ds, "days", "hook-night", "2026-10-07T04:00:01Z", watchNight("2026-10-06 23:00:00 +0300"))
		check(t, ds, "the night after the segments")
	})
}

// Sleep sent without offsets on an Europe/Athens account, over both changes
// of offset: a segment's minutes and a night's latency are the time between
// the two instants, never the difference of the wall clocks.
func TestHealthAppleMeasuresSleepAcrossOffsetChanges(t *testing.T) {
	ds := newHealthApple(t)
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, ahAccount, "iphone", substrate.PatchInput{
		Properties: map[string]any{"timezone": "Europe/Athens"},
	}); err != nil {
		t.Fatalf("set the account's zone: %v", err)
	}
	segment := func(from, to, value, inBed, start, end string) map[string]any {
		return map[string]any{
			"startDate": from, "endDate": to, "value": value, "source": adaWatch,
			"sleepStart": start, "sleepEnd": end, "inBedStart": inBed, "inBedEnd": end,
		}
	}
	// 25 October: 04:00 at +03:00 is 03:00 at +02:00. 01:00 to 05:00 on the
	// wall is five hours.
	deliverSleep(t, ds, "seconds", "hook-autumn", "2026-10-25T06:00:00Z",
		segment("2026-10-25 01:00:00", "2026-10-25 05:00:00", "Awake",
			"2026-10-25 00:30:00", "2026-10-25 01:00:00", "2026-10-25 05:00:00"))
	// 29 March: 03:00 at +02:00 is 04:00 at +03:00. In bed at 02:50, asleep
	// at 04:10 on the wall is twenty minutes, and so is the awake stretch
	// between them.
	deliverSleep(t, ds, "seconds", "hook-spring", "2026-03-29T06:00:00Z",
		segment("2026-03-29 02:50:00", "2026-03-29 04:10:00", "Awake",
			"2026-03-29 02:50:00", "2026-03-29 04:10:00", "2026-03-29 07:10:00"),
		segment("2026-03-29 04:10:00", "2026-03-29 07:10:00", "Core",
			"2026-03-29 02:50:00", "2026-03-29 04:10:00", "2026-03-29 07:10:00"))
	for id, want := range map[string]string{
		"applehealth/iphone/apple-watch/sleep_stage/2026-10-24T22:00:00Z": "300",
		"applehealth/iphone/apple-watch/sleep_stage/2026-03-29T00:50:00Z": "20",
		"applehealth/iphone/apple-watch/sleep_stage/2026-03-29T01:10:00Z": "180",
	} {
		if got := obj(t, mustGet(t, ds, obsObservation, id).Properties["value"], "value"); got != want {
			t.Errorf("%s holds %v minutes, want %s", id, got, want)
		}
	}
	spring := mustGet(t, ds, healthSleep, "applehealth/iphone/apple-watch/sleep/2026-03-29T01:10:00Z").Properties
	if spring["latency"] != "PT20M" || asFloat(t, spring["utcOffsetStart"]) != 120 || asFloat(t, spring["utcOffsetEnd"]) != 180 {
		t.Errorf("the spring night = %v, want 20 minutes to fall asleep, from +120 to +180", spring)
	}
}
