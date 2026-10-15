package providertest

// The Apple Health suite. The applehealth closure has no upstream to fake:
// the Health Auto Export iOS app POSTs JSON to the `applehealth-webhook`
// trigger, so every case hands `applehealthingest` a request out of
// testdata/applehealth (the deliveries the research recorded or assembled,
// each one `{"request": …}` as the webhook door builds it) and asserts the
// mirror rows it leaves. The door itself, its key, its body cap and a parked
// delivery's retry run through the real API handler over the real engine.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	appleHealthDir    = providersDir + "/applehealth"
	appleHealthPkg    = "providers.substrate.reamde.dev/applehealth"
	appleHealthIngest = appleHealthPkg + "/applehealthingest"
	ahAccount         = appleHealthPkg + "/account"
	ahExport          = appleHealthPkg + "/export"
	ahBatch           = appleHealthPkg + "/metricbatch"
	ahWorkout         = appleHealthPkg + "/workout"
	ahStateOfMind     = appleHealthPkg + "/stateofmind"
	ahMedication      = appleHealthPkg + "/medication"
	ahSymptom         = appleHealthPkg + "/symptom"
	ahCycleEvent      = appleHealthPkg + "/cycleevent"
	ahECG             = appleHealthPkg + "/ecg"
	ahHRNotification  = appleHealthPkg + "/heartratenotification"
	ahTrigger         = "applehealth-webhook"
	ahFixtures        = "testdata/applehealth/"
	// appDate is how the app writes a date: `yyyy-MM-dd HH:mm:ss Z`.
	appDate = "2006-01-02 15:04:05 -0700"
	// The persona's two device names carry U+2019 and U+00A0, as Health writes them.
	adaWatch  = "Ada’s Apple Watch"
	adaIPhone = "Ada’s iPhone"
)

// ahRequest is one fixture's request, freshly decoded so a case may edit it.
func ahRequest(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(ahFixtures + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var doc struct {
		Request map[string]any `json:"request"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return doc.Request
}

// ahBodyText is the raw body a request carries.
func ahBodyText(t *testing.T, req map[string]any) string {
	t.Helper()
	body, _ := req["body"].(map[string]any)
	text, ok := body["text"].(string)
	if !ok {
		t.Fatal("the fixture request carries no body text")
	}
	return text
}

// ahMetric is one metric object of a fixture body, its rows decoded.
type ahMetric struct {
	Name  string           `json:"name"`
	Units string           `json:"units"`
	Data  []map[string]any `json:"data"`
}

// ahMetrics decodes a fixture body's `data.metrics`, by name.
func ahMetrics(t *testing.T, req map[string]any) map[string]ahMetric {
	t.Helper()
	var doc struct {
		Data struct {
			Metrics []ahMetric `json:"metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(ahBodyText(t, req)), &doc); err != nil {
		t.Fatalf("decode the fixture body: %v", err)
	}
	out := map[string]ahMetric{}
	for _, m := range doc.Data.Metrics {
		out[m.Name] = m
	}
	return out
}

// ahInstant parses one of the app's dates.
func ahInstant(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(appDate, s)
	if err != nil {
		t.Fatalf("parse the app date %q: %v", s, err)
	}
	return at.UTC()
}

// rowDate is a metric row's time key: `date`, or a sleep segment's `startDate`.
func rowDate(row map[string]any) string {
	if s, ok := row["date"].(string); ok {
		return s
	}
	s, _ := row["startDate"].(string)
	return s
}

// ahHours is the UTC hours a metric's rows fall in, which is how many hour
// batches a seconds or minutes delivery writes for it.
func ahHours(t *testing.T, m ahMetric) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, row := range m.Data {
		out[ahInstant(t, rowDate(row)).Format("2006-01-02T15")]++
	}
	return out
}

// batchID is a metric batch's composed id.
func batchID(account, metric, grouping, period string) string {
	return account + "/" + metric + "/" + grouping + "/" + period
}

// newAppleHealth installs the closure on a core repository and writes one
// account per id, the way an owner creates them by hand.
func newAppleHealth(t *testing.T, accounts ...string) (substrate.Service, substrate.Dataset) {
	t.Helper()
	requirePython(t)
	t.Parallel()
	svc, ds := newCoreDataset(t)
	install(t, ds, appleHealthDir, nil)
	for _, id := range accounts {
		mustPut(t, ds, substrate.PutInput{
			Kind: ahAccount, ID: id,
			Properties: map[string]any{"displayName": "Ada's iPhone"},
		})
	}
	return svc, ds
}

// ahConfig is the config a webhook fire hands the function: every account
// record, in the shape the engine injects (id, kind, version, properties).
func ahConfig(t *testing.T, ds substrate.Dataset) map[string]any {
	t.Helper()
	var accounts []any
	for _, row := range listLive(t, ds, ahAccount) {
		accounts = append(accounts, map[string]any{
			"id": row.ID, "kind": row.Kind, "version": row.Version, "properties": row.Properties,
		})
	}
	return map[string]any{"bundle": appleHealthPkg, "accounts": accounts}
}

// ahEnvelope is the envelope the webhook door builds around a request.
func ahEnvelope(req map[string]any, fire, at string) map[string]any {
	return map[string]any{
		"request":    req,
		"fire":       map[string]any{"id": fire, "at": at},
		"repository": map[string]any{"authority": "ada.example.com"},
	}
}

// deliverAH runs one delivery to the end, applying each page the way the
// dispatcher does, and returns the last page's output and the page count.
func deliverAH(t *testing.T, ds substrate.Dataset, req map[string]any, fire string) (map[string]any, int) {
	t.Helper()
	s := newStepper(t, ds, appleHealthIngest, ahConfig(t, ds))
	s.setEnvelope(ahEnvelope(req, fire, "2026-10-08T05:31:02Z"))
	var resume any
	for pages := 1; ; pages++ {
		if pages > 40 {
			t.Fatal("the delivery did not finish in 40 pages")
		}
		_, out, cur := s.step(resume)
		s.apply()
		if cur == nil {
			m, _ := out.(map[string]any)
			return m, pages
		}
		resume = cur
	}
}

