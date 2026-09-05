package catalog_test

// A PROVIDER package is published: the catalog's provider tier installs its
// closure with `source: published`, the repository's own token may not write
// its declarations afterwards, and everything else about it stays the user's —
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

	"github.com/geoah/substrate/internal/substrate"
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

// whoopClosure is the provider's vocabulary documents as they sit in the tree —
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
// `installed` — the tier decides, not the closure's shape.
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
	ds := newDataset(t)
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
}

// The promotion, which is the only thing that moves a stored `source` forward:
// a repository holding the provider as `installed` — a hand apply of the same
// files, the door record 0047 sanctions — is promoted by the next catalog
// install of it, and the boot upgrade never does it.
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
