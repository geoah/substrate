package providertest

// The Garmin bundle's two functions. `garminpush` is driven with the push
// deliveries under testdata/garmin as its envelope, the way the webhook door
// hands them over, and `garminsync` against a loopback Garmin that answers
// the user id, the permissions and the backfill requests. The deliveries are
// built from Garmin's documented shapes, not captured from a live account:
// Garmin's partner program was closed to new partners when they were cut.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	garminDir         = providersDir + "/garmin"
	garminPackage     = "providers.substrate.reamde.dev/garmin"
	garminAccountType = garminPackage + "/account"
	garminNoticeType  = garminPackage + "/notice"
	garminPushFn      = garminPackage + "/garminpush"
	garminSyncFn      = garminPackage + "/garminsync"
	garminAccountID   = "acct"
	// garminUser and garminClient are the ids the fixture deliveries carry.
	garminUser     = "7c1e5b9a0d2f4c3e8b6a1f0e9d8c7b6a"
	garminClient   = "9f2d6c1a-3b4e-4f5a-8c7d-0e1f2a3b4c5d"
	garminTestdata = "testdata/garmin"
)

// garminKinds maps each push envelope key to the kind it lands as.
var garminKinds = map[string]string{
	"dailies": "daily", "epochs": "epoch", "sleeps": "sleep", "bodyComps": "bodycomp",
	"stressDetails": "stressdetail", "userMetrics": "usermetric", "pulseox": "pulseox",
	"allDayRespiration": "respiration", "healthSnapshot": "healthsnapshot", "hrv": "hrv",
	"bloodPressures": "bloodpressure", "skinTemp": "skintemp", "activities": "activity",
	"activityDetails": "activitydetail", "manuallyUpdatedActivities": "manuallyupdatedactivity",
	"moveIQActivities": "moveiq",
}

// garminBackfilled is every envelope key Garmin backfills, with its path:
// manuallyUpdatedActivities has none.
var garminBackfilled = map[string]string{
	"dailies": "dailies", "epochs": "epochs", "sleeps": "sleeps", "bodyComps": "bodyComps",
	"stressDetails": "stressDetails", "userMetrics": "userMetrics", "pulseox": "pulseOx",
	"allDayRespiration": "respiration", "healthSnapshot": "healthSnapshot", "hrv": "hrv",
	"bloodPressures": "bloodPressures", "skinTemp": "skinTemp", "activities": "activities",
	"activityDetails": "activityDetails", "moveIQActivities": "moveiq",
}

// garminFixture is one testdata delivery: the request the door records, and
// the body decoded.
type garminFixture struct {
	name    string
	request map[string]any
	body    map[string][]map[string]any
}

