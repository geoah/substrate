// Package synth generates synthetic provider data for one invented person,
// in the exact shapes the shipped provider functions read: WHOOP and Google
// Fit API responses as mock recordings, Garmin push envelopes and Health
// Auto Export deliveries as webhook requests, and two lab panels as records.
//
// Everything derives from one Persona: a deterministic model of a body over
// a date range, so four emitters run independently and still describe one
// life, and every value is reproducible from the seed. Each emitter adds
// its own device noise on top (Noisy), so the sources disagree by a
// plausible margin instead of agreeing to the decimal.
//
// The contract tests in internal/providertest drive the emitted data through
// the real functions, so a change to a provider's shape that the emitter
// does not follow fails there, not in a demo.
package synth

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Date is a civil date without a zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate builds a Date.
func NewDate(y int, m time.Month, d int) Date { return Date{y, m, d} }

// ParseDate reads YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, err
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// Add moves the date by n days.
func (d Date) Add(n int) Date {
	t := time.Date(d.Year, d.Month, d.Day+n, 0, 0, 0, 0, time.UTC)
	return Date{t.Year(), t.Month(), t.Day()}
}

// Before reports d < o.
func (d Date) Before(o Date) bool {
	return d.Year < o.Year || d.Year == o.Year && (d.Month < o.Month || d.Month == o.Month && d.Day < o.Day)
}

// After reports d > o.
func (d Date) After(o Date) bool { return o.Before(d) }

// Weekday is the day of the week.
func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

// Sub is the number of days from o to d.
func (d Date) Sub(o Date) int {
	a := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
	b := time.Date(o.Year, o.Month, o.Day, 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b).Hours() / 24)
}

// Midnight is the start of the date in loc.
func (d Date) Midnight(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// At is a wall-clock time on the date in loc; minutes may overflow or go
// negative.
func (d Date) At(loc *time.Location, hh, mm int) time.Time {
	return time.Date(d.Year, d.Month, d.Day, hh, mm, 0, 0, loc)
}

// Span is an inclusive date range with an optional zone.
type Span struct {
	Start, End Date
	Zone       string
}

// Contains reports whether d lies in the span.
func (s Span) Contains(d Date) bool { return !d.Before(s.Start) && !d.After(s.End) }

// Options shape a Persona. The zero value is Ada's default life: 90 days
// ending 2026-10-11, Athens, a cold in late August and a trip to London in
// September.
type Options struct {
	Start, End Date
	Seed       string
	HomeZone   string
	Trip       *Span
	Illness    *Span
}

// Person is who the data is about.
type Person struct {
	Name, FirstName, LastName, Email string
	Sex                              string
	BirthDate                        Date
	HeightM                          float64
	MaxHR                            int
	BMRKcal                          int
}

// Ada is the default person.
var Ada = Person{
	Name: "Ada Lovelace", FirstName: "Ada", LastName: "Lovelace", Email: "ada@zephyr-lane.example",
	Sex: "female", BirthDate: NewDate(1991, time.December, 10), HeightM: 1.68, MaxHR: 188, BMRKcal: 1420,
}

// DefaultOptions is the life the demo and the tests use.
func DefaultOptions() Options {
	return Options{
		Start:    NewDate(2026, time.July, 14),
		End:      NewDate(2026, time.October, 11),
		Seed:     "ada",
		HomeZone: "Europe/Athens",
		Trip:     &Span{NewDate(2026, time.September, 20), NewDate(2026, time.September, 24), "Europe/London"},
		Illness:  &Span{NewDate(2026, time.August, 26), NewDate(2026, time.August, 29), ""},
	}
}

// Persona is one person's deterministic life over a date range.
type Persona struct {
	Options Options
	Person  Person
	home    *time.Location
	trip    *time.Location
	cache   map[Date]*Day
}

// NewPersona builds the person; Options fields left zero take the defaults.
func NewPersona(o Options) (*Persona, error) {
	def := DefaultOptions()
	if o.Start == (Date{}) {
		o.Start = def.Start
	}
	if o.End == (Date{}) {
		o.End = def.End
	}
	if o.Seed == "" {
		o.Seed = def.Seed
	}
	if o.HomeZone == "" {
		o.HomeZone = def.HomeZone
	}
	if o.Trip == nil && o.Start == def.Start && o.End == def.End {
		o.Trip = def.Trip
	}
	if o.Illness == nil && o.Start == def.Start && o.End == def.End {
		o.Illness = def.Illness
	}
	if o.End.Before(o.Start) {
		return nil, fmt.Errorf("synth: end %s is before start %s", o.End, o.Start)
	}
	home, err := time.LoadLocation(o.HomeZone)
	if err != nil {
		return nil, fmt.Errorf("synth: home zone: %w", err)
	}
	p := &Persona{Options: o, Person: Ada, home: home, cache: map[Date]*Day{}}
	if o.Trip != nil && o.Trip.Zone != "" {
		if p.trip, err = time.LoadLocation(o.Trip.Zone); err != nil {
			return nil, fmt.Errorf("synth: trip zone: %w", err)
		}
	}
	return p, nil
}

// Days is every date in the range, in order.
func (p *Persona) Days() []Date {
	var out []Date
	for d := p.Options.Start; !d.After(p.Options.End); d = d.Add(1) {
		out = append(out, d)
	}
	return out
}

// Zone is the zone the person is in on d.
func (p *Persona) Zone(d Date) *time.Location {
	if p.trip != nil && p.Options.Trip.Contains(d) {
		return p.trip
	}
	return p.home
}

// Home is the home zone.
func (p *Persona) Home() *time.Location { return p.home }

// Ill reports whether d falls in the illness.
func (p *Persona) Ill(d Date) bool { return p.Options.Illness != nil && p.Options.Illness.Contains(d) }

// Trip reports whether d falls in the trip.
func (p *Persona) Trip(d Date) bool { return p.Options.Trip != nil && p.Options.Trip.Contains(d) }

// Index is the day's position in the range, 0 for the first.
func (p *Persona) Index(d Date) int { return d.Sub(p.Options.Start) }

func (p *Persona) span() int { return max(1, p.Options.End.Sub(p.Options.Start)) }

// rng is a generator seeded by the key, so every value is reproducible.
func (p *Persona) rng(key ...any) *rand.Rand {
	h := sha256.Sum256([]byte(p.Options.Seed + "|" + fmt.Sprint(key...)))
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(h[:8]), binary.BigEndian.Uint64(h[8:16])))
}

