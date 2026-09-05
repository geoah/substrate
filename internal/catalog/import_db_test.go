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
		if _, err := c.Import(context.Background(), substrate.ActorAPI, id, ds); err != nil {
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
	_, err := c.Import(ctx, substrate.ActorAPI, tasksSampleID, ds)
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

	// THE PLACEHOLDER NEVER REACHES THE CHANGELOG. The changelog is the truth
	// a rebuild reads, so a mention there is vocabulary under an authority the
	// repository does not own, folded back on every rebuild.
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
		if strings.Contains(string(raw), samplesPlacehldr) {
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
}

// The two doors are not interchangeable, and the PROVIDER door still works:
// whoop installs under the authority that publishes it, unrehomed.
func TestImportRefusesAProviderAndInstallStillWorks(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()

	const whoop = "providers.substrate.reamde.dev/whoop"
	_, err := c.Import(ctx, substrate.ActorAPI, whoop, ds)
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("import of a provider = %v, want a validation refusal", err)
	}
	if !strings.Contains(err.Error(), "install") {
		t.Errorf("the refusal does not name the verb that works: %v", err)
	}

	if _, err := c.Install(ctx, substrate.ActorAPI, whoop, ds); err != nil {
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
}
