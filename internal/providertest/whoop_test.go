package providertest

// The WHOOP provider's `whoopsync` against a loopback WHOOP serving the
// fixtures under testdata/whoop: thirty days of one member, a second sync in
// which one cycle is rescored, one nap and one auto-detected workout are
// deleted and a hand-logged workout appears, and the reads by id a read-back
// makes.
//
// A file that differs between the two syncs holds `{"__responses": [first,
// second]}` and the fake serves the entry the test's phase names; a file
// holding `{"__response": {"status": 404}, "body": …}` answers with that
// status.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	whoopDir          = providersDir + "/whoop"
	whoopPackage      = "providers.substrate.reamde.dev/whoop"
	whoopAccountKind  = whoopPackage + "/account"
	whoopUserKind     = whoopPackage + "/user"
	whoopCycleKind    = whoopPackage + "/cycle"
	whoopRecoveryKind = whoopPackage + "/recovery"
	whoopSleepKind    = whoopPackage + "/sleep"
	whoopWorkoutKind  = whoopPackage + "/workout"
	whoopSyncFn       = whoopPackage + "/whoopsync"
	whoopFixtures     = "testdata/whoop"
	whoopAccount      = "acct-whoop"
	whoopMember       = "4021873"

	// The instant the fixtures' first sync ran at. A steady-state sync is
	// handed cursors at this instant, so its window is a function of the
	// fixtures and not of the clock the test runs under.
	whoopFirstSync = "2026-10-10T15:00:00Z"

	whoopRescoredCycle  = "1047510"
	whoopCurrentCycle   = "1047561"
	whoopLastSleep      = "adde68d0-1852-41bd-963a-f47e68df7da7"
	whoopDeletedNap     = "d728f6f4-45e8-4721-a301-2e2575cfef7f"
	whoopDeletedWorkout = "e3f89923-18c2-4f25-80e1-64e8e5c14a6f"
	whoopLateWorkout    = "9e266b74-7d93-47c3-acc1-1466371e40bf"
)

// whoopMirrors are the five kinds a WHOOP account's rows are.
var whoopMirrors = []string{whoopUserKind, whoopCycleKind, whoopRecoveryKind, whoopSleepKind, whoopWorkoutKind}

// whoopRow is the row id a body composes for one WHOOP object.
func whoopRow(prefix, key string) string {
	return runner.ExternalID("whoop", whoopAccount, prefix+"/"+key)
}

// --- the fake ---------------------------------------------------------------------

// fakeWhoop answers every GET from the fixture named for its path and its
// `nextToken` (`/v2/cycle?nextToken=X` is `GET_v2_cycle__nextToken-X.json`),
// ignoring the window parameters. A request with no fixture answers 502, never
// 404: a 404 is the deletion signal, and a missing fixture must not delete.
type fakeWhoop struct {
	fakeAPI
	phase  int
	misses []string
	// granted, when set, is the scope set the token carries: a read needing a
	// scope outside it answers 403, as WHOOP refuses a grant without it.
	granted []string
	// removed is what WHOOP deleted after the fixtures were cut: a collection
	// path to the cycle_id of the record its pages omit, and a read by id that
	// answers 404.
	removed map[string]float64
	gone    map[string]bool
	// limited is a path's 429s still to answer before it is served.
	limited map[string]int
	// denied is the status a path answers instead of being served, until
	// cleared: a 401 for a grant revoked mid-run.
	denied map[string]int
	// windowed makes a collection page omit the records before its `start`
	// parameter (a record's `start`, or a recovery's `created_at`), where the
	// fake otherwise serves every page whatever the window.
	windowed bool
}

// forget makes WHOOP answer 404 for each read by id, as for an object it
// deleted that no collection page carries.
func (f *fakeWhoop) forget(paths ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone == nil {
		f.gone = map[string]bool{}
	}
	for _, p := range paths {
		f.gone[p] = true
	}
}

// limit makes WHOOP answer the next n reads of path 429, a minute away.
func (f *fakeWhoop) limit(path string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.limited == nil {
		f.limited = map[string]int{}
	}
	f.limited[path] = n
}

// deny makes WHOOP answer path with status until it is denied again with 0.
func (f *fakeWhoop) deny(path string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denied == nil {
		f.denied = map[string]int{}
	}
	if status == 0 {
		delete(f.denied, path)
		return
	}
	f.denied[path] = status
}

// remove makes WHOOP forget one object: the collection's pages omit the
// record whose cycle_id is cycle, and a read of byID answers 404.
func (f *fakeWhoop) remove(collection string, cycle float64, byID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removed == nil {
		f.removed, f.gone = map[string]float64{}, map[string]bool{}
	}
	f.removed[collection] = cycle
	f.gone[byID] = true
}

// whoopScopeOf is the scope one read needs, from WHOOP's published
// description (testdata/whoop/NOTES.md, "Endpoints").
func whoopScopeOf(path string) string {
	switch {
	case strings.HasSuffix(path, "/recovery") || path == "/v2/recovery":
		return "read:recovery"
	case strings.HasSuffix(path, "/sleep") || strings.HasPrefix(path, "/v2/activity/sleep"):
		return "read:sleep"
	case strings.HasPrefix(path, "/v2/activity/workout"):
		return "read:workout"
	case strings.HasPrefix(path, "/v2/cycle"):
		return "read:cycles"
	case path == "/v2/user/measurement/body":
		return "read:body_measurement"
	default:
		return "read:profile"
	}
}

