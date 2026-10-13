package providertest

// The googlefit bundle's `googlefitsync` against a loopback Google Fit that
// serves the provider e2e suite's recordings (internal/providere2e/fixtures/
// googlefit), shifted by whole days so the day they were cut on lands where a
// test wants it. The fake answers a dataset read the way the Fit reference
// documents: the points whose interval overlaps the range, the latest `limit`
// of them first, and a nextPageToken naming the end time the next page stays
// strictly below.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	googlefitDir         = providersDir + "/googlefit"
	googlefitPackage     = "providers.substrate.reamde.dev/googlefit"
	googlefitAccountType = googlefitPackage + "/account"
	googlefitSourceType  = googlefitPackage + "/datasource"
	googlefitDatasetType = googlefitPackage + "/dataset"
	googlefitSessionType = googlefitPackage + "/session"
	googlefitSyncFn      = googlefitPackage + "/googlefitsync"
	googlefitAccount     = "acct-fit"
	googlefitRecordings  = "../providere2e/fixtures/googlefit"

	fitHeartRate = "derived:com.google.heart_rate.bpm:com.google.android.gms:merge_heart_rate_bpm"
	fitRestingHR = "derived:com.google.heart_rate.bpm:com.google.android.gms:resting_heart_rate<-merge_heart_rate_bpm"
	fitSteps     = "derived:com.google.step_count.delta:com.google.android.gms:estimated_steps"
	fitWeight    = "derived:com.google.weight:com.google.android.gms:merge_weight"
	fitSleep     = "derived:com.google.sleep.segment:com.google.android.gms:merged"
	fitWatchHR   = "raw:com.google.heart_rate.bpm:com.google.android.apps.fitness:Google:Pixel Watch 2:8d1f3c7a:"

	// The session feed's last token: the recordings' third page is empty and
	// hands it back unchanged.
	fitLastSessionToken = "1791608658001"
	fitDeletedWalk      = "1790952490000-7"
)

// fitCutOn is the UTC day the recordings were cut on: the newest point is on
// 2026-10-10 and the session feed was read on 2026-10-11.
var fitCutOn = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

// fitAllToggles is every feature toggle the account kind declares.
var fitAllToggles = []string{
	"enabledActivity", "enabledBody", "enabledHeartRate", "enabledSleep",
	"enabledOxygenSaturation", "enabledBloodPressure", "enabledBloodGlucose",
}

type fitPoint struct {
	start, end int64
	raw        map[string]any
}

// fakeFit is Google Fit over the recordings. Every instant it serves is the
// recorded one plus shift.
type fakeFit struct {
	fakeAPI
	shift time.Duration

	mu      sync.Mutex
	sources map[string]bool
	listing map[string]any
	points  map[string][]fitPoint
	feed    map[string]map[string]any
	window  map[string]any
	revoked bool
	// forbidden answers a dataset read of these data sources 403, the answer
	// to a grant missing the data type's scope.
	forbidden map[string]bool
	// ranges is every dataset read, by data source, as the UTC day its range
	// starts on.
	ranges map[string][]string
	// tokens is every session feed read's pageToken, "" for none.
	tokens []string
	// pageTokens counts dataset reads that carried a pageToken.
	pageTokens int
}

// newFakeFit serves the recordings with `cutOn` moved to `today`.
func newFakeFit(t *testing.T, today, cutOn time.Time) *fakeFit {
	t.Helper()
	f := &fakeFit{
		shift:   today.Sub(cutOn),
		sources: map[string]bool{},
		points:  map[string][]fitPoint{},
		feed:    map[string]map[string]any{},
		ranges:  map[string][]string{},
	}
	entries, err := os.ReadDir(googlefitRecordings)
	if err != nil {
		t.Fatalf("read the recordings: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		doc := readFitJSON(t, filepath.Join(googlefitRecordings, name))
		switch {
		case name == "GET_fitness_v1_users_me_dataSources.json":
			f.listing = doc
			for _, ds := range doc["dataSource"].([]any) {
				f.sources[ds.(map[string]any)["dataStreamId"].(string)] = true
			}
		case strings.Contains(name, "_datasets"):
			sid := doc["dataSourceId"].(string)
			for _, p := range doc["point"].([]any) {
				f.addPoint(sid, p.(map[string]any))
			}
		case name == "GET_fitness_v1_users_me_sessions.json":
			f.window = doc
		case strings.HasPrefix(name, "GET_fitness_v1_users_me_sessions__includeDeleted-true"):
			_, token, _ := strings.Cut(strings.TrimSuffix(name, ".json"), "_pageToken-")
			f.feed[token] = doc
		}
	}
	if f.listing == nil || len(f.points) == 0 || len(f.feed) != 3 || f.window == nil {
		t.Fatalf("the recordings under %s are incomplete", googlefitRecordings)
	}
	f.start(t)
	return f
}

func readFitJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// fitNumber reads a stored int, which a record serves as a JSON number.
func fitNumber(v any) float64 {
	n, _ := v.(float64)
	return n
}

func fitInt(v any) int64 {
	n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
	return n
}

func (f *fakeFit) addPoint(sid string, p map[string]any) {
	f.points[sid] = append(f.points[sid], fitPoint{
		start: fitInt(p["startTimeNanos"]), end: fitInt(p["endTimeNanos"]), raw: p,
	})
}

// setPoints replaces one data source's points, recorded instants.
func (f *fakeFit) setPoints(sid string, points []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.points[sid] = nil
	for _, p := range points {
		f.addPoint(sid, p.(map[string]any))
	}
}

// dropPoints removes one data source's points that start on a recorded UTC
// day.
func (f *fakeFit) dropPoints(sid string, day time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lo, hi := day.UnixNano(), day.Add(24*time.Hour).UnixNano()
	f.points[sid] = slices.DeleteFunc(f.points[sid], func(p fitPoint) bool {
		return p.start >= lo && p.start < hi
	})
}

func (f *fakeFit) forget() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ranges = map[string][]string{}
	f.tokens = nil
	f.pageTokens = 0
}

