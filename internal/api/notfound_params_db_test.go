package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Every parameter the list DOES support keeps working — the refusal of the
// unknown ones (TestUnknownListParamsAreRefused) must not be a blanket one.
// `expand` names a reference property a listed kind declares: the engine
// refuses one no kind declares, which is not the parameter refused.
func TestSupportedListParamsStillWork(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": "Ada"})

	for _, query := range []string{
		"",
		"?first=5",
		"?after=",
		"?orderBy=updatedAt:desc",
		"?withAnnotations=1",
		"?expand=memberOf",
		`?filter={"properties":{"name":{"eq":"Ada"}}}`,
		`?filter={"kinds":["` + personKind + `"]}&first=5&orderBy=createdAt&withAnnotations=1`,
	} {
		rec := env.do(t, http.MethodGet, recordsPath+query, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
	}
	// The watch mode's own parameters: the switch and the resume cursor.
	head, err := env.ds.Head(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	wantNotRefused(t, env.testEnv, recordsPath+"?watch=1&from=0", env.token)
	wantNotRefused(t, env.testEnv, recordsOf(t, personKind, "watch=1", "from=1", "generation="+head.Generation), env.token)
}

// With no parameter the single-record read answers the record, an empty
// query included.
func TestRecordReadAnswersWithoutParameters(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	id := createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": "Ada"}).ID
	for _, query := range []string{"", "?"} {
		rec := env.do(t, http.MethodGet, peoplePath+"/"+id+query, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.Record](t, rec).ID; got != id {
			t.Errorf("GET record%s: id = %q, want %q", query, got, id)
		}
	}
}

// The feed's real parameters keep working, in both of its modes.
func TestSupportedChangeParamsStillWork(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": "Ada"})
	head, err := env.ds.Head(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	for _, query := range []string{
		"",
		"?from=0",
		"?first=2",
		"?first=2&before=3&generation=" + head.Generation,
		"?kinds=samples.substrate.reamde.dev/people/person&ops=put&actors=owner",
		"?excludeKinds=samples.substrate.reamde.dev/tasks/task&excludeOps=delete&excludeActors=machine",
		"?recordId=e1&recordKind=samples.substrate.reamde.dev/people/person",
		"?q=ada",
	} {
		rec := env.do(t, http.MethodGet, changesPath+query, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
	}
	wantNotRefused(t, env.testEnv, changesPath+"?watch=1&from=3&generation="+head.Generation+"&kinds=samples.substrate.reamde.dev/people/person", env.token)
}
