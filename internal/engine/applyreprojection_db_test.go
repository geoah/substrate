package engine_test

// A vocabulary apply and a provider upgrade hold the registry lock every
// write waits on, so neither re-derives a reshaped kind's search index in
// its transaction: the transaction records the kind for the pass behind the
// commit (reprojection.go), which re-derives the rows that carry a moved
// property, and the door returns without waiting for it (issue 888).

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	rpPackage = "reshape.example.substrate.reamde.dev/meters"
	rpMeter   = rpPackage + "/meter"
)

// rpProviderCatalog is a provider tree shipping one version of the meters
// package, whose meter kind indexes its remark or does not.
func rpProviderCatalog(t *testing.T, version int64, remarkIndexed bool) *catalog.Catalog {
	t.Helper()
	remark := map[string]any{"type": "text"}
	if !remarkIndexed {
		remark["fts"] = false
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(rpPackage, version),
		vocabulary.ActorManifest(rpPackage, vocabulary.PackageActor(rpPackage)),
		vocabulary.BundleManifest(rpPackage, map[string]any{
			"description": "a provider whose upgrade indexes a property its rows carry",
			"installs":    []any{rpMeter},
		}),
		vocabulary.KindManifest(rpPackage, map[string]any{"singular": "meter"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"remark": remark,
			}}),
	}
	var file strings.Builder
	for _, d := range docs {
		out, err := yaml.Marshal(d)
		if err != nil {
			t.Fatalf("render a manifest: %v", err)
		}
		file.WriteString("---\n")
		file.Write(out)
	}
	c, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{
		"meters/bundle.yaml": &fstest.MapFile{Data: []byte(file.String())},
	}))
	if err != nil {
		t.Fatalf("load the provider tree: %v", err)
	}
	if w := c.Warnings(); len(w) > 0 {
		t.Fatalf("the provider tree did not load: %v", w)
	}
	return c
}

// TestAProviderUpgradeReturnsBeforeItReindexesAReshapedKind: the catalog's
// install over an older version indexes a property the stored rows carry.
// The install returns while the pass behind its commit is held before its
// first page, with the declaration published and the stored row still
// indexed as the old version indexed it; once the pass is let go and drained,
// search finds the row, the pass has logged the kind, and the fold equals a
// rebuild's.
func TestAProviderUpgradeReturnsBeforeItReindexesAReshapedKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs lockedLog
	release := make(chan struct{})
	svc, ds := newCoreDataset(t,
		engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		engine.WithTestReprojectionHook(func(ctx context.Context, kind string) error {
			if kind != rpMeter {
				return nil
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}))

	if _, _, err := rpProviderCatalog(t, 1, false).Install(ctx, owner, rpPackage, ds); err != nil {
		t.Fatalf("install version 1: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: rpMeter, ID: "kitchen", Properties: map[string]any{
		"name": "kitchen", "remark": "a wombat under the sink",
	}})
	if got := idsOfHits(lexicalHits(t, ds, "wombat")); len(got) != 0 {
		t.Fatalf("with remark opted out, search finds %v", got)
	}

	if _, _, err := rpProviderCatalog(t, 2, true).Install(ctx, owner, rpPackage, ds); err != nil {
		t.Fatalf("upgrade to version 2: %v", err)
	}
	// A row written now derives under the published declaration, so it is
	// found; the stored one waits for the pass, which the hook holds.
	mustPut(t, ds, owner, substrate.PutInput{Kind: rpMeter, ID: "pantry", Properties: map[string]any{
		"name": "pantry", "remark": "a wombat behind the flour",
	}})
	if got := idsOfHits(lexicalHits(t, ds, "wombat")); !reflect.DeepEqual(got, []string{"pantry"}) {
		t.Fatalf("before the pass, search finds %v, want only the row written after the upgrade [pantry]", got)
	}

	close(release)
	engine.DrainIndexReprojection(t, ds)
	if got := idsOfHits(lexicalHits(t, ds, "wombat")); len(got) != 2 {
		t.Fatalf("after the pass, search finds %v, want kitchen and pantry", got)
	}
	// The pass deletes the request before it logs the kind, so the log is
	// read once the run has returned.
	select {
	case <-engine.IndexReprojectionDone(ds):
	case <-time.After(time.Minute):
		t.Fatal("the pass behind the upgrade did not return within a minute")
	}
	pass := linesWith(logs.String(), `msg="substrate: re-derived the indexes of one kind"`)
	if len(pass) != 1 || !strings.Contains(pass[0], "kind="+rpMeter) || !strings.Contains(pass[0], "properties=[remark]") {
		t.Fatalf("the pass behind the upgrade logged %q, want one line for the meter's remark", pass)
	}

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(before) != string(after) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
	}
}

// TestTheReprojectionKeepsTheRequestOfAnApplyThatDidNotPublish: an apply
// whose commit answer is lost after Postgres committed and before its
// registry published leaves the request committed and this process serving
// the declarations it replaced, with writes refused until restart. The pass
// the apply starts must neither derive under those declarations nor delete
// the request, so the next open, which loads the committed declarations,
// re-derives the row under them.
func TestTheReprojectionKeepsTheRequestOfAnApplyThatDidNotPublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fault := &armedFault{}
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithTestCommitFault(fault.hook))
	root := engine.DataRootOf(svc)
	notes := func(remarkIndexed bool) []map[string]any {
		remark := map[string]any{"type": "text"}
		if !remarkIndexed {
			remark["fts"] = false
		}
		return []map[string]any{
			vocabulary.PackageManifest(ftsPackage, 0),
			vocabulary.KindManifest(ftsPackage, map[string]any{"singular": "note"},
				map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
					"name":   map[string]any{"type": "string"},
					"remark": remark,
				}}),
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, notes(false)); err != nil {
		t.Fatalf("declare the notes: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: ftsNote, ID: "kitchen", Properties: map[string]any{
		"name": "kitchen", "remark": "a wombat under the sink",
	}})

	fault.arm(engine.CommitUnpublished)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, notes(true)); !errors.Is(err, engine.ErrChangelogFileBehind) {
		t.Fatalf("an apply that did not publish: err = %v, want ErrChangelogFileBehind", err)
	}
	select {
	case <-engine.IndexReprojectionDone(ds):
	case <-time.After(time.Minute):
		t.Fatal("the pass the apply started did not return within a minute")
	}
	var owed []string
	rows, err := rawDB(t, dsn).QueryContext(ctx, `SELECT kind FROM index_reprojections ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		owed = append(owed, kind)
	}
	_ = rows.Close()
	if !reflect.DeepEqual(owed, []string{ftsNote}) {
		t.Fatalf("after the pass the repository owes %v, want the committed request for %s", owed, ftsNote)
	}
	_ = svc.Close()

	svc2 := mustReopen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	engine.DrainIndexReprojection(t, ds2)
	if got := idsOfHits(lexicalHits(t, ds2, "wombat")); !reflect.DeepEqual(got, []string{"kitchen"}) {
		t.Fatalf("after the reopen, search finds %v, want [kitchen] under the committed declaration", got)
	}
}
