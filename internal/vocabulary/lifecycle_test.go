package vocabulary_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// The lifecycle trait (decision 0226): a kind's status, an enum or a state
// machine, lists the values that close a record, and a binding may rename
// it.

// overSeed loads one package of the test's beside the seeded ones, so its
// kinds bind core's own traits.
func overSeed(t *testing.T, docs string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{}
	seed := kinds.Seed()
	if err := fs.WalkDir(seed, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(seed, path)
		fsys[path] = &fstest.MapFile{Data: data}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fsys["ada.example.com/work/all.yaml"] = &fstest.MapFile{Data: []byte(docs)}
	return vocabulary.LoadFS(fsys)
}

// workPackage is the test's package with one kind, `chore`, binding traits
// over properties, after any documents of its own.
func workPackage(traits, properties string, more ...string) string {
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/work
data:
  authority: ada.example.com
  package: work
  version: 1
---
` + documents(more) + `kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/work/chore
data:
  authority: ada.example.com
  package: work
  names:
    singular: chore
  traits:
` + traits + `  properties:
` + properties
}

// documents is each document followed by its separator.
func documents(docs []string) string {
	var out strings.Builder
	for _, d := range docs {
		out.WriteString(d + "---\n")
	}
	return out.String()
}

const (
	bindsLifecycle = "    - substrate.reamde.dev/core/lifecycle\n"
	renamesState   = "    - \"substrate.reamde.dev/core/lifecycle(status: state)\"\n"
	choreMachine   = `    status:
      type: state
      states: [pending, doing, completed, skipped]
      initial: pending
      transitions:
        - from: pending
          to: doing
        - from: doing
          to: completed
        - from: pending
          to: skipped
`
)

func chore(t *testing.T, reg *vocabulary.Registry) *vocabulary.Kind {
	t.Helper()
	k, ok := reg.ByIdentity("ada.example.com/work/chore")
	if !ok {
		t.Fatal("the chore kind did not load")
	}
	return k
}

func TestAStateMachineAnEnumOrARenamedEnumIsAStatus(t *testing.T) {
	reg, err := overSeed(t, workPackage(bindsLifecycle, choreMachine+"      done: [completed]\n      dropped: [skipped]\n"))
	if err != nil {
		t.Fatalf("a state machine with its done and dropped states: %v", err)
	}
	k := chore(t, reg)
	status := k.Props[vocabulary.LifecycleStatus]
	if !k.Implements(vocabulary.TraitLifecycleCore) || !slices.Equal(status.Done, []string{"completed"}) ||
		!slices.Equal(status.Dropped, []string{"skipped"}) {
		t.Fatalf("implements %v, done %v, dropped %v", k.Implements(vocabulary.TraitLifecycleCore), status.Done, status.Dropped)
	}

	enum := `    status:
      type: enum
      values: [open, closed]
      done: [closed]
`
	if _, err := overSeed(t, workPackage(bindsLifecycle, enum)); err != nil {
		t.Fatalf("an enum with its done value: %v", err)
	}

	// A kind whose status is called `state`: the binding renames it.
	renamed := `    state:
      type: enum
      values: [open, closed]
      done: [closed]
`
	reg, err = overSeed(t, workPackage(renamesState, renamed))
	if err != nil {
		t.Fatalf("a renamed status: %v", err)
	}
	for _, b := range chore(t, reg).Traits {
		if b.Identity == vocabulary.TraitLifecycleCore && b.Renames[vocabulary.LifecycleStatus] != "state" {
			t.Fatalf("the binding's renames are %v, want status to state", b.Renames)
		}
	}
}

func TestAStatusListsWhatClosesARecordAndNothingElse(t *testing.T) {
	cases := map[string]struct{ traits, properties, want string }{
		"a lifecycle kind names its done values": {
			bindsLifecycle, choreMachine,
			"trait substrate.reamde.dev/core/lifecycle requires status to list the values that mean done",
		},
		"a listed value is one of its own": {
			bindsLifecycle, choreMachine + "      done: [finished]\n",
			`"finished" is not one of the property's values`,
		},
		"no value is listed twice": {
			bindsLifecycle, choreMachine + "      done: [completed]\n      dropped: [completed]\n",
			`"completed" is already listed in done`,
		},
		"one value at least stays open": {
			bindsLifecycle, "    status:\n      type: enum\n      values: [open, closed]\n      done: [closed]\n      dropped: [open]\n",
			"done and dropped leave no value open",
		},
		"the lists are lists": {
			bindsLifecycle, choreMachine + "      done: completed\n",
			"a list of the property's own values",
		},
		"only an enum or a machine lists them": {
			"", "    note:\n      type: string\n      done: [x]\n",
			"done and dropped name an enum's values or a state machine's states",
		},
		"a machine starts open": {
			bindsLifecycle, choreMachine + "      done: [completed]\n      dropped: [pending]\n",
			`the initial state "pending" is listed in dropped`,
		},
		"a list closes nothing": {
			"", "    phases:\n      type: enum\n      repeated: true\n      values: [open, closed]\n      done: [closed]\n",
			"done and dropped close a record by its one status",
		},
		"a field closes nothing": {
			"", "    box:\n      type: object\n      fields:\n        phase:\n          type: enum\n          values: [open, closed]\n          done: [closed]\n",
			"never an object field or link data",
		},
		"link data closes nothing": {
			"", "    parent:\n      type: reference\n      kind: ada.example.com/work/chore\n      properties:\n        phase:\n          type: enum\n          values: [open, closed]\n          done: [closed]\n",
			"never an object field or link data",
		},
		"a renamed status's shape names the kind's property": {
			renamesState, "    state:\n      type: enum\n      repeated: true\n      values: [open, closed]\n",
			"data.properties.state: trait substrate.reamde.dev/core/lifecycle requires state to hold one value",
		},
		"the status is an enum or a machine": {
			bindsLifecycle, "    status:\n      type: string\n",
			"trait substrate.reamde.dev/core/lifecycle requires type enum, got string",
		},
		"a renamed status is declared under its new name": {
			renamesState, choreMachine + "      done: [completed]\n",
			"requires property status, renamed to state",
		},
		"a binding renames only what the trait contracts": {
			"    - \"substrate.reamde.dev/core/lifecycle(phase: state)\"\n", choreMachine + "      done: [completed]\n",
			`contracts no property "phase"`,
		},
		"the engine's own traits keep their names": {
			"    - \"substrate.reamde.dev/core/revised(revision: rev)\"\n", "    rev:\n      type: int\n",
			"the engine reads its properties by name, so a binding renames none",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			traits := tc.traits
			if traits == "" {
				traits = "    - substrate.reamde.dev/core/temporal(point)\n"
			}
			_, err := overSeed(t, workPackage(traits, tc.properties))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// A status names the open states a record waits in until its owner accepts
// it (decision 0247): a suggestion is born proposed, accepted into an open
// state and dismissed into a dropped one.
func TestAStatusNamesItsProposedStates(t *testing.T) {
	machine := `    status:
      type: state
      states: [proposed, open, done, abandoned]
      initial: proposed
      transitions:
        - from: proposed
          to: open
        - from: proposed
          to: abandoned
        - from: open
          to: done
      done: [done]
      dropped: [abandoned]
      proposed: [proposed]
`
	reg, err := overSeed(t, workPackage(bindsLifecycle, machine))
	if err != nil {
		t.Fatalf("a machine born proposed: %v", err)
	}
	if got := chore(t, reg).Props[vocabulary.LifecycleStatus].Proposed; !slices.Equal(got, []string{"proposed"}) {
		t.Fatalf("proposed %v", got)
	}
	enum := "    status:\n      type: enum\n      values: [suggested, open, closed]\n      done: [closed]\n      proposed: [suggested]\n"
	if _, err := overSeed(t, workPackage(bindsLifecycle, enum)); err != nil {
		t.Fatalf("an enum with a proposed value: %v", err)
	}

	cases := map[string]struct{ properties, want string }{
		"a proposed value is one of its own": {
			choreMachine + "      done: [completed]\n      proposed: [maybe]\n",
			`"maybe" is not one of the property's values`,
		},
		"a proposed value is open": {
			choreMachine + "      done: [completed]\n      proposed: [completed]\n",
			`"completed" is already listed in done`,
		},
		"a proposal has an open state to be accepted into": {
			"    status:\n      type: enum\n      values: [suggested, closed]\n      done: [closed]\n      proposed: [suggested]\n",
			"done, dropped and proposed leave no value open to accept a proposal into",
		},
		"a machine can accept each proposal": {
			choreMachine + "      done: [completed]\n      dropped: [skipped]\n      proposed: [doing]\n",
			`"doing" has no transition to an open state`,
		},
		"only an enum or a machine names them": {
			"    note:\n      type: string\n      proposed: [x]\n",
			"done and dropped name an enum's values or a state machine's states",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := overSeed(t, workPackage(bindsLifecycle, tc.properties))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// A trait of the owner's own, which no engine path reads, may be renamed as
// lifecycle is.
func TestATraitOutsideCoreMayBeRenamed(t *testing.T) {
	flagged := `kind: substrate.reamde.dev/core/trait
metadata:
  id: ada.example.com/work/flagged
data:
  authority: ada.example.com
  package: work
  description: something its owner marked
  properties:
    flagged: bool
`
	binds := "    - \"ada.example.com/work/flagged(flagged: starred)\"\n"
	if _, err := overSeed(t, workPackage(binds, "    starred:\n      type: bool\n", flagged)); err != nil {
		t.Fatalf("a renamed trait of the owner's own: %v", err)
	}
	if _, err := overSeed(t, workPackage(binds, "    flagged:\n      type: bool\n", flagged)); err == nil ||
		!strings.Contains(err.Error(), "requires property flagged, renamed to starred") {
		t.Fatalf("a rename to a property the kind lacks was not refused: %v", err)
	}
}
