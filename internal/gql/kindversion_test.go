package gql

import (
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/substrate"
)

// Every record object carries `kindVersion` beside `version` (decision 0060),
// so the stamp REST and the CLI serve is reachable over GraphQL too. It is
// nullable: a record not written since the stamp existed answers null, the
// way REST omits the key, never 0.
func TestRecordObjectsCarryKindVersion(t *testing.T) {
	task := traitKind("samples.substrate.reamde.dev/tasks/task", "task", "samples.substrate.reamde.dev/tasks", map[string]any{
		"name": map[string]any{"type": "string"},
	})
	schema, err := BuildSchema([]substrate.KindInfo{task})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var objects int
	for _, ty := range schema.TypeMap() {
		obj, ok := ty.(*graphql.Object)
		if !ok || obj.Fields()["version"] == nil {
			continue
		}
		objects++
		f := obj.Fields()["kindVersion"]
		if f == nil {
			t.Fatalf("%s carries version but no kindVersion", obj.Name())
		}
		if f.Type != longScalar {
			t.Fatalf("%s.kindVersion = %v, want the nullable Long", obj.Name(), f.Type)
		}
	}
	if objects == 0 {
		t.Fatal("no record object type found")
	}

	stamped, err := resolveKindVersion(graphql.ResolveParams{Source: &substrate.Record{KindVersion: 4}})
	if err != nil || stamped != int64(4) {
		t.Fatalf("a stamped record resolves kindVersion %v (%v), want 4", stamped, err)
	}
	absent, err := resolveKindVersion(graphql.ResolveParams{Source: &substrate.Record{}})
	if err != nil || absent != nil {
		t.Fatalf("an unstamped record resolves kindVersion %v (%v), want null", absent, err)
	}
}
