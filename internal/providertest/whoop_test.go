package providertest

// The WHOOP bundle — the sync-only wearable integration. Three proofs, from
// the shipped closure at ../../kinds/providers.substrate.reamde.dev/whoop:
//
//  1. TestWhoopBundleAdmitsSchema — the closure ADMITS through the schema
//     loader: the bundle declares the `client` input (facility-read, never
//     injected) the oauth2 block names, the config kind wears oauth2, the
//     account wears
//     accountconfig (and NOT oauth2), the trusted oauth2 block compiles with
//     all three feature toggles mapped (each carrying offline + read:profile
//     so any one toggle mints a refresh token and derives the email) and NO
//     revocation endpoint (WHOOP revokes via an authenticated DELETE the host
//     facility cannot speak — revocation is manual, per the README), the
//     three mirror types land with their shapes, and the install closure
//     balances. No DB, no uv — pure schema admission.
//
//  2. TestWhoopBundleInstalls — the whole closure installs into a live repository
//     and every member (bundle, types, function, both triggers) lands. Warms
//     the PEP 723 sync body through uv, so it SKIPS when uv is absent or
//     cannot provision.
//
//  3. TestWhoopBundleFakeSyncMirrors — the whole flow against a LOOPBACK fake
//     WHOOP (httptest; no live providers, ever, in tests): OAuth connect, the
//     on-connect trigger, the paged nextToken drain off the causal chain, the
//     CYCLE-keyed recovery ids (two records created on the SAME UTC date land
//     as two rows), per-record failure tolerance (one malformed record
//     skipped and counted into syncStatus, its page kept), and the
//     lastSyncedAt stamp.

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	whoopDir          = providersDir + "/whoop"
	whoopPackage      = "providers.substrate.reamde.dev/whoop"
	whoopConfigType   = whoopPackage + "/config"
	whoopAccountType  = whoopPackage + "/account"
	whoopRecoveryType = whoopPackage + "/recovery"
	whoopSleepType    = whoopPackage + "/sleep"
	whoopWorkoutType  = whoopPackage + "/workout"
	whoopSyncFn       = whoopPackage + "/whoopsync"

	whoopProdBase = "https://api.prod.whoop.com"
)

// TestWhoopBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm.
func TestWhoopBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, whoopDir)

	// The bundle exists and declares the `client` input the oauth2 block
	// names: facility-read, so it must NOT inject.
	b := assertBundleInput(t, reg, whoopPackage, "client", whoopConfigType, "")

	// The trusted oauth2 block compiled: WHOOP endpoints, the email derivation
	// pair, and every toggle mapped — each scope list carrying its read scope
	// plus offline (the refresh token) and read:profile (the email grant).
	if b.OAuth2 == nil {
		t.Fatal("bundle compiled no oauth2 metadata")
	}
	if b.OAuth2.ClientInput != "client" {
		t.Fatalf("oauth2 clientInput = %q, want %q", b.OAuth2.ClientInput, "client")
	}
	if b.OAuth2.AuthorizationEndpoint != whoopProdBase+"/oauth/oauth2/auth" ||
		b.OAuth2.TokenEndpoint != whoopProdBase+"/oauth/oauth2/token" {
		t.Fatalf("oauth2 endpoints wrong: %+v", b.OAuth2)
	}
	// DELIBERATELY no revocation endpoint: WHOOP's documented revocation is an
	// OAuth-authenticated DELETE /developer/v2/user/access — a method+auth
	// shape the host facility (RFC 7009-style form POST only) cannot speak.
	// Declaring an invented endpoint made every deletion changelog a revoke failure
	// while the remote grant stayed live; revocation is manual (README).
	if b.OAuth2.RevocationEndpoint != "" {
		t.Fatalf("bundle declares a revocation endpoint %q — WHOOP has none the host can speak",
			b.OAuth2.RevocationEndpoint)
	}
	if b.OAuth2.EmailEndpoint == "" || b.OAuth2.EmailProperty != "email" {
		t.Fatalf("email derivation not wired: endpoint=%q property=%q",
			b.OAuth2.EmailEndpoint, b.OAuth2.EmailProperty)
	}
	for toggle, read := range map[string]string{
		"enabledRecovery": "read:recovery",
		"enabledSleep":    "read:sleep",
		"enabledWorkouts": "read:workout",
	} {
		scopes := b.OAuth2.FeatureScopes[toggle]
		if len(scopes) == 0 {
			t.Fatalf("toggle %s maps to no scopes", toggle)
		}
		for _, want := range []string{read, "read:profile", "offline"} {
			found := false
			for _, s := range scopes {
				if s == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("toggle %s misses scope %s: %v", toggle, want, scopes)
			}
		}
	}

	// The config type: oauth2 (client fields), the client input's kind.
	cfg := mustKind(t, reg, whoopConfigType)
	if !cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("config type does not implement %s", vocabulary.TraitOAuth2Core)
	}

	// The account type: accountconfig, and NOT oauth2 — client creds bind on
	// the config; the facility's and the connector's hands carry writer roles.
	acct := mustKind(t, reg, whoopAccountType)
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("account type does not implement %s", vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatal("account type implements oauth2 — client creds belong on the config, not the account")
	}
	for prop, writer := range map[string]string{
		"tokenRef":      vocabulary.WriterOAuth,
		"email":         vocabulary.WriterOAuth,
		"lastSyncedAt":  vocabulary.WriterConnector,
		"syncStatus":    vocabulary.WriterConnector,
		"syncFrequency": vocabulary.WriterOwner,
	} {
		p, ok := acct.Prop(prop)
		if !ok {
			t.Fatalf("account declares no %s", prop)
		}
		if p.Writer != writer {
			t.Fatalf("account %s writer = %q, want %q", prop, p.Writer, writer)
		}
	}
	// The cadence enums are labeled, so the console renders human choices.
	for _, name := range []string{"syncFrequency", "backfillDepth"} {
		p, _ := acct.Prop(name)
		if p.Datatype != vocabulary.DatatypeEnum || len(p.Values) < 3 {
			t.Fatalf("%s is not a labeled enum: kind=%s values=%v", name, p.Datatype, p.Values)
		}
		for _, v := range p.Values {
			if v.Label == "" {
				t.Fatalf("%s value %q carries no label", name, v.Value)
			}
		}
	}

	// The three mirror types land with their key fields.
	for typ, want := range map[string][]string{
		whoopRecoveryType: {"account", "cycleId", "date", "recoveryScore", "hrvMs", "restingHeartRate", "raw"},
		whoopSleepType:    {"account", "start", "end", "performance", "durations", "raw"},
		whoopWorkoutType:  {"account", "start", "end", "sport", "strain", "calories", "raw"},
	} {
		ty, ok := reg.ByIdentity(typ)
		if !ok {
			t.Fatalf("mirror type %s missing", typ)
		}
		for _, prop := range want {
			if _, ok := ty.Prop(prop); !ok {
				t.Fatalf("%s declares no %s property", typ, prop)
			}
		}
	}

	// The sync function is a member of the authority.
	if _, err := reg.ResolveFunction(whoopSyncFn); err != nil {
		t.Fatalf("sync function %s did not register: %v", whoopSyncFn, err)
	}
}

// TestWhoopBundleInstalls applies the whole closure into a live repository and
// asserts every member installs. It warms the PEP 723 sync body through uv,
// so it skips when uv is absent or cannot provision.
func TestWhoopBundleInstalls(t *testing.T) {
	t.Parallel()
	requireUV(t)
	_, ds := newDataset(t)

	// The atomic install from the shipped manifest.
	install(t, ds, whoopDir, nil)

	// The bundle row and every schema member landed as its own record.
	assertMembers(t, ds, map[string]string{
		whoopPackage:      typeBundle,
		whoopConfigType:   typeKind,
		whoopAccountType:  typeKind,
		whoopRecoveryType: typeKind,
		whoopSleepType:    typeKind,
		whoopWorkoutType:  typeKind,
		whoopSyncFn:       typeFunction,
	})

	// Computed status: installed, enabled, and the closure's member counts.
	assertUnresolvedInput(t, ds, whoopPackage, "client", whoopConfigType, 1)

	// The delivery wiring installs as ordinary data records, both bound to
	// the sync function.
	installTriggers(t, ds, whoopDir)
	assertTriggers(t, ds, "whoop-on-connect", "whoop-scheduled")
}

