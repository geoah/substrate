package catalog_test

// A sample's record views (decision record 0166) through the import door: a
// view lands at an id under the repository's own authority, naming the
// repository's own kind and metric records, because the import rehomes its
// id, its `appliesTo` and its metric references with everything else; and a
// re-import writes a shipped view only where the owner never wrote or deleted
// the one at its id, so the owner's edit and the owner's deletion both
// survive.

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
	"github.com/geoah/substrate/samples"
)

const (
	recordViewKind      = "substrate.reamde.dev/console/recordview"
	observationsSample  = samplesPlacehldr + "/observations"
	healthSample        = samplesPlacehldr + "/health"
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

// homeView is the id a health view lands at under the repository's authority.
func homeView(name string) string { return homeAuthority + "/health/" + name }

func viewName(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	view, err := ds.Get(context.Background(), recordViewKind, id)
	if err != nil {
		t.Fatalf("read the %s view: %v", id, err)
	}
	name, _ := view.Properties["name"].(string)
	return name
}

func TestImportedRecordViewsFollowTheRehome(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	importSamples(t, c, ds, observationsSample, healthSample)

	for _, kind := range []string{"sleep", "workout"} {
		want := kindDeclarationPath + homeAuthority + "/health/" + kind
		if got := appliesTo(t, ds, homeView(kind)); got != want {
			t.Errorf("the %s view applies to %q, want %q", kind, got, want)
		}
		metrics := viewMetrics(t, ds, homeView(kind))
		if len(metrics) == 0 {
			t.Errorf("the %s view names no metric", kind)
		}
		for _, m := range metrics {
			if !strings.HasPrefix(m, homeAuthority+"/observations/metric/health/") {
				t.Errorf("the %s view reads %q, want a metric record of %s/observations/metric", kind, m, homeAuthority)
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
		if len(page.Records) != 1 || page.Records[0].ID != homeView(kind) {
			t.Errorf("the views of %s are %d records, want the one %s", kind, len(page.Records), homeView(kind))
		}
	}
}

func TestReimportKeepsAnEditedRecordView(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, observationsSample, healthSample)

	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, homeView("workout"), substrate.PatchInput{
		Properties: map[string]any{"name": "My runs"},
	}); err != nil {
		t.Fatalf("edit the workout view: %v", err)
	}
	importSamples(t, c, ds, healthSample)
	if got := viewName(t, ds, homeView("workout")); got != "My runs" {
		t.Fatalf("a re-import overwrote the owner's view: name = %q", got)
	}

	// A deletion is the owner's too: the re-import does not bring the view back.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, recordViewKind, homeView("workout"), substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the workout view: %v", err)
	}
	importSamples(t, c, ds, healthSample)
	if view, err := ds.Get(ctx, recordViewKind, homeView("workout")); err == nil && view.DeletedAt == nil {
		t.Fatalf("a re-import recreated the view the owner deleted: name = %q", view.Properties["name"])
	}
	// The view the owner never touched is still the shipped one.
	if got := viewName(t, ds, homeView("sleep")); got == "" {
		t.Fatal("the untouched sleep view is gone after the re-import")
	}
}

// A sample installed verbatim and the same sample imported are two copies,
// and their views stay two views: each lands at an id under its own
// authority, applies to its own copy of the kind, and reads its own copy's
// metric records, although both metric kinds hold a `health/heart_rate`.
func TestInstalledAndImportedRecordViewsStayApart(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	importVocabulary(t, c, ds, observationsSample, healthSample)
	importSamples(t, c, ds, observationsSample, healthSample)

	for _, copyOf := range []string{samplesPlacehldr, homeAuthority} {
		for _, kind := range []string{"sleep", "workout"} {
			id := copyOf + "/health/" + kind
			if got, want := appliesTo(t, ds, id), kindDeclarationPath+copyOf+"/health/"+kind; got != want {
				t.Errorf("the view %s applies to %q, want %q", id, got, want)
			}
			metrics := viewMetrics(t, ds, id)
			if len(metrics) == 0 {
				t.Errorf("the view %s names no metric", id)
			}
			for _, m := range metrics {
				if !strings.HasPrefix(m, copyOf+"/observations/metric/health/") {
					t.Errorf("the view %s reads %q, a metric outside its own copy", id, m)
				}
			}
		}
	}
	page, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds: []string{recordViewKind},
	}})
	if err != nil {
		t.Fatalf("list the views: %v", err)
	}
	if len(page.Records) != 4 {
		ids := make([]string, 0, len(page.Records))
		for _, r := range page.Records {
			ids = append(ids, r.ID)
		}
		t.Errorf("the two copies hold views %v, want four", ids)
	}
}

// applyShippedViews is the reset docs/bundles.md gives an owner of an imported
// health sample: rewrite the sample's authority to the repository's own in the
// shipped views.yaml (the `sed` step) and apply every document in it, as
// `substratectl apply -f -` does, one put per document.
func applyShippedViews(t *testing.T, ds substrate.Dataset, authority string) {
	t.Helper()
	raw, err := fs.ReadFile(samples.Samples(), "health/views.yaml")
	if err != nil {
		t.Fatalf("read the shipped views: %v", err)
	}
	if authority != samplesPlacehldr {
		raw = bytes.ReplaceAll(raw, []byte(samplesPlacehldr), []byte(authority))
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

// The owner of an imported sample takes a newer shipped view over an edited
// one by applying the shipped file rehomed onto the repository's authority.
// The file as shipped names the sample's authority in every id and reference,
// so applied unchanged it would write a view of a copy this repository does
// not hold and leave the edited one alone.
func TestShippedRecordViewAppliedRehomedResetsAnEditedOne(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importSamples(t, c, ds, observationsSample, healthSample)

	shipped, err := ds.Get(ctx, recordViewKind, homeView("sleep"))
	if err != nil {
		t.Fatalf("read the imported sleep view: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, homeView("sleep"), substrate.PatchInput{
		Properties: map[string]any{"name": "My nights", "fold": []any{"score"}},
	}); err != nil {
		t.Fatalf("edit the sleep view: %v", err)
	}

	// An apply merges: every property the shipped view sets comes back, and
	// one the owner added and the shipped view does not set stays.
	applyShippedViews(t, ds, homeAuthority)
	merged, err := ds.Get(ctx, recordViewKind, homeView("sleep"))
	if err != nil {
		t.Fatalf("read the sleep view: %v", err)
	}
	if got := merged.Properties["name"]; got != shipped.Properties["name"] {
		t.Errorf("the applied view is named %q, want the shipped %q", got, shipped.Properties["name"])
	}
	if !reflect.DeepEqual(merged.Properties["sections"], shipped.Properties["sections"]) {
		t.Errorf("the applied view's sections differ from the shipped ones")
	}
	if merged.Properties["fold"] == nil {
		t.Errorf("the apply pruned the owner's fold, but an apply never prunes")
	}

	// Purging the view first makes the apply land the shipped view whole: a
	// put at a tombstone restores it with its old properties, a put after a
	// purge creates it fresh.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, recordViewKind, homeView("sleep"), substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("delete the sleep view: %v", err)
	}
	applyShippedViews(t, ds, homeAuthority)
	reset, err := ds.Get(ctx, recordViewKind, homeView("sleep"))
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
