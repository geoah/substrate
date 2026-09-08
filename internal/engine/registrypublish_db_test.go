package engine

// THE PUBLISH BOUNDARY OF A VOCABULARY APPLY (#150).
//
// A vocabulary apply commits its rows and then swaps the dataset's registry
// pointer. Three things sit at that boundary and all are pinned here: the swap
// happens under ds.mu held across the commit, so a data write the commit wakes
// at the registry-dependency lock resolves the published declaration; the swap
// runs BEFORE the head signal, so a watcher woken by the kind's changelog
// entry resolves the kind; and the declared indexes are built BEFORE the
// transaction, every definition checked before any is created, so an index the
// engine cannot build refuses the apply with nothing landed and nothing left
// behind.

import (
	"context"
	"errors"
	"fmt"
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
	ctx := context.Background()
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
}

// A data write parked at the registry-dependency lock resolves the PUBLISHED
// declaration. The advisory lock releases at the commit and the write's next
// act is ds.registry(); the pointer swap happens under ds.mu held from before
// the commit, so the write blocks there until the candidate is in place. The
// write here carries a property the old declaration admits and the new one
// drops: it must be refused, never landed against the retired shape.
func TestAParkedWriteResolvesThePublishedDeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	const kind = publishPackage + "/gizmo"
	gizmo := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(publishPackage, 0),
			vocabulary.KindManifest(publishPackage,
				map[string]any{"singular": "gizmo", "plural": "gizmos"},
				map[string]any{"properties": props}),
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, gizmo(map[string]any{
		"name":   map[string]any{"type": "string"},
		"legacy": map[string]any{"type": "string"},
	})); err != nil {
		t.Fatalf("install: %v", err)
	}

	// The write parks INSIDE the apply's transaction, which holds the lock
	// exclusive from its first statement to its commit. slippedBeforePublish
	// records the write landing while the commit had happened and the swap
	// had not: the window the mutex closes.
	putDone := make(chan error, 1)
	var slippedBeforePublish atomic.Bool
	ds.mu.Lock()
	ds.beforePublish = func(*txn) {
		time.Sleep(400 * time.Millisecond)
		select {
		case err := <-putDone:
			slippedBeforePublish.Store(true)
			putDone <- err
		default:
		}
	}
	ds.mu.Unlock()
	docs, err := parseVocabularyDocs(gizmo(map[string]any{"name": map[string]any{"type": "string"}}))
	if err != nil {
		t.Fatal(err)
	}
	actor := substrate.ActorAPI
	_, err = ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{docs: docs, extra: func(*txn, *vocabulary.Registry) error {
		go func() {
			_, err := ds.Put(ctx, actor, substrate.PutInput{
				Kind: kind, ID: "racer", Properties: map[string]any{"name": "a", "legacy": "held"},
			})
			putDone <- err
		}()
		// Long enough for the write to reach the lock and park behind this
		// transaction's exclusive side.
		time.Sleep(400 * time.Millisecond)
		select {
		case err := <-putDone:
			return fmt.Errorf("the write did not park at the registry-dependency lock: %w", err)
		default:
			return nil
		}
	}})
	if err != nil {
		t.Fatalf("the narrowing apply: %v", err)
	}
	if slippedBeforePublish.Load() {
		t.Fatal("the parked write landed between the commit and the publish, against the retired declaration")
	}
	select {
	case err := <-putDone:
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "legacy") {
			t.Fatalf("the woken write must be refused against the published declaration, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parked write never finished")
	}
	if _, err := ds.Get(ctx, kind, "racer"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused write landed a row: %v", err)
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

// A refused index definition leaves no index behind. The name is the
// declaration's ordinal, so a first index created under a refusal of the
// second would survive, and a corrected retry that changed the first's
// definition would find the stale one "already there" and report success.
func TestARefusedIndexDefinitionLeavesNoIndexBehind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	const kind = publishPackage + "/dial"

	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "dial", "plural": "dials"},
			map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"indices": []any{
					map[string]any{"properties": []any{"name"}},
					map[string]any{"properties": []any{"created_at"}},
				},
			}),
	})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("the second index must refuse the apply: %v", err)
	}
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = $1`,
		"idx_"+derivedID(kind, "0")).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the refused apply created the first index before refusing the second")
	}
}
