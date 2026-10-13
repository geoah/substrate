package synth

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// Garmin is Garmin's Health and Activity API as the shipped `garmin` closure
// meets it: a connect step against the provider e2e runner's mock (the user
// id, the permissions and one answer per backfill type, 202 then 409), and
// the data as push deliveries to the `garmin-webhook` door, the envelope
// `{"<summary type>": [<summary>, ...]}` that `garminpush` writes. The
// shapes are the fixtures' under internal/providertest/testdata/garmin and
// the fields `garminpush` declares.
//
// One watch, a forerunner265, records the day: heart rate every 5 minutes
// in the daily, 15-minute epochs split by activity type, stress every 3
// minutes and Body Battery every 15 in the stress details, and the night's
// stages, SpO2 every 10 minutes, breathing every 15 and HRV every 5, and on
// a Saturday evening at home a two-minute health snapshot. Each
// workout is an activity and its details, a sample every 30 seconds with
// laps per kilometer on a run. A daily spans the persona's local day, which
// ends where the next one starts, so the dailies tile time across the trip.
type Garmin struct{}

func init() { register(Garmin{}) }

// Name is the emitter's name.
func (Garmin) Name() string { return "garmin" }

const (
	// garminUserID is what GET /rest/user/id answers and every summary
	// carries: the fixtures' user, so the deliveries match the account the
	// connect step records.
	garminUserID = "7c1e5b9a0d2f4c3e8b6a1f0e9d8c7b6a"
	// garminClientID is the config's clientId and the `garmin-client-id`
	// header: garminpush refuses a push naming another app.
	garminClientID = "9f2d6c1a-3b4e-4f5a-8c7d-0e1f2a3b4c5d"
	// garminSIDPrefix and a start second in hex make a summaryId, the
	// fixtures' convention.
	garminSIDPrefix = "x4b2c9e1-"
	garminDevice    = "forerunner265"
	// garminMockPort is the port providers/garmin/e2e.json pins: the rewired
	// function body names it, so it must not move between runs.
	garminMockPort = 48119
	// GarminTrigger is the webhook trigger every push is posted to.
	GarminTrigger = "garmin-webhook"
)

// garminBackfill is every backfill path garminsync asks for; Garmin offers
// none for manuallyUpdatedActivities.
var garminBackfill = []string{
	"activities", "activityDetails", "bloodPressures", "bodyComps", "dailies", "epochs", "healthSnapshot",
	"hrv", "moveiq", "pulseOx", "respiration", "skinTemp", "sleeps", "stressDetails", "userMetrics",
}

// garminToggles is every summary type's switch on the account.
var garminToggles = []string{
	"enabledActivities", "enabledActivityDetails", "enabledBloodPressures", "enabledBodyComps", "enabledDailies",
	"enabledEpochs", "enabledHRV", "enabledHealthSnapshot", "enabledManuallyUpdatedActivities", "enabledMoveIQ",
	"enabledPulseOx", "enabledRespiration", "enabledSkinTemp", "enabledSleeps", "enabledStressDetails", "enabledUserMetrics",
}

// garminWeekly are the summary types posted a week to a delivery, as a
// backfill would; the rest go one day to a delivery.
var garminWeekly = []string{
	"hrv", "pulseox", "skinTemp", "bodyComps", "bloodPressures", "userMetrics", "activities", "moveIQActivities",
}

// garminSport is the persona's sport as Garmin's activity type, the name a
// Garmin activity carries after the city, the epoch's activity type and
// intensity, and its MET.
var garminSport = map[Sport]struct {
	activity, label, epoch, intensity string
	met                               float64
}{
	Running:  {"RUNNING", "Running", "RUNNING", "HIGHLY_ACTIVE", 9.8},
	Cycling:  {"CYCLING", "Cycling", "CYCLING", "HIGHLY_ACTIVE", 7.5},
	Strength: {"STRENGTH_TRAINING", "Strength", "GENERIC", "ACTIVE", 3.5},
	Yoga:     {"YOGA", "Yoga", "GENERIC", "ACTIVE", 2.5},
	Walking:  {"WALKING", "Walking", "WALKING", "ACTIVE", 3.3},
}

// garminCity names an activity and places its start.
var garminCity = map[string]struct {
	name     string
	lat, lon float64
}{
	"Europe/Athens": {"Athens", 37.9740, 23.7370},
	"Europe/London": {"London", 51.5085, -0.1650},
}

// Emit writes recordings/ and e2e.json for the connect step, the pushes as
// deliveries in posting order, and the adapter sample as the setup.
func (Garmin) Emit(p *Persona) (*Output, error) {
	e := newGarminEmitter(p)
	// The runner installs the provider and creates the account, so the
	// loader imports only the adapter, after the connect step.
	out := &Output{Setup: Setup{Samples: []string{"samples.substrate.reamde.dev/healthgarmin"}}}
	add := func(name string, body any) {
		out.Files = append(out.Files, File{Path: "recordings/" + name, Body: JSON(body)})
	}
	add(RecordingName("GET", "/wellness-api/rest/user/id"), map[string]any{"userId": garminUserID})
	add(RecordingName("GET", "/wellness-api/rest/user/permissions"),
		[]string{"ACTIVITY_EXPORT", "WORKOUT_IMPORT", "HEALTH_EXPORT", "COURSE_IMPORT", "MCT_EXPORT"})
	for _, path := range garminBackfill {
		add(RecordingName("GET", "/wellness-api/rest/backfill/"+path), map[string]any{"__responses": []any{
			map[string]any{"__response": map[string]any{"status": 202}, "body": ""},
			map[string]any{
				"__response": map[string]any{"status": 409},
				"body":       map[string]any{"errorMessage": "Duplicate backfill request for this time range"},
			},
		}})
	}
	out.Files = append(out.Files, File{Path: "e2e.json", Body: JSON(garminE2E(p))})
	deliveries, err := e.deliveries()
	if err != nil {
		return nil, err
	}
	out.Deliveries = deliveries
	return out, nil
}