// Noisy is value plus Gaussian noise keyed on key: one device's own reading
// of a true value. The same key always yields the same reading.
func (p *Persona) Noisy(key string, value, sd float64) float64 {
	return value + p.rng("noisy", key).NormFloat64()*sd
}

// Sample is one measured instant.
type Sample struct {
	At    time.Time
	Value float64
}

// Bucket is a count over a half-open span.
type Bucket struct {
	Start, End time.Time
	Value      int
}

// Stage is a sleep stage.
type Stage string

// The sleep stages.
const (
	Awake Stage = "awake"
	Light Stage = "light"
	Deep  Stage = "deep"
	REM   Stage = "rem"
)

// StageSpan is one stretch of a stage.
type StageSpan struct {
	Start, End time.Time
	Stage      Stage
}

// Sleep is one sleep, the night ending on Date's morning or a nap.
type Sleep struct {
	Date         Date
	Start, End   time.Time
	Stages       []StageSpan
	LatencyS     int
	Efficiency   float64 // 0..1
	RestingHR    int
	HRAvg        int
	HRVRMSSDMs   float64
	SpO2Avg      float64
	RespRate     float64
	SkinTempDevC float64
	IsNap        bool
	persona      *Persona
}

// Duration is in bed.
func (s *Sleep) Duration() time.Duration { return s.End.Sub(s.Start) }

// StageTotals is seconds per stage.
func (s *Sleep) StageTotals() map[Stage]int {
	out := map[Stage]int{Awake: 0, Light: 0, Deep: 0, REM: 0}
	for _, st := range s.Stages {
		out[st.Stage] += int(st.End.Sub(st.Start).Seconds())
	}
	return out
}

// AsleepS is the seconds not awake.
func (s *Sleep) AsleepS() int {
	t := s.StageTotals()
	return t[Light] + t[Deep] + t[REM]
}

// StageAt is the stage at instant t, Light when t falls between spans.
func (s *Sleep) StageAt(t time.Time) Stage {
	for _, st := range s.Stages {
		if !t.Before(st.Start) && t.Before(st.End) {
			return st.Stage
		}
	}
	return Light
}

// HR is the heart rate while asleep at instant t.
func (s *Sleep) HR(t time.Time) int {
	base := map[Stage]float64{Awake: 8, Light: 0, Deep: -3, REM: 5}[s.StageAt(t)]
	v := float64(s.RestingHR) + base + s.persona.rng("sleephr", s.Date, t.Unix()/60).NormFloat64()*1.5
	return max(40, int(math.Round(v)))
}

// Sport is a workout's kind.
type Sport string

// The sports.
const (
	Running  Sport = "running"
	Cycling  Sport = "cycling"
	Strength Sport = "strength"
	Yoga     Sport = "yoga"
	Walking  Sport = "walking"
)