// shifted copies a recorded object with its int64 instants moved.
func (f *fakeFit) shifted(src map[string]any, nanos, millis []string) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	for _, k := range nanos {
		if v, ok := out[k]; ok {
			out[k] = strconv.FormatInt(fitInt(v)+f.shift.Nanoseconds(), 10)
		}
	}
	for _, k := range millis {
		if v, ok := out[k]; ok {
			out[k] = strconv.FormatInt(fitInt(v)+f.shift.Milliseconds(), 10)
		}
	}
	return out
}

func (f *fakeFit) shiftedSessions(list any) []any {
	out := []any{}
	for _, s := range list.([]any) {
		out = append(out, f.shifted(s.(map[string]any), nil,
			[]string{"startTimeMillis", "endTimeMillis", "modifiedTimeMillis"}))
	}
	return out
}

func (f *fakeFit) start(t *testing.T) {
	t.Helper()
	gate := func(w http.ResponseWriter, r *http.Request) bool {
		f.record(r)
		f.mu.Lock()
		revoked := f.revoked
		f.mu.Unlock()
		if revoked {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return bearer(w, r)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fitness/v1/users/me/dataSources", func(w http.ResponseWriter, r *http.Request) {
		if gate(w, r) {
			f.mu.Lock()
			listing := f.listing
			f.mu.Unlock()
			writeJSON(w, listing)
		}
	})
	mux.HandleFunc("GET /fitness/v1/users/me/dataSources/{id}/datasets/{window}",
		func(w http.ResponseWriter, r *http.Request) {
			if !gate(w, r) {
				return
			}
			sid := r.PathValue("id")
			lo, hi, ok := strings.Cut(r.PathValue("window"), "-")
			if !f.sources[sid] || !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			start, end := fitInt(lo), fitInt(hi)
			q := r.URL.Query()
			f.mu.Lock()
			if f.forbidden[sid] {
				f.mu.Unlock()
				w.WriteHeader(http.StatusForbidden)
				return
			}
			f.ranges[sid] = append(f.ranges[sid], time.Unix(0, start).UTC().Format(time.DateOnly))
			if q.Get("pageToken") != "" {
				f.pageTokens++
			}
			var hits []fitPoint
			shift := f.shift.Nanoseconds()
			for _, p := range f.points[sid] {
				if p.start+shift < end && p.end+shift >= start {
					hits = append(hits, p)
				}
			}
			f.mu.Unlock()
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].end < hits[j].end })
			if tok := q.Get("pageToken"); tok != "" {
				below := fitInt(tok)
				hits = slices.DeleteFunc(hits, func(p fitPoint) bool { return p.end+shift >= below })
			}
			resp := map[string]any{"minStartTimeNs": lo, "maxEndTimeNs": hi, "dataSourceId": sid}
			if limit, _ := strconv.Atoi(q.Get("limit")); limit > 0 && len(hits) > limit {
				hits = hits[len(hits)-limit:]
				resp["nextPageToken"] = strconv.FormatInt(hits[0].end+shift, 10)
			}
			points := []any{}
			for _, p := range hits {
				points = append(points, f.shifted(p.raw,
					[]string{"startTimeNanos", "endTimeNanos"}, []string{"modifiedTimeMillis"}))
			}
			resp["point"] = points
			writeJSON(w, resp)
		})
	mux.HandleFunc("GET /fitness/v1/users/me/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r) {
			return
		}
		q := r.URL.Query()
		if q.Has("startTime") || q.Has("endTime") {
			writeJSON(w, map[string]any{
				"session":        f.shiftedSessions(f.window["session"]),
				"deletedSession": []any{},
			})
			return
		}
		tok := q.Get("pageToken")
		f.mu.Lock()
		f.tokens = append(f.tokens, tok)
		f.mu.Unlock()
		page, ok := f.feed[tok]
		if !ok {
			page = f.feed[fitLastSessionToken]
		}
		out := map[string]any{
			"session":        f.shiftedSessions(page["session"]),
			"deletedSession": f.shiftedSessions(page["deletedSession"]),
		}
		if next, ok := page["nextPageToken"]; ok {
			out["nextPageToken"] = next
		}
		writeJSON(w, out)
	})
	f.serve(t, mux)
}

