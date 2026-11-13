package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A reference is written like every other property: one put carries the
// pointer, and a second write drops it by writing null. There is no link verb
// and no unlink verb, so a put is the whole mutation surface for a pointer.
func TestRESTReferenceIsWrittenAsAProperty(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	member, _ := recordsStaff(t, env)
	env.put(t, member, "p1", map[string]any{"name": "Sam"})
	path := "/api/v1/" + vocabulary.RecordPath(member, "p1")

	rec := env.do(t, http.MethodPut, path, env.token, map[string]any{
		"properties": map[string]any{"name": "Sam", "manager": personKind + "/p2"},
	})
	wantStatus(t, rec, http.StatusOK)
	e := decodeJSON[substrate.Record](t, rec)
	// The put sent the bare path (write-time shorthand) and the read serves
	// the object it normalized to (0044).
	manager, _ := e.Properties["manager"].(map[string]any)
	if manager["ref"] != personKind+"/p2" {
		t.Fatalf("after the put, properties = %+v", e.Properties)
	}

	rec = env.do(t, http.MethodPatch, path, env.token, map[string]any{
		"properties": map[string]any{"manager": nil},
	})
	wantStatus(t, rec, http.StatusOK)
	if got := env.get(t, member, "p1"); got == nil || got.Properties["manager"] != nil || got.Properties["name"] != "Sam" {
		t.Fatalf("the null must drop the pointer and nothing else: %+v", got)
	}
}

// A patch addressed to a tombstone answers 404 and writes nothing; a put
// at the same path is what restores the record.
func TestRESTPatchOnATombstoneIsNotFound(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "p1", map[string]any{"name": "Sam"})
	gone, err := env.ds.Delete(context.Background(), substrate.ActorSystem, personKind, "p1", substrate.DeleteInput{})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	rec := env.do(t, http.MethodPatch, peopleV1+"/p1", env.token, map[string]any{
		"properties": map[string]any{"name": "Renamed"},
	})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if e := env.get(t, personKind, "p1"); e == nil || e.Version != gone.Version || e.Properties["name"] != "Sam" || e.DeletedAt == nil {
		t.Fatalf("the refused patch changed the tombstone: %+v", e)
	}
}

// `edges` no longer names a sub-resource, so it is no longer a reserved record
// id: a record may be called `edges` and read back and written at its own
// path. So is `incoming`: the reverse read is a filter arm of /records, not a
// sub-resource, so no record id is reserved for it. A create through the API
// at either word is the record route's own refusal of an id the substrate
// did not mint (decision 0228), never a sub-resource's answer.
func TestRESTSubResourceWordsAreOrdinaryRecordIDs(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))

	for _, id := range []string{"edges", "incoming"} {
		rec := env.do(t, http.MethodPut, peopleV1+"/"+id, env.token, map[string]any{
			"properties": map[string]any{"name": "Sam"},
		})
		wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
		wantMessage(t, rec, `"`+id+`" is not a minted id`)

		env.put(t, personKind, id, map[string]any{"name": "Sam"})
		rec = env.do(t, http.MethodPut, peopleV1+"/"+id, env.token, map[string]any{
			"properties": map[string]any{"name": "Sam " + id},
		})
		wantStatus(t, rec, http.StatusOK)
		rec = env.do(t, http.MethodGet, peopleV1+"/"+id, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if e := decodeJSON[substrate.Record](t, rec); e.ID != id || e.Properties["name"] != "Sam "+id {
			t.Fatalf("record = %+v", e)
		}
	}
}