// grant makes the fake refuse every read the scopes do not cover.
func (f *fakeWhoop) grant(scopes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.granted = slices.Clone(scopes)
}

func newFakeWhoop(t *testing.T) *fakeWhoop {
	t.Helper()
	f := &fakeWhoop{}
	f.serve(t, http.HandlerFunc(f.handle))
	return f
}

// setPhase picks which entry of a two-sync fixture the fake serves: 0 is the
// first sync's, 1 the second's.
func (f *fakeWhoop) setPhase(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phase = n
}

func (f *fakeWhoop) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !bearer(w, r) {
		return
	}
	f.mu.Lock()
	refused := f.granted != nil && !slices.Contains(f.granted, whoopScopeOf(r.URL.Path))
	gone := f.gone[r.URL.Path]
	omit, omitting := f.removed[r.URL.Path]
	limited := f.limited[r.URL.Path] > 0
	if limited {
		f.limited[r.URL.Path]--
	}
	denied := f.denied[r.URL.Path]
	f.mu.Unlock()
	if refused {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if denied != 0 {
		w.WriteHeader(denied)
		return
	}
	if limited {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if gone {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
		return
	}
	name := "GET" + strings.ReplaceAll(r.URL.Path, "/", "_")
	if tok := r.URL.Query().Get("nextToken"); tok != "" {
		name += "__nextToken-" + tok
	}
	raw, err := os.ReadFile(filepath.Join(whoopFixtures, name+".json"))
	if err != nil {
		f.mu.Lock()
		f.misses = append(f.misses, r.URL.String())
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if m, ok := doc.(map[string]any); ok {
		if seq, ok := m["__responses"].([]any); ok && len(seq) > 0 {
			f.mu.Lock()
			doc = seq[min(f.phase, len(seq)-1)]
			f.mu.Unlock()
		}
	}
	f.mu.Lock()
	windowed := f.windowed
	f.mu.Unlock()
	if m, ok := doc.(map[string]any); ok && windowed && r.URL.Query().Get("start") != "" {
		floor := r.URL.Query().Get("start")
		records, _ := m["records"].([]any)
		kept := []any{}
		for _, rec := range records {
			r, _ := rec.(map[string]any)
			at, _ := r["start"].(string)
			if at == "" {
				at, _ = r["created_at"].(string)
			}
			if at >= floor {
				kept = append(kept, rec)
			}
		}
		doc = maps.Clone(m)
		doc.(map[string]any)["records"] = kept
	}
	if m, ok := doc.(map[string]any); ok && omitting {
		records, _ := m["records"].([]any)
		kept := []any{}
		for _, rec := range records {
			if r, _ := rec.(map[string]any); r["cycle_id"] != omit {
				kept = append(kept, rec)
			}
		}
		doc = maps.Clone(m)
		doc.(map[string]any)["records"] = kept
	}
	status := http.StatusOK
	if m, ok := doc.(map[string]any); ok {
		if meta, ok := m["__response"].(map[string]any); ok {
			if s, ok := meta["status"].(float64); ok {
				status = int(s)
			}
			doc = m["body"]
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(doc)
}

// whoopByID is a read of one object by its id, as opposed to a collection
// page or the profile.
var whoopByID = regexp.MustCompile(`^/v2/(cycle/[0-9]+(/recovery)?|activity/(sleep|workout)/[^/]+)$`)

// readBacks is every read by id the fake served, in order.
func (f *fakeWhoop) readBacks() []string {
	var out []string
	for _, p := range f.seenPaths() {
		if whoopByID.MatchString(p) {
			out = append(out, p)
		}
	}
	return out
}

func (f *fakeWhoop) missed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.misses)
}

// --- the repository and the sync ---------------------------------------------------

// newWhoop installs the closure into a fresh repository, starts the fake and
// writes the account record the rows reference.
func newWhoop(t *testing.T) (substrate.Service, substrate.Dataset, *fakeWhoop) {
	t.Helper()
	requireUV(t)
	t.Parallel()
	svc, ds := newCoreDataset(t)
	install(t, ds, whoopDir, nil)
	fake := newFakeWhoop(t)
	mustPut(t, ds, substrate.PutInput{
		Kind: whoopAccountKind, ID: whoopAccount,
		Properties: syncProps(map[string]any{"backfillDepth": "all"},
			"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep", "enabledWorkouts"),
	})
	return svc, ds, fake
}

// whoopProps is the account a sync is handed: every collection on, the
// whole history on a cold walk, and the scopes the body checks before the
// body measurement call. extra overrides any of it.
func whoopProps(extra map[string]any) map[string]any {
	props := syncProps(map[string]any{
		"backfillDepth": "all",
		"grantedScopes": []any{
			"read:profile", "read:body_measurement", "read:cycles",
			"read:recovery", "read:sleep", "read:workout", "offline",
		},
	}, "enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep", "enabledWorkouts")
	maps.Copy(props, extra)
	return props
}

// whoopSteady is whoopProps for a run after the first: every stream holds a
// cursor at the first sync's instant, so each reads, and reconciles, the 48
// hours before it. cursors moves single streams.
func whoopSteady(cursors map[string]string) map[string]any {
	c := map[string]any{}
	for _, key := range []string{"cycle", "recovery", "sleep", "workout"} {
		c[key] = whoopFirstSync
	}
	for k, v := range cursors {
		c[k] = v
	}
	return whoopProps(map[string]any{"syncCursors": c})
}

// whoopConfig is the injected config: the one account with a live token and
// the API base at the fake. It carries no client secret and no settings,
// because the engine hands a WHOOP function neither.
func whoopConfig(fake *fakeWhoop, props map[string]any) map[string]any {
	return map[string]any{
		"accounts": []any{map[string]any{
			"id": whoopAccount, "kind": whoopAccountKind, "properties": props, "token": fakeToken,
		}},
		"inputs": map[string]any{"client": map[string]any{
			"properties": map[string]any{"apiBase": fake.ts.URL},
		}},
	}
}

// whoopSync runs one record-triggered sync of the account to completion,
// applying each page the way the dispatcher does, and returns its effects.
func whoopSync(t *testing.T, ds substrate.Dataset, fake *fakeWhoop, props map[string]any) []engine.StepEffect {
	t.Helper()
	s := newStepper(t, ds, whoopSyncFn, whoopConfig(fake, props))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountKind, "id": whoopAccount}})
	return s.drainApplying(nil)
}

