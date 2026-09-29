package catalog_test

// The upgrade preview a catalog read reuses (issue #445), against a REAL
// engine: two reads with no write between count one plan per entry, any
// write (a live record as much as a declaration) counts a fresh one, the plan
// a read reuses carries the hash the door admits, and neither another
// repository nor another dataset instance is served a plan it did not count.

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// countedDataset counts the upgrade previews the catalog asks the dataset
// for, so a preview served from the catalog's cache is one it never counted.
type countedDataset struct {
	substrate.Dataset
	previews *atomic.Int64
}

func counted(ds substrate.Dataset) countedDataset {
	return countedDataset{Dataset: ds, previews: new(atomic.Int64)}
}

func (d countedDataset) PlanBundleUpgrade(ctx context.Context, docs []map[string]any) (substrate.BundleUpgrade, error) {
	d.previews.Add(1)
	return d.Dataset.PlanBundleUpgrade(ctx, docs)
}

// preview is one listing read's preview of entry id.
func preview(t *testing.T, c *catalog.Catalog, ds substrate.Dataset, id string, held *substrate.BundleStatus) *substrate.BundleUpgrade {
	t.Helper()
	up, err := c.Upgrade(context.Background(), id, ds, held)
	if err != nil {
		t.Fatalf("preview %s: %v", id, err)
	}
	if up == nil {
		t.Fatalf("no preview of %s", id)
	}
	return up
}

func wantPreviews(t *testing.T, ds countedDataset, want int64, when string) {
	t.Helper()
	if got := ds.previews.Load(); got != want {
		t.Fatalf("%s: the dataset counted %d previews, want %d", when, got, want)
	}
}

func TestUpgradePreviewIsCountedOncePerChangelogHead(t *testing.T) {
	ds := counted(newDataset(t))
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	importVocabulary(t, c, ds, whoopID)
	b, _ := c.ByID(tasksSampleID)
	landed := b.LandedID(homeAuthority)
	// An edited copy, so the preview takes the costly path: it stages the
	// closure and counts a plan with a hash to confirm.
	editKind(t, ds, homeAuthority+"/tasks/task", "mine")
	held := heldStatus(t, ds, landed)

	first := preview(t, c, ds, tasksSampleID, &held)
	provider := preview(t, c, ds, whoopID, nil)
	wantPreviews(t, ds, 2, "the first read")
	if !first.DiscardsEdits || first.PlanHash == "" {
		t.Fatalf("the edited copy previews %+v, want a discarding plan with a hash", first)
	}
	if again := preview(t, c, ds, tasksSampleID, &held); !reflect.DeepEqual(again, first) {
		t.Errorf("the second read of the sample is not the first one's plan:\n%+v\n%+v", again, first)
	}
	if again := preview(t, c, ds, whoopID, nil); !reflect.DeepEqual(again, provider) {
		t.Errorf("the second read of the provider is not the first one's plan:\n%+v\n%+v", again, provider)
	}
	wantPreviews(t, ds, 2, "a second read with no write between")

	// The plan a read reuses is the plan a fresh count answers, the one a
	// catalog with nothing cached computes.
	fresh := preview(t, loadCatalog(t), ds, tasksSampleID, &held)
	if fresh.PlanHash != first.PlanHash || fresh.ChangelogSeq != first.ChangelogSeq {
		t.Fatalf("the cached plan is %s at %d, a fresh one %s at %d",
			first.PlanHash, first.ChangelogSeq, fresh.PlanHash, fresh.ChangelogSeq)
	}
	wantPreviews(t, ds, 3, "the fresh catalog's read")

	// A DATA write moves the plan: a live task now holds the property the
	// re-import drops, so the plan nulls it, and its hash covers that row. A
	// key on the vocabulary alone would keep serving the old hash, which the
	// door refuses.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: homeAuthority + "/tasks/task", ID: "holds-mine",
		Properties: map[string]any{"mine": "a value the re-import drops"},
	}); err != nil {
		t.Fatalf("put the live task: %v", err)
	}
	afterPut := preview(t, c, ds, tasksSampleID, &held)
	wantPreviews(t, ds, 4, "a read after a record write")
	if !afterPut.Lossy || afterPut.PlanHash == first.PlanHash || afterPut.ChangelogSeq <= first.ChangelogSeq {
		t.Fatalf("the read after the record write previews %+v, want a lossy plan with a new hash past seq %d", afterPut, first.ChangelogSeq)
	}

	// A VOCABULARY write moves it too.
	editKind(t, ds, homeAuthority+"/tasks/task", "theirs")
	held = heldStatus(t, ds, landed)
	edited := preview(t, c, ds, tasksSampleID, &held)
	wantPreviews(t, ds, 5, "a read after a declaration write")
	if edited.PlanHash == afterPut.PlanHash {
		t.Fatalf("the read after the declaration write kept the hash %s", edited.PlanHash)
	}
	cached := preview(t, c, ds, tasksSampleID, &held)
	wantPreviews(t, ds, 5, "a second read after the declaration write")

	// The door recomputes the plan and admits the hash the cached preview
	// carries, never a superseded one. A refused take writes nothing, so it
	// leaves the cached plan standing.
	stale := &substrate.ConversionConfirm{PlanHash: afterPut.PlanHash, ChangelogSeq: afterPut.ChangelogSeq}
	if _, _, err := c.ImportConfirmed(ctx, substrate.ActorAPI, tasksSampleID, ds, stale); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a confirmation of a superseded plan: err = %v, want ErrConflict", err)
	}
	if again := preview(t, c, ds, tasksSampleID, &held); !reflect.DeepEqual(again, cached) {
		t.Errorf("the refused take moved the cached plan:\n%+v\n%+v", again, cached)
	}
	wantPreviews(t, ds, 5, "a read after a refused take")
	confirm := &substrate.ConversionConfirm{PlanHash: cached.PlanHash, ChangelogSeq: cached.ChangelogSeq}
	if _, _, err := c.ImportConfirmed(ctx, substrate.ActorAPI, tasksSampleID, ds, confirm); err != nil {
		t.Fatalf("the door refused the cached preview's confirmation: %v", err)
	}

	// The take wrote, so the next read counts again and reads the copy
	// pristine.
	held = heldStatus(t, ds, landed)
	after := preview(t, c, ds, tasksSampleID, &held)
	wantPreviews(t, ds, 6, "a read after the take")
	if after.DiscardsEdits || after.Available {
		t.Errorf("the re-imported copy still previews %+v", after)
	}
}

