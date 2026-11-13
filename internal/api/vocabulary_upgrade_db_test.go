package api

import (
	"context"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/kinds"
)

// upgradeReopenedEnv is a dbEnv over a repository that lived under one binary
// and is served by the next: the repository is created under the shipped tree
// with newDBEnv's options, before arranges it on that first open, and the
// engine is closed and opened again over the same database and data root with
// reopen added. What the second open does at a repository's first open (the
// shipped upgrade, the unit seed) has run when the first request arrives.
func upgradeReopenedEnv(t *testing.T, before func(ctx context.Context, ds substrate.Dataset), reopen ...engine.Option) *dbEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	clock := &testClock{t: time.Now().UTC().Truncate(time.Second)}
	base := []engine.Option{
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(dbCredentialKey),
		engine.WithTestTOTPClock(clock.now),
		engine.WithLLMProviderSeed(false),
		engine.WithUnitSeed(false),
		engine.WithRecordSeed(false),
	}
	dsn := dbTemplate.Clone(t)
	repository := testdb.Repository(t)
	first, err := engine.Open(ctx, dsn, base...)
	if err != nil {
		t.Fatalf("open the first engine: %v", err)
	}
	if _, err := first.CreateRepository(ctx, repository); err != nil {
		_ = first.Close()
		t.Fatalf("create repository: %v", err)
	}
	ds, err := first.Dataset(ctx, repository)
	if err != nil {
		_ = first.Close()
		t.Fatalf("open dataset: %v", err)
	}
	before(ctx, ds)
	if err := first.Close(); err != nil {
		t.Fatalf("close the first engine: %v", err)
	}

	svc, err := engine.Open(ctx, dsn, append(base, reopen...)...)
	if err != nil {
		t.Fatalf("open the second engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	env := &dbEnv{svc: svc, repository: repository}
	env.testEnv = &testEnv{h: New(Config{Service: seamService{Service: svc, env: env}, Now: clock.now, InviteCode: testInviteCode}), clock: clock}
	if env.ds, err = svc.Dataset(ctx, repository); err != nil {
		t.Fatalf("reopen dataset: %v", err)
	}
	env.token = env.mint(t, substrate.MintTokenInput{Label: "test"})
	return env
}

// upgradeMovedTree is a shipped tree with each named file rewritten: binary
// N+1's tree, against a repository binary N wrote.
func upgradeMovedTree(t *testing.T, src fs.FS, rewrite map[string]func(string) string) fs.FS {
	t.Helper()
	tree := fstest.MapFS{}
	err := fs.WalkDir(src, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(src, name)
		if err != nil {
			return err
		}
		if edit, ok := rewrite[name]; ok {
			raw = []byte(edit(string(raw)))
			delete(rewrite, name)
		}
		tree[name] = &fstest.MapFile{Data: raw, Mode: 0o644}
		return nil
	})
	if err != nil {
		t.Fatalf("copy the shipped tree: %v", err)
	}
	for name := range rewrite {
		t.Fatalf("the shipped tree has no %s", name)
	}
	return tree
}

// upgradeDeclaredVersion matches a declaration's own `version:`, at the data
// block's indentation, so a property named version is never the match.
var upgradeDeclaredVersion = regexp.MustCompile(`\n  version: (\d+)\n`)

