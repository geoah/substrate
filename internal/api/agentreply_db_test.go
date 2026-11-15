package api

// A question answered in words, and whose hand resolved a card, over HTTP on
// the real engine (decision 0327): an app's `agents:` token answers a
// question of a thread it reaches with the chat message that names it, the
// resolved record links the person the token acts for, and a person who may
// not read actor records still reads the link and asks which are their own.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// firstPersonActor is the path of the actor record of the repository's
// first person, which a token that names nobody acts for.
func firstPersonActor() string {
	return vocabulary.RecordPath(kindActorRow, vocabulary.ActorID(substrate.ActorPerson))
}

// resolvedByMeOn is the request a client makes for the cards of a thread it
// resolved itself.
func resolvedByMeOn(kind, prop, thread string) string {
	return listOf(kind, `{"thread":{"eq":"`+thread+`"},"`+prop+`":{"eq":"me"}}`)
}

// Each agent's turn ran on the in-process provider (appAgentsLLM), so it
// sets the egress escape and cannot run in parallel.
func TestAnAppsReplyAnswersItsQuestionAndLinksItsPerson(t *testing.T) {
	e := newAppAgentsEnv(t, true)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	chat := func(body string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(e.talked)+"/chat", app, []byte(body), "application/json")
	}
	waiting := func() any { return e.get(t, kindThread, e.lA).Properties["waitingOnPerson"] }
	before := waiting()

	// What a reply must carry, and a question the token does not reach: the
	// other agent's is refused in the words an id that names nothing is.
	other := chat(fmt.Sprintf(`{"thread":%q,"messageId":"x1","message":"teal","repliesTo":%q}`, e.lA, e.iB))
	wantErrorCode(t, other, http.StatusUnprocessableEntity, codeValidation)
	none := chat(fmt.Sprintf(`{"thread":%q,"messageId":"x1","message":"teal","repliesTo":"nothing-here"}`, e.lA))
	wantErrorCode(t, none, http.StatusUnprocessableEntity, codeValidation)
	if a, b := strings.ReplaceAll(other.Body.String(), e.iB, "ID"), strings.ReplaceAll(none.Body.String(), "nothing-here", "ID"); a != b {
		t.Fatalf("the refusal of another agent's question says more than the refusal of none:\n  %s\n  %s", a, b)
	}
	wantErrorCode(t, chat(fmt.Sprintf(`{"thread":%q,"message":"teal","repliesTo":%q}`, e.lA, e.iA)), http.StatusUnprocessableEntity, codeValidation)
	wantErrorCode(t, chat(fmt.Sprintf(`{"messageId":"x2","message":"teal","repliesTo":%q}`, e.iA)), http.StatusUnprocessableEntity, codeValidation)
	if got := e.get(t, kindInteraction, e.iA); got.Properties["state"] != "pending" || waiting() != before {
		t.Fatalf("a refused reply moved the question to %v, waiting on %v", got.Properties["state"], waiting())
	}

	// The reply: one request, and the stream of the turn that reads it.
	got := chat(fmt.Sprintf(`{"thread":%q,"messageId":"reply1","message":"neither: teal","repliesTo":%q}`, e.lA, e.iA))
	wantStatus(t, got, http.StatusOK)
	if !strings.Contains(got.Body.String(), `"done"`) {
		t.Fatalf("the reply streamed %s, want its turn", got.Body.String())
	}
	rec := e.get(t, kindInteraction, e.iA)
	if rec.Properties["state"] != "replied" {
		t.Fatalf("state = %v, want replied", rec.Properties["state"])
	}
	if in := referencedID(rec.Properties["repliedIn"], kindMessage); in != "reply1" {
		t.Fatalf("repliedIn = %v, want the message", rec.Properties["repliedIn"])
	}
	// The hand is the person the app acts for, not the app.
	if by := appScopedRefPath(rec.Properties["resolvedBy"]); by != firstPersonActor() {
		t.Fatalf("resolvedBy = %q, want the person's actor %q", by, firstPersonActor())
	}
	// The app reads the link, and that it is its own person's, with no
	// actor id in hand; `heldTo` stays out of its view.
	seen := decodeJSON[*substrate.Record](t, e.read(t, recordPath(kindInteraction, e.iA), app))
	if appScopedRefPath(seen.Properties["resolvedBy"]) != firstPersonActor() || seen.Properties["heldTo"] != nil {
		t.Fatalf("the app reads %v", seen.Properties)
	}
	if ids := recordIDs(t, e.read(t, resolvedByMeOn(kindInteraction, "resolvedBy", e.lA), app)); ids != e.iA {
		t.Fatalf("the questions the app's person resolved = %q, want %q", ids, e.iA)
	}
	wantStatus(t, e.read(t, recordPath(kindActorRow, vocabulary.ActorID(substrate.ActorPerson)), app), http.StatusNotFound)

	// A repeat of the request writes nothing and resolves nothing again.
	version := rec.Version
	wantStatus(t, chat(fmt.Sprintf(`{"thread":%q,"messageId":"reply1","message":"neither: teal","repliesTo":%q}`, e.lA, e.iA)), http.StatusOK)
	if again := e.get(t, kindInteraction, e.iA); again.Version != version {
		t.Fatalf("a repeated reply moved the question from version %d to %d", version, again.Version)
	}
	// A write of the record cannot claim a message.
	wantErrorCode(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindInteraction, e.iB), e.token,
		[]byte(fmt.Sprintf(`{"properties":{"state":"replied"},"ifVersion":%d}`, e.get(t, kindInteraction, e.iB).Version)), "application/json"),
		http.StatusForbidden, codeForbidden)

	// An approval the app decides links the same person.
	av := e.get(t, kindApproval, e.aA).Version
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindApproval, e.aA), app,
		[]byte(fmt.Sprintf(`{"properties":{"state":"denied"},"ifVersion":%d}`, av)), "application/json"), http.StatusOK)
	if by := appScopedRefPath(e.get(t, kindApproval, e.aA).Properties["decidedBy"]); by != firstPersonActor() {
		t.Fatalf("decidedBy = %q, want the person's actor", by)
	}
	if ids := recordIDs(t, e.read(t, resolvedByMeOn(kindApproval, "decidedBy", e.lA), app)); ids != e.aA {
		t.Fatalf("the approvals the app's person decided = %q, want %q", ids, e.aA)
	}
}

