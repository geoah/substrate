package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A record's URL is its stored reference value. The path after the version
// prefix is exactly what a `reference` property holds, which is the whole
// reason the collection segment is the kind's name (decision 0033).
func TestRecordURLIsItsReference(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))

	const kind = "samples.substrate.reamde.dev/people/person"
	const id = "r1"
	env.put(t, kind, id, map[string]any{"name": "Ada"})
	path := "/api/" + APIVersion + "/" + vocabulary.RecordPath(kind, id)
	rec := env.do(t, http.MethodGet, path, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != kind || got.ID != id {
		t.Fatalf("GET %s served %s/%s", path, got.Kind, got.ID)
	}
}

// The kinds the merge/split actions used to shadow are reachable:
// `recordmerge` is a shipped kind, and `POST /{core}/recordmerges` sat where
// its records do. The action moved to /api/v1/merge (#202), and the record a
// merge writes is listed and read like any other.
func TestShadowedKindsAreReachable(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	const kind = corePackage + "/recordmerge"
	env.put(t, personKind, "a1", map[string]any{"name": "Ada"})
	env.put(t, personKind, "b2", map[string]any{"name": "A. Lovelace"})
	merge, err := env.ds.Merge(context.Background(), substrate.ActorSystem, substrate.MergeInput{Kind: personKind, Winner: "a1", Loser: "b2"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	rec := env.do(t, http.MethodGet, recordsOf(t, kind), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 1 || page.Records[0].ID != merge.ID {
		t.Fatalf("the recordmerge list = %+v, want the merge's record", page.Records)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/"+kind+"/"+merge.ID, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != kind || got.ID != merge.ID {
		t.Fatalf("GET the merge served %s/%s", got.Kind, got.ID)
	}
}
