package synth

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GoogleFit is the Google Fit REST API v1 as the provider e2e runner's mock
// serves it: the data source list, every derived data source one UTC day at
// a time, the windowed session list and the session feed, and the address
// the OAuth facility reads. The shapes are the recorded fixtures' under
// internal/providere2e/fixtures/googlefit and the fields `googlefitsync`
// reads; the devices are the fixtures' too: a Pixel Watch 2 writing heart
// rate, steps, energy, sleep and oxygen through the Fit app, a Pixel 8, a
// Withings Body+ scale and a Withings BPM Connect cuff writing through the
// Withings app, an Ascensia Contour Next One glucose meter writing through
// its app, and Strava writing the runs.
//
// The sync asks for each day as `.../datasets/<startNs>-<endNs>?limit=1000`,
// so every day with points has its own EXACT recording, holding every point
// whose interval overlaps the day, as Google answers (a sleep stage across
// UTC midnight is in both days' files, and the sync keeps it on the day it
// started). Each derived source also has one RELAXED, empty recording named
// without the day, which the mock's PATH_RELAXATIONS serves for any day
// without an exact file: the days around the range, the open day, and the
// days a sparse stream (weight, blood pressure) has nothing. A day past
// 1,000 points pages from the end, as Google does; the default life never
// needs it.
type GoogleFit struct{}

func init() { register(GoogleFit{}) }

// Name is the emitter's name.
func (GoogleFit) Name() string { return "googlefit" }

const (
	fitAPI = "/fitness/v1/users/me"
	// fitPageLimit is the `limit` the sync sends on every dataset read.
	fitPageLimit = 1000
	// fitMockPort is the port providers/googlefit/e2e.json pins: the
	// rewired function body names it, so it must not move between runs.
	fitMockPort = 48120
	// fitUserSub is the account's Google subject, the recorded fixtures'.
	fitUserSub = "104729318845510034157"
)

// The apps and devices behind the raw streams, as the recorded listing
// spells them.
var (
	fitAppFit      = map[string]any{"packageName": "com.google.android.apps.fitness"}
	fitAppGMS      = map[string]any{"packageName": "com.google.android.gms"}
	fitAppWithings = map[string]any{"packageName": "com.withings.wiscale2"}
	fitAppStrava   = map[string]any{"packageName": "com.strava", "version": "421.9"}

	fitWatch = fitDevice{"8d1f3c7a", "watch", "Pixel Watch 2", "Google"}
	fitPhone = fitDevice{"b52e90c4", "phone", "Pixel 8", "Google"}
	fitScale = fitDevice{"4a91be20", "scale", "Body+", "Withings"}
	// Fit has no type for a blood pressure cuff or a glucose meter; `unknown`
	// is what one reports.
	fitCuff  = fitDevice{"7c03d5e1", "unknown", "BPM Connect", "Withings"}
	fitMeter = fitDevice{"5e2a91c7", "unknown", "Contour Next One", "Ascensia"}

	fitAppContour = map[string]any{"packageName": "com.ascensia.contour"}
)

type fitDevice struct{ uid, kind, model, manufacturer string }

func (d fitDevice) doc() map[string]any {
	return map[string]any{"uid": d.uid, "type": d.kind, "version": "", "model": d.model, "manufacturer": d.manufacturer}
}

// fitRawID is a raw stream's dataStreamId: type, data type, app, then the
// device's manufacturer, model and uid, then the stream name.
func fitRawID(dtype string, app map[string]any, dev *fitDevice, stream string) string {
	parts := []string{"raw", dtype, app["packageName"].(string)}
	if dev != nil {
		parts = append(parts, dev.manufacturer, dev.model, dev.uid)
	}
	return strings.Join(append(parts, stream), ":")
}

func fitDerivedID(dtype, stream string) string {
	return "derived:" + dtype + ":com.google.android.gms:" + stream
}

