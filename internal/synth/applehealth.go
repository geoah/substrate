package synth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// AppleHealth is what the Health Auto Export iOS app POSTs to the
// `applehealth-webhook` trigger: one delivery per local day from an
// automation at Time Grouping Minutes, Summarize Data on, Export Version 2,
// sent at the end of the day. The shapes are the recorded deliveries under
// internal/providertest/testdata/applehealth and the fields
// `applehealthingest` and the provider's `adaptapplehealth` adapter read.
//
// A day's delivery carries:
//
//   - heart_rate: one minute row about every 5 minutes (the watch samples at
//     irregular minutes and misses a few), Min = Avg = Max at rest and a
//     spread during a workout; source the watch.
//   - step_count: one row per 5-minute bucket with steps, from the iPhone by
//     day and from the watch before 07:00, from 22:00 and during a run; on
//     about one day in four a second row from the watch for each bucket of
//     the walks to work, to lunch and home.
//   - walking_running_distance, flights_climbed, active_energy and
//     basal_energy_burned: one row per minute that has any, as the app sends
//     them at Time Grouping Minutes (basal energy every minute, distance and
//     active energy over each step bucket's five minutes and each workout's
//     minutes, floors at the minute climbed), energy in kJ (the recorded
//     user's unit preference) and distance in km.
//   - resting_heart_rate (one row at local midnight), heart_rate_variability
//     (SDNN, three readings in the night), respiratory_rate (every 15
//     minutes of the night), blood_oxygen_saturation (every 10) and
//     apple_sleeping_wrist_temperature (one at the night's end): no `source`
//     key, as the app sends these metrics.
//   - vo2_max after each run, from the watch.
//   - weight_body_mass, body_fat_percentage and body_mass_index on weigh-in
//     mornings and blood_pressure on Sundays, source "Withings";
//     blood_glucose in mg/dL on meter mornings, source "Contour"; height in
//     cm once, on the first morning, from the iPhone.
//   - sleep_analysis: the night that ended that morning as one aggregated
//     row (inBed, asleep, core, deep, rem, awake and their bounds).
//   - workouts: every persona workout under the app's activity name, with
//     heartRateData every 60 s and three heartRateRecovery minutes.
//
// Every date is the app's `yyyy-MM-dd HH:mm:ss Z` in the day's zone: the
// phone is where the day was when it sends at 23:58.
type AppleHealth struct{}

func init() { register(AppleHealth{}) }

// Name is the emitter's name.
func (AppleHealth) Name() string { return "applehealth" }

const (
	// ahTrigger is the webhook trigger the app posts to.
	ahTrigger = "applehealth-webhook"
	// ahAccountID is the account the deliveries name in X-Substrate-Account;
	// the loader creates it before the first post.
	ahAccountID = "iphone"
	// ahUserAgent is the recorded app's user agent.
	ahUserAgent = "Health%20Auto%20Export/10.0.3 CFNetwork/3860.100.1 Darwin/27.0.0"
	// ahDate is how the app writes a date.
	ahDate = "2006-01-02 15:04:05 -0700"
	// ahKeyPlaceholder stands where the owner's webhook key goes: the loader
	// replaces an Authorization header with the key it armed the trigger with.
	ahKeyPlaceholder = "Bearer <webhook key>"
)

// The persona's devices as Health names them: U+2019 in the possessive and
// U+00A0 inside "Apple Watch", as the recorded deliveries carry them.
const (
	ahWatch    = "Ada’s Apple Watch"
	ahIPhone   = "Ada’s iPhone"
	ahMerged   = ahWatch + "|" + ahIPhone
	ahWithings = "Withings"
	// ahMeter is the glucose meter's app, which writes in mg/dL.
	ahMeter = "Contour"
)

// mgdlPerMmolL is glucose's mass concentration for one mmol/L.
const mgdlPerMmolL = 18.0156

// The cadences: what the app sends at Time Grouping Minutes for a watch
// that samples heart rate about every 5 minutes at rest.
const (
	ahHRStep     = 5 * time.Minute
	ahStepsStep  = 5 * time.Minute
	ahSpO2Step   = 10 * time.Minute
	ahRespStep   = 15 * time.Minute
	ahWorkoutHRS = 60 * time.Second
)