// upgradeVersionOf is the first declaration version a shipped seed file
// carries.
func upgradeVersionOf(t *testing.T, name string) int64 {
	t.Helper()
	raw, err := fs.ReadFile(kinds.Seed(), name)
	if err != nil {
		t.Fatal(err)
	}
	m := upgradeDeclaredVersion.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("%s declares no version of its own", name)
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// upgradePin rewrites the first declaration version in doc to v.
func upgradePin(t *testing.T, doc string, v int64) string {
	t.Helper()
	loc := upgradeDeclaredVersion.FindStringIndex(doc)
	if loc == nil {
		t.Fatal("the declaration no longer pins a version of its own")
	}
	return doc[:loc[0]] + "\n  version: " + strconv.FormatInt(v, 10) + "\n" + doc[loc[1]:]
}

// A withheld shipped upgrade is readable by a repository token: the package, the
// versions the boot compared and the guard lines it refused on, the same
// shape a catalog entry's `upgrade` carries. Binary N+1 narrows llm/provider's
// `label` to an int while a live row holds a string, so its boot refuses the
// upgrade and opens the repository on the stored declarations.
func TestVocabularyUpgradeServesTheShippedPreview(t *testing.T) {
	t.Parallel()
	const (
		header   = "substrate.reamde.dev/llm/llm.yaml"
		provider = "substrate.reamde.dev/llm/provider.yaml"
		moved    = int64(99)
	)
	from, kindFrom := upgradeVersionOf(t, header), upgradeVersionOf(t, provider)
	tree := upgradeMovedTree(t, kinds.Seed(), map[string]func(string) string{
		header: func(doc string) string { return upgradePin(t, doc, moved) },
		provider: func(doc string) string {
			const label = "    label:\n      type: string\n"
			if !strings.Contains(doc, label) {
				t.Fatal("llm/provider no longer declares `label` as a plain string")
			}
			return upgradePin(t, strings.Replace(doc, label, "    label:\n      type: int\n", 1), moved)
		},
	})
	env := upgradeReopenedEnv(t, func(ctx context.Context, ds substrate.Dataset) {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: llmPackageRef + "/provider", ID: "guarded",
			Properties: map[string]any{"label": "a string label", "wire": "openai", "baseURL": "https://llm.example.com/v1"},
		}); err != nil {
			t.Fatalf("put the guarded row: %v", err)
		}
	}, engine.WithKindsFS(tree))

	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	var got *substrate.ShippedUpgrade
	for _, item := range decodeJSON[substrate.OperationalList[substrate.ShippedUpgrade]](t, rec).Items {
		if item.Package == llmPackageRef {
			got = &item
		}
	}
	if got == nil {
		t.Fatalf("the preview lists no entry for %s: %s", llmPackageRef, rec.Body)
	}
	if !got.Upgrade.Available || got.Upgrade.From != from || got.Upgrade.To != moved {
		t.Fatalf("entry = %+v, want available %d -> %d", got.Upgrade, from, moved)
	}
	var change *substrate.BundleUpgradeChange
	for i := range got.Upgrade.Changes {
		if got.Upgrade.Changes[i].ID == llmPackageRef+"/provider" {
			change = &got.Upgrade.Changes[i]
		}
	}
	if change == nil || change.Kind != "kind" || change.From != kindFrom || change.To != moved {
		t.Fatalf("changes = %+v, want the provider kind %d -> %d", got.Upgrade.Changes, kindFrom, moved)
	}
	blocked := strings.Join(got.Upgrade.Blockers, "; ")
	if !strings.Contains(blocked, llmPackageRef+"/provider") || !strings.Contains(blocked, `"label"`) {
		t.Fatalf("blockers = %q, want the guard line naming the kind and the property", got.Upgrade.Blockers)
	}
	// The repository still speaks the stored shape: the boot wrote nothing.
	env.put(t, llmPackageRef+"/provider", "after", map[string]any{"label": "still a string", "wire": "anthropic"})

	// No token, no read: the preview is a repository read like the catalog.
	rec = env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", "", nil)
	wantStatus(t, rec, http.StatusUnauthorized)
}

// A repository the running binary seeded has nothing to upgrade, and the
// preview says so per package: one entry for every package the binary ships
// and seeds, none of them available, none blocked (docs/bundles.md).
func TestVocabularyUpgradeListsEveryPackageUpToDate(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	listed := map[string]bool{}
	for _, item := range decodeJSON[substrate.OperationalList[substrate.ShippedUpgrade]](t, rec).Items {
		listed[item.Package] = true
		if item.Upgrade.Available || len(item.Upgrade.Blockers) != 0 || len(item.Upgrade.Changes) != 0 || item.Upgrade.From != item.Upgrade.To {
			t.Errorf("%s previews a move on an up-to-date repository: %+v", item.Package, item.Upgrade)
		}
	}
	for _, pkg := range kinds.SeedPackages {
		if !listed[pkg] {
			t.Errorf("the preview lists no entry for %s: %s", pkg, rec.Body)
		}
	}
}
