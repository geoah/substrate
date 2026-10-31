package catalog_test

// What a catalog listing reads, against a REAL engine (issue #1086). The
// listing previews every installed entry, and a provider's preview walks the
// requirements its install would bring, which needs every installed bundle's
// version. It read them off the COUNTED statuses, an exact count of every live
// row of every installed kind, once per provider entry per listing, so the
// listing grew with the data until it ran past a client's read timeout.

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// statusCountingDataset counts the counted bundle status reads, the one read
// a catalog preview made whose cost is the repository's live rows.
type statusCountingDataset struct {
	substrate.Dataset
	counted *atomic.Int64
}

func (d statusCountingDataset) BundleStatuses(ctx context.Context) ([]substrate.BundleStatus, error) {
	d.counted.Add(1)
	return d.Dataset.BundleStatuses(ctx)
}

func TestTheCatalogPreviewsCountNoLiveRecords(t *testing.T) {
	base := newDataset(t)
	ctx := context.Background()
	// labs requires health, so its preview walks a requirement; the people
	// sample is a stamped copy, the other tier's preview.
	importVocabulary(t, loadCatalog(t), base, labsID)
	importSamples(t, loadCatalog(t), base, peopleSampleID)
	const live = 40
	for i := range live {
		if _, err := base.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: homeAuthority + "/people/person", ID: fmt.Sprintf("p%02d", i),
		}); err != nil {
			t.Fatalf("put a person: %v", err)
		}
	}

	counted, err := base.BundleStatuses(ctx)
	if err != nil {
		t.Fatalf("counted statuses: %v", err)
	}
	uncounted, err := base.BundleStatusesUncounted(ctx)
	if err != nil {
		t.Fatalf("uncounted statuses: %v", err)
	}
	if len(counted) != len(uncounted) {
		t.Fatalf("%d counted statuses, %d uncounted", len(counted), len(uncounted))
	}
	var people int64
	for i := range counted {
		if counted[i].ID == homeAuthority+"/people" {
			people = counted[i].LiveRecords
		}
		if uncounted[i].LiveRecords != 0 || uncounted[i].Accounts != 0 {
			t.Errorf("the uncounted status of %s counts %d live records and %d accounts", uncounted[i].ID, uncounted[i].LiveRecords, uncounted[i].Accounts)
		}
		want := counted[i]
		want.LiveRecords, want.Accounts = 0, 0
		if !reflect.DeepEqual(uncounted[i], want) {
			t.Errorf("the uncounted status differs from the counted one past its counts:\n%+v\n%+v", uncounted[i], want)
		}
	}
	if people != live {
		t.Fatalf("the counted status of the people copy holds %d live records, want %d", people, live)
	}

	// Every installed entry previewed, the way the listing asks: no preview
	// reads a count.
	ds := statusCountingDataset{Dataset: base, counted: new(atomic.Int64)}
	c := loadCatalog(t)
	held := map[string]*substrate.BundleStatus{}
	for i := range uncounted {
		st := &uncounted[i]
		if st.Origin != "" {
			held[st.Origin] = st
		} else {
			held[st.ID] = st
		}
	}
	previewed := 0
	for _, b := range c.Bundles() {
		st := held[b.ID]
		if st == nil {
			continue
		}
		if _, err := c.Upgrade(ctx, b.ID, ds, st); err != nil {
			t.Fatalf("preview %s: %v", b.ID, err)
		}
		previewed++
	}
	if previewed < 3 {
		t.Fatalf("previewed %d installed entries, want labs, health and people at least", previewed)
	}
	if n := ds.counted.Load(); n != 0 {
		t.Errorf("previewing %d installed entries read the counted statuses %d times, want none", previewed, n)
	}
}
