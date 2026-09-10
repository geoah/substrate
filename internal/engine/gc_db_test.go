package engine_test

// An owner pointer is a reference declaring `onDelete: cascade`: collecting
// the referent collects the record that names it. This file holds the cascade
// and the three ways it must NOT fire (a pointer at another owner, a
// reference the declaration did not mark, and a plain string carrying the
// same value), the same cascade reached through a TRAIT-typed reference, and
// what the incoming-reference read answers over one. Each case deletes its
// own rows and runs the repository-wide sweep, so the cases do not share a
// fixture.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An owner pointer is a reference declaring `onDelete: cascade`: collecting the
// referent collects the record that names it. This file holds the cascade and
// the three ways it must NOT fire — a pointer at another owner, a reference the
// declaration did not mark, and a plain string carrying the same value — plus
// the same cascade reached through a TRAIT-typed reference, and what the
// incoming-reference read answers over one. Each case deletes its own rows, so
// they do not share a fixture.

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
				"names":           map[string]any{"singular": last(id)},
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

	if _, err := ds.Delete(ctx, owner, acc.Kind, acc.ID, substrate.DeleteInput{}); err != nil {
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
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
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

// A shared kind (calendar, conversation, emailthread) owns its account through a
// TRAIT-pinned cascading reference, not a kind-pinned one: two providers each
// declare their own account kind, and pinning one would tie the shared kind to
// one provider (0034). This holds the two halves that pin buys: the write path
// refuses a referent whose kind does not implement the trait, and the GC cascade
// collects the shared record when the account it names is collected.

const (
	traitMirrorAuthority = "traitmirror.example.com"
	traitMirrorPackage   = traitMirrorAuthority + "/traitmirror"
)

// traitMirrorManifest declares a `connected` trait, two account kinds that
// implement it (two providers), a `session` kind whose account is a trait-pinned
// cascading reference, and a `stranger` kind that implements nothing — so a
// session pointing at a stranger is refused.
func traitMirrorManifest() enginetest.Manifest {
	kind := func(id string, extra map[string]any) map[string]any {
		data := map[string]any{
			"authority":       traitMirrorAuthority,
			"package":         "traitmirror",
			"names":           map[string]any{"singular": last(id)},
			"displayTemplate": "{label}",
			"properties": map[string]any{
				"label": map[string]any{"type": "string"},
			},
		}
		for k, v := range extra {
			data[k] = v
		}
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": id},
			"data":     data,
		}
	}
	accountProps := map[string]any{
		"label":   map[string]any{"type": "string"},
		"connRef": map[string]any{"type": "secret"},
	}
	account := func(id string) map[string]any {
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": id},
			"data": map[string]any{
				"authority":       traitMirrorAuthority,
				"package":         "traitmirror",
				"names":           map[string]any{"singular": last(id)},
				"displayTemplate": "{label}",
				"traits":          []any{"connected"},
				"properties":      accountProps,
			},
		}
	}
	return enginetest.Manifest{
		Name:      "traitmirror",
		Authority: traitMirrorAuthority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(traitMirrorPackage, 1),
			{
				"kind":     "substrate.reamde.dev/core/trait",
				"metadata": map[string]any{"id": traitMirrorPackage + "/connected"},
				"data": map[string]any{
					"authority":  traitMirrorAuthority,
					"package":    "traitmirror",
					"properties": map[string]any{"connRef": "secret"},
				},
			},
			account(traitMirrorPackage + "/providera"),
			account(traitMirrorPackage + "/providerb"),
			// The owner pointer under test: any kind that implements `connected`.
			kind(traitMirrorPackage+"/session", map[string]any{
				"properties": map[string]any{
					"label": map[string]any{"type": "string"},
					"account": map[string]any{
						"type": "reference", "trait": "connected", "onDelete": "cascade",
					},
				},
			}),
			// Same trait pin, no `onDelete:`: the sweep must leave it alone.
			kind(traitMirrorPackage+"/pointer", map[string]any{
				"properties": map[string]any{
					"label": map[string]any{"type": "string"},
					"account": map[string]any{
						"type": "reference", "trait": "connected",
					},
				},
			}),
			// Implements nothing: a session may not point here.
			kind(traitMirrorPackage+"/stranger", nil),
		},
	}
}

func TestOwnerRefTraitReferenceCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, traitMirrorManifest()); err != nil {
		t.Fatalf("install trait mirror types: %v", err)
	}

	accA := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/providera", ID: "acct-a",
		Properties: map[string]any{"label": "A"},
	})
	accB := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/providerb", ID: "acct-b",
		Properties: map[string]any{"label": "B"},
	})

	// A trait pin supplies no kind for a bare id, so the value is a full path.
	synced := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/session", ID: "sess-a",
		Properties: map[string]any{"label": "on A", "account": accA.Kind + "/" + accA.ID},
	})
	elsewhere := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/session", ID: "sess-b",
		Properties: map[string]any{"label": "on B", "account": accB.Kind + "/" + accB.ID},
	})
	provenance := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/pointer", ID: "ptr-a",
		Properties: map[string]any{"label": "not an owner", "account": accA.Kind + "/" + accA.ID},
	})

	// A referent whose kind does not implement the trait is refused: the write
	// path checks the pin, not only the shape.
	stranger := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/stranger", ID: "str-1",
		Properties: map[string]any{"label": "no trait"},
	})
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/session", ID: "sess-bad",
		Properties: map[string]any{"label": "bad", "account": stranger.Kind + "/" + stranger.ID},
	})
	if err == nil || !strings.Contains(err.Error(), "does not implement the pinned trait") {
		t.Fatalf("write to a non-implementing kind: err = %v, want the trait refusal", err)
	}

	if _, err := ds.Delete(ctx, owner, accA.Kind, accA.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, accA.Kind, accA.ID); err == nil {
		t.Fatal("account A should be hard-deleted")
	}
	if _, err := ds.Get(ctx, synced.Kind, synced.ID); err == nil {
		t.Fatal("the trait-pinned cascade should have collected the session on account A")
	}
	if _, err := ds.Get(ctx, elsewhere.Kind, elsewhere.ID); err != nil {
		t.Fatalf("the session on account B should stand: %v", err)
	}
	if mustGet(t, ds, provenance.Kind, provenance.ID).DeletedAt != nil {
		t.Fatal("a trait reference without onDelete: cascade was collected")
	}
}

// `incoming` enumerates a trait pin the same way the cascade does: standing on
// an account, the reverse read lists the session that names it through the
// trait-pinned reference. Enumerability is what `onDelete: cascade`
// requires, so this is the other half of the same registry read.
func TestIncomingOverTraitReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, traitMirrorManifest()); err != nil {
		t.Fatalf("install trait mirror types: %v", err)
	}
	accA := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/providera", ID: "acct-a",
		Properties: map[string]any{"label": "A"},
	})
	sess := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorPackage + "/session", ID: "sess-a",
		Properties: map[string]any{"label": "on A", "account": accA.Kind + "/" + accA.ID},
	})
	page, err := ds.Incoming(ctx, accA.Kind, accA.ID, substrate.IncomingOptions{})
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	found := false
	for _, row := range page.Incoming {
		if row.From.ID != sess.ID {
			continue
		}
		if row.Property != "account" || row.Path != "" {
			t.Fatalf("the trait-pinned reference row reads %+v", row)
		}
		found = true
	}
	if !found {
		t.Fatalf("incoming did not list the session pointing through the trait pin: %+v", page.Incoming)
	}
}
