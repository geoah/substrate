package engine

// A `values:` list declared where none was is a narrowing like a value
// removed: the write path holds every string to it from then on. The counts
// run in valuesadded_db_test.go.

import (
	"strings"
	"testing"
)

func TestClassifyValuesAdded(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		current, cand string
		want          string
	}{
		{
			name:    "on a property",
			current: "    size: {type: string}\n",
			cand:    "    size: {type: string, values: [small, large]}\n",
			want:    `property "size" adds the allowed values "small", "large"`,
		},
		{
			name:    "on a repeated property",
			current: "    sizes: {type: string, repeated: true}\n",
			cand:    "    sizes: {type: string, repeated: true, values: [small]}\n",
			want:    `property "sizes" adds the allowed values "small"`,
		},
		{
			name:    "on an object field",
			current: "    look:\n      type: object\n      fields:\n        tone: {type: string}\n",
			cand:    "    look:\n      type: object\n      fields:\n        tone: {type: string, values: [warm]}\n",
			want:    `object "look" field "tone" adds the allowed values "warm"`,
		},
		{
			name:    "on a link property",
			current: "    maker:\n      type: reference\n      kind: n.example.com/n/gadget\n      properties:\n        role: {type: string}\n",
			cand:    "    maker:\n      type: reference\n      kind: n.example.com/n/gadget\n      properties:\n        role: {type: string, values: [{value: lead}]}\n",
			want:    `reference "maker" adds the allowed values "lead" to link property "role"`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := classifyNarrowings(narrowingRegistry(t, c.current), narrowingRegistry(t, c.cand), map[string]bool{narrowPackage: true}, nil)
			if len(got) != 1 || !strings.Contains(got[0].format, c.want) {
				t.Fatalf("narrowings:\n%s\nwant one saying %q", narrowingFormats(got), c.want)
			}
		})
	}
	// Values added to a set that already had some narrow nothing, and so does
	// a set on a datatype no write is held to.
	for _, c := range [][2]string{
		{"    size: {type: string, values: [small]}\n", "    size: {type: string, values: [small, large]}\n"},
		{"    seen: {type: datetime}\n", "    seen: {type: datetime, values: [soon]}\n"},
		{"    blob: {type: json}\n", "    blob: {type: json, values: [soon]}\n"},
	} {
		if got := classifyNarrowings(narrowingRegistry(t, c[0]), narrowingRegistry(t, c[1]),
			map[string]bool{narrowPackage: true}, nil); len(got) != 0 {
			t.Fatalf("%s narrowed:\n%s", c[1], narrowingFormats(got))
		}
	}
}