// garminE2E is providers/garmin/e2e.json for this data: the recorded one's
// hosts, config, continuation and port, every summary type on, Garmin's
// deepest backfill, and an account named for the person.
func garminE2E(p *Persona) map[string]any {
	account := map[string]any{"backfillDepth": "last90d"}
	for _, t := range garminToggles {
		account[t] = true
	}
	return map[string]any{
		"_generated": fmt.Sprintf("internal/synth (garmin.go): %s to %s, seed %q. The runner reads this in place of the recorded e2e.json; the data arrives as the deliveries beside it.",
			p.Options.Start, p.Options.End, p.Options.Seed),
		"_hosts":     "Garmin authorizes at connect.garmin.com, exchanges tokens at connectapi.garmin.com and serves the wellness API at apis.garmin.com, so the rewire names all three.",
		"hosts":      []any{"https://apis.garmin.com", "https://connect.garmin.com", "https://connectapi.garmin.com"},
		"_config":    "the client id the deliveries carry in garmin-client-id: garminpush refuses a push naming another app",
		"config":     map[string]any{"clientId": garminClientID, "clientSecret": "e2e-client-secret"},
		"account":    account,
		"_accountId": "the sources the adapter writes are garmin/<account id>/account and garmin/<account id>/" + garminDevice,
		"accountId":  "ada",
		"_triggers":  "garmin-on-connect drives the first sync, which records the user id the pushes are matched on; garmin-on-request continues it. garmin-webhook is armed with its key after the connect step, by whoever posts the deliveries.",
		"triggers":   []any{"garmin-on-connect", "garmin-on-request"},
		"_settleSeconds": "a ceiling, not a delay: the first sync asks for 15 types x 90 one-day windows in about 5 s, " +
			"and in a repository other loaders write to the runner waits out the whole ceiling.",
		"settleSeconds":  300,
		"continueRounds": 20,
		"continue":       map[string]any{"property": "syncRequestedAt", "value": "now"},
		"mockPort":       garminMockPort,
	}
}

type garminEmitter struct {
	p    *Persona
	days []Date

	hrBias, stepScale, kcalScale, scaleBias, vo2Bias float64
}

func newGarminEmitter(p *Persona) *garminEmitter {
	e := &garminEmitter{p: p, days: p.Days()}
	e.hrBias = p.Noisy("garmin|hr-bias", 1.0, 0.4)
	e.stepScale = p.Noisy("garmin|steps-scale", 1.05, 0.01)
	e.kcalScale = p.Noisy("garmin|kcal-scale", 0.96, 0.01)
	e.scaleBias = p.Noisy("garmin|scale-bias", 0.2, 0.03)
	e.vo2Bias = p.Noisy("garmin|vo2-bias", 0.8, 0.1)
	return e
}

// --- the helpers -------------------------------------------------------------

func (e *garminEmitter) noisy(what string, key any, v, sd float64) float64 {
	return e.p.Noisy(fmt.Sprintf("garmin|%s|%v", what, key), v, sd)
}

func garminSID(t int64) string { return garminSIDPrefix + fmt.Sprintf("%x", t) }

func garminOffset(t time.Time) int {
	_, off := t.Zone()
	return off
}

func iround(v float64) int { return int(math.Round(v)) }

// span is the day's [start, end) in seconds.
func (e *garminEmitter) span(d Date) (int64, int64) {
	x := e.p.Day(d)
	return x.Start().Unix(), x.EndTime().Unix()
}

// hrAt is the watch's heart rate at t on day d.
func (e *garminEmitter) hrAt(d Date, t time.Time) int {
	return iround(e.noisy("hr", t.Unix(), float64(e.p.Day(d).HR(t))+e.hrBias, 1.4))
}

// stressAt is the watch's stress level at t, -1 where it measured none.
func (e *garminEmitter) stressAt(d Date, t time.Time) int {
	v := e.p.Day(d).Stress(t)
	if v < 0 {
		return -1
	}
	return max(1, min(99, iround(e.noisy("stress", t.Unix(), float64(v), 2))))
}

func (e *garminEmitter) stepsMinute(d Date, t time.Time) int {
	return e.p.Day(d).StepsIn(t, t.Add(time.Minute))
}

// --- the day -----------------------------------------------------------------

type garminEpochGroup struct {
	etype, intensity string
	minutes, steps   int
	dist, kcal       float64
	w                *Workout
}

