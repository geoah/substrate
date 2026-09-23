package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A trait binding is spelled in full, whatever declares the trait. A
// repository that still holds the `scheduling` sample's copy of `recurring`
// beside core's (decision record 0081 moved it) binds core's by identity and
// the copy stays declared and inert; a bare `recurring` is refused naming
// both copies, because a word that two packages declare names neither.
func TestATraitBindingIsSpelledInFull(t *testing.T) {
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
	tasks := func(recurring string) string {
		return `kind: substrate.reamde.dev/core/package
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
  traits: [` + recurring + `, substrate.reamde.dev/core/override]
  properties:
    name: {type: string}
    recurrence: {type: recurrence}
    rdates: {type: datetime, repeated: true}
    exdates: {type: datetime, repeated: true}
    timezone: {type: timezone}
    recurrenceOf: {type: reference, kind: ada.example.com/tasks/task}
    originalAt: {type: datetime}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/tasks/tasklog
data:
  authority: ada.example.com
  package: tasks
  names: {singular: tasklog}
  traits: [ada.example.com/scheduling/occurrencelog]
  properties:
    scheduledAt: {type: datetime}
`
	}
	files := func(recurring string) map[string]string {
		return map[string]string{
			"substrate.reamde.dev/core/core.yaml":        core,
			"ada.example.com/scheduling/scheduling.yaml": scheduling,
			"ada.example.com/tasks/tasks.yaml":           tasks(recurring),
		}
	}
	r := loadFixture(t, files(vocabulary.TraitRecurringCore))
	task, ok := r.ByIdentity("ada.example.com/tasks/task")
	if !ok {
		t.Fatal("task did not load")
	}
	bound := map[string]string{}
	for _, b := range task.Traits {
		bound[b.Trait] = b.Identity
	}
	if bound["recurring"] != vocabulary.TraitRecurringCore || bound["override"] != vocabulary.TraitOverrideCore {
		t.Fatalf("bindings = %v, want core's recurring and override", bound)
	}
	if !task.Implements(vocabulary.TraitRecurringCore) || !task.Implements("recurring") || task.Implements("ada.example.com/scheduling/recurring") {
		t.Fatalf("the shadowed copy counts as implemented, or the bare word does not: %v", task.Traits)
	}
	log, ok := r.ByIdentity("ada.example.com/tasks/tasklog")
	if !ok || len(log.Traits) != 1 || log.Traits[0].Identity != "ada.example.com/scheduling/occurrencelog" {
		t.Fatalf("tasklog bindings = %+v", log.Traits)
	}
	// The sample's own copy stays declared, addressable by identity.
	if c, ok := r.TraitByIdentity("ada.example.com/scheduling/recurring"); !ok || c.Identity() != "ada.example.com/scheduling/recurring" {
		t.Fatalf("the sample's copy is not addressable: %v %v", c, ok)
	}

	// Bare, the same binding is refused, and the refusal lists both copies.
	fsys := fstest.MapFS{}
	for name, body := range files("recurring") {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	_, err := vocabulary.LoadFS(fsys)
	if err == nil {
		t.Fatal("a bare trait binding loaded")
	}
	for _, want := range []string{
		`kind ada.example.com/tasks/task: data.traits: "recurring" is a bare name`,
		"named in full as <authority>/<package>/<name>",
		"this repository declares ada.example.com/scheduling/recurring, substrate.reamde.dev/core/recurring",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to say %q", err, want)
		}
	}
}

// A bare word declared once anywhere is refused all the same: the refusal is
// about the spelling, not about how many candidates there are, and it names
// the one full spelling to copy.
func TestABareTraitBindingIsRefusedEvenWhenUnique(t *testing.T) {
	scheduling := func(authority string) string {
		return `kind: substrate.reamde.dev/core/package
metadata:
  id: ` + authority + `/scheduling
data:
  authority: ` + authority + `
  package: scheduling
  version: 1
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: ` + authority + `/scheduling/occurrencelog
data:
  authority: ` + authority + `
  package: scheduling
  properties:
    scheduledAt: datetime
`
	}
	tasks := func(authority, binding string) string {
		return `kind: substrate.reamde.dev/core/package
metadata:
  id: ` + authority + `/tasks
data:
  authority: ` + authority + `
  package: tasks
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + authority + `/tasks/tasklog
data:
  authority: ` + authority + `
  package: tasks
  names: {singular: tasklog}
  traits: [` + binding + `]
  properties:
    scheduledAt: {type: datetime}
`
	}
	_, err := vocabulary.LoadFS(fstest.MapFS{
		"ada.example.com/scheduling/scheduling.yaml": {Data: []byte(scheduling("ada.example.com"))},
		"ada.example.com/tasks/tasks.yaml":           {Data: []byte(tasks("ada.example.com", "occurrencelog"))},
	})
	if err == nil || !strings.Contains(err.Error(), `"occurrencelog" is a bare name`) ||
		!strings.Contains(err.Error(), "this repository declares ada.example.com/scheduling/occurrencelog") {
		t.Fatalf("a bare binding under the declaring authority: %v, want the refusal naming the one full spelling", err)
	}
	// A word nothing declares is refused the same way, with nothing to offer.
	_, err = vocabulary.LoadFS(fstest.MapFS{
		"ada.example.com/tasks/tasks.yaml": {Data: []byte(tasks("ada.example.com", "nosuch"))},
	})
	if err == nil || !strings.Contains(err.Error(), `"nosuch" is a bare name`) || strings.Contains(err.Error(), "this repository declares") {
		t.Fatalf("a bare binding nothing declares: %v", err)
	}
	// Spelled in full, the binding lands from any authority.
	r := loadFixture(t, map[string]string{
		"ada.example.com/scheduling/scheduling.yaml": scheduling("ada.example.com"),
		"bo.example.com/tasks/tasks.yaml":            tasks("bo.example.com", "ada.example.com/scheduling/occurrencelog"),
	})
	log, ok := r.ByIdentity("bo.example.com/tasks/tasklog")
	if !ok || len(log.Traits) != 1 || log.Traits[0].Identity != "ada.example.com/scheduling/occurrencelog" {
		t.Fatalf("a qualified binding at another authority's trait = %+v", log.Traits)
	}
}
