package providertest

// The Beeper provider's `messagessync` when Beeper Desktop cannot be reached
// (decision 0183): the account reads `throttled` with a "not reachable since"
// sentence and no `syncError`, the walk keeps its place, the first unreachable
// instant survives a second such run, and the first run that reaches Beeper
// again ends the stretch. A refusal Beeper itself answers still reads
// `erroring`.
//
// "Not reachable" here is a loopback port bound and closed before the run, so
// the connect is refused at once (ECONNREFUSED) rather than waiting out a
// connect timeout. The python cases in py/test_beeper_offline.py hold the
// other shapes of the failure (timeouts, no route, DNS, the sandbox gate).

import (
	"maps"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	beeperDir         = providersDir + "/beeper"
	beeperPackage     = "providers.substrate.reamde.dev/beeper"
	beeperAccountKind = beeperPackage + "/account"
	beeperChatsync    = beeperPackage + "/chatsync"
	beeperSyncFn      = beeperPackage + "/messagessync"
	beeperAccount     = "acct-beeper"
	beeperUnreachable = "Beeper Desktop is not reachable (since "
)

// fakeBeeper is a Beeper Desktop with an empty inbox: no chat accounts, no
// bridges, no labels and no chats, so a walk reaches `done` in a few steps.
// denied, when set, is the status `/v1/info` answers instead.
type fakeBeeper struct {
	fakeAPI
	denied int
}

func newFakeBeeper(t *testing.T) *fakeBeeper {
	t.Helper()
	f := &fakeBeeper{}
	f.serve(t, http.HandlerFunc(f.handle))
	return f
}

func (f *fakeBeeper) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if !bearer(w, r) {
		return
	}
	f.mu.Lock()
	denied := f.denied
	f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/info":
		if denied != 0 {
			w.WriteHeader(denied)
			writeJSON(w, map[string]any{"message": "invalid token", "code": "UNAUTHORIZED"})
			return
		}
		writeJSON(w, map[string]any{"app": map[string]any{"version": "4.0.0"}})
	case "/v1/accounts", "/v1/labels":
		writeJSON(w, []any{})
	case "/v1/bridges":
		writeJSON(w, map[string]any{"items": []any{}})
	case "/v1/chats":
		writeJSON(w, map[string]any{"items": []any{}, "hasMore": false})
	default:
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"message": "not found"})
	}
}

// closedBase is a loopback origin nothing listens on.
func closedBase(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return "http://" + addr
}

func newBeeper(t *testing.T) substrate.Dataset {
	t.Helper()
	requireUV(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, beeperDir, nil)
	mustPut(t, ds, substrate.PutInput{
		Kind: beeperAccountKind, ID: beeperAccount,
		Properties: beeperProps(nil),
	})
	return ds
}

// beeperProps is the account a sync is handed: messages on, the whole
// history on a cold walk. extra overrides any of it.
func beeperProps(extra map[string]any) map[string]any {
	props := syncProps(map[string]any{"backfillDepth": "all"}, "enabledMessages")
	maps.Copy(props, extra)
	return props
}

// beeperConfig is the injected config: the account, and the pasted token and
// the desktop's origin on the connector input.
func beeperConfig(apiBase string, props map[string]any) map[string]any {
	return map[string]any{
		"accounts": []any{map[string]any{
			"id": beeperAccount, "kind": beeperAccountKind, "properties": props,
		}},
		"inputs": map[string]any{"connector": map[string]any{
			"properties": map[string]any{"accessToken": fakeToken, "apiBase": apiBase},
		}},
	}
}

// beeperSync runs one record-triggered sync to completion, applying each page
// the way the dispatcher does, and returns the account's last stamp and every
// effect.
func beeperSync(t *testing.T, ds substrate.Dataset, apiBase string, props map[string]any) (map[string]any, []engine.StepEffect) {
	t.Helper()
	s := newStepper(t, ds, beeperSyncFn, beeperConfig(apiBase, props))
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": beeperAccountKind, "id": beeperAccount}})
	effects := s.drainApplying(nil)
	return accountStamp(t, effects, beeperAccountKind, beeperAccount), effects
}

// beeperStored is the account row's properties as the runs left them.
func beeperStored(t *testing.T, ds substrate.Dataset) map[string]any {
	t.Helper()
	return mustGet(t, ds, beeperAccountKind, beeperAccount).Properties
}

// assertPaused holds the shape an unreachable run stamps.
func assertPaused(t *testing.T, where string, p map[string]any) {
	t.Helper()
	if p["syncState"] != "throttled" {
		t.Fatalf("%s: syncState = %v, want throttled", where, p["syncState"])
	}
	msg, _ := p["syncMessage"].(string)
	if !strings.HasPrefix(msg, beeperUnreachable) {
		t.Fatalf("%s: syncMessage = %q, want it to open %q", where, msg, beeperUnreachable)
	}
	if p["syncStatus"] != msg {
		t.Fatalf("%s: syncStatus = %v, want the syncMessage %q", where, p["syncStatus"], msg)
	}
	for _, key := range []string{"syncError", "syncErrorAt", "retryNotBefore"} {
		if v, ok := p[key]; ok && v != nil {
			t.Fatalf("%s: %s = %v, want none on a pause", where, key, v)
		}
	}
	if s, _ := p["unreachableSince"].(string); s == "" {
		t.Fatalf("%s: unreachableSince = %v, want the first unreachable attempt", where, p["unreachableSince"])
	}
	if s, _ := p["lastSyncedAt"].(string); s == "" {
		t.Fatalf("%s: lastSyncedAt = %v, want the attempt stamped", where, p["lastSyncedAt"])
	}
	streams, _ := p["syncStreams"].(map[string]any)
	for _, name := range []string{"chats", "messages"} {
		entry, _ := streams[name].(map[string]any)
		if entry["state"] != "throttled" {
			t.Fatalf("%s: syncStreams.%s = %v, want throttled", where, name, entry)
		}
	}
}

