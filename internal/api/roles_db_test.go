package api

// A person who is not an admin, over HTTP on the real engine (decisions 0275,
// 0276, 0294): held to their roles by the person's view for every read and
// every write, refused the routes for an admin alone, and the engine's one
// check answering their writes as the view does.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const rolesPassword = "a-password-for-the-role-tests"

// rolePerson is one person of the repository, signed in.
type rolePerson struct {
	name, identity, token, tokenID string
}

// addPerson registers a person and adds them to the env's repository holding
// role, signed in there. The env's repository is created with a first person
// who names nobody, its admin; the first person added is that admin, so the
// one asked for is another.
func addPerson(t *testing.T, env *dbEnv, label, role string) rolePerson {
	t.Helper()
	if label != "admin" {
		addPerson(t, env, "admin", scope.RoleAdmin)
	}
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

// noteKind declares a kind of the repository's own and answers it.
func noteKind(t *testing.T, env *dbEnv) string {
	t.Helper()
	pkg := env.repository + "/notes"
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare the note kind: %v", err)
	}
	return "~/notes/note"
}

func TestAViewerReadsAndIsRefusedEveryWrite(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	env.put(t, note, "one", map[string]any{"name": "one"})
	viewer := addPerson(t, env, "v", scope.RoleViewer)

	wantStatus(t, env.do(t, http.MethodGet, recordPath(note, "one"), viewer.token, nil), http.StatusOK)
	writes := map[string]func() int{
		"create a note": func() int {
			return env.do(t, http.MethodPost, recordsPath, viewer.token, map[string]any{"kind": note, "properties": map[string]any{"name": "two"}}).Code
		},
		"patch a note": func() int {
			return env.do(t, http.MethodPatch, recordPath(note, "one"), viewer.token, map[string]any{"properties": map[string]any{"name": "uno"}}).Code
		},
		"delete a note": func() int {
			return env.do(t, http.MethodDelete, recordPath(note, "one"), viewer.token, nil).Code
		},
		"put a trigger": func() int {
			return env.do(t, http.MethodPut, recordPath(vocabulary.CoreKind("trigger"), "t1"), viewer.token, map[string]any{"properties": map[string]any{
				"source": map[string]any{"webhook": map[string]any{}}, "callable": vocabulary.RecordPath(kindFunction, "x.example.com/p/f"),
			}}).Code
		},
	}
	for what, write := range writes {
		if code := write(); code != http.StatusForbidden && code != http.StatusNotFound {
			t.Errorf("a viewer's %s answered %d, want refused", what, code)
		}
	}
	if got := env.get(t, note, "one"); got.Properties["name"] != "one" {
		t.Fatalf("the note after the viewer's writes: %v", got.Properties)
	}
	// A route for an admin alone refuses them by a rule.
	rec := env.do(t, http.MethodGet, "/api/v1/export", viewer.token, nil)
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
	if env := decodeJSON[substrate.ErrorEnvelope](t, rec); env.Error.Reason != substrate.ReasonNotPermitted {
		t.Fatalf("the export refused a viewer with reason %q, want not_permitted", env.Error.Reason)
	}
	// Their own sign-ins are theirs whatever their role.
	wantStatus(t, env.do(t, http.MethodPost, "/tokens", viewer.token, map[string]any{"label": "laptop"}), http.StatusCreated)
	listed := decodeJSON[struct {
		Items []substrate.TokenInfo `json:"items"`
	}](t, env.do(t, http.MethodGet, "/tokens", viewer.token, nil)).Items
	for _, tok := range listed {
		if tok.Identity != viewer.identity {
			t.Fatalf("a viewer's token list holds %s's token %s", tok.Identity, tok.ID)
		}
	}
	if len(listed) != 2 {
		t.Fatalf("a viewer's token list = %d tokens, want their two", len(listed))
	}
}

func TestAnEditorWritesWhatIsNotAControlKind(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	editor := addPerson(t, env, "e", scope.RoleEditor)

	wantStatus(t, env.do(t, http.MethodPost, recordsPath, editor.token, map[string]any{"kind": note, "properties": map[string]any{"name": "two"}}), http.StatusCreated)
	for what, path := range map[string]string{
		"a trigger":  recordPath(vocabulary.CoreKind("trigger"), "t1"),
		"a provider": recordPath(vocabulary.LLMKind("provider"), "p1"),
		"a policy":   recordPath(vocabulary.KindRecordPatchPolicy, "p1"),
		"a kind":     recordPath(vocabulary.CoreKind("kind"), env.repository+"/notes/memo"),
	} {
		// A kind their rules do not read is answered as one the repository
		// never declared, as for an app's token.
		rec := env.do(t, http.MethodPut, path, editor.token, map[string]any{"properties": map[string]any{"wire": "openai"}})
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("an editor's put of %s answered %d, want refused (body %s)", what, rec.Code, rec.Body.String())
		}
	}
}

