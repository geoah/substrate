package providertest

// The healthgooglefit sample's `observegooglefit` over the mirror rows the
// googlefit provider writes from the recordings (googlefit_test.go): every
// dataset and session row is handed to the adapter on a record envelope, the
// way its trigger delivers it, and the observed records it writes are read
// back.

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	fitObservePackage = "samples.substrate.reamde.dev/healthgooglefit"
	fitObserveFn      = fitObservePackage + "/observegooglefit"
	obsHome           = "samples.substrate.reamde.dev"
	obsObservation    = obsHome + "/observations/observation"
	obsTrace          = obsHome + "/observations/trace"
	obsSource         = obsHome + "/observations/source"
	obsMetric         = obsHome + "/observations/metric"
	healthSleep       = obsHome + "/health/sleep"
	healthWorkout     = obsHome + "/health/workout"
	fitZone           = "Europe/Athens"
)

// newFitHealth is a repository with the googlefit provider, the health sample
// and the adapter, its account synced once against the recordings with
// `cutOn` moved to today.
func newFitHealth(t *testing.T, cutOn time.Time, toggles ...string) (substrate.Dataset, *fakeFit, time.Time) {
	t.Helper()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	importVocabulary(t, ds, "healthgooglefit")
	today := utcToday()
	fake := newFakeFit(t, today, cutOn)
	googlefitAccountWith(t, ds, toggles...)
	googlefitSync(t, ds, fake)
	return ds, fake, today
}

// observe hands the adapter one mirror row on the envelope its trigger
// delivers: the row as it stands, or a delete with no record.
func observe(t *testing.T, ds substrate.Dataset, kind, id string) map[string]any {
	t.Helper()
	env := map[string]any{"change": map[string]any{"op": "update", "kind": kind, "id": id}}
	row, err := ds.Get(context.Background(), kind, id)
	if err == nil && row.DeletedAt == nil {
		env["record"] = map[string]any{
			"kind": kind, "id": id, "version": row.Version, "properties": row.Properties,
		}
	} else {
		env["change"].(map[string]any)["op"] = "delete"
		env["record"] = nil
	}
	s := newStepper(t, ds, fitObserveFn, map[string]any{})
	s.setEnvelope(env)
	_, out, _ := s.step(nil)
	s.apply()
	m, _ := out.(map[string]any)
	return m
}

// observeAll delivers every live dataset row, then every session, the order
// the sync wrote them in.
func observeAll(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	for _, kind := range []string{googlefitDatasetType, googlefitSessionType} {
		for _, row := range listLive(t, ds, kind) {
			observe(t, ds, kind, row.ID)
		}
	}
}

// versions is every observed output by kind and id, with its version.
func versions(t *testing.T, ds substrate.Dataset) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, kind := range []string{obsObservation, obsTrace, obsSource, healthSleep, healthWorkout} {
		for _, row := range listLive(t, ds, kind) {
			out[kind+"/"+row.ID] = row.Version
		}
	}
	return out
}

func withMetric(t *testing.T, ds substrate.Dataset, kind, metric string) []*substrate.Record {
	t.Helper()
	var out []*substrate.Record
	for _, row := range listLive(t, ds, kind) {
		if storedRefPath(row.Properties["metric"]) == obsMetric+"/health/"+metric {
			out = append(out, row)
		}
	}
	return out
}

// athensOffset is the offset the adapter should stamp for an instant: the
// account's zone, Europe/Athens, at that instant.
func athensOffset(t *testing.T, at time.Time) float64 {
	t.Helper()
	zone, err := time.LoadLocation(fitZone)
	if err != nil {
		t.Skipf("no zone database for %s: %v", fitZone, err)
	}
	_, secs := at.In(zone).Zone()
	return float64(secs / 60)
}

func decimalSum(t *testing.T, values []any) *big.Rat {
	t.Helper()
	sum := new(big.Rat)
	for _, v := range values {
		r, ok := new(big.Rat).SetString(fmt.Sprint(v))
		if !ok {
			t.Fatalf("value %v is not a decimal", v)
		}
		sum.Add(sum, r)
	}
	return sum
}

func parseInstant(t *testing.T, v any) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(v))
	if err != nil {
		t.Fatalf("instant %v: %v", v, err)
	}
	return at
}