// fitFields is each data type's `field[]`: name, format, optional.
var fitFields = map[string][][3]string{
	"com.google.step_count.delta":      {{"steps", "integer"}},
	"com.google.step_count.cumulative": {{"steps", "integer"}},
	"com.google.heart_rate.bpm":        {{"bpm", "floatPoint"}},
	"com.google.weight":                {{"weight", "floatPoint"}},
	"com.google.body.fat.percentage":   {{"percentage", "floatPoint"}},
	"com.google.height":                {{"height", "floatPoint"}},
	"com.google.sleep.segment":         {{"sleep_segment_type", "integer"}},
	"com.google.calories.expended":     {{"calories", "floatPoint"}},
	"com.google.calories.bmr":          {{"calories", "floatPoint"}},
	"com.google.distance.delta":        {{"distance", "floatPoint"}},
	"com.google.activity.segment":      {{"activity", "integer"}},
	"com.google.heart_minutes":         {{"intensity", "floatPoint"}},
	"com.google.active_minutes":        {{"duration", "integer"}},
	"com.google.oxygen_saturation": {
		{"oxygen_saturation", "floatPoint"},
		{"supplemental_oxygen_flow_rate", "floatPoint"},
		{"oxygen_therapy_administration_mode", "integer", "optional"},
		{"oxygen_saturation_system", "integer", "optional"},
		{"oxygen_saturation_measurement_method", "integer", "optional"},
	},
	"com.google.blood_pressure": {
		{"blood_pressure_systolic", "floatPoint"},
		{"blood_pressure_diastolic", "floatPoint"},
		{"body_position", "integer", "optional"},
		{"blood_pressure_measurement_location", "integer", "optional"},
	},
	"com.google.blood_glucose": {
		{"blood_glucose_level", "floatPoint"},
		{"temporal_relation_to_meal", "integer", "optional"},
		{"meal_type", "integer", "optional"},
		{"temporal_relation_to_sleep", "integer", "optional"},
		{"blood_glucose_specimen_source", "integer", "optional"},
	},
}

// The derived streams: every derived source of a data type a toggle covers,
// which is what the sync reads, plus distance, which it lists and never
// reads (the bundle asks for no location scope). In listing order.
var fitDerived = []struct{ key, dtype, stream string }{
	{"steps", "com.google.step_count.delta", "estimated_steps"},
	{"stepsMerge", "com.google.step_count.delta", "merge_step_deltas"},
	{"hr", "com.google.heart_rate.bpm", "merge_heart_rate_bpm"},
	{"rhr", "com.google.heart_rate.bpm", "resting_heart_rate<-merge_heart_rate_bpm"},
	{"weight", "com.google.weight", "merge_weight"},
	{"fat", "com.google.body.fat.percentage", "merged"},
	{"height", "com.google.height", "merge_height"},
	{"sleep", "com.google.sleep.segment", "merged"},
	{"kcal", "com.google.calories.expended", "merge_calories_expended"},
	{"bmr", "com.google.calories.bmr", "merged"},
	{"dist", "com.google.distance.delta", "merge_distance_delta"},
	{"seg", "com.google.activity.segment", "merge_activity_segments"},
	{"heartMin", "com.google.heart_minutes", "merge_heart_minutes"},
	{"activeMin", "com.google.active_minutes", "merge_active_minutes"},
	{"spo2", "com.google.oxygen_saturation", "merged"},
	{"bp", "com.google.blood_pressure", "merged"},
	{"glucose", "com.google.blood_glucose", "merged"},
}

// The raw streams the points name as their origin, and the listing holds.
var (
	fitRawSteps      = fitRawID("com.google.step_count.delta", fitAppFit, &fitWatch, "")
	fitRawPhoneSteps = "raw:com.google.step_count.cumulative:Google:Pixel 8:b52e90c4:Step Counter"
	fitRawHR         = fitRawID("com.google.heart_rate.bpm", fitAppFit, &fitWatch, "")
	fitRawCuffHR     = fitRawID("com.google.heart_rate.bpm", fitAppWithings, &fitCuff, "")
	fitRawWeight     = fitRawID("com.google.weight", fitAppWithings, &fitScale, "")
	fitRawFat        = fitRawID("com.google.body.fat.percentage", fitAppWithings, &fitScale, "")
	fitRawHeight     = fitRawID("com.google.height", fitAppFit, nil, "user_input")
	fitRawSleep      = fitRawID("com.google.sleep.segment", fitAppFit, &fitWatch, "")
	fitRawKcal       = fitRawID("com.google.calories.expended", fitAppFit, &fitWatch, "")
	fitRawDist       = fitRawID("com.google.distance.delta", fitAppFit, &fitWatch, "")
	fitRawStravaDist = fitRawID("com.google.distance.delta", fitAppStrava, nil, "")
	fitRawSeg        = fitRawID("com.google.activity.segment", fitAppFit, &fitWatch, "")
	fitRawStravaSeg  = fitRawID("com.google.activity.segment", fitAppStrava, nil, "")
	fitRawSpO2       = fitRawID("com.google.oxygen_saturation", fitAppFit, &fitWatch, "")
	fitRawBP         = fitRawID("com.google.blood_pressure", fitAppWithings, &fitCuff, "")
	fitRawGlucose    = fitRawID("com.google.blood_glucose", fitAppContour, &fitMeter, "")
)

// Google's activity codes.
const (
	fitStill    = 3
	fitWalking  = 7
	fitSleeping = 72
)

var fitActivity = map[Sport]int{Running: 8, Cycling: 1, Strength: 80, Yoga: 100, Walking: 7}

