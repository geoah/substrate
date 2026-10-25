package providertest

// The contract between internal/synth's Apple Health emitter and the shipped
// applehealth closure: three generated days, each delivery read back from
// the envelope file `synth generate` writes, handed to the real
// `applehealthingest` the way the webhook door hands it, then delivered to
// the `healthapple` adapter. A change to a shape the ingest or the adapter
// reads that the emitter does not follow fails here.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/synth"
)

// synthAHDoorHeaders is what the door keeps of a request: the headers that
// describe the body, which it keeps for every trigger (engine/webhooks.go),
// and the ones the shipped applehealth-webhook trigger declares.
func synthAHDoorHeaders(t *testing.T) map[string]bool {
	t.Helper()
	kept := map[string]bool{"content-type": true, "content-length": true, "content-encoding": true, "user-agent": true, "date": true}
	for _, doc := range loadDocs(t, appleHealthDir+"/triggers.yaml") {
		if doc["metadata"].(map[string]any)["id"] != ahTrigger {
			continue
		}
		webhook := doc["data"].(map[string]any)["properties"].(map[string]any)["source"].(map[string]any)["webhook"].(map[string]any)
		for _, h := range webhook["headers"].([]any) {
			kept[strings.ToLower(h.(string))] = true
		}
		return kept
	}
	t.Fatalf("%s declares no %s trigger", appleHealthDir, ahTrigger)
	return nil
}

// synthAHDense is every app metric the adapter packs into hourly traces.
var synthAHDense = map[string]string{
	"heart_rate": "heart_rate", "respiratory_rate": "respiratory_rate", "blood_oxygen_saturation": "spo2",
	"step_count": "steps", "walking_running_distance": "distance", "flights_climbed": "floors",
	"active_energy": "active_energy", "basal_energy_burned": "basal_energy",
}

// synthAHSlug is the source the adapter names for a row's label.
func synthAHSlug(label any) string {
	s, _ := label.(string)
	switch {
	case s == "" || strings.Contains(s, "|"):
		return "health"
	case strings.HasSuffix(s, "Apple Watch"):
		return "apple-watch"
	case strings.HasSuffix(s, "iPhone"):
		return "iphone"
	}
	return strings.ToLower(s)
}

