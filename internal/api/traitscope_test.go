package api

import (
	"context"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// A trait scope (decision 0225) reaches what `*` would among the kinds whose
// declaration binds the trait when each request is checked.

const (
	temporalTrait    = "substrate.reamde.dev/core/temporal"
	conversationKind = "samples.substrate.reamde.dev/messaging/conversationmessage"
)

// traitscopeKinds is a repository whose implementors list names a kind its
// declarations do not bind the trait with, a disagreement no engine answers
// (it derives the list from the declarations) and the scoped dataset must not
// trust: the person is listed and binds nothing.
type traitscopeKinds struct{ stubDataset }

func (traitscopeKinds) Kinds(context.Context) ([]substrate.KindInfo, error) {
	return []substrate.KindInfo{
		{Identity: taskKind, Definition: map[string]any{"traits": []any{temporalTrait + "(point: dueAt)"}}},
		{Identity: personKind, Definition: map[string]any{}},
	}, nil
}

func (d traitscopeKinds) TypesImplementing(ctx context.Context, _ string) ([]substrate.KindInfo, error) {
	return d.Kinds(ctx)
}

// A trait scope's implementors are the listed kinds whose declaration binds
// the trait: the scoped dataset reads each declaration rather than the list.
func TestATraitScopeListsOnlyTheImplementorsWhoseDeclarationBindsIt(t *testing.T) {
	grant, err := scope.NewGrant([]string{"write:implements(" + temporalTrait + ")"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := newScopedDataset(traitscopeKinds{}, grant, substrate.AppActor("notes.example.org")).TypesImplementing(t.Context(), temporalTrait)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, k := range got {
		ids = append(ids, k.Identity)
	}
	if !slices.Equal(ids, []string{taskKind}) {
		t.Fatalf("implementors %v, want the task alone", ids)
	}
}
