package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// BOTH SPELLINGS MEET BEFORE EVERY CHECK (decision record 0259). Each door
// that takes a kind from outside is driven twice, once with `~` and once with
// the repository's full name, under each check that reads the kind: an app
// token's scope, a person's role, a property's `writer:`, a reference's pin,
// a change policy's selector. Every cell answers the same for both, and the
// answer it expects says the check was met at all. A control kind is core's
// and has one spelling.
func TestBothSpellingsOfAnOwnKindMeetBeforeEveryCheck(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	full := env.repository + "/notes"
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(full, 0),
		vocabulary.KindManifest(full, map[string]any{"singular": "note"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"stamp": map[string]any{"type": "string", "writer": []any{"function"}},
			},
		}),
		vocabulary.KindManifest(full, map[string]any{"singular": "link"}, map[string]any{
			"properties": map[string]any{"to": map[string]any{"type": "reference", "kind": full + "/note"}},
		}),
	}); err != nil {
		t.Fatalf("declare the notes: %v", err)
	}
	n1 := mintedID("both-spellings-n1")
	env.put(t, "~/notes/note", n1, map[string]any{"name": "first"})
	viewer := addPerson(t, env, "v", scope.RoleViewer)
	app := appScopedToken(t, env, "read:"+full+"/note")

	spellings := map[string]string{"~": "~/notes", "full": full}
	type answer struct {
		status int
		kind   string
	}
	read := func(status int, body []byte) answer {
		var got struct {
			Kind    string `json:"kind"`
			Records []struct {
				Kind string `json:"kind"`
			} `json:"records"`
			Changes []struct {
				Kind string `json:"kind"`
			} `json:"changes"`
		}
		_ = json.Unmarshal(body, &got)
		kind := got.Kind
		for _, r := range got.Records {
			kind = r.Kind
		}
		for _, c := range got.Changes {
			kind = c.Kind
		}
		return answer{status: status, kind: kind}
	}
	type cell struct {
		door, check string
		// drive answers the door for one spelling of the package, pkg.
		drive func(pkg string) answer
		// want is the status both spellings answer; the kind they answer,
		// where they answer one, is always `~`.
		want int
	}
	do := func(method, path, token string, body any) answer {
		rec := env.do(t, method, path, token, body)
		return read(rec.Code, rec.Body.Bytes())
	}
	listOf := func(pkg string) string {
		return recordsPath + "?filter=" + url.QueryEscape(`{"kinds":["`+pkg+`/note"]}`)
	}
	changesOf := func(pkg string) string {
		return "/api/v1/changes?first=50&recordKind=" + url.QueryEscape(pkg+"/note") + "&recordId=" + n1
	}
	cells := []cell{
		// An app token's scope, `read:` the note kind as the owner approved it.
		{"a record's address", "an app's scope", func(pkg string) answer {
			return do(http.MethodGet, "/api/v1/"+pkg+"/note/"+n1, app, nil)
		}, http.StatusOK},
		{"the records list", "an app's scope", func(pkg string) answer {
			return do(http.MethodGet, listOf(pkg), app, nil)
		}, http.StatusOK},
		{"the change feed", "an app's scope", func(pkg string) answer {
			return do(http.MethodGet, changesOf(pkg), app, nil)
		}, http.StatusOK},
		{"a write by address", "an app's scope", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/"+pkg+"/note/"+mintedID("app-"+strings.ReplaceAll(pkg, "/", "-")), app,
				map[string]any{"properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		// A person's role: a viewer reads every kind it holds and writes none.
		{"a record's address", "a role's rule", func(pkg string) answer {
			return do(http.MethodGet, "/api/v1/"+pkg+"/note/"+n1, viewer.token, nil)
		}, http.StatusOK},
		{"the records list", "a role's rule", func(pkg string) answer {
			return do(http.MethodGet, listOf(pkg), viewer.token, nil)
		}, http.StatusOK},
		{"a write by address", "a role's rule", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/"+pkg+"/note/"+n1, viewer.token,
				map[string]any{"properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		{"a create", "a role's rule", func(pkg string) answer {
			return do(http.MethodPost, recordsPath, viewer.token,
				map[string]any{"kind": pkg + "/note", "properties": map[string]any{"name": "no"}})
		}, http.StatusForbidden},
		// A property's `writer:`: only a function writes `stamp`.
		{"a write by address", "a property's writer", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/"+pkg+"/note/"+n1, env.token,
				map[string]any{"properties": map[string]any{"stamp": "by hand"}})
		}, http.StatusForbidden},
		{"a create", "a property's writer", func(pkg string) answer {
			return do(http.MethodPost, recordsPath, env.token,
				map[string]any{"kind": pkg + "/note", "properties": map[string]any{"stamp": "by hand"}})
		}, http.StatusForbidden},
		// A reference's pin: the link's `to` takes a note, in either spelling.
		{"a reference value", "a reference's pin", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/~/notes/link/"+mintedID("link-"+strings.ReplaceAll(pkg, "/", "-")), env.token,
				map[string]any{"properties": map[string]any{"to": map[string]any{"ref": pkg + "/note/" + n1}}})
		}, http.StatusCreated},
		{"a reference value", "a reference's pin, to another kind", func(pkg string) answer {
			return do(http.MethodPut, "/api/v1/~/notes/link/"+mintedID("bad-"+strings.ReplaceAll(pkg, "/", "-")), env.token,
				map[string]any{"properties": map[string]any{"to": map[string]any{"ref": pkg + "/link/" + n1}}})
		}, http.StatusUnprocessableEntity},
	}
	for _, c := range cells {
		got := map[string]answer{}
		for spelling, pkg := range spellings {
			got[spelling] = c.drive(pkg)
		}
		if got["~"].status != got["full"].status {
			t.Errorf("%s under %s: `~` answers %d and the full name %d", c.door, c.check, got["~"].status, got["full"].status)
		}
		if got["~"].status != c.want {
			t.Errorf("%s under %s: answers %d, want %d", c.door, c.check, got["~"].status, c.want)
		}
		for spelling, a := range got {
			if a.kind != "" && !strings.HasPrefix(a.kind, "~/") {
				t.Errorf("%s under %s, spelled %s: answers the kind %s, want it under ~", c.door, c.check, spelling, a.kind)
			}
		}
	}

	// A change policy's selector, written in either spelling, is the same
	// selector: the write door folds it before the policy is ever read.
	stored := map[string]any{}
	for spelling, pkg := range spellings {
		id := "both-spellings-" + strings.ReplaceAll(spelling, "~", "local")
		rec := env.do(t, http.MethodPut, "/api/v1/substrate.reamde.dev/core/recordpatchpolicy/"+id, env.token, map[string]any{
			"properties": map[string]any{"action": "gate", "selector": map[string]any{"kinds": []any{pkg + "/note"}}},
		})
		wantStatus(t, rec, http.StatusCreated)
		var got substrate.Record
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		stored[spelling] = got.Properties["selector"]
	}
	if a, b := stored["~"], stored["full"]; !jsonEqualForTest(a, b) {
		t.Errorf("a policy's selector stored as %v under `~` and %v under the full name", a, b)
	}

	// A control kind is core's: it has one spelling, and a viewer reads none.
	rec := env.do(t, http.MethodGet, recordsPath+"?filter="+url.QueryEscape(`{"kinds":["`+vocabulary.CoreKind("token")+`"]}`), viewer.token, nil)
	if rec.Code == http.StatusOK {
		if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 0 {
			t.Errorf("a viewer lists %d tokens", len(page.Records))
		}
	}
}

func jsonEqualForTest(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
