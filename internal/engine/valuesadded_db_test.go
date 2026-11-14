package engine_test

// A `values:` list declared where none was is refused while a live record
// holds a string outside it, on a property, an object field and a link
// property, and admitted where the records already satisfy it.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const vaPackage = "valuesadded.example.substrate.reamde.dev/va"

func vaProps(size, tone, role map[string]any) map[string]any {
	return map[string]any{
		"size": size,
		"look": map[string]any{"type": "object", "fields": map[string]any{"tone": tone}},
		"maker": map[string]any{
			"type": "reference", "kind": vaPackage + "/gadget",
			"properties": map[string]any{"role": role},
		},
	}
}

func vaApply(t *testing.T, ds substrate.Dataset, props map[string]any) error {
	t.Helper()
	_, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(vaPackage, 0),
		vocabulary.KindManifest(vaPackage, map[string]any{"singular": "gadget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(vaPackage, map[string]any{"singular": "widget"},
			map[string]any{"properties": props}),
	})
	return err
}

func vaString(values ...any) map[string]any {
	if len(values) == 0 {
		return map[string]any{"type": "string"}
	}
	return map[string]any{"type": "string", "values": values}
}

func TestValuesAddedIsANarrowing(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	open := vaProps(vaString(), vaString(), vaString())
	if err := vaApply(t, ds, open); err != nil {
		t.Fatalf("install: %v", err)
	}
	gadget := mustPut(t, ds, owner, substrate.PutInput{
		Kind: vaPackage + "/gadget", Properties: map[string]any{"name": "g"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: vaPackage + "/widget",
		Properties: map[string]any{
			"size":  "big",
			"look":  map[string]any{"tone": "warm"},
			"maker": map[string]any{vocabulary.ReferenceValueKey: gadget.ID, "role": "lead"},
		},
	})

	for _, c := range []struct {
		name  string
		props map[string]any
		says  string
	}{
		{
			"a property", vaProps(vaString("small"), vaString(), vaString()),
			`property "size" adds the allowed values "small"`,
		},
		{
			"an object field", vaProps(vaString(), vaString("cool"), vaString()),
			`object "look" field "tone" adds the allowed values "cool"`,
		},
		{
			"a link property", vaProps(vaString(), vaString(), vaString(map[string]any{"value": "member"})),
			`reference "maker" adds the allowed values "member" to link property "role"`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantNarrowingGuard(t, vaApply(t, ds, c.props), c.says, "while 1 live")
		})
	}

	// The same lists naming what the record holds admit, and hold the next
	// write to them.
	closed := vaProps(vaString("big", "small"), vaString("warm"), vaString(map[string]any{"value": "lead"}))
	if err := vaApply(t, ds, closed); err != nil {
		t.Fatalf("lists the record satisfies: %v", err)
	}
	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: vaPackage + "/widget", Properties: map[string]any{"size": "huge"},
	})
	wantRefusal(t, err, substrate.ErrValidation, `expected one of big, small`)
}

func TestValuesAddedAdmitsWithoutRecords(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := vaApply(t, ds, vaProps(vaString(), vaString(), vaString())); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := vaApply(t, ds, vaProps(vaString("small"), vaString("cool"), vaString(map[string]any{"value": "member"}))); err != nil {
		t.Fatalf("lists added over no records: %v", err)
	}
}
