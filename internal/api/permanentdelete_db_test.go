package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An app's permanent delete of a record of a kind its scopes do not let it
// write is refused before the engine reads or writes anything: a record
// that is there and an id that is not answer alike, the record, its
// history and the change request that targets it stay, and nothing is
// appended. A kind its scopes cover it deletes for good.
func TestAnAppsPermanentDeleteOfAKindItsScopesDoNotCoverIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	pkg := env.repository + "/details"
	kind := func(name string) map[string]any {
		return vocabulary.KindManifest(pkg, map[string]any{"singular": name}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		})
	}
	if _, err := env.ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0), kind("note"), kind("memo"),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	note := env.put(t, pkg+"/note", "n1", map[string]any{"name": "kept"})
	request := env.put(t, vocabulary.KindRecordPatchRequest, "", map[string]any{
		"diff": map[string]any{"properties": map[string]any{"name": "asked"}}, "target": vocabulary.RecordPath(note.Kind, note.ID),
	})
	memo := env.put(t, pkg+"/memo", "m1", map[string]any{"name": "gone"})
	app := appScopedToken(t, env, "read:"+pkg+"/*", "write:"+pkg+"/memo")
	head, err := env.ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{note.ID, "absent"} {
		rec := appScopedDo(t, env, http.MethodDelete, recordPath(note.Kind, id)+"?purge=true", app, nil, "")
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	if after, err := env.ds.Head(ctx); err != nil || after.Seq != head.Seq {
		t.Fatalf("the refused deletes moved the history from %d to %d (%v)", head.Seq, after.Seq, err)
	}
	if got := env.get(t, note.Kind, note.ID); got == nil || got.DeletedAt != nil || got.Version != note.Version {
		t.Fatalf("a refused permanent delete changed the note: %+v", got)
	}
	if got := env.get(t, request.Kind, request.ID); got == nil || got.DeletedAt != nil {
		t.Fatalf("a refused permanent delete took the request on the note: %+v", got)
	}
	wantStatus(t, appScopedDo(t, env, http.MethodDelete, recordPath(memo.Kind, memo.ID)+"?purge=true", app, nil, ""), http.StatusOK)
	if got := env.get(t, memo.Kind, memo.ID); got != nil {
		t.Fatalf("the app's permanent delete of a kind it writes left %+v", got)
	}
}