// whoopVersions is the version of every live mirror row, by `<kind>/<id>`.
func whoopVersions(t *testing.T, ds substrate.Dataset) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, kind := range whoopMirrors {
		for _, row := range listLive(t, ds, kind) {
			out[kind+"/"+row.ID] = row.Version
		}
	}
	return out
}

// whoopSameRow asserts two runs patched one row with the same properties: the
// sync's and the webhook's mapping of one WHOOP object agree, omissions
// included, which a version comparison cannot see because a patch leaves an
// omitted property alone.
func whoopSameRow(t *testing.T, a, b []engine.StepEffect, kind, id string) {
	t.Helper()
	pa, pb := accountStamp(t, a, kind, id), accountStamp(t, b, kind, id)
	if !reflect.DeepEqual(pa, pb) {
		t.Fatalf("the sync and the webhook map %s %s differently:\nsync    %v\nwebhook %v", kind, id, pa, pb)
	}
}

// whoopLive is live under the name healthwhoop_test.go and
// synth_whoop_test.go call it by.
func whoopLive(t *testing.T, ds substrate.Dataset, kind, id string) bool {
	t.Helper()
	return live(t, ds, kind, id)
}

// whoopDeletes is the ids a run's effects deleted, by kind.
func whoopDeletes(effects []engine.StepEffect) map[string][]string {
	out := map[string][]string{}
	for _, ef := range effects {
		if ef.Action == "delete" {
			out[ef.Kind] = append(out[ef.Kind], ef.ID)
		}
	}
	return out
}

// --- the webhook deliveries --------------------------------------------------------

