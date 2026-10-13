package synth

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestPersonaIsDeterministic(t *testing.T) {
	a, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := NewDate(2026, time.September, 8)
	da, db := a.Day(d), b.Day(d)
	if !da.Sleep.Start.Equal(db.Sleep.Start) || da.RestingHR != db.RestingHR || da.HRVRMSSDMs != db.HRVRMSSDMs {
		t.Fatalf("two personas from the same options disagree: %+v vs %+v", da.Sleep, db.Sleep)
	}
	sa, sb := da.HRSeries(5*time.Minute), db.HRSeries(5*time.Minute)
	for i := range sa {
		if !sa[i].At.Equal(sb[i].At) || sa[i].Value != sb[i].Value {
			t.Fatalf("heart rate sample %d differs: %v vs %v", i, sa[i], sb[i])
		}
	}
	if a.Noisy("whoop|hr", 60, 2) != b.Noisy("whoop|hr", 60, 2) {
		t.Fatal("Noisy is not deterministic")
	}
	if a.Noisy("whoop|hr", 60, 2) == a.Noisy("garmin|hr", 60, 2) {
		t.Fatal("two devices read the same noise")
	}
	c, err := NewPersona(Options{Seed: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Day(d).Sleep.Start == da.Sleep.Start && c.Day(d).RestingHR == da.RestingHR {
		t.Fatal("another seed produced the same night")
	}
}

func TestPersonaDefaultLife(t *testing.T) {
	p, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	days := p.Days()
	if len(days) != 90 {
		t.Fatalf("default range has %d days, want 90", len(days))
	}
	workouts := 0
	for _, d := range days {
		day := p.Day(d)
		if day.Sleep == nil {
			t.Fatalf("%s has no sleep", d)
		}
		if !day.Sleep.End.After(day.Sleep.Start) || day.Sleep.Duration() < 4*time.Hour || day.Sleep.Duration() > 12*time.Hour {
			t.Fatalf("%s sleeps %s", d, day.Sleep.Duration())
		}
		if day.Sleep.Efficiency < 0.7 || day.Sleep.Efficiency > 1 {
			t.Fatalf("%s sleep efficiency %v", d, day.Sleep.Efficiency)
		}
		if day.RestingHR < 40 || day.RestingHR > 80 {
			t.Fatalf("%s resting heart rate %d", d, day.RestingHR)
		}
		steps := day.StepsTotal()
		if steps < 500 || steps > 30000 {
			t.Fatalf("%s walks %d steps", d, steps)
		}
		if day.Ill && steps > 6000 {
			t.Fatalf("%s is ill and walks %d steps", d, steps)
		}
		if day.Ill && len(day.Workouts) > 0 {
			t.Fatalf("%s is ill and trains", d)
		}
		for _, w := range day.Workouts {
			if !w.End.After(w.Start) || w.AvgHR > w.MaxHR || w.MaxHR > p.Person.MaxHR {
				t.Fatalf("%s workout %+v is malformed", d, w)
			}
			for _, s := range w.HRSeries(time.Minute) {
				if s.Value < 60 || s.Value > float64(p.Person.MaxHR) {
					t.Fatalf("%s workout heart rate %v", d, s.Value)
				}
			}
		}
		workouts += len(day.Workouts)
		for _, s := range day.HRSeries(5 * time.Minute) {
			if s.Value < 40 || s.Value > float64(p.Person.MaxHR) {
				t.Fatalf("%s heart rate %v at %s", d, s.Value, s.At)
			}
			if sl := day.AsleepAt(s.At); sl != nil && s.Value != float64(sl.HR(s.At)) {
				t.Fatalf("%s asleep at %s reads %v, the night's own rate %d", d, s.At, s.Value, sl.HR(s.At))
			}
		}
		if day.Trip && day.Zone.String() != "Europe/London" {
			t.Fatalf("%s is on the trip in %s", d, day.Zone)
		}
		if !day.Trip && day.Zone.String() != "Europe/Athens" {
			t.Fatalf("%s is at home in %s", d, day.Zone)
		}
		if day.WeightKg != 0 && (day.WeightKg < 65 || day.WeightKg > 80) {
			t.Fatalf("%s weighs %v", d, day.WeightKg)
		}
		if day.Recovery < 5 || day.Recovery > 99 || day.Strain() < 0 || day.Strain() > 21 {
			t.Fatalf("%s recovery %d strain %v", d, day.Recovery, day.Strain())
		}
	}
	if workouts < 50 || workouts > 70 {
		t.Fatalf("%d workouts over the block, want about 60", workouts)
	}
	ill := p.Day(NewDate(2026, time.August, 27))
	well := p.Day(NewDate(2026, time.August, 20))
	if ill.RestingHR <= well.RestingHR || ill.HRVRMSSDMs >= well.HRVRMSSDMs {
		t.Fatalf("the cold does not show: ill rhr %d hrv %v, well rhr %d hrv %v", ill.RestingHR, ill.HRVRMSSDMs, well.RestingHR, well.HRVRMSSDMs)
	}
	if labs := p.Labs(); len(labs) != 3 || len(labs[0].Results) != 11 || len(labs[1].Results) != 8 {
		t.Fatalf("labs: %d panels", len(labs))
	}
}

func TestPersonaOptions(t *testing.T) {
	p, err := NewPersona(Options{Start: NewDate(2026, time.March, 1), End: NewDate(2026, time.March, 3), HomeZone: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.Days()); n != 3 {
		t.Fatalf("%d days, want 3", n)
	}
	if p.Options.Trip != nil || p.Options.Illness != nil {
		t.Fatal("a range outside the default trip and illness took them")
	}
	// A range that holds the default trip or illness keeps it.
	sept, err := NewPersona(Options{Start: NewDate(2026, time.September, 1), End: NewDate(2026, time.September, 30)})
	if err != nil {
		t.Fatal(err)
	}
	if !sept.Trip(NewDate(2026, time.September, 22)) || sept.Day(NewDate(2026, time.September, 22)).Zone.String() != "Europe/London" {
		t.Error("a September range dropped the London trip it holds")
	}
	aug, err := NewPersona(Options{Start: NewDate(2026, time.August, 28), End: NewDate(2026, time.September, 4)})
	if err != nil {
		t.Fatal(err)
	}
	if !aug.Ill(NewDate(2026, time.August, 28)) || aug.Trip(NewDate(2026, time.September, 22)) {
		t.Error("a range that holds the end of the cold dropped it, or took a trip it does not hold")
	}
	if z := p.Day(NewDate(2026, time.March, 2)).Zone.String(); z != "America/New_York" {
		t.Fatalf("zone %s", z)
	}
	if _, err := NewPersona(Options{Start: NewDate(2026, time.March, 3), End: NewDate(2026, time.March, 1)}); err == nil {
		t.Fatal("an inverted range was accepted")
	}
	if _, err := NewPersona(Options{HomeZone: "Mars/Olympus"}); err == nil {
		t.Fatal("an unknown zone was accepted")
	}
}

func TestDate(t *testing.T) {
	d := NewDate(2026, time.December, 31)
	if d.Add(1) != NewDate(2027, time.January, 1) || d.Add(-31) != NewDate(2026, time.November, 30) {
		t.Fatalf("Add: %s %s", d.Add(1), d.Add(-31))
	}
	if d.Weekday() != time.Thursday {
		t.Fatalf("weekday %s", d.Weekday())
	}
	if got, _ := ParseDate("2026-07-14"); got != NewDate(2026, time.July, 14) || got.Sub(NewDate(2026, time.July, 1)) != 13 {
		t.Fatalf("parse or sub: %s", got)
	}
	loc, _ := time.LoadLocation("Europe/Athens")
	if at := d.At(loc, 8, -30); at.Hour() != 7 || at.Minute() != 30 {
		t.Fatalf("negative minutes: %s", at)
	}
}

// Consecutive local days tile time: each day ends where the next one
// starts, the days the zone changes included, so no instant belongs to two
// days and none to no day, whichever emitter samples them.
func TestPersonaDaysTileTime(t *testing.T) {
	p, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	days := p.Days()
	for i := 0; i+1 < len(days); i++ {
		day, next := p.Day(days[i]), p.Day(days[i+1])
		if !day.EndTime().Equal(next.Start()) {
			t.Errorf("%s ends %s, %s starts %s", days[i], day.EndTime().UTC(), days[i+1], next.Start().UTC())
		}
		if !day.EndTime().After(day.Start()) {
			t.Errorf("%s spans %s to %s", days[i], day.Start(), day.EndTime())
		}
	}
	// The flight out and the flight home are the two days the zone changes.
	out, home := p.Day(NewDate(2026, time.September, 19)), p.Day(NewDate(2026, time.September, 24))
	if got := out.EndTime().Sub(out.Start()); got != 26*time.Hour {
		t.Errorf("the day of the flight out lasts %s, want 26h", got)
	}
	if got := home.EndTime().Sub(home.Start()); got != 22*time.Hour {
		t.Errorf("the last day in London lasts %s, want 22h", got)
	}
}

// One body cannot sleep and train at once: no night or nap overlaps a
// workout, so every emitter reads the same instant as asleep or awake.
func TestPersonaNeverSleepsThroughAWorkout(t *testing.T) {
	p, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Days() {
		day := p.Day(d)
		sleeps := append([]*Sleep{day.Sleep, p.Day(d.Add(1)).Sleep}, day.Naps...)
		for _, w := range day.Workouts {
			for _, s := range sleeps {
				if w.Start.Before(s.End) && s.Start.Before(w.End) {
					t.Errorf("%s: the %s from %s overlaps the sleep %s to %s", d, w.Name,
						w.Start.Format("15:04"), s.Start.Format("15:04"), s.End.Format("15:04"))
				}
			}
			if w.Start.Before(day.Sleep.End.Add(15 * time.Minute)) {
				t.Errorf("%s: the %s starts %s, within 15 minutes of waking at %s", d, w.Name,
					w.Start.Format("15:04"), day.Sleep.End.Format("15:04"))
			}
		}
	}
}

// A weigh-in and a blood pressure reading happen awake and at rest: after
// the night, outside every workout.
func TestPersonaWeighsInAndReadsPressureAwake(t *testing.T) {
	p, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	weighIns, readings := 0, 0
	for _, d := range p.Days() {
		day := p.Day(d)
		check := func(what string, at time.Time) {
			if s := day.AsleepAt(at); s != nil {
				t.Errorf("%s: the %s at %s is inside the sleep %s to %s", d, what,
					at.Format("15:04"), s.Start.Format("15:04"), s.End.Format("15:04"))
			}
			if !at.After(day.Sleep.End) {
				t.Errorf("%s: the %s at %s is before waking at %s", d, what, at.Format("15:04"), day.Sleep.End.Format("15:04"))
			}
			for _, w := range day.Workouts {
				if !at.Before(w.Start.Add(-10*time.Minute)) && at.Before(w.End.Add(30*time.Minute)) {
					t.Errorf("%s: the %s at %s falls in the %s from %s to %s or the half hour after it", d, what,
						at.Format("15:04"), w.Name, w.Start.Format("15:04"), w.End.Format("15:04"))
				}
			}
		}
		if day.WeightKg != 0 {
			weighIns++
			check("weigh-in", day.WeighInAt)
		}
		if bp := day.BloodPressure; bp != nil {
			readings++
			check("blood pressure reading", bp.At)
		}
	}
	if weighIns < 40 || readings < 10 {
		t.Fatalf("%d weigh-ins and %d readings; the check read too few", weighIns, readings)
	}
}

// A session warms up from below its average and settles below it at the
// end, whatever the sport: a yoga class neither starts at a runner's warm-up
// rate nor climbs to its cap in the closing rest.
func TestWorkoutHeartRateWarmsUpAndSettles(t *testing.T) {
	p, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	mean := func(s []Sample) float64 {
		sum := 0.0
		for _, x := range s {
			sum += x.Value
		}
		return sum / float64(len(s))
	}
	yoga := 0
	for _, d := range p.Days() {
		for _, w := range p.Day(d).Workouts {
			s := w.HRSeries(30 * time.Second)
			first, last, avg := mean(s[:4]), mean(s[len(s)-4:]), float64(w.AvgHR)
			if first >= avg || last >= avg {
				t.Errorf("%s %s (average %d): the first two minutes read %.0f, the last two %.0f", d, w.Name, w.AvgHR, first, last)
			}
			if w.Sport == Yoga {
				yoga++
				if body := mean(s); math.Abs(body-avg) > 5 {
					t.Errorf("%s yoga averages %.0f, its declared average %d", d, body, w.AvgHR)
				}
			}
		}
	}
	if yoga == 0 {
		t.Fatal("no yoga in the default life; the yoga check read nothing")
	}
}

// The trip's run and the hotel weigh-in fall on the trip's third day,
// whichever trip the options name.
func TestPersonaTripRunFollowsTheTrip(t *testing.T) {
	trip := &Span{NewDate(2026, time.August, 3), NewDate(2026, time.August, 7), "Europe/London"}
	p, err := NewPersona(Options{Trip: trip})
	if err != nil {
		t.Fatal(err)
	}
	day, ok := p.TripRunDay()
	if !ok || day != NewDate(2026, time.August, 5) {
		t.Fatalf("the trip's run day is %s (%v), want 2026-08-05", day, ok)
	}
	for _, d := range p.Days() {
		if !p.Trip(d) {
			continue
		}
		ws := p.Day(d).Workouts
		if (d == day) != (len(ws) == 1) {
			t.Errorf("%s on the trip has %d workouts", d, len(ws))
		}
	}
	out, err := Manual{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.Files[0].Body), "manual/2026-08-05-weight") {
		t.Error("the hotel weigh-in is not on the trip's run day")
	}
}
