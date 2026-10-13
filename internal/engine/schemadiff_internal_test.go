package engine

// classifyNarrowings reads two registries and no rows, so which diffs it
// refuses, and which it leaves to the apply's own rewrites, is held here
// without a database. The counts the narrowings carry run in the DB suite.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	narrowPackage = "n.example.com/n"
	narrowKind    = "n.example.com/n/widget"
)

// narrowingRegistry loads one package holding the widget kind with the given
// property lines (each indented four spaces under `properties:`) and a gadget
// kind a reference can pin.
func narrowingRegistry(t *testing.T, properties string) *vocabulary.Registry {
	t.Helper()
	reg, err := vocabulary.LoadFS(fstest.MapFS{"n.yaml": {Data: []byte(`kind: substrate.reamde.dev/core/package
metadata:
  id: n.example.com/n
data:
  authority: n.example.com
  package: n
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: n.example.com/n/gadget
data:
  authority: n.example.com
  package: n
  names:
    singular: gadget
  properties:
    name: {type: string}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: n.example.com/n/widget
data:
  authority: n.example.com
  package: n
  names:
    singular: widget
  properties:
` + properties)}})
	if err != nil {
		t.Fatalf("load the declarations: %v\n%s", err, properties)
	}
	return reg
}

func TestClassifyNarrowings(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		current, cand string
		// want holds one fragment of each narrowing's message, in order.
		want []string
		// query, where set, is the first narrowing's count query.
		query string
	}{
		{
			name:    "nothing changed",
			current: "    name: {type: string}\n",
			cand:    "    name: {type: string}\n",
		},
		{
			name:    "an optional property added",
			current: "    name: {type: string}\n",
			cand:    "    name: {type: string}\n    note: {type: string}\n",
		},
		{
			name:    "an optional property dropped is the apply's null step",
			current: "    name: {type: string}\n    note: {type: string}\n",
			cand:    "    name: {type: string}\n",
		},
		{
			name:    "required taken away",
			current: "    name: {type: string, required: true}\n",
			cand:    "    name: {type: string}\n",
		},
		{
			name:    "a scalar retyped",
			current: "    size: {type: string}\n",
			cand:    "    size: {type: bool}\n",
			want:    []string{`property "size" changes kind string → bool while %d live records hold values of the old kind`},
			query:   countPropQuery,
		},
		{
			name:    "a scalar becomes a list",
			current: "    size: {type: string}\n",
			cand:    "    size: {type: string, repeated: true}\n",
			want:    []string{`property "size" changes kind string → repeated string`},
			query:   countPropQuery,
		},
		{
			name:    "a string retyped to int counts the values that are not integers",
			current: "    size: {type: string}\n",
			cand:    "    size: {type: int}\n",
			want:    []string{`hold values that are not integers`},
			query:   countNonIntPropQuery,
		},
		{
			name:    "a string retyped to an enum counts the values outside it",
			current: "    size: {type: string}\n",
			cand:    "    size: {type: enum, values: [small, large]}\n",
			want:    []string{`hold a value outside "small", "large"`},
		},
		{
			name:    "an enum value added",
			current: "    size: {type: enum, values: [small]}\n",
			cand:    "    size: {type: enum, values: [small, large]}\n",
		},
		{
			name:    "an enum value removed",
			current: "    size: {type: enum, values: [small, large]}\n",
			cand:    "    size: {type: enum, values: [small]}\n",
			want:    []string{`property "size" removes value(s) "large"`},
		},
		{
			name:    "an enum value renamed is the apply's remap",
			current: "    size: {type: enum, values: [small, large]}\n",
			cand:    "    size: {type: enum, values: [{value: small}, {value: big, renamedFrom: large}]}\n",
		},
		{
			name:    "a state removed",
			current: "    phase: {type: state, states: [open, done, gone], initial: open, transitions: [{from: open, to: done}, {from: open, to: gone}]}\n",
			cand:    "    phase: {type: state, states: [open, done], initial: open, transitions: [{from: open, to: done}]}\n",
			want:    []string{`state property "phase" removes state(s) "gone"`},
			query:   countStateValuesQuery,
		},
		{
			name:    "a value turned into a state",
			current: "    phase: {type: string}\n",
			cand:    "    phase: {type: state, states: [open, done], initial: open, transitions: [{from: open, to: done}]}\n",
			want:    []string{`property "phase" changes kind (state and value do not convert) while %d live records hold a value`},
			query:   countPropQuery,
		},
		{
			name:    "an existing property becomes required",
			current: "    name: {type: string}\n",
			cand:    "    name: {type: string, required: true}\n",
			want:    []string{`property "name" becomes required`},
		},
		{
			name:    "an existing property becomes required with a default to backfill",
			current: "    name: {type: string}\n",
			cand:    "    name: {type: string, required: true, default: unnamed}\n",
		},
		{
			name:    "a new property added as required",
			current: "    name: {type: string}\n",
			cand:    "    name: {type: string}\n    code: {type: string, required: true}\n",
			want:    []string{`property "code" is added as required`},
		},
		{
			name:    "a new required property with a default to backfill",
			current: "    name: {type: string}\n",
			cand:    "    name: {type: string}\n    code: {type: string, required: true, default: none}\n",
		},
		{
			name:    "a property renamed counts the rows already holding the new name",
			current: "    caption: {type: string}\n",
			cand:    "    label: {type: string, renamedFrom: caption}\n",
			want:    []string{`property "caption" renamed to "label" while %d live records already carry a value under "label"`},
			query:   countPropQuery,
		},
		{
			name:    "a rename that also retypes refuses under the old name",
			current: "    caption: {type: string}\n",
			cand:    "    label: {type: bool, renamedFrom: caption}\n",
			want: []string{
				`property "caption" renamed to "label"`,
				`property "caption" changes kind string → bool`,
			},
		},
		{
			name:    "a pattern added",
			current: "    code: {type: string}\n",
			cand:    "    code: {type: string, pattern: \"^[a-z]+$\"}\n",
			want:    []string{`property "code" changes its pattern to ^[a-z]+$`},
			query:   propValuesQuery,
		},
		{
			name:    "a pattern dropped",
			current: "    code: {type: string, pattern: \"^[a-z]+$\"}\n",
			cand:    "    code: {type: string}\n",
		},
		{
			name:    "a min raised and a max lowered",
			current: "    count: {type: int, min: 0, max: 10}\n",
			cand:    "    count: {type: int, min: 1, max: 5}\n",
			want: []string{
				`property "count" requires values >= 1`,
				`property "count" requires values <= 5`,
			},
		},
		{
			name:    "a min lowered and a max raised",
			current: "    count: {type: int, min: 1, max: 5}\n",
			cand:    "    count: {type: int, min: 0, max: 10}\n",
		},
		{
			name:    "a reference pinned to a kind",
			current: "    part: {type: reference, kind: any}\n",
			cand:    "    part: {type: reference, kind: n.example.com/n/gadget}\n",
			want:    []string{`reference "part" narrows its target to n.example.com/n/gadget`},
		},
		{
			name:    "a reference that must now exist",
			current: "    part: {type: reference, kind: n.example.com/n/gadget}\n",
			cand:    "    part: {type: reference, kind: n.example.com/n/gadget, mustExist: true}\n",
			want:    []string{`reference "part" requires its target to exist`},
			query:   countDanglingRefQuery,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			current, cand := narrowingRegistry(t, c.current), narrowingRegistry(t, c.cand)
			got := classifyNarrowings(current, cand, map[string]bool{narrowPackage: true}, nil)
			if len(got) != len(c.want) {
				t.Fatalf("%d narrowings, want %d:\n%s", len(got), len(c.want), narrowingFormats(got))
			}
			for i, want := range c.want {
				if !strings.Contains(got[i].format, "kind "+narrowKind+": ") || !strings.Contains(got[i].format, want) {
					t.Errorf("narrowing %d = %q, want it to name the kind and say %q", i, got[i].format, want)
				}
			}
			if c.query != "" && got[0].query != c.query {
				t.Errorf("narrowing 0 counts with %q, want %q", got[0].query, c.query)
			}
		})
	}
}

