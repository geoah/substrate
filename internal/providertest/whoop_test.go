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
	"net/url"
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
	whoopAccountKind  = whoopPackage + "/account"
	whoopUserKind     = whoopPackage + "/user"
	whoopCycleKind    = whoopPackage + "/cycle"
	whoopRecoveryKind = whoopPackage + "/recovery"
	whoopSleepKind    = whoopPackage + "/sleep"
	whoopWorkoutKind  = whoopPackage + "/workout"
	whoopSyncFn       = whoopPackage + "/whoopsync"
	whoopWebhookFn    = whoopPackage + "/whoopwebhook"
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
	// during is a path's hook, run once while that request is being served:
	// whatever it commits lands after the body asked WHOOP and before it has
	// WHOOP's answer.
	during map[string]func()
}

// onRead runs fn once, while the next read of path is being served.
func (f *fakeWhoop) onRead(path string, fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.during == nil {
		f.during = map[string]func(){}
	}
	f.during[path] = fn
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
	hook := f.during[r.URL.Path]
	delete(f.during, r.URL.Path)
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
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

// whoopSameRow asserts two runs wrote one row with the same properties: the
// sync's and the webhook's mapping of one WHOOP object agree, omissions
// included, which a version comparison cannot see because a patch leaves an
// omitted property alone.
func whoopSameRow(t *testing.T, a, b []engine.StepEffect, kind, id string) {
	t.Helper()
	pa, pb := whoopRowWrite(t, a, kind, id), whoopRowWrite(t, b, kind, id)
	if !reflect.DeepEqual(pa, pb) {
		t.Fatalf("the sync and the webhook map %s %s differently:\nsync    %v\nwebhook %v", kind, id, pa, pb)
	}
}

// whoopRowWrite is the properties of the newest write a run staged for one
// mirror row: a put where the row was absent or deleted, a patch where it was
// live (`whoop_mirror.write_row`), so a first sync and a later webhook compare
// whichever each wrote.
func whoopRowWrite(t *testing.T, effects []engine.StepEffect, kind, id string) map[string]any {
	t.Helper()
	for i := len(effects) - 1; i >= 0; i-- {
		ef := &effects[i]
		if (ef.Action == "patch" || ef.Action == "put") && ef.Kind == kind && ef.ID == id {
			return ef.Properties
		}
	}
	t.Fatalf("no %s write for %s in %d effects", kind, id, len(effects))
	return nil
}

// whoopLive is live under the name whoop_adapter_test.go and
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
func whoopHookConfig(t *testing.T, ds substrate.Dataset, fake *fakeWhoop) map[string]any {
	t.Helper()
	row := mustGet(t, ds, whoopAccountKind, whoopAccount)
	if refIDs(row, "user") == nil {
		t.Fatal("the account names no WHOOP member; the first sync did not stamp account.user")
	}
	return whoopConfig(fake, row.Properties)
}

// --- the tests --------------------------------------------------------------------

// TestWhoopWebhookFixturesAreSignedAsWhoopDocuments is a fixture-integrity
// check, and no product code runs in it: every recorded delivery verifies
// under the fixture secret over the timestamp header plus the body, by
// WHOOP's documented scheme, except 08, which another secret signed. The
// door's own check runs in TestWhoopWebhookThroughTheDoor.
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

// TestWhoopWebhookLeavesTheSignatureToTheDoor: the body no longer verifies.
// A delivery stripped of its signature headers, arriving an hour after it was
// signed, under a config holding no secret, is acted on, because the door
// checked the signature before the fire. The body refuses only a request it
// cannot read as an event.
func TestWhoopWebhookLeavesTheSignatureToTheDoor(t *testing.T) {
	_, ds, fake := newWhoop(t)
	// No sync ran: the account's `user` is handed in directly.
	props := whoopProps(map[string]any{"user": map[string]any{"ref": whoopUserKind + "/" + whoopRow("user", whoopMember)}})
	cfg := whoopConfig(fake, props)

	garbled := whoopDelivery(t, "01_sleep.updated.json")
	garbled["body"] = map[string]any{"text": "not json"}
	if _, _, err := whoopHook(t, ds, cfg, whoopArrived(garbled, time.Now())); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Fatalf("a body that is not an event: err = %v, want the JSON refusal", err)
	}

	stripped := whoopDelivery(t, "01_sleep.updated.json")
	stripped["headers"] = map[string]any{"content-type": "application/json; charset=utf-8"}
	out, _ := mustHook(t, ds, cfg, whoopArrived(stripped, whoopSigned(t, whoopDelivery(t, "01_sleep.updated.json")).Add(time.Hour)))
	if !live(t, ds, whoopSleepKind, whoopRow("sleep", whoopLastSleep)) {
		t.Fatalf("sleep.updated without its signature headers wrote no sleep row (output %v)", out)
	}
	if got := fake.readBacks(); !slices.Equal(got, []string{"/v2/activity/sleep/" + whoopLastSleep}) {
		t.Fatalf("the delivery read %v, want the one sleep", got)
	}
}

