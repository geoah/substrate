package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// retentionOf, the one answer a caller asks, reads what the declaration
// states, then the repository's override over it, and falls back to all; a
// deleted override gives the declared value back.
func TestRetentionOfReadsTheOverrideThenTheDeclarationThenAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	ask := func(kind string) string {
		t.Helper()
		r, err := ds.retentionOf(ctx, kind)
		if err != nil {
			t.Fatal(err)
		}
		return r.Value
	}
	const runs = "substrate.reamde.dev/core/triggerrun"
	if got := ask(runs); got != "P7D" {
		t.Errorf("the declared bookkeeping retention reads %q, want P7D", got)
	}
	if got := ask("substrate.reamde.dev/core/contact"); got != "all" {
		t.Errorf("a kind that states none reads %q, want all", got)
	}
	if got := ask("nobody.example.com/nothing/here"); got != "all" {
		t.Errorf("an undeclared kind reads %q, want all", got)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindRetention, ID: "runs", Properties: map[string]any{
		propRetentionAppliesTo: map[string]any{"ref": kindKind + "/" + runs},
		propRetentionValue:     "none",
	}}); err != nil {
		t.Fatal(err)
	}
	if got := ask(runs); got != "none" {
		t.Errorf("the overridden kind reads %q, want none", got)
	}
	if _, err := ds.Delete(ctx, substrate.ActorPerson, kindRetention, "runs", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if got := ask(runs); got != "P7D" {
		t.Errorf("after the override went the kind reads %q, want its declared P7D", got)
	}
}

// An override names a kind by reference, and the reference may wait for the
// kind: it applies to nothing until the kind is declared, and nothing again
// once the kind is removed, whose history then keeps all. retentionOf and
// the listing agree, and the listing names only declared kinds.
func TestAnOverrideOfAnUndeclaredKindAppliesToNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	const kind = "kept.example.com/kept/note"
	ask := func() string {
		t.Helper()
		r, err := ds.retentionOf(ctx, kind)
		if err != nil {
			t.Fatal(err)
		}
		return r.Value
	}
	listed := func() (substrate.KindRetention, bool) {
		t.Helper()
		all, err := ds.KindRetentions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, kr := range all {
			if kr.Kind == kind {
				return kr, true
			}
		}
		return substrate.KindRetention{}, false
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindRetention, ID: "note", Properties: map[string]any{
		propRetentionAppliesTo: map[string]any{"ref": kindKind + "/" + kind},
		propRetentionValue:     "none",
	}}); err != nil {
		t.Fatal(err)
	}
	if got := ask(); got != "all" {
		t.Errorf("before the kind is declared, its override makes it %q, want all", got)
	}
	if kr, ok := listed(); ok {
		t.Errorf("the listing names an undeclared kind: %+v", kr)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		{
			"kind": kindPackage, "metadata": map[string]any{"id": "kept.example.com/kept"},
			"data": map[string]any{"authority": "kept.example.com", "package": "kept", "version": 1},
		},
		{
			"kind": kindKind, "metadata": map[string]any{"id": kind},
			"data": map[string]any{
				"authority": "kept.example.com", "package": "kept",
				"names":           map[string]any{"singular": "note"},
				"displayTemplate": "{label}",
				"properties":      map[string]any{"label": map[string]any{"type": "string"}},
			},
		},
	}); err != nil {
		t.Fatalf("declare the kind: %v", err)
	}
	if got := ask(); got != "none" {
		t.Errorf("once the kind is declared its override makes it %q, want none", got)
	}
	if kr, ok := listed(); !ok || kr.Effective != "none" || kr.Override != "none" {
		t.Errorf("the listing reads %+v (listed %v), want the override applied", kr, ok)
	}
	if _, err := ds.Delete(ctx, substrate.ActorPerson, kindKind, kind, substrate.DeleteInput{}); err != nil {
		t.Fatalf("remove the kind: %v", err)
	}
	if got := ask(); got != "all" {
		t.Errorf("once the kind is removed its override makes it %q, want all", got)
	}
	if kr, ok := listed(); ok {
		t.Errorf("the listing names the removed kind: %+v", kr)
	}
}
