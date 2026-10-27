package catalog_test

// A first-party catalogue's record views (decision records 0166 and 0192)
// through the provider door: a view lands at its shipped id, naming the
// published kind and the metric records the catalogue ships, because a
// published package is installed verbatim; and a re-install writes a shipped
// view only where no hand but its shipper wrote or deleted the one at its id
// (record 0191), so the owner's edit and the owner's deletion both survive.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/kinds"
)

const (
	recordViewKind      = "substrate.reamde.dev/console/recordview"
	kindDeclarationPath = "substrate.reamde.dev/core/kind/"
)

func appliesTo(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	return refPath(view.Properties["appliesTo"])
}

func refPath(v any) string {
	ref, _ := v.(map[string]any)
	path, _ := ref["ref"].(string)
	return path
}

// viewMetrics lists every metric reference a view's chart and spans blocks
// hold, as stored record paths, in section order.
func viewMetrics(t *testing.T, ds substrate.Dataset, id string) []string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	var out []string
	sections, _ := view.Properties["sections"].([]any)
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		if chart, ok := section["chart"].(map[string]any); ok {
			metrics, _ := chart["metrics"].([]any)
			for _, m := range metrics {
				out = append(out, refPath(m))
			}
		}
		if spans, ok := section["spans"].(map[string]any); ok {
			out = append(out, refPath(spans["metric"]))
		}
	}
	return out
}

// healthView and labsView are the ids a first-party catalogue's views land
// at: the shipped ids, the same in every repository.
func healthView(name string) string { return healthID + "/" + name }

func labsView(name string) string { return labsID + "/" + name }

func viewName(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	name, _ := view.Properties["name"].(string)
	return name
}

func viewIntro(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	intro, _ := view.Properties["intro"].(string)
	return intro
}

// A first-party catalogue's views (record 0192) land as shipped: at the
// shipped id, applying to the published kind, and reading the metric records
// the catalogue writes into the seeded metric kind. Nothing is rehomed, so
// every repository holds them under the same names.
func TestInstalledRecordViewsLandVerbatim(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	importVocabulary(t, c, ds, healthID)

	for _, kind := range []string{"sleep", "workout"} {
		want := kindDeclarationPath + healthID + "/" + kind
		if got := appliesTo(t, ds, healthView(kind)); got != want {
			t.Errorf("the %s view applies to %q, want %q", kind, got, want)
		}
		metrics := viewMetrics(t, ds, healthView(kind))
		if len(metrics) == 0 {
			t.Errorf("the %s view names no metric", kind)
		}
		for _, m := range metrics {
			if !strings.HasPrefix(m, metricKind+"/health/") {
				t.Errorf("the %s view reads %q, want a health metric record of %s", kind, m, metricKind)
			}
		}
		// The console finds a kind's views by this one filter.
		page, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
			Kinds:      []string{recordViewKind},
			Properties: map[string]substrate.Cond{"appliesTo": {Eq: want}},
		}})
		if err != nil {
			t.Fatalf("list the views of %s: %v", kind, err)
		}
		if len(page.Records) != 1 || page.Records[0].ID != healthView(kind) {
			t.Errorf("the views of %s are %d records, want the one %s", kind, len(page.Records), healthView(kind))
		}
	}
}

func TestReinstallKeepsAnEditedRecordView(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importVocabulary(t, c, ds, healthID)

	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, healthView("workout"), substrate.PatchInput{
		Properties: map[string]any{"name": "My runs"},
	}); err != nil {
		t.Fatalf("edit the workout view: %v", err)
	}
	importVocabulary(t, c, ds, healthID)
	if got := viewName(t, ds, healthView("workout")); got != "My runs" {
		t.Fatalf("a re-install overwrote the owner's view: name = %q", got)
	}

	// A deletion is the owner's too: the re-install does not bring the view back.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, recordViewKind, healthView("workout"), substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the workout view: %v", err)
	}
	importVocabulary(t, c, ds, healthID)
	if view, err := ds.Get(ctx, recordViewKind, healthView("workout")); err == nil && view.DeletedAt == nil {
		t.Fatalf("a re-install recreated the view the owner deleted: name = %q", view.Properties["name"])
	}
	// The view the owner never touched is still the shipped one.
	if got := viewName(t, ds, healthView("sleep")); got == "" {
		t.Fatal("the untouched sleep view is gone after the re-install")
	}
}

