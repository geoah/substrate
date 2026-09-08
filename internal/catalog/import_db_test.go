package catalog_test

// The SAMPLE door against a REAL engine (decision record 0048): importing
// `samples/tasks` into a fresh repository lands its kinds under the
// repository's OWN authority as the repository's own vocabulary, the
// placeholder never reaches the changelog, a second import is an ordinary
// re-apply, and the provider door is untouched.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// The authority newDataset registers its repository with. Every imported
	// sample lands under it.
	homeAuthority    = "geoah.example.com"
	samplesPlacehldr = "samples.substrate.reamde.dev"
	tasksSampleID    = samplesPlacehldr + "/tasks"
	peopleSampleID   = samplesPlacehldr + "/people"
	schedulingSample = samplesPlacehldr + "/scheduling"
)

// importSamples takes each sample the user's way, in the order `requires:`
// forces.
func importSamples(t *testing.T, c *catalog.Catalog, ds substrate.Dataset, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, _, err := c.Import(context.Background(), substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("import %s: %v", id, err)
		}
	}
}

func TestImportLandsASampleUnderTheRepositoryAuthority(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	// `requires:` is rehomed with everything else, so a sample that declares
	// against another is refused until THAT one is imported, and the refusal
	// names it under this repository's authority, which is where it will land.
	_, _, err := c.Import(ctx, substrate.ActorAPI, tasksSampleID, ds)
	if err == nil {
		t.Fatal("imported a sample whose required vocabulary is absent")
	}
	if !strings.Contains(err.Error(), homeAuthority+"/people") {
		t.Errorf("the refusal does not name what to import first: %v", err)
	}

	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)

	// What landed is the repository's own kind, at the repository's own
	// authority, with the sample's package (record 0047), and it is
	// `installed`, so the repository's token may add a property to it
	// tomorrow.
	for _, name := range []string{"project", "task", "tasklog"} {
		ref := homeAuthority + "/tasks/" + name
		info, err := ds.KindByRef(ctx, ref)
		if err != nil {
			t.Fatalf("kind %s absent after import: %v", ref, err)
		}
		if info.Source != vocabulary.SourceInstalled {
			t.Errorf("%s source = %q, want %q", ref, info.Source, vocabulary.SourceInstalled)
		}
	}
	// And the placeholder landed nothing at all.
	if _, err := ds.KindByRef(ctx, tasksSampleID+"/task"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("the placeholder kind is live after an import: %v", err)
	}

	// The bundle record is the landed package, not the shipped id.
	b, _ := c.ByID(tasksSampleID)
	st, err := ds.(bundleStatuser).BundleStatus(ctx, b.LandedID(homeAuthority))
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || st.Kinds != 3 {
		t.Errorf("status = %+v, want installed with 3 kinds", st)
	}
	// The import stamps the shipped id and the shipped version on the landed
	// package row, and a copy nobody has edited reads unmodified. Without the
	// stamp `geoah.example.com/tasks` would be indistinguishable from a
	// package the user declared by hand.
	if b.Version == 0 {
		t.Fatal("the tasks sample ships no package version, so the stamp cannot be checked")
	}
	wantOrigin(t, st, tasksSampleID, b.Version, false)
	pkg, err := ds.Get(ctx, kindPackageRef, homeAuthority+"/tasks")
	if err != nil {
		t.Fatalf("read the landed package row: %v", err)
	}
	if got, _ := pkg.Properties["origin"].(string); got != tasksSampleID {
		t.Errorf("package row origin = %q, want %q", got, tasksSampleID)
	}
	if got, _ := vocabulary.VersionValue(pkg.Properties["originVersion"]); got != b.Version {
		t.Errorf("package row originVersion = %d, want %d", got, b.Version)
	}
	if got, _ := pkg.Properties["originDigest"].(string); got == "" {
		t.Error("package row carries no originDigest, so nothing can say whether the copy was edited")
	}

	// THE PLACEHOLDER NEVER REACHES THE CHANGELOG AS VOCABULARY. The changelog
	// is the truth a rebuild reads, so a declaration there under the
	// placeholder is vocabulary under an authority the repository does not
	// own, folded back on every rebuild. The one sanctioned mention is the
	// `origin` stamp the package row's delta sets, a string naming where the
	// copy came from, which declares nothing; nowhere else, on no other kind.
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 5000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("the import wrote no changelog entries")
	}
	for _, ch := range changes {
		raw, err := json.Marshal(ch)
		if err != nil {
			t.Fatalf("marshal change %d: %v", ch.Seq, err)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode change %d: %v", ch.Seq, err)
		}
		if mentionsPlaceholder(decoded, nil, samplesPlacehldr, ch.Kind == kindPackageRef) {
			t.Fatalf("changelog entry %d still names %s: %s", ch.Seq, samplesPlacehldr, raw)
		}
	}

	// A second import is an ordinary re-apply of what the repository now owns:
	// no error, and nothing new.
	before := len(changes)
	importSamples(t, c, ds, tasksSampleID)
	st2, err := ds.(bundleStatuser).BundleStatus(ctx, b.LandedID(homeAuthority))
	if err != nil {
		t.Fatalf("bundle status after re-import: %v", err)
	}
	if st2.Kinds != st.Kinds {
		t.Errorf("re-import changed the closure: %+v -> %+v", st, st2)
	}
	after, err := ds.Changes(ctx, 0, substrate.ChangeFilter{}, 5000)
	if err != nil {
		t.Fatalf("changes after re-import: %v", err)
	}
	if len(after) != before {
		t.Errorf("re-import wrote %d changelog entries, want none", len(after)-before)
	}
	wantOrigin(t, st2, tasksSampleID, b.Version, false)

	// ONE KIND EDIT, THROUGH THE ORDINARY APPLY, and the copy reads modified.
	// The package's version does not move for a kind edit (the kind's own
	// does), so this is what the digest is for. The provenance itself stays:
	// the edit re-projects the package row and the stamp is the engine's.
	editKind(t, ds, homeAuthority+"/tasks/task", "mine")
	st3, err := ds.(bundleStatuser).BundleStatus(ctx, b.LandedID(homeAuthority))
	if err != nil {
		t.Fatalf("bundle status after the edit: %v", err)
	}
	wantOrigin(t, st3, tasksSampleID, b.Version, true)

	// An ACTOR declared into the package is an edit too: the digest keeps every
	// actor row but the two the package itself stands for.
	importSamples(t, c, ds, tasksSampleID)
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not support ApplyVocabularyDocuments")
	}
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.ActorManifest(homeAuthority+"/tasks", "helper"),
	}); err != nil {
		t.Fatalf("declare an actor into the copy: %v", err)
	}
	stActor, err := ds.(bundleStatuser).BundleStatus(ctx, b.LandedID(homeAuthority))
	if err != nil {
		t.Fatalf("bundle status after the actor: %v", err)
	}
	wantOrigin(t, stActor, tasksSampleID, b.Version, true)

	// A re-import REPLACES the package (record 0048), edits included, and
	// re-stamps it: the copy is the shipped closure again and reads so, even
	// though its versions now sit above the shipped ones.
	importSamples(t, c, ds, tasksSampleID)
	st4, err := ds.(bundleStatuser).BundleStatus(ctx, b.LandedID(homeAuthority))
	if err != nil {
		t.Fatalf("bundle status after the second re-import: %v", err)
	}
	wantOrigin(t, st4, tasksSampleID, b.Version, false)
	if _, still := declaredProperties(t, ds, homeAuthority+"/tasks/task")["mine"]; still {
		t.Error("the re-import kept the local edit, so the copy is not the shipped closure")
	}
}