// ahWorkoutNames is the persona's sport as the app names the activity.
var ahWorkoutNames = map[Sport]string{
	Running: "Outdoor Run", Cycling: "Outdoor Cycle", Strength: "Traditional Strength Training",
	Yoga: "Yoga", Walking: "Outdoor Walk",
}

// Emit writes one delivery per day, in posting order.
func (AppleHealth) Emit(p *Persona) (*Output, error) {
	out := &Output{Setup: Setup{
		Provider: "providers.substrate.reamde.dev/applehealth",
		Records: []map[string]any{{
			"kind":     "providers.substrate.reamde.dev/applehealth/account",
			"metadata": map[string]any{"id": ahAccountID},
			"data": map[string]any{"properties": map[string]any{
				"displayName": ahIPhone, "timezone": p.Options.HomeZone,
			}},
		}},
	}}
	for _, d := range p.Days() {
		e := &ahDay{p: p, d: d, day: p.Day(d)}
		body, err := ahBody(map[string]any{"data": map[string]any{
			"metrics":  e.metrics(),
			"workouts": e.workouts(),
		}})
		if err != nil {
			return nil, err
		}
		out.Deliveries = append(out.Deliveries, Delivery{
			Name:    d.String(),
			Trigger: ahTrigger,
			Headers: map[string]string{
				"content-type":           "application/json",
				"user-agent":             ahUserAgent,
				"authorization":          ahKeyPlaceholder,
				"x-substrate-account":    ahAccountID,
				"automation-name":        "substrate minutes",
				"automation-id":          ahUUID(p, "automation", "minutes"),
				"automation-aggregation": "minutes",
				"automation-period":      "Today",
				"session-id":             ahUUID(p, "session", d),
			},
			Body: body,
		})
	}
	return out, nil
}

