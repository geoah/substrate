package engine_test

// Ordering by a declared NUMBER sorts as a number. `props->>` is text, so an
// int-typed property used to sort lexicographically — 0, 1, 10, 11, 2 — which
// is not an ordering anyone asked for and said nothing about it. The agent
// transcript is where this bites: `turn` counts rows, a single tool-using
// exchange burns several, and a conversation crosses row 10 almost at once.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const orderAuthority = "order.example.substrate.reamde.dev"

func TestOrderByAnIntPropertySortsNumerically(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.AuthorityManifest(orderAuthority, ""),
		vocabulary.KindManifest(orderAuthority,
			map[string]any{"singular": "step", "plural": "steps"},
			map[string]any{"properties": map[string]any{
				"turn": map[string]any{"type": "int"},
				"name": map[string]any{"type": "string"},
			}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Deliberately spanning the boundary a text sort gets wrong.
	for _, n := range []int{0, 1, 2, 9, 10, 11, 20, 100} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: orderAuthority + "/step",
			Properties: map[string]any{
				"turn": n, "name": "step",
			},
		})
	}

	page, err := ds.List(ctx, substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{orderAuthority + "/step"}},
		OrderBy: []substrate.Order{{Property: "turn"}},
		First:   50,
	})
	if err != nil {
		t.Fatalf("list ordered by an int: %v", err)
	}
	var got []int
	for _, r := range page.Records {
		f, _ := r.Properties["turn"].(float64)
		got = append(got, int(f))
	}
	want := []int{0, 1, 2, 9, 10, 11, 20, 100}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordered %v, want %v", got, want)
		}
	}
}

func TestOrderByAStringPropertyStaysTextual(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.AuthorityManifest(orderAuthority, ""),
		vocabulary.KindManifest(orderAuthority,
			map[string]any{"singular": "step", "plural": "steps"},
			map[string]any{"properties": map[string]any{
				"turn": map[string]any{"type": "int"},
				"name": map[string]any{"type": "string"},
			}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// A numeric-LOOKING string is still a string: casting it would be the same
	// silent reinterpretation, in the other direction.
	for _, name := range []string{"10", "9", "2"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind:       orderAuthority + "/step",
			Properties: map[string]any{"name": name},
		})
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{orderAuthority + "/step"}},
		OrderBy: []substrate.Order{{Property: "name"}},
		First:   50,
	})
	if err != nil {
		t.Fatalf("list ordered by a string: %v", err)
	}
	var got []string
	for _, r := range page.Records {
		s, _ := r.Properties["name"].(string)
		got = append(got, s)
	}
	want := []string{"10", "2", "9"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordered %v, want %v", got, want)
		}
	}
}
