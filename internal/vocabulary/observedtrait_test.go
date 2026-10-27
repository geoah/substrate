package vocabulary_test

import (
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// THE OBSERVED TRAIT NEVER GAINS A PROPERTY (decision 0154).
//
// Every kind that binds a trait declares every property the trait lists, and
// those kinds are the seeded observations package's, the installed
// catalogues' and any a repository declares itself. A property added here
// refuses every one of them that lacks it at the next boot, and a removed or
// retyped one changes what the observed index reads. So
// the contract is pinned exactly: a later need is a second trait bound beside
// this one, never an edit of this list. The one removal, `measure`, went with
// the engine's unit table (decision 0186), before any release stored it.
func TestObservedTraitContractIsPinned(t *testing.T) {
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	tr, ok := reg.TraitByIdentity(vocabulary.TraitObservedCore)
	if !ok {
		t.Fatalf("%s is not declared in core", vocabulary.TraitObservedCore)
	}
	shared := map[string]vocabulary.Datatype{
		"at": "datetime", "endsAt": "datetime", "metric": "reference", "source": "reference",
		"about": "reference", "channel": "string", "form": "string", "hold": "duration",
		"resetRule": "string", "resetAt": "datetime", "delta": "bool", "circular": "bool",
		"wrapAt": "decimal", "utcOffset": "int", "revision": "int",
	}
	want := map[string]map[string]vocabulary.Datatype{
		vocabulary.ObservedSingle: with(shared, map[string]vocabulary.Datatype{
			"value": "quantity", "code": "string", "statistic": "string", "period": "string",
			"sourceValue": "string", "sourceUnit": "string", "absentReason": "string", "origin": "reference",
		}),
		vocabulary.ObservedPacked: with(shared, map[string]vocabulary.Datatype{
			"unit": "string", "every": "duration", "offsets": "int", "values": "decimal", "codes": "string",
			"origins": "reference", "stats": "json",
		}),
	}
	if len(tr.Properties) != 0 {
		t.Errorf("observed declares variant-free properties %v; its properties live on the two variants", tr.Properties)
	}
	if !reflect.DeepEqual(tr.Variants, want) {
		for variant, props := range want {
			got := tr.Variants[variant]
			for name, dt := range props {
				if got[name] != dt {
					t.Errorf("%s.%s is %q, pinned %q", variant, name, got[name], dt)
				}
			}
			for name, dt := range got {
				if _, pinned := props[name]; !pinned {
					t.Errorf("%s.%s (%s) was added: a later need is a second trait bound beside observed", variant, name, dt)
				}
			}
		}
		for variant := range tr.Variants {
			if _, pinned := want[variant]; !pinned {
				t.Errorf("variant %s was added", variant)
			}
		}
	}
}

// The traits beside `observed` are pinned the same way, for the same reason:
// a kind that binds one may be installed or declared by a repository, so a
// property added to one refuses those kinds at the next boot. The metric's
// `units` and its `unit` as a reference came with the unit graph (decision
// 0186), before any release stored a metric.
func TestObservedCompanionTraitContractsArePinned(t *testing.T) {
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	for trait, want := range map[string]map[string]vocabulary.Datatype{
		vocabulary.TraitMetricCore: {
			"name": "string", "unit": "reference", "units": "reference", "form": "string", "codeSet": "string",
			"displayPrecision": "int", "category": "string", "bucketPlace": "string", "rollups": "string",
		},
		vocabulary.TraitSourceCore: {"priorities": "int"},
		vocabulary.TraitResultCore: {
			"codedValue": "json", "ratio": "json", "text": "text", "interpretation": "json", "referenceRanges": "json",
		},
		vocabulary.TraitRevisedCore: {"revision": "int", "origin": "reference"},
	} {
		tr, ok := reg.TraitByIdentity(trait)
		if !ok {
			t.Errorf("%s is not declared in core", trait)
			continue
		}
		if len(tr.Variants) != 0 || !reflect.DeepEqual(tr.Properties, want) {
			t.Errorf("%s declares %v (variants %v), pinned %v", trait, tr.Properties, tr.Variants, want)
		}
	}
}

func with(a, b map[string]vocabulary.Datatype) map[string]vocabulary.Datatype {
	out := map[string]vocabulary.Datatype{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