// days is every UTC day one data source was read on, sorted and distinct.
func (f *fakeFit) days(sid string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.ranges[sid])
	slices.Sort(out)
	return slices.Compact(out)
}

func (f *fakeFit) sessionTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tokens)
}

// googlefitConfig is the injected config: the account as stored, a live
// token, and the API base pointed at the fake.
func googlefitConfig(t *testing.T, ds substrate.Dataset, fake *fakeFit) map[string]any {
	t.Helper()
	row := mustGet(t, ds, googlefitAccountType, googlefitAccount)
	return map[string]any{
		"accounts": []any{map[string]any{
			"id": googlefitAccount, "kind": googlefitAccountType,
			"properties": row.Properties, "token": fakeToken,
		}},
		"inputs": map[string]any{"client": map[string]any{
			"properties": map[string]any{"apiBase": fake.ts.URL + "/fitness/v1"},
		}},
	}
}

// googlefitAccountWith writes the account with the shipped defaults and the
// named toggles on.
func googlefitAccountWith(t *testing.T, ds substrate.Dataset, toggles ...string) {
	t.Helper()
	mustPut(t, ds, substrate.PutInput{
		Kind: googlefitAccountType, ID: googlefitAccount,
		Properties: syncProps(map[string]any{"timezone": "Europe/Athens"}, toggles...),
	})
}

// googlefitSync drains one sync of the account the way a record delivery
// does: the envelope names the account, so the cadence does not gate it.
func googlefitSync(t *testing.T, ds substrate.Dataset, fake *fakeFit) []map[string]any {
	t.Helper()
	s := newStepper(t, ds, googlefitSyncFn, googlefitConfig(t, ds, fake))
	s.setEnvelope(map[string]any{"record": map[string]any{
		"kind": googlefitAccountType, "id": googlefitAccount,
	}})
	var stamps []map[string]any
	for _, ef := range s.drainApplying(nil) {
		if ef.Action == "patch" && ef.Kind == googlefitAccountType {
			stamps = append(stamps, ef.Properties)
		}
	}
	return stamps
}

// fitSourceRow is a data source row's id: the account, then Google's id, or
// its hash where the id leaves the record-id alphabet.
func fitSourceRow(sid string) string {
	if strings.ContainsAny(sid, " <+") {
		return googlefitAccount + "/" + runner.ExternalID("googlefit", googlefitAccount, sid)
	}
	return googlefitAccount + "/" + sid
}

func fitDatasetRow(sid string, day time.Time) string {
	return fitSourceRow(sid) + "/" + day.Format(time.DateOnly)
}

// fitDatasets is the live dataset rows of one data source, by id.
func fitDatasets(t *testing.T, ds substrate.Dataset, sid string) map[string]*substrate.Record {
	t.Helper()
	out := map[string]*substrate.Record{}
	for _, row := range listLive(t, ds, googlefitDatasetType) {
		if row.Properties["dataSourceId"] == sid {
			out[row.ID] = row
		}
	}
	return out
}

func fitPoints(row *substrate.Record) []map[string]any {
	var out []map[string]any
	for _, p := range row.Properties["point"].([]any) {
		out = append(out, p.(map[string]any))
	}
	return out
}

func utcToday() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour)
}

// TestGoogleFitClosureAdmits: the closure loads through the ordinary
// resolver, its function may write every kind it writes and read the two it
// checks before a delete, and the client input is injected.
func TestGoogleFitClosureAdmits(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, googlefitDir)
	b := assertBundleInput(t, reg, googlefitPackage, "client", googlefitPackage+"/config",
		vocabulary.BundleInputInjectFunctions)
	if b.OAuth2 == nil || len(b.OAuth2.FeatureScopes) != len(fitAllToggles) {
		t.Fatalf("oauth2 block = %+v, want one featureScopes entry per toggle", b.OAuth2)
	}
	fn, err := reg.ResolveFunction(googlefitSyncFn)
	if err != nil {
		t.Fatalf("resolve %s: %v", googlefitSyncFn, err)
	}
	mustEmit(t, fn, googlefitSourceType, googlefitDatasetType, googlefitSessionType, googlefitAccountType)
	mustRead(t, fn, googlefitDatasetType, googlefitSessionType)
	mustProps(t, mustKind(t, reg, googlefitAccountType),
		append([]string{"timezone", "syncCursors", "syncFloors", "syncTokens"}, fitAllToggles...)...)
	mustProps(t, mustKind(t, reg, googlefitDatasetType), "account", "dataSource", "dataSourceId", "point")
	mustProps(t, mustKind(t, reg, googlefitSourceType), "dataStreamId", "type", "dataType", "device")
	mustProps(t, mustKind(t, reg, googlefitSessionType), "sessionId", "activityType", "application")
}

