package engine_test

// `required` requires, and `default` is stored.
//
// The two are one contract: a required property is writable without being
// named only because a declared default fills it, and the default is filled at
// the WRITE so the stored row and the changelog delta carry the same value. A
// default applied on the way out would be derived data, and the records table
// would stop being a fold of the changelog. The rebuild here is what proves it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const requiredAuthority = "required.example.substrate.reamde.dev"

const requiredTicket = requiredAuthority + "/ticket"

// badDefaultAuthority is where the unstorable default is declared.
const badDefaultAuthority = "baddefault.example.substrate.reamde.dev"

// requiredVocabulary declares one kind carrying the three cases: a required
// scalar with no default, a required scalar WITH one, and an ordinary optional
// property to patch with.
func requiredVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		vocabulary.AuthorityManifest(requiredAuthority, 0),
		vocabulary.KindManifest(requiredAuthority,
			map[string]any{"singular": "ticket", "plural": "tickets"},
			map[string]any{
				"displayTemplate": "{name}",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "required": true},
					"priority": map[string]any{
						"type": "enum", "values": []any{"none", "high"},
						"required": true, "default": "none",
					},
					"note": map[string]any{"type": "string"},
				},
			}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("apply the required vocabulary: %v", err)
	}
}

// wantProblem asserts the error is the validation error the API turns into a
// 422 with a populated `problems` array, naming the property.
func wantProblem(t *testing.T, err error, want string) {
	t.Helper()
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *substrate.ValidationError, got %v", err)
	}
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a validation error must match ErrValidation, got %v", err)
	}
	for _, p := range ve.Problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Fatalf("problems %v: expected one naming %q", ve.Problems, want)
}

func TestRequiredScalarIsRefusedNamingTheProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	requiredVocabulary(t, ds)

	_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: requiredTicket, ID: "t1"})
	wantProblem(t, err, "props.name")

	// An empty string is the unfilled form field the declaration refuses.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"name": ""},
	})
	wantProblem(t, err, "props.name")

	// An EXPLICIT null is the writer saying this record has no value here, so
	// the declared default does not refill it.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1",
		Properties: map[string]any{"name": "ship it", "priority": nil},
	})
	wantProblem(t, err, "props.priority")
}

func TestDeclaredDefaultLandsInTheRecordAndSurvivesARebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	requiredVocabulary(t, ds)

	created := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"name": "ship it"},
	})
	if created.Properties["priority"] != "none" {
		t.Fatalf("the declared default must be the stored value, got %v", created.Properties["priority"])
	}

	// The delta the write appended is what a rebuild replays: if the default
	// had been applied on the way out instead, the rebuilt row would lose it.
	rb, ok := svc.(interface {
		RebuildRepository(ctx context.Context, username string) (engine.RebuildReport, error)
	})
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := mustGet(t, ds, requiredTicket, "t1"); rebuilt.Properties["priority"] != "none" {
		t.Fatalf("the default must ride the changelog, got %v after the rebuild", rebuilt.Properties["priority"])
	}

	// A default seeds a create and never re-asserts itself: a later write that
	// does not name the property leaves what the record holds.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"priority": "high"},
	})
	after := mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"note": "later"},
	})
	if after.Properties["priority"] != "high" {
		t.Fatalf("a default must not overwrite a value on update, got %v", after.Properties["priority"])
	}
}

// `required` is a statement about the RECORD, so the merged row is what
// satisfies it: a patch that clears the value is refused, and one that does not
// mention it is not.
func TestRequiredIsCheckedOnTheMergedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	requiredVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: requiredTicket, ID: "t1", Properties: map[string]any{"name": "ship it"},
	})
	_, err := ds.Patch(ctx, owner, requiredTicket, "t1", substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	})
	wantProblem(t, err, "props.name")

	patched := mustPatch(t, ds, owner, requiredTicket, "t1", substrate.PatchInput{
		Properties: map[string]any{"note": "still here"},
	})
	if patched.Properties["name"] != "ship it" {
		t.Fatalf("a patch that never mentions a required property must pass, got %v", patched.Properties)
	}
}

// A default no write could store is refused where it is declared, not at every
// create of the kind.
func TestDeclaredDefaultMustBeStorable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.AuthorityManifest(badDefaultAuthority, 0),
		vocabulary.KindManifest(badDefaultAuthority,
			map[string]any{"singular": "seen", "plural": "seens"},
			map[string]any{
				"properties": map[string]any{
					// Postgres has no year zero, so no write could ever store it.
					"seenAt": map[string]any{"type": "datetime", "default": "0000-01-01T00:00:00Z"},
				},
			}),
	})
	wantProblem(t, err, "no year zero")
}
