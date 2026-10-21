package engine_test

// The seeded console package (decision record 0166): a repository created
// before the binary shipped it receives it at its next open like any shipped
// upgrade, and a record view's declared shape refuses a malformed view on
// write.

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

const recordViewKind = "substrate.reamde.dev/console/recordview"

// authorityRowBeforeConsole is the seed authority's own row exactly as the
// binary before the console package shipped it, a fixed copy so the upgrade
// it tests does not move with the tree.
const authorityRowBeforeConsole = `kind: substrate.reamde.dev/core/authority
metadata:
  id: substrate.reamde.dev
data:
  version: 2
  description: >-
    The substrate's own vocabulary: the core and llm packages every repository
    is seeded with.
`

// authorityRowVersion is the version the stored seed authority row holds.
func authorityRowVersion(t *testing.T, ds substrate.Dataset) int64 {
	t.Helper()
	row, err := ds.Get(context.Background(), "substrate.reamde.dev/core/authority", vocabulary.AuthorityPublisher)
	if err != nil {
		t.Fatalf("read the seed authority row: %v", err)
	}
	v, _ := vocabulary.VersionValue(row.Properties["version"])
	return v
}

// aRecordView is a view every block of which is well formed, over a kind the
// seed declares, whose chart, spans, stats, trend and lanes blocks read
// metrics of metricKind: a kind that binds core's `metric` trait.
func aRecordView(metricKind string) map[string]any {
	return map[string]any{
		"appliesTo": "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/token",
		"name":      "Token",
		"window":    map[string]any{"start": "createdAt", "end": "expiresAt"},
		"fold":      []any{"note"},
		"sections": []any{
			map[string]any{"heading": map[string]any{"text": "Usage", "note": "what it did"}},
			map[string]any{"text": map[string]any{"text": "A **token**."}},
			map[string]any{"properties": map[string]any{"list": []any{
				map[string]any{"property": "note", "label": "Note"},
			}}},
			map[string]any{"related": map[string]any{"property": "owner", "show": "count"}},
			map[string]any{"chart": map[string]any{
				"metrics":  []any{metricKind + "/health/heart_rate"},
				"reducers": []any{map[string]any{"reducer": "durationIn", "low": 95.0, "high": 114.0, "label": "Zone 1"}},
				"bucket":   "whole",
				"unit":     "min",
			}},
			map[string]any{"spans": map[string]any{
				"metric": metricKind + "/health/sleep_stage",
				"codes":  []any{map[string]any{"code": "awake", "label": "Awake"}},
			}},
			map[string]any{"stats": map[string]any{
				"label":    "Scores",
				"baseline": 30,
				"bands": []any{map[string]any{
					"metric": metricKind + "/health/heart_rate", "high": 34.0, "label": "Low", "tone": "poor",
				}},
				"items": []any{map[string]any{
					"metric": metricKind + "/health/heart_rate", "from": "summaries", "statistic": "vendor",
					"show": "dial", "min": 0.0, "max": 100.0, "better": "higher",
				}},
			}},
			map[string]any{"trend": map[string]any{
				"span":    30,
				"rolling": 7,
				"items": []any{map[string]any{
					"metric": metricKind + "/health/heart_rate", "reducer": "avg", "show": "bars",
				}},
			}},
			map[string]any{"table": map[string]any{
				"kind":    "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/token",
				"via":     "owner",
				"columns": []any{"note"},
				"empty":   "hide",
			}},
			map[string]any{"lanes": map[string]any{
				"rows": []any{
					map[string]any{"kind": "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/token", "via": "owner", "spanTitle": "note"},
					map[string]any{"metric": metricKind + "/health/sleep_stage", "label": "Stages"},
				},
			}},
			map[string]any{"breakdown": map[string]any{
				"items": []any{map[string]any{"property": "note"}},
			}},
		},
	}
}

// unmeasuredRecordView is aRecordView without the blocks after its related one, for
// a repository that declares no metric kind for them to name.
func unmeasuredRecordView() map[string]any {
	v := aRecordView("")
	v["sections"] = v["sections"].([]any)[:4]
	return v
}

