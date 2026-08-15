package engine_test

// Where the DISPLAY title lives, and why it is not a property (#47).
//
// A kind with a `displayTemplate` DERIVES its title, so the value is nobody's
// property: it was computed, not authored. Injecting it into the property map
// put it beside the declared properties with nothing to tell them apart, and
// made `properties` mean "what a writer wrote, plus one thing nobody did". The
// derived value rides at the top level now.
//
// A kind with NO template is the other half: `title` there is the built-in
// authored slot, written through `properties.title`, so it must keep reading
// back in the property map or `get -o yaml | apply -f` would drop it. Same
// value in both places, two honest meanings — the property is what was
// authored, the field is what to render.
//
// FOLLOW-UP, deliberately not here: `reservedProps` still refuses a kind that
// declares a property CALLED `title` ("every record carries `title`"), which
// is why a `book` kind cannot have one. That refusal is a WRITE-path
// constraint — splitProps intercepts `title` before the declared-property
// branch, so a declared one would be validated against the wrong declaration.
// This change clears the read side; lifting the refusal needs splitProps to
// route by what the kind declares, and that is its own piece of work.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const titleAuthority = "titles.example.com"

func titleVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		vocabulary.AuthorityManifest(titleAuthority, ""),
		// Derives its title from other properties.
		vocabulary.KindManifest(titleAuthority,
			map[string]any{"singular": "book", "plural": "books"},
			map[string]any{
				"displayTemplate": "{author}: {subject}",
				"properties": map[string]any{
					"author":  map[string]any{"type": "string"},
					"subject": map[string]any{"type": "string"},
				},
			}),
		// No template: `title` is the built-in authored slot.
		vocabulary.KindManifest(titleAuthority,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{
				"properties": map[string]any{"tag": map[string]any{"type": "string"}},
			}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install title vocabulary: %v", err)
	}
}

func TestDerivedTitleIsNotAProperty(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	titleVocabulary(t, ds)

	book := mustPut(t, ds, owner, substrate.PutInput{
		Kind: titleAuthority + "/book",
		ID:   "b1",
		Properties: map[string]any{
			"author":  "Ada",
			"subject": "the Analytical Engine",
		},
	})

	if book.Title != "Ada: the Analytical Engine" {
		t.Fatalf("display title = %q, want the rendered template", book.Title)
	}
	// The property map is what a writer wrote, and nobody wrote this.
	if v, ok := book.Properties["title"]; ok {
		t.Fatalf("the derived title leaked into the property map: %v", v)
	}
	// The properties that DID get written are all still there.
	if book.Properties["author"] != "Ada" || book.Properties["subject"] != "the Analytical Engine" {
		t.Fatalf("declared properties = %v", book.Properties)
	}
}

func TestBuiltInTitleSlotStillReadsBackAsAProperty(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	titleVocabulary(t, ds)

	note := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       titleAuthority + "/note",
		ID:         "n1",
		Properties: map[string]any{"tag": "todo", "title": "Buy milk"},
	})

	// It is the display title...
	if note.Title != "Buy milk" {
		t.Fatalf("display title = %q", note.Title)
	}
	// ...AND a property, because here it is one: a writer authored it through
	// `properties.title`, so dropping it from the map would make
	// `get -o yaml | apply -f` lose the value.
	if note.Properties["title"] != "Buy milk" {
		t.Fatalf("the authored title slot did not read back as a property: %v", note.Properties["title"])
	}

	// And the round trip that depends on it: re-putting exactly what came back
	// keeps the title, rather than clearing a column the document no longer
	// mentions.
	again := mustPut(t, ds, owner, substrate.PutInput{
		Kind: titleAuthority + "/note", ID: "n1", Properties: note.Properties,
	})
	if again.Title != "Buy milk" {
		t.Fatalf("re-applying the read document dropped the title: %q", again.Title)
	}
}