// Each catalogue's views land under its own package, the lab trend view on
// the seeded metric kind included: installing labs brings health first, and
// the two hold five views between them, each at the id its package ships.
func TestFirstPartyRecordViewsLandUnderTheirOwnPackages(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	importVocabulary(t, c, ds, labsID)

	want := map[string]string{
		healthView("sleep"):   kindDeclarationPath + healthID + "/sleep",
		healthView("workout"): kindDeclarationPath + healthID + "/workout",
		labsView("labreport"): kindDeclarationPath + labsID + "/labreport",
		labsView("labresult"): kindDeclarationPath + labsID + "/labresult",
		labsView("metric"):    kindDeclarationPath + metricKind,
	}
	page, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds: []string{recordViewKind},
	}})
	if err != nil {
		t.Fatalf("list the views: %v", err)
	}
	got := map[string]string{}
	for _, r := range page.Records {
		got[r.ID] = refPath(r.Properties["appliesTo"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the views landed as %v, want %v", got, want)
	}
}

// applyShippedViews is the reset docs/bundles.md gives an owner of a
// first-party catalogue: apply every document in the shipped views.yaml as it
// stands, as `substratectl apply -f` does, one put per document. Nothing is
// rewritten, because the installed views carry the shipped ids.
func applyShippedViews(t *testing.T, ds substrate.Dataset, pkg string) {
	t.Helper()
	raw, err := fs.ReadFile(kinds.Bundles(), pkg+"/views.yaml")
	if err != nil {
		t.Fatalf("read the shipped views: %v", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				ID string `yaml:"id"`
			} `yaml:"metadata"`
			Data struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"data"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			t.Fatalf("decode the shipped views: %v", err)
		}
		if _, err := ds.Put(context.Background(), substrate.ActorCLI, substrate.PutInput{
			Kind: doc.Kind, ID: doc.Metadata.ID, Properties: doc.Data.Properties,
		}); err != nil {
			t.Fatalf("apply the shipped view %s: %v", doc.Metadata.ID, err)
		}
	}
}

// The owner of a first-party catalogue takes a newer shipped view over an
// edited one by applying the shipped file: the views landed at the shipped
// ids, so the file needs no rewrite.
func TestShippedRecordViewAppliedResetsAnEditedOne(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importVocabulary(t, c, ds, labsID)

	shipped, err := ds.Get(ctx, recordViewKind, healthView("sleep"))
	if err != nil {
		t.Fatalf("read the imported sleep view: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, healthView("sleep"), substrate.PatchInput{
		Properties: map[string]any{"name": "My nights", "fold": []any{"score"}},
	}); err != nil {
		t.Fatalf("edit the sleep view: %v", err)
	}

	// The lab report view ships no intro, so the owner's stands for a
	// property the shipped file does not set.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, labsView("labreport"), substrate.PatchInput{
		Properties: map[string]any{"intro": "My notes"},
	}); err != nil {
		t.Fatalf("edit the lab report view: %v", err)
	}

	// An apply merges: every property the shipped view sets comes back, and
	// one the owner added and the shipped view does not set stays.
	applyShippedViews(t, ds, healthID)
	applyShippedViews(t, ds, labsID)
	merged, err := ds.Get(ctx, recordViewKind, healthView("sleep"))
	if err != nil {
		t.Fatalf("read the sleep view: %v", err)
	}
	if got := merged.Properties["name"]; got != shipped.Properties["name"] {
		t.Errorf("the applied view is named %q, want the shipped %q", got, shipped.Properties["name"])
	}
	if !reflect.DeepEqual(merged.Properties["sections"], shipped.Properties["sections"]) {
		t.Errorf("the applied view's sections differ from the shipped ones")
	}
	if !reflect.DeepEqual(merged.Properties["fold"], shipped.Properties["fold"]) {
		t.Errorf("the applied view folds %v, want the shipped %v", merged.Properties["fold"], shipped.Properties["fold"])
	}
	if got := viewIntro(t, ds, labsView("labreport")); got != "My notes" {
		t.Errorf("the apply pruned the owner's intro (%q), but an apply never prunes", got)
	}

	// Purging the view first makes the apply land the shipped view whole: a
	// put at a tombstone restores it with its old properties, a put after a
	// purge creates it fresh.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, recordViewKind, healthView("sleep"), substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("delete the sleep view: %v", err)
	}
	applyShippedViews(t, ds, healthID)
	applyShippedViews(t, ds, labsID)
	reset, err := ds.Get(ctx, recordViewKind, healthView("sleep"))
	if err != nil {
		t.Fatalf("read the reset sleep view: %v", err)
	}
	if reset.DeletedAt != nil {
		t.Fatal("the apply left the sleep view deleted")
	}
	if !reflect.DeepEqual(reset.Properties, shipped.Properties) {
		t.Errorf("the reset view is %v, want the shipped %v", reset.Properties, shipped.Properties)
	}
}
