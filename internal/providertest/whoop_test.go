package providertest

// The WHOOP provider's `whoopsync` and `whoopwebhook` against a loopback WHOOP
// serving the fixtures under testdata/whoop: thirty days of one member, a
// second sync in which one cycle is rescored, one nap and one auto-detected
// workout are deleted and a hand-logged workout appears, the reads by id a
// webhook or a read-back makes, and the webhook deliveries WHOOP would post
// between the two syncs, signed with the fake client secret in
// testdata/whoop/inbound/SECRET.txt. testdata/whoop/NOTES.md says what each
// fact rests on.
//
// A file that differs between the two syncs holds `{"__responses": [first,
// second]}` and the fake serves the entry the test's phase names; a file
// holding `{"__response": {"status": 404}, "body": …}` answers with that
// status.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	whoopDir          = providersDir + "/whoop"
	whoopPackage      = "providers.substrate.reamde.dev/whoop"
	whoopAccountType  = whoopPackage + "/account"
	whoopUserType     = whoopPackage + "/user"
	whoopCycleType    = whoopPackage + "/cycle"
	whoopRecoveryType = whoopPackage + "/recovery"
	whoopSleepType    = whoopPackage + "/sleep"
	whoopWorkoutType  = whoopPackage + "/workout"
	whoopSyncFn       = whoopPackage + "/whoopsync"
	whoopWebhookFn    = whoopPackage + "/whoopwebhook"
	whoopSecretRecord = whoopPackage + "/webhooksecret"
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
var whoopMirrors = []string{whoopUserType, whoopCycleType, whoopRecoveryType, whoopSleepType, whoopWorkoutType}

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
		Kind: whoopAccountType, ID: whoopAccount,
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

// whoopConfig is the injected config: the one account with a live token, the
// API base at the fake, and the webhook secret when one is given.
func whoopConfig(fake *fakeWhoop, props map[string]any, secret string) map[string]any {
	cfg := map[string]any{
		"accounts": []any{map[string]any{
			"id": whoopAccount, "kind": whoopAccountType, "properties": props, "token": fakeToken,
		}},
		"inputs": map[string]any{"client": map[string]any{
			"properties": map[string]any{"apiBase": fake.ts.URL},
		}},
	}
	if secret != "" {
		cfg["settings"] = map[string]any{"webhooksecret": secret}
	}
	return cfg
}

