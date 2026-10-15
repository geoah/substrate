package synth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fitEmit(t *testing.T, o Options) (*Persona, map[string][]byte) {
	t.Helper()
	p, err := NewPersona(o)
	if err != nil {
		t.Fatal(err)
	}
	out, err := GoogleFit{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Deliveries) != 0 {
		t.Fatalf("Google Fit is mock-driven, yet it emitted %d deliveries", len(out.Deliveries))
	}
	// The runner installs the provider and connects the account; the loader
	// only imports the adapter.
	if out.Setup.Provider != "" || len(out.Setup.Records) != 0 ||
		len(out.Setup.Samples) != 1 || out.Setup.Samples[0] != "samples.substrate.reamde.dev/healthgooglefit" {
		t.Fatalf("setup %+v, want only the healthgooglefit sample", out.Setup)
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

// fitLookup answers a request the way the mock server does for googlefit:
// the exact name, then without the pagination parameters, then the bare
// path; then the same three over the path with its day window dropped
// (PATH_RELAXATIONS). It answers the body and whether the window was dropped.
func fitLookup(files map[string][]byte, path string, pairs ...Pair) ([]byte, bool, bool) {
	var thin []Pair
	for _, p := range pairs {
		if !PaginationParams[p.Key] {
			thin = append(thin, p)
		}
	}
	paths := []string{path}
	if relaxed := regexp.MustCompile(`/datasets/\d+-\d+$`).ReplaceAllString(path, "/datasets"); relaxed != path {
		paths = append(paths, relaxed)
	}
	for i, p := range paths {
		for _, name := range []string{RecordingName("GET", p, pairs...), RecordingName("GET", p, thin...), RecordingName("GET", p)} {
			if b, ok := files["recordings/"+name]; ok {
				return b, i == 1, true
			}
		}
	}
	return nil, false, false
}

type fitDataset struct {
	DataSourceID  string           `json:"dataSourceId"`
	NextPageToken string           `json:"nextPageToken"`
	Point         []map[string]any `json:"point"`
}

// fitReadDay is googlefitsync's `_read_day`: one UTC day of one source,
// `limit` on every page, `pageToken` from the page before, the points that
// STARTED on the day. It answers them and whether the relaxed file served.
func fitReadDay(t *testing.T, files map[string][]byte, sid string, day time.Time) ([]map[string]any, bool) {
	t.Helper()
	lo := day.UnixNano()
	hi := day.Add(24 * time.Hour).UnixNano()
	path := fitDatasetPath(sid, lo, hi)
	var out []map[string]any
	token, relaxedAny := "", false
	for pages := 0; ; pages++ {
		pairs := []Pair{{"limit", "1000"}}
		if token != "" {
			pairs = append(pairs, Pair{"pageToken", token})
		}
		raw, relaxed, ok := fitLookup(files, path, pairs...)
		if !ok {
			t.Fatalf("the mock would answer %s (%v) with 404", path, pairs)
		}
		relaxedAny = relaxedAny || relaxed
		var page fitDataset
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if page.DataSourceID != sid {
			t.Fatalf("%s answers dataSourceId %q", path, page.DataSourceID)
		}
		if len(page.Point) > 1000 {
			t.Fatalf("%s page %d holds %d points, more than the limit", path, pages, len(page.Point))
		}
		for _, p := range page.Point {
			if s := fitNanos(t, p["startTimeNanos"]); s >= lo && s < hi {
				out = append(out, p)
			}
		}
		if page.NextPageToken == "" || page.NextPageToken == token {
			return out, relaxedAny
		}
		token = page.NextPageToken
	}
}

func fitNanos(t *testing.T, v any) int64 {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%v (%T) is not Google's decimal-string int64", v, v)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// fitValue is one positional value entry's number.
func fitValue(t *testing.T, p map[string]any, i int, field string) float64 {
	t.Helper()
	vals, _ := p["value"].([]any)
	if i >= len(vals) {
		t.Fatalf("point %v has %d value entries, want entry %d", p, len(vals), i)
	}
	v, ok := vals[i].(map[string]any)[field].(float64)
	if !ok {
		t.Fatalf("point %v value[%d].%s is %v", p, i, field, vals[i])
	}
	return v
}

func TestGoogleFitEmitsTheDefaultLife(t *testing.T) {
	t.Parallel()
	p, files := fitEmit(t, Options{})
	days := p.Days()
	var nights, naps, workouts, weighIns, pressures, glucose int
	for _, d := range days {
		day := p.Day(d)
		nights++
		naps += len(day.Naps)
		workouts += len(day.Workouts)
		if day.WeightKg != 0 {
			weighIns++
		}
		if day.BloodPressure != nil {
			pressures++
		}
		if day.Glucose != nil {
			glucose++
		}
	}

	// Every file is a recording the mock can name, or e2e.json; it parses and
	// stays under 4 MB.
	var size int
	for name, body := range files {
		size += len(body)
		if name != "e2e.json" && !strings.HasPrefix(name, "recordings/GET_fitness_v1_users_me_") &&
			name != "recordings/GET_oauth2_v3_userinfo.json" {
			t.Errorf("%s is outside the mock's naming scheme", name)
		}
		if strings.Contains(name, "_datasets_") && !strings.HasSuffix(name, "__limit-1000.json") {
			t.Errorf("%s is a day the sync asks for with limit=1000, under another name", name)
		}
		if len(body) > 4<<20 {
			t.Errorf("%s is %d bytes, over 4 MB", name, len(body))
		}
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Errorf("%s does not parse: %v", name, err)
		}
	}
	t.Logf("90 days: %d files, %d bytes", len(files), size)

	// The listing: every derived stream, and every raw origin a point names.
	var listing struct {
		DataSource []map[string]any `json:"dataSource"`
	}
	raw, _, _ := fitLookup(files, fitAPI+"/dataSources")
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	listed := map[string]map[string]any{}
	for _, ds := range listing.DataSource {
		listed[ds["dataStreamId"].(string)] = ds
	}
	if len(listed) != len(fitDerived)+16 {
		t.Fatalf("%d data sources listed, want %d derived and 16 raw", len(listed), len(fitDerived))
	}
	watch := listed[fitRawHR]
	if dev, _ := watch["device"].(map[string]any); watch["type"] != "raw" || dev["model"] != "Pixel Watch 2" || dev["type"] != "watch" || dev["uid"] != "8d1f3c7a" {
		t.Errorf("the watch's heart rate source is %v", watch)
	}

	// Every day the sync walks, read the way the sync reads it: every point
	// comes back exactly once, on the UTC day it started, and only through an
	// exact recording.
	first := p.Options.Start.Add(-1).Midnight(time.UTC)
	last := p.Options.End.Add(1).Midnight(time.UTC)
	kept := map[string][]map[string]any{}
	exactDays := map[string]int{}
	for _, d := range fitDerived {
		sid := fitDerivedID(d.dtype, d.stream)
		if listed[sid] == nil || listed[sid]["type"] != "derived" {
			t.Fatalf("%s is not listed as derived", sid)
		}
		for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
			pts, relaxed := fitReadDay(t, files, sid, day)
			if relaxed && len(pts) != 0 {
				t.Fatalf("the relaxed file for %s answered %d points on %s", sid, len(pts), day.Format(time.DateOnly))
			}
			if !relaxed {
				exactDays[d.key]++
			}
			kept[d.key] = append(kept[d.key], pts...)
		}
	}
	for key, want := range map[string]int{
		"hr": len(days) + 1, "sleep": len(days) + 1, "kcal": len(days) + 1, "spo2": len(days),
		"weight": weighIns, "fat": weighIns, "bp": pressures, "height": 1, "glucose": glucose,
	} {
		if exactDays[key] != want {
			t.Errorf("%s has %d exact days, want %d", key, exactDays[key], want)
		}
	}

	// The points, with the shapes googlefitsync and adaptgooglefit read.
	origins := map[string]bool{fitRawHR: true, fitRawCuffHR: true}
	var cuff int
	for _, pt := range kept["hr"] {
		if pt["dataTypeName"] != "com.google.heart_rate.bpm" || pt["startTimeNanos"] != pt["endTimeNanos"] || !origins[pt["originDataSourceId"].(string)] {
			t.Fatalf("heart rate point %v", pt)
		}
		if v := fitValue(t, pt, 0, "fpVal"); v < 35 || v > 210 {
			t.Fatalf("heart rate %v", v)
		}
		if fitNanos(t, pt["modifiedTimeMillis"])*1_000_000 < fitNanos(t, pt["endTimeNanos"]) {
			t.Fatalf("heart rate point %v was modified before it ended", pt)
		}
		if pt["originDataSourceId"] == fitRawCuffHR {
			cuff++
		}
	}
	if cuff != pressures || len(kept["hr"]) < 288*len(days) {
		t.Errorf("%d heart rate points (%d from the cuff), want at least %d and %d", len(kept["hr"]), cuff, 288*len(days), pressures)
	}
	for _, pt := range kept["steps"] {
		s, e := fitNanos(t, pt["startTimeNanos"]), fitNanos(t, pt["endTimeNanos"])
		if pt["originDataSourceId"] != fitRawSteps || time.Duration(e-s) != 15*time.Minute || fitValue(t, pt, 0, "intVal") <= 0 {
			t.Fatalf("step point %v", pt)
		}
	}
	codes := map[float64]bool{1: true, 4: true, 5: true, 6: true}
	for _, pt := range kept["sleep"] {
		if pt["originDataSourceId"] != fitRawSleep || !codes[fitValue(t, pt, 0, "intVal")] {
			t.Fatalf("sleep segment %v", pt)
		}
	}
	for _, pt := range kept["bp"] {
		if vals := pt["value"].([]any); len(vals) != 4 || fitValue(t, pt, 2, "intVal") != 2 || fitValue(t, pt, 3, "intVal") != 3 ||
			fitValue(t, pt, 0, "fpVal") <= fitValue(t, pt, 1, "fpVal") {
			t.Fatalf("blood pressure point %v", pt)
		}
	}
	for _, pt := range kept["spo2"] {
		if vals := pt["value"].([]any); len(vals) != 5 || fitValue(t, pt, 0, "fpVal") < 90 || fitValue(t, pt, 0, "fpVal") > 100 {
			t.Fatalf("oxygen point %v", pt)
		}
	}
	for _, pt := range kept["glucose"] {
		if vals := pt["value"].([]any); len(vals) != 5 || pt["originDataSourceId"] != fitRawGlucose ||
			fitValue(t, pt, 0, "fpVal") < 3.5 || fitValue(t, pt, 0, "fpVal") > 8 || fitValue(t, pt, 1, "intVal") != 2 {
			t.Fatalf("glucose point %v", pt)
		}
	}
	if len(kept["glucose"]) != glucose || glucose == 0 {
		t.Errorf("%d glucose points, the persona's meter read %d", len(kept["glucose"]), glucose)
	}
	for _, pt := range kept["weight"] {
		if pt["originDataSourceId"] != fitRawWeight || fitValue(t, pt, 0, "fpVal") < 65 || fitValue(t, pt, 0, "fpVal") > 80 {
			t.Fatalf("weight point %v", pt)
		}
	}
	// Every point the adapter resolves to a device names a listed raw source.
	for _, key := range []string{"hr", "steps", "kcal", "weight", "fat", "height", "sleep", "spo2", "bp", "glucose"} {
		for _, pt := range kept[key] {
			if src := listed[pt["originDataSourceId"].(string)]; src == nil || src["type"] != "raw" {
				t.Fatalf("%s point names %v, which the listing does not hold as raw", key, pt["originDataSourceId"])
			}
		}
	}
	// What the adapter turns into observed points, within the brief's
	// order of magnitude: heart rate every 5 minutes is most of it.
	var observed int
	for _, key := range []string{"hr", "steps", "kcal", "weight", "fat", "height", "sleep", "spo2", "bp"} {
		observed += len(kept[key])
	}
	t.Logf("observed points the adapter reads: %d (heart rate %d, steps %d, energy %d, sleep %d)",
		observed, len(kept["hr"]), len(kept["steps"]), len(kept["kcal"]), len(kept["sleep"]))
	if observed < 25_000 || observed > 45_000 {
		t.Errorf("%d observed points", observed)
	}

	// The sessions: one per night and nap, one per workout, runs from Strava.
	var window struct {
		Session []map[string]any `json:"session"`
	}
	raw, _, _ = fitLookup(files, fitAPI+"/sessions", Pair{"startTime", "2025-10-12T00:00:00Z"}, Pair{"endTime", "2026-10-12T03:00:00Z"})
	if err := json.Unmarshal(raw, &window); err != nil {
		t.Fatal(err)
	}
	if len(window.Session) != nights+naps+workouts {
		t.Fatalf("%d sessions, want %d nights, %d naps and %d workouts", len(window.Session), nights, naps, workouts)
	}
	byType := map[string]int{}
	for _, s := range window.Session {
		app := s["application"].(map[string]any)["packageName"]
		byType[fmt.Sprintf("%v %v", s["activityType"], app)]++
		a, b := fitNanos(t, s["startTimeMillis"]), fitNanos(t, s["endTimeMillis"])
		hours := time.Duration(b-a) * time.Millisecond
		if s["activityType"] == float64(fitSleeping) && s["name"] == "Sleep" && (hours < 4*time.Hour || hours > 12*time.Hour) {
			t.Errorf("night %v lasts %s", s["id"], hours)
		}
		if app == "com.strava" && !strings.HasPrefix(s["id"].(string), "strava-") {
			t.Errorf("a Strava session with id %v", s["id"])
		}
	}
	if byType["72 com.google.android.apps.fitness"] != nights+naps || byType["100 com.google.android.apps.fitness"] == 0 ||
		byType["80 com.google.android.apps.fitness"] == 0 || byType["1 com.google.android.apps.fitness"] == 0 || byType["8 com.strava"] == 0 {
		t.Errorf("sessions by type and app: %v", byType)
	}
	var feed struct {
		Session       []map[string]any `json:"session"`
		NextPageToken string           `json:"nextPageToken"`
	}
	raw, _, _ = fitLookup(files, fitAPI+"/sessions", Pair{"includeDeleted", "true"})
	_ = json.Unmarshal(raw, &feed)
	if len(feed.Session) == 0 || len(feed.Session) >= len(window.Session) || feed.NextPageToken == "" {
		t.Errorf("the feed's first page holds %d sessions and token %q", len(feed.Session), feed.NextPageToken)
	}
	raw, _, ok := fitLookup(files, fitAPI+"/sessions", Pair{"includeDeleted", "true"}, Pair{"pageToken", feed.NextPageToken})
	var end struct {
		Session       []any  `json:"session"`
		NextPageToken string `json:"nextPageToken"`
	}
	_ = json.Unmarshal(raw, &end)
	if !ok || len(end.Session) != 0 || end.NextPageToken != feed.NextPageToken {
		t.Errorf("the feed's last page holds %d sessions and token %q", len(end.Session), end.NextPageToken)
	}

	// The runner's config for this data.
	var cfg map[string]any
	if err := json.Unmarshal(files["e2e.json"], &cfg); err != nil {
		t.Fatal(err)
	}
	account, _ := cfg["account"].(map[string]any)
	for _, toggle := range []string{"enabledActivity", "enabledBody", "enabledHeartRate", "enabledSleep", "enabledOxygenSaturation", "enabledBloodPressure", "enabledBloodGlucose"} {
		if account[toggle] != true {
			t.Errorf("e2e.json leaves %s off", toggle)
		}
	}
	if account["backfillDepth"] != "last1y" || account["timezone"] != "Europe/Athens" || cfg["mockPort"] != float64(fitMockPort) {
		t.Errorf("e2e.json %v", cfg)
	}
	var userinfo map[string]any
	raw, _, _ = fitLookup(files, "/oauth2/v3/userinfo")
	_ = json.Unmarshal(raw, &userinfo)
	if userinfo["email"] != p.Person.Email {
		t.Errorf("userinfo %v", userinfo)
	}
}

func TestGoogleFitIsDeterministic(t *testing.T) {
	t.Parallel()
	_, a := fitEmit(t, Options{})
	_, b := fitEmit(t, Options{})
	if len(a) != len(b) {
		t.Fatalf("%d files, then %d", len(a), len(b))
	}
	for name, body := range a {
		if !bytes.Equal(body, b[name]) {
			t.Fatalf("%s differs between two runs", name)
		}
	}
	_, c := fitEmit(t, Options{Seed: "other"})
	listing := "recordings/" + RecordingName("GET", fitAPI+"/sessions")
	if bytes.Equal(a[listing], c[listing]) {
		t.Fatal("another seed wrote the same sessions")
	}
}

func TestGoogleFitThreeDaysIsSmall(t *testing.T) {
	t.Parallel()
	p, files := fitEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	var size int
	var exact []string
	for name, b := range files {
		size += len(b)
		if strings.Contains(name, "_datasets_") {
			exact = append(exact, name)
		}
	}
	sort.Strings(exact)
	// Listing, window, feed and its last page, userinfo, e2e.json, one relaxed
	// file per derived stream, and at most four UTC days per stream.
	if len(files) < 6+len(fitDerived) || len(exact) > 4*len(fitDerived) || size > 1_500_000 {
		t.Fatalf("three days are %d files (%d exact days) and %d bytes", len(files), len(exact), size)
	}
	var cfg map[string]any
	_ = json.Unmarshal(files["e2e.json"], &cfg)
	if cfg["account"].(map[string]any)["backfillDepth"] != "last30d" {
		t.Errorf("three days back off a backfill of %v", cfg["account"])
	}
	pts, relaxed := fitReadDay(t, files, fitDerivedID("com.google.heart_rate.bpm", "merge_heart_rate_bpm"),
		p.Options.Start.Add(1).Midnight(time.UTC))
	if relaxed || len(pts) < 288 {
		t.Fatalf("a whole UTC day of heart rate is %d points (relaxed %v)", len(pts), relaxed)
	}
}

// The watch reads a night whichever local day it falls in: between bedtime
// and midnight its points are the sleeping persona's, not the waking curve
// of the day that is ending.
func TestGoogleFitHeartRateFollowsTheNextNight(t *testing.T) {
	t.Parallel()
	p, files := fitEmit(t, Options{Start: NewDate(2026, time.September, 12), End: NewDate(2026, time.September, 14)})
	sid := fitDerivedID("com.google.heart_rate.bpm", "merge_heart_rate_bpm")
	var nights []*Sleep
	for _, d := range p.Days() {
		nights = append(nights, p.Day(d.Add(1)).Sleep)
	}
	sum, truth, n := 0.0, 0.0, 0
	for day := p.Options.Start.Midnight(time.UTC); day.Before(p.Options.End.Add(1).Midnight(time.UTC)); day = day.AddDate(0, 0, 1) {
		pts, _ := fitReadDay(t, files, sid, day)
		for _, pt := range pts {
			at := time.Unix(0, fitNanos(t, pt["startTimeNanos"]))
			if pt["originDataSourceId"] != fitRawHR {
				continue
			}
			for _, s := range nights {
				// Before the local midnight that ends the evening's day.
				if !at.Before(s.Start) && at.Before(s.End) && at.Before(s.Date.Midnight(p.Zone(s.Date))) {
					sum += fitValue(t, pt, 0, "fpVal")
					truth += float64(s.HR(at))
					n++
				}
			}
		}
	}
	if n < 6 {
		t.Fatalf("%d heart rate points between bedtime and midnight; the check reads nothing", n)
	}
	if got, want := sum/float64(n), truth/float64(n); math.Abs(got-want) > 3 {
		t.Fatalf("heart rate averages %.1f between bedtime and midnight over %d points, the sleeping persona's %.1f", got, n, want)
	}
}
