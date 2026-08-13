package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// TestSk1MergeSystemTypeProjections probes whether merge enforces the same
// system-type guard put/patch/delete do.
func TestSk1MergeSystemTypeProjections(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/kind"}}, First: 500,
	})
	if err != nil {
		t.Fatalf("list types: %v", err)
	}
	before := len(page.Records)
	var contactTy, teamTy string
	for _, e := range page.Records {
		switch e.ID {
		case "people.substrate.geoah.me/person":
			contactTy = e.ID
		case "people.substrate.geoah.me/organization":
			teamTy = e.ID
		}
	}
	t.Logf("type projections: %d, contact=%q team=%q", before, contactTy, teamTy)
	if contactTy == "" || teamTy == "" {
		for _, e := range page.Records {
			t.Logf("  id=%s type=%s", e.ID, e.Kind)
		}
		t.Fatalf("type projections not found")
	}

	// Control: put/patch/delete all refuse.
	_, perr := ds.Put(ctx, owner, substrate.PutInput{Kind: "core.substrate.reamde.dev/kind", ID: teamTy, Properties: map[string]any{"name": "hax"}})
	t.Logf("put on type projection: %v (forbidden=%v)", perr, errors.Is(perr, substrate.ErrForbidden))
	_, derr := ds.Delete(ctx, owner, "core.substrate.reamde.dev/kind", teamTy)
	t.Logf("delete on type projection: %v (forbidden=%v)", derr, errors.Is(derr, substrate.ErrForbidden))

	rec, merr := ds.Merge(ctx, owner, "core.substrate.reamde.dev/kind", contactTy, teamTy)
	if merr != nil {
		t.Logf("merge REFUSED: %v (forbidden=%v)", merr, errors.Is(merr, substrate.ErrForbidden))
		return
	}
	t.Errorf("merge of two type projections SUCCEEDED, record %s", rec.ID)

	after, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/kind"}}, First: 500,
	})
	if err != nil {
		t.Fatalf("list types after: %v", err)
	}
	t.Logf("type projections after merge: %d (was %d)", len(after.Records), before)
	for _, e := range after.Records {
		if e.ID == teamTy {
			t.Errorf("team type still listed")
		}
	}
	got, err := ds.Get(ctx, "core.substrate.reamde.dev/kind", teamTy)
	if err != nil {
		t.Logf("get team type: %v", err)
	} else {
		t.Logf("team type deletedAt=%v finalizers=%v", got.DeletedAt, got.Finalizers)
	}

	// Can it still be used as a type for writes?
	e, err := ds.Put(ctx, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "still works?"}})
	t.Logf("put team record after type merge: %v err=%v", e != nil, err)

	// Does split undo it?
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Logf("split FAILED (damage is unrecoverable via API): %v", err)
	} else {
		back, err := ds.Get(ctx, "core.substrate.reamde.dev/kind", teamTy)
		t.Logf("after split: team type err=%v deletedAt=%v", err, func() any {
			if back == nil {
				return nil
			}
			return back.DeletedAt
		}())
	}
}

// TestSk1MergeNonFusingTypes probes merge on a type the spec says never fuses.
func TestSk1MergeNonFusingTypes(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	acct := mustPut(t, ds, owner, substrate.PutInput{Kind: enginetest.AccountType, Properties: map[string]any{"provider": "slack", "label": "w"}})
	conv := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "conversation", Properties: map[string]any{"kind": "channel", "name": "general"},
		Edges: []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acct.ID}}},
	})
	author := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	mk := func(ext string) *substrate.Record {
		return mustPut(t, ds, owner, substrate.PutInput{
			Kind: "conversationmessage", Properties: map[string]any{"text": "hi " + ext},
			Edges: []substrate.EdgeInput{
				{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
				{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
			},
		})
	}
	m1, m2 := mk("M1"), mk("M2")
	// Merging two records of ONE type is legal now: the only
	// refusals are across types and inside core.
	rec, err := ds.Merge(ctx, owner, m1.Kind, m1.ID, m2.ID)
	if err != nil {
		t.Fatalf("merge of two messages: %v", err)
	}
	if got := mustGet(t, ds, m2.Kind, m2.ID); got.ID != m1.ID {
		t.Fatalf("the loser should resolve to the winner: %+v", got)
	}
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	// Across types it is inexpressible: the verb addresses one type, so the
	// person's id does not exist among messages — a not-found, no merge.
	if _, err := ds.Merge(ctx, owner, m1.Kind, m1.ID, author.ID); err == nil {
		t.Fatal("merging a message into a person must be refused")
	} else {
		wantErr(t, err, substrate.ErrNotFound, "merge across types")
	}
}

// TestSk1MergedTypeSurvivesRestart checks that a type projection cannot be
// merged away in the first place, restart or no restart.
func TestSk1MergedTypeSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn,
			engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "people")
	_, merr := ds.Merge(ctx, owner, "core.substrate.reamde.dev/kind", "people.substrate.geoah.me/person", "people.substrate.geoah.me/organization")
	if !errors.Is(merr, substrate.ErrForbidden) {
		t.Fatalf("merge of two type projections: %v", merr)
	}
	_ = svc.Close()

	svc2 := open()
	defer func() { _ = svc2.Close() }()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	e, err := ds2.Get(ctx, "core.substrate.reamde.dev/kind", "people.substrate.geoah.me/organization")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if e.DeletedAt != nil {
		t.Errorf("after restart the team type projection is STILL tombstoned: deletedAt=%v finalizers=%v", e.DeletedAt, e.Finalizers)
	} else {
		t.Logf("restart revived the projection")
	}
}

// TestSk1RepositoryRowsAreNotRecords replaces the old "merge two repository rows
// in the control plane" probe. The attack it guarded — reaching the ledger
// Dataset()/ExchangeOTP() resolve through, and merging or deleting a row out
// from under another user — has no surface left: the ledger is a table on the
// maintenance pool, substrate_app has no grant on it, and a repository's own
// `repository` record describes only itself.
func TestSk1RepositoryRowsAreNotRecords(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	alpha, err := svc.CreateRepository(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, "beta"); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/repository"}}, First: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != alpha.ID {
		t.Fatalf("alpha sees %v, want only its own description", ids(page.Records))
	}
	if _, err := ds.Delete(ctx, owner, "core.substrate.reamde.dev/repository", alpha.ID); err == nil {
		t.Fatal("deleting the repository's own description succeeded")
	}
	// beta stays reachable whatever alpha does with its own rows.
	if _, err := svc.Dataset(ctx, "beta"); err != nil {
		t.Fatalf("repository beta became unreachable: %v", err)
	}
}
