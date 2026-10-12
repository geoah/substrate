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
// `applehealthingest` and the `healthapple` adapter read.
//
// A day's delivery carries:
//
//   - heart_rate: one minute row about every 5 minutes (the watch samples at
//     irregular minutes and misses a few), Min = Avg = Max at rest and a
//     spread during a workout; source the watch.
//   - step_count: one row per 5-minute bucket with steps, from the iPhone by
//     day and from the watch before 07:00, from 22:00 and during a run.
//   - walking_running_distance, flights_climbed, active_energy and
//     basal_energy_burned: one row per local hour, at the hour, energy in kJ
//     (the recorded user's unit preference) and distance in km.
//   - resting_heart_rate (one row at local midnight), heart_rate_variability
//     (SDNN, three readings in the night), respiratory_rate (every 15
//     minutes of the night) and blood_oxygen_saturation (every 10): no
//     `source` key, as the app sends these metrics.
//   - weight_body_mass and body_fat_percentage on weigh-in mornings and
//     blood_pressure on Sundays, source "Withings".
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
)

// The cadences, as the demo brief asked for them.
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
	out := &Output{}
	for _, d := range p.Days() {
		e := &ahDay{p: p, d: d, day: p.Day(d), next: p.Day(d.Add(1))}
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

// ahRound keeps n decimals.
func ahRound(v float64, n int) float64 {
	f := math.Pow(10, float64(n))
	return math.Round(v*f) / f
}

// ahMetric is one metric object.
func ahMetric(name, units string, rows []map[string]any) map[string]any {
	return map[string]any{"name": name, "units": units, "data": rows}
}

// ahBucket is one 5-minute step bucket: the persona's steps and the device's
// reading of them.
type ahBucket struct {
	start time.Time
	steps int
	read  float64
	src   string
}

type ahDay struct {
	p         *Persona
	d         Date
	day, next *Day
}

func (e *ahDay) noisy(what string, key any, v, sd float64) float64 {
	return e.p.Noisy(fmt.Sprintf("applehealth|%s|%s|%v", what, e.d, key), v, sd)
}

func (e *ahDay) rng(key ...any) *rand.Rand {
	return e.p.rng(append([]any{"applehealth", e.d}, key...)...)
}

// date is t as the app writes it, in the day's zone.
func (e *ahDay) date(t time.Time) string { return t.In(e.day.Zone).Format(ahDate) }

// hr is the persona's heart rate at t, with the night that ends tomorrow
// morning folded in: Day.HR only knows the night that ended this morning,
// so after bedtime it would answer a waking rate.
func (e *ahDay) hr(t time.Time) int {
	if s := e.next.Sleep; s != nil && !t.Before(s.Start) && t.Before(s.End) {
		return s.HR(t)
	}
	return e.day.HR(t)
}

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
	weight, fat := e.body()
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
			v := e.noisy("hr", i, float64(e.hr(t)), 2)
			if e.day.WorkoutAt(t) != nil {
				avg := ahRound(v, 6)
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

// buckets is every 5-minute bucket with steps, and which device counted it.
func (e *ahDay) buckets() []ahBucket {
	var out []ahBucket
	i := 0
	for a := e.day.Start(); a.Before(e.day.EndTime()); a = a.Add(ahStepsStep) {
		n := e.day.StepsIn(a, a.Add(ahStepsStep))
		if n > 0 {
			src, factor := ahIPhone, 0.95
			if lt := a.In(e.day.Zone); e.running(a) || lt.Hour() < 7 || lt.Hour() >= 22 {
				src, factor = ahWatch, 1.04
			}
			v := math.Max(0, e.noisy("steps", i, float64(n)*factor, math.Max(0.6, float64(n)*0.03)))
			if v > 0 {
				out = append(out, ahBucket{a, n, v, src})
			}
		}
		i++
	}
	return out
}

func (e *ahDay) steps(buckets []ahBucket) []map[string]any {
	rows := make([]map[string]any, 0, len(buckets))
	for _, b := range buckets {
		rows = append(rows, map[string]any{"qty": ahRound(b.read, 10), "date": e.date(b.start), "source": b.src})
	}
	return rows
}

func (e *ahDay) distance(buckets []ahBucket) []map[string]any {
	var rows []map[string]any
	for k, a := range e.hours() {
		b := a.Add(time.Hour)
		meters := 0.0
		watch, phone := false, false
		for _, s := range buckets {
			if !s.start.Before(a) && s.start.Before(b) && !e.running(s.start) {
				meters += s.read * 0.74
				watch = watch || s.src == ahWatch
				phone = phone || s.src == ahIPhone
			}
		}
		for _, w := range e.day.Workouts {
			if ov := overlap(a, b, w.Start, w.End); w.Sport == Running && w.DistanceM > 0 && ov > 0 {
				meters += w.DistanceM * ov / w.Duration().Seconds()
				watch = true
			}
		}
		if meters <= 0 {
			continue
		}
		meters = math.Max(0, e.noisy("dist", k, meters, meters*0.02))
		label := ahIPhone
		switch {
		case watch && phone:
			label = ahMerged
		case watch:
			label = ahWatch
		}
		rows = append(rows, map[string]any{"qty": ahRound(meters/1000, 10), "date": e.date(a), "source": label})
	}
	return rows
}

// flights spreads the day's floors over the four hours the phone walked most.
func (e *ahDay) flights(buckets []ahBucket) []map[string]any {
	total := e.day.Floors()
	perHour := map[time.Time]float64{}
	for _, b := range buckets {
		if b.src == ahIPhone {
			lt := b.start.In(e.day.Zone)
			perHour[time.Date(lt.Year(), lt.Month(), lt.Day(), lt.Hour(), 0, 0, 0, e.day.Zone)] += b.read
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
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	var rows []map[string]any
	for _, h := range hours {
		if alloc[h] > 0 {
			rows = append(rows, map[string]any{"qty": alloc[h], "date": e.date(h), "source": ahIPhone})
		}
	}
	return rows
}

func (e *ahDay) activeEnergy(buckets []ahBucket) []map[string]any {
	var rows []map[string]any
	for k, a := range e.hours() {
		b := a.Add(time.Hour)
		kcal := 0.0
		for _, s := range buckets {
			if !s.start.Before(a) && s.start.Before(b) {
				kcal += float64(s.steps) * 0.04
			}
		}
		for _, w := range e.day.Workouts {
			kcal += float64(w.Kcal) * overlap(a, b, w.Start, w.End) / w.Duration().Seconds()
		}
		if kcal <= 0 {
			continue
		}
		kcal = math.Max(0.05, kcal*e.noisy("aekcal", k, 1, 0.04))
		rows = append(rows, map[string]any{"qty": ahRound(kcal*4.184, 10), "date": e.date(a), "source": ahWatch})
	}
	return rows
}

// basalEnergy is the day's basal energy hour by hour, a little lower in the
// small hours.
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
	scale := float64(e.day.BMRKcal()) / sum
	rows := make([]map[string]any, 0, len(hours))
	for k, a := range hours {
		kcal := e.noisy("basal", k, weights[k]*scale, 0.6)
		rows = append(rows, map[string]any{"qty": ahRound(kcal*4.184, 10), "date": e.date(a), "source": ahWatch})
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
		rows = append(rows, map[string]any{"qty": ahRound(math.Max(8, v), 6), "date": e.date(t.Truncate(time.Minute))})
	}
	return rows
}

func (e *ahDay) respiratory() []map[string]any {
	var rows []map[string]any
	for _, s := range e.day.RespirationNight(ahRespStep) {
		rows = append(rows, map[string]any{"qty": ahRound(e.noisy("resp", s.At.Unix(), s.Value, 0.3), 1), "date": e.date(s.At)})
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

// body is the weigh-in, weight and body fat at one instant.
func (e *ahDay) body() (weight, fat []map[string]any) {
	if e.day.WeightKg == 0 {
		return nil, nil
	}
	at := e.date(e.day.WeighInAt.Add(time.Duration(e.rng("weighsec").IntN(60)) * time.Second))
	return []map[string]any{{"qty": ahRound(e.noisy("weight", 0, e.day.WeightKg, 0.2), 2), "date": at, "source": ahWithings}},
		[]map[string]any{{"qty": ahRound(e.noisy("fat", 0, e.day.BodyFatPct, 0.4), 1), "date": at, "source": ahWithings}}
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
				"date": e.date(s.At.Add(lead)), "Min": mn, "Avg": ahRound(avg, 6), "Max": mx, "units": "bpm", "source": ahWatch,
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
		kcal := ahRound(e.noisy("wokcal", n, float64(w.Kcal), float64(w.Kcal)*0.04), 1)
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
			entry["totalEnergy"] = ahQty(ahRound(kcal+float64(e.p.Person.BMRKcal)/1440*w.Duration().Minutes(), 1), "kcal")
			entry["intensity"] = ahQty(met, "MET")
		}
		if w.DistanceM > 0 {
			km := e.noisy("wodist", n, w.DistanceM, w.DistanceM*0.01) / 1000
			climb := 9.0
			if w.Sport == Cycling {
				climb = 6
			}
			entry["distance"] = ahQty(ahRound(km, 3), "km")
			entry["elevationUp"] = ahQty(ahRound(math.Abs(e.noisy("woelev", n, km*climb, 8)), 1), "m")
			entry["avgSpeed"] = ahQty(ahRound(km/w.Duration().Hours(), 2), "kmph")
		}
		out = append(out, entry)
	}
	return out
}