// fitSleepCodes is the persona's stage as Fit's sleep segment type.
var fitSleepCodes = map[Stage]int{Awake: 1, Light: 4, Deep: 5, REM: 6}

// fitPoint is one point and its span in epoch milliseconds.
type fitPoint struct {
	start, end int64
	doc        map[string]any
}

type fitStream struct {
	sid, dtype string
	points     []fitPoint
}

func (s *fitStream) add(start, end int64, origin string, values []any, modified int64) {
	s.points = append(s.points, fitPoint{start, end, map[string]any{
		"startTimeNanos":     strconv.FormatInt(start*1_000_000, 10),
		"endTimeNanos":       strconv.FormatInt(end*1_000_000, 10),
		"dataTypeName":       s.dtype,
		"originDataSourceId": origin,
		"value":              values,
		"modifiedTimeMillis": strconv.FormatInt(modified, 10),
	}})
}

func fitFp(v float64) map[string]any { return map[string]any{"fpVal": v, "mapVal": []any{}} }
func fitIv(v int64) map[string]any   { return map[string]any{"intVal": v, "mapVal": []any{}} }

// fitUnset is a positional value entry that sets nothing, as Google sends
// an optional field it has no value for.
func fitUnset() map[string]any { return map[string]any{"mapVal": []any{}} }

func fitMs(t time.Time) int64 { return t.UnixMilli() }

// fitUpload is when a batch sync uploads a point ending at ms: the next
// `every` boundary plus a lag.
func fitUpload(ms int64, every, lag time.Duration) int64 {
	step := every.Milliseconds()
	return (ms/step+1)*step + lag.Milliseconds()
}

type fitEmitter struct {
	p       *Persona
	days    []Date
	streams map[string]*fitStream
}

func (e *fitEmitter) noisy(what, key string, v, sd float64) float64 {
	return e.p.Noisy("googlefit|"+what+"|"+key, v, sd)
}

// fitNights is the night that ended on the day's morning and its naps.
func fitNights(day *Day) []*Sleep {
	var out []*Sleep
	if day.Sleep != nil {
		out = append(out, day.Sleep)
	}
	return append(out, day.Naps...)
}

// Emit writes recordings/ and e2e.json.
func (GoogleFit) Emit(p *Persona) (*Output, error) {
	e := &fitEmitter{p: p, days: p.Days(), streams: map[string]*fitStream{}}
	for _, d := range fitDerived {
		e.streams[d.key] = &fitStream{sid: fitDerivedID(d.dtype, d.stream), dtype: d.dtype}
	}
	e.heartRate()
	e.steps()
	e.energy()
	e.body()
	e.oxygenAndPressure()
	e.sleep()
	e.activity()

	out := &Output{Setup: Setup{Samples: []string{"samples.substrate.reamde.dev/healthgooglefit"}}}
	add := func(name string, body any) {
		out.Files = append(out.Files, File{Path: "recordings/" + name, Body: JSON(body)})
	}
	for _, d := range fitDerived {
		for name, body := range e.stream(e.streams[d.key]) {
			add(name, body)
		}
	}
	add(RecordingName("GET", fitAPI+"/dataSources"), fitDataSources())

	sessions := e.sessions()
	add(RecordingName("GET", fitAPI+"/sessions"), map[string]any{"session": sessions, "deletedSession": []any{}})
	// The feed with no token answers what changed in the 30 days before the
	// generation's "today", the day after the range.
	today := e.p.Options.End.Add(1).Midnight(time.UTC)
	floor := fitMs(today.AddDate(0, 0, -30))
	recent := []any{}
	var newest int64
	for _, s := range sessions {
		mod, _ := strconv.ParseInt(s["modifiedTimeMillis"].(string), 10, 64)
		newest = max(newest, mod)
		if mod >= floor {
			recent = append(recent, s)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].(map[string]any)["modifiedTimeMillis"].(string) < recent[j].(map[string]any)["modifiedTimeMillis"].(string)
	})
	token := strconv.FormatInt(newest+1, 10)
	add(RecordingName("GET", fitAPI+"/sessions", Pair{"includeDeleted", "true"}),
		map[string]any{"session": recent, "deletedSession": []any{}, "nextPageToken": token})
	add(RecordingName("GET", fitAPI+"/sessions", Pair{"includeDeleted", "true"}, Pair{"pageToken", token}),
		map[string]any{"session": []any{}, "deletedSession": []any{}, "nextPageToken": token})
	add(RecordingName("GET", "/oauth2/v3/userinfo"),
		map[string]any{"sub": fitUserSub, "email": p.Person.Email, "email_verified": true})

	sort.SliceStable(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	out.Files = append(out.Files, File{Path: "e2e.json", Body: JSON(fitE2E(p))})
	return out, nil
}

