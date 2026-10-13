package synth

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// whoopPage is one recorded collection page as whoopsync reads it.
type whoopPage struct {
	Records   []map[string]any `json:"records"`
	NextToken string           `json:"next_token"`
}

// whoopLookup answers a request the way the mock server does: the exact
// name, then the name without the pagination parameters, then the bare path.
func whoopLookup(files map[string][]byte, rawURL string) ([]byte, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, false
	}
	pairs := Query(u.Query())
	var thin []Pair
	for _, p := range pairs {
		if !PaginationParams[p.Key] {
			thin = append(thin, p)
		}
	}
	for _, name := range []string{
		RecordingName("GET", u.Path, pairs...), RecordingName("GET", u.Path, thin...), RecordingName("GET", u.Path),
	} {
		if b, ok := files["recordings/"+name]; ok {
			return b, true
		}
	}
	return nil, false
}

// whoopWalk follows one collection the way whoopsync pages it, `limit` and
// `start` on every request and `nextToken` from the page before, and answers
// every record and how many pages it read.
func whoopWalk(t *testing.T, files map[string][]byte, path string) ([]map[string]any, int) {
	t.Helper()
	var out []map[string]any
	token := ""
	for pages := 1; ; pages++ {
		q := url.Values{"limit": {"25"}, "start": {"2015-01-01T00:00:00Z"}}
		if token != "" {
			q.Set("nextToken", token)
		}
		raw, ok := whoopLookup(files, path+"?"+q.Encode())
		if !ok {
			t.Fatalf("the mock would answer %s?%s with 404", path, q.Encode())
		}
		var page whoopPage
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("%s page %d: %v", path, pages, err)
		}
		if len(page.Records) > whoopPageSize {
			t.Fatalf("%s page %d holds %d records, more than WHOOP's 25", path, pages, len(page.Records))
		}
		out = append(out, page.Records...)
		if page.NextToken == "" {
			return out, pages
		}
		if page.NextToken == token {
			t.Fatalf("%s page %d answers its own token %s", path, pages, token)
		}
		token = page.NextToken
	}
}

func whoopEmit(t *testing.T, o Options) (*Persona, map[string][]byte) {
	t.Helper()
	p, err := NewPersona(o)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Whoop{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Deliveries) != 0 {
		t.Fatalf("WHOOP is mock-driven, yet it emitted %d deliveries", len(out.Deliveries))
	}
	files := map[string][]byte{}
	for _, f := range out.Files {
		if _, dup := files[f.Path]; dup {
			t.Fatalf("%s is emitted twice", f.Path)
		}
		files[f.Path] = f.Body
	}
	return p, files
}

