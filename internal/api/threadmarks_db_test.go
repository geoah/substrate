package api

// Thread marks over HTTP on the real engine (decision 0313): a mark's
// dot-joined id travels the REST path, a viewer keeps their own and nobody
// else's, `me` lists the caller's own marks beside another person's, and an
// app's `agents:` scope reaches the marks of the threads it reaches.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// markDo sends a JSON body as bearer, to the host an app's documents name.
func markDo(t *testing.T, env *dbEnv, method, path, bearer string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	contentType := ""
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
		contentType = "application/json"
	}
	return appScopedDo(t, env, method, path, bearer, raw, contentType)
}

// markOf posts a mark of thread through message as bearer and answers the
// stored record.
func markOf(t *testing.T, env *dbEnv, bearer, thread, message string, want int) *substrate.Record {
	t.Helper()
	rec := markDo(t, env, http.MethodPost, recordsPath, bearer, map[string]any{
		"kind": kindThreadMark, "properties": map[string]any{"thread": thread, "readThrough": message},
	})
	wantStatus(t, rec, want)
	return decodeJSON[*substrate.Record](t, rec)
}

// myMarks is the request a client makes for its own marks of a page of
// threads.
func myMarks(threads ...string) string {
	return listOf(kindThreadMark, `{"thread":{"in":["`+strings.Join(threads, `","`)+`"]},"readBy":{"eq":"me"}}`)
}

// markThrough is the message a mark record reads through.
func markThrough(rec *substrate.Record) string {
	return referencedID(rec.Properties["readThrough"], kindMessage)
}

// addMessage plants a later message of thread.
func addMessage(t *testing.T, env *dbEnv, thread, id string, turn int) {
	t.Helper()
	env.put(t, kindMessage, id, map[string]any{
		"thread": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindThread, thread)},
		"role":   "assistant", "content": "a reply", "turn": turn,
	})
}

