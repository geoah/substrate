package api

// HOW LONG A KIND'S HISTORY IS KEPT, OVER HTTP ON THE REAL ENGINE (decision
// records 0282 and 0283). A kind declares each form and its definition
// answers it; the repository overrides a shipped kind's and one of its own,
// the effective value moves, and deleting the override restores the declared
// one. The test reads the wire as JSON, so it says the same about a server
// that does not have the key: there, every step refuses.

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

const (
	coreKindPath      = "/api/v1/substrate.reamde.dev/core/kind/"
	coreRetentionPath = "/api/v1/substrate.reamde.dev/core/retention/"
	kindTriggerRun    = "substrate.reamde.dev/core/triggerrun"
)

// retentionRow is one item of GET /api/v1/retention as the wire spells it.
type retentionRow struct {
	Kind       string `json:"kind"`
	Declared   string `json:"declared"`
	Override   string `json:"override"`
	OverrideID string `json:"overrideId"`
	Effective  string `json:"effective"`
}

func (e *dbEnv) retentions(t *testing.T) map[string]retentionRow {
	t.Helper()
	rec := e.do(t, http.MethodGet, "/api/v1/retention", e.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/retention = %d %s", rec.Code, rec.Body)
	}
	var list struct {
		Items []retentionRow `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]retentionRow{}
	for _, r := range list.Items {
		out[r.Kind] = r
	}
	return out
}

// retentionDocs is the repository's own package with one kind per form, and
// one that states none.
func retentionDocs(authority, form string) []map[string]any {
	kind := func(name, retention string) map[string]any {
		data := map[string]any{
			"authority": authority, "package": "keep",
			"names":      map[string]any{"singular": name},
			"properties": map[string]any{"name": map[string]any{"type": "string", "description": "what it is"}},
		}
		if retention != "" {
			data["retention"] = retention
		}
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": authority + "/keep/" + name},
			"data":     data,
		}
	}
	return []map[string]any{
		{
			"kind":     "substrate.reamde.dev/core/package",
			"metadata": map[string]any{"id": authority + "/keep"},
			"data":     map[string]any{"authority": authority, "package": "keep", "version": 1},
		},
		kind("everything", "all"),
		kind("nothing", "none"),
		kind("month", form),
		kind("plain", ""),
	}
}

func TestARepositoryOverridesAKindsRetention(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	own := func(name string) string { return "~/keep/" + name }

	// The three forms, each answered by its kind's definition; a kind that
	// states none carries no key.
	if rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", env.token,
		map[string]any{"documents": retentionDocs(env.repository, "P30D")}); rec.Code/100 != 2 {
		t.Fatalf("apply the three forms: %d %s", rec.Code, rec.Body)
	}
	for name, want := range map[string]string{"everything": "all", "nothing": "none", "month": "P30D", "plain": ""} {
		rec := env.do(t, http.MethodGet, coreKindPath+url.PathEscape(own(name)), env.token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("read the declaration of %s: %d %s", name, rec.Code, rec.Body)
		}
		var kind struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &kind); err != nil {
			t.Fatal(err)
		}
		got, stated := kind.Properties["retention"]
		if (want == "") == stated || (stated && got != want) {
			t.Errorf("the definition of %s answers retention %v (%v), want %q", name, got, stated, want)
		}
	}

	// A malformed value is refused at the apply door, with the reason.
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/apply", env.token,
		map[string]any{"documents": retentionDocs(env.repository, "forever")})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "data.retention") ||
		!strings.Contains(rec.Body.String(), "forever") {
		t.Fatalf("a malformed retention at the apply door = %d %s, want 422 naming data.retention", rec.Code, rec.Body)
	}

	// What applies: each own kind's form, all for the one that states none,
	// and the shipped bookkeeping kind's week.
	got := env.retentions(t)
	for kind, want := range map[string]retentionRow{
		own("everything"):                   {Declared: "all", Effective: "all"},
		own("nothing"):                      {Declared: "none", Effective: "none"},
		own("month"):                        {Declared: "P30D", Effective: "P30D"},
		own("plain"):                        {Effective: "all"},
		kindTriggerRun:                      {Declared: "P7D", Effective: "P7D"},
		"substrate.reamde.dev/core/alert":   {Declared: "P7D", Effective: "P7D"},
		"substrate.reamde.dev/core/contact": {Effective: "all"},
	} {
		want.Kind = kind
		if got[kind] != want {
			t.Errorf("%s reads %+v, want %+v", kind, got[kind], want)
		}
	}

	// The repository overrides a shipped kind's and one of its own.
	override := func(id, kind, value string) int {
		return env.do(t, http.MethodPut, coreRetentionPath+id, env.token, map[string]any{"properties": map[string]any{
			"appliesTo": map[string]any{"ref": "substrate.reamde.dev/core/kind/" + kind},
			"retention": value,
		}}).Code
	}
	for id, o := range map[string][2]string{"runs": {kindTriggerRun, "P30D"}, "month": {own("month"), "none"}} {
		if code := override(id, o[0], o[1]); code/100 != 2 {
			t.Fatalf("override %s with %s: %d", o[0], o[1], code)
		}
	}
	got = env.retentions(t)
	if r := got[kindTriggerRun]; r.Declared != "P7D" || r.Override != "P30D" || r.OverrideID != "runs" || r.Effective != "P30D" {
		t.Errorf("the overridden shipped kind reads %+v", r)
	}
	if r := got[own("month")]; r.Declared != "P30D" || r.Override != "none" || r.Effective != "none" {
		t.Errorf("the overridden own kind reads %+v", r)
	}

	// One override per kind, and the declaration's grammar.
	if code := override("runs-again", kindTriggerRun, "all"); code != http.StatusConflict {
		t.Errorf("a second override of one kind = %d, want 409", code)
	}
	for _, bad := range []string{"P0D", "forever", "P1Y"} {
		if code := override("runs", kindTriggerRun, bad); code != http.StatusUnprocessableEntity {
			t.Errorf("an override of %q = %d, want 422", bad, code)
		}
	}

	// Deleting the override restores what the kind declares.
	if rec := env.do(t, http.MethodDelete, coreRetentionPath+"runs", env.token, nil); rec.Code/100 != 2 {
		t.Fatalf("delete the override: %d %s", rec.Code, rec.Body)
	}
	if r := env.retentions(t)[kindTriggerRun]; r.Override != "" || r.Effective != "P7D" {
		t.Errorf("after the override went, the shipped kind reads %+v, want its declared P7D", r)
	}

	// A scoped app's token reads no kind's retention. A full-access app's is a
	// token without scopes, like a sign-in's (decision 0204), and reads it.
	full := env.mint(t, substrate.MintTokenInput{Label: "Everything", ClientID: "https://everything.example.com", ClientName: "Everything"})
	if rec := env.do(t, http.MethodGet, "/api/v1/retention", full, nil); rec.Code != http.StatusOK {
		t.Errorf("a full-access app's token reads the retentions: %d %s, want 200", rec.Code, rec.Body)
	}
	app := env.mint(t, substrate.MintTokenInput{Label: "Notes", Scopes: []string{"read:" + own("plain")}, ClientID: "https://notes.example.com", ClientName: "Notes"})
	if rec := env.do(t, http.MethodGet, "/api/v1/retention", app, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a scoped app's token reads the retentions: %d %s", rec.Code, rec.Body)
	}
}

// A malformed retention is refused by the catalog and by the boot as it is by
// the apply door: all three load declarations through the one loader, and
// each says why.
func TestAMalformedRetentionIsRefusedAtEveryDoor(t *testing.T) {
	t.Parallel()

	// The catalog: importing a sample whose header states a retention that
	// is no form is refused, and the refusal says why.
	env := newDBEnv(t)
	tree := fstest.MapFS{}
	err := fs.WalkDir(samples.Samples(), "readinglist", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(samples.Samples(), p)
		if err != nil {
			return err
		}
		if filepath.Base(p) == "bundle.yaml" {
			patched := strings.Replace(string(raw), "  package: readinglist\n", "  package: readinglist\n  retention: forever\n", 1)
			if patched == string(raw) {
				return fmt.Errorf("the reading list's header was not where the test breaks it")
			}
			raw = []byte(patched)
		}
		tree[p] = &fstest.MapFile{Data: raw}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(catalog.SampleRoot(tree))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = cat.Import(context.Background(), substrate.ActorPerson, "samples.substrate.reamde.dev/readinglist", env.ds)
	if err == nil || !strings.Contains(err.Error(), "data.retention") || !strings.Contains(err.Error(), "forever") {
		t.Errorf("the catalog's import of a header with a malformed retention: %v, want refused naming data.retention", err)
	}

	// The boot: a binary whose shipped tree carries one does not start, and
	// names the key, before any repository's upgrade could take it.
	root := t.TempDir()
	if err := os.CopyFS(root, kinds.Seed()); err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(root, "substrate.reamde.dev", "core", "triggerrun.yaml")
	raw, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(raw), "  retention: P7D\n", "  retention: P0D\n", 1)
	if patched == string(raw) {
		t.Fatal("the shipped triggerrun declares no retention for the test to break")
	}
	if err := os.WriteFile(runs, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := engine.Open(context.Background(), dbTemplate.Clone(t), engine.WithKindsDir(root),
		engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(dbCredentialKey))
	if err == nil {
		_ = svc.Close()
		t.Fatal("a shipped tree with a malformed retention booted")
	}
	if !strings.Contains(err.Error(), "data.retention") || !strings.Contains(err.Error(), "P0D") {
		t.Fatalf("the boot's refusal does not say why: %v", err)
	}
}