// refuseAH runs one delivery's first page and returns the error it raised.
func refuseAH(t *testing.T, ds substrate.Dataset, req map[string]any) error {
	t.Helper()
	s, err := engine.NewStepper(ds, appleHealthIngest, ahConfig(t, ds))
	if err != nil {
		t.Fatalf("stepper: %v", err)
	}
	s.SetEnvelope(ahEnvelope(req, "hook-refused", "2026-10-08T05:31:02Z"))
	effects, _, _, err := s.Step(context.Background(), nil)
	if err == nil {
		t.Fatalf("the delivery was not refused: it staged %d effects", len(effects))
	}
	return err
}

// rowsOf is a batch's rows.
func rowsOf(t *testing.T, row *substrate.Record) []map[string]any {
	t.Helper()
	list, _ := row.Properties["rows"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s: a row is a %T", row.ID, item)
		}
		out = append(out, m)
	}
	return out
}

// obj reads a nested object property by path.
func obj(t *testing.T, v any, path ...string) any {
	t.Helper()
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("at %q: want an object, got %T", p, v)
		}
		v = m[p]
	}
	return v
}

// A day of per-minute heart rate, steps, distance, energy and effort: one
// batch per metric and UTC hour, the rows in the app's own fields, units and
// source labels, an export receipt that names the body, and the account
// stamped.
func TestAppleHealthMinuteHeartRateAndSteps(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	req := ahRequest(t, "delivery_1_heart_rate_steps_minutes.json")
	metrics := ahMetrics(t, req)
	out, pages := deliverAH(t, ds, req, "hook-minutes")
	if pages != 1 {
		t.Fatalf("a 90 KB delivery took %d pages", pages)
	}

	wantBatches, wantRows := 0, 0
	for name, m := range metrics {
		hours := ahHours(t, m)
		wantBatches += len(hours)
		wantRows += len(m.Data)
		for hour, n := range hours {
			row := mustGet(t, ds, ahBatch, batchID("iphone", name, "minutes", hour))
			if got := len(rowsOf(t, row)); got != n {
				t.Fatalf("%s holds %d rows, want the %d the delivery carried for that hour", row.ID, got, n)
			}
			if row.Properties["units"] != m.Units {
				t.Fatalf("%s units = %v, want the app's %q verbatim", row.ID, row.Properties["units"], m.Units)
			}
			if row.Properties["grouping"] != "minutes" || row.Properties["name"] != name {
				t.Fatalf("%s: name %v grouping %v", row.ID, row.Properties["name"], row.Properties["grouping"])
			}
			start, _ := time.Parse("2006-01-02T15", hour)
			if row.At == nil || !row.At.Equal(start) || row.EndsAt == nil || !row.EndsAt.Equal(start.Add(time.Hour)) {
				t.Fatalf("%s spans %v to %v, want the UTC hour %s", row.ID, row.At, row.EndsAt, hour)
			}
		}
	}
	if got := countLive(t, ds, ahBatch); got != wantBatches {
		t.Fatalf("%d metric batches, want %d (one per metric and UTC hour)", got, wantBatches)
	}
	if asFloat(t, out["written"]) != float64(wantBatches) {
		t.Fatalf("output %v, want %d written", out, wantBatches)
	}

	// The first heart rate row, field by field: Min/Avg/Max camelCased, the
	// date verbatim with its offset, the source with Health's own characters.
	first := metrics["heart_rate"].Data[0]
	hour := ahInstant(t, first["date"].(string)).Format("2006-01-02T15")
	hr := rowsOf(t, mustGet(t, ds, ahBatch, batchID("iphone", "heart_rate", "minutes", hour)))
	if hr[0]["date"] != first["date"] || hr[0]["source"] != adaWatch ||
		hr[0]["min"] != first["Min"] || hr[0]["avg"] != first["Avg"] || hr[0]["max"] != first["Max"] {
		t.Fatalf("first heart rate row = %v, want the app's %v", hr[0], first)
	}
	for i := 1; i < len(hr); i++ {
		if ahInstant(t, rowDate(hr[i-1])).After(ahInstant(t, rowDate(hr[i]))) {
			t.Fatalf("rows out of time order at %d: %v then %v", i, hr[i-1]["date"], hr[i]["date"])
		}
	}
	effort := listLive(t, ds, ahBatch)
	for _, row := range effort {
		if row.Properties["name"] == "physical_effort" && row.Properties["units"] != "kcal/hr·kg" {
			t.Fatalf("physical_effort units = %q, want the middle dot kept", row.Properties["units"])
		}
	}

	text := ahBodyText(t, req)
	sum := sha256.Sum256([]byte(text))
	ex := mustGet(t, ds, ahExport, "hook-minutes")
	if asFloat(t, ex.Properties["byteSize"]) != float64(len(text)) || ex.Properties["bodySha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("export names the body as %v bytes %v", ex.Properties["byteSize"], ex.Properties["bodySha256"])
	}
	if asFloat(t, obj(t, ex.Properties["counts"], "metrics")) != 5 || asFloat(t, obj(t, ex.Properties["counts"], "metricRows")) != float64(wantRows) {
		t.Fatalf("export counts = %v, want 5 metrics and %d rows", ex.Properties["counts"], wantRows)
	}
	names := []string{}
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	if fmt.Sprint(ex.Properties["metricNames"]) != fmt.Sprint(names) {
		t.Fatalf("export metricNames = %v, want %v", ex.Properties["metricNames"], names)
	}
	if ex.Properties["automationAggregation"] != "minutes" || ex.Properties["sessionId"] != "003E2396-AD04-49DB-B1D3-050A8E0A3E31" {
		t.Fatalf("export headers: %v %v", ex.Properties["automationAggregation"], ex.Properties["sessionId"])
	}
	if ex.At == nil || !ex.At.Equal(time.Date(2026, 10, 8, 5, 31, 2, 0, time.UTC)) {
		t.Fatalf("export at = %v, want the fire's receipt time", ex.At)
	}

	acct := mustGet(t, ds, ahAccount, "iphone")
	if acct.Properties["syncState"] != "ok" || acct.Properties["lastSyncedAt"] == nil ||
		storedRefPath(acct.Properties["lastExport"]) != ahExport+"/hook-minutes" {
		t.Fatalf("account stamp = %v", acct.Properties)
	}
}

