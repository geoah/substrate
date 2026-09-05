package catalog_test

// A PROVIDER package is published: the catalog's provider tier installs its
// closure with `source: published`, the repository's own token may not write
// its declarations afterwards, and everything else about it stays the user's:
// the records of its kinds, and every bundle lifecycle verb (decision record
// 0048).

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// whoop is the provider these tests install: it declares against nothing, so
// `requires:` brings no sample vocabulary along and what lands is the
// provider's closure alone.
const (
	whoopAuthority = "providers.substrate.reamde.dev"
	whoopPackage   = "whoop"
	whoopID        = whoopAuthority + "/" + whoopPackage
	whoopDir       = "../../kinds/" + whoopAuthority + "/" + whoopPackage

	kindPackageRef = "substrate.reamde.dev/core/package"
	kindKindRef    = "substrate.reamde.dev/core/kind"
)

// bundleLifecycler is the lifecycle half of substrate.BundleOps: the verbs a
// repository user keeps on a provider's bundle.
type bundleLifecycler interface {
	DisableBundle(ctx context.Context, id string) error
	EnableBundle(ctx context.Context, id string) error
	BindBundleInput(ctx context.Context, id, input, record string) error
	UninstallBundle(ctx context.Context, id string) error
	PurgeBundle(ctx context.Context, id string) (int, error)
}

// reopenableDataset is a repository whose SERVICE can be closed and opened
// again on the same database, which is the only way to test what a rebuild
// reads: a dataset holds the registry the write path built, so a stored origin
// that degrades on the way back in is invisible to every assertion made
// against the live one.
type reopenableDataset struct {
	ds  substrate.Dataset
	dsn string
	svc substrate.Service
}

func newReopenableDataset(t *testing.T) *reopenableDataset {
	t.Helper()
	r := &reopenableDataset{dsn: testdb.NewSchema(t)}
	r.open(t)
	ctx := context.Background()
	if _, err := r.svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := r.svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	r.ds = ds
	return r
}

func (r *reopenableDataset) open(t *testing.T) {
	t.Helper()
	svc, err := engine.Open(context.Background(), r.dsn,
		engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithCredentialKey(credKey),
	)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	r.svc = svc
}

// reopen closes the service and opens the repository again from its stored
// rows: the registry the returned dataset holds was rebuilt, never inherited.
func (r *reopenableDataset) reopen(t *testing.T) substrate.Dataset {
	t.Helper()
	if err := r.svc.Close(); err != nil {
		t.Fatalf("close the service: %v", err)
	}
	r.open(t)
	ds, err := r.svc.Dataset(context.Background(), "geoah")
	if err != nil {
		t.Fatalf("reopen the dataset: %v", err)
	}
	r.ds = ds
	return ds
}

// sourceOf reads a declaration row's stored origin.
func sourceOf(t *testing.T, ds substrate.Dataset, kind, id string) string {
	t.Helper()
	rec, err := ds.Get(context.Background(), kind, id)
	if err != nil {
		t.Fatalf("read %s %s: %v", kind, id, err)
	}
	source, _ := rec.Properties["source"].(string)
	return source
}

// whoopClosure is the provider's vocabulary documents as they sit in the tree:
// what a hand `substratectl apply -f` of the same files would send.
func whoopClosure(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, dir := range []string{filepath.Dir(whoopDir), whoopDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			dec := yaml.NewDecoder(strings.NewReader(string(raw)))
			for {
				var d map[string]any
				err := dec.Decode(&d)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("decode %s: %v", e.Name(), err)
				}
				// The closure is the DECLARATIONS; a bundle directory's
				// delivery wiring (its triggers) is data the batch verb does
				// not take.
				kind, _ := d["kind"].(string)
				if vocabulary.VocabularyDocumentKind(vocabulary.KindName(kind)) {
					out = append(out, d)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("the whoop closure read as no documents")
	}
	return out
}

// A provider install writes `published` on the package it lands and on the
// declarations inside it; a sample install, through the very same verb, stays
// `installed`. The tier is what the two installs differ by, not the shape of
// the closure.
func TestProviderInstallLandsPublished(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	if _, err := c.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install %s: %v", whoopID, err)
	}
	if got := sourceOf(t, ds, kindPackageRef, whoopID); got != vocabulary.SourcePublished {
		t.Errorf("package source = %q, want %q", got, vocabulary.SourcePublished)
	}
	if got := sourceOf(t, ds, kindKindRef, whoopID+"/recovery"); got != vocabulary.SourcePublished {
		t.Errorf("kind source = %q, want %q", got, vocabulary.SourcePublished)
	}

	// A sample is the other tier: installed, and the repository's own.
	const peopleID = "samples.substrate.reamde.dev/people"
	if _, err := c.Install(ctx, substrate.ActorAPI, peopleID, ds); err != nil {
		t.Fatalf("install %s: %v", peopleID, err)
	}
	if got := sourceOf(t, ds, kindPackageRef, peopleID); got != vocabulary.SourceInstalled {
		t.Errorf("sample package source = %q, want %q", got, vocabulary.SourceInstalled)
	}
}

