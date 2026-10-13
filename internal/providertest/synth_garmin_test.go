package providertest

// The contract between internal/synth's Garmin emitter and the shipped Garmin
// closure: three generated days. The connect recordings answer the real
// `garminsync` through the mock server's lookup, every generated delivery
// goes through the real `garminpush` in posting order as the webhook door
// hands it over, and every mirror row then through the `healthgarmin`
// adapter. A change to a shape either function reads that the emitter does
// not follow fails here.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/synth"
)

// synthGarminFake serves a generated recordings directory by the mock
// server's rules (internal/providere2e/runner/mockserver.py): the exact name,
// then the name without the pagination parameters, then the bare path; a
// `__responses` recording answers its entries in order and repeats the last,
// each an `{"__response": {"status"}, "body"}` envelope. A request nothing
// answers is 502 and counted.
type synthGarminFake struct {
	fakeAPI

	mu     sync.Mutex
	calls  map[string]int
	served map[string]int
	misses []string
}

func newSynthGarminFake(t *testing.T, dir string) *synthGarminFake {
	t.Helper()
	f := &synthGarminFake{calls: map[string]int{}, served: map[string]int{}}
	f.serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		pairs := synth.Query(r.URL.Query())
		var thin []synth.Pair
		for _, p := range pairs {
			if !synth.PaginationParams[p.Key] {
				thin = append(thin, p)
			}
		}
		for _, name := range []string{
			synth.RecordingName(r.Method, r.URL.Path, pairs...),
			synth.RecordingName(r.Method, r.URL.Path, thin...),
			synth.RecordingName(r.Method, r.URL.Path),
		} {
			raw, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a name the lookup composed under the test's own directory
			if err != nil {
				continue
			}
			f.answer(t, w, name, raw)
			return
		}
		f.mu.Lock()
		f.misses = append(f.misses, r.Method+" "+r.URL.String())
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	return f
}

