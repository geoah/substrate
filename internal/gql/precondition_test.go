package gql

import (
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/substrate"
)

// Every mutation that destroys or moves a record takes the version
// precondition put and patch already take: `ifVersion` on delete and split,
// one per participant on merge. Each is optional and typed Long, the scalar
// `version` is served as, so a client reads a version and sends it back
// without a conversion.
func TestMutationsDeclareVersionPreconditions(t *testing.T) {
	person := substrate.KindInfo{
		Identity: "samples.substrate.reamde.dev/people/person", Name: "person",
		Authority: "samples.substrate.reamde.dev", Version: 1, Source: "builtin",
		Definition: map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
	}
	schema, err := BuildSchema([]substrate.KindInfo{person})
	if err != nil {
		t.Fatal(err)
	}
	fields := schema.MutationType().Fields()
	for field, args := range map[string][]string{
		"delete": {"ifVersion"},
		"merge":  {"winnerVersion", "loserVersion"},
		"split":  {"ifVersion"},
	} {
		f := fields[field]
		if f == nil {
			t.Fatalf("no %s mutation", field)
		}
		declared := map[string]graphql.Input{}
		for _, a := range f.Args {
			declared[a.Name()] = a.Type
		}
		for _, name := range args {
			typ, ok := declared[name]
			if !ok {
				t.Fatalf("%s declares no %s argument: %v", field, name, declared)
			}
			if typ != longScalar {
				t.Fatalf("%s.%s is %v, want the optional Long a version is served as", field, name, typ)
			}
		}
	}
}
