package testenv_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
)

// Issue #709: a function's `enum` argument admits only its declared `values`.
// A value outside them is refused with 422 on the call API and on a schedule
// trigger's `arguments` (decision 0106), and the refusal names the argument
// and the allowed set, so the body never receives a typo like `wekly`.
func TestAnEnumArgumentOutsideItsValuesAnswers422(t *testing.T) {
	e := testenv.Start(t)
	e.ApplyVocabularyYAML(`
kind: substrate.reamde.dev/core/package
metadata: {id: ` + probeRef + `}
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  version: 1
---
kind: substrate.reamde.dev/core/function
metadata: {id: ` + probeRef + `/rollup}
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  description: echoes the period it was called with
  runtime: python
  arguments:
    - {name: period, type: enum, values: [daily, weekly, monthly], required: true}
  source: |
    def main(input, host):
        return {"output": {"period": input["period"]}}
`)
	callPath := "/api/v1/substrate.reamde.dev/core/function/" + url.PathEscape(ref("rollup")) + "/call"
	wantRefusal := func(t *testing.T, status int, body []byte) {
		t.Helper()
		if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"validation"`) {
			t.Fatalf("got %d %s, want 422 validation", status, body)
		}
		for _, part := range []string{"period", "wekly", "daily, weekly, monthly"} {
			if !strings.Contains(string(body), part) {
				t.Errorf("the refusal does not name %q: %s", part, body)
			}
		}
	}

	t.Run("the call API", func(t *testing.T) {
		status, body := e.Do(http.MethodPost, callPath, map[string]any{"input": map[string]any{"period": "wekly"}})
		wantRefusal(t, status, body)
	})

	t.Run("a schedule trigger write", func(t *testing.T) {
		status, body := e.Do(http.MethodPut, "/api/v1/"+corePkg+"/trigger/weekly-rollup", map[string]any{
			"properties": map[string]any{
				"source": map[string]any{"schedule": map[string]any{
					"recurrence": "FREQ=WEEKLY", "timezone": "UTC", "startsAt": "2026-09-01T00:00:00Z",
				}},
				"callable":  corePkg + "/function/" + ref("rollup"),
				"arguments": map[string]any{"period": "wekly"},
			},
		})
		wantRefusal(t, status, body)
		if status, _ := e.Do(http.MethodGet, "/api/v1/"+corePkg+"/trigger/weekly-rollup", nil); status != http.StatusNotFound {
			t.Fatalf("the refused trigger landed: %d", status)
		}
	})
}