// fakeWhoop is a WHOOP in a box: the OAuth endpoints the manifest is rewired
// to, the basic-profile endpoint the facility derives the email from, and the
// three v2 collections the sync pages — recovery over TWO pages (a nextToken
// hop) with one deliberately malformed record on page one.
type fakeWhoop struct {
	fakeAPI
}

func newFakeWhoop(t *testing.T) *fakeWhoop {
	t.Helper()
	f := &fakeWhoop{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := map[string]any{"token_type": "Bearer", "expires_in": 3600}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "code-123" {
				http.Error(w, "bad exchange", http.StatusBadRequest)
				return
			}
			out["access_token"] = "at-1"
			out["refresh_token"] = "rt-1"
		case "refresh_token":
			out["access_token"] = "at-1"
		default:
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		writeJSON(w, out)
	})
	// NO revoke handler: the bundle declares no revocationEndpoint — WHOOP's
	// real revocation is an authenticated DELETE the host cannot speak, so
	// faking a form-POST revoke here would validate an invented manifest.
	// The basic profile the facility derives the connected email from — the
	// flat OIDC-style `{email}` shape AccountEmail accepts.
	mux.HandleFunc("/developer/v2/user/profile/basic", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		writeJSON(w, map[string]any{"user_id": 7, "email": "geo@whoop.example"})
	})
	mux.HandleFunc("/developer/v2/recovery", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		f.bump("recovery")
		if r.URL.Query().Get("nextToken") == "" {
			// Page one: a good cycle, and a malformed record (no cycle_id and
			// no sleep_id — nothing to key by) the sync must SKIP AND COUNT
			// while keeping the page and its token.
			writeJSON(w, map[string]any{
				"records": []any{
					map[string]any{
						"cycle_id": 1, "created_at": "2026-08-01T04:05:06Z",
						"score": map[string]any{
							"recovery_score": 87, "hrv_rmssd_milli": 62.5,
							"resting_heart_rate": 48,
						},
					},
					map[string]any{"score": map[string]any{"recovery_score": 12}},
				},
				"next_token": "r2",
			})
			return
		}
		// Page two: a SECOND cycle created on the SAME UTC date as page one —
		// cycle-keyed ids must land it as its own row, never overwrite day
		// one's (the date-keyed collision review-fleet codex #14 fixed).
		writeJSON(w, map[string]any{
			"records": []any{
				map[string]any{
					"cycle_id": 2, "created_at": "2026-08-01T21:45:00Z",
					"score": map[string]any{
						"recovery_score": 55, "hrv_rmssd_milli": 41,
						"resting_heart_rate": 55,
					},
				},
			},
			"next_token": "",
		})
	})
	mux.HandleFunc("/developer/v2/activity/sleep", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		f.bump("sleep")
		writeJSON(w, map[string]any{
			"records": []any{
				map[string]any{
					"id": "1a2b3c", "start": "2026-08-01T22:10:00Z",
					"end": "2026-08-02T06:40:00Z", "nap": false,
					"score": map[string]any{
						"sleep_performance_percentage": 91,
						"stage_summary": map[string]any{
							"total_light_sleep_time_milli":     14400000,
							"total_rem_sleep_time_milli":       7200000,
							"total_slow_wave_sleep_time_milli": 5400000,
							"total_awake_time_milli":           1800000,
						},
					},
				},
			},
			"next_token": "",
		})
	})
	mux.HandleFunc("/developer/v2/activity/workout", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		f.bump("workout")
		writeJSON(w, map[string]any{
			"records": []any{
				map[string]any{
					"id": "9f8e7d", "start": "2026-08-02T10:00:00Z",
					"end": "2026-08-02T11:00:00Z", "sport_name": "running",
					"score": map[string]any{"strain": 12.3, "kilojoule": 800},
				},
			},
			"next_token": "",
		})
	})
	f.serve(t, mux)
	return f
}