// fitBackfill is the smallest backfillDepth whose walk still reaches the UTC
// day before the range's first local day (an east-of-UTC morning starts
// there) when the data is loaded within a week of the range's end. The
// default 90 days need 91 UTC days and so `last1y`: `last90d` would drop the
// first night and the first hours.
func fitBackfill(p *Persona) string {
	need := p.Options.End.Sub(p.Options.Start) + 2 + 7
	switch {
	case need <= 30:
		return "last30d"
	case need <= 90:
		return "last90d"
	}
	return "last1y"
}

// fitE2E is providers/googlefit/e2e.json for this data: the recorded one's
// hosts, config, triggers, continuation and port, every toggle on, the
// owner's zone, a backfill that reaches the whole range, and the mock's time
// shift off, so the points land on the days the other emitters describe.
func fitE2E(p *Persona) map[string]any {
	return map[string]any{
		"_generated": fmt.Sprintf("internal/synth (googlefit.go): %s to %s, seed %q. The runner reads this in place of the recorded e2e.json.",
			p.Options.Start, p.Options.End, p.Options.Seed),
		"_hosts": "Google serves the consent at accounts.google.com, the token exchange at oauth2.googleapis.com, and both the userinfo address and the Fit API under www.googleapis.com.",
		"hosts":  []any{"https://accounts.google.com", "https://oauth2.googleapis.com", "https://www.googleapis.com"},
		"config": map[string]any{"clientId": "e2e-client-id", "clientSecret": "e2e-client-secret"},
		"account": map[string]any{
			"enabledActivity": true, "enabledBody": true, "enabledHeartRate": true, "enabledSleep": true,
			"enabledOxygenSaturation": true, "enabledBloodPressure": true, "enabledBloodGlucose": true,
			"timezone": p.Options.HomeZone, "syncFrequency": "hourly", "backfillDepth": fitBackfill(p),
		},
		"_triggers": "googlefit-on-connect drives the first sync, googlefit-on-request a re-drive; googlefit-scheduled stays off so a catch-up fire cannot race the drain.",
		"triggers":  []any{"googlefit-on-connect", "googlefit-on-request"},
		"_settleSeconds": "a ceiling, not a delay: the 90-day walk is about 1,500 reads (6,000 under last1y) and takes 20 to 90 s; in a repository " +
			"other loaders write to, the runner waits out the whole ceiling, because the lag it waits on counts their entries too.",
		"settleSeconds":   300,
		"_continueRounds": "a walk the 75 s chain budget cuts off reports `ok (N pending)`, and each round stamps syncRequestedAt once more.",
		"continueRounds":  20,
		"continue":        map[string]any{"property": "syncRequestedAt", "value": "now"},
		"mockPort":        fitMockPort,
		"_timeShift":      "the recordings describe the generated days, beside the other emitters' data for the same days, so the mock serves their instants as written.",
		"timeShift":       false,
	}
}

// --- the streams -------------------------------------------------------------

func (e *fitEmitter) heartRate() {
	hr, rhr := e.streams["hr"], e.streams["rhr"]
	for _, d := range e.days {
		day := e.p.Day(d)
		bias := e.noisy("hrbias", d.String(), 0, 1)
		for _, s := range day.HRSeries(5 * time.Minute) {
			if day.WorkoutAt(s.At) != nil {
				continue
			}
			key := s.At.Format(time.RFC3339)
			at := fitMs(s.At) + int64(e.p.rng("googlefit", "hrjit", s.At.Unix()).IntN(20))*1000
			v := math.Max(35, math.Min(210, math.Round(e.noisy("hr", key, s.Value+bias, 1.2))))
			hr.add(at, at, fitRawHR, []any{fitFp(v)}, fitUpload(at, 30*time.Minute, 2*time.Minute))
		}
		for _, w := range day.Workouts {
			for _, s := range w.HRSeries(time.Minute) {
				at := fitMs(s.At)
				v := math.Max(35, math.Min(210, math.Round(e.noisy("wohr", s.At.Format(time.RFC3339), s.Value+bias, 1.5))))
				hr.add(at, at, fitRawHR, []any{fitFp(v)}, fitMs(w.End)+25*60_000)
			}
		}
		if bp := day.BloodPressure; bp != nil {
			at := fitMs(bp.At) + 41_000
			hr.add(at, at, fitRawCuffHR, []any{fitFp(math.Round(e.noisy("cuffpulse", d.String(), float64(bp.Pulse), 1)))}, at+95_000)
		}
		if day.Sleep != nil {
			at := fitMs(day.Sleep.End)
			v := round(e.noisy("rhr", d.String(), float64(day.RestingHR), 0.8), 1)
			rhr.add(at, at, rhr.sid, []any{fitFp(v)}, at+3*3600_000)
		}
	}
}