// TestHealthGoogleFitWritesTracesObservationsSleepAndWorkouts: one delivery
// per mirror row turns the recordings into hourly heart rate traces with
// per-minute offsets and engine stats, hourly step amounts per device, one
// observation per weigh-in, a sleep per night with a coded amount per stage
// segment, and a workout per other session; every output names its origin
// and the account's zone decides each offset.
func TestHealthGoogleFitWritesTracesObservationsSleepAndWorkouts(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _, today := newFitHealth(t, fitCutOn, "enabledActivity", "enabledBody", "enabledHeartRate", "enabledSleep")
	observeAll(t, ds)
	day := func(recorded time.Time) time.Time { return today.Add(recorded.Sub(fitCutOn)) }

	// Heart rate: one gauge trace per UTC hour, offsets in milliseconds from
	// the hour, the engine's stats beside the points.
	hrDay := day(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	hour := hrDay.Add(6 * time.Hour)
	id := "googlefit/" + googlefitAccount + "/google-pixel-watch-2/heart_rate/" + hour.Format("2006-01-02T15")
	tr := mustGet(t, ds, obsTrace, id).Properties
	values, _ := tr["values"].([]any)
	offsets, _ := tr["offsets"].([]any)
	if tr["form"] != "gauge" || tr["unit"] != "/min" || tr["hold"] != "PT4M" || len(values) < 50 || len(offsets) != len(values) {
		t.Fatalf("the 06:00 heart rate trace = form %v unit %v hold %v, %d values, %d offsets",
			tr["form"], tr["unit"], tr["hold"], len(values), len(offsets))
	}
	for i := 1; i < len(offsets); i++ {
		gap := fitNumber(offsets[i]) - fitNumber(offsets[i-1])
		if gap < 30000 || gap > 120000 {
			t.Fatalf("offsets %v and %v are not about a minute apart", offsets[i-1], offsets[i])
		}
	}
	stats, _ := tr["stats"].(map[string]any)
	if fitNumber(stats["count"]) != float64(len(values)) || stats["min"] == nil || stats["max"] == nil {
		t.Fatalf("the trace's stats = %v, want the engine's over its %d points", stats, len(values))
	}
	if tr["utcOffsetStart"] != athensOffset(t, hour) {
		t.Fatalf("utcOffsetStart = %v, want %s's offset at %s", tr["utcOffsetStart"], fitZone, hour)
	}
	if origins, _ := tr["origins"].([]any); len(origins) != 1 ||
		storedRefPath(origins[0]) != googlefitDatasetType+"/"+fitDatasetRow(fitHeartRate, hrDay) {
		t.Fatalf("the trace's origins = %v", tr["origins"])
	}
	if n := len(withMetric(t, ds, obsTrace, "heart_rate")); n < 24 {
		t.Fatalf("%d heart rate traces, want one per recorded hour", n)
	}

	// Steps: hourly amounts, one trace per device, summing to the day.
	var stepValues []any
	devices := map[string]bool{}
	for _, row := range withMetric(t, ds, obsTrace, "steps") {
		if row.Properties["form"] != "amount" || row.Properties["unit"] != "{steps}" {
			t.Fatalf("step trace %s = form %v unit %v", row.ID, row.Properties["form"], row.Properties["unit"])
		}
		stepValues = append(stepValues, row.Properties["values"].([]any)...)
		devices[strings.Split(row.ID, "/")[2]] = true
	}
	if len(stepValues) != 184 || !devices["google-pixel-watch-2"] || !devices["google-pixel-8"] {
		t.Fatalf("%d step values from devices %v, want the 184 deltas from the watch and the phone",
			len(stepValues), sortedKeys(devices))
	}
	watch := mustGet(t, ds, obsSource, "googlefit/"+googlefitAccount+"/google-pixel-watch-2").Properties
	if watch["class"] != "watch" || watch["hardwareModel"] != "Pixel Watch 2" ||
		storedRefPath(watch["account"]) != googlefitAccountType+"/"+googlefitAccount {
		t.Fatalf("the watch's source = %v", watch)
	}

	// Weight: one observation per weigh-in, the scale or the hand entry
	// named as its source.
	weights := withMetric(t, ds, obsObservation, "body_mass")
	if len(weights) != 17 {
		t.Fatalf("%d weight observations, want the 17 recorded weigh-ins", len(weights))
	}
	first := mustGet(t, ds, obsObservation, fmt.Sprintf("googlefit/%s/body_mass/%d",
		fitDatasetRow(fitWeight, day(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))),
		1789099361000+today.Sub(fitCutOn).Milliseconds())).Properties
	value, _ := first["value"].(map[string]any)
	if first["form"] != "gauge" || fmt.Sprint(value["value"]) != "72.26" || value["unit"] != "kg" {
		t.Fatalf("the first weigh-in = %v", first)
	}
	if !strings.HasSuffix(storedRefPath(first["source"]), "/googlefit/"+googlefitAccount+"/withings-body") {
		t.Fatalf("the scale weigh-in's source = %v", first["source"])
	}
	quals, _ := first["qualifiers"].([]any)
	if len(quals) != 1 || quals[0].(map[string]any)["code"] != "timezone:"+fitZone {
		t.Fatalf("the weigh-in's qualifiers = %v, want the offset rule", quals)
	}
	at := parseInstant(t, first["at"])
	if first["utcOffset"] != athensOffset(t, at) {
		t.Fatalf("utcOffset = %v, want %s's at %s", first["utcOffset"], fitZone, at)
	}

	// Sleep: one record per night, and the stage amounts beside it add up to
	// the record's stage totals.
	sleeps := listLive(t, ds, healthSleep)
	if len(sleeps) < 28 {
		t.Fatalf("%d sleep records, want one per recorded night", len(sleeps))
	}
	stages := map[string][]*substrate.Record{}
	for _, row := range withMetric(t, ds, obsObservation, "sleep_stage") {
		stages[storedRefPath(row.Properties["origin"])] = append(stages[storedRefPath(row.Properties["origin"])], row)
	}
	var checked int
	for _, sl := range sleeps {
		origin := storedRefPath(sl.Properties["origin"])
		rows := stages[origin]
		if len(rows) == 0 {
			continue
		}
		byCode := map[string]time.Duration{}
		for _, st := range rows {
			v, _ := st.Properties["value"].(map[string]any)
			span := parseInstant(t, st.Properties["endsAt"]).Sub(parseInstant(t, st.Properties["at"]))
			mins, _ := new(big.Rat).SetString(fmt.Sprint(v["value"]))
			diff := new(big.Rat).Sub(mins, new(big.Rat).SetFrac64(span.Milliseconds(), 60000))
			if st.Properties["form"] != "amount" || v["unit"] != "min" ||
				diff.Abs(diff).Cmp(big.NewRat(1, 1000000)) > 0 {
				t.Fatalf("stage %s = %v over %s", st.ID, st.Properties, span)
			}
			byCode[fmt.Sprint(st.Properties["code"])] += span
		}
		for code, prop := range map[string]string{"light": "light", "deep": "deep", "rem": "rem", "awake": "awake"} {
			want := byCode[code]
			got, err := parseISODuration(fmt.Sprint(sl.Properties[prop]))
			if err != nil || got != want {
				t.Fatalf("sleep %s: %s = %v, the %s stage amounts add up to %s", sl.ID, prop, sl.Properties[prop], code, want)
			}
		}
		checked++
	}
	if checked < 25 {
		t.Fatalf("only %d nights had stage amounts to check", checked)
	}

	// Workouts: every other session, the Strava run among them.
	run := mustGet(t, ds, healthWorkout, "googlefit/"+googlefitAccount+"/strava-16238731866").Properties
	if run["sport"] != "running" || run["sportSource"] != "8" || run["duration"] != "PT40M9S" ||
		!strings.HasSuffix(storedRefPath(run["source"]), "/googlefit/"+googlefitAccount+"/com-strava") {
		t.Fatalf("the Strava run = %v", run)
	}
	if n := len(sleeps) + countLive(t, ds, healthWorkout); n != 59 {
		t.Fatalf("%d sleeps and workouts, want one per session (59)", n)
	}
}