// epochs is the day's 15-minute epochs: one per activity type inside each
// slice, numbered within the slice.
func (e *garminEmitter) epochs(d Date, a, b int64, off int) []map[string]any {
	x := e.p.Day(d)
	var out []map[string]any
	for t := a; t < b; t += 900 {
		var groups []*garminEpochGroup
		byClass := map[string]*garminEpochGroup{}
		for m := int64(0); m < 15 && t+60*m < b; m++ {
			at := time.Unix(t+60*m, 0).In(x.Zone)
			w := x.WorkoutAt(at)
			n := e.stepsMinute(d, at)
			etype, intensity := "SEDENTARY", "SEDENTARY"
			switch {
			case w != nil:
				etype, intensity = garminSport[w.Sport].epoch, garminSport[w.Sport].intensity
			case n >= 50:
				etype, intensity = "WALKING", "ACTIVE"
			}
			g := byClass[etype]
			if g == nil {
				g = &garminEpochGroup{etype: etype, intensity: intensity, w: w}
				byClass[etype] = g
				groups = append(groups, g)
			}
			g.minutes++
			g.steps += n
			switch {
			case w != nil && w.Sport == Running:
				g.dist += w.DistanceM / w.Duration().Minutes()
			case w == nil:
				g.dist += float64(n) * StrideM
			}
			// the persona's ActiveKcal: each step, plus each workout's own
			g.kcal += float64(n) * KcalPerStep
			if w != nil {
				g.kcal += float64(w.Kcal) / w.Duration().Minutes()
			}
		}
		for i, g := range groups {
			steps := iround(float64(g.steps) * e.stepScale * e.noisy("epoch-steps", fmt.Sprint(t, g.etype), 1, 0.02))
			scale := 1.0
			if g.steps > 0 {
				scale = float64(steps) / float64(g.steps)
			}
			var met, meanMI, maxMI float64
			switch g.etype {
			case "SEDENTARY":
				met, meanMI, maxMI = 1.0, 0.05+0.002*float64(steps), 2.0
				if steps > 0 {
					met = 1.3
				}
				if steps > 20 {
					maxMI = 3.0
				}
			case "WALKING":
				met, meanMI, maxMI = 3.3, 2.6, 5.0
			default:
				met, meanMI, maxMI = garminSport[g.w.Sport].met, 5.5, 7.0
			}
			out = append(out, map[string]any{
				"userId":                   garminUserID,
				"summaryId":                fmt.Sprintf("%s-%d", garminSID(t), i),
				"activityType":             g.etype,
				"activeKilocalories":       iround(g.kcal * e.kcalScale),
				"steps":                    steps,
				"distanceInMeters":         round(g.dist*scale, 2),
				"durationInSeconds":        900,
				"activeTimeInSeconds":      60 * g.minutes,
				"startTimeInSeconds":       t,
				"startTimeOffsetInSeconds": off,
				"met":                      round(met, 4),
				"intensity":                g.intensity,
				"meanMotionIntensity":      round(meanMI, 2),
				"maxMotionIntensity":       maxMI,
			})
		}
	}
	return out
}

func garminStressQualifier(avg int) string {
	switch {
	case avg <= 25:
		return "calm"
	case avg <= 40:
		return "balanced"
	case avg <= 50:
		return "stressful"
	}
	return "very_stressful"
}

// dayOf is the daily summary, its epochs and its stress details.
func (e *garminEmitter) dayOf(d Date, acts []garminActivity) (daily map[string]any, epochs []map[string]any, stress map[string]any) {
	x := e.p.Day(d)
	a, b := e.span(d)
	off := garminOffset(x.Start())
	at := func(s int64) time.Time { return time.Unix(s, 0).In(x.Zone) }

	hr := map[string]int{}
	var hrs []int
	hrSum := 0
	for k := int64(0); k < b-a; k += 300 {
		v := e.hrAt(d, at(a+k))
		hr[fmt.Sprint(k)] = v
		hrs = append(hrs, v)
		hrSum += v
	}
	for _, act := range acts {
		for _, s := range act.samples {
			hrs = append(hrs, s["heartRate"].(int))
		}
	}
	epochs = e.epochs(d, a, b, off)
	levels := map[string]int{}
	var measured []int
	bands := map[string]int{}
	for k := int64(0); k < b-a; k += 180 {
		v := e.stressAt(d, at(a+k))
		levels[fmt.Sprint(k)] = v
		switch {
		case v < 0:
			bands["activity"] += 180
		case v <= 25:
			bands["rest"] += 180
		case v <= 50:
			bands["low"] += 180
		case v <= 75:
			bands["medium"] += 180
		default:
			bands["high"] += 180
		}
		if v >= 0 {
			measured = append(measured, v)
		}
	}
	battery := map[string]int{}
	var bb []int
	for k := int64(0); k < b-a; k += 900 {
		v := max(5, min(100, iround(e.noisy("bb", a+k, float64(x.BodyBattery(at(a+k))), 1))))
		battery[fmt.Sprint(k)] = v
		bb = append(bb, v)
	}
	charged, drained, top := 0, 0, 0
	for i, v := range bb {
		top = max(top, v)
		if i > 0 {
			charged += max(0, v-bb[i-1])
			drained += max(0, bb[i-1]-v)
		}
	}
	avgStress, maxStress := -1, -1
	if len(measured) > 0 {
		sum := 0
		for _, v := range measured {
			sum += v
			maxStress = max(maxStress, v)
		}
		avgStress = iround(float64(sum) / float64(len(measured)))
	}
	var steps, akcal, active, moderate, vigorous int
	var dist float64
	for _, ep := range epochs {
		steps += ep["steps"].(int)
		akcal += ep["activeKilocalories"].(int)
		dist += ep["distanceInMeters"].(float64)
		switch ep["intensity"] {
		case "HIGHLY_ACTIVE":
			vigorous += ep["activeTimeInSeconds"].(int)
			active += ep["activeTimeInSeconds"].(int)
		case "ACTIVE":
			active += ep["activeTimeInSeconds"].(int)
			if ep["activityType"] != "WALKING" || ep["steps"].(int) >= 900 {
				moderate += ep["activeTimeInSeconds"].(int)
			}
		}
	}
	minHR, maxHR := hrs[0], hrs[0]
	for _, v := range hrs {
		minHR, maxHR = min(minHR, v), max(maxHR, v)
	}
	daily = map[string]any{
		"userId":                             garminUserID,
		"summaryId":                          garminSID(a),
		"calendarDate":                       d.String(),
		"activityType":                       "GENERIC",
		"activeKilocalories":                 akcal,
		"bmrKilocalories":                    iround(e.noisy("bmr", d, float64(x.BMRKcal())*float64(b-a)/86400, 4)),
		"steps":                              steps,
		"distanceInMeters":                   round(dist, 1),
		"durationInSeconds":                  b - a,
		"activeTimeInSeconds":                active,
		"startTimeInSeconds":                 a,
		"startTimeOffsetInSeconds":           off,
		"moderateIntensityDurationInSeconds": moderate,
		"vigorousIntensityDurationInSeconds": vigorous,
		"floorsClimbed":                      max(0, x.Floors()+iround(e.noisy("floors", d, 0, 0.8))),
		"minHeartRateInBeatsPerMinute":       minHR,
		"maxHeartRateInBeatsPerMinute":       maxHR,
		"averageHeartRateInBeatsPerMinute":   iround(float64(hrSum) / float64(len(hr))),
		"restingHeartRateInBeatsPerMinute":   iround(e.noisy("rhr", d, float64(x.RestingHR), 1)),
		"timeOffsetHeartRateSamples":         hr,
		"averageStressLevel":                 avgStress,
		"maxStressLevel":                     maxStress,
		"stressDurationInSeconds":            bands["low"] + bands["medium"] + bands["high"],
		"restStressDurationInSeconds":        bands["rest"],
		"activityStressDurationInSeconds":    bands["activity"],
		"lowStressDurationInSeconds":         bands["low"],
		"mediumStressDurationInSeconds":      bands["medium"],
		"highStressDurationInSeconds":        bands["high"],
		"stressQualifier":                    garminStressQualifier(avgStress),
		"bodyBatteryChargedValue":            charged,
		"bodyBatteryDrainedValue":            drained,
		"stepsGoal":                          8000,
		"floorsClimbedGoal":                  10,
		"intensityDurationGoalInSeconds":     9000,
	}
	var events []any
	if s := x.Sleep; s != nil {
		events = append(events, map[string]any{
			"eventType": "SLEEP", "eventStartTimeInSeconds": s.Start.Unix(),
			"eventStartTimeOffsetInSeconds": garminOffset(s.Start), "duration": int64(s.Duration().Seconds()),
			"bodyBatteryImpact": iround(e.noisy("bb-sleep", d, 45+float64(x.Recovery-50)*0.4, 3)),
		})
	}
	for _, act := range acts {
		events = append(events, map[string]any{
			"eventType": "ACTIVITY", "eventStartTimeInSeconds": act.activity["startTimeInSeconds"],
			"eventStartTimeOffsetInSeconds": act.activity["startTimeOffsetInSeconds"],
			"duration":                      act.activity["durationInSeconds"],
			"bodyBatteryImpact":             -iround(math.Min(18, act.strain*1.2)),
			"activityName":                  act.activity["activityName"], "activityType": act.activity["activityType"],
			"activityId": act.activity["activityId"],
		})
	}
	level := "LOW"
	switch {
	case top >= 75:
		level = "HIGH"
	case top >= 50:
		level = "MODERATE"
	}
	stress = map[string]any{
		"userId":                      garminUserID,
		"summaryId":                   garminSID(a),
		"startTimeInSeconds":          a,
		"startTimeOffsetInSeconds":    off,
		"durationInSeconds":           b - a,
		"calendarDate":                d.String(),
		"maxStressLevel":              maxStress,
		"averageStressLevel":          avgStress,
		"timeOffsetStressLevelValues": levels,
		"timeOffsetBodyBatteryValues": battery,
		"bodyBatteryDynamicFeedbackEvent": map[string]any{
			"eventStartTimeInSeconds": d.At(x.Zone, 10, 0).Unix(), "bodyBatteryLevel": level,
		},
		"bodyBatteryActivityEvents": events,
	}
	return daily, epochs, stress
}