// TestWhoopSyncResyncOfTheSamePagesChangesNothing: a steady-state sync over
// the pages the first one read moves no mirror row and reads nothing back,
// because every row in its window is one it met.
func TestWhoopSyncResyncOfTheSamePagesChangesNothing(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	before := whoopVersions(t, ds)
	if got := len(before); got != 1+30+30+33+29 {
		t.Fatalf("the first sync mirrored %d rows, want the member, 30 cycles, 30 recoveries, 33 sleeps and 29 workouts", got)
	}
	if rb := fake.readBacks(); len(rb) != 0 {
		t.Fatalf("a cold walk read rows back: %v", rb)
	}

	effects := whoopSync(t, ds, fake, whoopSteady(nil))
	if dels := whoopDeletes(effects); len(dels) != 0 {
		t.Fatalf("the re-sync deleted %v", dels)
	}
	if rb := fake.readBacks(); len(rb) != 0 {
		t.Fatalf("the re-sync read rows back it had just met: %v", rb)
	}
	after := whoopVersions(t, ds)
	for k, v := range after {
		if before[k] != v {
			t.Errorf("%s moved from version %d to %d", k, before[k], v)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("the re-sync left %d rows, want %d", len(after), len(before))
	}
	if missed := fake.missed(); len(missed) != 0 {
		t.Fatalf("the fake had no fixture for %v", missed)
	}
}

// TestWhoopSyncRewritesARescoredCycleAndKeepsTheNewerRevision: the second
// sync's page carries cycle 1047510 rescored with a later `updated_at`, and
// that row alone is rewritten. A third read of the FIRST page, which carries
// the older revision, leaves the rescored row as it is.
func TestWhoopSyncRewritesARescoredCycleAndKeepsTheNewerRevision(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	before := whoopVersions(t, ds)
	rescored := whoopCycleKind + "/" + whoopRow("cycle", whoopRescoredCycle)
	strain := func() any {
		row := mustGet(t, ds, whoopCycleKind, whoopRow("cycle", whoopRescoredCycle))
		score, _ := row.Properties["score"].(map[string]any)
		return score["strain"]
	}
	if got := fmt.Sprint(strain()); got != "12.0731" {
		t.Fatalf("the first sync's strain = %s, want 12.0731", got)
	}

	fake.setPhase(1)
	whoopSync(t, ds, fake, whoopSteady(nil))
	after := whoopVersions(t, ds)
	if after[rescored] <= before[rescored] {
		t.Fatalf("the rescored cycle stayed at version %d", after[rescored])
	}
	if got := fmt.Sprint(strain()); got != "13.6118" {
		t.Fatalf("the rescored strain = %s, want 13.6118", got)
	}
	for _, row := range listLive(t, ds, whoopCycleKind) {
		k := whoopCycleKind + "/" + row.ID
		if k != rescored && after[k] != before[k] {
			t.Errorf("cycle %v moved without being rescored", row.Properties["cycleId"])
		}
	}

	// The first page again, cycles only: its 1047510 is the older revision.
	fake.setPhase(0)
	cyclesOnly := whoopSteady(nil)
	for _, toggle := range []string{"enabledProfile", "enabledRecovery", "enabledSleep", "enabledWorkouts"} {
		cyclesOnly[toggle] = false
	}
	whoopSync(t, ds, fake, cyclesOnly)
	if got := fmt.Sprint(strain()); got != "13.6118" {
		t.Fatalf("an older page put the old strain back: %s", got)
	}
	if v := whoopVersions(t, ds)[rescored]; v != after[rescored] {
		t.Fatalf("an older page moved the rescored cycle from version %d to %d", after[rescored], v)
	}
}

// TestWhoopSyncDeletesWhatWhoopNoLongerHas: between the syncs WHOOP deleted
// an auto-detected workout and a nap. Neither appears in the second sync's
// pages; each is read back by id inside its stream's window, answers 404, and
// its row is deleted. The hand-logged workout that replaced the first lands,
// and nothing outside a window is read back.
func TestWhoopSyncDeletesWhatWhoopNoLongerHas(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	if !live(t, ds, whoopWorkoutKind, whoopRow("workout", whoopDeletedWorkout)) ||
		!live(t, ds, whoopSleepKind, whoopRow("sleep", whoopDeletedNap)) {
		t.Fatal("the first sync did not mirror the workout and the nap the second one loses")
	}

	fake.setPhase(1)
	// The nap is from 2026-10-04: the sleep stream's cursor sits early enough
	// that its window reaches back over it.
	effects := whoopSync(t, ds, fake, whoopSteady(map[string]string{"sleep": "2026-10-06T00:00:00Z"}))

	if got, want := fake.readBacks(), []string{
		"/v2/activity/sleep/" + whoopDeletedNap,
		"/v2/activity/workout/" + whoopDeletedWorkout,
	}; !slices.Equal(got, want) {
		t.Fatalf("the second sync read back %v, want %v", got, want)
	}
	dels := whoopDeletes(effects)
	if !slices.Equal(dels[whoopWorkoutKind], []string{whoopRow("workout", whoopDeletedWorkout)}) ||
		!slices.Equal(dels[whoopSleepKind], []string{whoopRow("sleep", whoopDeletedNap)}) || len(dels) != 2 {
		t.Fatalf("the second sync deleted %v", dels)
	}
	if live(t, ds, whoopWorkoutKind, whoopRow("workout", whoopDeletedWorkout)) {
		t.Fatal("the deleted workout's row survived")
	}
	if live(t, ds, whoopSleepKind, whoopRow("sleep", whoopDeletedNap)) {
		t.Fatal("the deleted nap's row survived")
	}
	if !live(t, ds, whoopWorkoutKind, whoopRow("workout", whoopLateWorkout)) {
		t.Fatal("the hand-logged workout did not land")
	}
	if n := countLive(t, ds, whoopWorkoutKind); n != 29 {
		t.Fatalf("%d workouts are mirrored, want 29", n)
	}
	if n := countLive(t, ds, whoopSleepKind); n != 32 {
		t.Fatalf("%d sleeps are mirrored, want 32", n)
	}

	// Only a 404 deletes. The first sync's workout page does not carry the
	// hand-logged workout, which WHOOP still answers for by id: it is read
	// back and kept as it is.
	fake.setPhase(0)
	workoutsOnly := whoopSteady(nil)
	for _, toggle := range []string{"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep"} {
		workoutsOnly[toggle] = false
	}
	kept := whoopWorkoutKind + "/" + whoopRow("workout", whoopLateWorkout)
	before := whoopVersions(t, ds)[kept]
	reads := len(fake.readBacks())
	effects = whoopSync(t, ds, fake, workoutsOnly)
	if got := fake.readBacks()[reads:]; !slices.Equal(got, []string{"/v2/activity/workout/" + whoopLateWorkout}) {
		t.Fatalf("the third sync read back %v, want the hand-logged workout", got)
	}
	if dels := whoopDeletes(effects); len(dels) != 0 {
		t.Fatalf("a read back WHOOP answered deleted %v", dels)
	}
	if v := whoopVersions(t, ds)[kept]; v == 0 || v != before {
		t.Fatalf("the hand-logged workout went from version %d to %d", before, v)
	}
	if missed := fake.missed(); len(missed) != 0 {
		t.Fatalf("the fake had no fixture for %v", missed)
	}
}

// TestWhoopSyncDeletesARecoveryWithCyclesOff: a recovery is read back by
// the cycle it scores, so its row keeps WHOOP's `cycleId`. With the cycles
// toggle off no cycle row exists to look it up on, and a recovery WHOOP
// deleted (its webhook missed) is still read back inside the window,
// answers 404 and is deleted.
func TestWhoopSyncDeletesARecoveryWithCyclesOff(t *testing.T) {
	_, ds, fake := newWhoop(t)
	noCycles := func(props map[string]any) map[string]any {
		props["enabledCycles"] = false
		return props
	}
	whoopSync(t, ds, fake, noCycles(whoopProps(nil)))
	if n := countLive(t, ds, whoopCycleKind); n != 0 {
		t.Fatalf("%d cycle rows with the cycles toggle off", n)
	}
	gone := whoopRow("recovery", whoopRescoredCycle)
	if !live(t, ds, whoopRecoveryKind, gone) {
		t.Fatal("the first sync did not mirror the recovery WHOOP then deletes")
	}

	cycle, _ := strconv.ParseFloat(whoopRescoredCycle, 64)
	fake.remove("/v2/recovery", cycle, "/v2/cycle/"+whoopRescoredCycle+"/recovery")
	effects := whoopSync(t, ds, fake, noCycles(whoopSteady(nil)))
	if got := whoopDeletes(effects)[whoopRecoveryKind]; !slices.Equal(got, []string{gone}) {
		t.Fatalf("the steady sync deleted recoveries %v, want the one WHOOP lost; read back %v", got, fake.readBacks())
	}
	if live(t, ds, whoopRecoveryKind, gone) {
		t.Fatal("the recovery WHOOP deleted is still mirrored")
	}
}

// whoopWorkoutsOnly is a steady-state account with every collection but the
// workouts off.
func whoopWorkoutsOnly(cursors map[string]string) map[string]any {
	props := whoopSteady(cursors)
	for _, toggle := range []string{"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep"} {
		props[toggle] = false
	}
	return props
}

// TestWhoopSyncHoldsAStreamsCursorUntilItsReadBacksAnswer: WHOOP deleted a
// workout, and the read-back that would find it gone is answered 429. The
// run ends throttled with the workout stream's cursor where it was, so the
// next run walks the same window, reads the workout back and deletes its row.
// A cursor moved past the window would leave the row live for good.
func TestWhoopSyncHoldsAStreamsCursorUntilItsReadBacksAnswer(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	gone := whoopRow("workout", whoopDeletedWorkout)
	readBack := "/v2/activity/workout/" + whoopDeletedWorkout

	fake.setPhase(1)
	fake.limit(readBack, 1)
	effects := whoopSync(t, ds, fake, whoopSteady(nil))
	stamp := accountStamp(t, effects, whoopAccountKind, whoopAccount)
	if stamp["syncState"] != "throttled" {
		t.Fatalf("the run with a throttled read-back ended %v, want throttled", stamp["syncState"])
	}
	cursors, _ := stamp["syncCursors"].(map[string]any)
	if cursors["workout"] != whoopFirstSync {
		t.Fatalf("the workout cursor moved to %v with a read-back unanswered, want it held at %s", cursors["workout"], whoopFirstSync)
	}
	if !live(t, ds, whoopWorkoutKind, gone) {
		t.Fatal("the throttled run deleted the workout without an answer from WHOOP")
	}

	next := whoopSteady(nil)
	next["syncCursors"] = cursors
	effects = whoopSync(t, ds, fake, next)
	if got := whoopDeletes(effects)[whoopWorkoutKind]; !slices.Equal(got, []string{gone}) {
		t.Fatalf("the next run deleted workouts %v, want the one WHOOP lost", got)
	}
	if live(t, ds, whoopWorkoutKind, gone) {
		t.Fatal("the workout WHOOP deleted is still mirrored")
	}
	stamp = accountStamp(t, effects, whoopAccountKind, whoopAccount)
	if c, _ := stamp["syncCursors"].(map[string]any); c["workout"] == whoopFirstSync {
		t.Fatal("the workout cursor stayed put after every read-back answered")
	}
}

// TestWhoopSyncHoldsAStreamsCursorWhenAReadBackIsRefused: WHOOP deleted a
// workout, and the read-back that would find it gone answers 401, as for a
// grant revoked part-way through the run. A 401 says nothing about the
// object, so the workout stream's cursor stays where it was while the
// streams that finished move theirs, and once the grant is back the next run
// walks the same window and deletes the row. A cursor moved past the window
// would leave the row live for good.
func TestWhoopSyncHoldsAStreamsCursorWhenAReadBackIsRefused(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	gone := whoopRow("workout", whoopDeletedWorkout)
	readBack := "/v2/activity/workout/" + whoopDeletedWorkout

	fake.setPhase(1)
	fake.deny(readBack, http.StatusUnauthorized)
	effects := whoopSync(t, ds, fake, whoopSteady(nil))
	stamp := accountStamp(t, effects, whoopAccountKind, whoopAccount)
	cursors, _ := stamp["syncCursors"].(map[string]any)
	if cursors["workout"] != whoopFirstSync {
		t.Fatalf("the workout cursor moved to %v past a refused read-back, want it held at %s", cursors["workout"], whoopFirstSync)
	}
	if cursors["cycle"] == whoopFirstSync {
		t.Fatal("the cycle cursor stayed put although its stream finished")
	}
	if _, completed := stamp["lastCompletedAt"]; completed {
		t.Fatal("a run with a refused read-back stamped lastCompletedAt")
	}
	if !strings.Contains(fmt.Sprint(stamp["syncStatus"]), "workout read-back") {
		t.Fatalf("the syncStatus %q does not name the refused read-back", stamp["syncStatus"])
	}
	if !live(t, ds, whoopWorkoutKind, gone) {
		t.Fatal("a refused read-back deleted the workout")
	}

	fake.deny(readBack, 0)
	next := whoopSteady(nil)
	next["syncCursors"] = cursors
	effects = whoopSync(t, ds, fake, next)
	if got := whoopDeletes(effects)[whoopWorkoutKind]; !slices.Equal(got, []string{gone}) {
		t.Fatalf("the next run deleted workouts %v, want the one WHOOP lost", got)
	}
	if live(t, ds, whoopWorkoutKind, gone) {
		t.Fatal("the workout WHOOP deleted is still mirrored")
	}
}

// TestWhoopSyncReadsBackEveryRowItsWindowMissed: 130 workout rows in the
// window that WHOOP no longer has, more than one scan page holds. Every one
// is read back and deleted in the one run, and the run ends ok.
func TestWhoopSyncReadsBackEveryRowItsWindowMissed(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	const ghosts = 130
	var paths []string
	for i := range ghosts {
		wid := fmt.Sprintf("ghost-%03d", i)
		mustPut(t, ds, substrate.PutInput{
			Kind: whoopWorkoutKind, ID: whoopRow("workout", wid),
			Properties: map[string]any{
				"account":   map[string]any{"ref": whoopAccountKind + "/" + whoopAccount},
				"user":      map[string]any{"ref": whoopUserKind + "/" + whoopRow("user", whoopMember)},
				"workoutId": wid, "at": "2026-10-09T08:00:00Z", "endsAt": "2026-10-09T09:00:00Z",
				"updatedAt": "2026-10-09T09:05:00Z",
			},
		})
		paths = append(paths, "/v2/activity/workout/"+wid)
	}
	fake.forget(paths...)
	real := countLive(t, ds, whoopWorkoutKind) - ghosts

	effects := whoopSync(t, ds, fake, whoopWorkoutsOnly(nil))
	if got := len(whoopDeletes(effects)[whoopWorkoutKind]); got != ghosts {
		t.Fatalf("the run deleted %d workouts, want the %d WHOOP no longer has", got, ghosts)
	}
	if n := countLive(t, ds, whoopWorkoutKind); n != real {
		t.Fatalf("%d workouts are mirrored after the run, want %d", n, real)
	}
	if stamp := accountStamp(t, effects, whoopAccountKind, whoopAccount); stamp["syncState"] != "ok" {
		t.Fatalf("the run ended %v (%v), want ok", stamp["syncState"], stamp["syncStatus"])
	}
}

// TestWhoopSyncGuardsAReadBackWithTheRowsVersion: a read-back is staged
// under the version of the row it read, so a write that lands on the row
// before the effects commit refuses them, and the retry reads the row again
// and keeps the newer revision. Unguarded, the read-back would put the older
// object back over it.
func TestWhoopSyncGuardsAReadBackWithTheRowsVersion(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	fake.setPhase(1)
	whoopSync(t, ds, fake, whoopSteady(nil))
	late := whoopRow("workout", whoopLateWorkout)
	if !live(t, ds, whoopWorkoutKind, late) {
		t.Fatal("the second sync did not mirror the hand-logged workout")
	}

	// Phase 0's workout page does not carry the hand-logged workout, so the
	// run reads it back by id.
	fake.setPhase(0)
	s := newStepper(t, ds, whoopSyncFn, whoopConfig(fake, whoopWorkoutsOnly(nil)))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountKind, "id": whoopAccount}})
	var resume any
	raced := false
	for i := 0; ; i++ {
		if i > 40 {
			t.Fatal("the paged chain did not drain in 40 steps")
		}
		effects, _, cur := s.step(resume)
		writes := false
		for _, ef := range effects {
			if ef.Kind == whoopWorkoutKind && ef.ID == late && ef.Action != "delete" {
				writes = true
			}
		}
		if writes && !raced {
			raced = true
			mustPut(t, ds, substrate.PutInput{
				Kind: whoopWorkoutKind, ID: late,
				Properties: map[string]any{"sportName": "padel, edited", "updatedAt": "2026-10-12T09:00:00Z"},
			})
			err := s.s.Apply(context.Background())
			var cas *substrate.VersionConflictError
			if !errors.As(err, &cas) {
				t.Fatalf("a read-back over a row written since it was read applied: err = %v, want a version conflict", err)
			}
			// The delivery's retry: the same step again.
			continue
		}
		if writes {
			t.Fatalf("the retry wrote the hand-logged workout again over the newer row: %+v", effects)
		}
		s.apply()
		if cur == nil {
			break
		}
		resume = cur
	}
	if !raced {
		t.Fatal("the run never read the hand-logged workout back")
	}
	if got := mustGet(t, ds, whoopWorkoutKind, late).Properties["sportName"]; got != "padel, edited" {
		t.Fatalf("the hand-logged workout's sportName is %v, want the newer write's", got)
	}
}