// synthAHRequest reads one delivery file back and strips what the door
// strips: the Authorization header and every header the trigger does not
// declare.
func synthAHRequest(t *testing.T, path string) (map[string]any, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Trigger string         `json:"trigger"`
		Request map[string]any `json:"request"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	headers, _ := env.Request["headers"].(map[string]any)
	if !strings.HasPrefix(fmt.Sprint(headers["authorization"]), "Bearer ") {
		t.Fatalf("%s carries no Authorization header for the loader to put the key in", path)
	}
	declared := synthAHDoorHeaders(t)
	kept := map[string]any{}
	for k, v := range headers {
		if declared[k] {
			kept[k] = v
		}
	}
	env.Request["headers"] = kept
	return env.Request, env.Trigger
}

func TestSynthAppleHealthIngestsAndAdapts(t *testing.T) {
	// The repository first: newHealthApple skips without python3 and goes
	// parallel before any data is generated.
	ds := newHealthApple(t)
	// Saturday to Monday: two weigh-ins, the Sunday blood pressure, a strength
	// session, the long run and maybe yoga.
	p, err := synth.NewPersona(synth.Options{
		Start: synth.NewDate(2026, time.September, 12), End: synth.NewDate(2026, time.September, 14),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := synth.AppleHealth{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := synth.WriteOutput(dir, synth.AppleHealth{}, out); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "applehealth", "deliveries", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	days := p.Days()
	if len(files) != len(days) {
		t.Fatalf("%d delivery files, want one per day: %v", len(files), files)
	}
	var workouts, runs, weighIns, pressures, glucose int
	for _, d := range days {
		day := p.Day(d)
		workouts += len(day.Workouts)
		for _, w := range day.Workouts {
			if w.Sport == synth.Running {
				runs++
			}
		}
		if day.WeightKg != 0 {
			weighIns++
		}
		if day.BloodPressure != nil {
			pressures++
		}
		if day.Glucose != nil {
			glucose++
		}
	}
	if workouts == 0 || weighIns == 0 || pressures == 0 || glucose == 0 {
		t.Fatalf("%d workouts, %d weigh-ins, %d blood pressures, %d glucose readings: pick days that hold each", workouts, weighIns, pressures, glucose)
	}

	// What the bodies imply, counted the way the ingest keys batches and the
	// adapter keys traces: one batch per metric and UTC hour, one trace per
	// dense metric, source and UTC hour.
	batches, traces := map[string]bool{}, map[string]int{}
	var hrv, nights, stages int
	for i, f := range files {
		req, trigger := synthAHRequest(t, f)
		if trigger != "applehealth-webhook" {
			t.Fatalf("%s goes to %q", f, trigger)
		}
		body, _ := req["body"].(map[string]any)
		var doc struct {
			Data struct {
				Metrics []ahMetric `json:"metrics"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body["text"].(string)), &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, m := range doc.Data.Metrics {
			for _, row := range m.Data {
				hour := ahInstant(t, rowDate(row)).Format("2006-01-02T15")
				batches[batchID("iphone", m.Name, "minutes", hour)] = true
				if metric, ok := synthAHDense[m.Name]; ok {
					traces[metric+" "+synthAHSlug(row["source"])+" "+hour]++
				}
			}
			switch m.Name {
			case "heart_rate_variability":
				hrv += len(m.Data)
			case "sleep_analysis":
				nights += len(m.Data)
				// One amount per staged total and per unstaged (asleep) total;
				// the in-bed total is the night's timeInBed, no stage.
				for _, row := range m.Data {
					for _, k := range []string{"core", "deep", "rem", "awake", "asleep"} {
						if v, ok := row[k].(float64); ok && v > 0 {
							stages++
						}
					}
				}
			}
		}
		at := days[i].Add(1).Midnight(p.Zone(days[i])).Add(-2 * time.Minute).UTC().Format(time.RFC3339)
		deliverAt(t, ds, req, fmt.Sprintf("hook-synth-%d", i+1), at)
	}

	// The mirror: one batch per metric and hour, every workout, a receipt per
	// delivery, nothing in the sections the emitter does not send.
	for kind, want := range map[string]int{
		ahAccount: 1, ahExport: len(days), ahBatch: len(batches), ahWorkout: workouts,
		ahStateOfMind: 0, ahMedication: 0, ahSymptom: 0, ahCycleEvent: 0, ahECG: 0, ahHRNotification: 0,
	} {
		if n := countLive(t, ds, kind); n != want {
			t.Errorf("%d rows of %s, want %d", n, kind, want)
		}
	}
	for _, row := range listLive(t, ds, ahExport) {
		if fmt.Sprint(row.Properties["droppedRows"]) != "0" || row.Properties["droppedFields"] != nil ||
			row.Properties["automationAggregation"] != "minutes" || row.Properties["sessionId"] == nil {
			t.Errorf("receipt %s = %v", row.ID, row.Properties)
		}
	}
	for _, row := range listLive(t, ds, ahBatch) {
		if row.Properties["grouping"] != "minutes" || len(rowsOf(t, row)) == 0 {
			t.Errorf("batch %s = %v", row.ID, row.Properties)
		}
	}
	if acct := mustGet(t, ds, ahAccount, "iphone").Properties; acct["syncState"] != "ok" || storedRefPath(acct["lastExport"]) != ahExport+"/hook-synth-3" {
		t.Errorf("the account is stamped %v", acct)
	}

	// The adapter: a trace per dense metric, source and hour; HRV, resting
	// heart rate, the weigh-ins and the blood pressure as observations; a
	// night and its stage totals; a workout each; four sources.
	gotTraces := map[string]int{}
	for _, row := range listLive(t, ds, obsTrace) {
		parts := strings.Split(row.ID, "/")
		if len(parts) != 5 || parts[0] != "applehealth" || parts[1] != "iphone" {
			t.Fatalf("trace id %s", row.ID)
		}
		gotTraces[parts[3]+" "+parts[2]+" "+parts[4]] = len(strs(row.Properties["values"]))
	}
	if len(gotTraces) != len(traces) {
		t.Errorf("%d traces, the bodies imply %d", len(gotTraces), len(traces))
	}
	for key, rows := range traces {
		got, ok := gotTraces[key]
		if !ok {
			t.Errorf("no trace for %s", key)
			continue
		}
		// An amount's minute ends in a null where the next minute has no row.
		if metric := strings.Fields(key)[0]; metric == "heart_rate" || metric == "respiratory_rate" || metric == "spo2" {
			if got != rows {
				t.Errorf("trace %s holds %d values of %d rows", key, got, rows)
			}
		} else if got < rows || got > 2*rows {
			t.Errorf("trace %s holds %d values for %d rows", key, got, rows)
		}
		// Basal energy accrues every minute, so an hour's trace holds sixty
		// minutes of it, not one minute holding the hour.
		if strings.HasPrefix(key, "basal_energy ") && got != 60 {
			t.Errorf("trace %s holds %d values, want one per minute of its hour", key, got)
		}
	}
	byMetric := map[string]int{}
	for _, row := range listLive(t, ds, obsObservation) {
		byMetric[metricOf(row.Properties)]++
	}
	wantMetric := map[string]int{
		"health/hrv_sdnn": hrv, "health/resting_heart_rate": len(days),
		"health/body_mass": weighIns, "health/body_fat_fraction": weighIns,
		"health/systolic_blood_pressure": pressures, "health/diastolic_blood_pressure": pressures,
		"health/sleep_stage": stages, "health/blood_glucose_mass": glucose,
		"health/vo2_max": runs, "health/skin_temperature": len(days), "health/bmi": weighIns,
		"health/height": 1,
	}
	for metric, want := range wantMetric {
		if byMetric[metric] != want {
			t.Errorf("%d observations of %s, want %d", byMetric[metric], metric, want)
		}
	}
	if len(byMetric) != len(wantMetric) {
		t.Errorf("observations of %d metrics, want %d: %v", len(byMetric), len(wantMetric), byMetric)
	}
	// The height is sent in centimeters and lands in the metric's meters.
	for _, row := range listLive(t, ds, obsObservation) {
		if metricOf(row.Properties) != "health/height" {
			continue
		}
		v, u := quantityOf(t, row.Properties, "value")
		if f, err := strconv.ParseFloat(v, 64); err != nil || f != p.Person.HeightM || u != "m" {
			t.Errorf("the height lands as %s %s, the persona is %v m", v, u, p.Person.HeightM)
		}
	}
	for kind, want := range map[string]int{healthSleep: nights, healthWorkout: workouts} {
		if n := countLive(t, ds, kind); n != want {
			t.Errorf("%d %s, want %d", n, kind, want)
		}
	}
	var sources []string
	for _, row := range listLive(t, ds, obsSource) {
		sources = append(sources, row.ID)
	}
	sort.Strings(sources)
	if want := "applehealth/iphone/apple-watch applehealth/iphone/contour applehealth/iphone/health applehealth/iphone/iphone applehealth/iphone/withings"; strings.Join(sources, " ") != want {
		t.Errorf("sources %v, want %s", sources, want)
	}
	for _, row := range listLive(t, ds, healthWorkout) {
		pr := row.Properties
		if storedRefPath(pr["source"]) != obsSource+"/applehealth/iphone/apple-watch" || pr["averageHeartRate"] == nil || pr["activeEnergy"] == nil {
			t.Errorf("workout %s = %v", row.ID, pr)
		}
		switch pr["sport"] {
		case "running", "cycling":
			if pr["distance"] == nil {
				t.Errorf("workout %s (%v) has no distance", row.ID, pr["sport"])
			}
		case "strength", "yoga":
		default:
			t.Errorf("workout %s sport %v from %v", row.ID, pr["sport"], pr["sportSource"])
		}
	}

	// One value end to end: the first weigh-in reaches body_mass in kg.
	first := p.Day(days[0])
	req, _ := synthAHRequest(t, files[0])
	var weight string
	for name, m := range ahMetrics(t, req) {
		if name == "weight_body_mass" {
			weight = fmt.Sprint(m.Data[0]["qty"])
			at := ahInstant(t, m.Data[0]["date"].(string))
			if d := at.Sub(first.WeighInAt); d < 0 || d >= time.Minute {
				t.Errorf("the weigh-in is sent at %s, the persona stepped on at %s", at, first.WeighInAt)
			}
			obs := mustGet(t, ds, obsObservation, "applehealth/iphone/withings/body_mass/"+at.Format("2006-01-02T15:04:05Z")).Properties
			if v, u := quantityOf(t, obs, "value"); v != weight || u != "kg" {
				t.Errorf("body_mass is %s %s, the delivery sent %s kg", v, u, weight)
			}
		}
	}
	if weight == "" {
		t.Fatal("the first day sends no weigh-in")
	}
	if !regexp.MustCompile(`^\d{2}\.\d{1,2}$`).MatchString(weight) {
		t.Errorf("weight %q", weight)
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}
}
