package testenv_test

// The details of the API a client builds its retries on, over a real socket
// against the real engine: a retry key on the writes of a named record, an
// error that says a write may have landed, one answer for a list that has to
// start again, and the refusal of a request field the server does not know.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testenv"
)

const (
	detailsAuthority = "apidetails.example.com"
	detailsRef       = detailsAuthority + "/details"
	detailNotesPath  = "/api/v1/" + detailsRef + "/note"
)

// detailsVocabulary declares a note with a name, and a rank no record below
// holds, so redeclaring the rank's type changes the order a list by rank
// resolves to without stranding a record.
func detailsVocabulary(rankType string) string {
	return `
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + detailsRef + `
data:
  authority: ` + detailsAuthority + `
  package: details
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + detailsRef + `/note
data:
  authority: ` + detailsAuthority + `
  package: details
  names:
    singular: note
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: what the note says
    rank:
      type: ` + rankType + `
      description: where the note sorts
`
}

// noteID is a minted-shape id, which a client may create a record of an
// installed kind under (decision 0228).
func noteID(n int) string {
	return "0000000000000000000000000" + string(rune('0'+n))
}

// keyed sends one JSON write under an Idempotency-Key.
func keyed(t *testing.T, e *testenv.Env, method, path, key string, body any) (int, map[string]any) {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	status, out, _ := e.DoRaw(method, path, raw, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": key,
	})
	var decoded map[string]any
	_ = json.Unmarshal(out, &decoded)
	return status, decoded
}

func notesHead(t *testing.T, e *testenv.Env) float64 {
	t.Helper()
	var page struct {
		Head float64 `json:"head"`
	}
	mustDecode(t, e, http.MethodGet, listPath(detailsRef+"/note", nil)+"&first=1", nil, &page)
	return page.Head
}

// A put, a patch and a delete each take a retry key: the repeat answers the
// first attempt's outcome, status included, and writes nothing, where the same
// guarded write sent again without a key meets the version it moved.
func TestARetryKeyOnTheWritesOfANamedRecord(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	path := detailNotesPath + "/" + noteID(1)

	// A put's and a patch's guard rides in the body: one in the address is
	// refused before the key is bound, so the key stays free for the request
	// the client meant.
	put := map[string]any{"properties": map[string]any{"name": "first"}}
	if status, body := keyed(t, e, http.MethodPut, path+"?ifVersion=0", "put-1", put); status != http.StatusBadRequest {
		t.Fatalf("a keyed put guarded in its address answered %d %v, want 400", status, body)
	}
	status, first := keyed(t, e, http.MethodPut, path, "put-1", put)
	if status != http.StatusCreated {
		t.Fatalf("put: %d %v", status, first)
	}
	at := notesHead(t, e)
	status, again := keyed(t, e, http.MethodPut, path, "put-1", put)
	if status != http.StatusCreated || again["version"] != first["version"] || again["updatedAt"] != first["updatedAt"] {
		t.Fatalf("the repeated put answered %d %v, want the first attempt's 201 %v", status, again, first)
	}
	if status, body := keyed(t, e, http.MethodPut, path, "put-1", map[string]any{"properties": map[string]any{"name": "other"}}); status != http.StatusConflict {
		t.Fatalf("the key with another body answered %d %v, want 409", status, body)
	}

	patch := map[string]any{"ifVersion": 1, "properties": map[string]any{"name": "second"}}
	if status, body := keyed(t, e, http.MethodPatch, path+"?ifVersion=1", "patch-1", patch); status != http.StatusBadRequest {
		t.Fatalf("a keyed patch guarded in its address answered %d %v, want 400", status, body)
	}
	status, patched := keyed(t, e, http.MethodPatch, path, "patch-1", patch)
	if status != http.StatusOK || patched["version"] != float64(2) {
		t.Fatalf("patch: %d %v", status, patched)
	}
	status, again = keyed(t, e, http.MethodPatch, path, "patch-1", patch)
	if status != http.StatusOK || again["version"] != float64(2) {
		t.Fatalf("the repeated guarded patch answered %d %v, want the first attempt's 200 at version 2", status, again)
	}
	if status, body := e.Do(http.MethodPatch, path, patch); status != http.StatusConflict {
		t.Fatalf("the same guarded patch without a key answered %d %s, want the 409 its own first attempt causes", status, body)
	}
	// The same key at another record is another request.
	if status, body := keyed(t, e, http.MethodPatch, detailNotesPath+"/"+noteID(2), "patch-1", patch); status != http.StatusConflict {
		t.Fatalf("the key at another record answered %d %v, want 409", status, body)
	}

	status, deleted := keyed(t, e, http.MethodDelete, path+"?ifVersion=2", "delete-1", nil)
	if status != http.StatusOK || deleted["deletedAt"] == nil {
		t.Fatalf("delete: %d %v", status, deleted)
	}
	status, again = keyed(t, e, http.MethodDelete, path+"?ifVersion=2", "delete-1", nil)
	if status != http.StatusOK || again["deletedAt"] != deleted["deletedAt"] {
		t.Fatalf("the repeated guarded delete answered %d %v, want the first attempt's tombstone", status, again)
	}

	// Each repeat wrote nothing: the changelog moved by the patch and the
	// delete alone after the put.
	if got := notesHead(t, e); got != at+2 {
		t.Fatalf("the head moved from %v to %v, want two writes", at, got)
	}

	// A declaration is no write the key store answers for.
	if status, body := keyed(t, e, http.MethodPatch, "/api/v1/substrate.reamde.dev/core/kind/"+url.PathEscape(detailsRef+"/note"), "decl-1",
		map[string]any{"properties": map[string]any{"description": "notes"}}); status != http.StatusUnprocessableEntity ||
		!strings.Contains(body["error"].(map[string]any)["message"].(string), "Idempotency-Key is not accepted on a vocabulary kind") {
		t.Fatalf("a keyed patch of a declaration answered %d %v, want 422", status, body)
	}
}

