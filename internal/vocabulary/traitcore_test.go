package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A bare trait name that core declares resolves to core's, even when another
// package declares a same-named trait (decision 0080): `recurring` moved from
// the `scheduling` sample into core, and a repository that still holds the
// sample's copy must keep every bare binding working, shadowed onto core's.
// Resolution stays package-first, so the sample's own kinds binding its own
// trait would still get it, and stays unique-across-packages for a name core
// does not declare.
func TestBareTraitNameResolvesToCoreFirst(t *testing.T) {
	const core = `kind: substrate.reamde.dev/core/package
metadata:
  id: substrate.reamde.dev/core
data:
  authority: substrate.reamde.dev
  package: core
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/recurring
data:
  authority: substrate.reamde.dev
  package: core
  properties:
    recurrence: recurrence
    rdates: datetime
    exdates: datetime
    timezone: timezone
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/override
data:
  authority: substrate.reamde.dev
  package: core
  properties:
    recurrenceOf: reference
    originalAt: datetime
`
	// The sample's leftover copy, same name, same shape.
	const scheduling = `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/scheduling
data:
  authority: ada.example.com
  package: scheduling
  version: 3
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: ada.example.com/scheduling/recurring
data:
  authority: ada.example.com
  package: scheduling
  properties:
    recurrence: recurrence
    rdates: datetime
    exdates: datetime
    timezone: timezone
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: ada.example.com/scheduling/occurrencelog
data:
  authority: ada.example.com
  package: scheduling
  properties:
    scheduledAt: datetime
`
	// A kind in a third package binding both names bare: `recurring` is
	// declared twice across packages and must land on core's; `occurrencelog`
	// is declared once and resolves as it always did. `override` contracts a
	// reference, which the kind pins itself.
	const tasks = `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/tasks
data:
  authority: ada.example.com
  package: tasks
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/tasks/task
data:
  authority: ada.example.com
  package: tasks
  names: {singular: task}
  traits: [recurring, override]
  properties:
    name: {type: string}
    recurrence: {type: recurrence}
    rdates: {type: datetime, repeated: true}
    exdates: {type: datetime, repeated: true}
    timezone: {type: timezone}
    recurrenceOf: {type: reference, kind: task}
    originalAt: {type: datetime}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/tasks/tasklog
data:
  authority: ada.example.com
  package: tasks
  names: {singular: tasklog}
  traits: [occurrencelog]
  properties:
    scheduledAt: {type: datetime}
`
	r := loadFixture(t, map[string]string{
		"substrate.reamde.dev/core/core.yaml":        core,
		"ada.example.com/scheduling/scheduling.yaml": scheduling,
		"ada.example.com/tasks/tasks.yaml":           tasks,
	})
	task, ok := r.ByIdentity("ada.example.com/tasks/task")
	if !ok {
		t.Fatal("task did not load")
	}
	bound := map[string]string{}
	for _, b := range task.Traits {
		bound[b.Trait] = b.Identity
	}
	if bound["recurring"] != vocabulary.TraitRecurringCore {
		t.Fatalf("bare `recurring` resolved to %q, want core's %q", bound["recurring"], vocabulary.TraitRecurringCore)
	}
	if bound["override"] != vocabulary.TraitOverrideCore {
		t.Fatalf("bare `override` resolved to %q, want core's", bound["override"])
	}
	if !task.Implements(vocabulary.TraitRecurringCore) || task.Implements("ada.example.com/scheduling/recurring") {
		t.Fatalf("the shadowed copy still counts as implemented: %v", task.Traits)
	}
	log, ok := r.ByIdentity("ada.example.com/tasks/tasklog")
	if !ok || len(log.Traits) != 1 || log.Traits[0].Identity != "ada.example.com/scheduling/occurrencelog" {
		t.Fatalf("a name core does not declare must still resolve uniquely across packages: %+v", log.Traits)
	}
	// The sample's own copy stays declared and inert.
	if c, err := r.ResolveTrait("ada.example.com/scheduling", "recurring"); err != nil || c.Identity() != "ada.example.com/scheduling/recurring" {
		t.Fatalf("the declaring package must still see its own trait first: %v %v", c, err)
	}
}
