package catalog_test

// A SAMPLE's upgrade preview against a REAL engine (decision record 0070): the
// copy's origin stamp is what the preview reads, so a shipped sample that moved
// past the version the copy was taken at is offered, an edited copy says the
// re-import would discard the edits and needs the confirmation the preview
// hands out, a requires floor the repository does not meet blocks it, and a
// hand-rehomed apply that names its origin is stamped the same way.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// heldStatus is the status of the copy this repository holds for a sample: what
// the API hands the catalog's preview.
func heldStatus(t *testing.T, ds substrate.Dataset, landed string) substrate.BundleStatus {
	t.Helper()
	st, err := ds.(bundleStatuser).BundleStatus(context.Background(), landed)
	if err != nil {
		t.Fatalf("bundle status %s: %v", landed, err)
	}
	return st
}

// bumpedTasks is binary N+1's tasks sample: the package a version ahead with
// one optional property added to the task kind, whose own pin moves with it.
func bumpedTasks(t *testing.T, shipped int64) *catalog.Catalog {
	t.Helper()
	return movedTasksSampleCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string { return bumpTasksPackage(t, shipped, doc) },
		"task.yaml": func(doc string) string {
			doc = bumpTaskPin(t, doc)
			return mustReplace(t, doc, "    status:",
				"    upgradeProbe:\n      type: string\n      description: a property this binary added\n    status:")
		},
	})
}

func TestSampleUpgradePreviewReadsTheOriginStamp(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	b, _ := c.ByID(tasksSampleID)
	landed := b.LandedID(homeAuthority)
	held := heldStatus(t, ds, landed)

	// A pristine copy at the shipped version: the preview runs and has
	// nothing to offer, nothing to block and nothing to discard, which is
	// what the API drops from the entry.
	up, err := c.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview of a current copy: %v", err)
	}
	if up == nil || up.Available || up.DiscardsEdits || len(up.Blockers) != 0 {
		t.Fatalf("a current pristine copy previews %+v, want nothing to offer", up)
	}

	// Only a copy stamped with THIS entry's id is previewed: a status with no
	// stamp, or one naming another origin, is a package the shipped sample
	// may not claim.
	other := held
	other.Origin = "elsewhere.example.com/tasks"
	if up, err := c.Upgrade(ctx, tasksSampleID, ds, &other); err != nil || up != nil {
		t.Errorf("a copy stamped from another origin previews %+v (%v), want nothing", up, err)
	}
	if up, err := c.Upgrade(ctx, tasksSampleID, ds, nil); err != nil || up != nil {
		t.Errorf("a sample with no held copy previews %+v (%v), want nothing", up, err)
	}

	// Binary N+1 ships tasks a version ahead: offered, over the REHOMED
	// closure, so the moved kind is named as this repository holds it.
	moved := bumpedTasks(t, b.Version)
	up, err = moved.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview of the moved sample: %v", err)
	}
	if up == nil || !up.Available {
		t.Fatalf("a moved sample previews no upgrade: %+v", up)
	}
	if up.From != b.Version || up.To != movedVersion {
		t.Errorf("motion reads %d -> %d, want %d -> %d", up.From, up.To, b.Version, movedVersion)
	}
	if up.DiscardsEdits || len(up.Blockers) != 0 {
		t.Errorf("an additive upgrade of a pristine copy discards or blocks: %+v", up)
	}
	movedKinds := map[string]bool{}
	for _, ch := range up.Changes {
		if ch.Kind == "kind" {
			movedKinds[ch.ID] = true
		}
	}
	if !movedKinds[homeAuthority+"/tasks/task"] {
		t.Errorf("the moved kind is not named under this repository's authority: %+v", up.Changes)
	}

	// Taking it is the import door, unconfirmed, and afterwards the copy
	// reads current and pristine at the new version.
	if _, _, err := moved.Import(ctx, substrate.ActorAPI, tasksSampleID, ds); err != nil {
		t.Fatalf("import the moved sample: %v", err)
	}
	held = heldStatus(t, ds, landed)
	wantOrigin(t, held, tasksSampleID, movedVersion, false)
	up, err = moved.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview after the upgrade: %v", err)
	}
	if up == nil || up.Available || up.DiscardsEdits {
		t.Errorf("an upgraded copy still previews an upgrade: %+v", up)
	}
}

