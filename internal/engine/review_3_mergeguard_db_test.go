package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// TestSk3MergeSystemTypes probes whether merge refuses system-type records
// the way put/patch/delete do, and whether a restart revives the projection.
func TestSk3MergeSystemTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn,
			engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}

	// Sanity: put/patch/delete refuse type projections.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/kind", ID: "people.substrate.geoah.me/organization",
		Properties: map[string]any{"name": "organization"},
	}); err == nil {
		t.Fatal("put on a type projection unexpectedly succeeded")
	} else {
		t.Logf("put refused: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, "core.substrate.reamde.dev/kind", "people.substrate.geoah.me/organization"); err == nil {
		t.Fatal("delete on a type projection unexpectedly succeeded")
	} else {
		t.Logf("delete refused: %v", err)
	}

	rec, err := ds.Merge(ctx, owner, "core.substrate.reamde.dev/kind", "people.substrate.geoah.me/person", "people.substrate.geoah.me/organization")
	if err != nil {
		t.Logf("merge refused (claim REFUTED): %v", err)
		return
	}
	t.Errorf("merge of two type projections SUCCEEDED: record %s", rec.ID)

	listed := func(d substrate.Dataset) bool {
		page, err := d.List(ctx, substrate.Query{
			Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/kind"}}, First: 500,
		})
		if err != nil {
			t.Fatalf("list types: %v", err)
		}
		for _, e := range page.Records {
			if e.ID == "people.substrate.geoah.me/organization" {
				return true
			}
		}
		return false
	}
	t.Logf("organization type listed right after merge: %v", listed(ds))

	// Can it be undone via the public split verb?
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Logf("split refused: %v", err)
	} else {
		t.Logf("split ok; organization type listed again: %v", listed(ds))
	}
	// Re-merge, this time with a connector actor, to see if authz blocks it.
	rec2, err := ds.Merge(ctx, substrate.Actor("connector:slack"), "core.substrate.reamde.dev/kind", "people.substrate.geoah.me/person", "people.substrate.geoah.me/organization")
	if err != nil {
		t.Logf("connector-actor merge refused: %v", err)
	} else {
		t.Logf("connector-actor merge SUCCEEDED: %s; listed=%v", rec2.ID, listed(ds))
	}

	// Restart: does reconcileProjections revive it?
	_ = svc.Close()
	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset after restart: %v", err)
	}
	t.Logf("organization type listed after restart: %v", listed(ds2))
	after, err := ds2.Get(ctx, "core.substrate.reamde.dev/kind", "people.substrate.geoah.me/organization")
	if err == nil {
		t.Logf("after restart: deletedAt=%v finalizers=%v version=%d", after.DeletedAt, after.Finalizers, after.Version)
	} else {
		t.Logf("get after restart: %v", err)
	}
}

// TestSk3MergeOfOneType probes merging two messages: legal now, and
// reversible.
func TestSk3MergeOfOneType(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	ctx := context.Background()

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, Properties: map[string]any{"provider": "slack"},
	})
	conv := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "messaging.substrate.geoah.me/conversation",
		Properties: map[string]any{"title": "c"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	author := mustPut(t, ds, owner, substrate.PutInput{Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "alex"}})
	mk := func(ext, text string) *substrate.Record {
		return mustPut(t, ds, owner, substrate.PutInput{
			Kind:       "messaging.substrate.geoah.me/conversationmessage",
			ID:         extID("slack.msg", ext),
			Properties: map[string]any{"at": "2026-01-01T10:00:00Z", "text": text},
			Edges: []substrate.EdgeInput{
				{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
				{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
			},
		})
	}
	a := mk("m1", "one")
	b := mk("m2", "two")
	rec, err := ds.Merge(ctx, owner, a.Kind, a.ID, b.ID)
	if err != nil {
		t.Fatalf("merge of two messages: %v", err)
	}
	if got := mustGet(t, ds, b.Kind, b.ID); got.ID != a.ID || got.CanonicalID != a.ID {
		t.Fatalf("the loser should resolve to the winner: %+v", got)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if got := mustGet(t, ds, b.Kind, b.ID); got.ID != b.ID || got.DeletedAt != nil {
		t.Fatalf("split did not restore the loser: %+v", got)
	}
}