// Workout is one session.
type Workout struct {
	Date       Date
	Start, End time.Time
	Sport      Sport
	Name       string  // easy run | intervals | long run | strength | yoga | ride
	DistanceM  float64 // 0 where the sport has none
	AvgHR      int
	MaxHR      int
	Kcal       int
	Strain     float64 // WHOOP-style 0..21
	persona    *Persona
}

// Duration is the session length.
func (w *Workout) Duration() time.Duration { return w.End.Sub(w.Start) }

// HR is the heart rate during the workout at t: a warm-up ramp, a body at
// the average, intervals on interval days, a cool-down at the end.
func (w *Workout) HR(t time.Time) int {
	dur := math.Max(1, w.Duration().Seconds())
	el := t.Sub(w.Start).Seconds()
	f := el / dur
	r := w.persona.rng("wohr", w.Date, string(w.Sport), int(el/30))
	avg, mx := float64(w.AvgHR), float64(w.MaxHR)
	var v float64
	switch {
	case f < 0.08:
		v = 95 + (avg-95)*(f/0.08)
	case f > 0.93:
		v = avg - (avg-110)*((f-0.93)/0.07)
	case w.Name == "intervals":
		if int(el/180)%2 == 0 {
			v = mx - 6
		} else {
			v = avg - 18
		}
	default:
		v = avg + 6*math.Sin(f*7)
	}
	return int(math.Round(math.Min(mx, v+r.NormFloat64()*2.5)))
}

// HRSeries samples HR every step over the workout.
func (w *Workout) HRSeries(step time.Duration) []Sample {
	var out []Sample
	for t := w.Start; t.Before(w.End); t = t.Add(step) {
		out = append(out, Sample{t, float64(w.HR(t))})
	}
	return out
}

// StepsPerMin is the cadence of the sport.
func (w *Workout) StepsPerMin() int {
	return map[Sport]int{Running: 168, Walking: 108, Cycling: 0, Strength: 12, Yoga: 2}[w.Sport]
}

// BloodPressure is one reading.
type BloodPressure struct {
	Systolic, Diastolic, Pulse int
	At                         time.Time
}

// Day is the ground truth for one local day.
type Day struct {
	Date          Date
	Zone          *time.Location
	Sleep         *Sleep
	Naps          []*Sleep
	Workouts      []*Workout
	RestingHR     int
	HRVRMSSDMs    float64
	WeightKg      float64 // 0 when no weigh-in
	WeighInAt     time.Time
	BodyFatPct    float64
	BloodPressure *BloodPressure
	VO2Max        float64
	FitnessAge    int
	Recovery      int // WHOOP-style 0..100
	Readiness     int // 0..100
	Ill, Trip     bool
	persona       *Persona
}

// Start is local midnight.
func (d *Day) Start() time.Time { return d.Date.Midnight(d.Zone) }

// EndTime is where the next day starts: its local midnight, in its own zone.
// A day the zone changes is longer or shorter than 24 hours, so the days
// tile time and every emitter samples each instant once.
func (d *Day) EndTime() time.Time {
	next := d.Date.Add(1)
	return next.Midnight(d.persona.Zone(next))
}

// AsleepAt is the sleep covering t, if any: the night that ended this
// morning, a nap, or the night that ends tomorrow morning, which begins
// before this day ends.
func (d *Day) AsleepAt(t time.Time) *Sleep {
	all := append([]*Sleep{}, d.Naps...)
	if d.Sleep != nil {
		all = append(all, d.Sleep)
	}
	if next := d.persona.Day(d.Date.Add(1)).Sleep; next != nil {
		all = append(all, next)
	}
	for _, s := range all {
		if !t.Before(s.Start) && t.Before(s.End) {
			return s
		}
	}
	return nil
}

// DayAt is the local day whose span holds t.
func (p *Persona) DayAt(t time.Time) *Day {
	u := t.UTC()
	d := NewDate(u.Year(), u.Month(), u.Day())
	for _, c := range []Date{d.Add(-1), d, d.Add(1)} {
		if day := p.Day(c); !t.Before(day.Start()) && t.Before(day.EndTime()) {
			return day
		}
	}
	return p.Day(d)
}

// HR is the heart rate at t without device noise, whichever day and night
// hold it.
func (p *Persona) HR(t time.Time) int { return p.DayAt(t).HR(t) }

// WorkoutAt is the workout covering t, if any.
func (d *Day) WorkoutAt(t time.Time) *Workout {
	for _, w := range d.Workouts {
		if !t.Before(w.Start) && t.Before(w.End) {
			return w
		}
	}
	return nil
}