func (e *fitEmitter) steps() {
	steps, merged, dist := e.streams["steps"], e.streams["stepsMerge"], e.streams["dist"]
	scale := 1 + e.p.Noisy("googlefit|stepscale", 0.05, 0.01)           // the watch counts about 5 % more
	mergeScale := 1 + e.p.Noisy("googlefit|mergestepscale", 0.03, 0.01) // Google's merge of watch and phone
	for _, d := range e.days {
		day := e.p.Day(d)
		for _, b := range day.StepsSeries(15 * time.Minute) {
			if b.Value <= 0 {
				continue
			}
			key, n := b.Start.Format(time.RFC3339), float64(b.Value)
			v := int64(math.Round(e.noisy("steps", key, n*scale, math.Max(1, n*0.01))))
			v2 := int64(math.Round(e.noisy("msteps", key, n*mergeScale, math.Max(1, n*0.01))))
			sa, sb := fitMs(b.Start), fitMs(b.End)
			up := fitUpload(sb, 30*time.Minute, 2*time.Minute)
			if v > 0 {
				steps.add(sa, sb, fitRawSteps, []any{fitIv(v)}, up)
			}
			if v2 > 0 {
				merged.add(sa, sb, fitRawSteps, []any{fitIv(v2)}, up)
			}
			// A stride on foot; a run or a ride carries its own distance below.
			if w := day.WorkoutAt(b.Start); w == nil || (w.Sport != Running && w.Sport != Cycling) {
				dist.add(sa, sb, fitRawDist, []any{fitFp(round(float64(v)*StrideM, 1))}, up)
			}
		}
		for _, w := range day.Workouts {
			if w.DistanceM > 0 && (w.Sport == Running || w.Sport == Cycling) {
				origin := fitRawDist
				if w.Sport == Running {
					origin = fitRawStravaDist
				}
				dist.add(fitMs(w.Start), fitMs(w.End), origin, []any{fitFp(round(w.DistanceM, 1))}, fitMs(w.End)+25*60_000)
			}
		}
	}
}

// energy is the hour's total energy: a 24th of the basal rate, the steps'
// share and each workout's energy pro rata to its overlap with the hour.
func (e *fitEmitter) energy() {
	kcal, bmr := e.streams["kcal"], e.streams["bmr"]
	scale := 1 + e.p.Noisy("googlefit|kcalscale", 0.03, 0.01)
	for _, d := range e.days {
		day := e.p.Day(d)
		base := float64(day.BMRKcal()) / 24
		for a := day.Start(); a.Before(day.EndTime()); a = a.Add(time.Hour) {
			b := a.Add(time.Hour)
			v := base + KcalPerStep*float64(day.StepsIn(a, b))
			for _, w := range day.Workouts {
				lo, hi := fitMaxTime(a, w.Start), fitMinTime(b, w.End)
				if hi.After(lo) {
					v += float64(w.Kcal) * hi.Sub(lo).Seconds() / math.Max(1, w.Duration().Seconds())
				}
			}
			v = round(v*scale*(1+e.noisy("kcal", a.Format(time.RFC3339), 0, 0.01)), 3)
			kcal.add(fitMs(a), fitMs(b), fitRawKcal, []any{fitFp(v)}, fitUpload(fitMs(b), 30*time.Minute, 2*time.Minute))
		}
		lo, hi := fitMs(day.Start()), fitMs(day.EndTime())
		bmr.add(lo, hi, "derived:com.google.calories.bmr:com.google.android.gms:from_height&weight",
			[]any{fitFp(round(float64(day.BMRKcal())*float64(hi-lo)/86_400_000, 3))}, lo+6*3600_000)
	}
}

func fitMaxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func fitMinTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// body is the scale's weigh-ins, 0.15 kg heavy, the body fat beside each,
// and the height typed in once.
func (e *fitEmitter) body() {
	weight, fat, height := e.streams["weight"], e.streams["fat"], e.streams["height"]
	for _, d := range e.days {
		day := e.p.Day(d)
		if day.WeightKg == 0 {
			continue
		}
		at := fitMs(day.WeighInAt) + 17_000
		weight.add(at, at, fitRawWeight, []any{fitFp(round(e.noisy("weight", d.String(), day.WeightKg+0.15, 0.05), 2))}, at+4*60_000)
		if day.BodyFatPct != 0 {
			fat.add(at, at, fitRawFat, []any{fitFp(round(e.noisy("fat", d.String(), day.BodyFatPct+0.4, 0.15), 1))}, at+4*60_000)
		}
	}
	first := e.days[0]
	at := fitMs(first.At(e.p.Zone(first), 9, 12))
	height.add(at, at, fitRawHeight, []any{fitFp(e.p.Person.HeightM)}, at+60_000)
}

