package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A bind takes the shared registry-dependency lock before it locks the bundle
// row: the patch it ends in takes the shared side too, but only after the row
// is held, and an upgrade of the same bundle holds the exclusive side while
// locking that row. Started inside an apply's transaction the bind parks, and
// while parked the bundle row is free.
func TestABindParksAtTheRegistryDepLockBeforeItsBundleRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	const (
		pkg    = "bindlock.example.substrate.reamde.dev/bindlock"
		config = pkg + "/config"
	)
	closure := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "a bundle with one input to bind",
			"inputs":      map[string]any{"client": map[string]any{"kind": config}},
			"installs":    []any{config},
		}),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "config", "plural": "configs"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.ActorAPI, closure, nil, substrate.BundleInstall{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: config, ID: "c1", Properties: map[string]any{"name": "one"},
	}); err != nil {
		t.Fatalf("put the config: %v", err)
	}

	// The apply touches an unrelated package, so the only thing between the
	// bind and its commit is the registry-dependency lock the apply holds.
	bound := make(chan error, 1)
	docs, err := parseVocabularyDocs([]map[string]any{
		vocabulary.PackageManifest(publishPackage, 0),
		vocabulary.KindManifest(publishPackage,
			map[string]any{"singular": "lamp", "plural": "lamps"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorAPI, vocabularyBatch{docs: docs, extra: func(*txn) error {
		go func() { bound <- ds.BindBundleInput(ctx, pkg, "client", "c1") }()
		if err := waitParkedOn(t, ds, registryDepKey(ds), changelogLockKey); err != nil {
			return err
		}
		select {
		case err := <-bound:
			return fmt.Errorf("the bind did not park behind the apply: %w", err)
		default:
		}
		if !rowLockFree(t, ds, kindBundle, pkg) {
			return errors.New("the parked bind holds the bundle row: it queued for the registry lock with the row in hand")
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("the apply: %v", err)
	}
	select {
	case err := <-bound:
		if err != nil {
			t.Fatalf("the woken bind: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parked bind never finished")
	}
	row, err := ds.Get(ctx, kindBundle, pkg)
	if err != nil {
		t.Fatalf("the bundle row: %v", err)
	}
	if bindings, _ := row.Properties["bindings"].(map[string]any); bindings["client"] == nil {
		t.Fatalf("the bind did not land once the apply published: %v", row.Properties["bindings"])
	}
}