// TestGoogleFitFirstSyncMirrorsSourcesDaysAndSessions: a first sync with
// every toggle on and the default 30-day backfill lists the data sources,
// reads every derived source of each enabled data type once per UTC day from
// thirty days back to today, writes a dataset row for each day that had
// points (joining the heart rate day's two pages), and walks the session feed
// to its end.
func TestGoogleFitFirstSyncMirrorsSourcesDaysAndSessions(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	today := utcToday()
	fake := newFakeFit(t, today, fitCutOn)
	googlefitAccountWith(t, ds, fitAllToggles...)

	googlefitSync(t, ds, fake)
	day := func(recorded time.Time) time.Time { return today.Add(recorded.Sub(fitCutOn)) }

	// Every data source, raw ones included: a derived point's origin resolves
	// to one of them.
	if n := countLive(t, ds, googlefitSourceType); n != 22 {
		t.Fatalf("%d data source rows, want the 22 the listing names", n)
	}
	watch := mustGet(t, ds, googlefitSourceType, fitSourceRow(fitWatchHR))
	device, _ := watch.Properties["device"].(map[string]any)
	if watch.Properties["type"] != "raw" || device["model"] != "Pixel Watch 2" || device["type"] != "watch" {
		t.Fatalf("the watch's heart rate source = %v", watch.Properties)
	}
	dataType, _ := watch.Properties["dataType"].(map[string]any)
	fields, _ := dataType["field"].([]any)
	if dataType["name"] != "com.google.heart_rate.bpm" || len(fields) != 1 ||
		fields[0].(map[string]any)["format"] != "floatpoint" {
		t.Fatalf("the watch's data type = %v", dataType)
	}

	// Each enabled derived source was read once per day of the window, and
	// no raw one was read at all.
	var want []string
	for d := today.AddDate(0, 0, -30); !d.After(today); d = d.AddDate(0, 0, 1) {
		want = append(want, d.Format(time.DateOnly))
	}
	for _, sid := range []string{fitHeartRate, fitRestingHR, fitSteps, fitWeight, fitSleep} {
		if got := fake.days(sid); !slices.Equal(got, want) {
			t.Fatalf("%s was read on %v, want every day from %s to %s", sid, got, want[0], want[len(want)-1])
		}
	}
	if got := fake.days(fitWatchHR); len(got) != 0 {
		t.Fatalf("the raw watch stream was read on %v; its points reach the merged stream", got)
	}

	// Heart rate: the recorded day spans two UTC days, and the second one
	// arrives in two pages joined into one row.
	if fake.pageTokens == 0 {
		t.Fatal("no dataset read carried a pageToken; the second heart rate page was never asked for")
	}
	hr := fitDatasets(t, ds, fitHeartRate)
	if len(hr) != 2 {
		t.Fatalf("%d heart rate rows, want 2: %v", len(hr), sortedKeys(hr))
	}
	busy := hr[fitDatasetRow(fitHeartRate, day(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)))]
	if busy == nil {
		t.Fatalf("no heart rate row for the recorded 2026-10-07: %v", sortedKeys(hr))
	}
	points := fitPoints(busy)
	if len(points) != 1223 {
		t.Fatalf("the 2026-10-07 heart rate row holds %d points, want 1223", len(points))
	}
	for i := 1; i < len(points); i++ {
		if fitInt(points[i]["endTimeNanos"]) < fitInt(points[i-1]["endTimeNanos"]) {
			t.Fatalf("heart rate points are not in end-time order at %d", i)
		}
	}
	start := day(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if first := fitInt(points[0]["startTimeNanos"]); first < start.UnixNano() {
		t.Fatalf("the row holds a point from the day before (%d < %d)", first, start.UnixNano())
	}
	if !fitSameInstant(busy.Properties["at"], start) || !fitSameInstant(busy.Properties["endsAt"], start.Add(24*time.Hour)) {
		t.Fatalf("the row's span = %v to %v, want the UTC day %s", busy.Properties["at"], busy.Properties["endsAt"], start)
	}
	value, _ := points[0]["value"].([]any)
	if len(value) != 1 || value[0].(map[string]any)["fpVal"] == nil {
		t.Fatalf("a heart rate point's value = %v", points[0]["value"])
	}
	if refIDs(busy, "dataSource")[0] != fitSourceRow(fitHeartRate) {
		t.Fatalf("the row's dataSource = %v", busy.Properties["dataSource"])
	}

	if n := len(fitDatasets(t, ds, fitSteps)); n != 1 {
		t.Fatalf("%d step rows, want the one recorded day", n)
	}
	weights := fitDatasets(t, ds, fitWeight)
	if len(weights) != 16 {
		t.Fatalf("%d weight rows, want the 16 recorded weigh-in days", len(weights))
	}
	twice := weights[fitDatasetRow(fitWeight, day(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)))]
	if twice == nil || len(fitPoints(twice)) != 2 {
		t.Fatalf("the 2026-10-03 weight row = %v, want its two weigh-ins", twice)
	}
	if n := len(fitDatasets(t, ds, fitSleep)); n != 30 {
		t.Fatalf("%d sleep rows, want one per night from the recorded 2026-09-11 to 2026-10-10", n)
	}
	if n := countLive(t, ds, googlefitDatasetType); n != 2+1+16+30 {
		t.Fatalf("%d dataset rows, want 49: a source with no points writes none", n)
	}

	if n := countLive(t, ds, googlefitSessionType); n != 59 {
		t.Fatalf("%d sessions, want the 59 the feed's two pages name", n)
	}
	if got := fake.sessionTokens(); !slices.Equal(got, []string{"", "1790405742001", fitLastSessionToken}) {
		t.Fatalf("the session feed was read with tokens %q, want the three pages in order", got)
	}

	acct := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties
	cursors, _ := acct["syncCursors"].(map[string]any)
	if len(cursors) != 14 {
		t.Fatalf("syncCursors = %v, want one per data type the toggles cover", cursors)
	}
	tokens, _ := acct["syncTokens"].(map[string]any)
	if tokens["sessions"] != fitLastSessionToken {
		t.Fatalf("syncTokens = %v, want the feed's last token", tokens)
	}
	if acct["syncState"] != "ok" || acct["syncStatus"] != "ok" || acct["lastCompletedAt"] == nil {
		t.Fatalf("the account after a clean first sync: state %v, status %v, completed %v",
			acct["syncState"], acct["syncStatus"], acct["lastCompletedAt"])
	}
	if acct["syncFloors"] != nil {
		t.Fatalf("syncFloors = %v after every walk finished", acct["syncFloors"])
	}
}

