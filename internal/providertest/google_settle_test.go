package providertest

// The google bundle's sync bodies through the real dispatcher, where a
// record-sourced delivery is wrapped by the engine's `sync` trait stamps:
// `running` before the body runs, and a settle that turns a `running` it
// finds into `ok` and clears the error pair (decision 0085). A stream whose
// first sync failed keeps its on-connect guard open, so every owner edit and
// every reconnect delivers it again against the same failure (#817).

import (
	"context"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// googleFailing is a google closure on one repository whose API fails each
// stream one way: contacts with a 503 until contactsUp is set, drive with
// the 403 that means the grant lacks drive.readonly, and calendar with a 429
// that names a wall two minutes out.
type googleFailing struct {
	svc        substrate.Service
	ds         substrate.Dataset
	fake       fakeAPI
	contactsUp atomic.Bool
}

// The endpoint each stream's first call reaches, as fakeAPI counts it.
var googleFirstCall = map[string]string{
	"contacts": "connections", "drive": "startPageToken", "calendar": "calendarList",
}

func newGoogleFailing(t *testing.T) *googleFailing {
	t.Helper()
	g := &googleFailing{}
	refuse := func(w http.ResponseWriter, code int, body map[string]any) {
		w.WriteHeader(code)
		writeJSON(w, map[string]any{"error": body})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"token_type": "Bearer", "expires_in": 3600,
			"access_token": fakeToken, "refresh_token": "rt-1",
		})
	})
	mux.HandleFunc("/v1/people/me/connections", func(w http.ResponseWriter, r *http.Request) {
		g.fake.bump("connections")
		if !bearer(w, r) {
			return
		}
		if !g.contactsUp.Load() {
			refuse(w, http.StatusServiceUnavailable, map[string]any{
				"code": 503, "message": "The service is currently unavailable.",
			})
			return
		}
		writeJSON(w, map[string]any{"connections": []any{}, "nextSyncToken": "conn-1"})
	})
	mux.HandleFunc("/v1/people:listDirectoryPeople", func(w http.ResponseWriter, r *http.Request) {
		if bearer(w, r) {
			writeJSON(w, map[string]any{"people": []any{}, "nextSyncToken": "dir-1"})
		}
	})
	mux.HandleFunc("/v1/contactGroups", func(w http.ResponseWriter, r *http.Request) {
		if bearer(w, r) {
			writeJSON(w, map[string]any{"contactGroups": []any{}})
		}
	})
	mux.HandleFunc("/drive/v3/changes/startPageToken", func(w http.ResponseWriter, r *http.Request) {
		g.fake.bump("startPageToken")
		if !bearer(w, r) {
			return
		}
		refuse(w, http.StatusForbidden, map[string]any{
			"code":    403,
			"message": "Request had insufficient authentication scopes.",
			"status":  "PERMISSION_DENIED",
			"details": []any{map[string]any{"reason": "ACCESS_TOKEN_SCOPE_INSUFFICIENT"}},
		})
	})
	mux.HandleFunc("/calendar/v3/users/me/calendarList", func(w http.ResponseWriter, r *http.Request) {
		g.fake.bump("calendarList")
		if !bearer(w, r) {
			return
		}
		w.Header().Set("Retry-After", "120")
		refuse(w, http.StatusTooManyRequests, map[string]any{
			"code": 429, "status": "RESOURCE_EXHAUSTED", "message": "Quota exceeded.",
		})
	})
	g.fake.serve(t, mux)

	g.svc, g.ds = newCoreDataset(t, engine.WithOAuth("test-state-key", oauthCallback, g.fake.ts.Client()))
	install(t, g.ds, googleDir, func(docs []map[string]any) {
		rewriteOAuthEndpoints(t, docs, g.fake.ts.URL, "https://accounts.google.com",
			"https://oauth2.googleapis.com", "https://www.googleapis.com")
	})
	installTriggers(t, g.ds, googleDir,
		"google-contacts-on-connect", "google-drive-on-connect", "google-calendar-on-connect")
	mustPut(t, g.ds, substrate.PutInput{
		Kind: googlePackage + "/config", ID: "default",
		Properties: map[string]any{
			"clientId": "client-1", "clientSecret": "s3cret",
			"apiBase": g.fake.ts.URL,
		},
	})
	return g
}