// Sleep as segments (Summarize Data off) and three workouts. A night with
// stages from the watch AND Garmin Connect keeps both: the mirror says what
// arrived and the adapter decides. A workout keeps every documented field,
// its `{qty, units}` objects and its per-minute and route arrays.
func TestAppleHealthSleepAndWorkouts(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	req := ahRequest(t, "delivery_2_sleep_and_workouts.json")
	deliverAH(t, ds, req, "hook-night")

	sleep := ahMetrics(t, req)["sleep_analysis"]
	var rows []map[string]any
	for _, row := range listLive(t, ds, ahBatch) {
		if row.Properties["name"] != "sleep_analysis" || row.Properties["grouping"] != "seconds" {
			t.Fatalf("unexpected batch %s", row.ID)
		}
		rows = append(rows, rowsOf(t, row)...)
	}
	if len(rows) != len(sleep.Data) {
		t.Fatalf("%d sleep rows, want the %d segments sent", len(rows), len(sleep.Data))
	}
	sources := map[string]int{}
	for _, row := range rows {
		if row["startDate"] == nil || row["endDate"] == nil || row["value"] == nil {
			t.Fatalf("a segment lost its bounds or stage: %v", row)
		}
		if _, ok := row["date"]; ok {
			t.Fatalf("a segment grew a date: %v", row)
		}
		sources[row["source"].(string)]++
	}
	if sources[adaWatch] == 0 || sources["Garmin Connect"] == 0 || sources[adaIPhone] == 0 {
		t.Fatalf("segment sources = %v, want the watch, Garmin Connect and the iPhone's In Bed", sources)
	}

	if got := countLive(t, ds, ahWorkout); got != 3 {
		t.Fatalf("%d workouts, want 3", got)
	}
	run := mustGet(t, ds, ahWorkout, "iphone/EB0B1D81-50F6-4AB6-8841-28B68D3DE5C6")
	if run.Properties["name"] != "Running" || run.Properties["start"] != "2026-10-07 08:05:37 +0300" ||
		run.At == nil || !run.At.Equal(time.Date(2026, 10, 7, 5, 5, 37, 0, time.UTC)) ||
		run.EndsAt == nil || !run.EndsAt.Equal(time.Date(2026, 10, 7, 5, 41, 56, 0, time.UTC)) {
		t.Fatalf("run = %v at %v to %v", run.Properties, run.At, run.EndsAt)
	}
	if asFloat(t, obj(t, run.Properties["distance"], "qty")) != 6.1 || obj(t, run.Properties["distance"], "units") != "km" ||
		asFloat(t, obj(t, run.Properties["heartRate"], "max", "qty")) != 173 || asFloat(t, run.Properties["duration"]) != 2179 {
		t.Fatalf("run quantities = %v %v", run.Properties["distance"], run.Properties["heartRate"])
	}
	hrData, _ := run.Properties["heartRateData"].([]any)
	if len(hrData) != 37 || obj(t, hrData[0], "source") != "Garmin Connect" || asFloat(t, obj(t, hrData[0], "max")) != 107 {
		t.Fatalf("run heartRateData = %d rows, first %v", len(hrData), hrData)
	}
	walk := mustGet(t, ds, ahWorkout, "iphone/3E24A55C-AED2-46E5-BA55-1C209B348C0E")
	route, _ := walk.Properties["route"].([]any)
	steps, _ := walk.Properties["stepCount"].([]any)
	if len(route) != 105 || len(steps) != 27 || obj(t, walk.Properties["avgSpeed"], "units") != "kmph" ||
		obj(t, walk.Properties["metadata"], "HKTimeZone") != "Europe/Athens" ||
		asFloat(t, obj(t, route[0], "latitude")) != 37.972587 {
		t.Fatalf("walk: route %d, steps %d, avgSpeed %v, metadata %v", len(route), len(steps),
			walk.Properties["avgSpeed"], walk.Properties["metadata"])
	}
	lift := mustGet(t, ds, ahWorkout, "iphone/E956E694-D171-4E4D-922B-A0B57DC8E5FB")
	if recovery, _ := lift.Properties["heartRateRecovery"].([]any); len(recovery) != 3 ||
		obj(t, lift.Properties["intensity"], "units") != "MET" {
		t.Fatalf("strength workout: recovery %v intensity %v", lift.Properties["heartRateRecovery"], lift.Properties["intensity"])
	}
}

// Thirty days of weigh-ins, body fat and blood pressure: two weigh-ins on one
// day stay two rows, a row the app sent without `source` stays without one,
// and the per-metric extras (systolic and diastolic, mealTime) are kept.
func TestAppleHealthBodyMeasurements(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	req := ahRequest(t, "delivery_3_body_measurements.json")
	metrics := ahMetrics(t, req)
	deliverAH(t, ds, req, "hook-body")

	byName := map[string][]map[string]any{}
	for _, row := range listLive(t, ds, ahBatch) {
		name := row.Properties["name"].(string)
		byName[name] = append(byName[name], rowsOf(t, row)...)
	}
	for name, m := range metrics {
		if len(byName[name]) != len(m.Data) {
			t.Fatalf("%s: %d rows written, %d sent", name, len(byName[name]), len(m.Data))
		}
	}
	var oct3, noSource int
	for _, row := range byName["weight_body_mass"] {
		if strings.HasPrefix(row["date"].(string), "2026-10-03") {
			oct3++
		}
		if _, ok := row["source"]; !ok {
			noSource++
			if !strings.HasPrefix(row["date"].(string), "2026-09-18") {
				t.Fatalf("a weigh-in lost its source: %v", row)
			}
		}
	}
	if oct3 != 2 || noSource != 1 {
		t.Fatalf("weigh-ins on 2026-10-03 = %d (want 2), rows without source = %d (want 1)", oct3, noSource)
	}
	for _, row := range byName["blood_pressure"] {
		if asFloat(t, row["systolic"]) < 90 || asFloat(t, row["diastolic"]) < 50 {
			t.Fatalf("blood pressure row = %v", row)
		}
	}
	for _, row := range byName["blood_glucose"] {
		if row["mealTime"] == nil {
			t.Fatalf("glucose row lost its mealTime: %v", row)
		}
	}
}