// whoopSync runs one record-triggered sync of the account to completion,
// applying each page the way the dispatcher does, and returns its effects.
func whoopSync(t *testing.T, ds substrate.Dataset, fake *fakeWhoop, props map[string]any) []engine.StepEffect {
	t.Helper()
	s := newStepper(t, ds, whoopSyncFn, whoopConfig(fake, props, ""))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": whoopAccountType, "id": whoopAccount}})
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

// whoopLive reports whether one row exists and is not deleted.
func whoopLive(t *testing.T, ds substrate.Dataset, kind, id string) bool {
	t.Helper()
	row, err := ds.Get(context.Background(), kind, id)
	if errors.Is(err, substrate.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("get %s %s: %v", kind, id, err)
	}
	return row.DeletedAt == nil
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

func whoopSecret(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(whoopFixtures, "inbound", "SECRET.txt"))
	if err != nil {
		t.Fatalf("read the fixture secret: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

// whoopSignature is WHOOP's documented scheme: base64 of HMAC-SHA256 under the
// client secret over the timestamp header's value followed by the raw body.
func whoopSignature(secret, stamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// whoopDelivery reads one recorded delivery's request.
func whoopDelivery(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(whoopFixtures, "inbound", name))
	if err != nil {
		t.Fatalf("read delivery %s: %v", name, err)
	}
	var doc struct {
		Request map[string]any `json:"request"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Request == nil {
		t.Fatalf("decode delivery %s: %v", name, err)
	}
	return doc.Request
}

// whoopSigned is the instant a request's timestamp header names.
func whoopSigned(t *testing.T, request map[string]any) time.Time {
	t.Helper()
	headers, _ := request["headers"].(map[string]any)
	stamp, _ := headers["x-whoop-signature-timestamp"].(string)
	ms, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		t.Fatalf("the delivery's timestamp %q is not milliseconds: %v", stamp, err)
	}
	return time.UnixMilli(ms).UTC()
}

// whoopArrived is the envelope the door hands the function: the request, and
// the fire stamped with the instant the door accepted it.
func whoopArrived(request map[string]any, at time.Time) map[string]any {
	return map[string]any{
		"request": request,
		"fire":    map[string]any{"id": "webhook-test", "at": at.UTC().Format(time.RFC3339)},
	}
}

// whoopOnTime is a recorded delivery arriving two seconds after WHOOP signed it.
func whoopOnTime(t *testing.T, name string) map[string]any {
	t.Helper()
	req := whoopDelivery(t, name)
	return whoopArrived(req, whoopSigned(t, req).Add(2*time.Second))
}

// whoopEvent signs a notification WHOOP could have sent, at `at`.
func whoopEvent(t *testing.T, secret string, at time.Time, event map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	stamp := strconv.FormatInt(at.UnixMilli(), 10)
	return map[string]any{
		"method":      "POST",
		"contentType": "application/json",
		"headers": map[string]any{
			"content-type":                "application/json; charset=utf-8",
			"x-whoop-signature":           whoopSignature(secret, stamp, body),
			"x-whoop-signature-timestamp": stamp,
		},
		"query": map[string]any{},
		"body":  map[string]any{"text": string(body)},
	}
}

// whoopHook runs the webhook body once over one envelope and applies its
// effects, handing back the body's refusal instead of failing on it.
func whoopHook(t *testing.T, ds substrate.Dataset, cfg, env map[string]any) (map[string]any, []engine.StepEffect, error) {
	t.Helper()
	s := newStepper(t, ds, whoopWebhookFn, cfg)
	s.setEnvelope(env)
	effects, out, cur, err := s.s.Step(context.Background(), nil)
	if err != nil {
		return nil, nil, err
	}
	if cur != nil {
		t.Fatalf("the webhook body handed back a cursor: %v", cur)
	}
	if err := s.s.Apply(context.Background()); err != nil {
		t.Fatalf("apply the webhook's effects: %v", err)
	}
	o, _ := out.(map[string]any)
	return o, effects, nil
}

// mustHook is whoopHook for a delivery that must be accepted.
func mustHook(t *testing.T, ds substrate.Dataset, cfg, env map[string]any) (map[string]any, []engine.StepEffect) {
	t.Helper()
	out, effects, err := whoopHook(t, ds, cfg, env)
	if err != nil {
		t.Fatalf("the webhook refused a delivery it should accept: %v", err)
	}
	return out, effects
}

// whoopHookConfig is the webhook's injected config off the STORED account,
// whose `user` reference the first sync wrote: that reference is how the
// body finds the account a delivery is for.
func whoopHookConfig(t *testing.T, ds substrate.Dataset, fake *fakeWhoop, secret string) map[string]any {
	t.Helper()
	row := mustGet(t, ds, whoopAccountType, whoopAccount)
	if refIDs(row, "user") == nil {
		t.Fatal("the account names no WHOOP member; the first sync did not stamp account.user")
	}
	return whoopConfig(fake, row.Properties, secret)
}

// --- the tests --------------------------------------------------------------------

// TestWhoopWebhookFixturesAreSignedAsWhoopDocuments pins the scheme the body
// implements to the recorded deliveries: every one verifies under the fixture
// secret over the timestamp header plus the body, except 08, which another
// secret signed.
func TestWhoopWebhookFixturesAreSignedAsWhoopDocuments(t *testing.T) {
	t.Parallel()
	secret := whoopSecret(t)
	names, err := filepath.Glob(filepath.Join(whoopFixtures, "inbound", "*.json"))
	if err != nil || len(names) != 8 {
		t.Fatalf("want the 8 recorded deliveries, found %d (%v)", len(names), err)
	}
	for _, path := range names {
		name := filepath.Base(path)
		req := whoopDelivery(t, name)
		headers, _ := req["headers"].(map[string]any)
		body, _ := req["body"].(map[string]any)
		text, _ := body["text"].(string)
		stamp, _ := headers["x-whoop-signature-timestamp"].(string)
		got := whoopSignature(secret, stamp, []byte(text)) == headers["x-whoop-signature"]
		if want := !strings.HasPrefix(name, "08_"); got != want {
			t.Errorf("%s verifies = %v, want %v", name, got, want)
		}
	}
}

// TestWhoopWebhookRefusesWhatItCannotVerify: a wrong signature, a stale one,
// an empty secret setting, missing headers and an altered body are each
// refused before the body reads or writes anything, and the same delivery
// arriving on time with the secret set is acted on, which is what says each
// refusal was the check's.
func TestWhoopWebhookRefusesWhatItCannotVerify(t *testing.T) {
	_, ds, fake := newWhoop(t)
	secret := whoopSecret(t)
	// No sync ran: the account's `user` is handed in directly.
	props := whoopProps(map[string]any{"user": map[string]any{"ref": whoopUserType + "/" + whoopRow("user", whoopMember)}})
	cfg := whoopConfig(fake, props, secret)

	stripped := whoopDelivery(t, "01_sleep.updated.json")
	stripped["headers"] = map[string]any{"content-type": "application/json; charset=utf-8"}
	altered := whoopDelivery(t, "01_sleep.updated.json")
	body, _ := altered["body"].(map[string]any)
	body["text"] = strings.Replace(body["text"].(string), whoopLastSleep, whoopDeletedNap, 1)
	late := whoopDelivery(t, "01_sleep.updated.json")

	for _, tc := range []struct {
		name string
		cfg  map[string]any
		env  map[string]any
		want string
	}{
		{"another secret signed it", cfg, whoopOnTime(t, "08_bad_signature.json"), "X-WHOOP-Signature does not match"},
		{"the body was altered", cfg, whoopArrived(altered, whoopSigned(t, altered)), "X-WHOOP-Signature does not match"},
		{"it arrived an hour after it was signed", cfg, whoopArrived(late, whoopSigned(t, late).Add(time.Hour)), "seconds away from when it arrived"},
		{"it arrived before it was signed", cfg, whoopArrived(late, whoopSigned(t, late).Add(-10*time.Minute)), "seconds away from when it arrived"},
		{"it carries no signature", cfg, whoopArrived(stripped, time.Now()), "carries no X-WHOOP-Signature"},
		{"the secret setting is empty", whoopConfig(fake, props, ""), whoopOnTime(t, "01_sleep.updated.json"), "webhooksecret setting is empty"},
	} {
		_, _, err := whoopHook(t, ds, tc.cfg, tc.env)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want a refusal saying %q", tc.name, err, tc.want)
		}
	}
	if seen := fake.seenPaths(); len(seen) != 0 {
		t.Fatalf("a refused delivery reached WHOOP: %v", seen)
	}
	if n := countLive(t, ds, whoopSleepType); n != 0 {
		t.Fatalf("a refused delivery wrote %d sleeps", n)
	}

	// The control: the same delivery, on time, under the right secret.
	out, _ := mustHook(t, ds, cfg, whoopOnTime(t, "01_sleep.updated.json"))
	if !whoopLive(t, ds, whoopSleepType, whoopRow("sleep", whoopLastSleep)) {
		t.Fatalf("the verified sleep.updated wrote no sleep row (output %v)", out)
	}
	if got := fake.readBacks(); !slices.Equal(got, []string{"/v2/activity/sleep/" + whoopLastSleep}) {
		t.Fatalf("the verified delivery read %v, want the one sleep", got)
	}
}

// TestWhoopWebhookRewritesAndDeletesMirrorRows walks the recorded deliveries
// over a mirror the first sync filled: an update reads the object back and
// rewrites its row with exactly the properties the sync wrote for the same
// object (the two bodies carry the mapping twice, and this is what holds them
// to one), a recovery event is resolved through its sleep, a redelivery
// changes nothing, and a delete event tombstones the row it names.
func TestWhoopWebhookRewritesAndDeletesMirrorRows(t *testing.T) {
	_, ds, fake := newWhoop(t)
	secret := whoopSecret(t)
	synced := whoopSync(t, ds, fake, whoopProps(nil))
	cfg := whoopHookConfig(t, ds, fake, secret)
	before := whoopVersions(t, ds)

	sleepRow := whoopSleepType + "/" + whoopRow("sleep", whoopLastSleep)
	recoveryRow := whoopRecoveryType + "/" + whoopRow("recovery", whoopCurrentCycle)

	// sleep.updated, for a sleep the sync wrote from the same object.
	out, hooked := mustHook(t, ds, cfg, whoopOnTime(t, "01_sleep.updated.json"))
	if fmt.Sprint(out["wrote"]) != fmt.Sprint([]any{sleepRow}) {
		t.Fatalf("sleep.updated wrote %v, want %s", out["wrote"], sleepRow)
	}
	whoopSameRow(t, synced, hooked, whoopSleepType, whoopRow("sleep", whoopLastSleep))
	// recovery.updated names the SLEEP: the body reads it for its cycle and
	// then reads the recovery through the cycle.
	out, hooked = mustHook(t, ds, cfg, whoopOnTime(t, "02_recovery.updated.json"))
	if !slices.Contains(out["wrote"].([]any), any(recoveryRow)) {
		t.Fatalf("recovery.updated wrote %v, want %s among them", out["wrote"], recoveryRow)
	}
	whoopSameRow(t, synced, hooked, whoopRecoveryType, whoopRow("recovery", whoopCurrentCycle))
	whoopSameRow(t, synced, hooked, whoopSleepType, whoopRow("sleep", whoopLastSleep))
	if got, want := fake.readBacks(), []string{
		"/v2/activity/sleep/" + whoopLastSleep,
		"/v2/activity/sleep/" + whoopLastSleep,
		"/v2/cycle/" + whoopCurrentCycle + "/recovery",
	}; !slices.Equal(got, want) {
		t.Fatalf("the two deliveries read %v, want %v", got, want)
	}
	// 03 is a redelivery of 01: same trace id, signed afresh.
	mustHook(t, ds, cfg, whoopOnTime(t, "03_sleep.updated.retry.json"))
	if after := whoopVersions(t, ds); !maps.Equal(before, after) {
		for k, v := range after {
			if before[k] != v {
				t.Errorf("%s moved from version %d to %d", k, before[k], v)
			}
		}
		t.Fatal("an update for an object the mirror already holds moved a row")
	}

	// workout.updated for a workout logged after the sync: a create.
	mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	late := mustGet(t, ds, whoopWorkoutType, whoopRow("workout", whoopLateWorkout))
	if late.Properties["sportName"] != "padel" || refIDs(late, "user")[0] != whoopRow("user", whoopMember) {
		t.Fatalf("the late workout landed as %v", late.Properties)
	}

	// The deletions: a workout, a nap, and the recovery of the last sleep,
	// which WHOOP names by that sleep.
	mustHook(t, ds, cfg, whoopOnTime(t, "05_workout.deleted.json"))
	mustHook(t, ds, cfg, whoopOnTime(t, "06_sleep.deleted.json"))
	now := time.Now()
	env := whoopEvent(t, secret, now, map[string]any{
		"user_id": 4021873, "id": whoopLastSleep, "type": "recovery.deleted", "trace_id": "t-recovery-deleted",
	})
	out, _ = mustHook(t, ds, cfg, whoopArrived(env, now))
	if fmt.Sprint(out["deleted"]) != fmt.Sprint([]any{recoveryRow}) {
		t.Fatalf("recovery.deleted deleted %v, want %s", out["deleted"], recoveryRow)
	}
	for _, row := range [][2]string{
		{whoopWorkoutType, whoopRow("workout", whoopDeletedWorkout)},
		{whoopSleepType, whoopRow("sleep", whoopDeletedNap)},
		{whoopRecoveryType, whoopRow("recovery", whoopCurrentCycle)},
	} {
		if whoopLive(t, ds, row[0], row[1]) {
			t.Errorf("%s/%s survived its delete event", row[0], row[1])
		}
	}
	if !whoopLive(t, ds, whoopSleepType, whoopRow("sleep", whoopLastSleep)) {
		t.Fatal("recovery.deleted took the sleep row with it; only sleep.deleted deletes a sleep")
	}

	// A delete for a row already gone, and one for a sleep this mirror never
	// held (07 names a sleep outside the series), are acknowledged and
	// change nothing.
	for _, name := range []string{"05_workout.deleted.json", "07_recovery.deleted.example.json"} {
		out, effects := mustHook(t, ds, cfg, whoopOnTime(t, name))
		if len(effects) != 0 {
			t.Fatalf("%s staged %d effects for rows that are not there (output %v)", name, len(effects), out)
		}
	}
	// A member no account holds.
	env = whoopEvent(t, secret, now, map[string]any{
		"user_id": 99, "id": whoopLateWorkout, "type": "workout.updated", "trace_id": "t-stranger",
	})
	out, effects := mustHook(t, ds, cfg, whoopArrived(env, now))
	if len(effects) != 0 || !strings.Contains(fmt.Sprint(out["skipped"]), "no connected account") {
		t.Fatalf("an event for another member: output %v, %d effects", out, len(effects))
	}
	if missed := fake.missed(); len(missed) != 0 {
		t.Fatalf("the fake had no fixture for %v", missed)
	}
}

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
	rescored := whoopCycleType + "/" + whoopRow("cycle", whoopRescoredCycle)
	strain := func() any {
		row := mustGet(t, ds, whoopCycleType, whoopRow("cycle", whoopRescoredCycle))
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
	for _, row := range listLive(t, ds, whoopCycleType) {
		k := whoopCycleType + "/" + row.ID
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
	if !whoopLive(t, ds, whoopWorkoutType, whoopRow("workout", whoopDeletedWorkout)) ||
		!whoopLive(t, ds, whoopSleepType, whoopRow("sleep", whoopDeletedNap)) {
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
	if !slices.Equal(dels[whoopWorkoutType], []string{whoopRow("workout", whoopDeletedWorkout)}) ||
		!slices.Equal(dels[whoopSleepType], []string{whoopRow("sleep", whoopDeletedNap)}) || len(dels) != 2 {
		t.Fatalf("the second sync deleted %v", dels)
	}
	if whoopLive(t, ds, whoopWorkoutType, whoopRow("workout", whoopDeletedWorkout)) {
		t.Fatal("the deleted workout's row survived")
	}
	if whoopLive(t, ds, whoopSleepType, whoopRow("sleep", whoopDeletedNap)) {
		t.Fatal("the deleted nap's row survived")
	}
	if !whoopLive(t, ds, whoopWorkoutType, whoopRow("workout", whoopLateWorkout)) {
		t.Fatal("the hand-logged workout did not land")
	}
	if n := countLive(t, ds, whoopWorkoutType); n != 29 {
		t.Fatalf("%d workouts are mirrored, want 29", n)
	}
	if n := countLive(t, ds, whoopSleepType); n != 32 {
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
	kept := whoopWorkoutType + "/" + whoopRow("workout", whoopLateWorkout)
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

// TestWhoopWebhookThroughTheDoor posts to the shipped `whoop-webhook` trigger
// the way WHOOP does. The trigger delivers the two headers it declares, the
// engine injects the `webhooksecret` setting the owner filled in and the
// account the first sync stamped, and a signed `workout.deleted` deletes its
// row. A delivery signed with another secret parks with the reason.
func TestWhoopWebhookThroughTheDoor(t *testing.T) {
	svc, ds, fake := newWhoop(t)
	ctx := context.Background()
	secret := whoopSecret(t)
	installTriggers(t, ds, whoopDir, "whoop-webhook")
	for _, d := range loadDocs(t, whoopDir+"/settings.yaml") {
		putDataDoc(t, ds, d)
	}
	st, err := ds.BundleStatus(ctx, whoopPackage)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	for _, item := range st.Setup {
		if item.Record == whoopSecretRecord {
			t.Fatalf("the optional webhook secret is a setup item: %+v", item)
		}
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeSecret, whoopSecretRecord, substrate.PatchInput{
		Properties: map[string]any{"value": secret},
	}); err != nil {
		t.Fatalf("fill in the webhook secret: %v", err)
	}
	whoopSync(t, ds, fake, whoopProps(nil))
	deleted := whoopRow("workout", whoopDeletedWorkout)
	if !whoopLive(t, ds, whoopWorkoutType, deleted) {
		t.Fatal("the first sync did not mirror the workout the delivery deletes")
	}

	authority := ds.Repository().Authority
	post := func(signWith string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"user_id": 4021873, "id": whoopDeletedWorkout, "type": "workout.deleted", "trace_id": "t-door",
		})
		stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		if _, err := svc.ReceiveWebhook(ctx, authority, "whoop-webhook", "", substrate.WebhookRequest{
			Method: "POST", ContentType: "application/json",
			Headers: map[string]string{
				"content-type":                "application/json",
				"x-whoop-signature":           whoopSignature(signWith, stamp, body),
				"x-whoop-signature-timestamp": stamp,
			},
			Body: body,
		}); err != nil {
			t.Fatalf("the door refused the request: %v", err)
		}
	}
	status := func() substrate.TriggerStatus {
		t.Helper()
		all, err := ds.TriggerStatuses(ctx)
		if err != nil {
			t.Fatalf("trigger statuses: %v", err)
		}
		for _, s := range all {
			if s.ID == "whoop-webhook" {
				return s
			}
		}
		t.Fatal("no whoop-webhook trigger status")
		return substrate.TriggerStatus{}
	}
	if got, want := status().WebhookPath, "/webhooks/"+authority+"/whoop-webhook"; got != want {
		t.Fatalf("the webhook path is %q, want %q", got, want)
	}
	waitFor := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !done() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting until %s (trigger status %+v)", what, status())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	post(secret)
	waitFor("the signed workout.deleted deleted its row", func() bool {
		return !whoopLive(t, ds, whoopWorkoutType, deleted)
	})
	waitFor("the signed delivery settled", func() bool {
		s := status()
		return s.Pending == 0 && s.InFlight == 0
	})
	if s := status(); s.Parked != 0 {
		t.Fatalf("the signed delivery parked: %s", s.LastParkedError)
	}

	post("not-the-client-secret")
	waitFor("the forged delivery parked", func() bool { return status().Parked == 1 })
	if msg := status().LastParkedError; !strings.Contains(msg, "X-WHOOP-Signature does not match") {
		t.Fatalf("the forged delivery parked with %q, want the signature refusal", msg)
	}
}