// declaredProperties reads a kind declaration's stored `properties` map.
func declaredProperties(t *testing.T, ds substrate.Dataset, ref string) map[string]any {
	t.Helper()
	rec, err := ds.Get(context.Background(), kindKindRef, ref)
	if err != nil {
		t.Fatalf("read the %s declaration: %v", ref, err)
	}
	return mapOf(rec.Properties["properties"])
}

// Every sample the tree ships lands as a copy that compares equal to itself:
// the digest the import stamps is the digest the status recomputes from the
// stored rows, across kinds, traits, property types, mappings, functions,
// agents and bundle documents alike. One sample whose read-back differs from
// its projection would read modified the moment it landed.
func TestEveryImportedSampleReadsUnmodified(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	// `requires:` forces an order; a refused import is retried after the rest
	// of the pass, so the loop needs no knowledge of the graph.
	var pending []*catalog.Bundle
	for _, b := range c.Bundles() {
		if b.Tier == substrate.TierSample {
			pending = append(pending, b)
		}
	}
	for len(pending) > 0 {
		var next []*catalog.Bundle
		for _, b := range pending {
			if _, _, err := c.Import(ctx, substrate.ActorAPI, b.ID, ds); err != nil {
				next = append(next, b)
			}
		}
		if len(next) == len(pending) {
			t.Fatalf("no sample imports: %d left", len(next))
		}
		pending = next
	}
	for _, b := range c.Bundles() {
		if b.Tier != substrate.TierSample {
			continue
		}
		st, err := ds.(bundleStatuser).BundleStatus(ctx, b.LandedID(homeAuthority))
		if err != nil {
			t.Fatalf("bundle status %s: %v", b.ID, err)
		}
		wantOrigin(t, st, b.ID, b.Version, false)
	}
}