// ahBody is the body as the app sends it: compact JSON, no HTML escaping.
func ahBody(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("synth: applehealth body: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// ahUUID is an upper-case v4-shaped UUID derived from the key, as HealthKit
// and the app spell their ids.
func ahUUID(p *Persona, key ...any) string {
	h := sha256.Sum256([]byte(p.Options.Seed + "|applehealth|" + fmt.Sprint(key...)))
	b := h[:16]
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	x := strings.ToUpper(hex.EncodeToString(b))
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}

// ahMetric is one metric object.
func ahMetric(name, units string, rows []map[string]any) map[string]any {
	return map[string]any{"name": name, "units": units, "data": rows}
}

// ahBucket is one 5-minute step bucket: the persona's steps and the device's
// reading of them. A second device's reading of the same bucket is `again`,
// which the hourly distance, floors and energy do not count twice.
type ahBucket struct {
	start time.Time
	steps int
	read  float64
	src   string
	again bool
}

type ahDay struct {
	p   *Persona
	d   Date
	day *Day
}

func (e *ahDay) noisy(what string, key any, v, sd float64) float64 {
	return e.p.Noisy(fmt.Sprintf("applehealth|%s|%s|%v", what, e.d, key), v, sd)
}

func (e *ahDay) rng(key ...any) *rand.Rand {
	return e.p.rng(append([]any{"applehealth", e.d}, key...)...)
}

// date is t as the app writes it, in the day's zone.
func (e *ahDay) date(t time.Time) string { return t.In(e.day.Zone).Format(ahDate) }

func (e *ahDay) running(t time.Time) bool {
	w := e.day.WorkoutAt(t)
	return w != nil && w.Sport == Running
}

func (e *ahDay) hours() []time.Time {
	var out []time.Time
	for t := e.day.Start(); t.Before(e.day.EndTime()); t = t.Add(time.Hour) {
		out = append(out, t)
	}
	return out
}

func overlap(a, b, c, d time.Time) float64 {
	lo, hi := a, b
	if c.After(lo) {
		lo = c
	}
	if d.Before(hi) {
		hi = d
	}
	return math.Max(0, hi.Sub(lo).Seconds())
}

func (e *ahDay) metrics() []any {
	buckets := e.buckets()
	weight, fat, bmi := e.body()
	all := []map[string]any{
		ahMetric("heart_rate", "count/min", e.heartRate()),
		ahMetric("step_count", "count", e.steps(buckets)),
		ahMetric("walking_running_distance", "km", e.distance(buckets)),
		ahMetric("flights_climbed", "count", e.flights(buckets)),
		ahMetric("active_energy", "kJ", e.activeEnergy(buckets)),
		ahMetric("basal_energy_burned", "kJ", e.basalEnergy()),
		ahMetric("resting_heart_rate", "count/min", e.resting()),
		ahMetric("heart_rate_variability", "ms", e.hrv()),
		ahMetric("respiratory_rate", "count/min", e.respiratory()),
		ahMetric("blood_oxygen_saturation", "%", e.spo2()),
		ahMetric("weight_body_mass", "kg", weight),
		ahMetric("body_fat_percentage", "%", fat),
		ahMetric("blood_pressure", "mmHg", e.pressure()),
		ahMetric("blood_glucose", "mg/dL", e.glucose()),
		ahMetric("vo2_max", "ml/(kg·min)", e.vo2Max()),
		ahMetric("apple_sleeping_wrist_temperature", "degC", e.wristTemperature()),
		ahMetric("body_mass_index", "count", bmi),
		ahMetric("height", "cm", e.height()),
		ahMetric("sleep_analysis", "hr", e.sleep()),
	}
	out := []any{}
	for _, m := range all {
		if len(m["data"].([]map[string]any)) > 0 {
			out = append(out, m)
		}
	}
	return out
}

func (e *ahDay) heartRate() []map[string]any {
	var rows []map[string]any
	end := e.day.EndTime()
	jitters := []int{0, 0, 1, 1, 1, 2, 3}
	i := 0
	for slot := e.day.Start(); slot.Before(end); slot = slot.Add(ahHRStep) {
		r := e.rng("hrslot", i)
		t := slot.Add(time.Duration(jitters[r.IntN(len(jitters))]) * time.Minute)
		skip := r.Float64() < 0.04
		if !skip && t.Before(end) {
			v := e.noisy("hr", i, float64(e.day.HR(t)), 2)
			if e.day.WorkoutAt(t) != nil {
				avg := round(v, 6)
				rows = append(rows, map[string]any{
					"Min": math.Floor(avg - math.Abs(r.NormFloat64()*3) - 1), "Avg": avg,
					"Max": math.Ceil(avg + math.Abs(r.NormFloat64()*3) + 1), "date": e.date(t), "source": ahWatch,
				})
			} else {
				b := math.Round(v)
				rows = append(rows, map[string]any{"Min": b, "Avg": b, "Max": b, "date": e.date(t), "source": ahWatch})
			}
		}
		i++
	}
	return rows
}

// buckets is every 5-minute bucket with steps, and which device counted it:
// the watch before 07:00, from 22:00 and on a run, the phone otherwise. On
// about one day in four the watch counts the walks to work, to lunch and
// home as well, and both readings are sent.
func (e *ahDay) buckets() []ahBucket {
	var out []ahBucket
	both := e.rng("bothdevices").Float64() < 0.25
	i := 0
	for a := e.day.Start(); a.Before(e.day.EndTime()); a = a.Add(ahStepsStep) {
		n := e.day.StepsIn(a, a.Add(ahStepsStep))
		if n > 0 {
			lt := a.In(e.day.Zone)
			src, factor := ahIPhone, 0.95
			if e.running(a) || lt.Hour() < 7 || lt.Hour() >= 22 {
				src, factor = ahWatch, 1.04
			}
			v := math.Max(0, e.noisy("steps", i, float64(n)*factor, math.Max(0.6, float64(n)*0.03)))
			if v > 0 {
				out = append(out, ahBucket{a, n, v, src, false})
			}
			if h := float64(lt.Hour()) + float64(lt.Minute())/60; both && src == ahIPhone && n >= 50 && ahWalk(h) {
				w := math.Max(0, e.noisy("watchsteps", i, float64(n)*1.04, math.Max(0.6, float64(n)*0.03)))
				if w > 0 {
					out = append(out, ahBucket{a, n, w, ahWatch, v > 0})
				}
			}
		}
		i++
	}
	return out
}

// ahWalk reports whether a local hour falls in one of the persona's walks:
// to work, to lunch, home.
func ahWalk(h float64) bool {
	return h >= 8.25 && h < 8.5 || h >= 13.0 && h < 13.3 || h >= 17.75 && h < 18.0
}

func (e *ahDay) steps(buckets []ahBucket) []map[string]any {
	rows := make([]map[string]any, 0, len(buckets))
	for _, b := range buckets {
		rows = append(rows, map[string]any{"qty": round(b.read, 10), "date": e.date(b.start), "source": b.src})
	}
	return rows
}

// minutes is every minute of the hour from a, up to the day's end.
func (e *ahDay) minutes(a time.Time) []time.Time {
	end := a.Add(time.Hour)
	if dayEnd := e.day.EndTime(); dayEnd.Before(end) {
		end = dayEnd
	}
	var out []time.Time
	for m := a; m.Before(end); m = m.Add(time.Minute) {
		out = append(out, m)
	}
	return out
}

// perMinute spreads each bucket's share over the five minutes it covers and
// each workout's over the minutes it overlaps, by minute.
func perMinute(buckets []ahBucket, share func(ahBucket) float64, workouts []*Workout, workShare func(*Workout) float64) map[time.Time]float64 {
	out := map[time.Time]float64{}
	for _, b := range buckets {
		if v := share(b); v > 0 {
			for m := b.start; m.Before(b.start.Add(ahStepsStep)); m = m.Add(time.Minute) {
				out[m] += v / ahStepsStep.Minutes()
			}
		}
	}
	for _, w := range workouts {
		v := workShare(w)
		if v <= 0 {
			continue
		}
		for m := w.Start.Truncate(time.Minute); m.Before(w.End); m = m.Add(time.Minute) {
			out[m] += v * overlap(m, m.Add(time.Minute), w.Start, w.End) / w.Duration().Seconds()
		}
	}
	return out
}

// distance is one row per minute that walked or ran, as the app sends it at
// Time Grouping Minutes: a step bucket's distance over its five minutes and
// a run's over its own, each hour's rows scaled by that hour's noise.
func (e *ahDay) distance(buckets []ahBucket) []map[string]any {
	meters := perMinute(buckets, func(s ahBucket) float64 {
		if s.again || e.running(s.start) {
			return 0
		}
		return s.read * StrideM
	}, e.day.Workouts, func(w *Workout) float64 {
		if w.Sport != Running {
			return 0
		}
		return w.DistanceM
	})
	watch, phone := map[time.Time]bool{}, map[time.Time]bool{}
	for _, s := range buckets {
		if s.again || e.running(s.start) {
			continue
		}
		for m := s.start; m.Before(s.start.Add(ahStepsStep)); m = m.Add(time.Minute) {
			watch[m] = watch[m] || s.src == ahWatch
			phone[m] = phone[m] || s.src == ahIPhone
		}
	}
	var rows []map[string]any
	for k, a := range e.hours() {
		f := math.Max(0, e.noisy("dist", k, 1, 0.02))
		for _, m := range e.minutes(a) {
			v := meters[m] * f
			if v <= 0 {
				continue
			}
			label := ahIPhone
			switch w := watch[m] || e.running(m); {
			case w && phone[m]:
				label = ahMerged
			case w:
				label = ahWatch
			}
			rows = append(rows, map[string]any{"qty": round(v/1000, 10), "date": e.date(m), "source": label})
		}
	}
	return rows
}

// flights spreads the day's floors over the four hours the phone walked
// most, each floor at the start of one of that hour's busiest phone step
// buckets, one row per minute that climbed.
func (e *ahDay) flights(buckets []ahBucket) []map[string]any {
	total := e.day.Floors()
	perHour := map[time.Time]float64{}
	inHour := map[time.Time][]ahBucket{}
	for _, b := range buckets {
		if b.src == ahIPhone {
			lt := b.start.In(e.day.Zone)
			h := time.Date(lt.Year(), lt.Month(), lt.Day(), lt.Hour(), 0, 0, 0, e.day.Zone)
			perHour[h] += b.read
			inHour[h] = append(inHour[h], b)
		}
	}
	if total <= 0 || len(perHour) == 0 {
		return nil
	}
	hours := make([]time.Time, 0, len(perHour))
	for h := range perHour {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool {
		if perHour[hours[i]] != perHour[hours[j]] {
			return perHour[hours[i]] > perHour[hours[j]]
		}
		return hours[i].Before(hours[j])
	})
	hours = hours[:min(4, len(hours))]
	alloc := map[time.Time]int{}
	r := e.rng("floors")
	for range total {
		alloc[hours[r.IntN(len(hours))]]++
	}
	byMinute := map[time.Time]int{}
	for _, h := range hours {
		bs := inHour[h]
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].read > bs[j].read })
		for n := range alloc[h] {
			byMinute[bs[n%len(bs)].start]++
		}
	}
	minutes := make([]time.Time, 0, len(byMinute))
	for m := range byMinute {
		minutes = append(minutes, m)
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i].Before(minutes[j]) })
	rows := make([]map[string]any, 0, len(minutes))
	for _, m := range minutes {
		rows = append(rows, map[string]any{"qty": byMinute[m], "date": e.date(m), "source": ahIPhone})
	}
	return rows
}