// whoopGrantFor is the scope set the OAuth facility requests for an account
// whose named toggles are on: the union of their shipped featureScopes.
func whoopGrantFor(t *testing.T, toggles ...string) []string {
	t.Helper()
	b, ok := bundleRegistry(t, whoopDir).BundleOf(whoopPackage)
	if !ok || b.OAuth2 == nil {
		t.Fatal("the WHOOP bundle declares no oauth2 block")
	}
	var out []string
	for _, toggle := range toggles {
		for _, s := range b.OAuth2.FeatureScopes[toggle] {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// TestWhoopRecoveryEventUnderTheRecoveryGrantAlone: `recovery.updated` names
// a sleep, so the body reads that sleep to find the recovery's cycle. An
// account with recoveries on and sleeps off is granted only what its toggles
// request, and that grant must cover the sleep read, or every recovery event
// fails at WHOOP and parks. Under a grant that lacks the scope (connected
// before its toggle asked for it), a retry with the same token is refused the
// same way: the recovery row is marked for the sync, whose read of a recovery
// needs no `read:sleep`, and an event naming a sleep with no recovery row is
// dropped with WHOOP's refusal as the reason.
func TestWhoopRecoveryEventUnderTheRecoveryGrantAlone(t *testing.T) {
	_, ds, fake := newWhoop(t)
	props := whoopProps(map[string]any{
		"user":         map[string]any{"ref": whoopUserKind + "/" + whoopRow("user", whoopMember)},
		"enabledSleep": false, "enabledCycles": false, "enabledWorkouts": false, "enabledProfile": false,
	})
	cfg := whoopConfig(fake, props)
	recoveryRow := whoopRecoveryKind + "/" + whoopRow("recovery", whoopCurrentCycle)

	fake.grant(whoopGrantFor(t, "enabledRecovery"))
	out, _, err := whoopHook(t, ds, cfg, whoopOnTime(t, "02_recovery.updated.json"))
	if err != nil {
		t.Fatalf("recovery.updated under the recovery toggle's own grant failed: %v", err)
	}
	if fmt.Sprint(out["wrote"]) != fmt.Sprint([]any{recoveryRow}) {
		t.Fatalf("recovery.updated wrote %v, want only %s (sleeps are off)", out["wrote"], recoveryRow)
	}

	fake.grant([]string{"read:recovery", "read:cycles", "read:profile", "offline"})
	out, effects, err := whoopHook(t, ds, cfg, whoopOnTime(t, "02_recovery.updated.json"))
	if err != nil {
		t.Fatalf("a grant without read:sleep failed the delivery, which parks it: %v", err)
	}
	if len(effects) != 1 || fmt.Sprint(out["marked"]) != fmt.Sprint([]any{recoveryRow}) ||
		!strings.Contains(fmt.Sprint(out["skipped"]), "403") {
		t.Fatalf("a grant without read:sleep: output %v, %d effects; want the recovery row marked, naming the 403", out, len(effects))
	}
	if mustGet(t, ds, whoopRecoveryKind, whoopRow("recovery", whoopCurrentCycle)).Properties["refreshRequestedAt"] == nil {
		t.Fatal("the recovery row holds no refreshRequestedAt mark")
	}

	// A nap has no recovery row to mark.
	now := time.Now()
	env := whoopEvent(t, whoopSecret(t), now, map[string]any{
		"user_id": 4021873, "id": whoopDeletedNap, "type": "recovery.updated", "trace_id": "t-nap",
	})
	out, effects, err = whoopHook(t, ds, cfg, whoopArrived(env, now))
	if err != nil {
		t.Fatalf("a 403 for an event with no row to mark failed the delivery, which parks it: %v", err)
	}
	if len(effects) != 0 || !strings.Contains(fmt.Sprint(out["skipped"]), "403") {
		t.Fatalf("a 403 with no row to mark: output %v, %d effects; want the event dropped naming the 403", out, len(effects))
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
	cfg := whoopHookConfig(t, ds, fake)
	before := whoopVersions(t, ds)

	sleepRow := whoopSleepKind + "/" + whoopRow("sleep", whoopLastSleep)
	recoveryRow := whoopRecoveryKind + "/" + whoopRow("recovery", whoopCurrentCycle)

	// sleep.updated, for a sleep the sync wrote from the same object.
	out, hooked := mustHook(t, ds, cfg, whoopOnTime(t, "01_sleep.updated.json"))
	if fmt.Sprint(out["wrote"]) != fmt.Sprint([]any{sleepRow}) {
		t.Fatalf("sleep.updated wrote %v, want %s", out["wrote"], sleepRow)
	}
	whoopSameRow(t, synced, hooked, whoopSleepKind, whoopRow("sleep", whoopLastSleep))
	// recovery.updated names the SLEEP: the body reads it for its cycle and
	// then reads the recovery through the cycle.
	out, hooked = mustHook(t, ds, cfg, whoopOnTime(t, "02_recovery.updated.json"))
	if !slices.Contains(out["wrote"].([]any), any(recoveryRow)) {
		t.Fatalf("recovery.updated wrote %v, want %s among them", out["wrote"], recoveryRow)
	}
	whoopSameRow(t, synced, hooked, whoopRecoveryKind, whoopRow("recovery", whoopCurrentCycle))
	whoopSameRow(t, synced, hooked, whoopSleepKind, whoopRow("sleep", whoopLastSleep))
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
	late := mustGet(t, ds, whoopWorkoutKind, whoopRow("workout", whoopLateWorkout))
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
		{whoopWorkoutKind, whoopRow("workout", whoopDeletedWorkout)},
		{whoopSleepKind, whoopRow("sleep", whoopDeletedNap)},
		{whoopRecoveryKind, whoopRow("recovery", whoopCurrentCycle)},
	} {
		if live(t, ds, row[0], row[1]) {
			t.Errorf("%s/%s survived its delete event", row[0], row[1])
		}
	}
	if !live(t, ds, whoopSleepKind, whoopRow("sleep", whoopLastSleep)) {
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
	// back and kept as it is. That page still carries the deleted workout,
	// whose row is deleted: it is read back too, WHOOP answers 404, and the
	// stale page does not bring it back.
	fake.setPhase(0)
	workoutsOnly := whoopSteady(nil)
	for _, toggle := range []string{"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep"} {
		workoutsOnly[toggle] = false
	}
	kept := whoopWorkoutKind + "/" + whoopRow("workout", whoopLateWorkout)
	before := whoopVersions(t, ds)[kept]
	reads := len(fake.readBacks())
	effects = whoopSync(t, ds, fake, workoutsOnly)
	if got, want := fake.readBacks()[reads:], []string{
		"/v2/activity/workout/" + whoopDeletedWorkout,
		"/v2/activity/workout/" + whoopLateWorkout,
	}; !slices.Equal(got, want) {
		t.Fatalf("the third sync read back %v, want %v", got, want)
	}
	if dels := whoopDeletes(effects); len(dels) != 0 {
		t.Fatalf("a read back WHOOP answered deleted %v", dels)
	}
	if live(t, ds, whoopWorkoutKind, whoopRow("workout", whoopDeletedWorkout)) {
		t.Fatal("a page read before the deletion brought the deleted workout back")
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
	if got := whoopStarts(fake, "/v2/recovery"); len(got) == 0 || got[0] > "2026-09-05" {
		t.Fatalf("the recovery walk started at %v, want a week before the oldest recovery", got)
	}

	// Nothing is left without a cycleId, so the run after reads the usual
	// window and deletes nothing.
	walked := len(whoopStarts(fake, "/v2/recovery"))
	effects = whoopSync(t, ds, fake, noCycles(whoopSteady(nil)))
	if dels := whoopDeletes(effects); len(dels) != 0 {
		t.Fatalf("the run after the re-walk deleted %v", dels)
	}
	if got := whoopStarts(fake, "/v2/recovery")[walked:]; len(got) == 0 || got[0] != "2026-10-08T15:00:00Z" {
		t.Fatalf("the run after the re-walk started at %v, want 48 hours before the cursor", got)
	}
}

// TestWhoopSyncTitlesACycleByTheDayItsMemberWokeInto: a cycle's `day` is
// `start` at the cycle's own offset, 12 hours on, and its title reads it.
// Cycle 1047561 starts at 22:55 on 9 October at +03:00, so its member woke
// into 10 October.
func TestWhoopSyncTitlesACycleByTheDayItsMemberWokeInto(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	row := mustGet(t, ds, whoopCycleKind, whoopRow("cycle", whoopCurrentCycle))
	if got := row.Properties["day"]; got != "2026-10-10" {
		t.Fatalf("cycle %s has day %v, want 2026-10-10", whoopCurrentCycle, got)
	}
	if row.Title != "Cycle 2026-10-10" {
		t.Fatalf("cycle %s is titled %q, want %q", whoopCurrentCycle, row.Title, "Cycle 2026-10-10")
	}
}

// TestWhoopSyncRewalksCyclesWrittenWithoutADay: cycles synced before version
// 24 have no `day` and are titled by their WHOOP id. The next run reaches back
// a week past the oldest of them and writes each one's `day`, and the run
// after reads the usual window.
func TestWhoopSyncRewalksCyclesWrittenWithoutADay(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	for _, row := range listLive(t, ds, whoopCycleKind) {
		whoopPatch(t, ds, whoopCycleKind, row.ID, map[string]any{"day": nil})
	}
	old := mustGet(t, ds, whoopCycleKind, whoopRow("cycle", whoopCurrentCycle))
	if want := "Cycle " + whoopCurrentCycle; old.Title != want {
		t.Fatalf("a cycle with no day is titled %q, want %q", old.Title, want)
	}

	effects := whoopSync(t, ds, fake, whoopSteady(nil))
	if dels := whoopDeletes(effects); len(dels) != 0 {
		t.Fatalf("the re-walk deleted %v", dels)
	}
	rows := listLive(t, ds, whoopCycleKind)
	if len(rows) != 30 {
		t.Fatalf("%d cycles are mirrored, want 30", len(rows))
	}
	for _, row := range rows {
		day, _ := row.Properties["day"].(string)
		if day == "" || row.Title != "Cycle "+day {
			t.Errorf("cycle %v has day %q and title %q after the re-walk",
				row.Properties["cycleId"], day, row.Title)
		}
	}
	// The oldest cycle starts on 2026-09-10, so the re-walk starts a week
	// before it, not 48 hours before the cursor.
	if got := whoopStarts(fake, "/v2/cycle"); len(got) == 0 || got[0] > "2026-09-04" {
		t.Fatalf("the cycle walk started at %v, want a week before the oldest cycle", got)
	}

	walked := len(whoopStarts(fake, "/v2/cycle"))
	whoopSync(t, ds, fake, whoopSteady(nil))
	if got := whoopStarts(fake, "/v2/cycle")[walked:]; len(got) == 0 || got[0] != "2026-10-08T15:00:00Z" {
		t.Fatalf("the run after the re-walk started at %v, want 48 hours before the cursor", got)
	}
}

// whoopStarts is the `start` of every page of one collection the fake served.
func whoopStarts(f *fakeWhoop, path string) []string {
	var out []string
	queries := f.seen()
	for i, p := range f.seenPaths() {
		if p != path {
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

// TestShippedWebhookTriggersAreClosed pins how each shipped webhook trigger
// keeps an install from taking unauthenticated requests. WHOOP ships enabled
// because its declared signature fails closed until the config record holds
// the client secret. Apple Health has no signature to declare: it ships
// disabled and keyless, and the key the owner sets is its gate.
func TestShippedWebhookTriggersAreClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dir, id         string
		enabled, signed bool
	}{
		{whoopDir, "whoop-webhook", true, true},
		{appleHealthDir, "applehealth-webhook", false, false},
	} {
		var found bool
		for _, d := range loadDocs(t, tc.dir+"/triggers.yaml") {
			meta, _ := d["metadata"].(map[string]any)
			if meta["id"] != tc.id {
				continue
			}
			found = true
			data, _ := d["data"].(map[string]any)
			props, _ := data["properties"].(map[string]any)
			source, _ := props["source"].(map[string]any)
			arm, _ := source["webhook"].(map[string]any)
			if props["enabled"] != tc.enabled {
				t.Errorf("%s ships enabled = %v, want %v", tc.id, props["enabled"], tc.enabled)
			}
			if _, keyed := arm["key"]; keyed {
				t.Errorf("%s ships a key; the owner chooses one", tc.id)
			}
			sig, _ := arm["signature"].(map[string]any)
			if (sig != nil) != tc.signed {
				t.Errorf("%s declares a signature = %v, want %v", tc.id, sig != nil, tc.signed)
			}
			if tc.signed && sig["secret"] != "oauthClientSecret" {
				t.Errorf("%s signs with %v, want the OAuth client secret", tc.id, sig["secret"])
			}
		}
		if !found {
			t.Errorf("%s/triggers.yaml ships no %s", tc.dir, tc.id)
		}
	}
}

// TestWhoopWebhookThroughTheDoor posts to the shipped `whoop-webhook` trigger
// the way WHOOP does. The trigger declares WHOOP's signature, so the door
// checks it against the config record's client secret before it stores
// anything (decision 0164). Before that record exists, a signed delivery is
// refused; after it, an unsigned, a forged and a stale one are refused, each
// with the door's one 404 because the trigger ships keyless, and each
// leaving no blob, no ledger entry and no run, and a signed
// `workout.deleted` deletes its row.
func TestWhoopWebhookThroughTheDoor(t *testing.T) {
	svc, ds, fake := newWhoop(t)
	ctx := context.Background()
	secret := whoopSecret(t)
	installTriggers(t, ds, whoopDir, "whoop-webhook")
	whoopSync(t, ds, fake, whoopProps(nil))
	deleted := whoopRow("workout", whoopDeletedWorkout)
	if !live(t, ds, whoopWorkoutKind, deleted) {
		t.Fatal("the first sync did not mirror the workout the delivery deletes")
	}

	authority := ds.Repository().Authority
	request := func(signWith string, at time.Time) substrate.WebhookRequest {
		body, _ := json.Marshal(map[string]any{
			"user_id": 4021873, "id": whoopDeletedWorkout, "type": "workout.deleted", "trace_id": "t-door",
		})
		stamp := strconv.FormatInt(at.UnixMilli(), 10)
		return substrate.WebhookRequest{
			Method: "POST", ContentType: "application/json",
			Headers: map[string]string{
				"content-type":                "application/json",
				"x-whoop-signature":           whoopSignature(signWith, stamp, body),
				"x-whoop-signature-timestamp": stamp,
			},
			Body: body,
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
	refused := func(name string, req substrate.WebhookRequest) {
		t.Helper()
		blobs, runs := countLive(t, ds, "substrate.reamde.dev/core/blob"), countLive(t, ds, "substrate.reamde.dev/core/triggerrun")
		if _, err := svc.ReceiveWebhook(ctx, authority, "whoop-webhook", "", req); !errors.Is(err, substrate.ErrNotFound) || !strings.Contains(err.Error(), "signature refused") {
			t.Fatalf("%s: err = %v, want the door's one refusal for a keyless trigger's signature", name, err)
		}
		if n := countLive(t, ds, "substrate.reamde.dev/core/blob"); n != blobs {
			t.Fatalf("%s: the refused request stored %d blobs", name, n-blobs)
		}
		if n := countLive(t, ds, "substrate.reamde.dev/core/triggerrun"); n != runs {
			t.Fatalf("%s: the refused request ran %d times", name, n-runs)
		}
		if failures, err := ds.TriggerFailures(ctx, "whoop-webhook"); err != nil || len(failures) != 0 {
			t.Fatalf("%s: the ledger holds %+v (%v)", name, failures, err)
		}
		if !live(t, ds, whoopWorkoutKind, deleted) {
			t.Fatalf("%s: the refused request deleted the workout", name)
		}
	}

	// Shipped enabled and closed: no config record, no client secret, so a
	// request WHOOP signed is refused like any other.
	refused("a signed delivery before the config exists", request(secret, time.Now()))

	mustPut(t, ds, substrate.PutInput{
		Kind: whoopPackage + "/config", ID: "default",
		Properties: map[string]any{"clientId": "whoop-client", "clientSecret": secret},
	})
	unsigned := request(secret, time.Now())
	delete(unsigned.Headers, "x-whoop-signature")
	refused("an unsigned delivery", unsigned)
	refused("a delivery another secret signed", request("not-the-client-secret", time.Now()))
	refused("a delivery signed ten minutes ago", request(secret, time.Now().Add(-10*time.Minute)))

	if _, err := svc.ReceiveWebhook(ctx, authority, "whoop-webhook", "", request(secret, time.Now())); err != nil {
		t.Fatalf("the door refused a signed request: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for live(t, ds, whoopWorkoutKind, deleted) {
		if time.Now().After(deadline) {
			t.Fatalf("the signed workout.deleted never deleted its row (trigger status %+v)", status())
		}
		time.Sleep(100 * time.Millisecond)
	}
	for deadline = time.Now().Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		s := status()
		if s.Pending == 0 && s.InFlight == 0 {
			if s.Parked != 0 {
				t.Fatalf("the signed delivery parked: %s", s.LastParkedError)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the signed delivery never settled: %+v", s)
		}
	}
}

// whoopSettingsReport is a body that returns the names under its injected
// `config.settings`, sorted.
const whoopSettingsReport = `
def main(input, host):
    settings = (input.get("config") or {}).get("settings") or {}
    return {"output": {"settings": sorted(settings.keys())}}
`

// TestWhoopFunctionsReceiveNoWebhookSecretSetting pins that the WHOOP closure
// ships no secret or setting: the door checks WHOOP's signature against the
// OAuth client secret (decision 0164), so neither `whoopsync` nor
// `whoopwebhook` receives a copy of it in `config.settings`.
func TestWhoopFunctionsReceiveNoWebhookSecretSetting(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ctx := context.Background()
	files, err := filepath.Glob(whoopDir + "/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("the WHOOP closure's files: %v %v", files, err)
	}
	for _, f := range files {
		for _, d := range loadDocs(t, f) {
			if k := d["kind"]; k == "substrate.reamde.dev/core/secret" || k == "substrate.reamde.dev/core/setting" {
				t.Fatalf("%s ships a %v: %v", f, k, d["metadata"])
			}
		}
	}

	_, ds := newCoreDataset(t)
	install(t, ds, whoopDir, func(docs []map[string]any) {
		for _, d := range docs {
			meta, _ := d["metadata"].(map[string]any)
			data, _ := d["data"].(map[string]any)
			if id := meta["id"]; id == whoopSyncFn || id == whoopWebhookFn {
				data["source"] = whoopSettingsReport
			}
		}
	})
	for _, fn := range []string{whoopSyncFn, whoopWebhookFn} {
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fn, map[string]any{})
		if err != nil {
			t.Fatalf("call %s: %v", fn, err)
		}
		om, _ := out.(map[string]any)
		if names, _ := om["settings"].([]any); len(names) != 0 {
			t.Fatalf("%s receives config.settings %v from a fresh install", fn, names)
		}
	}
}

// whoopHookStaged runs the webhook body once and hands back its stepper with
// the effects staged and not applied, so a test can land another write first.
func whoopHookStaged(t *testing.T, ds substrate.Dataset, cfg, env map[string]any) (*stepper, []engine.StepEffect) {
	t.Helper()
	s := newStepper(t, ds, whoopWebhookFn, cfg)
	s.setEnvelope(env)
	effects, _, cur := s.step(nil)
	if cur != nil {
		t.Fatalf("the webhook body handed back a cursor: %v", cur)
	}
	return s, effects
}

// TestWhoopWebhookGuardsItsWriteWithTheRowsVersion: a webhook's write is
// staged under the version of the row it read. A newer write that lands
// first refuses it, and the retry keeps the newer row. A deletion that lands
// first refuses it too, and the retry, which reads WHOOP again and is
// answered 404, leaves the row deleted rather than writing it back.
func TestWhoopWebhookGuardsItsWriteWithTheRowsVersion(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	cfg := whoopHookConfig(t, ds, fake)
	sleep := whoopRow("sleep", whoopLastSleep)

	// A newer revision lands between the read and the commit.
	s, _ := whoopHookStaged(t, ds, cfg, whoopOnTime(t, "01_sleep.updated.json"))
	mustPut(t, ds, substrate.PutInput{
		Kind: whoopSleepKind, ID: sleep,
		Properties: map[string]any{"nap": true, "updatedAt": "2026-10-12T09:00:00Z"},
	})
	var cas *substrate.VersionConflictError
	if err := s.s.Apply(context.Background()); !errors.As(err, &cas) {
		t.Fatalf("a webhook write over a row written since it was read applied: err = %v, want a version conflict", err)
	}
	out, effects := mustHook(t, ds, cfg, whoopOnTime(t, "01_sleep.updated.json"))
	if len(effects) != 0 || fmt.Sprint(out["kept"]) != fmt.Sprint([]any{whoopSleepKind + "/" + sleep}) {
		t.Fatalf("the retry: output %v, %d effects; want the newer row kept", out, len(effects))
	}
	if got := mustGet(t, ds, whoopSleepKind, sleep).Properties["nap"]; got != true {
		t.Fatalf("the sleep's nap is %v, want the newer write's", got)
	}

	// WHOOP deletes the workout, and its deletion lands between another
	// delivery's read and its commit.
	workout := whoopRow("workout", whoopLateWorkout)
	mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	s, _ = whoopHookStaged(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	fake.forget("/v2/activity/workout/" + whoopLateWorkout)
	if _, err := ds.Delete(t.Context(), substrate.ActorAPI, whoopWorkoutKind, workout, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	// A patch of a deleted row is refused before its version is compared.
	if err := s.s.Apply(context.Background()); !errors.As(err, &cas) && !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a webhook write over a row deleted since it was read applied: err = %v, want it refused", err)
	}
	if live(t, ds, whoopWorkoutKind, workout) {
		t.Fatal("the refused write brought the deleted workout back")
	}
	mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	if live(t, ds, whoopWorkoutKind, workout) {
		t.Fatal("the retry wrote back a workout WHOOP deleted")
	}
}

// TestWhoopWebhookMarksAnUpdateItCannotReadForTheSync: an update WHOOP
// answers 429 for, one under the account's retry hold and one with no live
// token are not dropped: the row the event names is marked
// `refreshRequestedAt`. The 429 also puts its `Retry-After` on the
// account's `retryNotBefore`, and until then neither a delivery nor a sync
// asks WHOOP anything. A later sync, whose window does not reach the
// object, reads the marked row back by id and clears the mark. An update for
// an object with no row to mark fails the delivery, so the request is kept,
// after a page of its own commits the hold.
func TestWhoopWebhookMarksAnUpdateItCannotReadForTheSync(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	cfg := whoopHookConfig(t, ds, fake)
	late := whoopRow("workout", whoopLateWorkout)
	lateRow := whoopWorkoutKind + "/" + late
	readBack := "/v2/activity/workout/" + whoopLateWorkout
	mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	mark := func() any {
		return mustGet(t, ds, whoopWorkoutKind, late).Properties["refreshRequestedAt"]
	}
	clearMark := func() {
		t.Helper()
		if _, err := ds.Patch(t.Context(), substrate.ActorAPI, whoopWorkoutKind, late,
			substrate.PatchInput{Properties: map[string]any{"refreshRequestedAt": nil}}); err != nil {
			t.Fatal(err)
		}
	}

	// WHOOP answers 429, `Retry-After: 60`: the row is marked, and the
	// account holds the instant WHOOP named.
	fake.limit(readBack, 1)
	out, effects := mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	if len(effects) != 2 || fmt.Sprint(out["marked"]) != fmt.Sprint([]any{lateRow}) || mark() == nil {
		t.Fatalf("a 429: output %v, %d effects, mark %v; want the workout row marked and the account held", out, len(effects), mark())
	}
	hold, _ := mustGet(t, ds, whoopAccountKind, whoopAccount).Properties["retryNotBefore"].(string)
	if at, err := time.Parse(time.RFC3339, hold); err != nil || time.Until(at) < 50*time.Second || time.Until(at) > 70*time.Second {
		t.Fatalf("after a 429 with Retry-After: 60 the account's retryNotBefore is %q, want a minute ahead", hold)
	}
	clearMark()

	// Until that instant neither another delivery nor the sync asks WHOOP
	// anything: the delivery marks the row instead.
	held := whoopHookConfig(t, ds, fake)
	asked := len(fake.seenPaths())
	out, _ = mustHook(t, ds, held, whoopOnTime(t, "04_workout.updated.json"))
	if fmt.Sprint(out["marked"]) != fmt.Sprint([]any{lateRow}) || len(fake.seenPaths()) != asked {
		t.Fatalf("under the hold: output %v, %d requests; want the row marked and nothing asked", out, len(fake.seenPaths())-asked)
	}
	whoopSync(t, ds, fake, mustGet(t, ds, whoopAccountKind, whoopAccount).Properties)
	if len(fake.seenPaths()) != asked {
		t.Fatalf("a sync under the hold asked WHOOP %v", fake.seenPaths()[asked:])
	}
	clearMark()

	// The account's token failed to refresh.
	tokenless := whoopHookConfig(t, ds, fake)
	acct := tokenless["accounts"].([]any)[0].(map[string]any)
	delete(acct, "token")
	acct["tokenError"] = "refresh failed: whoop returned HTTP 503"
	out, _ = mustHook(t, ds, tokenless, whoopOnTime(t, "04_workout.updated.json"))
	if fmt.Sprint(out["marked"]) != fmt.Sprint([]any{lateRow}) {
		t.Fatalf("without a token: output %v; want the row marked", out)
	}

	// No row to mark: the delivery fails and the request is kept.
	now := time.Now()
	env := whoopEvent(t, whoopSecret(t), now, map[string]any{
		"user_id": 4021873, "id": "0b4c6e1f-0000-4000-8000-000000000001", "type": "workout.updated", "trace_id": "t-new",
	})
	fake.limit("/v2/activity/workout/0b4c6e1f-0000-4000-8000-000000000001", 1)
	cursors, err := whoopHookChain(t, ds, cfg, whoopArrived(env, now), nil)
	if err == nil || !strings.Contains(err.Error(), "keeping the request") {
		t.Fatalf("a 429 for an object with no row: err = %v, want the delivery failed so the request is kept", err)
	}
	// The hold commits on a page of its own before the delivery fails.
	if len(cursors) != 1 || mustGet(t, ds, whoopAccountKind, whoopAccount).Properties["retryNotBefore"] == nil {
		t.Fatalf("a 429 for an object with no row: cursors %v; want the account's hold committed before the delivery failed", cursors)
	}

	// The sync's window starts after the workout, so only the mark brings it
	// back; its write clears the mark.
	props := whoopSteady(map[string]string{"workout": "2026-10-20T00:00:00Z"})
	for _, toggle := range []string{"enabledProfile", "enabledCycles", "enabledRecovery", "enabledSleep"} {
		props[toggle] = false
	}
	reads := len(fake.readBacks())
	whoopSync(t, ds, fake, props)
	if got := fake.readBacks()[reads:]; !slices.Contains(got, readBack) {
		t.Fatalf("the sync read back %v, want the marked workout", got)
	}
	if mark() != nil {
		t.Fatalf("the marked workout still holds refreshRequestedAt %v after the sync read it back", mark())
	}
}

// whoopHookChain runs one delivery's pages the way the dispatcher does: each
// page's effects commit before the next page runs, and a page that fails
// ends the chain with its error. resume is the cursor a parked delivery
// stands at, nil for a fresh one. It returns every cursor a page handed back.
func whoopHookChain(t *testing.T, ds substrate.Dataset, cfg, env, resume map[string]any) ([]map[string]any, error) {
	t.Helper()
	s := newStepper(t, ds, whoopWebhookFn, cfg)
	s.setEnvelope(env)
	var cur any
	if resume != nil {
		cur = resume
	}
	var cursors []map[string]any
	for range 10 {
		_, _, next, err := s.s.Step(context.Background(), cur)
		if err != nil {
			return cursors, err
		}
		if err := s.s.Apply(context.Background()); err != nil {
			t.Fatalf("apply a webhook page's effects: %v", err)
		}
		if next == nil {
			return cursors, nil
		}
		cursors = append(cursors, next)
		cur = next
	}
	t.Fatal("the webhook's pages did not end in 10 steps")
	return nil, nil
}

// TestWhoopWebhookReachesEveryAccountOfTheMember: two connections hold one
// WHOOP member, and the first by id is paused. The event still reaches the
// other one, which writes its row; the paused one is named as skipped.
func TestWhoopWebhookReachesEveryAccountOfTheMember(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	cfg := whoopHookConfig(t, ds, fake)
	const paused = "a-old"
	other := whoopProps(map[string]any{
		"syncPaused": true,
		"user":       map[string]any{"ref": whoopUserKind + "/" + runner.ExternalID("whoop", paused, "user/"+whoopMember)},
	})
	cfg["accounts"] = append([]any{map[string]any{
		"id": paused, "kind": whoopAccountKind, "properties": other, "token": fakeToken,
	}}, cfg["accounts"].([]any)...)

	out, _ := mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	want := whoopWorkoutKind + "/" + whoopRow("workout", whoopLateWorkout)
	if fmt.Sprint(out["wrote"]) != fmt.Sprint([]any{want}) {
		t.Fatalf("the event wrote %v, want %s for the account that is not paused", out["wrote"], want)
	}
	if !strings.Contains(fmt.Sprint(out["skipped"]), paused+": the account is paused") {
		t.Fatalf("the output's skipped is %v, want the paused account named", out["skipped"])
	}
}

// TestWhoopRecoveryEventIsKeptWhenOnlyItsSleepCanBeMarked: an old sleep
// whose recovery the mirror does not hold, and a `recovery.updated` event
// for it under the account's retry hold. Marking the sleep alone would lose
// the update: a sync reads a marked sleep back as a sleep and never asks for
// its recovery, and the recovery's cycle is outside the sync's window. So the
// delivery keeps the request, and its retry once the hold is over writes the
// recovery.
func TestWhoopRecoveryEventIsKeptWhenOnlyItsSleepCanBeMarked(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	recovery := whoopRow("recovery", whoopCurrentCycle)
	if _, err := ds.Delete(t.Context(), substrate.ActorAPI, whoopRecoveryKind, recovery, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	cfg := whoopHookConfig(t, ds, fake)
	acct := cfg["accounts"].([]any)[0].(map[string]any)
	acct["properties"].(map[string]any)["retryNotBefore"] = ahead(time.Hour)
	env := whoopOnTime(t, "02_recovery.updated.json")

	cursors, err := whoopHookChain(t, ds, cfg, env, nil)
	if err == nil || !strings.Contains(err.Error(), "keeping the request") {
		t.Fatalf("a recovery event with only its sleep row to mark: err = %v, want the request kept", err)
	}
	if len(cursors) == 0 {
		t.Fatal("the delivery handed back no cursor to retry from")
	}

	// The sync reads the marked sleep back and clears its mark, and the
	// recovery stays missing.
	fake.windowed = true
	after := map[string]string{}
	for _, key := range []string{"cycle", "recovery", "sleep", "workout"} {
		after[key] = "2026-10-20T00:00:00Z"
	}
	reads := len(fake.readBacks())
	whoopSync(t, ds, fake, whoopSteady(after))
	if got := fake.readBacks()[reads:]; !slices.Equal(got, []string{"/v2/activity/sleep/" + whoopLastSleep}) {
		t.Fatalf("the sync read back %v, want the marked sleep alone", got)
	}
	if live(t, ds, whoopRecoveryKind, recovery) {
		t.Fatal("the sync brought the recovery back, so this test no longer shows why the request is kept")
	}

	// The retry, once WHOOP's hold is over.
	delete(acct["properties"].(map[string]any), "retryNotBefore")
	if _, err := whoopHookChain(t, ds, cfg, env, cursors[len(cursors)-1]); err != nil {
		t.Fatalf("the kept request's retry failed: %v", err)
	}
	if !live(t, ds, whoopRecoveryKind, recovery) {
		t.Fatal("the kept request's retry did not write the recovery")
	}
}

// TestWhoopWebhookReadsTheRowBeforeAskingWhoop: a deletion that commits
// while a delivery waits for WHOOP's answer refuses that delivery's write.
// Read after the answer, the row would be the tombstone, and a put under the
// tombstone's version restores the row.
func TestWhoopWebhookReadsTheRowBeforeAskingWhoop(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	cfg := whoopHookConfig(t, ds, fake)
	workout := whoopRow("workout", whoopLateWorkout)
	mustHook(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))

	fake.onRead("/v2/activity/workout/"+whoopLateWorkout, func() {
		if _, err := ds.Delete(context.Background(), substrate.ActorAPI, whoopWorkoutKind, workout, substrate.DeleteInput{}); err != nil {
			t.Errorf("delete the workout during the read: %v", err)
		}
	})
	s, _ := whoopHookStaged(t, ds, cfg, whoopOnTime(t, "04_workout.updated.json"))
	var cas *substrate.VersionConflictError
	if err := s.s.Apply(context.Background()); !errors.As(err, &cas) && !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a webhook write over a row deleted while it read WHOOP applied: err = %v, want it refused", err)
	}
	if live(t, ds, whoopWorkoutKind, workout) {
		t.Fatal("the webhook restored a workout deleted while it read WHOOP")
	}
}

// TestWhoopSyncReadsADeletedRowBackBeforeAPageWritesIt: a page can be read
// before a deletion commits, so a page record whose row is deleted is read
// back by id first. WHOOP deleted the workout (its page, read too early,
// still carries it): the read answers 404 and the row stays deleted. WHOOP
// still has the recovery whose row was deleted: the read answers it and the
// row is written back.
func TestWhoopSyncReadsADeletedRowBackBeforeAPageWritesIt(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	workout := whoopRow("workout", whoopDeletedWorkout)
	recovery := whoopRow("recovery", whoopCurrentCycle)
	if _, err := ds.Delete(t.Context(), substrate.ActorAPI, whoopRecoveryKind, recovery, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	fake.onRead("/v2/activity/workout", func() {
		fake.forget("/v2/activity/workout/" + whoopDeletedWorkout)
		if _, err := ds.Delete(context.Background(), substrate.ActorAPI, whoopWorkoutKind, workout, substrate.DeleteInput{}); err != nil {
			t.Errorf("delete the workout during the page read: %v", err)
		}
	})

	reads := len(fake.readBacks())
	whoopSync(t, ds, fake, whoopProps(nil))
	if got, want := fake.readBacks()[reads:], []string{
		"/v2/cycle/" + whoopCurrentCycle + "/recovery",
		"/v2/activity/workout/" + whoopDeletedWorkout,
	}; !slices.Equal(got, want) {
		t.Fatalf("the sync read back %v, want %v", got, want)
	}
	if live(t, ds, whoopWorkoutKind, workout) {
		t.Fatal("a page read before the deletion brought the deleted workout back")
	}
	if !live(t, ds, whoopRecoveryKind, recovery) {
		t.Fatal("the recovery WHOOP still has was not written back over its deleted row")
	}
}

// TestWhoopWebhookCommitsTheAccountsThatFinish: two connections hold one
// WHOOP member. The first has no live token and no row to mark, so its part
// of the event must be kept; the second reads the workout and writes it. The
// second's row commits on the first page, the delivery fails naming only the
// first, and a retry from its cursor acts for the first alone.
func TestWhoopWebhookCommitsTheAccountsThatFinish(t *testing.T) {
	_, ds, fake := newWhoop(t)
	whoopSync(t, ds, fake, whoopProps(nil))
	cfg := whoopHookConfig(t, ds, fake)
	const broken = "a-old"
	brokenAcct := map[string]any{
		"id": broken, "kind": whoopAccountKind, "tokenError": "refresh failed: whoop returned HTTP 503",
		"properties": whoopProps(map[string]any{
			"user": map[string]any{"ref": whoopUserKind + "/" + runner.ExternalID("whoop", broken, "user/"+whoopMember)},
		}),
	}
	cfg["accounts"] = append([]any{brokenAcct}, cfg["accounts"].([]any)...)
	env := whoopOnTime(t, "04_workout.updated.json")
	workout := whoopRow("workout", whoopLateWorkout)

	cursors, err := whoopHookChain(t, ds, cfg, env, nil)
	if err == nil || !strings.Contains(err.Error(), "the account has no live token") {
		t.Fatalf("err = %v, want the delivery failed naming %s", err, broken)
	}
	if len(cursors) != 1 || fmt.Sprint(cursors[0]["retry"]) != fmt.Sprint([]any{broken}) {
		t.Fatalf("cursors %v, want one naming %s alone", cursors, broken)
	}
	if !live(t, ds, whoopWorkoutKind, workout) {
		t.Fatal("the account with a live token did not commit its workout")
	}

	// The retry, once the first account has its token back, reads for it alone.
	before := whoopVersions(t, ds)[whoopWorkoutKind+"/"+workout]
	delete(brokenAcct, "tokenError")
	brokenAcct["token"] = fakeToken
	reads := len(fake.readBacks())
	if _, err := whoopHookChain(t, ds, cfg, env, cursors[0]); err != nil {
		t.Fatalf("the retry failed: %v", err)
	}
	if got := fake.readBacks()[reads:]; len(got) != 1 {
		t.Fatalf("the retry read %v, want one read for %s", got, broken)
	}
	if !live(t, ds, whoopWorkoutKind, runner.ExternalID("whoop", broken, "workout/"+whoopLateWorkout)) {
		t.Fatalf("the retry did not write %s's workout", broken)
	}
	if v := whoopVersions(t, ds)[whoopWorkoutKind+"/"+workout]; v != before {
		t.Fatalf("the retry rewrote the finished account's workout: version %d to %d", before, v)
	}
}