func loadGarminFixture(t *testing.T, name string) garminFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(garminTestdata, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var env struct {
		Request map[string]any `json:"request"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	f := garminFixture{name: name, request: env.Request}
	text, _ := env.Request["body"].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(text), &f.body); err != nil {
		t.Fatalf("decode the body of %s: %v", name, err)
	}
	return f
}

// withBody is the fixture's request carrying another body.
func (f garminFixture) withBody(t *testing.T, body any) map[string]any {
	t.Helper()
	text, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := map[string]any{}
	for k, v := range f.request {
		req[k] = v
	}
	req["body"] = map[string]any{"text": string(text)}
	return req
}

// garminEnvelope is a webhook delivery's envelope: the fire and the request.
func garminEnvelope(fire string, req map[string]any) map[string]any {
	return map[string]any{
		"fire":    map[string]any{"id": fire, "at": time.Now().UTC().Format(time.RFC3339)},
		"request": req,
	}
}

// newGarminDataset installs the closure and creates the account with every
// toggle at its default (on).
func newGarminDataset(t *testing.T, props map[string]any) substrate.Dataset {
	t.Helper()
	_, ds := newCoreDataset(t)
	install(t, ds, garminDir, nil)
	if props == nil {
		props = map[string]any{}
	}
	mustPut(t, ds, substrate.PutInput{Kind: garminAccountType, ID: garminAccountID, Properties: props})
	return ds
}

// garminConfig is the injected config: the account as stored plus the
// connector-written properties a test hands it (the user id a first sync
// recorded), its token, and the client input.
func garminConfig(t *testing.T, ds substrate.Dataset, apiBase string, extra map[string]any) map[string]any {
	t.Helper()
	row := mustGet(t, ds, garminAccountType, garminAccountID)
	props := map[string]any{}
	for k, v := range row.Properties {
		props[k] = v
	}
	for k, v := range extra {
		props[k] = v
	}
	client := map[string]any{"clientId": garminClient}
	if apiBase != "" {
		client["apiBase"] = apiBase
	}
	return map[string]any{
		"accounts": []any{map[string]any{
			"id": garminAccountID, "kind": garminAccountType, "properties": props, "token": fakeToken,
		}},
		"inputs": map[string]any{"client": map[string]any{"properties": client}},
	}
}

// pushGarmin drains one delivery through garminpush, applying every page, and
// answers how many invocations it took.
func pushGarmin(t *testing.T, ds substrate.Dataset, cfg map[string]any, fire string, req map[string]any) int {
	t.Helper()
	s := newStepper(t, ds, garminPushFn, cfg)
	s.setEnvelope(garminEnvelope(fire, req))
	s.drainApplying(nil)
	return s.n
}

// normal is a JSON value with every number as float64, so a stored value and
// the delivered one compare whatever integer type each was decoded as.
func normal(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// bySummaryID is one kind's live rows keyed by their summaryId.
func bySummaryID(t *testing.T, ds substrate.Dataset, kind string) map[string]*substrate.Record {
	t.Helper()
	out := map[string]*substrate.Record{}
	for _, row := range listLive(t, ds, garminPackage+"/"+kind) {
		id, _ := row.Properties["summaryId"].(string)
		if _, dup := out[id]; dup {
			t.Fatalf("%s: two rows carry summaryId %s", kind, id)
		}
		out[id] = row
	}
	return out
}

// TestGarminPushWritesEverySummaryType: one delivery of every summary type
// lands one row per summaryId, at `<account>/<summaryId>`, with every field
// the delivery carried stored verbatim under Garmin's own name, and the
// account stamped by the body.
func TestGarminPushWritesEverySummaryType(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, nil)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})

	files := []string{
		"push_dailies.json", "push_epochs.json", "push_sleeps.json", "push_bodyComps.json",
		"push_stressDetails.json", "push_userMetrics.json", "push_pulseox.json",
		"push_allDayRespiration.json", "push_healthSnapshot.json", "push_hrv.json",
		"push_bloodPressures.json", "push_skinTemp.json", "push_activities.json",
		"push_activityDetails_21004995534.json", "push_manuallyUpdatedActivities.json",
		"push_moveIQActivities.json",
	}
	want := map[string]map[string]map[string]any{}
	for i, name := range files {
		f := loadGarminFixture(t, name)
		pushGarmin(t, ds, cfg, fmt.Sprintf("webhook-%d", i), f.request)
		for key, entries := range f.body {
			kind, ok := garminKinds[key]
			if !ok {
				t.Fatalf("%s carries %s, which no kind mirrors", name, key)
			}
			if want[kind] == nil {
				want[kind] = map[string]map[string]any{}
			}
			for _, e := range entries {
				want[kind][e["summaryId"].(string)] = e
			}
		}
	}
	if len(want) != len(garminKinds) {
		t.Fatalf("the fixtures cover %d summary types, want all %d", len(want), len(garminKinds))
	}

	for kind, entries := range want {
		rows := bySummaryID(t, ds, kind)
		if len(rows) != len(entries) {
			t.Errorf("%s: %d rows, want one per summaryId (%d)", kind, len(rows), len(entries))
		}
		for sid, entry := range entries {
			row := rows[sid]
			if row == nil {
				t.Errorf("%s %s: no row", kind, sid)
				continue
			}
			if row.ID != garminAccountID+"/"+sid {
				t.Errorf("%s %s: id %q, want <account>/<summaryId>", kind, sid, row.ID)
			}
			if got := refIDs(row, "account"); len(got) != 1 || got[0] != garminAccountID {
				t.Errorf("%s %s: account %v", kind, sid, got)
			}
			for field, v := range entry {
				if !reflect.DeepEqual(normal(t, row.Properties[field]), normal(t, v)) {
					t.Errorf("%s %s: %s = %v, delivered %v", kind, sid, field,
						row.Properties[field], v)
				}
			}
		}
	}

	// The temporal slots are the summary's own seconds as instants.
	daily := bySummaryID(t, ds, "daily")["x4b2c9e1-6aa31a50"]
	start := time.Unix(1789074000, 0).UTC()
	if daily.At == nil || !daily.At.Equal(start) || daily.EndsAt == nil || !daily.EndsAt.Equal(start.Add(24*time.Hour)) {
		t.Errorf("daily at/endsAt = %v/%v, want %v and a day later", daily.At, daily.EndsAt, start)
	}
	sleep := bySummaryID(t, ds, "sleep")["x4b2c9e1-6aa30c2d"]
	if sleep.At == nil || sleep.EndsAt == nil || !sleep.EndsAt.After(*sleep.At) {
		t.Errorf("sleep at/endsAt = %v/%v", sleep.At, sleep.EndsAt)
	}
	detail := bySummaryID(t, ds, "activitydetail")["21004995534-detail"]
	if samples, _ := detail.Properties["samples"].([]any); len(samples) != 2180 {
		t.Errorf("activity detail holds %d samples, want 2180", len(samples))
	}
	if detail.At == nil || !detail.At.Equal(time.Unix(1791349537, 0).UTC()) {
		t.Errorf("activity detail at = %v", detail.At)
	}
	if metric := bySummaryID(t, ds, "usermetric")["x4b2c9e1-6aa46bd0"]; metric.At != nil {
		t.Errorf("a user metric binds no temporal trait, yet at = %v", metric.At)
	}

	acct := mustGet(t, ds, garminAccountType, garminAccountID)
	if acct.Properties["syncState"] != "ok" || acct.Properties["lastPushAt"] == nil ||
		acct.Properties["lastSyncedAt"] == nil || acct.Properties["lastSyncDurationMs"] == nil {
		t.Errorf("the push did not stamp the account's sync trait: %v", acct.Properties)
	}
	if msg, _ := acct.Properties["syncMessage"].(string); msg != "push: 3 moveIQActivities" {
		t.Errorf("syncMessage = %q, want the last delivery's counts", msg)
	}
	if n := parkedFailures(t, ds); n != 0 {
		t.Errorf("%d parked deliveries", n)
	}
}

// TestGarminPushResentSummaryRewritesTheRow: Garmin re-sends a summary under
// the same summaryId when it has more (a day filling in, a sleep turning
// final). The re-send rewrites the row whole: what it carries replaces the
// old values, and a field it no longer carries is cleared rather than left
// standing.
func TestGarminPushResentSummaryRewritesTheRow(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, nil)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	const sid = "x4b2c9e1-6ac56150"

	first := loadGarminFixture(t, "push_dailies.json")
	pushGarmin(t, ds, cfg, "webhook-1", first.request)
	before := bySummaryID(t, ds, "daily")[sid]
	partial, _ := before.Properties["timeOffsetHeartRateSamples"].(map[string]any)

	resend := loadGarminFixture(t, "push_dailies_2026-10-07_resend.json")
	full := resend.body["dailies"][0]
	if full["summaryId"] != sid {
		t.Fatalf("the re-send carries %v, want %s", full["summaryId"], sid)
	}
	if _, ok := full["floorsClimbed"]; !ok {
		t.Fatal("the re-send fixture carries no floorsClimbed to drop")
	}
	delete(full, "floorsClimbed")
	pushGarmin(t, ds, cfg, "webhook-2", resend.withBody(t, resend.body))

	rows := bySummaryID(t, ds, "daily")
	if len(rows) != len(first.body["dailies"]) {
		t.Fatalf("%d daily rows after the re-send, want %d", len(rows), len(first.body["dailies"]))
	}
	after := rows[sid]
	if after.ID != before.ID || after.Version <= before.Version {
		t.Fatalf("the re-send did not rewrite the row: %s v%d -> %s v%d", before.ID, before.Version, after.ID, after.Version)
	}
	samples, _ := after.Properties["timeOffsetHeartRateSamples"].(map[string]any)
	if len(samples) != 5660 || len(samples) <= len(partial) {
		t.Errorf("heart rate samples %d -> %d, want the re-send's 5660", len(partial), len(samples))
	}
	if !reflect.DeepEqual(normal(t, after.Properties["steps"]), normal(t, full["steps"])) {
		t.Errorf("steps = %v, want the re-send's %v", after.Properties["steps"], full["steps"])
	}
	if v, ok := after.Properties["floorsClimbed"]; ok {
		t.Errorf("floorsClimbed = %v survived a re-send that no longer carries it", v)
	}

	// A sleep turning final: same id, the final validation and its scores.
	pushGarmin(t, ds, cfg, "webhook-3", loadGarminFixture(t, "push_sleeps.json").request)
	tentative := bySummaryID(t, ds, "sleep")["x4b2c9e1-6ac54ecc"]
	if tentative == nil || tentative.Properties["validation"] != "ENHANCED_TENTATIVE" {
		t.Fatalf("the 2026-10-07 sleep did not land tentative: %v", tentative)
	}
	pushGarmin(t, ds, cfg, "webhook-4", loadGarminFixture(t, "push_sleeps_2026-10-07_final.json").request)
	final := bySummaryID(t, ds, "sleep")["x4b2c9e1-6ac54ecc"]
	if final.ID != tentative.ID || final.Properties["validation"] != "ENHANCED_FINAL" ||
		final.Properties["overallSleepScore"] == nil {
		t.Errorf("the final sleep did not replace the tentative one: %v", final.Properties)
	}
}

// TestGarminPushRecordsLifecycleNotices: a permission change and a
// disconnect each land as a notice row, and the account takes the outcome:
// the permissions the user left, and the disconnect mark with the sync
// erroring.
func TestGarminPushRecordsLifecycleNotices(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, nil)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})

	pushGarmin(t, ds, cfg, "webhook-perm", loadGarminFixture(t, "push_userPermissionsChange.json").request)
	acct := mustGet(t, ds, garminAccountType, garminAccountID)
	if got := normal(t, acct.Properties["permissions"]); !reflect.DeepEqual(got, []any{"ACTIVITY_EXPORT", "HEALTH_EXPORT"}) {
		t.Errorf("permissions = %v", got)
	}
	if acct.Properties["permissionsChangedAt"] == nil {
		t.Error("permissionsChangedAt was not stamped")
	}

	pushGarmin(t, ds, cfg, "webhook-dereg", loadGarminFixture(t, "push_deregistrations.json").request)
	notices := listLive(t, ds, garminNoticeType)
	types := []string{}
	for _, n := range notices {
		types = append(types, n.Properties["noticeType"].(string))
		if n.Properties["userId"] != garminUser || n.At == nil {
			t.Errorf("notice %s: %v at %v", n.ID, n.Properties, n.At)
		}
	}
	slices.Sort(types)
	if !slices.Equal(types, []string{"deregistration", "permissionchange"}) {
		t.Fatalf("notices = %v", types)
	}
	acct = mustGet(t, ds, garminAccountType, garminAccountID)
	if acct.Properties["deregisteredAt"] == nil || acct.Properties["syncState"] != "erroring" ||
		acct.Properties["syncError"] == nil {
		t.Errorf("the disconnect did not mark the account: %v", acct.Properties)
	}

	// Data for the user again means the user connected again: the mark goes.
	cfg = garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	pushGarmin(t, ds, cfg, "webhook-data", loadGarminFixture(t, "push_bloodPressures.json").request)
	acct = mustGet(t, ds, garminAccountType, garminAccountID)
	if v, ok := acct.Properties["deregisteredAt"]; ok {
		t.Errorf("deregisteredAt = %v survived a push of data", v)
	}
}

// TestGarminPushRefusesAnUnknownUser: a delivery none of whose summaries
// names a connected account's Garmin user id fails with an error naming the
// id and writes nothing, so it parks for a retry once the account exists. A
// delivery that mixes users writes the known one's summaries.
func TestGarminPushRefusesAnUnknownUser(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, nil)

	// The account has not recorded its user id yet: nothing matches.
	cfg := garminConfig(t, ds, "", nil)
	s, err := engine.NewStepper(ds, garminPushFn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := loadGarminFixture(t, "push_bodyComps.json")
	s.SetEnvelope(garminEnvelope("webhook-1", f.request))
	_, _, _, err = s.Step(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no connected account has Garmin user id "+garminUser) {
		t.Fatalf("an unknown user's delivery answered %v, want the refusal naming %s", err, garminUser)
	}
	if n := countLive(t, ds, garminPackage+"/bodycomp"); n != 0 {
		t.Fatalf("%d rows written for an unknown user", n)
	}

	// A summary is matched by its userId alone. One carrying only an OAuth
	// 1.0a `userAccessToken` is an unknown user even where an account holds
	// that token: the row would have no userId, which every mirror requires.
	tokenOnly := map[string]any{}
	for k, v := range f.body["bodyComps"][0] {
		if k != "userId" {
			tokenOnly[k] = v
		}
	}
	tokenOnly["userAccessToken"] = "oauth1-token"
	s, err = engine.NewStepper(ds, garminPushFn, garminConfig(t, ds, "", map[string]any{"userAccessToken": "oauth1-token"}))
	if err != nil {
		t.Fatal(err)
	}
	s.SetEnvelope(garminEnvelope("webhook-token", f.withBody(t, map[string]any{"bodyComps": []any{tokenOnly}})))
	effects, _, _, err := s.Step(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no connected account has Garmin user id") || len(effects) != 0 {
		t.Fatalf("a summary naming only a userAccessToken answered %v with %d effects, want the unknown-user refusal", err, len(effects))
	}

	// A mixed delivery: the known user's summaries land, the stranger's do not.
	cfg = garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	entries := f.body["bodyComps"]
	stranger := map[string]any{}
	for k, v := range entries[0] {
		stranger[k] = v
	}
	stranger["userId"] = "0000000000000000000000000000ffff"
	stranger["summaryId"] = "stranger-1"
	mixed := map[string]any{"bodyComps": append([]map[string]any{stranger}, entries...)}
	s2 := newStepper(t, ds, garminPushFn, cfg)
	s2.setEnvelope(garminEnvelope("webhook-2", f.withBody(t, mixed)))
	_, out, _ := s2.step(nil)
	s2.apply()
	rows := bySummaryID(t, ds, "bodycomp")
	if len(rows) != len(entries) || rows["stranger-1"] != nil {
		t.Fatalf("%d body composition rows, want the known user's %d", len(rows), len(entries))
	}
	if !strings.Contains(fmt.Sprint(out), "0000000000000000000000000000ffff") {
		t.Errorf("the output does not name the unknown user: %v", out)
	}

	// The push's garmin-client-id must be this bundle's.
	other := garminConfig(t, ds, "", map[string]any{"userId": garminUser})
	other["inputs"].(map[string]any)["client"].(map[string]any)["properties"].(map[string]any)["clientId"] = "another-client"
	s3, err := engine.NewStepper(ds, garminPushFn, other)
	if err != nil {
		t.Fatal(err)
	}
	s3.SetEnvelope(garminEnvelope("webhook-3", f.request))
	if _, _, _, err := s3.Step(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "garmin-client-id") {
		t.Fatalf("a push for another client answered %v, want the refusal", err)
	}
}

// TestGarminPushPagesALargeDelivery: a body worth more than one invocation's
// frame is written over several invocations of one chain, and nothing is
// lost or written twice. Eight copies of the 36-minute run's detail are
// about six megabytes, the size the door is set to admit.
func TestGarminPushPagesALargeDelivery(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, nil)
	cfg := garminConfig(t, ds, "", map[string]any{"userId": garminUser})

	f := loadGarminFixture(t, "push_activityDetails_21004995534.json")
	detail := f.body["activityDetails"][0]
	var many []map[string]any
	for i := range 8 {
		d := map[string]any{}
		for k, v := range detail {
			d[k] = v
		}
		d["summaryId"] = fmt.Sprintf("%s-%d", detail["summaryId"], i)
		many = append(many, d)
	}
	steps := pushGarmin(t, ds, cfg, "webhook-big", f.withBody(t, map[string]any{"activityDetails": many}))
	if steps < 2 {
		t.Errorf("a %d-copy body took %d invocation; want it paged", len(many), steps)
	}
	if n := countLive(t, ds, garminPackage+"/activitydetail"); n != len(many) {
		t.Errorf("%d activity details, want %d", n, len(many))
	}
}

// TestGarminPushFollowsAPing: a ping names a callback URL instead of carrying
// summaries; the body fetches it with the account's token and writes what it
// answers. A callback outside the API base is refused before the token is
// sent.
func TestGarminPushFollowsAPing(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, nil)
	fake := newFakeGarmin(t)
	base := fake.ts.URL + "/wellness-api"
	cfg := garminConfig(t, ds, base, map[string]any{"userId": garminUser})

	f := loadGarminFixture(t, "ping_dailies.json")
	text, _ := f.request["body"].(map[string]any)["text"].(string)
	req := f.withBody(t, nil)
	req["body"] = map[string]any{"text": strings.ReplaceAll(text, "https://apis.garmin.com", fake.ts.URL)}
	pushGarmin(t, ds, cfg, "webhook-ping", req)

	if fake.count("pull dailies") != 1 {
		t.Fatalf("the ping's callback was fetched %d times, want once", fake.count("pull dailies"))
	}
	var answer []map[string]any
	raw, err := os.ReadFile(filepath.Join(garminTestdata, "GET_wellness-api_rest_dailies.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatal(err)
	}
	rows := bySummaryID(t, ds, "daily")
	if len(rows) != len(answer) || rows[answer[0]["summaryId"].(string)] == nil {
		t.Fatalf("%d daily rows from the ping, want the callback's %d", len(rows), len(answer))
	}

	stray := f.withBody(t, nil)
	stray["body"] = map[string]any{"text": strings.ReplaceAll(text, "https://apis.garmin.com", "http://127.0.0.2:9")}
	s, err := engine.NewStepper(ds, garminPushFn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.SetEnvelope(garminEnvelope("webhook-stray", stray))
	if _, _, _, err := s.Step(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "outside the API base") {
		t.Fatalf("a callback outside the API base answered %v, want the refusal", err)
	}
}

// fakeGarmin answers the calls garminsync makes and the pull a ping names.
// A backfill window is accepted (202) the first time it is asked for and a
// conflict (409) every time after, which is what Garmin answers a repeat.
type fakeGarmin struct {
	fakeAPI

	mu sync.Mutex
	// asked is every backfill window, "<path> <start> <end>", in order.
	asked []string
	seen  map[string]bool
	// refuse answers a backfill path with this status.
	refuse map[string]int
	// limitAfter answers 429 to every backfill request past this many.
	limitAfter int
}

func newFakeGarmin(t *testing.T) *fakeGarmin {
	t.Helper()
	f := &fakeGarmin{seen: map[string]bool{}, refuse: map[string]int{}}
	pull, err := os.ReadFile(filepath.Join(garminTestdata, "GET_wellness-api_rest_dailies.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /wellness-api/rest/user/id", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if bearer(w, r) {
			writeJSON(w, map[string]any{"userId": garminUser})
		}
	})
	mux.HandleFunc("GET /wellness-api/rest/user/permissions", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if bearer(w, r) {
			writeJSON(w, []string{"ACTIVITY_EXPORT", "WORKOUT_IMPORT", "HEALTH_EXPORT"})
		}
	})
	mux.HandleFunc("GET /wellness-api/rest/dailies", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		f.bump("pull dailies")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pull)
	})
	mux.HandleFunc("GET /wellness-api/rest/backfill/{path}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		path := r.PathValue("path")
		q := r.URL.Query()
		key := path + " " + q.Get("summaryStartTimeInSeconds") + " " + q.Get("summaryEndTimeInSeconds")
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.limitAfter > 0 && len(f.asked) >= f.limitAfter {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		f.asked = append(f.asked, key)
		if code := f.refuse[path]; code != 0 {
			w.WriteHeader(code)
			writeJSON(w, map[string]any{"errorMessage": "refused"})
			return
		}
		if f.seen[key] {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"errorMessage": "Duplicate backfill request for this time range"})
			return
		}
		f.seen[key] = true
		w.WriteHeader(http.StatusAccepted)
	})
	f.serve(t, mux)
	return f
}

// askedCount is how many backfill windows the fake has answered.
func (f *fakeGarmin) askedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

// windows is every backfill window asked for, per path, in order.
func (f *fakeGarmin) windows(t *testing.T) map[string][][2]int64 {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][][2]int64{}
	for _, a := range f.asked {
		parts := strings.Fields(a)
		start, err1 := strconv.ParseInt(parts[1], 10, 64)
		end, err2 := strconv.ParseInt(parts[2], 10, 64)
		if err1 != nil || err2 != nil {
			t.Fatalf("a backfill window without integer bounds: %q", a)
		}
		out[parts[0]] = append(out[parts[0]], [2]int64{start, end})
	}
	return out
}

// syncGarmin drains one garminsync run fired by the account's record trigger.
func syncGarmin(t *testing.T, ds substrate.Dataset, cfg map[string]any) []engine.StepEffect {
	t.Helper()
	s := newStepper(t, ds, garminSyncFn, cfg)
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": garminAccountType, "id": garminAccountID}})
	return s.drainApplying(nil)
}

// TestGarminSyncRequestsBackfillWindows: the first sync records the Garmin
// user id and permissions, then asks for every enabled type's history one
// 24-hour window at a time, back to backfillDepth, and keeps each type's
// place in syncCursors. Garmin answers 202 to a new window and 409 to a
// repeat; a second walk over the same windows meets only 409s and still
// counts every window as asked for.
func TestGarminSyncRequestsBackfillWindows(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, map[string]any{"backfillDepth": "last7d"})
	fake := newFakeGarmin(t)
	base := fake.ts.URL + "/wellness-api"

	// The first invocation records the user id and hands back its cursor
	// before any backfill request, so the id is committed by the time Garmin
	// pushes what the backfill asks for, and those pushes match the account.
	s := newStepper(t, ds, garminSyncFn, garminConfig(t, ds, base, nil))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": garminAccountType, "id": garminAccountID}})
	effects, _, cur := s.step(nil)
	if user := stampOf(effects, garminAccountID); cur == nil || user["userId"] != garminUser || fake.askedCount() != 0 {
		t.Fatalf("the first invocation staged %v, continued %v, and asked %d backfill windows; want the user id alone, a cursor and none",
			user, cur != nil, fake.askedCount())
	}
	s.apply()
	s.drainApplying(cur)
	acct := mustGet(t, ds, garminAccountType, garminAccountID)
	if acct.Properties["userId"] != garminUser {
		t.Fatalf("userId = %v, want %s", acct.Properties["userId"], garminUser)
	}
	if got := normal(t, acct.Properties["permissions"]); !reflect.DeepEqual(got, []any{"ACTIVITY_EXPORT", "HEALTH_EXPORT", "WORKOUT_IMPORT"}) {
		t.Errorf("permissions = %v", got)
	}
	until, err := time.Parse(time.RFC3339, fmt.Sprint(acct.Properties["backfillUntil"]))
	if err != nil {
		t.Fatalf("backfillUntil = %v: %v", acct.Properties["backfillUntil"], err)
	}
	floor := until.Add(-7 * 24 * time.Hour).Unix()

	windows := fake.windows(t)
	if len(windows) != len(garminBackfilled) {
		t.Errorf("backfill asked for %d types, want %d: %v", len(windows), len(garminBackfilled), sortedKeys(windows))
	}
	for key, path := range garminBackfilled {
		ws := windows[path]
		if len(ws) != 7 {
			t.Errorf("%s: %d windows, want 7 one-day windows", path, len(ws))
			continue
		}
		next := until.Unix()
		for _, w := range ws {
			if w[1] != next || w[1]-w[0] > 86400 || w[0] < floor {
				t.Errorf("%s: window %v does not continue back from %d inside the floor %d", path, w, next, floor)
			}
			next = w[0]
		}
		cursor, _ := acct.Properties["syncCursors"].(map[string]any)[key].(string)
		if at, err := time.Parse(time.RFC3339, cursor); err != nil || at.Unix() != floor {
			t.Errorf("syncCursors[%s] = %q, want the floor %s", key, cursor, time.Unix(floor, 0).UTC())
		}
	}
	if _, ok := windows["manuallyUpdatedActivities"]; ok {
		t.Error("asked to backfill manuallyUpdatedActivities, which Garmin offers no backfill for")
	}
	if acct.Properties["syncState"] != "ok" || acct.Properties["lastCompletedAt"] == nil {
		t.Errorf("the walk did not end ok and complete: %v %v", acct.Properties["syncState"], acct.Properties["syncMessage"])
	}

	// The same windows again: every answer is a 409 and every one counts.
	first := fake.askedCount()
	props := map[string]any{"userId": garminUser, "backfillUntil": acct.Properties["backfillUntil"], "syncCursors": nil}
	syncGarmin(t, ds, garminConfig(t, ds, base, props))
	if again := fake.askedCount() - first; again != first {
		t.Errorf("the second walk asked %d windows, want the same %d", again, first)
	}
	acct = mustGet(t, ds, garminAccountType, garminAccountID)
	if acct.Properties["syncState"] != "ok" {
		t.Errorf("a walk answered only 409s ended %v: %v", acct.Properties["syncState"], acct.Properties["syncMessage"])
	}
	for key := range garminBackfilled {
		cursor, _ := acct.Properties["syncCursors"].(map[string]any)[key].(string)
		if at, err := time.Parse(time.RFC3339, cursor); err != nil || at.Unix() != floor {
			t.Errorf("after the 409s syncCursors[%s] = %q, want the floor", key, cursor)
		}
	}
}

// TestGarminSyncHoldsOnRefusalAndRateLimit: a type Garmin refuses (a 403 on
// a permission the user did not grant) stops that type and names it while
// the others carry on; a 429 stops the walk, holds the account until
// Garmin's instant, and keeps the windows already accepted.
func TestGarminSyncHoldsOnRefusalAndRateLimit(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds := newGarminDataset(t, map[string]any{"backfillDepth": "last7d"})
	fake := newFakeGarmin(t)
	base := fake.ts.URL + "/wellness-api"
	fake.refuse["activityDetails"] = http.StatusForbidden

	syncGarmin(t, ds, garminConfig(t, ds, base, nil))
	acct := mustGet(t, ds, garminAccountType, garminAccountID)
	if got := len(fake.windows(t)["activityDetails"]); got != 1 {
		t.Errorf("activityDetails was asked %d times after its 403, want once", got)
	}
	msg, _ := acct.Properties["syncMessage"].(string)
	if !strings.Contains(msg, "activityDetails (HTTP 403)") || acct.Properties["lastCompletedAt"] != nil {
		t.Errorf("a refused type did not show: %q, lastCompletedAt %v", msg, acct.Properties["lastCompletedAt"])
	}
	if got := len(fake.windows(t)["dailies"]); got != 7 {
		t.Errorf("dailies asked %d windows beside the refused type, want 7", got)
	}

	// A deeper reach on a fresh account, cut off by a 429 part way.
	ds2 := newGarminDataset(t, map[string]any{"backfillDepth": "last30d"})
	fake2 := newFakeGarmin(t)
	fake2.limitAfter = 20
	syncGarmin(t, ds2, garminConfig(t, ds2, fake2.ts.URL+"/wellness-api", nil))
	acct = mustGet(t, ds2, garminAccountType, garminAccountID)
	if acct.Properties["syncState"] != "throttled" || acct.Properties["retryNotBefore"] == nil {
		t.Fatalf("a 429 left %v, retryNotBefore %v", acct.Properties["syncState"], acct.Properties["retryNotBefore"])
	}
	cursors, _ := acct.Properties["syncCursors"].(map[string]any)
	accepted := 0
	for _, ws := range fake2.windows(t) {
		accepted += len(ws)
	}
	if accepted != 20 || len(cursors) == 0 {
		t.Errorf("%d windows accepted before the 429 and %d cursors kept", accepted, len(cursors))
	}
}
