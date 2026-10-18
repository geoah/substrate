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
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

const healthGarminFn = samples.Authority + "/healthgarmin/adaptgarmin"

// newHealthGarminDataset installs the garmin provider and imports the
// adapter, which brings the observations and health samples with it.
func newHealthGarminDataset(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newCoreDataset(t)
	install(t, ds, garminDir, nil)
	importVocabulary(t, ds, "healthgarmin")
	mustPut(t, ds, substrate.PutInput{Kind: garminAccountKind, ID: garminAccountID, Properties: map[string]any{}})
	return ds
}

// garminAdapt runs the adapter once on one mirror row as its record trigger
// delivers it, applying the effects, and answers the output.
func garminAdapt(t *testing.T, ds substrate.Dataset, kind, id string) map[string]any {
	t.Helper()
	return adaptRow(t, ds, healthGarminFn, kind, id)
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

func quantity(v any) string {
	m, _ := v.(map[string]any)
	return fmt.Sprint(m["value"], " ", m["unit"])
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
	out := adapterOutputs(t, ds)
	acct := "garmin/" + garminAccountID

	// Heart rate: the 2026-09-12 daily starts 2026-09-11T21:00Z and holds a
	// sample every 15 minutes, but only two in its 20:00 local hour
	// (2026-09-12T17Z), so that hour is written with offsets, and the engine
	// computes the stats.
	for key, row := range out {
		if !strings.HasPrefix(key, obsTrace+"/"+acct+"/account/heart_rate/") {
			continue
		}
		if storedRefPath(row.Properties["metric"]) != healthMetrics+"heart_rate" || row.Properties["form"] != "gauge" ||
			row.Properties["unit"] != "/min" || row.Properties["hold"] != "PT4M" {
			t.Fatalf("%s: %v", key, row.Properties)
		}
	}
	irregular := out[obsTrace+"/"+acct+"/account/heart_rate/2026-09-12T17"]
	if irregular == nil {
		t.Fatal("no heart rate trace for the 2026-09-12T17Z hour")
	}
	stats, _ := irregular.Properties["stats"].(map[string]any)
	values, _ := irregular.Properties["values"].([]any)
	offsets, _ := irregular.Properties["offsets"].([]any)
	if len(values) != 2 || len(offsets) != 2 || stats == nil || asInt(t, stats["count"]) != 2 {
		t.Errorf("trace %s: %d values at %d offsets, stats %v; want the hour's two samples", irregular.ID, len(values), len(offsets), stats)
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
	if p["form"] != "summary" || p["statistic"] != "sum" || p["period"] != "day" ||
		p["periodId"] != daily["calendarDate"] || quantity(p["value"]) != fmt.Sprint(daily["steps"], " {steps}") ||
		asInt(t, p["utcOffset"]) != 180 || sum.EndsAt == nil {
		t.Errorf("daily steps summary %v, want a sum over the day", p)
	}
	// The five day totals are sums, so a summaries read's sum takes them;
	// resting heart rate is Garmin's own figure, vendor.
	for _, role := range []string{"distance", "floors", "active_energy", "basal_energy"} {
		if row := out[obsObservation+"/"+acct+"/"+daily["summaryId"].(string)+"/"+role]; row == nil || row.Properties["statistic"] != "sum" {
			t.Errorf("daily %s summary %v, want statistic sum", role, row)
		}
	}
	if row := out[obsObservation+"/"+acct+"/"+daily["summaryId"].(string)+"/resting_heart_rate"]; row == nil || row.Properties["statistic"] != "vendor" {
		t.Errorf("daily resting heart rate %v, want statistic vendor", row)
	}
	dayStart := time.Unix(int64(daily["startTimeInSeconds"].(float64)), 0).UTC()
	read, err := ds.Aggregate(t.Context(), substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: healthMetrics + "steps"}, From: substrate.AggregateFromSummaries,
		Range:    &substrate.AggregateRange{Gte: dayStart.Format(time.RFC3339), Lt: dayStart.Add(24 * time.Hour).Format(time.RFC3339)},
		Reducers: []substrate.AggregateReducer{{Name: substrate.ReducerSum}},
	}})
	if err != nil {
		t.Fatalf("the summaries read of the day's steps: %v", err)
	}
	if len(read.Rows) != 1 || read.Rows[0].Values[0].Value == nil || *read.Rows[0].Values[0].Value != fmt.Sprint(daily["steps"]) {
		t.Errorf("the summaries read's sum of the day's steps is %+v, want Garmin's total %v", read.Rows, daily["steps"])
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
		if row.Properties["form"] != "amount" || storedRefPath(row.Properties["metric"]) != healthMetrics+"sleep_stage" {
			t.Errorf("stage %s: %v", key, row.Properties)
		}
		// Minutes, the unit every adapter writes a stage in.
		v, _ := row.Properties["value"].(map[string]any)
		minutes, ok := new(big.Rat).SetString(fmt.Sprint(v["value"]))
		span := row.EndsAt.Sub(*row.At)
		if !ok || v["unit"] != "min" || new(big.Rat).Sub(new(big.Rat).Mul(minutes, big.NewRat(60, 1)), big.NewRat(int64(span.Seconds()), 1)).Abs(new(big.Rat)).Cmp(big.NewRat(1, 1000000)) > 0 {
			t.Errorf("stage %s value %v over %s, want its minutes", key, v, span)
		}
		summed[row.Properties["code"].(string)] += int64(span.Seconds())
		// The night is ENHANCED_TENTATIVE: Garmin may still revise it.
		if row.Properties["quality"] != "estimated" || row.Properties["recordingMethod"] != "automatic" {
			t.Errorf("stage %s quality %v recorded %v, want a tentative night's estimated and automatic",
				key, row.Properties["quality"], row.Properties["recordingMethod"])
		}
	}
	if stages != segments || !maps.Equal(summed, seconds) {
		t.Errorf("%d stage rows summing %v, want %d segments summing %v", stages, summed, segments, seconds)
	}
	// Efficiency is time asleep over time in bed, where Garmin sends both.
	nightRow := out[healthSleep+"/"+acct+"/"+sid+"/sleep"].Properties
	asleep, err1 := vocabulary.ParseISODuration(fmt.Sprint(nightRow["timeAsleep"]))
	inBed, err2 := vocabulary.ParseISODuration(fmt.Sprint(nightRow["timeInBed"]))
	eff, _ := nightRow["efficiency"].(map[string]any)
	wantEff := new(big.Rat).SetFrac64(int64(asleep.Seconds())*100, int64(inBed.Seconds())).FloatString(2)
	if err1 != nil || err2 != nil || eff == nil || eff["value"] != wantEff || eff["unit"] != "%" {
		t.Errorf("the night's efficiency is %v, want %s %% (%v asleep of %v in bed)", nightRow["efficiency"], wantEff, asleep, inBed)
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
	// The night and the padel session are titled on their own clock: padel
	// has no sport of its own, so it is a workout.
	for row, want := range map[*substrate.Record]string{
		out[healthSleep+"/"+acct+"/"+sid+"/sleep"]: "Night of Tue 6 Oct",
		w: "Workout, Fri 11 Sep 19:05",
	} {
		if row.Properties["name"] != want {
			t.Errorf("%s is named %v, want %q", row.ID, row.Properties["name"], want)
		}
	}
	// The source is named for the product, not Garmin's deviceName token.
	if src := mustGet(t, ds, obsSource, acct+"/forerunner265"); src.Properties["class"] != "watch" ||
		src.Properties["name"] != "Garmin Forerunner 265" || src.Properties["hardwareModel"] != "forerunner265" ||
		storedRefPath(src.Properties["account"]) != garminAccountKind+"/"+garminAccountID {
		t.Errorf("device source %v", src.Properties)
	}

	// What Garmin pushes without naming a device (dailies, sleeps, weigh-ins)
	// is the Garmin Connect account's, a service rather than a watch.
	if src := mustGet(t, ds, obsSource, acct+"/account"); src.Properties["class"] != "service" ||
		src.Properties["name"] != "Garmin Connect" {
		t.Errorf("account source %v, want Garmin Connect, a service", src.Properties)
	}
	assertCatalogued(t, ds)

	// Garmin's final version of the night clears the tentative marker.
	pushGarmin(t, ds, cfg, "webhook-final", loadGarminFixture(t, "push_sleeps_2026-10-07_final.json").request)
	adaptAll(t, ds, "sleep")
	for key, row := range adapterOutputs(t, ds) {
		if strings.HasPrefix(key, obsObservation+"/"+acct+"/"+sid+"/stage/") && row.Properties["quality"] != nil {
			t.Errorf("stage %s of the final night keeps quality %v", key, row.Properties["quality"])
		}
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
	before := adapterOutputs(t, ds)
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
	for k, r := range adapterOutputs(t, ds) {
		if b := before[k]; b == nil || b.Version != r.Version {
			t.Errorf("an identical re-send moved %s", k)
		}
	}

	// Fuller: the full day under the same summaryId.
	pushGarmin(t, ds, cfg, "webhook-3", loadGarminFixture(t, "push_dailies_2026-10-07_resend.json").request)
	garminAdapt(t, ds, garminPackage+"/daily", mirror)
	second := mine(adapterOutputs(t, ds))
	if len(second) <= len(first) {
		t.Errorf("the full day wrote %d rows, the partial %d", len(second), len(first))
	}
	for k, r := range first {
		now := second[k]
		if now == nil {
			t.Errorf("%s is gone after the fuller re-send", k)
			continue
		}
		if asInt(t, now.Properties["revision"]) <= asInt(t, r.Properties["revision"]) {
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
		asInt(t, now.Properties["revision"]) <= asInt(t, was.Properties["revision"]) ||
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
		for k := range adapterOutputs(t, ds) {
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

// TestHealthGarminAdapterUpgradeRewritesUnchangedMirrors: every output names
// the adapter version that wrote it, and an upgrade of the adapter alone,
// replayed over mirrors Garmin did not change, rewrites each output at the
// revision it holds (decision record 0162).
func TestHealthGarminAdapterUpgradeRewritesUnchangedMirrors(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	for i, name := range []string{"push_dailies.json", "push_epochs.json", "push_sleeps.json", "push_activities.json"} {
		pushGarmin(t, ds, cfg, fmt.Sprintf("webhook-%d", i), loadGarminFixture(t, name).request)
	}
	adaptAll(t, ds, "daily", "epoch", "sleep", "activity")
	before := adapterOutputs(t, ds)
	assertAdapterStamp(t, before, healthGarminAdapter)
	next := upgradeAdapter(t, ds, healthGarminAdapter)
	replayAdapter(t, ds, "healthgarmin-on-mirror")
	assertAdapterRewrite(t, ds, next, before)
}

// garminEditedRun pushes the London run and its edit in Garmin Connect (the
// same summaryId, the distance corrected to 5000 m), adapts both, and answers
// the workout's id: the workout stands on the edit.
func garminEditedRun(t *testing.T) (substrate.Dataset, string) {
	t.Helper()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	pushGarmin(t, ds, cfg, "webhook-1", loadGarminFixture(t, "push_activities.json").request)
	pushGarmin(t, ds, cfg, "webhook-2", loadGarminFixture(t, "push_manuallyUpdatedActivities.json").request)
	garminAdapt(t, ds, garminActivityKind, garminRunRow)
	garminAdapt(t, ds, garminEditKind, garminRunRow)
	wid := "garmin/" + garminAccountID + "/" + garminRun + "/workout"
	assertWorkout(t, ds, wid, "5000 m", garminEditKind)
	return ds, wid
}

const (
	garminRun          = "21004834320"
	garminRunRow       = garminAccountID + "/" + garminRun
	garminActivityKind = garminPackage + "/activity"
	garminEditKind     = garminPackage + "/manuallyupdatedactivity"
)

// assertWorkout holds the run's workout to a distance and to the mirror kind
// its origin names.
func assertWorkout(t *testing.T, ds substrate.Dataset, wid, distance, originKind string) *substrate.Record {
	t.Helper()
	w, err := ds.Get(t.Context(), healthWorkout, wid)
	if err != nil || w.DeletedAt != nil {
		t.Fatalf("the workout is gone (%v), want it built from %s", err, originKind)
	}
	if got := quantity(w.Properties["distance"]); got != distance || storedRefPath(w.Properties["origin"]) != originKind+"/"+garminRunRow {
		t.Fatalf("the workout is %s from %v, want %s from %s", got, w.Properties["origin"], distance, originKind)
	}
	return w
}

func deleteMirror(t *testing.T, ds substrate.Dataset, kind string) {
	t.Helper()
	if _, err := ds.Delete(t.Context(), substrate.ActorAPI, kind, garminRunRow, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete %s/%s: %v", kind, garminRunRow, err)
	}
}

func workoutGone(t *testing.T, ds substrate.Dataset, wid string) bool {
	t.Helper()
	return !live(t, ds, healthWorkout, wid)
}

// TestHealthGarminReadsADeletedActivityAsAbsent: a deleted mirror row is
// absent to the adapter, whichever half of an edited activity it is and
// whichever delivery arrives. Deleting the edit falls back to the activity,
// deleting the activity keeps the edit, deleting both deletes the workout
// even when only one delete is delivered, and a replayed delete delivery
// changes nothing.
func TestHealthGarminReadsADeletedActivityAsAbsent(t *testing.T) {
	requireUV(t)
	t.Parallel()
	t.Run("the edit deleted", func(t *testing.T) {
		t.Parallel()
		ds, wid := garminEditedRun(t)
		deleteMirror(t, ds, garminEditKind)
		garminAdapt(t, ds, garminEditKind, garminRunRow)
		w := assertWorkout(t, ds, wid, "5278.69 m", garminActivityKind)
		garminAdapt(t, ds, garminEditKind, garminRunRow)
		if again := assertWorkout(t, ds, wid, "5278.69 m", garminActivityKind); again.Version != w.Version {
			t.Fatalf("a replayed delete moved the workout from version %d to %d", w.Version, again.Version)
		}
	})
	t.Run("the activity deleted, then the edit", func(t *testing.T) {
		t.Parallel()
		ds, wid := garminEditedRun(t)
		deleteMirror(t, ds, garminActivityKind)
		garminAdapt(t, ds, garminActivityKind, garminRunRow)
		w := assertWorkout(t, ds, wid, "5000 m", garminEditKind)
		garminAdapt(t, ds, garminActivityKind, garminRunRow)
		if again := assertWorkout(t, ds, wid, "5000 m", garminEditKind); again.Version != w.Version {
			t.Fatalf("a replayed delete moved the workout from version %d to %d", w.Version, again.Version)
		}
		deleteMirror(t, ds, garminEditKind)
		garminAdapt(t, ds, garminEditKind, garminRunRow)
		if !workoutGone(t, ds, wid) {
			t.Fatal("the workout outlived both halves of its activity")
		}
		garminAdapt(t, ds, garminEditKind, garminRunRow)
		garminAdapt(t, ds, garminActivityKind, garminRunRow)
		if !workoutGone(t, ds, wid) {
			t.Fatal("a replayed delete brought the workout back")
		}
	})
	t.Run("both deleted, one delete delivered", func(t *testing.T) {
		t.Parallel()
		ds, wid := garminEditedRun(t)
		deleteMirror(t, ds, garminEditKind)
		deleteMirror(t, ds, garminActivityKind)
		// The workout names the edit as its origin; the activity's delete
		// alone must still find and delete it.
		garminAdapt(t, ds, garminActivityKind, garminRunRow)
		if !workoutGone(t, ds, wid) {
			t.Fatal("the activity's delete left the workout its edit built")
		}
		garminAdapt(t, ds, garminEditKind, garminRunRow)
		if !workoutGone(t, ds, wid) {
			t.Fatal("the edit's delete brought the workout back")
		}
	})
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
	for k, r := range adapterOutputs(t, ds) {
		if strings.HasPrefix(k, prefix) {
			traces = append(traces, r)
		}
	}
	if len(traces) != 2 {
		t.Fatalf("%d step traces for the hour, want 2 split at the offset change", len(traces))
	}
	slices.SortFunc(traces, func(a, b *substrate.Record) int { return a.At.Compare(*b.At) })
	half := time.Unix(hour+1800, 0).UTC()
	if asInt(t, traces[0].Properties["utcOffset"]) != 180 || asInt(t, traces[1].Properties["utcOffset"]) != 60 ||
		!traces[1].At.Equal(half) || !traces[0].EndsAt.Equal(half) {
		t.Errorf("split traces %s (%v..%v, %v) and %s (%v..%v, %v)",
			traces[0].ID, traces[0].At, traces[0].EndsAt, traces[0].Properties["utcOffset"],
			traces[1].ID, traces[1].At, traces[1].EndsAt, traces[1].Properties["utcOffset"])
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
	for _, r := range adapterOutputs(t, ds) {
		byMetric[strings.TrimPrefix(storedRefPath(r.Properties["metric"]), healthMetrics)]++
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
	for k := range adapterOutputs(t, ds) {
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
	assertCatalogued(t, ds)

	acct := "garmin/" + garminAccountID
	// The night's HRV and skin temperature cover the sleep, the period word
	// every adapter uses for one.
	for _, row := range listLive(t, ds, obsObservation) {
		metric := strings.TrimPrefix(storedRefPath(row.Properties["metric"]), healthMetrics)
		if (metric == "hrv_rmssd" || metric == "skin_temperature_deviation") && row.Properties["form"] == "summary" &&
			row.Properties["period"] != "sleep" {
			t.Errorf("%s %s covers period %v, want sleep", metric, row.ID, row.Properties["period"])
		}
	}

	// A blood pressure's two numbers carry each other as components, and the
	// cuff's sourceType says how it was taken.
	for _, bp := range loadGarminFixture(t, "push_bloodPressures.json").body["bloodPressures"] {
		method := map[string]string{"DEVICE": "automatic", "MANUAL": "manual"}[bp["sourceType"].(string)]
		for _, metric := range []string{"systolic_blood_pressure", "diastolic_blood_pressure"} {
			row := mustGet(t, ds, obsObservation, acct+"/"+bp["summaryId"].(string)+"/"+metric).Properties
			parts, _ := row["components"].([]any)
			var named []string
			for _, part := range parts {
				named = append(named, strings.TrimPrefix(storedRefPath(part.(map[string]any)["metric"]), healthMetrics))
			}
			if strings.Join(named, " ") != "systolic_blood_pressure diastolic_blood_pressure" || row["recordingMethod"] != method {
				t.Errorf("%s of %s has components %v recorded %v, want both numbers and %s", metric, bp["summaryId"], named, row["recordingMethod"], method)
			}
		}
	}

	// VO2 max is a level Garmin dates, never times: a gauge at the local
	// midnight of its calendarDate, at that day's offset. With no daily for
	// the date the offset is unknown, so the reading is written at the UTC
	// midnight with none, and the daily's arrival moves it.
	vo2 := acct + "/x4b2c9e1-6aa46bd0/vo2_max"
	before := mustGet(t, ds, obsObservation, vo2)
	if before.Properties["form"] != "gauge" || before.Properties["utcOffset"] != nil ||
		!before.At.Equal(time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("VO2 max before its daily: %s at %v offset %v, want a gauge at 2026-09-12T00:00Z with no offset",
			before.Properties["form"], before.At, before.Properties["utcOffset"])
	}
	pushGarmin(t, ds, cfg, "webhook-dailies", loadGarminFixture(t, "push_dailies.json").request)
	for _, d := range listLive(t, ds, garminPackage+"/daily") {
		if d.Properties["calendarDate"] == "2026-09-12" {
			garminAdapt(t, ds, d.Kind, d.ID)
		}
	}
	after := mustGet(t, ds, obsObservation, vo2)
	if after.Properties["form"] != "gauge" || asInt(t, after.Properties["utcOffset"]) != 180 ||
		!after.At.Equal(time.Date(2026, 9, 11, 21, 0, 0, 0, time.UTC)) || quantity(after.Properties["value"]) != "49 mL/(kg.min)" {
		t.Errorf("VO2 max after its daily: %v at %v, want 49 at the local midnight 2026-09-11T21:00Z, +180", after.Properties, after.At)
	}
}

// TestHealthGarminWritesAnActivitysSamples: an activity's detail samples are
// traces on the source of the device that recorded it: heart rate, speed,
// step cadence and the distance each sample adds, which sums to Garmin's own
// distance, so a reader builds splits and a pace curve from stored rows. A
// ride's cadence is pedal cadence, a metric of its own. An activity naming
// no device gets a source of its own, never the account's, so its samples
// never land in the stream the daily's heart rate fills.
func TestHealthGarminWritesAnActivitysSamples(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	f := loadGarminFixture(t, "push_activityDetails_21004995534.json")
	pushGarmin(t, ds, cfg, "webhook-run", f.request)
	// The same samples as a ride a day later, from a device Garmin did not name.
	raw, err := json.Marshal(f.body["activityDetails"][0])
	if err != nil {
		t.Fatal(err)
	}
	var ride map[string]any
	if err := json.Unmarshal(raw, &ride); err != nil {
		t.Fatal(err)
	}
	ride["summaryId"], ride["activityId"] = "21004995599-detail", 21004995599
	summary := ride["summary"].(map[string]any)
	summary["deviceName"], summary["activityType"] = "unknown", "ROAD_BIKING"
	summary["startTimeInSeconds"] = summary["startTimeInSeconds"].(float64) + 86400
	for _, s := range ride["samples"].([]any) {
		sample := s.(map[string]any)
		sample["startTimeInSeconds"] = sample["startTimeInSeconds"].(float64) + 86400
		if spm, ok := sample["stepsPerMinute"]; ok {
			sample["bikeCadenceInRPM"] = spm.(float64) / 2
			delete(sample, "stepsPerMinute")
		}
	}
	pushGarmin(t, ds, cfg, "webhook-ride", f.withBody(t, map[string]any{"activityDetails": []any{ride}}))
	acts := loadGarminFixture(t, "push_activities.json")
	unnamed := acts.body["activities"][0]
	unnamed["deviceName"] = "unknown"
	pushGarmin(t, ds, cfg, "webhook-unnamed", acts.withBody(t, map[string]any{"activities": []any{unnamed}}))
	adaptAll(t, ds, "activitydetail", "activity")

	acct := "garmin/" + garminAccountID
	traces := map[string][]*substrate.Record{}
	for _, row := range listLive(t, ds, obsTrace) {
		parts := strings.Split(row.ID, "/")
		traces[parts[2]+"/"+parts[3]] = append(traces[parts[2]+"/"+parts[3]], row)
	}
	// The run's distance, sample by sample, adds up to the last cumulative
	// total Garmin sent.
	samples := f.body["activityDetails"][0]["samples"].([]any)
	total := fmt.Sprint(samples[len(samples)-1].(map[string]any)["totalDistanceInMeters"])
	sum := new(big.Rat)
	for _, row := range traces["forerunner265/distance"] {
		if row.Properties["form"] != "amount" || row.Properties["unit"] != "m" {
			t.Errorf("distance trace %s is %v in %v, want an amount in m", row.ID, row.Properties["form"], row.Properties["unit"])
		}
		for _, v := range row.Properties["values"].([]any) {
			if v == nil {
				continue
			}
			r, ok := new(big.Rat).SetString(fmt.Sprint(v))
			if !ok {
				t.Fatalf("distance value %v", v)
			}
			sum.Add(sum, r)
		}
	}
	if want, _ := new(big.Rat).SetString(total); want == nil || sum.Cmp(want) != 0 {
		t.Errorf("the run's distance traces sum to %s m, want Garmin's %s", sum.FloatString(2), total)
	}
	for _, key := range []string{
		"forerunner265/heart_rate", "forerunner265/speed", "forerunner265/cadence",
		"unknown-device/heart_rate", "unknown-device/speed", "unknown-device/cycling_cadence", "unknown-device/distance",
	} {
		if len(traces[key]) == 0 {
			t.Errorf("no %s traces: %v", key, slices.Sorted(maps.Keys(traces)))
		}
	}
	for _, key := range []string{"unknown-device/cadence", "forerunner265/cycling_cadence"} {
		if len(traces[key]) != 0 {
			t.Errorf("%d %s traces, want none", len(traces[key]), key)
		}
	}
	for key := range traces {
		if strings.HasPrefix(key, "account/") {
			t.Errorf("an activity wrote %s traces on the account's source", key)
		}
	}
	assertCatalogued(t, ds)
	device := mustGet(t, ds, obsSource, acct+"/unknown-device").Properties
	if device["class"] != nil || storedRefPath(device["account"]) != garminAccountKind+"/"+garminAccountID {
		t.Errorf("the unnamed device's source is %v", device)
	}
	w := mustGet(t, ds, healthWorkout, acct+"/"+unnamed["summaryId"].(string)+"/workout").Properties
	if storedRefPath(w["source"]) != obsSource+"/"+acct+"/unknown-device" {
		t.Errorf("the unnamed device's workout is on %v", w["source"])
	}
}

// TestHealthGarminRebuildsAnHourWhenASliceGoes: an hour's step trace is
// built from every epoch slice in it, so deleting one slice's mirror row
// rebuilds the hour without it, and deleting the last deletes the trace.
func TestHealthGarminRebuildsAnHourWhenASliceGoes(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	f := loadGarminFixture(t, "push_epochs.json")
	first := int64(f.body["epochs"][0]["startTimeInSeconds"].(float64))
	hour := first - first%3600
	var inHour []map[string]any
	for _, e := range f.body["epochs"] {
		if st := int64(e["startTimeInSeconds"].(float64)); st >= hour && st < hour+3600 {
			inHour = append(inHour, e)
		}
	}
	pushGarmin(t, ds, cfg, "webhook-1", f.withBody(t, map[string]any{"epochs": inHour}))
	adaptAll(t, ds, "epoch")
	id := "garmin/" + garminAccountID + "/account/steps/" + time.Unix(hour, 0).UTC().Format("2006-01-02T15")
	rows := listLive(t, ds, garminPackage+"/epoch")
	if len(rows) < 2 {
		t.Fatalf("%d epoch rows in the hour, want at least two", len(rows))
	}
	before := len(mustGet(t, ds, obsTrace, id).Properties["origins"].([]any))
	for i, row := range rows {
		if _, err := ds.Delete(t.Context(), substrate.ActorAPI, row.Kind, row.ID, substrate.DeleteInput{}); err != nil {
			t.Fatal(err)
		}
		garminAdapt(t, ds, row.Kind, row.ID)
		if i == len(rows)-1 {
			break
		}
		left := mustGet(t, ds, obsTrace, id).Properties
		if n := len(left["origins"].([]any)); n != before-i-1 {
			t.Fatalf("after deleting %d slices the hour's trace names %d origins, want %d", i+1, n, before-i-1)
		}
	}
	if live(t, ds, obsTrace, id) {
		t.Fatal("the hour's trace outlived every slice it was built from")
	}
}

// TestHealthGarminKeepsAnHoursRevisionWhenASliceArrivesAfterADelete: an
// hour's trace keeps the revision of a slice Garmin re-sent after that slice
// is deleted, and a new slice in the hour is delivered without the trace
// holding it. Its delivery reads the trace by id and writes it at the stored
// revision; written at the remaining slices' lower one, the write path would
// refuse it and the delivery would park.
func TestHealthGarminKeepsAnHoursRevisionWhenASliceArrivesAfterADelete(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newHealthGarminDataset(t)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	f := loadGarminFixture(t, "push_epochs.json")
	first := int64(f.body["epochs"][0]["startTimeInSeconds"].(float64))
	hour := first - first%3600
	var inHour []map[string]any
	for _, e := range f.body["epochs"] {
		if st := int64(e["startTimeInSeconds"].(float64)); st >= hour && st < hour+3600 {
			inHour = append(inHour, e)
		}
	}
	if len(inHour) < 3 {
		t.Fatalf("%d slices in the first hour, want at least three", len(inHour))
	}
	late := inHour[len(inHour)-1]
	pushGarmin(t, ds, cfg, "webhook-1", f.withBody(t, map[string]any{"epochs": inHour[:len(inHour)-1]}))
	adaptAll(t, ds, "epoch")
	slice := func(summaryID any) *substrate.Record {
		for _, row := range listLive(t, ds, garminPackage+"/epoch") {
			if row.Properties["summaryId"] == summaryID {
				return row
			}
		}
		t.Fatalf("no epoch row for %v", summaryID)
		return nil
	}
	id := "garmin/" + garminAccountID + "/account/steps/" + time.Unix(hour, 0).UTC().Format("2006-01-02T15")

	resent := maps.Clone(inHour[0])
	resent["steps"] = 999
	pushGarmin(t, ds, cfg, "webhook-2", f.withBody(t, map[string]any{"epochs": []map[string]any{resent}}))
	raised := slice(resent["summaryId"])
	if raised.Version < 2 {
		t.Fatalf("the re-sent slice is at version %d, want it raised", raised.Version)
	}
	garminAdapt(t, ds, raised.Kind, raised.ID)
	if _, err := ds.Delete(t.Context(), substrate.ActorAPI, raised.Kind, raised.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	garminAdapt(t, ds, raised.Kind, raised.ID)
	if rev := asInt(t, mustGet(t, ds, obsTrace, id).Properties["revision"]); rev != raised.Version {
		t.Fatalf("after the delete the hour's trace is at revision %d, want the deleted slice's %d", rev, raised.Version)
	}

	pushGarmin(t, ds, cfg, "webhook-3", f.withBody(t, map[string]any{"epochs": []map[string]any{late}}))
	arrived := slice(late["summaryId"])
	if arrived.Version >= raised.Version {
		t.Fatalf("the new slice is at version %d, want it below %d", arrived.Version, raised.Version)
	}
	garminAdapt(t, ds, arrived.Kind, arrived.ID)
	tr := mustGet(t, ds, obsTrace, id).Properties
	if rev := asInt(t, tr["revision"]); rev != raised.Version {
		t.Fatalf("the hour's trace is at revision %d, want the stored %d kept", rev, raised.Version)
	}
	if origins, _ := tr["origins"].([]any); len(origins) != len(inHour)-1 {
		t.Fatalf("the hour's trace names %d origins, want the %d live slices", len(origins), len(inHour)-1)
	}
}