// --- the night ---------------------------------------------------------------

type garminNight struct {
	sleep, hrv, pulseox, skin map[string]any
	resp                      []map[string]any
}

func garminQualifier(v, good, fair float64) string {
	switch {
	case v >= good*1.1:
		return "EXCELLENT"
	case v >= good:
		return "GOOD"
	case v >= fair:
		return "FAIR"
	}
	return "POOR"
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// night is the sleep summary of the night ending on d, with its HRV, Pulse Ox,
// skin temperature and breathing slices.
func (e *garminEmitter) night(d Date) garminNight {
	x := e.p.Day(d)
	s := x.Sleep
	a, end := s.Start.Unix(), s.End.Unix()
	off := garminOffset(s.Start)
	levels := map[string]any{}
	totals := map[Stage]int64{}
	for _, st := range []Stage{Deep, Light, REM, Awake} {
		var spans []any
		for _, sp := range s.Stages {
			if sp.Stage == st && sp.End.Unix() > sp.Start.Unix() {
				spans = append(spans, map[string]any{"startTimeInSeconds": sp.Start.Unix(), "endTimeInSeconds": sp.End.Unix()})
				totals[st] += sp.End.Unix() - sp.Start.Unix()
			}
		}
		if len(spans) > 0 {
			levels[string(st)] = spans
		}
	}
	asleep := totals[Deep] + totals[Light] + totals[REM]
	spo2 := map[string]int{}
	for _, smp := range x.SpO2Night(10 * time.Minute) {
		k := smp.At.Unix() - a
		spo2[fmt.Sprint(k)] = min(100, iround(e.noisy("spo2", a+k, smp.Value, 0.5)))
	}
	respSleep := map[string]float64{}
	for _, smp := range x.RespirationNight(15 * time.Minute) {
		k := smp.At.Unix() - a
		respSleep[fmt.Sprint(k)] = round(e.noisy("resp", a+k, smp.Value, 0.3), 2)
	}
	deepFrac := float64(totals[Deep]) / math.Max(1, float64(asleep))
	score := 25 + 25*clamp01((float64(asleep)/3600-5.5)/3) + 20*clamp01((s.Efficiency-0.85)/0.12) +
		10*clamp01((deepFrac-0.12)/0.1) + 0.2*float64(x.Recovery)
	scoreV := max(30, min(98, iround(e.noisy("sleep-score", d, score, 3))))
	overall := "POOR"
	switch {
	case scoreV >= 90:
		overall = "EXCELLENT"
	case scoreV >= 80:
		overall = "GOOD"
	case scoreV >= 60:
		overall = "FAIR"
	}
	wakes := 0
	for i, sp := range s.Stages {
		if i > 0 && sp.Stage == Awake {
			wakes++
		}
	}
	q := func(k string) map[string]any { return map[string]any{"qualifierKey": k} }
	stressQ, awakeQ, restQ := "GOOD", "POOR", "FAIR"
	if x.Ill {
		stressQ = "FAIR"
	}
	switch {
	case wakes <= 3:
		awakeQ = "GOOD"
	case wakes <= 5:
		awakeQ = "FAIR"
	}
	if s.Efficiency >= 0.9 {
		restQ = "GOOD"
	}
	sleep := map[string]any{
		"userId":                      garminUserID,
		"summaryId":                   garminSID(a),
		"calendarDate":                d.String(),
		"durationInSeconds":           asleep,
		"startTimeInSeconds":          a,
		"startTimeOffsetInSeconds":    off,
		"unmeasurableSleepInSeconds":  0,
		"deepSleepDurationInSeconds":  totals[Deep],
		"lightSleepDurationInSeconds": totals[Light],
		"remSleepInSeconds":           totals[REM],
		"awakeDurationInSeconds":      totals[Awake],
		"sleepLevelsMap":              levels,
		"validation":                  "ENHANCED_FINAL",
		"timeOffsetSleepRespiration":  respSleep,
		"timeOffsetSleepSpo2":         spo2,
		"overallSleepScore":           map[string]any{"value": scoreV, "qualifierKey": overall},
		"sleepScores": map[string]any{
			"totalDuration":   q(garminQualifier(float64(asleep)/3600, 7, 6)),
			"stress":          q(stressQ),
			"awakeCount":      q(awakeQ),
			"remPercentage":   q(garminQualifier(float64(totals[REM])/math.Max(1, float64(asleep)), 0.21, 0.15)),
			"restlessness":    q(restQ),
			"lightPercentage": q("GOOD"),
			"deepPercentage":  q(garminQualifier(deepFrac, 0.16, 0.10)),
		},
	}
	if len(x.Naps) > 0 {
		var naps []any
		var total int64
		for _, n := range x.Naps {
			dur := int64(n.Duration().Seconds())
			naps = append(naps, map[string]any{
				"napStartTimeInSeconds": n.Start.Unix(), "napOffsetInSeconds": garminOffset(n.Start),
				"napDurationInSeconds": dur, "napValidation": "device",
			})
			total += dur
		}
		sleep["naps"] = naps
		sleep["totalNapDurationInSeconds"] = total
	}

	// HRV every five minutes, deep higher and REM lower, centered on the night.
	target := e.noisy("hrv", d, s.HRVRMSSDMs, 2)
	bump := map[Stage]float64{Deep: 12, Light: 0, REM: -8, Awake: -10}
	type kv struct {
		k int64
		v float64
	}
	var raw []kv
	sum := 0.0
	for k := int64(300); k <= end-a; k += 300 {
		v := target + bump[s.StageAt(time.Unix(a+k, 0))] + e.p.rng("garmin", "hrv5", d, k).NormFloat64()*6
		raw = append(raw, kv{k, v})
		sum += v
	}
	shift := 0.0
	if len(raw) > 0 {
		shift = target - sum/float64(len(raw))
	}
	hrvValues := map[string]int{}
	high := iround(target)
	for i, r := range raw {
		v := max(12, iround(r.v+shift))
		hrvValues[fmt.Sprint(r.k)] = v
		if i == 0 || v > high {
			high = v
		}
	}
	night := garminNight{
		sleep: sleep,
		hrv: map[string]any{
			"userId": garminUserID, "summaryId": garminSID(a), "calendarDate": d.String(),
			"lastNightAvg": iround(target), "lastNight5MinHigh": high,
			"startTimeOffsetInSeconds": off, "durationInSeconds": end - a, "startTimeInSeconds": a,
			"hrvValues": hrvValues,
		},
		pulseox: map[string]any{
			"userId": garminUserID, "summaryId": garminSID(a), "calendarDate": d.String(),
			"startTimeInSeconds": a, "durationInSeconds": end - a, "startTimeOffsetInSeconds": off,
			"timeOffsetSpo2Values": spo2, "onDemand": false,
		},
		skin: map[string]any{
			"userId": garminUserID, "summaryId": garminSID(a), "calendarDate": d.String(),
			"avgDeviationCelsius": round(e.noisy("skin", d, s.SkinTempDevC, 0.05), 2),
			"durationInSeconds":   end - a, "startTimeInSeconds": a, "startTimeOffsetInSeconds": off,
		},
	}
	// Breathing, one 15-minute slice per quarter hour of the night.
	for t := a - a%900; t < end; t += 900 {
		v := s.RespRate + e.p.rng("garmin", "resp", d, t).NormFloat64()*0.6
		night.resp = append(night.resp, map[string]any{
			"userId": garminUserID, "summaryId": garminSID(t), "startTimeInSeconds": t,
			"durationInSeconds": 900, "startTimeOffsetInSeconds": off,
			"timeOffsetEpochToBreaths": map[string]float64{"0": round(e.noisy("resp-slice", t, v, 0.3), 2)},
		})
	}
	return night
}

// --- the workouts ------------------------------------------------------------

type garminActivity struct {
	activity, detail map[string]any
	samples          []map[string]any
	strain           float64
}

// garminAltitude is the ground an activity starts on in each home of the
// persona's, in metres; elsewhere 50.
var garminAltitude = map[string]float64{"Europe/Athens": 95, "Europe/London": 20}

// activities is each workout of d as an activity summary and its details.
func (e *garminEmitter) activities(d Date) []garminActivity {
	x := e.p.Day(d)
	var out []garminActivity
	for k, w := range x.Workouts {
		out = append(out, e.activity(d, k, w, x.Zone.String()))
	}
	return out
}

// activity is one workout: the heart rate every 30 seconds, and on a
// workout with a distance the route the watch recorded beside it.
func (e *garminEmitter) activity(d Date, k int, w *Workout, zone string) garminActivity {
	sport := garminSport[w.Sport]
	city, placed := garminCity[zone]
	if !placed {
		parts := strings.Split(zone, "/")
		city.name = parts[len(parts)-1]
	}
	aid := int64(21100000000 + e.p.Index(d)*100 + k)
	a, dur := w.Start.Unix(), int64(w.Duration().Seconds())
	samples, avgHR, maxHR := e.heartSamples(w)
	summary := map[string]any{
		"activityName":                     city.name + " " + sport.label,
		"durationInSeconds":                dur,
		"startTimeInSeconds":               a,
		"startTimeOffsetInSeconds":         garminOffset(w.Start),
		"activityType":                     sport.activity,
		"averageHeartRateInBeatsPerMinute": iround(avgHR),
		"maxHeartRateInBeatsPerMinute":     maxHR,
		"activeKilocalories":               iround(e.noisy("kcal", fmt.Sprint(d, k), float64(w.Kcal), float64(w.Kcal)*0.03)),
		"deviceName":                       garminDevice,
		"manual":                           false,
		"isWebUpload":                      false,
	}
	var laps []any
	if w.DistanceM > 0 {
		r := e.p.rng("garmin", "route", d, k)
		e.route(d, k, w, samples, avgHR, summary)
		e.altitude(w, samples, summary, r, garminAltitude[zone])
		if placed {
			summary["startingLatitudeInDegree"] = round(city.lat-0.004+0.008*r.Float64(), 7)
			summary["startingLongitudeInDegree"] = round(city.lon-0.004+0.008*r.Float64(), 7)
		}
		if w.Sport == Running {
			laps = e.stride(d, k, w, samples, summary, r)
		} else {
			e.pedals(d, k, samples, summary, r)
		}
	}
	for _, s := range samples {
		el := s["startTimeInSeconds"].(int64) - a
		s["timerDurationInSeconds"], s["clockDurationInSeconds"], s["movingDurationInSeconds"] = el, el, el
	}
	activity := map[string]any{"userId": garminUserID, "summaryId": fmt.Sprint(aid), "activityId": aid}
	for k, v := range summary {
		activity[k] = v
	}
	sampleList := make([]any, len(samples))
	for i, s := range samples {
		sampleList[i] = s
	}
	detail := map[string]any{
		"userId": garminUserID, "summaryId": fmt.Sprintf("%d-detail", aid), "activityId": aid,
		"summary": summary, "samples": sampleList,
	}
	if len(laps) > 0 {
		detail["laps"] = laps
	}
	return garminActivity{activity: activity, detail: detail, samples: samples, strain: w.Strain}
}

// heartSamples is the workout's heart rate every 30 seconds, end included,
// with its average and maximum.
func (e *garminEmitter) heartSamples(w *Workout) ([]map[string]any, float64, int) {
	a, dur := w.Start.Unix(), int64(w.Duration().Seconds())
	var samples []map[string]any
	sum, maxHR := 0, 0
	for t := a; t <= a+dur; t += 30 {
		v := iround(e.noisy("hr", t, float64(w.HR(time.Unix(t, 0)))+e.hrBias, 1.4))
		samples = append(samples, map[string]any{"startTimeInSeconds": t, "heartRate": v})
		sum += v
		maxHR = max(maxHR, v)
	}
	return samples, float64(sum) / float64(len(samples)), maxHR
}

// route spreads the distance over the samples, faster where the heart works
// harder: each sample's running total and its speed over the 30 seconds it
// starts, and the summary's distance, speeds and paces.
func (e *garminEmitter) route(d Date, k int, w *Workout, samples []map[string]any, avgHR float64, summary map[string]any) {
	dur := int64(w.Duration().Seconds())
	dist := round(e.noisy("dist", fmt.Sprint(d, k), w.DistanceM, w.DistanceM*0.005), 2)
	weights := make([]float64, len(samples))
	total := 0.0
	for i, s := range samples {
		weights[i] = math.Max(0.4, 1+0.9*(float64(s["heartRate"].(int))-avgHR)/avgHR)
		if i < len(samples)-1 {
			total += weights[i]
		}
	}
	total = math.Max(total, 1e-9)
	run, maxSpeed := 0.0, 0.0
	for i, s := range samples {
		speed := dist * weights[min(i, len(samples)-2)] / total / 30
		maxSpeed = math.Max(maxSpeed, speed)
		s["totalDistanceInMeters"] = round(run, 2)
		s["speedMetersPerSecond"] = round(speed, 3)
		if i < len(samples)-1 {
			run += dist * weights[i] / total
		}
	}
	avgSpeed := dist / float64(dur)
	summary["distanceInMeters"] = dist
	summary["averageSpeedInMetersPerSecond"] = round(avgSpeed, 3)
	summary["maxSpeedInMetersPerSecond"] = round(maxSpeed, 3)
	summary["averagePaceInMinutesPerKilometer"] = round(1000/avgSpeed/60, 6)
	summary["maxPaceInMinutesPerKilometer"] = round(1000/maxSpeed/60, 6)
}

// altitude is the ground under each sample: a loop over two rises from the
// start's altitude, whose climbs add up to the summary's elevation gain,
// and back down to where it started.
func (e *garminEmitter) altitude(w *Workout, samples []map[string]any, summary map[string]any, r *rand.Rand, base float64) {
	hi := 260.0
	if w.Sport == Running {
		hi = 90
	}
	gain := math.Round(15 + (hi-15)*r.Float64())
	summary["totalElevationGainInMeters"] = gain
	summary["totalElevationLossInMeters"] = gain
	last := float64(len(samples) - 1)
	for i, s := range samples {
		f := float64(i) / math.Max(1, last)
		s["elevationInMeters"] = round(base+gain/4*(1-math.Cos(4*math.Pi*f)), 1)
	}
}

// Running power is about 1.04 W per kilogram for each metre a second, on
// the watch's stored weight for the runner.
const (
	garminWattsPerKgMps = 1.04
	garminPowerKg       = 72.0
)

// stride is a run's step cadence and running power, sample by sample, with
// the summary's cadence and steps, and a lap at every kilometre.
func (e *garminEmitter) stride(d Date, k int, w *Workout, samples []map[string]any, summary map[string]any, r *rand.Rand) []any {
	a, dur := w.Start.Unix(), int64(w.Duration().Seconds())
	base := 166.0
	if w.Name == "intervals" {
		base = 172
	}
	cad := round(e.noisy("cadence", fmt.Sprint(d, k), base, 2), 1)
	avgSpeed := summary["averageSpeedInMetersPerSecond"].(float64)
	for _, s := range samples {
		speed := s["speedMetersPerSecond"].(float64)
		t := s["startTimeInSeconds"].(int64)
		s["stepsPerMinute"] = round(e.noisy("spm", t, cad*(0.9+0.1*speed/avgSpeed), 1.5), 1)
		s["powerInWatts"] = float64(iround(e.noisy("power", t, garminWattsPerKgMps*garminPowerKg*speed, 6)))
	}
	summary["averageRunCadenceInStepsPerMinute"] = cad
	summary["maxRunCadenceInStepsPerMinute"] = round(cad+11+4*r.Float64(), 1)
	summary["steps"] = iround(cad * float64(dur) / 60)
	laps := []any{map[string]any{"startTimeInSeconds": a}}
	km := 1000.0
	for i := 1; i < len(samples); i++ {
		before, at := samples[i-1]["totalDistanceInMeters"].(float64), samples[i]["totalDistanceInMeters"].(float64)
		for at >= km {
			frac := (km - before) / math.Max(1e-9, at-before)
			laps = append(laps, map[string]any{
				"startTimeInSeconds": iround(float64(samples[i-1]["startTimeInSeconds"].(int64)) + 30*frac),
			})
			km += 1000
		}
	}
	return laps
}

// pedals is a ride's pedal cadence, sample by sample, with the summary's.
func (e *garminEmitter) pedals(d Date, k int, samples []map[string]any, summary map[string]any, r *rand.Rand) {
	cad := round(e.noisy("cadence", fmt.Sprint(d, k), 84, 2), 1)
	avgSpeed := summary["averageSpeedInMetersPerSecond"].(float64)
	for _, s := range samples {
		speed := s["speedMetersPerSecond"].(float64)
		s["bikeCadenceInRPM"] = float64(iround(e.noisy("rpm", s["startTimeInSeconds"].(int64), cad*(0.85+0.15*speed/avgSpeed), 3)))
	}
	summary["averageBikeCadenceInRoundsPerMinute"] = cad
	summary["maxBikeCadenceInRoundsPerMinute"] = round(cad+22+5*r.Float64(), 1)
}

// --- the mornings and the rest -----------------------------------------------

// morning is the day's weigh-in and blood pressure reading, by envelope key.
func (e *garminEmitter) morning(d Date) map[string]map[string]any {
	x := e.p.Day(d)
	out := map[string]map[string]any{}
	if x.WeightKg != 0 {
		t := x.WeighInAt.Unix()
		kg := round(e.noisy("weight", d, x.WeightKg+e.scaleBias, 0.05), 2)
		fat := round(e.noisy("fat", d, x.BodyFatPct, 0.3), 1)
		out["bodyComps"] = map[string]any{
			"userId":                         garminUserID,
			"summaryId":                      garminSID(t),
			"measurementTimeInSeconds":       t,
			"measurementTimeOffsetInSeconds": garminOffset(x.WeighInAt),
			"weightInGrams":                  iround(kg * 1000),
			"bodyFatInPercent":               fat,
			"bodyWaterInPercent":             round(e.noisy("water", d, 73.0*(1-fat/100)*0.72+1.0, 0.2), 1),
			"muscleMassInGrams":              iround(kg * 1000 * (1 - fat/100) * e.noisy("muscle", d, 0.62, 0.004)),
			"boneMassInGrams":                iround(e.noisy("bone", d, 2650, 10)),
			"bodyMassIndex":                  round(kg/(e.p.Person.HeightM*e.p.Person.HeightM), 1),
		}
	}
	if bp := x.BloodPressure; bp != nil {
		t := bp.At.Unix()
		out["bloodPressures"] = map[string]any{
			"userId":                         garminUserID,
			"summaryId":                      garminSID(t),
			"systolic":                       iround(e.noisy("sys", d, float64(bp.Systolic), 1.5)),
			"diastolic":                      iround(e.noisy("dia", d, float64(bp.Diastolic), 1)),
			"pulse":                          iround(e.noisy("pulse", d, float64(bp.Pulse), 1)),
			"sourceType":                     "DEVICE",
			"measurementTimeInSeconds":       t,
			"measurementTimeOffsetInSeconds": garminOffset(bp.At),
		}
	}
	return out
}

// userMetric is VO2 max and fitness age on d, when either changed since the
// last one written, and every Sunday; nil otherwise.
func (e *garminEmitter) userMetric(d Date, last *[2]float64) map[string]any {
	x := e.p.Day(d)
	vo2 := round(x.VO2Max+e.vo2Bias, 1)
	now := [2]float64{vo2, float64(x.FitnessAge)}
	if *last == now && d.Weekday() != time.Sunday {
		return nil
	}
	*last = now
	a, _ := e.span(d)
	return map[string]any{
		"userId": garminUserID, "summaryId": garminSID(a), "calendarDate": d.String(),
		"vo2Max": vo2, "fitnessAge": x.FitnessAge, "enhanced": true,
	}
}

// moveIQ is an evening stroll the watch detected on its own, every sixth
// day the persona strolls at home and well.
func (e *garminEmitter) moveIQ(d Date, strolls *int) map[string]any {
	x := e.p.Day(d)
	if x.Trip || x.Ill || !x.Strolls() {
		return nil
	}
	*strolls++
	if (*strolls-1)%6 != 0 {
		return nil
	}
	start := d.At(x.Zone, 19, 1)
	return map[string]any{
		"userId": garminUserID, "summaryId": garminSID(start.Unix()), "calendarDate": d.String(),
		"startTimeInSeconds": start.Unix(), "durationInSeconds": 1380,
		"activityType": "Walking", "activitySubType": "Walking", "offsetInSeconds": garminOffset(start),
	}
}

// snapshot is the two-minute health snapshot the watch takes on a Saturday
// evening at home: heart rate, breathing, stress, SpO2 and HRV every five
// seconds; nil on other days.
func (e *garminEmitter) snapshot(d Date) map[string]any {
	x := e.p.Day(d)
	if d.Weekday() != time.Saturday || x.Trip || x.Ill {
		return nil
	}
	start := d.At(x.Zone, 21, 30)
	a := start.Unix()
	truth := map[string]func(t time.Time) float64{
		"heart_rate":  func(t time.Time) float64 { return float64(x.HR(t)) },
		"respiration": func(time.Time) float64 { return x.Sleep.RespRate + 1.5 },
		"stress":      func(t time.Time) float64 { return float64(max(5, x.Stress(t))) },
		"spo2":        func(time.Time) float64 { return x.Sleep.SpO2Avg + 0.5 },
		"rmssd_hrv":   func(time.Time) float64 { return x.HRVRMSSDMs * 0.8 },
	}
	var summaries []any
	for _, typ := range []string{"heart_rate", "respiration", "stress", "spo2", "rmssd_hrv"} {
		vals := map[string]float64{}
		lo, hi, sum := math.Inf(1), math.Inf(-1), 0.0
		for k := int64(0); k < 120; k += 5 {
			v := e.noisy("snapshot-"+typ, a+k, truth[typ](time.Unix(a+k, 0)), 1)
			if typ == "spo2" {
				v = math.Min(100, v)
			}
			v = round(v, 1)
			vals[fmt.Sprint(k)] = v
			lo, hi, sum = math.Min(lo, v), math.Max(hi, v), sum+v
		}
		summaries = append(summaries, map[string]any{
			"summaryType": typ, "minValue": lo, "maxValue": hi, "avgValue": round(sum/24, 1), "epochSummaries": vals,
		})
	}
	return map[string]any{
		"userId": garminUserID, "summaryId": garminSID(a), "calendarDate": d.String(),
		"startTimeInSeconds": a, "durationInSeconds": 120, "startTimeOffsetInSeconds": garminOffset(start),
		"summaries": summaries,
	}
}

// --- the deliveries ----------------------------------------------------------

// deliveries is every push in posting order: for each day the daily, the
// epochs, the stress details, the night and its breathing slices, each
// activity's details and a Saturday's health snapshot, one delivery each;
// on Sundays and on the last day,
// the week's HRV, Pulse Ox, skin temperature, weigh-ins, blood pressure,
// user metrics, activities and Move IQ events, one delivery per type.
func (e *garminEmitter) deliveries() ([]Delivery, error) {
	var out []Delivery
	var failed error
	emit := func(key string, first Date, entries []map[string]any) {
		if len(entries) == 0 || failed != nil {
			return
		}
		list := make([]any, len(entries))
		for i, en := range entries {
			list[i] = en
		}
		body, err := json.Marshal(map[string]any{key: list})
		if err != nil {
			failed = fmt.Errorf("garmin: %s %s: %w", key, first, err)
			return
		}
		out = append(out, Delivery{
			Name:    key + "_" + first.String(),
			Trigger: GarminTrigger,
			Headers: map[string]string{"content-type": "application/json", "garmin-client-id": garminClientID},
			Body:    body,
		})
	}
	type item struct {
		d Date
		m map[string]any
	}
	week := map[string][]item{}
	flush := func() {
		for _, key := range garminWeekly {
			items := week[key]
			if len(items) == 0 {
				continue
			}
			entries := make([]map[string]any, len(items))
			for i, it := range items {
				entries[i] = it.m
			}
			emit(key, items[0].d, entries)
			week[key] = nil
		}
	}
	var last [2]float64
	strolls := 0
	for _, d := range e.days {
		acts := e.activities(d)
		daily, epochs, stress := e.dayOf(d, acts)
		night := e.night(d)
		emit("dailies", d, []map[string]any{daily})
		emit("epochs", d, epochs)
		emit("stressDetails", d, []map[string]any{stress})
		emit("sleeps", d, []map[string]any{night.sleep})
		emit("allDayRespiration", d, night.resp)
		for _, a := range acts {
			emit("activityDetails", d, []map[string]any{a.detail})
		}
		if snap := e.snapshot(d); snap != nil {
			emit("healthSnapshot", d, []map[string]any{snap})
		}
		week["hrv"] = append(week["hrv"], item{d, night.hrv})
		week["pulseox"] = append(week["pulseox"], item{d, night.pulseox})
		week["skinTemp"] = append(week["skinTemp"], item{d, night.skin})
		morning := e.morning(d)
		for _, key := range []string{"bodyComps", "bloodPressures"} {
			if m := morning[key]; m != nil {
				week[key] = append(week[key], item{d, m})
			}
		}
		if m := e.userMetric(d, &last); m != nil {
			week["userMetrics"] = append(week["userMetrics"], item{d, m})
		}
		for _, a := range acts {
			week["activities"] = append(week["activities"], item{d, a.activity})
		}
		if m := e.moveIQ(d, &strolls); m != nil {
			week["moveIQActivities"] = append(week["moveIQActivities"], item{d, m})
		}
		if d.Weekday() == time.Sunday || d == e.p.Options.End {
			flush()
		}
	}
	return out, failed
}