func TestBootUpgradeSeedsTheConsolePackageIntoAnOlderRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)

	// --- binary N: the seed authority without the console package, its own
	// row at the version that binary shipped.
	before := shippedTree(t)
	if err := os.RemoveAll(filepath.Join(before, vocabulary.PackageConsole)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(before, vocabulary.AuthorityPublisher, "authority.yaml"),
		[]byte(authorityRowBeforeConsole), 0o600); err != nil {
		t.Fatal(err)
	}
	svc1 := openTree(t, dsn, before)
	const name = "older.example.com"
	if _, err := svc1.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds1.KindByRef(ctx, recordViewKind); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("binary N already declares %s: %v", recordViewKind, err)
	}
	if v := authorityRowVersion(t, ds1); v != 2 {
		t.Fatalf("binary N seeded the authority row at version %d, want 2", v)
	}
	head := maxSeq(t, ds1)
	_ = svc1.Close()

	// --- binary N+1: the shipped tree, console included.
	svc2 := openTree(t, dsn, shippedTree(t))
	ds2, err := svc2.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	info, err := ds2.KindByRef(ctx, recordViewKind)
	if err != nil {
		t.Fatalf("the boot upgrade did not land %s: %v", recordViewKind, err)
	}
	if info.Source != vocabulary.SourceBuiltin {
		t.Errorf("%s source = %q, want %q", recordViewKind, info.Source, vocabulary.SourceBuiltin)
	}
	sawPackage, sawAuthority := false, false
	for _, ch := range changesSince(t, ds2, head) {
		if ch.Actor != substrate.ActorSystem {
			t.Fatalf("upgrade entry %d is attributed to %q", ch.Seq, ch.Actor)
		}
		switch ch.RecordID {
		case vocabulary.PackageConsole:
			sawPackage = true
		case vocabulary.AuthorityPublisher:
			sawAuthority = true
		}
	}
	if !sawPackage {
		t.Fatal("the upgrade entries do not include the console package row")
	}
	// The authority row names the package now, and moved from 2 to 3.
	if !sawAuthority {
		t.Fatal("the upgrade entries do not include the authority row")
	}
	if v := authorityRowVersion(t, ds2); v != 3 {
		t.Fatalf("the upgraded authority row is at version %d, want 3", v)
	}
	// The upgraded repository takes a view like a fresh one does.
	if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "token", Properties: unmeasuredRecordView(),
	}); err != nil {
		t.Fatalf("write a record view on the upgraded repository: %v", err)
	}
	// And the declaration is the user's to read, not to write.
	_, err = ds2.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{{
		"kind":     "substrate.reamde.dev/core/kind",
		"metadata": map[string]any{"id": recordViewKind},
		"data": map[string]any{
			"authority": "substrate.reamde.dev", "package": "console",
			"names":      map[string]any{"singular": "recordview"},
			"properties": map[string]any{"note": map[string]any{"type": "string"}},
		},
	}})
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a token rewrote the seeded console package: %v", err)
	}
}

// console1 holds the console package as the binary at b7628e92 shipped it
// (version 1: chart and spans, a window with a start and an end) and that
// binary's health sample views, fixed copies so the upgrade this tests does
// not move with the tree.
const console1 = "testdata/console1"

// console2 holds the console package as the binary at f15583784 shipped it
// (version 2: the stats, trend, table, lanes and breakdown blocks; a table
// names `sameAs` and a chart has no `empty`) and that binary's health sample
// views, whose lab result view lists by `sameAs: metric`.
const console2 = "testdata/console2"

// console3 holds the console package as the binary at ac85863e shipped it
// (version 3: a table names `sharing` and a chart `empty`, and a table has
// no `show`) and that binary's health sample views.
const console3 = "testdata/console3"

