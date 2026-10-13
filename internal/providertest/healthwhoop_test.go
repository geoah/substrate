package providertest

// The `healthwhoop` sample's adapter over the WHOOP provider's mirror, driven
// through the dispatcher: the provider's sync (whoop_test.go's stepper and
// fake, over testdata/whoop) writes the mirror rows, and the sample's record
// trigger delivers each one to `adaptwhoop`, which writes the health records
// docs/plans/observations/proposal/02a-whoop.md tables.

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/samples"
)

const (
	healthObservation = "samples.substrate.reamde.dev/observations/observation"
	healthSource      = "samples.substrate.reamde.dev/observations/source"
	healthMetric      = "samples.substrate.reamde.dev/observations/metric"
	healthWhoopRun    = "healthwhoop-on-mirror"

	// The proposal's worked example: one cycle, its recovery and its sleep.
	whoopExampleCycle = "1047392"
	whoopExampleSleep = "e3b0c442-98fc-4c14-9afb-1c1e0a7d4b9a"
	// Two cycles that start on one local date, 2026-09-19 at +03:00.
	whoopEarlyCycle = "1046422"
	whoopLateCycle  = "1046463"
	// The night WHOOP could not score.
	whoopUnscoredCycle = "1046319"
	// A run whose heart rate the worked example reads.
	whoopExampleWorkout = "37a41337-1dee-49f3-99fb-cf5cfb59acb3"
	// The adapter version the shipped closure stamps.
	healthWhoopAdapter = "healthwhoop@2"
)

// healthOutputs are the kinds the adapter writes.
var healthOutputs = []string{healthObservation, healthSleep, healthWorkout, healthSource}

// newHealthWhoop is newWhoop with the health samples and the adapter imported
// before anything syncs, so every mirror row the sync writes is delivered.
func newHealthWhoop(t *testing.T) (substrate.Dataset, *fakeWhoop) {
	t.Helper()
	_, ds, fake := newWhoop(t)
	importVocabulary(t, ds, "healthwhoop")
	return ds, fake
}

// healthDrain runs the trigger chain to quiescence and fails on any parked
// delivery: a refused write parks the whole delivery.
func healthDrain(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	drainTriggers(t, ds)
	if n := parkedFailures(t, ds); n != 0 {
		statuses, _ := ds.TriggerStatuses(context.Background())
		for _, st := range statuses {
			if st.Parked > 0 {
				t.Errorf("%s parked %d: %s", st.ID, st.Parked, st.LastParkedError)
			}
		}
		t.Fatalf("%d deliveries parked", n)
	}
}

// hw is an output id the adapter composes: whoop/<account>/<parts>.
func hw(parts ...string) string {
	return "whoop/" + whoopAccount + "/" + strings.Join(parts, "/")
}

func healthProps(t *testing.T, ds substrate.Dataset, kind, id string) map[string]any {
	t.Helper()
	return mustGet(t, ds, kind, id).Properties
}

// healthVersions is the version of every live adapter output, by kind and id.
func healthVersions(t *testing.T, ds substrate.Dataset) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, kind := range healthOutputs {
		for _, row := range listLive(t, ds, kind) {
			out[kind+"/"+row.ID] = row.Version
		}
	}
	return out
}

// quantityOf reads a quantity property as its digits and unit.
func quantityOf(t *testing.T, props map[string]any, name string) (string, string) {
	t.Helper()
	q, ok := props[name].(map[string]any)
	if !ok {
		t.Fatalf("%s is %v, want a quantity", name, props[name])
	}
	value, _ := q["value"].(string)
	unit, _ := q["unit"].(string)
	return value, unit
}

func sameInstant(t *testing.T, got any, want string) bool {
	t.Helper()
	a, err1 := time.Parse(time.RFC3339Nano, fmt.Sprint(got))
	b, err2 := time.Parse(time.RFC3339Nano, want)
	return err1 == nil && err2 == nil && a.Equal(b)
}

func epochSeconds(t *testing.T, instant string) int64 {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, instant)
	if err != nil {
		t.Fatal(err)
	}
	return ts.Unix()
}

// metricOf is the metric id an observation names.
func metricOf(props map[string]any) string {
	return strings.TrimPrefix(storedRefPath(props["metric"]), healthMetric+"/")
}

// asInt reads a stored integer however JSON decoded it.
func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return math.MinInt64
}

