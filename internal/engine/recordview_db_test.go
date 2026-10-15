package engine_test

// The seeded console package (decision record 0166): a repository created
// before the binary shipped it receives it at its next open like any shipped
// upgrade, and a record view's declared shape refuses a malformed view on
// write.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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
// seed declares.
func aRecordView() map[string]any {
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
				"metrics":  []any{"health/heart_rate"},
				"reducers": []any{map[string]any{"reducer": "durationIn", "low": 95.0, "high": 114.0, "label": "Zone 1"}},
				"bucket":   "whole",
				"unit":     "min",
			}},
			map[string]any{"spans": map[string]any{
				"metric": "health/sleep_stage",
				"codes":  []any{map[string]any{"code": "awake", "label": "Awake"}},
			}},
		},
	}
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
		Kind: recordViewKind, ID: "token", Properties: aRecordView(),
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

func TestRecordViewRefusesAMalformedView(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: recordViewKind, ID: "token", Properties: aRecordView(),
	}); err != nil {
		t.Fatalf("a well-formed view was refused: %v", err)
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
		{"spans naming no metric", func(v map[string]any) {
			delete(section(v, 5)["spans"].(map[string]any), "metric")
		}, "metric"},
		{"a heading with no text", func(v map[string]any) {
			delete(section(v, 0)["heading"].(map[string]any), "text")
		}, "text"},
		{"a window with no end", func(v map[string]any) {
			delete(v["window"].(map[string]any), "end")
		}, "end"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := aRecordView()
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

// A closure's shipped view replaces the stored one while every property of it
// was last written by the installing bundle, and stays as the owner left it
// once any other actor wrote one.
func TestReinstallReplacesAnUneditedViewAndKeepsAnEditedOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	pkg := ds.Repository().Authority + "/viewtest"
	installer := substrate.BundleActor(ds.Repository().Authority, "viewtest")
	closure := []map[string]any{vocabulary.PackageManifest(pkg, 0)}
	install := func(name string) {
		t.Helper()
		views := make([]substrate.PutInput, 0, 2)
		for _, id := range []string{"untouched", "edited"} {
			v := aRecordView()
			v["name"] = name
			views = append(views, substrate.PutInput{Kind: recordViewKind, ID: id, Properties: v})
		}
		if _, err := ds.InstallBundleClosure(ctx, installer, closure, views, substrate.BundleInstall{}); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
	name := func(id string) string {
		t.Helper()
		v, err := ds.Get(ctx, recordViewKind, id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		s, _ := v.Properties["name"].(string)
		return s
	}

	install("shipped one")
	if _, err := ds.Patch(ctx, substrate.ActorAPI, recordViewKind, "edited", substrate.PatchInput{
		Properties: map[string]any{"intro": "mine"},
	}); err != nil {
		t.Fatalf("edit a view: %v", err)
	}
	install("shipped two")
	if got := name("untouched"); got != "shipped two" {
		t.Errorf("an unedited view kept %q, want the newer shipped one", got)
	}
	if got := name("edited"); got != "shipped one" {
		t.Errorf("an edited view became %q, want the owner's copy left alone", got)
	}
}
