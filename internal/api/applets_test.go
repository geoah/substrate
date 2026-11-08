package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
)

func appletPath(id, verb string) string {
	return "/api/v1/" + applet.Kind + "/" + id + "/" + verb
}

// A one-screen applet over people: a list, a field and a button that adds one.
func putPeopleApplet(t *testing.T, env *testEnv) {
	t.Helper()
	ds := env.svc.datasets[fakeRepository]
	_, err := ds.Put(context.Background(), substrate.ActorConsole, substrate.PutInput{
		Kind: applet.Kind, ID: "people", Properties: peopleApplet(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func peopleApplet() map[string]any {
	return map[string]any{
		"name":    "People",
		"runtime": "a2ui",
		"state":   map[string]any{"name": ""},
		"sources": map[string]any{
			"people": map[string]any{"kind": personKind},
		},
		"actions": map[string]any{
			"add": []any{
				map[string]any{"op": "create", "kind": personKind, "properties": map[string]any{"name": map[string]any{"@path": "/ui/name"}}},
				map[string]any{"op": "set", "values": map[string]any{"/ui/name": ""}},
				map[string]any{"op": "notify", "text": "Added"},
			},
		},
		"screens": map[string]any{
			"home": map[string]any{
				"title":   "People",
				"sources": []any{"people"},
				"components": []any{
					map[string]any{"id": "root", "component": "Column", "children": []any{"name", "add", "list"}},
					map[string]any{"id": "name", "component": "TextField", "label": "Name", "value": map[string]any{"@path": "/ui/name"}},
					map[string]any{"id": "add", "component": "Button", "child": "addLabel", "action": map[string]any{"event": map[string]any{"name": "add"}}},
					map[string]any{"id": "addLabel", "component": "Text", "text": "Add"},
					map[string]any{"id": "list", "component": "List", "children": map[string]any{"path": "/data/people", "componentId": "row"}},
					map[string]any{"id": "row", "component": "Text", "text": map[string]any{"@path": "properties/name"}},
				},
			},
		},
	}
}

func TestAnAppletRendersItsStartAndRunsAnActionAsTheCaller(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	putPeopleApplet(t, env)

	rec := env.do(t, http.MethodPost, appletPath("people", "render"), tok, map[string]any{})
	wantStatus(t, rec, http.StatusOK)
	frame := decodeJSON[substrate.AppletFrame](t, rec)
	if frame.State.Screen != "home" || frame.Applet.Title != "People" || len(frame.Messages) != 1 || frame.Messages[0].CreateSurface == nil {
		t.Fatalf("the first render: %+v", frame)
	}

	rec = env.do(t, http.MethodPost, appletPath("people", "act"), tok, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "home", UI: map[string]any{"name": "Ada"}},
		Shown:  &frame.Shown,
		Action: substrate.AppletAction{Name: "add"},
	}, actorHeader, "console")
	wantStatus(t, rec, http.StatusOK)
	frame = decodeJSON[substrate.AppletFrame](t, rec)
	ds := env.svc.datasets[fakeRepository]
	if ds.lastPut.Kind != personKind || ds.lastPut.Properties["name"] != "Ada" || ds.lastActor != substrate.ActorConsole {
		t.Fatalf("the action wrote %+v as %q", ds.lastPut, ds.lastActor)
	}
	if frame.Notice != "Added" || frame.State.UI["name"] != "" {
		t.Fatalf("after the action: notice %q, ui %v", frame.Notice, frame.State.UI)
	}
}

func TestAnAppletTheRepositoryLacksIs404AndAnUnknownActionIs422(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	putPeopleApplet(t, env)

	wantErrorCode(t, env.do(t, http.MethodPost, appletPath("nope", "render"), tok, map[string]any{}), http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodPost, appletPath("people", "act"), tok, substrate.AppletActInput{
		State: substrate.AppletState{Screen: "home"},
	}), http.StatusUnprocessableEntity, codeValidation)
	wantErrorCode(t, env.do(t, http.MethodPost, appletPath("people", "act"), tok, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "home"},
		Action: substrate.AppletAction{Name: "remove"},
	}), http.StatusUnprocessableEntity, codeValidation)
}

// The preview checks a draft that was never written and draws a stored
// applet's screen, reading under the caller's token and writing nothing.
func TestThePreviewChecksADraftAndDrawsAStoredApplet(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	putPeopleApplet(t, env)
	if _, err := env.svc.datasets[fakeRepository].Put(context.Background(), substrate.ActorConsole, substrate.PutInput{
		Kind: personKind, ID: "ada", Properties: map[string]any{"name": "Ada"},
	}); err != nil {
		t.Fatal(err)
	}

	rec := env.do(t, http.MethodPost, appletPath("people", "preview"), tok, map[string]any{})
	wantStatus(t, rec, http.StatusOK)
	p := decodeJSON[substrate.AppletPreview](t, rec)
	if len(p.Problems) != 0 || !strings.Contains(p.Outline, `row Text text=@/data/people/0/properties/name="Ada"`) {
		t.Fatalf("stored preview: %+v", p)
	}

	draft := peopleApplet()
	draft["sources"].(map[string]any)["people"].(map[string]any)["orderBy"] = "nickname"
	rec = env.do(t, http.MethodPost, appletPath("draft", "preview"), tok, map[string]any{"applet": draft})
	wantStatus(t, rec, http.StatusOK)
	p = decodeJSON[substrate.AppletPreview](t, rec)
	if len(p.Problems) == 0 || !strings.Contains(p.Problems[0], `declares no property "nickname"`) {
		t.Fatalf("draft preview: %+v", p)
	}
}

// An assistant at the MCP door reads the guide, writes an applet with write
// and previews it with the applets tool.
func TestAnAssistantBuildsAnAppletAtTheDoor(t *testing.T) {
	env := newTestEnv(t)
	tok := assistantToken(env.svc)
	text, isErr := callTool(t, env.h, tok, "applets", map[string]any{"op": "guide"})
	if isErr || !strings.Contains(text, "Building an applet") {
		t.Fatalf("guide = %.200s", text)
	}
	text, isErr = callTool(t, env.h, tok, "applets", map[string]any{"op": "preview", "id": "people", "applet": peopleApplet()})
	if isErr || !strings.Contains(text, `"screen":"home"`) {
		t.Fatalf("draft preview = %s", text)
	}
	text, isErr = callTool(t, env.h, tok, "write", map[string]any{
		"op": "put", "kind": applet.Kind, "id": "people", "properties": peopleApplet(),
	})
	if isErr {
		t.Fatalf("write = %s", text)
	}
	text, isErr = callTool(t, env.h, tok, "applets", map[string]any{"op": "preview", "id": "people"})
	if isErr || !strings.Contains(text, `"outline"`) {
		t.Fatalf("stored preview = %s", text)
	}
}
