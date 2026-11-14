package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// spellingsOf is both spellings of an own package: `~` and the repository's
// full name.
func spellingsOf(full string) map[string]string {
	return map[string]string{"~": "~/notes", "full": full}
}

// notesDeclarations are the own package the spellings tests drive: a note
// titled by its name, a `stamp` only a function writes, and a link whose
// `to` takes a note.
func notesDeclarations(full string) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(full, 0),
		vocabulary.KindManifest(full, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"stamp": map[string]any{"type": "string", "writer": []any{"function"}},
			},
		}),
		vocabulary.KindManifest(full, map[string]any{"singular": "link"}, map[string]any{
			"properties": map[string]any{"to": map[string]any{"type": "reference", "kind": full + "/note"}},
		}),
	}
}

// BOTH SPELLINGS MEET BEFORE EVERY CHECK (decision record 0259). Each door
// that takes a kind from outside is driven twice, once with `~` and once with
// the repository's full name, under each check that reads the kind: an app
// token's scope, a person's role, a property's `writer:`, a reference's pin,
// the assistant's door, a function's grant, a change policy's selector, the
// vocabulary's admission and the catalog. Every cell answers the same for
// both, and the answer it expects says the check was met at all. A control
// kind is core's and has one spelling.
func TestBothSpellingsOfAnOwnKindMeetBeforeEveryCheck(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withConfig(func(c *Config) { c.Catalog = catalogShipped(t) }))
	full := env.repository + "/notes"
	const scribe = "~/notes/scribe"
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, append(notesDeclarations(full),
		// The scribe puts a note and lists the notes under the kind its
		// caller names, and answers the kinds the list read.
		vocabulary.FunctionManifest(full, "scribe", map[string]any{
			"description": "puts a note and lists the notes, under the kind it is handed",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{
				"reads":  map[string]any{"kinds": []any{full + "/note"}},
				"writes": []any{full + "/note"},
			},
			"source": "def main(input, host):\n" +
				"    kind = input[\"args\"][\"kind\"]\n" +
				"    host.effects.put(kind, input[\"args\"][\"id\"], {\"name\": \"by the scribe\"})\n" +
				"    page = host.records.list([kind])\n" +
				"    return {\"output\": {\"kinds\": sorted({r[\"kind\"] for r in page.get(\"records\") or []})}}\n",
		}),
	)); err != nil {
		t.Fatalf("declare the notes: %v", err)
	}
	n1 := mintedID("both-spellings-n1")
	env.put(t, "~/notes/note", n1, map[string]any{"name": "first"})
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	app := appScopedToken(t, env, "read:"+full+"/note")
	writer := appScopedToken(t, env, "write:"+full+"/note")
	_, assistant := mcpAssistantToken(t, env)

	spellings := spellingsOf(full)
	type answer struct {
		status int
		kind   string
	}
	read := func(status int, body []byte) answer {
		var got struct {
			Kind    string `json:"kind"`
			Records []struct {
				Kind string `json:"kind"`
			} `json:"records"`
			Changes []struct {
				Kind string `json:"kind"`
			} `json:"changes"`
		}
		_ = json.Unmarshal(body, &got)
		kind := got.Kind
		for _, r := range got.Records {
			kind = r.Kind
		}
		for _, c := range got.Changes {
			kind = c.Kind
		}
		return answer{status: status, kind: kind}
	}
	type cell struct {
		door, check string
		// drive answers the door for one spelling of the package, pkg.
		drive func(pkg string) answer
		// want is the status both spellings answer; the kind they answer,
		// where they answer one, is always `~`.
		want int
	}
	do := func(method, path, token string, body any) answer {
		rec := env.do(t, method, path, token, body)
		return read(rec.Code, rec.Body.Bytes())
	}
	// An MCP tool's answer is the status the route behind it gave: 200, or
	// the refusal its error carries.
	tool := func(name string, args map[string]any) answer {
		text, isErr := callTool(t, env.h, assistant, name, args)
		if isErr {
			return answer{status: http.StatusBadRequest}
		}
		return read(http.StatusOK, []byte(text))
	}
	// key is a record id part of one spelling of pkg.
	key := func(pkg string) string { return strings.NewReplacer("~", "local", "/", "-").Replace(pkg) }
	listOf := func(pkg string) string {
		return recordsPath + "?filter=" + url.QueryEscape(`{"kinds":["`+pkg+`/note"]}`)
	}
	changesOf := func(pkg string) string {
		return "/api/v1/changes?first=50&recordKind=" + url.QueryEscape(pkg+"/note") + "&recordId=" + n1
	}
	// A note of each spelling's own to patch and one to delete.
	for _, pkg := range spellings {
		env.put(t, "~/notes/note", mintedID("patched-"+key(pkg)), map[string]any{"name": "before"})
		env.put(t, "~/notes/note", mintedID("deleted-"+key(pkg)), map[string]any{"name": "doomed"})
	}
	cells := []cell{
		// An app token's scope, `read:` or `write:` the note kind as the
		// owner approved it.
		{"a record's address", "an app's scope", func(pkg string) answer {
			return do(http.MethodGet, "/api/v1/"+pkg+"/note/"+n1, app, nil)
		}, http.StatusOK},
		{"the records list", "an app's scope", func(pkg string) answer {
			return do(http.MethodGet, listOf(pkg), app, nil)
		}, http.StatusOK},
		{"the change feed", "an app's scope", func(pkg string) answer {
			return do(http.MethodGet, changesOf(pkg), app, nil)
		}, http.StatusOK},
		{"a write by address", "an app's scope", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/"+pkg+"/note/"+mintedID("app-"+key(pkg)), app,
				map[string]any{"properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		{"a patch by address", "an app's scope", func(pkg string) answer {
			return do(http.MethodPatch, "/api/v1/"+pkg+"/note/"+n1, app,
				map[string]any{"properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		{"a create", "an app's scope", func(pkg string) answer {
			return do(http.MethodPost, recordsPath, writer,
				map[string]any{"kind": pkg + "/note", "properties": map[string]any{"name": "by the app"}})
		}, http.StatusCreated},
		// A person's role: a viewer reads every kind it holds and writes none.
		{"a record's address", "a role's rule", func(pkg string) answer {
			return do(http.MethodGet, "/api/v1/"+pkg+"/note/"+n1, viewer.token, nil)
		}, http.StatusOK},
		{"the records list", "a role's rule", func(pkg string) answer {
			return do(http.MethodGet, listOf(pkg), viewer.token, nil)
		}, http.StatusOK},
		{"the change feed", "a role's rule", func(pkg string) answer {
			return do(http.MethodGet, changesOf(pkg), viewer.token, nil)
		}, http.StatusOK},
		{"a write by address", "a role's rule", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/"+pkg+"/note/"+n1, viewer.token,
				map[string]any{"properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		{"a patch by address", "a role's rule", func(pkg string) answer {
			return do(http.MethodPatch, "/api/v1/"+pkg+"/note/"+n1, viewer.token,
				map[string]any{"properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		{"a delete by address", "a role's rule", func(pkg string) answer {
			return do(http.MethodDelete, "/api/v1/"+pkg+"/note/"+n1, viewer.token, nil)
		}, http.StatusForbidden},
		{"a create", "a role's rule", func(pkg string) answer {
			return do(http.MethodPost, recordsPath, viewer.token,
				map[string]any{"kind": pkg + "/note", "properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		// A property's `writer:`: only a function writes `stamp`.
		{"a write by address", "a property's writer", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/"+pkg+"/note/"+n1, env.token,
				map[string]any{"properties": map[string]any{"stamp": "by hand"}})
		}, http.StatusForbidden},
		{"a create", "a property's writer", func(pkg string) answer {
			return do(http.MethodPost, recordsPath, env.token,
				map[string]any{"kind": pkg + "/note", "properties": map[string]any{"stamp": "by hand"}})
		}, http.StatusForbidden},
		// The owner's own writes by address, each checked in what it stored.
		{"a patch by address", "the owner's write", func(pkg string) answer {
			id := mintedID("patched-" + key(pkg))
			a := do(http.MethodPatch, "/api/v1/"+pkg+"/note/"+id, env.token,
				map[string]any{"properties": map[string]any{"name": "patched " + key(pkg)}})
			if rec := env.get(t, "~/notes/note", id); rec == nil || rec.Properties["name"] != "patched "+key(pkg) {
				return answer{status: 0}
			}
			return a
		}, http.StatusOK},
		{"a delete by address", "the owner's write", func(pkg string) answer {
			id := mintedID("deleted-" + key(pkg))
			a := do(http.MethodDelete, "/api/v1/"+pkg+"/note/"+id, env.token, nil)
			if rec := env.get(t, "~/notes/note", id); rec != nil && rec.DeletedAt == nil {
				return answer{status: 0}
			}
			return a
		}, http.StatusOK},
		// A record's marks: pin and release a property.
		{"a pin", "the owner's write", func(pkg string) answer {
			return do(http.MethodPost, "/api/v1/"+pkg+"/note/"+n1+"/pin", env.token, map[string]any{"properties": []any{"name"}})
		}, http.StatusOK},
		{"a release", "the owner's write", func(pkg string) answer {
			return do(http.MethodPost, "/api/v1/"+pkg+"/note/"+n1+"/release", env.token, map[string]any{"properties": []any{"name"}})
		}, http.StatusOK},
		{"a pin", "a role's rule", func(pkg string) answer {
			return do(http.MethodPost, "/api/v1/"+pkg+"/note/"+n1+"/pin", viewer.token, map[string]any{"properties": []any{"name"}})
		}, http.StatusForbidden},
		// A reference's pin: the link's `to` takes a note, in either spelling.
		{"a reference value", "a reference's pin", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/~/notes/link/"+mintedID("link-"+key(pkg)), env.token,
				map[string]any{"properties": map[string]any{"to": map[string]any{"ref": pkg + "/note/" + n1}}})
		}, http.StatusCreated},
		{"a reference value", "a reference's pin, to another kind", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/~/notes/link/"+mintedID("bad-"+key(pkg)), env.token,
				map[string]any{"properties": map[string]any{"to": map[string]any{"ref": pkg + "/link/" + n1}}})
		}, http.StatusUnprocessableEntity},
		// The assistant's door: a tool names the kind, and the route behind
		// it reads or writes it as the assistant's person.
		{"an MCP tool's read", "the assistant's reach", func(pkg string) answer {
			return tool("query", map[string]any{"kind": pkg + "/note", "id": n1})
		}, http.StatusOK},
		{"an MCP tool's write", "the assistant's reach", func(pkg string) answer {
			return tool("write", map[string]any{
				"op": "put", "kind": pkg + "/note", "id": mintedID("mcp-" + key(pkg)), "properties": map[string]any{"name": "by the assistant"},
			})
		}, http.StatusOK},
		// A function's grant: its put effect and its host list name the kind
		// in the spelling its caller hands it.
		{"a function's put effect and host list", "the function's grant", func(pkg string) answer {
			id := mintedID("scribe-" + key(pkg))
			rec := env.do(t, http.MethodPost, callPath(scribe), env.token, map[string]any{
				"input": map[string]any{"kind": pkg + "/note", "id": id},
			})
			if rec.Code != http.StatusOK {
				return answer{status: rec.Code}
			}
			var called struct {
				Output struct {
					Kinds []string `json:"kinds"`
				} `json:"output"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &called)
			if env.get(t, "~/notes/note", id) == nil || len(called.Output.Kinds) != 1 {
				return answer{status: 0}
			}
			return answer{status: rec.Code, kind: called.Output.Kinds[0]}
		}, http.StatusOK},
		// The catalog takes a package, never a kind: a package of the
		// repository's own is in no catalog, in either spelling.
		{"the catalog's add", "a package of the repository's own", func(pkg string) answer {
			return do(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(pkg)+"/add", env.token, nil)
		}, http.StatusNotFound},
		{"the catalog's entry", "a package of the repository's own", func(pkg string) answer {
			return do(http.MethodGet, "/api/v1/catalog/"+url.PathEscape(pkg), env.token, nil)
		}, http.StatusNotFound},
	}
	for _, c := range cells {
		got := map[string]answer{}
		for spelling, pkg := range spellings {
			got[spelling] = c.drive(pkg)
		}
		if got["~"].status != got["full"].status {
			t.Errorf("%s under %s: `~` answers %d and the full name %d", c.door, c.check, got["~"].status, got["full"].status)
		}
		if got["~"].status != c.want {
			t.Errorf("%s under %s: answers %d, want %d", c.door, c.check, got["~"].status, c.want)
		}
		for spelling, a := range got {
			if a.kind != "" && !strings.HasPrefix(a.kind, "~/") {
				t.Errorf("%s under %s, spelled %s: answers the kind %s, want it under ~", c.door, c.check, spelling, a.kind)
			}
		}
	}

	// A change policy's selector, stored in either spelling, decides the
	// same: the scribe's put of a note is held as a request, the note
	// unwritten. The scribe is named one way; only the kind's spelling moves.
	for spelling, pkg := range spellings {
		policy := "/api/v1/substrate.reamde.dev/core/recordpatchpolicy/both-spellings-" + key(pkg)
		wantStatus(t, env.do(t, http.MethodPut, policy, env.token, map[string]any{
			"properties": map[string]any{"action": "gate", "selector": map[string]any{
				"kinds": []any{pkg + "/note"}, "functions": []any{scribe},
			}},
		}), http.StatusCreated)
		id := mintedID("held-" + key(pkg))
		call := env.do(t, http.MethodPost, callPath(scribe), env.token, map[string]any{
			"input": map[string]any{"kind": "~/notes/note", "id": id},
		})
		if call.Code != http.StatusOK {
			t.Errorf("under the selector spelled %s, the scribe's call answers %d %s", spelling, call.Code, call.Body.String())
		}
		if env.get(t, "~/notes/note", id) != nil {
			t.Errorf("under the selector spelled %s, the scribe's put landed, want it held", spelling)
		}
		held, err := env.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
			Kinds:      []string{vocabulary.KindRecordPatchRequest},
			Properties: map[string]substrate.Cond{"targetId": {Eq: id}},
		}})
		if err != nil || len(held.Records) != 1 || held.Records[0].Properties["targetKind"] != "~/notes/note" {
			t.Errorf("under the selector spelled %s, the requests holding the put are %+v, %v: want one, of ~/notes/note", spelling, held, err)
		}
		wantStatus(t, env.do(t, http.MethodDelete, policy, env.token, nil), http.StatusOK)
	}

	// One declaration naming an own kind, sent to the apply door in either
	// spelling, is one stored declaration: the second send changes nothing.
	var stored []*substrate.Record
	for _, spelling := range []string{"~", "full"} {
		pkg := spellings[spelling]
		tag := vocabulary.KindManifest(pkg, map[string]any{"singular": "tag"}, map[string]any{
			"properties": map[string]any{"on": map[string]any{"type": "reference", "kind": pkg + "/note"}},
		})
		wantStatus(t, env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", env.token, map[string]any{"documents": []any{tag}}), http.StatusOK)
		rec := env.get(t, vocabulary.CoreKind(vocabulary.DocKind), "~/notes/tag")
		if rec == nil {
			t.Fatalf("the tag sent spelled %s is not stored under ~", spelling)
		}
		stored = append(stored, rec)
	}
	if a, b := stored[0], stored[1]; a.Version != b.Version || !jsonEqualForTest(a.Properties, b.Properties) {
		t.Errorf("one declaration sent in both spellings is stored as\n  %d %v\n  %d %v", a.Version, a.Properties, b.Version, b.Properties)
	}

	// A control kind is core's: it has one spelling, and a viewer reads none.
	rec := env.do(t, http.MethodGet, recordsPath+"?filter="+url.QueryEscape(`{"kinds":["`+vocabulary.CoreKind("token")+`"]}`), viewer.token, nil)
	if rec.Code == http.StatusOK {
		if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 0 {
			t.Errorf("a viewer lists %d tokens", len(page.Records))
		}
	}
}

// AN AGENT'S GRANT on an own kind, declared in either spelling, admits the
// agent's reads and writes of the kind named in either spelling, and refuses
// a kind outside it. Not parallel: the in-process provider needs the egress
// escape set (appAgentsLLM).
func TestBothSpellingsOfAnOwnKindMeetAnAgentsGrant(t *testing.T) {
	env := newDBEnv(t)
	llm := appAgentsLLM(t, env)
	full := env.repository + "/notes"
	agent := func(name, granted string) map[string]any {
		return vocabulary.AgentManifest(full, name, map[string]any{
			"description": name + " under test", "prompt": "You are " + name + ".",
			"provider": appAgentsLLMProvider, "model": name,
			"tools": []any{
				map[string]any{"function": vocabulary.HostFunctionQuery},
				map[string]any{"function": vocabulary.HostFunctionWrite},
			},
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{granted}}, "writes": []any{granted}},
		})
	}
	appAgentsApply(t, env, append(notesDeclarations(full), agent("tilde", "~/notes/note"), agent("named", full+"/note"))...)
	n1 := mintedID("both-spellings-n1")
	env.put(t, "~/notes/note", n1, map[string]any{"name": "first"})
	spellings := spellingsOf(full)

	type outcome struct {
		ok   map[string]bool
		read map[string]bool
	}
	got := map[string]outcome{}
	for _, name := range []string{"tilde", "named"} {
		var calls [][2]string
		var names []string
		for _, spelling := range []string{"~", "full"} {
			pkg := spellings[spelling]
			calls = append(calls,
				[2]string{"query", `{"kind":"` + pkg + `/note","id":"` + n1 + `"}`},
				[2]string{"write", `{"op":"put","kind":"` + pkg + `/note","id":"` + mintedID(name+"-"+spelling) + `","input":{"properties":{"name":"by ` + name + `"}}}`},
			)
			names = append(names, "read "+spelling, "write "+spelling)
		}
		calls = append(calls, [2]string{"write", `{"op":"put","kind":"~/notes/link","id":"` + mintedID(name+"-link") + `","input":{"properties":{}}}`})
		names = append(names, "write outside")
		llm.script(name, appAgentsTurn{calls: calls}, appAgentsTurn{content: "done"})
		res, err := env.ds.ChatAgent(context.Background(), substrate.ActorPerson, "~/notes/"+name, substrate.ChatInput{Message: "go"}, func(substrate.AgentEvent) {})
		if err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
		page, err := env.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
			Kinds: []string{kindMessage}, Properties: map[string]substrate.Cond{"thread": {Eq: res.Thread}, "role": {Eq: "tool"}},
		}, First: 50})
		if err != nil {
			t.Fatal(err)
		}
		o := outcome{ok: map[string]bool{}, read: map[string]bool{}}
		for _, m := range page.Records {
			// The in-process provider ids its calls `call_<n>`, in order.
			callID, _ := m.Properties["toolCallId"].(string)
			i, err := strconv.Atoi(strings.TrimPrefix(callID, "call_"))
			if err != nil || i >= len(names) {
				t.Fatalf("%s's tool message %+v", name, m.Properties)
			}
			o.ok[names[i]] = m.Properties["ok"] == true
			content, _ := m.Properties["content"].(string)
			o.read[names[i]] = strings.Contains(content, n1) && strings.Contains(content, "~/notes/note")
		}
		got[name] = o
		for _, spelling := range []string{"~", "full"} {
			if env.get(t, "~/notes/note", mintedID(name+"-"+spelling)) == nil {
				t.Errorf("%s's write of the note spelled %s did not land", name, spelling)
			}
		}
		if env.get(t, "~/notes/link", mintedID(name+"-link")) != nil {
			t.Errorf("%s wrote a link, outside its grant", name)
		}
	}
	want := outcome{
		ok:   map[string]bool{"read ~": true, "write ~": true, "read full": true, "write full": true, "write outside": false},
		read: map[string]bool{"read ~": true, "write ~": false, "read full": true, "write full": false, "write outside": false},
	}
	for name, o := range got {
		if !reflect.DeepEqual(o, want) {
			t.Errorf("the agent granted the note spelled %s answers %+v, want %+v", name, o, want)
		}
	}
}

// AN APP'S AUTHORIZATION asking a scope on an own kind, in either spelling,
// shows the owner the same scope reaching the same kind, and mints the same
// grant, which reads the kind.
func TestBothSpellingsOfAnOwnKindAskOneScope(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t)
	full := e.db.repository + "/notes"
	appAgentsApply(t, e.db, notesDeclarations(full)...)
	n1 := mintedID("both-spellings-n1")
	e.db.put(t, "~/notes/note", n1, map[string]any{"name": "first"})

	type asked struct {
		scopes, verbs []string
		kinds         [][]string
		minted        []string
		read          int
	}
	got := map[string]asked{}
	for spelling, pkg := range spellingsOf(full) {
		id := e.park(appRequest("read:" + pkg + "/note write:" + pkg + "/note"))
		view := e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
		wantStatus(t, view, http.StatusOK)
		var a asked
		for _, s := range decodeJSON[substrate.AuthorizationRequest](t, view).Scopes {
			a.scopes = append(a.scopes, s.Scope)
			a.verbs = append(a.verbs, s.Verb)
			var ks []string
			for _, k := range s.Kinds {
				ks = append(ks, k.Identity)
			}
			a.kinds = append(a.kinds, ks)
		}
		approved := e.approveShown(id, a.scopes, 30)
		wantStatus(t, approved, http.StatusOK)
		tok := e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, approved).Redirect)))
		wantStatus(t, tok, http.StatusOK)
		secret := decodeJSON[tokenResponse](t, tok).AccessToken
		a.minted = e.token(secret).Scopes
		a.read = e.do(http.MethodGet, "/api/v1/~/notes/note/"+n1, secret, nil, "").Code
		got[spelling] = a
	}
	if !reflect.DeepEqual(got["~"], got["full"]) {
		t.Errorf("an authorization asked in `~` is %+v and in the full name %+v", got["~"], got["full"])
	}
	want := asked{
		scopes: []string{"read:~/notes/note", "write:~/notes/note"}, verbs: []string{"read", "write"},
		kinds: [][]string{{"~/notes/note"}, {"~/notes/note"}}, minted: []string{"read:~/notes/note", "write:~/notes/note"},
		read: http.StatusOK,
	}
	if !reflect.DeepEqual(got["~"], want) {
		t.Errorf("an authorization asked in `~` is %+v, want %+v", got["~"], want)
	}
}

// A PUBLISHER'S NAMES UNDER THE REPOSITORY'S NAME. The repository holds a
// package `pub` publishes and is renamed `pub`, its early refusal lifted: the
// name does not spell `~` while the package is held, so on every door the
// published kind is the publisher's, a reference stored in full keeps
// pointing at it, the repository's own kinds answer under `~` and under the
// name it had, and `pub` spelling one of them names nothing.
func TestAPublishersNamesKeepTheirMeaningUnderTheRepositorysName(t *testing.T) {
	t.Parallel()
	const password = "correct-horse-battery-staple"
	env := newDBEnv(t, withPassword(password), withEngine(engine.WithTestNameDoorsOpen()))
	ctx := context.Background()
	old, pub := env.repository, testdb.RepositoryLabel(t)+"-pub.example.com"
	task := pub + "/tasks/task"
	if _, err := env.ds.InstallBundleClosure(ctx, substrate.BundleActor(pub, "tasks"), []map[string]any{
		vocabulary.PackageManifest(pub+"/tasks", 1),
		vocabulary.KindManifest(pub+"/tasks", map[string]any{"singular": "task"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.BundleManifest(pub+"/tasks", map[string]any{"description": "a package pub publishes", "installs": []any{task}}),
	}, nil, substrate.BundleInstall{Published: true}); err != nil {
		t.Fatalf("install %s/tasks: %v", pub, err)
	}
	if _, err := env.ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, append(notesDeclarations(old+"/notes"),
		vocabulary.KindManifest(old+"/notes", map[string]any{"singular": "pin"}, map[string]any{
			"properties": map[string]any{"about": map[string]any{"type": "reference", "kind": task}},
		}))); err != nil {
		t.Fatalf("declare the notes: %v", err)
	}
	t1, n1, p1 := mintedID("held-task"), mintedID("held-note"), mintedID("held-pin")
	env.put(t, task, t1, map[string]any{"name": "published"})
	env.put(t, "~/notes/note", n1, map[string]any{"name": "own"})
	env.put(t, "~/notes/pin", p1, map[string]any{"about": map[string]any{"ref": task + "/" + t1}})
	if _, err := env.svc.RenameRepository(ctx, substrate.LoginInput{Identity: old, Repository: old, Password: password}, pub); err != nil {
		t.Fatalf("rename onto %s with its refusal lifted: %v", pub, err)
	}
	if names := env.ds.Repository().LocalNames(); !reflect.DeepEqual(names, []string{old}) {
		t.Fatalf("the repository spells ~ with %v, want its former name alone", names)
	}
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	app := appScopedToken(t, env, "read:"+task)
	_, assistant := mcpAssistantToken(t, env)
	type answer struct {
		status int
		kind   string
	}
	read := func(body []byte, status int) answer {
		var got struct {
			Kind    string `json:"kind"`
			Records []struct {
				Kind string `json:"kind"`
			} `json:"records"`
			Changes []struct {
				Kind string `json:"kind"`
			} `json:"changes"`
		}
		_ = json.Unmarshal(body, &got)
		kind := got.Kind
		for _, r := range got.Records {
			kind = r.Kind
		}
		for _, c := range got.Changes {
			kind = c.Kind
		}
		return answer{status: status, kind: kind}
	}
	do := func(method, path, token string, body any) answer {
		rec := env.do(t, method, path, token, body)
		return read(rec.Body.Bytes(), rec.Code)
	}
	list := func(kind string) string {
		return recordsPath + "?filter=" + url.QueryEscape(`{"kinds":["`+kind+`"]}`)
	}
	for _, c := range []struct {
		door string
		got  answer
		want answer
	}{
		{"a record's address", do(http.MethodGet, "/api/v1/"+task+"/"+t1, env.token, nil), answer{http.StatusOK, task}},
		{"the records list", do(http.MethodGet, list(task), env.token, nil), answer{http.StatusOK, task}},
		{"the change feed", do(http.MethodGet, "/api/v1/changes?first=50&recordKind="+url.QueryEscape(task)+"&recordId="+t1, env.token, nil), answer{http.StatusOK, task}},
		{"a write by address", do(http.MethodPut, "/api/v1/"+task+"/"+mintedID("held-put"), env.token, map[string]any{"properties": map[string]any{"name": "put"}}), answer{http.StatusCreated, task}},
		{"a create", do(http.MethodPost, recordsPath, env.token, map[string]any{"kind": task, "properties": map[string]any{"name": "posted"}}), answer{http.StatusCreated, task}},
		{"a patch by address", do(http.MethodPatch, "/api/v1/"+task+"/"+t1, env.token, map[string]any{"properties": map[string]any{"name": "patched"}}), answer{http.StatusOK, task}},
		{"an app's scope", do(http.MethodGet, "/api/v1/"+task+"/"+t1, app, nil), answer{http.StatusOK, task}},
		{"a viewer's role", do(http.MethodGet, list(task), viewer.token, nil), answer{http.StatusOK, task}},
		{"the repository's own kind under ~", do(http.MethodGet, "/api/v1/~/notes/note/"+n1, env.token, nil), answer{http.StatusOK, "~/notes/note"}},
		{"the repository's own kind under its former name", do(http.MethodGet, "/api/v1/"+old+"/notes/note/"+n1, env.token, nil), answer{http.StatusOK, "~/notes/note"}},
		{"a write of its own kind under ~", do(http.MethodPut, "/api/v1/~/notes/note/"+mintedID("held-own"), env.token, map[string]any{"properties": map[string]any{"name": "own"}}), answer{http.StatusCreated, "~/notes/note"}},
	} {
		if c.got != c.want {
			t.Errorf("%s: answers %+v, want %+v", c.door, c.got, c.want)
		}
	}
	// The name spelling one of the repository's own kinds names nothing.
	if a := do(http.MethodGet, "/api/v1/"+pub+"/notes/note/"+n1, env.token, nil); a.status == http.StatusOK {
		t.Errorf("%s/notes/note answers %+v: it folded to ~ under a name the repository holds a package under", pub, a)
	}
	// The assistant's door names the published kind as written.
	text, isErr := callTool(t, env.h, assistant, "query", map[string]any{"kind": task, "id": t1})
	if isErr || !strings.Contains(text, `"kind":"`+task+`"`) {
		t.Errorf("an MCP query of %s answers %s", task, text)
	}
	// A reference stored in full keeps pointing at the published record, and
	// one written in full now lands as written.
	if got := env.get(t, "~/notes/pin", p1); got == nil || got.Properties["about"].(map[string]any)["ref"] != task+"/"+t1 {
		t.Errorf("the pin stored before the rename reads %v", got)
	}
	p2 := mintedID("held-pin-after")
	wantStatus(t, env.do(t, http.MethodPut, "/api/v1/~/notes/pin/"+p2, env.token,
		map[string]any{"properties": map[string]any{"about": map[string]any{"ref": task + "/" + t1}}}), http.StatusCreated)
	if got := env.get(t, "~/notes/pin", p2); got == nil || got.Properties["about"].(map[string]any)["ref"] != task+"/"+t1 {
		t.Errorf("a pin written after the rename reads %v", got)
	}
	// A declaration and a policy naming the published kind keep its name.
	tag := vocabulary.KindManifest("~/notes", map[string]any{"singular": "tag"}, map[string]any{
		"properties": map[string]any{"on": map[string]any{"type": "reference", "kind": task}},
	})
	wantStatus(t, env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", env.token, map[string]any{"documents": []any{tag}}), http.StatusOK)
	if decl := env.get(t, vocabulary.CoreKind(vocabulary.DocKind), "~/notes/tag"); decl == nil || !strings.Contains(jsonOf(decl.Properties), `"kind":"`+task+`"`) {
		t.Errorf("the tag's declaration reads %v, want its reference to %s as written", decl, task)
	}
	rec := env.do(t, http.MethodPut, "/api/v1/substrate.reamde.dev/core/recordpatchpolicy/held", env.token, map[string]any{
		"properties": map[string]any{"action": "gate", "selector": map[string]any{"kinds": []any{task}}},
	})
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeJSON[substrate.Record](t, rec); !strings.Contains(jsonOf(got.Properties["selector"]), task) {
		t.Errorf("the policy's selector reads %v, want %s as written", got.Properties["selector"], task)
	}
}

func jsonOf(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func jsonEqualForTest(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
