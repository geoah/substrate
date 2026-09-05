package engine_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An owner pointer is a reference declaring `onDelete: cascade`: collecting the
// referent collects the record that names it. This file holds the cascade and
// the three ways it must NOT fire — a pointer at another owner, a reference the
// declaration did not mark, and a plain string carrying the same value.

// mirrorPackage is a provider bundle's shape in miniature: three kinds that
// name an account three different ways, so one delete separates them.
const (
	mirrorAuthority = "testmirror.example.com"
	mirrorPackage   = mirrorAuthority + "/testmirror"
)

func mirrorManifest() enginetest.Manifest {
	kind := func(id string, account map[string]any) map[string]any {
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": id},
			"data": map[string]any{
				"authority":       mirrorAuthority,
				"package":         "testmirror",
				"names":           map[string]any{"singular": last(id), "plural": last(id) + "s"},
				"displayTemplate": "{label}",
				"properties": map[string]any{
					"label":   map[string]any{"type": "string"},
					"account": account,
				},
			},
		}
	}
	return enginetest.Manifest{
		Name:      "testmirror",
		Authority: mirrorAuthority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(mirrorPackage, 1),
			// The owner pointer under test.
			kind(mirrorPackage+"/owned", map[string]any{
				"type": "reference", "kind": enginetest.AccountType,
				"required": true, "onDelete": "cascade",
			}),
			// The same pin, the same value, no `onDelete:`: provenance, and the
			// sweep must leave it alone. Without this the test would pass on a
			// cascade that collected every reference at the owner's kind.
			kind(mirrorPackage+"/pointer", map[string]any{
				"type": "reference", "kind": enginetest.AccountType,
			}),
			// The pre-0032 spelling: a string holding the account's id.
			kind(mirrorPackage+"/legacy", map[string]any{"type": "string"}),
		},
	}
}

func last(id string) string {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '/' {
			return id[i+1:]
		}
	}
	return id
}

func TestOwnerRefReferenceCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, mirrorManifest()); err != nil {
		t.Fatalf("install mirror types: %v", err)
	}

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "acct-one",
		Properties: map[string]any{"provider": "testmirror", "label": "One"},
	})
	other := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "acct-two",
		Properties: map[string]any{"provider": "testmirror", "label": "Two"},
	})

	// The bare id is the authored short form a connector writes; the pin
	// supplies the kind, so the stored reference carries the path the sweep
	// probes under `ref` (decision 0044).
	synced := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mirrorPackage + "/owned", ID: "synced-one",
		Properties: map[string]any{"label": "synced", "account": acc.ID},
	})
	if got := storedRefPath(mustGet(t, ds, synced.Kind, synced.ID).Properties["account"]); got != enginetest.AccountType+"/"+acc.ID {
		t.Fatalf("stored account = %v, want the canonical path", got)
	}
	elsewhere := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mirrorPackage + "/owned", ID: "synced-two",
		Properties: map[string]any{"label": "other account", "account": other.ID},
	})
	provenance := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mirrorPackage + "/pointer", ID: "pointer-one",
		Properties: map[string]any{"label": "not an owner", "account": acc.ID},
	})
	legacy := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mirrorPackage + "/legacy", ID: "legacy-one",
		Properties: map[string]any{"label": "a string", "account": acc.ID},
	})

	// A finalizer holds the cascade AT the tombstone, so the tombstone itself is
	// observable: RunGC runs to a fixpoint, and without the hold the same call
	// would collect the synced record in its second pass.
	mustPatch(t, ds, owner, synced.Kind, synced.ID,
		substrate.PatchInput{AddFinalizers: []string{"testmirror.example.com/testmirror/teardown"}})

	if _, err := ds.Delete(ctx, owner, acc.Kind, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, acc.Kind, acc.ID); err == nil {
		t.Fatal("the account should be hard-deleted")
	}
	if mustGet(t, ds, synced.Kind, synced.ID).DeletedAt == nil {
		t.Fatal("the cascading reference should have tombstoned the synced record")
	}
	if mustGet(t, ds, elsewhere.Kind, elsewhere.ID).DeletedAt != nil {
		t.Fatal("a record pointing at another account was collected")
	}
	if mustGet(t, ds, provenance.Kind, provenance.ID).DeletedAt != nil {
		t.Fatal("a reference without onDelete: cascade was collected")
	}
	if mustGet(t, ds, legacy.Kind, legacy.ID).DeletedAt != nil {
		t.Fatal("a string property was collected")
	}

	// The tombstone is a fold effect on the entry that reports it, so the same
	// changelog replays to the same records table.
	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); !bytes.Equal(before, after) {
		t.Fatal("the rebuild did not reproduce the cascade")
	}

	// Release the hold: the next sweep takes the tombstone the first one wrote.
	mustPatch(t, ds, owner, synced.Kind, synced.ID,
		substrate.PatchInput{RemoveFinalizers: []string{"testmirror.example.com/testmirror/teardown"}})
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, synced.Kind, synced.ID); err == nil {
		t.Fatal("the synced record should be collected")
	}
	if _, err := ds.Get(ctx, elsewhere.Kind, elsewhere.ID); err != nil {
		t.Fatalf("the other account's record should stand: %v", err)
	}
}