func TestSampleReimportOverAnEditedCopyNeedsTheConfirmationItPreviewed(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	b, _ := c.ByID(tasksSampleID)
	landed := b.LandedID(homeAuthority)
	editKind(t, ds, homeAuthority+"/tasks/task", "mine")
	held := heldStatus(t, ds, landed)
	if !held.Modified {
		t.Fatal("the edited copy does not read modified")
	}

	// Nothing shipped moved, and still the preview is there: the re-import
	// would discard the edit, and the hash a confirmation needs is here.
	up, err := c.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview of an edited copy: %v", err)
	}
	if up == nil || !up.DiscardsEdits {
		t.Fatalf("an edited copy previews %+v, want discardsEdits", up)
	}
	if up.Available || len(up.Blockers) != 0 {
		t.Errorf("an unmoved sample over an edited copy reads available or blocked: %+v", up)
	}
	if up.PlanHash == "" || up.ChangelogSeq == 0 {
		t.Fatalf("the preview carries no plan identity to confirm: hash %q at seq %d", up.PlanHash, up.ChangelogSeq)
	}

	// The bare re-import is refused under the lossy code, naming the edits,
	// and the edit stays.
	_, _, err = c.Import(ctx, substrate.ActorAPI, tasksSampleID, ds)
	if !errors.Is(err, substrate.ErrLossyConversion) {
		t.Fatalf("a bare re-import over an edited copy: err = %v, want ErrLossyConversion", err)
	}
	if !strings.Contains(err.Error(), "edited since the copy was imported") {
		t.Errorf("the refusal does not say what is lost: %v", err)
	}
	if _, still := declaredProperties(t, ds, homeAuthority+"/tasks/task")["mine"]; !still {
		t.Fatal("the refused re-import dropped the edit")
	}

	// A confirmation for another head, or for another plan, is refused the
	// way a lossy plan's is (decision 0067).
	stale := &substrate.ConversionConfirm{PlanHash: up.PlanHash, ChangelogSeq: up.ChangelogSeq - 1}
	if _, _, err := c.ImportConfirmed(ctx, substrate.ActorAPI, tasksSampleID, ds, stale); !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("a confirmation at another head: err = %v, want ErrConflict", err)
	}
	wrong := &substrate.ConversionConfirm{PlanHash: "cafe", ChangelogSeq: up.ChangelogSeq}
	if _, _, err := c.ImportConfirmed(ctx, substrate.ActorAPI, tasksSampleID, ds, wrong); !errors.Is(err, substrate.ErrLossyConversion) {
		t.Errorf("a confirmation for another plan: err = %v, want ErrLossyConversion", err)
	}

	// The confirmation the preview handed out lands the re-import, and the
	// copy reads pristine again.
	confirm := &substrate.ConversionConfirm{PlanHash: up.PlanHash, ChangelogSeq: up.ChangelogSeq}
	if _, _, err := c.ImportConfirmed(ctx, substrate.ActorAPI, tasksSampleID, ds, confirm); err != nil {
		t.Fatalf("the confirmed re-import: %v", err)
	}
	held = heldStatus(t, ds, landed)
	wantOrigin(t, held, tasksSampleID, b.Version, false)
	if _, still := declaredProperties(t, ds, homeAuthority+"/tasks/task")["mine"]; still {
		t.Error("the confirmed re-import kept the edit")
	}
	up, err = c.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview after the re-import: %v", err)
	}
	if up == nil || up.Available || up.DiscardsEdits {
		t.Errorf("a pristine copy still previews something: %+v", up)
	}

	// A moved sample over an edited copy is both: offered and discarding. The
	// edit here moves the PACKAGE header too, so the stored version runs past
	// the one the copy was taken at: the motion still reads from that one.
	editKind(t, ds, homeAuthority+"/tasks/task", "theirs")
	header := vocabulary.PackageManifest(landed, b.Version+1)
	header["data"].(map[string]any)["description"] = "my tasks, edited"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{header}); err != nil {
		t.Fatalf("edit the package header: %v", err)
	}
	held = heldStatus(t, ds, landed)
	if held.Version <= b.Version {
		t.Fatalf("the header edit did not move the stored package version past %d: %d", b.Version, held.Version)
	}
	moved := bumpedTasks(t, b.Version)
	up, err = moved.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview of the moved sample over an edited copy: %v", err)
	}
	if up == nil || !up.Available || !up.DiscardsEdits {
		t.Fatalf("a moved sample over an edited copy previews %+v, want available and discardsEdits", up)
	}
	if up.From != b.Version || up.To != movedVersion {
		t.Errorf("motion reads %d -> %d, want %d -> %d (from the version the copy was taken at)", up.From, up.To, b.Version, movedVersion)
	}
	// An edit after the preview refuses the confirmation: the head moved.
	editKind(t, ds, homeAuthority+"/tasks/task", "later")
	confirm = &substrate.ConversionConfirm{PlanHash: up.PlanHash, ChangelogSeq: up.ChangelogSeq}
	if _, _, err := moved.ImportConfirmed(ctx, substrate.ActorAPI, tasksSampleID, ds, confirm); !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("a confirmation from before a later edit: err = %v, want ErrConflict", err)
	}
}