// The refusal itself: a published package's declarations are the publisher's,
// so the token that installed it is forbidden to write them and the stored
// declaration does not move.
func TestPublishedDeclarationsRefuseATokenWrite(t *testing.T) {
	repo := newReopenableDataset(t)
	ds := repo.ds
	c := loadCatalog(t)
	ctx := context.Background()

	if _, err := c.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install %s: %v", whoopID, err)
	}
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not support ApplyVocabularyDocuments")
	}

	// The narrowest possible edit: one added property on one mirror kind,
	// which is exactly what breaks the next sync.
	edit := []map[string]any{{
		"kind":     kindKindRef,
		"metadata": map[string]any{"id": whoopID + "/recovery"},
		"data": map[string]any{
			"authority": whoopAuthority,
			"package":   whoopPackage,
			"names":     map[string]any{"singular": "recovery"},
			"properties": map[string]any{
				"mine": map[string]any{"type": "string"},
			},
		},
	}}
	for _, actor := range []substrate.Actor{substrate.ActorAPI, substrate.ActorConsole, substrate.ActorCLI} {
		_, err := applier.ApplyVocabularyDocuments(ctx, actor, edit)
		if !errors.Is(err, substrate.ErrForbidden) {
			t.Fatalf("actor %s writing a published declaration: err = %v, want ErrForbidden", actor, err)
		}
		if !strings.Contains(err.Error(), whoopID) {
			t.Errorf("the refusal does not name the package: %v", err)
		}
	}

	// Nothing landed: the declaration still carries the shipped properties and
	// not the one the refused write named.
	rec, err := ds.Get(ctx, kindKindRef, whoopID+"/recovery")
	if err != nil {
		t.Fatalf("read the recovery declaration: %v", err)
	}
	props, _ := rec.Properties["properties"].(map[string]any)
	if _, added := props["mine"]; added {
		t.Error("the refused write landed its property anyway")
	}
	if _, shipped := props["recoveryScore"]; !shipped {
		t.Error("the refused write replaced the shipped declaration")
	}

	// AND IT SURVIVES THE REBUILD. Everything above was decided by the registry
	// the install itself built; the ownership a repository actually runs under
	// is the one its rows rebuild into at open (engine storedPackages), so the
	// refusals are asked again on the far side of a restart.
	ds = repo.reopen(t)
	if got := sourceOf(t, ds, kindPackageRef, whoopID); got != vocabulary.SourcePublished {
		t.Fatalf("package source after a reopen = %q, want %q", got, vocabulary.SourcePublished)
	}
	if got := sourceOf(t, ds, kindKindRef, whoopID+"/recovery"); got != vocabulary.SourcePublished {
		t.Fatalf("kind source after a reopen = %q, want %q", got, vocabulary.SourcePublished)
	}
	applier, ok = ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("the reopened dataset does not support ApplyVocabularyDocuments")
	}
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, edit); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a batch apply after a reopen: err = %v, want ErrForbidden", err)
	}
	// The other door into the same chokepoint: a declaration record is a
	// record, so a PATCH of one is a vocabulary batch of one.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, kindKindRef, whoopID+"/recovery", substrate.PatchInput{
		Properties: map[string]any{"description": "mine now"},
	}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a declaration PATCH after a reopen: err = %v, want ErrForbidden", err)
	}
	rec, err = ds.Get(ctx, kindKindRef, whoopID+"/recovery")
	if err != nil {
		t.Fatalf("read the recovery declaration after a reopen: %v", err)
	}
	if got, _ := rec.Properties["description"].(string); got == "mine now" {
		t.Error("the refused PATCH landed anyway")
	}
}