// account creates one account with the named streams enabled. The create is
// delivered before any connect, while the guards still refuse it, so a
// connect is each stream's first delivery.
func (g *googleFailing) account(t *testing.T, id string, streams ...string) {
	t.Helper()
	props := map[string]any{}
	for _, s := range streams {
		props["enabled"+strings.ToUpper(s[:1])+s[1:]] = true
	}
	before := g.calls()
	mustPut(t, g.ds, substrate.PutInput{Kind: googleAccountType, ID: id, Properties: props})
	g.drain(t, before, nil)
}

// connect runs the OAuth facility's consent for one account, a first connect
// or a reconnect alike.
func (g *googleFailing) connect(t *testing.T, id string) {
	t.Helper()
	consent, err := g.ds.StartOAuth(context.Background(), substrate.ActorAPI,
		vocabulary.RecordPath(googleAccountType, id))
	if err != nil {
		t.Fatalf("start the consent for %s: %v", id, err)
	}
	completeOAuth(t, g.svc, oauthState(t, consent), "code-123")
}

// edit is an owner edit of one account, which an open on-connect guard
// delivers.
func (g *googleFailing) edit(t *testing.T, id string) {
	t.Helper()
	if _, err := g.ds.Patch(context.Background(), substrate.ActorAPI, googleAccountType, id, substrate.PatchInput{
		Properties: map[string]any{"syncFrequency": "hourly"},
	}); err != nil {
		t.Fatalf("edit %s: %v", id, err)
	}
}

func (g *googleFailing) calls() map[string]int {
	out := map[string]int{}
	for _, call := range googleFirstCall {
		out[call] = g.fake.count(call)
	}
	return out
}

// drain runs dispatcher passes until every record trigger's cursor stands at
// the changelog head, then requires each first call to have moved by exactly
// want's count since before: one delivery per stream per edit, connect or
// reconnect. A pass that delivered nothing is not proof of quiet on its own,
// and a call past want fails at once, because a guard that delivers the sync
// writes of another stream never goes quiet.
func (g *googleFailing) drain(t *testing.T, before, want map[string]int) {
	t.Helper()
	ctx := context.Background()
	for range 40 {
		n, err := g.ds.ProcessTriggers(ctx)
		if err != nil {
			t.Fatalf("process triggers: %v", err)
		}
		for call, base := range before {
			if got := g.fake.count(call); got > base+want[call] {
				t.Fatalf("%s calls = %d, want %d: a delivery fired another", call, got, base+want[call])
			}
		}
		if n > 0 || lagging(t, g.ds) {
			continue
		}
		for call, base := range before {
			if got := g.fake.count(call); got != base+want[call] {
				t.Fatalf("%s calls = %d, want %d", call, got, base+want[call])
			}
		}
		if n := parkedFailures(t, g.ds); n != 0 {
			t.Fatalf("parked deliveries = %d, want none", n)
		}
		return
	}
	t.Fatal("the triggers did not go quiet in forty passes")
}

// lagging reports whether a trigger's cursor stands short of the changelog
// head or a delivery is still in flight.
func lagging(t *testing.T, ds substrate.Dataset) bool {
	t.Helper()
	statuses, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	return slices.ContainsFunc(statuses, func(st substrate.TriggerStatus) bool {
		return st.Lag > 0 || st.InFlight > 0
	})
}

// nextSecond waits out the current second: the bodies stamp whole seconds,
// so a repeat inside the first failure's second could not show a moved
// syncErrorAt.
func nextSecond() {
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
}