// TestWhoopSyncGuardsAPageWriteWithTheRowsVersion: the same guard on a page
// record. A rescore staged from the page refuses to commit over a row
// another writer moved, and its retry compares against that row.
func TestWhoopSyncGuardsAPageWriteWithTheRowsVersion(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	rescored := whoopRow("cycle", whoopRescoredCycle)
	fake.setPhase(1)
	props := whoopSteady(nil)
	for _, toggle := range []string{"enabledProfile", "enabledRecovery", "enabledSleep", "enabledWorkouts"} {
		props[toggle] = false
	}
	s := newStepper(t, ds, whoopSyncFn, whoopConfig(fake, props))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountKind, "id": whoopAccount}})
	var resume any
	raced := false
	for i := 0; ; i++ {
		if i > 40 {
			t.Fatal("the paged chain did not drain in 40 steps")
		}
		effects, _, cur := s.step(resume)
		writes := false
		for _, ef := range effects {
			if ef.Kind == whoopCycleKind && ef.ID == rescored {
				writes = true
			}
		}
		if writes && !raced {
			raced = true
			// A later rescore lands first.
			mustPut(t, ds, substrate.PutInput{
				Kind: whoopCycleKind, ID: rescored,
				Properties: map[string]any{"updatedAt": "2026-10-12T09:00:00Z"},
			})
			if err := s.s.Apply(context.Background()); !errors.Is(err, substrate.ErrConflict) {
				t.Fatalf("a page write over a row written since it was read applied: err = %v, want a conflict", err)
			}
			continue
		}
		if writes {
			t.Fatalf("the retry wrote the rescored cycle over a newer row: %+v", effects)
		}
		s.apply()
		if cur == nil {
			break
		}
		resume = cur
	}
	if !raced {
		t.Fatal("the run never wrote the rescored cycle")
	}
	if got := mustGet(t, ds, whoopCycleKind, rescored).Properties["updatedAt"]; !strings.HasPrefix(fmt.Sprint(got), "2026-10-12T09:00:00") {
		t.Fatalf("the rescored cycle's updatedAt is %v, want the newer write's", got)
	}
}