// Runs real turns on the in-process provider, so it cannot run in parallel.
func TestAViewerReadsWhoResolvedACardWithoutReadingActors(t *testing.T) {
	e := newAppAgentsEnv(t, true)
	viewer := addPerson(t, e.dbEnv, "v", scope.RoleViewer)

	// The first person dismisses the question and rejects the change
	// request, with the token that names nobody else.
	iv := e.get(t, kindInteraction, e.iA).Version
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindInteraction, e.iA), e.token,
		[]byte(fmt.Sprintf(`{"properties":{"state":"dismissed"},"ifVersion":%d}`, iv)), "application/json"), http.StatusOK)
	rv := e.get(t, kindRequest, e.rA).Version
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindRequest, e.rA), e.token,
		[]byte(fmt.Sprintf(`{"properties":{"decision":"rejected"},"ifVersion":%d}`, rv)), "application/json"), http.StatusOK)

	// The viewer reads each link: an address, and nothing of the actor.
	for kind, c := range map[string]struct{ id, prop string }{
		kindInteraction: {e.iA, "resolvedBy"},
		kindRequest:     {e.rA, "decidedBy"},
	} {
		rec := decodeJSON[*substrate.Record](t, e.read(t, recordPath(kind, c.id), viewer.token))
		if by := appScopedRefPath(rec.Properties[c.prop]); by != firstPersonActor() {
			t.Fatalf("the viewer reads %s of %s as %q, want %q", c.prop, kind, by, firstPersonActor())
		}
		if rec.Properties["heldTo"] != nil {
			t.Fatalf("the viewer reads whose rules hold %s: %v", kind, rec.Properties["heldTo"])
		}
	}
	if code := e.read(t, recordPath(kindActorRow, vocabulary.ActorID(substrate.ActorPerson)), viewer.token).Code; code != http.StatusNotFound && code != http.StatusForbidden {
		t.Fatalf("the viewer read the actor record: %d", code)
	}
	// "Which of these did I resolve" is theirs to ask, and answers none of
	// another person's; the person who did resolve them reads both as their
	// own.
	for kind, c := range map[string]struct{ id, prop, thread string }{
		kindInteraction: {e.iA, "resolvedBy", e.lA},
		kindRequest:     {e.rA, "decidedBy", e.tA},
	} {
		if ids := recordIDs(t, e.read(t, resolvedByMeOn(kind, c.prop, c.thread), viewer.token)); ids != "" {
			t.Fatalf("the viewer reads %q of %s as resolved by them", ids, kind)
		}
		if ids := recordIDs(t, e.read(t, resolvedByMeOn(kind, c.prop, c.thread), e.token)); ids != c.id {
			t.Fatalf("the person who resolved %s reads %q as their own, want %q", kind, ids, c.id)
		}
	}
}