// newDatasets opens ONE engine and registers each named repository in it.
func newDatasets(t *testing.T, names ...string) []substrate.Dataset {
	t.Helper()
	svc, err := engine.Open(context.Background(), testdb.NewSchema(t),
		engine.WithKindsDir("../../kinds/substrate.reamde.dev"),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(credKey),
	)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	out := make([]substrate.Dataset, 0, len(names))
	for _, name := range names {
		if _, err := svc.CreateRepository(ctx, name); err != nil {
			t.Fatalf("create repository %s: %v", name, err)
		}
		ds, err := svc.Dataset(ctx, name)
		if err != nil {
			t.Fatalf("open dataset %s: %v", name, err)
		}
		out = append(out, ds)
	}
	return out
}

// Two repositories behind one catalog each count their own preview, and each
// is served its own: one holds an edited copy, the other a pristine one.
func TestUpgradePreviewsAreHeldPerRepository(t *testing.T) {
	const other = "ada.example.com"
	both := newDatasets(t, homeAuthority, other)
	geoah, ada := counted(both[0]), counted(both[1])
	c := loadCatalog(t)
	for _, ds := range []substrate.Dataset{geoah, ada} {
		importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	}
	b, _ := c.ByID(tasksSampleID)
	editKind(t, geoah, homeAuthority+"/tasks/task", "mine")
	geoahHeld := heldStatus(t, geoah, b.LandedID(homeAuthority))
	adaHeld := heldStatus(t, ada, b.LandedID(other))

	for range 2 {
		if up := preview(t, c, geoah, tasksSampleID, &geoahHeld); !up.DiscardsEdits {
			t.Fatalf("the edited repository previews %+v, want discardsEdits", up)
		}
		if up := preview(t, c, ada, tasksSampleID, &adaHeld); up.DiscardsEdits || up.PlanHash != "" {
			t.Fatalf("the pristine repository is served %+v, the edited one's plan", up)
		}
	}
	wantPreviews(t, geoah, 1, "the edited repository, read twice")
	wantPreviews(t, ada, 1, "the pristine repository, read twice")
}

// A dataset opened again for the same repository, at the same head and the
// same history generation, counts its own preview: the plans the closed one
// counted went with it.
func TestUpgradePreviewIsCountedAgainOverAReopenedDataset(t *testing.T) {
	repo := newReopenableDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importVocabulary(t, c, repo.ds, whoopID)
	before := counted(repo.ds)
	preview(t, c, before, whoopID, nil)
	preview(t, c, before, whoopID, nil)
	wantPreviews(t, before, 1, "the first dataset, read twice")
	headBefore, err := before.Head(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	// One counter across both, so the two wrappers differ only in the
	// dataset instance under them.
	after := countedDataset{Dataset: repo.reopen(t), previews: before.previews}
	headAfter, err := after.Head(ctx)
	if err != nil {
		t.Fatalf("head after the reopen: %v", err)
	}
	if headAfter != headBefore {
		t.Fatalf("the reopen moved the head from %+v to %+v, so this proves nothing about the instance", headBefore, headAfter)
	}
	preview(t, c, after, whoopID, nil)
	preview(t, c, after, whoopID, nil)
	wantPreviews(t, after, 2, "both datasets, each read twice")
}