// TestWhoopSyncRewalksRecoveriesWrittenWithoutACycleID: an account that
// synced recoveries before version 21, with cycles off, holds recovery rows
// with no `cycleId` and no cycle row to read one off. The next run reaches
// back past the oldest of them, writes each one's `cycleId`, and deletes the
// one WHOOP's collection no longer lists, which nothing could read back.
func TestWhoopSyncRewalksRecoveriesWrittenWithoutACycleID(t *testing.T) {
	_, ds, fake := newWhoop(t)
	noCycles := func(props map[string]any) map[string]any {
		props["enabledCycles"] = false
		return props
	}
	whoopSync(t, ds, fake, noCycles(whoopProps(nil)))
	rows := listLive(t, ds, whoopRecoveryKind)
	if len(rows) != 30 {
		t.Fatalf("the first sync mirrored %d recoveries, want 30", len(rows))
	}
	for _, row := range rows {
		if _, err := ds.Patch(t.Context(), substrate.ActorAPI, whoopRecoveryKind, row.ID,
			substrate.PatchInput{Properties: map[string]any{"cycleId": nil}}); err != nil {
			t.Fatal(err)
		}
	}
	cycle, _ := strconv.ParseFloat(whoopRescoredCycle, 64)
	fake.remove("/v2/recovery", cycle, "/v2/cycle/"+whoopRescoredCycle+"/recovery")

	effects := whoopSync(t, ds, fake, noCycles(whoopSteady(nil)))
	gone := whoopRow("recovery", whoopRescoredCycle)
	if got := whoopDeletes(effects)[whoopRecoveryKind]; !slices.Equal(got, []string{gone}) {
		t.Fatalf("the run deleted recoveries %v, want the one WHOOP's collection no longer lists", got)
	}
	for _, row := range listLive(t, ds, whoopRecoveryKind) {
		if row.Properties["cycleId"] == nil {
			t.Errorf("recovery %s still has no cycleId", row.ID)
		}
	}
	if n := countLive(t, ds, whoopRecoveryKind); n != 29 {
		t.Fatalf("%d recoveries are mirrored, want 29", n)
	}

	// The fake serves every page whatever the window, so the floor is read off
	// the request: the re-walk starts a week before the oldest recovery
	// (2026-09-11), not 48 hours before the cursor.
	if got := whoopRecoveryStarts(fake); len(got) == 0 || got[0] > "2026-09-05" {
		t.Fatalf("the recovery walk started at %v, want a week before the oldest recovery", got)
	}

	// Nothing is left without a cycleId, so the run after reads the usual
	// window and deletes nothing.
	walked := len(whoopRecoveryStarts(fake))
	effects = whoopSync(t, ds, fake, noCycles(whoopSteady(nil)))
	if dels := whoopDeletes(effects); len(dels) != 0 {
		t.Fatalf("the run after the re-walk deleted %v", dels)
	}
	if got := whoopRecoveryStarts(fake)[walked:]; len(got) == 0 || got[0] != "2026-10-08T15:00:00Z" {
		t.Fatalf("the run after the re-walk started at %v, want 48 hours before the cursor", got)
	}
}

