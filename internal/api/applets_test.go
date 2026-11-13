package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
)

func appletPath(id, verb string) string {
	return "/api/v1/" + applet.Kind + "/" + id + "/" + verb
}

// peopleApplet is a one-screen applet over people: a list, a field and a
// button that adds one.
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

// An act that names no action is refused before the applet is read: the stub
// holds no applet, so reaching the dataset would be a 500.
func TestAnAppletActNamingNoActionIs422(t *testing.T) {
	env := newStubEnv(t, nil)
	wantErrorCode(t, env.do(t, http.MethodPost, appletPath("people", "act"), stubToken, substrate.AppletActInput{
		State: substrate.AppletState{Screen: "home"},
	}), http.StatusUnprocessableEntity, codeValidation)
}