// TestGoogleFitIncrementalSyncRereadsOnlyTheOverlap: the run after a finished
// walk reads each derived source from two UTC days before its cursor to
// today, asks the session feed only for what changed since its token, and
// writes nothing when nothing changed.
func TestGoogleFitIncrementalSyncRereadsOnlyTheOverlap(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	today := utcToday()
	fake := newFakeFit(t, today, fitCutOn)
	googlefitAccountWith(t, ds, "enabledBody", "enabledHeartRate", "enabledSleep")

	googlefitSync(t, ds, fake)
	before := map[string]int64{}
	for _, row := range listLive(t, ds, googlefitDatasetType) {
		before[row.ID] = row.Version
	}
	if len(before) == 0 {
		t.Fatal("the first sync wrote no dataset rows")
	}
	firstCursors := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties["syncCursors"]

	fake.forget()
	googlefitSync(t, ds, fake)

	want := []string{
		today.AddDate(0, 0, -2).Format(time.DateOnly),
		today.AddDate(0, 0, -1).Format(time.DateOnly),
		today.Format(time.DateOnly),
	}
	for _, sid := range []string{fitHeartRate, fitRestingHR, fitWeight, fitSleep} {
		if got := fake.days(sid); !slices.Equal(got, want) {
			t.Fatalf("the second run read %s on %v, want only %v", sid, got, want)
		}
	}
	if got := fake.sessionTokens(); !slices.Equal(got, []string{fitLastSessionToken}) {
		t.Fatalf("the second run read the session feed with %q, want one read from the stored token", got)
	}
	after := listLive(t, ds, googlefitDatasetType)
	if len(after) != len(before) {
		t.Fatalf("%d dataset rows after the second run, want the %d the first wrote", len(after), len(before))
	}
	for _, row := range after {
		if row.Version != before[row.ID] {
			t.Fatalf("%s moved from version %d to %d with nothing changed upstream", row.ID, before[row.ID], row.Version)
		}
	}
	cursors, _ := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties["syncCursors"].(map[string]any)
	was, _ := firstCursors.(map[string]any)
	for key, at := range cursors {
		a, _ := time.Parse(time.RFC3339, fmt.Sprint(was[key]))
		b, _ := time.Parse(time.RFC3339, fmt.Sprint(at))
		if b.Before(a) {
			t.Fatalf("syncCursors.%s went back from %v to %v", key, was[key], at)
		}
	}
}

