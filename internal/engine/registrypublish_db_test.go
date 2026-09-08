package engine

// THE PUBLISH BOUNDARY OF A VOCABULARY APPLY (#150).
//
// A vocabulary apply commits its rows and then swaps the dataset's registry
// pointer. Two things sit at that boundary and both are pinned here: the swap
// runs BEFORE the head signal, so a watcher woken by the kind's changelog
// entry resolves the kind; and the declared indexes are built BEFORE the
// transaction, so an index the engine cannot build refuses the apply with
// nothing landed instead of failing an apply that already published.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const publishPackage = "publish.example.substrate.reamde.dev/publish"

// The registry publishes before the head signal. The seam between the two is
// the only place the order is observable without a race: the broadcaster
// coalesces signals for 300ms, so a subscriber that checked the registry on
// wake would pass against a swap that ran a microsecond after the signal.
func TestTheRegistryPublishesBeforeTheHeadSignal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	open, _ := w2Opener(t)
	ds := open()
	const kind = publishPackage + "/beacon"

	// entrySignaled: the apply's transaction reached the signal. published:
	// the live registry resolved the kind at that moment.
	var entrySignaled, published atomic.Bool
	ds.mu.Lock()
	ds.beforeSignal = func(tx *txn) {
		for _, e := range tx.entries {
			if e.kind == kindKind && e.id == kind {
				entrySignaled.Store(true)
				_, ok := ds.registry().ByIdentity(kind)
				published.Store(ok)
			}
		}
	}
	ds.mu.Unlock()
	signals := ds.WatchSignal(ctx)

	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "beacon", "plural": "beacons"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !entrySignaled.Load() {
		t.Fatal("the apply signaled no transaction carrying the kind's entry")
	}
	if !published.Load() {
		t.Fatal("the head signaled before the registry published: a watcher woken by the kind's entry resolves the retired registry")
	}
	// The subscriber itself, woken by that signal, resolves the kind.
	select {
	case <-signals:
		if _, err := ds.KindByRef(ctx, kind); err != nil {
			t.Fatalf("the woken subscriber cannot resolve the kind: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the apply woke no subscriber")
	}
}

// An index the engine cannot build refuses the apply whole. `created_at` is a
// column spelled the way indexExpr refuses (columnFor names `createdAt`), and
// the loader does not check index names, so it reaches the CREATE INDEX path.
// That path ran after the commit once: the kind landed, the registry
// published, and the caller was told the apply failed.
func TestAnIndexTheEngineCannotBuildRefusesTheApplyWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	const kind = publishPackage + "/gauge"
	before := maxSeqOf(t, ds)

	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "gauge", "plural": "gauges"},
			map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"indices":    []any{map[string]any{"properties": []any{"created_at"}}},
			}),
	})
	if err == nil {
		t.Fatal("an apply declaring an index the engine cannot build must be refused")
	}
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("the refusal names the index column as a validation error: %v", err)
	}
	// Nothing landed: no registry entry, no declaration row, no changelog entry.
	if _, ok := ds.registry().ByIdentity(kind); ok {
		t.Fatal("the refused apply published the kind")
	}
	if _, err := ds.Get(ctx, kindKind, kind); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused apply left a declaration row: %v", err)
	}
	if after := maxSeqOf(t, ds); after != before {
		t.Fatalf("the refused apply appended to the changelog: head %d -> %d", before, after)
	}
}
