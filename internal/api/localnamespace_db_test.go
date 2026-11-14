package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// localNotes declares a package of the repository's own under its FULL name,
// as an owner who never heard of `~` would: a note kind whose `related`
// points at another note.
func localNotes(t *testing.T, env *dbEnv) {
	t.Helper()
	name := env.repository
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", env.token, map[string]any{"documents": []any{
		map[string]any{
			"kind": corePackage + "/package", "metadata": map[string]any{"id": name + "/notes"},
			"data": map[string]any{"authority": name, "package": "notes"},
		},
		map[string]any{
			"kind": corePackage + "/kind", "metadata": map[string]any{"id": name + "/notes/note"},
			"data": map[string]any{
				"authority": name, "package": "notes", "names": map[string]any{"singular": "note"},
				"properties": map[string]any{
					"text":    map[string]any{"type": "string"},
					"related": map[string]any{"type": "reference", "kind": name + "/notes/note"},
				},
			},
		},
	}})
	wantStatus(t, rec, http.StatusOK)
}

// A repository's own kinds are `~/…` in storage and on the wire, and its full
// name is another spelling of `~` on every address and in every value it
// sends (decision record 0259): a declaration made under the name lands under
// `~`, a record path, a record's reference and a list's filter answer in
// either spelling, and every reply says `~`.
func TestOwnKindsAnswerUnderTheLocalNamespaceInEitherSpelling(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	name := env.repository
	localNotes(t, env)

	for _, id := range []string{"~/notes/note", name + "/notes/note"} {
		rec := env.do(t, http.MethodGet, "/api/v1/"+corePackage+"/kind/"+url.PathEscape(id), env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.Record](t, rec); got.ID != "~/notes/note" || got.Properties["authority"] != "~" {
			t.Fatalf("the declaration read as %s is %s with authority %v, want ~/notes/note under ~", id, got.ID, got.Properties["authority"])
		}
	}

	first, second := mintedID("first"), mintedID("second")
	rec := env.do(t, http.MethodPut, "/api/v1/"+name+"/notes/note/"+first, env.token,
		map[string]any{"properties": map[string]any{"text": "written under the name"}})
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != "~/notes/note" {
		t.Fatalf("a put under the full name answered kind %q, want ~/notes/note", got.Kind)
	}
	rec = env.do(t, http.MethodPut, "/api/v1/~/notes/note/"+second, env.token,
		map[string]any{"properties": map[string]any{"text": "written under ~", "related": map[string]any{"ref": name + "/notes/note/" + first}}})
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeJSON[substrate.Record](t, rec); got.Kind != "~/notes/note" || got.Properties["related"].(map[string]any)["ref"] != "~/notes/note/"+first {
		t.Fatalf("a reference written under the full name is stored as %v, want ~/notes/note/%s", got.Properties["related"], first)
	}

	for _, path := range []string{"/api/v1/~/notes/note/" + first, "/api/v1/" + name + "/notes/note/" + first} {
		rec := env.do(t, http.MethodGet, path, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.Record](t, rec); got.Kind != "~/notes/note" || got.ID != first {
			t.Fatalf("GET %s answered %s/%s, want ~/notes/note/%s", path, got.Kind, got.ID, first)
		}
	}
	for _, kind := range []string{"~/notes/note", name + "/notes/note"} {
		rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["`+kind+`"]}`), env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[substrate.Page](t, rec)
		if len(page.Records) != 2 {
			t.Fatalf("the list filtered on %s holds %d records, want both", kind, len(page.Records))
		}
		for _, r := range page.Records {
			if r.Kind != "~/notes/note" || strings.Contains(r.Kind, name) {
				t.Fatalf("the list filtered on %s answered kind %q", kind, r.Kind)
			}
		}
	}
}

// Two repositories that each add the tasks sample hold one kind, `~/tasks/task`
// from the same origin, and neither's name is in it (decision record 0259).
func TestTwoRepositoriesCopiesOfOneSampleAreOneKind(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	other := env.repository + "2.example.com"
	if _, err := env.svc.CreateRepository(t.Context(), other); err != nil {
		t.Fatalf("create the second repository: %v", err)
	}
	otherDS, err := env.svc.Dataset(t.Context(), other)
	if err != nil {
		t.Fatalf("open the second repository: %v", err)
	}
	_, otherToken, err := otherDS.MintToken(t.Context(), substrate.MintTokenInput{Label: "test"})
	if err != nil {
		t.Fatalf("mint into the second repository: %v", err)
	}
	for _, tok := range []string{env.token, otherToken} {
		for _, id := range []string{peopleBundleID, "samples.substrate.reamde.dev/scheduling", "samples.substrate.reamde.dev/tasks"} {
			rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/add", tok, nil)
			wantStatus(t, rec, http.StatusOK)
		}
		rec := env.do(t, http.MethodGet, "/api/v1/"+corePackage+"/kind/"+url.PathEscape("~/tasks/task"), tok, nil)
		wantStatus(t, rec, http.StatusOK)
		kind := decodeJSON[substrate.Record](t, rec)
		if kind.ID != "~/tasks/task" {
			t.Fatalf("the tasks kind is %q, want ~/tasks/task", kind.ID)
		}
		rec = env.do(t, http.MethodGet, "/api/v1/"+corePackage+"/package/"+url.PathEscape("~/tasks"), tok, nil)
		wantStatus(t, rec, http.StatusOK)
		if pkg := decodeJSON[substrate.Record](t, rec); pkg.Properties["origin"] != "samples.substrate.reamde.dev/tasks" {
			t.Fatalf("the copy's origin is %v, want the shipped sample", pkg.Properties["origin"])
		}
	}
}
