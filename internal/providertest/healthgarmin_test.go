package providertest

// The healthgarmin sample's adapter over the rows the garmin provider's push
// writes. Each case pushes fixture deliveries through garminpush (the phase
// the garmin suite proves), then hands the adapter each mirror row the way
// its record trigger would: one stepped invocation per row, with the row as
// the envelope's record.

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/samples"
)

const (
	healthGarminFn     = samples.Authority + "/healthgarmin/adaptgarmin"
	garminHealthMetric = samples.Authority + "/observations/metric/health/"
)

// newHealthGarminDataset installs the garmin provider and imports the
// adapter, which brings the observations and health samples with it.
func newHealthGarminDataset(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newCoreDataset(t)
	install(t, ds, garminDir, nil)
	importVocabulary(t, ds, "healthgarmin")
	mustPut(t, ds, substrate.PutInput{Kind: garminAccountType, ID: garminAccountID, Properties: map[string]any{}})
	return ds
}

// garminAdapt runs the adapter once on one mirror row as its record trigger
// delivers it, applying the effects, and answers the output.
func garminAdapt(t *testing.T, ds substrate.Dataset, kind, id string) map[string]any {
	t.Helper()
	env := map[string]any{
		"change":     map[string]any{"op": "update", "kind": kind, "id": id},
		"repository": map[string]any{"authority": ds.Repository().Authority},
	}
	if row, err := ds.Get(t.Context(), kind, id); err == nil && row.DeletedAt == nil {
		env["record"] = map[string]any{"id": row.ID, "kind": row.Kind, "version": row.Version, "properties": row.Properties}
	} else {
		env["change"].(map[string]any)["op"] = "delete"
		env["record"] = nil
	}
	s := newStepper(t, ds, healthGarminFn, nil)
	s.setEnvelope(env)
	_, out, _ := s.step(nil)
	s.apply()
	o, _ := out.(map[string]any)
	return o
}

// adaptAll runs the adapter on every live row of the named mirror kinds.
func adaptAll(t *testing.T, ds substrate.Dataset, names ...string) {
	t.Helper()
	for _, name := range names {
		for _, row := range listLive(t, ds, garminPackage+"/"+name) {
			garminAdapt(t, ds, row.Kind, row.ID)
		}
	}
}

// outputs is every adapter output, keyed "<kind>/<id>", with its version.
func outputs(t *testing.T, ds substrate.Dataset) map[string]*substrate.Record {
	t.Helper()
	out := map[string]*substrate.Record{}
	for _, kind := range []string{obsTrace, obsObservation, healthSleep, healthWorkout} {
		for _, row := range listLive(t, ds, kind) {
			out[kind+"/"+row.ID] = row
		}
	}
	return out
}

func quantity(v any) string {
	m, _ := v.(map[string]any)
	return fmt.Sprint(m["value"], " ", m["unit"])
}

func garminAsInt(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			t.Fatalf("%v is not an integer", v)
		}
		return i
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	t.Fatalf("%v (%T) is not a number", v, v)
	return 0
}

