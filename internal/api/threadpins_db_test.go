package api

// Pinned and archived chats over HTTP on the real engine (decision 0322):
// the two flags on a person's own mark, the three lists a client reads by
// them (`referencedBy`), and how roles and an app's `agents:` scope hold
// both. Another person's flags never change what a reader's list holds.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// flagOf posts one flag of the bearer's own mark of thread, with no id.
func flagOf(t *testing.T, env *dbEnv, bearer, thread, name string, on bool, want int) *substrate.Record {
	t.Helper()
	rec := markDo(t, env, http.MethodPost, recordsPath, bearer, map[string]any{
		"kind": kindThreadMark, "properties": map[string]any{"thread": thread, name: on},
	})
	wantStatus(t, rec, want)
	return decodeJSON[*substrate.Record](t, rec)
}

// threadsBy is the request a client makes for the threads its person's own
// mark says something about (flag true), or says no such thing about
// (exists false), oldest id first, with whatever else the query carries.
func threadsBy(flag string, exists bool, more string) string {
	f := `{"kinds":["` + kindThread + `"],"referencedBy":{"kind":"` + kindThreadMark + `","property":"thread",` +
		`"properties":{"readBy":{"eq":"me"},"` + flag + `":{"eq":true}}`
	if !exists {
		f += `,"exists":false`
	}
	return "/api/v1/records?orderBy=id&filter=" + url.QueryEscape(f+"}}") + more
}

func TestAPersonsListLeavesOutWhatTheyArchivedAndNobodyElses(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	viewer := addPerson(t, e.dbEnv, "v", scope.RoleViewer)
	editor := addAnotherPerson(t, e.dbEnv, "e", scope.RoleEditor)

	// A viewer archives a chat they never opened: the write makes their
	// mark, which reads through nothing.
	mark := flagOf(t, e.dbEnv, viewer.token, e.tA, "archived", true, http.StatusCreated)
	if mark.Properties["archived"] != true || mark.Properties["readThrough"] != nil || mark.Properties["readAt"] != nil {
		t.Fatalf("an archive of a chat never opened stored %v, want the flag and no position", mark.Properties)
	}
	flagOf(t, e.dbEnv, viewer.token, e.tB, "pinned", true, http.StatusCreated)
	flagOf(t, e.dbEnv, editor.token, e.tA, "pinned", true, http.StatusCreated)
	// A second write of the same mark is a write, not a create, and reading
	// the chat leaves the flag.
	if again := markOf(t, e.dbEnv, viewer.token, e.tA, e.mA, http.StatusOK); again.Properties["archived"] != true || markThrough(again) != e.mA {
		t.Fatalf("reading an archived chat left the mark %v", again.Properties)
	}

	for _, tc := range []struct {
		who, token                   string
		unarchived, archived, pinned string
	}{
		{"the viewer", viewer.token, "tA2,tB", "tA", "tB"},
		{"the editor", editor.token, "tA,tA2,tB", "", "tA"},
		{"the admin, who has no mark", e.token, "tA,tA2,tB", "", ""},
	} {
		if got := recordIDs(t, e.read(t, threadsBy("archived", false, ""), tc.token)); got != tc.unarchived {
			t.Errorf("%s: unarchived threads %q, want %q", tc.who, got, tc.unarchived)
		}
		if got := recordIDs(t, e.read(t, threadsBy("archived", true, ""), tc.token)); got != tc.archived {
			t.Errorf("%s: archived threads %q, want %q", tc.who, got, tc.archived)
		}
		if got := recordIDs(t, e.read(t, threadsBy("pinned", true, ""), tc.token)); got != tc.pinned {
			t.Errorf("%s: pinned threads %q, want %q", tc.who, got, tc.pinned)
		}
	}
	// The count and the cursor are of the narrowed list.
	page := decodeJSON[substrate.Page](t, e.read(t, threadsBy("archived", false, "&first=1&count=1"), viewer.token))
	if page.Count == nil || *page.Count != 2 || len(page.Records) != 1 || page.Records[0].ID != "tA2" || page.Cursor == "" {
		t.Fatalf("the first page of the viewer's unarchived threads: %d records, count %v, cursor %q", len(page.Records), page.Count, page.Cursor)
	}
	if got := recordIDs(t, e.read(t, threadsBy("archived", false, "&first=1&after="+url.QueryEscape(page.Cursor)), viewer.token)); got != "tB" {
		t.Fatalf("the second page is %q, want tB", got)
	}

	// Unarchiving is the same write, and the list follows; nobody clears
	// another person's flag.
	path := recordPath(kindThreadMark, mark.ID)
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		rec := markDo(t, e.dbEnv, method, path, editor.token, map[string]any{"properties": map[string]any{"archived": false}})
		wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	}
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPatch, path, viewer.token, map[string]any{"properties": map[string]any{"archived": false}}), http.StatusOK)
	if got := recordIDs(t, e.read(t, threadsBy("archived", false, ""), viewer.token)); got != "tA,tA2,tB" {
		t.Fatalf("after the unarchive the viewer's threads are %q", got)
	}

	// The arm is the list's alone, and names what it cannot follow.
	arm := `"referencedBy":{"kind":"` + kindThreadMark + `","property":"thread"}`
	filter := url.QueryEscape(`{"kinds":["` + kindThread + `"],` + arm + `}`)
	for what, target := range map[string]string{
		"the ranked read": "/api/v1/records?q=hello&filter=" + filter,
		"the tail":        "/api/v1/records?watch=1&filter=" + filter,
		"the aggregate": "/api/v1/records?filter=" + filter + "&aggregate=" + url.QueryEscape(
			`{"metric":{"ref":"substrate.reamde.dev/observations/metric/x"},"range":{"gte":"2026-01-01T00:00:00Z","lt":"2026-02-01T00:00:00Z"},"reducers":["avg"]}`),
	} {
		rec := e.read(t, target, e.token)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if !strings.Contains(rec.Body.String(), "filter.referencedBy") {
			t.Errorf("%s refused without naming the arm: %s", what, rec.Body.String())
		}
	}
	unknown := url.QueryEscape(`{"kinds":["` + kindThread + `"],"referencedBy":{"kind":"` + llmPackage + `/nothing","property":"thread"}}`)
	wantStatus(t, e.read(t, "/api/v1/records?filter="+unknown, e.token), http.StatusNotFound)
	misspelled := url.QueryEscape(`{"kinds":["` + kindThread + `"],"referencedBy":{"kind":"` + kindThreadMark + `","property":"thread","properties":{"archivd":{"eq":true}},"exists":false}}`)
	wantStatus(t, e.read(t, "/api/v1/records?filter="+misspelled, e.token), http.StatusUnprocessableEntity)
}