// The same day sent again after Garmin Connect synced. The 36 minutes of the
// run keep their minute and change both value and source label, and heart
// rate gains Garmin's rows: those three hour batches are rewritten, with no
// row twice, and every other batch is left exactly as it was.
func TestAppleHealthCorrectedRedelivery(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	deliverAH(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-first")
	before := map[string]int64{}
	for _, row := range listLive(t, ds, ahBatch) {
		before[row.ID] = row.Version
	}
	req := ahRequest(t, "delivery_4_redelivery_corrected.json")
	out, _ := deliverAH(t, ds, req, "hook-again")
	if asFloat(t, out["written"]) != 3 || asFloat(t, out["unchanged"]) != float64(len(before)-3) {
		t.Fatalf("redelivery output = %v, want 3 written and %d unchanged", out, len(before)-3)
	}

	changed := []string{}
	for _, row := range listLive(t, ds, ahBatch) {
		v, ok := before[row.ID]
		if !ok {
			t.Fatalf("the redelivery made a new batch %s", row.ID)
		}
		export := storedRefPath(row.Properties["export"])
		switch {
		case row.Version != v:
			changed = append(changed, row.ID)
			if export != ahExport+"/hook-again" {
				t.Fatalf("%s changed but names %s", row.ID, export)
			}
		case export != ahExport+"/hook-first":
			t.Fatalf("%s is unchanged but names %s", row.ID, export)
		}
	}
	sort.Strings(changed)
	want := []string{
		batchID("iphone", "heart_rate", "minutes", "2026-10-07T05"),
		batchID("iphone", "step_count", "minutes", "2026-10-07T05"),
		batchID("iphone", "walking_running_distance", "minutes", "2026-10-07T05"),
	}
	if !slices.Equal(changed, want) {
		t.Fatalf("changed batches = %v, want %v", changed, want)
	}

	sent := map[string]map[string]any{}
	for _, row := range ahMetrics(t, req)["step_count"].Data {
		sent[row["date"].(string)] = row
	}
	steps := rowsOf(t, mustGet(t, ds, ahBatch, want[1]))
	seen := map[string]bool{}
	relabelled := 0
	for _, row := range steps {
		date := row["date"].(string)
		if seen[date] {
			t.Fatalf("minute %s is in the batch twice", date)
		}
		seen[date] = true
		s, ok := sent[date]
		if !ok {
			t.Fatalf("row %s is not in the redelivery", date)
		}
		if row["qty"] != s["qty"] || row["source"] != s["source"] {
			t.Fatalf("row %s = %v, want the redelivery's %v", date, row, s)
		}
		if row["source"] == "Garmin Connect|"+adaIPhone {
			relabelled++
		}
	}
	if relabelled != 36 {
		t.Fatalf("%d rows carry the corrected label, want 36", relabelled)
	}
	garmin := 0
	for _, row := range rowsOf(t, mustGet(t, ds, ahBatch, want[0])) {
		if row["source"] == "Garmin Connect" {
			garmin++
		}
	}
	if garmin != 36 {
		t.Fatalf("%d heart rate rows from Garmin Connect in the run's hour, want 36", garmin)
	}
}

// ahMetricRequest is a request carrying one metric's rows, built the way the
// app sends a JSON export at one time grouping.
func ahMetricRequest(t *testing.T, grouping, name, units string, rows []map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"data": map[string]any{"metrics": []any{
		map[string]any{"name": name, "units": units, "data": rows},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"method": "POST", "contentType": "application/json",
		"headers": map[string]any{"content-type": "application/json", "automation-aggregation": grouping},
		"query":   map[string]any{},
		"body":    map[string]any{"text": string(body)},
	}
}

// heartRows is one heart rate row per app date.
func heartRows(dates ...string) []map[string]any {
	out := make([]map[string]any, 0, len(dates))
	for i, d := range dates {
		out = append(out, map[string]any{"Min": 50 + i, "Avg": 55 + i, "Max": 60 + i, "date": d, "source": adaWatch})
	}
	return out
}

// A redelivery replaces what it covers. The first delivery writes heart
// rate in four UTC hours; the user then deletes the 11:05 sample in Health,
// and a Previous 7 Days export carries 10:05 and 12:05. The 11:00 batch,
// which the redelivery carries no row for, lies inside its span and is
// deleted; the 09:00 batch lies outside it and stays.
func TestAppleHealthRedeliveryEmptiesBatchesInsideItsSpan(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	first := ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-10-01 09:05:00 +0000", "2026-10-01 10:05:00 +0000",
		"2026-10-01 11:05:00 +0000", "2026-10-01 12:05:00 +0000"))
	deliverAH(t, ds, first, "hook-first")
	hour := func(h string) string { return batchID("iphone", "heart_rate", "minutes", "2026-10-01T"+h) }
	for _, h := range []string{"09", "10", "11", "12"} {
		if got := len(rowsOf(t, mustGet(t, ds, ahBatch, hour(h)))); got != 1 {
			t.Fatalf("hour %s holds %d rows after the first delivery, want 1", h, got)
		}
	}

	again := ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-10-01 10:05:00 +0000", "2026-10-01 12:05:00 +0000"))
	out, _ := deliverAH(t, ds, again, "hook-again")
	if live(t, ds, ahBatch, hour("11")) {
		t.Fatalf("the 11:00 batch survived a redelivery whose span covers it and carries none of its rows (output %v)", out)
	}
	if !live(t, ds, ahBatch, hour("09")) {
		t.Fatal("the 09:00 batch, outside the redelivery's span, was removed")
	}
	if got := countLive(t, ds, ahBatch); got != 3 {
		t.Fatalf("%d batches after the redelivery, want 3", got)
	}
	if asFloat(t, out["written"]) != 2 || asFloat(t, out["deleted"]) != 1 {
		t.Fatalf("redelivery output = %v, want 2 written (the rewritten batches) and 1 deleted", out)
	}
	ex := mustGet(t, ds, ahExport, "hook-again").Properties
	if asFloat(t, ex["written"]) != 2 || asFloat(t, ex["deleted"]) != 1 {
		t.Fatalf("the receipt counts %v written and %v deleted, want 2 and 1", ex["written"], ex["deleted"])
	}
}