// A closure applied BY HAND has no origin to name: it is the repository's own
// package from the first write, and its status says so by carrying no
// provenance at all.
func TestAHandAppliedClosureCarriesNoOrigin(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not support ApplyVocabularyDocuments")
	}
	const pkg = homeAuthority + "/hand"
	closure := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "declared by hand",
			"installs":    []any{pkg + "/widget"},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, closure); err != nil {
		t.Fatalf("apply a closure by hand: %v", err)
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, pkg)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	wantOrigin(t, st, "", 0, false)
}

// wantOrigin holds a status to the provenance it should carry.
func wantOrigin(t *testing.T, st substrate.BundleStatus, origin string, version int64, modified bool) {
	t.Helper()
	if st.Origin != origin || st.OriginVersion != version || st.Modified != modified {
		t.Errorf("%s: origin = %q v%d modified=%v, want %q v%d modified=%v",
			st.ID, st.Origin, st.OriginVersion, st.Modified, origin, version, modified)
	}
}

// editKind adds one string property to a landed kind through the ordinary
// vocabulary apply, from the declaration as the registry holds it.
func editKind(t *testing.T, ds substrate.Dataset, ref, property string) {
	t.Helper()
	ctx := context.Background()
	info, err := ds.KindByRef(ctx, ref)
	if err != nil {
		t.Fatalf("kind %s: %v", ref, err)
	}
	data := make(map[string]any, len(info.Definition))
	for k, v := range info.Definition {
		data[k] = v
	}
	props := map[string]any{}
	for k, v := range mapOf(data["properties"]) {
		props[k] = v
	}
	props[property] = map[string]any{"type": "string"}
	data["properties"] = props
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not support ApplyVocabularyDocuments")
	}
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{{
		"kind":     kindKindRef,
		"metadata": map[string]any{"id": ref},
		"data":     data,
	}}); err != nil {
		t.Fatalf("edit %s: %v", ref, err)
	}
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// mentionsPlaceholder walks a decoded changelog entry, `path` being the keys
// above v, and reports whether any string in it contains needle. The one
// exemption is the `origin` the PACKAGE row's delta sets (`…delta.set.origin`
// on an entry whose kind is the package kind); an `origin` anywhere else, or
// on any other kind, is a mention like any other.
func mentionsPlaceholder(v any, path []string, needle string, packageRow bool) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, needle)
	case []any:
		for _, item := range x {
			if mentionsPlaceholder(item, path, needle, packageRow) {
				return true
			}
		}
	case map[string]any:
		n := len(path)
		underSet := n >= 2 && path[n-2] == "delta" && path[n-1] == "set"
		for k, item := range x {
			if k == "origin" && packageRow && underSet {
				continue
			}
			if mentionsPlaceholder(item, append(path, k), needle, packageRow) {
				return true
			}
		}
	}
	return false
}

// The two doors are not interchangeable, and the PROVIDER door still works:
// whoop installs under the authority that publishes it, unrehomed.
func TestImportRefusesAProviderAndInstallStillWorks(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	const whoop = "providers.substrate.reamde.dev/whoop"
	_, _, err := c.Import(ctx, substrate.ActorAPI, whoop, ds)
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("import of a provider = %v, want a validation refusal", err)
	}
	if !strings.Contains(err.Error(), "install") {
		t.Errorf("the refusal does not name the verb that works: %v", err)
	}

	if _, _, err := c.Install(ctx, substrate.ActorAPI, whoop, ds); err != nil {
		t.Fatalf("install a provider: %v", err)
	}
	// It landed under the authority that publishes it, untouched.
	if _, err := ds.KindByRef(ctx, whoop+"/workout"); err != nil {
		t.Errorf("the provider's kind is absent after install: %v", err)
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, whoop)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed {
		t.Error("the provider is not marked installed")
	}
	// A provider is published, not copied: it lands the id it was asked for
	// and records no origin.
	wantOrigin(t, st, "", 0, false)
}
