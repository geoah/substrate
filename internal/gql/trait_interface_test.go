package gql

import (
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/substrate"
)

func traitKind(identity, name, authority string, props map[string]any) substrate.KindInfo {
	return substrate.KindInfo{
		Identity: identity, Name: name, Authority: authority, Version: 1, Source: "builtin",
		Definition: map[string]any{"traits": []any{"recurring"}, "properties": props},
	}
}

// A trait interface takes each shared property at the type the implementing
// kinds give it. With one implementer, every property is shared, and a
// datetime among them (here also a machine stamp, which the machine interface
// types DateTime) used to meet an interface field spelled String, so the
// schema failed to build for any repository holding that one kind.
func TestTraitInterfaceFieldsTakeTheImplementersTypes(t *testing.T) {
	task := traitKind("samples.substrate.reamde.dev/tasks/task", "task", "samples.substrate.reamde.dev/tasks", map[string]any{
		"name":        map[string]any{"type": "string"},
		"recurrence":  map[string]any{"type": "recurrence"},
		"completedAt": map[string]any{"type": "datetime"},
		"project":     map[string]any{"type": "reference", "kind": "samples.substrate.reamde.dev/tasks/project"},
		"status": map[string]any{
			"type": "state", "states": []any{"open", "done"},
			"transitions": []any{map[string]any{"from": "open", "to": "done", "stamps": map[string]any{"completedAt": "now"}}},
		},
	})
	schema, err := BuildSchema([]substrate.KindInfo{task})
	if err != nil {
		t.Fatalf("one implementer with a datetime property: %v", err)
	}
	iface, ok := schema.Type("Recurring").(*graphql.Interface)
	if !ok {
		t.Fatalf("no Recurring interface: %v", schema.Type("Recurring"))
	}
	fields := iface.Fields()
	if fields["completedAt"] == nil || fields["completedAt"].Type != graphql.DateTime {
		t.Fatalf("Recurring.completedAt = %v, want DateTime", fields["completedAt"])
	}
	if fields["name"] == nil || fields["name"].Type != graphql.String {
		t.Fatalf("Recurring.name = %v, want String", fields["name"])
	}
	if fields["project"] == nil || fields["project"].Type.String() != "TaskProjectReference" {
		t.Fatalf("Recurring.project = %v, want the kind's reference object", fields["project"])
	}

	// Two implementers that disagree on a property's type share the interface
	// without that field; each object keeps its own.
	workout := traitKind("samples.substrate.reamde.dev/fitness/workout", "workout", "samples.substrate.reamde.dev/fitness", map[string]any{
		"name":        map[string]any{"type": "string"},
		"recurrence":  map[string]any{"type": "recurrence"},
		"completedAt": map[string]any{"type": "string"},
	})
	schema, err = BuildSchema([]substrate.KindInfo{task, workout})
	if err != nil {
		t.Fatalf("two implementers disagreeing on a type: %v", err)
	}
	iface = schema.Type("Recurring").(*graphql.Interface)
	if f := iface.Fields()["completedAt"]; f != nil {
		t.Fatalf("Recurring.completedAt = %v, want the disputed field omitted", f.Type)
	}
	if f := iface.Fields()["recurrence"]; f == nil || f.Type != graphql.String {
		t.Fatalf("Recurring.recurrence = %v, want String", f)
	}
	if obj := schema.Type("Task").(*graphql.Object); obj.Fields()["completedAt"].Type != graphql.DateTime {
		t.Fatalf("Task.completedAt = %v, want DateTime", obj.Fields()["completedAt"].Type)
	}
}