func TestSampleUpgradeIsBlockedByARequiresFloorTheRepositoryDoesNotMeet(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	b, _ := c.ByID(tasksSampleID)
	held := heldStatus(t, ds, b.LandedID(homeAuthority))

	// Binary N+1's tasks declares against a people the repository does not
	// hold yet.
	const requires = "  requires:\n    - samples.substrate.reamde.dev/people\n    - samples.substrate.reamde.dev/scheduling\n"
	moved := movedTasksSampleCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string {
			doc = bumpTasksPackage(t, b.Version, doc)
			doc = mustReplace(t, doc, "  requiresAtLeast:\n    samples.substrate.reamde.dev/people: 4\n", "")
			return mustReplace(t, doc, requires, requires+"  requiresAtLeast:\n    samples.substrate.reamde.dev/people: 99\n")
		},
	})
	mb, _ := moved.ByID(tasksSampleID)
	if mb.RequiresAtLeast[peopleSampleID] != 99 {
		t.Fatalf("the catalog entry does not carry the floor: %v", mb.RequiresAtLeast)
	}
	up, err := moved.Upgrade(ctx, tasksSampleID, ds, &held)
	if err != nil {
		t.Fatalf("preview of a sample whose floor is not met: %v", err)
	}
	if up == nil || !up.Available {
		t.Fatalf("a moved sample previews no upgrade: %+v", up)
	}
	if len(up.Blockers) == 0 {
		t.Fatal("a floor the repository does not meet blocks nothing")
	}
	line := strings.Join(up.Blockers, "\n")
	for _, want := range []string{"requiresAtLeast", homeAuthority + "/people", "version 99 or later", "holds version"} {
		if !strings.Contains(line, want) {
			t.Errorf("the blocker does not say %q:\n%s", want, line)
		}
	}
	// The preview's blocker IS the door's refusal.
	if _, _, err := moved.Import(ctx, substrate.ActorAPI, tasksSampleID, ds); err == nil {
		t.Fatal("the import door admitted a closure whose floor is not met")
	} else if !strings.Contains(err.Error(), "requiresAtLeast") {
		t.Errorf("the refusal does not name the floor: %v", err)
	}
}