// TestHealthWhoopWritesTheProposalsTable runs the thirty days through the
// sync and the adapter and checks the worked example of 02a-whoop.md field by
// field, the counts per metric, a month of sleeps with their stage totals,
// two cycles on one date, the current cycle and the unscored one.
func TestHealthWhoopWritesTheProposalsTable(t *testing.T) {
	ds, fake := newHealthWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	healthDrain(t, ds)

	recoveryOrigin := whoopRecoveryType + "/" + whoopRow("recovery", whoopExampleCycle)
	cycleStart, cycleEnd := "2026-10-06T19:41:00.862Z", "2026-10-07T19:38:00.174Z"
	// The later of the recovery's and the cycle's updated_at.
	recoveryRevision := epochSeconds(t, "2026-10-07T19:58:00.853Z")
	for _, c := range []struct{ role, metric, value, unit string }{
		{"recovery", "health/whoop_recovery", "67", "%"},
		{"hrv", "health/hrv_rmssd", "61.3", "ms"},
		{"resting_heart_rate", "health/resting_heart_rate", "52", "/min"},
		{"spo2", "health/spo2", "96.5", "%"},
		{"skin_temperature", "health/skin_temperature", "33.8", "Cel"},
	} {
		p := healthProps(t, ds, healthObservation, hw(whoopExampleCycle, c.role))
		if v, u := quantityOf(t, p, "value"); v != c.value || u != c.unit {
			t.Errorf("%s = %s %s, want %s %s", c.role, v, u, c.value, c.unit)
		}
		if metricOf(p) != c.metric || p["form"] != "summary" || p["statistic"] != "vendor" ||
			p["period"] != "cycle" || p["periodId"] != whoopExampleCycle {
			t.Errorf("%s is %v %v/%v/%v period %v, want %s summary vendor cycle %s",
				c.role, metricOf(p), p["form"], p["statistic"], p["period"], p["periodId"], c.metric, whoopExampleCycle)
		}
		if !sameInstant(t, p["at"], cycleStart) || !sameInstant(t, p["endsAt"], cycleEnd) {
			t.Errorf("%s spans %v to %v, want the cycle %s to %s", c.role, p["at"], p["endsAt"], cycleStart, cycleEnd)
		}
		if asInt(p["utcOffset"]) != 180 || asInt(p["revision"]) != recoveryRevision {
			t.Errorf("%s utcOffset %v revision %v, want 180 and %d", c.role, p["utcOffset"], p["revision"], recoveryRevision)
		}
		if storedRefPath(p["origin"]) != recoveryOrigin {
			t.Errorf("%s origin %v, want %s", c.role, p["origin"], recoveryOrigin)
		}
		if storedRefPath(p["source"]) != healthSource+"/whoop/"+whoopAccount {
			t.Errorf("%s source %v", c.role, p["source"])
		}
	}
	strain := healthProps(t, ds, healthObservation, hw(whoopExampleCycle, "strain"))
	if v, u := quantityOf(t, strain, "value"); v != "14.2" || u != "{score}" || strain["form"] != "summary" ||
		storedRefPath(strain["origin"]) != whoopCycleType+"/"+whoopRow("cycle", whoopExampleCycle) {
		t.Errorf("strain = %s %s %v from %v", v, u, strain["form"], strain["origin"])
	}
	energy := healthProps(t, ds, healthObservation, hw(whoopExampleCycle, "energy"))
	if v, u := quantityOf(t, energy, "value"); v != "8125.4" || u != "kJ" || energy["form"] != "amount" ||
		energy["statistic"] != nil || metricOf(energy) != "health/energy_expended" {
		t.Errorf("energy = %s %s %v %v %s, want 8125.4 kJ, an amount of energy_expended", v, u, energy["form"], energy["statistic"], metricOf(energy))
	}

	// Heart rate: the cycle's average and maximum over the cycle, and a run's
	// over the run on the `workout` channel, each with the statistic it is.
	cycleOrigin := whoopCycleType + "/" + whoopRow("cycle", whoopExampleCycle)
	cycleRevision := epochSeconds(t, "2026-10-07T19:58:00.853Z")
	workoutOrigin := whoopWorkoutType + "/" + whoopRow("workout", whoopExampleWorkout)
	runStart, runEnd := "2026-10-10T04:08:05.615Z", "2026-10-10T04:48:18.355Z"
	runRevision := epochSeconds(t, "2026-10-10T05:04:18.468Z")
	for _, c := range []struct {
		id, value, statistic, period, periodID, channel, start, end, origin string
		revision                                                            int64
	}{
		{hw(whoopExampleCycle, "heart_rate_avg"), "74", "avg", "cycle", whoopExampleCycle, "", cycleStart, cycleEnd, cycleOrigin, cycleRevision},
		{hw(whoopExampleCycle, "heart_rate_max"), "171", "max", "cycle", whoopExampleCycle, "", cycleStart, cycleEnd, cycleOrigin, cycleRevision},
		{hw(whoopExampleWorkout, "heart_rate_avg"), "155", "avg", "workout", whoopExampleWorkout, "workout", runStart, runEnd, workoutOrigin, runRevision},
		{hw(whoopExampleWorkout, "heart_rate_max"), "180", "max", "workout", whoopExampleWorkout, "workout", runStart, runEnd, workoutOrigin, runRevision},
	} {
		p := healthProps(t, ds, healthObservation, c.id)
		if v, u := quantityOf(t, p, "value"); v != c.value || u != "/min" {
			t.Errorf("%s = %s %s, want %s /min", c.id, v, u, c.value)
		}
		if metricOf(p) != "health/heart_rate" || p["form"] != "summary" || p["statistic"] != c.statistic ||
			p["period"] != c.period || p["periodId"] != c.periodID {
			t.Errorf("%s is %s %v/%v/%v period %v, want health/heart_rate summary %s %s %s",
				c.id, metricOf(p), p["form"], p["statistic"], p["period"], p["periodId"], c.statistic, c.period, c.periodID)
		}
		if got, _ := p["channel"].(string); got != c.channel {
			t.Errorf("%s is on channel %q, want %q", c.id, got, c.channel)
		}
		if !sameInstant(t, p["at"], c.start) || !sameInstant(t, p["endsAt"], c.end) {
			t.Errorf("%s spans %v to %v, want %s to %s", c.id, p["at"], p["endsAt"], c.start, c.end)
		}
		if asInt(p["utcOffset"]) != 180 || asInt(p["revision"]) != c.revision || storedRefPath(p["origin"]) != c.origin {
			t.Errorf("%s utcOffset %v revision %v origin %v, want 180, %d and %s",
				c.id, p["utcOffset"], p["revision"], p["origin"], c.revision, c.origin)
		}
		if storedRefPath(p["source"]) != healthSource+"/whoop/"+whoopAccount || p["adapter"] != healthWhoopAdapter {
			t.Errorf("%s source %v adapter %v", c.id, p["source"], p["adapter"])
		}
	}
	// One stream per kind of span: the cycles' on no channel, the workouts'
	// on `workout`, so a day's average never mixes the two.
	channels := map[string]int{}
	for _, row := range listLive(t, ds, healthObservation) {
		if metricOf(row.Properties) == "health/heart_rate" {
			ch, _ := row.Properties["channel"].(string)
			channels[ch+"/"+fmt.Sprint(row.Properties["statistic"])]++
		}
	}
	if want := map[string]int{"/avg": 29, "/max": 29, "workout/avg": 29, "workout/max": 29}; fmt.Sprint(channels) != fmt.Sprint(want) {
		t.Errorf("heart rate summaries by channel and statistic %v, want %v", channels, want)
	}

	// The sleep: the typed record, its four stage totals and two scores.
	night := healthProps(t, ds, healthSleep, hw(whoopExampleSleep, "sleep"))
	for name, want := range map[string]string{
		"timeInBed": "PT8H14M49S", "timeAsleep": "PT7H54M49S", "awake": "PT20M",
		"light": "PT4H56M", "deep": "PT1H38M30S", "rem": "PT1H20M19S",
	} {
		if fmt.Sprint(night[name]) != want {
			t.Errorf("sleep %s = %v, want %s", name, night[name], want)
		}
	}
	if v, u := quantityOf(t, night, "efficiency"); v != "95.9580078" || u != "%" {
		t.Errorf("sleep efficiency = %s %s", v, u)
	}
	if scores, _ := night["scores"].(map[string]any); fmt.Sprint(scores["sleepPerformance"]) != "100" ||
		fmt.Sprint(scores["sleepConsistency"]) != "71" {
		t.Errorf("sleep scores = %v", night["scores"])
	}
	for code, ms := range map[string]float64{"awake": 1200000, "light": 17760000, "deep": 5910000, "rem": 4819000} {
		p := healthProps(t, ds, healthObservation, hw(whoopExampleSleep, "stage", code))
		v, u := quantityOf(t, p, "value")
		minutes, err := strconv.ParseFloat(v, 64)
		if err != nil || u != "min" || math.Abs(minutes-ms/60000) > 1e-9 {
			t.Errorf("stage %s = %s %s, want %v min", code, v, u, ms/60000)
		}
		if p["code"] != code || p["form"] != "amount" || metricOf(p) != "health/sleep_stage" ||
			p["periodId"] != whoopExampleSleep || !sameInstant(t, p["at"], "2026-10-06T19:41:00.271Z") {
			t.Errorf("stage %s is %v", code, p)
		}
	}
	for role, metric := range map[string]string{
		"sleep_performance": "health/whoop_sleep_performance", "respiratory_rate": "health/respiratory_rate",
	} {
		p := healthProps(t, ds, healthObservation, hw(whoopExampleSleep, role))
		if metricOf(p) != metric || p["form"] != "summary" || p["period"] != "sleep" {
			t.Errorf("%s is %v", role, p)
		}
	}

	// Counts per metric: 29 scored recoveries and cycles, 32 scored sleeps.
	byMetric := map[string]int{}
	for _, row := range listLive(t, ds, healthObservation) {
		byMetric[metricOf(row.Properties)]++
	}
	for metric, want := range map[string]int{
		"health/whoop_recovery": 29, "health/hrv_rmssd": 29, "health/resting_heart_rate": 29,
		"health/spo2": 29, "health/skin_temperature": 29, "health/whoop_strain": 29,
		"health/energy_expended": 29, "health/sleep_stage": 4 * 32,
		"health/whoop_sleep_performance": 32, "health/respiratory_rate": 32,
		// An average and a maximum per scored cycle and per scored workout.
		"health/heart_rate": 2*29 + 2*29,
	} {
		if byMetric[metric] != want {
			t.Errorf("%d observations of %s, want %d", byMetric[metric], metric, want)
		}
	}
	if len(byMetric) != 11 {
		t.Errorf("observations of %d metrics, want 11: %v", len(byMetric), byMetric)
	}
	if n := countLive(t, ds, healthWorkout); n != 29 {
		t.Errorf("%d workouts, want 29", n)
	}
	// One sport code set for every source: WHOOP's weightlifting is the
	// strength other adapters write, and a name with no value of its own is
	// other; WHOOP's word stays in sportSource either way.
	sports := map[string]string{}
	for _, row := range listLive(t, ds, healthWorkout) {
		sports[fmt.Sprint(row.Properties["sportSource"])] = fmt.Sprint(row.Properties["sport"])
	}
	if want := map[string]string{
		"running": "running", "cycling": "cycling", "walking": "walking",
		"weightlifting": "strength", "padel": "other", "activity": "other",
	}; !maps.Equal(sports, want) {
		t.Errorf("workout sports by WHOOP's name %v, want %v", sports, want)
	}
	if n := countLive(t, ds, healthSource); n != 1 {
		t.Errorf("%d sources, want the one WHOOP", n)
	}

	// The month of sleeps: thirty nights, every scored one with its four
	// stage totals beside it.
	var nights, staged int
	for _, row := range listLive(t, ds, healthSleep) {
		if row.Properties["isNap"] == true {
			continue
		}
		nights++
		if row.Properties["rem"] == nil {
			continue
		}
		staged++
		sid := strings.TrimSuffix(strings.TrimPrefix(row.ID, hw()), "/sleep")
		for _, code := range []string{"awake", "light", "deep", "rem"} {
			if !whoopLive(t, ds, healthObservation, hw(sid, "stage", code)) {
				t.Errorf("night %s has no %s total", sid, code)
			}
		}
	}
	if nights != 30 || staged != 29 {
		t.Errorf("%d nights, %d with stages; want 30 and 29", nights, staged)
	}

	// Two cycles on one local date stay two, told apart by periodId.
	days := map[string]string{}
	for _, cycle := range []string{whoopEarlyCycle, whoopLateCycle} {
		p := healthProps(t, ds, healthObservation, hw(cycle, "strain"))
		if p["periodId"] != cycle {
			t.Errorf("strain of %s carries periodId %v", cycle, p["periodId"])
		}
		at, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(p["at"]))
		days[cycle] = at.Add(time.Duration(asInt(p["utcOffset"])) * time.Minute).Format(time.DateOnly)
	}
	if days[whoopEarlyCycle] != "2026-09-19" || days[whoopLateCycle] != "2026-09-19" {
		t.Errorf("the two cycles start on local days %v, want both 2026-09-19", days)
	}

	// The current cycle has no end: its span ends at its updatedAt.
	current := healthProps(t, ds, healthObservation, hw(whoopCurrentCycle, "strain"))
	if !sameInstant(t, current["endsAt"], "2026-10-10T14:42:00.093Z") {
		t.Errorf("the current cycle's strain ends %v, want its updatedAt", current["endsAt"])
	}
	// The unscored night implies a sleep and nothing else.
	for _, role := range []string{"recovery", "strain", "energy"} {
		if whoopLive(t, ds, healthObservation, hw(whoopUnscoredCycle, role)) {
			t.Errorf("the unscorable cycle has a %s observation", role)
		}
	}
}

