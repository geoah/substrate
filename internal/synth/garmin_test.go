package synth

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// garminEmit emits the persona and answers the files by path, the deliveries
// in order, and each delivery's body decoded.
func garminEmit(t *testing.T, o Options) (*Persona, map[string][]byte, []Delivery, []map[string][]map[string]any) {
	t.Helper()
	p, err := NewPersona(o)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Garmin{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	// The runner installs the provider and creates the account; the loader
	// imports the adapter and nothing else.
	if out.Setup.Provider != "" || len(out.Setup.Records) != 0 ||
		len(out.Setup.Samples) != 1 || out.Setup.Samples[0] != "samples.substrate.reamde.dev/healthgarmin" {
		t.Fatalf("setup %+v, want the healthgarmin sample alone", out.Setup)
	}
	files := map[string][]byte{}
	for _, f := range out.Files {
		if _, dup := files[f.Path]; dup {
			t.Fatalf("%s is emitted twice", f.Path)
		}
		files[f.Path] = f.Body
	}
	bodies := make([]map[string][]map[string]any, len(out.Deliveries))
	for i, d := range out.Deliveries {
		dec := json.NewDecoder(bytes.NewReader(d.Body))
		dec.UseNumber()
		if err := dec.Decode(&bodies[i]); err != nil {
			t.Fatalf("delivery %d (%s) does not parse: %v", i, d.Name, err)
		}
		if len(bodies[i]) != 1 {
			t.Fatalf("delivery %d (%s) carries %d summary types, want one", i, d.Name, len(bodies[i]))
		}
	}
	return p, files, out.Deliveries, bodies
}

// gInt is a JSON number as int64, failing on anything that is not an integer.
func gInt(t *testing.T, e map[string]any, key string) int64 {
	t.Helper()
	n, ok := e[key].(json.Number)
	if !ok {
		t.Fatalf("%s is %v (%T), want a number", key, e[key], e[key])
	}
	i, err := n.Int64()
	if err != nil {
		t.Fatalf("%s is %v, want an integer", key, n)
	}
	return i
}

func gFloat(t *testing.T, v any) float64 {
	t.Helper()
	n, ok := v.(json.Number)
	if !ok {
		t.Fatalf("%v (%T) is not a number", v, v)
	}
	f, err := n.Float64()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// gLocalDay is the date an instant falls on at a summary's own offset.
func gLocalDay(t *testing.T, secs, off int64) Date {
	t.Helper()
	if off != 10800 && off != 3600 {
		t.Fatalf("offset %d, want 10800 at home or 3600 in London", off)
	}
	lt := time.Unix(secs+off, 0).UTC()
	return NewDate(lt.Year(), lt.Month(), lt.Day())
}

// gKeys is a sample map's offsets, sorted, each checked to be a multiple of
// step below limit.
func gKeys(t *testing.T, m map[string]any, step, limit int64, what string) []int64 {
	t.Helper()
	var out []int64
	for k := range m {
		v, err := strconv.ParseInt(k, 10, 64)
		if err != nil || v < 0 || v%step != 0 || v >= limit {
			t.Fatalf("%s offset %q is not a multiple of %d inside %d", what, k, step, limit)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// garminWeekChunks splits the days where the emitter flushes its weekly
// deliveries: after each Sunday and after the last day.
func garminWeekChunks(p *Persona) [][]Date {
	var out [][]Date
	var cur []Date
	for _, d := range p.Days() {
		cur = append(cur, d)
		if d.Weekday() == time.Sunday || d == p.Options.End {
			out = append(out, cur)
			cur = nil
		}
	}
	return out
}

func TestGarminEmitsTheDefaultLife(t *testing.T) {
	p, files, deliveries, bodies := garminEmit(t, Options{})
	days := p.Days()
	inRange := func(d Date) bool { return !d.Before(p.Options.Start) && !d.After(p.Options.End) }

	// The connect step: the user id, the permissions and one answer per
	// backfill type, byte for byte the recorded fixtures the runner's mock
	// serves, and e2e.json.
	if len(files) != 2+len(garminBackfill)+1 {
		t.Fatalf("%d files, want %d", len(files), 2+len(garminBackfill)+1)
	}
	fixtures := filepath.Join("..", "providere2e", "fixtures", "garmin")
	for name, body := range files {
		if name == "e2e.json" {
			continue
		}
		want, err := os.ReadFile(filepath.Join(fixtures, strings.TrimPrefix(name, "recordings/")))
		if err != nil {
			t.Errorf("%s answers no request the recorded fixtures answer: %v", name, err)
			continue
		}
		if !bytes.Equal(body, want) {
			t.Errorf("%s differs from the recorded fixture:\n%s", name, body)
		}
	}
	var cfg map[string]any
	if err := json.Unmarshal(files["e2e.json"], &cfg); err != nil {
		t.Fatal(err)
	}
	account, _ := cfg["account"].(map[string]any)
	for _, toggle := range garminToggles {
		if account[toggle] != true {
			t.Errorf("e2e.json leaves %s off", toggle)
		}
	}
	clientID := cfg["config"].(map[string]any)["clientId"]
	if account["backfillDepth"] != "last90d" || cfg["mockPort"] != float64(garminMockPort) || cfg["accountId"] != "ada" ||
		clientID != garminClientID || cfg["settleSeconds"] == nil || cfg["continueRounds"] == nil {
		t.Errorf("e2e.json %v", cfg)
	}
	for _, trig := range cfg["triggers"].([]any) {
		if trig == GarminTrigger {
			t.Error("e2e.json keeps garmin-webhook on through the connect step, before it has a key")
		}
	}

	// The deliveries: one daily, epochs, stress details, night and breathing
	// delivery a day and one per activity's details; one per weekly type and week.
	var workouts, weighIns, pressures int
	for _, d := range days {
		x := p.Day(d)
		workouts += len(x.Workouts)
		if x.WeightKg != 0 {
			weighIns++
		}
		if x.BloodPressure != nil {
			pressures++
		}
	}
	perType := map[string]int{}
	entries := map[string]int{}
	byType := map[string][]map[string]any{}
	for i, d := range deliveries {
		for key, list := range bodies[i] {
			perType[key]++
			entries[key] += len(list)
			byType[key] = append(byType[key], list...)
			if !strings.HasPrefix(d.Name, key+"_") {
				t.Errorf("delivery %d is named %s, carrying %s", i, d.Name, key)
			}
		}
		if d.Trigger != GarminTrigger || d.Headers["content-type"] != "application/json" || d.Headers["garmin-client-id"] != clientID {
			t.Errorf("delivery %s: trigger %s, headers %v", d.Name, d.Trigger, d.Headers)
		}
		if len(d.Body) > 4<<20 {
			t.Errorf("delivery %s is %d bytes, over 4 MiB", d.Name, len(d.Body))
		}
	}
	weeks := len(garminWeekChunks(p))
	for key, want := range map[string]int{
		"dailies": len(days), "epochs": len(days), "stressDetails": len(days), "sleeps": len(days),
		"allDayRespiration": len(days), "activityDetails": workouts,
		"hrv": weeks, "pulseox": weeks, "skinTemp": weeks, "bodyComps": weeks, "bloodPressures": weeks - 1,
		"activities": weeks, "userMetrics": weeks,
	} {
		if perType[key] != want {
			t.Errorf("%d %s deliveries, want %d", perType[key], key, want)
		}
	}
	for key, want := range map[string]int{
		"dailies": len(days), "stressDetails": len(days), "sleeps": len(days), "hrv": len(days), "pulseox": len(days),
		"skinTemp": len(days), "bodyComps": weighIns, "bloodPressures": pressures, "activities": workouts,
		"activityDetails": workouts,
	} {
		if entries[key] != want {
			t.Errorf("%d %s summaries, want %d", entries[key], key, want)
		}
	}
	if perType["moveIQActivities"] == 0 || entries["moveIQActivities"] > 12 {
		t.Errorf("%d Move IQ events over %d deliveries; want a few", entries["moveIQActivities"], perType["moveIQActivities"])
	}
	if len(perType) != 14 {
		t.Errorf("%d summary types, want 14: %v", len(perType), perType)
	}

	// Every summary: Garmin's user, a summaryId unique within its type.
	for key, list := range byType {
		seen := map[string]bool{}
		for _, e := range list {
			sid, _ := e["summaryId"].(string)
			if e["userId"] != garminUserID || sid == "" || seen[sid] {
				t.Fatalf("%s summary %v: userId %v, a duplicate or empty summaryId", key, sid, e["userId"])
			}
			seen[sid] = true
		}
	}

	// The dailies: what garminpush mirrors and the adapter traces. Days tile
	// the range, the two the zone changes on 22 and 26 hours long; heart rate
	// every 5 minutes.
	var prevEnd int64
	london := 0
	spans := map[string][2]int64{}
	for i, e := range byType["dailies"] {
		start, dur, off := gInt(t, e, "startTimeInSeconds"), gInt(t, e, "durationInSeconds"), gInt(t, e, "startTimeOffsetInSeconds")
		date, err := ParseDate(e["calendarDate"].(string))
		if err != nil || date != gLocalDay(t, start, off) || !inRange(date) {
			t.Fatalf("daily %v: calendarDate %v starts %d at %d", e["summaryId"], e["calendarDate"], start, off)
		}
		if off == 3600 {
			london++
		}
		if dur < 22*3600 || dur > 26*3600 || (i > 0 && start != prevEnd) {
			t.Errorf("daily %s spans %d s from %d, the day before ending %d", date, dur, start, prevEnd)
		}
		prevEnd = start + dur
		spans[date.String()] = [2]int64{start, start + dur}
		hr, _ := e["timeOffsetHeartRateSamples"].(map[string]any)
		if keys := gKeys(t, hr, 300, dur, "heart rate"); int64(len(keys)) != dur/300 {
			t.Errorf("daily %s holds %d heart rate samples over %d s", date, len(keys), dur)
		}
		for k, v := range hr {
			if b := gFloat(t, v); b < 35 || b > float64(p.Person.MaxHR+5) {
				t.Errorf("daily %s heart rate %v at %s", date, b, k)
			}
		}
		if s := gInt(t, e, "steps"); s < 1000 || s > 40000 {
			t.Errorf("daily %s steps %d", date, s)
		}
		if rhr := gInt(t, e, "restingHeartRateInBeatsPerMinute"); rhr < 40 || rhr > 70 {
			t.Errorf("daily %s resting heart rate %d", date, rhr)
		}
	}
	if london != 5 {
		t.Errorf("%d dailies in London, want 5", london)
	}

	// Epochs: 15-minute slices inside their day, one per activity type each.
	slices := map[int64]map[string]bool{}
	for _, e := range byType["epochs"] {
		start := gInt(t, e, "startTimeInSeconds")
		if gInt(t, e, "durationInSeconds") != 900 || gInt(t, e, "activeTimeInSeconds") > 900 || gInt(t, e, "steps") < 0 {
			t.Fatalf("epoch %v: %v", e["summaryId"], e)
		}
		if slices[start] == nil {
			slices[start] = map[string]bool{}
		}
		typ, _ := e["activityType"].(string)
		if slices[start][typ] {
			t.Fatalf("slice %d carries %s twice", start, typ)
		}
		slices[start][typ] = true
		if !regexp.MustCompile(`^(SEDENTARY|WALKING|RUNNING|CYCLING|GENERIC)$`).MatchString(typ) {
			t.Errorf("epoch activity type %s", typ)
		}
	}
	for date, s := range spans {
		n := 0
		for st := range slices {
			if st >= s[0] && st < s[1] {
				n++
			}
		}
		if int64(n) != (s[1]-s[0]+899)/900 {
			t.Errorf("%s has %d epoch slices over %d s", date, n, s[1]-s[0])
		}
	}

	// Stress every 3 minutes, -1 where unmeasured; Body Battery every 15.
	for _, e := range byType["stressDetails"] {
		dur := gInt(t, e, "durationInSeconds")
		levels, _ := e["timeOffsetStressLevelValues"].(map[string]any)
		battery, _ := e["timeOffsetBodyBatteryValues"].(map[string]any)
		if int64(len(gKeys(t, levels, 180, dur, "stress"))) != (dur+179)/180 || int64(len(gKeys(t, battery, 900, dur, "body battery"))) != (dur+899)/900 {
			t.Errorf("stress details %v: %d levels and %d battery values over %d s", e["calendarDate"], len(levels), len(battery), dur)
		}
		for _, v := range levels {
			if s := gFloat(t, v); s != -1 && (s < 1 || s > 99) {
				t.Errorf("stress level %v", s)
			}
		}
	}

	// Nights: stage segments contiguous, the totals theirs, four to twelve
	// hours, never into a workout; the night's HRV, SpO2 and skin on its start.
	activities := byType["activities"]
	for _, e := range byType["sleeps"] {
		start, off := gInt(t, e, "startTimeInSeconds"), gInt(t, e, "startTimeOffsetInSeconds")
		levels, _ := e["sleepLevelsMap"].(map[string]any)
		type seg struct{ a, b int64 }
		var segs []seg
		totals := map[string]int64{}
		for stage, list := range levels {
			if stage != "deep" && stage != "light" && stage != "rem" && stage != "awake" {
				t.Fatalf("sleep stage %s", stage)
			}
			for _, r := range list.([]any) {
				m := r.(map[string]any)
				a, b := gInt(t, m, "startTimeInSeconds"), gInt(t, m, "endTimeInSeconds")
				segs = append(segs, seg{a, b})
				totals[stage] += b - a
			}
		}
		sort.Slice(segs, func(i, j int) bool { return segs[i].a < segs[j].a })
		for i, s := range segs {
			if s.b <= s.a || (i == 0 && s.a != start) || (i > 0 && s.a != segs[i-1].b) {
				t.Fatalf("sleep %v: segments are not contiguous from its start at %d", e["calendarDate"], i)
			}
		}
		end := segs[len(segs)-1].b
		if h := float64(end-start) / 3600; h < 4 || h > 12 {
			t.Errorf("sleep %v lasts %.1f h", e["calendarDate"], h)
		}
		if gInt(t, e, "durationInSeconds") != totals["deep"]+totals["light"]+totals["rem"] ||
			gInt(t, e, "awakeDurationInSeconds") != totals["awake"] || e["validation"] != "ENHANCED_FINAL" {
			t.Errorf("sleep %v totals %v against %v", e["calendarDate"], totals, e)
		}
		date, _ := ParseDate(e["calendarDate"].(string))
		if !inRange(date) || gLocalDay(t, end, off) != date {
			t.Errorf("sleep %v ends on another day", e["calendarDate"])
		}
		for _, a := range activities {
			as := gInt(t, a, "startTimeInSeconds")
			if as < end && start < as+gInt(t, a, "durationInSeconds") {
				t.Errorf("sleep %v runs into activity %v", e["calendarDate"], a["activityName"])
			}
		}
		spo2, _ := e["timeOffsetSleepSpo2"].(map[string]any)
		gKeys(t, spo2, 600, end-start, "sleep SpO2")
	}
	for _, key := range []string{"hrv", "pulseox", "skinTemp"} {
		for _, e := range byType[key] {
			if !strings.HasPrefix(e["summaryId"].(string), garminSIDPrefix) || gInt(t, e, "durationInSeconds") < 4*3600 {
				t.Errorf("%s %v: %v", key, e["calendarDate"], e)
			}
		}
	}
	for _, e := range byType["hrv"] {
		values, _ := e["hrvValues"].(map[string]any)
		gKeys(t, values, 300, gInt(t, e, "durationInSeconds")+1, "hrv")
		if avg := gInt(t, e, "lastNightAvg"); avg < 15 || avg > 120 || gInt(t, e, "lastNight5MinHigh") < avg {
			t.Errorf("hrv %v: avg %d high %v", e["calendarDate"], avg, e["lastNight5MinHigh"])
		}
	}
	for _, e := range byType["pulseox"] {
		for _, v := range e["timeOffsetSpo2Values"].(map[string]any) {
			if s := gFloat(t, v); s < 85 || s > 100 {
				t.Errorf("SpO2 %v", s)
			}
		}
	}

	// Activities: Garmin's types on the one watch; details every 30 seconds,
	// a lap a kilometer on a run.
	types := map[string]bool{"RUNNING": true, "CYCLING": true, "STRENGTH_TRAINING": true, "YOGA": true}
	details := map[string]map[string]any{}
	for _, d := range byType["activityDetails"] {
		details[d["summaryId"].(string)] = d
	}
	for _, a := range activities {
		if !types[a["activityType"].(string)] || a["deviceName"] != garminDevice || a["summaryId"] != a["activityId"].(json.Number).String() {
			t.Errorf("activity %v: %v", a["summaryId"], a)
		}
		d := details[a["summaryId"].(string)+"-detail"]
		if d == nil {
			t.Fatalf("activity %v has no details", a["summaryId"])
		}
		samples := d["samples"].([]any)
		start, dur := gInt(t, a, "startTimeInSeconds"), gInt(t, a, "durationInSeconds")
		if int64(len(samples)) != dur/30+1 || gInt(t, samples[0].(map[string]any), "startTimeInSeconds") != start {
			t.Errorf("activity %v: %d samples over %d s", a["summaryId"], len(samples), dur)
		}
		laps, _ := d["laps"].([]any)
		if a["activityType"] == "RUNNING" {
			km := gFloat(t, a["distanceInMeters"]) / 1000
			if len(laps) != int(km)+1 || gInt(t, laps[0].(map[string]any), "startTimeInSeconds") != start {
				t.Errorf("run %v of %.1f km has %d laps", a["summaryId"], km, len(laps))
			}
		} else if laps != nil {
			t.Errorf("%v carries laps", a["activityType"])
		}
	}

	// The mornings.
	for _, e := range byType["bodyComps"] {
		if g := gInt(t, e, "weightInGrams"); g < 65000 || g > 80000 {
			t.Errorf("weigh-in %d g", g)
		}
	}
	for _, e := range byType["bloodPressures"] {
		if s, d := gInt(t, e, "systolic"), gInt(t, e, "diastolic"); s < 100 || s > 140 || d < 60 || d >= s {
			t.Errorf("blood pressure %d/%d", s, d)
		}
	}
	for _, e := range byType["userMetrics"] {
		if v := gFloat(t, e["vo2Max"]); v < 45 || v > 52 {
			t.Errorf("VO2 max %v", v)
		}
	}
}

func TestGarminIsDeterministic(t *testing.T) {
	_, a, da, _ := garminEmit(t, Options{})
	_, b, db, _ := garminEmit(t, Options{})
	for name, body := range a {
		if !bytes.Equal(body, b[name]) {
			t.Fatalf("%s differs between two runs", name)
		}
	}
	if len(da) != len(db) {
		t.Fatalf("%d deliveries, then %d", len(da), len(db))
	}
	for i := range da {
		if da[i].Name != db[i].Name || !bytes.Equal(da[i].Body, db[i].Body) {
			t.Fatalf("delivery %d (%s) differs between two runs", i, da[i].Name)
		}
	}
	_, _, dc, _ := garminEmit(t, Options{Seed: "other"})
	if bytes.Equal(da[0].Body, dc[0].Body) {
		t.Fatal("another seed wrote the same first daily")
	}
}

func TestGarminThreeDaysIsSmall(t *testing.T) {
	p, files, deliveries, _ := garminEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	size := 0
	for _, d := range deliveries {
		size += len(d.Body)
	}
	workouts := 0
	for _, d := range p.Days() {
		workouts += len(p.Day(d).Workouts)
	}
	// Five a day and one per workout's details; then, split at the Sunday, the
	// weekly types: HRV, Pulse Ox and skin twice, a weigh-in each side, the
	// Sunday's blood pressure, and user metrics, activities and Move IQ walks
	// where the days hold them.
	if len(files) != 18 || len(deliveries) < 15+workouts+11 || len(deliveries) > 15+workouts+16 || size > 400_000 {
		t.Fatalf("three days are %d files and %d deliveries of %d bytes", len(files), len(deliveries), size)
	}
	if deliveries[0].Name != "dailies_2026-09-12" {
		t.Errorf("the first delivery is %s", deliveries[0].Name)
	}
}

// The watch walks the persona's stride: a quarter hour with steps and no
// workout covers the steps times StrideM, as every other emitter's does.
func TestGarminWalksThePersonasStride(t *testing.T) {
	_, _, deliveries, bodies := garminEmit(t, Options{Start: NewDate(2026, time.September, 15), End: NewDate(2026, time.September, 15)})
	checked := 0
	for i := range deliveries {
		for _, e := range bodies[i]["epochs"] {
			steps := gInt(t, e, "steps")
			if e["activityType"] != "WALKING" || steps < 200 {
				continue
			}
			checked++
			if per := gFloat(t, e["distanceInMeters"]) / float64(steps); math.Abs(per-StrideM) > 0.01 {
				t.Errorf("epoch %v walks %.3f m a step, the persona's stride is %.2f", e["summaryId"], per, StrideM)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no walking epoch; the check read nothing")
	}
}