// treeAtConsole is the shipped tree with the console package put back to the
// version a fixture directory holds.
func treeAtConsole(t *testing.T, fixture string) string {
	t.Helper()
	tree := shippedTree(t)
	for _, f := range []string{"console.yaml", "recordview.yaml"} {
		raw, err := os.ReadFile(filepath.Join(fixture, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tree, vocabulary.PackageConsole, f), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

// samplesAtConsole is the shipped sample catalog with the health sample's
// views put back to the ones a fixture directory holds.
func samplesAtConsole(t *testing.T, fixture string) *catalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, samples.Samples()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture, "health-views.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "health", "views.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Load(catalog.SampleRoot(os.DirFS(dir)))
	if err != nil {
		t.Fatalf("load the sample catalog: %v", err)
	}
	return c
}

func packageRowVersion(t *testing.T, ds substrate.Dataset, pkg string) int64 {
	t.Helper()
	row, err := ds.Get(context.Background(), "substrate.reamde.dev/core/package", pkg)
	if err != nil {
		t.Fatalf("read the %s package row: %v", pkg, err)
	}
	v, _ := vocabulary.VersionValue(row.Properties["version"])
	return v
}

// A repository at console package 1, holding the version 1 sleep and workout
// views (one of them edited by its owner, one written by the owner outright),
// opened by the binary that ships package 2: the boot upgrade lands version
// 2, every stored view reads back as it was and still writes, its references
// are re-derived, and a view only version 2 admits lands.
func TestBootUpgradeMovesAConsole1RepositoryWithLiveViewsToConsole2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	const name = "console1.example.com"
	metric := name + "/observations/metric"

	// --- binary N: console package 1, the health sample imported with its
	// version 1 views, an owner's edit and an owner's own view.
	// The shipped units: the samples' metrics and quantities name them.
	svc1 := openTree(t, dsn, treeAtConsole(t, console1), engine.WithUnitSeed(true))
	if _, err := svc1.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if v := packageRowVersion(t, ds1, vocabulary.PackageConsole); v != 1 {
		t.Fatalf("binary N seeded the console package at version %d, want 1", v)
	}
	c := samplesAtConsole(t, console1)
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds1); err != nil {
			t.Fatalf("import %s on binary N: %v", id, err)
		}
	}
	sleep, workout := name+"/health/sleep", name+"/health/workout"
	if _, err := ds1.Patch(ctx, substrate.ActorAPI, recordViewKind, workout, substrate.PatchInput{
		Properties: map[string]any{"name": "My runs"},
	}); err != nil {
		t.Fatalf("edit the workout view on binary N: %v", err)
	}
	// The owner's own view, the shape Mneme forked from the sample: a
	// three-metric chart, a whole-window zones chart and a spans block.
	own := map[string]any{
		"appliesTo": "substrate.reamde.dev/core/kind/" + name + "/health/sleep",
		"name":      "My night",
		"window":    map[string]any{"start": "at", "end": "endsAt", "about": "about", "source": "source"},
		"sections": []any{
			map[string]any{"chart": map[string]any{
				"label": "Heart rate",
				"metrics": []any{
					metric + "/health/heart_rate", metric + "/health/hrv_rmssd", metric + "/health/respiratory_rate",
				},
				"reducers": []any{map[string]any{"reducer": "avg", "label": "Average"}},
				"bucket":   "auto", "sources": "prefer",
			}},
			map[string]any{"chart": map[string]any{
				"metrics": []any{metric + "/health/heart_rate"},
				"reducers": []any{
					map[string]any{"reducer": "durationIn", "low": 95, "high": 114, "label": "Zone 1"},
					map[string]any{"reducer": "durationIn", "low": 171, "high": 220, "label": "Zone 5"},
				},
				"bucket": "whole", "unit": "min", "sources": "prefer",
			}},
			map[string]any{"spans": map[string]any{
				"metric": metric + "/health/sleep_stage",
				"codes":  []any{map[string]any{"code": "deep", "label": "Deep"}},
				"totals": true, "sources": "only",
			}},
		},
	}
	if _, err := ds1.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine", Properties: own,
	}); err != nil {
		t.Fatalf("write the owner's view on binary N: %v", err)
	}
	// A view only version 2 admits, a window with a start alone and a stats
	// block, which binary N refuses: the fixture is the version 1 grammar.
	v2 := maps.Clone(own)
	v2["window"] = map[string]any{"start": "at", "open": true}
	v2["sections"] = append(slices.Clone(own["sections"].([]any)), map[string]any{"stats": map[string]any{
		"items": []any{map[string]any{"metric": metric + "/health/heart_rate", "reducer": "avg", "show": "number"}},
	}})
	if _, err := ds1.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine-v2", Properties: v2,
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("binary N admitted a version 2 view: %v", err)
	}
	stored := map[string]map[string]any{}
	for _, id := range []string{sleep, workout, "mine"} {
		rec, err := ds1.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s on binary N: %v", id, err)
		}
		stored[id] = rec.Properties
	}
	_ = svc1.Close()

	// --- binary N+1: the console package 2 tree.
	svc2 := openTree(t, dsn, treeAtConsole(t, console2))
	ds2, err := svc2.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	if v := packageRowVersion(t, ds2, vocabulary.PackageConsole); v != 2 {
		t.Fatalf("the boot upgrade left the console package at version %d, want 2", v)
	}
	info, err := ds2.KindByRef(ctx, recordViewKind)
	if err != nil {
		t.Fatalf("read %s on binary N+1: %v", recordViewKind, err)
	}
	if _, ok := info.Definition["properties"].(map[string]any)["sections"]; !ok {
		t.Fatalf("the upgraded %s declares no sections", recordViewKind)
	}
	engine.DrainIndexReprojection(t, ds2)

	for id, before := range stored {
		rec, err := ds2.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s after the upgrade: %v", id, err)
		}
		if !reflect.DeepEqual(rec.Properties, before) {
			t.Errorf("the upgrade changed %s:\n got %v\nwant %v", id, rec.Properties, before)
		}
		// The view still writes under the version 2 grammar, as it stands.
		if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: recordViewKind, ID: id, Properties: rec.Properties,
		}); err != nil {
			t.Errorf("re-put %s after the upgrade: %v", id, err)
		}
	}
	// The refs index answers for the live views: every view naming heart
	// rate comes back from the reverse read.
	page, err := ds2.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:       []string{recordViewKind},
		Referencing: &substrate.Referencing{Ref: metric + "/health/heart_rate"},
	}, First: 50})
	if err != nil {
		t.Fatalf("list the views reading heart rate: %v", err)
	}
	got := map[string]bool{}
	for _, rec := range page.Records {
		got[rec.ID] = true
	}
	for _, id := range []string{sleep, workout, "mine"} {
		if !got[id] {
			t.Errorf("the reverse read of heart rate misses the %s view: %v", id, got)
		}
	}
	// An owner patch lands, and so does the version 2 view binary N refused.
	if _, err := ds2.Patch(ctx, substrate.ActorAPI, recordViewKind, sleep, substrate.PatchInput{
		Properties: map[string]any{"intro": "Last night."},
	}); err != nil {
		t.Fatalf("patch the sleep view after the upgrade: %v", err)
	}
	if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine-v2", Properties: v2,
	}); err != nil {
		t.Fatalf("a version 2 view was refused after the upgrade: %v", err)
	}
}

