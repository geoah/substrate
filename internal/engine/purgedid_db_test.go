package engine_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// A purged id is reserved. GC hard-deletes a tombstone, and until #372 it
// erased every trace of the id with it: a put at the same kind and id minted
// a fresh record, and every pointer that still named the old one resolved to
// the stranger. The purge now leaves the id in the former-id trail, denoting
// nothing, inside the purge effect the `gc` entry carries, so the live sweep
// and a rebuild agree on it.

func accountInput(id, label string) substrate.PutInput {
	return substrate.PutInput{
		Kind: enginetest.AccountType, ID: id,
		Properties: map[string]any{"provider": "testmirror", "label": label},
	}
}

func TestPurgedIDIsReserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, mirrorManifest()); err != nil {
		t.Fatalf("install mirror types: %v", err)
	}

	acc := mustPut(t, ds, owner, accountInput("acct-gone", "Gone"))
	// A referrer with no `onDelete:`, so its pointer outlives the purge.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: mirrorPackage + "/pointer", ID: "pointer-at-gone",
		Properties: map[string]any{"label": "still points", "account": acc.ID},
	})

	// Before the sweep the same put is the documented undelete: same id,
	// same row.
	if _, err := ds.Delete(ctx, owner, acc.Kind, acc.ID); err != nil {
		t.Fatal(err)
	}
	if restored := mustPut(t, ds, owner, accountInput(acc.ID, "Back")); restored.ID != acc.ID || restored.DeletedAt != nil {
		t.Fatalf("a put onto a tombstone must restore it, got %+v", restored)
	}

	// After it the id is gone for good.
	if _, err := ds.Delete(ctx, owner, acc.Kind, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := ds.Get(ctx, acc.Kind, acc.ID)
	wantErr(t, err, substrate.ErrNotFound, "the purged record")
	_, err = ds.Put(ctx, owner, accountInput(acc.ID, "Impostor"))
	wantErr(t, err, substrate.ErrConflict, "a put at a purged id")

	// The refused put minted nothing, so the pointer still names nothing.
	_, err = ds.Get(ctx, acc.Kind, acc.ID)
	wantErr(t, err, substrate.ErrNotFound, "the purged id after the refused put")
	// Any other id in the kind is still the writer's to name.
	mustPut(t, ds, owner, accountInput("acct-new", "New"))

	// The reservation is a fold effect on the gc entry, so a rebuild
	// reproduces it and refuses the same put.
	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); !bytes.Equal(before, after) {
		t.Fatal("the rebuild did not reproduce the reservation")
	}
	_, err = ds.Put(ctx, owner, accountInput(acc.ID, "Impostor"))
	wantErr(t, err, substrate.ErrConflict, "a put at a purged id after a rebuild")
}

// Purging a merge winner keeps its losers' trail rows: a loser's id is still
// a former id, so a put at it is refused rather than reviving the merged-away
// tombstone without a split.
func TestPurgedWinnerKeepsLoserReservation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	winner := mustPut(t, ds, owner, accountInput("acct-winner", "Winner"))
	loser := mustPut(t, ds, owner, accountInput("acct-loser", "Loser"))
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, winner.Kind, winner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := ds.Get(ctx, winner.Kind, winner.ID)
	wantErr(t, err, substrate.ErrNotFound, "the purged winner")

	_, err = ds.Put(ctx, owner, accountInput(loser.ID, "Revived"))
	wantErr(t, err, substrate.ErrConflict, "a put at the loser of a purged winner")
	_, err = ds.Put(ctx, owner, accountInput(winner.ID, "Revived"))
	wantErr(t, err, substrate.ErrConflict, "a put at the purged winner")

	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); !bytes.Equal(before, after) {
		t.Fatal("the rebuild did not reproduce the loser's trail")
	}
	_, err = ds.Put(ctx, owner, accountInput(loser.ID, "Revived"))
	wantErr(t, err, substrate.ErrConflict, "a put at the loser after a rebuild")
}

// The blob manifest is the one exemption: its id is the content digest, so
// the same bytes uploaded after their manifest was purged land at the same
// id. Not parallel: it writes the package-level grace.
func TestPurgedBlobDigestIsReusable(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	data := []byte("bytes that come back")
	first, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// The first sweep collects the bytes and tombstones the manifest; the
	// second purges the tombstone.
	for range 2 {
		if _, err := ds.RunGC(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_, err = ds.Get(ctx, "substrate.reamde.dev/core/blob", first.Digest)
	wantErr(t, err, substrate.ErrNotFound, "the purged manifest")

	again, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("re-upload after purge: %v", err)
	}
	if again.Digest != first.Digest {
		t.Fatalf("digest = %s, want %s", again.Digest, first.Digest)
	}
	if m := mustGet(t, ds, "substrate.reamde.dev/core/blob", first.Digest); m.DeletedAt != nil {
		t.Fatal("the re-uploaded manifest is a tombstone")
	}
	if _, _, err := bs.GetBlob(ctx, first.Digest); err != nil {
		t.Fatalf("re-uploaded bytes unreadable: %v", err)
	}
}