// TestGoogleFitRereadRewritesAChangedDayAndDeletesAnEmptiedOne: the
// recordings' second weight read changed the 2026-10-03 evening weigh-in
// (72.56 kg to 71.76 kg, a later modifiedTimeMillis). With that day inside the
// overlap, the next run rewrites its row with the new point and leaves the
// unchanged days alone; a day whose only point is gone loses its row.
func TestGoogleFitRereadRewritesAChangedDayAndDeletesAnEmptiedOne(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	today := utcToday()
	// The recordings' 2026-10-04 is today, so 2026-10-03 is yesterday.
	recordedToday := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	fake := newFakeFit(t, today, recordedToday)
	googlefitAccountWith(t, ds, "enabledBody")

	googlefitSync(t, ds, fake)
	changed := fitDatasetRow(fitWeight, today.AddDate(0, 0, -1))
	emptied := fitDatasetRow(fitWeight, today)
	steady := fitDatasetRow(fitWeight, today.AddDate(0, 0, -2))
	rows := fitDatasets(t, ds, fitWeight)
	for _, id := range []string{changed, emptied, steady} {
		if rows[id] == nil {
			t.Fatalf("the first sync wrote no %s: %v", id, sortedKeys(rows))
		}
	}
	changedVersion, steadyVersion := rows[changed].Version, rows[steady].Version

	reread := readFitJSON(t, "testdata/googlefit/weight_reread.json")
	fake.setPoints(fitWeight, reread["point"].([]any))
	fake.dropPoints(fitWeight, recordedToday)
	googlefitSync(t, ds, fake)

	row := mustGet(t, ds, googlefitDatasetType, changed)
	if row.Version <= changedVersion {
		t.Fatalf("the re-read day stayed at version %d", row.Version)
	}
	var evening map[string]any
	for _, p := range fitPoints(row) {
		if fitInt(p["startTimeNanos"]) == 1791052863000000000+today.Sub(recordedToday).Nanoseconds() {
			evening = p
		}
	}
	if evening == nil {
		t.Fatalf("the re-read day lost its evening weigh-in: %v", fitPoints(row))
	}
	value := evening["value"].([]any)[0].(map[string]any)
	wantModified := strconv.FormatInt(1791652324331+today.Sub(recordedToday).Milliseconds(), 10)
	if value["fpVal"] != 71.76 || evening["modifiedTimeMillis"] != wantModified {
		t.Fatalf("the evening weigh-in = %v, want 71.76 kg modified at %s", evening, wantModified)
	}
	if len(fitPoints(row)) != 2 {
		t.Fatalf("the re-read day holds %d points, want both weigh-ins", len(fitPoints(row)))
	}
	if got := mustGet(t, ds, googlefitDatasetType, steady).Version; got != steadyVersion {
		t.Fatalf("an unchanged day moved from version %d to %d", steadyVersion, got)
	}
	if fitLive(t, ds, googlefitDatasetType, emptied) {
		t.Fatalf("%s survived a re-read that found no points", emptied)
	}
}

// TestGoogleFitSessionFeedPagesToItsEndAndDeletes: with no token the feed
// answers the last 30 days in pages; the sync follows nextPageToken until a
// page names nothing, keeps that page's token, and deletes the session the
// second page reports deleted.
func TestGoogleFitSessionFeedPagesToItsEndAndDeletes(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	today := utcToday()
	fake := newFakeFit(t, today, fitCutOn)
	googlefitAccountWith(t, ds, "enabledSleep")
	walk := googlefitAccount + "/" + fitDeletedWalk
	mustPut(t, ds, substrate.PutInput{Kind: googlefitSessionType, ID: walk, Properties: map[string]any{
		"account": googlefitAccount, "sessionId": fitDeletedWalk, "name": "Afternoon walk",
		"at": today.AddDate(0, 0, -9).Format(time.RFC3339),
	}})

	googlefitSync(t, ds, fake)

	if got := fake.sessionTokens(); !slices.Equal(got, []string{"", "1790405742001", fitLastSessionToken}) {
		t.Fatalf("the feed was read with tokens %q, want three pages in order", got)
	}
	if n := countLive(t, ds, googlefitSessionType); n != 59 {
		t.Fatalf("%d sessions, want 59", n)
	}
	if fitLive(t, ds, googlefitSessionType, walk) {
		t.Fatal("the walk the feed reports deleted is still here")
	}
	shift := today.Sub(fitCutOn)
	run := mustGet(t, ds, googlefitSessionType, googlefitAccount+"/strava-16238731866").Properties
	app, _ := run["application"].(map[string]any)
	if app["packageName"] != "com.strava" || app["version"] != "421.9" ||
		fitNumber(run["activityType"]) != 8 || fitNumber(run["activeTimeMillis"]) != 2409000 ||
		run["name"] != "Morning run" {
		t.Fatalf("the Strava run = %v", run)
	}
	if !fitSameInstant(run["at"], time.UnixMilli(1789186117000).Add(shift)) ||
		!fitSameInstant(run["endsAt"], time.UnixMilli(1789188589000).Add(shift)) {
		t.Fatalf("the Strava run spans %v to %v", run["at"], run["endsAt"])
	}
	sleep := mustGet(t, ds, googlefitSessionType, googlefitAccount+"/Sleep1789070381000").Properties
	if fitNumber(sleep["activityType"]) != 72 {
		t.Fatalf("the first night's session = %v", sleep)
	}
	tokens, _ := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties["syncTokens"].(map[string]any)
	if tokens["sessions"] != fitLastSessionToken {
		t.Fatalf("syncTokens = %v", tokens)
	}
}

