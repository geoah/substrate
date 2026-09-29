package testenv_test

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
)

// Issue 464: core's `trigger.source` and `recordpatchpolicy.action` are
// required, so a write that leaves either out is refused over HTTP with `422`
// naming the property, and a patch that never mentions it is not.
func TestATriggerWithoutASourceOrAPolicyWithoutAnActionAnswers422(t *testing.T) {
	e := testenv.Start(t)
	e.ApplyVocabularyYAML(`
kind: substrate.reamde.dev/core/package
metadata: {id: ` + probeRef + `}
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  version: 1
---
kind: substrate.reamde.dev/core/agent
metadata: {id: ` + probeRef + `/keeper}
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  description: the callable the triggers name
  prompt: You keep.
  provider: openai
  model: gpt-5
`)
	triggers := "/api/v1/" + corePkg + "/trigger/"
	policies := "/api/v1/" + corePkg + "/recordpatchpolicy/"
	callable := corePkg + "/agent/" + ref("keeper")

	for _, tc := range []struct {
		name, path, problem string
		props               map[string]any
	}{
		{
			"a trigger without a source", triggers + "sourceless", "props.source",
			map[string]any{"callable": callable},
		},
		{
			"a trigger whose source is empty", triggers + "emptysource", "props.source",
			map[string]any{"callable": callable, "source": map[string]any{}},
		},
		{
			"a policy without an action", policies + "actionless", "props.action",
			map[string]any{"selector": map[string]any{"kinds": []any{"*"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := e.Do(http.MethodPut, tc.path, map[string]any{"properties": tc.props})
			wantError(t, status, body, http.StatusUnprocessableEntity, "validation", tc.problem)
			if status, body := e.Do(http.MethodGet, tc.path, nil); status != http.StatusNotFound {
				t.Fatalf("the refused write landed: %d %s", status, body)
			}
		})
	}

	t.Run("a patch that never mentions the property", func(t *testing.T) {
		for _, w := range []struct {
			path         string
			props, patch map[string]any
		}{
			{
				triggers + "kept",
				map[string]any{"callable": callable, "source": map[string]any{"webhook": map[string]any{}}},
				map[string]any{"enabled": false},
			},
			{
				policies + "kept",
				map[string]any{"selector": map[string]any{"kinds": []any{"*"}}, "action": "gate"},
				map[string]any{"criteria": "only what the owner wrote"},
			},
		} {
			if status, body := e.Do(http.MethodPut, w.path, map[string]any{"properties": w.props}); status/100 != 2 {
				t.Fatalf("put %s: %d %s", w.path, status, body)
			}
			if status, body := e.Do(http.MethodPatch, w.path, map[string]any{"properties": w.patch}); status/100 != 2 {
				t.Fatalf("patch %s without the required property: %d %s", w.path, status, body)
			}
		}
	})

	// The upgrade note's search for a row an older binary left without the
	// value is a read the records route serves; here it finds nothing, because
	// every write above that lacked the value was refused.
	t.Run("the upgrade note's search", func(t *testing.T) {
		for kind, prop := range map[string]string{
			corePkg + "/trigger":           "source",
			corePkg + "/recordpatchpolicy": "action",
		} {
			var page substrate.Page
			e.MustJSON(http.MethodGet, listPath(kind, map[string]any{
				"properties": map[string]any{prop: map[string]any{"exists": false}},
			}), nil, &page)
			if len(page.Records) != 0 {
				t.Fatalf("%s without %s: %d records, want none", kind, prop, len(page.Records))
			}
		}
	})
}