// A repository at console package 2, holding the version 4 health views (the
// lab result view lists by `sameAs: metric`), an owner's edit and an owner's
// own view naming `sameAs`, opened by the binary that ships package 3: the
// boot upgrade lands version 3, every stored view reads back as it was and
// still writes, and a view naming `sharing` and a chart's `empty`, which
// binary N refuses, lands. Retyping `sameAs` into a list would fail here:
// the engine refuses a scalar-to-list flip while live views hold the scalar.
func TestBootUpgradeMovesAConsole2RepositoryWithLiveViewsToConsole3(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	const name = "console2.example.com"
	metric := name + "/observations/metric"
	labresult := "substrate.reamde.dev/core/kind/" + name + "/health/labresult"

	// --- binary N: console package 2, the health sample imported with its
	// version 4 views, an owner's edit and an owner's own view.
	// The shipped units: the samples' metrics and quantities name them.
	svc1 := openTree(t, dsn, treeAtConsole(t, console2), engine.WithUnitSeed(true))
	if _, err := svc1.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if v := packageRowVersion(t, ds1, vocabulary.PackageConsole); v != 2 {
		t.Fatalf("binary N seeded the console package at version %d, want 2", v)
	}
	c := samplesAtConsole(t, console2)
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds1); err != nil {
			t.Fatalf("import %s on binary N: %v", id, err)
		}
	}
	lab, sleep := name+"/health/labresult", name+"/health/sleep"
	if got, err := ds1.Get(ctx, recordViewKind, lab); err != nil {
		t.Fatalf("read the lab result view on binary N: %v", err)
	} else if table := got.Properties["sections"].([]any)[0].(map[string]any)["table"].(map[string]any); table["sameAs"] != "metric" {
		t.Fatalf("binary N's lab result view lists by %v, want sameAs metric", table)
	}
	if _, err := ds1.Patch(ctx, substrate.ActorAPI, recordViewKind, lab, substrate.PatchInput{
		Properties: map[string]any{"name": "My analyte"},
	}); err != nil {
		t.Fatalf("edit the lab result view on binary N: %v", err)
	}
	own := map[string]any{
		"appliesTo": labresult,
		"name":      "Same analyte",
		"window":    map[string]any{"about": "about"},
		"sections": []any{
			map[string]any{"table": map[string]any{
				"kind": labresult, "sameAs": "metric", "chart": true, "limit": 20,
			}},
			map[string]any{"chart": map[string]any{
				"metrics":  []any{metric + "/health/heart_rate"},
				"reducers": []any{map[string]any{"reducer": "avg"}},
			}},
		},
	}
	if _, err := ds1.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine", Properties: own,
	}); err != nil {
		t.Fatalf("write the owner's view on binary N: %v", err)
	}
	// Views only version 3 admits: a table naming sharing, and a chart
	// naming empty. Binary N refuses both.
	sharing := maps.Clone(own)
	sharing["sections"] = []any{map[string]any{"table": map[string]any{
		"kind": labresult, "sharing": []any{"metric", "channel"}, "chart": true,
	}}}
	hiding := maps.Clone(own)
	hiding["sections"] = []any{map[string]any{"chart": map[string]any{
		"metrics":  []any{metric + "/health/heart_rate"},
		"reducers": []any{map[string]any{"reducer": "avg"}},
		"empty":    "hide",
	}}}
	v3 := map[string]map[string]any{"mine-sharing": sharing, "mine-hiding": hiding}
	for id, v := range v3 {
		if _, err := ds1.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: recordViewKind, ID: id, Properties: v,
		}); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("binary N admitted the version 3 view %s: %v", id, err)
		}
	}
	stored := map[string]map[string]any{}
	for _, id := range []string{lab, sleep, "mine"} {
		rec, err := ds1.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s on binary N: %v", id, err)
		}
		stored[id] = rec.Properties
	}
	_ = svc1.Close()

	// --- binary N+1: console package 3.
	svc2 := openTree(t, dsn, treeAtConsole(t, console3))
	ds2, err := svc2.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	if v := packageRowVersion(t, ds2, vocabulary.PackageConsole); v != 3 {
		t.Fatalf("the boot upgrade left the console package at version %d, want 3", v)
	}
	engine.DrainIndexReprojection(t, ds2)
	for id, before := range stored {
		rec, err := ds2.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s after the upgrade: %v", id, err)
		}
		if !reflect.DeepEqual(rec.Properties, before) {
			t.Errorf("the upgrade changed %s:\n got %v\nwant %v", id, rec.Properties, before)
		}
		if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: recordViewKind, ID: id, Properties: rec.Properties,
		}); err != nil {
			t.Errorf("re-put %s after the upgrade: %v", id, err)
		}
	}
	for id, v := range v3 {
		if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: recordViewKind, ID: id, Properties: v,
		}); err != nil {
			t.Errorf("the version 3 view %s was refused after the upgrade: %v", id, err)
		}
	}
}