// An `agents:` token searches the messages of the threads it names, and its
// threads by what they are called, with the list's `search` predicate: the
// engine narrows before it pages, so another agent's rows are never read.
// The ranked read (`q`) stays refused for a kind the token reaches row by
// row: it ranks over the whole kind, and has no thread to narrow by.
func TestAnAgentsScopeSearchesTheMessagesOfTheThreadsItNames(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	search := func(kind, words, props string) string {
		f := `{"kinds":["` + kind + `"],"search":"` + words + `"`
		if props != "" {
			f += `,"properties":` + props
		}
		return "/api/v1/records?filter=" + url.QueryEscape(f+"}")
	}
	// Both agents' threads hold a message saying "hello".
	if ids := recordIDs(t, e.read(t, search(kindMessage, "hello", `{"thread":{"in":["`+e.tA+`"]}}`), app)); ids != e.mA {
		t.Fatalf("the app's search of its thread = %q, want %q alone", ids, e.mA)
	}
	if ids := recordIDs(t, e.read(t, search(kindMessage, "nothingsaysthis", `{"thread":{"in":["`+e.tA+`"]}}`), app)); ids != "" {
		t.Fatalf("a search that matches nothing = %q", ids)
	}
	// A thread of another agent is missing, and no thread at all is refused:
	// the token never reads a message it has not named the thread of.
	wantStatus(t, e.read(t, search(kindMessage, "hello", `{"thread":{"in":["`+e.tA+`","`+e.tB+`"]}}`), app), http.StatusNotFound)
	wantStatus(t, e.read(t, search(kindMessage, "hello", ""), app), http.StatusForbidden)
	// Its threads by title, with none named: the other agent's never comes
	// back, though it is called the same.
	ids := recordIDs(t, e.read(t, search(kindThread, "first", ""), app))
	if !strings.Contains(ids, e.tA) || strings.Contains(ids, e.tB) {
		t.Fatalf("the app's search of its threads = %q, want %s and never %s", ids, e.tA, e.tB)
	}
	// Another kind's records are not the token's to search.
	if rec := e.read(t, search(personKind, "Ada", ""), app); rec.Code == http.StatusOK && recordIDs(t, rec) != "" {
		t.Fatalf("the app searched people: %s", rec.Body.String())
	}
	// The ranked read is refused, for messages and for threads.
	for _, kind := range []string{kindMessage, kindThread} {
		ranked := "/api/v1/records?q=hello&mode=lexical&filter=" + url.QueryEscape(`{"kinds":["`+kind+`"]}`)
		wantStatus(t, e.read(t, ranked, app), http.StatusForbidden)
	}
}
