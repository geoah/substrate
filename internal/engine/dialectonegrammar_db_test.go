package engine

// THE DIALECT-1 GRAMMAR AGAINST A STORE. The fixtures in
// dialectonegrammar_internal_test.go hold the translation to the live loader's
// parse; these three hold it to a REPOSITORY OPEN, which is the thing a bad
// translation actually breaks: the rung runs at open, and a spelling it hands the
// loader unchanged fails there with the user locked out of everything.
//
// A dialect-1 store is built the one way it can be — a row planted in the
// dialect-1 shape (PlantDeclarationRow), its `definition` blob then written back
// to the spelling an OLDER binary stored (w2SeedLegacyBlob), and the dialect stamp
// wound back (windBackDialect) so the ladder re-runs.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// plantDialectOneRow rewrites one declaration row into the dialect-1 shape: the
// declaration back inside a `definition` blob, the mirrors beside it.
func plantDialectOneRow(t *testing.T, ds *dataset, kind, id string) {
	t.Helper()
	ctx := context.Background()
	row, err := ds.Get(ctx, kind, id)
	if err != nil {
		t.Fatalf("read %s %s: %v", kind, id, err)
	}
	if err := ds.PlantDeclarationRow(ctx, kind, id, ds.DialectOneProps(kind, id, row.Properties)); err != nil {
		t.Fatalf("plant %s %s: %v", kind, id, err)
	}
}

// TestDialectOneStoredDescriptionWeakensInsteadOfFailingTheOpen is F1: a stored
// function whose input property carries PROSE — many lines, past the live bound —
// migrates, and the repository opens. Dialect 1 passed an `input:` schema through
// without parsing what was inside it, so a store may hold a description the live
// parse refuses; the migration may lose prose, never a wire shape, so it is
// collapsed and truncated rather than refused.
func TestDialectOneStoredDescriptionWeakensInsteadOfFailingTheOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, closeSvc := w2Opener(t)
	ds := open()
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, w2Manifest(true)); err != nil {
		t.Fatalf("install the widgets authority: %v", err)
	}
	plantDialectOneRow(t, ds, kindFunction, w2Mirror)
	// 300 runes over ten lines: both refusals of the live parse at once.
	prose := strings.TrimSuffix(strings.Repeat("what the caller is looking for\n", 10), "\n")
	if n := utf8.RuneCountInString(prose); n != 309 {
		t.Fatalf("the fixture prose is %d runes — it has to be past the bound", n)
	}
	w2SeedLegacy(t, ds, map[string]any{
		"input": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": prose},
			},
			"required": []any{"query"},
		},
	}, 3)
	windBackDialect(t, ds.db)
	closeSvc()

	// The rung runs here. Before the weakening this open FAILED: the argument
	// carried the blob's prose verbatim and the live loader refused it, so the
	// repository holding one could not be opened at all.
	ds2 := open()
	row, err := ds2.Get(ctx, kindFunction, w2Mirror)
	if err != nil {
		t.Fatalf("read the migrated function: %v", err)
	}
	args, _ := row.Properties["arguments"].([]any)
	if len(args) != 1 {
		t.Fatalf("the migrated function carries %#v", row.Properties["arguments"])
	}
	arg, _ := args[0].(map[string]any)
	desc, _ := arg["description"].(string)
	switch {
	case strings.ContainsAny(desc, "\n\r"):
		t.Fatalf("the migrated description is still multi-line: %q", desc)
	case utf8.RuneCountInString(desc) != dialectOneMaxDescription:
		t.Fatalf("the migrated description is %d runes, want the bound %d: %q",
			utf8.RuneCountInString(desc), dialectOneMaxDescription, desc)
	case !strings.HasPrefix(desc, "what the caller is looking for what the caller"):
		t.Fatalf("the weakening did not keep the prose it kept: %q", desc)
	}
	// The wire shape is untouched: the argument the caller sends is still the
	// required `query` string.
	if arg["name"] != "query" || arg["type"] != vocabulary.ArgumentString || arg["required"] != true {
		t.Fatalf("the weakening moved the argument: %#v", arg)
	}
}