// whoopRecoveryStarts is the `start` of every recovery page the fake served.
func whoopRecoveryStarts(f *fakeWhoop) []string {
	var out []string
	queries := f.seen()
	for i, p := range f.seenPaths() {
		if p != "/v2/recovery" {
			continue
		}
		q, err := url.ParseQuery(queries[i])
		if err == nil && q.Get("start") != "" {
			out = append(out, q.Get("start"))
		}
	}
	return out
}

// TestWhoopSyncGuardsAReadBackDeleteWithTheRowsVersion: a 404 deletes the
// row under the version it was read at, so a write that lands on the row
// between the read-back and the commit (a webhook refreshing it) refuses the
// delete, and the row and the write survive until the retry reads it again.
func TestWhoopSyncGuardsAReadBackDeleteWithTheRowsVersion(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	gone := whoopRow("workout", whoopDeletedWorkout)
	fake.setPhase(1)
	s := newStepper(t, ds, whoopSyncFn, whoopConfig(fake, whoopWorkoutsOnly(nil)))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountKind, "id": whoopAccount}})
	var resume any
	raced := false
	for i := 0; ; i++ {
		if i > 40 {
			t.Fatal("the paged chain did not drain in 40 steps")
		}
		effects, _, cur := s.step(resume)
		deletes := slices.ContainsFunc(effects, func(ef engine.StepEffect) bool {
			return ef.Action == "delete" && ef.Kind == whoopWorkoutKind && ef.ID == gone
		})
		if deletes && !raced {
			raced = true
			whoopPatch(t, ds, whoopWorkoutKind, gone, map[string]any{"sportName": "padel, edited"})
			err := s.s.Apply(context.Background())
			var cas *substrate.VersionConflictError
			if !errors.As(err, &cas) {
				t.Fatalf("a delete over a row written since it was read applied: err = %v, want a version conflict", err)
			}
			if got := mustGet(t, ds, whoopWorkoutKind, gone).Properties["sportName"]; got != "padel, edited" {
				t.Fatalf("the refused delete lost the newer write: sportName %v", got)
			}
			continue
		}
		s.apply()
		if cur == nil {
			break
		}
		resume = cur
	}
	if !raced {
		t.Fatal("the run never deleted the workout WHOOP lost")
	}
	if live(t, ds, whoopWorkoutKind, gone) {
		t.Fatal("the retry, reading the row again and WHOOP's 404 again, did not delete it")
	}
}