// A person who is not an admin reads through the view alone: a record of a
// control kind or of sign-in material is answered as one the repository
// never declared, and the list of every kind, a search and the history hold
// none of them, while what their rules read is read, its declaration
// included.
func TestAPersonReadsOnlyWhatTheirRolesRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	note := noteKind(t, env)
	env.put(t, note, "one", map[string]any{"name": "anthropic notes"})
	env.put(t, vocabulary.LLMKind("provider"), "anthropic", map[string]any{"wire": "anthropic", "apiKey": "a-key-an-editor-never-reads"})
	editor := addPerson(t, env, "e", scope.RoleEditor)
	kinds, err := env.ds.Kinds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	control := map[string]bool{}
	for _, k := range kinds {
		if k.Control != "" {
			control[k.Identity] = true
		}
	}
	for _, k := range []string{vocabulary.LLMKind("provider"), vocabulary.CoreKind("token"), vocabulary.CoreKind("role"), vocabulary.CoreKind("actor"), vocabulary.CoreKind("kind")} {
		if !control[k] {
			t.Fatalf("%s is not a control kind; the test reads the wrong list", k)
		}
	}

	wantStatus(t, env.do(t, http.MethodGet, recordPath(note, "one"), editor.token, nil), http.StatusOK)
	wantStatus(t, env.do(t, http.MethodGet, recordPath(vocabulary.LLMKind("provider"), "anthropic"), env.token, nil), http.StatusOK)
	for what, path := range map[string]string{
		"a provider":                 recordPath(vocabulary.LLMKind("provider"), "anthropic"),
		"their own token":            recordPath(vocabulary.CoreKind("token"), editor.tokenID),
		"the admin role":             recordPath(vocabulary.CoreKind("role"), scope.RoleAdmin),
		"the provider's declaration": recordPath(vocabulary.CoreKind("kind"), vocabulary.LLMKind("provider")),
	} {
		if rec := env.do(t, http.MethodGet, path, editor.token, nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("an editor's get of %s answered %d, want refused (body %s)", what, rec.Code, rec.Body.String())
		}
	}
	// The declaration of a kind they read says what they already see of its
	// records, as the kinds route does, and as for an app's token.
	wantStatus(t, env.do(t, http.MethodGet, recordPath(vocabulary.CoreKind("kind"), note), editor.token, nil), http.StatusOK)
	// The list of declarations holds the kinds they read, and no control
	// kind's (TestAViewerListsTheKindsTheyRead walks it whole).
	rec := env.do(t, http.MethodGet, "/api/v1/records?first=500&filter="+url.QueryEscape(`{"kinds":["`+vocabulary.CoreKind("kind")+`"]}`), editor.token, nil)
	wantStatus(t, rec, http.StatusOK)
	var declared []string
	for _, r := range decodeJSON[substrate.Page](t, rec).Records {
		declared = append(declared, r.ID)
	}
	if !slices.Contains(declared, note) || slices.Contains(declared, vocabulary.LLMKind("provider")) {
		t.Errorf("an editor's list of the declarations: %v, want the note's and no provider's", declared)
	}
	for _, kind := range []string{vocabulary.CoreKind("token"), vocabulary.CoreKind("actor"), vocabulary.CoreKind("role"), vocabulary.CoreKind("setting")} {
		rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["`+kind+`"]}`), editor.token, nil)
		if rec.Code == http.StatusOK {
			if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 0 {
				t.Errorf("an editor's list of %s holds %d records", kind, len(page.Records))
			}
			continue
		}
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("an editor's list of %s answered %d, want refused (body %s)", kind, rec.Code, rec.Body.String())
		}
	}

	// Every kind, page by page: the note, and nothing of a control kind.
	sawNote := false
	for cursor, pages := "", 0; ; pages++ {
		if pages > 100 {
			t.Fatal("the list of every kind did not end")
		}
		path := "/api/v1/records?first=200"
		if cursor != "" {
			path += "&after=" + url.QueryEscape(cursor)
		}
		rec := env.do(t, http.MethodGet, path, editor.token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[substrate.Page](t, rec)
		for _, r := range page.Records {
			if control[r.Kind] {
				t.Fatalf("the editor's list of every kind holds %s/%s", r.Kind, r.ID)
			}
			sawNote = sawNote || r.Kind == note
		}
		if page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	if !sawNote {
		t.Fatal("the editor's list of every kind does not hold the note")
	}

	rec = env.do(t, http.MethodGet, recordsPath+"?q=anthropic", editor.token, nil)
	wantStatus(t, rec, http.StatusOK)
	hits := decodeJSON[substrate.RankedPage](t, rec)
	for _, r := range hits.Records {
		if control[r.Kind] {
			t.Fatalf("the editor's search holds %s/%s", r.Kind, r.ID)
		}
	}
	if len(hits.Records) == 0 {
		t.Fatal("the editor's search found not even the note")
	}

	head, err := env.ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, http.MethodGet, fmt.Sprintf("/api/v1/changes?first=%d", head.Seq), editor.token, nil)
	wantStatus(t, rec, http.StatusOK)
	changes := decodeJSON[struct {
		Changes []substrate.Change `json:"changes"`
	}](t, rec).Changes
	for _, c := range changes {
		if control[c.Kind] {
			t.Fatalf("the editor's history holds change %d of %s", c.Seq, c.Kind)
		}
	}
	if len(changes) == 0 {
		t.Fatal("the editor's history is empty: the test proves nothing")
	}
}

// The view and the engine agree: a write the view refuses a person is
// refused by the engine's one check for the same person, and one it admits
// the engine admits.
func TestThePersonsViewAndTheEngineAgree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	note := noteKind(t, env)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	as := substrate.WithIdentity(ctx, editor.identity)
	cases := []struct {
		kind  string
		props map[string]any
	}{
		{note, map[string]any{"name": "agree"}},
		{vocabulary.LLMKind("provider"), map[string]any{"wire": "openai"}},
		{vocabulary.CoreKind("setting"), map[string]any{"value": "Europe/Athens"}},
	}
	for _, c := range cases {
		rec := env.do(t, http.MethodPost, recordsPath, editor.token, map[string]any{"kind": c.kind, "properties": c.props})
		// The view answers a kind the person does not read as one the
		// repository never declared.
		viewRefused := rec.Code == http.StatusForbidden || rec.Code == http.StatusNotFound
		_, err := env.ds.Put(as, substrate.ActorPerson, substrate.PutInput{Kind: c.kind, Properties: c.props})
		engineRefused := errors.Is(err, substrate.ErrNotPermitted)
		if viewRefused != engineRefused {
			t.Errorf("%s: the view refused %v (%d), the engine refused %v (%v)", c.kind, viewRefused, rec.Code, engineRefused, err)
		}
	}
}

// An app an editor approved for writing everything writes no trigger, and
// writes what the editor may.
func TestAnAppAnEditorApprovedWritesNoTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	note := noteKind(t, env)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	_, app, err := env.ds.MintToken(substrate.WithIdentity(ctx, editor.identity), substrate.MintTokenInput{
		Label: "Notes", Scopes: []string{"write:*"}, ClientID: appClientID, ClientName: "Notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, env.do(t, http.MethodPost, recordsPath, app, map[string]any{"kind": note, "properties": map[string]any{"name": "x"}}), http.StatusCreated)
	rec := env.do(t, http.MethodPut, recordPath(vocabulary.CoreKind("trigger"), "t1"), app, map[string]any{"properties": map[string]any{
		"source": map[string]any{"webhook": map[string]any{}}, "callable": vocabulary.RecordPath(kindFunction, "x.example.com/p/f"),
	}})
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("an app an editor approved put a trigger: %d (body %s)", rec.Code, rec.Body.String())
	}
}

// A role changed or taken away holds from the next request of a token that
// already exists.
func TestARoleChangeHoldsFromTheNextRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	note := noteKind(t, env)
	bo := addPerson(t, env, "b", scope.RoleEditor)
	put := func(name string) int {
		return env.do(t, http.MethodPost, recordsPath, bo.token, map[string]any{"kind": note, "properties": map[string]any{"name": name}}).Code
	}
	wantCode := func(id string, want int) {
		t.Helper()
		if got := put(id); got != want {
			t.Fatalf("put %s: %d, want %d", id, got, want)
		}
	}
	wantCode("as-an-editor", http.StatusCreated)
	if err := env.svc.(engine.Operator).SetPersonRoles(ctx, bo.name, env.repository, scope.RoleViewer); err != nil {
		t.Fatal(err)
	}
	wantCode("as-a-viewer", http.StatusForbidden)
	if err := env.svc.(engine.Operator).SetPersonRoles(ctx, bo.name, env.repository, scope.RoleEditor, scope.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	wantCode("as-an-admin-again", http.StatusCreated)
	wantStatus(t, env.do(t, http.MethodGet, "/api/v1/export", bo.token, nil), http.StatusOK)
}

// An app an editor approved is held by its own view over the editor's, and
// calls a function both cover: the declaration it is held to is read
// through the engine, not through the editor's view, which reads no
// declaration.
func TestAnAppAnEditorApprovedCallsAFunctionBothCover(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newDBEnv(t)
	note := noteKind(t, env)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	tools := env.repository + "/tools"
	fn := "~/tools/touch"
	appAgentsApply(t, env,
		vocabulary.PackageManifest(tools, 1),
		agentReachFunction(tools, "touch", map[string]any{
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{note}}, "writes": []any{note}},
		}),
	)
	as := substrate.WithIdentity(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson), editor.identity)
	held, err := env.ds.PersonRoles(as)
	if err != nil {
		t.Fatal(err)
	}
	person := newPersonDataset(env.ds, rolesOf(held, env.ds.Repository().Authority))
	app := substrate.Actor("app:notes.example.org")
	for _, c := range []struct {
		scopes []string
		calls  bool
	}{
		{[]string{"call:" + fn, "write:" + note}, true},
		{[]string{"call:" + fn, "read:" + note}, false},
	} {
		grant, err := scope.NewGrant(c.scopes)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = newScopedDataset(person, grant, app).CallFunction(as, app, fn, nil)
		if (err == nil) != c.calls {
			t.Errorf("an editor's app under %v calls %s: %v, want called %v", c.scopes, fn, err, c.calls)
		}
	}
}

// An editor renames a thread of an agent they cannot talk to: their role
// writes the kind whole, so the patch is an ordinary write, not an app's
// agent verb.
func TestAnEditorRenamesAThreadTheirRoleWrites(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	editor := addPerson(t, env, "e", scope.RoleEditor)
	chat := env.repository + "/chat"
	env.put(t, llmPackage+"/provider", appAgentsLLMProvider, map[string]any{"wire": "openai", "baseURL": "https://llm.example.com", "apiKey": "row-key"})
	appAgentsApply(t, env,
		vocabulary.PackageManifest(chat, 1),
		agentReachAgent(chat, "wide", map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{"*"}}}}),
	)
	env.put(t, kindThread, "t1", map[string]any{
		"agent": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindAgent, chat+"/wide")}, "subject": "first", "mode": "chat", "status": "ok",
		"provider": appAgentsLLMProvider, "model": "wide", "agentDepth": 0, "startedAt": "2026-01-01T00:00:00Z",
	})
	rec := env.do(t, http.MethodPatch, recordPath(kindThread, "t1"), editor.token, map[string]any{"properties": map[string]any{"subject": "renamed"}})
	wantStatus(t, rec, http.StatusOK)
	if got := env.get(t, kindThread, "t1"); got.Properties["subject"] != "renamed" {
		t.Fatalf("the thread after the editor's rename: %v", got.Properties)
	}
}

// A person reads the history of a record their rules read with its values,
// by the route and the request an admin's console makes (values=1); an
// app's token reads names alone.
func TestAPersonReadsTheHistoryOfWhatTheyReadWithItsValues(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	note := noteKind(t, env)
	env.put(t, note, "one", map[string]any{"name": "first"})
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	path := "/api/v1/changes?before=0&first=10&values=1&recordKind=" + url.QueryEscape(note) + "&recordId=one"
	for who, token := range map[string]string{"the admin": env.token, "a viewer": viewer.token} {
		rec := env.do(t, http.MethodGet, path, token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[substrate.ChangePage](t, rec)
		if len(page.Changes) == 0 || page.Changes[0].Payload == nil {
			t.Fatalf("%s's history of the note carries no values: %+v", who, page)
		}
	}
}