// TestTriggerVerbsLiveUnderCore is ruling A8's verb placement: the trigger
// verbs answer AT the resource, and trigger records are CORE's. A fresh
// repository holds no trigger, so the status read answers an empty list,
// which is the trigger handler answering (an unknown path would be 404).
func TestTriggerVerbsLiveUnderCore(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)

	rec := env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/trigger/status", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); body != "{\"items\":[]}\n" {
		t.Fatalf("body = %s, want an empty items list", body)
	}
	if w := rec.Header().Get("Warning"); w != "" {
		t.Fatalf("the resource path must not carry a deprecation Warning: %q", w)
	}

	// The folded-away authority (automation.substrate.reamde.dev, folded into
	// core 2026-08-12) is not a route: it is the router's 404, never a second
	// spelling of the verbs.
	rec = env.do(t, http.MethodGet, "/api/v1/automation.substrate.reamde.dev/triggers/status", env.token, nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// TestSyncStatusLivesAtTheRoot: the synchronization read spans every kind
// binding the `sync` trait, so it sits at the version root beside /records
// and /catalog, and an empty repository answers the empty list envelope.
func TestSyncStatusLivesAtTheRoot(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)

	rec := env.do(t, http.MethodGet, "/api/v1/sync/status", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); body != "{\"items\":[]}\n" {
		t.Fatalf("body = %s, want an empty items list", body)
	}
	// No bearer, no answer: the read is the repository's own.
	rec = env.do(t, http.MethodGet, "/api/v1/sync/status", "", nil)
	wantStatus(t, rec, http.StatusUnauthorized)
}

// verbsHubFailingAfter is how long every delivery of a hub trigger must have
// parked before it reads failing; verbsHub's tests advance the health clock
// past it rather than wait.
const verbsHubFailingAfter = 10 * time.Minute

// verbsHub installs, under the repository's own authority, a provider-shaped
// package: an `account` kind binding core's `sync`, a plain `repo` kind, a
// Python `pull` that fails on a record named "bad" as GitHub refusing a
// token would and succeeds otherwise, and the triggers given. It answers the
// two kind references and the trigger whose source is each kind's.
func verbsHub(t *testing.T, env *dbEnv, triggers map[string]string) (account, repo string) {
	t.Helper()
	pkg := env.repository + "/hub"
	account, repo = pkg+"/account", pkg+"/repo"
	connector := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "connector", "fts": false}
	}
	owner := func(typ string) map[string]any {
		return map[string]any{"type": typ, "writer": "owner", "fts": false}
	}
	m := enginetest.Manifest{
		Name: "hub", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "account"}, map[string]any{
				"traits":          []any{vocabulary.TraitSyncCore},
				"displayTemplate": "{name}",
				"properties": map[string]any{
					"name":               map[string]any{"type": "string"},
					"syncState":          connector("string"),
					"syncMessage":        connector("string"),
					"lastSyncedAt":       connector("datetime"),
					"lastSyncStartedAt":  connector("datetime"),
					"lastSyncDurationMs": connector("int"),
					"syncRequestedAt":    owner("datetime"),
					"syncRequestedAck":   connector("datetime"),
					"syncPaused":         owner("bool"),
					"syncProgress":       connector("json"),
					"syncError":          connector("string"),
					"syncErrorAt":        connector("datetime"),
					"syncStreams":        connector("json"),
					"tokenStatus":        map[string]any{"type": "string", "fts": false},
					"tokenError":         map[string]any{"type": "string", "fts": false},
					"tokenErrorAt":       map[string]any{"type": "datetime"},
				},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "repo"}, map[string]any{
				"displayTemplate": "{name}",
				"properties":      map[string]any{"name": map[string]any{"type": "string"}},
			}),
			vocabulary.FunctionManifest(pkg, "pull", map[string]any{
				"description": "pulls one account, refused for the one named bad",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{account}},
				"source": "def main(input, host):\n" +
					"    record = input[\"envelope\"].get(\"record\") or {}\n" +
					"    if (record.get(\"properties\") or {}).get(\"name\") == \"bad\":\n" +
					"        raise RuntimeError(\"GitHub answered 401\")\n" +
					"    return {}\n",
			}),
		},
	}
	for id, kind := range triggers {
		m.Triggers = append(m.Triggers, enginetest.Trigger{ID: id, Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{pkg + "/" + kind}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/pull"),
		}})
	}
	if err := enginetest.Install(context.Background(), env.ds, substrate.ActorSystem, m); err != nil {
		t.Fatalf("install the hub: %v", err)
	}
	return account, repo
}

// verbsProcess runs one dispatcher pass.
func verbsProcess(t *testing.T, env *dbEnv) {
	t.Helper()
	if _, err := env.ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("process triggers: %v", err)
	}
}

