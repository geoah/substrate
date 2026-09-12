package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const personKind = "samples.substrate.reamde.dev/people/person"

// DELETE carries its version precondition as the `ifVersion` query parameter,
// spelled as `put` and `patch` spell it in their bodies. It reaches the dataset
// as DeleteInput.IfVersion; a stale one is the same `409 conflict` a stale
// patch is, and an absent one checks nothing.
func TestRESTDeleteIfVersion(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "p1", Kind: personKind, Version: 3})

	rec := env.do(t, http.MethodDelete, peoplePath+"/p1?ifVersion=2", tok, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if ds.lastDelete.IfVersion == nil || *ds.lastDelete.IfVersion != 2 {
		t.Fatalf("the dataset saw IfVersion %v, want 2", ds.lastDelete.IfVersion)
	}
	if ds.records["p1"].DeletedAt != nil {
		t.Fatal("a 409 delete tombstoned the record")
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/p1?ifVersion=3", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if decodeJSON[substrate.Record](t, rec).DeletedAt == nil {
		t.Fatal("the conditioned delete did not tombstone")
	}
	// The tombstone moved the version, so the same precondition retried is
	// the conflict the engine answers, not a second success.
	rec = env.do(t, http.MethodDelete, peoplePath+"/p1?ifVersion=3", tok, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)

	ds.put(&substrate.Record{ID: "p2", Kind: personKind, Version: 5})
	rec = env.do(t, http.MethodDelete, peoplePath+"/p2", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if ds.lastDelete.IfVersion != nil {
		t.Fatalf("an absent ifVersion reached the dataset as %d", *ds.lastDelete.IfVersion)
	}
}

// A precondition the server cannot read is refused by name, never dropped: a
// non-integer value and a miscased or unknown parameter are each a
// `400 bad_request`, and the dataset is not reached.
func TestRESTDeleteRefusesAMalformedPrecondition(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "p1", Kind: personKind, Version: 3})

	for _, query := range []string{"?ifVersion=three", "?ifVersion=", "?ifversion=3", "?version=3"} {
		ds.lastDeleteID = ""
		rec := env.do(t, http.MethodDelete, peoplePath+"/p1"+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if ds.lastDeleteID != "" {
			t.Fatalf("DELETE %s reached the dataset", query)
		}
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		key := strings.TrimPrefix(strings.SplitN(query, "=", 2)[0], "?")
		if !strings.Contains(msg, key) {
			t.Fatalf("DELETE %s said %q; it must name %q", query, msg, key)
		}
	}
}

// The merge body carries one precondition per participant, each optional, and
// the split body carries the recordmerge record's. Both are the engine's own
// inputs, decoded strictly, so a stale version is `409 conflict` and a
// misspelled key is `400 bad_request` naming it.
func TestRESTMergeSplitPreconditions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "a1", Kind: personKind, Version: 4})
	ds.put(&substrate.Record{ID: "b2", Kind: personKind, Version: 2})

	rec := env.do(t, http.MethodPost, "/api/v1/merge", tok, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2", "winnerVersion": 4, "loserVersion": 1,
	})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if ds.lastMerge.WinnerVersion == nil || *ds.lastMerge.WinnerVersion != 4 ||
		ds.lastMerge.LoserVersion == nil || *ds.lastMerge.LoserVersion != 1 {
		t.Fatalf("the dataset saw %+v", ds.lastMerge)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/merge", tok, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2", "loserVersion": 2,
	})
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastMerge.WinnerVersion != nil {
		t.Fatalf("an absent winnerVersion reached the dataset as %d", *ds.lastMerge.WinnerVersion)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/merge", tok, map[string]any{
		"kind": personKind, "winner": "a1", "loser": "b2", "winnerversion": 4,
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "winnerversion") {
		t.Fatalf("a miscased key said %q; it must name the key", msg)
	}

	ds.put(&substrate.Record{ID: "merge1", Kind: corePackage + "/recordmerge", Version: 1})
	rec = env.do(t, http.MethodPost, "/api/v1/split", tok, map[string]any{"merge": "merge1", "ifVersion": 2})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if ds.lastSplit.IfVersion == nil || *ds.lastSplit.IfVersion != 2 {
		t.Fatalf("the dataset saw %+v", ds.lastSplit)
	}
	rec = env.do(t, http.MethodPost, "/api/v1/split", tok, map[string]any{"merge": "merge1", "ifVersion": 1})
	wantStatus(t, rec, http.StatusCreated)
	rec = env.do(t, http.MethodPost, "/api/v1/split", tok, map[string]any{"merge": "merge1"})
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastSplit.IfVersion != nil {
		t.Fatalf("an absent ifVersion reached the dataset as %d", *ds.lastSplit.IfVersion)
	}
}