// Classification walks the touched packages' kinds that BOTH registries
// declare: an untouched package is not the batch's, and a dropped kind is
// droppedTypeGuards' whole-kind count.
func TestClassifyNarrowingsReadsOnlyTouchedKindsBothSidesDeclare(t *testing.T) {
	t.Parallel()
	current := narrowingRegistry(t, "    size: {type: string}\n")
	retyped := narrowingRegistry(t, "    size: {type: bool}\n")
	if got := classifyNarrowings(current, retyped, map[string]bool{"other.example.com/o": true}, nil); len(got) != 0 {
		t.Fatalf("a package outside the batch was classified:\n%s", narrowingFormats(got))
	}
	if got := classifyNarrowings(current, retyped, map[string]bool{narrowPackage: true}, nil); len(got) != 1 {
		t.Fatalf("the touched package's retype gave %d narrowings, want 1:\n%s", len(got), narrowingFormats(got))
	}
	dropped, err := vocabulary.LoadFS(fstest.MapFS{"n.yaml": {Data: []byte(`kind: substrate.reamde.dev/core/package
metadata:
  id: n.example.com/n
data:
  authority: n.example.com
  package: n
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: n.example.com/n/gadget
data:
  authority: n.example.com
  package: n
  names:
    singular: gadget
  properties:
    name: {type: string}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := classifyNarrowings(current, dropped, map[string]bool{narrowPackage: true}, nil); len(got) != 0 {
		t.Fatalf("a dropped kind was classified property by property:\n%s", narrowingFormats(got))
	}
}

func narrowingFormats(ns []narrowing) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString("  ")
		b.WriteString(n.format)
		b.WriteString("\n")
	}
	return b.String()
}