// TestStatusReadsCarryTheLatestParkedReason: both status reads put the
// newest parked delivery's reason and instant beside the parked count, so a
// list says why runs failed without reading each trigger's `…/parked`. An
// account delivered once, then refused until the trigger reads failing.
func TestStatusReadsCarryTheLatestParkedReason(t *testing.T) {
	t.Parallel()
	clock := &engine.TestClock{}
	env := newDBEnv(t, withEngine(engine.WithHealthFailingAfter(verbsHubFailingAfter), engine.WithTestHealthClock(clock.Now)))
	account, _ := verbsHub(t, env, map[string]string{"github-pull": "account"})
	env.put(t, account, "geoah", map[string]any{"name": "good"})
	verbsProcess(t, env)
	env.put(t, account, "geoah", map[string]any{"name": "bad"})
	verbsProcess(t, env)
	clock.Advance(verbsHubFailingAfter + 2*time.Minute)
	verbsProcess(t, env)

	var failingSince []string
	for _, path := range []string{"/api/v1/sync/status", "/api/v1/substrate.reamde.dev/core/trigger/status"} {
		rec := env.do(t, http.MethodGet, path, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		var body struct {
			Items []struct {
				ID              string `json:"id"`
				Parked          int64  `json:"parked"`
				LastParkedError string `json:"lastParkedError"`
				LastParkedAt    string `json:"lastParkedAt"`
				Health          string `json:"health"`
				FailingSince    string `json:"failingSince"`
				LastOkAt        string `json:"lastOkAt"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		if len(body.Items) != 1 {
			t.Fatalf("%s: items = %s, want one", path, rec.Body.String())
		}
		got := body.Items[0]
		if got.Parked != 1 || !strings.Contains(got.LastParkedError, "GitHub answered 401") ||
			strings.Contains(got.LastParkedError, "\n") || got.LastParkedAt == "" {
			t.Fatalf("%s: item = %+v, want the count, the reason on one line and when", path, got)
		}
		parked, err := time.Parse(time.RFC3339Nano, got.LastParkedAt)
		if err != nil {
			t.Fatalf("%s: lastParkedAt %q: %v", path, got.LastParkedAt, err)
		}
		lastOK, err := time.Parse(time.RFC3339Nano, got.LastOkAt)
		if err != nil {
			t.Fatalf("%s: lastOkAt %q: %v", path, got.LastOkAt, err)
		}
		if got.Health != substrate.HealthFailing || got.FailingSince == "" || !lastOK.Before(parked) {
			t.Fatalf("%s: item = %+v, want failing, and the ok run before the park", path, got)
		}
		failingSince = append(failingSince, got.FailingSince)
	}
	if failingSince[0] != failingSince[1] {
		t.Fatalf("the account reads failing since %s, its trigger since %s", failingSince[0], failingSince[1])
	}
}

// TestSyncStatusCarriesTheSignIn: a sync status row carries the account's
// OAuth sign-in, so a refused grant reads as one on the status alone; a row
// with no sign-in leaves all three keys out.
func TestSyncStatusCarriesTheSignIn(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	account, _ := verbsHub(t, env, nil)
	env.put(t, account, "geoah", map[string]any{
		"name": "geoah", "tokenStatus": "erroring",
		"tokenError": `provider answered 400, error code "invalid_grant"`, "tokenErrorAt": "2026-10-08T09:00:00Z",
	})
	env.put(t, account, "work", map[string]any{"name": "work"})

	rec := env.do(t, http.MethodGet, "/api/v1/sync/status", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 || body.Items[0]["id"] != "geoah" {
		t.Fatalf("items = %s, want two, geoah first", rec.Body.String())
	}
	refused := body.Items[0]
	if refused["tokenStatus"] != "erroring" || refused["tokenError"] != `provider answered 400, error code "invalid_grant"` || refused["tokenErrorAt"] != "2026-10-08T09:00:00Z" {
		t.Fatalf("refused row = %v, want the sign-in's status, reason and when", refused)
	}
	for _, key := range []string{"tokenStatus", "tokenError", "tokenErrorAt"} {
		if v, ok := body.Items[1][key]; ok {
			t.Fatalf("a row with no sign-in carries %s = %v", key, v)
		}
	}
}

// TestStatusReadsCarryHealth: a healthy row says `ok` and leaves
// failingSince out, and a row that never delivered leaves lastOkAt out too.
func TestStatusReadsCarryHealth(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	_, repo := verbsHub(t, env, map[string]string{"delivered": "repo", "never": "account"})
	env.put(t, repo, "substrate", map[string]any{"name": "substrate"})
	verbsProcess(t, env)

	rec := env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/trigger/status", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %s, want two", rec.Body.String())
	}
	byID := map[string]map[string]any{}
	for _, item := range body.Items {
		if item["health"] != substrate.HealthOK {
			t.Fatalf("%v: health = %v, want ok", item["id"], item["health"])
		}
		if _, ok := item["failingSince"]; ok {
			t.Fatalf("%v: a healthy row carries failingSince %v", item["id"], item["failingSince"])
		}
		byID[item["id"].(string)] = item
	}
	if at, _ := byID["delivered"]["lastOkAt"].(string); at == "" {
		t.Fatalf("delivered: lastOkAt = %v", byID["delivered"]["lastOkAt"])
	}
	if _, ok := byID["never"]["lastOkAt"]; ok {
		t.Fatalf("never: a trigger that never delivered carries lastOkAt %v", byID["never"]["lastOkAt"])
	}
}