func gauss(x, mu, width float64) float64 { return math.Exp(-((x - mu) * (x - mu)) / width) }

// HR is the heart rate at t without device noise.
func (d *Day) HR(t time.Time) int {
	if s := d.AsleepAt(t); s != nil {
		return s.HR(t)
	}
	if w := d.WorkoutAt(t); w != nil {
		return w.HR(t)
	}
	lt := t.In(d.Zone)
	h := float64(lt.Hour()) + float64(lt.Minute())/60
	base := float64(d.RestingHR) + 14
	base += 10*gauss(h, 8.6, 0.4) + 6*gauss(h, 13.2, 0.3) + 8*gauss(h, 19.3, 0.6)
	if h >= 22.5 {
		base -= 5
	}
	for _, w := range d.Workouts {
		after := t.Sub(w.End).Seconds()
		if after >= 0 && after < 3600 {
			base += 18 * (1 - after/3600)
		}
	}
	r := d.persona.rng("dayhr", d.Date, lt.Hour(), lt.Minute()/5)
	return int(math.Round(base + r.NormFloat64()*3 + 2*math.Sin(h*3.1)))
}

// HRSeries samples the whole local day every step.
func (d *Day) HRSeries(step time.Duration) []Sample {
	var out []Sample
	end := d.EndTime()
	for t := d.Start(); t.Before(end); t = t.Add(step) {
		out = append(out, Sample{t, float64(d.HR(t))})
	}
	return out
}

func (d *Day) stepsMinute(t time.Time) int {
	if d.AsleepAt(t) != nil {
		return 0
	}
	u := t.UTC()
	r := d.persona.rng("steps", d.Date, u.Hour(), u.Minute())
	if w := d.WorkoutAt(t); w != nil {
		return max(0, int(math.Round(float64(w.StepsPerMin())+r.NormFloat64()*6)))
	}
	lt := t.In(d.Zone)
	h := float64(lt.Hour()) + float64(lt.Minute())/60
	weekend := lt.Weekday() == time.Saturday || lt.Weekday() == time.Sunday
	var rate float64
	switch {
	case h >= 8.25 && h < 8.5 && !weekend && !d.Trip:
		rate = 100 // walk to the office
	case h >= 13.0 && h < 13.3:
		rate = 75 // lunch walk
	case h >= 17.75 && h < 18.0 && !weekend && !d.Trip:
		rate = 100 // walk home
	case h >= 19.0 && h < 19.4 && d.persona.rng("stroll", d.Date).Float64() < 0.55:
		rate = 85 // an evening stroll, most days
	case d.Trip && h >= 10 && h < 18:
		if d.persona.rng("tripwalk", d.Date, lt.Hour(), lt.Minute()/10).Float64() < 0.6 {
			rate = 30 // a city on foot
		}
	case weekend && h >= 10.5 && h < 12.0:
		rate = 40
	case h >= 7 && h < 23:
		if r.Float64() < 0.4 {
			rate = 5
		}
	}
	if d.Ill {
		rate *= 0.25
	}
	if rate == 0 {
		return 0
	}
	return max(0, int(math.Round(rate*(1+r.NormFloat64()*0.25))))
}

// StepsIn counts steps in [a, b).
func (d *Day) StepsIn(a, b time.Time) int {
	total := 0
	for t := a; t.Before(b); t = t.Add(time.Minute) {
		total += d.stepsMinute(t)
	}
	return total
}

// StepsSeries buckets the local day every step.
func (d *Day) StepsSeries(step time.Duration) []Bucket {
	var out []Bucket
	end := d.EndTime()
	for t := d.Start(); t.Before(end); t = t.Add(step) {
		nxt := t.Add(step)
		if nxt.After(end) {
			nxt = end
		}
		out = append(out, Bucket{t, nxt, d.StepsIn(t, nxt)})
	}
	return out
}

// StepsTotal is the day's steps.
func (d *Day) StepsTotal() int {
	total := 0
	for _, b := range d.StepsSeries(15 * time.Minute) {
		total += b.Value
	}
	return total
}

// DistanceM is the day's walking and running distance.
func (d *Day) DistanceM() float64 { return math.Round(float64(d.StepsTotal())*0.74*10) / 10 }

// ActiveKcal is the day's active energy.
func (d *Day) ActiveKcal() int {
	k := float64(d.StepsTotal()) * 0.04
	for _, w := range d.Workouts {
		k += float64(w.Kcal)
	}
	return int(math.Round(k))
}

// BMRKcal is the day's basal energy.
func (d *Day) BMRKcal() int {
	if d.Ill {
		return d.persona.Person.BMRKcal + 5
	}
	return d.persona.Person.BMRKcal
}