// TestHealthGarminWritesTheProposalsRows: the proposal's table for Garmin
// (docs/plans/observations/proposal/02b-garmin.md) out of one push of each
// summary type: an hourly heart rate trace with irregular offsets and the
// engine's stats, hourly step amounts summed across an epoch slice's activity
// types, Garmin's daily total as a summary observation, a night with one coded
// amount per stage segment, and a workout.
func TestHealthGarminWritesTheProposalsRows(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	for i, name := range []string{
		"push_dailies.json", "push_epochs.json", "push_sleeps.json",
		"push_activities.json", "push_bodyComps.json",
	} {
		pushGarmin(t, ds, cfg, fmt.Sprintf("webhook-%d", i), loadGarminFixture(t, name).request)
	}
	adaptAll(t, ds, "daily", "epoch", "sleep", "activity", "bodycomp")
	out := outputs(t, ds)
	acct := "garmin/" + garminAccountID

	// Heart rate: the 2026-09-11 daily starts 21:00Z; its 19:00 local hour has
	// gaps in Garmin's map, so it is written with offsets, and the engine
	// computes the stats.
	var irregular *substrate.Record
	for key, row := range out {
		if !strings.HasPrefix(key, obsTrace+"/"+acct+"/account/heart_rate/") {
			continue
		}
		if storedRefPath(row.Properties["metric"]) != garminHealthMetric+"heart_rate" || row.Properties["form"] != "gauge" ||
			row.Properties["unit"] != "/min" || row.Properties["hold"] != "PT4M" {
			t.Fatalf("%s: %v", key, row.Properties)
		}
		if _, ok := row.Properties["offsets"]; ok && irregular == nil {
			irregular = row
		}
	}
	if irregular == nil {
		t.Fatal("no heart rate trace carries offsets")
	}
	stats, _ := irregular.Properties["stats"].(map[string]any)
	values, _ := irregular.Properties["values"].([]any)
	if stats == nil || garminAsInt(t, stats["count"]) != int64(len(values)) {
		t.Errorf("trace %s: stats %v for %d values", irregular.ID, stats, len(values))
	}
	if refs := refIDs(irregular, "origins"); len(refs) != 1 || !strings.HasPrefix(refs[0], garminAccountID+"/") {
		t.Errorf("trace %s origins %v, want its daily", irregular.ID, refs)
	}

	// Steps: the hour whose slices hold the most epochs (Garmin splits a
	// slice by activity type), summed per slice.
	epochs := loadGarminFixture(t, "push_epochs.json").body["epochs"]
	perHour := map[int64]int{}
	for _, e := range epochs {
		st := int64(e["startTimeInSeconds"].(float64))
		perHour[st-st%3600]++
	}
	var hour int64
	for h, n := range perHour {
		if n > perHour[hour] || (n == perHour[hour] && h < hour) {
			hour = h
		}
	}
	if perHour[hour] <= 4 {
		t.Fatalf("no hour of the fixture holds a slice of two activity types: %v", perHour)
	}
	perSlice := map[int64]float64{}
	for _, e := range epochs {
		st := int64(e["startTimeInSeconds"].(float64))
		if st >= hour && st < hour+3600 {
			perSlice[st] += e["steps"].(float64)
		}
	}
	label := time.Unix(hour, 0).UTC().Format("2006-01-02T15")
	steps := out[obsTrace+"/"+acct+"/account/steps/"+label]
	if steps == nil {
		t.Fatalf("no steps trace %s/account/steps/%s", acct, label)
	}
	if steps.Properties["form"] != "amount" || steps.Properties["unit"] != "{steps}" {
		t.Errorf("steps trace %v", steps.Properties)
	}
	var want []string
	for _, st := range slices.Sorted(maps.Keys(perSlice)) {
		want = append(want, fmt.Sprint(perSlice[st]))
	}
	var got []string
	for _, v := range steps.Properties["values"].([]any) {
		got = append(got, fmt.Sprint(v))
	}
	if !slices.Equal(got, want) {
		t.Errorf("steps per slice %v, want the epochs summed per slice %v", got, want)
	}
	if n := len(refIDs(steps, "origins")); n < len(perSlice) {
		t.Errorf("steps trace names %d origins for %d slices", n, len(perSlice))
	}

	// Garmin's daily total.
	daily := loadGarminFixture(t, "push_dailies.json").body["dailies"][0]
	sum := out[obsObservation+"/"+acct+"/"+daily["summaryId"].(string)+"/steps"]
	if sum == nil {
		t.Fatal("no daily steps summary")
	}
	p := sum.Properties
	if p["form"] != "summary" || p["statistic"] != "vendor" || p["period"] != "day" ||
		p["periodId"] != daily["calendarDate"] || quantity(p["value"]) != fmt.Sprint(daily["steps"], " {steps}") ||
		garminAsInt(t, p["utcOffset"]) != 180 || sum.EndsAt == nil {
		t.Errorf("daily steps summary %v", p)
	}

	// A night: the sleep record and one coded amount per stage segment.
	sleeps := loadGarminFixture(t, "push_sleeps.json").body["sleeps"]
	night := sleeps[len(sleeps)-1]
	sid := night["summaryId"].(string)
	if out[healthSleep+"/"+acct+"/"+sid+"/sleep"] == nil {
		t.Fatalf("no health/sleep for %s", sid)
	}
	levels := night["sleepLevelsMap"].(map[string]any)
	segments, seconds := 0, map[string]int64{}
	for _, stage := range []string{"awake", "light", "deep", "rem"} {
		for _, r := range levels[stage].([]any) {
			rr := r.(map[string]any)
			segments++
			seconds[stage] += int64(rr["endTimeInSeconds"].(float64) - rr["startTimeInSeconds"].(float64))
		}
	}
	stages, summed := 0, map[string]int64{}
	for key, row := range out {
		if !strings.HasPrefix(key, obsObservation+"/"+acct+"/"+sid+"/stage/") {
			continue
		}
		stages++
		if row.Properties["form"] != "amount" || storedRefPath(row.Properties["metric"]) != garminHealthMetric+"sleep_stage" {
			t.Errorf("stage %s: %v", key, row.Properties)
		}
		v, _ := row.Properties["value"].(map[string]any)
		var secs int64
		if _, err := fmt.Sscan(fmt.Sprint(v["value"]), &secs); err != nil || v["unit"] != "s" {
			t.Errorf("stage %s value %v", key, v)
		}
		summed[row.Properties["code"].(string)] += secs
	}
	if stages != segments || !maps.Equal(summed, seconds) {
		t.Errorf("%d stage rows summing %v, want %d segments summing %v", stages, summed, segments, seconds)
	}

	// A workout, from the activity, on its device's source.
	act := loadGarminFixture(t, "push_activities.json").body["activities"][0]
	w := out[healthWorkout+"/"+acct+"/"+act["summaryId"].(string)+"/workout"]
	if w == nil {
		t.Fatal("no workout")
	}
	if w.Properties["sportSource"] != act["activityType"] || storedRefPath(w.Properties["source"]) != obsSource+"/"+acct+"/forerunner265" {
		t.Errorf("workout %v", w.Properties)
	}
	if src := mustGet(t, ds, obsSource, acct+"/forerunner265"); src.Properties["class"] != "watch" ||
		storedRefPath(src.Properties["account"]) != garminAccountType+"/"+garminAccountID {
		t.Errorf("device source %v", src.Properties)
	}

	// A weigh-in in Garmin's grams lands in the catalogue's kilograms.
	bc := loadGarminFixture(t, "push_bodyComps.json").body["bodyComps"][0]
	mass := out[obsObservation+"/"+acct+"/"+bc["summaryId"].(string)+"/body_mass"]
	if mass == nil || quantity(mass.Properties["value"]) != "72.26 kg" {
		t.Errorf("body mass %v", mass)
	}
}