// addAnotherPerson adds one more person to a repository addPerson already
// gave its admin.
func addAnotherPerson(t *testing.T, env *dbEnv, label, role string) rolePerson {
	t.Helper()
	ctx := context.Background()
	name := testdb.RepositoryLabel(t) + label + ".example.com"
	if _, err := env.svc.Register(ctx, substrate.RegisterInput{Identity: name, Repository: name, Password: rolesPassword, SkipTOTP: true}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	if err := env.svc.(engine.Operator).AddPerson(ctx, name, env.repository, role); err != nil {
		t.Fatalf("add %s as %s: %v", name, role, err)
	}
	minted, err := env.svc.Login(ctx, substrate.LoginInput{Identity: name, Repository: env.repository, Password: rolesPassword, Label: "session"})
	if err != nil {
		t.Fatalf("sign %s in: %v", name, err)
	}
	return rolePerson{name: name, identity: minted.Token.Identity, token: minted.Secret, tokenID: minted.Token.ID}
}

func TestAMarkTravelsTheRESTPathAndEachPersonKeepsTheirOwn(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	addMessage(t, e.dbEnv, e.tA, "mA2", 2)
	viewer := addPerson(t, e.dbEnv, "v", scope.RoleViewer)
	editor := addAnotherPerson(t, e.dbEnv, "e", scope.RoleEditor)

	// A create with no id is named for the thread and the person.
	mark := markOf(t, e.dbEnv, viewer.token, e.tA, e.mA, http.StatusCreated)
	actor := referencedID(mark.Properties["readBy"], kindActorRow)
	if actor == "" || mark.ID != e.tA+"."+actor {
		t.Fatalf("the viewer's mark is %s read by %v, want <thread id>.<actor id>", mark.ID, mark.Properties["readBy"])
	}
	// The dot-joined id is one path segment: read, put and patch at it.
	path := recordPath(kindThreadMark, mark.ID)
	got := decodeJSON[*substrate.Record](t, markDo(t, e.dbEnv, http.MethodGet, path, viewer.token, nil))
	if got.ID != mark.ID || markThrough(got) != e.mA {
		t.Fatalf("the mark read at its path is %s through %s", got.ID, markThrough(got))
	}
	// A put of what was read, `readBy` and the stored `readAt` included,
	// with the position moved.
	got.Properties["readThrough"] = "mA2"
	rec := markDo(t, e.dbEnv, http.MethodPut, path, viewer.token, map[string]any{"properties": got.Properties})
	wantStatus(t, rec, http.StatusOK)
	if moved := decodeJSON[*substrate.Record](t, rec); markThrough(moved) != "mA2" || moved.Properties["readAt"] == got.Properties["readAt"] {
		t.Fatalf("the put at the path left the mark through %s at %v", markThrough(moved), moved.Properties["readAt"])
	}
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPatch, path, viewer.token, map[string]any{"properties": map[string]any{"markedUnread": true}}), http.StatusOK)
	// A late write is answered with what is stored, as a write and not a
	// create, and is no failure.
	if late := markOf(t, e.dbEnv, viewer.token, e.tA, e.mA, http.StatusOK); markThrough(late) != "mA2" || late.Properties["markedUnread"] != true {
		t.Fatalf("a late write answered %v, want the stored position and flag", late.Properties)
	}
	// A reference names the mark by its path, dot and all.
	if kind, id, ok := vocabulary.SplitRecordPath(vocabulary.RecordPath(kindThreadMark, mark.ID)); !ok || kind != kindThreadMark || id != mark.ID {
		t.Fatalf("the mark's record path splits to %s %s", kind, id)
	}

	// A second person has their own, and neither moves the other's.
	theirs := markOf(t, e.dbEnv, editor.token, e.tA, e.mA, http.StatusCreated)
	if theirs.ID == mark.ID {
		t.Fatal("two people share one mark")
	}
	for who, c := range map[string]struct{ token, path string }{
		"the editor, the viewer's": {editor.token, path},
		"the viewer, the editor's": {viewer.token, recordPath(kindThreadMark, theirs.ID)},
	} {
		for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
			var body any
			if method != http.MethodDelete {
				body = map[string]any{"properties": map[string]any{"markedUnread": false}}
			}
			rec := markDo(t, e.dbEnv, method, c.path, c.token, body)
			wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
			if env := decodeJSON[substrate.ErrorEnvelope](t, rec); env.Error.Reason != substrate.ReasonNotPermitted {
				t.Errorf("%s %s: reason %q, want not_permitted", who, method, env.Error.Reason)
			}
		}
	}
	if kept := e.get(t, kindThreadMark, mark.ID); markThrough(kept) != "mA2" || kept.Properties["markedUnread"] != true {
		t.Fatalf("the viewer's mark after the editor's writes: %v", kept.Properties)
	}

	// "My marks for these threads" is one list read, and the other person's
	// do not come back. Without `me` both do, told apart by `readBy`.
	for who, c := range map[string]struct{ token, want string }{
		"the viewer": {viewer.token, mark.ID},
		"the editor": {editor.token, theirs.ID},
	} {
		if ids := recordIDs(t, markDo(t, e.dbEnv, http.MethodGet, myMarks(e.tA, e.tB), c.token, nil)); ids != c.want {
			t.Errorf("%s's own marks = %q, want %q alone", who, ids, c.want)
		}
	}
	both := decodeJSON[substrate.Page](t, markDo(t, e.dbEnv, http.MethodGet, listOf(kindThreadMark, `{"thread":{"in":["`+e.tA+`"]}}`), viewer.token, nil))
	if len(both.Records) != 2 {
		t.Fatalf("the marks of the thread, everyone's = %d, want the two people's", len(both.Records))
	}
	// The admin's full-access token names nobody new: it has no mark yet, and
	// reads none of the others' as its own.
	if ids := recordIDs(t, markDo(t, e.dbEnv, http.MethodGet, myMarks(e.tA, e.tB), e.token, nil)); ids != "" {
		t.Errorf("a person with no mark reads %q as their own", ids)
	}

	// The viewer's role writes nothing else.
	wantErrorCode(t, markDo(t, e.dbEnv, http.MethodPatch, recordPath(kindThread, e.tA), viewer.token, map[string]any{"properties": map[string]any{"subject": "mine"}}),
		http.StatusForbidden, codeForbidden)
	// A viewer's own delete is theirs.
	wantStatus(t, markDo(t, e.dbEnv, http.MethodDelete, path, viewer.token, nil), http.StatusOK)
}