// Floors is flights climbed.
func (d *Day) Floors() int {
	n := d.persona.rng("floors", d.Date).IntN(10) + 2
	if d.Ill {
		return int(float64(n) * 0.3)
	}
	return n
}

// Stress is a Garmin-style stress level 0..100 at t, -1 while a workout
// runs (not measured).
func (d *Day) Stress(t time.Time) int {
	if s := d.AsleepAt(t); s != nil {
		v := 18.0
		if d.persona.Ill(s.Date) {
			v += 12
		}
		return max(5, int(math.Round(v+d.persona.rng("stress", s.Date, t.Unix()/180).NormFloat64()*5)))
	}
	if d.WorkoutAt(t) != nil {
		return -1
	}
	lt := t.In(d.Zone)
	h := float64(lt.Hour()) + float64(lt.Minute())/60
	base := 28.0
	if h >= 9 && h < 18 && lt.Weekday() >= time.Monday && lt.Weekday() <= time.Friday {
		base += 12
	}
	if d.Ill {
		base += 10
	}
	base += 10*gauss(h, 11, 1.5) + 8*gauss(h, 16, 1.2)
	v := base + d.persona.rng("stress", d.Date, t.Unix()/180).NormFloat64()*8
	return int(math.Max(5, math.Min(95, v)))
}

// BodyBattery is a Garmin-style body battery 0..100 at t: charged by sleep,
// drained by the day.
func (d *Day) BodyBattery(t time.Time) int {
	lt := t.In(d.Zone)
	h := float64(lt.Hour()) + float64(lt.Minute())/60
	top := 92.0
	if d.Ill {
		top -= 25
	}
	if d.Recovery < 50 {
		top -= 6
	}
	wake := 7.1
	if lt.Weekday() == time.Saturday || lt.Weekday() == time.Sunday {
		wake = 8.7
	}
	var v float64
	if h < wake {
		v = top - 10 + 10*(h/wake)
	} else {
		v = top - (top-22)*math.Pow((h-wake)/(24-wake), 0.8)
		for _, w := range d.Workouts {
			if !w.End.After(t) {
				v -= math.Min(18, w.Strain*1.2)
			}
		}
	}
	v += d.persona.rng("bb", d.Date, lt.Hour()).NormFloat64() * 2
	return int(math.Max(5, math.Min(100, v)))
}

// SpO2Night samples oxygen saturation through the night every step.
func (d *Day) SpO2Night(step time.Duration) []Sample {
	if d.Sleep == nil {
		return nil
	}
	var out []Sample
	for t := d.Sleep.Start; t.Before(d.Sleep.End); t = t.Add(step) {
		v := d.Sleep.SpO2Avg + d.persona.rng("spo2", d.Date, t.Unix()/60).NormFloat64()*0.9
		out = append(out, Sample{t, math.Round(math.Min(100, v)*10) / 10})
	}
	return out
}

// RespirationNight samples breathing rate through the night every step.
func (d *Day) RespirationNight(step time.Duration) []Sample {
	if d.Sleep == nil {
		return nil
	}
	var out []Sample
	for t := d.Sleep.Start; t.Before(d.Sleep.End); t = t.Add(step) {
		v := d.Sleep.RespRate + d.persona.rng("resp", d.Date, t.Unix()/60).NormFloat64()*0.6
		out = append(out, Sample{t, math.Round(v*10) / 10})
	}
	return out
}

// Strain is a WHOOP-style day strain 0..21 from the workouts and the walking.
func (d *Day) Strain() float64 {
	s := 3.5 + 0.00025*float64(d.StepsTotal())
	for _, w := range d.Workouts {
		s += w.Strain * 0.75
	}
	if d.Ill {
		s *= 0.6
	}
	return math.Round(math.Min(21, s)*10) / 10
}

// --- building a day ---------------------------------------------------------

func (p *Persona) longRunOn(d Date) bool {
	return d.Weekday() == time.Sunday && !p.Ill(d) && !p.Trip(d)
}

func (p *Persona) restingHR(d Date) int {
	i := float64(p.Index(d))
	base := 53 - 2.5*i/float64(p.span())
	if p.Ill(d) {
		base += 9
	}
	if y := d.Add(-1); !y.Before(p.Options.Start) && p.longRunOn(y) {
		base += 2
	}
	return int(math.Round(base + p.rng("rhr", d).NormFloat64()*1.3))
}

