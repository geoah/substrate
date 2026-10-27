package providertest

// Substrate Sync's deliveries. The iPhone and iPad app (apps/apple/SubstrateSync)
// reads HealthKit itself and POSTs Health Auto Export's JSON to the same
// webhook, as two deliveries per local day: every metric at Time Grouping
// Minutes, and the sleep segments and workouts at Seconds. app_minutes.json
// and app_seconds.json are what its encoder writes for one fixed day, and
// the app's own test holds the encoder to them byte for byte, so these cases
// are what keeps the app's format and the receiver in agreement: change one
// and the other's suite fails.

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// syncAccount is the account the app creates, `substrate-sync-` and the
// first eight characters of its install id.
const syncAccount = "substrate-sync-6f1c2e9a"

// newSubstrateSync is a repository as the app sets one up: one install of
// applehealth through the catalog, which brings the health catalogue and the
// adapter (decision record 0192), and the app's account.
func newSubstrateSync(t *testing.T) substrate.Dataset {
	t.Helper()
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	installShipped(t, ds, appleHealthPkg)
	mustPut(t, ds, substrate.PutInput{
		Kind: ahAccount, ID: syncAccount,
		Properties: map[string]any{"displayName": "Ada's iPhone", "timezone": "Europe/Athens"},
	})
	return ds
}

// syncID is an output id under the app's account.
func syncID(parts ...string) string {
	return "applehealth/" + syncAccount + "/" + strings.Join(parts, "/")
}