// activeEnergy is one row per minute that burned above the resting rate: a
// step bucket's energy over its five minutes and a workout's over its own,
// each hour's rows scaled by that hour's noise.
func (e *ahDay) activeEnergy(buckets []ahBucket) []map[string]any {
	kcal := perMinute(buckets, func(s ahBucket) float64 {
		if s.again {
			return 0
		}
		return float64(s.steps) * KcalPerStep
	}, e.day.Workouts, func(w *Workout) float64 { return float64(w.Kcal) })
	var rows []map[string]any
	for k, a := range e.hours() {
		f := e.noisy("aekcal", k, 1, 0.04)
		for _, m := range e.minutes(a) {
			v := kcal[m] * f
			if v <= 0 {
				continue
			}
			rows = append(rows, map[string]any{"qty": round(v*4.184, 10), "date": e.date(m), "source": ahWatch})
		}
	}
	return rows
}

// basalEnergy is the day's basal energy minute by minute, a little lower in
// the small hours. BMRKcal is a 24-hour rate, so a day the zone changes on
// burns it over its own length.
func (e *ahDay) basalEnergy() []map[string]any {
	hours := e.hours()
	weights := make([]float64, len(hours))
	sum := 0.0
	for i, a := range hours {
		weights[i] = 1.03
		if a.In(e.day.Zone).Hour() < 6 {
			weights[i] = 0.92
		}
		sum += weights[i]
	}
	scale := float64(e.day.BMRKcal()) * float64(len(hours)) / 24 / sum
	rows := make([]map[string]any, 0, len(hours)*60)
	for k, a := range hours {
		perMinute := e.noisy("basal", k, weights[k]*scale, 0.6) / 60
		for _, m := range e.minutes(a) {
			rows = append(rows, map[string]any{"qty": round(perMinute*4.184, 10), "date": e.date(m), "source": ahWatch})
		}
	}
	return rows
}