func (p *Persona) hrv(d Date, longRunYesterday bool) float64 {
	i := float64(p.Index(d))
	base := 58 + 7*i/float64(p.span())
	if p.Ill(d) {
		base -= 20
	}
	if longRunYesterday {
		base -= 9
	}
	return math.Round(math.Max(18, base+p.rng("hrv", d).NormFloat64()*8)*10) / 10
}

// sleepFor is the night that ends on the morning of d.
func (p *Persona) sleepFor(d Date, ill, trip, longRunYesterday bool) *Sleep {
	r := p.rng("sleep", d)
	loc := p.Zone(d)
	weekend := d.Weekday() == time.Saturday || d.Weekday() == time.Sunday
	bedMin := 23*60 + 15
	if weekend {
		bedMin = 24*60 + 10
	}
	bedMin += int(r.NormFloat64() * 25)
	if trip {
		bedMin += 35
	}
	start := d.Add(-1).At(loc, 0, bedMin)
	wakeMin := 7*60 + 5
	if weekend {
		wakeMin = 8*60 + 40
	}
	wakeMin += int(r.NormFloat64() * 15)
	if ill {
		wakeMin += 50
	}
	if trip {
		wakeMin += 20
	}
	end := d.At(loc, 0, wakeMin)
	if !end.After(start.Add(4 * time.Hour)) {
		end = start.Add(6*time.Hour + 30*time.Minute)
	}
	latMean := 11.0
	if ill {
		latMean = 22
	}
	latency := int(math.Max(3, latMean+r.NormFloat64()*5)) * 60
	var stages []StageSpan
	t := start
	stages = append(stages, StageSpan{t, t.Add(time.Duration(latency) * time.Second), Awake})
	t = t.Add(time.Duration(latency) * time.Second)
	total := end.Sub(start).Seconds()
	for cycle := 0; t.Before(end); cycle++ {
		f := t.Sub(start).Seconds() / math.Max(1, total)
		deep := math.Max(300, (28-22*f)*60+r.NormFloat64()*240)
		rem := math.Max(300, (10+24*f)*60+r.NormFloat64()*240)
		light := math.Max(600, 5400-deep-rem+r.NormFloat64()*300)
		wake := 0.0
		if cycle > 0 {
			wake = r.NormFloat64()*60 + 90
		}
		if ill {
			wake += 240
			deep *= 0.8
		}
		for _, part := range []struct {
			st   Stage
			secs float64
		}{{Light, light * 0.6}, {Deep, deep}, {Light, light * 0.4}, {REM, rem}, {Awake, math.Max(0, wake)}} {
			if part.secs <= 0 || !t.Before(end) {
				continue
			}
			secs := math.Floor(math.Min(part.secs, end.Sub(t).Seconds()))
			if secs < 1 {
				continue
			}
			nxt := t.Add(time.Duration(secs) * time.Second)
			stages = append(stages, StageSpan{t, nxt, part.st})
			t = nxt
		}
	}
	var merged []StageSpan
	for _, s := range stages {
		if n := len(merged); n > 0 && merged[n-1].Stage == s.Stage && merged[n-1].End.Equal(s.Start) {
			merged[n-1].End = s.End
		} else if s.End.After(s.Start) {
			merged = append(merged, s)
		}
	}
	asleep := 0.0
	for _, s := range merged {
		if s.Stage != Awake {
			asleep += s.End.Sub(s.Start).Seconds()
		}
	}
	rhr := p.restingHR(d)
	hrAvg := rhr + 4
	spo2, resp, skin := 96.8, 14.2, 0.0
	if ill {
		hrAvg += 3
		spo2 -= 1.2
		resp += 1.8
		skin = 0.9
	}
	return &Sleep{
		Date: d, Start: start, End: end, Stages: merged, LatencyS: latency,
		Efficiency:   math.Round(asleep/total*1000) / 1000,
		RestingHR:    rhr,
		HRAvg:        hrAvg,
		HRVRMSSDMs:   p.hrv(d, longRunYesterday),
		SpO2Avg:      math.Round((spo2+r.NormFloat64()*0.5)*10) / 10,
		RespRate:     math.Round((resp+r.NormFloat64()*0.5)*10) / 10,
		SkinTempDevC: math.Round((skin+r.NormFloat64()*0.25)*100) / 100,
		persona:      p,
	}
}