// A metric row may carry its date as epoch seconds, as the app writes ECG
// voltages and heart rate notifications. The batch keeps the number as its
// decimal text, and a second delivery of the same rows still finds them by
// that instant: three samples stay three rows.
func TestAppleHealthEpochDatesMergeOnRedelivery(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	rows := []map[string]any{
		{"qty": 1.5, "date": 1790157600, "source": adaWatch},
		{"qty": 2.5, "date": 1790157660, "source": adaWatch},
		{"qty": 3.5, "date": 1790157720.5, "source": adaWatch},
	}
	req := ahMetricRequest(t, "seconds", "step_count", "count", rows)
	deliverAH(t, ds, req, "hook-epoch-1")
	deliverAH(t, ds, req, "hook-epoch-2")
	id := batchID("iphone", "step_count", "seconds", "2026-09-23T10")
	got := rowsOf(t, mustGet(t, ds, ahBatch, id))
	if len(got) != 3 {
		t.Fatalf("%s holds %d rows after the same three samples twice, want 3: %v", id, len(got), got)
	}
	if got[0]["date"] != "1790157600" || got[2]["date"] != "1790157720.5" {
		t.Fatalf("the epoch dates are stored as %v and %v, want their decimal text", got[0]["date"], got[2]["date"])
	}
}

