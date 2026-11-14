package testenv_test

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
)

// A repository of the release that a binary started on while it refused the
// shipped upgrade, as every binary did from the actor links until the string
// to reference count was mended: it served on the declarations the release
// stored, so a blob's upload and a package's declaration were refused (each
// stamps a link where the stored declaration still says string), while
// records, tokens and sign-ins went on. The next start on a binary that
// admits the upgrade lands it, and nothing the repository held, or took in
// between, is lost.
func TestAKeptDirectoryStartedUnderARefusedUpgradeUpgradesAtTheNextStart(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	root := t.TempDir()
	copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(root, "repositories"))
	dsn := testdb.NewSchema(t)
	start := func(refusals keptRefusals, opts ...engine.Option) *testenv.Env {
		e := testenv.Start(t, testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
			testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey),
			testenv.WithEngineOptions(append([]engine.Option{engine.WithLogger(slog.New(refusals))}, opts...)...))
		e.Token = fixture.Token
		return e
	}
	blob := func(e *testenv.Env) (int, string) {
		status, raw, _ := e.DoRaw(http.MethodPut, "/api/v1/blobs?name=between.txt", []byte("between"), map[string]string{"Content-Type": "text/plain"})
		return status, string(raw)
	}
	declare := func(e *testenv.Env, name string) (int, string) {
		status, raw := e.Do(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": []any{
			map[string]any{
				"kind": "substrate.reamde.dev/core/package", "metadata": map[string]any{"id": fixture.Repository + "/" + name},
				"data": map[string]any{"authority": fixture.Repository, "package": name, "version": 1},
			},
			map[string]any{
				"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": fixture.Repository + "/" + name + "/note"},
				"data": map[string]any{
					"authority": fixture.Repository, "package": name, "names": map[string]any{"singular": "note"},
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				},
			},
		}})
		return status, string(raw)
	}

	before := newKeptRefusals()
	e := start(before, engine.WithTestShippedUpgradeRefusal(func(string) []string {
		return []string{"refused as the string to reference count refused it"}
	}))
	if status, raw := blob(e); status != http.StatusUnprocessableEntity || !strings.Contains(raw, "createdBy") {
		t.Errorf("a blob's upload under the refused upgrade = %d %s, want refused on createdBy", status, raw)
	}
	if status, raw := declare(e, "between"); status != http.StatusUnprocessableEntity || !strings.Contains(raw, "declaredBy") {
		t.Errorf("a package's declaration under the refused upgrade = %d %s, want refused on declaredBy", status, raw)
	}
	var contact struct {
		ID string `json:"id"`
	}
	e.MustJSON(http.MethodPost, "/api/v1/records", map[string]any{
		"kind": "substrate.reamde.dev/core/contact", "properties": map[string]any{"name": "Between"},
	}, &contact)
	if lines := before.lines(); len(lines) != 1 || !strings.Contains(lines[0], "refused as the string to reference count") {
		t.Errorf("the first start's refusals: %v, want the one the hook made", lines)
	}
	if diffs := keptSeededVersions(t, e, "fresh.example.com"); len(diffs) == 0 {
		t.Error("the refused upgrade left every seeded package at this binary's version: the hook refused nothing")
	}
	held := keptRead(t, e)
	e.Stop()

	after := newKeptRefusals()
	e = start(after)
	got := keptRead(t, e)
	if lines := after.lines(); len(lines) > 0 {
		t.Errorf("the next start refused the shipped upgrade:\n  %s", strings.Join(lines, "\n  "))
	}
	for _, d := range keptSeededVersions(t, e, "fresh-again.example.com") {
		t.Error(d)
	}
	for path := range held {
		if _, ok := got[path]; !ok {
			t.Errorf("%s was served under the refused upgrade and is gone after it landed", path)
		}
	}
	if rec := got["substrate.reamde.dev/core/contact/"+contact.ID]; rec.Read == nil {
		t.Error("the contact written under the refused upgrade did not survive it")
	}
	if status, raw := blob(e); status != http.StatusCreated {
		t.Errorf("a blob's upload after the upgrade landed = %d %s", status, raw)
	}
	if status, raw := declare(e, "after"); status/100 != 2 {
		t.Errorf("a package's declaration after the upgrade landed = %d %s", status, raw)
	}
}