func (f *synthGarminFake) answer(t *testing.T, w http.ResponseWriter, name string, raw []byte) {
	f.mu.Lock()
	n := f.calls[name]
	f.calls[name]++
	f.mu.Unlock()
	var rec struct {
		Responses []json.RawMessage `json:"__responses"`
	}
	if err := json.Unmarshal(raw, &rec); err == nil && len(rec.Responses) > 0 {
		raw = rec.Responses[min(n, len(rec.Responses)-1)]
	}
	status, body := http.StatusOK, raw
	var env struct {
		Response *struct {
			Status int `json:"status"`
		} `json:"__response"`
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Response != nil {
		status, body = env.Response.Status, env.Body
		if string(body) == `""` {
			body = nil
		}
	}
	f.mu.Lock()
	f.served[fmt.Sprint(status)]++
	f.mu.Unlock()
	if len(body) > 0 {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		t.Errorf("write %s: %v", name, err)
	}
}

func TestSynthGarminConnectsPushesAndAdapts(t *testing.T) {
	requireUV(t)
	t.Parallel()
	// Saturday to Monday: a weigh-in either side, the Sunday's blood pressure,
	// a strength session, the long run and maybe yoga.
	p, err := synth.NewPersona(synth.Options{
		Start: synth.NewDate(2026, time.September, 12), End: synth.NewDate(2026, time.September, 14),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := synth.Garmin{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := synth.WriteOutput(dir, synth.Garmin{}, out); err != nil {
		t.Fatal(err)
	}
	days := p.Days()
	var workouts, weighIns, pressures, naps int
	for _, d := range days {
		x := p.Day(d)
		workouts += len(x.Workouts)
		naps += len(x.Naps)
		if x.WeightKg != 0 {
			weighIns++
		}
		if x.BloodPressure != nil {
			pressures++
		}
	}
	if workouts == 0 || weighIns == 0 || pressures == 0 {
		t.Fatal("the three days hold no workout, weigh-in or reading; pick days that do")
	}

	ds := newHealthGarminDataset(t)

	// The connect step: garminsync records the user and permissions and asks
	// for every type's backfill, all answered by the generated recordings.
	fake := newSynthGarminFake(t, filepath.Join(dir, "garmin", "recordings"))
	syncGarmin(t, ds, garminConfig(t, ds, fake.ts.URL+"/wellness-api", nil))
	if len(fake.misses) != 0 {
		t.Fatalf("the generated recordings did not answer %v", fake.misses)
	}
	acct := mustGet(t, ds, garminAccountType, garminAccountID).Properties
	if acct["userId"] != garminUser || acct["syncState"] != "ok" || acct["lastCompletedAt"] == nil {
		t.Fatalf("the first sync left %v", acct)
	}
	if got := normal(t, acct["permissions"]); !reflect.DeepEqual(got, []any{"ACTIVITY_EXPORT", "COURSE_IMPORT", "HEALTH_EXPORT", "MCT_EXPORT", "WORKOUT_IMPORT"}) {
		t.Errorf("permissions %v", got)
	}
	backfilled := map[string]bool{}
	fake.fakeAPI.mu.Lock()
	paths := append([]string{}, fake.paths...)
	fake.fakeAPI.mu.Unlock()
	for _, path := range paths {
		if rest, ok := strings.CutPrefix(path, "/wellness-api/rest/backfill/"); ok {
			backfilled[rest] = true
		}
	}
	if len(backfilled) != len(garminBackfilled) || fake.served["202"] != len(garminBackfilled) || fake.served["409"] == 0 {
		t.Errorf("backfill asked for %v, answered %v", sortedKeys(backfilled), fake.served)
	}

	// The pushes, in posting order, each the request the door records.
	cfg := garminConfig(t, ds, "", nil)
	want := map[string]map[string]map[string]any{}
	for i, d := range out.Deliveries {
		raw, err := d.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		var env struct {
			Trigger string         `json:"trigger"`
			Request map[string]any `json:"request"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if env.Trigger != "garmin-webhook" {
			t.Fatalf("delivery %s goes to %s", d.Name, env.Trigger)
		}
		pushGarmin(t, ds, cfg, fmt.Sprintf("synth-%d", i), env.Request)
		var body map[string][]map[string]any
		if err := json.Unmarshal(d.Body, &body); err != nil {
			t.Fatal(err)
		}
		for key, entries := range body {
			kind, ok := garminKinds[key]
			if !ok {
				t.Fatalf("delivery %s carries %s, which no kind mirrors", d.Name, key)
			}
			if want[kind] == nil {
				want[kind] = map[string]map[string]any{}
			}
			for _, e := range entries {
				want[kind][e["summaryId"].(string)] = e
			}
		}
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}

	// The mirror: one row per summary, every delivered field verbatim, and the
	// counts the three days imply.
	for kind, n := range map[string]int{
		"daily": len(days), "stressdetail": len(days), "sleep": len(days), "hrv": len(days), "pulseox": len(days),
		"skintemp": len(days), "activity": workouts, "activitydetail": workouts, "bodycomp": weighIns,
		"bloodpressure": pressures,
	} {
		if len(want[kind]) != n {
			t.Errorf("the deliveries carry %d %s summaries, want %d", len(want[kind]), kind, n)
		}
	}
	if len(want["epoch"]) < len(days)*96 || len(want["respiration"]) < len(days)*20 || len(want["usermetric"]) == 0 {
		t.Errorf("%d epochs, %d breathing slices, %d user metrics over %d days",
			len(want["epoch"]), len(want["respiration"]), len(want["usermetric"]), len(days))
	}
	for kind, entries := range want {
		rows := bySummaryID(t, ds, kind)
		if len(rows) != len(entries) {
			t.Errorf("%s: %d rows for %d summaries", kind, len(rows), len(entries))
		}
		for sid, entry := range entries {
			row := rows[sid]
			if row == nil || row.ID != garminAccountID+"/"+sid {
				t.Errorf("%s %s: row %v", kind, sid, row)
				continue
			}
			for field, v := range entry {
				if !reflect.DeepEqual(normal(t, row.Properties[field]), normal(t, v)) {
					t.Errorf("%s %s: %s stored %v, delivered %v", kind, sid, field, row.Properties[field], v)
				}
			}
		}
	}
	if n := countLive(t, ds, garminNoticeType); n != 0 {
		t.Errorf("%d notices", n)
	}
	acct = mustGet(t, ds, garminAccountType, garminAccountID).Properties
	if acct["syncState"] != "ok" || acct["lastPushAt"] == nil {
		t.Errorf("the pushes left the account %v: %v", acct["syncState"], acct["syncMessage"])
	}

	// The adapter, every mirror row once, as its record trigger delivers them.
	adaptAll(t, ds, "daily", "epoch", "sleep", "bodycomp", "stressdetail", "usermetric", "pulseox",
		"respiration", "hrv", "bloodpressure", "skintemp", "activity", "activitydetail")
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d adapter deliveries parked", n)
	}

	// Observations: Garmin's daily totals, a coded amount per stage segment,
	// the night's HRV average and high, its skin temperature, every weigh-in's
	// six, the reading's three and VO2 max.
	stages := 0
	for _, s := range want["sleep"] {
		for _, list := range s["sleepLevelsMap"].(map[string]any) {
			stages += len(list.([]any))
		}
	}
	byMetric := map[string]int{}
	for _, row := range listLive(t, ds, obsObservation) {
		byMetric[metricOf(row.Properties)]++
	}
	wantMetric := map[string]int{
		"health/steps": len(days), "health/distance": len(days), "health/floors": len(days),
		"health/active_energy": len(days), "health/basal_energy": len(days), "health/resting_heart_rate": len(days),
		"health/garmin_stress": len(days), "health/sleep_stage": stages, "health/hrv_rmssd": 2 * len(days),
		"health/skin_temperature_deviation": len(days), "health/vo2_max": len(want["usermetric"]),
		"health/systolic_blood_pressure": pressures, "health/diastolic_blood_pressure": pressures, "health/heart_rate": pressures,
	}
	for _, m := range []string{"body_mass", "muscle_mass", "bone_mass", "body_fat_fraction", "body_water_fraction", "bmi"} {
		wantMetric["health/"+m] = weighIns
	}
	if !reflect.DeepEqual(byMetric, wantMetric) {
		t.Errorf("observations by metric %v, want %v", byMetric, wantMetric)
	}

	// Traces: every sample the watch sent, on the account's source by the
	// hour, and the workouts' on the device's.
	samples := map[string]int{}
	for _, e := range want["daily"] {
		samples["account/heart_rate"] += len(e["timeOffsetHeartRateSamples"].(map[string]any))
	}
	for _, e := range want["stressdetail"] {
		samples["account/garmin_stress"] += len(e["timeOffsetStressLevelValues"].(map[string]any))
		samples["account/garmin_body_battery"] += len(e["timeOffsetBodyBatteryValues"].(map[string]any))
	}
	slices := map[float64]bool{}
	for _, e := range want["epoch"] {
		slices[e["startTimeInSeconds"].(float64)] = true
	}
	for _, m := range []string{"steps", "distance", "active_energy"} {
		samples["account/"+m] = len(slices)
	}
	for _, e := range want["pulseox"] {
		samples["account/spo2"] += len(e["timeOffsetSpo2Values"].(map[string]any))
	}
	for _, e := range want["hrv"] {
		samples["account/hrv_rmssd"] += len(e["hrvValues"].(map[string]any))
	}
	samples["account/respiratory_rate"] = len(want["respiration"])
	for _, e := range want["activitydetail"] {
		for _, s := range e["samples"].([]any) {
			for field, metric := range map[string]string{
				"heartRate": "heart_rate", "speedMetersPerSecond": "speed", "stepsPerMinute": "cadence",
				"powerInWatts": "running_power", "bikeCadenceInRPM": "cycling_cadence", "totalDistanceInMeters": "distance",
			} {
				if s.(map[string]any)[field] != nil {
					samples["forerunner265/"+metric]++
				}
			}
		}
	}
	points := map[string]int{}
	for _, row := range listLive(t, ds, obsTrace) {
		src := storedRefPath(row.Properties["source"])
		slug := src[strings.LastIndex(src, "/")+1:]
		if !strings.HasPrefix(row.ID, "garmin/"+garminAccountID+"/"+slug+"/") {
			t.Errorf("trace %s is on source %s", row.ID, src)
		}
		key := slug + "/" + strings.TrimPrefix(metricOf(row.Properties), "health/")
		var vals []any
		if v, ok := row.Properties["values"].([]any); ok {
			vals = v
		}
		points[key] += len(vals)
	}
	if !reflect.DeepEqual(points, samples) {
		keys := map[string]bool{}
		for k := range points {
			keys[k] = true
		}
		for k := range samples {
			keys[k] = true
		}
		var diff []string
		for _, k := range sortedKeys(keys) {
			if points[k] != samples[k] {
				diff = append(diff, fmt.Sprintf("%s: %d traced of %d sent", k, points[k], samples[k]))
			}
		}
		sort.Strings(diff)
		t.Errorf("trace points differ from the samples sent:\n%s", strings.Join(diff, "\n"))
	}

	// The records: a sleep per night and nap, a workout per activity, and the
	// two sources, the account's and the watch's.
	for kind, n := range map[string]int{healthSleep: len(days) + naps, healthWorkout: workouts, obsSource: 2} {
		if got := countLive(t, ds, kind); got != n {
			t.Errorf("%d %s, want %d", got, kind, n)
		}
	}
	for id, class := range map[string]string{
		"garmin/" + garminAccountID + "/account": "service", "garmin/" + garminAccountID + "/forerunner265": "watch",
	} {
		src := mustGet(t, ds, obsSource, id).Properties
		if src["manufacturer"] != "Garmin" || src["class"] != class {
			t.Errorf("source %s is %v, want class %s", id, src, class)
		}
	}

	// One value end to end: the first day's steps are the daily's.
	var first map[string]any
	for _, e := range want["daily"] {
		if e["calendarDate"] == days[0].String() {
			first = e
		}
	}
	obs := mustGet(t, ds, obsObservation, "garmin/"+garminAccountID+"/"+first["summaryId"].(string)+"/steps").Properties
	if v, u := quantityOf(t, obs, "value"); v != fmt.Sprint(first["steps"]) || u != "{steps}" || obs["periodId"] != days[0].String() {
		t.Errorf("the first day's steps are %s %s on %v, the daily sent %v", v, u, obs["periodId"], first["steps"])
	}
}