func (e *ahDay) resting() []map[string]any {
	v := math.Round(e.noisy("rhr", 0, float64(e.day.RestingHR), 1))
	return []map[string]any{{"qty": v, "date": e.date(e.day.Start())}}
}

// hrv is three SDNN readings in the night: an hour and twenty minutes in,
// the middle, and fifty minutes before waking. SDNN reads about 1.3 times
// the persona's RMSSD.
func (e *ahDay) hrv() []map[string]any {
	s := e.day.Sleep
	if s == nil {
		return nil
	}
	mid := s.Start.Add(s.End.Sub(s.Start) / 2)
	var rows []map[string]any
	for k, t := range []time.Time{s.Start.Add(80 * time.Minute), mid, s.End.Add(-50 * time.Minute)} {
		v := e.noisy("hrv", k, s.HRVRMSSDMs*1.3, s.HRVRMSSDMs*0.09)
		rows = append(rows, map[string]any{"qty": round(math.Max(8, v), 6), "date": e.date(t.Truncate(time.Minute))})
	}
	return rows
}

func (e *ahDay) respiratory() []map[string]any {
	var rows []map[string]any
	for _, s := range e.day.RespirationNight(ahRespStep) {
		rows = append(rows, map[string]any{"qty": round(e.noisy("resp", s.At.Unix(), s.Value, 0.3), 1), "date": e.date(s.At)})
	}
	return rows
}

// spo2 is in whole percent, as the watch measures it.
func (e *ahDay) spo2() []map[string]any {
	var rows []map[string]any
	for _, s := range e.day.SpO2Night(ahSpO2Step) {
		rows = append(rows, map[string]any{"qty": math.Min(100, math.Round(e.noisy("spo2", s.At.Unix(), s.Value, 0.5))), "date": e.date(s.At)})
	}
	return rows
}

