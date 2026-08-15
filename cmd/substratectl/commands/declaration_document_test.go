package commands

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// declarationRow is one kind declaration as the collection API hands it back:
// a record whose property map holds the authored keys, PLUS what the engine
// stamps (`source`) and what the record projection derives (`title`).
func declarationRow() *substrate.Record {
	at := time.Unix(1_700_000_000, 0).UTC()
	return &substrate.Record{
		ID:        "mine.example.com/widget",
		Kind:      "core.substrate.reamde.dev/kind",
		Title:     "widget",
		Version:   1,
		CreatedAt: at,
		UpdatedAt: at,
		Properties: map[string]any{
			"authority":       "mine.example.com",
			"version":         "v1alpha1",
			"displayTemplate": "{label}",
			"names":           map[string]any{"singular": "widget", "plural": "widgets"},
			"properties": map[string]any{
				"label": map[string]any{"type": "string", "description": "what the widget is called"},
			},
			// Not authored: the engine's origin stamp, and the display title
			// the projection injects into every record's property map.
			"source": "installed",
			"title":  "widget",
		},
	}
}

// A declaration reads back in the shape its AUTHOR writes: `data` holds the
// declaration's own keys, not a `data.properties` map holding them. Rendering
// it like an ordinary record nests everything one level too deep, and
// `apply -f` of that output is refused for a missing `data.authority` — which
// is a read whose output the writer will not take.
func TestDeclarationRendersInItsAuthoredShape(t *testing.T) {
	b, err := marshalDocument(documentOf(declarationRow(), nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Kind     string              `yaml:"kind"`
		Metadata struct{ ID string } `yaml:"metadata"`
		Data     map[string]any      `yaml:"data"`
	}
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("parse: %v\n%s", err, b)
	}
	if got.Kind != "core.substrate.reamde.dev/kind" || got.Metadata.ID != "mine.example.com/widget" {
		t.Fatalf("envelope: %+v\n%s", got, b)
	}
	// The authored keys sit directly under data.
	if got.Data["authority"] != "mine.example.com" {
		t.Fatalf("data.authority is not directly under data:\n%s", b)
	}
	// ...and NOT nested under a data.properties holding the whole declaration.
	// `properties` is present, but it is the declaration's own property block:
	// it holds `label`, not `authority`.
	props, _ := got.Data["properties"].(map[string]any)
	if props == nil || props["label"] == nil {
		t.Fatalf("data.properties is not the declaration's property block:\n%s", b)
	}
	if props["authority"] != nil {
		t.Fatalf("the declaration is nested one level too deep:\n%s", b)
	}
}

// The rendered `data` is a WHITELIST — vocabulary.DeclarationDataKeys — not
// the property map with today's known stamps subtracted. Every key the engine
// adds is refused by /vocabulary/apply as unknown, so a subtractive filter
// would let the NEXT stamped property break the round trip silently.
func TestDeclarationDropsEveryKeyTheWriterRefuses(t *testing.T) {
	e := declarationRow()
	// A property no binary stamps today. A whitelist drops it; a denylist of
	// {source, title} would ship it and break `apply`.
	e.Properties["quarantinedAt"] = "2026-08-15T00:00:00Z"

	b, err := marshalDocument(documentOf(e, nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, refused := range []string{"source:", "title:", "quarantinedAt:"} {
		if strings.Contains(string(b), refused) {
			t.Errorf("rendered a key /vocabulary/apply refuses (%s):\n%s", refused, b)
		}
	}
	for _, want := range []string{"authority:", "version:", "displayTemplate:", "names:"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("dropped an authored key (%s):\n%s", want, b)
		}
	}
}

// An ordinary record is untouched by any of this: its properties stay under
// `data.properties`, which is where they belong and where `apply` reads them.
func TestOrdinaryRecordStillNestsItsProperties(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	e := &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person",
		Version: 1, CreatedAt: at, UpdatedAt: at,
		Properties: map[string]any{"name": "Ada"},
	}
	if _, ok := documentOf(e, nil).(*document); !ok {
		t.Fatalf("a data record rendered as a declaration")
	}
	b, err := marshalDocument(documentOf(e, nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "  properties:\n    name: Ada") {
		t.Fatalf("a data record's properties moved:\n%s", b)
	}
}

// Every core meta-kind renders through the declaration path, not just `kind`.
// The set is the one `apply` routes on, so a meta-kind that rendered as an
// ordinary record would be a read the writer refuses.
func TestEveryMetaKindTakesTheDeclarationPath(t *testing.T) {
	for _, short := range []string{
		"kind", "authority", "trait", "propertytype",
		"function", "agent", "bundle", "recordmapping", "actor",
	} {
		ref := "core.substrate.reamde.dev/" + short
		if _, ok := declarationKindOf(ref); !ok {
			t.Errorf("%s is not recognized as a declaration", ref)
		}
	}
	for _, ref := range []string{
		"people.substrate.reamde.dev/person",
		"core.substrate.reamde.dev/token",
		"core.substrate.reamde.dev/run",
		"task",
	} {
		if _, ok := declarationKindOf(ref); ok {
			t.Errorf("%s is not a declaration but took the declaration path", ref)
		}
	}
}