// The minutes delivery: a batch for every metric it sends, heart rate and
// the cumulative metrics as hourly traces in the metric's unit, a merged
// `Watch|iPhone` label as Health's own source, and every spot reading
// (body, glucose with its meal, blood pressure, SpO2 and the rest) as an
// observation.
func TestSubstrateSyncMinutesDelivery(t *testing.T) {
	ds := newSubstrateSync(t)
	req := ahRequest(t, "app_minutes.json")
	deliverAt(t, ds, req, "hook-minutes", "2026-10-07T21:00:05Z")

	sent := slices.Sorted(maps.Keys(ahMetrics(t, req)))
	got := map[string]bool{}
	for _, row := range listLive(t, ds, ahBatch) {
		if storedRefPath(row.Properties["account"]) != ahAccount+"/"+syncAccount {
			t.Fatalf("batch %s names account %v", row.ID, row.Properties["account"])
		}
		got[strings.Split(strings.TrimPrefix(row.ID, syncID()), "/")[0]] = true
	}
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, sent) {
		t.Fatalf("batches for %v, want one or more for each metric sent %v", names, sent)
	}

	hr := mustGet(t, ds, obsTrace, syncID("apple-watch", "heart_rate", "2026-10-07T04")).Properties
	if hr["form"] != "gauge" || hr["unit"] != "1/min" || strings.Join(strs(hr["values"]), ",") != "63.333333,72,74.5" ||
		strings.Join(strs(hr["offsets"]), ",") != "180000,420000,900000" {
		t.Fatalf("the 07:00 heart rate trace = values %v offsets %v", hr["values"], hr["offsets"])
	}
	steps := mustGet(t, ds, obsTrace, syncID("iphone", "steps", "2026-10-07T04")).Properties
	if steps["form"] != "amount" || steps["unit"] != "steps" || strs(steps["values"])[0] != "47" {
		t.Fatalf("the phone's steps = %v", steps)
	}
	merged := mustGet(t, ds, obsTrace, syncID("health", "steps", "2026-10-07T04")).Properties
	if strs(merged["values"])[0] != "52.5" {
		t.Fatalf("the merged steps = %v", merged["values"])
	}
	dist := mustGet(t, ds, obsTrace, syncID("iphone", "distance", "2026-10-07T04")).Properties
	if dist["unit"] != "m" || strs(dist["values"])[0] != "34" {
		t.Fatalf("distance in m = %v %v", dist["unit"], dist["values"])
	}
	for _, id := range []string{
		syncID("apple-watch", "active_energy", "2026-10-07T04"), syncID("apple-watch", "basal_energy", "2026-10-07T04"),
		syncID("iphone", "floors", "2026-10-07T04"), syncID("apple-watch", "respiratory_rate", "2026-10-07T00"),
		syncID("apple-watch", "spo2", "2026-10-07T00"),
	} {
		mustGet(t, ds, obsTrace, id)
	}
	spo2 := mustGet(t, ds, obsTrace, syncID("apple-watch", "spo2", "2026-10-07T00")).Properties
	if spo2["unit"] != "percent" || strings.Join(strs(spo2["values"]), ",") != "97,96" {
		t.Fatalf("SpO2 in percent = %v %v", spo2["unit"], spo2["values"])
	}

	for id, want := range map[string]string{
		syncID("withings", "body_mass", "2026-10-07T04:01:00Z"):               "71.4 kg",
		syncID("withings", "body_fat_fraction", "2026-10-07T04:01:00Z"):       "18.2 percent",
		syncID("withings", "bmi", "2026-10-07T04:01:00Z"):                     "22.6 kg/m2",
		syncID("iphone", "height", "2026-10-07T04:01:00Z"):                    "1.775 m",
		syncID("apple-watch", "hrv_sdnn", "2026-10-07T00:10:00Z"):             "48.2 ms",
		syncID("apple-watch", "vo2_max", "2026-10-07T15:43:00Z"):              "44.2 mL/kg/min",
		syncID("apple-watch", "skin_temperature", "2026-10-07T03:50:00Z"):     "35.62 Cel",
		syncID("contour", "blood_glucose", "2026-10-07T04:30:00Z"):            "92 mg/dL_glucose",
		syncID("contour", "blood_glucose", "2026-10-07T10:45:00Z"):            "128 mg/dL_glucose",
		syncID("withings", "systolic_blood_pressure", "2026-10-07T04:15:00Z"): "118 mmHg",
	} {
		p := mustGet(t, ds, obsObservation, id).Properties
		if v := fmt.Sprint(obj(t, p["value"], "value"), " ", obj(t, p["value"], "unit")); v != want {
			t.Errorf("%s = %s, want %s", id, v, want)
		}
	}
	resting := mustGet(t, ds, obsObservation, syncID("apple-watch", "resting_heart_rate", "day", "2026-10-06T21:00:00Z", "vendor")).Properties
	if resting["form"] != "summary" || obj(t, resting["value"], "value") != "56" {
		t.Errorf("resting heart rate = %v", resting)
	}
	watch := mustGet(t, ds, obsSource, syncID("apple-watch")).Properties
	if watch["class"] != "watch" || watch["name"] != adaWatch {
		t.Errorf("the watch's source = %v", watch)
	}

	// What a phone logs rather than measures: each an amount at its minute,
	// and a basal temperature a spot reading.
	for id, want := range map[string]string{
		syncID("apple-watch", "mindful_minutes", "2026-10-07T04"): "min 10",
		syncID("iphone", "mindful_minutes", "2026-10-07T19"):      "min 3.5",
		syncID("iphone", "water", "2026-10-07T05"):                "mL 250",
		syncID("iphone", "caffeine", "2026-10-07T05"):             "mg 95",
		syncID("myfitnesspal", "dietary_energy", "2026-10-07T05"): "kcal 420",
		syncID("myfitnesspal", "protein", "2026-10-07T05"):        "g 22.5",
		syncID("myfitnesspal", "sodium", "2026-10-07T05"):         "mg 540",
	} {
		p := mustGet(t, ds, obsTrace, id).Properties
		if got := fmt.Sprint(p["unit"], " ", strs(p["values"])[0]); p["form"] != "amount" || got != want {
			t.Errorf("%s = %s %v, want an amount of %s", id, p["form"], got, want)
		}
	}
	basal := mustGet(t, ds, obsObservation, syncID("iphone", "basal_body_temperature", "2026-10-07T03:45:00Z")).Properties
	if v := fmt.Sprint(obj(t, basal["value"], "value"), " ", obj(t, basal["value"], "unit")); v != "36.42 Cel" {
		t.Errorf("basal body temperature = %s", v)
	}
	assertCatalogued(t, ds)
}