// TestWhoopSyncGuardsALegacyRecoveryDeleteWithTheRowsVersion: a recovery from
// before version 21 that the re-walk did not meet is deleted under the version
// the scan listed it at. Another run that fills its `cycleId` in between
// refuses the delete, and the retry reads that id back from WHOOP and keeps
// the row.
func TestWhoopSyncGuardsALegacyRecoveryDeleteWithTheRowsVersion(t *testing.T) {
	_, ds, fake := newWhoop(t)
	noCycles := func(props map[string]any) map[string]any {
		props["enabledCycles"] = false
		return props
	}
	whoopSync(t, ds, fake, noCycles(whoopProps(nil)))
	for _, row := range listLive(t, ds, whoopRecoveryKind) {
		whoopPatch(t, ds, whoopRecoveryKind, row.ID, map[string]any{"cycleId": nil})
	}
	// The collection stops listing the current cycle's recovery, which WHOOP
	// still answers for by id: only the guard keeps the re-walk from deleting
	// it once another run has written its cycleId.
	kept := whoopRow("recovery", whoopCurrentCycle)
	cycle, _ := strconv.ParseFloat(whoopCurrentCycle, 64)
	fake.mu.Lock()
	fake.removed = map[string]float64{"/v2/recovery": cycle}
	fake.mu.Unlock()

	s := newStepper(t, ds, whoopSyncFn, whoopConfig(fake, noCycles(whoopSteady(nil))))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountKind, "id": whoopAccount}})
	var resume any
	raced := false
	for i := 0; ; i++ {
		if i > 40 {
			t.Fatal("the paged chain did not drain in 40 steps")
		}
		effects, _, cur := s.step(resume)
		deletes := slices.ContainsFunc(effects, func(ef engine.StepEffect) bool {
			return ef.Action == "delete" && ef.Kind == whoopRecoveryKind && ef.ID == kept
		})
		if deletes && !raced {
			raced = true
			whoopPatch(t, ds, whoopRecoveryKind, kept, map[string]any{"cycleId": cycle})
			err := s.s.Apply(context.Background())
			var cas *substrate.VersionConflictError
			if !errors.As(err, &cas) {
				t.Fatalf("a legacy delete over a row written since it was listed applied: err = %v, want a version conflict", err)
			}
			continue
		}
		if deletes {
			t.Fatal("the retry deleted the recovery whose cycleId another run wrote")
		}
		s.apply()
		if cur == nil {
			break
		}
		resume = cur
	}
	if !raced {
		t.Fatal("the re-walk never deleted the recovery its collection stopped listing")
	}
	if !live(t, ds, whoopRecoveryKind, kept) {
		t.Fatal("the recovery whose cycleId another run wrote was deleted")
	}
	if got := mustGet(t, ds, whoopRecoveryKind, kept).Properties["cycleId"]; got != cycle {
		t.Fatalf("the kept recovery's cycleId is %v, want %s", got, whoopCurrentCycle)
	}
}

// whoopPatch writes props onto one row as another writer would.
func whoopPatch(t *testing.T, ds substrate.Dataset, kind, id string, props map[string]any) {
	t.Helper()
	if _, err := ds.Patch(t.Context(), substrate.ActorAPI, kind, id, substrate.PatchInput{Properties: props}); err != nil {
		t.Fatalf("patch %s/%s: %v", kind, id, err)
	}
}