// oxygenAndPressure is the watch's nightly saturation average at wake, the
// cuff's Sunday reading, sitting (2), left upper arm (3), and the glucose
// meter's reading in mmol/L, fasting (2), from capillary blood (2).
func (e *fitEmitter) oxygenAndPressure() {
	spo2, bp, glucose := e.streams["spo2"], e.streams["bp"], e.streams["glucose"]
	for _, d := range e.days {
		day := e.p.Day(d)
		if day.Sleep != nil {
			at := fitMs(day.Sleep.End)
			v := math.Min(100, round(e.noisy("spo2", d.String(), day.Sleep.SpO2Avg, 0.3), 1))
			spo2.add(at, at, fitRawSpO2, []any{fitFp(v), fitFp(0), fitUnset(), fitUnset(), fitUnset()}, at+40*60_000)
		}
		if r := day.BloodPressure; r != nil {
			at := fitMs(r.At)
			bp.add(at, at, fitRawBP, []any{
				fitFp(math.Round(e.noisy("sys", d.String(), float64(r.Systolic), 1))),
				fitFp(math.Round(e.noisy("dia", d.String(), float64(r.Diastolic), 1))),
				fitIv(2), fitIv(3),
			}, at+95_000)
		}
		if g := day.Glucose; g != nil {
			at := fitMs(g.At)
			glucose.add(at, at, fitRawGlucose, []any{fitFp(g.MmolL), fitIv(2), fitUnset(), fitUnset(), fitIv(2)}, at+2*60_000)
		}
	}
}

func (e *fitEmitter) sleep() {
	s := e.streams["sleep"]
	for _, d := range e.days {
		for _, night := range fitNights(e.p.Day(d)) {
			up := fitMs(night.End) + 10*60_000
			for _, st := range night.Stages {
				if a, b := fitMs(st.Start), fitMs(st.End); b > a {
					s.add(a, b, fitRawSleep, []any{fitIv(int64(fitSleepCodes[st.Stage]))}, up)
				}
			}
		}
	}
}

type fitLabel struct {
	start, end time.Time
	code       int
	origin     string
	rank       int
}

// activity is a continuous cover of each local day: sleeping 72, a
// workout's own code, walking 7 where a quarter hour holds a walk, still 3
// everywhere else; then active minutes over every bout that is neither
// still nor asleep, and heart points over the same bouts (one a minute
// moderate, two vigorous, none for yoga).
func (e *fitEmitter) activity() {
	seg, active, heart := e.streams["seg"], e.streams["activeMin"], e.streams["heartMin"]
	for _, d := range e.days {
		day := e.p.Day(d)
		lo, hi := day.Start(), day.EndTime()
		var labels []fitLabel
		sleeps := fitNights(day)
		if next := d.Add(1); !next.After(e.p.Options.End) && e.p.Day(next).Sleep != nil {
			sleeps = append(sleeps, e.p.Day(next).Sleep)
		}
		for _, sl := range sleeps {
			labels = append(labels, fitLabel{fitMaxTime(lo, sl.Start), fitMinTime(hi, sl.End), fitSleeping, fitRawSeg, 1})
		}
		for _, w := range day.Workouts {
			origin := fitRawSeg
			if w.Sport == Running {
				origin = fitRawStravaSeg
			}
			labels = append(labels, fitLabel{w.Start, w.End, fitActivity[w.Sport], origin, 2})
		}
		for _, b := range day.StepsSeries(15 * time.Minute) {
			if b.Value >= 900 {
				labels = append(labels, fitLabel{b.Start, b.End, fitWalking, fitRawSeg, 0})
			}
		}
		cutSet := map[int64]time.Time{lo.UnixNano(): lo, hi.UnixNano(): hi}
		for _, l := range labels {
			for _, t := range []time.Time{l.start, l.end} {
				if !t.Before(lo) && !t.After(hi) {
					cutSet[t.UnixNano()] = t
				}
			}
		}
		cuts := make([]time.Time, 0, len(cutSet))
		for _, t := range cutSet {
			cuts = append(cuts, t)
		}
		sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
		var spans []fitLabel
		for i := 0; i+1 < len(cuts); i++ {
			a, b := cuts[i], cuts[i+1]
			best := fitLabel{code: fitStill, origin: fitRawSeg, rank: -1}
			for _, l := range labels {
				if !l.start.After(a) && !l.end.Before(b) && l.rank > best.rank {
					best = l
				}
			}
			if n := len(spans); n > 0 && spans[n-1].code == best.code && spans[n-1].origin == best.origin && spans[n-1].end.Equal(a) {
				spans[n-1].end = b
				continue
			}
			spans = append(spans, fitLabel{a, b, best.code, best.origin, 0})
		}
		for _, sp := range spans {
			sa, sb := fitMs(sp.start), fitMs(sp.end)
			if sb <= sa {
				continue
			}
			up := fitUpload(sb, time.Hour, 5*time.Minute)
			seg.add(sa, sb, sp.origin, []any{fitIv(int64(sp.code))}, up)
			if sp.code == fitStill || sp.code == fitSleeping {
				continue
			}
			active.add(sa, sb, "derived:com.google.active_minutes:com.google.android.gms:from_activity_segments",
				[]any{fitIv(sb - sa)}, up)
			perMin := 1.0
			switch sp.code {
			case fitActivity[Running], fitActivity[Cycling]:
				perMin = 2
			case fitActivity[Yoga]:
				perMin = 0
			}
			if perMin > 0 {
				heart.add(sa, sb, "derived:com.google.heart_minutes:com.google.android.gms:from_heart_rate<-merge_heart_rate_bpm",
					[]any{fitFp(round(perMin*float64(sb-sa)/60_000, 1))}, up)
			}
		}
	}
}