// TestHealthGoogleFitRereadsConverge is the acceptance list: an identical
// re-read changes nothing, a modified weigh-in rewrites the same id with a
// higher revision, and a day whose only point is gone deletes its output.
func TestHealthGoogleFitRereadsConverge(t *testing.T) {
	requireUV(t)
	t.Parallel()
	recordedToday := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	ds, fake, today := newFitHealth(t, recordedToday, "enabledBody")
	observeAll(t, ds)

	before := versions(t, ds)
	if len(before) == 0 {
		t.Fatal("the adapter wrote nothing")
	}
	observeAll(t, ds)
	after := versions(t, ds)
	for key, v := range before {
		if after[key] != v {
			t.Fatalf("an identical re-delivery moved %s from version %d to %d", key, v, after[key])
		}
	}
	if len(after) != len(before) {
		t.Fatalf("an identical re-delivery left %d outputs, want %d", len(after), len(before))
	}

	shift := today.Sub(recordedToday)
	changedDay := fitDatasetRow(fitWeight, today.AddDate(0, 0, -1))
	evening := fmt.Sprintf("googlefit/%s/body_mass/%d", changedDay, 1791052863000+shift.Milliseconds())
	old := mustGet(t, ds, obsObservation, evening)
	emptiedDay := fitDatasetRow(fitWeight, today)
	emptied := listLive(t, ds, obsObservation)
	var gone []string
	for _, row := range emptied {
		if storedRefPath(row.Properties["origin"]) == googlefitDatasetType+"/"+emptiedDay {
			gone = append(gone, row.ID)
		}
	}
	if len(gone) != 1 {
		t.Fatalf("%d observations from today's weigh-in, want 1", len(gone))
	}

	reread := readFitJSON(t, "testdata/googlefit/weight_reread.json")
	fake.setPoints(fitWeight, reread["point"].([]any))
	fake.dropPoints(fitWeight, recordedToday)
	googlefitSync(t, ds, fake)
	observe(t, ds, googlefitDatasetType, changedDay)
	observe(t, ds, googlefitDatasetType, emptiedDay)

	now := mustGet(t, ds, obsObservation, evening)
	value, _ := now.Properties["value"].(map[string]any)
	if fmt.Sprint(value["value"]) != "71.76" || fitNumber(now.Properties["revision"]) <= fitNumber(old.Properties["revision"]) {
		t.Fatalf("the re-read weigh-in = %v at revision %v (was %v)", value, now.Properties["revision"], old.Properties["revision"])
	}
	for _, id := range gone {
		if fitLive(t, ds, obsObservation, id) {
			t.Fatalf("%s survived its day coming back empty", id)
		}
	}
	for key, v := range before {
		kind, id, _ := strings.Cut(key, "/observations/observation/")
		if id == "" || kind == "" || id == evening || strings.Contains(id, emptiedDay) {
			continue
		}
		if got := mustGet(t, ds, obsObservation, id).Version; got != v {
			t.Fatalf("%s, an unchanged weigh-in, moved from version %d to %d", id, v, got)
		}
	}
}