// TestDialectOneTraitVariantNameRefusesTheOpenByName is F2. A variant name is
// part of the trait's SHAPE — a kind binds one by name and its records store the
// binding — so a name the typed variant list cannot hold has no translation, and
// the rung says so with the fix in it. Without the refusal the rung hands the
// loader a list entry it rejects, and the open fails with the loader's own
// complaint about a document nobody in this store ever wrote.
//
// The names that DO translate are held by the grammar's own fixtures
// (dialectonegrammar_internal_test.go): point and range, this trait's real
// variants, are one of the cases there.
func TestDialectOneTraitVariantNameRefusesTheOpenByName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, closeSvc := w2Opener(t)
	ds := open()
	dsn := ds.svc.dsn
	const temporal = "core.substrate.reamde.dev/temporal"
	plantDialectOneRow(t, ds, kindTrait, temporal)
	w2SeedLegacyBlob(t, ds, temporal, map[string]any{
		// Dialect 1's variant MAPPING, with a name it never validated.
		"oneOf": map[string]any{
			"point":         map[string]any{"at": "datetime"},
			"point_in_time": map[string]any{"at": "datetime"},
			"range":         map[string]any{"at": "datetime", "endsAt": "datetime"},
		},
	})
	windBackDialect(t, ds.db)
	closeSvc()

	svc, err := Open(ctx, dsn, WithCredentialKey("test-cred-key"), WithKindsDir("../../kinds/core.substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("open the service: %v", err)
	}
	defer func() { _ = svc.Close() }()
	if _, err := svc.Dataset(ctx, "geoah"); err == nil {
		t.Fatal("the rung migrated a variant name the loader refuses")
	} else if !errors.Is(err, ErrDeclarationUntranslated) {
		t.Fatalf("the refusal is not the rung's guided one: %v", err)
	} else {
		for _, want := range []string{temporal, `"point_in_time"`, "re-declare the trait"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal names neither the trait nor the variant nor the fix (%q missing): %v", want, err)
			}
		}
	}
}

// TestDialectOnePropertyTypeValuesMigrateLabeled is F3: a refinement whose
// `values:` a dialect-1 blob spells as bare scalars migrates into the {value,
// label} objects core's `propertytype` declares, and the repository opens with
// the value set intact — which is what the row a bare scalar migrates into has to
// look like, whoever states the shape. That the RUNG is the one stating it is
// pinned by the grammar's own fixture (dialectonegrammar_internal_test.go): the
// live parse normalizes the same value today, so this open would survive the
// frozen translation being deleted, and that fixture would not.
func TestDialectOnePropertyTypeValuesMigrateLabeled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, closeSvc := w2Opener(t)
	ds := open()
	const shades = "shades.test.dev"
	const shade = shades + "/shade"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "shades", Authority: shades,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(shades, 0),
			vocabulary.ActorManifest(shades, vocabulary.AuthorityActor(shades)),
			{
				"kind":     kindPropertyType,
				"metadata": map[string]any{"id": shade},
				"data": map[string]any{
					"authority": shades, "base": "enum",
					"values": []any{"warm", "cool"},
				},
			},
			vocabulary.KindManifest(shades, map[string]any{"singular": "swatch", "plural": "swatches"},
				map[string]any{"properties": map[string]any{"shade": map[string]any{"type": "shade"}}}),
		},
	}); err != nil {
		t.Fatalf("install the shades authority: %v", err)
	}
	plantDialectOneRow(t, ds, kindPropertyType, shade)
	w2SeedLegacyBlob(t, ds, shade, map[string]any{"values": []any{"warm", "cool"}})
	windBackDialect(t, ds.db)
	closeSvc()

	ds2 := open()
	row, err := ds2.Get(ctx, kindPropertyType, shade)
	if err != nil {
		t.Fatalf("read the migrated refinement: %v", err)
	}
	values, _ := row.Properties["values"].([]any)
	if len(values) != 2 {
		t.Fatalf("the migrated refinement carries %#v", row.Properties["values"])
	}
	for i, want := range []string{"warm", "cool"} {
		entry, isEntry := values[i].(map[string]any)
		if !isEntry {
			t.Fatalf("value %d migrated as %T, not the labeled object the declaration holds", i, values[i])
		}
		if entry["value"] != want {
			t.Fatalf("value %d is %#v, want %q — the value set moved", i, entry, want)
		}
	}
	// The refinement still refines: a record writing a value outside the set is
	// refused, which is what makes the migrated `values` the contract and not
	// decoration.
	if _, err := ds2.Put(ctx, substrate.Actor(shades), substrate.PutInput{
		Kind: shades + "/swatch", ID: "one", Properties: map[string]any{"shade": "warm"},
	}); err != nil {
		t.Fatalf("an admitted value was refused after the migration: %v", err)
	}
	if _, err := ds2.Put(ctx, substrate.Actor(shades), substrate.PutInput{
		Kind: shades + "/swatch", ID: "two", Properties: map[string]any{"shade": "tepid"},
	}); err == nil {
		t.Fatal("the migrated value set admits a value it never declared")
	}
}
