package engine_test

// A KIND TITLES ITSELF from a property it declares (decision record 0016).
// The built-in `title` column is what a displayTemplate RENDERS INTO, never
// what a kind stores. These tests hold the shipped kinds that moved onto a
// declared heading, the alternation that carries the records written before
// they had one, and what that alternation costs.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	tsAuthority    = "titles.example.substrate.reamde.dev"
	transcriptKind = "calendar.substrate.reamde.dev/transcript"
)

// The two shipped kinds this closed (issue 123): `task` and `transcript` used
// to keep their heading in the built-in slot, and both now declare `name` and
// render it. The put that writes `title` instead is the cost, stated: the
// engine derives the column, so the value is dropped rather than refused.
func TestTaskAndTranscriptTitleThemselvesFromName(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, Properties: map[string]any{"name": "Buy milk"},
	})
	if task.Title != "Buy milk" || task.Properties["name"] != "Buy milk" {
		t.Fatalf("a task titles itself from `name`: title %q, properties %v", task.Title, task.Properties)
	}
	transcript := mustPut(t, ds, owner, substrate.PutInput{
		Kind: transcriptKind, Properties: map[string]any{"name": "Standup"},
	})
	if transcript.Title != "Standup" {
		t.Fatalf("a transcript titles itself from `name`: %q", transcript.Title)
	}
	// The write that used to work: a kind with a template derives its title,
	// so the slot a writer fills is dropped on the way in.
	written := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, Properties: map[string]any{"title": "Buy milk"},
	})
	if written.Title != "" {
		t.Fatalf("a written title on a templated kind is ignored, got %q", written.Title)
	}
}

// The legacy alternative, which is why `{name|title}` names the slot it
// retires: records written before the kind declared a heading hold it in the
// column alone, and a template of `{name}` would blank every one of them on
// its next write. The alternation reads the column instead, and yields to
// `name` the moment something writes one.
func TestLegacyTitlesSurviveADisplayTemplate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	memoNames := map[string]any{"singular": "memo", "plural": "memos"}
	before := []map[string]any{
		vocabulary.AuthorityManifest(tsAuthority, 0),
		vocabulary.KindManifest(tsAuthority, memoNames, map[string]any{
			"properties": map[string]any{"note": map[string]any{"type": "string"}},
		}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, before); err != nil {
		t.Fatalf("install the memo kind: %v", err)
	}
	memo := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tsAuthority + "/memo", ID: "m1",
		Properties: map[string]any{"title": "old heading", "note": "as written"},
	})
	if memo.Title != "old heading" {
		t.Fatalf("a kind with no template keeps the writer's title, got %q", memo.Title)
	}

	after := []map[string]any{
		vocabulary.KindManifest(tsAuthority, memoNames, map[string]any{
			"displayTemplate": "{name|title}",
			"properties": map[string]any{
				"note": map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			},
		}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, after); err != nil {
		t.Fatalf("upgrade the memo kind: %v", err)
	}

	// A write that touches something else re-derives the title. Without the
	// `|title` alternative this row would go blank here.
	moved := mustPatch(t, ds, owner, tsAuthority+"/memo", "m1", substrate.PatchInput{
		Properties: map[string]any{"note": "amended"},
	})
	if moved.Title != "old heading" {
		t.Fatalf("the legacy title did not survive the template: %q", moved.Title)
	}
	// And the declared property wins the moment it holds anything.
	named := mustPatch(t, ds, owner, tsAuthority+"/memo", "m1", substrate.PatchInput{
		Properties: map[string]any{"name": "new heading"},
	})
	if named.Title != "new heading" || named.Properties["name"] != "new heading" {
		t.Fatalf("`name` did not take over the title: %q, %v", named.Title, named.Properties)
	}
}

// The cost of reading the column the render writes into, pinned rather than
// discovered: the alternative cannot tell a title a writer authored from one
// it derived a moment ago, so CLEARING the heading leaves what was last
// rendered. Setting `name` again is the way back. It goes with the column
// (issue 68), which is where an authored title gets storage of its own.
func TestClearingTheHeadingKeepsTheRenderedTitle(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, ID: "t-headed",
		Properties: map[string]any{"name": "Buy milk"},
	})
	cleared := mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	})
	if _, still := cleared.Properties["name"]; still {
		t.Fatalf("the heading did not clear: %v", cleared.Properties)
	}
	if cleared.Title != "Buy milk" {
		t.Fatalf("the rendered title after clearing `name` = %q, want the stale \"Buy milk\"", cleared.Title)
	}
	renamed := mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Buy oat milk"},
	})
	if renamed.Title != "Buy oat milk" {
		t.Fatalf("a new heading did not take: %q", renamed.Title)
	}
}
