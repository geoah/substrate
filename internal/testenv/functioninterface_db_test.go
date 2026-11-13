package testenv_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A package names the function interface its code is written against
// (decision record 0265). At the apply door a version this server does not
// run is refused with that reason, before any of the package lands; one it
// runs is stored on the package record as written.
func TestAPackageNamingAnInterfaceTheServerLacksIsRefused(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(probeAuthority))
	header := func(line string) map[string]any {
		data := map[string]any{"authority": probeAuthority, "package": probePackage, "version": 1}
		if line != "" {
			data["interface"] = json.RawMessage(line)
		}
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/package",
			"metadata": map[string]any{"id": probeRef},
			"data":     data,
		}
	}
	function := map[string]any{
		"kind":     "substrate.reamde.dev/core/function",
		"metadata": map[string]any{"id": ref("echo")},
		"data": map[string]any{
			"authority": probeAuthority, "package": probePackage,
			"description": "answers what it was called with",
			"runtime":     "python",
			"source":      "def main(input, host):\n    return {\"output\": input.get(\"args\")}\n",
		},
	}
	packagePath := "/api/v1/substrate.reamde.dev/core/package/" + url.PathEscape(probeRef)

	next := vocabulary.FunctionInterface + 1
	status, body := e.Do(http.MethodPost, "/api/v1/vocabulary/apply",
		map[string]any{"documents": []any{header(fmt.Sprint(next)), function}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("apply naming interface %d: got %d %s, want 422", next, status, body)
	}
	want := fmt.Sprintf("written against function interface %d, and this server runs interface %d alone", next, vocabulary.FunctionInterface)
	if !strings.Contains(string(body), want) {
		t.Fatalf("the refusal does not give the reason %q: %s", want, body)
	}
	if status, _ := e.Do(http.MethodGet, packagePath, nil); status != http.StatusNotFound {
		t.Fatalf("the refused package's record answers %d, want 404: nothing of it lands", status)
	}

	e.ApplyVocabulary(header(fmt.Sprint(vocabulary.FunctionInterface)), function)
	var rec struct {
		Properties map[string]any `json:"properties"`
	}
	e.MustJSON(http.MethodGet, packagePath, nil, &rec)
	if got := rec.Properties["interface"]; got != float64(vocabulary.FunctionInterface) {
		t.Fatalf("the package record's interface = %v, want %d", got, vocabulary.FunctionInterface)
	}
	if out := e.MustCallFunction(ref("echo"), map[string]any{"ok": true}); fmt.Sprint(out) != "map[ok:true]" {
		t.Fatalf("the package's function answered %v", out)
	}
}
