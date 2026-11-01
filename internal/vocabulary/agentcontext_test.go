package vocabulary_test

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// pinnedAgent is the classifier with a reads grant over widgets and the given
// `context:` block.
func pinnedAgent(reads, context string) string {
	return agAuthority(`  description: d
  prompt: p
  provider: default
  model: claude-opus-5
` + reads + context)
}

const widgetReads = "  permissions: {reads: {kinds: [ag.example.com/ag/widget]}}\n"

func TestAgentContextParses(t *testing.T) {
	r, err := loadAgAuthority(t, pinnedAgent(widgetReads, `  context:
    maxBytes: 4096
    pinned:
      - kind: ag.example.com/ag/widget
        filter: '{"properties": {"name": {"eq": "keep"}}}'
        orderBy: createdAt:desc,id
        first: 20
        fields: [name]
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, err := r.ResolveAgent("ag.example.com/ag/classifier")
	if err != nil {
		t.Fatal(err)
	}
	if ag.Context.MaxBytes != 4096 || len(ag.Context.Pinned) != 1 {
		t.Fatalf("context = %+v", ag.Context)
	}
	pin := ag.Context.Pinned[0]
	if pin.Kind != "ag.example.com/ag/widget" || pin.First != 20 || strings.Join(pin.Fields, ",") != "name" {
		t.Fatalf("pin = %+v", pin)
	}
	want := []substrate.Order{{Property: "createdAt", Desc: true}, {Property: "id"}}
	if len(pin.OrderBy) != 2 || pin.OrderBy[0] != want[0] || pin.OrderBy[1] != want[1] {
		t.Fatalf("orderBy = %+v", pin.OrderBy)
	}
	if c, ok := pin.Filter.Properties["name"]; !ok || c.Eq != "keep" {
		t.Fatalf("filter = %+v", pin.Filter)
	}
}

func TestAgentContextDefaults(t *testing.T) {
	r, err := loadAgAuthority(t, pinnedAgent(widgetReads, `  context:
    pinned:
      - {kind: ag.example.com/ag/widget, orderBy: id}
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, _ := r.ResolveAgent("ag.example.com/ag/classifier")
	if ag.Context.MaxBytes != vocabulary.DefaultAgentPinnedBytes || ag.Context.Pinned[0].First != vocabulary.DefaultAgentPinnedFirst {
		t.Fatalf("defaults = %+v", ag.Context)
	}
	// No context at all pins nothing.
	r, err = loadAgAuthority(t, pinnedAgent(widgetReads, ""))
	if err != nil {
		t.Fatal(err)
	}
	ag, _ = r.ResolveAgent("ag.example.com/ag/classifier")
	if len(ag.Context.Pinned) != 0 {
		t.Fatalf("context = %+v", ag.Context)
	}
}

func TestAgentContextRefusals(t *testing.T) {
	pin := func(entry string) string {
		return "  context:\n    pinned:\n      - " + entry + "\n"
	}
	cases := []struct {
		name, reads, context, want string
	}{
		{"no reads grant", "", pin("{kind: ag.example.com/ag/widget, orderBy: id}"), "needs data.permissions.reads"},
		{
			"kind outside the grant", "  permissions: {reads: {kinds: [ag.example.com/ag/other]}}\n",
			pin("{kind: ag.example.com/ag/widget, orderBy: id}"), "is not in data.permissions.reads.kinds",
		},
		{"no orderBy", widgetReads, pin("{kind: ag.example.com/ag/widget}"), "orderBy is required"},
		{"a bad direction", widgetReads, pin("{kind: ag.example.com/ag/widget, orderBy: 'id:up'}"), "direction must be asc or desc"},
		{"no kind", widgetReads, pin("{orderBy: id}"), "kind is required"},
		{"a glob", widgetReads, pin("{kind: 'ag.example.com/ag/*', orderBy: id}"), "no globs"},
		{"filter.kinds", widgetReads, pin(`{kind: ag.example.com/ag/widget, orderBy: id, filter: '{"kinds": ["x"]}'}`), "filter.kinds"},
		{"an unknown filter key", widgetReads, pin(`{kind: ag.example.com/ag/widget, orderBy: id, filter: '{"active": true}'}`), "filter:"},
		{"first above the cap", widgetReads, pin("{kind: ag.example.com/ag/widget, orderBy: id, first: 501}"), "between 1 and 500"},
		{"an undeclared field", widgetReads, pin("{kind: ag.example.com/ag/widget, orderBy: id, fields: [shade]}"), `declares no property "shade"`},
		{"a field twice", widgetReads, pin("{kind: ag.example.com/ag/widget, orderBy: id, fields: [name, name]}"), "named once"},
		{"an unknown entry key", widgetReads, pin("{kind: ag.example.com/ag/widget, orderBy: id, limit: 3}"), `unknown key "limit"`},
		{"maxBytes above the cap", widgetReads, "  context: {maxBytes: 70000}\n", "between 1024 and 65536"},
		{"an unknown context key", widgetReads, "  context: {pins: []}\n", `unknown key "pins"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadAgAuthority(t, pinnedAgent(tc.reads, tc.context))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// The same grant glob that reaches a kind for the query tool reaches it for
// a pinned read: no new permission.
func TestAgentContextGlobGrant(t *testing.T) {
	_, err := loadAgAuthority(t, pinnedAgent("  permissions: {reads: {kinds: ['ag.example.com/ag/*']}}\n",
		"  context: {pinned: [{kind: ag.example.com/ag/widget, orderBy: id}]}\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
}
