package testenv_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
)

// A put and a patch read `ifVersion` from the body. A `?ifVersion=` in the
// address is refused by name, as every read refuses a parameter it does not
// honor: taken silently, it was a guarded write landing with no guard.
func TestARecordWriteRefusesAQueryParameter(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(probeAuthority))
	e.ApplyVocabularyYAML(`
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + probeRef + `
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + ref("note") + `
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  names:
    singular: note
  properties:
    name:
      type: string
`)
	var created struct {
		ID string `json:"id"`
	}
	e.MustJSON(http.MethodPost, "/api/v1/records", map[string]any{
		"kind": ref("note"), "properties": map[string]any{"name": "Ada"},
	}, &created)
	path := "/api/v1/" + ref("note") + "/" + created.ID

	refusal := func(t *testing.T, method, query string) string {
		t.Helper()
		status, body := e.Do(method, path+query, map[string]any{
			"properties": map[string]any{"name": "Grace"},
		})
		var out struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &out)
		if status != http.StatusBadRequest || out.Error.Code != "bad_request" {
			t.Fatalf("%s %s: %d %s, want 400 bad_request", method, query, status, body)
		}
		return out.Error.Message
	}
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			if msg := refusal(t, method, "?ifVersion=7"); !strings.Contains(msg, `"ifVersion"`) || !strings.Contains(msg, "in the body") {
				t.Fatalf("a guard in the address is refused as %q, want it named and pointed at the body", msg)
			}
			if msg := refusal(t, method, "?dryRun=true"); !strings.Contains(msg, `"dryRun"`) {
				t.Fatalf("an unknown parameter is refused as %q, want it named", msg)
			}
		})
	}

	// Nothing was written past the refusals: the record is as created.
	status, body := e.Do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("read back: %d %s", status, body)
	}
	var got struct {
		Version    int64          `json:"version"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Properties["name"] != "Ada" {
		t.Fatalf("the refused writes landed: version %d, name %v", got.Version, got.Properties["name"])
	}

	// The body's guard still works.
	if status, body := e.Do(http.MethodPatch, path, map[string]any{
		"properties": map[string]any{"name": "Grace"}, "ifVersion": 7,
	}); status != http.StatusConflict {
		t.Fatalf("a stale guard in the body: %d %s, want 409", status, body)
	}
}