// --- sessions ----------------------------------------------------------------

// fitStravaName is Strava's default title by the hour a run starts, or the
// name the runner gave a session she does every week.
func (e *fitEmitter) stravaName(w *Workout) string {
	switch w.Name {
	case "intervals":
		return "Thursday intervals"
	case "long run":
		return "Sunday long run"
	}
	switch h := w.Start.In(e.p.Zone(w.Date)).Hour(); {
	case h < 11:
		return "Morning Run"
	case h < 14:
		return "Lunch Run"
	case h < 17:
		return "Afternoon Run"
	case h < 21:
		return "Evening Run"
	}
	return "Night Run"
}

// sessions is one sleep session per night and nap from the Fit app, the
// runs from Strava under Strava's own ids, and the other workouts from the
// Fit app, sorted by start.
func (e *fitEmitter) sessions() []map[string]any {
	ms := func(v int64) string { return strconv.FormatInt(v, 10) }
	var out []map[string]any
	for _, d := range e.days {
		day := e.p.Day(d)
		for _, night := range fitNights(day) {
			a, b := fitMs(night.Start), fitMs(night.End)
			name := "Sleep"
			if night.IsNap {
				name = "Nap"
			}
			out = append(out, map[string]any{
				"id": "Sleep" + ms(a), "name": name, "description": "",
				"startTimeMillis": ms(a), "endTimeMillis": ms(b), "modifiedTimeMillis": ms(b + 12*60_000),
				"application": fitAppFit, "activityType": fitSleeping,
			})
		}
		for _, w := range day.Workouts {
			a, b := fitMs(w.Start), fitMs(w.End)
			code := fitActivity[w.Sport]
			if w.Sport == Running {
				out = append(out, map[string]any{
					"id": fmt.Sprintf("strava-%d", 15_000_000_000+a/10_000), "name": e.stravaName(w), "description": "",
					"startTimeMillis": ms(a), "endTimeMillis": ms(b), "modifiedTimeMillis": ms(b + (8+a%23)*60_000),
					"application": fitAppStrava, "activityType": code,
					"activeTimeMillis": ms(int64(float64(b-a)*0.97) / 1000 * 1000),
				})
				continue
			}
			name := map[Sport]string{Cycling: "Evening ride", Strength: "Strength training", Yoga: "Yoga"}[w.Sport]
			movingShare := 1.0
			if w.Sport == Cycling {
				movingShare = 0.93
			}
			out = append(out, map[string]any{
				"id": fmt.Sprintf("%d-%d", a, code), "name": name, "description": "",
				"startTimeMillis": ms(a), "endTimeMillis": ms(b), "modifiedTimeMillis": ms(b + 11*60_000),
				"application": fitAppFit, "activityType": code,
				"activeTimeMillis": ms(int64(float64(b-a)*movingShare) / 1000 * 1000),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["startTimeMillis"].(string) < out[j]["startTimeMillis"].(string)
	})
	return out
}

// --- the data source list and the per-day recordings ------------------------

func fitSourceDoc(sid string, dev *fitDevice, app map[string]any, streamName string) map[string]any {
	parts := strings.SplitN(sid, ":", 4)
	var fields []any
	for _, f := range fitFields[parts[1]] {
		entry := map[string]any{"name": f[0], "format": f[1]}
		if f[2] == "optional" {
			entry["optional"] = true
		}
		fields = append(fields, entry)
	}
	doc := map[string]any{
		"dataStreamId": sid, "type": parts[0],
		"dataType":            map[string]any{"name": parts[1], "field": fields},
		"dataQualityStandard": []any{},
	}
	if streamName != "" {
		doc["dataStreamName"] = streamName
	}
	if app != nil {
		doc["application"] = app
	}
	if dev != nil {
		doc["device"] = dev.doc()
	}
	return doc
}

// fitDataSources is the listing: every derived stream, then the raw ones the
// points name as their origin.
func fitDataSources() map[string]any {
	var docs []any
	for _, d := range fitDerived {
		docs = append(docs, fitSourceDoc(fitDerivedID(d.dtype, d.stream), nil, fitAppGMS, d.stream))
	}
	for _, r := range []struct {
		sid    string
		dev    *fitDevice
		app    map[string]any
		stream string
	}{
		{fitRawSteps, &fitWatch, fitAppFit, ""},
		{fitRawPhoneSteps, &fitPhone, nil, "Step Counter"},
		{fitRawHR, &fitWatch, fitAppFit, ""},
		{fitRawCuffHR, &fitCuff, fitAppWithings, ""},
		{fitRawWeight, &fitScale, fitAppWithings, ""},
		{fitRawFat, &fitScale, fitAppWithings, ""},
		{fitRawHeight, nil, fitAppFit, "user_input"},
		{fitRawSleep, &fitWatch, fitAppFit, ""},
		{fitRawKcal, &fitWatch, fitAppFit, ""},
		{fitRawDist, &fitWatch, fitAppFit, ""},
		{fitRawStravaDist, nil, fitAppStrava, ""},
		{fitRawSeg, &fitWatch, fitAppFit, ""},
		{fitRawStravaSeg, nil, fitAppStrava, ""},
		{fitRawSpO2, &fitWatch, fitAppFit, ""},
		{fitRawBP, &fitCuff, fitAppWithings, ""},
		{fitRawGlucose, &fitMeter, fitAppContour, ""},
	} {
		docs = append(docs, fitSourceDoc(r.sid, r.dev, r.app, r.stream))
	}
	return map[string]any{"dataSource": docs}
}

// fitDatasetPath is the dataset read's path, with its day window or without.
func fitDatasetPath(sid string, window ...int64) string {
	p := fitAPI + "/dataSources/" + sid + "/datasets"
	if len(window) == 2 {
		p += fmt.Sprintf("/%d-%d", window[0], window[1])
	}
	return p
}

// fitOverlaps is Google's rule: a point is in a range when its interval
// overlaps it, an instantaneous one when it lies inside it.
func fitOverlaps(s, e, lo, hi int64) bool {
	if s == e {
		return lo <= s && s < hi
	}
	return s < hi && e > lo
}

// stream is one derived source's recordings by name: the relaxed empty
// file, and an exact file per UTC day that has points, paged from the end
// past fitPageLimit points.
func (e *fitEmitter) stream(s *fitStream) map[string]any {
	files := map[string]any{
		RecordingName("GET", fitDatasetPath(s.sid)): map[string]any{"dataSourceId": s.sid, "point": []any{}},
	}
	if len(s.points) == 0 {
		return files
	}
	first, last := s.points[0].start, s.points[0].end
	for _, pt := range s.points {
		first, last = min(first, pt.start), max(last, pt.end)
	}
	day := time.UnixMilli(first).UTC().Truncate(24 * time.Hour)
	end := time.UnixMilli(last).UTC().Truncate(24 * time.Hour)
	for ; !day.After(end); day = day.AddDate(0, 0, 1) {
		lo, hi := fitMs(day), fitMs(day.AddDate(0, 0, 1))
		var pts []fitPoint
		for _, pt := range s.points {
			if fitOverlaps(pt.start, pt.end, lo, hi) {
				pts = append(pts, pt)
			}
		}
		if len(pts) == 0 {
			continue
		}
		sort.SliceStable(pts, func(i, j int) bool {
			if pts[i].end != pts[j].end {
				return pts[i].end < pts[j].end
			}
			return pts[i].start < pts[j].start
		})
		loNs, hiNs := lo*1_000_000, hi*1_000_000
		path := fitDatasetPath(s.sid, loNs, hiNs)
		token := ""
		for rest := pts; len(rest) > 0; {
			cut := max(0, len(rest)-fitPageLimit)
			page := rest[cut:]
			rest = rest[:cut]
			points := make([]any, 0, len(page))
			for _, pt := range page {
				points = append(points, pt.doc)
			}
			doc := map[string]any{
				"minStartTimeNs": strconv.FormatInt(loNs, 10), "maxEndTimeNs": strconv.FormatInt(hiNs, 10),
				"dataSourceId": s.sid, "point": points,
			}
			if len(rest) > 0 {
				doc["nextPageToken"] = page[0].doc["startTimeNanos"]
			}
			pairs := []Pair{{"limit", strconv.Itoa(fitPageLimit)}}
			if token != "" {
				pairs = append(pairs, Pair{"pageToken", token})
			}
			files[RecordingName("GET", path, pairs...)] = doc
			token, _ = doc["nextPageToken"].(string)
		}
	}
	return files
}