// A record bigger than one response frame can carry is skipped and named,
// not staged: a ten-hour ride with 37,000 route points re-encodes past the
// runner's 8 MiB frame although its body fits the door, and staging it would
// fail every attempt the same way. The rest of the delivery is written.
func TestAppleHealthSkipsARecordTooBigForAFrame(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	start := time.Date(2026, 10, 7, 6, 0, 0, 0, time.FixedZone("", 3*3600))
	route := make([]map[string]any, 0, 37000)
	for i := range 37000 {
		route = append(route, map[string]any{
			"latitude": 37.9715 + float64(i)*1e-5, "longitude": 23.7257 + float64(i)*1e-5,
			"altitude": 112.5, "course": 181.25, "courseAccuracy": 5.5, "speed": 6.75,
			"speedAccuracy": 0.5, "horizontalAccuracy": 3.25, "verticalAccuracy": 2.5,
			"timestamp": start.Add(time.Duration(i) * time.Second).Format(appDate),
		})
	}
	workout := func(id string, points []map[string]any) map[string]any {
		return map[string]any{
			"id": id, "name": "Outdoor Cycling",
			"start": start.Format(appDate), "end": start.Add(10 * time.Hour).Format(appDate),
			"route": points,
		}
	}
	body, err := json.Marshal(map[string]any{"data": map[string]any{"workouts": []any{
		workout("W-LONG", route), workout("W-SHORT", route[:10]),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) >= 8<<20 {
		t.Fatalf("built a %d byte body, want one the door admits", len(body))
	}
	req := map[string]any{
		"method": "POST", "contentType": "application/json",
		"headers": map[string]any{"content-type": "application/json"},
		"query":   map[string]any{}, "body": map[string]any{"text": string(body)},
	}
	out, _ := deliverAH(t, ds, req, "hook-long-ride")
	if !live(t, ds, ahWorkout, "iphone/W-SHORT") {
		t.Fatalf("the short workout beside the oversized one was not written (output %v)", out)
	}
	if live(t, ds, ahWorkout, "iphone/W-LONG") {
		t.Fatal("the oversized workout was written")
	}
	ex := mustGet(t, ds, ahExport, "hook-long-ride")
	named := ahWorkout + " iphone/W-LONG ("
	if !strings.Contains(fmt.Sprint(ex.Properties["oversized"]), named) ||
		!strings.Contains(fmt.Sprint(out["oversized"]), named) {
		t.Fatalf("the receipt %v and the output %v do not name the skipped workout", ex.Properties["oversized"], out["oversized"])
	}
}

// A record the owner deleted is absent to the next delivery: its stored
// rows are not merged back, and a record re-sent as it was is written again
// rather than counted unchanged and left deleted.
func TestAppleHealthDeletedRecordsAreAbsent(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	ctx := context.Background()
	deliverAH(t, ds, ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-10-01 10:05:00 +0000", "2026-10-01 10:15:00 +0000",
		"2026-10-01 10:25:00 +0000", "2026-10-01 10:35:00 +0000")), "hook-1")
	batch := batchID("iphone", "heart_rate", "minutes", "2026-10-01T10")
	if _, err := ds.Delete(ctx, substrate.ActorAPI, ahBatch, batch, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the batch: %v", err)
	}
	deliverAH(t, ds, ahMetricRequest(t, "minutes", "heart_rate", "count/min", heartRows(
		"2026-10-01 10:25:00 +0000", "2026-10-01 10:35:00 +0000")), "hook-2")
	rows := rowsOf(t, mustGet(t, ds, ahBatch, batch))
	if len(rows) != 2 || rows[0]["date"] != "2026-10-01 10:25:00 +0000" {
		t.Fatalf("the deleted batch came back holding %v, want only the two rows the delivery carried", rows)
	}

	req := ahRequest(t, "delivery_2_sleep_and_workouts.json")
	deliverAH(t, ds, req, "hook-3")
	workouts := listLive(t, ds, ahWorkout)
	if len(workouts) == 0 {
		t.Fatal("the fixture wrote no workouts")
	}
	gone := workouts[0].ID
	if _, err := ds.Delete(ctx, substrate.ActorAPI, ahWorkout, gone, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the workout: %v", err)
	}
	out, _ := deliverAH(t, ds, req, "hook-4")
	if !live(t, ds, ahWorkout, gone) {
		t.Fatalf("the deleted workout, re-sent as it was, stayed deleted (output %v)", out)
	}
}

// Day buckets are midnights of the phone's zone when it sent. A delivery from
// London on 2026-09-25, then a 30-day one from Athens on 2026-10-10: the
// Athens span covers London's two buckets, so the month batch ends holding the
// Athens days alone, each in the UTC month its instant falls in (Athens's
// 1 October starts on 30 September in UTC), and the aggregated sleep rows keep
// their stage hours.
func TestAppleHealthDailyBucketsAcrossZones(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	deliverAH(t, ds, ahRequest(t, "delivery_5_sent_from_london.json"), "hook-london")
	london := rowsOf(t, mustGet(t, ds, ahBatch, batchID("iphone", "step_count", "days", "2026-09")))
	if len(london) != 2 || !strings.HasSuffix(london[0]["date"].(string), "+0100") {
		t.Fatalf("London's step batch = %v", london)
	}
	req := ahRequest(t, "delivery_0_daily_30d.json")
	deliverAH(t, ds, req, "hook-athens")

	want := map[string]int{}
	for _, row := range ahMetrics(t, req)["step_count"].Data {
		want[ahInstant(t, row["date"].(string)).Format("2006-01")]++
	}
	for month, n := range want {
		rows := rowsOf(t, mustGet(t, ds, ahBatch, batchID("iphone", "step_count", "days", month)))
		if len(rows) != n {
			t.Fatalf("step_count %s holds %d rows, want the %d Athens days in it", month, len(rows), n)
		}
		for _, row := range rows {
			if !strings.HasSuffix(row["date"].(string), "+0300") {
				t.Fatalf("a London bucket survived the Athens delivery that covers it: %v", row)
			}
		}
	}
	if want["2026-09"] != 21 {
		t.Fatalf("the fixture puts %d Athens days in September UTC, want 21 (11 September to 1 October)", want["2026-09"])
	}
	sleep := rowsOf(t, mustGet(t, ds, ahBatch, batchID("iphone", "sleep_analysis", "days", "2026-09")))
	if asFloat(t, sleep[0]["totalSleep"]) <= 0 || asFloat(t, sleep[0]["deep"]) <= 0 || sleep[0]["inBedStart"] == nil {
		t.Fatalf("an aggregated night lost its stage hours: %v", sleep[0])
	}
}

// State of mind, medications, symptoms, cycle tracking, an ECG and both
// heart rate notification shapes, each in its own kind. Two symptoms of one
// name and start get two ids, a number where the app writes a date is kept as
// its text, and the same delivery again writes nothing.
func TestAppleHealthOtherSections(t *testing.T) {
	_, ds := newAppleHealth(t, "iphone")
	req := ahRequest(t, "delivery_7_other_sections.json")
	out, _ := deliverAH(t, ds, req, "hook-other")
	if asFloat(t, out["written"]) != 11 {
		t.Fatalf("output = %v, want 11 written", out)
	}
	for kind, n := range map[string]int{ahStateOfMind: 2, ahMedication: 2, ahSymptom: 2, ahCycleEvent: 2, ahECG: 1, ahHRNotification: 2} {
		if got := countLive(t, ds, kind); got != n {
			t.Fatalf("%d %s, want %d", got, kind, n)
		}
	}
	mood := mustGet(t, ds, ahStateOfMind, "iphone/6F1D2C1A-0B7E-4C59-9A8E-1D2B3C4D5E6F")
	if mood.Properties["kind"] != "momentary_emotion" || asFloat(t, mood.Properties["valence"]) != 0.42 ||
		fmt.Sprint(mood.Properties["labels"]) != "[calm hopeful]" {
		t.Fatalf("state of mind = %v", mood.Properties)
	}
	dose := mustGet(t, ds, ahMedication, "iphone/vitamin-d3/2026-10-07T05:00:00Z")
	codings, _ := dose.Properties["codings"].([]any)
	if dose.Properties["status"] != "Taken" || len(codings) != 1 || obj(t, codings[0], "system") != "RxNorm" {
		t.Fatalf("medication = %v", dose.Properties)
	}
	mustGet(t, ds, ahSymptom, "iphone/headache/2026-10-07T12:00:00Z")
	second := mustGet(t, ds, ahSymptom, "iphone/headache/2026-10-07T12:00:00Z/2")
	if second.Properties["severity"] != "Moderate" {
		t.Fatalf("second symptom = %v", second.Properties)
	}
	flow := mustGet(t, ds, ahCycleEvent, "iphone/menstrual-flow/2026-10-01T21:00:00Z")
	if flow.Properties["isCycleStart"] != true || flow.Properties["value"] != "Medium" {
		t.Fatalf("cycle entry = %v", flow.Properties)
	}
	if open := mustGet(t, ds, ahCycleEvent, "iphone/contraceptive/2026-10-04T21:00:00Z"); open.EndsAt != nil {
		t.Fatalf("an entry with `end: null` ends at %v", open.EndsAt)
	}
	ecg := mustGet(t, ds, ahECG, "iphone/2026-10-07T09:10:00Z")
	volts, _ := ecg.Properties["voltageMeasurements"].([]any)
	if len(volts) != 4 || obj(t, volts[0], "date") != "1791364200.0" || obj(t, volts[0], "units") != "mcV" ||
		asFloat(t, ecg.Properties["numberOfVoltageMeasurements"]) != 4 {
		t.Fatalf("ecg = %v voltages %v", ecg.Properties["classification"], volts)
	}
	observed := mustGet(t, ds, ahHRNotification, "iphone/high-heart-rate/2026-10-07T23:10:00Z")
	if obj(t, observed.Properties["source"], "name") != adaWatch || obj(t, observed.Properties["heartRateData"].([]any)[0], "start") != "1791414600.0" {
		t.Fatalf("observed notification = %v", observed.Properties)
	}
	documented := mustGet(t, ds, ahHRNotification, "iphone/heartratenotifications/2026-10-07T20:10:00Z")
	if asFloat(t, obj(t, documented.Properties["heartRate"].([]any)[0], "timestamp", "interval", "duration")) != 5 {
		t.Fatalf("documented notification = %v", documented.Properties)
	}

	again, _ := deliverAH(t, ds, ahRequest(t, "delivery_7_other_sections.json"), "hook-other-again")
	if asFloat(t, again["written"]) != 0 || asFloat(t, again["unchanged"]) != 11 {
		t.Fatalf("the same delivery again = %v, want nothing written", again)
	}
}

// The account's toggles drop a section before it is written, and the receipt
// says which; a paused account drops the whole delivery.
func TestAppleHealthTogglesAndPause(t *testing.T) {
	_, ds := newAppleHealth(t)
	mustPut(t, ds, substrate.PutInput{
		Kind: ahAccount, ID: "iphone",
		Properties: map[string]any{"enabledWorkouts": false},
	})
	acct := mustGet(t, ds, ahAccount, "iphone")
	if acct.Properties["enabledMetrics"] != true || acct.Properties["enabledEcg"] != true {
		t.Fatalf("a toggle the owner did not name is not on: %v", acct.Properties)
	}
	deliverAH(t, ds, ahRequest(t, "delivery_2_sleep_and_workouts.json"), "hook-noworkouts")
	if got := countLive(t, ds, ahWorkout); got != 0 {
		t.Fatalf("%d workouts written with enabledWorkouts off", got)
	}
	if countLive(t, ds, ahBatch) == 0 {
		t.Fatal("the sleep rows were dropped with the workouts")
	}
	ex := mustGet(t, ds, ahExport, "hook-noworkouts")
	if fmt.Sprint(ex.Properties["skippedSections"]) != "[workouts]" {
		t.Fatalf("skippedSections = %v", ex.Properties["skippedSections"])
	}

	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, ahAccount, "iphone",
		substrate.PatchInput{Properties: map[string]any{"syncPaused": true}}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	batches := countLive(t, ds, ahBatch)
	out, _ := deliverAH(t, ds, ahRequest(t, "delivery_1_heart_rate_steps_minutes.json"), "hook-paused")
	if out["skipped"] == nil || countLive(t, ds, ahBatch) != batches {
		t.Fatalf("a paused account wrote: %v", out)
	}
}

// Every refusal raises, so the delivery parks with its body kept, and says
// what to fix.
func TestAppleHealthRefusals(t *testing.T) {
	_, ds := newAppleHealth(t)
	req := ahRequest(t, "delivery_6_app_10.0.4_empty_source.json")
	if err := refuseAH(t, ds, req); !strings.Contains(err.Error(), "no "+ahAccount+" record exists") {
		t.Fatalf("no account: %v", err)
	}
	mustPut(t, ds, substrate.PutInput{Kind: ahAccount, ID: "iphone"})
	mustPut(t, ds, substrate.PutInput{Kind: ahAccount, ID: "ipad"})
	if err := refuseAH(t, ds, req); !strings.Contains(err.Error(), "2 account records exist (ipad, iphone)") {
		t.Fatalf("two accounts and no header: %v", err)
	}
	named := ahRequest(t, "delivery_6_app_10.0.4_empty_source.json")
	named["headers"].(map[string]any)["x-substrate-account"] = "watch"
	if err := refuseAH(t, ds, named); !strings.Contains(err.Error(), `names account 'watch'`) {
		t.Fatalf("an unknown account: %v", err)
	}

	for _, c := range []struct{ name, body, want string }{
		{"not JSON", "<html>", "the body is not JSON"},
		{"no data", `{"metrics": []}`, "the body has no `data` object"},
		{"a section that is not a list", `{"data": {"metrics": {}}}`, "data.metrics is not a list"},
		{"a nameless metric", `{"data": {"metrics": [{"units": "count", "data": []}]}}`, "data.metrics[0] has no name"},
		{
			"a version 1 workout", `{"data": {"workouts": [{"name": "Running", "start": "2026-10-07 08:05:37 +0300"}]}}`,
			"data.workouts[0] has no id: set the automation's Export Version to 2",
		},
	} {
		bad := ahRequest(t, "delivery_6_app_10.0.4_empty_source.json")
		bad["headers"].(map[string]any)["x-substrate-account"] = "iphone"
		bad["body"] = map[string]any{"text": c.body}
		if err := refuseAH(t, ds, bad); !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	csv := ahRequest(t, "delivery_6_app_10.0.4_empty_source.json")
	csv["headers"].(map[string]any)["x-substrate-account"] = "iphone"
	delete(csv, "body")
	csv["parts"] = []any{map[string]any{"name": "file", "filename": "export.csv", "blob": "blob-sha256-x"}}
	if err := refuseAH(t, ds, csv); !strings.Contains(err.Error(), "set the automation's Export Format to JSON") {
		t.Fatalf("multipart: %v", err)
	}
}

// --- the webhook door ----------------------------------------------------------

// ahDoor serves the API over the engine and arms the shipped trigger with a
// key, the way an owner does before pointing the app at it.
func ahDoor(t *testing.T, svc substrate.Service, ds substrate.Dataset, key string) (*httptest.Server, string) {
	t.Helper()
	for _, m := range loadDocs(t, appleHealthDir+"/triggers.yaml") {
		props := m["data"].(map[string]any)["properties"].(map[string]any)
		props["enabled"] = true
		props["source"].(map[string]any)["webhook"].(map[string]any)["key"] = key
		putDataDoc(t, ds, m)
	}
	srv := httptest.NewServer(api.New(api.Config{Service: svc}))
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/webhooks/" + ds.Repository().Authority + "/" + ahTrigger
}

// ahClient posts to the door with a deadline, so a door that hangs fails
// the one test instead of holding the package until its timeout.
var ahClient = &http.Client{Timeout: 30 * time.Second}

// postAH posts one body the way the app does: JSON, the bearer key, the
// automation headers and the account header.
func postAH(t *testing.T, url, key string, body []byte, headers map[string]any) (int, string) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	for k, v := range headers {
		s, ok := v.(string)
		if ok && (strings.HasPrefix(k, "automation-") || k == "session-id" || k == "x-substrate-account" || k == "user-agent") {
			r.Header.Set(k, s)
		}
	}
	res, err := ahClient.Do(r)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var accepted substrate.WebhookAccepted
	_ = json.NewDecoder(res.Body).Decode(&accepted)
	return res.StatusCode, accepted.Fire
}

// awaitExport waits for a fire's export receipt, failing fast on a park.
func awaitExport(t *testing.T, ds substrate.Dataset, fire string) *substrate.Record {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if row, err := ds.Get(ctx, ahExport, fire); err == nil {
			return row
		}
		parked, err := ds.TriggerFailures(ctx, ahTrigger)
		if err != nil {
			t.Fatalf("trigger failures: %v", err)
		}
		for _, f := range parked {
			if f.FireID == fire && f.Attempts > 0 && !f.Running {
				t.Fatalf("delivery %s parked: %s", fire, f.LastError)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("delivery %s wrote no export in 90 s", fire)
	return nil
}

// awaitParked waits for a fire to park and returns its failure row.
func awaitParked(t *testing.T, ds substrate.Dataset, fire string) substrate.TriggerFailure {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		parked, err := ds.TriggerFailures(context.Background(), ahTrigger)
		if err != nil {
			t.Fatalf("trigger failures: %v", err)
		}
		for _, f := range parked {
			if f.FireID == fire && f.Attempts > 0 && !f.Running {
				return f
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("delivery %s did not park", fire)
	return substrate.TriggerFailure{}
}

// The door, end to end: the key is the credential, the account header names
// the account, a body past 8 MiB is a 413 that never reaches the function,
// and a delivery that arrives before its account parks with its body and is
// written by a retry once the account exists.
func TestAppleHealthWebhookDoor(t *testing.T) {
	svc, ds := newAppleHealth(t)
	const key = "ahk_0123456789abcdef"
	_, url := ahDoor(t, svc, ds, key)
	req := ahRequest(t, "delivery_6_app_10.0.4_empty_source.json")
	headers := req["headers"].(map[string]any)
	body := []byte(ahBodyText(t, req))

	if status, _ := postAH(t, url, "ahk_not-the-key-at-all", body, headers); status != http.StatusNotFound {
		t.Fatalf("a wrong key answered %d, want 404", status)
	}
	over := bytes.Repeat([]byte(" "), 8<<20+1)
	if status, _ := postAH(t, url, key, over, headers); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body one byte past 8 MiB answered %d, want 413", status)
	}

	status, fire := postAH(t, url, key, body, headers)
	if status != http.StatusAccepted || fire == "" {
		t.Fatalf("the first delivery answered %d %q", status, fire)
	}
	failure := awaitParked(t, ds, fire)
	if !strings.Contains(failure.LastError, "no "+ahAccount+" record exists") {
		t.Fatalf("parked with %q", failure.LastError)
	}
	mustPut(t, ds, substrate.PutInput{Kind: ahAccount, ID: "iphone"})
	if _, err := ds.RetryTriggerFailure(context.Background(), ahTrigger, failure.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	ex := awaitExport(t, ds, fire)
	if !strings.Contains(fmt.Sprint(ex.Properties["userAgent"]), "10.0.4") {
		t.Fatalf("export headers = %v", ex.Properties)
	}
	// Every app header the function records on the receipt reaches it
	// through the door. The door keeps only the headers the shipped trigger
	// declares, so one the trigger stopped declaring is missing here.
	for name, prop := range map[string]string{
		"automation-name": "automationName", "automation-id": "automationId",
		"automation-aggregation": "automationAggregation", "automation-period": "automationPeriod",
		"session-id": "sessionId",
	} {
		if want := headers[name]; ex.Properties[prop] != want {
			t.Errorf("the app's %s header reached the receipt as %s = %v, want %v", name, prop, ex.Properties[prop], want)
		}
	}
	// App 10.0.4 sends merged series with `source: ""` and distance with no
	// source key at all; the mirror keeps the difference.
	steps := rowsOf(t, mustGet(t, ds, ahBatch, batchID("iphone", "step_count", "hours", "2026-10-10")))
	dist := rowsOf(t, mustGet(t, ds, ahBatch, batchID("iphone", "walking_running_distance", "hours", "2026-10-10")))
	if src, ok := steps[0]["source"]; !ok || src != "" {
		t.Fatalf("a step row's empty source became %v (present %v)", src, ok)
	}
	if _, ok := dist[0]["source"]; ok {
		t.Fatalf("a distance row grew a source: %v", dist[0])
	}
}

// A delivery just under the 8 MiB cap: a day of per-second heart rate,
// written over several pages because the function's response frame holds
// 8 MiB and the batches total more than one page's share of it.
func TestAppleHealthDeliveryNearTheCap(t *testing.T) {
	svc, ds := newAppleHealth(t, "iphone")
	const key = "ahk_0123456789abcdef"
	_, url := ahDoor(t, svc, ds, key)

	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	var buf bytes.Buffer
	buf.WriteString(`{"data":{"metrics":[{"name":"heart_rate","units":"count/min","data":[`)
	n := 0
	for ; ; n++ {
		at := start.Add(time.Duration(n) * time.Second).Format(appDate)
		row := fmt.Sprintf(`{"Min":%d,"Avg":%d.5,"Max":%d,"date":"%s","source":"%s"}`, 60+n%40, 60+n%40, 61+n%40, at, adaWatch)
		if buf.Len()+len(row)+8 >= 8<<20 {
			break
		}
		if n > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(row)
	}
	buf.WriteString(`]}]}}`)
	if buf.Len() >= 8<<20 || buf.Len() < 8<<20-1024 {
		t.Fatalf("built a %d byte body, want just under 8 MiB", buf.Len())
	}
	headers := map[string]any{"automation-aggregation": "seconds", "x-substrate-account": "iphone"}
	status, fire := postAH(t, url, key, buf.Bytes(), headers)
	if status != http.StatusAccepted {
		t.Fatalf("a body just under 8 MiB answered %d", status)
	}
	ex := awaitExport(t, ds, fire)
	hours := (n - 1) / 3600
	if asFloat(t, ex.Properties["written"]) != float64(hours+1) || asFloat(t, obj(t, ex.Properties["counts"], "metricRows")) != float64(n) {
		t.Fatalf("export = %v, want %d batches from %d rows", ex.Properties, hours+1, n)
	}
	total := 0
	for _, row := range listLive(t, ds, ahBatch) {
		total += len(rowsOf(t, row))
	}
	if total != n {
		t.Fatalf("%d rows written, %d sent", total, n)
	}
}