// body is the weigh-in: weight, body fat and the scale's BMI at one instant.
func (e *ahDay) body() (weight, fat, bmi []map[string]any) {
	if e.day.WeightKg == 0 {
		return nil, nil, nil
	}
	at := e.date(e.day.WeighInAt.Add(time.Duration(e.rng("weighsec").IntN(60)) * time.Second))
	kg := round(e.noisy("weight", 0, e.day.WeightKg, 0.2), 2)
	h := e.p.Person.HeightM
	return []map[string]any{{"qty": kg, "date": at, "source": ahWithings}},
		[]map[string]any{{"qty": round(e.noisy("fat", 0, e.day.BodyFatPct, 0.4), 1), "date": at, "source": ahWithings}},
		[]map[string]any{{"qty": round(kg/(h*h), 1), "date": at, "source": ahWithings}}
}

// vo2Max is the watch's estimate after each outdoor run, a little under the
// persona's own figure, as a wrist estimate runs.
func (e *ahDay) vo2Max() []map[string]any {
	var rows []map[string]any
	for n, w := range e.day.Workouts {
		if w.Sport != Running {
			continue
		}
		v := round(e.noisy("vo2", n, e.day.VO2Max-1.2, 0.4), 2)
		rows = append(rows, map[string]any{"qty": v, "date": e.date(w.End.Add(2 * time.Minute)), "source": ahWatch})
	}
	return rows
}

// height is typed into the phone's Health app once, on the range's first
// morning, in the centimeters a metric locale shows.
func (e *ahDay) height() []map[string]any {
	if e.d != e.p.Options.Start {
		return nil
	}
	at := e.day.atRest(e.d.At(e.day.Zone, 9, 12), time.Minute)
	return []map[string]any{{"qty": round(e.p.Person.HeightM*100, 1), "date": e.date(at), "source": ahIPhone}}
}

// wristTemperature is the night's sleeping wrist temperature, absolute, at
// the night's end, without a source key as the app sends it.
func (e *ahDay) wristTemperature() []map[string]any {
	s := e.day.Sleep
	if s == nil {
		return nil
	}
	return []map[string]any{{"qty": round(e.noisy("wrist", 0, 35.6+s.SkinTempDevC, 0.08), 6), "date": e.date(s.End)}}
}

func (e *ahDay) pressure() []map[string]any {
	bp := e.day.BloodPressure
	if bp == nil {
		return nil
	}
	return []map[string]any{{
		"date":      e.date(bp.At.Add(30 * time.Second)),
		"systolic":  math.Round(e.noisy("sys", 0, float64(bp.Systolic), 1.5)),
		"diastolic": math.Round(e.noisy("dia", 0, float64(bp.Diastolic), 1.2)),
		"source":    ahWithings,
	}}
}

// glucose is the meter's fasting reading, in whole mg/dL as the meter
// shows it.
func (e *ahDay) glucose() []map[string]any {
	g := e.day.Glucose
	if g == nil {
		return nil
	}
	return []map[string]any{{
		"qty": math.Round(g.MmolL * mgdlPerMmolL), "date": e.date(g.At), "mealTime": "Before Meal", "source": ahMeter,
	}}
}

// sleep is the night that ended this morning as the app aggregates it, in
// hours: Core is the persona's light sleep, and `asleep` (unstaged sleep) is
// zero because the watch stages every minute.
func (e *ahDay) sleep() []map[string]any {
	s := e.day.Sleep
	if s == nil {
		return nil
	}
	tot := s.StageTotals()
	first, last := s.Start, s.End
	found := false
	for _, st := range s.Stages {
		if st.Stage != Awake {
			if !found {
				first, found = st.Start, true
			}
			last = st.End
		}
	}
	h := func(secs int) float64 { return float64(secs) / 3600 }
	return []map[string]any{{
		"date":       e.date(e.day.Start()),
		"totalSleep": h(s.AsleepS()),
		"asleep":     0,
		"core":       h(tot[Light]),
		"deep":       h(tot[Deep]),
		"rem":        h(tot[REM]),
		"awake":      h(tot[Awake]),
		"sleepStart": e.date(first),
		"sleepEnd":   e.date(last),
		"inBed":      s.Duration().Hours(),
		"inBedStart": e.date(s.Start),
		"inBedEnd":   e.date(s.End),
		"source":     ahWatch,
	}}
}