// The seconds delivery: each segment a coded stage amount and the In Bed
// segment none, one night spanning the time in bed with the latency every
// segment's night fields give it, and each workout under the sport its
// Fitness name maps to, attributed to the device its heart rate rows name.
func TestSubstrateSyncSecondsDelivery(t *testing.T) {
	ds := newSubstrateSync(t)
	deliverAt(t, ds, ahRequest(t, "app_seconds.json"), "hook-seconds", "2026-10-07T21:00:06Z")

	stages := map[string]int{}
	for _, row := range listLive(t, ds, obsObservation) {
		if storedRefPath(row.Properties["metric"]) == healthMetrics+"sleep_stage" {
			stages[fmt.Sprint(row.Properties["code"])]++
		}
	}
	if want := map[string]int{"awake": 3, "light": 3, "deep": 2, "rem": 2, "asleep": 1}; !maps.Equal(stages, want) {
		t.Fatalf("stage amounts by code = %v, want %v", stages, want)
	}
	nights := listLive(t, ds, healthSleep)
	if len(nights) != 1 {
		t.Fatalf("%d sleep records, want one night", len(nights))
	}
	night := nights[0].Properties
	if nights[0].ID != syncID("apple-watch", "sleep", "2026-10-06T21:26:00Z") ||
		storedRefPath(night["source"]) != obsSource+"/"+syncID("apple-watch") ||
		night["at"] != "2026-10-06T21:12:00Z" || night["endsAt"] != "2026-10-07T04:02:00Z" ||
		night["timeInBed"] != "PT6H50M" || night["latency"] != "PT14M" || night["isNap"] != false {
		t.Fatalf("the night %s = %v", nights[0].ID, night)
	}

	run := mustGet(t, ds, healthWorkout, syncID("5B1F7A2C-3D4E-4F60-8A9B-0C1D2E3F4A5B")).Properties
	if run["sport"] != "running" || run["sportSource"] != "Outdoor Run" ||
		storedRefPath(run["source"]) != obsSource+"/"+syncID("apple-watch") ||
		obj(t, run["distance"], "value") != "6.1" || obj(t, run["distance"], "unit") != "km" ||
		obj(t, run["activeEnergy"], "value") != "412.4" || run["duration"] != "PT36M30S" ||
		obj(t, run["averageHeartRate"], "unit") != "1/min" || run["elevationGain"] == nil {
		t.Fatalf("the run = %v", run)
	}
	lift := mustGet(t, ds, healthWorkout, syncID("9E8D7C6B-5A49-4382-9170-6F5E4D3C2B1A")).Properties
	if lift["sport"] != "strength" || lift["distance"] != nil || storedRefPath(lift["source"]) != obsSource+"/"+syncID("apple-watch") {
		t.Fatalf("the strength workout = %v", lift)
	}
	mirror := mustGet(t, ds, ahWorkout, syncID("5B1F7A2C-3D4E-4F60-8A9B-0C1D2E3F4A5B")).Properties
	if obj(t, mirror["metadata"], "HKTimeZone") != "Europe/Athens" {
		t.Errorf("the run's mirror metadata = %v", mirror["metadata"])
	}
	// The route stays on the mirror (decision record 0163: a position is not
	// a quantity), whole.
	if route, _ := mirror["route"].([]any); len(route) != 3 || obj(t, route[0], "latitude") == nil || obj(t, route[2], "timestamp") != "2026-10-07 18:05:30 +0300" {
		t.Errorf("the run's route = %v", mirror["route"])
	}
	cycle := map[string]string{}
	for _, row := range listLive(t, ds, ahCycleEvent) {
		cycle[fmt.Sprint(row.Properties["name"])] = fmt.Sprint(row.Properties["value"], " ", row.Properties["isCycleStart"])
	}
	if want := map[string]string{"Menstrual Flow": "Medium true", "Ovulation Test Result": "Negative <nil>"}; !maps.Equal(cycle, want) {
		t.Errorf("cycle tracking entries = %v, want %v", cycle, want)
	}
	assertCatalogued(t, ds)
}

// A day sent twice, as every later sync re-sends yesterday and today,
// rewrites what it wrote and adds nothing.
func TestSubstrateSyncResentDayChangesNothing(t *testing.T) {
	ds := newSubstrateSync(t)
	deliverAt(t, ds, ahRequest(t, "app_minutes.json"), "hook-1", "2026-10-07T21:00:05Z")
	deliverAt(t, ds, ahRequest(t, "app_seconds.json"), "hook-2", "2026-10-07T21:00:06Z")
	count := func() map[string]int {
		n := map[string]int{}
		for _, kind := range []string{ahBatch, ahWorkout, obsTrace, obsObservation, healthSleep, healthWorkout} {
			page, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{kind}}, First: 1000})
			if err != nil {
				t.Fatal(err)
			}
			n[kind] = len(page.Records)
		}
		return n
	}
	before := count()
	deliverAt(t, ds, ahRequest(t, "app_minutes.json"), "hook-3", "2026-10-08T06:00:05Z")
	deliverAt(t, ds, ahRequest(t, "app_seconds.json"), "hook-4", "2026-10-08T06:00:06Z")
	if after := count(); !maps.Equal(before, after) {
		t.Fatalf("a re-sent day changed the counts from %v to %v", before, after)
	}
}