// TestHealthGarminReSendsReconcile: an identical re-send changes nothing; a
// fuller re-send of the same summaryId rewrites the same ids at a higher
// revision; an edit in Garmin Connect replaces the workout; and a deleted
// mirror row takes what it produced with it.
func TestHealthGarminReSendsReconcile(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	dailies := loadGarminFixture(t, "push_dailies.json")
	pushGarmin(t, ds, cfg, "webhook-1", dailies.request)
	const sid = "x4b2c9e1-6ac56150"
	mirror := garminAccountID + "/" + sid
	garminAdapt(t, ds, garminPackage+"/daily", mirror)
	before := outputs(t, ds)
	mine := func(all map[string]*substrate.Record) map[string]*substrate.Record {
		out := map[string]*substrate.Record{}
		for k, r := range all {
			if strings.Contains(k, sid) || slices.ContainsFunc(refIDs(r, "origins"), func(o string) bool { return o == mirror }) ||
				slices.Contains(refIDs(r, "origin"), mirror) {
				out[k] = r
			}
		}
		return out
	}
	first := mine(before)
	if len(first) == 0 {
		t.Fatal("the partial daily produced nothing")
	}

	// Identical: the same delivery again, and the same row adapted again.
	pushGarmin(t, ds, cfg, "webhook-2", dailies.request)
	garminAdapt(t, ds, garminPackage+"/daily", mirror)
	for k, r := range outputs(t, ds) {
		if b := before[k]; b == nil || b.Version != r.Version {
			t.Errorf("an identical re-send moved %s", k)
		}
	}

	// Fuller: the full day under the same summaryId.
	pushGarmin(t, ds, cfg, "webhook-3", loadGarminFixture(t, "push_dailies_2026-10-07_resend.json").request)
	garminAdapt(t, ds, garminPackage+"/daily", mirror)
	second := mine(outputs(t, ds))
	if len(second) <= len(first) {
		t.Errorf("the full day wrote %d rows, the partial %d", len(second), len(first))
	}
	for k, r := range first {
		now := second[k]
		if now == nil {
			t.Errorf("%s is gone after the fuller re-send", k)
			continue
		}
		if garminAsInt(t, now.Properties["revision"]) <= garminAsInt(t, r.Properties["revision"]) {
			t.Errorf("%s: revision %v -> %v, want it higher", k, r.Properties["revision"], now.Properties["revision"])
		}
	}

	// An edit: the London run, renamed and corrected, replaces its workout.
	pushGarmin(t, ds, cfg, "webhook-4", loadGarminFixture(t, "push_activities.json").request)
	const run = "21004834320"
	garminAdapt(t, ds, garminPackage+"/activity", garminAccountID+"/"+run)
	wid := "garmin/" + garminAccountID + "/" + run + "/workout"
	was := mustGet(t, ds, healthWorkout, wid)
	pushGarmin(t, ds, cfg, "webhook-5", loadGarminFixture(t, "push_manuallyUpdatedActivities.json").request)
	garminAdapt(t, ds, garminPackage+"/manuallyupdatedactivity", garminAccountID+"/"+run)
	now := mustGet(t, ds, healthWorkout, wid)
	if quantity(now.Properties["distance"]) != "5000 m" ||
		garminAsInt(t, now.Properties["revision"]) <= garminAsInt(t, was.Properties["revision"]) ||
		!strings.HasSuffix(storedRefPath(now.Properties["origin"]), "/manuallyupdatedactivity/"+garminAccountID+"/"+run) {
		t.Errorf("the edit did not replace the workout: %v -> %v", was.Properties, now.Properties)
	}

	// A deleted mirror row takes its outputs with it.
	pushGarmin(t, ds, cfg, "webhook-6", loadGarminFixture(t, "push_bloodPressures.json").request)
	bp := listLive(t, ds, garminPackage+"/bloodpressure")[0]
	garminAdapt(t, ds, bp.Kind, bp.ID)
	prefix := obsObservation + "/garmin/" + bp.ID + "/"
	count := func() int {
		n := 0
		for k := range outputs(t, ds) {
			if strings.HasPrefix(k, prefix) {
				n++
			}
		}
		return n
	}
	if count() != 3 {
		t.Fatalf("a blood pressure wrote %d observations, want systolic, diastolic and pulse", count())
	}
	if _, err := ds.Delete(t.Context(), substrate.ActorAPI, bp.Kind, bp.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	garminAdapt(t, ds, bp.Kind, bp.ID)
	if n := count(); n != 0 {
		t.Errorf("%d observations outlived their deleted mirror row", n)
	}
}

// TestHealthGarminSplitsTracesAtAnOffsetChange: an hour whose epochs change
// their UTC offset half way (a flight, a daylight saving change) is two step
// traces, each with one offset, the second named for its own start.
func TestHealthGarminSplitsTracesAtAnOffsetChange(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	f := loadGarminFixture(t, "push_epochs.json")
	epochs := f.body["epochs"]
	first := int64(epochs[0]["startTimeInSeconds"].(float64))
	hour := first - first%3600
	var hourEpochs []map[string]any
	for _, e := range epochs {
		st := int64(e["startTimeInSeconds"].(float64))
		if st < hour || st >= hour+3600 {
			continue
		}
		if st >= hour+1800 {
			e["startTimeOffsetInSeconds"] = 3600
		}
		hourEpochs = append(hourEpochs, e)
	}
	pushGarmin(t, ds, cfg, "webhook-1", f.withBody(t, map[string]any{"epochs": hourEpochs}))
	adaptAll(t, ds, "epoch")

	prefix := obsTrace + "/garmin/" + garminAccountID + "/account/steps/"
	var traces []*substrate.Record
	for k, r := range outputs(t, ds) {
		if strings.HasPrefix(k, prefix) {
			traces = append(traces, r)
		}
	}
	if len(traces) != 2 {
		t.Fatalf("%d step traces for the hour, want 2 split at the offset change", len(traces))
	}
	slices.SortFunc(traces, func(a, b *substrate.Record) int { return a.At.Compare(*b.At) })
	half := time.Unix(hour+1800, 0).UTC()
	if garminAsInt(t, traces[0].Properties["utcOffsetStart"]) != 180 || garminAsInt(t, traces[1].Properties["utcOffsetStart"]) != 60 ||
		!traces[1].At.Equal(half) || !traces[0].EndsAt.Equal(half) {
		t.Errorf("split traces %s (%v..%v, %v) and %s (%v..%v, %v)",
			traces[0].ID, traces[0].At, traces[0].EndsAt, traces[0].Properties["utcOffsetStart"],
			traces[1].ID, traces[1].At, traces[1].EndsAt, traces[1].Properties["utcOffsetStart"])
	}
	if !strings.HasSuffix(traces[1].ID, half.Format("2006-01-02T15:04")) {
		t.Errorf("the second part is named %s, want its own start %s", traces[1].ID, half.Format("2006-01-02T15:04"))
	}
}

// TestHealthGarminCoversEveryMirrorKind: every mirror kind that implies an
// observation produces one through the adapter without a refusal, and the
// skipped fields are counted in the output rather than dropped silently.
func TestHealthGarminCoversEveryMirrorKind(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	for i, name := range []string{
		"push_stressDetails.json", "push_userMetrics.json", "push_pulseox.json",
		"push_allDayRespiration.json", "push_healthSnapshot.json", "push_hrv.json",
		"push_bloodPressures.json", "push_skinTemp.json", "push_activityDetails_21004995534.json",
	} {
		pushGarmin(t, ds, cfg, fmt.Sprintf("webhook-%d", i), loadGarminFixture(t, name).request)
	}
	byMetric := map[string]int{}
	skipped := map[string]bool{}
	for _, name := range []string{
		"stressdetail", "usermetric", "pulseox", "respiration",
		"healthsnapshot", "hrv", "bloodpressure", "skintemp", "activitydetail",
	} {
		for _, row := range listLive(t, ds, garminPackage+"/"+name) {
			out := garminAdapt(t, ds, row.Kind, row.ID)
			if s, ok := out["skipped"].(map[string]any); ok {
				for k := range s {
					skipped[k] = true
				}
			}
		}
	}
	for _, r := range outputs(t, ds) {
		byMetric[strings.TrimPrefix(storedRefPath(r.Properties["metric"]), garminHealthMetric)]++
	}
	for _, metric := range []string{
		"garmin_stress", "garmin_body_battery", "vo2_max", "spo2",
		"respiratory_rate", "hrv_rmssd", "systolic_blood_pressure", "diastolic_blood_pressure",
		"skin_temperature_deviation", "heart_rate", "speed", "cadence",
	} {
		if byMetric[metric] == 0 {
			t.Errorf("no %s row: %v", metric, byMetric)
		}
	}
	for _, field := range []string{"activitydetail.latitudeInDegree", "healthsnapshot.sdrr_hrv", "usermetric.fitnessAge"} {
		if !skipped[field] {
			t.Errorf("%s was not counted as skipped: %v", field, skipped)
		}
	}
	// Respiration is a quarter hour a row and an hour a trace: one trace per
	// UTC hour the slices fall in (the fixture's day has one offset).
	resp := 0
	for k := range outputs(t, ds) {
		if strings.Contains(k, "/account/respiratory_rate/") && !strings.Contains(k, "/snapshot/") {
			resp++
		}
	}
	hours := map[int64]bool{}
	for _, e := range loadGarminFixture(t, "push_allDayRespiration.json").body["allDayRespiration"] {
		st := int64(e["startTimeInSeconds"].(float64))
		hours[st-st%3600] = true
	}
	if resp != len(hours) {
		t.Errorf("%d respiration traces, want one for each of the %d hours the slices fall in", resp, len(hours))
	}
}