// TestHealthWhoopRerunChangesNothing: replaying the trigger over every mirror
// change the sync made, and an identical re-sync, move no output.
func TestHealthWhoopRerunChangesNothing(t *testing.T) {
	ds, fake := newHealthWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	healthDrain(t, ds)
	before := healthVersions(t, ds)
	// 511 observations (116 of them heart rate), 33 sleeps, 29 workouts and
	// the one source.
	if len(before) != 574 {
		t.Fatalf("%d outputs after the first run, want 574", len(before))
	}

	whoopSync(t, ds, fake, whoopSteady(nil))
	healthDrain(t, ds)
	if err := ds.ReplayTrigger(context.Background(), healthWhoopRun, 0); err != nil {
		t.Fatalf("replay %s: %v", healthWhoopRun, err)
	}
	healthDrain(t, ds)

	after := healthVersions(t, ds)
	for k, v := range after {
		if before[k] != v {
			t.Errorf("%s moved from version %d to %d", k, before[k], v)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("%d outputs after the rerun, want %d", len(after), len(before))
	}
}

// TestHealthWhoopFollowsARescoreAndADeletion: the second sync rescores cycle
// 1047510 and loses a nap and a workout. The rescored cycle's outputs are
// rewritten at the same ids with a higher revision; the lost rows' outputs
// are deleted; the hand-logged workout that replaced one arrives.
func TestHealthWhoopFollowsARescoreAndADeletion(t *testing.T) {
	ds, fake := newHealthWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	healthDrain(t, ds)
	strainID := hw(whoopRescoredCycle, "strain")
	before := mustGet(t, ds, healthObservation, strainID)
	napOutputs := []string{
		hw(whoopDeletedNap, "stage", "awake"), hw(whoopDeletedNap, "stage", "light"),
		hw(whoopDeletedNap, "stage", "deep"), hw(whoopDeletedNap, "stage", "rem"),
		hw(whoopDeletedNap, "sleep_performance"), hw(whoopDeletedNap, "respiratory_rate"),
	}
	for _, id := range napOutputs {
		if !whoopLive(t, ds, healthObservation, id) {
			t.Fatalf("the nap's %s was never written", id)
		}
	}
	if !whoopLive(t, ds, healthWorkout, hw(whoopDeletedWorkout, "workout")) {
		t.Fatal("the auto-detected workout was never written")
	}
	workoutHR := []string{hw(whoopDeletedWorkout, "heart_rate_avg"), hw(whoopDeletedWorkout, "heart_rate_max")}
	for _, id := range workoutHR {
		if !whoopLive(t, ds, healthObservation, id) {
			t.Fatalf("the auto-detected workout's %s was never written", id)
		}
	}
	hrBefore := mustGet(t, ds, healthObservation, hw(whoopRescoredCycle, "heart_rate_max"))
	recoveryBefore := mustGet(t, ds, healthObservation, hw(whoopRescoredCycle, "recovery"))

	fake.setPhase(1)
	whoopSync(t, ds, fake, whoopSteady(map[string]string{"sleep": "2026-10-06T00:00:00Z"}))
	healthDrain(t, ds)

	after := mustGet(t, ds, healthObservation, strainID)
	if v, _ := quantityOf(t, after.Properties, "value"); v != "13.6118" {
		t.Errorf("the rescored strain = %s, want 13.6118", v)
	}
	if asInt(after.Properties["revision"]) <= asInt(before.Properties["revision"]) || after.Version <= before.Version {
		t.Errorf("the rescore moved revision %v -> %v and version %d -> %d, want both higher",
			before.Properties["revision"], after.Properties["revision"], before.Version, after.Version)
	}
	if asInt(after.Properties["revision"]) != epochSeconds(t, "2026-10-10T15:31:47.865Z") {
		t.Errorf("the rescored strain's revision = %v, want the rescore's updated_at", after.Properties["revision"])
	}
	// The cycle's recovery takes its span from the cycle, so the cycle's
	// delivery rebuilds it; the rescore moved neither the span nor the
	// recovery's scores, so the rebuild finds it holding what it implies and
	// leaves it unwritten, at the revision it had.
	recovery := mustGet(t, ds, healthObservation, hw(whoopRescoredCycle, "recovery"))
	if recovery.Version != recoveryBefore.Version ||
		fmt.Sprint(recovery.Properties["revision"]) != fmt.Sprint(recoveryBefore.Properties["revision"]) ||
		!sameInstant(t, recovery.Properties["endsAt"], fmt.Sprint(after.Properties["endsAt"])) {
		t.Errorf("the rescored cycle's recovery moved to version %d revision %v ending %v (was %d, %v); want it unwritten over the cycle's span",
			recovery.Version, recovery.Properties["revision"], recovery.Properties["endsAt"],
			recoveryBefore.Version, recoveryBefore.Properties["revision"])
	}
	for _, id := range append(napOutputs, hw(whoopDeletedNap, "sleep")) {
		kind := healthObservation
		if strings.HasSuffix(id, "/sleep") {
			kind = healthSleep
		}
		if whoopLive(t, ds, kind, id) {
			t.Errorf("%s outlived the nap WHOOP deleted", id)
		}
	}
	if whoopLive(t, ds, healthWorkout, hw(whoopDeletedWorkout, "workout")) {
		t.Error("the deleted workout's record survived")
	}
	for _, id := range workoutHR {
		if whoopLive(t, ds, healthObservation, id) {
			t.Errorf("%s outlived the workout WHOOP deleted", id)
		}
	}
	// The rescore rewrites the cycle's heart rate at the same ids.
	hrAfter := mustGet(t, ds, healthObservation, hw(whoopRescoredCycle, "heart_rate_max"))
	if v, _ := quantityOf(t, hrAfter.Properties, "value"); v != "168" || hrAfter.Version <= hrBefore.Version ||
		asInt(hrAfter.Properties["revision"]) != epochSeconds(t, "2026-10-10T15:31:47.865Z") {
		t.Errorf("the rescored cycle's maximum heart rate is %s at version %d (was %d), revision %v; want 168 at a higher version and the rescore's revision",
			v, hrAfter.Version, hrBefore.Version, hrAfter.Properties["revision"])
	}
	if v, _ := quantityOf(t, healthProps(t, ds, healthObservation, hw(whoopRescoredCycle, "heart_rate_avg")), "value"); v != "76" {
		t.Errorf("the rescored cycle's average heart rate is %s, want 76", v)
	}
	late := healthProps(t, ds, healthWorkout, hw(whoopLateWorkout, "workout"))
	// Padel has no value of its own in the workout kind's code set: it is
	// `other`, and WHOOP's word stays in sportSource.
	if late["sport"] != "other" || late["sportSource"] != "padel" {
		t.Errorf("the hand-logged workout landed as sport %v from %v, want other from padel", late["sport"], late["sportSource"])
	}
	lateHR := healthProps(t, ds, healthObservation, hw(whoopLateWorkout, "heart_rate_avg"))
	if v, _ := quantityOf(t, lateHR, "value"); v != "137" || lateHR["channel"] != "workout" {
		t.Errorf("the hand-logged workout's average heart rate is %s on channel %v, want 137 on workout", v, lateHR["channel"])
	}
	// WHOOP's kilojoules are the whole energy spent, resting included.
	if v, u := quantityOf(t, late, "totalEnergy"); v != "2816.74341" || u != "kJ" || late["activeEnergy"] != nil {
		t.Errorf("the padel energy = total %s %s, active %v; want 2816.74341 kJ total and no active", v, u, late["activeEnergy"])
	}
	if zones, _ := late["zoneDurations"].(map[string]any); len(zones) != 6 || zones["zone5"] != "PT0S" {
		t.Errorf("the padel zones = %v", late["zoneDurations"])
	}
}

// TestHealthWhoopAdapterUpgradeRewritesUnchangedMirrors: every output names
// the adapter version that wrote it, and an upgrade of the adapter alone,
// replayed over mirrors WHOOP did not change, rewrites each output at the
// revision it holds (decision record 0162).
func TestHealthWhoopAdapterUpgradeRewritesUnchangedMirrors(t *testing.T) {
	ds, fake := newHealthWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	healthDrain(t, ds)
	before := adapterOutputs(t, ds)
	assertAdapterStamp(t, before, healthWhoopAdapter)
	// health_test.go's upgradeAdapter raises @1 to @2, and this adapter
	// ships at @2, so the upgrade here raises it to @3.
	docs := loadDocs(t, samplesDir+"/healthwhoop/bundle.yaml")
	rewriteSource(t, docs, `ADAPTER = "healthwhoop@2"`, `ADAPTER = "healthwhoop@3"`)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.BundleActor(samples.Authority, "healthwhoop"), docs); err != nil {
		t.Fatalf("upgrade healthwhoop: %v", err)
	}
	replayAdapter(t, ds, healthWhoopRun)
	after := adapterOutputs(t, ds)
	if len(after) != len(before) {
		t.Fatalf("%d outputs after the upgrade, %d before", len(after), len(before))
	}
	for key, was := range before {
		now := after[key]
		switch {
		case now == nil:
			t.Fatalf("%s is gone after the upgrade", key)
		case now.Properties["adapter"] != "healthwhoop@3" || now.Version <= was.Version:
			t.Fatalf("%s: adapter %v at version %d (was %d), want healthwhoop@3 at a higher version",
				key, now.Properties["adapter"], now.Version, was.Version)
		case fmt.Sprint(now.Properties["revision"]) != fmt.Sprint(was.Properties["revision"]):
			t.Fatalf("%s: revision %v, was %v; an adapter-only rewrite keeps the upstream's",
				key, now.Properties["revision"], was.Properties["revision"])
		}
	}
}

