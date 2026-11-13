package api

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// DELETE carries its version precondition as the `ifVersion` query parameter,
// spelled as `put` and `patch` spell it in their bodies. A stale one is the
// same `409 conflict` a stale patch is and leaves the record, and an absent
// one checks nothing.
func TestRESTDeleteIfVersion(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	v := env.put(t, personKind, "p1", map[string]any{"name": "Ada Lovelace"}).Version
	stale := strconv.FormatInt(v-1, 10)
	current := strconv.FormatInt(v, 10)

	rec := env.do(t, http.MethodDelete, peoplePath+"/p1?ifVersion="+stale, env.token, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if got := env.get(t, personKind, "p1"); got == nil || got.DeletedAt != nil {
		t.Fatalf("a 409 delete changed the record: %+v", got)
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/p1?ifVersion="+current, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if decodeJSON[substrate.Record](t, rec).DeletedAt == nil {
		t.Fatal("the conditioned delete did not tombstone")
	}
	// The tombstone moved the version, so the same precondition retried is
	// the conflict the engine answers, not a second success.
	rec = env.do(t, http.MethodDelete, peoplePath+"/p1?ifVersion="+current, env.token, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)

	// A record two writes along, deleted with no precondition: nothing is
	// compared, so it goes.
	env.put(t, personKind, "p2", map[string]any{"name": "Grace"})
	env.put(t, personKind, "p2", map[string]any{"name": "Grace Hopper"})
	rec = env.do(t, http.MethodDelete, peoplePath+"/p2", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if decodeJSON[substrate.Record](t, rec).DeletedAt == nil {
		t.Fatal("the unconditioned delete did not tombstone")
	}
}

// DELETE ?purge=true collects the record at once, a record a finalizer holds
// answers `409 conflict` and stays, and purge=false is an ordinary delete
// (#585).
func TestRESTDeletePurge(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	held, err := env.ds.Put(t.Context(), substrate.ActorSystem, substrate.PutInput{
		Kind: personKind, ID: "p2", Properties: map[string]any{"name": "Grace"},
	})
	if err != nil {
		t.Fatalf("put p2: %v", err)
	}
	if _, err := env.ds.Patch(t.Context(), substrate.ActorSystem, personKind, "p2", substrate.PatchInput{
		AddFinalizers: []string{"example.com/hold"},
	}); err != nil {
		t.Fatalf("hold p2: %v", err)
	}

	rec := env.do(t, http.MethodDelete, peoplePath+"/p1?purge=true", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := env.get(t, personKind, "p1"); got != nil {
		t.Fatalf("the purged record is still stored: %+v", got)
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/p2?purge=1", env.token, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if got := env.get(t, personKind, "p2"); got == nil || got.DeletedAt != nil || got.Version == held.Version {
		t.Fatalf("a refused purge changed the held record, or the hold did not land: %+v", got)
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/p2?purge=false", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := env.get(t, personKind, "p2"); got == nil || got.DeletedAt == nil {
		t.Fatalf("purge=false is a delete that tombstones and keeps the row: %+v", got)
	}
}

// The merge body carries one precondition per participant, each optional, and
// the split body carries the recordmerge record's. A stale version is `409
// conflict` and changes nothing; an absent one checks nothing.
func TestRESTMergeSplitPreconditions(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "a1", map[string]any{"name": "Ada"})
	winner := env.put(t, personKind, "a1", map[string]any{"name": "Ada Lovelace"}).Version
	loser := env.put(t, personKind, "b2", map[string]any{"name": "A. Lovelace"}).Version

	rec := env.do(t, http.MethodPost, "/api/v1/merge", env.token, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2", "winnerVersion": winner, "loserVersion": loser + 1,
	})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if got := env.get(t, personKind, "b2"); got == nil || got.DeletedAt != nil {
		t.Fatalf("a 409 merge took the loser: %+v", got)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/merge", env.token, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2", "loserVersion": loser,
	})
	wantStatus(t, rec, http.StatusCreated)
	merge := decodeJSON[substrate.Record](t, rec)
	if merge.Kind != corePackage+"/recordmerge" {
		t.Fatalf("the merge answered a %s, want the recordmerge record", merge.Kind)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/split", env.token, map[string]any{"merge": merge.ID, "ifVersion": merge.Version + 1})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	rec = env.do(t, http.MethodPost, "/api/v1/split", env.token, map[string]any{"merge": merge.ID, "ifVersion": merge.Version})
	wantStatus(t, rec, http.StatusCreated)
	if got := env.get(t, personKind, "b2"); got == nil || got.DeletedAt != nil {
		t.Fatalf("the split did not bring the loser back: %+v", got)
	}

	// A second merge, split with no precondition: nothing is compared.
	rec = env.do(t, http.MethodPost, "/api/v1/merge", env.token, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2",
	})
	wantStatus(t, rec, http.StatusCreated)
	merge = decodeJSON[substrate.Record](t, rec)
	rec = env.do(t, http.MethodPost, "/api/v1/split", env.token, map[string]any{"merge": merge.ID})
	wantStatus(t, rec, http.StatusCreated)
}