// TestGoogleFitStopsOnTheDrainBudgetAndResumes: a chain that has spent its
// drain budget ends itself with each unfinished data type's resume day saved
// in syncFloors, and the next scheduled run, although the cadence says the
// account synced a moment ago, finishes the walk from those days.
func TestGoogleFitStopsOnTheDrainBudgetAndResumes(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	today := utcToday()
	fake := newFakeFit(t, today, fitCutOn)
	googlefitAccountWith(t, ds, "enabledBody")

	// A chain that began ten minutes ago: the engine's drain deadline is two.
	s := newStepper(t, ds, googlefitSyncFn, googlefitConfig(t, ds, fake))
	long := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	effects, _, next := s.step(map[string]any{
		"queue": []any{googlefitAccount}, "dispatch": "schedule",
		"chainStart": long, "startedAt": long, "types": nil,
	})
	s.apply()
	if next != nil {
		t.Fatalf("a page past the drain budget asked for another page: %v", next)
	}
	if got := fake.days(fitWeight); len(got) != 0 {
		t.Fatalf("the stopping page read %s on %v", fitWeight, got)
	}
	stamp := accountStamp(t, effects, googlefitAccountType, googlefitAccount)
	floors, _ := stamp["syncFloors"].(map[string]any)
	wantFloor := today.AddDate(0, 0, -30).Format(time.RFC3339)
	if len(floors) != 3 || floors["weight"] != wantFloor {
		t.Fatalf("syncFloors = %v, want the three body types at %s", floors, wantFloor)
	}
	if !strings.Contains(fmt.Sprint(stamp["syncStatus"]), "pending") || stamp["lastCompletedAt"] != nil {
		t.Fatalf("the stop stamped status %v, completed %v", stamp["syncStatus"], stamp["lastCompletedAt"])
	}

	// The scheduled sweep: no envelope, and lastSyncedAt is a moment old.
	s = newStepper(t, ds, googlefitSyncFn, googlefitConfig(t, ds, fake))
	s.drainApplying(nil)
	if got := fake.days(fitWeight); len(got) != 31 || got[0] != today.AddDate(0, 0, -30).Format(time.DateOnly) {
		t.Fatalf("the resumed walk read %s on %v, want the 31 days from the saved floor", fitWeight, got)
	}
	if n := len(fitDatasets(t, ds, fitWeight)); n != 16 {
		t.Fatalf("%d weight rows after the resumed walk, want 16", n)
	}
	acct := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties
	if acct["syncFloors"] != nil || acct["lastCompletedAt"] == nil || acct["syncStatus"] != "ok" {
		t.Fatalf("after the resumed walk: floors %v, completed %v, status %v",
			acct["syncFloors"], acct["lastCompletedAt"], acct["syncStatus"])
	}
}

// TestGoogleFitDropsTheFloorOfATypeItNoLongerWalks: a floor in syncFloors is
// a cut-off walk's resume day, and any floor makes the account due on every
// tick. A run that does not walk a type, because its toggle is off or Google
// refused it, drops that type's floor, so the account goes back to its
// cadence instead of a full sync every fifteen minutes.
func TestGoogleFitDropsTheFloorOfATypeItNoLongerWalks(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	today := utcToday()
	fake := newFakeFit(t, today, fitCutOn)
	googlefitAccountWith(t, ds, "enabledBody")
	cut := today.AddDate(0, 0, -20).Format(time.RFC3339)

	run := func(floors map[string]any) map[string]any {
		t.Helper()
		cfg := googlefitConfig(t, ds, fake)
		acct := cfg["accounts"].([]any)[0].(map[string]any)
		props := map[string]any{}
		for k, v := range acct["properties"].(map[string]any) {
			props[k] = v
		}
		props["syncFloors"] = floors
		props["syncFrequency"] = "daily"
		acct["properties"] = props
		s := newStepper(t, ds, googlefitSyncFn, cfg)
		s.setEnvelope(map[string]any{"record": map[string]any{"kind": googlefitAccountType, "id": googlefitAccount}})
		return accountStamp(t, s.drainApplying(nil), googlefitAccountType, googlefitAccount)
	}

	// Heart rate was cut off at a floor, then switched off.
	if stamp := run(map[string]any{"heartRateBpm": cut}); stamp["syncFloors"] != nil {
		t.Fatalf("a run with heart rate off kept syncFloors %v", stamp["syncFloors"])
	}
	// Weight was cut off at a floor, and Google now refuses its data source.
	fake.mu.Lock()
	fake.forbidden = map[string]bool{fitWeight: true}
	fake.mu.Unlock()
	stamp := run(map[string]any{"weight": cut})
	if !strings.Contains(fmt.Sprint(stamp["syncMessage"]), "weight") {
		t.Fatalf("the refused type is not named: %v", stamp["syncMessage"])
	}
	if stamp["syncFloors"] != nil {
		t.Fatalf("a run Google refused weight in kept syncFloors %v", stamp["syncFloors"])
	}

	// A scheduled sweep right after: daily, synced a moment ago, no floor.
	fake.forget()
	newStepper(t, ds, googlefitSyncFn, googlefitConfig(t, ds, fake)).drainApplying(nil)
	if got := fake.days(fitWeight); len(got) != 0 {
		t.Fatalf("the sweep synced an account that is not due: read %s on %v", fitWeight, got)
	}
}