// TestGoogleARepeatedFailureStaysErroring connects one account per failing
// stream. The first delivery states each failure; an owner edit then
// delivers each stream again, and the account must still read erroring (or
// throttled, behind the 429) with the error pair the first delivery wrote,
// and nothing else moved but the dispatcher's stamps and a later wall.
func TestGoogleARepeatedFailureStaysErroring(t *testing.T) {
	requirePython(t)
	t.Parallel()
	g := newGoogleFailing(t)
	streams := map[string]string{"acct-c": "contacts", "acct-d": "drive", "acct-k": "calendar"}
	states := map[string]string{
		"acct-c": substrate.SyncStateErroring, "acct-d": substrate.SyncStateErroring,
		"acct-k": substrate.SyncStateThrottled,
	}
	for id, stream := range streams {
		g.account(t, id, stream)
	}
	once := map[string]int{"connections": 1, "startPageToken": 1, "calendarList": 1}
	before := g.calls()
	for id := range streams {
		g.connect(t, id)
	}
	g.drain(t, before, once)

	first := map[string]*substrate.Record{}
	for id, stream := range streams {
		row := mustGet(t, g.ds, googleAccountType, id)
		assertFailing(t, row, stream, states[id])
		if row.Properties["syncError"] == nil || row.Properties["syncErrorAt"] == nil {
			t.Fatalf("%s after its first failure: syncError %v, syncErrorAt %v, want both written",
				id, row.Properties["syncError"], row.Properties["syncErrorAt"])
		}
		first[id] = row
	}
	if got := first["acct-d"].Properties["driveSyncState"]; got != "needsreconsent" {
		t.Fatalf("acct-d driveSyncState = %v, want needsreconsent", got)
	}
	if first["acct-k"].Properties["retryNotBefore"] == nil {
		t.Fatalf("acct-k after a 429: retryNotBefore unset, want the wall")
	}

	nextSecond()
	before = g.calls()
	for id := range streams {
		g.edit(t, id)
	}
	g.drain(t, before, once)

	// What moves on a repeat: the dispatcher's own stamps, a later wall
	// behind a 429, and the edit. The state is written over the dispatcher's
	// `running`, so it reads as the first failure left it.
	moves := []string{"lastSyncStartedAt", "lastSyncDurationMs", "retryNotBefore", "syncFrequency"}
	for id, stream := range streams {
		row := mustGet(t, g.ds, googleAccountType, id)
		if row.Properties["lastSyncStartedAt"] == first[id].Properties["lastSyncStartedAt"] {
			t.Fatalf("%s: lastSyncStartedAt did not move, so the edit delivered no %s run", id, stream)
		}
		assertFailing(t, row, stream, states[id])
		for _, name := range slices.Sorted(maps.Keys(row.Properties)) {
			if slices.Contains(moves, name) {
				continue
			}
			if !reflect.DeepEqual(row.Properties[name], first[id].Properties[name]) {
				t.Fatalf("%s %s = %v after a repeated failure, want %v as the first failure left it",
					id, name, row.Properties[name], first[id].Properties[name])
			}
		}
		for name := range first[id].Properties {
			if _, ok := row.Properties[name]; !ok {
				t.Fatalf("%s %s gone after a repeated failure, want %v", id, name, first[id].Properties[name])
			}
		}
	}
	wall, _ := mustGet(t, g.ds, googleAccountType, "acct-k").Properties["retryNotBefore"].(string)
	if was, _ := first["acct-k"].Properties["retryNotBefore"].(string); wall <= was {
		t.Fatalf("acct-k retryNotBefore = %q after a second 429, want later than %q", wall, was)
	}
}

