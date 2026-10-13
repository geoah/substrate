package providertest

// The contract between internal/synth's Google Fit emitter and the shipped
// googlefit closure: three generated days, served by the mock server's own
// lookup (exact per-day names, then the relaxed path for every other day),
// walked by the real `googlefitsync`, then delivered to the
// `healthgooglefit` adapter by its own trigger. A change to a shape the sync
// or the adapter reads that the emitter does not follow fails here.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/synth"
)

// synthFitFake is fakeFit serving a generated recordings directory by the
// mock server's lookup for googlefit (internal/providere2e/runner/
// mockserver.py): the exact name, then without the pagination parameters,
// then the bare path, then the same three over the path with the dataset's
// day window dropped (PATH_RELAXATIONS). A request nothing answers is 502,
// because a 404 tells the sync a data source is gone.
type synthFitFake struct {
	*fakeFit
	misses  []string
	relaxed int
	// days is every dataset read, by data source, as the UTC day it names.
	days map[string]map[string]bool
}

var fitWindowSegment = regexp.MustCompile(`/datasets/(\d+)-\d+$`)

func newSynthFitFake(t *testing.T, dir string) *synthFitFake {
	t.Helper()
	f := &synthFitFake{fakeFit: &fakeFit{}, days: map[string]map[string]bool{}}
	f.serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !bearer(w, r) {
			return
		}
		path := r.URL.Path
		var relaxed []string
		if m := fitWindowSegment.FindStringSubmatch(path); m != nil {
			sid := strings.TrimPrefix(strings.TrimSuffix(path[:len(path)-len(m[0])], "/"), "/fitness/v1/users/me/dataSources/")
			start, _ := strconv.ParseInt(m[1], 10, 64)
			f.mu.Lock()
			if f.days[sid] == nil {
				f.days[sid] = map[string]bool{}
			}
			f.days[sid][time.Unix(0, start).UTC().Format(time.DateOnly)] = true
			f.mu.Unlock()
			relaxed = append(relaxed, fitWindowSegment.ReplaceAllString(path, "/datasets"))
		}
		if _, raw, i, ok := synthRecording(dir, r, relaxed...); ok {
			if i == 1 {
				f.mu.Lock()
				f.relaxed++
				f.mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
			return
		}
		f.mu.Lock()
		f.misses = append(f.misses, r.URL.String())
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	return f
}

// synthFitKept is every generated point the sync keeps, by data source: the
// points of each per-day recording that started on its day, the rule the
// sync applies to Google's overlapping answers.
func synthFitKept(t *testing.T, out *synth.Output) map[string][]map[string]any {
	t.Helper()
	kept := map[string][]map[string]any{}
	for _, f := range out.Files {
		if !strings.Contains(f.Path, "_datasets_") {
			continue
		}
		var page struct {
			MinStartTimeNs string           `json:"minStartTimeNs"`
			MaxEndTimeNs   string           `json:"maxEndTimeNs"`
			DataSourceID   string           `json:"dataSourceId"`
			Point          []map[string]any `json:"point"`
		}
		if err := json.Unmarshal(f.Body, &page); err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		lo, hi := asInt(t, page.MinStartTimeNs), asInt(t, page.MaxEndTimeNs)
		for _, p := range page.Point {
			if s := asInt(t, p["startTimeNanos"]); s >= lo && s < hi {
				kept[page.DataSourceID] = append(kept[page.DataSourceID], p)
			}
		}
	}
	return kept
}

// synthFitDays is the UTC days one data source's kept points start on.
func synthFitDays(t *testing.T, points []map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, p := range points {
		out[time.Unix(0, asInt(t, p["startTimeNanos"])).UTC().Format(time.DateOnly)] = true
	}
	return out
}

// synthFitHours is the UTC hours the points of one origin start in.
func synthFitHours(t *testing.T, points []map[string]any, origin string) int {
	t.Helper()
	hours := map[int64]bool{}
	for _, p := range points {
		if origin == "" || p["originDataSourceId"] == origin {
			hours[asInt(t, p["startTimeNanos"])/int64(time.Hour)] = true
		}
	}
	return len(hours)
}

func TestSynthGoogleFitSyncsAndAdapts(t *testing.T) {
	requireUV(t)
	t.Parallel()
	// Three local days around the latest Wednesday or Saturday (a glucose
	// meter morning, a weigh-in and a workout either side) that ends two or
	// more days ago, so the default 30-day walk covers them, the UTC evening
	// before the first included, whenever this runs.
	meter := utcToday().AddDate(0, 0, -3)
	for meter.Weekday() != time.Wednesday && meter.Weekday() != time.Saturday {
		meter = meter.AddDate(0, 0, -1)
	}
	start, end := meter.AddDate(0, 0, -1), meter.AddDate(0, 0, 1)
	p, err := synth.NewPersona(synth.Options{
		Start: synth.NewDate(start.Year(), start.Month(), start.Day()),
		End:   synth.NewDate(end.Year(), end.Month(), end.Day()),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := synth.GoogleFit{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := synth.WriteOutput(dir, synth.GoogleFit{}, out); err != nil {
		t.Fatal(err)
	}
	var nights, naps, workouts, weighIns, pressures, glucose, stages int
	sports := map[string]int{}
	for _, d := range p.Days() {
		day := p.Day(d)
		nights++
		naps += len(day.Naps)
		workouts += len(day.Workouts)
		for _, s := range append([]*synth.Sleep{day.Sleep}, day.Naps...) {
			for _, st := range s.Stages {
				if st.End.UnixMilli() > st.Start.UnixMilli() {
					stages++
				}
			}
		}
		for _, w := range day.Workouts {
			sports[string(w.Sport)]++
		}
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
	if workouts == 0 || weighIns == 0 || glucose == 0 {
		t.Fatalf("the three days hold %d workouts, %d weigh-ins and %d glucose readings; the window needs each", workouts, weighIns, glucose)
	}

	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	importVocabulary(t, ds, "healthgooglefit")
	fake := newSynthFitFake(t, filepath.Join(dir, "googlefit", "recordings"))
	googlefitAccountWith(t, ds, fitAllToggles...)
	stamps := googlefitSync(t, ds, fake.fakeFit)
	healthDrain(t, ds)

	// The walk: every request answered; every derived source the sync reads
	// asked for each of the 31 days, distance never.
	if len(fake.misses) != 0 {
		t.Fatalf("the generated recordings did not answer %v", fake.misses)
	}
	if len(stamps) == 0 || stamps[len(stamps)-1]["syncMessage"] != "ok" {
		t.Fatalf("the sync stamped %v", stamps)
	}
	if fake.relaxed == 0 {
		t.Error("no read fell back to a relaxed recording, yet most days have no points")
	}
	kept := synthFitKept(t, out)
	var listing struct {
		DataSource []map[string]any `json:"dataSource"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "googlefit", "recordings", synth.RecordingName("GET", "/fitness/v1/users/me/dataSources")))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	wantRows := map[string]int{}
	for _, src := range listing.DataSource {
		sid := src["dataStreamId"].(string)
		if src["type"] != "derived" {
			continue
		}
		read := len(fake.days[sid])
		if strings.HasPrefix(sid, "derived:com.google.distance.delta:") {
			if read != 0 {
				t.Errorf("the sync read distance on %d days; the bundle asks for no location scope", read)
			}
			continue
		}
		if read != 31 {
			t.Errorf("%s was read on %d days, want the 31 of the walk", sid, read)
		}
		wantRows[sid] = len(synthFitDays(t, kept[sid]))
	}

	// The mirror: every listed source, a dataset row per day with points per
	// derived source, a session per night, nap and workout.
	if n := countLive(t, ds, googlefitSourceType); n != len(listing.DataSource) {
		t.Errorf("%d data source rows, want the %d the listing names", n, len(listing.DataSource))
	}
	got := map[string]int{}
	for _, row := range listLive(t, ds, googlefitDatasetType) {
		got[row.Properties["dataSourceId"].(string)]++
	}
	for sid, want := range wantRows {
		if got[sid] != want {
			t.Errorf("%d dataset rows of %s, want %d", got[sid], sid, want)
		}
	}
	if n := countLive(t, ds, googlefitSessionType); n != nights+naps+workouts {
		t.Errorf("%d sessions, want %d nights, %d naps and %d workouts", n, nights, naps, workouts)
	}
	hrKept := kept[fitHeartRate]
	middle := start.AddDate(0, 0, 1)
	row := mustGet(t, ds, googlefitDatasetType, fitDatasetRow(fitHeartRate, middle))
	var onDay int
	for _, pt := range hrKept {
		if time.Unix(0, asInt(t, pt["startTimeNanos"])).UTC().Format(time.DateOnly) == middle.Format(time.DateOnly) {
			onDay++
		}
	}
	if pts := fitPoints(row); len(pts) != onDay || onDay < 288 {
		t.Errorf("the heart rate day %s holds %d points, the recording %d", middle.Format(time.DateOnly), len(pts), onDay)
	}

	// The adapter: hourly traces per device, a reading per weigh-in, oxygen
	// and blood pressure reading, a coded amount per sleep stage, a sleep per
	// night and nap, a workout per session, and the sources behind them.
	const (
		watchHR = "raw:com.google.heart_rate.bpm:com.google.android.apps.fitness:Google:Pixel Watch 2:8d1f3c7a:"
		cuffHR  = "raw:com.google.heart_rate.bpm:com.withings.wiscale2:Withings:BPM Connect:7c03d5e1:"
		kcal    = "derived:com.google.calories.expended:com.google.android.gms:merge_calories_expended"
	)
	traces := map[string]int{}
	for _, tr := range listLive(t, ds, obsTrace) {
		src := strings.TrimPrefix(storedRefPath(tr.Properties["source"]), obsSource+"/googlefit/"+googlefitAccount+"/")
		traces[strings.TrimPrefix(storedRefPath(tr.Properties["metric"]), obsMetric+"/")+" "+src]++
	}
	wantTraces := map[string]int{
		"health/heart_rate " + fitWatchSlug:      synthFitHours(t, hrKept, watchHR),
		"health/steps " + fitWatchSlug:           synthFitHours(t, kept[fitSteps], ""),
		"health/energy_expended " + fitWatchSlug: synthFitHours(t, kept[kcal], ""),
	}
	if pressures > 0 {
		wantTraces["health/heart_rate withings-bpm-connect.7c03d5e1"] = synthFitHours(t, hrKept, cuffHR)
	}
	for k, want := range wantTraces {
		if traces[k] != want {
			t.Errorf("%d traces of %s, want %d", traces[k], k, want)
		}
	}
	if len(traces) != len(wantTraces) {
		t.Errorf("traces %v, want only %v", traces, wantTraces)
	}
	byMetric := map[string]int{}
	for _, o := range listLive(t, ds, obsObservation) {
		byMetric[strings.TrimPrefix(storedRefPath(o.Properties["metric"]), obsMetric+"/")]++
	}
	wantObs := map[string]int{
		"health/body_mass": weighIns, "health/body_fat_fraction": weighIns, "health/height": 1,
		"health/spo2": nights, "health/sleep_stage": stages, "health/blood_glucose": glucose,
	}
	if pressures > 0 {
		wantObs["health/systolic_blood_pressure"] = pressures
		wantObs["health/diastolic_blood_pressure"] = pressures
	}
	for metric, want := range wantObs {
		if byMetric[metric] != want {
			t.Errorf("%d observations of %s, want %d", byMetric[metric], metric, want)
		}
	}
	if len(byMetric) != len(wantObs) {
		t.Errorf("observations %v, want only %v", byMetric, wantObs)
	}
	if n := countLive(t, ds, healthSleep); n != nights+naps {
		t.Errorf("%d sleeps, want %d", n, nights+naps)
	}
	codes := map[string]string{"running": "8", "cycling": "1", "strength": "80", "yoga": "100"}
	bySport := map[string]int{}
	for _, w := range listLive(t, ds, healthWorkout) {
		bySport[w.Properties["sportSource"].(string)]++
		if sport, ok := w.Properties["sport"].(string); ok && codes[sport] != w.Properties["sportSource"] {
			t.Errorf("workout %s is %s from code %v", w.ID, sport, w.Properties["sportSource"])
		}
	}
	for sport, n := range sports {
		if bySport[codes[sport]] != n {
			t.Errorf("%d workouts with code %s, want %d %s", bySport[codes[sport]], codes[sport], n, sport)
		}
	}
	sources := map[string]bool{}
	for _, s := range listLive(t, ds, obsSource) {
		sources[strings.TrimPrefix(s.ID, "googlefit/"+googlefitAccount+"/")] = true
	}
	for _, slug := range []string{fitWatchSlug, fitScaleSlug, "ascensia-contour-next-one.5e2a91c7", "manual"} {
		if !sources[slug] {
			t.Errorf("no source %s among %v", slug, sources)
		}
	}
	if sports["running"] > 0 && !sources["com-strava"] {
		t.Errorf("the Strava runs have no com-strava source among %v", sources)
	}

	// One value end to end: the first weigh-in as the scale sent it.
	weights := kept["derived:com.google.weight:com.google.android.gms:merge_weight"]
	first := weights[0]
	sent, _ := first["value"].([]any)[0].(map[string]any)["fpVal"].(float64)
	obs := mustGet(t, ds, obsObservation, fitReadingID(
		fitDatasetRow("derived:com.google.weight:com.google.android.gms:merge_weight",
			time.Unix(0, asInt(t, first["startTimeNanos"])).UTC().Truncate(24*time.Hour)),
		fitScaleSlug, "body_mass", time.Unix(0, asInt(t, first["startTimeNanos"])))).Properties
	value, _ := obs["value"].(map[string]any)
	if v, _ := strconv.ParseFloat(value["value"].(string), 64); v != sent || value["unit"] != "kg" {
		t.Errorf("the first weigh-in is %v, the scale sent %v kg", value, sent)
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}
	t.Logf("%s to %s: %d data sources, %d dataset rows, %d sessions; traces %v; observations %v; %d sleeps, workouts by code %v; sources %v",
		start.Format(time.DateOnly), end.Format(time.DateOnly), len(listing.DataSource), countLive(t, ds, googlefitDatasetType),
		countLive(t, ds, googlefitSessionType), traces, byMetric, countLive(t, ds, healthSleep), bySport, sortedKeys(sources))
}
