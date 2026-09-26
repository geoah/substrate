package engine_test

// No path holds a repository connection while it asks for a second one
// (#640). With every repository on one shared pool, a path that does so waits
// for a connection only its own kind of caller can release, and once enough
// of them run at once the whole process waits. Each case here runs with ONE
// connection per repository, so such a path deadlocks the test (and fails at
// its context deadline) instead of production.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// singleConnDataset opens a repository, on a shared pool at the floor cap,
// whose handle takes one connection at a time: its creation, its open ladder
// and everything after. The open runs the repository migrations, and the
// two landed ones read the stored declarations through the handle inside
// their transaction; the runner's own connection is what lets that open
// finish here.
func singleConnDataset(t *testing.T) (substrate.Service, substrate.Dataset, string) {
	t.Helper()
	svc, dsn := newService(t, engine.WithRepositoryConnections(engine.MinRepositoryConnections))
	engine.SetRepositoryConnsPerHandle(svc, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository on one connection: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open repository on one connection: %v", err)
	}
	return svc, ds, dsn
}

// originBundle is a bundle package with one kind, the shape a sample import
// lands and stamps with its origin.
func originBundle(pkg string) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description": "one kind, imported as a copy",
			"installs":    []any{pkg + "/tool"},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "tool"}, map[string]any{
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"parent": map[string]any{"type": "reference", "kind": pkg + "/tool"},
			},
		}),
	}
}

func TestNoPathTakesASecondConnectionWhileHoldingOne(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := singleConnDataset(t)
	home := testdb.Repository(t)
	pkg := home + "/tools"
	within := func(t *testing.T) context.Context {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)
		return ctx
	}

	// An apply naming an origin reads the copy's stamp and closure digest
	// inside its transaction (editedCopy). The first lands the stamp; the
	// second is the one that reads it.
	t.Run("origin apply", func(t *testing.T) {
		ctx := within(t)
		apply := substrate.VocabularyApply{Origin: "samples.example.com/tools"}
		for i := range 2 {
			if _, err := ds.ApplyVocabularyDocumentsWith(ctx, owner, originBundle(pkg), apply); err != nil {
				t.Fatalf("apply %d of a stamped copy on one connection: %v", i+1, err)
			}
		}
	})

	// The trigger statuses read the bundle states beside the trigger rows
	// (loadTriggers).
	t.Run("trigger statuses", func(t *testing.T) {
		if _, err := ds.TriggerStatuses(within(t)); err != nil {
			t.Fatalf("trigger statuses on one connection: %v", err)
		}
	})

	// A quarantined copy's status recomputes its closure digest for every
	// row the quarantine read returns (quarantinedBundleStatuses).
	t.Run("quarantined copy status", func(t *testing.T) {
		ctx := within(t)
		raw, err := engine.OpenScopedDB(dsn, home, engine.RoleApp)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = raw.Close() })
		if _, err := raw.ExecContext(ctx,
			`UPDATE records SET props = props || '{"quarantined": "true", "quarantineReason": "test"}'::jsonb
			 WHERE kind = 'substrate.reamde.dev/core/package' AND id = $1`, pkg); err != nil {
			t.Fatalf("mark the copy quarantined: %v", err)
		}
		statuses, err := ds.BundleStatuses(ctx)
		if err != nil {
			t.Fatalf("bundle statuses on one connection: %v", err)
		}
		for _, st := range statuses {
			if st.ID == pkg && st.Quarantined && st.Origin == "samples.example.com/tools" {
				return
			}
		}
		t.Fatalf("the quarantined copy is missing its stamped status: %+v", statuses)
	})

	// The verify reads the stored declarations beside its snapshot
	// transaction (verifySecretRefs).
	t.Run("verify", func(t *testing.T) {
		report, err := svc.(verifier).VerifyRepository(within(t), home)
		if err != nil {
			t.Fatalf("verify on one connection: %v", err)
		}
		if !report.OK {
			t.Fatalf("verify on one connection found: %+v", report.Findings)
		}
	})

	// A write, whose transaction holds its connection through the commit,
	// then a list filtered on a reference: the filter follows the referent's
	// merge trail inside the list's read transaction (referenceFilterIDs).
	t.Run("write and reference filter", func(t *testing.T) {
		ctx := within(t)
		parent, err := ds.Put(ctx, owner, substrate.PutInput{Kind: pkg + "/tool", Properties: map[string]any{"name": "one connection"}})
		if err != nil {
			t.Fatalf("write on one connection: %v", err)
		}
		path := pkg + "/tool/" + parent.ID
		if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: pkg + "/tool", Properties: map[string]any{
			"name": "child", "parent": map[string]any{"ref": path},
		}}); err != nil {
			t.Fatalf("write the child on one connection: %v", err)
		}
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds:      []string{pkg + "/tool"},
			Properties: map[string]substrate.Cond{"parent": {Eq: path}},
		}})
		if err != nil {
			t.Fatalf("reference-filtered list on one connection: %v", err)
		}
		if len(page.Records) != 1 {
			t.Fatalf("the reference filter matched %d records, want the child", len(page.Records))
		}
	})
}