// TestHealthWhoopKeepsAnAccountIdWithSlashes: a record id may hold slashes,
// and the adapter reads an account reference whole. The thirty days synced
// under the account `team/watch` land under that account's ids and source,
// never under its last segment, `watch`, which another account could share.
func TestHealthWhoopKeepsAnAccountIdWithSlashes(t *testing.T) {
	ds, fake := newHealthWhoop(t)
	const aid = "team/watch"
	mustPut(t, ds, substrate.PutInput{
		Kind: whoopAccountType, ID: aid,
		Properties: syncProps(map[string]any{"backfillDepth": "all"},
			"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep", "enabledWorkouts"),
	})
	cfg := whoopConfig(fake, whoopProps(nil))
	cfg["accounts"].([]any)[0].(map[string]any)["id"] = aid
	s := newStepper(t, ds, whoopSyncFn, cfg)
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountType, "id": aid}})
	s.drainApplying(nil)
	healthDrain(t, ds)

	base := "whoop/" + aid + "/"
	recovery := healthProps(t, ds, healthObservation, base+whoopExampleCycle+"/recovery")
	if storedRefPath(recovery["source"]) != healthSource+"/whoop/"+aid {
		t.Errorf("the recovery's source is %v, want whoop/%s", recovery["source"], aid)
	}
	src := healthProps(t, ds, healthSource, "whoop/"+aid)
	if storedRefPath(src["account"]) != whoopAccountType+"/"+aid {
		t.Errorf("the source's account is %v, want %s", src["account"], aid)
	}
	if whoopLive(t, ds, healthSource, "whoop/watch") {
		t.Error("a source was written for the account id's last segment")
	}
	n := 0
	for _, kind := range []string{healthObservation, healthSleep, healthWorkout} {
		for _, row := range listLive(t, ds, kind) {
			n++
			if !strings.HasPrefix(row.ID, base) {
				t.Errorf("%s %s is not under %s", kind, row.ID, base)
			}
		}
	}
	if n != 573 {
		t.Errorf("%d outputs under %s, want the 573 one account's thirty days imply", n, base)
	}
}