// A repository at console package 3, holding the health sample's views, an
// owner's edit of the lab report view and an owner's own table, opened by the
// binary that ships package 4: the boot upgrade lands version 4, every stored
// view reads back as it was and still writes, and a table naming `show:
// tiles`, which binary N refuses, lands (decision record 0185).
func TestBootUpgradeMovesAConsole3RepositoryWithLiveViewsToConsole4(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	const name = "console3.example.com"
	labresult := "substrate.reamde.dev/core/kind/" + name + "/health/labresult"
	labreport := "substrate.reamde.dev/core/kind/" + name + "/health/labreport"

	// --- binary N: console package 3, the health sample imported with its
	// views, an owner's edit and an owner's own view.
	// The shipped units: the samples' metrics and quantities name them.
	svc1 := openTree(t, dsn, treeAtConsole(t, console3), engine.WithUnitSeed(true))
	if _, err := svc1.CreateRepository(ctx, name); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if v := packageRowVersion(t, ds1, vocabulary.PackageConsole); v != 3 {
		t.Fatalf("binary N seeded the console package at version %d, want 3", v)
	}
	c := samplesAtConsole(t, console3)
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds1); err != nil {
			t.Fatalf("import %s on binary N: %v", id, err)
		}
	}
	report := name + "/health/labreport"
	if _, err := ds1.Patch(ctx, substrate.ActorAPI, recordViewKind, report, substrate.PatchInput{
		Properties: map[string]any{"name": "My report"},
	}); err != nil {
		t.Fatalf("edit the lab report view on binary N: %v", err)
	}
	own := map[string]any{
		"appliesTo": labreport,
		"name":      "Results",
		"window":    map[string]any{"about": "about"},
		"sections": []any{map[string]any{"table": map[string]any{
			"kind": labresult, "via": "report", "nest": "derivedFrom", "limit": 100,
		}}},
	}
	if _, err := ds1.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine", Properties: own,
	}); err != nil {
		t.Fatalf("write the owner's view on binary N: %v", err)
	}
	// A view only version 4 admits: a table naming show. Binary N refuses it.
	tiles := maps.Clone(own)
	tiles["sections"] = []any{map[string]any{"table": map[string]any{
		"kind": labresult, "via": "report", "nest": "derivedFrom", "show": "tiles",
	}}}
	if _, err := ds1.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine-tiles", Properties: tiles,
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("binary N admitted the version 4 view: %v", err)
	}
	stored := map[string]map[string]any{}
	for _, id := range []string{report, name + "/health/labresult", "mine"} {
		rec, err := ds1.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s on binary N: %v", id, err)
		}
		stored[id] = rec.Properties
	}
	_ = svc1.Close()

	// --- binary N+1: the shipped tree, console package 4.
	svc2 := openTree(t, dsn, shippedTree(t))
	ds2, err := svc2.Dataset(ctx, name)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	if v := packageRowVersion(t, ds2, vocabulary.PackageConsole); v != 4 {
		t.Fatalf("the boot upgrade left the console package at version %d, want 4", v)
	}
	engine.DrainIndexReprojection(t, ds2)
	for id, before := range stored {
		rec, err := ds2.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s after the upgrade: %v", id, err)
		}
		if !reflect.DeepEqual(rec.Properties, before) {
			t.Errorf("the upgrade changed %s:\n got %v\nwant %v", id, rec.Properties, before)
		}
		if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: recordViewKind, ID: id, Properties: rec.Properties,
		}); err != nil {
			t.Errorf("re-put %s after the upgrade: %v", id, err)
		}
	}
	if _, err := ds2.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "mine-tiles", Properties: tiles,
	}); err != nil {
		t.Errorf("the version 4 view was refused after the upgrade: %v", err)
	}
}