func TestBeeperUnreachableDesktopPauses(t *testing.T) {
	ds := newBeeper(t)
	gone := closedBase(t)

	stamp, effects := beeperSync(t, ds, gone, beeperProps(nil))
	assertPaused(t, "the stamp", stamp)
	cur, _ := stamp["streamCursors"].(map[string]any)
	if cur["phase"] != "info" {
		t.Fatalf("streamCursors.phase = %v, want info: the walk keeps its place", cur["phase"])
	}
	for _, ef := range effects {
		if ef.Kind == beeperChatsync {
			t.Fatalf("an unreachable run wrote a chatsync row: %+v", ef)
		}
	}
	stored := beeperStored(t, ds)
	assertPaused(t, "the stored row", stored)

	// A second unreachable run keeps the first instant, and the sentence with it.
	again, _ := beeperSync(t, ds, gone, stored)
	assertPaused(t, "the second stamp", again)
	if again["unreachableSince"] != stored["unreachableSince"] {
		t.Fatalf("unreachableSince moved: %v, then %v", stored["unreachableSince"], again["unreachableSince"])
	}
	if again["syncMessage"] != stored["syncMessage"] {
		t.Fatalf("syncMessage moved: %q, then %q", stored["syncMessage"], again["syncMessage"])
	}
}

func TestBeeperUnreachableMidWalkKeepsItsPlace(t *testing.T) {
	ds := newBeeper(t)
	const chat = "!c1:beeper.local"
	props := beeperProps(nil)
	props["lastSyncedAt"] = ago(0)
	props["streamCursors"] = map[string]any{
		"phase": "whole", "queue": []any{chat}, "msgq": []any{chat}, "cold": false,
	}

	stamp, effects := beeperSync(t, ds, closedBase(t), props)
	assertPaused(t, "the stamp", stamp)
	cur, _ := stamp["streamCursors"].(map[string]any)
	if cur["phase"] != "whole" {
		t.Fatalf("streamCursors.phase = %v, want whole", cur["phase"])
	}
	if q, _ := cur["queue"].([]any); len(q) != 1 || q[0] != chat {
		t.Fatalf("streamCursors.queue = %v, want [%s] kept", cur["queue"], chat)
	}
	if f, _ := cur["failed"].([]any); len(f) != 0 {
		t.Fatalf("streamCursors.failed = %v, want the chat not filed as failed", f)
	}
	for _, ef := range effects {
		if ef.Kind == beeperChatsync {
			if _, ok := ef.Properties["messagesStatus"]; ok {
				t.Fatalf("an unreachable desktop was filed as the chat's failure: %+v", ef)
			}
		}
	}
}

func TestBeeperDesktopBackClearsTheStretch(t *testing.T) {
	ds := newBeeper(t)
	beeperSync(t, ds, closedBase(t), beeperProps(nil))
	stored := beeperStored(t, ds)
	assertPaused(t, "the unreachable run", stored)

	fake := newFakeBeeper(t)
	stamp, _ := beeperSync(t, ds, fake.ts.URL, stored)
	if stamp["syncState"] != "ok" {
		t.Fatalf("syncState = %v (%v), want ok once the desktop answers", stamp["syncState"], stamp["syncMessage"])
	}
	if v, ok := stamp["unreachableSince"]; !ok || v != nil {
		t.Fatalf("unreachableSince = %v (present %v), want a null that deletes it", v, ok)
	}
	if v, ok := beeperStored(t, ds)["unreachableSince"]; ok {
		t.Fatalf("the stored row still holds unreachableSince = %v", v)
	}
}

func TestBeeperDesktopErrorStillErrors(t *testing.T) {
	ds := newBeeper(t)
	beeperSync(t, ds, closedBase(t), beeperProps(nil))
	stored := beeperStored(t, ds)

	fake := newFakeBeeper(t)
	fake.denied = http.StatusUnauthorized
	stamp, _ := beeperSync(t, ds, fake.ts.URL, stored)
	if stamp["syncState"] != "erroring" {
		t.Fatalf("syncState = %v, want erroring for a 401 Beeper answered", stamp["syncState"])
	}
	if e, _ := stamp["syncError"].(string); !strings.Contains(e, "401") {
		t.Fatalf("syncError = %v, want the 401", stamp["syncError"])
	}
	if v, ok := stamp["unreachableSince"]; !ok || v != nil {
		t.Fatalf("unreachableSince = %v (present %v), want it cleared: Beeper answered", v, ok)
	}
	if v, ok := beeperStored(t, ds)["unreachableSince"]; ok {
		t.Fatalf("the stored row still holds unreachableSince = %v", v)
	}
}