func TestAnAgentsScopeReachesTheMarksOfTheThreadsItReaches(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	addMessage(t, e.dbEnv, e.tA, "mA2", 2)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	other := addPerson(t, e.dbEnv, "e", scope.RoleEditor)
	theirs := markOf(t, e.dbEnv, other.token, e.tA, e.mA, http.StatusCreated)

	// The app writes the mark of the person it acts for, without knowing
	// their actor, on a thread its scope reaches and on no other.
	mark := markOf(t, e.dbEnv, app, e.tA, e.mA, http.StatusCreated)
	if mark.ID == theirs.ID || !strings.HasPrefix(mark.ID, e.tA+".") {
		t.Fatalf("the app's mark is %s, want its own person's of %s", mark.ID, e.tA)
	}
	rec := markDo(t, e.dbEnv, http.MethodPost, recordsPath, app, map[string]any{
		"kind": kindThreadMark, "properties": map[string]any{"thread": e.tB, "readThrough": e.mB},
	})
	wantStatus(t, rec, http.StatusNotFound)

	path := recordPath(kindThreadMark, mark.ID)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodGet, path, app, nil), http.StatusOK)
	moved := markDo(t, e.dbEnv, http.MethodPatch, path, app, map[string]any{"properties": map[string]any{"readThrough": "mA2"}})
	wantStatus(t, moved, http.StatusOK)
	if got := decodeJSON[*substrate.Record](t, moved); markThrough(got) != "mA2" {
		t.Fatalf("the app's move left the mark through %s", markThrough(got))
	}
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPut, path, app, map[string]any{"properties": map[string]any{"markedUnread": true}}), http.StatusOK)

	// Its own marks of a page of threads, the other person's left out; a
	// thread it does not reach is answered as missing, and the list names
	// its threads.
	if ids := recordIDs(t, markDo(t, e.dbEnv, http.MethodGet, myMarks(e.tA), app, nil)); ids != mark.ID {
		t.Fatalf("the app's own marks = %q, want %q alone", ids, mark.ID)
	}
	wantStatus(t, markDo(t, e.dbEnv, http.MethodGet, myMarks(e.tA, e.tB), app, nil), http.StatusNotFound)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodGet, listOf(kindThreadMark, `{"readBy":{"eq":"me"}}`), app, nil), http.StatusForbidden)
	// Another actor is a record of a kind the app does not read.
	wantStatus(t, markDo(t, e.dbEnv, http.MethodGet,
		listOf(kindThreadMark, `{"thread":{"in":["`+e.tA+`"]},"readBy":{"eq":"`+referencedID(theirs.Properties["readBy"], kindActorRow)+`"}}`), app, nil), http.StatusForbidden)

	// Nothing else of a mark is the app's: not another person's, not a
	// property outside the mark's own, not its deletion.
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPatch, recordPath(kindThreadMark, theirs.ID), app, map[string]any{"properties": map[string]any{"markedUnread": true}}), http.StatusForbidden)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPatch, path, app, map[string]any{"properties": map[string]any{"thread": e.tB}}), http.StatusForbidden)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodPut, path, app, map[string]any{"properties": map[string]any{"markedUnread": true}, "labels": map[string]any{"app/x": "y"}}), http.StatusForbidden)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodDelete, path, app, nil), http.StatusForbidden)
	if kept := e.get(t, kindThreadMark, theirs.ID); markThrough(kept) != e.mA || kept.Properties["markedUnread"] == true {
		t.Fatalf("the other person's mark after the app's writes: %v", kept.Properties)
	}

	// An app with no agents scope reaches no mark.
	reader := appScopedToken(t, e.dbEnv, "read:"+personKind)
	wantStatus(t, markDo(t, e.dbEnv, http.MethodGet, path, reader, nil), http.StatusNotFound)
	rec = markDo(t, e.dbEnv, http.MethodPost, recordsPath, reader, map[string]any{
		"kind": kindThreadMark, "properties": map[string]any{"thread": e.tA, "readThrough": e.mA},
	})
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("an app with no agents scope wrote a mark: %d", rec.Code)
	}
}