// A write whose commit's answer is lost after Postgres committed answers 500
// with the reason in_doubt, and the same request under the same key then
// answers the write that landed instead of writing it again.
func TestAWriteInDoubtIsMarkedAndItsKeyAnswersIt(t *testing.T) {
	var lose atomic.Bool
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority),
		testenv.WithEngineOptions(engine.WithTestCommitInDoubt(lose.Load)))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	path := detailNotesPath + "/" + noteID(1)
	body := map[string]any{"properties": map[string]any{"name": "landed"}}

	lose.Store(true)
	status, refused := keyed(t, e, http.MethodPut, path, "doubt-1", body)
	lose.Store(false)
	problem, _ := refused["error"].(map[string]any)
	if status != http.StatusInternalServerError || problem["code"] != "internal" || problem["reason"] != "in_doubt" ||
		!strings.Contains(problem["message"].(string), "may have landed") {
		t.Fatalf("a commit whose answer was lost answered %d %v, want 500 internal with the reason in_doubt", status, refused)
	}

	status, replayed := keyed(t, e, http.MethodPut, path, "doubt-1", body)
	if status != http.StatusCreated || replayed["version"] != float64(1) {
		t.Fatalf("the retry under the key answered %d %v, want the landed write's 201 at version 1", status, replayed)
	}
	var stored map[string]any
	mustDecode(t, e, http.MethodGet, path, nil, &stored)
	if stored["version"] != float64(1) || stored["properties"].(map[string]any)["name"] != "landed" {
		t.Fatalf("the record = %v, want the one write at version 1", stored)
	}

	// An error before the commit is no doubt: a refused write carries no
	// reason.
	status, plain := keyed(t, e, http.MethodPatch, path, "doubt-2", map[string]any{"ifVersion": 7, "properties": map[string]any{"name": "x"}})
	if p, _ := plain["error"].(map[string]any); status != http.StatusConflict || p["reason"] != nil {
		t.Fatalf("a stale guard answered %d %v, want a 409 with no reason", status, plain)
	}
}