// What a published package does NOT close: the records of its kinds are the
// repository's to write, and every lifecycle verb on its bundle still runs for
// the user.
func TestAProvidersRecordsAndLifecycleStayTheUsers(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	if _, err := c.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install %s: %v", whoopID, err)
	}

	// A data record of a provider kind, written by the same token the
	// declaration write above refused.
	account, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: whoopID + "/account", ID: "mine",
		Properties: map[string]any{"enabledRecovery": true},
	})
	if err != nil {
		t.Fatalf("write a record of a provider kind: %v", err)
	}
	if got, _ := account.Properties["enabledRecovery"].(bool); !got {
		t.Errorf("enabledRecovery = %v, want true", account.Properties["enabledRecovery"])
	}

	// And the lifecycle verbs: disable, then enable, both of them the
	// substrate's own hand writing the bundle declaration on the user's word.
	ops, ok := ds.(bundleLifecycler)
	if !ok {
		t.Fatal("dataset does not offer the bundle lifecycle verbs")
	}
	if err := ops.DisableBundle(ctx, whoopID); err != nil {
		t.Fatalf("disable a published bundle: %v", err)
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, whoopID)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.Enabled {
		t.Error("the bundle reports enabled after a disable")
	}
	if err := ops.EnableBundle(ctx, whoopID); err != nil {
		t.Fatalf("enable a published bundle: %v", err)
	}
	st, err = ds.(bundleStatuser).BundleStatus(ctx, whoopID)
	if err != nil {
		t.Fatalf("bundle status after enable: %v", err)
	}
	if !st.Enabled {
		t.Error("the bundle reports disabled after an enable")
	}

	// BIND writes the bundle's own DECLARATION row (a managed property on the
	// record the chokepoint guards), so it is the lifecycle verb a published
	// package is likeliest to catch, and the one worth naming.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: whoopID + "/config", ID: "client",
		Properties: map[string]any{"clientId": "abc"},
	}); err != nil {
		t.Fatalf("write the bundle's input record: %v", err)
	}
	if err := ops.BindBundleInput(ctx, whoopID, "client", "client"); err != nil {
		t.Fatalf("bind an input on a published bundle: %v", err)
	}
	st, err = ds.(bundleStatuser).BundleStatus(ctx, whoopID)
	if err != nil {
		t.Fatalf("bundle status after bind: %v", err)
	}
	bound := false
	for _, in := range st.Inputs {
		if in.Name == "client" && in.Via == substrate.InputViaBound {
			bound = true
		}
	}
	if !bound {
		t.Errorf("the input did not resolve to the bound record: %+v", st.Inputs)
	}

	// A record of a provider kind deletes like any other.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, whoopID+"/account", "mine"); err != nil {
		t.Fatalf("delete a record of a published kind: %v", err)
	}

	// And the destructive order runs to the end: disable, purge, uninstall.
	// Purge clears the data the uninstall's refuse-with-instances guard counts,
	// and the uninstall tears the published declarations down whole, through
	// the same admission a write into them is refused by.
	if err := ops.DisableBundle(ctx, whoopID); err != nil {
		t.Fatalf("disable before purge: %v", err)
	}
	if _, err := ops.PurgeBundle(ctx, whoopID); err != nil {
		t.Fatalf("purge a published bundle: %v", err)
	}
	if err := ops.UninstallBundle(ctx, whoopID); err != nil {
		t.Fatalf("uninstall a published bundle: %v", err)
	}
	if _, err := ds.KindByRef(ctx, whoopID+"/recovery"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("a published kind survived the uninstall: %v", err)
	}
	// A torn-down declaration row reads back tombstoned, which is how the
	// changelog says the package left.
	row, err := ds.Get(ctx, kindPackageRef, whoopID)
	if err != nil || row.DeletedAt == nil {
		t.Errorf("the published package row was not pruned: %+v %v", row, err)
	}
}