// sampleClosure reads one shipped sample's vocabulary documents from the tree,
// as `substratectl apply -f samples/<pkg>/*.yaml` would send them.
func sampleClosure(t *testing.T, pkg string) []map[string]any {
	t.Helper()
	root := filepath.Join("..", "..", "samples")
	var out []map[string]any
	for _, dir := range []string{root, filepath.Join(root, pkg)} {
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
				if kind, _ := d["kind"].(string); vocabulary.VocabularyDocumentKind(vocabulary.KindName(kind)) {
					out = append(out, d)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("the %s closure read as no documents", pkg)
	}
	return out
}

// A closure rehomed by hand (`substratectl apply --as`) records the origin it
// claims: the stamp is the import door's, so the copy previews and re-imports
// as one the door landed would, and its re-apply over an edited copy takes the
// same confirmation.
func TestAHandRehomedApplyRecordsItsOrigin(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	const foodSample = samplesPlacehldr + "/food"
	b, ok := c.ByID(foodSample)
	if !ok {
		t.Fatal("the shipped catalog no longer carries the food sample")
	}
	docs, err := vocabulary.RehomeAuthority(sampleClosure(t, "food"), samplesPlacehldr, homeAuthority)
	if err != nil {
		t.Fatalf("rehome: %v", err)
	}
	landed := homeAuthority + "/food"

	// The claim is held to its shape before anything is written.
	for name, origin := range map[string]string{
		"not an identity":                "food",
		"a package the batch lacks":      samplesPlacehldr + "/drinks",
		"the package the batch declares": landed,
	} {
		if _, err := ds.ApplyVocabularyDocumentsWith(ctx, substrate.ActorAPI, docs, substrate.VocabularyApply{Origin: origin}); !errors.Is(err, substrate.ErrValidation) {
			t.Errorf("origin %s (%q): err = %v, want ErrValidation", name, origin, err)
		}
	}

	if _, err := ds.ApplyVocabularyDocumentsWith(ctx, substrate.ActorAPI, docs, substrate.VocabularyApply{Origin: foodSample}); err != nil {
		t.Fatalf("apply the rehomed closure with its origin: %v", err)
	}
	held := heldStatus(t, ds, landed)
	wantOrigin(t, held, foodSample, b.Version, false)
	// The stamped copy IS a copy to the catalog: the shipped sample previews
	// it, with nothing to offer while nothing moved.
	up, err := c.Upgrade(ctx, foodSample, ds, &held)
	if err != nil {
		t.Fatalf("preview of a hand-stamped copy: %v", err)
	}
	if up == nil || up.Available || up.DiscardsEdits {
		t.Errorf("a current hand-stamped copy previews %+v, want nothing to offer", up)
	}

	// Edited since, the re-apply naming the origin replaces the edit, so it
	// takes the confirmation its own preview hands out.
	editKind(t, ds, landed+"/meal", "mine")
	plan, err := ds.PlanVocabularyApplyWith(ctx, substrate.ActorAPI, docs, substrate.VocabularyApply{Origin: foodSample})
	if err != nil {
		t.Fatalf("plan the re-apply: %v", err)
	}
	if !plan.DiscardsEdits || plan.PlanHash == "" {
		t.Fatalf("the plan over an edited copy reads %+v, want discardsEdits with a hash", plan)
	}
	bare, err := ds.PlanVocabularyApply(ctx, substrate.ActorAPI, docs)
	if err != nil {
		t.Fatalf("plan the re-apply without an origin: %v", err)
	}
	if bare.DiscardsEdits || bare.PlanHash == plan.PlanHash {
		t.Errorf("a plan claiming no origin discards edits or hashes like one that does: %+v", bare)
	}
	if _, err := ds.ApplyVocabularyDocumentsWith(ctx, substrate.ActorAPI, docs, substrate.VocabularyApply{Origin: foodSample}); !errors.Is(err, substrate.ErrLossyConversion) {
		t.Fatalf("an unconfirmed re-apply over an edited copy: err = %v, want ErrLossyConversion", err)
	}
	confirm := &substrate.ConversionConfirm{PlanHash: plan.PlanHash, ChangelogSeq: plan.ChangelogSeq}
	if _, err := ds.ApplyVocabularyDocumentsWith(ctx, substrate.ActorAPI, docs, substrate.VocabularyApply{Origin: foodSample, Confirm: confirm}); err != nil {
		t.Fatalf("the confirmed re-apply: %v", err)
	}
	wantOrigin(t, heldStatus(t, ds, landed), foodSample, b.Version, false)
	if _, still := declaredProperties(t, ds, landed+"/meal")["mine"]; still {
		t.Error("the confirmed re-apply kept the edit")
	}

	// The same closure applied WITHOUT an origin is the owner's own write:
	// it replaces the package as it always has and stamps nothing new, but
	// the stamp already there stays with the row.
	editKind(t, ds, landed+"/meal", "again")
	if _, err := ds.ApplyVocabularyDocumentsWith(ctx, substrate.ActorAPI, docs, substrate.VocabularyApply{}); err != nil {
		t.Fatalf("a re-apply naming no origin: %v", err)
	}
	wantOrigin(t, heldStatus(t, ds, landed), foodSample, b.Version, false)
}