func ahQty(v float64, units string) map[string]any { return map[string]any{"qty": v, "units": units} }

func (e *ahDay) workouts() []any {
	out := []any{}
	for n, w := range e.day.Workouts {
		r := e.rng("workout", n)
		lead := time.Duration(r.IntN(60)) * time.Second
		start, end := w.Start.Add(lead), w.End.Add(time.Duration(r.IntN(60))*time.Second)
		var hrd []map[string]any
		lo, hi, sum := math.Inf(1), math.Inf(-1), 0.0
		for k, s := range w.HRSeries(ahWorkoutHRS) {
			avg := e.noisy("wohr", fmt.Sprint(n, "/", k), s.Value, 2)
			mn := math.Floor(avg - math.Abs(r.NormFloat64()*3) - 1)
			mx := math.Ceil(avg + math.Abs(r.NormFloat64()*3) + 1)
			lo, hi, sum = math.Min(lo, mn), math.Max(hi, mx), sum+avg
			hrd = append(hrd, map[string]any{
				"date": e.date(s.At.Add(lead)), "Min": mn, "Avg": round(avg, 6), "Max": mx, "units": "bpm", "source": ahWatch,
			})
		}
		var rec []map[string]any
		for k := range 3 {
			truth := float64(e.day.HR(w.End.Add(time.Duration(k)*time.Minute+time.Second))) + 20 - 7*float64(k)
			avg := math.Round(e.noisy("wohrrec", fmt.Sprint(n, "/", k), truth, 2))
			rec = append(rec, map[string]any{
				"date": e.date(end.Add(time.Duration(k) * time.Minute)), "Min": avg - 5, "Avg": avg, "Max": avg + 6,
				"units": "bpm", "source": ahWatch,
			})
		}
		avgHR := math.Round(sum / float64(max(1, len(hrd))))
		outdoor := w.Sport == Running || w.Sport == Cycling || w.Sport == Walking
		kcal := round(e.noisy("wokcal", n, float64(w.Kcal), float64(w.Kcal)*0.04), 1)
		entry := map[string]any{
			"id":                 ahUUID(e.p, "workout", e.d, n),
			"name":               ahWorkoutNames[w.Sport],
			"start":              e.date(start),
			"end":                e.date(end),
			"duration":           end.Sub(start).Truncate(time.Second).Seconds(),
			"location":           "Indoor",
			"isIndoor":           !outdoor,
			"activeEnergyBurned": ahQty(kcal, "kcal"),
			"heartRate": map[string]any{
				"min": ahQty(lo, "bpm"), "avg": ahQty(avgHR, "bpm"), "max": ahQty(hi, "bpm"),
			},
			"avgHeartRate":      ahQty(avgHR, "bpm"),
			"maxHeartRate":      ahQty(hi, "bpm"),
			"heartRateData":     hrd,
			"heartRateRecovery": rec,
			"metadata":          map[string]any{},
		}
		if outdoor {
			entry["location"] = "Outdoor"
			entry["metadata"] = map[string]any{"HKTimeZone": e.day.Zone.String()}
		}
		if w.Sport == Strength || w.Sport == Yoga {
			met := 5.2
			if w.Sport == Yoga {
				met = 2.6
			}
			entry["totalEnergy"] = ahQty(round(kcal+float64(e.p.Person.BMRKcal)/1440*w.Duration().Minutes(), 1), "kcal")
			entry["intensity"] = ahQty(met, "MET")
		}
		if w.DistanceM > 0 {
			km := e.noisy("wodist", n, w.DistanceM, w.DistanceM*0.01) / 1000
			climb := 9.0
			if w.Sport == Cycling {
				climb = 6
			}
			entry["distance"] = ahQty(round(km, 3), "km")
			entry["elevationUp"] = ahQty(round(math.Abs(e.noisy("woelev", n, km*climb, 8)), 1), "m")
			entry["avgSpeed"] = ahQty(round(km/w.Duration().Hours(), 2), "kmph")
		}
		out = append(out, entry)
	}
	return out
}
