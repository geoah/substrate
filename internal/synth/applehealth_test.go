package synth

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ahDoc is one delivery body as applehealthingest reads it.
type ahDoc struct {
	Data struct {
		Metrics []struct {
			Name  string           `json:"name"`
			Units string           `json:"units"`
			Data  []map[string]any `json:"data"`
		} `json:"metrics"`
		Workouts []map[string]any `json:"workouts"`
	} `json:"data"`
}

// ahUnits is every metric the emitter sends and its unit: each one a metric
// the healthapple adapter maps, so nothing lands only to be skipped.
var ahUnits = map[string]string{
	"heart_rate": "count/min", "step_count": "count", "walking_running_distance": "km",
	"flights_climbed": "count", "active_energy": "kJ", "basal_energy_burned": "kJ",
	"resting_heart_rate": "count/min", "heart_rate_variability": "ms", "respiratory_rate": "count/min",
	"blood_oxygen_saturation": "%", "weight_body_mass": "kg", "body_fat_percentage": "%",
	"blood_pressure": "mmHg", "sleep_analysis": "hr", "blood_glucose": "mg/dL",
	"vo2_max": "ml/(kg·min)", "apple_sleeping_wrist_temperature": "degC", "body_mass_index": "count",
	"height": "cm",
}

func ahEmit(t *testing.T, o Options) (*Persona, *Output) {
	t.Helper()
	p, err := NewPersona(o)
	if err != nil {
		t.Fatal(err)
	}
	out, err := AppleHealth{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 0 {
		t.Fatalf("Apple Health is pushed, yet it emitted %d files", len(out.Files))
	}
	return p, out
}

func ahDecode(t *testing.T, d Delivery) ahDoc {
	t.Helper()
	var doc ahDoc
	dec := json.NewDecoder(bytes.NewReader(d.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("%s: the body does not parse as the app's export: %v", d.Name, err)
	}
	return doc
}

// ahWhen parses an app date and holds its offset to the two the persona lives in.
func ahWhen(t *testing.T, v any) time.Time {
	t.Helper()
	s, _ := v.(string)
	at, err := time.Parse(ahDate, s)
	if err != nil {
		t.Fatalf("%q is not the app's date: %v", s, err)
	}
	if !strings.HasSuffix(s, " +0300") && !strings.HasSuffix(s, " +0100") {
		t.Fatalf("%q is at neither +0300 (Athens) nor +0100 (London)", s)
	}
	return at
}

func ahNum(t *testing.T, row map[string]any, key string) float64 {
	t.Helper()
	v, ok := row[key].(float64)
	if !ok {
		t.Fatalf("%s is %v (%T), want a number: %v", key, row[key], row[key], row)
	}
	return v
}

func TestAppleHealthEmitsTheDefaultLife(t *testing.T) {
	p, out := ahEmit(t, Options{})
	days := p.Days()
	if len(out.Deliveries) != len(days) {
		t.Fatalf("%d deliveries, want one per day: %d", len(out.Deliveries), len(days))
	}
	first, last := p.Day(days[0].Add(-1)).Start(), p.Day(days[len(days)-1]).EndTime()
	sessions := map[string]bool{}
	london := 0
	rows := map[string]int{}
	total, largest := 0, 0
	workouts := 0
	for i, d := range out.Deliveries {
		day := p.Day(days[i])
		if d.Name != days[i].String() || d.Trigger != "applehealth-webhook" {
			t.Fatalf("delivery %d is %s to %s", i, d.Name, d.Trigger)
		}
		h := d.Headers
		if h["content-type"] != "application/json" || h["automation-aggregation"] != "minutes" ||
			h["x-substrate-account"] != "iphone" || !strings.HasPrefix(h["authorization"], "Bearer ") ||
			h["automation-name"] == "" || h["automation-period"] == "" || !strings.HasPrefix(h["user-agent"], "Health%20Auto%20Export/") {
			t.Fatalf("%s headers %v", d.Name, h)
		}
		if sessions[h["session-id"]] {
			t.Fatalf("%s repeats session-id %s", d.Name, h["session-id"])
		}
		sessions[h["session-id"]] = true
		if len(d.Body) > 4<<20 {
			t.Errorf("%s is %d bytes, over 4 MiB", d.Name, len(d.Body))
		}
		total += len(d.Body)
		largest = max(largest, len(d.Body))
		doc := ahDecode(t, d)
		byName := map[string][]map[string]any{}
		for _, m := range doc.Data.Metrics {
			if want, ok := ahUnits[m.Name]; !ok || m.Units != want {
				t.Fatalf("%s sends %s in %q, want one of the mapped metrics in its unit", d.Name, m.Name, m.Units)
			}
			if len(m.Data) == 0 {
				t.Errorf("%s sends %s with no rows", d.Name, m.Name)
			}
			byName[m.Name] = m.Data
			rows[m.Name] += len(m.Data)
			for _, row := range m.Data {
				at := ahWhen(t, row["date"])
				if at.Before(first) || !at.Before(last) {
					t.Errorf("%s %s row at %s, outside the range", d.Name, m.Name, row["date"])
				}
				if day.Trip != strings.HasSuffix(row["date"].(string), "+0100") {
					t.Errorf("%s (trip %v) writes %s", d.Name, day.Trip, row["date"])
				}
			}
		}
		if day.Trip {
			london++
		}

		// What the ingest keys and the adapter reads, metric by metric.
		hr := byName["heart_rate"]
		slots := int(day.EndTime().Sub(day.Start()) / (5 * time.Minute))
		if len(hr) < slots*87/100 || len(hr) > slots {
			t.Errorf("%s: %d heart rate rows over %d slots, want about one per 5 minutes", d.Name, len(hr), slots)
		}
		for _, row := range hr {
			mn, avg, mx := ahNum(t, row, "Min"), ahNum(t, row, "Avg"), ahNum(t, row, "Max")
			if row["source"] != ahWatch || mn > avg || avg > mx || avg < 38 || avg > 195 {
				t.Fatalf("%s heart rate row %v", d.Name, row)
			}
		}
		for _, row := range byName["step_count"] {
			if q := ahNum(t, row, "qty"); q <= 0 || q > 1000 || (row["source"] != ahIPhone && row["source"] != ahWatch) {
				t.Fatalf("%s step row %v", d.Name, row)
			}
		}
		if n, hours := len(byName["basal_energy_burned"]), int(day.EndTime().Sub(day.Start())/time.Hour); n != hours {
			t.Errorf("%s: %d basal energy rows over %d hours", d.Name, n, hours)
		}
		for _, name := range []string{"resting_heart_rate", "heart_rate_variability", "respiratory_rate", "blood_oxygen_saturation", "apple_sleeping_wrist_temperature"} {
			for _, row := range byName[name] {
				if _, ok := row["source"]; ok {
					t.Fatalf("%s %s row carries a source; the app sends none: %v", d.Name, name, row)
				}
			}
		}
		if len(byName["resting_heart_rate"]) != 1 || len(byName["heart_rate_variability"]) != 3 {
			t.Errorf("%s: resting %v, hrv %v", d.Name, byName["resting_heart_rate"], byName["heart_rate_variability"])
		}
		if w := byName["apple_sleeping_wrist_temperature"]; len(w) != 1 || ahNum(t, w[0], "qty") < 34 || ahNum(t, w[0], "qty") > 37.5 {
			t.Errorf("%s: wrist temperature %v", d.Name, w)
		}
		runs := 0
		for _, w := range day.Workouts {
			if w.Sport == Running {
				runs++
			}
		}
		if v := byName["vo2_max"]; len(v) != runs {
			t.Errorf("%s: %d VO2 max estimates after %d runs", d.Name, len(v), runs)
		}
		// The height is typed into the phone once, on the first day.
		if h := byName["height"]; (i == 0) != (len(h) == 1) || len(h) > 1 ||
			(len(h) == 1 && (ahNum(t, h[0], "qty") != p.Person.HeightM*100 || h[0]["source"] != ahIPhone)) {
			t.Errorf("%s: height %v", d.Name, h)
		}
		if b := byName["body_mass_index"]; len(b) != len(byName["weight_body_mass"]) {
			t.Errorf("%s: BMI %v beside weight %v", d.Name, b, byName["weight_body_mass"])
		}
		if (day.WeightKg != 0) != (len(byName["weight_body_mass"]) == 1) || len(byName["weight_body_mass"]) != len(byName["body_fat_percentage"]) {
			t.Errorf("%s: weigh-in %v, weight %v, fat %v", d.Name, day.WeightKg, byName["weight_body_mass"], byName["body_fat_percentage"])
		}
		for _, row := range byName["weight_body_mass"] {
			if kg := ahNum(t, row, "qty"); row["source"] != ahWithings || kg < 69 || kg > 76 {
				t.Errorf("%s weight %v", d.Name, row)
			}
		}
		if bp := byName["blood_pressure"]; (day.BloodPressure != nil) != (len(bp) == 1) {
			t.Errorf("%s blood pressure %v", d.Name, bp)
		} else if len(bp) == 1 {
			if sy, di := ahNum(t, bp[0], "systolic"), ahNum(t, bp[0], "diastolic"); sy <= di || di < 60 || sy > 140 || bp[0]["source"] != ahWithings {
				t.Errorf("%s blood pressure %v", d.Name, bp[0])
			}
		}
		if g := byName["blood_glucose"]; (day.Glucose != nil) != (len(g) == 1) {
			t.Errorf("%s: meter reading %v, glucose rows %v", d.Name, day.Glucose, g)
		} else if len(g) == 1 {
			if q := ahNum(t, g[0], "qty"); q < 65 || q > 140 || g[0]["mealTime"] != "Before Meal" || g[0]["source"] != ahMeter {
				t.Errorf("%s glucose %v", d.Name, g[0])
			}
		}
		night := byName["sleep_analysis"]
		if len(night) != 1 {
			t.Fatalf("%s: %d sleep rows, want the one night", d.Name, len(night))
		}
		tot := day.Sleep.StageTotals()
		n := night[0]
		for k, want := range map[string]int{"core": tot[Light], "deep": tot[Deep], "rem": tot[REM], "awake": tot[Awake], "totalSleep": day.Sleep.AsleepS()} {
			if got := ahNum(t, n, k) * 3600; got < float64(want)-0.5 || got > float64(want)+0.5 {
				t.Errorf("%s night %s = %v h, the persona's is %d s", d.Name, k, n[k], want)
			}
		}
		if bed := ahWhen(t, n["inBedEnd"]).Sub(ahWhen(t, n["inBedStart"])).Hours(); bed < 4 || bed > 12 || n["source"] != ahWatch ||
			!ahWhen(t, n["sleepStart"]).After(ahWhen(t, n["inBedStart"])) || ahNum(t, n, "asleep") != 0 {
			t.Errorf("%s night %v", d.Name, n)
		}

		if len(doc.Data.Workouts) != len(day.Workouts) {
			t.Fatalf("%s: %d workouts, the persona's %d", d.Name, len(doc.Data.Workouts), len(day.Workouts))
		}
		for _, w := range doc.Data.Workouts {
			workouts++
			id, _ := w["id"].(string)
			if !regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$`).MatchString(id) {
				t.Errorf("workout id %q is not an upper-case v4 UUID", id)
			}
			switch w["name"] {
			case "Outdoor Run", "Outdoor Cycle", "Traditional Strength Training", "Yoga":
			default:
				t.Errorf("workout name %v", w["name"])
			}
			start, end := ahWhen(t, w["start"]), ahWhen(t, w["end"])
			if ahNum(t, w, "duration") != end.Sub(start).Seconds() {
				t.Errorf("workout %s duration %v, span %s", id, w["duration"], end.Sub(start))
			}
			data, _ := w["heartRateData"].([]any)
			if len(data) < 20 {
				t.Errorf("workout %s has %d heart rate minutes", id, len(data))
			}
			for _, item := range data {
				row := item.(map[string]any)
				if row["source"] != ahWatch || row["units"] != "bpm" || ahNum(t, row, "Min") > ahNum(t, row, "Avg") {
					t.Fatalf("workout %s heart rate row %v", id, row)
				}
			}
			if avg := obj(w, "avgHeartRate", "qty"); avg == nil || obj(w, "activeEnergyBurned", "units") != "kcal" {
				t.Errorf("workout %s %v", id, w)
			}
			if _, ok := w["distance"]; ok != (w["name"] == "Outdoor Run" || w["name"] == "Outdoor Cycle") {
				t.Errorf("workout %s (%v) distance %v", id, w["name"], w["distance"])
			}
		}
	}
	var points int
	for _, n := range rows {
		points += n
	}
	t.Logf("90 days: %d deliveries, %d bytes (largest %d), %d metric rows %v, %d workouts", len(out.Deliveries), total, largest, points, rows, workouts)
	if london != 5 || workouts < 50 || points < 40000 || points > 70000 {
		t.Errorf("%d London days (want 5), %d workouts, %d metric rows", london, workouts, points)
	}
}

func obj(m map[string]any, path ...string) any {
	var v any = m
	for _, k := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}

// The device's readings follow the persona: the day's steps within the
// phone's and the watch's few percent, the evening after bedtime asleep.
func TestAppleHealthFollowsThePersona(t *testing.T) {
	p, out := ahEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	asleep := 0
	for i, d := range out.Deliveries {
		day := p.Day(p.Days()[i])
		doc := ahDecode(t, d)
		for _, m := range doc.Data.Metrics {
			switch m.Name {
			case "step_count":
				// Where both devices counted one walk, the walk is read once.
				read := map[string]float64{}
				for _, row := range m.Data {
					read[row["date"].(string)] = math.Max(read[row["date"].(string)], ahNum(t, row, "qty"))
				}
				sum := 0.0
				for _, v := range read {
					sum += v
				}
				if truth := float64(day.StepsTotal()); sum < truth*0.92 || sum > truth*1.06 {
					t.Errorf("%s: %0.f steps read, the persona walked %0.f", d.Name, sum, truth)
				}
			case "heart_rate":
				// After bedtime the watch reads the night that ends tomorrow
				// morning; Day.HR alone would answer a waking rate, some nine
				// beats above it.
				next := p.Day(p.Days()[i].Add(1)).Sleep
				sum, truth, n := 0.0, 0.0, 0
				for _, row := range m.Data {
					if at := ahWhen(t, row["date"]); !at.Before(next.Start) {
						sum += ahNum(t, row, "Avg")
						truth += float64(next.HR(at))
						n++
					}
				}
				if n > 0 {
					asleep++
					if got, want := sum/float64(n), truth/float64(n); math.Abs(got-want) > 3 {
						t.Errorf("%s: heart rate averages %.1f after the %s bedtime, the sleeping persona's %.1f",
							d.Name, got, next.Start.Format("15:04"), want)
					}
				}
			}
		}
	}
	if asleep < 2 {
		t.Fatalf("%d of the three evenings reach bedtime before midnight; the check above read nothing", asleep)
	}
}

func TestAppleHealthIsDeterministic(t *testing.T) {
	_, a := ahEmit(t, Options{})
	_, b := ahEmit(t, Options{})
	if len(a.Deliveries) != len(b.Deliveries) {
		t.Fatalf("%d deliveries, then %d", len(a.Deliveries), len(b.Deliveries))
	}
	for i := range a.Deliveries {
		ea, _ := a.Deliveries[i].Envelope()
		eb, _ := b.Deliveries[i].Envelope()
		if !bytes.Equal(ea, eb) {
			t.Fatalf("%s differs between two runs", a.Deliveries[i].Name)
		}
	}
	_, c := ahEmit(t, Options{Seed: "other"})
	if bytes.Equal(a.Deliveries[0].Body, c.Deliveries[0].Body) {
		t.Fatal("another seed wrote the same first day")
	}
}

func TestAppleHealthThreeDaysIsSmall(t *testing.T) {
	_, out := ahEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	size := 0
	for _, d := range out.Deliveries {
		size += len(d.Body)
	}
	if len(out.Deliveries) != 3 || size > 250_000 {
		t.Fatalf("three days are %d deliveries and %d bytes, want 3 under 250 KB", len(out.Deliveries), size)
	}
}

// On about one day in four the phone in a pocket and the watch on the wrist
// both count the walks to work, to lunch and home, and both rows are sent:
// two sources over the same five minutes, which a reader has to choose
// between. Every other bucket comes from one device.
func TestAppleHealthSendsBothDevicesStepsOnSomeDays(t *testing.T) {
	_, out := ahEmit(t, Options{})
	days := 0
	for _, d := range out.Deliveries {
		doc := ahDecode(t, d)
		both := 0
		for _, m := range doc.Data.Metrics {
			if m.Name != "step_count" {
				continue
			}
			bySlot := map[string][]map[string]any{}
			for _, row := range m.Data {
				bySlot[row["date"].(string)] = append(bySlot[row["date"].(string)], row)
			}
			for slot, rows := range bySlot {
				switch len(rows) {
				case 1:
				case 2:
					a, b := ahNum(t, rows[0], "qty"), ahNum(t, rows[1], "qty")
					if rows[0]["source"] == rows[1]["source"] || math.Abs(a-b) > 0.2*math.Max(a, b) {
						t.Errorf("%s %s: %v and %v", d.Name, slot, rows[0], rows[1])
					}
					both++
				default:
					t.Errorf("%s %s has %d step rows", d.Name, slot, len(rows))
				}
			}
		}
		if both > 0 {
			days++
		}
	}
	if days < 10 || days > 40 {
		t.Errorf("%d of 90 days carry both devices' steps, want about one in four", days)
	}
}