// whoopInstallRewired installs the closure with every prod-WHOOP reference
// pointed at the loopback fake: the bundle document's trusted oauth2
// endpoints (a static manifest cannot bake a dynamic httptest URL) and the
// sync body's API base constant, so the paged fetch drains against the same
// box.
func whoopInstallRewired(t *testing.T, ds substrate.Dataset, baseURL string) {
	t.Helper()
	install(t, ds, whoopDir, func(docs []map[string]any) {
		rewriteOAuthEndpoints(t, docs, baseURL, whoopProdBase)
		rewriteSource(t, docs, whoopProdBase, baseURL)
	})
}

func whoopPropFloat(t *testing.T, e *substrate.Record, name string) float64 {
	t.Helper()
	v, ok := e.Properties[name]
	if !ok {
		t.Fatalf("%s %s carries no %s: %v", e.Kind, e.ID, name, e.Properties)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("%s %s %s is a %T, want a number", e.Kind, e.ID, name, v)
	}
	return f
}

// TestWhoopBundleFakeSyncMirrors drives the whole integration against the
// loopback fake: install (endpoints + API base rewired), configure, connect
// over the host OAuth facility, then let the on-connect trigger drain the
// paged sync and assert the mirrors, the skip tally and the stamp.
func TestWhoopBundleFakeSyncMirrors(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeWhoop(t)
	svc, ds := newOAuthDataset(t, fake.ts.Client())
	whoopInstallRewired(t, ds, fake.ts.URL)
	// Only the on-connect trigger: the schedule would race catch-up fires into
	// the drain below and prove nothing this test is after.
	installTriggers(t, ds, whoopDir, "whoop-on-connect")

	// Configure the client record (the sole record resolves the input), then
	// add one pending account with every collection on.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: whoopConfigType,
		Properties: map[string]any{
			"clientId": "client-1", "clientSecret": "s3cret",
		},
	}); err != nil {
		t.Fatalf("create config: %v", err)
	}
	account, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: whoopAccountType,
		Properties: map[string]any{
			"enabledRecovery": true, "enabledSleep": true, "enabledWorkouts": true,
			"syncFrequency": "hourly", "backfillDepth": "last30d",
		},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// The host OAuth round trip against the fake: start derives the scope
	// union from the toggles, the callback exchanges the code, stores the
	// grant, derives the email off the basic profile and stamps the account.
	consent, err := ds.StartOAuth(ctx, substrate.ActorAPI, account.ID)
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}
	cu, err := url.Parse(consent)
	if err != nil {
		t.Fatalf("parse consent url: %v", err)
	}
	for _, scope := range []string{"offline", "read:profile", "read:recovery", "read:sleep", "read:workout"} {
		if !strings.Contains(cu.Query().Get("scope"), scope) {
			t.Fatalf("consent scope misses %s: %q", scope, cu.Query().Get("scope"))
		}
	}
	completeOAuth(t, svc, cu.Query().Get("state"), "code-123")
	connected, err := ds.Get(ctx, account.Kind, account.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if connected.Properties["tokenStatus"] != "connected" {
		t.Fatalf("account tokenStatus = %v", connected.Properties["tokenStatus"])
	}
	if connected.Properties["email"] != "geo@whoop.example" {
		t.Fatalf("derived email = %v", connected.Properties["email"])
	}

	// The on-connect trigger fires the sync; the paged chain drains recovery
	// (two pages over nextToken), then sleep, then workouts.
	drainTriggers(t, ds)

	// The recovery mirrors are CYCLE-KEYED: provider "whoop", the account, and
	// the cycle id, recomputed here through runner.ExternalID, so what the
	// python body composed is proven byte for byte. The date is derived
	// display data, never the identity.
	cycle1 := runner.ExternalID("whoop", account.ID, "recovery-1")
	rec1, err := ds.Get(ctx, whoopRecoveryType, cycle1)
	if err != nil {
		t.Fatalf("recovery cycle one did not sync: %v", err)
	}
	if rec1.Kind != whoopRecoveryType || rec1.Properties["cycleId"] != "1" {
		t.Fatalf("recovery cycle one wrong: type=%s cycleId=%v", rec1.Kind, rec1.Properties["cycleId"])
	}
	if rec1.Properties["date"] != "2026-08-01" {
		t.Fatalf("recovery cycle one derived date = %v, want 2026-08-01", rec1.Properties["date"])
	}
	if got := whoopPropFloat(t, rec1, "recoveryScore"); got != 87 {
		t.Fatalf("recoveryScore = %v, want 87", got)
	}
	if got := whoopPropFloat(t, rec1, "hrvMs"); got != 62.5 {
		t.Fatalf("hrvMs = %v, want 62.5", got)
	}
	// Cycle two arrived over the nextToken hop — the page-two proof — and it
	// was created on the SAME UTC date as cycle one: both rows land, the
	// second never overwrites the first (the date-keyed collision, fixed).
	cycle2 := runner.ExternalID("whoop", account.ID, "recovery-2")
	rec2, err := ds.Get(ctx, whoopRecoveryType, cycle2)
	if err != nil {
		t.Fatalf("recovery cycle two (page two, same UTC date) did not sync: %v", err)
	}
	if rec2.Properties["date"] != "2026-08-01" {
		t.Fatalf("recovery cycle two derived date = %v, want 2026-08-01 (same day as cycle one)", rec2.Properties["date"])
	}
	if got := whoopPropFloat(t, rec2, "recoveryScore"); got != 55 {
		t.Fatalf("cycle two recoveryScore = %v, want 55", got)
	}
	if pages := fake.count("recovery"); pages != 2 {
		t.Fatalf("recovery fetched %d pages, want 2 (a nextToken hop)", pages)
	}

	// The sleep mirror: provider-id keyed, stage durations rolled up.
	sleep, err := ds.Get(ctx, whoopSleepType, runner.ExternalID("whoop", account.ID, "sleep-1a2b3c"))
	if err != nil {
		t.Fatalf("sleep did not sync: %v", err)
	}
	if got := whoopPropFloat(t, sleep, "performance"); got != 91 {
		t.Fatalf("sleep performance = %v, want 91", got)
	}
	durations, _ := sleep.Properties["durations"].(map[string]any)
	if durations == nil {
		t.Fatalf("sleep carries no durations: %v", sleep.Properties)
	}
	if got, _ := durations["asleepMs"].(float64); got != 27000000 {
		t.Fatalf("asleepMs = %v, want 27000000 (light+rem+deep)", durations["asleepMs"])
	}

	// The workout mirror: sport, strain, and kilojoules converted to kcal.
	workout, err := ds.Get(ctx, whoopWorkoutType, runner.ExternalID("whoop", account.ID, "workout-9f8e7d"))
	if err != nil {
		t.Fatalf("workout did not sync: %v", err)
	}
	if workout.Properties["sport"] != "running" {
		t.Fatalf("workout sport = %v", workout.Properties["sport"])
	}
	if got := whoopPropFloat(t, workout, "strain"); got != 12.3 {
		t.Fatalf("workout strain = %v, want 12.3", got)
	}
	if got := whoopPropFloat(t, workout, "calories"); math.Abs(got-191.2048) > 1e-9 {
		t.Fatalf("workout calories = %v, want 800 kJ * 0.239006 = 191.2048", got)
	}

	// Per-record tolerance surfaced where the owner looks: the malformed
	// page-one record was skipped, counted, and its page kept — the final
	// stamp carries the tally, and lastSyncedAt marks the run complete (which
	// also drops the on-connect guard, so the stamp cannot re-fire it).
	stamped, err := ds.Get(ctx, account.Kind, account.ID)
	if err != nil {
		t.Fatalf("get stamped account: %v", err)
	}
	if stamped.Properties["syncStatus"] != "ok (1 skipped)" {
		t.Fatalf("syncStatus = %v, want \"ok (1 skipped)\"", stamped.Properties["syncStatus"])
	}
	if s, _ := stamped.Properties["lastSyncedAt"].(string); s == "" {
		t.Fatalf("lastSyncedAt not stamped: %v", stamped.Properties["lastSyncedAt"])
	}
}
