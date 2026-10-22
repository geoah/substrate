package engine_test

// Repository migration 3 (repomigration_0003_name_units_by_unit_records.go)
// over a repository as the binary before decision 0186 left it: core at the
// version `main` shipped before the unit graph (testdata/preunits/kinds), and
// the observations and health samples as `main` shipped them
// (testdata/preunits/samples), imported under the repository's own
// authority, with metric records and observations written. The next binary
// opens it, migrates it, upgrades core, seeds the units and reads the same
// values.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

const preunitsDir = "testdata/preunits"

// preunitsTree is the shipped tree as the binary before the unit graph
// shipped it: today's tree with core's header and propertytype from then,
// and no unit kinds.
func preunitsTree(t *testing.T) string {
	t.Helper()
	tree := shippedTree(t)
	core := filepath.Join(tree, "substrate.reamde.dev", "core")
	for _, name := range []string{"core.yaml", "propertytype.yaml"} {
		raw, err := os.ReadFile(filepath.Join(preunitsDir, "kinds", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(core, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(core, "unit.yaml")); err != nil {
		t.Fatal(err)
	}
	return tree
}

var reMeasuresLine = regexp.MustCompile(`(?m)^ +measures: [a-z-]+\n`)

// preunitsSamples copies the historical samples into a catalog root this
// binary's loader admits: the `measures:` of a kind's quantity property is a
// key it refuses, so those lines go, and preunitsMeasures says where they
// were so the stored rows can be put back as they were imported.
func preunitsSamples(t *testing.T) (string, map[string]map[string]string) {
	t.Helper()
	root := t.TempDir()
	measures := map[string]map[string]string{}
	for _, pkg := range []string{"observations", "health"} {
		dir := filepath.Join(preunitsDir, "samples", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, pkg), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for id, props := range kindMeasures(t, raw) {
				measures[id] = props
				raw = reMeasuresLine.ReplaceAll(raw, nil)
			}
			if err := os.WriteFile(filepath.Join(root, pkg, e.Name()), raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, measures
}

// kindMeasures reads, from one file's kind documents, each quantity
// property's `measures`, by kind id.
func kindMeasures(t *testing.T, raw []byte) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
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
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "substrate.reamde.dev/core/kind" {
			continue
		}
		for name, raw := range doc.Data.Properties {
			p, _ := raw.(map[string]any)
			if m, ok := p["measures"].(string); ok && p["type"] == "quantity" {
				if out[doc.Metadata.ID] == nil {
					out[doc.Metadata.ID] = map[string]string{}
				}
				out[doc.Metadata.ID][name] = m
			}
		}
	}
}

// rehome is a shipped sample id under the repository's own authority.
func rehome(repo, id string) string {
	return repo + strings.TrimPrefix(id, "samples.substrate.reamde.dev")
}

type preunitsReading struct {
	id, metric, value, unit, measure string
}

func TestRepositoryMigrationMovesPreGraphImportsOntoUnitRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := engine.MigratedDSN(t), t.TempDir()
	repo := testdb.Repository(t)
	observation := repo + "/observations/observation"
	metric := repo + "/observations/metric"

	// Binary N: core before the unit graph, the table path, the samples as
	// main imported them.
	svc := openTree(t, dsn, preunitsTree(t), engine.WithDataRoot(root), engine.WithUnitGraph(false))
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create on binary N: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.KindByRef(ctx, vocabulary.KindUnit); err == nil {
		t.Fatal("binary N declares the unit kind; the migration would prove nothing")
	}
	samplesRoot, measures := preunitsSamples(t)
	c, err := catalog.Load(catalog.SampleRoot(os.DirFS(samplesRoot)))
	if err != nil {
		t.Fatalf("load the historical samples: %v", err)
	}
	for _, id := range []string{"samples.substrate.reamde.dev/observations", "samples.substrate.reamde.dev/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("import %s on binary N: %v", id, err)
		}
	}
	// The kinds back as main stored them, `measures` and all, and each copy
	// stamped as the unedited import it was.
	for id, props := range measures {
		kind := rehome(repo, id)
		row := mustGet(t, ds, rmKind, kind).Properties
		declared := row["properties"].(map[string]any)
		for name, m := range props {
			declared[name].(map[string]any)["measures"] = m
		}
		if err := planter(t, ds).PlantDeclarationRow(ctx, rmKind, kind, row); err != nil {
			t.Fatalf("plant %s: %v", kind, err)
		}
	}
	for _, pkg := range []string{repo + "/observations", repo + "/health"} {
		if err := ds.(interface {
			StampOriginDigest(context.Context, string) error
		}).StampOriginDigest(ctx, pkg); err != nil {
			t.Fatalf("stamp %s: %v", pkg, err)
		}
	}
	// Two metrics of the owner's own: one in a unit no shipped unit answers
	// but whose spelling is a valid id, one whose spelling is neither.
	for _, m := range []struct{ id, unit, measure string }{{"test/flow", "kg/h", "mass-flow"}, {"test/odd", "{foo}", "count"}} {
		mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: metric, ID: m.id, Properties: map[string]any{"name": m.id, "form": "gauge"}})
		row := mustGet(t, ds, metric, m.id).Properties
		row["unit"], row["measures"] = m.unit, m.measure
		if err := planter(t, ds).PlantDeclarationRow(ctx, metric, m.id, row); err != nil {
			t.Fatalf("plant %s: %v", m.id, err)
		}
	}
	readings := []preunitsReading{
		{"w1", "health/body_mass", "72.40", "kg", "mass"},
		{"w2", "health/body_mass", "160", "[lb_av]", "mass"},
		{"o1", "health/spo2", "97", "%", "fraction"},
		{"h1", "health/resting_heart_rate", "52", "/min", "rate"},
	}
	for _, r := range readings {
		mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: observation, ID: r.id, Properties: map[string]any{
			"metric": map[string]any{"ref": metric + "/" + r.metric}, "form": "gauge", "hold": "PT0S",
			"value": map[string]any{"value": r.value, "unit": r.unit},
			"at":    "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": 1,
		}})
		row := mustGet(t, ds, observation, r.id).Properties
		row["measure"] = r.measure
		if err := planter(t, ds).PlantDeclarationRow(ctx, observation, r.id, row); err != nil {
			t.Fatalf("plant %s: %v", r.id, err)
		}
	}
	if got := mustGet(t, ds, metric, "health/body_mass").Properties; got["unit"] != "kg" || got["measures"] != "mass" {
		t.Fatalf("binary N's body_mass metric = unit %v, measures %v; want the strings main stored", got["unit"], got["measures"])
	}
	// Binary N stands for a ledger that never ran this migration.
	rmClearLedger(t, dsn, repo)
	_ = svc.Close()

	// Binary N+1.
	svc = openTree(t, dsn, shippedTree(t), engine.WithDataRoot(root), engine.WithUnitSeed(true))
	ds, err = svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	if _, err := ds.KindByRef(ctx, vocabulary.KindUnit); err != nil {
		t.Fatalf("core was not upgraded to declare the unit kind: %v", err)
	}
	tab := unitsOf(t, ds)
	units, convs := shippedCounts()
	if len(tab.Units) != units+1 || len(tab.Conversions) != convs || len(tab.Unseeded) != 0 {
		t.Errorf("the open holds %d units and %d conversions, skipped %v; want the %d shipped units and kg/h, and %d conversions",
			len(tab.Units), len(tab.Conversions), tab.Unseeded, units, convs)
	}
	statuses, err := ds.BundleStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{repo + "/observations", repo + "/health"} {
		var found bool
		for _, st := range statuses {
			if st.ID != pkg {
				continue
			}
			found = true
			if st.Quarantined || st.Modified {
				t.Errorf("%s: quarantined %v (%s), modified %v; want the copy admitted and unedited", pkg, st.Quarantined, st.QuarantineReason, st.Modified)
			}
		}
		if !found {
			t.Errorf("no bundle status for %s", pkg)
		}
	}

	metricKind, err := ds.KindByRef(ctx, metric)
	if err != nil {
		t.Fatal(err)
	}
	mp := metricKind.Definition["properties"].(map[string]any)
	if u := mp["unit"].(map[string]any); u["type"] != "reference" || u["kind"] != vocabulary.KindUnit {
		t.Errorf("the metric kind's unit = %v, want a reference to the unit kind", u)
	}
	if _, held := mp["measures"]; held {
		t.Error("the metric kind still declares measures")
	}
	sleep, err := ds.KindByRef(ctx, repo+"/health/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if eff := sleep.Definition["properties"].(map[string]any)["efficiency"].(map[string]any); eff["unit"] != "percent" || eff["measures"] != nil {
		t.Errorf("sleep efficiency = %v, want unit percent and no measures", eff)
	}
	obsKind, err := ds.KindByRef(ctx, observation)
	if err != nil {
		t.Fatal(err)
	}
	if _, held := obsKind.Definition["properties"].(map[string]any)["measure"]; held {
		t.Error("the observation kind still declares measure")
	}

	unitRef := func(id string) map[string]any { return map[string]any{"ref": vocabulary.KindUnit + "/" + id} }
	for id, want := range map[string]any{
		"health/body_mass":          unitRef("kg"),
		"health/spo2":               unitRef("percent"),
		"health/resting_heart_rate": unitRef("1/min"),
		"test/flow":                 unitRef("kg/h"),
		"test/odd":                  "{foo}",
	} {
		got := mustGet(t, ds, metric, id).Properties
		if !jsonSame(got["unit"], want) || got["measures"] != nil {
			t.Errorf("metric %s: unit %v, measures %v; want unit %v and no measures", id, got["unit"], got["measures"], want)
		}
	}
	// The metric left as written refuses a write until the owner points it
	// at a unit record.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, metric, "test/odd", substrate.PatchInput{Properties: map[string]any{"name": "odd"}}); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a write to the metric left with {foo}: err = %v, want a validation refusal", err)
	}
	if u, ok := unitByID(tab, "kg/h"); !ok || u.Name != "kg/h" {
		t.Errorf("the unit kg/h = %+v, %v; want one written for the owner's metric", u, ok)
	}
	for _, r := range readings {
		got := mustGet(t, ds, observation, r.id).Properties
		v, _ := got["value"].(map[string]any)
		if v["value"] != r.value || v["unit"] != r.unit || got["measure"] != nil {
			t.Errorf("observation %s = value %v, measure %v; want %s %s and no measure", r.id, got["value"], got["measure"], r.value, r.unit)
		}
	}
	engine.DrainIndexReprojection(t, ds)
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{observation}, Properties: map[string]substrate.Cond{
		"value": {Gt: map[string]any{"value": "70", "unit": "kg"}},
	}}, OrderBy: []substrate.Order{{Property: "createdAt"}}})
	if err != nil {
		t.Fatalf("a quantity filter over the migrated observations: %v", err)
	}
	var ids []string
	for _, r := range page.Records {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "w1,w2" {
		t.Errorf("observations above 70 kg = %s, want w1,w2 (72.40 kg and 160 lb)", got)
	}
	// A new value is admitted by its metric through the graph.
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: observation, ID: "w3", Properties: map[string]any{
		"metric": map[string]any{"ref": metric + "/health/body_mass"}, "form": "gauge", "hold": "PT0S",
		"value": map[string]any{"value": "71", "unit": "kg"},
		"at":    "2026-10-08T06:41:12+03:00", "utcOffset": 180, "revision": 1,
	}})

	// The catalog's upgrade of each migrated copy to the samples this binary
	// ships lands over the stored records, the new convertlab included.
	shipped, err := catalog.Load(catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := shipped.Import(ctx, substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("upgrade the migrated %s to the shipped sample: %v", id, err)
		}
	}
	if got := mustGet(t, ds, metric, "health/body_mass").Properties["unit"]; !jsonSame(got, unitRef("kg")) {
		t.Errorf("body_mass after the sample upgrade: unit %v", got)
	}

	raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if got := rmLedger(t, raw); got[3] != "name_units_by_unit_records" {
		t.Fatalf("ledger = %v, want migration 3 recorded", got)
	}
	rebuiltEqualsLive(t, svc, ds, "after the migration")
	head := changelogHead(t, raw)
	_ = svc.Close()

	// The next open, and an open whose ledger forgot the migration, append
	// nothing: the rewrite finds nothing left to rewrite.
	rmClearLedger(t, dsn, repo)
	svc = openTree(t, dsn, shippedTree(t), engine.WithDataRoot(root), engine.WithUnitSeed(true))
	defer func() { _ = svc.Close() }()
	if _, err := svc.Dataset(ctx, repo); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := changelogHead(t, raw); got != head {
		t.Errorf("the migration's second run moved the head from %d to %d", head, got)
	}
}