func TestRecordViewRefusesAMalformedView(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openObserved(t)
	ds := r.ds

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "token", Properties: aRecordView(r.metric),
	}); err != nil {
		t.Fatalf("a well-formed view was refused: %v", err)
	}

	// A window may name a start alone: open reads a record with no end up to
	// now, and a view on a record with no window of its own names neither.
	open := aRecordView(r.metric)
	open["window"] = map[string]any{"start": "createdAt", "open": true}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "open", Properties: open,
	}); err != nil {
		t.Fatalf("a view whose window has a start alone and open was refused: %v", err)
	}

	// A trend item takes the reducer arguments a stats item does: time in a
	// heart rate zone per day, and a percentile.
	zones := aRecordView(r.metric)
	zones["sections"].([]any)[7].(map[string]any)["trend"].(map[string]any)["items"] = []any{
		map[string]any{"metric": r.metric + "/health/heart_rate", "reducer": "durationIn", "low": 133.0, "high": 152.0, "unit": "min"},
		map[string]any{"metric": r.metric + "/health/heart_rate", "reducer": "percentile", "rank": 0.9},
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "zones", Properties: zones,
	}); err != nil {
		t.Fatalf("a trend of time in a heart rate zone was refused: %v", err)
	}

	// A chart, a stats or a trend block, as a table, may be left out where
	// none of its items holds a value: a vendor's scores on a record another
	// source wrote.
	hiding := aRecordView(r.metric)
	hiding["sections"].([]any)[6].(map[string]any)["stats"].(map[string]any)["empty"] = "hide"
	hiding["sections"].([]any)[7].(map[string]any)["trend"].(map[string]any)["empty"] = "hide"
	hiding["sections"].([]any)[4].(map[string]any)["chart"].(map[string]any)["empty"] = "hide"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "hiding", Properties: hiding,
	}); err != nil {
		t.Fatalf("a chart, a stats and a trend block that hide when empty were refused: %v", err)
	}

	section := func(v map[string]any, i int) map[string]any {
		return v["sections"].([]any)[i].(map[string]any)
	}
	cases := []struct {
		name   string
		mutate func(v map[string]any)
		says   string
	}{
		{"no kind", func(v map[string]any) { delete(v, "appliesTo") }, "appliesTo"},
		{"a kind named by a pointer at another kind's record", func(v map[string]any) {
			v["appliesTo"] = "substrate.reamde.dev/core/token/t1"
		}, "appliesTo"},
		{"an unknown block", func(v map[string]any) {
			v["sections"] = append(v["sections"].([]any), map[string]any{"gauge": map[string]any{"text": "x"}})
		}, "gauge"},
		{"a chart naming no metric", func(v map[string]any) {
			delete(section(v, 4)["chart"].(map[string]any), "metrics")
		}, "metrics"},
		{"a chart bucket outside the list", func(v map[string]any) {
			section(v, 4)["chart"].(map[string]any)["bucket"] = "fortnight"
		}, "bucket"},
		{"a band bound that is no number", func(v map[string]any) {
			section(v, 4)["chart"].(map[string]any)["reducers"].([]any)[0].(map[string]any)["low"] = "ninety"
		}, "low"},
		{"a related kind named by a pointer at another kind's record", func(v map[string]any) {
			v["sections"] = append(v["sections"].([]any), map[string]any{"related": map[string]any{"kind": "substrate.reamde.dev/core/token/t1", "via": "session"}})
		}, "kind"},
		{"a related list shown as a grid", func(v map[string]any) {
			section(v, 3)["related"].(map[string]any)["show"] = "grid"
		}, "show"},
		// A metric is a reference to a record whose kind binds core's
		// `metric`: a bare id names no kind, so two metric kinds holding the
		// same id could not be told apart.
		{"a chart metric named by its bare id", func(v map[string]any) {
			section(v, 4)["chart"].(map[string]any)["metrics"] = []any{"health/heart_rate"}
		}, "metrics"},
		{"a chart metric that is no metric", func(v map[string]any) {
			section(v, 4)["chart"].(map[string]any)["metrics"] = []any{r.source + "/strap"}
		}, "substrate.reamde.dev/core/metric"},
		{"a spans metric named by its bare id", func(v map[string]any) {
			section(v, 5)["spans"].(map[string]any)["metric"] = "health/sleep_stage"
		}, "metric"},
		{"a spans metric that is no metric", func(v map[string]any) {
			section(v, 5)["spans"].(map[string]any)["metric"] = r.source + "/strap"
		}, "substrate.reamde.dev/core/metric"},
		{"spans naming no metric", func(v map[string]any) {
			delete(section(v, 5)["spans"].(map[string]any), "metric")
		}, "metric"},
		{"a heading with no text", func(v map[string]any) {
			delete(section(v, 0)["heading"].(map[string]any), "text")
		}, "text"},
		{"a stats item metric named by its bare id", func(v map[string]any) {
			section(v, 6)["stats"].(map[string]any)["items"].([]any)[0].(map[string]any)["metric"] = "health/heart_rate"
		}, "metric"},
		{"a table kind named by a pointer at another kind's record", func(v map[string]any) {
			section(v, 8)["table"].(map[string]any)["kind"] = "substrate.reamde.dev/core/token/t1"
		}, "kind"},
		{"a lanes row metric that is no metric", func(v map[string]any) {
			section(v, 9)["lanes"].(map[string]any)["rows"].([]any)[1].(map[string]any)["metric"] = r.source + "/strap"
		}, "substrate.reamde.dev/core/metric"},
		{"a trend over one bucket", func(v map[string]any) {
			section(v, 7)["trend"].(map[string]any)["span"] = 1
		}, "span"},
		{"a band tone outside the list", func(v map[string]any) {
			section(v, 6)["stats"].(map[string]any)["bands"].([]any)[0].(map[string]any)["tone"] = "great"
		}, "tone"},
		{"a stats empty outside the list", func(v map[string]any) {
			section(v, 6)["stats"].(map[string]any)["empty"] = "fade"
		}, "empty"},
		{"a trend empty outside the list", func(v map[string]any) {
			section(v, 7)["trend"].(map[string]any)["empty"] = "fade"
		}, "empty"},
		{"a chart empty outside the list", func(v map[string]any) {
			section(v, 4)["chart"].(map[string]any)["empty"] = "fade"
		}, "empty"},
		{"a table sharing that is no list of names", func(v map[string]any) {
			section(v, 8)["table"].(map[string]any)["sharing"] = map[string]any{"metric": true}
		}, "sharing"},
		{"a trend percentile rank above 1", func(v map[string]any) {
			item := section(v, 7)["trend"].(map[string]any)["items"].([]any)[0].(map[string]any)
			item["reducer"], item["rank"] = "percentile", 1.5
		}, "rank"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := aRecordView(r.metric)
			tc.mutate(v)
			_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
				Kind: recordViewKind, ID: "broken", Properties: v,
			})
			if !errors.Is(err, substrate.ErrValidation) {
				t.Fatalf("a view with %s was admitted: %v", tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal does not name %q: %v", tc.says, err)
			}
		})
	}
}

