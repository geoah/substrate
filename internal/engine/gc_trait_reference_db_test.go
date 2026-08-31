package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// A shared kind (calendar, conversation, emailthread) owns its account through a
// TRAIT-pinned cascading reference, not a kind-pinned one: two providers each
// declare their own account kind, and pinning one would tie the shared kind to
// one provider (0034). This holds the two halves that pin buys: the write path
// refuses a referent whose kind does not implement the trait, and the GC cascade
// collects the shared record when the account it names is collected.

const traitMirrorAuthority = "traitmirror.example.com"

// traitMirrorManifest declares a `connected` trait, two account kinds that
// implement it (two providers), a `session` kind whose account is a trait-pinned
// cascading reference, and a `stranger` kind that implements nothing — so a
// session pointing at a stranger is refused.
func traitMirrorManifest() enginetest.Manifest {
	kind := func(id string, extra map[string]any) map[string]any {
		data := map[string]any{
			"authority":       traitMirrorAuthority,
			"names":           map[string]any{"singular": last(id), "plural": last(id) + "s"},
			"displayTemplate": "{label}",
			"properties": map[string]any{
				"label": map[string]any{"type": "string"},
			},
		}
		for k, v := range extra {
			data[k] = v
		}
		return map[string]any{
			"kind":     "core.substrate.reamde.dev/kind",
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
			"kind":     "core.substrate.reamde.dev/kind",
			"metadata": map[string]any{"id": id},
			"data": map[string]any{
				"authority":       traitMirrorAuthority,
				"names":           map[string]any{"singular": last(id), "plural": last(id) + "s"},
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
			{
				"kind":     "core.substrate.reamde.dev/authority",
				"metadata": map[string]any{"id": traitMirrorAuthority},
				"data":     map[string]any{"version": 1},
			},
			{
				"kind":     "core.substrate.reamde.dev/trait",
				"metadata": map[string]any{"id": traitMirrorAuthority + "/connected"},
				"data": map[string]any{
					"authority":  traitMirrorAuthority,
					"properties": map[string]any{"connRef": "secret"},
				},
			},
			account(traitMirrorAuthority + "/providera"),
			account(traitMirrorAuthority + "/providerb"),
			// The owner pointer under test: any kind that implements `connected`.
			kind(traitMirrorAuthority+"/session", map[string]any{
				"properties": map[string]any{
					"label": map[string]any{"type": "string"},
					"account": map[string]any{
						"type": "reference", "trait": "connected", "onDelete": "cascade",
					},
				},
			}),
			// Same trait pin, no `onDelete:`: the sweep must leave it alone.
			kind(traitMirrorAuthority+"/pointer", map[string]any{
				"properties": map[string]any{
					"label": map[string]any{"type": "string"},
					"account": map[string]any{
						"type": "reference", "trait": "connected",
					},
				},
			}),
			// Implements nothing: a session may not point here.
			kind(traitMirrorAuthority+"/stranger", nil),
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
		Kind: traitMirrorAuthority + "/providera", ID: "acct-a",
		Properties: map[string]any{"label": "A"},
	})
	accB := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/providerb", ID: "acct-b",
		Properties: map[string]any{"label": "B"},
	})

	// A trait pin supplies no kind for a bare id, so the value is a full path.
	synced := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/session", ID: "sess-a",
		Properties: map[string]any{"label": "on A", "account": accA.Kind + "/" + accA.ID},
	})
	elsewhere := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/session", ID: "sess-b",
		Properties: map[string]any{"label": "on B", "account": accB.Kind + "/" + accB.ID},
	})
	provenance := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/pointer", ID: "ptr-a",
		Properties: map[string]any{"label": "not an owner", "account": accA.Kind + "/" + accA.ID},
	})

	// A referent whose kind does not implement the trait is refused: the write
	// path checks the pin, not only the shape.
	stranger := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/stranger", ID: "str-1",
		Properties: map[string]any{"label": "no trait"},
	})
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/session", ID: "sess-bad",
		Properties: map[string]any{"label": "bad", "account": stranger.Kind + "/" + stranger.ID},
	})
	if err == nil || !strings.Contains(err.Error(), "does not implement the pinned trait") {
		t.Fatalf("write to a non-implementing kind: err = %v, want the trait refusal", err)
	}

	if _, err := ds.Delete(ctx, owner, accA.Kind, accA.ID); err != nil {
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
		Kind: traitMirrorAuthority + "/providera", ID: "acct-a",
		Properties: map[string]any{"label": "A"},
	})
	sess := mustPut(t, ds, owner, substrate.PutInput{
		Kind: traitMirrorAuthority + "/session", ID: "sess-a",
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