func TestAnAgentsScopePinsArchivesAndListsItsPersonsOwn(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	other := addPerson(t, e.dbEnv, "e", scope.RoleEditor)
	// Another person archives the thread the app reaches, and pins it.
	flagOf(t, e.dbEnv, other.token, e.tA, "archived", true, http.StatusCreated)
	flagOf(t, e.dbEnv, other.token, e.tA, "pinned", true, http.StatusOK)

	// The app's lists hold its own person's flags: none yet, whatever the
	// other person did. Its scope reaches the talked agent's two threads.
	if got := recordIDs(t, e.read(t, threadsBy("archived", false, ""), app)); got != "tA,tA2" {
		t.Fatalf("the app's unarchived threads are %q, want its agent's two", got)
	}
	for _, flag := range []string{"archived", "pinned"} {
		if got := recordIDs(t, e.read(t, threadsBy(flag, true, ""), app)); got != "" {
			t.Fatalf("the app's %s threads are %q, want none: only another person set it", flag, got)
		}
	}

	// It pins by a create that names the thread, and archives by a patch at
	// the mark's id; a thread it does not reach is missing.
	mark := flagOf(t, e.dbEnv, app, e.tA, "pinned", true, http.StatusCreated)
	path := recordPath(kindThreadMark, mark.ID)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPatch, path, app, map[string]any{"properties": map[string]any{"archived": true}}), http.StatusOK)
	rec := markDo(t, e.dbEnv, http.MethodPost, recordsPath, app, map[string]any{
		"kind": kindThreadMark, "properties": map[string]any{"thread": e.tB, "archived": true},
	})
	wantStatus(t, rec, http.StatusNotFound)
	if got := recordIDs(t, e.read(t, threadsBy("archived", false, ""), app)); got != "tA2" {
		t.Fatalf("after its archive the app's unarchived threads are %q, want tA2", got)
	}
	for _, flag := range []string{"archived", "pinned"} {
		if got := recordIDs(t, e.read(t, threadsBy(flag, true, ""), app)); got != "tA" {
			t.Fatalf("the app's %s threads are %q, want tA", flag, got)
		}
	}
	// The other person's mark is not the app's to change.
	marks := decodeJSON[substrate.Page](t, e.read(t, listOf(kindThreadMark, `{"thread":{"in":["`+e.tA+`"]}}`), app))
	for _, m := range marks.Records {
		if m.ID == mark.ID {
			continue
		}
		wantStatus(t, markDo(t, e.dbEnv, http.MethodPatch, recordPath(kindThreadMark, m.ID), app, map[string]any{"properties": map[string]any{"archived": false}}), http.StatusForbidden)
		if kept := e.get(t, kindThreadMark, m.ID); kept.Properties["archived"] != true || kept.Properties["pinned"] != true {
			t.Fatalf("the other person's mark after the app's write: %v", kept.Properties)
		}
	}
	if len(marks.Records) != 2 {
		t.Fatalf("the marks of the thread, everyone's = %d, want the two people's", len(marks.Records))
	}

	// The arm reaches what the scope reaches and nothing past it: a kind the
	// agent verbs reach through its thread, through `thread`, on a list of
	// threads. A change request is reached by another verb, a property that
	// is not the thread is no such reach, and a kind the token cannot read
	// is unknown.
	by := func(kinds, kind, property string) string {
		return "/api/v1/records?filter=" + url.QueryEscape(`{"kinds":["`+kinds+`"],"referencedBy":{"kind":"`+kind+`","property":"`+property+`"}}`)
	}
	if got := recordIDs(t, e.read(t, by(kindThread, kindMessage, "thread"), app)); got != "tA" {
		t.Fatalf("the threads a message points at are %q, want tA", got)
	}
	wantStatus(t, e.read(t, by(kindThread, kindRequest, "thread"), app), http.StatusNotFound)
	wantStatus(t, e.read(t, by(kindThread, kindThreadMark, "readBy"), app), http.StatusForbidden)
	wantStatus(t, e.read(t, by(kindThread, personKind, "thread"), app), http.StatusNotFound)
	wantStatus(t, e.read(t, by(kindMessage, kindThreadMark, "thread"), app), http.StatusForbidden)

	// A token that reads a kind whole may not ask what of a row-gated kind
	// points at its rows.
	both := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*", "read:"+personKind)
	wantStatus(t, e.read(t, by(personKind, kindThreadMark, "thread"), both), http.StatusForbidden)
	// And one with no agents scope reaches neither kind.
	reader := appScopedToken(t, e.dbEnv, "read:"+personKind)
	wantStatus(t, e.read(t, by(personKind, kindThreadMark, "thread"), reader), http.StatusNotFound)
	wantStatus(t, e.read(t, threadsBy("archived", false, ""), reader), http.StatusNotFound)
}
