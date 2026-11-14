package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// appletsPutPeople stores peopleApplet under the id "people".
func appletsPutPeople(t *testing.T, env *dbEnv) {
	t.Helper()
	env.put(t, applet.Kind, "people", peopleApplet())
}

func TestAnAppletRendersItsStartAndRunsAnActionAsTheCaller(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	appletsPutPeople(t, env)

	rec := env.do(t, http.MethodPost, appletPath("people", "render"), env.token, map[string]any{})
	wantStatus(t, rec, http.StatusOK)
	frame := decodeJSON[substrate.AppletFrame](t, rec)
	if frame.State.Screen != "home" || frame.Applet.Title != "People" || len(frame.Messages) != 1 || frame.Messages[0].CreateSurface == nil {
		t.Fatalf("the first render: %+v", frame)
	}

	head, err := env.ds.Head(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, http.MethodPost, appletPath("people", "act"), env.token, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "home", UI: map[string]any{"name": "Ada"}},
		Shown:  &frame.Shown,
		Action: substrate.AppletAction{Name: "add"},
	})
	wantStatus(t, rec, http.StatusOK)
	frame = decodeJSON[substrate.AppletFrame](t, rec)
	changes, err := env.ds.Changes(t.Context(), head.Seq, substrate.ChangeFilter{Kinds: []string{personKind}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Op != substrate.OpPut || changes[0].Actor != vocabulary.ActorID(substrate.ActorPerson) {
		t.Fatalf("the action wrote %+v, want one person put as the person", changes)
	}
	if got := env.get(t, personKind, changes[0].RecordID); got == nil || got.Properties["name"] != "Ada" {
		t.Fatalf("the action stored %+v", got)
	}
	if frame.Notice != "Added" || frame.State.UI["name"] != "" {
		t.Fatalf("after the action: notice %q, ui %v", frame.Notice, frame.State.UI)
	}
}

func TestAnAppletTheRepositoryLacksIs404AndAnUnknownActionIs422(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	appletsPutPeople(t, env)

	wantErrorCode(t, env.do(t, http.MethodPost, appletPath("nope", "render"), env.token, map[string]any{}), http.StatusNotFound, codeNotFound)
	wantErrorCode(t, env.do(t, http.MethodPost, appletPath("people", "act"), env.token, substrate.AppletActInput{
		State:  substrate.AppletState{Screen: "home"},
		Action: substrate.AppletAction{Name: "remove"},
	}), http.StatusUnprocessableEntity, codeValidation)
}

// The preview checks a draft that was never written and draws a stored
// applet's screen, reading under the caller's token and writing nothing.
func TestThePreviewChecksADraftAndDrawsAStoredApplet(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	appletsPutPeople(t, env)
	env.put(t, personKind, "ada", map[string]any{"name": "Ada"})
	head, err := env.ds.Head(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	rec := env.do(t, http.MethodPost, appletPath("people", "preview"), env.token, map[string]any{})
	wantStatus(t, rec, http.StatusOK)
	p := decodeJSON[substrate.AppletPreview](t, rec)
	if len(p.Problems) != 0 || !strings.Contains(p.Outline, `row Text text=@/data/people/0/properties/name="Ada"`) {
		t.Fatalf("stored preview: %+v", p)
	}

	draft := peopleApplet()
	draft["sources"].(map[string]any)["people"].(map[string]any)["orderBy"] = "nickname"
	rec = env.do(t, http.MethodPost, appletPath("draft", "preview"), env.token, map[string]any{"applet": draft})
	wantStatus(t, rec, http.StatusOK)
	p = decodeJSON[substrate.AppletPreview](t, rec)
	if len(p.Problems) == 0 || !strings.Contains(p.Problems[0], `declares no property "nickname"`) {
		t.Fatalf("draft preview: %+v", p)
	}
	if after, err := env.ds.Head(t.Context()); err != nil || after != head {
		t.Fatalf("the previews wrote: head %+v, was %+v (%v)", after, head, err)
	}
	if got := env.get(t, applet.Kind, "draft"); got != nil {
		t.Fatalf("the draft preview stored the draft: %+v", got)
	}
}

// An assistant at the MCP door reads the guide, writes an applet with write
// and previews it with the applets tool.
func TestAnAssistantBuildsAnAppletAtTheDoor(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	tok := env.mint(t, substrate.MintTokenInput{
		Label: "assistant", Scopes: []string{"mcp"}, AppLocal: true,
		ClientID: "https://claude.ai/oauth/claude-code-client-metadata", ClientName: "Claude Code",
	})
	text, isErr := callTool(t, env.h, tok, "applets", map[string]any{"op": "guide"})
	if isErr || !strings.Contains(text, "Building an applet") {
		t.Fatalf("guide = %.200s", text)
	}
	text, isErr = callTool(t, env.h, tok, "applets", map[string]any{"op": "preview", "id": "people", "applet": peopleApplet()})
	if isErr || !strings.Contains(text, `"screen":"home"`) {
		t.Fatalf("draft preview = %s", text)
	}
	id := mintedID("people")
	text, isErr = callTool(t, env.h, tok, "write", map[string]any{
		"op": "put", "kind": applet.Kind, "id": id, "properties": peopleApplet(),
	})
	if isErr {
		t.Fatalf("write = %s", text)
	}
	if got := env.get(t, applet.Kind, id); got == nil || got.Properties["name"] != "People" {
		t.Fatalf("the write stored %+v", got)
	}
	text, isErr = callTool(t, env.h, tok, "applets", map[string]any{"op": "preview", "id": id})
	if isErr || !strings.Contains(text, `"outline"`) {
		t.Fatalf("stored preview = %s", text)
	}
}