// A list that has to start again answers one way, 410 compacted with the head
// to start from, whatever made it: the order the request resolves to changed
// under a declaration, or the token is from another version of the paging.
// A token replayed under another request is still the client's own mistake.
func TestAListThatMustStartAgainAnswersOneWay(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	for n := 1; n <= 3; n++ {
		if status, body := e.Do(http.MethodPut, detailNotesPath+"/"+noteID(n),
			map[string]any{"properties": map[string]any{"name": noteID(n)}}); status != http.StatusCreated {
			t.Fatalf("seed %d: %d %s", n, status, body)
		}
	}
	byRank := listPath(detailsRef+"/note", nil) + "&orderBy=rank&first=1"
	var page struct {
		Cursor string `json:"cursor"`
	}
	mustDecode(t, e, http.MethodGet, byRank, nil, &page)
	if page.Cursor == "" {
		t.Fatal("the first page carries no cursor")
	}

	wantListAgain := func(what, path string) {
		t.Helper()
		status, raw := e.Do(http.MethodGet, path, nil)
		var env struct {
			Error struct {
				Code string `json:"code"`
				Head *int64 `json:"head"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		if status != http.StatusGone || env.Error.Code != "compacted" || env.Error.Head == nil {
			t.Fatalf("%s answered %d %s, want 410 compacted with the head to list again from", what, status, raw)
		}
	}

	// A token without the version this server mints, as an earlier release
	// minted it.
	raw, err := base64.RawURLEncoding.DecodeString(page.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	var tok map[string]any
	if err := json.Unmarshal(raw, &tok); err != nil {
		t.Fatal(err)
	}
	delete(tok, "v")
	old, _ := json.Marshal(tok)
	wantListAgain("a token of an earlier format", byRank+"&after="+base64.RawURLEncoding.EncodeToString(old))

	// The same token under another request: the client's mistake, a 422.
	byName := listPath(detailsRef+"/note", nil) + "&orderBy=name&first=1&after=" + page.Cursor
	if status, body := e.Do(http.MethodGet, byName, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("the token under another orderBy answered %d %s, want 422", status, body)
	}

	// The rank becomes a number: the same request now orders by a numeric
	// cast, so the token's position means nothing in it.
	e.ApplyVocabularyYAML(detailsVocabulary("int"))
	wantListAgain("a token whose order a declaration changed", byRank+"&after="+page.Cursor)

	// And the walk started again goes on.
	mustDecode(t, e, http.MethodGet, byRank, nil, &page)
	if status, body := e.Do(http.MethodGet, byRank+"&after="+page.Cursor, nil); status != http.StatusOK {
		t.Fatalf("the next page of the new walk: %d %s", status, body)
	}
}

// The server refuses a request field it does not know by name, which is why
// a client checks a feature's revision before it sends a field that revision
// added; discovery serves the revision it checks.
func TestAnUnknownRequestFieldIsRefusedAndDiscoveryServesTheRevisions(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(detailsAuthority))
	e.ApplyVocabularyYAML(detailsVocabulary("string"))
	status, raw := e.Do(http.MethodPut, detailNotesPath+"/"+noteID(1),
		map[string]any{"properties": map[string]any{"name": "n"}, "addedLater": true})
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "addedLater") || !strings.Contains(string(raw), `"bad_request"`) {
		t.Fatalf("an unknown request field answered %d %s, want 400 bad_request naming it", status, raw)
	}
	var doc struct {
		Features []struct {
			Name     string `json:"name"`
			Revision int    `json:"revision"`
		} `json:"features"`
	}
	mustDecode(t, e, http.MethodGet, "/.well-known/substrate/server.json", nil, &doc)
	revisions := map[string]int{}
	for _, f := range doc.Features {
		revisions[f.Name] = f.Revision
	}
	// Revision 2 of records is the retry key on a named record's writes, and
	// revision 2 of errors the reason in_doubt. Sign-in is a feature too.
	if revisions["records"] < 2 || revisions["errors"] < 2 || revisions["auth"] < 1 {
		t.Fatalf("discovery serves records at %d, errors at %d and auth at %d, want at least 2, 2 and 1",
			revisions["records"], revisions["errors"], revisions["auth"])
	}
}
