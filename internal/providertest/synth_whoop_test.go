package providertest

// The contract between internal/synth's WHOOP emitter and the shipped WHOOP
// closure: three generated days, served by the mock server's own lookup,
// walked by the real `whoopsync`, then delivered to the `healthwhoop`
// adapter. A change to a shape the sync or the adapter reads that the
// emitter does not follow fails here.

import (
	"encoding/json"
	"net/http"
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
		if _, raw, _, ok := synthRecording(dir, r); ok {
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
	// The repository first: newHealthWhoop skips without uv and goes
	// parallel before any data is generated.
	ds, _ := newHealthWhoop(t)
	// Friday to Monday across the end of the cold: a sick day's nap WHOOP
	// cannot score, a scored Sunday nap, the long run and maybe yoga, and a
	// strap new on Friday that calibrates until Monday.
	p, err := synth.NewPersona(synth.Options{
		Start: synth.NewDate(2026, time.August, 28), End: synth.NewDate(2026, time.August, 31),
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
	var naps, unscorable, workouts int
	for _, d := range days {
		naps += len(p.Day(d).Naps)
		if p.Day(d).Ill {
			unscorable += len(p.Day(d).Naps)
		}
		workouts += len(p.Day(d).Workouts)
	}
	if workouts == 0 || unscorable == 0 || naps == unscorable {
		t.Fatalf("the days hold %d workouts and %d naps, %d of them unscorable; pick days that hold each", workouts, naps, unscorable)
	}

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
	stamp := accountStamp(t, effects, whoopAccountKind, whoopAccount)
	if stamp["syncStatus"] != "ok" || stamp["syncState"] != "ok" || stamp["lastCompletedAt"] == nil {
		t.Fatalf("the sync stamped %v", stamp)
	}

	// The mirror: one row per generated record, counted from the persona.
	for kind, want := range map[string]int{
		whoopUserKind: 1, whoopCycleKind: len(days), whoopRecoveryKind: len(days),
		whoopSleepKind: len(days) + naps, whoopWorkoutKind: workouts,
	} {
		if n := countLive(t, ds, kind); n != want {
			t.Errorf("%d rows of %s, want %d", n, kind, want)
		}
	}
	user := mustGet(t, ds, whoopUserKind, whoopRow("user", whoopMember)).Properties
	if user["email"] != p.Person.Email || user["heightMeter"] == nil || user["weightKilogram"] == nil ||
		asInt(t, user["maxHeartRate"]) != int64(p.Person.MaxHR) {
		t.Errorf("the member row is %v", user)
	}
	cycles := synthWhoopRecords(t, out, "GET_v2_cycle")
	if len(cycles) != len(days) {
		t.Fatalf("%d generated cycles, want %d", len(cycles), len(days))
	}
	var open int
	for _, c := range cycles {
		id := strconv.FormatInt(int64(c["id"].(float64)), 10)
		row := mustGet(t, ds, whoopCycleKind, whoopRow("cycle", id)).Properties
		if !sameInstant(row["at"], asInstant(t, c["start"])) || row["timezoneOffset"] != "+03:00" || row["scoreState"] != "scored" {
			t.Errorf("cycle %s mirrored as %v", id, row)
		}
		if c["end"] == nil {
			open++
			if row["endsAt"] != nil {
				t.Errorf("the open cycle %s has endsAt %v", id, row["endsAt"])
			}
		} else if !sameInstant(row["endsAt"], asInstant(t, c["end"])) {
			t.Errorf("cycle %s ends %v, generated %v", id, row["endsAt"], c["end"])
		}
	}
	if open != 1 {
		t.Errorf("%d open cycles, want the last day's", open)
	}
	for _, row := range listLive(t, ds, whoopRecoveryKind) {
		for _, ref := range []struct{ name, kind string }{{"cycle", whoopCycleKind}, {"sleep", whoopSleepKind}} {
			path := storedRefPath(row.Properties[ref.name])
			if !strings.HasPrefix(path, ref.kind+"/") || !whoopLive(t, ds, ref.kind, strings.TrimPrefix(path, ref.kind+"/")) {
				t.Errorf("recovery %s names %s %s, which is not mirrored", row.ID, ref.name, path)
			}
		}
	}
	for _, row := range listLive(t, ds, whoopWorkoutKind) {
		if row.Properties["sportName"] == nil || row.Properties["sportId"] != nil || row.Properties["score"] == nil {
			t.Errorf("workout %s mirrored as %v", row.ID, row.Properties)
		}
	}

	// The adapter: four observations per cycle (strain, energy, an average
	// and a maximum heart rate), five per recovery, six per scored sleep, two
	// heart rates per workout, a typed sleep and workout each, one source,
	// and no trace, because WHOOP sends summaries and never samples.
	sleeps := len(days) + naps
	scored := sleeps - unscorable
	byMetric := map[string]int{}
	for _, row := range listLive(t, ds, obsObservation) {
		byMetric[metricOf(row.Properties)]++
	}
	for metric, want := range map[string]int{
		"health/whoop_strain": len(days), "health/energy_expended": len(days),
		"health/whoop_recovery": len(days), "health/hrv_rmssd": len(days), "health/resting_heart_rate": len(days),
		"health/spo2": len(days), "health/skin_temperature": len(days),
		"health/sleep_stage": 4 * scored, "health/whoop_sleep_performance": scored, "health/respiratory_rate": scored,
		"health/heart_rate": 2*len(days) + 2*workouts,
	} {
		if byMetric[metric] != want {
			t.Errorf("%d observations of %s, want %d", byMetric[metric], metric, want)
		}
	}
	if len(byMetric) != 11 {
		t.Errorf("observations of %d metrics, want 11: %v", len(byMetric), byMetric)
	}
	// The strap is new on the window's first day, so WHOOP calibrates the
	// first three recoveries and the adapter marks those scores estimated.
	var estimated []string
	for _, row := range listLive(t, ds, obsObservation) {
		if metricOf(row.Properties) == "health/whoop_recovery" && row.Properties["quality"] == "estimated" {
			estimated = append(estimated, row.ID)
		}
	}
	if len(estimated) != 3 {
		t.Errorf("%d estimated recovery scores, want the first three: %v", len(estimated), estimated)
	}
	// Every recovery spans the night it scores, the sleep the recovery names,
	// so a day read places it on the wake day beside the other sources'
	// nightly summaries.
	for _, row := range listLive(t, ds, whoopRecoveryKind) {
		cid := strconv.FormatInt(asInt(t, row.Properties["cycleId"]), 10)
		night := mustGet(t, ds, whoopSleepKind, strings.TrimPrefix(storedRefPath(row.Properties["sleep"]), whoopSleepKind+"/")).Properties
		for _, role := range []string{"recovery", "hrv", "resting_heart_rate", "spo2", "skin_temperature"} {
			obs := healthProps(t, ds, obsObservation, hw(cid, role))
			if obs["period"] != "sleep" || obs["periodId"] != night["sleepId"] ||
				!sameInstant(obs["at"], asInstant(t, night["at"])) || !sameInstant(obs["endsAt"], asInstant(t, night["endsAt"])) {
				t.Errorf("cycle %s's %s spans %v to %v over %v %v, want the night %v from %v to %v",
					cid, role, obs["at"], obs["endsAt"], obs["period"], obs["periodId"], night["sleepId"], night["at"], night["endsAt"])
			}
		}
	}
	for kind, want := range map[string]int{healthSleep: sleeps, healthWorkout: workouts, obsSource: 1, obsTrace: 0} {
		if n := countLive(t, ds, kind); n != want {
			t.Errorf("%d %s, want %d", n, kind, want)
		}
	}
	// One value end to end: the first day's recovery score is the persona's.
	first := cycles[len(cycles)-1]
	id := strconv.FormatInt(int64(first["id"].(float64)), 10)
	rec := healthProps(t, ds, obsObservation, hw(id, "recovery"))
	if v, u := quantityOf(t, rec, "value"); v != strconv.Itoa(p.Day(days[0]).Recovery) || u != "percent" {
		t.Errorf("the first recovery is %s %s, the persona's is %d %%", v, u, p.Day(days[0]).Recovery)
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked", n)
	}
}