// TestGoogleTwoFailingStreamsDeliverOnce connects one account whose contacts
// and drive both fail their first sync. Each stream's on-connect guard stays
// open, and each delivery's writes (the dispatcher's `running` and settle,
// the body's failure) are an update to the account; a guard that matched
// the other stream's writes delivered the two streams to each other without
// end.
func TestGoogleTwoFailingStreamsDeliverOnce(t *testing.T) {
	requirePython(t)
	t.Parallel()
	g := newGoogleFailing(t)
	g.account(t, "acct-2", "contacts", "drive")
	once := map[string]int{"connections": 1, "startPageToken": 1}

	before := g.calls()
	g.connect(t, "acct-2")
	g.drain(t, before, once)
	row := mustGet(t, g.ds, googleAccountType, "acct-2")
	assertFailing(t, row, "contacts", substrate.SyncStateErroring)
	assertFailing(t, row, "drive", substrate.SyncStateErroring)

	before = g.calls()
	g.edit(t, "acct-2")
	g.drain(t, before, once)
	assertFailing(t, mustGet(t, g.ds, googleAccountType, "acct-2"), "contacts", substrate.SyncStateErroring)
}

// TestGoogleAReconnectRestatesTheFailure reconnects an account whose contacts
// stream failed. The reconnect clears the error pair and leaves the stream's
// failure sentence, so the same failure on the delivery the reconnect fires
// is the one failure the account records, and its pair is written again.
// When the API then answers, the next delivery settles `ok` and clears it.
func TestGoogleAReconnectRestatesTheFailure(t *testing.T) {
	requirePython(t)
	t.Parallel()
	g := newGoogleFailing(t)
	g.account(t, "acct-c", "contacts")
	once := map[string]int{"connections": 1}
	before := g.calls()
	g.connect(t, "acct-c")
	g.drain(t, before, once)
	first := mustGet(t, g.ds, googleAccountType, "acct-c")
	assertFailing(t, first, "contacts", substrate.SyncStateErroring)

	nextSecond()
	before = g.calls()
	g.connect(t, "acct-c")
	g.drain(t, before, once)
	row := mustGet(t, g.ds, googleAccountType, "acct-c")
	assertFailing(t, row, "contacts", substrate.SyncStateErroring)
	if row.Properties["syncError"] != first.Properties["syncError"] {
		t.Fatalf("syncError = %v after the reconnect's failure, want %v", row.Properties["syncError"], first.Properties["syncError"])
	}
	at, _ := row.Properties["syncErrorAt"].(string)
	if was, _ := first.Properties["syncErrorAt"].(string); at <= was {
		t.Fatalf("syncErrorAt = %q after the reconnect's failure, want later than %q", at, was)
	}

	g.contactsUp.Store(true)
	before = g.calls()
	g.edit(t, "acct-c")
	g.drain(t, before, once)
	row = mustGet(t, g.ds, googleAccountType, "acct-c")
	if got := row.Properties["syncState"]; got != substrate.SyncStateOK {
		t.Fatalf("syncState = %v after the API answered, want %s (syncMessage %v)",
			got, substrate.SyncStateOK, row.Properties["syncMessage"])
	}
	if row.Properties["syncError"] != nil || row.Properties["syncErrorAt"] != nil {
		t.Fatalf("after a recovery: syncError %v, syncErrorAt %v, want both cleared",
			row.Properties["syncError"], row.Properties["syncErrorAt"])
	}
	if row.Properties["contactsLastSyncedAt"] == nil {
		t.Fatalf("contactsLastSyncedAt unset after a recovery")
	}
}

// assertFailing fails unless the account reads state and the stream's entry
// reads its own failure.
func assertFailing(t *testing.T, row *substrate.Record, stream, state string) {
	t.Helper()
	if got := row.Properties["syncState"]; got != state {
		t.Fatalf("%s syncState = %v, want %s", row.ID, got, state)
	}
	entries, _ := row.Properties["syncStreams"].(map[string]any)
	entry, _ := entries[stream].(map[string]any)
	if got := entry["state"]; got != state {
		t.Fatalf("%s syncStreams.%s = %v, want state %s", row.ID, stream, entry, state)
	}
}