// TestHealthGoogleFitTriggerDeliversMirrorRows: the shipped trigger, not a
// hand-built envelope, delivers the sync's writes to the adapter, deletes
// included, and nothing parks.
func TestHealthGoogleFitTriggerDeliversMirrorRows(t *testing.T) {
	requireUV(t)
	t.Parallel()
	recordedToday := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	ds, fake, today := newFitHealth(t, recordedToday, "enabledBody")
	assertTriggers(t, ds, "healthgooglefit-on-mirror")
	drainTriggers(t, ds)
	if n := len(withMetric(t, ds, obsObservation, "body_mass")); n != 13 {
		t.Fatalf("%d weight observations after the trigger drained, want the 13 weigh-ins from the recorded 2026-09-04 on", n)
	}

	fake.dropPoints(fitWeight, recordedToday)
	googlefitSync(t, ds, fake)
	drainTriggers(t, ds)
	for _, row := range withMetric(t, ds, obsObservation, "body_mass") {
		if strings.Contains(row.ID, fitDatasetRow(fitWeight, today)) {
			t.Fatalf("%s survived the delete of its day", row.ID)
		}
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}
}

// parseISODuration reads the PT#H#M#S durations the adapter writes.
func parseISODuration(s string) (time.Duration, error) {
	rest, ok := strings.CutPrefix(s, "PT")
	if !ok {
		return 0, fmt.Errorf("%q is not a PT duration", s)
	}
	return time.ParseDuration(strings.ToLower(rest))
}
