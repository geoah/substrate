package api

// A property's `writer:` names types of actor (decision 0290), on the real
// engine over HTTP: the apply door refuses each word it took before, naming
// the type that replaced it, and a write is admitted or refused by its
// actor's type, for each of the six types against each value.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// writerDocs is one package of the repository's own with one kind whose
// property `cursor` declares the writer given, as YAML would spell it.
func writerDocs(authority string, writer any) []map[string]any {
	pkg := authority + "/writers"
	return []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"cursor": map[string]any{"type": "string", "writer": writer},
			},
		}),
	}
}

func TestTheApplyDoorRefusesARetiredWriterNamingItsReplacement(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	for old, instead := range map[string]string{
		"owner":     "write person",
		"connector": "write [function, agent]",
		"oauth":     "write system",
	} {
		rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", env.token,
			map[string]any{"documents": writerDocs(env.repository, old)})
		wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
		refusal := decodeJSON[substrate.ErrorEnvelope](t, rec)
		if want := `writer "` + old + `" is retired: ` + instead; !strings.Contains(refusal.Error.Message, want) {
			t.Errorf("writer: %s: the refusal does not say %q: %s", old, want, refusal.Error.Message)
		}
	}
}

// actorAs is the seam for the hands no token is: it writes every Put and
// Patch a request reaches as the actor given, so the engine's own check
// decides what that type may write. A person and an app write with tokens of
// their own.
type actorAs struct {
	substrate.Dataset
	actor substrate.Actor
}

func (a actorAs) Put(ctx context.Context, _ substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	return a.Dataset.Put(ctx, a.actor, in)
}

func (a actorAs) Patch(ctx context.Context, _ substrate.Actor, kind, id string, in substrate.PatchInput) (*substrate.Record, error) {
	return a.Dataset.Patch(ctx, a.actor, kind, id, in)
}

// Each of the six types of actor writes a property declared for each value a
// `writer:` takes, one type, a list or none: admitted where the value names
// its type or names none, refused 403 otherwise, and the system everywhere.
// A refusal names the hand in words and its type, never the hand's spelling,
// because an accepted change request stores it.
func TestAWriteIsAdmittedByItsActorsType(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	authority := env.repository
	pkg := authority + "/writers"
	kind := pkg + "/note"
	values := map[string]any{
		"byPerson":   "person",
		"byApp":      "app",
		"byFunction": "function",
		"byAgent":    "agent",
		"byPackage":  "package",
		"bySystem":   "system",
		"byCode":     []any{"function", "agent"},
		"byAnyone":   nil,
	}
	props := map[string]any{"name": map[string]any{"type": "string"}}
	for name, w := range values {
		p := map[string]any{"type": "string"}
		if w != nil {
			p["writer"] = w
		}
		props[name] = p
	}
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{"displayTemplate": "{name}", "properties": props}),
	}); err != nil {
		t.Fatalf("declare the kind: %v", err)
	}
	app := env.mint(t, substrate.MintTokenInput{Label: "Notes", Scopes: []string{"read:" + kind, "write:" + kind}, ClientID: appClientID, ClientName: "Notes"})
	authorityOf, pkgName := vocabulary.SplitPackageRef(pkg)
	hands := []struct {
		typ   string
		token string
		actor substrate.Actor
	}{
		{typ: substrate.ActorTypePerson, token: env.token},
		{typ: substrate.ActorTypeApp, token: app},
		{typ: substrate.ActorTypeFunction, token: env.token, actor: substrate.FunctionActor(authorityOf, pkgName, "fn")},
		{typ: substrate.ActorTypeAgent, token: env.token, actor: substrate.AgentActor(authorityOf, pkgName, "ag")},
		{typ: substrate.ActorTypePackage, token: env.token, actor: substrate.BundleActor(authorityOf, pkgName)},
		{typ: substrate.ActorTypeSystem, token: env.token, actor: substrate.ActorSystem},
	}
	for _, hand := range hands {
		if hand.actor != "" {
			actor := hand.actor
			env.wrap = func(ds substrate.Dataset) substrate.Dataset { return actorAs{Dataset: ds, actor: actor} }
		} else {
			env.wrap = nil
		}
		if got := substrate.ActorTypeOf(hand.actor); hand.actor != "" && got != hand.typ {
			t.Fatalf("the seam's actor %s is of type %q, want %q", hand.actor, got, hand.typ)
		}
		for name, w := range values {
			admits := w == nil || hand.typ == substrate.ActorTypeSystem
			switch v := w.(type) {
			case string:
				admits = admits || v == hand.typ
			case []any:
				for _, x := range v {
					admits = admits || x == hand.typ
				}
			}
			id := vocabulary.DerivedMintedID("writers", hand.typ, name)
			rec := env.do(t, http.MethodPut, "/api/v1/"+vocabulary.RecordPath(kind, id), hand.token,
				map[string]any{"properties": map[string]any{"name": hand.typ, name: "written"}})
			switch {
			case admits && rec.Code/100 != 2:
				t.Errorf("a %s writing %s (writer %v) = %d %s, want admitted", hand.typ, name, w, rec.Code, rec.Body)
			case !admits && rec.Code != http.StatusForbidden:
				t.Errorf("a %s writing %s (writer %v) = %d %s, want 403", hand.typ, name, w, rec.Code, rec.Body)
			case !admits && !strings.Contains(rec.Body.String(), "("+hand.typ+")"):
				t.Errorf("a %s writing %s: the refusal does not name its type: %s", hand.typ, name, rec.Body)
			case !admits && hand.actor != "" && strings.Contains(rec.Body.String(), string(hand.actor)):
				t.Errorf("a %s writing %s: the refusal spells the hand rather than naming it in words: %s", hand.typ, name, rec.Body)
			}
		}
	}
	env.wrap = nil

	// A scoped token whose client id names no app reads as the person (its
	// derivation falls back to it), and passes no guarded property, as
	// `owner` refused a scoped token before.
	noApp := env.mint(t, substrate.MintTokenInput{Label: "no app", Scopes: []string{"read:" + kind, "write:" + kind}})
	for name, w := range values {
		id := vocabulary.DerivedMintedID("writers", "noapp", name)
		rec := env.do(t, http.MethodPut, "/api/v1/"+vocabulary.RecordPath(kind, id), noApp,
			map[string]any{"properties": map[string]any{"name": "no app", name: "written"}})
		switch {
		case w == nil && rec.Code/100 != 2:
			t.Errorf("a scoped token naming no app writing %s (no writer) = %d %s, want admitted", name, rec.Code, rec.Body)
		case w != nil && (rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "a scoped token that names no app")):
			t.Errorf("a scoped token naming no app writing %s (writer %v) = %d %s, want 403 naming it", name, w, rec.Code, rec.Body)
		}
	}
}

