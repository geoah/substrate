package providertest

// The contract between internal/synth's WHOOP emitter and the shipped WHOOP
// closure: three generated days, served by the mock server's own lookup,
// walked by the real `whoopsync`, then delivered to the `healthwhoop`
// adapter. A change to a shape the sync or the adapter reads that the
// emitter does not follow fails here.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/synth"
)

// newSynthWhoopFake is fakeWhoop serving a generated recordings directory by
// the mock server's lookup (internal/providere2e/runner/mockserver.py): the
// exact name, then the name without the pagination parameters (`limit`,
// `start`), then the bare path. A request nothing answers is 502, as in
// fakeWhoop, because a 404 is a deletion to the sync.
func newSynthWhoopFake(t *testing.T, dir string) *fakeWhoop {
	t.Helper()
	f := &fakeWhoop{}
	f.serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !bearer(w, r) {
			return
		}
		pairs := synth.Query(r.URL.Query())
		var thin []synth.Pair
		for _, p := range pairs {
			if !synth.PaginationParams[p.Key] {
				thin = append(thin, p)
			}
		}
		for _, name := range []string{
			synth.RecordingName(r.Method, r.URL.Path, pairs...),
			synth.RecordingName(r.Method, r.URL.Path, thin...),
			synth.RecordingName(r.Method, r.URL.Path),
		} {
			raw, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a name the lookup composed under the test's own directory
			if err == nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
				return
			}
		}
		f.mu.Lock()
		f.misses = append(f.misses, r.URL.String())
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	return f
}

// synthWhoopRecords reads every record of one collection off its pages, in
// the order the files were written.
func synthWhoopRecords(t *testing.T, out *synth.Output, prefix string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, f := range out.Files {
		if filepath.Dir(f.Path) != "recordings" || !strings.HasPrefix(filepath.Base(f.Path), prefix) {
			continue
		}
		var page struct {
			Records []map[string]any `json:"records"`
		}
		if err := json.Unmarshal(f.Body, &page); err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		recs = append(recs, page.Records...)
	}
	return recs
}