// workoutsFor is the day's training. A session never starts sooner than 15
// to 35 minutes after the night ends at wake, so a late weekend morning
// pushes the long run back instead of overlapping the night.
func (p *Persona) workoutsFor(d Date, ill, trip bool, wake time.Time) []*Workout {
	if ill {
		return nil
	}
	r := p.rng("workouts", d)
	loc := p.Zone(d)
	week := p.Index(d) / 7
	earliest := wake.Add(time.Duration(15+p.rng("afterwake", d).IntN(21)) * time.Minute)
	mk := func(hh, mm, minutes int, sport Sport, dist float64, avg, mx, kcal int, strain float64, name string) *Workout {
		s := d.At(loc, hh, mm+int(r.NormFloat64()*6))
		if s.Before(earliest) {
			s = earliest
		}
		e := s.Add(time.Duration(minutes+int(r.NormFloat64()*3)) * time.Minute)
		return &Workout{
			Date: d, Start: s, End: e, Sport: sport, Name: name, DistanceM: math.Round(dist), AvgHR: avg,
			MaxHR: min(p.Person.MaxHR, mx), Kcal: kcal, Strain: strain, persona: p,
		}
	}
	if trip {
		if d == (Date{2026, time.September, 22}) {
			return []*Workout{mk(7, 40, 38, Running, 6400+r.NormFloat64()*200, 146, 164, 380, 10.4, "easy run")}
		}
		return nil
	}
	switch d.Weekday() {
	case time.Tuesday:
		return []*Workout{mk(7, 30, 44+int(r.NormFloat64()*4), Running, 7800+r.NormFloat64()*350, 144+int(r.NormFloat64()*3), 162, 430, 10.8, "easy run")}
	case time.Thursday:
		return []*Workout{mk(18, 30, 42, Running, 7100+r.NormFloat64()*300, 158, 182, 470, 14.6, "intervals")}
	case time.Saturday:
		return []*Workout{mk(10, 0, 52, Strength, 0, 112+int(r.NormFloat64()*4), 148, 260, 7.9, "strength")}
	case time.Sunday:
		minutes := 70 + min(30, week*3)
		km := 11.5 + math.Min(6.5, float64(week)*0.6)
		return []*Workout{mk(8, 30, minutes, Running, km*1000+r.NormFloat64()*400, 150, 171, int(float64(minutes)*10.4), 16.8, "long run")}
	case time.Monday:
		if r.Float64() < 0.75 {
			return []*Workout{mk(19, 0, 32, Yoga, 0, 84, 104, 95, 3.6, "yoga")}
		}
	case time.Wednesday:
		if week%2 == 0 {
			return []*Workout{mk(18, 5, 61, Cycling, 24500+r.NormFloat64()*1500, 138, 166, 520, 12.1, "ride")}
		}
	}
	return nil
}

// Day is the ground truth for d, built once and cached.
func (p *Persona) Day(d Date) *Day {
	if x, ok := p.cache[d]; ok {
		return x
	}
	ill, trip := p.Ill(d), p.Trip(d)
	longRunY := p.longRunOn(d.Add(-1))
	sleep := p.sleepFor(d, ill, trip, longRunY)
	r := p.rng("day", d)
	loc := p.Zone(d)
	var naps []*Sleep
	if (ill && r.Float64() < 0.7) || (d.Weekday() == time.Sunday && r.Float64() < 0.25) {
		ns := d.At(loc, 14, 30+int(r.NormFloat64()*20))
		ne := ns.Add(time.Duration(math.Max(15, 35+r.NormFloat64()*10)) * time.Minute)
		naps = append(naps, &Sleep{
			Date: d, Start: ns, End: ne, IsNap: true, LatencyS: 240, Efficiency: 0.9,
			Stages:    []StageSpan{{ns, ns.Add(4 * time.Minute), Awake}, {ns.Add(4 * time.Minute), ne, Light}},
			RestingHR: sleep.RestingHR, HRAvg: sleep.RestingHR + 5, HRVRMSSDMs: sleep.HRVRMSSDMs,
			SpO2Avg: sleep.SpO2Avg, RespRate: sleep.RespRate, persona: p,
		})
	}
	i := float64(p.Index(d))
	span := float64(p.span())
	day := &Day{
		Date: d, Zone: loc, Sleep: sleep, Naps: naps, Workouts: p.workoutsFor(d, ill, trip, sleep.End),
		RestingHR: sleep.RestingHR, HRVRMSSDMs: sleep.HRVRMSSDMs, Ill: ill, Trip: trip, persona: p,
	}
	switch d.Weekday() {
	case time.Monday, time.Wednesday, time.Friday, time.Saturday:
		if !trip {
			day.WeighInAt = day.atRest(d.At(loc, 7, 10+int(r.NormFloat64()*4)), 4*time.Minute)
			w := 73.4 - 1.8*i/span + r.NormFloat64()*0.28
			if ill {
				w += 0.4
			}
			day.WeightKg = math.Round(w*100) / 100
			day.BodyFatPct = math.Round((27.5-1.1*i/span+r.NormFloat64()*0.35)*10) / 10
		}
	case time.Sunday:
		if !trip {
			at := day.atRest(d.At(loc, 8, int(r.NormFloat64()*10)), 12*time.Minute)
			day.BloodPressure = &BloodPressure{int(118 + r.NormFloat64()*4), int(76 + r.NormFloat64()*3), sleep.RestingHR + 6 + int(r.NormFloat64()*3), at}
		}
	}
	day.VO2Max = math.Round((47.0+1.6*i/span)*10) / 10
	day.FitnessAge = 29
	if i >= span*2/3 {
		day.FitnessAge = 28
	}
	rec := 55 + (sleep.HRVRMSSDMs-(58+7*i/span))*1.6 - float64(sleep.RestingHR-int(53-2.5*i/span))*3 + (sleep.Efficiency-0.9)*120
	day.Recovery = int(math.Max(5, math.Min(99, rec+r.NormFloat64()*4)))
	day.Readiness = int(math.Max(5, math.Min(99, float64(day.Recovery)*0.6+36+r.NormFloat64()*5)))
	p.cache[d] = day
	return day
}