func whoopInstant(t *testing.T, v any) time.Time {
	t.Helper()
	s, _ := v.(string)
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`).MatchString(s) {
		t.Fatalf("%q is not WHOOP's UTC millisecond instant", s)
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func whoopNum(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s is %v (%T), want a number", key, m[key], m[key])
	}
	return v
}

// localDay is the date t falls on at the record's own offset.
func localDay(t *testing.T, at time.Time, tzd any) Date {
	t.Helper()
	s, _ := tzd.(string)
	if s != "+03:00" && s != "+01:00" {
		t.Fatalf("timezone_offset %q, want +03:00 at home or +01:00 in London", s)
	}
	off, _ := time.Parse("-07:00", s)
	_, secs := off.Zone()
	lt := at.In(time.FixedZone(s, secs))
	return NewDate(lt.Year(), lt.Month(), lt.Day())
}

func TestWhoopEmitsTheDefaultLife(t *testing.T) {
	p, files := whoopEmit(t, Options{})
	days := p.Days()
	var naps, workouts int
	for _, d := range days {
		naps += len(p.Day(d).Naps)
		workouts += len(p.Day(d).Workouts)
	}

	// The file set: the profile, the body, e2e.json and every page, nothing else.
	pages := func(n int) int { return (n + whoopPageSize - 1) / whoopPageSize }
	if want := 3 + pages(len(days))*2 + pages(len(days)+naps) + pages(workouts); len(files) != want {
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("%d files, want %d:\n%s", len(files), want, strings.Join(names, "\n"))
	}
	for name, body := range files {
		if !strings.HasPrefix(name, "recordings/GET_v2_") && name != "e2e.json" {
			t.Errorf("%s is outside the mock's naming scheme", name)
		}
		if strings.HasPrefix(name, "recordings/") && strings.Contains(name, "__") && !strings.Contains(name, "__nextToken-") {
			t.Errorf("%s names a parameter other than nextToken; the mock drops limit and start", name)
		}
		if len(body) > 4<<20 {
			t.Errorf("%s is %d bytes, over 4 MB", name, len(body))
		}
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Errorf("%s does not parse: %v", name, err)
		}
	}

	// The member: what the profile step reads into the user row.
	var profile, body map[string]any
	_ = json.Unmarshal(files["recordings/GET_v2_user_profile_basic.json"], &profile)
	_ = json.Unmarshal(files["recordings/GET_v2_user_measurement_body.json"], &body)
	if whoopNum(t, profile, "user_id") != whoopUserID || profile["email"] != p.Person.Email || profile["first_name"] != "Ada" {
		t.Errorf("profile %v", profile)
	}
	if whoopNum(t, body, "height_meter") != p.Person.HeightM || whoopNum(t, body, "max_heart_rate") != float64(p.Person.MaxHR) ||
		whoopNum(t, body, "weight_kilogram") < 65 {
		t.Errorf("body measurement %v", body)
	}

	// Each collection walked the way the sync pages it, through the mock's lookup.
	cycles, n := whoopWalk(t, files, whoopCyclePath)
	if len(cycles) != len(days) || n != pages(len(days)) {
		t.Fatalf("%d cycles over %d pages, want %d over %d", len(cycles), n, len(days), pages(len(days)))
	}
	recoveries, _ := whoopWalk(t, files, whoopRecoveryPath)
	sleeps, _ := whoopWalk(t, files, whoopSleepPath)
	works, _ := whoopWalk(t, files, whoopWorkoutPath)
	if len(recoveries) != len(days) || len(sleeps) != len(days)+naps || len(works) != workouts {
		t.Fatalf("%d recoveries, %d sleeps, %d workouts; want %d, %d, %d",
			len(recoveries), len(sleeps), len(works), len(days), len(days)+naps, workouts)
	}

	inRange := func(d Date) bool { return !d.Before(p.Options.Start) && !d.After(p.Options.End) }
	cycleIDs := map[float64]bool{}
	cycleByID := map[float64]map[string]any{}
	var prevStart time.Time
	open := 0
	london := 0
	for i, c := range cycles {
		start := whoopInstant(t, c["start"])
		if i > 0 && !start.Before(prevStart) {
			t.Fatalf("cycles are not newest first at %d", i)
		}
		prevStart = start
		if c["timezone_offset"] == "+01:00" {
			london++
		}
		cycleIDs[whoopNum(t, c, "id")] = true
		cycleByID[whoopNum(t, c, "id")] = c
		whoopInstant(t, c["created_at"])
		whoopInstant(t, c["updated_at"])
		if c["end"] == nil {
			open++
			if i != 0 {
				t.Errorf("an open cycle that is not the current one: %v", c["id"])
			}
		} else if end := whoopInstant(t, c["end"]); !end.After(start) || end.Sub(start) > 30*time.Hour {
			t.Errorf("cycle %v spans %s", c["id"], end.Sub(start))
		} else if i > 0 && !end.Equal(whoopInstant(t, cycles[i-1]["start"])) {
			t.Errorf("cycle %v ends at %s, not where the next one starts", c["id"], end)
		}
		score, _ := c["score"].(map[string]any)
		if c["score_state"] != "SCORED" || score == nil {
			t.Fatalf("cycle %v is %v", c["id"], c["score_state"])
		}
		if s := whoopNum(t, score, "strain"); s <= 0 || s > 21 {
			t.Errorf("cycle %v strain %v", c["id"], s)
		}
		if kj := whoopNum(t, score, "kilojoule"); kj < 4000 || kj > 20000 {
			t.Errorf("cycle %v %v kJ", c["id"], kj)
		}
		if avg, mx := whoopNum(t, score, "average_heart_rate"), whoopNum(t, score, "max_heart_rate"); avg < 45 || avg > 90 || mx <= avg || mx > 192 {
			t.Errorf("cycle %v heart rate avg %v max %v", c["id"], avg, mx)
		}
	}
	if open != 1 || london != 5 || len(cycleIDs) != len(cycles) {
		t.Errorf("%d open cycles (want 1), %d in London (want 5), %d distinct ids of %d", open, london, len(cycleIDs), len(cycles))
	}

	// WHOOP's cycle runs from one sleep onset to the next: each night starts
	// the cycle it names, at the night's own start, and a nap lies inside
	// the cycle in progress, the one it names.
	cycleDay := map[float64]Date{}
	for _, s := range sleeps {
		c := cycleByID[whoopNum(t, s, "cycle_id")]
		if c == nil {
			continue
		}
		start, end := whoopInstant(t, s["start"]), whoopInstant(t, s["end"])
		cs := whoopInstant(t, c["start"])
		if s["nap"] == false {
			cycleDay[whoopNum(t, c, "id")] = localDay(t, end, s["timezone_offset"])
			if !cs.Equal(start) || c["timezone_offset"] != s["timezone_offset"] {
				t.Errorf("cycle %v starts at %s %v, not at the onset of the night %v that names it (%s %v)",
					c["id"], cs, c["timezone_offset"], s["id"], start, s["timezone_offset"])
			}
			continue
		}
		if start.Before(cs) || c["end"] != nil && end.After(whoopInstant(t, c["end"])) {
			t.Errorf("nap %v from %s to %s lies outside the cycle %v it names (%v to %v)", s["id"], start, end, c["id"], c["start"], c["end"])
		}
	}
	if len(cycleDay) != len(cycles) {
		t.Errorf("%d cycles opened by a night, want all %d", len(cycleDay), len(cycles))
	}

	sleepByID := map[string]map[string]any{}
	unscorable := 0
	for _, s := range sleeps {
		id, _ := s["id"].(string)
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
			t.Errorf("sleep id %q is not a v4 UUID", id)
		}
		sleepByID[id] = s
		if !cycleIDs[whoopNum(t, s, "cycle_id")] {
			t.Errorf("sleep %s names cycle %v, which is not served", id, s["cycle_id"])
		}
		start, end := whoopInstant(t, s["start"]), whoopInstant(t, s["end"])
		if d := localDay(t, end, s["timezone_offset"]); !inRange(d) {
			t.Errorf("sleep %s ends on %s, outside the range", id, d)
		}
		nap, ok := s["nap"].(bool)
		if !ok {
			t.Fatalf("sleep %s nap is %v", id, s["nap"])
		}
		if h := end.Sub(start).Hours(); nap && h > 2 || !nap && (h < 4 || h > 12) {
			t.Errorf("sleep %s (nap %v) lasts %.1f h", id, nap, h)
		}
		// A nap on a sick day is one WHOOP cannot score: no score at all.
		if ill := p.Ill(localDay(t, end, s["timezone_offset"])); nap && ill {
			unscorable++
			if s["score_state"] != "UNSCORABLE" || s["score"] != nil {
				t.Errorf("sick-day nap %s is %v with score %v", id, s["score_state"], s["score"])
			}
			continue
		} else if s["score_state"] != "SCORED" {
			t.Errorf("sleep %s is %v", id, s["score_state"])
		}
		score, _ := s["score"].(map[string]any)
		st, _ := score["stage_summary"].(map[string]any)
		inBed := whoopNum(t, st, "total_in_bed_time_milli")
		sum := 0.0
		for _, k := range []string{
			"total_awake_time_milli", "total_no_data_time_milli", "total_light_sleep_time_milli",
			"total_slow_wave_sleep_time_milli", "total_rem_sleep_time_milli",
		} {
			if whoopNum(t, st, k) < 0 {
				t.Errorf("sleep %s %s is negative", id, k)
			}
			sum += whoopNum(t, st, k)
		}
		if sum != inBed || inBed != float64(end.Sub(start).Milliseconds()) {
			t.Errorf("sleep %s stages sum to %v of %v in bed, span %v ms", id, sum, inBed, end.Sub(start).Milliseconds())
		}
		if _, ok := score["sleep_needed"].(map[string]any)["baseline_milli"]; !ok {
			t.Errorf("sleep %s has no sleep need", id)
		}
		for _, k := range []string{"respiratory_rate", "sleep_performance_percentage", "sleep_consistency_percentage", "sleep_efficiency_percentage"} {
			if v := whoopNum(t, score, k); v <= 0 || v > 100 {
				t.Errorf("sleep %s %s = %v", id, k, v)
			}
		}
	}

	for _, r := range recoveries {
		if !cycleIDs[whoopNum(t, r, "cycle_id")] {
			t.Errorf("recovery names cycle %v, which is not served", r["cycle_id"])
		}
		night := sleepByID[r["sleep_id"].(string)]
		if night == nil || night["nap"] != false || night["cycle_id"] != r["cycle_id"] {
			t.Errorf("recovery of cycle %v names sleep %v, not the night that opened its cycle", r["cycle_id"], r["sleep_id"])
		}
		whoopInstant(t, r["created_at"])
		score, _ := r["score"].(map[string]any)
		d := cycleDay[whoopNum(t, r, "cycle_id")]
		if !inRange(d) || whoopNum(t, score, "recovery_score") != float64(p.Day(d).Recovery) {
			t.Errorf("recovery on %s scores %v, persona says %d", d, score["recovery_score"], p.Day(d).Recovery)
		}
		// WHOOP calibrates a new strap over its first days.
		if want := !d.After(NewDate(2026, time.July, 16)); score["user_calibrating"] != want {
			t.Errorf("recovery on %s user_calibrating %v, want %v", d, score["user_calibrating"], want)
		}
		for k, lim := range map[string][2]float64{
			"resting_heart_rate": {38, 75}, "hrv_rmssd_milli": {12, 120}, "spo2_percentage": {90, 100}, "skin_temp_celsius": {32, 36},
		} {
			if v := whoopNum(t, score, k); v < lim[0] || v > lim[1] {
				t.Errorf("recovery on %s %s = %v", d, k, v)
			}
		}
	}

	if unscorable == 0 {
		t.Error("no sick-day nap in the default life; the unscorable check read nothing")
	}

	sports := map[string]bool{"running": true, "cycling": true, "weightlifting": true, "yoga": true}
	for _, w := range works {
		start, end := whoopInstant(t, w["start"]), whoopInstant(t, w["end"])
		if d := localDay(t, start, w["timezone_offset"]); !inRange(d) {
			t.Errorf("workout %v on %s, outside the range", w["id"], d)
		}
		if !sports[w["sport_name"].(string)] {
			t.Errorf("workout sport %v", w["sport_name"])
		}
		if _, ok := w["sport_id"]; ok {
			t.Errorf("workout %v carries sport_id, which WHOOP stopped sending", w["id"])
		}
		score, _ := w["score"].(map[string]any)
		zones, _ := score["zone_durations"].(map[string]any)
		total := 0.0
		for _, v := range zones {
			total += v.(float64)
		}
		if len(zones) != 6 || total != float64(end.Sub(start).Milliseconds()) {
			t.Errorf("workout %v zones %v sum to %v of %v ms", w["id"], zones, total, end.Sub(start).Milliseconds())
		}
		_, hasDistance := score["distance_meter"]
		if want := w["sport_name"] == "running" || w["sport_name"] == "cycling"; hasDistance != want {
			t.Errorf("workout %v (%v) distance %v", w["id"], w["sport_name"], score["distance_meter"])
		}
		if avg, mx := whoopNum(t, score, "average_heart_rate"), whoopNum(t, score, "max_heart_rate"); avg < 70 || mx <= avg || mx > 192 {
			t.Errorf("workout %v heart rate avg %v max %v", w["id"], avg, mx)
		}
		if s := whoopNum(t, score, "strain"); s < 0 || s > 21 {
			t.Errorf("workout %v strain %v", w["id"], s)
		}
	}

	// The runner's config for this data.
	var cfg map[string]any
	if err := json.Unmarshal(files["e2e.json"], &cfg); err != nil {
		t.Fatal(err)
	}
	account, _ := cfg["account"].(map[string]any)
	for _, toggle := range []string{"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep", "enabledWorkouts"} {
		if account[toggle] != true {
			t.Errorf("e2e.json leaves %s off", toggle)
		}
	}
	if account["backfillDepth"] != "all" || cfg["mockPort"] != float64(whoopMockPort) || cfg["settleSeconds"] == nil || cfg["continueRounds"] == nil {
		t.Errorf("e2e.json %v", cfg)
	}
}

// The spellings whoopsync and the recorded fixtures share: floats keep their
// decimal point, instants are UTC milliseconds.
func TestWhoopNumbersAreSpelledAsWhoopSendsThem(t *testing.T) {
	for v, want := range map[float64]string{55: "55.0", 96.4375: "96.4375", 17.04960001: "17.0496", 100: "100.0", 0.5: "0.5"} {
		if got := whoopDec(v, 4); string(got) != want {
			t.Errorf("whoopDec(%v) = %s, want %s", v, got, want)
		}
	}
	_, files := whoopEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	raw := string(files["recordings/GET_v2_recovery.json"])
	if !regexp.MustCompile(`"recovery_score": \d+\.\d`).MatchString(raw) {
		t.Errorf("recovery_score is not spelled as a float:\n%s", raw)
	}
}

func TestWhoopIsDeterministic(t *testing.T) {
	_, a := whoopEmit(t, Options{})
	_, b := whoopEmit(t, Options{})
	if len(a) != len(b) {
		t.Fatalf("%d files, then %d", len(a), len(b))
	}
	for name, body := range a {
		if !bytes.Equal(body, b[name]) {
			t.Fatalf("%s differs between two runs", name)
		}
	}
	_, c := whoopEmit(t, Options{Seed: "other"})
	if bytes.Equal(a["recordings/GET_v2_cycle.json"], c["recordings/GET_v2_cycle.json"]) {
		t.Fatal("another seed wrote the same cycles")
	}
}

func TestWhoopThreeDaysIsSmall(t *testing.T) {
	_, files := whoopEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	var size int
	for _, b := range files {
		size += len(b)
	}
	// The profile, the body, e2e.json and one page per collection.
	if len(files) != 7 || size > 40_000 {
		t.Fatalf("three days are %d files and %d bytes, want 7 files under 40 KB", len(files), size)
	}
	cycles, _ := whoopWalk(t, files, whoopCyclePath)
	if len(cycles) != 3 || cycles[0]["end"] != nil {
		t.Fatalf("%d cycles, the newest ending %v; want 3 with the newest open", len(cycles), cycles[0]["end"])
	}
	var page whoopPage
	_ = json.Unmarshal(files["recordings/GET_v2_cycle.json"], &page)
	if page.NextToken != "" {
		t.Fatalf("one page answers a next_token %q", page.NextToken)
	}
	if _, ok := files["recordings/GET_v2_activity_workout.json"]; !ok {
		t.Fatal("a collection with no records still answers an empty page")
	}
}

// A new strap calibrates over its first three days, whatever day the range
// starts on.
func TestWhoopCalibratesTheRangesFirstThreeRecoveries(t *testing.T) {
	p, files := whoopEmit(t, Options{Start: NewDate(2026, time.July, 1), End: NewDate(2026, time.July, 31)})
	recoveries, _ := whoopWalk(t, files, whoopRecoveryPath)
	if len(recoveries) != len(p.Days()) {
		t.Fatalf("%d recoveries over %d days", len(recoveries), len(p.Days()))
	}
	sort.Slice(recoveries, func(i, j int) bool {
		return whoopInstant(t, recoveries[i]["created_at"]).Before(whoopInstant(t, recoveries[j]["created_at"]))
	})
	for i, r := range recoveries {
		score, _ := r["score"].(map[string]any)
		if want := i < 3; score["user_calibrating"] != want {
			t.Errorf("recovery %d, created %v: user_calibrating %v, want %v", i+1, r["created_at"], score["user_calibrating"], want)
		}
	}
}