// TestGoogleFitWalkCheckpointsBetweenInvocations: an invocation that has
// staged its byte budget hands its position back, and the chain that follows
// writes what one long invocation would have.
func TestGoogleFitWalkCheckpointsBetweenInvocations(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	installRewired(t, ds, googlefitDir, [2]string{"EFFECT_BYTES = 4 << 20", "EFFECT_BYTES = 1"})
	today := utcToday()
	fake := newFakeFit(t, today, fitCutOn)
	googlefitAccountWith(t, ds, "enabledHeartRate", "enabledBody")

	s := newStepper(t, ds, googlefitSyncFn, googlefitConfig(t, ds, fake))
	var steps int
	var resume any
	for {
		steps++
		if steps > 40 {
			t.Fatal("the chain did not drain in 40 invocations")
		}
		_, _, cur := s.step(resume)
		s.apply()
		if cur == nil {
			break
		}
		resume = cur
	}
	if steps < 10 {
		t.Fatalf("the chain drained in %d invocations; a one-byte budget should hand back after every written day", steps)
	}
	if n := len(fitDatasets(t, ds, fitHeartRate)); n != 2 {
		t.Fatalf("%d heart rate rows, want 2", n)
	}
	if n := len(fitDatasets(t, ds, fitWeight)); n != 16 {
		t.Fatalf("%d weight rows, want 16", n)
	}
	for _, sid := range []string{fitHeartRate, fitWeight} {
		if got := fake.days(sid); len(got) != 31 {
			t.Fatalf("%s was read on %d days across the chain, want 31", sid, len(got))
		}
	}
	if acct := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties; acct["lastCompletedAt"] == nil {
		t.Fatalf("the chain finished without completing: %v", acct["syncStatus"])
	}
}

// TestGoogleFitRevokedGrantStampsErroring: a 401 on the first call fails the
// whole account, not one stream, and says so on the sync trait.
func TestGoogleFitRevokedGrantStampsErroring(t *testing.T) {
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googlefitDir, nil)
	fake := newFakeFit(t, utcToday(), fitCutOn)
	fake.revoked = true
	googlefitAccountWith(t, ds, "enabledBody", "enabledSleep")

	stamps := googlefitSync(t, ds, fake)
	if len(stamps) == 0 {
		t.Fatal("the failed run stamped nothing on the account")
	}
	acct := mustGet(t, ds, googlefitAccountType, googlefitAccount).Properties
	if acct["syncState"] != "erroring" || !strings.Contains(fmt.Sprint(acct["syncError"]), "unauthorized") ||
		!strings.HasPrefix(fmt.Sprint(acct["syncStatus"]), "erroring: ") {
		t.Fatalf("after a 401 the account reads state %v, error %v, status %v",
			acct["syncState"], acct["syncError"], acct["syncStatus"])
	}
	if acct["lastSyncedAt"] == nil || acct["lastCompletedAt"] != nil {
		t.Fatalf("lastSyncedAt %v, lastCompletedAt %v: a failed attempt is an attempt and not a completion",
			acct["lastSyncedAt"], acct["lastCompletedAt"])
	}
	if n := countLive(t, ds, googlefitSourceType) + countLive(t, ds, googlefitDatasetType); n != 0 {
		t.Fatalf("a revoked grant wrote %d mirror rows", n)
	}
}

// fitLive reports whether a record exists and is not a tombstone: a read by id
// answers a deleted record too.
func fitLive(t *testing.T, ds substrate.Dataset, kind, id string) bool {
	t.Helper()
	row, err := ds.Get(t.Context(), kind, id)
	return err == nil && row.DeletedAt == nil
}

// fitSameInstant compares a stored datetime with an instant, whatever spelling
// the engine stored it in.
func fitSameInstant(v any, want time.Time) bool {
	got, err := time.Parse(time.RFC3339Nano, fmt.Sprint(v))
	return err == nil && got.Equal(want)
}