// atRest is the first instant from `want` that is awake and at rest: at
// least `afterWake` after the night ends, and outside every workout from
// ten minutes before it starts to half an hour after it ends.
func (d *Day) atRest(want time.Time, afterWake time.Duration) time.Time {
	at := want
	if woke := d.Sleep.End.Add(afterWake); at.Before(woke) {
		at = woke
	}
	for _, w := range d.Workouts {
		if !at.Before(w.Start.Add(-10*time.Minute)) && at.Before(w.End.Add(30*time.Minute)) {
			at = w.End.Add(30 * time.Minute)
		}
	}
	return at
}

// --- labs -------------------------------------------------------------------

// LabResult is one analyte on one panel.
type LabResult struct {
	Name      string  // the persona's analyte key
	Value     float64 // in Unit
	Unit      string  // UCUM
	Low, High float64 // the reference range; 0 is open
	HasLow    bool
	HasHigh   bool
}

// LabPanel is one report.
type LabPanel struct {
	Date    Date
	Lab     string
	Results []LabResult
}

// Labs is the two panels, independent of the date range.
func (p *Persona) Labs() []LabPanel {
	mk := func(name string, v float64, unit string, lo, hi float64, hasLo, hasHi bool) LabResult {
		return LabResult{name, v, unit, lo, hi, hasLo, hasHi}
	}
	return []LabPanel{
		{Date: NewDate(2026, time.July, 20), Lab: "Bioiatriki Athens", Results: []LabResult{
			mk("glucose_fasting", 5.4, "mmol/L", 3.9, 5.6, true, true),
			mk("hba1c", 5.3, "%", 4.0, 5.6, true, true),
			mk("cholesterol_total", 5.1, "mmol/L", 0, 5.2, false, true),
			mk("ldl_cholesterol", 3.1, "mmol/L", 0, 3.0, false, true),
			mk("hdl_cholesterol", 1.6, "mmol/L", 1.2, 0, true, false),
			mk("triglycerides", 0.9, "mmol/L", 0, 1.7, false, true),
			mk("ferritin", 24, "ug/L", 15, 150, true, true),
			mk("vitamin_d_25oh", 48, "nmol/L", 50, 125, true, true),
			mk("tsh", 1.9, "m[IU]/L", 0.4, 4.0, true, true),
			mk("hemoglobin", 131, "g/L", 120, 160, true, true),
			mk("crp", 0.8, "mg/L", 0, 5.0, false, true),
		}},
		{Date: NewDate(2026, time.October, 5), Lab: "Bioiatriki Athens", Results: []LabResult{
			mk("glucose_fasting", 5.1, "mmol/L", 3.9, 5.6, true, true),
			mk("hba1c", 5.2, "%", 4.0, 5.6, true, true),
			mk("cholesterol_total", 4.8, "mmol/L", 0, 5.2, false, true),
			mk("ldl_cholesterol", 2.8, "mmol/L", 0, 3.0, false, true),
			mk("hdl_cholesterol", 1.7, "mmol/L", 1.2, 0, true, false),
			mk("triglycerides", 0.8, "mmol/L", 0, 1.7, false, true),
			mk("ferritin", 31, "ug/L", 15, 150, true, true),
			mk("vitamin_d_25oh", 71, "nmol/L", 50, 125, true, true),
			mk("tsh", 2.1, "m[IU]/L", 0.4, 4.0, true, true),
			mk("hemoglobin", 134, "g/L", 120, 160, true, true),
			mk("crp", 0.6, "mg/L", 0, 5.0, false, true),
		}},
	}
}