func TestSynthWhoopSyncsAndAdapts(t *testing.T) {
	// Saturday to Monday: a strength session, the Sunday long run, maybe yoga.
	p, err := synth.NewPersona(synth.Options{
		Start: synth.NewDate(2026, time.September, 12), End: synth.NewDate(2026, time.September, 14),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := synth.Whoop{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := synth.WriteOutput(dir, synth.Whoop{}, out); err != nil {
		t.Fatal(err)
	}
	days := p.Days()
	var naps, workouts int
	for _, d := range days {
		naps += len(p.Day(d).Naps)
		workouts += len(p.Day(d).Workouts)
	}
	if workouts == 0 {
		t.Fatal("the three days hold no workout; pick days that do")
	}

	ds, _ := newHealthWhoop(t)
	fake := newSynthWhoopFake(t, filepath.Join(dir, "whoop", "recordings"))
	effects := whoopSync(t, ds, fake, whoopProps(nil))
	healthDrain(t, ds)

	// The walk: every request answered, nothing read back on a cold walk.
	if missed := fake.missed(); len(missed) != 0 {
		t.Fatalf("the generated recordings did not answer %v", missed)
	}
	if rb := fake.readBacks(); len(rb) != 0 {
		t.Fatalf("a cold walk read rows back: %v", rb)
	}
	stamp := accountStamp(t, effects, whoopAccountType, whoopAccount)
	if stamp["syncStatus"] != "ok" || stamp["syncState"] != "ok" || stamp["lastCompletedAt"] == nil {
		t.Fatalf("the sync stamped %v", stamp)
	}

	// The mirror: one row per generated record, counted from the persona.
	for kind, want := range map[string]int{
		whoopUserType: 1, whoopCycleType: len(days), whoopRecoveryType: len(days),
		whoopSleepType: len(days) + naps, whoopWorkoutType: workouts,
	} {
		if n := countLive(t, ds, kind); n != want {
			t.Errorf("%d rows of %s, want %d", n, kind, want)
		}
	}
	user := mustGet(t, ds, whoopUserType, whoopRow("user", whoopMember)).Properties
	if user["email"] != p.Person.Email || user["heightMeter"] == nil || user["weightKilogram"] == nil ||
		asInt(user["maxHeartRate"]) != int64(p.Person.MaxHR) {
		t.Errorf("the member row is %v", user)
	}
	cycles := synthWhoopRecords(t, out, "GET_v2_cycle")
	if len(cycles) != len(days) {
		t.Fatalf("%d generated cycles, want %d", len(cycles), len(days))
	}
	var open int
	for _, c := range cycles {
		id := strconv.FormatInt(int64(c["id"].(float64)), 10)
		row := mustGet(t, ds, whoopCycleType, whoopRow("cycle", id)).Properties
		if !sameInstant(t, row["at"], c["start"].(string)) || row["timezoneOffset"] != "+03:00" || row["scoreState"] != "scored" {
			t.Errorf("cycle %s mirrored as %v", id, row)
		}
		if c["end"] == nil {
			open++
			if row["endsAt"] != nil {
				t.Errorf("the open cycle %s has endsAt %v", id, row["endsAt"])
			}
		} else if !sameInstant(t, row["endsAt"], c["end"].(string)) {
			t.Errorf("cycle %s ends %v, generated %v", id, row["endsAt"], c["end"])
		}
	}
	if open != 1 {
		t.Errorf("%d open cycles, want the last day's", open)
	}
	for _, row := range listLive(t, ds, whoopRecoveryType) {
		for _, ref := range []struct{ name, kind string }{{"cycle", whoopCycleType}, {"sleep", whoopSleepType}} {
			path := storedRefPath(row.Properties[ref.name])
			if !strings.HasPrefix(path, ref.kind+"/") || !whoopLive(t, ds, ref.kind, strings.TrimPrefix(path, ref.kind+"/")) {
				t.Errorf("recovery %s names %s %s, which is not mirrored", row.ID, ref.name, path)
			}
		}
	}
	for _, row := range listLive(t, ds, whoopWorkoutType) {
		if row.Properties["sportName"] == nil || row.Properties["sportId"] != nil || row.Properties["score"] == nil {
			t.Errorf("workout %s mirrored as %v", row.ID, row.Properties)
		}
	}

	// The adapter: two observations per cycle, five per recovery, six per
	// sleep, a typed sleep and workout each, one source, and no trace,
	// because WHOOP sends summaries and never samples.
	sleeps := len(days) + naps
	byMetric := map[string]int{}
	for _, row := range listLive(t, ds, healthObservation) {
		byMetric[metricOf(row.Properties)]++
	}
	for metric, want := range map[string]int{
		"health/whoop_strain": len(days), "health/energy_expended": len(days),
		"health/whoop_recovery": len(days), "health/hrv_rmssd": len(days), "health/resting_heart_rate": len(days),
		"health/spo2": len(days), "health/skin_temperature": len(days),
		"health/sleep_stage": 4 * sleeps, "health/whoop_sleep_performance": sleeps, "health/respiratory_rate": sleeps,
	} {
		if byMetric[metric] != want {
			t.Errorf("%d observations of %s, want %d", byMetric[metric], metric, want)
		}
	}
	if len(byMetric) != 10 {
		t.Errorf("observations of %d metrics, want 10: %v", len(byMetric), byMetric)
	}
	for kind, want := range map[string]int{healthSleep: sleeps, healthWorkout: workouts, healthSource: 1, obsTrace: 0} {
		if n := countLive(t, ds, kind); n != want {
			t.Errorf("%d %s, want %d", n, kind, want)
		}
	}
	// One value end to end: the first day's recovery score is the persona's.
	first := cycles[len(cycles)-1]
	id := strconv.FormatInt(int64(first["id"].(float64)), 10)
	rec := healthProps(t, ds, healthObservation, hw(id, "recovery"))
	if v, u := quantityOf(t, rec, "value"); v != strconv.Itoa(p.Day(days[0]).Recovery) || u != "%" {
		t.Errorf("the first recovery is %s %s, the persona's is %d %%", v, u, p.Day(days[0]).Recovery)
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}
}