// A closure's shipped data record replaces the stored one while the owner
// never wrote it, and stays as the owner left it once the owner did: an edit,
// a removed property and a deletion all survive the upgrade (record 0166).
// Another bundle's or the engine's write is not the owner's.
func TestReinstallReplacesAnUneditedViewAndKeepsTheOwnersOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openObserved(t)
	ds := r.ds
	pkg := ds.Repository().Authority + "/viewtest"
	installer := substrate.BundleActor(ds.Repository().Authority, "viewtest")
	closure := []map[string]any{vocabulary.PackageManifest(pkg, 0)}
	ids := []string{"untouched", "machine", "edited", "trimmed", "deleted"}
	install := func(name string) {
		t.Helper()
		views := make([]substrate.PutInput, 0, len(ids))
		for _, id := range ids {
			v := aRecordView(r.metric)
			v["name"] = name
			views = append(views, substrate.PutInput{Kind: recordViewKind, ID: id, Properties: v})
		}
		if _, err := ds.InstallBundleClosure(ctx, installer, closure, views, substrate.BundleInstall{}); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
	read := func(id string) map[string]any {
		t.Helper()
		v, err := ds.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return v.Properties
	}
	patch := func(actor substrate.Actor, id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Patch(ctx, actor, recordViewKind, id, substrate.PatchInput{Properties: props}); err != nil {
			t.Fatalf("patch %s: %v", id, err)
		}
	}

	install("shipped one")
	patch(substrate.ActorSystem, "machine", map[string]any{"intro": "the engine's"})
	patch(substrate.ActorAPI, "edited", map[string]any{"intro": "mine"})
	patch(substrate.ActorConsole, "trimmed", map[string]any{"fold": nil})
	if _, err := ds.Delete(ctx, substrate.ActorCLI, recordViewKind, "deleted", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete a view: %v", err)
	}
	install("shipped two")

	for _, id := range []string{"untouched", "machine"} {
		if got := read(id)["name"]; got != "shipped two" {
			t.Errorf("the %s view kept %q, want the newer shipped one", id, got)
		}
	}
	edited := read("edited")
	if edited["name"] != "shipped one" || edited["intro"] != "mine" {
		t.Errorf("the edited view became name %q intro %q, want the owner's copy left alone", edited["name"], edited["intro"])
	}
	trimmed := read("trimmed")
	if trimmed["name"] != "shipped one" || trimmed["fold"] != nil {
		t.Errorf("the trimmed view became name %q fold %v, want the owner's removal kept", trimmed["name"], trimmed["fold"])
	}
	if v, err := ds.Get(ctx, recordViewKind, "deleted"); err == nil && v.DeletedAt == nil {
		t.Error("the upgrade recreated the view the owner deleted")
	}
}