// A mark is held to a property's `writer:` as its value is: an app's patch of
// a property only the person writes, its pin of it and its release of it are
// refused alike, each with the same words, and the person's pin and release
// land (decision 0284).
func TestAPinAndAReleaseMeetTheWriterAPatchMeets(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	pkg := env.repository + "/guarded"
	kind := pkg + "/note"
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"owned": map[string]any{"type": "string", "writer": "person"},
		}}),
	}); err != nil {
		t.Fatalf("declare the kind: %v", err)
	}
	path := "/api/v1/" + vocabulary.RecordPath(kind, vocabulary.DerivedMintedID("guarded", "note"))
	if rec := env.do(t, http.MethodPut, path, env.token, map[string]any{"properties": map[string]any{"name": "n", "owned": "the person's"}}); rec.Code/100 != 2 {
		t.Fatalf("the person's write = %d %s", rec.Code, rec.Body)
	}
	app := env.mint(t, substrate.MintTokenInput{Label: "Notes", Scopes: []string{"read:" + kind, "write:" + kind}, ClientID: appClientID, ClientName: "Notes"})
	message := func(rec *httptest.ResponseRecorder) string {
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Error.Message
	}
	patched := env.do(t, http.MethodPatch, path, app, map[string]any{"properties": map[string]any{"owned": "the app's"}})
	if patched.Code != http.StatusForbidden || message(patched) == "" {
		t.Fatalf("the app's patch = %d %s, want 403", patched.Code, patched.Body)
	}
	for _, action := range []string{"pin", "release"} {
		rec := env.do(t, http.MethodPost, path+"/"+action, app, map[string]any{"properties": []string{"owned"}})
		if rec.Code != http.StatusForbidden || message(rec) != message(patched) {
			t.Errorf("the app's %s = %d %q, want the patch's 403 %q", action, rec.Code, message(rec), message(patched))
		}
	}
	for _, action := range []string{"pin", "release"} {
		if rec := env.do(t, http.MethodPost, path+"/"+action, env.token, map[string]any{"properties": []string{"owned"}}); rec.Code != http.StatusOK {
			t.Errorf("the person's %s = %d %s, want 200", action, rec.Code, rec.Body)
		}
	}
}
