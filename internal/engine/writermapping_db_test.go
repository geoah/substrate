package engine_test

// A mapping writes no property that names its writers (decision 0290): the
// recompute carries the value of whoever wrote the winning source, and what
// the engine keeps of that is attribution, not proof of who chose each value,
// so the write that would carry such a value, or remove one, is refused
// whoever started it. An unguarded property maps as it always did.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAMappingWritesNoPropertyThatNamesItsWriters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const pkg = "mapped.example.com/mapped"
	work, row := pkg+"/work", pkg+"/row"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "mapped", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "work"}, map[string]any{"properties": map[string]any{
				"name":     map[string]any{"type": "string"},
				"verified": map[string]any{"type": "bool", "writer": "person"},
				"status":   map[string]any{"type": "string", "writer": "system"},
				"note":     map[string]any{"type": "string", "writer": []any{"function", "agent"}},
			}}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "row"}, map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"flag": map[string]any{"type": "bool"},
				"code": map[string]any{"type": "string"},
				"text": map[string]any{"type": "string"},
				"work": map[string]any{"type": "reference", "kind": work, "required": true, "mustExist": true, "subject": true},
			}}),
			vocabulary.MappingManifest(pkg, "rowwork", map[string]any{
				"from": row, "to": work, "property": "work",
				"map": map[string]any{
					"name":     map[string]any{"path": "name"},
					"verified": map[string]any{"path": "flag"},
					"status":   map[string]any{"path": "code"},
					"note":     map[string]any{"path": "text"},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	fn := substrate.FunctionActor("mapped.example.com", "mapped", "sync")
	refused := func(t *testing.T, err error, what string) {
		t.Helper()
		if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "a mapping writes no property that names its writers") {
			t.Fatalf("%s = %v, want refused: a mapping writes no guarded property", what, err)
		}
	}

	// Every hand's source, the system's included, is refused a guarded
	// target, even one its own direct write may set.
	for _, c := range []struct {
		hand  substrate.Actor
		props map[string]any
		what  string
	}{
		{fn, map[string]any{"name": "one", "text": "fetched"}, "a function's source onto a function's property"},
		{fn, map[string]any{"name": "two", "flag": true}, "a function's source onto a person's property"},
		{owner, map[string]any{"name": "three", "flag": true}, "a person's source onto a person's property"},
		{owner, map[string]any{"name": "four", "code": "set"}, "a person's source onto the system's property"},
		{substrate.ActorSystem, map[string]any{"name": "five", "text": "swept"}, "the system's source onto a function's property"},
	} {
		_, err := ds.Put(ctx, c.hand, substrate.PutInput{Kind: row, ID: fid(c.props["name"].(string)), Properties: c.props})
		refused(t, err, c.what)
	}

	// An unguarded property maps as it did.
	src := mustPut(t, ds, owner, substrate.PutInput{Kind: row, ID: fid("six"), Properties: map[string]any{"name": "six"}})
	k, wid, _ := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, row, src.ID), "work"))
	if got := mustGet(t, ds, k, wid).Properties["name"]; got != "six" {
		t.Fatalf("the mapped name = %v, want six", got)
	}

	// A release of an unguarded value runs as it did, the system's included:
	// the recompute after the source's delete removes the mapped name.
	if _, err := ds.Delete(ctx, substrate.ActorSystem, row, src.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("the system's delete of a source whose mapped values are unguarded: %v", err)
	}
	if got := mustGet(t, ds, k, wid).Properties["name"]; got != nil {
		t.Fatalf("after the source's delete the mapped name = %v, want it released", got)
	}
}