// The promotion, which is the only thing that moves a stored `source` forward:
// a repository holding the provider as `installed` (a hand apply of the same
// files, which record 0047 sanctions) is promoted by the next catalog install
// of it, and the boot upgrade never does it.
func TestAnInstalledProviderIsPromotedByTheCatalogInstall(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not support ApplyVocabularyDocuments")
	}

	// The hand door: the closure's own files, applied by the repository's
	// token. What lands is the repository's, and it stays writable.
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, whoopClosure(t)); err != nil {
		t.Fatalf("apply the whoop closure by hand: %v", err)
	}
	if got := sourceOf(t, ds, kindPackageRef, whoopID); got != vocabulary.SourceInstalled {
		t.Fatalf("a hand-applied closure landed source %q, want %q", got, vocabulary.SourceInstalled)
	}

	// The same closure through the catalog's provider tier promotes it, package
	// row and declarations together.
	if _, err := loadCatalog(t).Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install %s over the hand-applied closure: %v", whoopID, err)
	}
	if got := sourceOf(t, ds, kindPackageRef, whoopID); got != vocabulary.SourcePublished {
		t.Errorf("package source after the catalog install = %q, want %q", got, vocabulary.SourcePublished)
	}
	if got := sourceOf(t, ds, kindKindRef, whoopID+"/sleep"); got != vocabulary.SourcePublished {
		t.Errorf("kind source after the catalog install = %q, want %q", got, vocabulary.SourcePublished)
	}

	// And it stuck: the write the hand door allowed a moment ago is refused now.
	_, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{{
		"kind":     kindKindRef,
		"metadata": map[string]any{"id": whoopID + "/sleep"},
		"data": map[string]any{
			"authority": whoopAuthority, "package": whoopPackage,
			"names":      map[string]any{"singular": "sleep"},
			"properties": map[string]any{"mine": map[string]any{"type": "string"}},
		},
	}})
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("writing a promoted package: err = %v, want ErrForbidden", err)
	}
}

// A published package is an ordinary upgrade target: the preview reads its
// stored versions and the install verb re-applies it, exactly as it does an
// installed one.
func TestPublishedPackageStillUpgrades(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	if _, err := c.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install %s: %v", whoopID, err)
	}
	plan, err := c.Upgrade(ctx, whoopID, ds)
	if err != nil {
		t.Fatalf("upgrade preview: %v", err)
	}
	if plan == nil {
		t.Fatal("no upgrade preview for an installed provider")
	}
	if len(plan.Blockers) != 0 {
		t.Errorf("the preview reports blockers on a freshly installed provider: %v", plan.Blockers)
	}
	if _, err := c.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("re-install a published package: %v", err)
	}
	if got := sourceOf(t, ds, kindPackageRef, whoopID); got != vocabulary.SourcePublished {
		t.Errorf("package source after re-install = %q, want %q", got, vocabulary.SourcePublished)
	}
}

// THE AUTHORITY ROW IS NOT PUBLISHED. One `authority` document travels with
// every closure published under it (the catalog reads a package directory plus
// the documents above it), so publishing that row on the first provider
// install would close `providers.substrate.reamde.dev` itself: every later hand
// apply of any sibling closure carries the same document and would be refused.
// This is the order that catches it, a hand apply first and a catalog install
// of a sibling second.
func TestAProviderInstallLeavesTheAuthorityRowOpen(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not support ApplyVocabularyDocuments")
	}

	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, whoopClosure(t)); err != nil {
		t.Fatalf("apply the whoop closure by hand: %v", err)
	}
	const notionID = whoopAuthority + "/notion"
	if _, err := loadCatalog(t).Install(ctx, substrate.ActorAPI, notionID, ds); err != nil {
		t.Fatalf("install %s: %v", notionID, err)
	}
	if got := sourceOf(t, ds, kindPackageRef, notionID); got != vocabulary.SourcePublished {
		t.Fatalf("notion source = %q, want %q", got, vocabulary.SourcePublished)
	}
	// The authority row is the shared one, and it stays the repository's.
	if got := sourceOf(t, ds, "substrate.reamde.dev/core/authority", whoopAuthority); got != vocabulary.SourceInstalled {
		t.Errorf("authority source = %q, want %q", got, vocabulary.SourceInstalled)
	}

	// So the first provider re-applies by hand, authority document and all,
	// which is the door the catalog install must not have shut.
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, whoopClosure(t)); err != nil {
		t.Fatalf("re-apply the whoop closure by hand after a provider install: %v", err)
	}
	if got := sourceOf(t, ds, kindPackageRef, whoopID); got != vocabulary.SourceInstalled {
		t.Errorf("the hand-applied package source = %q, want %q", got, vocabulary.SourceInstalled)
	}
	// And the published sibling is still published: a hand apply beside it does
	// not demote it.
	if got := sourceOf(t, ds, kindPackageRef, notionID); got != vocabulary.SourcePublished {
		t.Errorf("notion source after the hand apply = %q, want %q", got, vocabulary.SourcePublished)
	}
}
