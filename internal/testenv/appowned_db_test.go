package testenv_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

// AN APP OWNS THE PACKAGE IT DECLARES, END TO END (decision 0205): over the
// real server, an app's `declare:` token installs its package; the owner's
// token, through the console's and substratectl's doors, is refused every
// change to its declarations and keeps every record; another app holding the
// same scope is refused; the app still upgrades it; and the owner's confirmed
// takeover makes it theirs, after which the app is refused.
func TestAnAppOwnsThePackageItDeclares(t *testing.T) {
	e := testenv.Start(t)
	ctx := context.Background()
	pkg := e.Repository + "/training"
	set := pkg + "/set"
	ds, _, err := e.Service.Authenticate(ctx, e.Token)
	if err != nil {
		t.Fatal(err)
	}
	mint := func(host string) *testenv.Env {
		t.Helper()
		_, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{
			Label: host, ClientID: "https://" + host + "/client.json", ClientName: host,
			Scopes: []string{"declare:" + pkg, "read:" + pkg + "/*", "write:" + pkg + "/*"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return e.WithToken(secret)
	}
	workouts, notes := mint("workouts.example.org"), mint("notes.example.org")
	const owner = "app:workouts.example.org"

	docs := func(props ...string) map[string]any {
		declared := map[string]any{}
		for _, p := range props {
			declared[p] = map[string]any{"type": "string"}
		}
		return map[string]any{"documents": []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.BundleManifest(pkg, map[string]any{"installs": []any{set}}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "set"}, map[string]any{"properties": declared}),
		}}
	}
	as := func(env *testenv.Env, actor, method, path string, body any) (int, string) {
		t.Helper()
		var raw []byte
		headers := map[string]string{"Content-Type": "application/json"}
		if actor != "" {
			headers["X-Substrate-Actor"] = actor
		}
		if body != nil {
			var err error
			if raw, err = json.Marshal(body); err != nil {
				t.Fatal(err)
			}
		}
		status, out, _ := env.DoRaw(method, path, raw, headers)
		return status, string(out)
	}
	refused := func(what string, status int, body string) {
		t.Helper()
		if status != http.StatusForbidden || !strings.Contains(body, owner) {
			t.Errorf("%s: %d %s, want a 403 naming %s", what, status, body, owner)
		}
	}

	if status, body := as(workouts, "", http.MethodPost, "/api/v1/vocabulary/apply", docs("reps")); status != http.StatusOK {
		t.Fatalf("the app installs its package: %d %s", status, body)
	}

	kindPath := "/api/v1/substrate.reamde.dev/core/kind/" + url.PathEscape(set)
	packagePath := "/api/v1/substrate.reamde.dev/core/package/" + url.PathEscape(pkg)
	for _, door := range []string{"console", "substratectl"} {
		status, body := as(e, door, http.MethodPost, "/api/v1/vocabulary/apply", docs("reps", "weight"))
		refused(door+" applies the package", status, body)
		status, body = as(e, door, http.MethodPost, "/api/v1/vocabulary/plan", docs("reps", "weight"))
		refused(door+" previews the apply", status, body)
		status, body = as(e, door, http.MethodPatch, kindPath, map[string]any{"properties": map[string]any{"description": "mine"}})
		refused(door+" patches the kind", status, body)
		status, body = as(e, door, http.MethodPatch, packagePath, map[string]any{"properties": map[string]any{"description": "mine"}})
		refused(door+" patches the package", status, body)
		status, body = as(e, door, http.MethodDelete, kindPath, nil)
		refused(door+" deletes the kind", status, body)
		status, body = as(e, door, http.MethodDelete, packagePath, nil)
		refused(door+" deletes the package", status, body)
		status, body = as(e, door, http.MethodPatch, "/api/v1/substrate.reamde.dev/core/bundle/"+url.PathEscape(pkg),
			map[string]any{"properties": map[string]any{"uninstalled": true}})
		refused(door+" uninstalls the bundle", status, body)
	}
	status, body := as(notes, "", http.MethodPost, "/api/v1/vocabulary/apply", docs("reps", "weight"))
	refused("another app with the same scope applies the package", status, body)

	// The records stay the owner's.
	recordPath := "/api/v1/" + set + "/s1"
	if status, body := as(e, "console", http.MethodPut, recordPath, map[string]any{"properties": map[string]any{"reps": "8"}}); status/100 != 2 {
		t.Fatalf("the owner writes a record of the app's kind: %d %s", status, body)
	}
	if status, body := as(e, "substratectl", http.MethodPatch, recordPath, map[string]any{"properties": map[string]any{"reps": "10"}}); status/100 != 2 {
		t.Fatalf("the owner edits a record of the app's kind: %d %s", status, body)
	}

	if status, body := as(workouts, "", http.MethodPost, "/api/v1/vocabulary/apply", docs("reps", "weight")); status != http.StatusOK {
		t.Fatalf("the app upgrades its package: %d %s", status, body)
	}

	// The escape hatch: the app's token never reaches it; the owner's needs the
	// package repeated; then the package is the owner's and not the app's.
	takeover := map[string]any{"package": pkg, "confirm": pkg}
	if status, body := as(workouts, "", http.MethodPost, "/api/v1/vocabulary/takeover", takeover); status != http.StatusForbidden {
		t.Errorf("the app took its package over: %d %s", status, body)
	}
	if status, body := as(e, "console", http.MethodPost, "/api/v1/vocabulary/takeover", map[string]any{"package": pkg, "confirm": "training"}); status != http.StatusUnprocessableEntity {
		t.Errorf("a takeover with the wrong confirmation: %d %s", status, body)
	}
	if status, body := as(e, "console", http.MethodPost, "/api/v1/vocabulary/takeover", takeover); status != http.StatusOK || !strings.Contains(body, `"declaredBy":"console"`) {
		t.Fatalf("the owner takes the package over: %d %s", status, body)
	}
	if status, body := as(e, "substratectl", http.MethodPost, "/api/v1/vocabulary/apply", docs("reps", "weight", "rpe")); status != http.StatusOK {
		t.Fatalf("the owner edits the package after taking it over: %d %s", status, body)
	}
	if status, body := as(workouts, "", http.MethodPost, "/api/v1/vocabulary/apply", docs("reps", "weight", "rpe", "tempo")); status != http.StatusForbidden {
		t.Fatalf("the app wrote a package the owner took over: %d %s", status, body)
	}
	if status, body := as(e, "console", http.MethodDelete, recordPath, nil); status/100 != 2 {
		t.Fatalf("the owner deletes a record of the kind: %d %s", status, body)
	}
}
