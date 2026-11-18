package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// seededNoControl names every seeded kind that is in no control class and
// holds no sign-in material (vocabulary/control.go), each with why its
// records decide nothing a control kind decides: what code runs and what it
// may do, what the vocabulary is, and who may do what (decision 0276).
var seededNoControl = map[string]string{
	"substrate.reamde.dev/console/collectionview": "a saved view of one collection, a filter, sort and columns the console shows as a tab: what a person sees, not what anything may do",
	"substrate.reamde.dev/console/recordview":     "how the console lays out a kind's record page",
	"substrate.reamde.dev/core/alert":             "a problem the substrate found and reports, opened and closed by the engine",
	"substrate.reamde.dev/core/blob":              "the manifest of one stored blob: digest, size, name and media type",
	"substrate.reamde.dev/core/consolepreference": "the console's navigation and display preferences",
	"substrate.reamde.dev/core/contact":           "the owner's data: someone they deal with (decision 0231)",
	"substrate.reamde.dev/core/device":            "one install's push key and which events it takes, each person's own, written by their own token: where a push goes, not what anything may do",
	"substrate.reamde.dev/core/conversion":        "how a value in one unit converts to another, the owner's data reads convert through (decision 0186)",
	"substrate.reamde.dev/core/unit":              "a unit a quantity may be written in (decision 0186)",
	"substrate.reamde.dev/core/recordmerge":       "the record of one merge of two records, written as the merge is performed",
	"substrate.reamde.dev/core/recordsplit":       "the undo of one merge, performed as the merge was",
	"substrate.reamde.dev/core/recordmergerequest": "a proposed merge waiting for the owner: a request is the gate, writes nothing until it is decided, " +
		"and its accept writes as the hand that decides, held to that hand's rules",
	"substrate.reamde.dev/core/recordpatchrequest": "a proposed change waiting for the owner: a request is the gate, writes nothing until it is decided, " +
		"and its accept writes as the hand that decides, held to that hand's rules",
	"substrate.reamde.dev/core/repository": "the repository's read-only description of itself, projected from the control plane by the engine",
	"substrate.reamde.dev/core/release": "one version of a package the repository publishes: a system kind the generic surface refuses every write of " +
		"(forbidSystemKind), written only by the publish and the release behind mayPublish, and never changed or removed",
	"substrate.reamde.dev/core/triggerrun":          "one trigger delivery attempt, written by the engine once it settles",
	"substrate.reamde.dev/llm/approval":             "a call an agent waits to make: approving it runs a call the agent's grant already allows",
	"substrate.reamde.dev/llm/instruction":          "the owner's standing direction to one agent: text its prompt reads, which grants it nothing",
	"substrate.reamde.dev/llm/interaction":          "questions an agent asked the user, waiting in its thread",
	"substrate.reamde.dev/llm/memory":               "the owner's standing facts for their agents: text a prompt reads, which grants nothing (decision 0231)",
	"substrate.reamde.dev/llm/message":              "one turn of an agent thread's transcript",
	"substrate.reamde.dev/llm/thread":               "one agent invocation's conversation state",
	"substrate.reamde.dev/llm/threadmark":           "how far one person has read one thread: what a chat list shows as unread, which grants nothing (decision 0313)",
	"substrate.reamde.dev/observations/metric":      "the owner's data: something that can be measured (decision 0192)",
	"substrate.reamde.dev/observations/observation": "the owner's data: one measured value (decision 0192)",
	"substrate.reamde.dev/observations/source":      "the owner's data: a device, app or service that produces measured values (decision 0192)",
	"substrate.reamde.dev/observations/sourcelink":  "the owner's data: the sources that are one device (decision 0192)",
	"substrate.reamde.dev/observations/trace":       "the owner's data: many values of one metric over one period (decision 0192)",
}

// EVERY SEEDED KIND IS A CONTROL KIND OR NAMED AS NONE. A kind a seeded
// package gains is in a control class, holds sign-in material, or is named
// in seededNoControl with why it decides nothing, so a new kind is never a
// control kind by omission. It needs no database.
func TestEverySeededKindIsInAControlClassOrNamedAsNone(t *testing.T) {
	t.Parallel()
	reg, err := seedRegistry()
	if err != nil {
		t.Fatal(err)
	}
	seeded := map[string]bool{}
	for _, k := range reg.Kinds() {
		seeded[k.Identity] = true
		class := controlClassIn(reg, k)
		signIn := vocabulary.SignInMaterial(k.Identity)
		why, named := seededNoControl[k.Identity]
		switch {
		case (class != "" || signIn) && named:
			t.Errorf("%s is a control kind (class %q, sign-in %t) and seededNoControl names it as none", k.Identity, class, signIn)
		case class == "" && !signIn && !named:
			t.Errorf("%s is a seeded kind in no control class that holds no sign-in material: put it in a class (vocabulary/control.go) or name it in seededNoControl with why it decides nothing", k.Identity)
		case named && why == "":
			t.Errorf("seededNoControl names %s with no reason", k.Identity)
		}
	}
	for k := range seededNoControl {
		if !seeded[k] {
			t.Errorf("seededNoControl names %s, which no seeded package declares", k)
		}
	}
}
